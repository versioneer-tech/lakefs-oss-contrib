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

// LakeFSRoleBindingReconciler reconciles a LakeFSRoleBinding object.
type LakeFSRoleBindingReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsrolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsrolebindings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsrolebindings/finalizers,verbs=update
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsusers,verbs=get;list;watch
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsgroups,verbs=get;list;watch
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsroles,verbs=get;list;watch

func (r *LakeFSRoleBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	binding := &pkgv1beta1.LakeFSRoleBinding{}
	if err := r.Get(ctx, req.NamespacedName, binding); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	condition := r.resolveRoleBinding(ctx, binding)
	binding.Status.ObservedGeneration = binding.Generation
	metaSetStatusCondition(&binding.Status.Conditions, condition)
	if err := r.Status().Update(ctx, binding); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *LakeFSRoleBindingReconciler) resolveRoleBinding(ctx context.Context, binding *pkgv1beta1.LakeFSRoleBinding) metav1.Condition {
	roleKey := types.NamespacedName{Namespace: binding.Namespace, Name: binding.Spec.RoleRef.Name}
	role := &pkgv1beta1.LakeFSRole{}
	if err := r.Get(ctx, roleKey, role); err != nil {
		return readyCondition(metav1.ConditionFalse, ReasonUnavailable, fmt.Sprintf("Role %s is not available", roleKey.String()), binding.Generation)
	}
	if binding.Spec.Repository == "" {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSRoleBinding must set spec.repository", binding.Generation)
	}

	subjectKey := types.NamespacedName{Namespace: binding.Namespace, Name: binding.Spec.Subject.Name}
	switch binding.Spec.Subject.Kind {
	case pkgv1beta1.LakeFSRoleBindingSubjectKindUser:
		user := &pkgv1beta1.LakeFSUser{}
		if err := r.Get(ctx, subjectKey, user); err != nil {
			return readyCondition(metav1.ConditionFalse, ReasonUnavailable, fmt.Sprintf("User %s is not available", subjectKey.String()), binding.Generation)
		}
	case pkgv1beta1.LakeFSRoleBindingSubjectKindGroup:
		group := &pkgv1beta1.LakeFSGroup{}
		if err := r.Get(ctx, subjectKey, group); err != nil {
			return readyCondition(metav1.ConditionFalse, ReasonUnavailable, fmt.Sprintf("Group %s is not available", subjectKey.String()), binding.Generation)
		}
	default:
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSRoleBinding subject kind must be LakeFSUser or LakeFSGroup", binding.Generation)
	}

	return readyCondition(metav1.ConditionTrue, ReasonResolved, "LakeFSRoleBinding resolved subject and role", binding.Generation)
}

// SetupWithManager sets up the controller with the Manager.
func (r *LakeFSRoleBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pkgv1beta1.LakeFSRoleBinding{}).
		Named("lakefsrolebinding").
		Complete(r)
}
