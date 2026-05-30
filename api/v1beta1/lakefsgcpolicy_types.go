// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	DefaultGCSchedule       = "0 3 * * *"
	DefaultGCRetentionDays  = int32(14)
	DefaultGCSparkImage     = "ghcr.io/versioneer-tech/lakefs-oss-contrib/gc-spark:latest"
	DefaultGCSparkCommand   = "/opt/spark/bin/spark-submit"
	DefaultGCSparkJarURL    = "/opt/lakefs-gc/lakefs-spark-client.jar"
	DefaultGCSparkClass     = "io.treeverse.gc.GarbageCollection"
	DefaultGCSparkIvyDir    = "/tmp/.ivy2"
	DefaultGCPolicyName     = "default"
	DefaultGCSuccessfulJobs = int32(1)
	DefaultGCFailedJobs     = int32(3)
	DefaultGCBackoffLimit   = int32(1)
	DefaultGCTTLSeconds     = int32(86400)
)

// LakeFSGCPolicyReference points at a LakeFSGCPolicy in the same namespace as the LakeFSRepository.
type LakeFSGCPolicyReference struct {
	// name is the referenced LakeFSGCPolicy.
	// +optional
	// +kubebuilder:default:=default
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name,omitempty"`
}

// LakeFSRepositoryGCSpec configures managed garbage collection for a repository.
type LakeFSRepositoryGCSpec struct {
	// enabled controls whether the operator manages lakeFS GC retention rules and CronJob.
	// +optional
	// +kubebuilder:default:=true
	Enabled *bool `json:"enabled,omitempty"`

	// policyRef selects the namespaced GC policy used by this repository.
	// +optional
	// +kubebuilder:default:={name:default}
	PolicyRef LakeFSGCPolicyReference `json:"policyRef,omitempty"`
}

// LakeFSGCRetentionRule configures retention for a single branch.
type LakeFSGCRetentionRule struct {
	// branchId is the lakeFS branch id the rule applies to.
	// +kubebuilder:validation:MinLength=1
	BranchID string `json:"branchId"`

	// retentionDays is the number of days to retain deleted or replaced objects for this branch.
	// +kubebuilder:validation:Minimum=1
	RetentionDays int32 `json:"retentionDays"`
}

// LakeFSGCRetentionSpec configures lakeFS garbage collection retention rules.
type LakeFSGCRetentionSpec struct {
	// defaultRetentionDays is used when no branch-specific rule applies.
	// +optional
	// +kubebuilder:default:=14
	// +kubebuilder:validation:Minimum=1
	DefaultRetentionDays int32 `json:"defaultRetentionDays,omitempty"`

	// branches contains branch-specific retention overrides.
	// +optional
	// +listType=map
	// +listMapKey=branchId
	Branches []LakeFSGCRetentionRule `json:"branches,omitempty"`
}

// LakeFSGCSparkConf configures a Spark property passed as "-c name=value".
type LakeFSGCSparkConf struct {
	// name is the Spark configuration key.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// value is the Spark configuration value. Kubernetes-style $(VAR_NAME)
	// references are expanded by the container runtime from the Job environment.
	Value string `json:"value"`
}

// LakeFSGCSparkSpec configures the spark-submit invocation.
type LakeFSGCSparkSpec struct {
	// className is the Spark class to run.
	// +optional
	ClassName string `json:"className,omitempty"`

	// jarURL is the lakeFS Spark client assembly jar URL or local path.
	// +optional
	JarURL string `json:"jarURL,omitempty"`

	// packages are passed to spark-submit as a comma-separated --packages value.
	// +optional
	Packages []string `json:"packages,omitempty"`

	// conf contains additional Spark configuration entries.
	// +optional
	// +listType=map
	// +listMapKey=name
	Conf []LakeFSGCSparkConf `json:"conf,omitempty"`

	// args are appended after the repository argument in the spark-submit command.
	// Use this for provider-specific positional arguments such as an AWS region.
	// +optional
	Args []string `json:"args,omitempty"`
}

// LakeFSGCInitContainerSpec configures a lightweight init container for the GC Job pod.
type LakeFSGCInitContainerSpec struct {
	// name is the init container name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// image is the init container image.
	// +kubebuilder:validation:MinLength=1
	Image string `json:"image"`

	// imagePullPolicy is the image pull policy for the init container.
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// command is the init container command.
	// +optional
	Command []string `json:"command,omitempty"`

	// args are the init container arguments.
	// +optional
	Args []string `json:"args,omitempty"`

	// env contains environment variables for the init container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// volumeMounts are added to the init container.
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`
}

// LakeFSGCVolumeSpec configures a lightweight pod volume for the GC Job pod.
type LakeFSGCVolumeSpec struct {
	// name is the volume name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// emptyDir configures an emptyDir volume.
	// +optional
	EmptyDir *corev1.EmptyDirVolumeSource `json:"emptyDir,omitempty"`
}

// LakeFSGCJobSpec configures the Kubernetes CronJob pod.
type LakeFSGCJobSpec struct {
	// image is the container image that provides spark-submit.
	// +optional
	Image string `json:"image,omitempty"`

	// command is the container command used to invoke spark-submit.
	// Override this when the image exposes spark-submit at a different path.
	// +optional
	Command []string `json:"command,omitempty"`

	// imagePullPolicy is the image pull policy for the GC container.
	// +optional
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// serviceAccountName is assigned to GC Job pods when set.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// initContainers are copied to the GC Job pod.
	// +optional
	InitContainers []LakeFSGCInitContainerSpec `json:"initContainers,omitempty"`

	// env contains additional environment variables for the GC container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// envFrom imports additional environment variables for the GC container.
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// resources configures GC container resource requests and limits.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// volumeMounts are added to the GC container.
	// +optional
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// volumes are copied to the GC Job pod.
	// +optional
	Volumes []LakeFSGCVolumeSpec `json:"volumes,omitempty"`

	// successfulJobsHistoryLimit is copied to the CronJob.
	// +optional
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit,omitempty"`

	// failedJobsHistoryLimit is copied to the CronJob.
	// +optional
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty"`

	// backoffLimit is copied to the Job template.
	// +optional
	BackoffLimit *int32 `json:"backoffLimit,omitempty"`

	// ttlSecondsAfterFinished is copied to the Job template when set.
	// +optional
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`
}

// LakeFSGCPolicySpec defines the desired state of LakeFSGCPolicy.
type LakeFSGCPolicySpec struct {
	// schedule is the CronJob schedule used to run lakeFS GC.
	// +optional
	// +kubebuilder:default:="0 3 * * *"
	// +kubebuilder:validation:MinLength=1
	Schedule string `json:"schedule,omitempty"`

	// suspend is copied to the CronJob suspend field.
	// +optional
	Suspend *bool `json:"suspend,omitempty"`

	// retention configures lakeFS GC retention rules for repositories using this policy.
	// +optional
	Retention LakeFSGCRetentionSpec `json:"retention,omitempty"`

	// spark configures spark-submit.
	// +optional
	Spark LakeFSGCSparkSpec `json:"spark,omitempty"`

	// job configures the Kubernetes CronJob pod.
	// +optional
	Job LakeFSGCJobSpec `json:"job,omitempty"`
}

// LakeFSGCPolicyStatus defines the observed state of LakeFSGCPolicy.
type LakeFSGCPolicyStatus struct {
	// observedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the LakeFSGCPolicy resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Schedule",type=string,JSONPath=".spec.schedule"
// +kubebuilder:printcolumn:name="Retention",type=integer,JSONPath=".spec.retention.defaultRetentionDays"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"

// LakeFSGCPolicy is the Schema for the lakefsgcpolicies API.
type LakeFSGCPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of LakeFSGCPolicy.
	// +required
	Spec LakeFSGCPolicySpec `json:"spec"`

	// status defines the observed state of LakeFSGCPolicy.
	// +optional
	Status LakeFSGCPolicyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LakeFSGCPolicyList contains a list of LakeFSGCPolicy.
type LakeFSGCPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LakeFSGCPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LakeFSGCPolicy{}, &LakeFSGCPolicyList{})
}

func (p *LakeFSGCPolicy) Schedule() string {
	if p.Spec.Schedule != "" {
		return p.Spec.Schedule
	}
	return DefaultGCSchedule
}

func (p *LakeFSGCPolicy) DefaultRetentionDays() int32 {
	if p.Spec.Retention.DefaultRetentionDays > 0 {
		return p.Spec.Retention.DefaultRetentionDays
	}
	return DefaultGCRetentionDays
}

func (p *LakeFSGCPolicy) SparkImage() string {
	if p.Spec.Job.Image != "" {
		return p.Spec.Job.Image
	}
	return DefaultGCSparkImage
}

func (p *LakeFSGCPolicy) SparkCommand() []string {
	if len(p.Spec.Job.Command) > 0 {
		return p.Spec.Job.Command
	}
	return []string{DefaultGCSparkCommand}
}

func (p *LakeFSGCPolicy) SparkClassName() string {
	if p.Spec.Spark.ClassName != "" {
		return p.Spec.Spark.ClassName
	}
	return DefaultGCSparkClass
}

func (p *LakeFSGCPolicy) SparkJarURL() string {
	if p.Spec.Spark.JarURL != "" {
		return p.Spec.Spark.JarURL
	}
	return DefaultGCSparkJarURL
}

func (p *LakeFSGCPolicy) SparkPackages() []string {
	if len(p.Spec.Spark.Packages) > 0 {
		return p.Spec.Spark.Packages
	}
	return nil
}
