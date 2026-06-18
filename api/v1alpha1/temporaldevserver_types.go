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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TemporalDevServerSpec defines the desired state of TemporalDevServer.
//
// A TemporalDevServer runs a single-pod, disposable `temporal server start-dev`
// instance backed by SQLite. It is NOT for production use.
type TemporalDevServerSpec struct {
	// Version is the temporalio/temporal CLI image tag. Default "latest".
	// +kubebuilder:default="latest"
	// +optional
	Version string `json:"version,omitempty"`

	// Image overrides the full image reference. Default
	// temporalio/temporal:<Version>.
	// +optional
	Image string `json:"image,omitempty"`

	// ImagePullSecrets references secrets for pulling the image.
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// Namespaces are extra Temporal namespaces created at startup, in addition
	// to the always-present "default" namespace. These are created once at boot
	// with no drift management; use TemporalNamespace CRs for managed namespaces.
	// +optional
	Namespaces []string `json:"namespaces,omitempty"`

	// UI controls the bundled Temporal Web UI (port 8233).
	// +optional
	UI *DevServerUISpec `json:"ui,omitempty"`

	// Storage selects ephemeral (default) or PVC-backed SQLite storage.
	// +optional
	Storage *DevServerStorageSpec `json:"storage,omitempty"`

	// Service configures how the frontend/UI Service is exposed.
	// +optional
	Service *ServiceExposureSpec `json:"service,omitempty"`

	// Resources sets the dev server container resource requirements.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// NodeSelector constrains the dev server pod to matching nodes.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations applied to the dev server pod.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Affinity applied to the dev server pod.
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
}

// DevServerUISpec controls the bundled Web UI.
type DevServerUISpec struct {
	// Enabled toggles the bundled Web UI. Default true.
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`
}

// DevServerStorageSpec configures SQLite storage.
type DevServerStorageSpec struct {
	// Type selects ephemeral (emptyDir, wiped on restart) or Persistent (PVC).
	// +kubebuilder:validation:Enum=Ephemeral;Persistent
	// +kubebuilder:default=Ephemeral
	// +optional
	Type string `json:"type,omitempty"`

	// Size is the PVC size when Type=Persistent. Default "1Gi".
	// +optional
	Size *resource.Quantity `json:"size,omitempty"`

	// StorageClassName is the PVC storage class when Type=Persistent.
	// +optional
	StorageClassName *string `json:"storageClassName,omitempty"`
}

// TemporalDevServerStatus defines the observed state of TemporalDevServer.
type TemporalDevServerStatus struct {
	// Phase is a coarse, human-friendly lifecycle summary.
	// +optional
	Phase string `json:"phase,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the current state of the TemporalDevServer resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Endpoints reports the resolved service endpoints.
	// +optional
	Endpoints DevServerEndpoints `json:"endpoints,omitempty"`

	// Version is the currently-running image tag.
	// +optional
	Version string `json:"version,omitempty"`
}

// DevServerEndpoints reports the dev server's resolved endpoints.
type DevServerEndpoints struct {
	// Frontend is the gRPC frontend endpoint (host:7233).
	// +optional
	Frontend string `json:"frontend,omitempty"`
	// UI is the Web UI endpoint (host:8233).
	// +optional
	UI string `json:"ui,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tds
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.version`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="UI",type=string,JSONPath=`.status.endpoints.ui`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TemporalDevServer is the Schema for the temporaldevservers API.
type TemporalDevServer struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of TemporalDevServer
	// +required
	Spec TemporalDevServerSpec `json:"spec"`

	// status defines the observed state of TemporalDevServer
	// +optional
	Status TemporalDevServerStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TemporalDevServerList contains a list of TemporalDevServer.
type TemporalDevServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TemporalDevServer `json:"items"`
}

func init() {
	registerType(&TemporalDevServer{}, &TemporalDevServerList{})
}
