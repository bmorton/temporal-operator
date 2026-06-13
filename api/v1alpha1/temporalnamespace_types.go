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

// TemporalNamespaceSpec defines the desired state of TemporalNamespace.
type TemporalNamespaceSpec struct {
	// ClusterRef references the TemporalCluster that owns this namespace.
	ClusterRef corev1.LocalObjectReference `json:"clusterRef"`

	// RetentionPeriod is how long closed workflows are retained.
	// +kubebuilder:default="72h"
	// +optional
	RetentionPeriod *metav1.Duration `json:"retentionPeriod,omitempty"`

	// Description is a human-friendly description of the namespace.
	// +optional
	Description string `json:"description,omitempty"`

	// OwnerEmail is the owner contact for the namespace.
	// +optional
	OwnerEmail string `json:"ownerEmail,omitempty"`
}

// TemporalNamespaceStatus defines the observed state of TemporalNamespace.
type TemporalNamespaceStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tns
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef.name`
// +kubebuilder:printcolumn:name="Retention",type=string,JSONPath=`.spec.retentionPeriod`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TemporalNamespace is the Schema for the temporalnamespaces API.
type TemporalNamespace struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TemporalNamespace
	// +required
	Spec TemporalNamespaceSpec `json:"spec"`

	// status defines the observed state of TemporalNamespace
	// +optional
	Status TemporalNamespaceStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TemporalNamespaceList contains a list of TemporalNamespace.
type TemporalNamespaceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TemporalNamespace `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TemporalNamespace{}, &TemporalNamespaceList{})
}
