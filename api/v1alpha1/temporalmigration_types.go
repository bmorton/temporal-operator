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

// TemporalMigrationSpec defines a migration from an external source Temporal
// cluster to an operator-managed target TemporalCluster via a managed proxy.
type TemporalMigrationSpec struct {
	// Source describes the EXTERNAL Temporal cluster being migrated away from.
	Source SourceClusterSpec `json:"source"`

	// TargetRef references the operator-managed TemporalCluster to migrate to.
	TargetRef corev1.LocalObjectReference `json:"targetRef"`

	// Namespaces to migrate. Empty means all namespaces present on the source.
	// +optional
	Namespaces []NamespaceMapping `json:"namespaces,omitempty"`

	// Cutover is the manual gate. false keeps the proxy in passthrough mode
	// (100% to source). true routes new starts to the target and falls back to
	// the source for operations on existing workflows.
	// +optional
	Cutover bool `json:"cutover,omitempty"`

	// Proxy tunes the provisioned proxy Deployment.
	// +optional
	Proxy *ProxySpec `json:"proxy,omitempty"`
}

// SourceClusterSpec describes how to reach the external source frontend.
type SourceClusterSpec struct {
	// Address is the source frontend host:port (e.g. "old-temporal:7233").
	Address string `json:"address"`

	// TLS configures how the proxy connects to the source.
	// +optional
	TLS *SourceTLSSpec `json:"tls,omitempty"`
}

// SourceTLSSpec configures TLS/mTLS from the proxy to the source frontend.
type SourceTLSSpec struct {
	// Enabled turns on TLS to the source frontend.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// SecretRef holds ca.crt (and optional tls.crt/tls.key for client mTLS).
	// +optional
	SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`

	// ServerName overrides SNI / certificate verification name.
	// +optional
	ServerName string `json:"serverName,omitempty"`
}

// NamespaceMapping maps a source namespace to a target namespace.
type NamespaceMapping struct {
	Source string `json:"source"`
	// +optional
	Target string `json:"target,omitempty"`
}

// TargetOrSource returns the target namespace, defaulting to the source name.
func (m NamespaceMapping) TargetOrSource() string {
	if m.Target != "" {
		return m.Target
	}
	return m.Source
}

// ProxySpec tunes the provisioned proxy Deployment.
type ProxySpec struct {
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// Image overrides the proxy image. Defaults to the operator image.
	// +optional
	Image string `json:"image,omitempty"`
}

// TemporalMigrationStatus is the observed state of a TemporalMigration.
type TemporalMigrationStatus struct {
	// Phase is one of Pending, Passthrough, Cutover, Draining, Complete.
	// +optional
	Phase string `json:"phase,omitempty"`

	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ProxyEndpoint is the Service address clients should target.
	// +optional
	ProxyEndpoint string `json:"proxyEndpoint,omitempty"`

	// Draining reports per-namespace source running-workflow counts.
	// +optional
	Draining []NamespaceDrainStatus `json:"draining,omitempty"`

	// CutoverTime records when cutover was first enabled.
	// +optional
	CutoverTime *metav1.Time `json:"cutoverTime,omitempty"`
}

// NamespaceDrainStatus reports drain progress for one source namespace.
type NamespaceDrainStatus struct {
	Namespace string `json:"namespace"`
	// +optional
	SourceRunningWorkflows int64 `json:"sourceRunningWorkflows"`
	// +optional
	Drained bool `json:"drained"`
}

// Migration phase constants.
const (
	MigrationPhasePending     = "Pending"
	MigrationPhasePassthrough = "Passthrough"
	MigrationPhaseCutover     = "Cutover"
	MigrationPhaseDraining    = "Draining"
	MigrationPhaseComplete    = "Complete"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tm
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.targetRef.name`
// +kubebuilder:printcolumn:name="Cutover",type=boolean,JSONPath=`.spec.cutover`
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.status.proxyEndpoint`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TemporalMigration is the Schema for the temporalmigrations API.
type TemporalMigration struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`
	// +required
	Spec TemporalMigrationSpec `json:"spec"`
	// +optional
	Status TemporalMigrationStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TemporalMigrationList contains a list of TemporalMigration.
type TemporalMigrationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TemporalMigration `json:"items"`
}

func init() {
	registerType(&TemporalMigration{}, &TemporalMigrationList{})
}
