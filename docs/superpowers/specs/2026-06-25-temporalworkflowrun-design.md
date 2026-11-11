# TemporalWorkflowRun CRD — Design

**Status:** Approved (brainstorming)
**Date:** 2026-06-25

## Summary

Add a new namespaced CRD, `TemporalWorkflowRun`, that lets users trigger
**one-off Temporal workflow executions** declaratively from Kubernetes — the
workflow analog of a Kubernetes `Job`. Creating the CR starts exactly one
workflow; the operator tracks the execution in `status`; an optional TTL cleans
up the CR after the workflow reaches a terminal state.

The feature is **closed by default**: a `TemporalCluster` must explicitly opt in
via a `workflowRunPolicy` before the operator will start workflows against it,
and that policy can constrain which Temporal namespaces and task queues are
permitted. Kubernetes RBAC governs who may author runs; same-namespace
`clusterRef` resolution bounds the blast radius.

## Goals

- A `Job`-like, one-shot, immutable CRD for starting a single workflow.
- Keep `status` continuously updated with workflow execution details.
- Optional `ttlSecondsAfterFinished` that deletes the **CR** after the workflow
  finishes (success or failure).
- Configurable behavior for the in-flight workflow when the CR is deleted.
- A cluster-owner-controlled opt-in policy so this cannot open a hole by
  default.
- Reuse the existing mTLS target-resolution path unchanged.
- A Chainsaw e2e suite runnable locally on `nsc` and dispatchable/nightly in CI.

## Non-Goals

- Token-based (JWT/Entra) **outbound** authorization for operator-initiated
  gRPC calls. This affects the existing `TemporalSchedule`/`TemporalNamespace`
  controllers equally and is a separate, cross-cutting change. This PR reuses
  the existing mTLS path only.
- Capturing the workflow **success result** payload into status (lives in
  Temporal history; potentially large/sensitive). Only failure message/type is
  captured.
- Re-running / declarative "ensure running" semantics. The CR is one-shot and
  immutable; to run again, create a new CR.

## Design Decisions (from brainstorming)

1. **One-shot & immutable** (`Job`-like). The workflow definition is immutable
   after create; only cleanup knobs (`ttlSecondsAfterFinished`,
   `cancellationPolicy`) may change.
2. **TTL deletes the CR**, not the Temporal execution. Unset TTL = keep forever.
   The countdown starts at the observed terminal time (`status.completionTime`).
3. **On-delete behavior** is a `cancellationPolicy`: `Abandon` (default, leave
   running), `Cancel` (graceful), `Terminate` (hard), enacted via a finalizer.
4. **Status captures failure** (message + type) on non-success terminal states,
   but **not** the success result payload.
5. **mTLS only** for now; outbound JWT authz is out of scope (see Non-Goals).
6. **Closed-by-default cluster policy.** `TemporalCluster.spec.workflowRunPolicy`
   gates everything; `enabled` defaults `false`. `TemporalDevServer` carries the
   same policy but defaults `enabled: true` (throwaway dev environments).

## CRD: `TemporalWorkflowRun`

Naming follows the `Temporal*` convention shared by every existing CRD.
shortName: `twr`. Scope: Namespaced. Storage version: v1alpha1.

### Spec

```go
type TemporalWorkflowRunSpec struct {
    // ClusterRef references the TemporalCluster or TemporalDevServer that runs
    // the workflow. Resolved in the same Kubernetes namespace as this CR.
    ClusterRef ClusterReference `json:"clusterRef"`

    // Namespace is the Temporal namespace to start the workflow in.
    Namespace string `json:"namespace"`

    // Workflow describes the one-off workflow to start. Immutable after create.
    Workflow StartWorkflowAction `json:"workflow"`

    // TTLSecondsAfterFinished, when set, deletes this CR that many seconds
    // after the workflow reaches a terminal state. Unset = keep forever.
    // Mutable.
    // +optional
    TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`

    // CancellationPolicy controls what happens to a still-running workflow when
    // this CR is deleted. Mutable.
    // +kubebuilder:validation:Enum=Abandon;Cancel;Terminate
    // +kubebuilder:default=Abandon
    // +optional
    CancellationPolicy string `json:"cancellationPolicy,omitempty"`
}
```

`Workflow` **reuses the existing `StartWorkflowAction`** type (from
`temporalschedule_types.go`): `workflowType`, `taskQueue`, `workflowID`, `args`,
`workflowExecutionTimeout`, `workflowRunTimeout`, `workflowTaskTimeout`,
`workflowIDReusePolicy`, `retryPolicy`, `memo`, `searchAttributes`.
`workflow.workflowID` defaults to `metadata.name` when empty.

### Status

```go
type TemporalWorkflowRunStatus struct {
    // +optional
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
    // Phase is a friendly lifecycle summary.
    // +optional
    Phase string `json:"phase,omitempty"` // Pending|Running|Completed|Failed|Terminated|Canceled|TimedOut|ContinuedAsNew
    // +optional
    WorkflowID string `json:"workflowID,omitempty"`
    // +optional
    RunID string `json:"runID,omitempty"`
    // +optional
    WorkflowType string `json:"workflowType,omitempty"`
    // +optional
    TaskQueue string `json:"taskQueue,omitempty"`
    // +optional
    StartTime *metav1.Time `json:"startTime,omitempty"`
    // +optional
    CloseTime *metav1.Time `json:"closeTime,omitempty"`
    // CompletionTime is when the operator first observed a terminal state; it
    // drives the TTL countdown.
    // +optional
    CompletionTime *metav1.Time `json:"completionTime,omitempty"`
    // +optional
    HistoryLength int64 `json:"historyLength,omitempty"`
    // Failure carries the failure message/type for non-success terminal states.
    // +optional
    Failure *WorkflowRunFailure `json:"failure,omitempty"`
    // +listType=map
    // +listMapKey=type
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type WorkflowRunFailure struct {
    // +optional
    Message string `json:"message,omitempty"`
    // +optional
    Type string `json:"type,omitempty"`
}
```

Conditions: `Ready` (reconcile health / policy / start success) and `Finished`
(true once the workflow reaches a terminal state).

Print columns: Cluster (`.spec.clusterRef.name`), Namespace
(`.spec.namespace`), Phase (`.status.phase`), Ready
(`.status.conditions[?(@.type=="Ready")].status`), Age.

### Phase mapping

Temporal `WorkflowExecutionStatus` → `phase`:

| Temporal status               | phase           | terminal |
|-------------------------------|-----------------|----------|
| `UNSPECIFIED`                 | `Pending`       | no       |
| `RUNNING`                     | `Running`       | no       |
| `COMPLETED`                   | `Completed`     | yes      |
| `FAILED`                      | `Failed`        | yes      |
| `CANCELED`                    | `Canceled`      | yes      |
| `TERMINATED`                  | `Terminated`    | yes      |
| `CONTINUED_AS_NEW`            | `ContinuedAsNew`| yes      |
| `TIMED_OUT`                   | `TimedOut`      | yes      |

`failure` is populated for `Failed`/`Terminated`/`TimedOut`.

## Cluster-side opt-in policy

Add to both `TemporalClusterSpec` and `TemporalDevServerSpec`:

```go
type WorkflowRunPolicy struct {
    // Enabled permits operator-initiated workflow runs against this target.
    // TemporalCluster default: false. TemporalDevServer default: true.
    // +optional
    Enabled bool `json:"enabled,omitempty"`
    // AllowedNamespaces optionally restricts which Temporal namespaces runs may
    // target. Empty = any.
    // +optional
    AllowedNamespaces []string `json:"allowedNamespaces,omitempty"`
    // AllowedTaskQueues optionally restricts which task queues runs may use.
    // Empty = any.
    // +optional
    AllowedTaskQueues []string `json:"allowedTaskQueues,omitempty"`
}
```

Field is `spec.workflowRunPolicy *WorkflowRunPolicy` on both targets.

**Default semantics** are encoded in the resolver/controller, not via kubebuilder
defaults, so that absence is meaningful per-kind:

- `TemporalCluster`: a nil/absent policy means **disabled** (closed by default).
- `TemporalDevServer`: a nil/absent policy means **enabled with no allowlists**.

`resolveTarget` surfaces the effective policy on `ResolvedTarget`:

```go
type ResolvedTarget struct {
    Address           string
    TLSConfig         *tls.Config
    Ready             bool
    WorkflowRunPolicy WorkflowRunPolicy // effective, defaults already applied
}
```

The controller enforces the policy before starting: if disabled, the namespace
is not in a non-empty `AllowedNamespaces`, or the task queue is not in a
non-empty `AllowedTaskQueues`, it sets `Ready=False` with reason
`WorkflowRunNotPermitted` and a clear message, and **never starts the workflow**.

## Temporal client — `internal/temporal/workflowrun.go`

New interface, built with the raw `workflowservice` gRPC client, consistent with
the existing schedule/namespace clients. mTLS is handled entirely by the
injected `*tls.Config`.

```go
type WorkflowRunClient interface {
    // Start starts the workflow and returns its runID. Idempotent: uses
    // RequestId derived from the CR UID plus the configured workflow-id reuse
    // policy so a retried Start does not double-execute.
    Start(ctx context.Context, params StartWorkflowParams) (runID string, err error)
    // Describe returns the observed execution state. For non-success terminal
    // states it reads the close event from history to extract the failure
    // message/type.
    Describe(ctx context.Context, namespace, workflowID, runID string) (*WorkflowExecutionInfo, error)
    Cancel(ctx context.Context, namespace, workflowID, runID string) error
    Terminate(ctx context.Context, namespace, workflowID, runID, reason string) error
    Close() error
}

type WorkflowRunClientFactory func(ctx context.Context, address string, tlsConfig *tls.Config) (WorkflowRunClient, error)

type WorkflowExecutionInfo struct {
    Status        enumspb.WorkflowExecutionStatus
    RunID         string
    StartTime     *time.Time
    CloseTime     *time.Time
    HistoryLength int64
    Failure       *WorkflowFailure // message + type, only when applicable
}
```

`Start` reuses the existing `StartWorkflowParams` plus the json/plain payload
encoding helpers already in `schedule.go` (`encodeJSONPayloads`,
`encodeJSONFields`) — those should be reused (and, if convenient, hoisted to a
shared location) rather than duplicated.

## Controller — `internal/controller/temporalworkflowrun_controller.go`

Finalizer: `temporal.bmor10.com/workflowrun`.

Reconcile flow:

1. Get the CR. Resolve the target via `resolveTarget` (mTLS handled here).
   - Not found / unreachable while deleting → drop finalizer and forget
     (existing `removeFinalizerAndForget` pattern).
   - `ErrTargetNotFound` → `Ready=False` (`ClusterNotFound`), requeue.
2. Build the `WorkflowRunClient` via the injectable factory.
3. **Deletion** (`DeletionTimestamp != nil`): if the workflow is still running,
   apply `cancellationPolicy` (`Cancel`/`Terminate`; `Abandon` is a no-op), then
   remove the finalizer.
4. Ensure the finalizer is present.
5. Target not Ready → `Ready=False` (`ClusterNotReady`), short requeue.
6. **Enforce `WorkflowRunPolicy`.** Violation → `Ready=False`
   (`WorkflowRunNotPermitted`), no start, status update, return.
7. **Start if needed** (`status.runID == ""`): call `Start`; record
   `workflowID`, `runID`, `workflowType`, `taskQueue`, `startTime`, phase
   `Running`, `Ready=True`.
8. **Poll** if non-terminal: `Describe`; update status; requeue ~10s.
9. **Terminal**: set `phase`, `failure` (if any), `closeTime`, `historyLength`,
   `completionTime` (once), `Finished=True`. If `ttlSecondsAfterFinished` is set,
   delete the CR when `completionTime + TTL <= now`; otherwise requeue for the
   remaining duration.

RBAC markers: full verbs on `temporalworkflowruns` (+`/status`, `/finalizers`),
and `get;list;watch` on `temporalclusters`/`temporaldevservers` (already present
for satellite controllers).

Idempotency: deterministic workflow ID (`spec.workflow.workflowID` or CR name) +
`RequestId` from the CR UID means a duplicate `Start` is de-duplicated by
Temporal. On `AlreadyExists`, the controller resolves the existing runID via
`Describe`.

## Webhook — `internal/webhook/v1alpha1/temporalworkflowrun_webhook.go`

Validating webhook (`failurePolicy=fail`, `verbs=create;update`):

- **Create:** require `clusterRef.name`, `namespace`, `workflow.workflowType`,
  `workflow.taskQueue`; validate `args`/`memo`/`searchAttributes` are valid JSON
  (reuse `validateJSONList`/`validateJSONMap`); validate
  `workflow.workflowIDReusePolicy` enum.
- **Update:** reject changes to `clusterRef`, `namespace`, and the entire
  `workflow` block (immutable, one-shot). Allow `ttlSecondsAfterFinished` and
  `cancellationPolicy` to change. Re-run create-style validation on the
  (unchanged) workflow for safety.

## Wiring & generated artifacts

- `cmd/main.go`: register `TemporalWorkflowRunReconciler` and the webhook setup.
- `PROJECT`: add the new resource/webhook entries.
- `make generate manifests`: deepcopy + CRD manifest (including the new
  `workflowRunPolicy` fields on cluster/devserver CRDs).
- `make helm-chart`: regenerate `dist/chart` (new CRD + RBAC). Do not hand-edit
  `dist/chart`.
- `make api-docs docs-crd-reference`: regenerate and commit
  `docs/api/v1alpha1.md` and `docs/content/reference/_index.md` (docs CI drift
  check).
- `config/samples`: a `TemporalWorkflowRun` example (+ a cluster snippet showing
  `workflowRunPolicy`).

## Testing

**Unit / envtest:**

- Controller (fake client + fake `WorkflowRunClient`): start → status populated;
  poll to each terminal state; failure capture; TTL deletion timing; each
  `cancellationPolicy` on delete; policy-denied (disabled, namespace not
  allowed, task queue not allowed) starts nothing.
- Webhook: required-field validation, JSON validation, reuse-policy enum, and
  immutability of `clusterRef`/`namespace`/`workflow` on update.
- Client mapping: payload encoding, status→phase mapping, failure extraction
  from the close event.

**E2e — `test/e2e/workflowrun/` (Chainsaw):**

Runs against a `TemporalDevServer` (embedded SQLite — no external DB). Terminal
state is driven by terminating the workflow with the `admin-tools` image, so no
custom worker image is required.

Steps:

1. `dev-server` — apply a `TemporalDevServer` whose `workflowRunPolicy` sets an
   `allowedTaskQueues` allowlist; assert Ready.
2. `register-namespace` — apply a `TemporalNamespace` (`orders`); assert
   registered.
3. `happy-path` — apply a `TemporalWorkflowRun` using an allowed task queue and
   a short `ttlSecondsAfterFinished`. Assert `phase: Running`,
   `status.workflowID`/`runID` populated, `Ready=True`.
4. `terminal-and-ttl` — `kubectl run … admin-tools temporal workflow terminate`;
   assert operator observes `phase: Terminated` and `Finished=True`; then assert
   the CR is auto-deleted once the TTL elapses.
5. `policy-deny` — apply a `TemporalWorkflowRun` whose `taskQueue` is not in the
   allowlist; assert `Ready=False`, reason `WorkflowRunNotPermitted`, and no
   `workflowID` in status.

**CI wiring (`.github/workflows/e2e.yml`):** add
`workflowrun='{"suite":"workflowrun"}'` (devserver-style, no `persistence`);
include it in the nightly `schedule` combo list, the `workflow_dispatch` `case`
switch, and the `all` option (matching how `mtls`/`upgrade`/etc. are
dispatchable but excluded from the default PR run). Mirror the devserver image
pre-pull handling (dev-server + admin-tools images).

**Local:** `make chainsaw-test-nsc SUITE=workflowrun`.

## Delivery

Conventional Commits with DCO sign-off, on a feature branch, opened as a PR.
Run `make generate manifests build test lint helm-chart api-docs
docs-crd-reference` before pushing. The commitlint check is non-blocking but the
DCO and generated-artifact drift checks are enforced.
