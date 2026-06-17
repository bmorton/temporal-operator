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
