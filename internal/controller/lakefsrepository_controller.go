// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	pkgv1beta1 "github.com/versioneer-tech/lakefs-oss-contrib/api/v1beta1"
	"github.com/versioneer-tech/lakefs-oss-contrib/internal/lakefs"
)

const lakeFSRepositoryFinalizer = "lakefs-oss-contrib.versioneer.at/repository"
const lakeFSGCCronJobNameMaxLength = 52

// LakeFSRepositoryReconciler reconciles a LakeFSRepository object
type LakeFSRepositoryReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsrepositories/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsrepositories/finalizers,verbs=update
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsgcpolicies,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete

func (r *LakeFSRepositoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	repository := &pkgv1beta1.LakeFSRepository{}
	if err := r.Get(ctx, req.NamespacedName, repository); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !repository.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(repository, lakeFSRepositoryFinalizer) {
			return ctrl.Result{}, nil
		}
		if err := r.deleteRepository(ctx, repository); err != nil {
			return ctrl.Result{RequeueAfter: time.Minute}, err
		}
		controllerutil.RemoveFinalizer(repository, lakeFSRepositoryFinalizer)
		return ctrl.Result{}, r.Update(ctx, repository)
	}

	if !controllerutil.ContainsFinalizer(repository, lakeFSRepositoryFinalizer) {
		controllerutil.AddFinalizer(repository, lakeFSRepositoryFinalizer)
		if err := r.Update(ctx, repository); err != nil {
			return ctrl.Result{}, err
		}
	}

	condition, reconcileErr := r.ensureRepository(ctx, repository)
	repository.Status.ObservedGeneration = repository.Generation
	repository.Status.Repository = repository.RepositoryName()
	if repository.GCEnabled() {
		repository.Status.GCPolicy = repository.GCPolicyName()
		repository.Status.GCCronJob = gcCronJobName(repository)
	} else {
		repository.Status.GCPolicy = ""
		repository.Status.GCCronJob = ""
	}
	metaSetStatusCondition(&repository.Status.Conditions, condition)
	if err := r.Status().Update(ctx, repository); err != nil {
		return ctrl.Result{}, err
	}
	if reconcileErr != nil {
		return ctrl.Result{RequeueAfter: time.Minute}, reconcileErr
	}
	if condition.Status != metav1.ConditionTrue {
		return ctrl.Result{RequeueAfter: time.Minute}, nil
	}

	return ctrl.Result{}, nil
}

func (r *LakeFSRepositoryReconciler) deleteRepository(ctx context.Context, repository *pkgv1beta1.LakeFSRepository) error {
	repositoryName := repository.RepositoryName()
	if repositoryName == "" {
		return fmt.Errorf("LakeFSRepository must resolve to a repository name")
	}
	if repository.Spec.Endpoint == "" {
		return fmt.Errorf("LakeFSRepository must set spec.endpoint")
	}

	client, err := r.repositoryClient(ctx, repository)
	if err != nil {
		return err
	}
	return client.DeleteRepository(ctx, repositoryName)
}

func (r *LakeFSRepositoryReconciler) ensureRepository(ctx context.Context, repository *pkgv1beta1.LakeFSRepository) (metav1.Condition, error) {
	repositoryName := repository.RepositoryName()
	if repositoryName == "" {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSRepository must resolve to a repository name", repository.Generation), nil
	}
	if repository.Spec.Endpoint == "" {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSRepository must set spec.endpoint", repository.Generation), nil
	}
	if repository.Spec.StorageNamespace == "" {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSRepository must set spec.storageNamespace", repository.Generation), nil
	}

	lakeFSClient, condition, err := r.repositoryClientForStatus(ctx, repository)
	if err != nil {
		return condition, nil
	}
	err = lakeFSClient.EnsureRepository(ctx, lakefs.RepositorySpec{
		Name:             repositoryName,
		StorageNamespace: repository.Spec.StorageNamespace,
		DefaultBranch:    repository.DefaultBranchName(),
		SampleData:       repository.Spec.SampleData,
	})
	if err != nil {
		return readyCondition(metav1.ConditionFalse, ReasonUnavailable, "lakeFS repository could not be ensured", repository.Generation), err
	}

	if !repository.GCEnabled() {
		if err := r.deleteGCCronJob(ctx, repository); err != nil {
			return readyCondition(metav1.ConditionFalse, ReasonUnavailable, "managed GC CronJob could not be deleted", repository.Generation), err
		}
		return readyCondition(metav1.ConditionTrue, ReasonResolved, "lakeFS repository exists; managed GC is disabled", repository.Generation), nil
	}

	policy := &pkgv1beta1.LakeFSGCPolicy{}
	policyKey := types.NamespacedName{Namespace: repository.Namespace, Name: repository.GCPolicyName()}
	if err := r.Get(ctx, policyKey, policy); err != nil {
		if apierrors.IsNotFound(err) {
			if repository.GCPolicyName() == pkgv1beta1.DefaultGCPolicyName {
				policy = defaultGCPolicy(repository.Namespace)
				createErr := r.Create(ctx, policy)
				if createErr != nil && !apierrors.IsAlreadyExists(createErr) {
					return readyCondition(metav1.ConditionFalse, ReasonUnavailable, "default LakeFSGCPolicy could not be created", repository.Generation), createErr
				}
				if apierrors.IsAlreadyExists(createErr) {
					return readyCondition(metav1.ConditionFalse, ReasonUnavailable, "default LakeFSGCPolicy is being created", repository.Generation), nil
				}
			} else {
				return readyCondition(metav1.ConditionFalse, ReasonInvalid, fmt.Sprintf("LakeFSGCPolicy %q is not available in namespace %q", repository.GCPolicyName(), repository.Namespace), repository.Generation), nil
			}
		} else {
			return readyCondition(metav1.ConditionFalse, ReasonUnavailable, "LakeFSGCPolicy could not be read", repository.Generation), err
		}
	}

	if err := lakeFSClient.SetGCRules(ctx, repositoryName, gcRulesFromPolicy(policy)); err != nil {
		return readyCondition(metav1.ConditionFalse, ReasonUnavailable, "lakeFS GC retention rules could not be ensured", repository.Generation), err
	}
	if err := r.ensureGCCronJob(ctx, repository, policy); err != nil {
		return readyCondition(metav1.ConditionFalse, ReasonUnavailable, "managed GC CronJob could not be ensured", repository.Generation), err
	}

	return readyCondition(metav1.ConditionTrue, ReasonResolved, "lakeFS repository and managed GC exist", repository.Generation), nil
}

func (r *LakeFSRepositoryReconciler) repositoryClientForStatus(ctx context.Context, repository *pkgv1beta1.LakeFSRepository) (*lakefs.Client, metav1.Condition, error) {
	client, err := r.repositoryClient(ctx, repository)
	if err != nil {
		return nil, readyCondition(metav1.ConditionFalse, ReasonUnavailable, err.Error(), repository.Generation), err
	}
	return client, metav1.Condition{}, nil
}

func (r *LakeFSRepositoryReconciler) repositoryClient(ctx context.Context, repository *pkgv1beta1.LakeFSRepository) (*lakefs.Client, error) {
	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Namespace: repository.CredentialsSecretNamespace(),
		Name:      repository.Spec.CredentialsSecretRef.Name,
	}
	if err := r.Get(ctx, secretKey, secret); err != nil {
		return nil, fmt.Errorf("Credentials Secret %s is not available", secretKey.String())
	}

	accessKeyID := string(secret.Data[repository.Spec.CredentialsSecretRef.AccessKeyIDDataKey()])
	secretAccessKey := string(secret.Data[repository.Spec.CredentialsSecretRef.SecretAccessKeyDataKey()])
	if accessKeyID == "" || secretAccessKey == "" {
		return nil, fmt.Errorf("Credentials Secret %s is missing configured keys", secretKey.String())
	}

	return lakefs.NewClient(repository.Spec.Endpoint, accessKeyID, secretAccessKey, nil), nil
}

func (r *LakeFSRepositoryReconciler) ensureGCCronJob(ctx context.Context, repository *pkgv1beta1.LakeFSRepository, policy *pkgv1beta1.LakeFSGCPolicy) error {
	if repository.CredentialsSecretNamespace() != repository.Namespace {
		return fmt.Errorf("managed GC requires credentialsSecretRef Secret %s to be in LakeFSRepository namespace %s", repository.Spec.CredentialsSecretRef.Name, repository.Namespace)
	}

	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gcCronJobName(repository),
			Namespace: repository.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, cronJob, func() error {
		if err := controllerutil.SetControllerReference(repository, cronJob, r.Scheme); err != nil {
			return err
		}
		labels := gcLabels(repository, policy)
		cronJob.Labels = mergeStringMap(cronJob.Labels, labels)
		cronJob.Spec.Schedule = policy.Schedule()
		cronJob.Spec.Suspend = policy.Spec.Suspend
		cronJob.Spec.ConcurrencyPolicy = batchv1.ForbidConcurrent
		cronJob.Spec.SuccessfulJobsHistoryLimit = int32PtrValue(policy.Spec.Job.SuccessfulJobsHistoryLimit, pkgv1beta1.DefaultGCSuccessfulJobs)
		cronJob.Spec.FailedJobsHistoryLimit = int32PtrValue(policy.Spec.Job.FailedJobsHistoryLimit, pkgv1beta1.DefaultGCFailedJobs)
		cronJob.Spec.JobTemplate.ObjectMeta.Labels = labels
		cronJob.Spec.JobTemplate.Spec.BackoffLimit = int32PtrValue(policy.Spec.Job.BackoffLimit, pkgv1beta1.DefaultGCBackoffLimit)
		cronJob.Spec.JobTemplate.Spec.TTLSecondsAfterFinished = int32PtrValue(policy.Spec.Job.TTLSecondsAfterFinished, pkgv1beta1.DefaultGCTTLSeconds)
		cronJob.Spec.JobTemplate.Spec.Template.ObjectMeta.Labels = labels
		cronJob.Spec.JobTemplate.Spec.Template.Spec.RestartPolicy = corev1.RestartPolicyOnFailure
		cronJob.Spec.JobTemplate.Spec.Template.Spec.ServiceAccountName = policy.Spec.Job.ServiceAccountName
		cronJob.Spec.JobTemplate.Spec.Template.Spec.InitContainers = gcInitContainers(policy)
		cronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes = gcVolumes(policy)
		cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers = []corev1.Container{
			{
				Name:            "gc",
				Image:           policy.SparkImage(),
				ImagePullPolicy: policy.Spec.Job.ImagePullPolicy,
				Command:         policy.SparkCommand(),
				Args:            gcSparkArgs(repository, policy),
				Env:             append(gcLakeFSEnv(repository), policy.Spec.Job.Env...),
				EnvFrom:         policy.Spec.Job.EnvFrom,
				Resources:       policy.Spec.Job.Resources,
				VolumeMounts:    policy.Spec.Job.VolumeMounts,
			},
		}
		return nil
	})
	return err
}

func (r *LakeFSRepositoryReconciler) deleteGCCronJob(ctx context.Context, repository *pkgv1beta1.LakeFSRepository) error {
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gcCronJobName(repository),
			Namespace: repository.Namespace,
		},
	}
	return client.IgnoreNotFound(r.Delete(ctx, cronJob))
}

func gcRulesFromPolicy(policy *pkgv1beta1.LakeFSGCPolicy) lakefs.GCRules {
	branches := make([]lakefs.GCRule, 0, len(policy.Spec.Retention.Branches))
	for _, branch := range policy.Spec.Retention.Branches {
		branches = append(branches, lakefs.GCRule{
			BranchID:      branch.BranchID,
			RetentionDays: branch.RetentionDays,
		})
	}
	return lakefs.GCRules{
		DefaultRetentionDays: policy.DefaultRetentionDays(),
		Branches:             branches,
	}
}

func defaultGCPolicy(namespace string) *pkgv1beta1.LakeFSGCPolicy {
	return &pkgv1beta1.LakeFSGCPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: pkgv1beta1.DefaultGCPolicyName, Namespace: namespace},
		Spec: pkgv1beta1.LakeFSGCPolicySpec{
			Schedule: pkgv1beta1.DefaultGCSchedule,
			Retention: pkgv1beta1.LakeFSGCRetentionSpec{
				DefaultRetentionDays: pkgv1beta1.DefaultGCRetentionDays,
			},
		},
	}
}

func gcLakeFSEnv(repository *pkgv1beta1.LakeFSRepository) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "LAKEFS_ACCESS_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: repository.Spec.CredentialsSecretRef.Name},
					Key:                  repository.Spec.CredentialsSecretRef.AccessKeyIDDataKey(),
				},
			},
		},
		{
			Name: "LAKEFS_SECRET_ACCESS_KEY",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: repository.Spec.CredentialsSecretRef.Name},
					Key:                  repository.Spec.CredentialsSecretRef.SecretAccessKeyDataKey(),
				},
			},
		},
	}
}

func gcInitContainers(policy *pkgv1beta1.LakeFSGCPolicy) []corev1.Container {
	initContainers := make([]corev1.Container, 0, len(policy.Spec.Job.InitContainers))
	for _, item := range policy.Spec.Job.InitContainers {
		initContainers = append(initContainers, corev1.Container{
			Name:            item.Name,
			Image:           item.Image,
			ImagePullPolicy: item.ImagePullPolicy,
			Command:         item.Command,
			Args:            item.Args,
			Env:             item.Env,
			VolumeMounts:    item.VolumeMounts,
		})
	}
	return initContainers
}

func gcVolumes(policy *pkgv1beta1.LakeFSGCPolicy) []corev1.Volume {
	volumes := make([]corev1.Volume, 0, len(policy.Spec.Job.Volumes))
	for _, item := range policy.Spec.Job.Volumes {
		volume := corev1.Volume{Name: item.Name}
		if item.EmptyDir != nil {
			volume.EmptyDir = item.EmptyDir
		}
		volumes = append(volumes, volume)
	}
	return volumes
}

func gcSparkArgs(repository *pkgv1beta1.LakeFSRepository, policy *pkgv1beta1.LakeFSGCPolicy) []string {
	args := []string{"--class", policy.SparkClassName()}
	packages := policy.SparkPackages()
	if len(packages) > 0 {
		args = append(args, "--packages", strings.Join(packages, ","))
	}

	conf := make([]pkgv1beta1.LakeFSGCSparkConf, 0, 4+len(policy.Spec.Spark.Conf))
	if len(packages) > 0 {
		conf = append(conf, pkgv1beta1.LakeFSGCSparkConf{Name: "spark.jars.ivy", Value: pkgv1beta1.DefaultGCSparkIvyDir})
	}
	lakeFSAPIURL := strings.TrimRight(repository.Spec.Endpoint, "/") + "/api/v1"
	conf = append(conf, []pkgv1beta1.LakeFSGCSparkConf{
		{Name: "spark.hadoop.lakefs.api.url", Value: lakeFSAPIURL},
		{Name: "spark.hadoop.lakefs.api.access_key", Value: "$(LAKEFS_ACCESS_KEY)"},
		{Name: "spark.hadoop.lakefs.api.secret_key", Value: "$(LAKEFS_SECRET_ACCESS_KEY)"},
	}...)
	conf = append(conf, policy.Spec.Spark.Conf...)
	for _, item := range conf {
		args = append(args, "-c", item.Name+"="+item.Value)
	}

	args = append(args, policy.SparkJarURL(), repository.RepositoryName())
	args = append(args, policy.Spec.Spark.Args...)
	return args
}

func gcCronJobName(repository *pkgv1beta1.LakeFSRepository) string {
	name := repository.Name + "-gc"
	if len(name) <= lakeFSGCCronJobNameMaxLength {
		return name
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	suffix := fmt.Sprintf("%08x", hash.Sum32())
	prefixLength := lakeFSGCCronJobNameMaxLength - len(suffix) - 1
	return name[:prefixLength] + "-" + suffix
}

func gcLabels(repository *pkgv1beta1.LakeFSRepository, policy *pkgv1beta1.LakeFSGCPolicy) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":         "lakefs-oss-contrib",
		"app.kubernetes.io/component":    "gc",
		"lakefs.versioneer.at/repo-cr":   repository.Name,
		"lakefs.versioneer.at/gc-policy": policy.Name,
	}
}

func mergeStringMap(base, overlay map[string]string) map[string]string {
	if base == nil {
		base = map[string]string{}
	}
	for key, value := range overlay {
		base[key] = value
	}
	return base
}

func int32PtrValue(value *int32, fallback int32) *int32 {
	if value != nil {
		return value
	}
	return &fallback
}

func (r *LakeFSRepositoryReconciler) repositoriesForPolicy(ctx context.Context, object client.Object) []reconcile.Request {
	repositories := &pkgv1beta1.LakeFSRepositoryList{}
	if err := r.List(ctx, repositories, client.InNamespace(object.GetNamespace())); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0)
	for _, repository := range repositories.Items {
		if repository.GCEnabled() && repository.GCPolicyName() == object.GetName() {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: repository.Namespace,
					Name:      repository.Name,
				},
			})
		}
	}
	return requests
}

// SetupWithManager sets up the controller with the Manager.
func (r *LakeFSRepositoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pkgv1beta1.LakeFSRepository{}).
		Owns(&batchv1.CronJob{}).
		Watches(&pkgv1beta1.LakeFSGCPolicy{}, handler.EnqueueRequestsFromMapFunc(r.repositoriesForPolicy)).
		Named("lakefsrepository").
		Complete(r)
}
