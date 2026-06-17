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

// TemporalClusterClientSpec defines the desired state of TemporalClusterClient.
type TemporalClusterClientSpec struct {
	// ClusterRef references the TemporalCluster to generate client credentials for.
	ClusterRef corev1.LocalObjectReference `json:"clusterRef"`

	// SecretName is the name of the Secret to write generated client credentials into.
	// Defaults to the resource name when empty.
	// +optional
	SecretName string `json:"secretName,omitempty"`
}

// TemporalClusterClientStatus defines the observed state of TemporalClusterClient.
type TemporalClusterClientStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// SecretRef references the generated credentials Secret.
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tcc
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef.name`
// +kubebuilder:printcolumn:name="Secret",type=string,JSONPath=`.status.secretRef.name`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TemporalClusterClient is the Schema for the temporalclusterclients API.
type TemporalClusterClient struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TemporalClusterClient
	// +required
	Spec TemporalClusterClientSpec `json:"spec"`

	// status defines the observed state of TemporalClusterClient
	// +optional
	Status TemporalClusterClientStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TemporalClusterClientList contains a list of TemporalClusterClient.
type TemporalClusterClientList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TemporalClusterClient `json:"items"`
}

func init() {
	registerType(&TemporalClusterClient{}, &TemporalClusterClientList{})
}
