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

// Mux roles for ProxyMux.Role.
const (
	// ProxyRoleServer opens a listening mux port (the exposed side).
	ProxyRoleServer = "server"
	// ProxyRoleClient dials out to a remote mux-server (never opens a port).
	ProxyRoleClient = "client"
)

// TemporalClusterProxySpec describes one local cluster's s2s-proxy and its link
// to one replication peer over an s2s-proxy mux connection.
type TemporalClusterProxySpec struct {
	// LocalClusterRef references the local operator-managed TemporalCluster this
	// proxy fronts. Its frontend address and issuer CA are resolved automatically.
	LocalClusterRef ClusterReference `json:"localClusterRef"`

	// LocalClusterName overrides the replication-group name of the local cluster.
	// Defaults to the referenced cluster's clusterMetadata.currentClusterName.
	// +optional
	LocalClusterName string `json:"localClusterName,omitempty"`

	// Peer is the remote replication cluster on the far side of the mux.
	Peer ProxyPeer `json:"peer"`

	// Mux configures the s2s-proxy multiplexed transport.
	Mux ProxyMux `json:"mux"`

	// Translation optionally renames namespaces and search attributes in-flight.
	// +optional
	Translation *ProxyTranslation `json:"translation,omitempty"`

	// FailoverVersionIncrement optionally translates failover-version increments.
	// +optional
	FailoverVersionIncrement *ProxyFailoverVersionIncrement `json:"failoverVersionIncrement,omitempty"`

	// ACL optionally restricts the admin methods and namespaces the proxy relays.
	// +optional
	ACL *ProxyACL `json:"acl,omitempty"`

	// Image overrides the pinned s2s-proxy image.
	// +optional
	Image string `json:"image,omitempty"`
}

// ProxyPeer identifies the remote replication cluster reached over the mux.
type ProxyPeer struct {
	// Name is the remote replication cluster name (== its currentClusterName).
	Name string `json:"name"`

	// ClusterRef optionally references an operator-managed remote TemporalCluster.
	// It is used only to reuse the peer's issuer CA when available.
	// +optional
	ClusterRef *ClusterReference `json:"clusterRef,omitempty"`

	// EnableConnection toggles replication without deleting the CR.
	// +kubebuilder:default=true
	// +optional
	EnableConnection *bool `json:"enableConnection,omitempty"`
}

// ProxyMux configures the s2s-proxy mux transport for one link.
type ProxyMux struct {
	// Role selects whether this proxy opens a port (server) or dials out (client).
	// +kubebuilder:validation:Enum=server;client
	Role string `json:"role"`

	// Server configures the listening side. Required when role=server.
	// +optional
	Server *ProxyMuxServer `json:"server,omitempty"`

	// Client configures the dialing side. Required when role=client.
	// +optional
	Client *ProxyMuxClient `json:"client,omitempty"`

	// MuxCount is the number of multiplexed sessions. Defaults to the upstream default.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MuxCount *int32 `json:"muxCount,omitempty"`

	// TLS configures the mux mTLS material.
	TLS ProxyMuxTLS `json:"tls"`
}

// ProxyMuxServer configures a mux-server (listening) proxy.
type ProxyMuxServer struct {
	// ListenPort is the port the mux listens on.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	ListenPort int32 `json:"listenPort"`

	// Exposure controls how the mux port is exposed (ClusterIP/NodePort/LoadBalancer).
	// +optional
	Exposure *ServiceExposureSpec `json:"exposure,omitempty"`
}

// ProxyMuxClient configures a mux-client (dialing) proxy.
type ProxyMuxClient struct {
	// ServerAddress is the reachable host:port of the remote mux-server.
	ServerAddress string `json:"serverAddress"`
}

// ProxyMuxTLS configures the mux mTLS material for one side.
type ProxyMuxTLS struct {
	// Provider selects how this side's mux certificate is sourced.
	// +kubebuilder:validation:Enum=cert-manager;secret
	// +kubebuilder:default=cert-manager
	// +optional
	Provider string `json:"provider,omitempty"`

	// IssuerRef mints this side's mux certificate. Required when provider=cert-manager.
	// +optional
	IssuerRef *IssuerReference `json:"issuerRef,omitempty"`

	// SecretRef supplies BYO cert/key/CA. Required when provider=secret.
	// +optional
	SecretRef *SecretReference `json:"secretRef,omitempty"`

	// PeerCARef supplies the remote side's CA to trust. When unset the CA bundle
	// from this side's own material is used (shared-issuer case).
	// +optional
	PeerCARef *SecretReference `json:"peerCARef,omitempty"`
}

// ProxyTranslation renames namespaces and search attributes in-flight.
type ProxyTranslation struct {
	// +optional
	Namespaces []ProxyNamespaceMapping `json:"namespaces,omitempty"`
	// +optional
	SearchAttributes []ProxySearchAttributeMapping `json:"searchAttributes,omitempty"`
}

// ProxyNamespaceMapping maps a local namespace name to a remote one.
type ProxyNamespaceMapping struct {
	Local  string `json:"local"`
	Remote string `json:"remote"`
}

// ProxySearchAttributeMapping maps search-attribute field names for a namespace.
type ProxySearchAttributeMapping struct {
	Namespace string              `json:"namespace"`
	Mappings  []ProxyFieldMapping `json:"mappings"`
}

// ProxyFieldMapping maps a local search-attribute field name to a remote one.
type ProxyFieldMapping struct {
	LocalFieldName  string `json:"localFieldName"`
	RemoteFieldName string `json:"remoteFieldName"`
}

// ProxyFailoverVersionIncrement translates failover-version increments across the link.
type ProxyFailoverVersionIncrement struct {
	Local  int64 `json:"local"`
	Remote int64 `json:"remote"`
}

// ProxyACL restricts what the proxy relays.
type ProxyACL struct {
	// +optional
	AllowedNamespaces []string `json:"allowedNamespaces,omitempty"`
	// AllowedAdminMethods defaults to the standard replication allowlist when empty.
	// +optional
	AllowedAdminMethods []string `json:"allowedAdminMethods,omitempty"`
}

// TemporalClusterProxyStatus is the observed state.
type TemporalClusterProxyStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ProxyEndpoint reports the exposed mux address (server role) to hand to the peer.
	// +optional
	ProxyEndpoint string `json:"proxyEndpoint,omitempty"`

	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tcproxy
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Role",type=string,JSONPath=`.spec.mux.role`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TemporalClusterProxy is the Schema for the temporalclusterproxies API.
type TemporalClusterProxy struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`
	// +required
	Spec TemporalClusterProxySpec `json:"spec"`
	// +optional
	Status TemporalClusterProxyStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TemporalClusterProxyList contains a list of TemporalClusterProxy.
type TemporalClusterProxyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TemporalClusterProxy `json:"items"`
}

func init() {
	registerType(&TemporalClusterProxy{}, &TemporalClusterProxyList{})
}
