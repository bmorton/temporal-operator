# Temporal Upgrade/Migration Proxy — Design

Status: Approved (brainstorm)
Date: 2026-06-18
Target: Alpha exploration (pre-1.0, additive — new CRD, no breaking changes)

## Goal

Make it easy to adopt the operator when a team already runs an existing
(external) Temporal cluster, by **gradually shifting traffic to a new,
operator-managed `TemporalCluster`** and safely retiring the old one.

We introduce a `TemporalMigration` custom resource. Its existence provisions a
**migration proxy** that sits in front of both clusters. Clients are repointed
to the proxy first (while it is a transparent passthrough to the old cluster),
then a single manual **cutover** gate sends *new* workflows to the new cluster
while *existing* workflows keep running on the old one until they drain. When
the old cluster has no running workflows, the migration reports **Complete**;
the operator surfaces drain progress so the cutover can be observed end to end.
Teardown is a manual step (repoint clients to the new cluster, delete the CR).

## Migration semantics (the hard constraint)

A Temporal workflow's entire history lives in **one** cluster's database. A
proxy can route *requests*, but it **cannot move *state*** — you cannot
transparently relocate an in-flight workflow by routing its traffic, because the
new cluster has no record of it.

This design therefore implements **drain-based cutover (semantic "A")**:

- New workflows start on the **new** cluster.
- Existing workflows keep running on the **old** cluster until they complete.
- The proxy routes each request to whichever cluster owns that workflow.
- No history is ever copied between clusters.

**Future (out of scope):** full state replication via Temporal multi-cluster /
XDC (semantic "B"), which actually copies namespace state and fails over. The
CRD and controller surface are designed so this can be added later without a
breaking change; only the proxy data plane and a new spec section would grow.

## Scope decisions (from brainstorming)

1. **Drain-based cutover (A) only.** Replication (B) is an explicit future.
2. **Ownership routing = try-new-then-fallback (stateless).** New-workflow calls
   always go to the new cluster. Operations on an existing workflow try the new
   cluster first; on `NotFound` they transparently retry the old cluster. No
   durable routing state; survives proxy restarts; self-corrects as the old
   cluster drains. Chosen over a stateful routing index (too much for an alpha)
   and over a visibility-lookup router (visibility is eventually consistent and
   would misroute freshly started workflows).
3. **Worker polling = run workers against both clusters (documented op step).**
   For the alpha the proxy does **not** fan out or alternate task-queue polls;
   users run their worker fleet against both the old and new clusters during
   migration. Alternate-polling (see below) is the documented next iteration and
   the proxy has a single seam reserved for it.
4. **Binary, user-gated cutover.** A single `spec.cutover` boolean is the manual
   gate. While `false`, the proxy is a 100% passthrough to the old cluster
   (clients can be repointed safely). Flipping it to `true` begins routing new
   starts to the new cluster. Weighted/canary start splitting is a future knob.
5. **Dedicated `TemporalMigration` CRD.** Its lifecycle *is* the migration:
   create to stand up the proxy, delete to tear it down. Chosen over a field on
   `TemporalCluster` (couples lifecycles, muddies the cluster spec) and over a
   bare Helm-deployed proxy (no reconciliation/status/managed teardown).
6. **Proxy data plane = transparent gRPC "director" (Approach 2).** Off-the-shelf
   `grpc-proxy`-style passthrough with a raw-bytes codec, so we do **not**
   re-implement the ~50 `WorkflowService` methods. A thin per-call director adds
   the only Temporal-specific knowledge (method classification + fallback). A
   full typed `WorkflowService` implementation (Approach 3) is the documented
   upgrade path for content-aware routing and the future replication work.
7. **Drain detection lives in the control plane.** The controller (not the proxy
   hot path) periodically and bounded-ly queries the old cluster's running
   workflow count via the SDK/visibility and reports progress in status.
8. **Optional routing-hint cache is a future opt-in.** A periodic, bounded
   visibility scan can publish a Bloom-filter hint set so the proxy picks the
   right upstream first and avoids a wasted round-trip. It is **never
   authoritative** (fallback still backstops correctness) and is **omitted from
   the alpha** to keep load off the source cluster by default.
9. **Manual teardown.** The operator cannot reassign the *external* old
   cluster's address, so completing the migration is a deliberate operator
   action: repoint clients to the new cluster's frontend, then delete the CR.

## Proxy implementation — approaches considered

**Approach 1 — Off-the-shelf L7 proxy (Envoy/nginx). Rejected (insufficient
alone).** Can route gRPC by method path and do weighted splits, but cannot do
**response-driven fallback** (retry on `NotFound`) or poll alternation, because
those decisions depend on the upstream's reply, not the request. Kept only as a
conceptual baseline.

**Approach 2 — Transparent gRPC "director" proxy. Chosen for the alpha.** A
small Go binary using the `grpc-proxy` pattern: a `StreamDirector` selects the
upstream per call and a **passthrough codec forwards raw bytes**, so no
`WorkflowService` methods are re-implemented. The only Temporal-specific logic
is a method-classification table plus `NotFound` fallback. Pros: minimal code,
version-agnostic (resilient to Temporal proto changes), fast to ship. Cons:
routing keys only off method name + response status, not request *contents* —
which is exactly all this design needs.

**Approach 3 — Full typed `WorkflowService` implementation. Documented future.**
Import `go.temporal.io/api/workflowservice/v1`, implement the whole
`WorkflowServiceServer`, each method explicitly forwarding to a chosen upstream.
Far more code and coupled to an API version, but enables **content-aware
control**: inspect/rewrite `WorkflowId`/`Namespace`, maintain a routing index,
rewrite responses. Needed for content-based routing, namespace content
remapping, and the future XDC/replication path. The `TemporalMigration` CRD and
controller are identical regardless of data plane, so Approach 2 → 3 is an
internal swap, not an API change.

## Architecture & topology

```
                         ┌─────────────────────────┐
  clients ──gRPC──▶      │   migration-proxy        │ ──▶ SOURCE frontend (external)
  (repointed to proxy)   │  (director, Approach 2)  │ ──▶ TARGET frontend (operator-managed)
                         └─────────────────────────┘
  workers ──▶ (alpha) point at BOTH source & target directly during migration
```

- A `TemporalMigration` CR references an **external source cluster** and an
  operator-managed **target `TemporalCluster`**.
- The operator provisions a migration-proxy **Deployment + Service** (new
  resource builders mirroring `internal/resources/deployment.go` and
  `service.go`), owned by the CR for garbage collection.
- Clients are pointed at the **proxy's frontend Service**
  (`status.proxyEndpoint`). The proxy holds pooled gRPC connections to both the
  source and target frontends.

## Lifecycle phases

Driven by the controller, surfaced in `status.phase` + conditions, gated by
`spec.cutover`:

1. **Pending** — proxy being provisioned; not yet routable.
2. **Passthrough** — proxy live, **100% → source**. Repoint clients now;
   behavior is identical (safe). Target receives no traffic.
3. **Cutover** — `spec.cutover: true`. New starts → target; existing-workflow
   ops try target, fall back to source on `NotFound`. Polls → source (workers
   already on both).
4. **Draining** — controller polls the **source** running-workflow count for the
   migrated namespaces and writes progress to status.
5. **Complete** — source running-count has been zero for a stable window. Proxy
   keeps running so late signals/queries to old workflows still resolve.
6. **Teardown (manual)** — repoint clients to the target frontend, then
   `kubectl delete` the CR; owner-ref GC removes the proxy Deployment/Service.

Every transition except enabling cutover (#3) and teardown (#6) is automatic and
observable; the operator holds exactly those two manual gates.

## CRD surface

Conventions follow existing CRDs (`LocalObjectReference` refs, mTLS via Secrets,
`status.phase` + `Conditions`, `+kubebuilder` markers, shortName).

```go
type TemporalMigrationSpec struct {
    // Source describes the EXTERNAL Temporal cluster being migrated away from.
    Source SourceClusterSpec `json:"source"`

    // TargetRef references the operator-managed TemporalCluster to migrate to.
    TargetRef corev1.LocalObjectReference `json:"targetRef"`

    // Namespaces to migrate. Empty = all namespaces present on the source.
    // +optional
    Namespaces []NamespaceMapping `json:"namespaces,omitempty"`

    // Cutover is the manual gate. false = Passthrough (100% -> source).
    // true  = route new starts to target, existing ops fall back to source.
    // +optional
    Cutover bool `json:"cutover,omitempty"`

    // Proxy tunes the provisioned proxy Deployment (replicas, resources, image).
    // +optional
    Proxy *ProxySpec `json:"proxy,omitempty"`
}

type SourceClusterSpec struct {
    // Address is the source frontend host:port (e.g. "old-temporal:7233").
    Address string `json:"address"`
    // TLS configures how the proxy connects to the source
    // (server CA + optional client mTLS).
    // +optional
    TLS *SourceTLSSpec `json:"tls,omitempty"`
}

type SourceTLSSpec struct {
    // Enabled turns on TLS to the source frontend.
    Enabled bool `json:"enabled,omitempty"`
    // SecretRef holds ca.crt (+ tls.crt/tls.key for mTLS client auth).
    // +optional
    SecretRef *corev1.LocalObjectReference `json:"secretRef,omitempty"`
    // ServerName overrides SNI / cert verification name.
    // +optional
    ServerName string `json:"serverName,omitempty"`
}

// NamespaceMapping allows source->target namespace renames;
// Target defaults to Source.
type NamespaceMapping struct {
    Source string `json:"source"`
    // +optional
    Target string `json:"target,omitempty"`
}

type TemporalMigrationStatus struct {
    // Phase: Pending|Passthrough|Cutover|Draining|Complete
    Phase string `json:"phase,omitempty"`
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
    Conditions []metav1.Condition `json:"conditions,omitempty"`
    // ProxyEndpoint is the Service address clients should target.
    ProxyEndpoint string `json:"proxyEndpoint,omitempty"`
    // Draining reports per-namespace source running-workflow counts.
    Draining []NamespaceDrainStatus `json:"draining,omitempty"`
    CutoverTime *metav1.Time `json:"cutoverTime,omitempty"`
}

type NamespaceDrainStatus struct {
    Namespace string `json:"namespace"`
    SourceRunningWorkflows int64 `json:"sourceRunningWorkflows"`
    Drained bool `json:"drained"`
}
```

**New condition types** (extending `api/v1alpha1/conditions.go`): `ProxyReady`,
`SourceReachable`, `TargetReachable`, `MigrationDraining`, `MigrationComplete`.

**Printer columns:** Phase, Target, Cutover, Proxy endpoint, Age. **shortName:**
`tm`.

Deliberate choices:
- `Namespaces` empty = migrate all (identity mapping); an explicit list enables
  renames and scoping.
- Hint-cache knobs are omitted from the alpha spec (documented future
  `Proxy.RoutingHints`).
- The proxy `Service` is a normal ClusterIP; external exposure (LB/Ingress) is
  the user's existing networking concern, same as the cluster frontend today.

## Proxy internals (`cmd/migration-proxy` + `internal/proxy/`)

A small standalone gRPC binary built into the operator image (new build target),
deployed by the controller. Built on the transparent director pattern:

- A `grpc.Server` with a **raw-bytes codec** and an `UnknownServiceHandler`, so
  all `WorkflowService` (and `OperatorService`) methods pass through untyped.
- A `director(ctx, fullMethod) -> upstream` holding two pooled client conns
  (source, target) and the current mode (read from mounted config / env, updated
  by the controller).
- **Method classification table** (the only Temporal-specific knowledge):
  - *Passthrough mode* → everything to **source**.
  - *Cutover mode*:
    - **Start** (`StartWorkflowExecution`, `SignalWithStartWorkflowExecution`)
      → **target**.
    - **Existing-workflow ops** (Signal/Query/Describe/Terminate/RequestCancel/
      Reset/Get*History, …) → **target first; on `codes.NotFound` /
      workflow-not-found, transparently retry source**.
    - **Polls** (`PollWorkflowTaskQueue`/`PollActivityTaskQueue`/Nexus) →
      **source** for the alpha (workers run against both directly). This is the
      single seam where alternate-polling drops in later.
    - **Everything else / cluster-wide reads** → target (with source fallback
      where the read is workflow-scoped).
- **mTLS:** source conn uses `spec.source.tls`; target conn uses the operator's
  existing cluster client-cert machinery (`internal/resources/clientcert.go`).
  The proxy's own server cert is provisioned via cert-manager, same pattern as
  the cluster frontend.
- **Fail-safe:** if a fallback also returns `NotFound`, return the target's
  original error (authoritative).
- **Long-poll handling:** the proxy must respect the client's context/deadline
  on poll methods and not impose its own short timeout (explicit test).

## Controller (`internal/controller/temporalmigration_controller.go`)

- Reconciles the CR → provisions the proxy `Deployment` + `Service` (owner refs
  for GC), writes the mode (passthrough/cutover) into the proxy's config, sets
  `status.proxyEndpoint`.
- Validates source & target reachability (gRPC health / `GetSystemInfo`) →
  `SourceReachable` / `TargetReachable` conditions.
- Phase machine: Pending → Passthrough → (on `spec.cutover=true`) Cutover →
  Draining → Complete, mirroring the existing upgrade controller's phase style
  (`internal/controller/temporalcluster_upgrade.go`).

### Drain detector (control plane, SDK-backed)

- While in Cutover/Draining, periodically call the **source**
  `CountWorkflowExecutions` with `ExecutionStatus="Running"` per migrated
  namespace; write counts into `status.draining[]`.
- **Rate-limited and bounded** so it never overloads the source; uses existing
  `internal/temporal` client plumbing.
- Mark a namespace `Drained=true` after a **stable-zero window** (configurable N
  consecutive zero reads, to absorb visibility lag); when all namespaces are
  drained → `MigrationComplete=true`, phase **Complete**. The proxy keeps
  serving so late queries to finished workflows still resolve.

### Optional routing-hint cache (future opt-in)

- A control-plane loop periodically does a **bounded** visibility scan of the
  source for **running** workflow IDs (rate-limited, paginated, hard cap) and
  publishes them into an in-memory **Bloom-filter** hint set the proxy consults
  to choose which upstream to try *first*.
- **Never authoritative:** fallback backstops every miss, so a stale/missing
  hint only costs an occasional extra round-trip — visibility lag is harmless.
- Bounded memory; degrades gracefully to pure try-new-then-fallback if the
  running set exceeds the cap. This is the natural seed for Approach 3's routing
  index later (promoted from "hint" to "authority").

## Teardown

Deleting the CR removes the proxy Deployment/Service/config via owner-ref GC.
Optional future: a `PreventDeletion`-style guard refusing teardown before
`Complete`, mirroring the cluster's existing safety webhook.

## Testing

- **Unit (pure, fast):** director method-classification + mode logic
  (passthrough vs cutover; start→target; existing-op fallback on `NotFound`)
  against fake upstream conns — no real Temporal. Proxy resource builders tested
  like existing `builders_test.go`. Drain-detector phase transitions
  (stable-zero window) with a fake source client.
- **Envtest (controller):** reconcile a `TemporalMigration` → proxy
  Deployment/Service created with owner refs, status/phase progression,
  conditions, teardown GC. Mirrors existing `*_controller_test.go` +
  `suite_test.go`, run via `make test`.
- **Integration / e2e (Chainsaw, follow-up):** two real Temporal clusters;
  verify passthrough→cutover→drain→complete and that a workflow started
  pre-cutover still signals/queries correctly through fallback. Fits the
  existing `test/e2e` + nsc harness.

## In/out of scope

**In (alpha):** `TemporalMigration` CRD + controller; provisioned director proxy
(Approach 2); passthrough/cutover gate; client-traffic routing with
try-new-then-fallback; SDK drain detection + status; mTLS to source; manual
teardown.

**Out (documented future):** alternate poll-proxying; routing-hint/Bloom cache;
weighted/canary starts; full typed `WorkflowService` impl (Approach 3); Temporal
XDC/replication (B); auto-teardown; any copying of workflow history.

## Risks & call-outs

- **Workers must run against both clusters during migration** — the primary
  operational caveat; documented prominently.
- **Visibility lag** → "Complete" uses a stable-zero window, not a single read.
  Long-running / cron / never-ending workflows keep the source alive
  indefinitely — that is correct behavior, not a bug.
- **Same workflow ID on both clusters** (rare): target wins; fallback is never
  reached. Documented limitation.
- **Long-poll passthrough:** the proxy must not impose its own short deadline on
  poll methods (respect client context) — explicit test.
- **`OperatorService` / Nexus / health** also pass through the same director;
  don't break health probes.

## Conventions

- Module `github.com/bmorton/temporal-operator`, API group
  `temporal.bmor10.com`, copyright owner "Brian Morton".
- After API type changes run `make generate manifests`; build with `make build`,
  test with `make test`, lint with `make lint`.
- Commits use Conventional Commits and are signed off (`git commit -s`).
