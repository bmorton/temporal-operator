# Multi-cluster replication automation — design

Date: 2026-06-23
Status: Approved

## Context

PR #92 (branch `feat/multi-cluster-replication`) landed the **foundation** for
multi-cluster replication, scoped deliberately to config-only:

- `ClusterMetadataSpec` is now a typed struct (`api/v1alpha1/shared_types.go:205`)
  with `EnableGlobalNamespace`, `FailoverVersionIncrement`, `CurrentClusterName`,
  `InitialFailoverVersion`, `MasterClusterName`, rendered into
  `config_template.yaml` via `BuildConfigData`.
- Webhook validation + immutability for those fields.
- `TemporalNamespace.spec.isGlobal` wired to `RegisterNamespace`'s
  `IsGlobalNamespace` (`internal/temporal/client.go`,
  `internal/controller/temporalnamespace_controller.go`).

That PR's design **explicitly lists as out of scope**: "Automatic remote cluster
connection (CLI `upsert` step)" and "Failover orchestration". Operators must run
`temporal operator cluster upsert` by hand on every cluster, and there is no
operator-driven failover.

This design adds those missing automation layers **on top of** the #92
foundation, without re-shaping the foundation's API. The goal is full automation:
the operator establishes remote-cluster connections and executes namespace
failover declaratively.

## Goals

- Automate the remote-cluster connection step (`temporal operator cluster upsert`
  / `AddOrUpdateRemoteCluster`) so operators never run it by hand.
- Support both topologies: peers that are local `TemporalCluster` CRs and peers
  that are external clusters referenced by frontend address + optional mTLS
  secret.
- Declarative namespace failover: changing the active cluster on a
  `TemporalNamespace` makes the operator execute the failover via Temporal APIs.
- e2e coverage that **asserts replication actually works** (data replicated +
  failover effective), not merely that resources reconcile.
- Reuse the #92 foundation and existing operator conventions; do not rename or
  restructure the typed `ClusterMetadataSpec`.

## Non-goals

- Automatic, health-based failover. Failover is declarative now. API/status names
  leave room for an auto-failover layer later behind a feature gate.
- Operator-provisioned cross-cluster networking (Ingress/LoadBalancer/cert
  issuance). The user owns L3 connectivity; the operator validates reachability
  and reports status. (Cross-cluster mTLS still relies on shared CA +
  `mtls.frontend.dnsNames`, as documented by #92.)
- Graceful/handover failover beyond the standard active-cluster update.
- Re-opening the `ClusterMetadataSpec` field shape from #92.

## Alignment with the #92 foundation

- The replication-group cluster name is the existing
  `clusterMetadata.currentClusterName` on each `TemporalCluster`. The new
  connection CRD's peer `name` must match it. No new "clusterName" concept is
  introduced.
- `failoverVersionIncrement` / `initialFailoverVersion` continue to live on each
  `TemporalCluster.spec.clusterMetadata` (the foundation already validates and
  freezes them). The connection CRD references peers; it does **not** restate or
  own those values, avoiding a second source of truth. The connection controller
  surfaces a drift condition if connected peers report inconsistent increments,
  but the cluster CRs remain authoritative.
- Field/`int32` pointer conventions, optional markers, and webhook helper style
  match the foundation.

## New CRD: `TemporalClusterConnection` (short name `tcconn`)

Declares a replication group and drives remote-cluster connection registration.

```go
type TemporalClusterConnectionSpec struct {
    // Peers participating in replication (minimum 2). Each peer's Name must match
    // the peer cluster's clusterMetadata.currentClusterName.
    Peers []ClusterConnectionPeer `json:"peers"`
}

type ClusterConnectionPeer struct {
    // Name is the replication-group cluster name
    // (== that cluster's clusterMetadata.currentClusterName).
    Name string `json:"name"`

    // Exactly one of ClusterRef or FrontendAddress must be set.
    // ClusterRef points at a local TemporalCluster CR (address + cluster CA
    // resolved automatically, reusing internal/controller/target.go).
    // +optional
    ClusterRef *ClusterReference `json:"clusterRef,omitempty"`
    // FrontendAddress is an external peer's gRPC frontend (host:port).
    // +optional
    FrontendAddress string `json:"frontendAddress,omitempty"`
    // TLSSecretRef supplies mTLS material (CA + optional client cert/key) for an
    // external peer. Ignored for ClusterRef peers.
    // +optional
    TLSSecretRef *SecretReference `json:"tlsSecretRef,omitempty"`

    // EnableConnection toggles replication traffic without removing the peer.
    // +kubebuilder:default=true
    // +optional
    EnableConnection *bool `json:"enableConnection,omitempty"`
}
```

`SecretReference` is a small new shared type (`name` + optional `caKey` /
`certKey` / `keyKey` overrides), following the existing shared-types style.

### Status

```go
type TemporalClusterConnectionStatus struct {
    ObservedGeneration int64                     `json:"observedGeneration,omitempty"`
    Peers              []PeerConnectionStatus    `json:"peers,omitempty"`
    Conditions         []metav1.Condition        `json:"conditions,omitempty"`
}

type PeerConnectionStatus struct {
    Name             string `json:"name"`
    Reachable        bool   `json:"reachable"`
    Connected        bool   `json:"connected"`         // listed as a remote cluster by peers
    Message          string `json:"message,omitempty"`
}
```

Print columns: peers count, `Ready`. A top-level `Ready` condition plus a
`ConfigDrift` condition (for increment/global-namespace mismatch) reuse the
existing `conditions.go` constants where possible.

## Remote-cluster client

Add to `internal/temporal/client.go`, mirroring the existing
`NamespaceClientFactory` pattern (injectable for tests):

```go
type RemoteClusterClient interface {
    List(ctx context.Context) (map[string]RemoteClusterInfo, error)         // ListClusters
    Upsert(ctx context.Context, address string, enable bool) error          // AddOrUpdateRemoteCluster
    Remove(ctx context.Context, name string) error                          // RemoveRemoteCluster
    Close() error
}
type RemoteClusterClientFactory func(ctx context.Context, address string, tlsConfig *tls.Config) (RemoteClusterClient, error)
```

Backed by `OperatorService.AddOrUpdateRemoteCluster` / `RemoveRemoteCluster` /
`ListClusters` (the operator client already imports `operatorservice`).

## `TemporalClusterConnection` controller (new)

`internal/controller/temporalclusterconnection_controller.go`, following the
namespace controller's structure (finalizer, drift requeue, status conditions).

1. Watch `TemporalClusterConnection`; watch referenced `TemporalCluster`s and
   enqueue the owning connection on change (peer readiness/address changes).
2. Resolve each peer to a connectable frontend:
   - local `clusterRef` → `resolveTarget` (address + cluster CA);
   - external → `frontendAddress` + `tlsSecretRef`.
3. For each **reachable local** peer (a `clusterRef` peer whose cluster is
   Ready), reconcile its remote-cluster set: `List` → desired = the other peers'
   frontend addresses → `Upsert` for missing/changed, `Remove` for stale.
   Honor `EnableConnection`.
4. Set `ConfigDrift` if a connected peer reports a `failoverVersionIncrement`
   inconsistent with this group (best-effort; informational).
5. Status: per-peer reachable/connected + top-level `Ready`. Requeue on the
   existing drift cadence to catch connectivity drift.
6. Finalizer: best-effort `Remove` of the registrations it created; unblock GC if
   peers are unreachable (same pattern as `removeFinalizerAndForget`).

External-only peers (no local `clusterRef`) cannot be driven by this operator
instance; the controller still records them as desired remotes for the local
peers to connect to, and reports them as `reachable=false, connected` based on
`List` from a local peer. (Each side runs its own operator/connection CR for its
local cluster — the symmetric setup.)

## Namespace replication + failover

Extend `TemporalNamespaceSpec` (additive to the existing `IsGlobal`):

```go
// Clusters the namespace is replicated to (cluster names from the group).
// Only meaningful when IsGlobal is true.
// +optional
Clusters []string `json:"clusters,omitempty"`

// ActiveCluster is the authoritative cluster for the namespace. Changing it
// triggers an operator-executed failover. Only meaningful when IsGlobal.
// +optional
ActiveCluster string `json:"activeCluster,omitempty"`
```

Client/`NamespaceParams` gains `Clusters []string` and `ActiveCluster string`,
passed into `RegisterNamespace` (`Clusters`, `ActiveClusterName`) and, for
failover, `UpdateNamespace` (`ReplicationConfig.ActiveClusterName`).

Controller behavior (extends `ensureRegistered`):

- Register global namespaces with the clusters list + active cluster.
- When observed `activeClusterName` (from `Describe`) differs from
  `spec.activeCluster` → **failover**: call `UpdateNamespace` with the new active
  cluster, emit a Kubernetes Event, set status, requeue until `Describe` confirms.
- `NamespaceInfo` / `Describe` extended to read `ReplicationConfig`
  (active cluster, clusters, isGlobal) so drift + failover can be detected.

Status additions:

```go
Replication *NamespaceReplicationStatus `json:"replication,omitempty"`
// { IsGlobal bool; ActiveCluster string; Clusters []string;
//   FailoverInProgress bool; LastFailoverTime *metav1.Time }
```

Webhook (`temporalnamespace_webhook.go`, extends #92's): `clusters`/`activeCluster`
only valid when `isGlobal`; `activeCluster` must be a member of `clusters`.

## Safety / validation

- `TemporalClusterConnection` webhook: ≥2 peers; unique peer names; exactly one
  peer source (`clusterRef` xor `frontendAddress`); `tlsSecretRef` only with
  `frontendAddress`.
- Eventual-consistency aware: replication lag never an error; reported via
  conditions + requeue (matches namespace drift model).
- Idempotent describe-then-act; `AlreadyExists`/`NotFound` handled like the
  existing controllers.
- All new Temporal calls go through injectable factories so controllers are unit
  testable with fakes.

## Testing

### Unit
- `RemoteClusterClient` add/remove/list against a fake operator client.
- Connection controller: correct `Upsert`/`Remove` for add, change, remove,
  `enableConnection=false`, external peers; status + finalizer behavior.
- Namespace failover: `UpdateNamespace` called with new active cluster; status
  transitions; global-namespace registration with clusters list.
- Webhook validation for all new rules.

### envtest
- Connection + namespace reconcile loops against the API server with fakes;
  finalizers and conditions verified.

### e2e (chainsaw) — must prove replication actually works

Two `TemporalCluster`s in one kind cluster, connected by a
`TemporalClusterConnection`. The suite must assert end-to-end behavior, not just
`Ready`:

1. **Connection established** — assert each cluster lists the other as a remote
   cluster (`ListClusters`), connection enabled. This proves the operator
   performed the upsert automatically (the step #92 left manual).
2. **Global namespace replicated** — create a global `TemporalNamespace` active on
   cluster A; assert it becomes describable on cluster B as global, with the
   expected active cluster + clusters list (bounded retry for lag).
3. **Data actually replicates** — start a workflow (or write verifiable state) on
   active cluster A; assert the same execution becomes observable on standby
   cluster B within a bounded poll. This is the assertion that proves replication.
4. **Failover effective** — set `spec.activeCluster` to B; assert `DescribeNamespace`
   on both clusters reports B active, and a workflow started against B (the new
   active) becomes observable back on A. Proves failover changes the
   authoritative cluster, not just a field.

Bounded retries accommodate asynchronous replication, following the existing
`test/` chainsaw conventions.

## Docs / generated artifacts

- `make generate manifests`; regenerate `docs/api` reference.
- Extend `examples/multi-cluster/` with a `TemporalClusterConnection` and a
  global namespace using `clusters` + `activeCluster`.
- Update README CR table with `TemporalClusterConnection`.

## Rollout / compatibility

All additions are optional and additive to the #92 foundation; single-cluster and
config-only multi-cluster setups are unaffected. `v1alpha1`, pre-1.0 — the new CRD
and namespace fields ship under conventional-commit `feat` types so
release-please records them.
