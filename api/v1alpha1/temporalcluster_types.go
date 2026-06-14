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
	"k8s.io/apimachinery/pkg/runtime"
)

// TemporalClusterSpec defines the desired state of TemporalCluster.
// +kubebuilder:validation:XValidation:rule="self.numHistoryShards == oldSelf.numHistoryShards",message="numHistoryShards is immutable"
type TemporalClusterSpec struct {
	// Version is the Temporal server version, e.g. "1.31.1".
	// +kubebuilder:validation:Pattern=`^\d+\.\d+\.\d+$`
	Version string `json:"version"`

	// NumHistoryShards is the number of history shards. IMMUTABLE after creation.
	// Choose carefully: 512 small prod, 4096 large prod.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=16384
	// +kubebuilder:default=512
	NumHistoryShards int32 `json:"numHistoryShards"`

	// Image is the Temporal server image. Default: temporalio/server:<Version>.
	// +optional
	Image string `json:"image,omitempty"`

	// ImagePullSecrets references secrets for pulling the server image.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Services configures each Temporal service.
	// +optional
	Services ServicesSpec `json:"services,omitempty"`

	// Persistence configures the default and visibility datastores. Required.
	Persistence PersistenceSpec `json:"persistence"`

	// MTLS configures mutual TLS (cert-manager-driven by default).
	// +optional
	MTLS *MTLSSpec `json:"mtls,omitempty"`

	// DynamicConfig is a passthrough for Temporal's dynamic config.
	// +optional
	DynamicConfig *DynamicConfigSpec `json:"dynamicConfig,omitempty"`

	// UI configures temporal-ui as part of this cluster.
	// +optional
	UI *UISpec `json:"ui,omitempty"`

	// Metrics configures Prometheus integration.
	// +optional
	Metrics *MetricsSpec `json:"metrics,omitempty"`

	// Archival configures cluster-wide archival enablement.
	// +optional
	Archival *ArchivalSpec `json:"archival,omitempty"`

	// Authorization configures the authorizer and claim mapper.
	// +optional
	Authorization *AuthorizationSpec `json:"authorization,omitempty"`

	// ClusterMetadata is a passthrough for multi-cluster setup.
	// +optional
	ClusterMetadata *ClusterMetadataSpec `json:"clusterMetadata,omitempty"`

	// PreventDeletion, when true, blocks deletion of the cluster via the
	// validating webhook as a safety measure.
	// +optional
	PreventDeletion bool `json:"preventDeletion,omitempty"`
}

// ServicesSpec configures each Temporal service plus shared overrides.
type ServicesSpec struct {
	// +optional
	Frontend *ServiceSpec `json:"frontend,omitempty"`
	// +optional
	History *ServiceSpec `json:"history,omitempty"`
	// +optional
	Matching *ServiceSpec `json:"matching,omitempty"`
	// +optional
	Worker *ServiceSpec `json:"worker,omitempty"`
	// +optional
	InternalFrontend *InternalFrontendSpec `json:"internalFrontend,omitempty"`
	// Overrides are applied to every service unless overridden per-service.
	// +optional
	Overrides *ServiceOverrides `json:"overrides,omitempty"`
}

// ServiceSpec configures a single Temporal service deployment.
type ServiceSpec struct {
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
	// +optional
	PodTemplate *PodTemplateOverride `json:"podTemplate,omitempty"`
	// +optional
	Service *ServiceExposureSpec `json:"service,omitempty"`
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// +optional
	TopologySpreadConstraints []corev1.TopologySpreadConstraint `json:"topologySpreadConstraints,omitempty"`
}

// InternalFrontendSpec configures the optional internal-frontend service.
type InternalFrontendSpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// ServiceOverrides are shared defaults applied across services.
type ServiceOverrides struct {
	// +optional
	PodTemplate *PodTemplateOverride `json:"podTemplate,omitempty"`
}

// PodTemplateOverride carries metadata and a strategic-merge pod spec override.
type PodTemplateOverride struct {
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// Spec is a partial PodSpec (strategic-merge patch) merged onto the
	// generated pod template. It is stored as an opaque object to keep the
	// CRD schema small.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Spec *runtime.RawExtension `json:"spec,omitempty"`
}

// ServiceExposureSpec configures how a service is exposed.
type ServiceExposureSpec struct {
	// +kubebuilder:validation:Enum=ClusterIP;NodePort;LoadBalancer
	// +kubebuilder:default=ClusterIP
	// +optional
	Type corev1.ServiceType `json:"type,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// TemporalClusterStatus defines the observed state of TemporalCluster.
type TemporalClusterStatus struct {
	// Phase is a coarse, human-friendly summary of the cluster lifecycle.
	// +optional
	Phase string `json:"phase,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the current state of the TemporalCluster resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Version is the currently-running Temporal server version.
	// +optional
	Version string `json:"version,omitempty"`

	// NumHistoryShards as observed from the database, not the spec.
	// +optional
	NumHistoryShards int32 `json:"numHistoryShards,omitempty"`

	// Services reports per-service readiness keyed by service name.
	// +optional
	Services map[string]ServiceStatus `json:"services,omitempty"`

	// Persistence reports datastore reachability and schema state.
	// +optional
	Persistence PersistenceStatus `json:"persistence,omitempty"`

	// Endpoints reports the resolved service endpoints.
	// +optional
	Endpoints EndpointsStatus `json:"endpoints,omitempty"`

	// Upgrade reports the state of an in-progress version upgrade, if any.
	// +optional
	Upgrade *UpgradeStatus `json:"upgrade,omitempty"`
}

// ServiceStatus reports the readiness of a single service.
type ServiceStatus struct {
	// +optional
	Ready int32 `json:"ready"`
	// +optional
	Desired int32 `json:"desired"`
	// +optional
	Version string `json:"version,omitempty"`
}

// EndpointsStatus reports resolved cluster endpoints.
type EndpointsStatus struct {
	// +optional
	Frontend string `json:"frontend,omitempty"`
	// +optional
	UI string `json:"ui,omitempty"`
	// +optional
	Metrics string `json:"metrics,omitempty"`
}

// UpgradeStatus reports the state of an in-progress version upgrade.
type UpgradeStatus struct {
	// +optional
	FromVersion string `json:"fromVersion,omitempty"`
	// +optional
	ToVersion string `json:"toVersion,omitempty"`
	// +optional
	Phase string `json:"phase,omitempty"`
	// Rollbackable is true until schema migration begins, after which a
	// rollback is no longer safe.
	// +optional
	Rollbackable bool `json:"rollbackable,omitempty"`
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tc
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.spec.version`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Shards",type=integer,JSONPath=`.spec.numHistoryShards`
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
