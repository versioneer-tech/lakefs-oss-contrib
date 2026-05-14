// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pkgv1beta1 "github.com/versioneer-tech/lakefs-oss-contrib/api/v1beta1"
	"github.com/versioneer-tech/lakefs-oss-contrib/internal/lakefs"
)

// LakeFSRepositoryReconciler reconciles a LakeFSRepository object
type LakeFSRepositoryReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsrepositories/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsrepositories/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *LakeFSRepositoryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	repository := &pkgv1beta1.LakeFSRepository{}
	if err := r.Get(ctx, req.NamespacedName, repository); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	condition, reconcileErr := r.ensureRepository(ctx, repository)
	repository.Status.ObservedGeneration = repository.Generation
	repository.Status.Repository = repository.RepositoryName()
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

	secret := &corev1.Secret{}
	secretKey := types.NamespacedName{
		Namespace: repository.CredentialsSecretNamespace(),
		Name:      repository.Spec.CredentialsSecretRef.Name,
	}
	if err := r.Get(ctx, secretKey, secret); err != nil {
		return readyCondition(metav1.ConditionFalse, ReasonUnavailable, fmt.Sprintf("Credentials Secret %s is not available", secretKey.String()), repository.Generation), nil
	}

	accessKeyID := string(secret.Data[repository.Spec.CredentialsSecretRef.AccessKeyIDDataKey()])
	secretAccessKey := string(secret.Data[repository.Spec.CredentialsSecretRef.SecretAccessKeyDataKey()])
	if accessKeyID == "" || secretAccessKey == "" {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, fmt.Sprintf("Credentials Secret %s is missing configured keys", secretKey.String()), repository.Generation), nil
	}

	client := lakefs.NewClient(repository.Spec.Endpoint, accessKeyID, secretAccessKey, nil)
	err := client.EnsureRepository(ctx, lakefs.RepositorySpec{
		Name:             repositoryName,
		StorageNamespace: repository.Spec.StorageNamespace,
		DefaultBranch:    repository.DefaultBranchName(),
		SampleData:       repository.Spec.SampleData,
	})
	if err != nil {
		return readyCondition(metav1.ConditionFalse, ReasonUnavailable, "lakeFS repository could not be ensured", repository.Generation), err
	}

	return readyCondition(metav1.ConditionTrue, ReasonResolved, "lakeFS repository exists", repository.Generation), nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LakeFSRepositoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pkgv1beta1.LakeFSRepository{}).
		Named("lakefsrepository").
		Complete(r)
}
