# TemporalSchedule Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a declarative `TemporalSchedule` CRD that manages [Temporal Schedules](https://docs.temporal.io/schedule) against a running cluster.

**Architecture:** Follow the existing data-plane CRD pattern (`TemporalNamespace`/`TemporalSearchAttribute`): a namespaced CRD with `clusterRef`, a `internal/temporal` gRPC client wrapper that maps operator-domain structs to `go.temporal.io/api/schedule/v1` protos, a finalizer-based reconciler that creates/updates/pauses/deletes the schedule (generation-driven push + last-applied spec hash for drift), and a typed validating webhook.

**Tech Stack:** Go, controller-runtime v0.23 (typed generic webhooks), `go.temporal.io/api` (raw gRPC, no SDK), kubebuilder markers, Ginkgo/Gomega (controller + webhook suites), standard `go test` (pure mapping).

**Spec:** `docs/superpowers/specs/2026-06-17-temporalschedule-design.md`

**Conventions (verify as you go):**
- Every commit signed off: `git commit -s`. Conventional Commit prefixes (`feat:`, `test:`, `chore:`).
- Build/test/lint: `make build`, `make test`, `make lint`. Regenerate after API changes: `make generate manifests`.
- Copyright header (2026 Brian Morton, Apache-2.0) at the top of every new `.go` file — copy from any existing file in the same package.
- API group: `temporal.bmor10.com`. Module: `github.com/bmorton/temporal-operator`.
- `api/v1alpha1` must stay WASM-safe (pure types only).

---

## File Structure

**Create:**
- `api/v1alpha1/temporalschedule_types.go` — CRD spec/status Go types + kubebuilder markers.
- `internal/temporal/schedule.go` — `ScheduleClient` interface, gRPC impl, domain `ScheduleParams`/`ScheduleInfo`, proto mapping + payload encoding helpers.
- `internal/temporal/schedule_test.go` — pure table tests for mapping/encoding/enums.
- `internal/controller/temporalschedule_controller.go` — reconciler.
- `internal/controller/temporalschedule_controller_test.go` — Ginkgo reconciler tests + fake client.
- `internal/webhook/v1alpha1/temporalschedule_webhook.go` — typed validator.
- `internal/webhook/v1alpha1/temporalschedule_webhook_test.go` — Ginkgo validation tests.
- `config/samples/temporal_v1alpha1_temporalschedule.yaml` — example CR.

**Modify:**
- `cmd/main.go` — register reconciler + webhook.
- `config/samples/kustomization.yaml` — add the new sample.
- Generated (via `make`): `api/v1alpha1/zz_generated.deepcopy.go`, `config/crd/...`, `config/rbac/role.yaml`, `dist/` (chart + install.yaml).

---

## Task 1: API types

**Files:**
- Create: `api/v1alpha1/temporalschedule_types.go`
- Modify (generated): `api/v1alpha1/zz_generated.deepcopy.go`, `config/crd/`, `config/rbac/role.yaml`

- [ ] **Step 1: Write the types file**

Create `api/v1alpha1/temporalschedule_types.go` with the Apache-2.0 header (copy from `temporalnamespace_types.go`), then:

```go
package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// TemporalScheduleSpec defines the desired state of TemporalSchedule.
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

// ScheduleSpec is the set of times an action should occur at.
type ScheduleSpec struct {
	// Calendars holds cron strings (5/6/7 field, or @daily etc).
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
	// StartTime bounds the schedule start (inclusive).
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`
	// EndTime bounds the schedule end (inclusive).
	// +optional
	EndTime *metav1.Time `json:"endTime,omitempty"`
	// Jitter randomizes each action time by 0..Jitter.
	// +optional
	Jitter *metav1.Duration `json:"jitter,omitempty"`
	// TimezoneName interprets calendar specs (IANA name; defaults to UTC).
	// +optional
	TimezoneName string `json:"timezoneName,omitempty"`
}

// IntervalSpec matches times of epoch + n*Every + Offset.
type IntervalSpec struct {
	Every metav1.Duration `json:"every"`
	// +optional
	Offset *metav1.Duration `json:"offset,omitempty"`
}

// StructuredCalendarSpec describes calendar times as field ranges.
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

// CalendarRange is an inclusive [Start,End] range with an optional Step.
type CalendarRange struct {
	Start int32 `json:"start"`
	// +optional
	End int32 `json:"end,omitempty"`
	// +kubebuilder:default=1
	// +optional
	Step int32 `json:"step,omitempty"`
}

// ScheduleActionSpec is the action taken when the schedule fires.
type ScheduleActionSpec struct {
	StartWorkflow StartWorkflowAction `json:"startWorkflow"`
}

// StartWorkflowAction starts a workflow when the schedule fires.
type StartWorkflowAction struct {
	WorkflowType string `json:"workflowType"`
	TaskQueue    string `json:"taskQueue"`
	// +optional
	WorkflowID string `json:"workflowID,omitempty"`
	// Args are JSON-serializable workflow inputs (one json/plain payload each).
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
	// +optional
	Memo map[string]runtime.RawExtension `json:"memo,omitempty"`
	// +optional
	SearchAttributes map[string]runtime.RawExtension `json:"searchAttributes,omitempty"`
}

// RetryPolicySpec is the retry policy for the started workflow.
type RetryPolicySpec struct {
	// +optional
	InitialInterval *metav1.Duration `json:"initialInterval,omitempty"`
	// BackoffCoefficient is a decimal string (e.g. "2.0") parsed to float64.
	// +optional
	BackoffCoefficient string `json:"backoffCoefficient,omitempty"`
	// +optional
	MaximumInterval *metav1.Duration `json:"maximumInterval,omitempty"`
	// +optional
	MaximumAttempts int32 `json:"maximumAttempts,omitempty"`
	// +optional
	NonRetryableErrorTypes []string `json:"nonRetryableErrorTypes,omitempty"`
}

// SchedulePoliciesSpec tunes overlap/catchup behavior.
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

// ScheduleStateSpec controls pause and action-limit state.
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

// TemporalScheduleStatus defines the observed state of TemporalSchedule.
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
	// NextActionTimes is a small window of upcoming action times.
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

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=tsch
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef.name`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.spec.namespace`
// +kubebuilder:printcolumn:name="Paused",type=boolean,JSONPath=`.status.paused`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TemporalSchedule is the Schema for the temporalschedules API.
type TemporalSchedule struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`
	// +required
	Spec TemporalScheduleSpec `json:"spec"`
	// +optional
	Status TemporalScheduleStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TemporalScheduleList contains a list of TemporalSchedule.
type TemporalScheduleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TemporalSchedule `json:"items"`
}

func init() {
	registerType(&TemporalSchedule{}, &TemporalScheduleList{})
}
```

- [ ] **Step 2: Generate deepcopy + manifests**

Run: `make generate manifests`
Expected: succeeds; creates `config/crd/bases/temporal.bmor10.com_temporalschedules.yaml`, updates `zz_generated.deepcopy.go` and `config/rbac/role.yaml` is regenerated later (after Task 4 adds the rbac markers — re-run then). No errors.

- [ ] **Step 3: Build**

Run: `make build`
Expected: compiles clean.

- [ ] **Step 4: Verify WASM safety of the API package**

Run: `GOOS=js GOARCH=wasm go build ./api/...`
Expected: exits 0 (no client/IO imports leaked into api types).

- [ ] **Step 5: Commit**

```bash
git add api/v1alpha1/temporalschedule_types.go api/v1alpha1/zz_generated.deepcopy.go config/crd
git commit -s -m "feat(api): add TemporalSchedule CRD types"
```

---

## Task 2: Client domain structs + pure proto mapping

This task builds the operator-domain structs and the pure functions that map them into `go.temporal.io/api/schedule/v1` protos and encode JSON payloads. No gRPC yet — fully unit-testable.

**Files:**
- Create: `internal/temporal/schedule.go` (domain structs + mapping helpers)
- Create: `internal/temporal/schedule_test.go`

- [ ] **Step 1: Write the domain structs + mapping skeleton**

Create `internal/temporal/schedule.go` with the Apache-2.0 header (copy from `client.go`), then:

```go
package temporal

import (
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	schedulepb "go.temporal.io/api/schedule/v1"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
)

// ErrScheduleNotFound is returned by Describe when the schedule does not exist.
var ErrScheduleNotFound = errors.New("schedule not found")

// ScheduleParams is the operator-domain description of a Temporal schedule.
type ScheduleParams struct {
	ScheduleID string
	Namespace  string
	Spec       ScheduleSpecParams
	Action     StartWorkflowParams
	Policies   SchedulePolicyParams
	State      ScheduleStateParams
}

// ScheduleSpecParams is when a schedule fires.
type ScheduleSpecParams struct {
	CronStrings      []string
	Intervals        []IntervalParams
	Calendars        []StructuredCalendarParams
	ExcludeCalendars []StructuredCalendarParams
	StartTime        *time.Time
	EndTime          *time.Time
	Jitter           *time.Duration
	TimezoneName     string
}

// IntervalParams is an interval-based fire spec.
type IntervalParams struct {
	Every  time.Duration
	Offset *time.Duration
}

// StructuredCalendarParams is a field-range calendar spec.
type StructuredCalendarParams struct {
	Second     []RangeParams
	Minute     []RangeParams
	Hour       []RangeParams
	DayOfMonth []RangeParams
	Month      []RangeParams
	Year       []RangeParams
	DayOfWeek  []RangeParams
	Comment    string
}

// RangeParams is an inclusive [Start,End] range with Step.
type RangeParams struct {
	Start int32
	End   int32
	Step  int32
}

// StartWorkflowParams describes the workflow to start. Args/Memo/SearchAttributes
// hold raw JSON bytes that are encoded into json/plain payloads.
type StartWorkflowParams struct {
	WorkflowType     string
	TaskQueue        string
	WorkflowID       string
	Args             [][]byte
	ExecutionTimeout *time.Duration
	RunTimeout       *time.Duration
	TaskTimeout      *time.Duration
	IDReusePolicy    string
	Retry            *RetryParams
	Memo             map[string][]byte
	SearchAttributes map[string][]byte
}

// RetryParams is the started workflow's retry policy.
type RetryParams struct {
	InitialInterval        *time.Duration
	BackoffCoefficient     float64
	MaximumInterval        *time.Duration
	MaximumAttempts        int32
	NonRetryableErrorTypes []string
}

// SchedulePolicyParams tunes overlap/catchup behavior.
type SchedulePolicyParams struct {
	OverlapPolicy          string
	CatchupWindow          *time.Duration
	PauseOnFailure         bool
	KeepOriginalWorkflowID bool
}

// ScheduleStateParams controls pause and action-limit state.
type ScheduleStateParams struct {
	Paused           bool
	Notes            string
	LimitedActions   bool
	RemainingActions int64
}

// ScheduleInfo is the observed state of a Temporal schedule.
type ScheduleInfo struct {
	Paused           bool
	Notes            string
	NextActionTimes  []time.Time
	RunningWorkflows int
}

// overlapPolicies maps CR overlap-policy strings to Temporal enums.
var overlapPolicies = map[string]enumspb.ScheduleOverlapPolicy{
	"":               enumspb.SCHEDULE_OVERLAP_POLICY_UNSPECIFIED,
	"Skip":           enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
	"BufferOne":      enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE,
	"BufferAll":      enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ALL,
	"CancelOther":    enumspb.SCHEDULE_OVERLAP_POLICY_CANCEL_OTHER,
	"TerminateOther": enumspb.SCHEDULE_OVERLAP_POLICY_TERMINATE_OTHER,
	"AllowAll":       enumspb.SCHEDULE_OVERLAP_POLICY_ALLOW_ALL,
}

// reusePolicies maps CR workflow-id-reuse strings to Temporal enums.
var reusePolicies = map[string]enumspb.WorkflowIdReusePolicy{
	"":                         enumspb.WORKFLOW_ID_REUSE_POLICY_UNSPECIFIED,
	"AllowDuplicate":           enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE,
	"AllowDuplicateFailedOnly": enumspb.WORKFLOW_ID_REUSE_POLICY_ALLOW_DUPLICATE_FAILED_ONLY,
	"RejectDuplicate":          enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
	"TerminateIfRunning":       enumspb.WORKFLOW_ID_REUSE_POLICY_TERMINATE_IF_RUNNING,
}

const jsonPlainEncoding = "json/plain"

// encodeJSONPayload wraps raw JSON bytes in a json/plain Payload.
func encodeJSONPayload(raw []byte) *commonpb.Payload {
	return &commonpb.Payload{
		Metadata: map[string][]byte{"encoding": []byte(jsonPlainEncoding)},
		Data:     raw,
	}
}

// encodeJSONPayloads wraps an ordered list of raw JSON values.
func encodeJSONPayloads(raws [][]byte) *commonpb.Payloads {
	if len(raws) == 0 {
		return nil
	}
	out := &commonpb.Payloads{Payloads: make([]*commonpb.Payload, 0, len(raws))}
	for _, r := range raws {
		out.Payloads = append(out.Payloads, encodeJSONPayload(r))
	}
	return out
}

// encodeJSONFields wraps a map of raw JSON values (for memo/search attributes).
func encodeJSONFields(raws map[string][]byte) map[string]*commonpb.Payload {
	if len(raws) == 0 {
		return nil
	}
	out := make(map[string]*commonpb.Payload, len(raws))
	for k, r := range raws {
		out[k] = encodeJSONPayload(r)
	}
	return out
}

func protoRanges(rs []RangeParams) []*schedulepb.Range {
	if len(rs) == 0 {
		return nil
	}
	out := make([]*schedulepb.Range, 0, len(rs))
	for _, r := range rs {
		step := r.Step
		if step == 0 {
			step = 1
		}
		out = append(out, &schedulepb.Range{Start: r.Start, End: r.End, Step: step})
	}
	return out
}

func protoCalendar(c StructuredCalendarParams) *schedulepb.StructuredCalendarSpec {
	return &schedulepb.StructuredCalendarSpec{
		Second:     protoRanges(c.Second),
		Minute:     protoRanges(c.Minute),
		Hour:       protoRanges(c.Hour),
		DayOfMonth: protoRanges(c.DayOfMonth),
		Month:      protoRanges(c.Month),
		Year:       protoRanges(c.Year),
		DayOfWeek:  protoRanges(c.DayOfWeek),
		Comment:    c.Comment,
	}
}

func optDuration(d *time.Duration) *durationpb.Duration {
	if d == nil {
		return nil
	}
	return durationpb.New(*d)
}

func optTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}

// toProtoSchedule maps ScheduleParams into a Temporal Schedule proto.
func toProtoSchedule(p ScheduleParams) (*schedulepb.Schedule, error) {
	overlap, ok := overlapPolicies[p.Policies.OverlapPolicy]
	if !ok {
		return nil, fmt.Errorf("unknown overlap policy %q", p.Policies.OverlapPolicy)
	}
	reuse, ok := reusePolicies[p.Action.IDReusePolicy]
	if !ok {
		return nil, fmt.Errorf("unknown workflow id reuse policy %q", p.Action.IDReusePolicy)
	}

	spec := &schedulepb.ScheduleSpec{
		CronString:   p.Spec.CronStrings,
		StartTime:    optTimestamp(p.Spec.StartTime),
		EndTime:      optTimestamp(p.Spec.EndTime),
		Jitter:       optDuration(p.Spec.Jitter),
		TimezoneName: p.Spec.TimezoneName,
	}
	for _, iv := range p.Spec.Intervals {
		spec.Interval = append(spec.Interval, &schedulepb.IntervalSpec{
			Interval: durationpb.New(iv.Every),
			Phase:    optDuration(iv.Offset),
		})
	}
	for _, c := range p.Spec.Calendars {
		spec.StructuredCalendar = append(spec.StructuredCalendar, protoCalendar(c))
	}
	for _, c := range p.Spec.ExcludeCalendars {
		spec.ExcludeStructuredCalendar = append(spec.ExcludeStructuredCalendar, protoCalendar(c))
	}

	wf := &workflowpb.NewWorkflowExecutionInfo{
		WorkflowId:               p.Action.WorkflowID,
		WorkflowType:             &commonpb.WorkflowType{Name: p.Action.WorkflowType},
		TaskQueue:                &taskqueuepb.TaskQueue{Name: p.Action.TaskQueue, Kind: enumspb.TASK_QUEUE_KIND_NORMAL},
		Input:                    encodeJSONPayloads(p.Action.Args),
		WorkflowExecutionTimeout: optDuration(p.Action.ExecutionTimeout),
		WorkflowRunTimeout:       optDuration(p.Action.RunTimeout),
		WorkflowTaskTimeout:      optDuration(p.Action.TaskTimeout),
		WorkflowIdReusePolicy:    reuse,
	}
	if m := encodeJSONFields(p.Action.Memo); m != nil {
		wf.Memo = &commonpb.Memo{Fields: m}
	}
	if sa := encodeJSONFields(p.Action.SearchAttributes); sa != nil {
		wf.SearchAttributes = &commonpb.SearchAttributes{IndexedFields: sa}
	}
	if r := p.Action.Retry; r != nil {
		wf.RetryPolicy = &commonpb.RetryPolicy{
			InitialInterval:        optDuration(r.InitialInterval),
			BackoffCoefficient:     r.BackoffCoefficient,
			MaximumInterval:        optDuration(r.MaximumInterval),
			MaximumAttempts:        r.MaximumAttempts,
			NonRetryableErrorTypes: r.NonRetryableErrorTypes,
		}
	}

	return &schedulepb.Schedule{
		Spec: spec,
		Action: &schedulepb.ScheduleAction{
			Action: &schedulepb.ScheduleAction_StartWorkflow{StartWorkflow: wf},
		},
		Policies: &schedulepb.SchedulePolicies{
			OverlapPolicy:          overlap,
			CatchupWindow:          optDuration(p.Policies.CatchupWindow),
			PauseOnFailure:         p.Policies.PauseOnFailure,
			KeepOriginalWorkflowId: p.Policies.KeepOriginalWorkflowID,
		},
		State: &schedulepb.ScheduleState{
			Notes:            p.State.Notes,
			Paused:           p.State.Paused,
			LimitedActions:   p.State.LimitedActions,
			RemainingActions: p.State.RemainingActions,
		},
	}, nil
}
```

- [ ] **Step 2: Write the failing test**

Create `internal/temporal/schedule_test.go` with the Apache-2.0 header, then:

```go
package temporal

import (
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
)

func baseParams() ScheduleParams {
	return ScheduleParams{
		ScheduleID: "sched-1",
		Namespace:  "orders",
		Spec:       ScheduleSpecParams{CronStrings: []string{"0 9 * * *"}},
		Action: StartWorkflowParams{
			WorkflowType: "ProcessOrders",
			TaskQueue:    "orders-tq",
			Args:         [][]byte{[]byte(`{"limit":10}`), []byte(`"hello"`)},
		},
	}
}

func TestToProtoSchedule_ActionAndPayloads(t *testing.T) {
	got, err := toProtoSchedule(baseParams())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wf := got.GetAction().GetStartWorkflow()
	if wf.GetWorkflowType().GetName() != "ProcessOrders" {
		t.Errorf("workflow type = %q", wf.GetWorkflowType().GetName())
	}
	if wf.GetTaskQueue().GetName() != "orders-tq" {
		t.Errorf("task queue = %q", wf.GetTaskQueue().GetName())
	}
	payloads := wf.GetInput().GetPayloads()
	if len(payloads) != 2 {
		t.Fatalf("payloads = %d, want 2", len(payloads))
	}
	if string(payloads[0].GetMetadata()["encoding"]) != "json/plain" {
		t.Errorf("encoding = %q", payloads[0].GetMetadata()["encoding"])
	}
	if string(payloads[0].GetData()) != `{"limit":10}` {
		t.Errorf("payload[0] data = %q", payloads[0].GetData())
	}
	if got.GetSpec().GetCronString()[0] != "0 9 * * *" {
		t.Errorf("cron = %v", got.GetSpec().GetCronString())
	}
}

func TestToProtoSchedule_OverlapAndReuseEnums(t *testing.T) {
	p := baseParams()
	p.Policies.OverlapPolicy = "BufferOne"
	p.Action.IDReusePolicy = "RejectDuplicate"
	got, err := toProtoSchedule(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GetPolicies().GetOverlapPolicy() != enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE {
		t.Errorf("overlap = %v", got.GetPolicies().GetOverlapPolicy())
	}
	if got.GetAction().GetStartWorkflow().GetWorkflowIdReusePolicy() != enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE {
		t.Errorf("reuse = %v", got.GetAction().GetStartWorkflow().GetWorkflowIdReusePolicy())
	}
}

func TestToProtoSchedule_UnknownOverlapPolicy(t *testing.T) {
	p := baseParams()
	p.Policies.OverlapPolicy = "Nonsense"
	if _, err := toProtoSchedule(p); err == nil {
		t.Fatal("expected error for unknown overlap policy")
	}
}

func TestToProtoSchedule_IntervalAndStructuredCalendar(t *testing.T) {
	p := baseParams()
	p.Spec.CronStrings = nil
	off := 15 * time.Minute
	p.Spec.Intervals = []IntervalParams{{Every: time.Hour, Offset: &off}}
	p.Spec.Calendars = []StructuredCalendarParams{{
		Hour:   []RangeParams{{Start: 9, End: 17, Step: 0}},
		Minute: []RangeParams{{Start: 0}},
	}}
	got, err := toProtoSchedule(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.GetSpec().GetInterval()[0].GetInterval().AsDuration() != time.Hour {
		t.Errorf("interval = %v", got.GetSpec().GetInterval()[0].GetInterval().AsDuration())
	}
	if got.GetSpec().GetInterval()[0].GetPhase().AsDuration() != 15*time.Minute {
		t.Errorf("phase = %v", got.GetSpec().GetInterval()[0].GetPhase().AsDuration())
	}
	// Step defaults to 1 when zero.
	if got.GetSpec().GetStructuredCalendar()[0].GetHour()[0].GetStep() != 1 {
		t.Errorf("hour step = %d, want 1", got.GetSpec().GetStructuredCalendar()[0].GetHour()[0].GetStep())
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `go test ./internal/temporal/ -run TestToProtoSchedule -v`
Expected: PASS (the implementation from Step 1 satisfies them).

- [ ] **Step 4: Lint + commit**

```bash
make lint
git add internal/temporal/schedule.go internal/temporal/schedule_test.go
git commit -s -m "feat(temporal): add schedule proto mapping and payload encoding"
```

---

## Task 3: gRPC ScheduleClient

Adds the interface, factory, and gRPC-backed implementation. The gRPC plumbing mirrors `NewNamespaceClient` in `client.go`. It is not unit-tested directly (needs a live server); correctness of the request building is covered by Task 2's mapping tests and Task 4's fake-client reconciler tests.

**Files:**
- Modify: `internal/temporal/schedule.go` (append)

- [ ] **Step 1: Append the interface, factory, and implementation**

Append to `internal/temporal/schedule.go`. Add these imports to the existing import block:

```go
	"context"
	"crypto/tls"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	workflowservice "go.temporal.io/api/workflowservice/v1"
```

Then append:

```go
// ScheduleClient manages schedules in a Temporal cluster.
type ScheduleClient interface {
	Describe(ctx context.Context, namespace, scheduleID string) (*ScheduleInfo, error)
	Create(ctx context.Context, params ScheduleParams) error
	Update(ctx context.Context, params ScheduleParams) error
	Pause(ctx context.Context, namespace, scheduleID, notes string) error
	Unpause(ctx context.Context, namespace, scheduleID, notes string) error
	Delete(ctx context.Context, namespace, scheduleID string) error
	Close() error
}

// ScheduleClientFactory builds a ScheduleClient connected to a frontend address.
// A nil tlsConfig means an insecure connection.
type ScheduleClientFactory func(ctx context.Context, address string, tlsConfig *tls.Config) (ScheduleClient, error)

// identity identifies this operator in Temporal mutation requests.
const scheduleIdentity = "temporal-operator"

type grpcScheduleClient struct {
	conn     *grpc.ClientConn
	workflow workflowservice.WorkflowServiceClient
}

// NewScheduleClient dials the frontend and returns a ScheduleClient.
func NewScheduleClient(_ context.Context, address string, tlsConfig *tls.Config) (ScheduleClient, error) {
	creds := insecure.NewCredentials()
	if tlsConfig != nil {
		creds = credentials.NewTLS(tlsConfig)
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, err
	}
	return &grpcScheduleClient{
		conn:     conn,
		workflow: workflowservice.NewWorkflowServiceClient(conn),
	}, nil
}

func (c *grpcScheduleClient) Describe(ctx context.Context, namespace, scheduleID string) (*ScheduleInfo, error) {
	resp, err := c.workflow.DescribeSchedule(ctx, &workflowservice.DescribeScheduleRequest{
		Namespace:  namespace,
		ScheduleId: scheduleID,
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrScheduleNotFound
		}
		return nil, err
	}
	info := &ScheduleInfo{
		Paused: resp.GetSchedule().GetState().GetPaused(),
		Notes:  resp.GetSchedule().GetState().GetNotes(),
	}
	for _, t := range resp.GetInfo().GetFutureActionTimes() {
		info.NextActionTimes = append(info.NextActionTimes, t.AsTime())
	}
	info.RunningWorkflows = len(resp.GetInfo().GetRunningWorkflows())
	return info, nil
}

func (c *grpcScheduleClient) Create(ctx context.Context, params ScheduleParams) error {
	sched, err := toProtoSchedule(params)
	if err != nil {
		return err
	}
	_, err = c.workflow.CreateSchedule(ctx, &workflowservice.CreateScheduleRequest{
		Namespace:  params.Namespace,
		ScheduleId: params.ScheduleID,
		Schedule:   sched,
		Identity:   scheduleIdentity,
	})
	return err
}

func (c *grpcScheduleClient) Update(ctx context.Context, params ScheduleParams) error {
	sched, err := toProtoSchedule(params)
	if err != nil {
		return err
	}
	_, err = c.workflow.UpdateSchedule(ctx, &workflowservice.UpdateScheduleRequest{
		Namespace:  params.Namespace,
		ScheduleId: params.ScheduleID,
		Schedule:   sched,
		Identity:   scheduleIdentity,
	})
	return err
}

func (c *grpcScheduleClient) Pause(ctx context.Context, namespace, scheduleID, notes string) error {
	return c.patch(ctx, namespace, scheduleID, &schedulepb.SchedulePatch{Pause: pauseNotes(notes, "paused by temporal-operator")})
}

func (c *grpcScheduleClient) Unpause(ctx context.Context, namespace, scheduleID, notes string) error {
	return c.patch(ctx, namespace, scheduleID, &schedulepb.SchedulePatch{Unpause: pauseNotes(notes, "unpaused by temporal-operator")})
}

func (c *grpcScheduleClient) patch(ctx context.Context, namespace, scheduleID string, p *schedulepb.SchedulePatch) error {
	_, err := c.workflow.PatchSchedule(ctx, &workflowservice.PatchScheduleRequest{
		Namespace:  namespace,
		ScheduleId: scheduleID,
		Patch:      p,
		Identity:   scheduleIdentity,
	})
	return err
}

func (c *grpcScheduleClient) Delete(ctx context.Context, namespace, scheduleID string) error {
	_, err := c.workflow.DeleteSchedule(ctx, &workflowservice.DeleteScheduleRequest{
		Namespace:  namespace,
		ScheduleId: scheduleID,
		Identity:   scheduleIdentity,
	})
	if err != nil && status.Code(err) == codes.NotFound {
		return ErrScheduleNotFound
	}
	return err
}

func (c *grpcScheduleClient) Close() error { return c.conn.Close() }

func pauseNotes(notes, fallback string) string {
	if notes != "" {
		return notes
	}
	return fallback
}
```

- [ ] **Step 2: Build**

Run: `make build`
Expected: compiles clean. Fix any import path mismatches (`taskqueuepb`, `workflowpb`, `commonpb` come from `go.temporal.io/api/taskqueue/v1`, `.../workflow/v1`, `.../common/v1`).

- [ ] **Step 3: Run temporal package tests**

Run: `go test ./internal/temporal/ -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/temporal/schedule.go
git commit -s -m "feat(temporal): add gRPC ScheduleClient"
```

---

## Task 4: Reconciler

Implements the controller and its Ginkgo tests with a fake `ScheduleClient`. Reconcile logic: resolve cluster, gate on readiness, ensure finalizer, then **generation-driven push + last-applied spec hash**: create if missing, update if the spec hash changed, otherwise reconcile pause state; refresh status from `Describe`.

**Files:**
- Create: `internal/controller/temporalschedule_controller.go`
- Create: `internal/controller/temporalschedule_controller_test.go`

- [ ] **Step 1: Write the controller**

Create `internal/controller/temporalschedule_controller.go` with the Apache-2.0 header, then:

```go
package controller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

const scheduleFinalizer = "temporal.bmor10.com/schedule"

// scheduleDriftRequeue is how often a schedule is re-asserted (existence + pause).
const scheduleDriftRequeue = 5 * time.Minute

// TemporalScheduleReconciler reconciles TemporalSchedule objects.
type TemporalScheduleReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// ClientFactory builds the Temporal schedule client; injectable for tests.
	ClientFactory temporal.ScheduleClientFactory
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalschedules,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalschedules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalschedules/finalizers,verbs=update

// Reconcile creates, updates, pauses, or deletes a Temporal schedule.
func (r *TemporalScheduleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var sched temporalv1alpha1.TemporalSchedule
	if err := r.Get(ctx, req.NamespacedName, &sched); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var cluster temporalv1alpha1.TemporalCluster
	clusterKey := types.NamespacedName{Namespace: sched.Namespace, Name: sched.Spec.ClusterRef.Name}
	if err := r.Get(ctx, clusterKey, &cluster); err != nil {
		r.setReady(&sched, metav1.ConditionFalse, "ClusterNotFound", "referenced TemporalCluster not found")
		return ctrl.Result{RequeueAfter: scheduleDriftRequeue}, r.statusUpdate(ctx, &sched)
	}

	tlsConfig, err := clusterTLSConfig(ctx, r.Client, &cluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building temporal client tls: %w", err)
	}
	sc, err := r.clientFactory()(ctx, frontendAddress(&cluster), tlsConfig)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building temporal client: %w", err)
	}
	defer func() { _ = sc.Close() }()

	if !sched.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, &sched, sc)
	}

	if !controllerutil.ContainsFinalizer(&sched, scheduleFinalizer) {
		controllerutil.AddFinalizer(&sched, scheduleFinalizer)
		if err := r.Update(ctx, &sched); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionReady) {
		r.setReady(&sched, metav1.ConditionFalse, "ClusterNotReady", "waiting for the TemporalCluster to become ready")
		return ctrl.Result{RequeueAfter: 15 * time.Second}, r.statusUpdate(ctx, &sched)
	}

	if err := r.reconcileSchedule(ctx, &sched, sc); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: scheduleDriftRequeue}, r.statusUpdate(ctx, &sched)
}

func (r *TemporalScheduleReconciler) reconcileSchedule(ctx context.Context, sched *temporalv1alpha1.TemporalSchedule, sc temporal.ScheduleClient) error {
	log := logf.FromContext(ctx)
	params, err := scheduleParams(sched)
	if err != nil {
		r.setReady(sched, metav1.ConditionFalse, "InvalidSpec", err.Error())
		return nil
	}
	specHash, err := computeSpecHash(params)
	if err != nil {
		return fmt.Errorf("hashing schedule spec: %w", err)
	}

	_, err = sc.Describe(ctx, params.Namespace, params.ScheduleID)
	switch {
	case errors.Is(err, temporal.ErrScheduleNotFound):
		if err := sc.Create(ctx, params); err != nil {
			return fmt.Errorf("creating schedule: %w", err)
		}
		log.Info("created schedule", "scheduleID", params.ScheduleID)
		sched.Status.LastAppliedSpecHash = specHash
	case err != nil:
		return fmt.Errorf("describing schedule: %w", err)
	default:
		if sched.Status.LastAppliedSpecHash != specHash {
			if err := sc.Update(ctx, params); err != nil {
				return fmt.Errorf("updating schedule: %w", err)
			}
			log.Info("updated schedule to apply spec change", "scheduleID", params.ScheduleID)
			sched.Status.LastAppliedSpecHash = specHash
		} else if err := r.reconcilePause(ctx, sched, sc, params); err != nil {
			return err
		}
	}

	info, err := sc.Describe(ctx, params.Namespace, params.ScheduleID)
	if err != nil {
		return fmt.Errorf("describing schedule: %w", err)
	}
	now := metav1.Now()
	sched.Status.ScheduleID = params.ScheduleID
	sched.Status.Created = true
	sched.Status.Paused = info.Paused
	sched.Status.Notes = info.Notes
	sched.Status.RunningWorkflows = int32(info.RunningWorkflows)
	sched.Status.NextActionTimes = nil
	for _, t := range info.NextActionTimes {
		sched.Status.NextActionTimes = append(sched.Status.NextActionTimes, metav1.NewTime(t))
	}
	sched.Status.LastUpdated = &now
	r.setReady(sched, metav1.ConditionTrue, "Reconciled", "schedule is reconciled")
	return nil
}

func (r *TemporalScheduleReconciler) reconcilePause(ctx context.Context, sched *temporalv1alpha1.TemporalSchedule, sc temporal.ScheduleClient, params temporal.ScheduleParams) error {
	info, err := sc.Describe(ctx, params.Namespace, params.ScheduleID)
	if err != nil {
		return fmt.Errorf("describing schedule: %w", err)
	}
	if params.State.Paused == info.Paused {
		return nil
	}
	if params.State.Paused {
		return sc.Pause(ctx, params.Namespace, params.ScheduleID, params.State.Notes)
	}
	return sc.Unpause(ctx, params.Namespace, params.ScheduleID, params.State.Notes)
}

func (r *TemporalScheduleReconciler) reconcileDelete(ctx context.Context, sched *temporalv1alpha1.TemporalSchedule, sc temporal.ScheduleClient) error {
	log := logf.FromContext(ctx)
	if controllerutil.ContainsFinalizer(sched, scheduleFinalizer) {
		if sched.Spec.AllowDeletion {
			id := resolveScheduleID(sched)
			if err := sc.Delete(ctx, sched.Spec.Namespace, id); err != nil && !errors.Is(err, temporal.ErrScheduleNotFound) {
				return fmt.Errorf("deleting schedule: %w", err)
			}
			log.Info("deleted temporal schedule", "scheduleID", id)
		}
		controllerutil.RemoveFinalizer(sched, scheduleFinalizer)
		if err := r.Update(ctx, sched); err != nil {
			return err
		}
	}
	return nil
}

func (r *TemporalScheduleReconciler) clientFactory() temporal.ScheduleClientFactory {
	if r.ClientFactory != nil {
		return r.ClientFactory
	}
	return temporal.NewScheduleClient
}

func (r *TemporalScheduleReconciler) setReady(sched *temporalv1alpha1.TemporalSchedule, status metav1.ConditionStatus, reason, message string) {
	sched.Status.ObservedGeneration = sched.Generation
	meta.SetStatusCondition(&sched.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: sched.Generation,
	})
}

func (r *TemporalScheduleReconciler) statusUpdate(ctx context.Context, sched *temporalv1alpha1.TemporalSchedule) error {
	return client.IgnoreNotFound(r.Status().Update(ctx, sched))
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalSchedule{}).
		Named("temporalschedule").
		Complete(r)
}

func resolveScheduleID(sched *temporalv1alpha1.TemporalSchedule) string {
	if sched.Spec.ScheduleID != "" {
		return sched.Spec.ScheduleID
	}
	return sched.Name
}

func computeSpecHash(params temporal.ScheduleParams) (string, error) {
	b, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// scheduleParams converts a TemporalSchedule CR into temporal.ScheduleParams.
func scheduleParams(sched *temporalv1alpha1.TemporalSchedule) (temporal.ScheduleParams, error) {
	a := sched.Spec.Action.StartWorkflow
	action := temporal.StartWorkflowParams{
		WorkflowType:     a.WorkflowType,
		TaskQueue:        a.TaskQueue,
		WorkflowID:       a.WorkflowID,
		Args:             rawList(a.Args),
		ExecutionTimeout: durPtr(a.WorkflowExecutionTimeout),
		RunTimeout:       durPtr(a.WorkflowRunTimeout),
		TaskTimeout:      durPtr(a.WorkflowTaskTimeout),
		IDReusePolicy:    a.WorkflowIDReusePolicy,
		Memo:             rawMap(a.Memo),
		SearchAttributes: rawMap(a.SearchAttributes),
	}
	if a.RetryPolicy != nil {
		backoff := 2.0
		if a.RetryPolicy.BackoffCoefficient != "" {
			f, err := strconv.ParseFloat(a.RetryPolicy.BackoffCoefficient, 64)
			if err != nil {
				return temporal.ScheduleParams{}, fmt.Errorf("invalid retryPolicy.backoffCoefficient: %w", err)
			}
			backoff = f
		}
		action.Retry = &temporal.RetryParams{
			InitialInterval:        durPtr(a.RetryPolicy.InitialInterval),
			BackoffCoefficient:     backoff,
			MaximumInterval:        durPtr(a.RetryPolicy.MaximumInterval),
			MaximumAttempts:        a.RetryPolicy.MaximumAttempts,
			NonRetryableErrorTypes: a.RetryPolicy.NonRetryableErrorTypes,
		}
	}

	spec := temporal.ScheduleSpecParams{
		CronStrings:      sched.Spec.Schedule.Calendars,
		StartTime:        timePtr(sched.Spec.Schedule.StartTime),
		EndTime:          timePtr(sched.Spec.Schedule.EndTime),
		Jitter:           durPtr(sched.Spec.Schedule.Jitter),
		TimezoneName:     sched.Spec.Schedule.TimezoneName,
		Calendars:        calendars(sched.Spec.Schedule.StructuredCalendar),
		ExcludeCalendars: calendars(sched.Spec.Schedule.ExcludeStructuredCalendar),
	}
	for _, iv := range sched.Spec.Schedule.Intervals {
		spec.Intervals = append(spec.Intervals, temporal.IntervalParams{
			Every:  iv.Every.Duration,
			Offset: durPtr(iv.Offset),
		})
	}

	var policies temporal.SchedulePolicyParams
	if p := sched.Spec.Policies; p != nil {
		policies = temporal.SchedulePolicyParams{
			OverlapPolicy:          p.OverlapPolicy,
			CatchupWindow:          durPtr(p.CatchupWindow),
			PauseOnFailure:         p.PauseOnFailure,
			KeepOriginalWorkflowID: p.KeepOriginalWorkflowID,
		}
	}

	var state temporal.ScheduleStateParams
	if s := sched.Spec.State; s != nil {
		state = temporal.ScheduleStateParams{
			Paused:           s.Paused,
			Notes:            s.Notes,
			LimitedActions:   s.LimitedActions,
			RemainingActions: s.RemainingActions,
		}
	}

	return temporal.ScheduleParams{
		ScheduleID: resolveScheduleID(sched),
		Namespace:  sched.Spec.Namespace,
		Spec:       spec,
		Action:     action,
		Policies:   policies,
		State:      state,
	}, nil
}

func calendars(in []temporalv1alpha1.StructuredCalendarSpec) []temporal.StructuredCalendarParams {
	if len(in) == 0 {
		return nil
	}
	out := make([]temporal.StructuredCalendarParams, 0, len(in))
	for _, c := range in {
		out = append(out, temporal.StructuredCalendarParams{
			Second:     ranges(c.Second),
			Minute:     ranges(c.Minute),
			Hour:       ranges(c.Hour),
			DayOfMonth: ranges(c.DayOfMonth),
			Month:      ranges(c.Month),
			Year:       ranges(c.Year),
			DayOfWeek:  ranges(c.DayOfWeek),
			Comment:    c.Comment,
		})
	}
	return out
}

func ranges(in []temporalv1alpha1.CalendarRange) []temporal.RangeParams {
	if len(in) == 0 {
		return nil
	}
	out := make([]temporal.RangeParams, 0, len(in))
	for _, r := range in {
		out = append(out, temporal.RangeParams{Start: r.Start, End: r.End, Step: r.Step})
	}
	return out
}

func rawList(in []runtime.RawExtension) [][]byte {
	if len(in) == 0 {
		return nil
	}
	out := make([][]byte, 0, len(in))
	for _, r := range in {
		out = append(out, r.Raw)
	}
	return out
}

func rawMap(in map[string]runtime.RawExtension) map[string][]byte {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(in))
	for k, r := range in {
		out[k] = r.Raw
	}
	return out
}

func durPtr(d *metav1.Duration) *time.Duration {
	if d == nil {
		return nil
	}
	v := d.Duration
	return &v
}

func timePtr(t *metav1.Time) *time.Time {
	if t == nil {
		return nil
	}
	v := t.Time
	return &v
}
```

- [ ] **Step 2: Write the failing reconciler test**

Create `internal/controller/temporalschedule_controller_test.go` with the Apache-2.0 header, then:

```go
package controller

import (
	"context"
	"crypto/tls"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// fakeScheduleClient records operations against an in-memory schedule store.
type fakeScheduleClient struct {
	store                     map[string]*temporal.ScheduleInfo
	created, updated, deleted []string
	paused, unpaused          []string
}

func (f *fakeScheduleClient) key(ns, id string) string { return ns + "/" + id }

func (f *fakeScheduleClient) Describe(_ context.Context, ns, id string) (*temporal.ScheduleInfo, error) {
	info, ok := f.store[f.key(ns, id)]
	if !ok {
		return nil, temporal.ErrScheduleNotFound
	}
	return info, nil
}

func (f *fakeScheduleClient) Create(_ context.Context, p temporal.ScheduleParams) error {
	f.created = append(f.created, p.ScheduleID)
	f.store[f.key(p.Namespace, p.ScheduleID)] = &temporal.ScheduleInfo{Paused: p.State.Paused, Notes: p.State.Notes}
	return nil
}

func (f *fakeScheduleClient) Update(_ context.Context, p temporal.ScheduleParams) error {
	f.updated = append(f.updated, p.ScheduleID)
	if info, ok := f.store[f.key(p.Namespace, p.ScheduleID)]; ok {
		info.Paused = p.State.Paused
	}
	return nil
}

func (f *fakeScheduleClient) Pause(_ context.Context, ns, id, _ string) error {
	f.paused = append(f.paused, id)
	if info, ok := f.store[f.key(ns, id)]; ok {
		info.Paused = true
	}
	return nil
}

func (f *fakeScheduleClient) Unpause(_ context.Context, ns, id, _ string) error {
	f.unpaused = append(f.unpaused, id)
	if info, ok := f.store[f.key(ns, id)]; ok {
		info.Paused = false
	}
	return nil
}

func (f *fakeScheduleClient) Delete(_ context.Context, ns, id string) error {
	f.deleted = append(f.deleted, id)
	delete(f.store, f.key(ns, id))
	return nil
}

func (f *fakeScheduleClient) Close() error { return nil }

var _ = Describe("TemporalSchedule reconciler", func() {
	ctx := context.Background()
	var counter int
	var fake *fakeScheduleClient

	var factory temporal.ScheduleClientFactory = func(_ context.Context, _ string, _ *tls.Config) (temporal.ScheduleClient, error) {
		return fake, nil
	}

	reconciler := func() *TemporalScheduleReconciler {
		return &TemporalScheduleReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ClientFactory: factory}
	}

	// readyCluster creates a TemporalCluster with a Ready=True condition.
	newCluster := func(name, ns string) *temporalv1alpha1.TemporalCluster {
		c := &temporalv1alpha1.TemporalCluster{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
		// Minimal valid spec fields required by the API; copy what the namespace
		// reconciler test uses (see temporalnamespace_controller_test.go helpers).
		return c
	}

	newSchedule := func(name, ns, cluster string) *temporalv1alpha1.TemporalSchedule {
		return &temporalv1alpha1.TemporalSchedule{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: temporalv1alpha1.TemporalScheduleSpec{
				ClusterRef: corev1.LocalObjectReference{Name: cluster},
				Namespace:  "orders",
				Schedule:   temporalv1alpha1.ScheduleSpec{Calendars: []string{"0 9 * * *"}},
				Action: temporalv1alpha1.ScheduleActionSpec{
					StartWorkflow: temporalv1alpha1.StartWorkflowAction{
						WorkflowType: "ProcessOrders",
						TaskQueue:    "orders-tq",
					},
				},
			},
		}
	}

	markClusterReady := func(c *temporalv1alpha1.TemporalCluster) {
		meta.SetStatusCondition(&c.Status.Conditions, metav1.Condition{
			Type: temporalv1alpha1.ConditionReady, Status: metav1.ConditionTrue,
			Reason: "Ready", Message: "ready",
		})
		Expect(k8sClient.Status().Update(ctx, c)).To(Succeed())
	}

	BeforeEach(func() {
		counter++
		fake = &fakeScheduleClient{store: map[string]*temporal.ScheduleInfo{}}
	})

	It("creates a schedule when missing", func() {
		ns := fmt.Sprintf("default")
		c := newCluster(fmt.Sprintf("cluster-%d", counter), ns)
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		markClusterReady(c)

		s := newSchedule(fmt.Sprintf("sched-%d", counter), ns, c.Name)
		Expect(k8sClient.Create(ctx, s)).To(Succeed())

		_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: s.Name, Namespace: ns}})
		Expect(err).NotTo(HaveOccurred())
		// Second reconcile (finalizer add triggers requeue in pattern); run again.
		_, err = reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: s.Name, Namespace: ns}})
		Expect(err).NotTo(HaveOccurred())

		Expect(fake.created).To(ContainElement(s.Name))

		var got temporalv1alpha1.TemporalSchedule
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: s.Name, Namespace: ns}, &got)).To(Succeed())
		Expect(got.Status.Created).To(BeTrue())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeTrue())
	})
})
```

NOTE: `newCluster` must produce a cluster that passes API validation in the envtest suite. Open `internal/controller/temporalnamespace_controller_test.go` and copy its exact cluster-construction helper (spec fields like version/persistence) so the `Create` succeeds; the stub above intentionally leaves the spec minimal and WILL need those fields filled in.

- [ ] **Step 3: Run the test (expect compile/setup success then green)**

Run: `make test` (envtest suite) or scoped: `go test ./internal/controller/ -run TestControllers -v`
Expected: the new spec passes (`creates a schedule when missing`). If the cluster `Create` fails validation, fix `newCluster` per the NOTE.

- [ ] **Step 4: Add remaining reconciler test cases**

Add these `It` blocks inside the `Describe`, mirroring the first (each does two reconciles after setup):

- `"updates the schedule when the spec hash changes"`: after the create flow, fetch the CR, change `Spec.Schedule.Calendars` to `[]string{"0 10 * * *"}`, `Update`, reconcile twice, assert `fake.updated` contains the name.
- `"pauses the schedule when spec.state.paused becomes true"`: after create, set `Spec.State = &temporalv1alpha1.ScheduleStateSpec{Paused: true}` WITHOUT changing the schedule spec hash... note: changing state changes the hash too. Instead assert pause via update path: set paused, reconcile, assert the live store shows `Paused == true` (covered by Update). To exercise the explicit Pause path, manually pre-seed `fake.store` with the current hash equal to status hash and only paused differing — document that the Pause branch is exercised when `LastAppliedSpecHash` already matches.
- `"deletes the schedule when AllowDeletion is true and the CR is deleted"`: set `Spec.AllowDeletion = true`, create + reconcile, then `k8sClient.Delete`, reconcile, assert `fake.deleted` contains the name and the finalizer is gone.
- `"does not delete when AllowDeletion is false"`: same but assert `fake.deleted` is empty while the finalizer is still removed.
- `"sets ClusterNotReady when the cluster is not ready"`: create a cluster WITHOUT marking ready; reconcile; assert Ready condition is False with reason `ClusterNotReady` and `fake.created` is empty.

(Write the bodies following the first test's structure; assert via `k8sClient.Get` + `meta.IsStatusConditionTrue` / reason checks.)

- [ ] **Step 5: Run all controller tests**

Run: `make test`
Expected: all pass.

- [ ] **Step 6: Regenerate RBAC + commit**

```bash
make manifests
git add internal/controller/temporalschedule_controller.go internal/controller/temporalschedule_controller_test.go config/rbac/role.yaml
git commit -s -m "feat(controller): reconcile TemporalSchedule"
```

---

## Task 5: Validating webhook

Typed generic validator (controller-runtime v0.23). Validates required action fields, at least one time source (unless paused), enum values, the backoff coefficient parses, JSON args, and immutable `scheduleID` on update.

**Files:**
- Create: `internal/webhook/v1alpha1/temporalschedule_webhook.go`
- Create: `internal/webhook/v1alpha1/temporalschedule_webhook_test.go`
- Modify: `internal/webhook/v1alpha1/webhook_suite_test.go` (register for envtest)

- [ ] **Step 1: Write the validator**

Create `internal/webhook/v1alpha1/temporalschedule_webhook.go` with the Apache-2.0 header, then:

```go
package v1alpha1

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

var temporalschedulelog = logf.Log.WithName("temporalschedule-resource")

// SetupTemporalScheduleWebhookWithManager registers the webhook for TemporalSchedule.
func SetupTemporalScheduleWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &temporalv1alpha1.TemporalSchedule{}).
		WithValidator(&TemporalScheduleCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-temporal-bmor10-com-v1alpha1-temporalschedule,mutating=false,failurePolicy=fail,sideEffects=None,groups=temporal.bmor10.com,resources=temporalschedules,verbs=create;update,versions=v1alpha1,name=vtemporalschedule-v1alpha1.kb.io,admissionReviewVersions=v1

// TemporalScheduleCustomValidator validates TemporalSchedule resources.
type TemporalScheduleCustomValidator struct{}

var _ admission.Validator[*temporalv1alpha1.TemporalSchedule] = &TemporalScheduleCustomValidator{}

func (v *TemporalScheduleCustomValidator) ValidateCreate(_ context.Context, sched *temporalv1alpha1.TemporalSchedule) (admission.Warnings, error) {
	temporalschedulelog.Info("Validation for TemporalSchedule upon creation", "name", sched.GetName())
	return nil, validateSchedule(sched)
}

func (v *TemporalScheduleCustomValidator) ValidateUpdate(_ context.Context, oldSched, newSched *temporalv1alpha1.TemporalSchedule) (admission.Warnings, error) {
	temporalschedulelog.Info("Validation for TemporalSchedule upon update", "name", newSched.GetName())
	if resolvedScheduleID(oldSched) != resolvedScheduleID(newSched) {
		return nil, fmt.Errorf("spec.scheduleID is immutable (was %q)", resolvedScheduleID(oldSched))
	}
	return nil, validateSchedule(newSched)
}

func (v *TemporalScheduleCustomValidator) ValidateDelete(_ context.Context, _ *temporalv1alpha1.TemporalSchedule) (admission.Warnings, error) {
	return nil, nil
}

var validOverlapPolicies = map[string]struct{}{
	"": {}, "Skip": {}, "BufferOne": {}, "BufferAll": {},
	"CancelOther": {}, "TerminateOther": {}, "AllowAll": {},
}

var validReusePolicies = map[string]struct{}{
	"": {}, "AllowDuplicate": {}, "AllowDuplicateFailedOnly": {},
	"RejectDuplicate": {}, "TerminateIfRunning": {},
}

func resolvedScheduleID(sched *temporalv1alpha1.TemporalSchedule) string {
	if sched.Spec.ScheduleID != "" {
		return sched.Spec.ScheduleID
	}
	return sched.Name
}

func validateSchedule(sched *temporalv1alpha1.TemporalSchedule) error {
	if sched.Spec.ClusterRef.Name == "" {
		return fmt.Errorf("spec.clusterRef.name must not be empty")
	}
	if sched.Spec.Namespace == "" {
		return fmt.Errorf("spec.namespace must not be empty")
	}
	a := sched.Spec.Action.StartWorkflow
	if a.WorkflowType == "" {
		return fmt.Errorf("spec.action.startWorkflow.workflowType must not be empty")
	}
	if a.TaskQueue == "" {
		return fmt.Errorf("spec.action.startWorkflow.taskQueue must not be empty")
	}

	paused := sched.Spec.State != nil && sched.Spec.State.Paused
	sp := sched.Spec.Schedule
	hasTime := len(sp.Calendars) > 0 || len(sp.Intervals) > 0 || len(sp.StructuredCalendar) > 0
	if !hasTime && !paused {
		return fmt.Errorf("spec.schedule must specify at least one of calendars, intervals, or structuredCalendar (unless spec.state.paused is true)")
	}

	if sched.Spec.Policies != nil {
		if _, ok := validOverlapPolicies[sched.Spec.Policies.OverlapPolicy]; !ok {
			return fmt.Errorf("spec.policies.overlapPolicy %q is not valid", sched.Spec.Policies.OverlapPolicy)
		}
	}
	if _, ok := validReusePolicies[a.WorkflowIDReusePolicy]; !ok {
		return fmt.Errorf("spec.action.startWorkflow.workflowIDReusePolicy %q is not valid", a.WorkflowIDReusePolicy)
	}
	if a.RetryPolicy != nil && a.RetryPolicy.BackoffCoefficient != "" {
		f, err := strconv.ParseFloat(a.RetryPolicy.BackoffCoefficient, 64)
		if err != nil || f < 1 {
			return fmt.Errorf("spec.action.startWorkflow.retryPolicy.backoffCoefficient must be a number >= 1")
		}
	}
	for i, raw := range a.Args {
		if len(raw.Raw) > 0 && !json.Valid(raw.Raw) {
			return fmt.Errorf("spec.action.startWorkflow.args[%d] is not valid JSON", i)
		}
	}
	for k, raw := range a.Memo {
		if len(raw.Raw) > 0 && !json.Valid(raw.Raw) {
			return fmt.Errorf("spec.action.startWorkflow.memo[%q] is not valid JSON", k)
		}
	}
	for k, raw := range a.SearchAttributes {
		if len(raw.Raw) > 0 && !json.Valid(raw.Raw) {
			return fmt.Errorf("spec.action.startWorkflow.searchAttributes[%q] is not valid JSON", k)
		}
	}
	return nil
}
```

- [ ] **Step 2: Register in the webhook envtest suite**

In `internal/webhook/v1alpha1/webhook_suite_test.go`, after the `SetupTemporalSearchAttributeWebhookWithManager(mgr)` block (~line 118), add:

```go
	err = SetupTemporalScheduleWebhookWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())
```

- [ ] **Step 3: Write the failing validator test**

Create `internal/webhook/v1alpha1/temporalschedule_webhook_test.go` with the Apache-2.0 header, then:

```go
package v1alpha1

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

var _ = Describe("TemporalSchedule Webhook", func() {
	var validator TemporalScheduleCustomValidator
	ctx := context.Background()

	valid := func() *temporalv1alpha1.TemporalSchedule {
		return &temporalv1alpha1.TemporalSchedule{
			ObjectMeta: metav1Object("sched-1"),
			Spec: temporalv1alpha1.TemporalScheduleSpec{
				ClusterRef: corev1.LocalObjectReference{Name: "cluster"},
				Namespace:  "orders",
				Schedule:   temporalv1alpha1.ScheduleSpec{Calendars: []string{"0 9 * * *"}},
				Action: temporalv1alpha1.ScheduleActionSpec{
					StartWorkflow: temporalv1alpha1.StartWorkflowAction{
						WorkflowType: "W", TaskQueue: "tq",
					},
				},
			},
		}
	}

	BeforeEach(func() { validator = TemporalScheduleCustomValidator{} })

	It("admits a valid schedule", func() {
		Expect(validator.ValidateCreate(ctx, valid())).Error().NotTo(HaveOccurred())
	})

	It("rejects a missing workflowType", func() {
		s := valid()
		s.Spec.Action.StartWorkflow.WorkflowType = ""
		Expect(validator.ValidateCreate(ctx, s)).Error().To(HaveOccurred())
	})

	It("rejects no time source when not paused", func() {
		s := valid()
		s.Spec.Schedule = temporalv1alpha1.ScheduleSpec{}
		Expect(validator.ValidateCreate(ctx, s)).Error().To(HaveOccurred())
	})

	It("admits no time source when paused", func() {
		s := valid()
		s.Spec.Schedule = temporalv1alpha1.ScheduleSpec{}
		s.Spec.State = &temporalv1alpha1.ScheduleStateSpec{Paused: true}
		Expect(validator.ValidateCreate(ctx, s)).Error().NotTo(HaveOccurred())
	})

	It("rejects invalid overlap policy", func() {
		s := valid()
		s.Spec.Policies = &temporalv1alpha1.SchedulePoliciesSpec{OverlapPolicy: "Nope"}
		Expect(validator.ValidateCreate(ctx, s)).Error().To(HaveOccurred())
	})

	It("rejects invalid JSON args", func() {
		s := valid()
		s.Spec.Action.StartWorkflow.Args = []runtime.RawExtension{{Raw: []byte(`{bad`)}}
		Expect(validator.ValidateCreate(ctx, s)).Error().To(HaveOccurred())
	})

	It("rejects changing scheduleID", func() {
		oldS := valid()
		oldS.Spec.ScheduleID = "a"
		newS := valid()
		newS.Spec.ScheduleID = "b"
		Expect(validator.ValidateUpdate(ctx, oldS, newS)).Error().To(HaveOccurred())
	})
})
```

NOTE: replace `metav1Object("sched-1")` with an inline `metav1.ObjectMeta{Name: "sched-1"}` and add the `metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"` import — there is no `metav1Object` helper. (Shown abbreviated for readability.)

- [ ] **Step 4: Run webhook tests**

Run: `go test ./internal/webhook/... -v`
Expected: all new specs pass.

- [ ] **Step 5: Commit**

```bash
git add internal/webhook/v1alpha1/temporalschedule_webhook.go internal/webhook/v1alpha1/temporalschedule_webhook_test.go internal/webhook/v1alpha1/webhook_suite_test.go
git commit -s -m "feat(webhook): validate TemporalSchedule"
```

---

## Task 6: Wiring, samples, generated artifacts, final verification

Wires the reconciler + webhook into the manager, adds CRD/sample kustomize entries and a sample CR, then regenerates all committed artifacts and runs the full verification.

**Files:**
- Modify: `cmd/main.go`
- Modify: `config/crd/kustomization.yaml`, `config/samples/kustomization.yaml`
- Create: `config/samples/temporal_v1alpha1_temporalschedule.yaml`
- Modify (generated): `config/webhook/manifests.yaml`, `dist/install.yaml`, `dist/chart/**`

- [ ] **Step 1: Register the reconciler in `cmd/main.go`**

After the `TemporalSearchAttributeReconciler` block (ends ~line 213, before `webhooksEnabled :=`), add:

```go
	if err := (&controller.TemporalScheduleReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TemporalSchedule")
		os.Exit(1)
	}
```

- [ ] **Step 2: Register the webhook in `cmd/main.go`**

After the `SetupTemporalSearchAttributeWebhookWithManager` block (ends ~line 232, before `// +kubebuilder:scaffold:builder`), add:

```go
	if webhooksEnabled {
		if err := webhookv1alpha1.SetupTemporalScheduleWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "TemporalSchedule")
			os.Exit(1)
		}
	}
```

- [ ] **Step 3: Add CRD + sample kustomize entries**

In `config/crd/kustomization.yaml`, add under `resources:` (before the `# +kubebuilder:scaffold:crdkustomizeresource` marker):

```yaml
- bases/temporal.bmor10.com_temporalschedules.yaml
```

In `config/samples/kustomization.yaml`, add under `resources:` (before the `# +kubebuilder:scaffold:manifestskustomizesamples` marker):

```yaml
- temporal_v1alpha1_temporalschedule.yaml
```

- [ ] **Step 4: Create the sample CR**

Create `config/samples/temporal_v1alpha1_temporalschedule.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalSchedule
metadata:
  name: nightly-report
spec:
  clusterRef:
    name: temporal-sample
  namespace: orders
  schedule:
    calendars:
      - "0 9 * * MON-FRI"
    timezoneName: "America/New_York"
  action:
    startWorkflow:
      workflowType: GenerateReport
      taskQueue: reports
      args:
        - { "format": "pdf", "limit": 100 }
  policies:
    overlapPolicy: Skip
    catchupWindow: 1h
  state:
    paused: false
```

- [ ] **Step 5: Regenerate all artifacts**

Run:
```bash
make generate manifests
make build-installer
make helm-chart
```
Expected: regenerates deepcopy, CRD bases, RBAC role, webhook manifests, `dist/install.yaml`, and the Helm chart; `helm lint` passes. The new CRD and the `vtemporalschedule` webhook appear in `config/webhook/manifests.yaml` and `dist/install.yaml`.

- [ ] **Step 6: Full verification**

Run:
```bash
make build
make test
make lint
GOOS=js GOARCH=wasm go build ./api/...
```
Expected: all succeed. Address any failures before committing.

- [ ] **Step 7: Commit**

```bash
git add cmd/main.go config/ dist/
git commit -s -m "feat: wire up TemporalSchedule controller, webhook, samples, and manifests"
```

---

## Final Self-Check (run after all tasks)

- [ ] `make build test lint` all green.
- [ ] `git status` clean after `make generate manifests build-installer helm-chart` (no uncommitted generated drift).
- [ ] A `kubectl apply` of the sample CR (against a kind/nsc cluster with a ready TemporalCluster + the `orders` TemporalNamespace) creates a schedule visible via `tctl schedule list` / Temporal UI. (Manual smoke test; optional.)
- [ ] Spec sections all covered: API types (Task 1), client + mapping (Tasks 2-3), reconciler with A+C drift (Task 4), webhook (Task 5), wiring/RBAC/samples/dist (Task 6).

## Notes for the implementer

- **Spec hash & `remainingActions`:** the hash is computed over `temporal.ScheduleParams` (spec-derived). Because drift is hash-based and never compared to the live server value, the server-side decrement of `remainingActions` does not cause an update loop. A spec change re-pushes the full schedule (resetting `remainingActions` to the spec value) — this is intended and documented behavior.
- **Pause branch coverage:** the explicit `Pause`/`Unpause` path only runs when `LastAppliedSpecHash` already matches the current spec hash (i.e. only `paused` changed externally, or the controller restarted with a matching hash). Since changing `spec.state.paused` also changes the hash, most pause changes flow through `Update`. Both are correct; the `reconcilePause` branch primarily reconciles external pause drift.
- **`backoffCoefficient` default:** when `retryPolicy` is set but `backoffCoefficient` is empty, the controller defaults it to `2.0` (Temporal's conventional default). The webhook only rejects non-empty values that don't parse or are `< 1`.
- **Identity:** all mutating schedule RPCs send `identity = "temporal-operator"` so actions are attributable in Temporal history.
