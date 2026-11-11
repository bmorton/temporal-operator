# TemporalClusterProxy: automating s2s-proxy for cross-network replication

Status: Draft
Date: 2026-07-03
Related: `2026-06-23-multi-cluster-replication-automation-design.md` (the direct-network
`TemporalClusterConnection` CRD this builds alongside).

## Summary

Add a new `TemporalClusterProxy` CRD that automates deployment and wiring of
[temporalio/s2s-proxy](https://github.com/temporalio/s2s-proxy) so operator-managed
Temporal clusters can replicate across **segregated networks** without exposing their
frontends to each other. The headline use case is cross-network disaster recovery over
s2s-proxy **mux mode** with mTLS; the CRD is designed as a general foundation that also
carries s2s-proxy's namespace / search-attribute translation, failover-version
translation, and ACL allowlists.

## Motivation

Today `TemporalClusterConnection` automates multi-cluster replication by having a single
operator dial **both** peers' Temporal frontends directly and call
`AddOrUpdateRemoteCluster`. That assumes the peers share a routable network and each
frontend is directly reachable. It cannot serve clusters that must not share a network
(different VPCs / accounts / on-prem ↔ cloud), where only one side can open a port.

s2s-proxy solves exactly this: in **mux mode** one side opens a port (mux-server) and the
other dials out (mux-client), multiplexing bidirectional replication over a single
TCP-TLS connection, so the dialing side never exposes a port. It also adds in-flight
namespace / search-attribute translation, per-API / per-namespace ACLs, failover-version
translation, and cross-version protobuf adaptation. Automating it removes a large amount
of fiddly, error-prone manual config (proxy YAML, cert wiring, remote-cluster
registration) and brings it under the operator's reconcile/status model.

## Non-goals

- Replacing `TemporalClusterConnection`. It remains the direct-network option;
  `TemporalClusterProxy` is the segregated-network option.
- TCP (non-mux) proxy mode and pure same-network namespace migration. The API leaves room
  for it, but the first deliverable targets mux DR.
- Auto-discovering the peer's exposed mux address across clusters. In the both-operator
  case the user copies the server's `status.proxyEndpoint` into the client CR.

## Deployment / topology model

The CR describes **one local cluster + its proxy + one peer link**. Both replication
directions ride the single mux, so there is one `TemporalClusterProxy` per cluster per
peer. For a symmetric, both-operator-managed DR pair the user authors two CRs — one
`mux.role: server` (in the cluster that exposes a port) and one `mux.role: client` (in the
cluster that dials out), one in each cluster's operator. The two operators do not talk to
each other; they converge independently from the matching CRs.

The peer may be:
- **operator-managed** (`peer.clusterRef` set) — used to reuse the peer's issuer CA
  automatically, or
- **external** (no `clusterRef`) — the remote proxy/cluster is run by someone else; its CA
  is supplied via `mux.tls.peerCARef`.

### Data flow (A = mux-client, B = mux-server)

1. Operator registers remote cluster **B** on **A's** Temporal via
   `AddOrUpdateRemoteCluster`, with `frontendAddress` = **A's own proxy** `tcpServer`
   Service (cluster-internal). This reuses the existing `internal/temporal`
   `RemoteClusterClient`; the only change from `TemporalClusterConnection` is that the
   address points at the local proxy instead of the remote frontend.
2. A's Temporal streams replication → A-proxy `tcpServer` → mux → B-proxy → B's Temporal
   frontend (`tcpClient`).
3. The reverse direction (B → A) rides the same mux; B's CR registers **A** on B's
   Temporal via B's local proxy.

## API design

New namespaced CRD, group `temporal.bmor10.com/v1alpha1`, shortName `tcproxy`, marked
`storageversion`, with a `status` subresource.

### Spec

```yaml
spec:
  localClusterRef:            # required — the local operator-managed TemporalCluster this proxy fronts
    name: cluster-a
    kind: TemporalCluster     # reuses existing ClusterReference (defaults to TemporalCluster)
  # localClusterName: optional override; else derived from the cluster's
  #                   clusterMetadata.currentClusterName

  peer:                       # the remote replication cluster on the far side of the mux
    name: cluster-b           # == that cluster's currentClusterName; must != local cluster name
    clusterRef:               # optional: set when the remote is also operator-managed (CA reuse only)
      name: cluster-b
      kind: TemporalCluster
    enableConnection: true    # default true; toggle replication without deleting the CR

  mux:
    role: server | client     # required — one side opens a port, the other dials out
    server:                   # required when role=server
      listenPort: 6334
      exposure: {...}         # reuses existing ServiceExposureSpec (LoadBalancer/NodePort/...)
    client:                   # required when role=client
      serverAddress: b.example.com:6334
    muxCount: 4               # optional; defaults to upstream default
    tls:
      provider: cert-manager | secret   # default cert-manager (mirrors MTLSSpec)
      issuerRef: {...}                   # cert-manager path — mints this side's mux cert
      secretRef: {...}                   # BYO path (cert/key/CA); 1Password OnePasswordItem-friendly
      peerCARef: {...}                   # remote CA to trust; auto-reused from peer.clusterRef issuer when available

  # --- general-foundation extras, all optional ---
  translation:
    namespaces:
      - { local: ns, remote: ns.accountid }
    searchAttributes:
      - namespace: ns
        mappings:
          - { localFieldName: x, remoteFieldName: y }
  failoverVersionIncrement: { local: 100, remote: 1000 }
  acl:
    allowedNamespaces: [ns]
    allowedAdminMethods: [...]   # defaults to the known replication allowlist

  image: temporalio/s2s-proxy:<pinned>   # pinned default; overridable (native render, per decision)
```

### Status

```yaml
status:
  observedGeneration: N
  proxyEndpoint: "1.2.3.4:6334"    # server role: the address to hand to the peer's client CR
  conditions:
    - type: Ready
    - type: ProxyDeployed           # proxy Deployment available
    - type: MTLSReady               # cert issued / BYO secret present & valid
    - type: RemoteClusterRegistered # local Temporal registered the peer via the local proxy
```

## Rendered resources

All owned by the CR via `ownerRef` (GC'd on delete), built by **pure**
`internal/resources` `Build*` functions (no client/IO, so they stay `GOOS=js
GOARCH=wasm`-compatible, matching the existing convention).

| Resource | Purpose |
|---|---|
| `ConfigMap` | Rendered s2s-proxy `clusterConnections` YAML (pure render func, mirroring `internal/temporal/configtemplate.go`) |
| `Certificate` (cert-manager) | Only when `tls.provider: cert-manager`; mints this side's mux cert → Secret |
| `Deployment` | `temporalio/s2s-proxy` pod; mounts config ConfigMap + TLS Secret; `--config` arg |
| `Service` | Two ports: **tcpServer** (ClusterIP, for local Temporal to reach) + **mux** (server role only, exposed per `mux.server.exposure`) |

### Config rendering

Maps the spec to one `clusterConnections` entry:

- `local.connectionType: tcp`; `local.tcpClient.address` = resolved local Temporal frontend
  Service; `local.tcpServer.address` = `0.0.0.0:<port>`.
- `remote.connectionType: mux-server | mux-client`; `muxAddressInfo` = listen address
  (server) or `serverAddress` (client), with TLS `certificatePath` / `keyPath` /
  `remoteCAPath` pointing at the mounted Secret.
- Optional `namespaceTranslation`, `searchAttributeTranslation`,
  `failoverVersionIncrementTranslation`, and `aclPolicy` rendered from the spec. `aclPolicy`
  defaults to the known replication admin-method allowlist
  (`AddOrUpdateRemoteCluster`, `DescribeCluster`, `DescribeMutableState`,
  `GetNamespaceReplicationMessages`, `GetWorkflowExecutionRawHistoryV2`, `ListClusters`,
  `StreamWorkflowReplicationMessages`).

## Reconciliation

One CR = one local cluster's proxy. The loop:

1. Resolve `localClusterRef` → frontend address + issuer CA (reuse `resolveTarget`).
2. Ensure TLS: create the `Certificate` (cert-manager) or validate the BYO Secret keys →
   set `MTLSReady`.
3. Render + apply ConfigMap, Deployment, Service via the existing server-side-apply
   `r.apply` pattern → set `ProxyDeployed` when the Deployment is available.
4. When the proxy is ready **and** the local Temporal is ready: `AddOrUpdateRemoteCluster`
   pointing at the local proxy `tcpServer` Service → set `RemoteClusterRegistered`.
5. Publish `status.proxyEndpoint` (server role), set `Ready`, requeue on drift
   (`namespaceDriftRequeue`).
6. **Finalizer:** on delete, best-effort `RemoveRemoteCluster` from the local Temporal,
   let ownerRef GC the proxy resources, then remove the finalizer — mirroring
   `TemporalClusterConnection` so deletion always converges even if Temporal is
   unreachable.

The `RemoteClusterClientFactory` is injectable for tests, as in the existing connection
controller.

## Webhook validation

Typed `admission.Validator[*TemporalClusterProxy]` (+ `Defaulter`), per the
controller-runtime v0.23 generic admission convention already used in this repo.

- `mux.role: server` requires `mux.server.listenPort` (and exposure); `role: client`
  requires `mux.client.serverAddress`. Reject wrong-role fields being set.
- `tls.provider: cert-manager` requires `issuerRef`; `provider: secret` requires
  `secretRef`.
- `peer.name` required and `!=` local cluster name; `localClusterRef` required.
- Immutable after creation: `mux.role`, `localClusterRef`.
- Defaulter fills: `tls.provider` (cert-manager), `peer.enableConnection` (true), `image`.

## Error handling

Per-condition, eventual-consistency; a single failing dependency never wedges the whole
reconcile.

- Local cluster missing / not ready → `RemoteClusterRegistered=False`, `Ready=False`
  (`ReasonClusterNotReady`), requeue; registration untouched.
- cert-manager Certificate not yet issued / BYO Secret missing keys → `MTLSReady=False`.
- Deployment not yet available → `ProxyDeployed=False`.
- Registration RPC fails (proxy up, Temporal transiently unreachable) →
  `RemoteClusterRegistered=False`, requeue.
- Deletion always converges (best-effort de-register → GC → remove finalizer).

## Testing

- **Unit (pure):** config-render func across server / client / translation / ACL /
  failover permutations (table tests, like `configtemplate_test.go`); the `Build*`
  resource builders; keep the `wasm` build green.
- **Webhook tests:** all validation rules (envtest suite).
- **Controller envtest:** CR → owned ConfigMap / Deployment / Service / Certificate created
  with ownerRefs; conditions progress; registration invoked via an injected fake
  `RemoteClusterClientFactory`; finalizer de-registers on delete.
- **e2e (chainsaw, final milestone):** two kind clusters, one server + one client CR,
  mTLS mux, replicate a namespace end-to-end.

## Delivery / repo conventions

- `make generate manifests` (deepcopy + CRD). RBAC markers for the new controller +
  `deployments` / `services` / `configmaps` / `secrets` and cert-manager `certificates`.
- `make api-docs docs-crd-reference`; commit `docs/api/v1alpha1.md` +
  `docs/content/reference/_index.md` (docs CI drift check).
- `make helm-chart`; commit `dist/chart` (verify-chart CI). Mirror any manager RBAC/args
  into `hack/helm/overrides/` if the manager Deployment changes.
- `make lint test build` green. Conventional-commit `feat(...)`, DCO sign-off.
- Add `examples/cluster-proxy-mux/` with a server + client CR pair.

## Milestones

Each is independently plan-able / reviewable:

1. **API** — types + deepcopy + CRD manifests + docs + chart regen.
2. **Rendering** — config-render func + `Build*` resource builders + unit tests.
3. **Controller** — reconcile (TLS → deploy → register) + finalizer + envtest.
4. **Webhook** — validation + defaulting + tests.
5. **e2e** — chainsaw two-cluster mux replication + example manifests.

## Open questions / risks

- s2s-proxy is "binary only; internal APIs subject to change." Mitigation: pin the image
  and treat it like the other pinned tool/image versions in the version matrix.
- Confirm the exact upstream config keys (`muxCount`, TLS `caServerName` /
  `skipCAVerification`, health-check blocks) against the pinned s2s-proxy release when
  implementing the render func.
- Cross-cluster CA distribution in the both-operator external case is manual (peer CA
  secret) — acceptable for the first cut.
