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

package resources_test

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	sigsyaml "sigs.k8s.io/yaml"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

func serverProxyCR() *temporalv1alpha1.TemporalClusterProxy {
	enable := true
	return &temporalv1alpha1.TemporalClusterProxy{
		ObjectMeta: metav1.ObjectMeta{Name: "link", Namespace: "temporal-system"},
		Spec: temporalv1alpha1.TemporalClusterProxySpec{
			LocalClusterRef:  temporalv1alpha1.ClusterReference{Name: "cluster-a"},
			LocalClusterName: "cluster-a",
			Peer:             temporalv1alpha1.ProxyPeer{Name: "cluster-b", EnableConnection: &enable},
			Mux: temporalv1alpha1.ProxyMux{
				Role:   temporalv1alpha1.ProxyRoleServer,
				Server: &temporalv1alpha1.ProxyMuxServer{ListenPort: 6334},
				TLS:    temporalv1alpha1.ProxyMuxTLS{Provider: "cert-manager"},
			},
		},
	}
}

func TestBuildClusterProxyConfig_ServerRole(t *testing.T) {
	out, err := resources.BuildClusterProxyConfig(serverProxyCR(), "cluster-a-frontend.temporal-system.svc.cluster.local:7233")
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	var cfg struct {
		ClusterConnections []struct {
			Name  string `json:"name"`
			Local struct {
				ConnectionType string `json:"connectionType"`
				TCPClient      struct{ Address string } `json:"tcpClient"`
				TCPServer      struct{ Address string } `json:"tcpServer"`
			} `json:"local"`
			Remote struct {
				ConnectionType string `json:"connectionType"`
				MuxAddressInfo struct {
					Address string `json:"address"`
					TLS     struct {
						CertificatePath string `json:"certificatePath"`
						KeyPath         string `json:"keyPath"`
						RemoteCAPath    string `json:"remoteCAPath"`
					} `json:"tls"`
				} `json:"muxAddressInfo"`
			} `json:"remote"`
		} `json:"clusterConnections"`
	}
	if err := sigsyaml.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("unmarshal rendered config: %v\n%s", err, out)
	}
	if len(cfg.ClusterConnections) != 1 {
		t.Fatalf("want 1 connection, got %d", len(cfg.ClusterConnections))
	}
	c := cfg.ClusterConnections[0]
	if c.Local.ConnectionType != "tcp" {
		t.Errorf("local.connectionType = %q, want tcp", c.Local.ConnectionType)
	}
	if c.Local.TCPClient.Address != "cluster-a-frontend.temporal-system.svc.cluster.local:7233" {
		t.Errorf("tcpClient.address = %q", c.Local.TCPClient.Address)
	}
	if !strings.HasSuffix(c.Local.TCPServer.Address, "6233") {
		t.Errorf("tcpServer.address = %q, want :6233", c.Local.TCPServer.Address)
	}
	if c.Remote.ConnectionType != "mux-server" {
		t.Errorf("remote.connectionType = %q, want mux-server", c.Remote.ConnectionType)
	}
	if !strings.HasSuffix(c.Remote.MuxAddressInfo.Address, "6334") {
		t.Errorf("mux address = %q, want :6334", c.Remote.MuxAddressInfo.Address)
	}
	if c.Remote.MuxAddressInfo.TLS.CertificatePath != resources.ProxyTLSMountPath+"/tls.crt" {
		t.Errorf("certificatePath = %q", c.Remote.MuxAddressInfo.TLS.CertificatePath)
	}
	if c.Remote.MuxAddressInfo.TLS.RemoteCAPath != resources.ProxyTLSMountPath+"/ca.crt" {
		t.Errorf("remoteCAPath = %q (want own ca.crt when no peerCARef)", c.Remote.MuxAddressInfo.TLS.RemoteCAPath)
	}
}

func TestBuildClusterProxyConfig_ClientRoleWithTranslation(t *testing.T) {
	cr := serverProxyCR()
	cr.Spec.Mux.Role = temporalv1alpha1.ProxyRoleClient
	cr.Spec.Mux.Server = nil
	cr.Spec.Mux.Client = &temporalv1alpha1.ProxyMuxClient{ServerAddress: "b.example.com:6334"}
	cr.Spec.Translation = &temporalv1alpha1.ProxyTranslation{
		Namespaces: []temporalv1alpha1.ProxyNamespaceMapping{{Local: "ns", Remote: "ns.acct"}},
	}

	out, err := resources.BuildClusterProxyConfig(cr, "cluster-a-frontend:7233")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(out, "mux-client") {
		t.Errorf("expected mux-client in:\n%s", out)
	}
	if !strings.Contains(out, "b.example.com:6334") {
		t.Errorf("expected serverAddress in:\n%s", out)
	}
	if !strings.Contains(out, "ns.acct") {
		t.Errorf("expected namespace translation in:\n%s", out)
	}
}

func TestBuildClusterProxyService_ServerExposesMux(t *testing.T) {
	cr := serverProxyCR()
	svc := resources.BuildClusterProxyService(cr)
	if svc.Name != resources.ClusterProxyServiceName(cr) {
		t.Errorf("service name = %q", svc.Name)
	}
	var haveTCP, haveMux bool
	for _, p := range svc.Spec.Ports {
		if p.Port == resources.ProxyTCPServerPort {
			haveTCP = true
		}
		if p.Port == 6334 {
			haveMux = true
		}
	}
	if !haveTCP {
		t.Error("expected tcpServer port 6233")
	}
	if !haveMux {
		t.Error("expected mux port 6334 for server role")
	}
}

func TestBuildClusterProxyService_ClientOmitsMuxPort(t *testing.T) {
	cr := serverProxyCR()
	cr.Spec.Mux.Role = temporalv1alpha1.ProxyRoleClient
	cr.Spec.Mux.Server = nil
	cr.Spec.Mux.Client = &temporalv1alpha1.ProxyMuxClient{ServerAddress: "b:6334"}
	svc := resources.BuildClusterProxyService(cr)
	for _, p := range svc.Spec.Ports {
		if p.Name == "mux" {
			t.Error("client role must not expose a mux port")
		}
	}
}

func TestBuildClusterProxyDeployment_MountsConfigAndTLS(t *testing.T) {
	cr := serverProxyCR()
	dep := resources.BuildClusterProxyDeployment(cr, "abc123")
	if dep.Name != resources.ClusterProxyName(cr) {
		t.Errorf("deployment name = %q", dep.Name)
	}
	c := dep.Spec.Template.Spec.Containers[0]
	var haveConfig, haveTLS bool
	for _, m := range c.VolumeMounts {
		if m.MountPath == resources.ProxyConfigMountPath {
			haveConfig = true
		}
		if m.MountPath == resources.ProxyTLSMountPath {
			haveTLS = true
		}
	}
	if !haveConfig || !haveTLS {
		t.Errorf("missing mounts: config=%v tls=%v", haveConfig, haveTLS)
	}
	if dep.Spec.Template.Annotations[resources.ConfigHashAnnotation] != "abc123" {
		t.Errorf("config hash annotation = %q", dep.Spec.Template.Annotations[resources.ConfigHashAnnotation])
	}
}

func TestBuildClusterProxyCertificate_UsesIssuer(t *testing.T) {
	cr := serverProxyCR()
	cr.Spec.Mux.TLS.IssuerRef = &temporalv1alpha1.IssuerReference{Name: "ca-issuer"}
	crt := resources.BuildClusterProxyCertificate(cr)
	if crt.Spec.IssuerRef.Name != "ca-issuer" {
		t.Errorf("issuer = %q", crt.Spec.IssuerRef.Name)
	}
	if crt.Spec.SecretName != resources.ClusterProxyTLSSecretName(cr) {
		t.Errorf("secretName = %q", crt.Spec.SecretName)
	}
}
