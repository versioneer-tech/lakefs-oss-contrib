// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LakeFSGroupSpec defines the desired state of LakeFSGroup.
type LakeFSGroupSpec struct {
	// externalId is the external identifier for this group when set.
	// +optional
	ExternalID string `json:"externalId,omitempty"`

	// users lists the LakeFSUser objects that belong to this group.
	// +optional
	Users []LakeFSUserReference `json:"users,omitempty"`

	// description is a human-readable explanation of the group.
	// +optional
	Description string `json:"description,omitempty"`
}

// LakeFSGroupStatus defines the observed state of LakeFSGroup.
type LakeFSGroupStatus struct {
	// observedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the LakeFSGroup resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="External ID",type=string,JSONPath=".spec.externalId"
// +kubebuilder:printcolumn:name="Users",type=string,JSONPath=".spec.users[*].name"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"

// LakeFSGroup is the Schema for the lakefsgroups API.
type LakeFSGroup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of LakeFSGroup.
	// +required
	Spec LakeFSGroupSpec `json:"spec"`

	// status defines the observed state of LakeFSGroup.
	// +optional
	Status LakeFSGroupStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LakeFSGroupList contains a list of LakeFSGroup.
type LakeFSGroupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LakeFSGroup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LakeFSGroup{}, &LakeFSGroupList{})
}
