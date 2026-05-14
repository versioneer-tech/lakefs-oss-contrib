// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LakeFSRepositoryCredentialSecretReference identifies a Secret containing lakeFS admin credentials.
type LakeFSRepositoryCredentialSecretReference struct {
	// name is the referenced Secret name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// namespace is the Secret namespace. Defaults to the LakeFSRepository namespace.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// accessKeyIdKey is the Secret data key for the lakeFS access key ID.
	// +optional
	// +kubebuilder:default:=accessKeyId
	AccessKeyIDKey string `json:"accessKeyIdKey,omitempty"`

	// secretAccessKeyKey is the Secret data key for the lakeFS secret access key.
	// +optional
	// +kubebuilder:default:=secretAccessKey
	SecretAccessKeyKey string `json:"secretAccessKeyKey,omitempty"`
}

// LakeFSRepositorySpec defines the desired state of LakeFSRepository
type LakeFSRepositorySpec struct {
	// repository is the lakeFS repository name. Defaults to metadata.name.
	// +optional
	Repository string `json:"repository,omitempty"`

	// endpoint is the lakeFS server base URL, for example http://lakefs.lakefs.svc:8000.
	// +kubebuilder:validation:MinLength=1
	Endpoint string `json:"endpoint"`

	// storageNamespace is the lakeFS backing storage namespace for the repository.
	// +kubebuilder:validation:MinLength=1
	StorageNamespace string `json:"storageNamespace"`

	// defaultBranch is the branch lakeFS should create with the repository.
	// +optional
	// +kubebuilder:default:=main
	DefaultBranch string `json:"defaultBranch,omitempty"`

	// sampleData controls whether lakeFS initializes sample data.
	// +optional
	SampleData bool `json:"sampleData,omitempty"`

	// credentialsSecretRef points to lakeFS admin credentials used to create the repository.
	CredentialsSecretRef LakeFSRepositoryCredentialSecretReference `json:"credentialsSecretRef"`
}

// LakeFSRepositoryStatus defines the observed state of LakeFSRepository.
type LakeFSRepositoryStatus struct {
	// observedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// repository is the resolved lakeFS repository name.
	// +optional
	Repository string `json:"repository,omitempty"`

	// conditions represent the current state of the LakeFSRepository resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Repository",type=string,JSONPath=".status.repository"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"

// LakeFSRepository is the Schema for the lakefsrepositories API
type LakeFSRepository struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of LakeFSRepository
	// +required
	Spec LakeFSRepositorySpec `json:"spec"`

	// status defines the observed state of LakeFSRepository
	// +optional
	Status LakeFSRepositoryStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LakeFSRepositoryList contains a list of LakeFSRepository
type LakeFSRepositoryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LakeFSRepository `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LakeFSRepository{}, &LakeFSRepositoryList{})
}

func (r *LakeFSRepository) RepositoryName() string {
	if r.Spec.Repository != "" {
		return r.Spec.Repository
	}
	return r.Name
}

func (r *LakeFSRepository) DefaultBranchName() string {
	if r.Spec.DefaultBranch != "" {
		return r.Spec.DefaultBranch
	}
	return "main"
}

func (r *LakeFSRepository) CredentialsSecretNamespace() string {
	if r.Spec.CredentialsSecretRef.Namespace != "" {
		return r.Spec.CredentialsSecretRef.Namespace
	}
	return r.Namespace
}

func (r *LakeFSRepositoryCredentialSecretReference) AccessKeyIDDataKey() string {
	if r.AccessKeyIDKey != "" {
		return r.AccessKeyIDKey
	}
	return "accessKeyId"
}

func (r *LakeFSRepositoryCredentialSecretReference) SecretAccessKeyDataKey() string {
	if r.SecretAccessKeyKey != "" {
		return r.SecretAccessKeyKey
	}
	return "secretAccessKey"
}
