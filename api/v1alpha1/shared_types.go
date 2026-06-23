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
	"k8s.io/apimachinery/pkg/runtime"
)

// MTLSSpec configures mutual TLS for the cluster.
type MTLSSpec struct {
	// Provider selects the certificate provider.
	// +kubebuilder:validation:Enum=cert-manager
	// +kubebuilder:default=cert-manager
	Provider string `json:"provider"`

	// IssuerRef references the cert-manager issuer used to mint certificates.
	// Required when provider is cert-manager.
	// +optional
	IssuerRef *IssuerReference `json:"issuerRef,omitempty"`

	// InternodeCA configures the internode certificate authority.
	// +optional
	InternodeCA *CertificateAuthoritySpec `json:"internodeCA,omitempty"`

	// Frontend configures the frontend certificate.
	// +optional
	Frontend *FrontendMTLSSpec `json:"frontend,omitempty"`

	// RefreshInterval is the certificate refresh interval.
	// +kubebuilder:default="720h"
	// +optional
	RefreshInterval *metav1.Duration `json:"refreshInterval,omitempty"`

	// RenewBefore is how long before expiry a certificate is renewed.
	// +kubebuilder:default="240h"
	// +optional
	RenewBefore *metav1.Duration `json:"renewBefore,omitempty"`
}

// IssuerReference references a cert-manager Issuer or ClusterIssuer.
type IssuerReference struct {
	Name string `json:"name"`
	// +kubebuilder:validation:Enum=Issuer;ClusterIssuer
	// +kubebuilder:default=Issuer
	// +optional
	Kind string `json:"kind,omitempty"`
	// +kubebuilder:default="cert-manager.io"
	// +optional
	Group string `json:"group,omitempty"`
}

// CertificateAuthoritySpec configures a certificate authority.
type CertificateAuthoritySpec struct {
	// +optional
	SecretName string `json:"secretName,omitempty"`
	// +optional
	Duration *metav1.Duration `json:"duration,omitempty"`
}

// FrontendMTLSSpec configures the frontend certificate.
type FrontendMTLSSpec struct {
	// +optional
	SecretName string `json:"secretName,omitempty"`
	// +optional
	DNSNames []string `json:"dnsNames,omitempty"`
}

// DynamicConfigSpec is a passthrough for Temporal's dynamic configuration.
type DynamicConfigSpec struct {
	// Values maps a dynamic config key to one or more constrained values.
	// +optional
	Values map[string][]DynamicConfigValue `json:"values,omitempty"`
}

// DynamicConfigValue is a single dynamic config value with optional constraints.
type DynamicConfigValue struct {
	// Value is an arbitrary JSON value for the dynamic config key.
	// +kubebuilder:pruning:PreserveUnknownFields
	Value runtime.RawExtension `json:"value"`

	// +optional
	Constraints *DynamicConfigConstraints `json:"constraints,omitempty"`
}

// DynamicConfigConstraints scopes a dynamic config value.
type DynamicConfigConstraints struct {
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// +optional
	TaskQueueName string `json:"taskQueueName,omitempty"`
	// +optional
	TaskQueueType string `json:"taskQueueType,omitempty"`
}

// UISpec configures temporal-ui.
type UISpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`

	// +optional
	Version string `json:"version,omitempty"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// +optional
	Ingress *UIIngressSpec `json:"ingress,omitempty"`

	// Auth is a passthrough for temporal-ui authentication config.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Auth *runtime.RawExtension `json:"auth,omitempty"`

	// +optional
	CodecServer *UICodecServerSpec `json:"codecServer,omitempty"`
}

// UIIngressSpec configures ingress for temporal-ui.
type UIIngressSpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
	// +optional
	IngressClassName string `json:"ingressClassName,omitempty"`
	// +optional
	Host string `json:"host,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	TLSSecretName string `json:"tlsSecretName,omitempty"`
}

// UICodecServerSpec configures the temporal-ui codec server.
type UICodecServerSpec struct {
	Endpoint string `json:"endpoint"`
	// +optional
	PassAccessToken bool `json:"passAccessToken,omitempty"`
	// +optional
	IncludeCredentials bool `json:"includeCredentials,omitempty"`
}

// MetricsSpec configures Prometheus integration.
type MetricsSpec struct {
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=9090
	// +optional
	Port int32 `json:"port,omitempty"`

	// +optional
	ServiceMonitor *ServiceMonitorSpec `json:"serviceMonitor,omitempty"`
}

// ServiceMonitorSpec configures a Prometheus Operator ServiceMonitor.
type ServiceMonitorSpec struct {
	// +kubebuilder:default=false
	Enabled bool `json:"enabled"`
	// +optional
	Interval *metav1.Duration `json:"interval,omitempty"`
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
}

// ArchivalSpec is a passthrough for cluster-wide archival configuration.
type ArchivalSpec struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	History *runtime.RawExtension `json:"history,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Visibility *runtime.RawExtension `json:"visibility,omitempty"`
}

// AuthorizationSpec configures the authorizer and claim mapper.
type AuthorizationSpec struct {
	// +optional
	Authorizer string `json:"authorizer,omitempty"`
	// +optional
	ClaimMapper string `json:"claimMapper,omitempty"`
	// Config is a passthrough for authorization provider configuration.
	// +kubebuilder:pruning:PreserveUnknownFields
	// +optional
	Config *runtime.RawExtension `json:"config,omitempty"`
}

// ClusterMetadataSpec configures multi-cluster replication.
type ClusterMetadataSpec struct {
	// +optional
	EnableGlobalNamespace bool `json:"enableGlobalNamespace,omitempty"`

	// +kubebuilder:validation:Minimum=1
	// +optional
	FailoverVersionIncrement *int32 `json:"failoverVersionIncrement,omitempty"`

	// +kubebuilder:validation:MinLength=1
	// +optional
	CurrentClusterName string `json:"currentClusterName,omitempty"`

	// +kubebuilder:validation:Minimum=1
	// +optional
	InitialFailoverVersion *int32 `json:"initialFailoverVersion,omitempty"`

	// +kubebuilder:validation:MinLength=1
	// +optional
	MasterClusterName string `json:"masterClusterName,omitempty"`
}

// Cluster reference kinds for ClusterReference.Kind.
const (
	// ClusterKindTemporalCluster references a full TemporalCluster.
	ClusterKindTemporalCluster = "TemporalCluster"
	// ClusterKindTemporalDevServer references a disposable TemporalDevServer.
	ClusterKindTemporalDevServer = "TemporalDevServer"
)

// ClusterReference points at a Temporal frontend in the same Kubernetes
// namespace: either a TemporalCluster (default) or a TemporalDevServer.
type ClusterReference struct {
	// Name is the name of the referenced object.
	Name string `json:"name"`

	// Kind selects the referenced object type.
	// +kubebuilder:validation:Enum=TemporalCluster;TemporalDevServer
	// +kubebuilder:default=TemporalCluster
	// +optional
	Kind string `json:"kind,omitempty"`
}

// SecretReference points at a Secret in the same namespace holding TLS material
// for connecting to an external Temporal peer. Keys default to the conventional
// "ca.crt", "tls.crt", "tls.key" when the overrides are empty.
type SecretReference struct {
	// Name is the Secret name.
	Name string `json:"name"`
	// CAKey is the Secret key holding the CA bundle. Defaults to "ca.crt".
	// +optional
	CAKey string `json:"caKey,omitempty"`
	// CertKey is the Secret key holding the client certificate. Defaults to "tls.crt".
	// +optional
	CertKey string `json:"certKey,omitempty"`
	// KeyKey is the Secret key holding the client private key. Defaults to "tls.key".
	// +optional
	KeyKey string `json:"keyKey,omitempty"`
}
