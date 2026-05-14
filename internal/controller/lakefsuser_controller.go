// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pkgv1beta1 "github.com/versioneer-tech/lakefs-oss-contrib/api/v1beta1"
)

// LakeFSUserReconciler reconciles a LakeFSUser object
type LakeFSUserReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsusers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsusers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsusers/finalizers,verbs=update

func (r *LakeFSUserReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	user := &pkgv1beta1.LakeFSUser{}
	if err := r.Get(ctx, req.NamespacedName, user); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	username := lakeFSUserUsername(user)
	condition := readyCondition(metav1.ConditionTrue, ReasonResolved, "LakeFSUser is available to the auth server", user.Generation)
	if username == "" {
		condition = readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSUser must resolve to a lakeFS username", user.Generation)
	}
	user.Status.ObservedGeneration = user.Generation
	metaSetStatusCondition(&user.Status.Conditions, condition)
	if err := r.Status().Update(ctx, user); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LakeFSUserReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pkgv1beta1.LakeFSUser{}).
		Named("lakefsuser").
		Complete(r)
}
