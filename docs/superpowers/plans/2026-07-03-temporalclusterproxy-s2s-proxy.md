# TemporalClusterProxy (s2s-proxy automation) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `TemporalClusterProxy` CRD that deploys and wires `temporalio/s2s-proxy` so operator-managed Temporal clusters replicate across segregated networks over mux + mTLS.

**Architecture:** A new namespaced CRD whose reconciler renders an s2s-proxy `Deployment` + `Service` + `ConfigMap` (+ optional cert-manager `Certificate`), all owned by the CR, and then registers the peer as a remote cluster on the *local* Temporal via the *local* proxy's `tcpServer` address (reusing the existing `internal/temporal.RemoteClusterClient`). Resource builders and config rendering are pure (no client/IO) so they stay `wasm`-compatible, matching the existing `internal/resources` convention.

**Tech Stack:** Go 1.26.4, kubebuilder/controller-runtime v0.23 (typed generic admission API), cert-manager, `sigs.k8s.io/yaml` for config marshalling, envtest + chainsaw for tests.

## Global Constraints

- Go version: `1.26.4` in `go.mod`; CI uses `1.26.x`. (verbatim from spec/memories)
- Module path: `github.com/bmorton/temporal-operator`; API group: `temporal.bmor10.com`; copyright owner "Brian Morton" (Apache-2.0 header on every new `.go` file — copy verbatim from any existing file in the same package).
- Resource builders in `internal/resources` and config rendering must be pure (no k8s client / IO) so `GOOS=js GOARCH=wasm go build ./internal/resources/ ./internal/temporal/ ./api/...` stays green.
- Webhooks use the typed generic admission API: `admission.Validator[*T]` / `admission.Defaulter[*T]`, registered with `ctrl.NewWebhookManagedBy(mgr, &T{}).WithValidator(...)`. The `WithCustomValidator`/`.For()` variants are removed.
- After changing `api/v1alpha1`: run `make generate manifests`, then `make api-docs docs-crd-reference` and commit `docs/api/v1alpha1.md` + `docs/content/reference/_index.md` (docs CI drift check).
- After changing API types or RBAC markers: run `make helm-chart` and commit `dist/chart` (verify-chart CI). Do NOT hand-edit `dist/chart`. Do NOT commit `.github/workflows/test-chart.yml` (kept deleted/untracked).
- Every commit: Conventional Commit prefix (`feat`/`test`/`docs`/`chore`) + DCO sign-off (`git commit -s`) + the `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>` trailer.
- Pin the s2s-proxy image (binary-only project; internal APIs may change). Verified pinned image: `temporalio/s2s-proxy:v0.2.1` (latest published tag as of 2026-07). The container entrypoint runs `s2s-proxy start --config $CONFIG_YML`, so the config path is passed via the `CONFIG_YML` env var (verified from the upstream `scripts/start.sh`).

---

## File structure

**Create:**
- `api/v1alpha1/temporalclusterproxy_types.go` — CRD Go types + `init()` registration.
- `internal/resources/clusterproxyconfig.go` — pure s2s-proxy config-YAML render.
- `internal/resources/clusterproxy.go` — pure `Build*` funcs (ConfigMap, Certificate, Deployment, Service) + name helpers.
- `internal/resources/clusterproxy_test.go` — unit tests for the two files above.
- `internal/controller/temporalclusterproxy_controller.go` — the reconciler.
- `internal/controller/temporalclusterproxy_controller_test.go` — envtest suite.
- `internal/webhook/v1alpha1/temporalclusterproxy_webhook.go` — validator + defaulter.
- `internal/webhook/v1alpha1/temporalclusterproxy_webhook_test.go` — webhook tests.
- `examples/cluster-proxy-mux/server.yaml`, `examples/cluster-proxy-mux/client.yaml` — example CRs.
- `test/e2e/clusterproxy/chainsaw-test.yaml` (+ manifests) — e2e (final task).

**Modify:**
- `cmd/main.go` — register the reconciler + webhook.
- Generated (via make targets, do not hand-edit): `api/v1alpha1/zz_generated.deepcopy.go`, `config/crd/...`, `config/rbac/role.yaml`, `dist/chart/**`, `docs/api/v1alpha1.md`, `docs/content/reference/_index.md`.

---

## Task 1: API types

**Files:**
- Create: `api/v1alpha1/temporalclusterproxy_types.go`
- Modify (generated): `api/v1alpha1/zz_generated.deepcopy.go`, `config/crd/`, `dist/chart/`, docs

**Interfaces:**
- Produces (used by every later task):
  - `type TemporalClusterProxy struct{ ...; Spec TemporalClusterProxySpec; Status TemporalClusterProxyStatus }`
  - `TemporalClusterProxySpec{ LocalClusterRef ClusterReference; LocalClusterName string; Peer ProxyPeer; Mux ProxyMux; Translation *ProxyTranslation; FailoverVersionIncrement *ProxyFailoverVersionIncrement; ACL *ProxyACL; Image string }`
  - `ProxyPeer{ Name string; ClusterRef *ClusterReference; EnableConnection *bool }`
  - `ProxyMux{ Role string; Server *ProxyMuxServer; Client *ProxyMuxClient; MuxCount *int32; TLS ProxyMuxTLS }`
  - `ProxyMuxServer{ ListenPort int32; Exposure *ServiceExposureSpec }`
  - `ProxyMuxClient{ ServerAddress string }`
  - `ProxyMuxTLS{ Provider string; IssuerRef *IssuerReference; SecretRef *SecretReference; PeerCARef *SecretReference }`
  - `ProxyTranslation{ Namespaces []ProxyNamespaceMapping; SearchAttributes []ProxySearchAttributeMapping }`
  - `ProxyNamespaceMapping{ Local, Remote string }`
  - `ProxySearchAttributeMapping{ Namespace string; Mappings []ProxyFieldMapping }`
  - `ProxyFieldMapping{ LocalFieldName, RemoteFieldName string }`
  - `ProxyFailoverVersionIncrement{ Local, Remote int64 }`
  - `ProxyACL{ AllowedNamespaces, AllowedAdminMethods []string }`
  - `TemporalClusterProxyStatus{ ObservedGeneration int64; ProxyEndpoint string; Conditions []metav1.Condition }`
  - constants: `ProxyRoleServer = "server"`, `ProxyRoleClient = "client"`, `ConditionProxyDeployed = "ProxyDeployed"`, `ConditionRemoteClusterRegistered = "RemoteClusterRegistered"`.

- [ ] **Step 1: Write the type file**

Create `api/v1alpha1/temporalclusterproxy_types.go` (copy the Apache header verbatim from `temporalclusterconnection_types.go`):

```go
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
```

- [ ] **Step 2: Add condition + reason constants**

In `api/v1alpha1/conditions.go`, add next to the existing condition/reason constants:

```go
// ConditionProxyDeployed indicates the s2s-proxy Deployment is available.
ConditionProxyDeployed = "ProxyDeployed"
// ConditionRemoteClusterRegistered indicates the local Temporal registered the
// peer as a remote cluster via the local proxy.
ConditionRemoteClusterRegistered = "RemoteClusterRegistered"
```

And add reason constants (match the existing `Reason*` style in that file):

```go
ReasonProxyNotReady       = "ProxyNotReady"
ReasonProxyReady          = "ProxyReady"
ReasonClusterNotReady     = "ClusterNotReady"
ReasonMTLSNotReady        = "MTLSNotReady"
ReasonRegistrationFailed  = "RegistrationFailed"
```

(If any of these names already exist in `conditions.go`, reuse the existing one and drop the duplicate.)

- [ ] **Step 3: Generate deepcopy + manifests**

Run: `make generate manifests`
Expected: `api/v1alpha1/zz_generated.deepcopy.go` gains `DeepCopy*` for the new types; a new `config/crd/bases/temporal.bmor10.com_temporalclusterproxies.yaml` appears; `config/rbac/role.yaml` unchanged (RBAC comes in Task 4). No errors.

- [ ] **Step 4: Verify it compiles + wasm build**

Run: `go build ./... && GOOS=js GOARCH=wasm go build ./internal/resources/ ./internal/temporal/ ./api/...`
Expected: both succeed.

- [ ] **Step 5: Regenerate docs + chart**

Run: `make api-docs docs-crd-reference helm-chart`
Then: `git status` — expect changes under `docs/api/v1alpha1.md`, `docs/content/reference/_index.md`, `dist/chart/`, `config/`. If `.github/workflows/test-chart.yml` reappears, `rm` it (do not stage).

- [ ] **Step 6: Commit**

```bash
git add api/ config/ dist/chart/ docs/api docs/content
git commit -s -m "feat(api): add TemporalClusterProxy CRD types

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 2: s2s-proxy config rendering (pure)

**Files:**
- Create: `internal/resources/clusterproxyconfig.go`
- Test: `internal/resources/clusterproxy_test.go`

**Interfaces:**
- Consumes: `TemporalClusterProxy` types (Task 1).
- Produces:
  - `func BuildClusterProxyConfig(cr *temporalv1alpha1.TemporalClusterProxy, localFrontendAddress string) (string, error)` — returns the s2s-proxy config YAML.
  - Path constants: `ProxyTLSMountPath = "/etc/s2s-proxy/tls"`, `ProxyPeerCAMountPath = "/etc/s2s-proxy/peer-ca"`, `ProxyConfigMountPath = "/etc/s2s-proxy"`, `ProxyConfigFileName = "config.yaml"`.
  - Port constant: `ProxyTCPServerPort int32 = 6233`.
  - `func DefaultAllowedAdminMethods() []string` — the standard replication allowlist.

- [ ] **Step 1: Write the failing test**

In `internal/resources/clusterproxy_test.go` (copy the Apache header from `builders_test.go`):

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/resources/ -run TestBuildClusterProxyConfig -v`
Expected: FAIL — `undefined: resources.BuildClusterProxyConfig`.

- [ ] **Step 3: Write the implementation**

Create `internal/resources/clusterproxyconfig.go` (Apache header):

```go
package resources

import (
	"fmt"

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
	CertificatePath string `json:"certificatePath"`
	KeyPath         string `json:"keyPath"`
	RemoteCAPath    string `json:"remoteCAPath"`
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
	Name                                 string                     `json:"name"`
	Local                                proxyLocal                 `json:"local"`
	Remote                               proxyRemote                `json:"remote"`
	NamespaceTranslation                 *proxyNamespaceTranslation `json:"namespaceTranslation,omitempty"`
	SearchAttributeTranslation           *proxySATranslation        `json:"searchAttributeTranslation,omitempty"`
	FailoverVersionIncrementTranslation  *proxyFailover             `json:"failoverVersionIncrementTranslation,omitempty"`
	ACLPolicy                            *proxyACLPolicy            `json:"aclPolicy,omitempty"`
}

type proxyConfigFile struct {
	ClusterConnections []proxyConnection `json:"clusterConnections"`
}

// BuildClusterProxyConfig renders the s2s-proxy config YAML for one CR. It is
// pure: localFrontendAddress is resolved by the caller.
func BuildClusterProxyConfig(cr *temporalv1alpha1.TemporalClusterProxy, localFrontendAddress string) (string, error) {
	mux := cr.Spec.Mux

	remote := proxyRemote{
		MuxCount: mux.MuxCount,
		MuxAddressInfo: proxyAddressInfo{
			TLS: proxyTLS{
				CertificatePath: ProxyTLSMountPath + "/tls.crt",
				KeyPath:         ProxyTLSMountPath + "/tls.key",
				RemoteCAPath:    proxyRemoteCAPath(cr),
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/resources/ -run TestBuildClusterProxyConfig -v`
Expected: PASS (both subtests).

- [ ] **Step 5: Verify wasm build**

Run: `GOOS=js GOARCH=wasm go build ./internal/resources/`
Expected: success (sigs.k8s.io/yaml is pure Go).

- [ ] **Step 6: Commit**

```bash
git add internal/resources/clusterproxyconfig.go internal/resources/clusterproxy_test.go
git commit -s -m "feat(resources): render s2s-proxy config from TemporalClusterProxy

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 3: Resource builders (pure)

**Files:**
- Create: `internal/resources/clusterproxy.go`
- Test: `internal/resources/clusterproxy_test.go` (append)

**Interfaces:**
- Consumes: config render + constants (Task 2).
- Produces:
  - `ClusterProxyName(cr) string` → `cr.Name + "-s2s-proxy"`
  - `ClusterProxyConfigMapName(cr) string`, `ClusterProxyServiceName(cr) string`, `ClusterProxyCertName(cr) string`, `ClusterProxyTLSSecretName(cr) string`
  - `ClusterProxyLabels(cr) map[string]string`
  - `DefaultClusterProxyImage = "temporalio/s2s-proxy:v0.2.1"`
  - `func BuildClusterProxyConfigMap(cr, configYAML string) *corev1.ConfigMap`
  - `func BuildClusterProxyCertificate(cr) *certmanagerv1.Certificate` (nil-safe: caller only invokes when provider=cert-manager)
  - `func BuildClusterProxyDeployment(cr, configHash string) *appsv1.Deployment`
  - `func BuildClusterProxyService(cr) *corev1.Service`

- [ ] **Step 1: Write the failing tests**

Append to `internal/resources/clusterproxy_test.go`:

```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/resources/ -run TestBuildClusterProxy -v`
Expected: FAIL — `undefined: resources.BuildClusterProxyService` (etc.).

- [ ] **Step 3: Write the implementation**

Create `internal/resources/clusterproxy.go` (Apache header). Note: this package's `intstrFromInt` helper already exists (used in `service.go`); reuse it.

```go
package resources

import (
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// DefaultClusterProxyImage is the pinned s2s-proxy image.
const DefaultClusterProxyImage = "temporalio/s2s-proxy:v0.2.1"

// ClusterProxyName returns the proxy Deployment (and base) name.
func ClusterProxyName(cr *temporalv1alpha1.TemporalClusterProxy) string {
	return cr.Name + "-s2s-proxy"
}

// ClusterProxyConfigMapName returns the rendered-config ConfigMap name.
func ClusterProxyConfigMapName(cr *temporalv1alpha1.TemporalClusterProxy) string {
	return ClusterProxyName(cr) + "-config"
}

// ClusterProxyServiceName returns the proxy Service name.
func ClusterProxyServiceName(cr *temporalv1alpha1.TemporalClusterProxy) string {
	return ClusterProxyName(cr)
}

// ClusterProxyCertName returns the cert-manager Certificate name.
func ClusterProxyCertName(cr *temporalv1alpha1.TemporalClusterProxy) string {
	return ClusterProxyName(cr) + "-tls"
}

// ClusterProxyTLSSecretName returns the mux TLS Secret name (own material).
func ClusterProxyTLSSecretName(cr *temporalv1alpha1.TemporalClusterProxy) string {
	if cr.Spec.Mux.TLS.Provider == "secret" && cr.Spec.Mux.TLS.SecretRef != nil {
		return cr.Spec.Mux.TLS.SecretRef.Name
	}
	return ClusterProxyCertName(cr)
}

// ClusterProxyLabels returns the standard label set for proxy resources.
func ClusterProxyLabels(cr *temporalv1alpha1.TemporalClusterProxy) map[string]string {
	return map[string]string{
		LabelName:      "s2s-proxy",
		LabelInstance:  cr.Name,
		LabelComponent: "s2s-proxy",
		LabelManagedBy: managedByValue,
	}
}

func clusterProxyImage(cr *temporalv1alpha1.TemporalClusterProxy) string {
	if cr.Spec.Image != "" {
		return cr.Spec.Image
	}
	return DefaultClusterProxyImage
}

// BuildClusterProxyConfigMap wraps the rendered config YAML in a ConfigMap.
func BuildClusterProxyConfigMap(cr *temporalv1alpha1.TemporalClusterProxy, configYAML string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: ClusterProxyConfigMapName(cr), Namespace: cr.Namespace, Labels: ClusterProxyLabels(cr)},
		Data:       map[string]string{ProxyConfigFileName: configYAML},
	}
}

// BuildClusterProxyCertificate builds the mux mTLS Certificate. Only call when
// mux.tls.provider is cert-manager (IssuerRef set).
func BuildClusterProxyCertificate(cr *temporalv1alpha1.TemporalClusterProxy) *certmanagerv1.Certificate {
	var dnsNames []string
	svc := ClusterProxyServiceName(cr)
	dnsNames = append(dnsNames,
		svc,
		svc+"."+cr.Namespace+".svc",
		svc+"."+cr.Namespace+".svc.cluster.local",
	)
	return &certmanagerv1.Certificate{
		TypeMeta:   metav1.TypeMeta{APIVersion: "cert-manager.io/v1", Kind: "Certificate"},
		ObjectMeta: metav1.ObjectMeta{Name: ClusterProxyCertName(cr), Namespace: cr.Namespace, Labels: ClusterProxyLabels(cr)},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: ClusterProxyTLSSecretName(cr),
			CommonName: ClusterProxyName(cr),
			DNSNames:   dnsNames,
			IssuerRef:  issuerRef(cr.Spec.Mux.TLS.IssuerRef),
			Usages:     []certmanagerv1.KeyUsage{certmanagerv1.UsageServerAuth, certmanagerv1.UsageClientAuth},
		},
	}
}

// BuildClusterProxyService builds the proxy Service. It always exposes the
// tcpServer port (ClusterIP, for the local Temporal) and, for the server role,
// the mux listen port using the configured exposure.
func BuildClusterProxyService(cr *temporalv1alpha1.TemporalClusterProxy) *corev1.Service {
	ports := []corev1.ServicePort{
		{Name: "tcp-server", Port: ProxyTCPServerPort, TargetPort: intstrFromInt(ProxyTCPServerPort)},
	}
	svcType := corev1.ServiceTypeClusterIP
	var annotations map[string]string
	if cr.Spec.Mux.Role == temporalv1alpha1.ProxyRoleServer && cr.Spec.Mux.Server != nil {
		ports = append(ports, corev1.ServicePort{
			Name: "mux", Port: cr.Spec.Mux.Server.ListenPort, TargetPort: intstrFromInt(cr.Spec.Mux.Server.ListenPort),
		})
		if ex := cr.Spec.Mux.Server.Exposure; ex != nil {
			if ex.Type != "" {
				svcType = ex.Type
			}
			annotations = ex.Annotations
		}
	}
	return &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{Name: ClusterProxyServiceName(cr), Namespace: cr.Namespace, Labels: ClusterProxyLabels(cr), Annotations: annotations},
		Spec: corev1.ServiceSpec{
			Type:     svcType,
			Selector: ClusterProxyLabels(cr),
			Ports:    ports,
		},
	}
}

// BuildClusterProxyDeployment builds the s2s-proxy Deployment. configHash stamps
// the pod template so config/cert changes trigger a rollout.
func BuildClusterProxyDeployment(cr *temporalv1alpha1.TemporalClusterProxy, configHash string) *appsv1.Deployment {
	volumes := []corev1.Volume{
		{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: ClusterProxyConfigMapName(cr)},
		}}},
		{Name: "tls", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName: ClusterProxyTLSSecretName(cr),
		}}},
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: ProxyConfigMountPath, ReadOnly: true},
		{Name: "tls", MountPath: ProxyTLSMountPath, ReadOnly: true},
	}
	if ref := cr.Spec.Mux.TLS.PeerCARef; ref != nil {
		volumes = append(volumes, corev1.Volume{Name: "peer-ca", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: ref.Name}}})
		mounts = append(mounts, corev1.VolumeMount{Name: "peer-ca", MountPath: ProxyPeerCAMountPath, ReadOnly: true})
	}

	replicas := int32(1)
	labels := ClusterProxyLabels(cr)
	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: ClusterProxyName(cr), Namespace: cr.Namespace, Labels: labels},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: map[string]string{ConfigHashAnnotation: configHash},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:         "s2s-proxy",
						Image:        clusterProxyImage(cr),
						Env:          []corev1.EnvVar{{Name: "CONFIG_YML", Value: ProxyConfigMountPath + "/" + ProxyConfigFileName}},
						VolumeMounts: mounts,
					}},
					Volumes: volumes,
				},
			},
		},
	}
}
```

Note: the upstream container entrypoint (`scripts/start.sh`) reads the config path from the `CONFIG_YML` env var and runs `s2s-proxy start --config $CONFIG_YML` (verified against the pinned `v0.2.1` image). The builder therefore sets `CONFIG_YML` rather than overriding the container command.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/resources/ -run TestBuildClusterProxy -v`
Expected: PASS.

- [ ] **Step 5: Full package test + wasm build**

Run: `go test ./internal/resources/... && GOOS=js GOARCH=wasm go build ./internal/resources/`
Expected: PASS and build success.

- [ ] **Step 6: Commit**

```bash
git add internal/resources/clusterproxy.go internal/resources/clusterproxy_test.go
git commit -s -m "feat(resources): build s2s-proxy Deployment/Service/ConfigMap/Certificate

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 4: Reconciler

**Files:**
- Create: `internal/controller/temporalclusterproxy_controller.go`
- Test: `internal/controller/temporalclusterproxy_controller_test.go`
- Modify: `cmd/main.go`

**Interfaces:**
- Consumes: `resolveTarget`/`ResolvedTarget`/`ErrTargetNotFound`/`namespaceDriftRequeue`/`serverSideApply` (existing `internal/controller`), `temporal.RemoteClusterClientFactory` + `RemoteClusterClient` (existing), resource builders (Task 3).
- Produces: `type TemporalClusterProxyReconciler struct{ client.Client; Scheme *runtime.Scheme; ClientFactory temporal.RemoteClusterClientFactory }` with `Reconcile` + `SetupWithManager`.

- [ ] **Step 1: Write the controller**

Create `internal/controller/temporalclusterproxy_controller.go` (Apache header). The test reuses the existing `fakeRemoteClient` type and inline `temporal.RemoteClusterClientFactory` defined in `internal/controller/temporalclusterconnection_controller_test.go` (same `package controller` test build) — do not redefine them.

```go
package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

const clusterProxyFinalizer = "temporal.bmor10.com/clusterproxy"
const proxyServicesFieldOwner = client.FieldOwner("temporal-operator-clusterproxy")

// TemporalClusterProxyReconciler deploys an s2s-proxy for one local cluster and
// registers the peer as a remote cluster via the local proxy.
type TemporalClusterProxyReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	ClientFactory temporal.RemoteClusterClientFactory
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterproxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterproxies/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete

func (r *TemporalClusterProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cr temporalv1alpha1.TemporalClusterProxy
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !cr.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, &cr)
	}
	if !controllerutil.ContainsFinalizer(&cr, clusterProxyFinalizer) {
		controllerutil.AddFinalizer(&cr, clusterProxyFinalizer)
		if err := r.Update(ctx, &cr); err != nil {
			return ctrl.Result{}, err
		}
	}

	target, err := resolveTarget(ctx, r.Client, cr.Namespace, cr.Spec.LocalClusterRef)
	if err != nil {
		if errors.Is(err, ErrTargetNotFound) {
			r.setReady(&cr, metav1.ConditionFalse, temporalv1alpha1.ReasonClusterNotReady, "local cluster not found")
			return ctrl.Result{RequeueAfter: namespaceDriftRequeue}, r.statusUpdate(ctx, &cr)
		}
		return ctrl.Result{}, err
	}

	// Render + apply proxy resources.
	configYAML, err := resources.BuildClusterProxyConfig(&cr, target.Address)
	if err != nil {
		return ctrl.Result{}, err
	}
	sum := sha256.Sum256([]byte(configYAML))
	configHash := hex.EncodeToString(sum[:])

	objs := []client.Object{
		resources.BuildClusterProxyConfigMap(&cr, configYAML),
		resources.BuildClusterProxyService(&cr),
		resources.BuildClusterProxyDeployment(&cr, configHash),
	}
	if cr.Spec.Mux.TLS.Provider != "secret" && cr.Spec.Mux.TLS.IssuerRef != nil {
		objs = append([]client.Object{resources.BuildClusterProxyCertificate(&cr)}, objs...)
	}
	for _, obj := range objs {
		if err := r.apply(ctx, &cr, obj); err != nil {
			return ctrl.Result{}, err
		}
	}

	deployReady := r.deploymentAvailable(ctx, &cr)
	if deployReady {
		meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{Type: temporalv1alpha1.ConditionProxyDeployed, Status: metav1.ConditionTrue, Reason: temporalv1alpha1.ReasonProxyReady, Message: "proxy deployment available", ObservedGeneration: cr.Generation})
	} else {
		meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{Type: temporalv1alpha1.ConditionProxyDeployed, Status: metav1.ConditionFalse, Reason: temporalv1alpha1.ReasonProxyNotReady, Message: "proxy deployment not yet available", ObservedGeneration: cr.Generation})
	}

	// Publish endpoint for server role.
	if cr.Spec.Mux.Role == temporalv1alpha1.ProxyRoleServer && cr.Spec.Mux.Server != nil {
		cr.Status.ProxyEndpoint = r.serverEndpoint(ctx, &cr)
	}

	// Register the peer via the local proxy once the proxy and local cluster are ready.
	registered := false
	if deployReady && target.Ready {
		if err := r.registerPeer(ctx, &cr, target.TLSConfig); err != nil {
			logf.FromContext(ctx).Error(err, "registering peer via proxy")
			meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{Type: temporalv1alpha1.ConditionRemoteClusterRegistered, Status: metav1.ConditionFalse, Reason: temporalv1alpha1.ReasonRegistrationFailed, Message: err.Error(), ObservedGeneration: cr.Generation})
		} else {
			registered = true
			meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{Type: temporalv1alpha1.ConditionRemoteClusterRegistered, Status: metav1.ConditionTrue, Reason: temporalv1alpha1.ReasonProxyReady, Message: "peer registered via local proxy", ObservedGeneration: cr.Generation})
		}
	} else {
		meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{Type: temporalv1alpha1.ConditionRemoteClusterRegistered, Status: metav1.ConditionFalse, Reason: temporalv1alpha1.ReasonClusterNotReady, Message: "waiting for proxy and local cluster", ObservedGeneration: cr.Generation})
	}

	if deployReady && registered {
		r.setReady(&cr, metav1.ConditionTrue, temporalv1alpha1.ReasonProxyReady, "proxy deployed and peer registered")
	} else {
		r.setReady(&cr, metav1.ConditionFalse, temporalv1alpha1.ReasonProxyNotReady, "proxy not fully converged")
	}
	return ctrl.Result{RequeueAfter: namespaceDriftRequeue}, r.statusUpdate(ctx, &cr)
}

// registerPeer dials the local Temporal frontend and registers the peer with the
// local proxy tcpServer address as its frontend.
func (r *TemporalClusterProxyReconciler) registerPeer(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy, tlsConfig interface{ /* *tls.Config */ }) error {
	target, err := resolveTarget(ctx, r.Client, cr.Namespace, cr.Spec.LocalClusterRef)
	if err != nil {
		return err
	}
	c, err := r.clientFactory()(ctx, target.Address, target.TLSConfig)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	proxyAddr := fmt.Sprintf("%s.%s.svc.cluster.local:%d", resources.ClusterProxyServiceName(cr), cr.Namespace, resources.ProxyTCPServerPort)
	enable := cr.Spec.Peer.EnableConnection == nil || *cr.Spec.Peer.EnableConnection
	return c.UpsertRemoteCluster(ctx, proxyAddr, enable)
}

func (r *TemporalClusterProxyReconciler) reconcileDelete(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy) error {
	if !controllerutil.ContainsFinalizer(cr, clusterProxyFinalizer) {
		return nil
	}
	target, err := resolveTarget(ctx, r.Client, cr.Namespace, cr.Spec.LocalClusterRef)
	if err == nil && target.Ready {
		if c, derr := r.clientFactory()(ctx, target.Address, target.TLSConfig); derr == nil {
			_ = c.RemoveRemoteCluster(ctx, cr.Spec.Peer.Name)
			_ = c.Close()
		}
	}
	// Owned resources are GC'd via owner references.
	controllerutil.RemoveFinalizer(cr, clusterProxyFinalizer)
	return r.Update(ctx, cr)
}

func (r *TemporalClusterProxyReconciler) deploymentAvailable(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy) bool {
	var dep appsv1.Deployment
	key := types.NamespacedName{Namespace: cr.Namespace, Name: resources.ClusterProxyName(cr)}
	if err := r.Get(ctx, key, &dep); err != nil {
		return false
	}
	return dep.Status.AvailableReplicas > 0
}

func (r *TemporalClusterProxyReconciler) serverEndpoint(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy) string {
	// For LoadBalancer, surface the assigned ingress; otherwise the in-cluster DNS name.
	var svc corev1Service
	_ = svc // resolved below
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d", resources.ClusterProxyServiceName(cr), cr.Namespace, cr.Spec.Mux.Server.ListenPort)
}

func (r *TemporalClusterProxyReconciler) apply(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy, obj client.Object) error {
	if err := controllerutil.SetControllerReference(cr, obj, r.Scheme); err != nil {
		return err
	}
	return serverSideApply(ctx, r.Client, r.Scheme, obj, proxyServicesFieldOwner)
}

func (r *TemporalClusterProxyReconciler) clientFactory() temporal.RemoteClusterClientFactory {
	if r.ClientFactory != nil {
		return r.ClientFactory
	}
	return temporal.NewRemoteClusterClient
}

func (r *TemporalClusterProxyReconciler) setReady(cr *temporalv1alpha1.TemporalClusterProxy, status metav1.ConditionStatus, reason, message string) {
	cr.Status.ObservedGeneration = cr.Generation
	meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cr.Generation,
	})
}

func (r *TemporalClusterProxyReconciler) statusUpdate(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy) error {
	return client.IgnoreNotFound(r.Status().Update(ctx, cr))
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalClusterProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalClusterProxy{}).
		Owns(&appsv1.Deployment{}).
		Named("temporalclusterproxy").
		Complete(r)
}
```

**IMPLEMENTATION FIXUPS (do these while writing, they are deliberate simplifications above):**
1. Remove the unused `tlsConfig interface{...}` parameter from `registerPeer` — it re-resolves the target itself, so the signature is `registerPeer(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy) error`, and the caller is `r.registerPeer(ctx, &cr)`.
2. Replace the placeholder `corev1Service` in `serverEndpoint` with a real read: `import corev1 "k8s.io/api/core/v1"`, `var svc corev1.Service; if err := r.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: resources.ClusterProxyServiceName(cr)}, &svc); err == nil { for _, ing := range svc.Status.LoadBalancer.Ingress { host := ing.Hostname; if host == "" { host = ing.IP }; if host != "" { return fmt.Sprintf("%s:%d", host, cr.Spec.Mux.Server.ListenPort) } } }` then fall through to the DNS name.
3. Drop the unused `logf` import if only used once — keep it; it is used in `registerPeer`'s caller.

- [ ] **Step 2: Write the failing envtest**

Create `internal/controller/temporalclusterproxy_controller_test.go` following the structure of `temporalclusterconnection_controller_test.go` (Ginkgo, uses the shared `suite_test.go` `k8sClient`/`cfg`). Minimum coverage:

```go
package controller

// Ginkgo spec (mirror temporalclusterconnection_controller_test.go layout):
// It("deploys the proxy and sets conditions", func() {
//   - create a TemporalCluster "cluster-a" (or a minimal Ready one, as the
//     connection test does) in a fresh namespace
//   - create a TemporalClusterProxy referencing it, role=server, listenPort 6334,
//     tls.provider=secret + secretRef to a pre-created Secret (avoids needing
//     cert-manager in envtest)
//   - reconcile via a TemporalClusterProxyReconciler{Client, Scheme, ClientFactory: fakeFactory}
//   - Eventually: a Deployment named cluster-a-s2s-proxy exists, a ConfigMap and
//     Service exist, all with an ownerRef to the proxy CR
//   - the ProxyDeployed condition is present
// })
```

Use the existing `fakeRemoteClient` and factory pattern from `temporalclusterconnection_controller_test.go` (lines ~49–90). If the connection test constructs `k8sClient` objects via helper builders, reuse them. Provide a BYO TLS Secret so no cert-manager dependency is needed:

```go
tlsSecret := &corev1.Secret{
	ObjectMeta: metav1.ObjectMeta{Name: "mux-tls", Namespace: ns},
	Data: map[string][]byte{"tls.crt": []byte("x"), "tls.key": []byte("y"), "ca.crt": []byte("z")},
}
```

and set `cr.Spec.Mux.TLS = temporalv1alpha1.ProxyMuxTLS{Provider: "secret", SecretRef: &temporalv1alpha1.SecretReference{Name: "mux-tls"}}`.

- [ ] **Step 3: Run to verify failure**

Run: `make test` (envtest). Expected: the new spec FAILS (reconciler/objects not created) while the rest pass. If compilation fails first, fix compile errors, then observe the intended assertion failure.

- [ ] **Step 4: Make the test pass**

Apply the fixups from Step 1; ensure the reconciler compiles and the spec passes.

Run: `make test`
Expected: PASS (whole suite).

- [ ] **Step 5: Register in main.go**

In `cmd/main.go`, after the `TemporalClusterConnectionReconciler` block (around line 251), add:

```go
	if err := (&controller.TemporalClusterProxyReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TemporalClusterProxy")
		os.Exit(1)
	}
```

- [ ] **Step 6: Regenerate RBAC + chart**

Run: `make manifests helm-chart`
Expected: `config/rbac/role.yaml` gains the new rules; `dist/chart` updated. Remove `.github/workflows/test-chart.yml` if it reappears.

- [ ] **Step 7: Build + commit**

Run: `go build ./... && make lint`
Expected: success, no lint errors.

```bash
git add internal/controller/ cmd/main.go config/ dist/chart/
git commit -s -m "feat(controller): reconcile TemporalClusterProxy (deploy + register)

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 5: Webhook validation + defaulting

**Files:**
- Create: `internal/webhook/v1alpha1/temporalclusterproxy_webhook.go`
- Test: `internal/webhook/v1alpha1/temporalclusterproxy_webhook_test.go`
- Modify: `cmd/main.go`

**Interfaces:**
- Consumes: `TemporalClusterProxy` types.
- Produces: `SetupTemporalClusterProxyWebhookWithManager(mgr) error`; `TemporalClusterProxyCustomValidator`; `TemporalClusterProxyCustomDefaulter`.

- [ ] **Step 1: Write the failing test**

Create `internal/webhook/v1alpha1/temporalclusterproxy_webhook_test.go` (mirror `temporalclusterconnection_webhook_test.go`). Cases:

```go
// server role without server block -> error
// client role without client block -> error
// tls provider cert-manager without issuerRef -> error
// tls provider secret without secretRef -> error
// peer.name == localClusterName -> error
// valid server CR -> no error
// defaulter fills tls.provider=cert-manager, peer.enableConnection=true, image
```

Write these as plain Go table tests calling `(&TemporalClusterProxyCustomValidator{}).ValidateCreate(ctx, cr)` and `(&TemporalClusterProxyCustomDefaulter{}).Default(ctx, cr)`.

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/webhook/v1alpha1/ -run TestTemporalClusterProxy -v`
Expected: FAIL — undefined validator/defaulter.

- [ ] **Step 3: Write the webhook**

Create `internal/webhook/v1alpha1/temporalclusterproxy_webhook.go` (Apache header):

```go
package v1alpha1

import (
	"context"

	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

var temporalclusterproxylog = logf.Log.WithName("temporalclusterproxy-resource")

// SetupTemporalClusterProxyWebhookWithManager registers the webhook.
func SetupTemporalClusterProxyWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &temporalv1alpha1.TemporalClusterProxy{}).
		WithValidator(&TemporalClusterProxyCustomValidator{}).
		WithDefaulter(&TemporalClusterProxyCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-temporal-bmor10-com-v1alpha1-temporalclusterproxy,mutating=false,failurePolicy=fail,sideEffects=None,groups=temporal.bmor10.com,resources=temporalclusterproxies,verbs=create;update,versions=v1alpha1,name=vtemporalclusterproxy-v1alpha1.kb.io,admissionReviewVersions=v1
// +kubebuilder:webhook:path=/mutate-temporal-bmor10-com-v1alpha1-temporalclusterproxy,mutating=true,failurePolicy=fail,sideEffects=None,groups=temporal.bmor10.com,resources=temporalclusterproxies,verbs=create;update,versions=v1alpha1,name=mtemporalclusterproxy-v1alpha1.kb.io,admissionReviewVersions=v1

// TemporalClusterProxyCustomDefaulter defaults optional fields.
type TemporalClusterProxyCustomDefaulter struct{}

var _ admission.Defaulter[*temporalv1alpha1.TemporalClusterProxy] = &TemporalClusterProxyCustomDefaulter{}

func (d *TemporalClusterProxyCustomDefaulter) Default(_ context.Context, cr *temporalv1alpha1.TemporalClusterProxy) error {
	if cr.Spec.Mux.TLS.Provider == "" {
		cr.Spec.Mux.TLS.Provider = "cert-manager"
	}
	if cr.Spec.Peer.EnableConnection == nil {
		enable := true
		cr.Spec.Peer.EnableConnection = &enable
	}
	if cr.Spec.Image == "" {
		cr.Spec.Image = resources.DefaultClusterProxyImage
	}
	return nil
}

// TemporalClusterProxyCustomValidator validates the CR.
type TemporalClusterProxyCustomValidator struct{}

var _ admission.Validator[*temporalv1alpha1.TemporalClusterProxy] = &TemporalClusterProxyCustomValidator{}

func validateClusterProxy(cr *temporalv1alpha1.TemporalClusterProxy) field.ErrorList {
	var errs field.ErrorList
	specPath := field.NewPath("spec")

	if cr.Spec.LocalClusterRef.Name == "" {
		errs = append(errs, field.Required(specPath.Child("localClusterRef", "name"), "local cluster reference is required"))
	}
	if cr.Spec.Peer.Name == "" {
		errs = append(errs, field.Required(specPath.Child("peer", "name"), "peer name is required"))
	}
	if cr.Spec.Peer.Name != "" && cr.Spec.Peer.Name == cr.Spec.LocalClusterName {
		errs = append(errs, field.Invalid(specPath.Child("peer", "name"), cr.Spec.Peer.Name, "peer name must differ from localClusterName"))
	}

	muxPath := specPath.Child("mux")
	switch cr.Spec.Mux.Role {
	case temporalv1alpha1.ProxyRoleServer:
		if cr.Spec.Mux.Server == nil {
			errs = append(errs, field.Required(muxPath.Child("server"), "server is required for role=server"))
		}
		if cr.Spec.Mux.Client != nil {
			errs = append(errs, field.Invalid(muxPath.Child("client"), "client", "client must be unset for role=server"))
		}
	case temporalv1alpha1.ProxyRoleClient:
		if cr.Spec.Mux.Client == nil || cr.Spec.Mux.Client.ServerAddress == "" {
			errs = append(errs, field.Required(muxPath.Child("client", "serverAddress"), "client.serverAddress is required for role=client"))
		}
		if cr.Spec.Mux.Server != nil {
			errs = append(errs, field.Invalid(muxPath.Child("server"), "server", "server must be unset for role=client"))
		}
	default:
		errs = append(errs, field.NotSupported(muxPath.Child("role"), cr.Spec.Mux.Role, []string{temporalv1alpha1.ProxyRoleServer, temporalv1alpha1.ProxyRoleClient}))
	}

	tlsPath := muxPath.Child("tls")
	switch cr.Spec.Mux.TLS.Provider {
	case "", "cert-manager":
		if cr.Spec.Mux.TLS.IssuerRef == nil || cr.Spec.Mux.TLS.IssuerRef.Name == "" {
			errs = append(errs, field.Required(tlsPath.Child("issuerRef"), "issuerRef is required for provider=cert-manager"))
		}
	case "secret":
		if cr.Spec.Mux.TLS.SecretRef == nil || cr.Spec.Mux.TLS.SecretRef.Name == "" {
			errs = append(errs, field.Required(tlsPath.Child("secretRef"), "secretRef is required for provider=secret"))
		}
	default:
		errs = append(errs, field.NotSupported(tlsPath.Child("provider"), cr.Spec.Mux.TLS.Provider, []string{"cert-manager", "secret"}))
	}
	return errs
}

func (v *TemporalClusterProxyCustomValidator) ValidateCreate(_ context.Context, cr *temporalv1alpha1.TemporalClusterProxy) (admission.Warnings, error) {
	temporalclusterproxylog.Info("validate create", "name", cr.GetName())
	if errs := validateClusterProxy(cr); len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	return nil, nil
}

func (v *TemporalClusterProxyCustomValidator) ValidateUpdate(_ context.Context, oldCR, newCR *temporalv1alpha1.TemporalClusterProxy) (admission.Warnings, error) {
	temporalclusterproxylog.Info("validate update", "name", newCR.GetName())
	var errs field.ErrorList
	if oldCR.Spec.Mux.Role != newCR.Spec.Mux.Role {
		errs = append(errs, field.Forbidden(field.NewPath("spec", "mux", "role"), "mux.role is immutable"))
	}
	if oldCR.Spec.LocalClusterRef.Name != newCR.Spec.LocalClusterRef.Name {
		errs = append(errs, field.Forbidden(field.NewPath("spec", "localClusterRef"), "localClusterRef is immutable"))
	}
	errs = append(errs, validateClusterProxy(newCR)...)
	if len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	return nil, nil
}

func (v *TemporalClusterProxyCustomValidator) ValidateDelete(_ context.Context, _ *temporalv1alpha1.TemporalClusterProxy) (admission.Warnings, error) {
	return nil, nil
}
```

Note: if the `Defaulter` mutating webhook causes an import cycle or the webhook package must not import `internal/resources`, inline the default image string constant instead of importing `resources`. Check whether other webhooks import `internal/resources`; if not, define a local `const defaultProxyImage = "temporalio/s2s-proxy:v0.2.1"` and use it in the defaulter.

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/webhook/v1alpha1/ -run TestTemporalClusterProxy -v`
Expected: PASS.

- [ ] **Step 5: Register webhook in main.go**

In `cmd/main.go`, add another `webhooksEnabled` block after the connection webhook (around line 282):

```go
	if webhooksEnabled {
		if err := webhookv1alpha1.SetupTemporalClusterProxyWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "TemporalClusterProxy")
			os.Exit(1)
		}
	}
```

- [ ] **Step 6: Regenerate + full validation**

Run: `make manifests generate helm-chart && make lint test build`
Expected: all green. `config/webhook/manifests.yaml` gains the new webhook entries; remove `.github/workflows/test-chart.yml` if it reappears.

- [ ] **Step 7: Commit**

```bash
git add internal/webhook/ cmd/main.go config/ dist/chart/
git commit -s -m "feat(webhook): validate and default TemporalClusterProxy

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 6: Examples + docs + e2e

**Files:**
- Create: `examples/cluster-proxy-mux/server.yaml`, `examples/cluster-proxy-mux/client.yaml`
- Create: `test/e2e/clusterproxy/chainsaw-test.yaml` (+ supporting manifests)
- Modify: docs/examples index if one exists (follow the pattern in `docs/content/examples` — check first).

- [ ] **Step 1: Write the example CRs**

`examples/cluster-proxy-mux/server.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalClusterProxy
metadata:
  name: to-cluster-a
  namespace: temporal-system
spec:
  localClusterRef:
    name: cluster-b
  localClusterName: cluster-b
  peer:
    name: cluster-a
  mux:
    role: server
    server:
      listenPort: 6334
      exposure:
        type: LoadBalancer
    tls:
      provider: cert-manager
      issuerRef:
        name: temporal-ca
```

`examples/cluster-proxy-mux/client.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalClusterProxy
metadata:
  name: to-cluster-b
  namespace: temporal-system
spec:
  localClusterRef:
    name: cluster-a
  localClusterName: cluster-a
  peer:
    name: cluster-b
  mux:
    role: client
    client:
      serverAddress: cluster-b-proxy.example.com:6334
    tls:
      provider: cert-manager
      issuerRef:
        name: temporal-ca
      peerCARef:
        name: cluster-b-mux-ca
```

- [ ] **Step 2: Validate examples against the CRD (dry run)**

Run (against a cluster or with `kubectl --dry-run=client` after `kubectl apply -f config/crd/bases`): confirm both manifests pass schema validation. If no cluster is available, run `go test ./internal/webhook/v1alpha1/ -run TestTemporalClusterProxy` after loading these YAMLs via a small test, or validate manually with the webhook validator function.

- [ ] **Step 3: Write the chainsaw e2e**

Create `test/e2e/clusterproxy/chainsaw-test.yaml` following the layout of `test/e2e/devserver/chainsaw-test.yaml` and `test/e2e/migration/` (both are multi-step replication tests). The e2e should: stand up two clusters (or one cluster + one dev server if single-network is acceptable for CI), apply a server + client `TemporalClusterProxy`, wait for both to reach `Ready=True`, register a global namespace, and assert replication of a workflow across the link. Reuse the existing migration e2e's worker/namespace manifests as a template.

Because two-network mux is hard to reproduce in a single kind cluster, the CI variant may run both proxies in one cluster (server role reachable via its ClusterIP Service, client `serverAddress` = that Service DNS) to exercise the full path deterministically.

- [ ] **Step 4: Run the e2e locally**

Run the project's e2e entrypoint (check `Makefile` for the chainsaw target, e.g. `make test-e2e` or the nsc runner). Expected: the clusterproxy suite passes. If the two-cluster topology is infeasible in the available CI, mark the suite as the deterministic single-cluster variant described in Step 3 and note the true cross-network path is validated manually.

- [ ] **Step 5: Final full validation**

Run: `make generate manifests api-docs docs-crd-reference helm-chart && make lint test build`
Then `git status`: ensure only intended files changed; remove `.github/workflows/test-chart.yml` if present/untracked.

- [ ] **Step 6: Commit**

```bash
git add examples/ test/e2e/clusterproxy/ docs/
git commit -s -m "test(e2e): TemporalClusterProxy mux replication + examples

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Self-review notes (already applied)

- **Spec coverage:** API (Task 1) ↔ spec §API; rendering (Task 2) ↔ §Config rendering; builders (Task 3) ↔ §Rendered resources; reconcile/registration/finalizer/status (Task 4) ↔ §Reconciliation + §Error handling; validation/defaulting (Task 5) ↔ §Webhook validation; testing + examples + e2e (Tasks 2–6) ↔ §Testing + §Delivery; chart/docs regen folded into each task per §Delivery.
- **Known simplifications flagged inline for the implementer:** the `registerPeer` signature and the `serverEndpoint` LoadBalancer read in Task 4 Step 1 are marked as fixups to apply while writing (the first-draft code intentionally shows the shape, the fixups make it compile).
- **Type consistency:** builder/name helpers (`ClusterProxyName`, `ClusterProxyServiceName`, `ClusterProxyTLSSecretName`, `ProxyTCPServerPort`, `ProxyTLSMountPath`) are defined in Tasks 2–3 and referenced verbatim in Task 4. Condition/reason constants are defined in Task 1 Step 2 and used in Task 4.
- **External assumptions already verified against the pinned `v0.2.1` release:** image tag (`temporalio/s2s-proxy:v0.2.1`), the container entrypoint (`CONFIG_YML` env var → `s2s-proxy start --config`), and the config keys used by the render func (`clusterConnections`, `tcpClient`/`tcpServer`, `mux-server`/`mux-client`, `muxAddressInfo.tls`, `namespaceTranslation`, `searchAttributeTranslation`, `failoverVersionIncrementTranslation`, `aclPolicy`) — all present in the upstream `develop/config` examples at that ref.
