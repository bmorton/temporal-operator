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

package resources

import (
	"fmt"
	"net"

	sigsyaml "sigs.k8s.io/yaml"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// Mount paths and ports for the rendered s2s-proxy pod.
const (
	ProxyConfigMountPath = "/etc/s2s-proxy"
	ProxyConfigFileName  = "config.yaml"
	ProxyTLSMountPath    = "/etc/s2s-proxy/tls"
	ProxyPeerCAMountPath = "/etc/s2s-proxy/peer-ca"

	// ProxyTCPServerPort is the port the proxy exposes for the local Temporal to
	// reach as the peer's frontend address.
	ProxyTCPServerPort int32 = 6233
)

// DefaultAllowedAdminMethods is the standard admin-service allowlist required
// for cross-cluster replication.
func DefaultAllowedAdminMethods() []string {
	return []string{
		"AddOrUpdateRemoteCluster",
		"DescribeCluster",
		"DescribeMutableState",
		"GetNamespaceReplicationMessages",
		"GetWorkflowExecutionRawHistoryV2",
		"ListClusters",
		"StreamWorkflowReplicationMessages",
	}
}

// --- s2s-proxy config schema (subset we render) ---

type proxyTLS struct {
	CertificatePath    string `json:"certificatePath"`
	KeyPath            string `json:"keyPath"`
	RemoteCAPath       string `json:"remoteCAPath"`
	CAServerName       string `json:"caServerName,omitempty"`
	SkipCAVerification bool   `json:"skipCAVerification,omitempty"`
}

type proxyAddressInfo struct {
	Address string   `json:"address"`
	TLS     proxyTLS `json:"tls"`
}

type proxyTCPEndpoint struct {
	Address string `json:"address"`
}

type proxyLocal struct {
	ConnectionType string           `json:"connectionType"`
	TCPClient      proxyTCPEndpoint `json:"tcpClient"`
	TCPServer      proxyTCPEndpoint `json:"tcpServer"`
}

type proxyRemote struct {
	ConnectionType string           `json:"connectionType"`
	MuxCount       *int32           `json:"muxCount,omitempty"`
	MuxAddressInfo proxyAddressInfo `json:"muxAddressInfo"`
}

type proxyNamespaceMapping struct {
	Local  string `json:"local"`
	Remote string `json:"remote"`
}

type proxyNamespaceTranslation struct {
	Mappings []proxyNamespaceMapping `json:"mappings"`
}

type proxyFieldMapping struct {
	LocalFieldName  string `json:"localFieldName"`
	RemoteFieldName string `json:"remoteFieldName"`
}

type proxySANamespaceMapping struct {
	Name        string              `json:"name"`
	NamespaceID string              `json:"namespaceId"`
	Mappings    []proxyFieldMapping `json:"mappings"`
}

type proxySATranslation struct {
	NamespaceMappings []proxySANamespaceMapping `json:"namespaceMappings"`
}

type proxyFailover struct {
	Local  int64 `json:"local"`
	Remote int64 `json:"remote"`
}

type proxyACLPolicy struct {
	AllowedMethods    map[string][]string `json:"allowedMethods"`
	AllowedNamespaces []string            `json:"allowedNamespaces,omitempty"`
}

type proxyConnection struct {
	Name                                string                     `json:"name"`
	Local                               proxyLocal                 `json:"local"`
	Remote                              proxyRemote                `json:"remote"`
	NamespaceTranslation                *proxyNamespaceTranslation `json:"namespaceTranslation,omitempty"`
	SearchAttributeTranslation          *proxySATranslation        `json:"searchAttributeTranslation,omitempty"`
	FailoverVersionIncrementTranslation *proxyFailover             `json:"failoverVersionIncrementTranslation,omitempty"`
	ACLPolicy                           *proxyACLPolicy            `json:"aclPolicy,omitempty"`
}

type proxyConfigFile struct {
	ClusterConnections []proxyConnection `json:"clusterConnections"`
}

// BuildClusterProxyConfig renders the s2s-proxy config YAML for one CR. It is
// pure: localFrontendAddress is resolved by the caller.
func BuildClusterProxyConfig(cr *temporalv1alpha1.TemporalClusterProxy, localFrontendAddress string) (string, error) {
	mux := cr.Spec.Mux

	skipVerify := mux.TLS.SkipCAVerification != nil && *mux.TLS.SkipCAVerification
	remote := proxyRemote{
		MuxCount: mux.MuxCount,
		MuxAddressInfo: proxyAddressInfo{
			TLS: proxyTLS{
				CertificatePath:    ProxyTLSMountPath + "/tls.crt",
				KeyPath:            ProxyTLSMountPath + "/tls.key",
				RemoteCAPath:       proxyRemoteCAPath(cr),
				SkipCAVerification: skipVerify,
			},
		},
	}
	switch mux.Role {
	case temporalv1alpha1.ProxyRoleServer:
		if mux.Server == nil {
			return "", fmt.Errorf("mux.server is required for role=server")
		}
		remote.ConnectionType = "mux-server"
		remote.MuxAddressInfo.Address = fmt.Sprintf("0.0.0.0:%d", mux.Server.ListenPort)
	case temporalv1alpha1.ProxyRoleClient:
		if mux.Client == nil {
			return "", fmt.Errorf("mux.client is required for role=client")
		}
		remote.ConnectionType = "mux-client"
		remote.MuxAddressInfo.Address = mux.Client.ServerAddress
		// s2s-proxy requires the mux-client to verify the server certificate by
		// name unless verification is skipped. Use the explicit override, else
		// derive it from the server address host (matches the server proxy cert).
		if !skipVerify {
			remote.MuxAddressInfo.TLS.CAServerName = proxyClientCAServerName(mux)
		}
	default:
		return "", fmt.Errorf("unknown mux.role %q", mux.Role)
	}

	conn := proxyConnection{
		Name: cr.Name,
		Local: proxyLocal{
			ConnectionType: "tcp",
			TCPClient:      proxyTCPEndpoint{Address: localFrontendAddress},
			TCPServer:      proxyTCPEndpoint{Address: fmt.Sprintf("0.0.0.0:%d", ProxyTCPServerPort)},
		},
		Remote:    remote,
		ACLPolicy: buildACLPolicy(cr),
	}
	if t := cr.Spec.Translation; t != nil {
		if len(t.Namespaces) > 0 {
			nt := &proxyNamespaceTranslation{}
			for _, m := range t.Namespaces {
				nt.Mappings = append(nt.Mappings, proxyNamespaceMapping{Local: m.Local, Remote: m.Remote})
			}
			conn.NamespaceTranslation = nt
		}
		if len(t.SearchAttributes) > 0 {
			st := &proxySATranslation{}
			for _, sa := range t.SearchAttributes {
				m := proxySANamespaceMapping{Name: sa.Namespace, NamespaceID: sa.Namespace}
				for _, f := range sa.Mappings {
					m.Mappings = append(m.Mappings, proxyFieldMapping{LocalFieldName: f.LocalFieldName, RemoteFieldName: f.RemoteFieldName})
				}
				st.NamespaceMappings = append(st.NamespaceMappings, m)
			}
			conn.SearchAttributeTranslation = st
		}
	}
	if f := cr.Spec.FailoverVersionIncrement; f != nil {
		conn.FailoverVersionIncrementTranslation = &proxyFailover{Local: f.Local, Remote: f.Remote}
	}

	file := proxyConfigFile{ClusterConnections: []proxyConnection{conn}}
	raw, err := sigsyaml.Marshal(file)
	if err != nil {
		return "", fmt.Errorf("marshal proxy config: %w", err)
	}
	return string(raw), nil
}

func proxyRemoteCAPath(cr *temporalv1alpha1.TemporalClusterProxy) string {
	if cr.Spec.Mux.TLS.PeerCARef != nil {
		return ProxyPeerCAMountPath + "/ca.crt"
	}
	return ProxyTLSMountPath + "/ca.crt"
}

// proxyClientCAServerName returns the TLS server name a mux-client verifies the
// remote server certificate against. It uses the explicit CAServerName override
// when set, otherwise the host portion of the client's server address.
func proxyClientCAServerName(mux temporalv1alpha1.ProxyMux) string {
	if mux.TLS.CAServerName != "" {
		return mux.TLS.CAServerName
	}
	if mux.Client == nil {
		return ""
	}
	if host, _, err := net.SplitHostPort(mux.Client.ServerAddress); err == nil {
		return host
	}
	return mux.Client.ServerAddress
}

func buildACLPolicy(cr *temporalv1alpha1.TemporalClusterProxy) *proxyACLPolicy {
	methods := DefaultAllowedAdminMethods()
	var namespaces []string
	if a := cr.Spec.ACL; a != nil {
		if len(a.AllowedAdminMethods) > 0 {
			methods = a.AllowedAdminMethods
		}
		namespaces = a.AllowedNamespaces
	}
	return &proxyACLPolicy{
		AllowedMethods:    map[string][]string{"adminService": methods},
		AllowedNamespaces: namespaces,
	}
}
