// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LakeFSRoleBindingSubjectKind identifies the kind of subject granted a role.
// +kubebuilder:validation:Enum=LakeFSUser;LakeFSGroup
type LakeFSRoleBindingSubjectKind string

const (
	LakeFSRoleBindingSubjectKindUser  LakeFSRoleBindingSubjectKind = "LakeFSUser"
	LakeFSRoleBindingSubjectKindGroup LakeFSRoleBindingSubjectKind = "LakeFSGroup"
)

// LakeFSRoleBindingSubject identifies the user or group receiving a role.
type LakeFSRoleBindingSubject struct {
	// kind is either LakeFSUser or LakeFSGroup.
	Kind LakeFSRoleBindingSubjectKind `json:"kind"`

	// name is the referenced LakeFSUser or LakeFSGroup name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// LakeFSRoleReference points to a LakeFSRole in the same namespace.
type LakeFSRoleReference struct {
	// name is the referenced role name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// LakeFSRoleBindingSpec defines the desired state of LakeFSRoleBinding.
type LakeFSRoleBindingSpec struct {
	// subject identifies the user or group receiving the role.
	Subject LakeFSRoleBindingSubject `json:"subject"`

	// roleRef points to the LakeFSRole being granted.
	RoleRef LakeFSRoleReference `json:"roleRef"`

	// repository scopes this binding to a lakeFS repository. Use "*" for cluster-wide policies.
	// Templates in the referenced role may use <REPOSITORY>, which is replaced by this value.
	// +kubebuilder:validation:MinLength=1
	Repository string `json:"repository"`
}

// LakeFSRoleBindingStatus defines the observed state of LakeFSRoleBinding.
type LakeFSRoleBindingStatus struct {
	// observedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the LakeFSRoleBinding resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Subject",type=string,JSONPath=".spec.subject.name"
// +kubebuilder:printcolumn:name="Kind",type=string,JSONPath=".spec.subject.kind"
// +kubebuilder:printcolumn:name="Role",type=string,JSONPath=".spec.roleRef.name"
// +kubebuilder:printcolumn:name="Repository",type=string,JSONPath=".spec.repository"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"

// LakeFSRoleBinding is the Schema for the lakefsrolebindings API.
type LakeFSRoleBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of LakeFSRoleBinding.
	// +required
	Spec LakeFSRoleBindingSpec `json:"spec"`

	// status defines the observed state of LakeFSRoleBinding.
	// +optional
	Status LakeFSRoleBindingStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LakeFSRoleBindingList contains a list of LakeFSRoleBinding.
type LakeFSRoleBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LakeFSRoleBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LakeFSRoleBinding{}, &LakeFSRoleBindingList{})
}
