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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TemporalClusterSpec defines the desired state of TemporalCluster.
type TemporalClusterSpec struct {
	// Version is the Temporal server version, e.g. "1.31.2".
	// +kubebuilder:validation:Pattern=`^\d+\.\d+\.\d+$`
	Version string `json:"version"`
}

// TemporalClusterStatus defines the observed state of TemporalCluster.
type TemporalClusterStatus struct {
	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the current state of the TemporalCluster resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tc
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TemporalCluster is the Schema for the temporalclusters API.
type TemporalCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TemporalCluster
	// +required
	Spec TemporalClusterSpec `json:"spec"`

	// status defines the observed state of TemporalCluster
	// +optional
	Status TemporalClusterStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TemporalClusterList contains a list of TemporalCluster.
type TemporalClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TemporalCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TemporalCluster{}, &TemporalClusterList{})
}
