/*
Copyright 2026 Brian Morton.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package temporal

import (
	"errors"
	"fmt"
	"time"

	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	schedulepb "go.temporal.io/api/schedule/v1"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	workflowpb "go.temporal.io/api/workflow/v1"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	//nolint:staticcheck // Temporal still exposes this legacy reuse-policy value.
	"TerminateIfRunning": enumspb.WORKFLOW_ID_REUSE_POLICY_TERMINATE_IF_RUNNING,
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
