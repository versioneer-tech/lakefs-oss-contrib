// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		var lakeFSServerMu sync.Mutex
		var lakeFSRepositoryExists bool
		var lakeFSRequests []string
		var lakeFSGCRulesBody []byte

		BeforeEach(func() {
			lakeFSRepositoryExists = false
			lakeFSRequests = nil
			lakeFSGCRulesBody = nil

			lakeFSServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				lakeFSServerMu.Lock()
				defer lakeFSServerMu.Unlock()
				lakeFSRequests = append(lakeFSRequests, r.Method+" "+r.URL.RequestURI())

				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/repositories/test-resource":
					if lakeFSRepositoryExists {
						w.WriteHeader(http.StatusOK)
					} else {
						w.WriteHeader(http.StatusNotFound)
					}
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/repositories":
					lakeFSRepositoryExists = true
					w.WriteHeader(http.StatusCreated)
				case r.Method == http.MethodPut && r.URL.Path == "/api/v1/repositories/test-resource/settings/gc_rules":
					body, err := io.ReadAll(r.Body)
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					lakeFSGCRulesBody = body
					w.WriteHeader(http.StatusNoContent)
				case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/repositories/test-resource":
					if r.URL.Query().Get("force") != "true" {
						http.Error(w, "force query parameter is required", http.StatusBadRequest)
						return
					}
					lakeFSRepositoryExists = false
					w.WriteHeader(http.StatusNoContent)
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

			policy := &pkgv1beta1.LakeFSGCPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "default"},
				Spec: pkgv1beta1.LakeFSGCPolicySpec{
					Schedule: "0 3 * * *",
					Retention: pkgv1beta1.LakeFSGCRetentionSpec{
						DefaultRetentionDays: 14,
						Branches: []pkgv1beta1.LakeFSGCRetentionRule{
							{BranchID: "main", RetentionDays: 21},
						},
					},
					Spark: pkgv1beta1.LakeFSGCSparkSpec{
						Args: []string{"us-east-1"},
					},
					Job: pkgv1beta1.LakeFSGCJobSpec{
						InitContainers: []pkgv1beta1.LakeFSGCInitContainerSpec{{Name: "stage-client", Image: "curlimages/curl:latest"}},
						VolumeMounts:   []corev1.VolumeMount{{Name: "gc-client", MountPath: "/opt/lakefs-gc", ReadOnly: true}},
						Volumes:        []pkgv1beta1.LakeFSGCVolumeSpec{{Name: "gc-client", EmptyDir: &corev1.EmptyDirVolumeSource{}}},
					},
				},
			}
			err = k8sClient.Create(ctx, policy)
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
			if err == nil {
				By("Cleanup the specific resource instance LakeFSRepository")
				if resource.DeletionTimestamp.IsZero() {
					Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
				}
				Eventually(func() bool {
					cleanupResource := &pkgv1beta1.LakeFSRepository{}
					getErr := k8sClient.Get(ctx, typeNamespacedName, cleanupResource)
					if errors.IsNotFound(getErr) {
						return true
					}
					if getErr != nil {
						return false
					}
					cleanupResource.Finalizers = nil
					return k8sClient.Update(ctx, cleanupResource) == nil
				}).Should(BeTrue())
			} else {
				Expect(errors.IsNotFound(err)).To(BeTrue())
			}

			err = k8sClient.Delete(ctx, &pkgv1beta1.LakeFSGCPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "default"},
			})
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			err = k8sClient.Delete(ctx, &batchv1.CronJob{
				ObjectMeta: metav1.ObjectMeta{Name: "test-resource-gc", Namespace: "default"},
			})
			Expect(client.IgnoreNotFound(err)).To(Succeed())

			err = k8sClient.Delete(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "lakefs-admin", Namespace: "default"},
			})
			Expect(client.IgnoreNotFound(err)).To(Succeed())
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
			Expect(updated.Status.GCPolicy).To(Equal("default"))
			Expect(updated.Status.GCCronJob).To(Equal("test-resource-gc"))

			condition := meta.FindStatusCondition(updated.Status.Conditions, ConditionReady)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionTrue))
			Expect(condition.Reason).To(Equal(ReasonResolved))
			Expect(updated.Finalizers).To(ContainElement(lakeFSRepositoryFinalizer))

			cronJob := &batchv1.CronJob{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-resource-gc", Namespace: "default"}, cronJob)).To(Succeed())
			Expect(cronJob.Spec.Schedule).To(Equal("0 3 * * *"))
			Expect(cronJob.Spec.ConcurrencyPolicy).To(Equal(batchv1.ForbidConcurrent))
			Expect(cronJob.Spec.JobTemplate.ObjectMeta.Labels).To(HaveKeyWithValue("lakefs.versioneer.at/repo-cr", resourceName))
			Expect(cronJob.Spec.JobTemplate.Spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyOnFailure))
			Expect(cronJob.Spec.JobTemplate.Spec.Template.Spec.InitContainers).To(HaveLen(1))
			Expect(cronJob.Spec.JobTemplate.Spec.Template.Spec.InitContainers[0].Name).To(Equal("stage-client"))
			Expect(cronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes).To(HaveLen(1))
			Expect(cronJob.Spec.JobTemplate.Spec.Template.Spec.Volumes[0].Name).To(Equal("gc-client"))
			Expect(cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers).To(HaveLen(1))

			container := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
			Expect(container.Name).To(Equal("gc"))
			Expect(container.Image).To(Equal(pkgv1beta1.DefaultGCSparkImage))
			Expect(container.Command).To(Equal([]string{pkgv1beta1.DefaultGCSparkCommand}))
			Expect(container.VolumeMounts).To(ConsistOf(corev1.VolumeMount{Name: "gc-client", MountPath: "/opt/lakefs-gc", ReadOnly: true}))
			Expect(container.Args).To(ContainElements(
				"--class",
				pkgv1beta1.DefaultGCSparkClass,
				"-c",
				"spark.hadoop.lakefs.api.url="+lakeFSServer.URL+"/api/v1",
				"spark.hadoop.lakefs.api.access_key=$(LAKEFS_ACCESS_KEY)",
				"spark.hadoop.lakefs.api.secret_key=$(LAKEFS_SECRET_ACCESS_KEY)",
				pkgv1beta1.DefaultGCSparkJarURL,
				resourceName,
				"us-east-1",
			))
			Expect(container.Env).To(HaveLen(2))
			Expect(container.Env[0].Name).To(Equal("LAKEFS_ACCESS_KEY"))
			Expect(container.Env[0].ValueFrom.SecretKeyRef.Name).To(Equal("lakefs-admin"))
			Expect(container.Env[0].ValueFrom.SecretKeyRef.Key).To(Equal("accessKeyId"))
			Expect(container.Env[1].Name).To(Equal("LAKEFS_SECRET_ACCESS_KEY"))
			Expect(container.Env[1].ValueFrom.SecretKeyRef.Name).To(Equal("lakefs-admin"))
			Expect(container.Env[1].ValueFrom.SecretKeyRef.Key).To(Equal("secretAccessKey"))

			lakeFSServerMu.Lock()
			defer lakeFSServerMu.Unlock()
			Expect(lakeFSRequests).To(ContainElement("PUT /api/v1/repositories/test-resource/settings/gc_rules"))
			var rules map[string]any
			Expect(json.Unmarshal(lakeFSGCRulesBody, &rules)).To(Succeed())
			Expect(rules["default_retention_days"]).To(BeEquivalentTo(14))
			Expect(rules["branches"]).To(ConsistOf(map[string]any{"branch_id": "main", "retention_days": float64(21)}))
		})

		It("should include explicit Spark packages and writable Ivy configuration", func() {
			policy := &pkgv1beta1.LakeFSGCPolicy{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default", Namespace: "default"}, policy)).To(Succeed())
			policy.Spec.Spark.Packages = []string{"org.example:custom-package:1.2.3"}
			Expect(k8sClient.Update(ctx, policy)).To(Succeed())

			controllerReconciler := &LakeFSRepositoryReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			cronJob := &batchv1.CronJob{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-resource-gc", Namespace: "default"}, cronJob)).To(Succeed())
			container := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
			Expect(container.Args).To(ContainElements(
				"--packages",
				"org.example:custom-package:1.2.3",
				"-c",
				"spark.jars.ivy="+pkgv1beta1.DefaultGCSparkIvyDir,
			))
		})

		It("should send an empty GC branch list when no branch-specific retention is configured", func() {
			policy := &pkgv1beta1.LakeFSGCPolicy{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default", Namespace: "default"}, policy)).To(Succeed())
			policy.Spec.Retention.DefaultRetentionDays = 7
			policy.Spec.Retention.Branches = nil
			Expect(k8sClient.Update(ctx, policy)).To(Succeed())

			controllerReconciler := &LakeFSRepositoryReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			lakeFSServerMu.Lock()
			defer lakeFSServerMu.Unlock()
			var rules map[string]any
			Expect(json.Unmarshal(lakeFSGCRulesBody, &rules)).To(Succeed())
			Expect(rules["default_retention_days"]).To(BeEquivalentTo(7))
			Expect(rules).To(HaveKey("branches"))
			Expect(rules["branches"]).To(BeEmpty())
		})

		It("should remove the managed GC CronJob when GC is disabled", func() {
			controllerReconciler := &LakeFSRepositoryReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling the created resource")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			cronJob := &batchv1.CronJob{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-resource-gc", Namespace: "default"}, cronJob)).To(Succeed())

			resource := &pkgv1beta1.LakeFSRepository{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			disabled := false
			resource.Spec.GC.Enabled = &disabled
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			By("Reconciling with managed GC disabled")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-resource-gc", Namespace: "default"}, cronJob)
			Expect(errors.IsNotFound(err)).To(BeTrue())

			updated := &pkgv1beta1.LakeFSRepository{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			Expect(updated.Status.GCPolicy).To(BeEmpty())
			Expect(updated.Status.GCCronJob).To(BeEmpty())
		})

		It("should create the default GC policy on demand", func() {
			Expect(k8sClient.Delete(ctx, &pkgv1beta1.LakeFSGCPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "default", Namespace: "default"},
			})).To(Succeed())

			controllerReconciler := &LakeFSRepositoryReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling the created resource without an existing default GC policy")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			policy := &pkgv1beta1.LakeFSGCPolicy{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "default", Namespace: "default"}, policy)).To(Succeed())
			Expect(policy.Spec.Schedule).To(Equal(pkgv1beta1.DefaultGCSchedule))
			Expect(policy.Spec.Retention.DefaultRetentionDays).To(Equal(pkgv1beta1.DefaultGCRetentionDays))
			Expect(policy.Spec.Job.Image).To(BeEmpty())
			Expect(policy.Spec.Job.Command).To(BeEmpty())
			Expect(policy.Spec.Spark.ClassName).To(BeEmpty())
			Expect(policy.Spec.Spark.JarURL).To(BeEmpty())
			Expect(policy.Spec.Spark.Packages).To(BeEmpty())

			cronJob := &batchv1.CronJob{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-resource-gc", Namespace: "default"}, cronJob)).To(Succeed())
			container := cronJob.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(Equal(pkgv1beta1.DefaultGCSparkImage))
			Expect(container.Command).To(Equal([]string{pkgv1beta1.DefaultGCSparkCommand}))
			Expect(container.Args).To(ContainElements(
				"--class",
				pkgv1beta1.DefaultGCSparkClass,
				pkgv1beta1.DefaultGCSparkJarURL,
			))
		})

		It("should not resolve GC policies from another namespace", func() {
			otherNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "other"}}
			err := k8sClient.Create(ctx, otherNamespace)
			Expect(err == nil || errors.IsAlreadyExists(err)).To(BeTrue())

			otherPolicy := &pkgv1beta1.LakeFSGCPolicy{
				ObjectMeta: metav1.ObjectMeta{Name: "custom", Namespace: "other"},
				Spec: pkgv1beta1.LakeFSGCPolicySpec{
					Schedule: "0 1 * * *",
				},
			}
			Expect(k8sClient.Create(ctx, otherPolicy)).To(Succeed())
			DeferCleanup(func() {
				Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, otherPolicy))).To(Succeed())
			})

			resource := &pkgv1beta1.LakeFSRepository{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			resource.Spec.GC.PolicyRef.Name = "custom"
			Expect(k8sClient.Update(ctx, resource)).To(Succeed())

			controllerReconciler := &LakeFSRepositoryReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			updated := &pkgv1beta1.LakeFSRepository{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updated)).To(Succeed())
			condition := meta.FindStatusCondition(updated.Status.Conditions, ConditionReady)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Status).To(Equal(metav1.ConditionFalse))
			Expect(condition.Reason).To(Equal(ReasonInvalid))
			Expect(condition.Message).To(ContainSubstring(`LakeFSGCPolicy "custom" is not available in namespace "default"`))

			cronJob := &batchv1.CronJob{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-resource-gc", Namespace: "default"}, cronJob)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})

		It("should delete the lakeFS repository before removing the finalizer", func() {
			controllerReconciler := &LakeFSRepositoryReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("Reconciling the created resource")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			resource := &pkgv1beta1.LakeFSRepository{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())
			Expect(resource.Finalizers).To(ContainElement(lakeFSRepositoryFinalizer))

			By("Deleting the LakeFSRepository custom resource")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Reconciling the deleting resource")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Confirming the Kubernetes resource disappeared")
			Eventually(func() bool {
				deleted := &pkgv1beta1.LakeFSRepository{}
				return errors.IsNotFound(k8sClient.Get(ctx, typeNamespacedName, deleted))
			}).Should(BeTrue())

			lakeFSServerMu.Lock()
			defer lakeFSServerMu.Unlock()
			Expect(lakeFSRequests).To(ContainElement("DELETE /api/v1/repositories/test-resource?force=true"))
		})
	})
})
