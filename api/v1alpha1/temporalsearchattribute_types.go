/*
Copyright 2026 Brian Morton.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TemporalSearchAttributeSpec defines the desired state of TemporalSearchAttribute.
type TemporalSearchAttributeSpec struct {
	// ClusterRef references the TemporalCluster this search attribute belongs to.
	ClusterRef corev1.LocalObjectReference `json:"clusterRef"`

	// Namespace is the Temporal namespace to register the attribute in.
	Namespace string `json:"namespace"`

	// Name is the search attribute name.
	Name string `json:"name"`

	// Type is the search attribute type. Immutable once created.
	// +kubebuilder:validation:Enum=Keyword;Text;Int;Double;Bool;Datetime;KeywordList
	Type string `json:"type"`

	// AllowDeletion permits the operator to remove the search attribute from the
	// namespace when the CR is deleted.
	// +optional
	AllowDeletion bool `json:"allowDeletion,omitempty"`
}

// TemporalSearchAttributeStatus defines the observed state of TemporalSearchAttribute.
type TemporalSearchAttributeStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Registered indicates whether the attribute has been registered with the cluster.
	// +optional
	Registered bool `json:"registered,omitempty"`

	// RegisteredAt is when the attribute was registered.
	// +optional
	RegisteredAt *metav1.Time `json:"registeredAt,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tsa
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef.name`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Registered",type=boolean,JSONPath=`.status.registered`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TemporalSearchAttribute is the Schema for the temporalsearchattributes API.
type TemporalSearchAttribute struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TemporalSearchAttribute
	// +required
	Spec TemporalSearchAttributeSpec `json:"spec"`

	// status defines the observed state of TemporalSearchAttribute
	// +optional
	Status TemporalSearchAttributeStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TemporalSearchAttributeList contains a list of TemporalSearchAttribute.
type TemporalSearchAttributeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TemporalSearchAttribute `json:"items"`
}

func init() {
	registerType(&TemporalSearchAttribute{}, &TemporalSearchAttributeList{})
}
