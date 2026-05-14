// Copyright 2026, Versioneer (https://versioneer.at)
// SPDX-License-Identifier: Apache-2.0

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LakeFSUserReference points to a LakeFSUser in the same namespace.
type LakeFSUserReference struct {
	// name is the referenced user name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// LakeFSSecretReference identifies the Secret and key the operator writes for this credential.
type LakeFSSecretReference struct {
	// name is the referenced Secret name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// key is the generated Secret data key. Defaults to secretAccessKey.
	// +optional
	// +kubebuilder:default:=secretAccessKey
	Key string `json:"key,omitempty"`
}

// LakeFSCredentialSpec defines the desired state of LakeFSCredential
type LakeFSCredentialSpec struct {
	// userRef points to the LakeFSUser that owns this credential.
	UserRef LakeFSUserReference `json:"userRef"`

	// accessKeyId is the non-secret key ID lakeFS uses to look up credentials.
	// The operator writes this value to the generated Secret as accessKeyId.
	// +kubebuilder:validation:MinLength=1
	AccessKeyID string `json:"accessKeyId"`

	// secretRef identifies the Secret that the operator creates or updates with generated secret material.
	SecretRef LakeFSSecretReference `json:"secretRef"`

	// expiresAt marks this credential as expired after the given time.
	// +optional
	ExpiresAt *metav1.Time `json:"expiresAt,omitempty"`

	// revoked disables this credential without deleting the object.
	// +optional
	Revoked bool `json:"revoked,omitempty"`
}

// LakeFSCredentialStatus defines the observed state of LakeFSCredential.
type LakeFSCredentialStatus struct {
	// observedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// secretName is the Secret currently resolved by the controller.
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// secretKey is the Secret data key currently resolved by the controller.
	// +optional
	SecretKey string `json:"secretKey,omitempty"`

	// conditions represent the current state of the LakeFSCredential resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func (r LakeFSCredentialSpec) SecretKey() string {
	if r.SecretRef.Key == "" {
		return "secretAccessKey"
	}
	return r.SecretRef.Key
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced
// +kubebuilder:printcolumn:name="User",type=string,JSONPath=".spec.userRef.name"
// +kubebuilder:printcolumn:name="Access Key",type=string,JSONPath=".spec.accessKeyId"
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=".status.conditions[?(@.type=='Ready')].status"

// LakeFSCredential is the Schema for the lakefscredentials API
type LakeFSCredential struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of LakeFSCredential
	// +required
	Spec LakeFSCredentialSpec `json:"spec"`

	// status defines the observed state of LakeFSCredential
	// +optional
	Status LakeFSCredentialStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LakeFSCredentialList contains a list of LakeFSCredential
type LakeFSCredentialList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LakeFSCredential `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LakeFSCredential{}, &LakeFSCredentialList{})
}
