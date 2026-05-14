// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pkgv1beta1 "github.com/versioneer-tech/lakefs-oss-contrib/api/v1beta1"
)

var _ = Describe("LakeFSCredential Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		lakefscredential := &pkgv1beta1.LakeFSCredential{}

		BeforeEach(func() {
			user := &pkgv1beta1.LakeFSUser{
				ObjectMeta: metav1.ObjectMeta{Name: "test-user", Namespace: "default"},
				Spec: pkgv1beta1.LakeFSUserSpec{
					ExternalID: "test-user",
				},
			}
			err := k8sClient.Create(ctx, user)
			Expect(err == nil || errors.IsAlreadyExists(err)).To(BeTrue())

			By("creating the custom resource for the Kind LakeFSCredential")
			err = k8sClient.Get(ctx, typeNamespacedName, lakefscredential)
			if err != nil && errors.IsNotFound(err) {
				resource := &pkgv1beta1.LakeFSCredential{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: pkgv1beta1.LakeFSCredentialSpec{
						UserRef:     pkgv1beta1.LakeFSUserReference{Name: "test-user"},
						AccessKeyID: "lakefs_ak_test",
						SecretRef:   pkgv1beta1.LakeFSSecretReference{Name: "test-secret", Key: "secretAccessKey"},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &pkgv1beta1.LakeFSCredential{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance LakeFSCredential")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			Expect(k8sClient.Delete(ctx, &pkgv1beta1.LakeFSUser{
				ObjectMeta: metav1.ObjectMeta{Name: "test-user", Namespace: "default"},
			})).To(Succeed())
			err = k8sClient.Delete(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "test-secret", Namespace: "default"},
			})
			Expect(err == nil || errors.IsNotFound(err)).To(BeTrue())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &LakeFSCredentialReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			secret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-secret", Namespace: "default"}, secret)).To(Succeed())
			Expect(secret.Data).To(HaveKey("secretAccessKey"))
			Expect(secret.Data).To(HaveKeyWithValue("accessKeyId", []byte("lakefs_ak_test")))
		})
	})
})
