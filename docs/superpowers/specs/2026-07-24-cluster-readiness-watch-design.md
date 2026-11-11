# Reconcile dependent CRs on cluster readiness (watch + backoff fix)

## Problem

When a `TemporalCluster` and a dependent object (e.g. `TemporalNamespace`) are
created at the same time, the dependent object registers much later than the
cluster becomes ready. Two mechanics cause this:

1. **No event-driven trigger.** The cluster-dependent controllers
   (`TemporalNamespace`, `TemporalSchedule`, `TemporalSearchAttribute`,
   `TemporalWorkflowRun`, `TemporalClusterConnection`) resolve their target
   cluster via `resolveTarget`, and when the cluster is not `Ready` they simply
   requeue on a timer. None of them **watch** the `TemporalCluster` /
   `TemporalDevServer`, so nothing re-triggers a reconcile the instant the
   cluster flips to `Ready` — the dependent waits for its next timed requeue.

2. **Exponential backoff on transient not-ready.** Once the cluster reports
   `Ready`, the controllers immediately call the frontend (e.g.
   `ensureRegistered` → `Describe`/`Register`). If the frontend is up but not
   yet serving RPCs, the call returns a raw error and the controllers do
   `return ctrl.Result{}, err`. Controller-runtime then applies **exponential
   backoff** (default max ~16.7 min), so the object is "caught in a backoff" and
   registers long after the cluster is actually usable.

This also slows the e2e (chainsaw) suites, which currently sequence
cluster-first-then-dependent to sidestep the delay.

## Goals

- Dependent CRs reconcile promptly (event-driven) when their referenced cluster
  becomes `Ready`.
- Transient "frontend up but not serving yet" conditions never enter
  exponential backoff.
- Consistent behavior across all cluster-dependent controllers.
- No API/CRD/Helm changes (no regen required).

## Non-goals

- No changes to how the `TemporalCluster` computes its own `Ready` condition.
- No changes to deletion/finalizer semantics.
- No broad refactor of the controllers beyond what serves this fix.

## Approach

Two shared mechanisms, applied to the five cluster-dependent controllers.

### 1. Event-driven readiness (primary fix)

Each dependent controller's `SetupWithManager` adds:

```go
.Watches(&temporalv1alpha1.TemporalCluster{},
    handler.EnqueueRequestsFromMapFunc(mapClusterToX(r.Client)),
    builder.WithPredicates(clusterReadinessChanged))
.Watches(&temporalv1alpha1.TemporalDevServer{},
    handler.EnqueueRequestsFromMapFunc(mapDevServerToX(r.Client)),
    builder.WithPredicates(clusterReadinessChanged))
```

- **Map function:** given a changed cluster/devserver, list the dependent CRs in
  the **same namespace** whose `ClusterRef` points at it, and enqueue reconcile
  requests for them. For `TemporalClusterConnection`, match when *any* peer's
  `ClusterRef` targets the changed cluster. An empty `ClusterRef.Kind` defaults
  to `TemporalCluster` and must be honored by the matcher.
- **Predicate (`clusterReadinessChanged`):** only enqueue on meaningful changes
  — a generation change OR a transition of the `Ready` condition. `CreateFunc`
  returns `true` so an already-`Ready` cluster still triggers dependents created
  afterward. This avoids fan-out churn on every routine cluster status write.

Result: the moment a cluster flips to `Ready`, its dependents reconcile without
waiting on a timer.

### 2. No exponential backoff on transient not-ready (Approach A)

A shared helper classifies transient connectivity errors:

```go
// isTransientClusterErr reports whether err is a transient frontend-not-serving
// condition (cluster reachable but not yet accepting RPCs) that should be
// retried on a short fixed interval rather than triggering exponential backoff.
func isTransientClusterErr(err error) bool {
    switch status.Code(err) {
    case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
        return true
    default:
        return false
    }
}
```

The four RPC-registering controllers (`TemporalNamespace`, `TemporalSchedule`,
`TemporalSearchAttribute`, `TemporalWorkflowRun`) wrap their frontend-call error
sites: if `isTransientClusterErr`, set a `Ready=False` /
`FrontendUnavailable` condition and return
`ctrl.Result{RequeueAfter: clusterUnavailableRequeue}` (short, ~5s) with a nil
error, instead of `return ctrl.Result{}, err`. Terminal errors (invalid TLS
material, invalid params, permission denied, etc.) still return as real errors
so they remain visible and retried appropriately.

`TemporalClusterConnection` already swallows transient per-peer failures and
never returns them as errors, so it receives only the watch change (mechanism 1).

The existing `ClusterNotReady` 15s requeue path is retained as a safety net but
becomes largely moot once the watch fires immediately.

## Components

New shared file `internal/controller/cluster_watch.go`:

- `clusterReadinessChanged predicate.Predicate` — generation-change or
  `Ready`-condition-transition filter (compares old/new conditions in
  `UpdateFunc`; `CreateFunc` → true; `DeleteFunc` → false).
- Per-controller map-func builders (or a small generic helper) that list the CR
  type and filter by `ClusterRef` name/kind within the cluster's namespace.
- `isTransientClusterErr(err) bool` — wraps `google.golang.org/grpc/status` +
  `codes`.
- `clusterUnavailableRequeue = 5 * time.Second`.

Per-controller edits:

- `SetupWithManager`: add the two `.Watches(...)` calls with map func +
  predicate.
- namespace/schedule/searchattribute/workflowrun: at frontend-call error sites,
  branch on `isTransientClusterErr` to requeue-short instead of erroring.

## Testing

- **Unit:** `clusterReadinessChanged` (transition vs no-op vs generation bump);
  map functions (matches only referencing CRs, same-namespace only, honors empty
  `Kind`); `isTransientClusterErr` classification (Unavailable → true, InvalidArgument → false).
- **Controller (envtest):** a dependent CR created before its cluster is `Ready`
  reconciles promptly once the cluster's `Ready` condition flips (watch fires).
  A transient RPC error yields a short requeue rather than a returned error.
- **e2e (chainsaw):** update `test/e2e/namespace` to apply the cluster and
  namespace together in a single step (instead of cluster-first sequencing) and
  assert the namespace registers, proving the fix and trimming wall-clock time.
  Keep sibling-suite changes minimal.

## Edge cases

- Both `TemporalCluster` and `TemporalDevServer` target kinds are watched.
- Cross-namespace safety: the map func only enqueues CRs in the cluster's own
  namespace, matching `resolveTarget` semantics.
- `ClusterRef.Kind == ""` defaults to `TemporalCluster`.
- Deletion/finalizer paths are unchanged.

## Rollout / verification

- No API type changes → no CRD/Helm regeneration expected.
- Run `make lint` and targeted `make test` before opening the PR.
