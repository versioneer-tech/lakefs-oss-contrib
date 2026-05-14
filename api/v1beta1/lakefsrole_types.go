// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LakeFSPolicyStatement describes a single lakeFS policy statement.
type LakeFSPolicyStatement struct {
	// effect is either allow or deny.
	// +kubebuilder:validation:Enum=allow;deny
	Effect string `json:"effect"`

	// resource is the lakeFS resource pattern. Templates may use <REPOSITORY>.
	// +kubebuilder:validation:MinLength=1
	Resource string `json:"resource"`

	// action is the list of lakeFS actions this statement covers.
	// +kubebuilder:validation:MinItems=1
	Action []string `json:"action"`
}

// LakeFSPolicyTemplate groups lakeFS policy statements.
type LakeFSPolicyTemplate struct {
	// name is the lakeFS policy name. Templates may use <REPOSITORY>.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// statement is the list of lakeFS statements.
	// +kubebuilder:validation:MinItems=1
	Statement []LakeFSPolicyStatement `json:"statement"`
}

// LakeFSRoleSpec defines the desired state of LakeFSRole
type LakeFSRoleSpec struct {
	// description is a human-readable explanation of the role.
	// +optional
	Description string `json:"description,omitempty"`

	// policies are lakeFS policy templates granted by this role.
	// +optional
	Policies []LakeFSPolicyTemplate `json:"policies,omitempty"`
}

// LakeFSRoleStatus defines the observed state of LakeFSRole.
type LakeFSRoleStatus struct {
	// observedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the LakeFSRole resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="Policies",type=string,JSONPath=".spec.policies[*].name"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"

// LakeFSRole is the Schema for the lakefsroles API
type LakeFSRole struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of LakeFSRole
	// +required
	Spec LakeFSRoleSpec `json:"spec"`

	// status defines the observed state of LakeFSRole
	// +optional
	Status LakeFSRoleStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LakeFSRoleList contains a list of LakeFSRole
type LakeFSRoleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LakeFSRole `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LakeFSRole{}, &LakeFSRoleList{})
}
