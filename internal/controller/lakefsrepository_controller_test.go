// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	pkgv1beta1 "github.com/versioneer-tech/lakefs-oss-contrib/api/v1beta1"
)

var _ = Describe("LakeFSRepository Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		lakefsrepository := &pkgv1beta1.LakeFSRepository{}
		var lakeFSServer *httptest.Server

		BeforeEach(func() {
			lakeFSServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					w.WriteHeader(http.StatusNotFound)
				case http.MethodPost:
					w.WriteHeader(http.StatusCreated)
				default:
					w.WriteHeader(http.StatusMethodNotAllowed)
				}
			}))

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "lakefs-admin", Namespace: "default"},
				Type:       corev1.SecretTypeOpaque,
				Data: map[string][]byte{
					"accessKeyId":     []byte("admin-access-key"),
					"secretAccessKey": []byte("admin-secret-access-key"),
				},
			}
			err := k8sClient.Create(ctx, secret)
			Expect(err == nil || errors.IsAlreadyExists(err)).To(BeTrue())

			By("creating the custom resource for the Kind LakeFSRepository")
			err = k8sClient.Get(ctx, typeNamespacedName, lakefsrepository)
			if err != nil && errors.IsNotFound(err) {
				resource := &pkgv1beta1.LakeFSRepository{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: pkgv1beta1.LakeFSRepositorySpec{
						Endpoint:         lakeFSServer.URL,
						StorageNamespace: "s3://bucket/test-resource",
						CredentialsSecretRef: pkgv1beta1.LakeFSRepositoryCredentialSecretReference{
							Name: "lakefs-admin",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &pkgv1beta1.LakeFSRepository{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance LakeFSRepository")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			Expect(k8sClient.Delete(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "lakefs-admin", Namespace: "default"},
			})).To(Succeed())
			lakeFSServer.Close()
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &LakeFSRepositoryReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &pkgv1beta1.LakeFSRepository{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(updated.Status.ObservedGeneration).To(Equal(updated.Generation))
			Expect(updated.Status.Repository).To(Equal(resourceName))

			condition := meta.FindStatusCondition(updated.Status.Conditions, ConditionReady)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal(ReasonResolved))
		})
	})
})
