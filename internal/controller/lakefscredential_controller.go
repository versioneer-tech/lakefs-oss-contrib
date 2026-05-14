// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"crypto/rand"
	"encoding/base32"
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
)

// LakeFSCredentialReconciler reconciles a LakeFSCredential object
type LakeFSCredentialReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=pkg.internal,resources=lakefscredentials,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefscredentials/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefscredentials/finalizers,verbs=update
// +kubebuilder:rbac:groups=pkg.internal,resources=lakefsusers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

func (r *LakeFSCredentialReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	credential := &pkgv1beta1.LakeFSCredential{}
	if err := r.Get(ctx, req.NamespacedName, credential); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	condition := r.resolveCredential(ctx, credential)
	credential.Status.ObservedGeneration = credential.Generation
	credential.Status.SecretName = credential.Spec.SecretRef.Name
	credential.Status.SecretKey = credential.Spec.SecretKey()
	metaSetStatusCondition(&credential.Status.Conditions, condition)
	if err := r.Status().Update(ctx, credential); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *LakeFSCredentialReconciler) resolveCredential(ctx context.Context, credential *pkgv1beta1.LakeFSCredential) metav1.Condition {
	if credential.Spec.Revoked {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSCredential is revoked", credential.Generation)
	}
	if credential.Spec.ExpiresAt != nil && !credential.Spec.ExpiresAt.Time.After(time.Now()) {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSCredential is expired", credential.Generation)
	}
	if credential.Spec.AccessKeyID == "" {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, "LakeFSCredential must set spec.accessKeyId", credential.Generation)
	}

	user := &pkgv1beta1.LakeFSUser{}
	userKey := types.NamespacedName{Namespace: credential.Namespace, Name: credential.Spec.UserRef.Name}
	if err := r.Get(ctx, userKey, user); err != nil {
		return readyCondition(metav1.ConditionFalse, ReasonUnavailable, fmt.Sprintf("User %s is not available", userKey.String()), credential.Generation)
	}

	secretKey := types.NamespacedName{Namespace: credential.Namespace, Name: credential.Spec.SecretRef.Name}
	if err := r.ensureSecret(ctx, credential, secretKey); err != nil {
		return readyCondition(metav1.ConditionFalse, ReasonUnavailable, fmt.Sprintf("Secret %s could not be reconciled", secretKey.String()), credential.Generation)
	}

	secret := &corev1.Secret{}
	if err := r.Get(ctx, secretKey, secret); err != nil {
		return readyCondition(metav1.ConditionFalse, ReasonUnavailable, fmt.Sprintf("Secret %s is not available", secretKey.String()), credential.Generation)
	}
	if len(secret.Data[credential.Spec.SecretKey()]) == 0 {
		return readyCondition(metav1.ConditionFalse, ReasonInvalid, fmt.Sprintf("Secret %s is missing configured key", secretKey.String()), credential.Generation)
	}

	return readyCondition(metav1.ConditionTrue, ReasonResolved, "LakeFSCredential resolved user and Secret", credential.Generation)
}

func (r *LakeFSCredentialReconciler) ensureSecret(ctx context.Context, credential *pkgv1beta1.LakeFSCredential, secretKey types.NamespacedName) error {
	secret := &corev1.Secret{}
	if err := r.Get(ctx, secretKey, secret); err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretKey.Name,
				Namespace: secretKey.Namespace,
				Labels: map[string]string{
					"lakefs.versioneer.at/credential": "true",
					"lakefs.versioneer.at/user":       credential.Spec.UserRef.Name,
				},
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"accessKeyId":               []byte(credential.Spec.AccessKeyID),
				credential.Spec.SecretKey(): []byte("lakefs_sk_" + randomCredentialToken(32)),
			},
		}
		if err := controllerutil.SetControllerReference(credential, secret, r.Scheme); err != nil {
			return err
		}
		return r.Create(ctx, secret)
	}

	changed := false
	if secret.Data == nil {
		secret.Data = map[string][]byte{}
		changed = true
	}
	if string(secret.Data["accessKeyId"]) != credential.Spec.AccessKeyID {
		secret.Data["accessKeyId"] = []byte(credential.Spec.AccessKeyID)
		changed = true
	}
	if len(secret.Data[credential.Spec.SecretKey()]) == 0 {
		secret.Data[credential.Spec.SecretKey()] = []byte("lakefs_sk_" + randomCredentialToken(32))
		changed = true
	}
	if secret.Labels == nil {
		secret.Labels = map[string]string{}
		changed = true
	}
	if secret.Labels["lakefs.versioneer.at/credential"] != "true" {
		secret.Labels["lakefs.versioneer.at/credential"] = "true"
		changed = true
	}
	if secret.Labels["lakefs.versioneer.at/user"] != credential.Spec.UserRef.Name {
		secret.Labels["lakefs.versioneer.at/user"] = credential.Spec.UserRef.Name
		changed = true
	}
	owned := metav1.IsControlledBy(secret, credential)
	if err := controllerutil.SetControllerReference(credential, secret, r.Scheme); err != nil {
		return err
	}
	if !owned {
		changed = true
	}
	if changed {
		return r.Update(ctx, secret)
	}
	return nil
}

func randomCredentialToken(length int) string {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		panic(fmt.Sprintf("failed to generate credential token: %v", err))
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
}

// SetupWithManager sets up the controller with the Manager.
func (r *LakeFSCredentialReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&pkgv1beta1.LakeFSCredential{}).
		Named("lakefscredential").
		Complete(r)
}
