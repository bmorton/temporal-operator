# TemporalSchedule CRD — Design

Status: Approved (brainstorm)
Date: 2026-06-17
Target: Land before 1.0 (pre-1.0, additive — no breaking changes to existing CRDs)

## Goal

Add a `TemporalSchedule` custom resource that lets users declaratively manage
[Temporal Schedules](https://docs.temporal.io/schedule) (the successor to cron
workflows) through Kubernetes. A schedule periodically starts a workflow
according to a calendar/interval specification, with overlap and catchup
policies. This mirrors the existing data-plane CRDs (`TemporalNamespace`,
`TemporalSearchAttribute`) that reconcile against a running Temporal cluster.

## Scope decisions (from brainstorming)

1. **Purely declarative core.** The CRD manages the schedule spec, action,
   policies, and pause/unpause state. Imperative one-shot operations
   (trigger-immediately, backfill) are **out of scope** for this version but the
   design must not preclude adding them later via annotations.
2. **Workflow args as JSON list.** `action.startWorkflow.args` is a list of
   arbitrary JSON values, each encoded into a Temporal `Payload` with
   `encoding: json/plain`. No raw-payload escape hatch yet (additive later).
3. **Drift detection = generation-driven push + last-applied spec hash (A+C).**
   We push `UpdateSchedule` when the spec changes (detected via a hash stored in
   status, robust across operator restarts), recreate the schedule if it has
   been deleted, and reconcile `paused` state. We do **not** attempt full
   external structured-drift reconciliation (Temporal compiles cron/calendar
   into `structuredCalendar` on describe, so byte round-trips don't match).
4. **Full `ScheduleSpec` surface** (cron strings, intervals, structured
   calendars, exclusions, start/end bounds, jitter, timezone name). The niche
   raw `timezoneData` TZif bytes field is omitted (`timezoneName` covers it).
5. **Action / policy / state surface** — fully featured minus SDK-internal or
   rarely-used fields (`header`, `userMetadata`, `versioningOverride`,
   `priority`, and the invalid-inside-a-schedule `cronSchedule`).

## Background: relevant Temporal API

The project depends only on `go.temporal.io/api` (raw gRPC protos), **not** the
Temporal Go SDK. Schedule RPCs live on `workflowservice`:
`CreateSchedule`, `DescribeSchedule`, `UpdateSchedule`, `DeleteSchedule`,
`PatchSchedule`, `ListScheduleMatchingTimes`.

Key proto messages (`go.temporal.io/api/schedule/v1`):

- `Schedule { Spec, Action, Policies, State }`
- `ScheduleSpec { StructuredCalendar[], CronString[], Calendar[], Interval[],
  ExcludeStructuredCalendar[], StartTime, EndTime, Jitter, TimezoneName,
  TimezoneData }`
- `StructuredCalendarSpec { Second/Minute/Hour/DayOfMonth/Month/Year/DayOfWeek:
  Range[], Comment }`, `Range { Start, End, Step }`
- `IntervalSpec { Interval, Phase }`
- `SchedulePolicies { OverlapPolicy, CatchupWindow, PauseOnFailure,
  KeepOriginalWorkflowId }`
- `ScheduleAction { StartWorkflow: workflow.NewWorkflowExecutionInfo }`
- `ScheduleState { Notes, Paused, LimitedActions, RemainingActions }`
- `SchedulePatch { Pause/Unpause (string notes), TriggerImmediately, BackfillRequest[] }`

`workflow.NewWorkflowExecutionInfo` carries the started workflow's
`WorkflowId, WorkflowType, TaskQueue, Input (Payloads), Workflow*Timeout,
WorkflowIdReusePolicy, RetryPolicy, Memo, SearchAttributes` (plus the omitted
fields above).

Important nuance: on `Describe`, Temporal returns `cron_string`/`calendar`
compiled into `structuredCalendar` (+ interval/timezone). This is why drift
detection uses a spec hash rather than comparing the live compiled spec.

## API type — `api/v1alpha1/temporalschedule_types.go`

Namespaced CRD. Markers mirror `TemporalNamespace`:
`+kubebuilder:resource:scope=Namespaced,shortName=tsch`,
`+kubebuilder:subresource:status`, `+kubebuilder:storageversion`, printcolumns
for Cluster / Namespace / Paused / Ready / Age.

```go
type TemporalScheduleSpec struct {
    // ClusterRef references the TemporalCluster that hosts this schedule.
    ClusterRef corev1.LocalObjectReference `json:"clusterRef"`

    // Namespace is the Temporal namespace the schedule lives in.
    Namespace string `json:"namespace"`

    // ScheduleID is the Temporal schedule ID. Defaults to metadata.name.
    // Immutable once set.
    // +optional
    ScheduleID string `json:"scheduleID,omitempty"`

    // AllowDeletion permits the operator to delete the Temporal schedule when
    // the CR is deleted. When false, the schedule is left in place.
    // +optional
    AllowDeletion bool `json:"allowDeletion,omitempty"`

    // Schedule describes when the action fires.
    Schedule ScheduleSpec `json:"schedule"`

    // Action describes what to do when the schedule fires.
    Action ScheduleActionSpec `json:"action"`

    // Policies tunes overlap/catchup/pause-on-failure behavior.
    // +optional
    Policies *SchedulePoliciesSpec `json:"policies,omitempty"`

    // State controls pause and action-limit state.
    // +optional
    State *ScheduleStateSpec `json:"state,omitempty"`
}

type ScheduleSpec struct {
    // Calendars holds cron strings (5/6/7 field, or @daily etc; may include
    // CRON_TZ=/TZ= prefix).
    // +optional
    Calendars []string `json:"calendars,omitempty"`

    // Intervals fire every Every (plus optional Offset/phase).
    // +optional
    Intervals []IntervalSpec `json:"intervals,omitempty"`

    // StructuredCalendar gives field-level control without cron syntax.
    // +optional
    StructuredCalendar []StructuredCalendarSpec `json:"structuredCalendar,omitempty"`

    // ExcludeStructuredCalendar subtracts matching times.
    // +optional
    ExcludeStructuredCalendar []StructuredCalendarSpec `json:"excludeStructuredCalendar,omitempty"`

    // StartTime/EndTime bound the schedule (inclusive).
    // +optional
    StartTime *metav1.Time `json:"startTime,omitempty"`
    // +optional
    EndTime *metav1.Time `json:"endTime,omitempty"`

    // Jitter randomizes each action time by 0..Jitter.
    // +optional
    Jitter *metav1.Duration `json:"jitter,omitempty"`

    // TimezoneName interprets calendar specs (IANA name; defaults to UTC).
    // +optional
    TimezoneName string `json:"timezoneName,omitempty"`
}

type IntervalSpec struct {
    Every  metav1.Duration  `json:"every"`
    // +optional
    Offset *metav1.Duration `json:"offset,omitempty"`
}

type StructuredCalendarSpec struct {
    // +optional
    Second []CalendarRange `json:"second,omitempty"`
    // +optional
    Minute []CalendarRange `json:"minute,omitempty"`
    // +optional
    Hour []CalendarRange `json:"hour,omitempty"`
    // +optional
    DayOfMonth []CalendarRange `json:"dayOfMonth,omitempty"`
    // +optional
    Month []CalendarRange `json:"month,omitempty"`
    // +optional
    Year []CalendarRange `json:"year,omitempty"`
    // +optional
    DayOfWeek []CalendarRange `json:"dayOfWeek,omitempty"`
    // +optional
    Comment string `json:"comment,omitempty"`
}

type CalendarRange struct {
    Start int32 `json:"start"`
    // +optional
    End int32 `json:"end,omitempty"`
    // +kubebuilder:default=1
    // +optional
    Step int32 `json:"step,omitempty"`
}

type ScheduleActionSpec struct {
    StartWorkflow StartWorkflowAction `json:"startWorkflow"`
}

type StartWorkflowAction struct {
    WorkflowType string `json:"workflowType"`
    TaskQueue    string `json:"taskQueue"`
    // +optional
    WorkflowID string `json:"workflowID,omitempty"`

    // Args are JSON-serializable workflow inputs, one payload each
    // (encoding json/plain).
    // +optional
    Args []runtime.RawExtension `json:"args,omitempty"`

    // +optional
    WorkflowExecutionTimeout *metav1.Duration `json:"workflowExecutionTimeout,omitempty"`
    // +optional
    WorkflowRunTimeout *metav1.Duration `json:"workflowRunTimeout,omitempty"`
    // +optional
    WorkflowTaskTimeout *metav1.Duration `json:"workflowTaskTimeout,omitempty"`

    // +kubebuilder:validation:Enum=AllowDuplicate;AllowDuplicateFailedOnly;RejectDuplicate;TerminateIfRunning
    // +optional
    WorkflowIDReusePolicy string `json:"workflowIDReusePolicy,omitempty"`

    // +optional
    RetryPolicy *RetryPolicySpec `json:"retryPolicy,omitempty"`

    // Memo / SearchAttributes are JSON-valued maps encoded to payloads.
    // +optional
    Memo map[string]runtime.RawExtension `json:"memo,omitempty"`
    // +optional
    SearchAttributes map[string]runtime.RawExtension `json:"searchAttributes,omitempty"`
}

type RetryPolicySpec struct {
    // +optional
    InitialInterval *metav1.Duration `json:"initialInterval,omitempty"`
    // +optional
    BackoffCoefficient string `json:"backoffCoefficient,omitempty"` // parsed float, avoids float in CRD
    // +optional
    MaximumInterval *metav1.Duration `json:"maximumInterval,omitempty"`
    // +optional
    MaximumAttempts int32 `json:"maximumAttempts,omitempty"`
    // +optional
    NonRetryableErrorTypes []string `json:"nonRetryableErrorTypes,omitempty"`
}

type SchedulePoliciesSpec struct {
    // +kubebuilder:validation:Enum=Skip;BufferOne;BufferAll;CancelOther;TerminateOther;AllowAll
    // +optional
    OverlapPolicy string `json:"overlapPolicy,omitempty"`
    // +optional
    CatchupWindow *metav1.Duration `json:"catchupWindow,omitempty"`
    // +optional
    PauseOnFailure bool `json:"pauseOnFailure,omitempty"`
    // +optional
    KeepOriginalWorkflowID bool `json:"keepOriginalWorkflowID,omitempty"`
}

type ScheduleStateSpec struct {
    // +optional
    Paused bool `json:"paused,omitempty"`
    // +optional
    Notes string `json:"notes,omitempty"`
    // +optional
    LimitedActions bool `json:"limitedActions,omitempty"`
    // +optional
    RemainingActions int64 `json:"remainingActions,omitempty"`
}
```

Notes:
- `BackoffCoefficient` is a string (parsed to float64) to avoid floats in the
  CRD schema, consistent with avoiding fragile float defaults. Webhook validates
  it parses.
- `RemainingActions` is treated as a desired-initial value; Temporal decrements
  it server-side. Because drift is hash-based on the spec (not compared to the
  live value), this does not cause an update loop. Documented as such.

### Status

```go
type TemporalScheduleStatus struct {
    // +optional
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
    // +optional
    ScheduleID string `json:"scheduleID,omitempty"`
    // +optional
    Created bool `json:"created,omitempty"`
    // +optional
    Paused bool `json:"paused,omitempty"`
    // +optional
    Notes string `json:"notes,omitempty"`
    // LastAppliedSpecHash detects spec changes across operator restarts.
    // +optional
    LastAppliedSpecHash string `json:"lastAppliedSpecHash,omitempty"`
    // NextActionTimes is a small window of upcoming action times (observability).
    // +optional
    NextActionTimes []metav1.Time `json:"nextActionTimes,omitempty"`
    // +optional
    RunningWorkflows int `json:"runningWorkflows,omitempty"`
    // +optional
    LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
    // +listType=map
    // +listMapKey=type
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}
```

## Client wrapper — `internal/temporal/schedule.go`

New `ScheduleClient` interface + gRPC implementation, paralleling
`NamespaceClient`. The controller works only with operator-domain structs
(`ScheduleParams`), never raw protos, keeping the proto mapping isolated and
unit-testable.

```go
var ErrScheduleNotFound = errors.New("schedule not found")

type ScheduleParams struct {
    ScheduleID string
    Namespace  string
    Spec       ScheduleSpecParams
    Action     StartWorkflowParams
    Policies   SchedulePolicyParams
    State      ScheduleStateParams
}

type ScheduleInfo struct {
    Paused           bool
    Notes            string
    NextActionTimes  []time.Time
    RunningWorkflows int
}

type ScheduleClient interface {
    Describe(ctx, namespace, scheduleID string) (*ScheduleInfo, error) // ErrScheduleNotFound
    Create(ctx, params ScheduleParams) error
    Update(ctx, params ScheduleParams) error
    Pause(ctx, namespace, scheduleID, notes string) error   // PatchSchedule
    Unpause(ctx, namespace, scheduleID, notes string) error // PatchSchedule
    Delete(ctx, namespace, scheduleID string) error
    Close() error
}

type ScheduleClientFactory func(ctx, address string, tlsConfig *tls.Config) (ScheduleClient, error)
```

Mapping helpers (pure, table-tested):
- `toProtoSchedule(ScheduleParams) (*schedule.Schedule, error)` — builds Spec
  (cron→`CronString`, intervals→`IntervalSpec`, structured→`StructuredCalendarSpec`/`Range`,
  start/end→`timestamppb`, jitter/timezone), Action
  (`NewWorkflowExecutionInfo` with encoded `Input`/`Memo`/`SearchAttributes`,
  retry policy, timeouts, reuse-policy enum), Policies (overlap enum map,
  catchup, flags), State.
- `encodeJSONPayloads([]runtime.RawExtension) (*common.Payloads, error)` and
  `encodeJSONPayload(runtime.RawExtension)` — each payload metadata
  `{"encoding": []byte("json/plain")}`, data = the `.Raw` JSON bytes.
- `overlapPolicyFromString` / enum maps for overlap + workflow-id-reuse.

The frontend dial reuses the existing connection pattern; `NewScheduleClient`
constructs a `grpc.NewClient` like `NewNamespaceClient` (insecure or TLS).

## Controller — `internal/controller/temporalschedule_controller.go`

Structurally identical to `TemporalNamespaceReconciler`:

1. `Get` the CR; resolve `clusterRef` → `TemporalCluster` (same namespace). If
   missing, set `Ready=False/ClusterNotFound`, requeue.
2. Build TLS via existing `clusterTLSConfig`, dial `frontendAddress(cluster)`,
   build `ScheduleClient` via injectable factory; `defer Close()`.
3. Deletion: if `DeletionTimestamp` set, run finalizer logic — `Delete` the
   Temporal schedule only when `AllowDeletion`; remove finalizer.
4. Ensure finalizer `temporal.bmor10.com/schedule` is present.
5. Gate on cluster `Ready` condition; requeue 15s if not ready.
6. Reconcile (A+C):
   - Compute `specHash` = stable hash of the marshaled `ScheduleParams`
     (spec-derived, excludes status).
   - `Describe(namespace, scheduleID)`:
     - `ErrScheduleNotFound` → `Create`; set status hash.
     - other error → return error.
     - exists → if `status.LastAppliedSpecHash != specHash` → `Update`, set hash.
       else if live `Paused != desiredPaused` → `Pause`/`Unpause`.
   - Refresh `Describe` to populate status observability fields
     (`NextActionTimes`, `RunningWorkflows`, `Paused`, `Notes`).
7. Set `Ready=True/Reconciled`, `Created=true`, `LastUpdated`, status update.
8. Return `RequeueAfter = scheduleDriftRequeue` (5 min) to re-assert existence +
   pause and refresh observability.

`scheduleID` resolution: `spec.ScheduleID` or default `metadata.name`.

## Webhook — `internal/webhook/v1alpha1/temporalschedule_webhook.go`

Typed generic admission API (controller-runtime v0.23):
`var _ admission.Validator[*temporalv1alpha1.TemporalSchedule]`, registered via
`ctrl.NewWebhookManagedBy(mgr, &TemporalSchedule{}).WithValidator(...)`.

Validation (create + update):
- `action.startWorkflow.workflowType` and `taskQueue` non-empty.
- At least one time source in `schedule` (`calendars`/`intervals`/
  `structuredCalendar`) **unless** `state.paused` is true (a paused schedule
  with no times is intentional; otherwise reject to avoid Temporal's
  auto-deletion of action-less schedules).
- `policies.overlapPolicy` and `workflowIDReusePolicy` are valid enum values
  (also enforced by kubebuilder enums; webhook gives a clearer message).
- Durations non-negative; `retryPolicy.backoffCoefficient` parses as float ≥ 1
  when set; `args`/`memo`/`searchAttributes` values are valid JSON.
- `CalendarRange`: `start`/`end` within field bounds, `step` ≥ 1.

Validation (update only):
- `scheduleID` (resolved, including the name default) is immutable.

No defaulter needed beyond kubebuilder `+kubebuilder:default` markers.

## Wiring, RBAC, manifests

- `cmd/main.go`: register `TemporalScheduleReconciler{ Client, Scheme,
  ClientFactory }` and `SetupTemporalScheduleWebhookWithManager`.
- `+kubebuilder:rbac` markers for `temporalschedules`,
  `temporalschedules/status`, `temporalschedules/finalizers`.
- `make generate manifests` regenerates `zz_generated.deepcopy.go`, the CRD
  YAML, and RBAC role.
- Add `config/samples/temporal_v1alpha1_temporalschedule.yaml` and include it in
  the kustomization; add webhook + CRD kustomize entries.
- Regenerate Helm chart (`make helm-chart`) and committed `dist/` artifacts
  (chart + `install.yaml`) per repo convention.

## Testing

- `internal/temporal/schedule_test.go` (pure, table-driven): `toProtoSchedule`
  field mapping; cron/interval/structured-calendar mapping; payload encoding
  (metadata + bytes); overlap/reuse enum maps; error on unknown enum.
- `internal/controller/temporalschedule_controller_test.go`: fake
  `ScheduleClient` added to `fakes_test.go`; cases — create on missing, update
  on spec-hash change, pause/unpause patch, no-op when unchanged, finalizer +
  `AllowDeletion` delete (and skip-delete when false), cluster-not-found and
  cluster-not-ready gating.
- `internal/webhook/v1alpha1/temporalschedule_webhook_test.go`: required-field,
  enum, immutable-`scheduleID`, paused-without-times allowed, JSON-arg
  validation.
- Run `make generate manifests build test lint` to validate.
- `api/v1alpha1` must stay WASM-safe (pure types; `runtime.RawExtension` and
  `metav1` are already used here and are fine). Confirm
  `GOOS=js GOARCH=wasm go build ./api/...`.

## Out of scope (future, additive — no breaking changes)

- Imperative ops: trigger-immediately and backfill (annotation-driven).
- `rawPayloads` escape hatch for custom codecs/encodings.
- Full external structured-drift reconciliation (compare live compiled spec).
- `priority`, `versioningOverride`, `header`, `userMetadata` action fields.
- A Chainsaw e2e suite for schedules.
```
