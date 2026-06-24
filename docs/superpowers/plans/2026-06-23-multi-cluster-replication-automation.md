# Multi-Cluster Replication Automation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Automate Temporal multi-cluster replication in the operator: a new `TemporalClusterConnection` CRD whose controller registers remote-cluster connections automatically, plus declarative namespace failover via a `clusters`/`activeCluster` extension to `TemporalNamespace`.

**Architecture:** Builds on PR #92's config-only foundation (typed `clusterMetadata` rendering + `isGlobal` namespace field). Adds (1) a `RemoteClusterClient` wrapping Temporal's `OperatorService` remote-cluster RPCs, (2) a `TemporalClusterConnection` CRD + controller that reconciles `AddOrUpdateRemoteCluster`/`RemoveRemoteCluster` across local peers, and (3) namespace replication-config + failover via `UpdateNamespace`. Each cluster owns its failover versions (reused from #92); the connection controller validates cross-cluster consistency.

**Tech Stack:** Go, controller-runtime (kubebuilder), `go.temporal.io/api` gRPC clients, Ginkgo/Gomega + envtest, Chainsaw (kyverno) for e2e, controller-gen for CRD/RBAC.

## Global Constraints

- Module path: `github.com/bmorton/temporal-operator`; API group `temporal.bmor10.com`, version `v1alpha1`.
- Conventional Commits + DCO sign-off on every commit (`git commit -s`).
- Run `make generate manifests` after any API type change; run `make lint` before finishing.
- All Temporal calls go through injectable factories (`...Factory` func types) so controllers are unit-testable with in-memory fakes — never dial real gRPC in unit tests.
- controller-runtime typed webhook API: `var _ admission.Validator[*T] = ...`; validator methods are `ValidateCreate(ctx, *T)`, `ValidateUpdate(ctx, old, new *T)`, `ValidateDelete(ctx, *T)` returning `(admission.Warnings, error)`.
- Eventual-consistency: replication lag is never a hard error — report via status conditions and requeue, mirroring `namespaceDriftRequeue` (5m).
- Reuse existing condition/reason constants in `api/v1alpha1/conditions.go`; add new ones there when needed.
- Copyright header block (Apache 2.0, "Copyright 2026 Brian Morton.") on every new `.go` file — copy verbatim from any existing file in the same package.

---

### Task 1: `SecretReference` shared type

**Files:**
- Modify: `api/v1alpha1/shared_types.go` (append type near `ClusterReference`, end of file ~line 246)

**Interfaces:**
- Produces: `temporalv1alpha1.SecretReference{ Name string; CAKey, CertKey, KeyKey string }`

- [ ] **Step 1: Add the type**

In `api/v1alpha1/shared_types.go`, after the `ClusterReference` type, add:

```go
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
```

- [ ] **Step 2: Regenerate deepcopy**

Run: `make generate`
Expected: `api/v1alpha1/zz_generated.deepcopy.go` gains `SecretReference` deepcopy funcs; no errors.

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: exit 0.

- [ ] **Step 4: Commit**

```bash
git add api/v1alpha1/shared_types.go api/v1alpha1/zz_generated.deepcopy.go
git commit -s -m "feat(api): add SecretReference shared type for external peer TLS"
```

---

### Task 2: `RemoteClusterClient` interface + gRPC implementation

**Files:**
- Modify: `internal/temporal/client.go`
- Modify: `internal/temporal/client_test.go`

**Interfaces:**
- Consumes: existing `grpcNamespaceClient` (already holds `operator operatorservice.OperatorServiceClient`), `NewNamespaceClient`.
- Produces:
  - `type RemoteClusterInfo struct { Name, Address string; InitialFailoverVersion int64; ConnectionEnabled bool; HistoryShardCount int32 }`
  - `type RemoteClusterClient interface { ListRemoteClusters(ctx) ([]RemoteClusterInfo, error); UpsertRemoteCluster(ctx, frontendAddress string, enableConnection bool) error; RemoveRemoteCluster(ctx, name string) error; Close() error }`
  - `type RemoteClusterClientFactory func(ctx context.Context, address string, tlsConfig *tls.Config) (RemoteClusterClient, error)`
  - `func NewRemoteClusterClient(ctx, address string, tlsConfig *tls.Config) (RemoteClusterClient, error)`

- [ ] **Step 1: Write the failing test**

In `internal/temporal/client_test.go`, add (the package already tests via a fake operator client; mirror its existing style — if a `fakeOperatorServer`/bufconn harness exists, reuse it; otherwise add a minimal table test asserting the request mapping by injecting a fake `operatorservice.OperatorServiceClient`). Add:

```go
func TestRemoteClusterInfoMapping(t *testing.T) {
	in := &operatorservice.ClusterMetadata{
		ClusterName:            "clusterB",
		Address:                "b.example.com:7233",
		InitialFailoverVersion: 2,
		IsConnectionEnabled:    true,
		HistoryShardCount:      512,
	}
	got := remoteClusterInfoFromProto(in)
	if got.Name != "clusterB" || got.Address != "b.example.com:7233" ||
		got.InitialFailoverVersion != 2 || !got.ConnectionEnabled || got.HistoryShardCount != 512 {
		t.Fatalf("unexpected mapping: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/temporal/ -run TestRemoteClusterInfoMapping`
Expected: FAIL — `remoteClusterInfoFromProto` undefined.

- [ ] **Step 3: Implement**

In `internal/temporal/client.go` add (after the `SearchAttribute` section). The `replicationpb` import alias is also needed in Task 6 — add `replicationpb "go.temporal.io/api/replication/v1"` to imports now if convenient, otherwise in Task 6.

```go
// RemoteClusterInfo is the observed state of a remote cluster connection.
type RemoteClusterInfo struct {
	Name                   string
	Address                string
	InitialFailoverVersion int64
	ConnectionEnabled      bool
	HistoryShardCount      int32
}

// RemoteClusterClient manages remote-cluster connections on a Temporal cluster.
type RemoteClusterClient interface {
	ListRemoteClusters(ctx context.Context) ([]RemoteClusterInfo, error)
	UpsertRemoteCluster(ctx context.Context, frontendAddress string, enableConnection bool) error
	RemoveRemoteCluster(ctx context.Context, name string) error
	Close() error
}

// RemoteClusterClientFactory builds a RemoteClusterClient connected to a frontend.
type RemoteClusterClientFactory func(ctx context.Context, address string, tlsConfig *tls.Config) (RemoteClusterClient, error)

// NewRemoteClusterClient dials the frontend and returns a RemoteClusterClient.
func NewRemoteClusterClient(ctx context.Context, address string, tlsConfig *tls.Config) (RemoteClusterClient, error) {
	c, err := NewNamespaceClient(ctx, address, tlsConfig)
	if err != nil {
		return nil, err
	}
	return c.(*grpcNamespaceClient), nil
}

func remoteClusterInfoFromProto(m *operatorservice.ClusterMetadata) RemoteClusterInfo {
	return RemoteClusterInfo{
		Name:                   m.GetClusterName(),
		Address:                m.GetAddress(),
		InitialFailoverVersion: m.GetInitialFailoverVersion(),
		ConnectionEnabled:      m.GetIsConnectionEnabled(),
		HistoryShardCount:      m.GetHistoryShardCount(),
	}
}

func (c *grpcNamespaceClient) ListRemoteClusters(ctx context.Context) ([]RemoteClusterInfo, error) {
	var out []RemoteClusterInfo
	var pageToken []byte
	for {
		resp, err := c.operator.ListClusters(ctx, &operatorservice.ListClustersRequest{
			PageSize:      100,
			NextPageToken: pageToken,
		})
		if err != nil {
			return nil, err
		}
		for _, m := range resp.GetClusters() {
			out = append(out, remoteClusterInfoFromProto(m))
		}
		pageToken = resp.GetNextPageToken()
		if len(pageToken) == 0 {
			break
		}
	}
	return out, nil
}

func (c *grpcNamespaceClient) UpsertRemoteCluster(ctx context.Context, frontendAddress string, enableConnection bool) error {
	_, err := c.operator.AddOrUpdateRemoteCluster(ctx, &operatorservice.AddOrUpdateRemoteClusterRequest{
		FrontendAddress:               frontendAddress,
		EnableRemoteClusterConnection: enableConnection,
	})
	return err
}

func (c *grpcNamespaceClient) RemoveRemoteCluster(ctx context.Context, name string) error {
	_, err := c.operator.RemoveRemoteCluster(ctx, &operatorservice.RemoveRemoteClusterRequest{
		ClusterName: name,
	})
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/temporal/ -run TestRemoteClusterInfoMapping`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/temporal/client.go internal/temporal/client_test.go
git commit -s -m "feat(temporal): add RemoteClusterClient for remote-cluster registration"
```

---

### Task 3: `TemporalClusterConnection` API types

**Files:**
- Create: `api/v1alpha1/temporalclusterconnection_types.go`
- Modify: `api/v1alpha1/conditions.go` (add reasons)

**Interfaces:**
- Produces: `TemporalClusterConnection`, `TemporalClusterConnectionList`, `TemporalClusterConnectionSpec{ Peers []ClusterConnectionPeer }`, `ClusterConnectionPeer{ Name string; ClusterRef *ClusterReference; FrontendAddress string; TLSSecretRef *SecretReference; EnableConnection *bool }`, `TemporalClusterConnectionStatus{ ObservedGeneration int64; Peers []PeerConnectionStatus; Conditions []metav1.Condition }`, `PeerConnectionStatus{ Name string; Reachable, Connected bool; Message string }`.

- [ ] **Step 1: Create the types file**

Create `api/v1alpha1/temporalclusterconnection_types.go` (copy the Apache header from `temporalclusterclient_types.go`):

```go
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
// +kubebuilder:printcolumn:name="Peers",type=integer,JSONPath=`.spec.peers[*]`
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
```

- [ ] **Step 2: Add condition reasons**

In `api/v1alpha1/conditions.go`, inside the reasons `const (...)` block, add:

```go
	ReasonPeersConnected     = "PeersConnected"
	ReasonPeerUnreachable    = "PeerUnreachable"
	ReasonReplicationDrift   = "ReplicationConfigDrift"
	ReasonActiveClusterInvalid = "ActiveClusterInvalid"
```

- [ ] **Step 3: Generate + build**

Run: `make generate && go build ./...`
Expected: deepcopy generated for the new types; exit 0.

- [ ] **Step 4: Commit**

```bash
git add api/v1alpha1/temporalclusterconnection_types.go api/v1alpha1/conditions.go api/v1alpha1/zz_generated.deepcopy.go
git commit -s -m "feat(api): add TemporalClusterConnection CRD types"
```

---

### Task 4: `TemporalClusterConnection` validating webhook

**Files:**
- Create: `internal/webhook/v1alpha1/temporalclusterconnection_webhook.go`
- Create: `internal/webhook/v1alpha1/temporalclusterconnection_webhook_test.go`

**Interfaces:**
- Consumes: `temporalv1alpha1.TemporalClusterConnection`.
- Produces: `func SetupTemporalClusterConnectionWebhookWithManager(mgr ctrl.Manager) error`; `TemporalClusterConnectionCustomValidator`.

- [ ] **Step 1: Write failing tests**

Create `internal/webhook/v1alpha1/temporalclusterconnection_webhook_test.go`. Mirror the table style in `temporalcluster_webhook_test.go`. Cover: (a) <2 peers rejected; (b) duplicate peer names rejected; (c) peer with neither clusterRef nor frontendAddress rejected; (d) peer with both set rejected; (e) tlsSecretRef with clusterRef (no frontendAddress) rejected; (f) a valid two-peer connection accepted.

```go
package v1alpha1

import (
	"context"
	"testing"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func conn(peers ...temporalv1alpha1.ClusterConnectionPeer) *temporalv1alpha1.TemporalClusterConnection {
	return &temporalv1alpha1.TemporalClusterConnection{
		Spec: temporalv1alpha1.TemporalClusterConnectionSpec{Peers: peers},
	}
}

func localPeer(name, ref string) temporalv1alpha1.ClusterConnectionPeer {
	return temporalv1alpha1.ClusterConnectionPeer{Name: name, ClusterRef: &temporalv1alpha1.ClusterReference{Name: ref}}
}

func TestClusterConnectionValidateCreate(t *testing.T) {
	v := &TemporalClusterConnectionCustomValidator{}
	cases := []struct {
		name    string
		obj     *temporalv1alpha1.TemporalClusterConnection
		wantErr bool
	}{
		{"valid", conn(localPeer("a", "cluster-a"), localPeer("b", "cluster-b")), false},
		{"too-few", conn(localPeer("a", "cluster-a")), true},
		{"dup-names", conn(localPeer("a", "cluster-a"), localPeer("a", "cluster-b")), true},
		{"no-source", conn(localPeer("a", "cluster-a"), temporalv1alpha1.ClusterConnectionPeer{Name: "b"}), true},
		{"both-source", conn(localPeer("a", "cluster-a"), temporalv1alpha1.ClusterConnectionPeer{
			Name: "b", ClusterRef: &temporalv1alpha1.ClusterReference{Name: "x"}, FrontendAddress: "y:7233",
		}), true},
		{"tls-without-external", conn(localPeer("a", "cluster-a"), temporalv1alpha1.ClusterConnectionPeer{
			Name: "b", ClusterRef: &temporalv1alpha1.ClusterReference{Name: "x"},
			TLSSecretRef: &temporalv1alpha1.SecretReference{Name: "s"},
		}), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := v.ValidateCreate(context.Background(), tc.obj)
			if tc.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v got err=%v", tc.wantErr, err)
			}
		})
	}
}
```

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/webhook/v1alpha1/ -run TestClusterConnectionValidateCreate`
Expected: FAIL — validator type undefined.

- [ ] **Step 3: Implement the webhook**

Create `internal/webhook/v1alpha1/temporalclusterconnection_webhook.go` (Apache header; mirror `temporalcluster_webhook.go`'s `field.ErrorList` style):

```go
package v1alpha1

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

var temporalclusterconnectionlog = logf.Log.WithName("temporalclusterconnection-resource")

func SetupTemporalClusterConnectionWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &temporalv1alpha1.TemporalClusterConnection{}).
		WithValidator(&TemporalClusterConnectionCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-temporal-bmor10-com-v1alpha1-temporalclusterconnection,mutating=false,failurePolicy=fail,sideEffects=None,groups=temporal.bmor10.com,resources=temporalclusterconnections,verbs=create;update,versions=v1alpha1,name=vtemporalclusterconnection-v1alpha1.kb.io,admissionReviewVersions=v1

type TemporalClusterConnectionCustomValidator struct{}

var _ admission.Validator[*temporalv1alpha1.TemporalClusterConnection] = &TemporalClusterConnectionCustomValidator{}

func validatePeers(c *temporalv1alpha1.TemporalClusterConnection) field.ErrorList {
	var errs field.ErrorList
	peersPath := field.NewPath("spec", "peers")
	seen := map[string]bool{}
	for i, p := range c.Spec.Peers {
		path := peersPath.Index(i)
		if p.Name == "" {
			errs = append(errs, field.Required(path.Child("name"), "peer name is required"))
		}
		if seen[p.Name] {
			errs = append(errs, field.Duplicate(path.Child("name"), p.Name))
		}
		seen[p.Name] = true

		hasRef := p.ClusterRef != nil && p.ClusterRef.Name != ""
		hasAddr := p.FrontendAddress != ""
		switch {
		case hasRef && hasAddr:
			errs = append(errs, field.Invalid(path, p.Name, "set exactly one of clusterRef or frontendAddress"))
		case !hasRef && !hasAddr:
			errs = append(errs, field.Invalid(path, p.Name, "one of clusterRef or frontendAddress is required"))
		}
		if p.TLSSecretRef != nil && !hasAddr {
			errs = append(errs, field.Invalid(path.Child("tlsSecretRef"), p.TLSSecretRef.Name, "tlsSecretRef is only valid with frontendAddress"))
		}
	}
	return errs
}

func (v *TemporalClusterConnectionCustomValidator) ValidateCreate(_ context.Context, c *temporalv1alpha1.TemporalClusterConnection) (admission.Warnings, error) {
	temporalclusterconnectionlog.Info("validate create", "name", c.GetName())
	if len(c.Spec.Peers) < 2 {
		return nil, fmt.Errorf("spec.peers must contain at least two peers")
	}
	if errs := validatePeers(c); len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	return nil, nil
}

func (v *TemporalClusterConnectionCustomValidator) ValidateUpdate(_ context.Context, _, newC *temporalv1alpha1.TemporalClusterConnection) (admission.Warnings, error) {
	temporalclusterconnectionlog.Info("validate update", "name", newC.GetName())
	if len(newC.Spec.Peers) < 2 {
		return nil, fmt.Errorf("spec.peers must contain at least two peers")
	}
	if errs := validatePeers(newC); len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	return nil, nil
}

func (v *TemporalClusterConnectionCustomValidator) ValidateDelete(_ context.Context, _ *temporalv1alpha1.TemporalClusterConnection) (admission.Warnings, error) {
	return nil, nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/webhook/v1alpha1/ -run TestClusterConnectionValidateCreate`
Expected: PASS.

- [ ] **Step 5: Register in main.go + webhook suite**

In `cmd/main.go`, after the existing webhook setup blocks (before `// +kubebuilder:scaffold:builder`), add:

```go
	if webhooksEnabled {
		if err := webhookv1alpha1.SetupTemporalClusterConnectionWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "TemporalClusterConnection")
			os.Exit(1)
		}
	}
```

In `internal/webhook/v1alpha1/webhook_suite_test.go`, register the webhook in `BeforeSuite` alongside the others (find the existing `SetupTemporal...WebhookWithManager(mgr)` calls and add the connection one).

- [ ] **Step 6: Build + commit**

Run: `go build ./...`

```bash
git add cmd/main.go internal/webhook/v1alpha1/
git commit -s -m "feat(webhook): validate TemporalClusterConnection peers"
```

---

### Task 5: `TemporalClusterConnection` controller

**Files:**
- Create: `internal/controller/temporalclusterconnection_controller.go`
- Create: `internal/controller/temporalclusterconnection_controller_test.go`
- Modify: `cmd/main.go` (reconciler registration)
- Modify: `internal/controller/suite_test.go` (no scheme change needed — `AddToScheme` already covers all kinds)

**Interfaces:**
- Consumes: `temporal.RemoteClusterClientFactory`, `resolveTarget` (target.go), `frontendAddress` (temporalnamespace_controller.go), `temporalv1alpha1.TemporalClusterConnection`.
- Produces: `TemporalClusterConnectionReconciler{ client.Client; Scheme *runtime.Scheme; ClientFactory temporal.RemoteClusterClientFactory }` with `Reconcile` + `SetupWithManager`.

**Reconcile algorithm (declarative):**
1. Resolve each peer to `{address, tlsConfig, ready, isLocal}`. Local = `ClusterRef != nil` → use `resolveTarget`; external = `FrontendAddress` + optional secret TLS (TLS resolution can be plaintext-nil in v1; load secret in a follow-up — see Step note).
2. For each **local, ready** peer: dial a `RemoteClusterClient`; `ListRemoteClusters`; for every *other* peer, `UpsertRemoteCluster(otherAddress, enableConnection)` when missing or when its enabled-state differs. (Disable rather than remove when `EnableConnection=false`.)
3. Compute `PeerConnectionStatus`: `Reachable` = local peer dialed OK (or external peer present in some local peer's remote list); `Connected` = peer appears as enabled remote on all *other* reachable local peers.
4. Set top-level `Ready` (`ReasonPeersConnected`) true when all peers connected; else false with a descriptive reason. Requeue `namespaceDriftRequeue`.
5. Finalizer `temporal.bmor10.com/clusterconnection`: on delete, best-effort `RemoveRemoteCluster(peerName)` for the other peers on each reachable local peer; unblock GC if unreachable.

- [ ] **Step 1: Write the failing test**

Create `internal/controller/temporalclusterconnection_controller_test.go`. Add a fake `RemoteClusterClient` recording upserts/removes keyed by the frontend address it was dialed with, and a factory closure keyed by address. Build two Ready `TemporalCluster`s (use `validClusterSpec`), set `clusterMetadata.currentClusterName` to `cluster-a`/`cluster-b`, create a connection with two local peers, reconcile twice, and assert each cluster got an upsert for the other's frontend address.

```go
var _ = Describe("TemporalClusterConnection reconciler", func() {
	ctx := context.Background()
	var fakes map[string]*fakeRemoteClient // key: dialed address

	factory := func(_ context.Context, address string, _ *tls.Config) (temporal.RemoteClusterClient, error) {
		f := fakes[address]
		if f == nil {
			f = &fakeRemoteClient{}
			fakes[address] = f
		}
		return f, nil
	}
	reconcile := func(name string) {
		r := &TemporalClusterConnectionReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ClientFactory: factory}
		_, err := r.Reconcile(ctx, ctrlreconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())
	}

	BeforeEach(func() { fakes = map[string]*fakeRemoteClient{} })

	It("registers each local peer as a remote on the other", func() {
		// create cluster-a, cluster-b (Ready) with currentClusterName set; create connection; reconcile.
		// Expect fakes[frontendAddr(cluster-a)].upserts contains frontendAddr(cluster-b) and vice versa.
	})
})
```

Fill in the cluster/connection creation following the `readyCluster` pattern from `temporalnamespace_controller_test.go` (set `Spec.ClusterMetadata.CurrentClusterName`). Define `fakeRemoteClient` with `upserts []string`, `removes []string`, implementing the interface; `ListRemoteClusters` returns its current view.

- [ ] **Step 2: Run to verify fail**

Run: `go test ./internal/controller/ -run TestControllers` (Ginkgo) — the spec compiles & fails because the reconciler type is undefined.
Expected: build failure / FAIL.

- [ ] **Step 3: Implement the controller**

Create `internal/controller/temporalclusterconnection_controller.go` (Apache header). Mirror the namespace controller structure: struct + `clientFactory()` fallback to `temporal.NewRemoteClusterClient`, finalizer handling, `setReady`/`statusUpdate` helpers, `SetupWithManager` with `For(&TemporalClusterConnection{}).Named("temporalclusterconnection")`. Include the RBAC markers:

```go
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterconnections,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterconnections/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterconnections/finalizers,verbs=update
```

Implement the algorithm above. For each peer build a resolved struct:

```go
type resolvedPeer struct {
	name    string
	address string
	tls     *tls.Config
	local   bool
	ready   bool
}
```

Local peers via `resolveTarget(ctx, r.Client, conn.Namespace, *p.ClusterRef)` (address, TLSConfig, Ready). External peers: `address = p.FrontendAddress`, `tls = nil` (TLS-from-secret deferred — leave a `// TODO(external mTLS): load p.TLSSecretRef` and treat as plaintext for now; document this in the spec's non-goals). Then the upsert loop and status as described. Use `enableConnection` defaulting to true when the pointer is nil.

- [ ] **Step 4: Register in main.go**

In `cmd/main.go`, before `// +kubebuilder:scaffold:builder` (with the other reconcilers):

```go
	if err := (&controller.TemporalClusterConnectionReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TemporalClusterConnection")
		os.Exit(1)
	}
```

- [ ] **Step 5: Run to verify pass**

Run: `make manifests` (so the new CRD exists for envtest), then `go test ./internal/controller/ -run TestControllers`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/controller/temporalclusterconnection_controller.go internal/controller/temporalclusterconnection_controller_test.go cmd/main.go config/ dist/
git commit -s -m "feat(controller): auto-register remote clusters for TemporalClusterConnection"
```

---

### Task 6: Namespace replication config + declarative failover

**Files:**
- Modify: `api/v1alpha1/temporalnamespace_types.go` (spec + status fields)
- Modify: `internal/temporal/client.go` (`NamespaceParams`, `NamespaceInfo`, `Register`, `Update`, `Describe`)
- Modify: `internal/controller/temporalnamespace_controller.go` (params, drift/failover, status)
- Modify: `internal/webhook/v1alpha1/temporalnamespace_webhook.go` (validate clusters/activeCluster)
- Modify: tests: `internal/controller/temporalnamespace_controller_test.go`, `internal/webhook/v1alpha1/temporalnamespace_webhook_test.go`

**Interfaces:**
- Produces: `TemporalNamespaceSpec.Clusters []string`, `TemporalNamespaceSpec.ActiveCluster string`; `NamespaceParams.Clusters []string`, `NamespaceParams.ActiveCluster string`; `NamespaceInfo.IsGlobal bool`, `NamespaceInfo.ActiveCluster string`, `NamespaceInfo.Clusters []string`; `TemporalNamespaceStatus.Replication *NamespaceReplicationStatus`.

- [ ] **Step 1: Add API fields**

In `api/v1alpha1/temporalnamespace_types.go`, after `IsGlobal`:

```go
	// Clusters lists the cluster names this namespace is replicated to. Only
	// meaningful when IsGlobal is true.
	// +optional
	Clusters []string `json:"clusters,omitempty"`

	// ActiveCluster is the authoritative cluster for this namespace. Changing it
	// triggers an operator-executed failover. Only meaningful when IsGlobal.
	// +optional
	ActiveCluster string `json:"activeCluster,omitempty"`
```

In `TemporalNamespaceStatus`, add:

```go
	// Replication reports the observed replication state of a global namespace.
	// +optional
	Replication *NamespaceReplicationStatus `json:"replication,omitempty"`
```

And the new status type (after the status struct):

```go
// NamespaceReplicationStatus reports the observed replication state.
type NamespaceReplicationStatus struct {
	// +optional
	IsGlobal bool `json:"isGlobal,omitempty"`
	// +optional
	ActiveCluster string `json:"activeCluster,omitempty"`
	// +optional
	Clusters []string `json:"clusters,omitempty"`
	// +optional
	FailoverInProgress bool `json:"failoverInProgress,omitempty"`
	// +optional
	LastFailoverTime *metav1.Time `json:"lastFailoverTime,omitempty"`
}
```

- [ ] **Step 2: Extend the Temporal client (write failing test first)**

In `internal/temporal/client_test.go` add a test asserting `Register` sends `IsGlobalNamespace`, `ActiveClusterName`, and `Clusters` when params set, and that `Describe` maps `ReplicationConfig` into `NamespaceInfo` (use the existing fake/bufconn harness in that file; if none, assert via a small proto-mapping helper test like Task 2).

Run: `go test ./internal/temporal/ -run TestNamespaceReplication`
Expected: FAIL.

- [ ] **Step 3: Implement client changes**

Add import `replicationpb "go.temporal.io/api/replication/v1"`. Extend structs:

```go
// in NamespaceParams:
	Clusters      []string
	ActiveCluster string

// in NamespaceInfo:
	IsGlobal      bool
	ActiveCluster string
	Clusters      []string
```

Helper:

```go
func clusterReplicationConfigs(clusters []string) []*replicationpb.ClusterReplicationConfig {
	out := make([]*replicationpb.ClusterReplicationConfig, 0, len(clusters))
	for _, c := range clusters {
		out = append(out, &replicationpb.ClusterReplicationConfig{ClusterName: c})
	}
	return out
}
```

`Register` — add fields:

```go
		Clusters:          clusterReplicationConfigs(params.Clusters),
		ActiveClusterName: params.ActiveCluster,
```

`Update` — set `ReplicationConfig` when global so failover/active-cluster changes apply:

```go
	req := &workflowservice.UpdateNamespaceRequest{
		Namespace: params.Name,
		UpdateInfo: &namespacepb.UpdateNamespaceInfo{
			Description: params.Description,
			OwnerEmail:  params.OwnerEmail,
		},
		Config: &namespacepb.NamespaceConfig{
			WorkflowExecutionRetentionTtl: durationpb.New(params.RetentionPeriod),
		},
	}
	if params.ActiveCluster != "" || len(params.Clusters) > 0 {
		req.ReplicationConfig = &replicationpb.NamespaceReplicationConfig{
			ActiveClusterName: params.ActiveCluster,
			Clusters:          clusterReplicationConfigs(params.Clusters),
		}
	}
	_, err := c.workflow.UpdateNamespace(ctx, req)
	return err
```

`Describe` — populate replication fields:

```go
	info.IsGlobal = resp.GetIsGlobalNamespace()
	if rc := resp.GetReplicationConfig(); rc != nil {
		info.ActiveCluster = rc.GetActiveClusterName()
		for _, cl := range rc.GetClusters() {
			info.Clusters = append(info.Clusters, cl.GetClusterName())
		}
	}
```

Run: `go test ./internal/temporal/ -run TestNamespaceReplication`
Expected: PASS.

- [ ] **Step 4: Controller — params, failover, status (write failing test first)**

In `internal/controller/temporalnamespace_controller_test.go` add a test: create a Ready cluster + a global namespace with `Clusters:[a,b]`, `ActiveCluster:a`; reconcile → assert `fake.registered` includes it and the register params carried global+active (extend the fake to record last register params). Then update `spec.ActiveCluster=b`, reconcile → assert the fake recorded an `Update` with `ActiveCluster=b` and status `Replication.ActiveCluster` eventually `b`.

Run (expect FAIL), then implement:

In `namespaceParams`, add:

```go
		Clusters:      ns.Spec.Clusters,
		ActiveCluster: ns.Spec.ActiveCluster,
```

In `namespaceDrifted`, treat an `ActiveCluster` mismatch (when `IsGlobal`) as drift so `Update` (failover) runs:

```go
	if params.ActiveCluster != "" && params.ActiveCluster != info.ActiveCluster {
		return true
	}
```

After `ensureRegistered`, populate `ns.Status.Replication` from the latest `info`, setting `FailoverInProgress` = `info.ActiveCluster != ns.Spec.ActiveCluster` and `LastFailoverTime`/Event when a failover update was issued. Emit an Event via a `record.EventRecorder` if you add one to the reconciler (optional; status-only is acceptable for v1 — keep it status + log to avoid adding the recorder wiring).

Run the controller suite: `go test ./internal/controller/ -run TestControllers`
Expected: PASS.

- [ ] **Step 5: Webhook validation (write failing test first)**

In `temporalnamespace_webhook_test.go` add cases: `clusters`/`activeCluster` set while `isGlobal=false` → reject; `activeCluster` not in `clusters` → reject; valid global namespace → accept.

Implement in `temporalnamespace_webhook.go` a helper used by both Create and Update:

```go
func validateReplication(ns *temporalv1alpha1.TemporalNamespace) error {
	if !ns.Spec.IsGlobal {
		if len(ns.Spec.Clusters) > 0 || ns.Spec.ActiveCluster != "" {
			return fmt.Errorf("spec.clusters and spec.activeCluster require spec.isGlobal=true")
		}
		return nil
	}
	if ns.Spec.ActiveCluster != "" && len(ns.Spec.Clusters) > 0 {
		found := false
		for _, c := range ns.Spec.Clusters {
			if c == ns.Spec.ActiveCluster {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("%s: spec.activeCluster %q must be one of spec.clusters", temporalv1alpha1.ReasonActiveClusterInvalid, ns.Spec.ActiveCluster)
		}
	}
	return nil
}
```

Call it in `ValidateCreate` and `ValidateUpdate` after the existing checks.

Run: `go test ./internal/webhook/v1alpha1/...`
Expected: PASS.

- [ ] **Step 6: Generate, build, commit**

Run: `make generate manifests && go build ./...`

```bash
git add api/ internal/ config/ dist/
git commit -s -m "feat(namespace): global namespace replication config and declarative failover"
```

---

### Task 7: Regenerate manifests, RBAC, Helm chart, API docs

**Files:**
- Generated: `config/crd/bases/temporal.bmor10.com_temporalclusterconnections.yaml`, `config/rbac/role.yaml`, `dist/chart/templates/crd/*`, `docs/api/v1alpha1.md`, `docs/content/reference/_index.md`.

- [ ] **Step 1: Regenerate everything**

Run:
```bash
make generate manifests
make helm-chart
```
Expected: new connection CRD appears under `config/crd/bases/` and `dist/chart/templates/crd/`; `config/rbac/role.yaml` gains the connection rules.

- [ ] **Step 2: Regenerate API reference docs**

Check `Makefile` for the docs target (e.g. `make api-docs` / `crd-ref-docs`); run it. If none exists, update `docs/api/v1alpha1.md` and `docs/content/reference/_index.md` by hand to include `TemporalClusterConnection`, `ClusterConnectionPeer`, `SecretReference`, and the new namespace fields.

- [ ] **Step 3: Verify the full suite + lint**

Run:
```bash
go build ./...
make test
make lint
```
Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add config/ dist/ docs/
git commit -s -m "chore: regenerate manifests, RBAC, chart, and API docs for connection CRD"
```

---

### Task 8: Examples + README

**Files:**
- Create: `examples/multi-cluster/cluster-connection.yaml`
- Create: `examples/multi-cluster/global-namespace.yaml` (or extend the existing one on this branch)
- Modify: `README.md` (CR table)

- [ ] **Step 1: Add example manifests**

`examples/multi-cluster/cluster-connection.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalClusterConnection
metadata:
  name: dr-pair
spec:
  peers:
    - name: clusterA
      clusterRef:
        name: cluster-a
    - name: clusterB
      clusterRef:
        name: cluster-b
```

`examples/multi-cluster/global-namespace.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalNamespace
metadata:
  name: orders
spec:
  clusterRef:
    name: cluster-a
  isGlobal: true
  clusters: [clusterA, clusterB]
  activeCluster: clusterA
```

- [ ] **Step 2: Update README CR table**

In `README.md`, add a row to the Custom Resources table:

```
| `TemporalClusterConnection` | `tcconn` | A multi-cluster replication group; auto-registers remote-cluster connections. |
```

- [ ] **Step 3: Commit**

```bash
git add examples/ README.md
git commit -s -m "docs: multi-cluster connection example and README entry"
```

---

### Task 9: e2e (chainsaw) — prove replication and failover work

**Files:**
- Create: `test/e2e/multicluster/chainsaw-test.yaml`
- Create: `test/e2e/multicluster/01-cluster-a.yaml`, `01-cluster-b.yaml`, asserts
- Create: `test/e2e/multicluster/02-connection.yaml`, `02-assert.yaml`
- Create: `test/e2e/multicluster/03-namespace.yaml`, `03-assert.yaml`

This suite must ASSERT enough to prove replication, not just reconciliation. Two `TemporalCluster`s in one kind cluster, postgres-backed, mTLS sharing a CA (reuse `test/e2e/mtls/` issuer + `test/e2e/postgres/` fixtures), `enableGlobalNamespace: true` with distinct `currentClusterName`/`initialFailoverVersion`.

- [ ] **Step 1: Cluster + connection steps**

`chainsaw-test.yaml` steps:
1. Provision postgres (two DBs) + a shared cert-manager issuer (reuse `../postgres/*` and `../mtls/00-issuer.yaml`).
2. Apply `01-cluster-a.yaml` + `01-cluster-b.yaml`; assert both reach `Ready` (assert files with `(conditions[?type=='Ready'].status | [0]): "True"`).
3. Apply `02-connection.yaml`; assert `TemporalClusterConnection` `Ready` true AND `status.peers` shows both `connected: true`.
4. **Connection proof script:** `kubectl run ... --image=temporalio/admin-tools:1.31.1 -- temporal operator cluster list --address temporal-cluster-a-frontend:7233` and assert (via chainsaw `check` / grep) that `clusterB` is listed. Repeat against cluster-b for `clusterA`. This proves the operator performed the upsert automatically.

- [ ] **Step 2: Replication-of-data proof**

5. Apply `03-namespace.yaml` (global, active clusterA). Assert it becomes `Registered`.
6. Script: against cluster-a, `temporal operator namespace describe --namespace orders` and assert `clusterA` active + global. Then, with bounded retry, run the same describe against **cluster-b** and assert the namespace exists there as global with active `clusterA` — proves namespace metadata replicated.
7. Script: start a workflow on cluster-a (`temporal workflow start --namespace orders --task-queue e2e --type E2E --workflow-id repl-1 ...` using a no-op or `--input`), then with bounded retry `temporal workflow describe --namespace orders --workflow-id repl-1 --address temporal-cluster-b-frontend:7233` against **cluster-b** and assert the execution is found — proves workflow history replicated.

- [ ] **Step 3: Failover proof**

8. Patch the namespace `spec.activeCluster: clusterB` (chainsaw `apply` of an updated `03-namespace.yaml`).
9. Bounded-retry script: `temporal operator namespace describe --namespace orders` on BOTH clusters and assert `Active Cluster: clusterB`. Proves the operator executed the failover.
10. Script: start a workflow against cluster-b (now active), then with bounded retry assert it is observable back on cluster-a — proves the new active cluster is authoritative and replication continues in the reverse direction.

Each script step uses `--rm -i --restart=Never` runner pods (as in `test/e2e/namespace/`) and a `catch:` block with `describe` + `events: {}` for diagnostics.

- [ ] **Step 4: Run locally**

Run:
```bash
make setup-test-e2e
make chainsaw-test            # or: chainsaw test --test-dir test/e2e/multicluster --config .chainsaw.yaml
```
Expected: the `multicluster` test passes, including the replication and failover assertions.

- [ ] **Step 5: Commit**

```bash
git add test/e2e/multicluster/
git commit -s -m "test(e2e): prove multi-cluster replication and failover end to end"
```

---

### Task 10: Final verification + PR

- [ ] **Step 1: Full gate**

Run:
```bash
make generate manifests
git diff --exit-code   # fail if generated artifacts are stale
make build
make test
make lint
```
Expected: all pass; no uncommitted generated changes.

- [ ] **Step 2: Open the PR**

```bash
git push -u origin feat/multi-cluster-replication-automation
gh pr create --base main --title "feat: automate multi-cluster replication (connection CRD + failover)" \
  --body "Builds on #92. Adds TemporalClusterConnection CRD + controller that auto-registers remote-cluster connections, and declarative namespace failover. e2e asserts data replication + failover effectiveness. See docs/superpowers/specs/2026-06-23-multi-cluster-replication-automation-design.md."
```

Return the PR URL.

---

## Self-Review notes

- **Spec coverage:** Task 2 → RemoteClusterClient; Tasks 3–5 → connection CRD/webhook/controller (remote-connection automation); Task 6 → namespace global config + failover; Task 9 → e2e proving replication+failover; Tasks 7–8 → generated artifacts/docs/examples. Connection-validates-consistency (`ConfigDrift`) is surfaced via `ReasonReplicationDrift` in Task 3/5.
- **External-peer mTLS** (loading `tlsSecretRef` into a `*tls.Config`) is intentionally deferred to a follow-up and marked TODO in Task 5; the spec's non-goals note connectivity is user-owned. Local-peer mTLS works via `resolveTarget`.
- **Type consistency:** `RemoteClusterClient` / `NamespaceParams` field names match across Tasks 2 and 6; controller field `ClientFactory temporal.RemoteClusterClientFactory` matches Task 2's factory type.
