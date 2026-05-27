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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	pkgv1beta1 "github.com/versioneer-tech/lakefs-oss-contrib/api/v1beta1"
	"github.com/versioneer-tech/lakefs-oss-contrib/internal/lakefs"
)

const lakeFSRepositoryFinalizer = "lakefs-oss-contrib.versioneer.at/repository"

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

	client, condition, err := r.repositoryClientForStatus(ctx, repository)
	if err != nil {
		return condition, nil
	}
	err = client.EnsureRepository(ctx, lakefs.RepositorySpec{
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

// SetupWithManager sets up the controller with the Manager.
func (r *LakeFSRepositoryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pkgv1beta1.LakeFSRepository{}).
		Named("lakefsrepository").
		Complete(r)
}
