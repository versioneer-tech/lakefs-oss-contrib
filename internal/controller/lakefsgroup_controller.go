// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	pkgv1beta1 "github.com/versioneer-tech/lakefs-oss-contrib/api/v1beta1"
)

// LakeFSGroupReconciler reconciles a LakeFSGroup object.
type LakeFSGroupReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsgroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsgroups/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsgroups/finalizers,verbs=update
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsusers,verbs=get;list;watch

func (r *LakeFSGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	group := &pkgv1beta1.LakeFSGroup{}
	if err := r.Get(ctx, req.NamespacedName, group); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	condition := r.resolveGroup(ctx, group)
	group.Status.ObservedGeneration = group.Generation
	metaSetStatusCondition(&group.Status.Conditions, condition)
	if err := r.Status().Update(ctx, group); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *LakeFSGroupReconciler) resolveGroup(ctx context.Context, group *pkgv1beta1.LakeFSGroup) metav1.Condition {
	for _, member := range group.Spec.Users {
		user := &pkgv1beta1.LakeFSUser{}
		key := types.NamespacedName{Namespace: group.Namespace, Name: member.Name}
		if err := r.Get(ctx, key, user); err != nil {
			return readyCondition(metav1.ConditionFalse, ReasonUnavailable, fmt.Sprintf("User %s is not available", key.String()), group.Generation)
		}
	}
	return readyCondition(metav1.ConditionTrue, ReasonResolved, "LakeFSGroup resolved all users", group.Generation)
}

// SetupWithManager sets up the controller with the Manager.
func (r *LakeFSGroupReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pkgv1beta1.LakeFSGroup{}).
		Named("lakefsgroup").
		Complete(r)
}
