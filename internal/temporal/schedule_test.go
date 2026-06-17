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

func ptr[T any](v T) *T {
	return &v
}

func TestToProtoSchedule_UnknownReusePolicy(t *testing.T) {
	p := baseParams()
	p.Action.IDReusePolicy = "Bogus"
	if _, err := toProtoSchedule(p); err == nil {
		t.Fatal("expected error for unknown workflow id reuse policy")
	}
}

func TestToProtoSchedule_RetryPolicyMapping(t *testing.T) {
	p := baseParams()
	p.Action.Retry = &RetryParams{
		InitialInterval:        ptr(time.Second),
		BackoffCoefficient:     2.5,
		MaximumInterval:        ptr(time.Minute),
		MaximumAttempts:        5,
		NonRetryableErrorTypes: []string{"BadInput"},
	}

	got, err := toProtoSchedule(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	retry := got.GetAction().GetStartWorkflow().GetRetryPolicy()
	if retry == nil {
		t.Fatal("retry policy = nil, want non-nil")
	}
	if retry.GetBackoffCoefficient() != 2.5 {
		t.Errorf("backoff coefficient = %v, want 2.5", retry.GetBackoffCoefficient())
	}
	if retry.GetMaximumAttempts() != 5 {
		t.Errorf("maximum attempts = %d, want 5", retry.GetMaximumAttempts())
	}
	if retry.GetInitialInterval().AsDuration() != time.Second {
		t.Errorf("initial interval = %v, want %v", retry.GetInitialInterval().AsDuration(), time.Second)
	}
	if retry.GetMaximumInterval().AsDuration() != time.Minute {
		t.Errorf("maximum interval = %v, want %v", retry.GetMaximumInterval().AsDuration(), time.Minute)
	}
	gotTypes := retry.GetNonRetryableErrorTypes()
	if len(gotTypes) != 1 || gotTypes[0] != "BadInput" {
		t.Errorf("non-retryable error types = %v, want [BadInput]", gotTypes)
	}
}

func TestToProtoSchedule_MemoAndSearchAttributesEncoding(t *testing.T) {
	p := baseParams()
	p.Action.Memo = map[string][]byte{"team": []byte(`"payments"`)}
	p.Action.SearchAttributes = map[string][]byte{"CustomKeyword": []byte(`"abc"`)}

	got, err := toProtoSchedule(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wf := got.GetAction().GetStartWorkflow()
	memoField := wf.GetMemo().GetFields()["team"]
	if memoField == nil {
		t.Fatal("memo field team = nil, want non-nil")
	}
	if string(memoField.GetMetadata()["encoding"]) != jsonPlainEncoding {
		t.Errorf("memo encoding = %q, want %q", memoField.GetMetadata()["encoding"], jsonPlainEncoding)
	}
	if string(memoField.GetData()) != `"payments"` {
		t.Errorf("memo data = %q, want %q", memoField.GetData(), `"payments"`)
	}

	searchAttr := wf.GetSearchAttributes().GetIndexedFields()["CustomKeyword"]
	if searchAttr == nil {
		t.Fatal("search attribute CustomKeyword = nil, want non-nil")
	}
	if string(searchAttr.GetMetadata()["encoding"]) != jsonPlainEncoding {
		t.Errorf("search attribute encoding = %q, want %q", searchAttr.GetMetadata()["encoding"], jsonPlainEncoding)
	}
	if string(searchAttr.GetData()) != `"abc"` {
		t.Errorf("search attribute data = %q, want %q", searchAttr.GetData(), `"abc"`)
	}
}

func TestToProtoSchedule_EmptyMemoAndSearchAttributesStayNil(t *testing.T) {
	got, err := toProtoSchedule(baseParams())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wf := got.GetAction().GetStartWorkflow()
	if wf.GetMemo() != nil {
		t.Errorf("memo = %v, want nil", wf.GetMemo())
	}
	if wf.GetSearchAttributes() != nil {
		t.Errorf("search attributes = %v, want nil", wf.GetSearchAttributes())
	}
}
