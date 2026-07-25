# Fail loudly, recover automatically

**Date:** 2026-07-25
**Track:** A1 (trustworthiness)

## Problem

An audit of the operator found four failure modes that are silent, unbounded, or
both. In each case the operator stops making progress and reports nothing an
operator could alert on.

1. **Stalled upgrades hang forever.** `advanceRollingPhase`
   (`internal/controller/temporalcluster_upgrade.go:175-185`) returns without
   advancing when `serviceRolledOut` is false. There is no deadline and no
   failure phase, so a crashlooping or unpullable service leaves the cluster
   split across two Temporal versions indefinitely, with no condition set.

2. **A permanently failed schema Job wedges the cluster.**
   `reconcileJobSchema` (`internal/controller/temporalcluster_persistence.go:243-275`)
   treats `jobFailed` as terminal. The Job's `BackoffLimit: 3`
   (`internal/resources/schemajob.go:58`) retries the *pod* within seconds,
   which does not cover the common cause — the Job was created while the
   database was still starting. Recovery requires a human to delete the Job.

3. **Cleanup is abandoned silently when a target is merely unreachable.**
   The namespace, schedule, search-attribute and connection controllers call
   `removeFinalizerAndForget` for *any* resolve or dial error during deletion
   (e.g. `internal/controller/temporalnamespace_controller.go:68-85`). A
   frontend restarting for 30 seconds takes the same branch as a cluster that
   no longer exists, orphaning the Temporal-side object without a trace.

4. **Status conflicts are dropped.** `statusUpdate`
   (`internal/controller/temporalclusterclient_controller.go:127-133`) returns
   `nil` on `409 Conflict`, losing Ready transitions and `ObservedGeneration`
   under concurrent writes.

Supporting these, three structural gaps make the above hard to see or fix:

- **No domain metrics.** `internal/` imports no Prometheus client at all. You
  cannot ask "is any cluster stuck upgrading?"
- **Events in 2 of 8 controllers.** Only `TemporalCluster` and its upgrade
  path emit any.
- **Duplicated status plumbing.** Seven controllers each carry a near-identical
  `setReady`/`statusUpdate` pair, which is how bug 4 came to exist in one copy
  and not the others.

Two related defects found alongside:

- `UpgradeStatus.Rollbackable` is documented as "true until schema migration
  begins" (`api/v1alpha1/temporalcluster_types.go:243`) but is set `false` at
  `upgradePreflight` (`temporalcluster_upgrade.go:154`), before migration. The
  code contradicts its own contract.
- `validateUpgradePath` (`internal/webhook/v1alpha1/temporalcluster_webhook.go:304`)
  validates `oldCluster.Spec.Version -> newCluster.Spec.Version`. Mid-upgrade,
  `spec.version` is already the target, so a second change is validated from the
  wrong baseline — never from where the cluster's older services actually are.

## Principle

Every abnormal state is:

1. named by a condition,
2. either auto-recovered on a **bounded** schedule or terminated with evidence,
3. observable without reading operator logs.

No stall is silent. No recovery is unbounded.

## Non-goals

Deliberately excluded to keep this bounded:

- `MaxConcurrentReconciles` and cache scoping — track A2.
- OpenTelemetry tracing. These reconciles are short; the interesting failures
  are stalls measured in minutes, which alerts show well and traces show badly.
- `apply.go`'s `ForceOwnership`. Flagged by the audit, but changing
  server-side-apply ownership semantics is high-risk and off-theme.
- Grafana dashboards. Alerts answer "is something broken", which is this
  project's job. Dashboards answer "why", which is not.
- Any new `spec` API surface. Status fields only.

## Architecture

Four shared packages under `internal/`. Each of the four bugs recurs across
multiple controllers, so fixing them in place would mean seven copies that
drift — which is precisely how the existing `setReady` duplication arose.

### `internal/status`

Replaces the seven duplicated `setReady`/`statusUpdate` pairs.

- `SetCondition(obj, type, status, reason, message)` — always stamps
  `ObservedGeneration` on both the condition and, where present, the status.
- `Update(ctx, client, obj)` — wraps `Status().Update` in
  `retry.RetryOnConflict`, re-fetching and re-applying the condition set. This
  fixes the swallowed-409 defect once for all controllers.

Constrained by a small interface (`GetConditions`, `SetConditions`,
`GetGeneration`) that all eight CRDs satisfy, implemented in a new
`api/v1alpha1` accessor file.

### `internal/metrics`

A `prometheus.Collector` that, on scrape, walks the manager cache for all eight
kinds and emits:

```text
temporal_operator_resource_condition{kind,namespace,name,type,status,reason}
```

Registered into controller-runtime's existing `metrics.Registry`, so it rides
the existing authenticated metrics endpoint and ServiceMonitor — no new server,
port, or RBAC beyond the reads the controllers already have.

Collect-on-scrape rather than gauges written during reconcile is deliberate:
per-reconcile gauges leak stale series for deleted objects and require every
controller to remember cleanup. Deriving from conditions also means every
condition added by this project becomes queryable with no extra plumbing, and
future conditions cannot drift out of sync with their metrics.

controller-runtime already exports `controller_runtime_reconcile_total`,
`_errors_total` and `_time_seconds` per controller. Those are not duplicated.
Four domain metrics cover what conditions cannot express:

| Metric | Type | Labels |
|---|---|---|
| `temporal_operator_upgrade_phase_seconds` | gauge | `namespace,name,phase` |
| `temporal_operator_schema_job_attempts` | gauge | `namespace,name,store` |
| `temporal_operator_cleanup_abandoned_total` | counter | `kind,namespace` |
| `temporal_operator_target_unreachable_total` | counter | `kind,namespace` |

### `internal/recovery`

The bounded-retry primitive shared by schema-Job recreation and cleanup
deadlines. A pure function:

```text
(attempts, firstFailureAt, now, policy) -> RetryAfter(d) | GiveUp
```

State lives in the resource's status, never in memory, so recovery survives
operator restarts and leader-election failover. Pure and time-injected,
therefore unit-testable without envtest.

### `internal/events`

A thin recorder wrapper giving all eight controllers a consistent reason
vocabulary drawn from the existing constants in `api/v1alpha1/conditions.go`,
replacing the free-form strings used by the two controllers that emit events
today.

## Behavior changes

### 1. Upgrade stall detection

`UpgradeStatus` gains `PhaseStartedAt`, `StalledService`, `Message`.

`advanceRollingPhase` compares elapsed time in the current phase against
`upgradePhaseTimeout` (package variable, default 15m). Past the deadline:

- set `UpgradeBlocked=True` and `Degraded=True`, with the stalled service and
  its Deployment's own unready reason in the message,
- emit a warning Event (once per entry into the blocked state, not per
  reconcile — the recorder wrapper deduplicates on unchanged reason plus
  message),
- stop advancing, and return a `RequeueAfter` of `upgradeStallRecheck`
  (default 1m).

The explicit requeue matters: the cluster reconciler watches owned Deployments,
so a recovering pod normally re-triggers reconciliation on its own, but a stall
whose cause is external (an unreachable registry, a pending PVC) may produce no
watched-object event at all. Without the requeue the blocked state would itself
be a stall.

The condition is recomputed on every reconcile, so it clears the moment the
service finishes rolling out. It is a report, not a latch.

There is no automatic rollback. Temporal schema migrations are forward-only,
and by the time any rolling phase can stall the schema has already moved.
Reverting binaries under a migrated schema is a decision that requires
information the operator does not have.

`Rollbackable` moves from the `upgradePreflight` branch to
`upgradeSchemaMigrating`, restoring the documented contract.

### 2. Mid-upgrade version guard

New `validateVersionChangeDuringUpgrade` in the TemporalCluster validating
webhook. While `status.upgrade != nil`:

- reject changes to `spec.version`,
- except a change to `status.upgrade.fromVersion`, which cancels the upgrade
  and is always allowed — this is the documented exit from a stall,
- when that revert happens and `Rollbackable == false`, return an
  `admission.Warning` stating that the schema has already migrated and older
  binaries will run against the newer schema.

`validateUpgradePath` is corrected to validate from `status.upgrade.fromVersion`
when an upgrade is in flight.

The webhook already returns `admission.Warnings` and already composes
`validate*` helpers, so this follows the established shape.

### 3. Schema Job recovery

`PersistenceStatus` gains per-store `SchemaAttempts` (`count`, `firstFailedAt`,
`lastError`).

On `jobFailed`, `reconcileJobSchema` consults `internal/recovery`:

- **within budget** — delete the Job, emit an Event, requeue after the backoff
  delay (1m, 5m, 15m; 3 attempts),
- **budget exhausted** — set `SchemaReady=False` with a terminal reason and
  `Degraded=True`, message carrying the failed pod's terminated-container
  reason, and stop.

Attempts reset to zero on success.

Re-running is safe: the Jobs run `setup-schema -v 0.0` and `update-schema -d`
with no `--overwrite` (`internal/resources/schemajob.go:151,153`).
`update-schema` applies only migrations above the current version, and without
`--overwrite` a retry cannot destroy an existing schema.

### 4. Cleanup deadline

The currently-merged deletion branches split in the namespace, schedule,
search-attribute and connection controllers:

- **`ErrTargetNotFound`** — unchanged. Immediate `removeFinalizerAndForget`.
  Issue #58's guarantee (deletion always terminates) is preserved exactly, and
  its regression tests must keep passing untouched. You cannot deregister an
  object from a cluster that no longer exists.
- **Any other resolve or dial error** — increment
  `temporal_operator_target_unreachable_total`, then requeue with backoff
  against `cleanupDeadline` (package variable, default 5m), measured from
  `DeletionTimestamp`. On expiry: warning Event `CleanupAbandoned` naming the
  orphaned Temporal object, increment
  `temporal_operator_cleanup_abandoned_total`, then forget.

`temporal_operator_target_unreachable_total` is incremented on the same error
class outside deletion too — any controller that fails to resolve or dial a
target it believes exists — which makes "the frontend is flapping" visible
before it causes an abandonment.

`TemporalClusterConnection`'s best-effort `RemoveRemoteCluster`
(`internal/controller/temporalclusterconnection_controller.go:373-381`), which
currently logs and ignores every error, joins the same path.

Deletion still always terminates. Abandonment becomes timed, recorded, and
alertable rather than invisible.

### 5. gRPC deadlines

The Temporal client factory used by `resolveTarget`
(`internal/controller/target.go`) wraps dial and RPCs in `context.WithTimeout`
(package variable, default 30s). Today a half-open connection can block a
reconcile indefinitely; with controller-runtime's default
`MaxConcurrentReconciles: 1` that stalls the controller entirely. This belongs
here rather than in A2 for that reason.

### 6. Events and conditions everywhere

The six controllers without a recorder get one. `Degraded` and `Progressing` —
declared in `conditions.go` and currently referenced nowhere — become real:
`Progressing` while work is in flight, `Degraded` on any exhausted
bounded-retry. New reason constants join the existing block.

### 7. Alerts

A `PrometheusRule` in `config/prometheus`, mirrored into
`hack/helm/overrides/` (never `dist/chart`, which is generated):

| Alert | Fires on |
|---|---|
| `TemporalClusterUpgradeStalled` | `UpgradeBlocked=True` sustained |
| `TemporalSchemaMigrationFailed` | `SchemaReady=False` with terminal reason |
| `TemporalResourceDegraded` | `Degraded=True` on any kind |
| `TemporalCleanupAbandoned` | `cleanup_abandoned_total` increase |

Each alert is keyed to a condition this project introduces.

## Testing

Matched to what each layer can prove:

- **Pure unit** — `internal/recovery` with injected time; `internal/status`
  conflict retry against a fake client; the upgrade phase machine's stall
  arithmetic. This is where the decision logic — and the bugs — actually live.
- **envtest**, one failure-path test per behavior change. This is precisely the
  code the current 48.1% coverage misses:
  - schema Job fails, is recreated after backoff, exhausts, sets `Degraded`;
  - upgrade rolling phase stalls, sets `UpgradeBlocked`, then clears when the
    pod recovers;
  - deletion with an unreachable-but-present target retries, hits the deadline,
    emits `CleanupAbandoned`;
  - deletion with an absent target forgets immediately — the #58 regression
    guard.
- **Webhook** — reject a third version mid-upgrade; allow revert to
  `fromVersion`; warn when `Rollbackable == false`; validate the path from
  `fromVersion`.
- **Metrics** — `testutil.CollectAndCompare`, asserting series appear and,
  critically, disappear when a resource is deleted.
- **e2e (chainsaw)** — one new `upgrade-stall` suite: deploy a cluster, trigger
  an upgrade pinned to an unpullable image, assert `UpgradeBlocked`, repair
  `spec.version`, assert recovery. No coverage exists for this today and it is
  the scenario most likely to bite a real operator.
- **CI** — add `-race` to `make test` (absent today, `Makefile:66`).

**Risk:** envtest is already the slowest gate and bounded-retry tests are
time-sensitive. Every timeout above is therefore a package-level variable, not
a constant, so tests inject short values instead of sleeping through a
15-minute deadline. If that proves awkward for the schema-Job path, the
fallback is to test `internal/recovery` purely and assert only the first
transition in envtest.

## Sequencing

Four increments, each independently shippable, each leaving the tree green.

1. **Foundations** — `internal/status`, `internal/recovery`, `internal/events`;
   migrate the seven controllers onto the shared helpers. Pure refactor plus the
   409 fix; no behavior change, so it lands low-risk and shrinks everything
   after it.
2. **Recovery semantics** — upgrade stall, schema Job recreation, cleanup
   deadline, gRPC deadlines, webhook guard.
3. **Visibility** — condition exporter, domain metrics, Events across all
   controllers, `PrometheusRule`.
4. **Verification** — e2e stall suite, `-race`, remaining coverage gaps.

## Repository obligations

Per `AGENTS.md`:

- Status field additions require `make generate manifests`.
- The `PrometheusRule` and any RBAC change require `make helm-chart`, with edits
  made in `hack/helm/overrides/` and never in the generated `dist/chart`.
- Commits follow Conventional Commits and are signed off (`git commit -s`).
- `make build`, `make test`, `make lint` are the gates.

## Context: where this sits

This is the first of six planned projects, ordered trustworthiness-first then
adoption:

- **A1 — this project.**
- **A2** — scale and concurrency: `MaxConcurrentReconciles`, cache scoping,
  watch indexers.
- **B1** — API stability: `v1beta1`, conversion webhooks, deprecation policy.
- **B2** — docs and diagrams: architecture diagrams, ADRs, day-2 runbooks.
- **B3** — backup, restore, disaster recovery.
- **B4** — exposure and scaling: frontend Ingress/Gateway API, HPA,
  configurable PDB.
