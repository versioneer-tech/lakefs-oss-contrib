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

// LakeFSGCPolicyReconciler reconciles a LakeFSGCPolicy object.
type LakeFSGCPolicyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsgcpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsgcpolicies/status,verbs=get;update;patch

func (r *LakeFSGCPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	policy := &pkgv1beta1.LakeFSGCPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	policy.Status.ObservedGeneration = policy.Generation
	metaSetStatusCondition(&policy.Status.Conditions, r.readyCondition(policy))
	return ctrl.Result{}, r.Status().Update(ctx, policy)
}

func (r *LakeFSGCPolicyReconciler) readyCondition(policy *pkgv1beta1.LakeFSGCPolicy) metav1.Condition {
	if policy.Schedule() == "" {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSGCPolicy must set spec.schedule", policy.Generation)
	}
	if policy.DefaultRetentionDays() <= 0 {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSGCPolicy must set positive retention days", policy.Generation)
	}
	if policy.SparkImage() == "" {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSGCPolicy must resolve to a Spark image", policy.Generation)
	}
	if policy.SparkClassName() == "" {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSGCPolicy must resolve to a Spark class", policy.Generation)
	}
	if policy.SparkJarURL() == "" {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSGCPolicy must resolve to a Spark jar URL", policy.Generation)
	}
	return readyCondition(metav1.ConditionTrue, ReasonResolved, "LakeFSGCPolicy is available to repositories", policy.Generation)
}

// SetupWithManager sets up the controller with the Manager.
func (r *LakeFSGCPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pkgv1beta1.LakeFSGCPolicy{}).
		Named("lakefsgcpolicy").
		Complete(r)
}
