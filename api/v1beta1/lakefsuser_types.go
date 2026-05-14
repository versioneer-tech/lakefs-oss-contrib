// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LakeFSUserSpec defines the desired state of LakeFSUser
type LakeFSUserSpec struct {
	// externalId is the external identifier for this user when set.
	// +optional
	ExternalID string `json:"externalId,omitempty"`

	// friendlyName is a display name for humans.
	// +optional
	FriendlyName string `json:"friendlyName,omitempty"`
}

// LakeFSUserStatus defines the observed state of LakeFSUser.
type LakeFSUserStatus struct {
	// observedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the LakeFSUser resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="External ID",type=string,JSONPath=".spec.externalId"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"

// LakeFSUser is the Schema for the lakefsusers API
type LakeFSUser struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of LakeFSUser
	// +required
	Spec LakeFSUserSpec `json:"spec"`

	// status defines the observed state of LakeFSUser
	// +optional
	Status LakeFSUserStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LakeFSUserList contains a list of LakeFSUser
type LakeFSUserList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LakeFSUser `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LakeFSUser{}, &LakeFSUserList{})
}
