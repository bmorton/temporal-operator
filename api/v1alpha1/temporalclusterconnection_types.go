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

// TemporalClusterConnectionSpec defines a multi-cluster replication group and
// drives automatic remote-cluster connection registration between its peers.
type TemporalClusterConnectionSpec struct {
	// Peers participating in replication. At least two are required. Each peer's
	// Name must equal that cluster's clusterMetadata.currentClusterName.
	// +kubebuilder:validation:MinItems=2
	Peers []ClusterConnectionPeer `json:"peers"`
}

// ClusterConnectionPeer identifies one cluster in a replication group. Exactly
// one of ClusterRef or FrontendAddress must be set.
type ClusterConnectionPeer struct {
	// Name is the replication-group cluster name (== clusterMetadata.currentClusterName).
	Name string `json:"name"`

	// ClusterRef points at a local TemporalCluster CR. The operator resolves its
	// frontend address and reuses its CA automatically.
	// +optional
	ClusterRef *ClusterReference `json:"clusterRef,omitempty"`

	// FrontendAddress is an external peer's gRPC frontend address (host:port).
	// +optional
	FrontendAddress string `json:"frontendAddress,omitempty"`

	// TLSSecretRef supplies mTLS material for an external peer. Ignored for
	// ClusterRef peers (the cluster CA is reused).
	// +optional
	TLSSecretRef *SecretReference `json:"tlsSecretRef,omitempty"`

	// EnableConnection toggles replication traffic without removing the peer.
	// +kubebuilder:default=true
	// +optional
	EnableConnection *bool `json:"enableConnection,omitempty"`
}

// TemporalClusterConnectionStatus defines the observed state.
type TemporalClusterConnectionStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Peers reports per-peer connection state.
	// +optional
	Peers []PeerConnectionStatus `json:"peers,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// PeerConnectionStatus reports the observed state of one peer.
type PeerConnectionStatus struct {
	Name string `json:"name"`
	// Reachable is true when the operator could connect to this peer's frontend.
	// +optional
	Reachable bool `json:"reachable"`
	// Connected is true when this peer appears as an enabled remote cluster on
	// the other reachable peers.
	// +optional
	Connected bool `json:"connected"`
	// +optional
	Message string `json:"message,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tcconn
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TemporalClusterConnection is the Schema for the temporalclusterconnections API.
type TemporalClusterConnection struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`
	// +required
	Spec TemporalClusterConnectionSpec `json:"spec"`
	// +optional
	Status TemporalClusterConnectionStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TemporalClusterConnectionList contains a list of TemporalClusterConnection.
type TemporalClusterConnectionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TemporalClusterConnection `json:"items"`
}

func init() {
	registerType(&TemporalClusterConnection{}, &TemporalClusterConnectionList{})
}
