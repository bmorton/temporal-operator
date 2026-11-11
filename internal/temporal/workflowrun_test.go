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

	enumspb "go.temporal.io/api/enums/v1"
	failurepb "go.temporal.io/api/failure/v1"
	historypb "go.temporal.io/api/history/v1"
)

func TestPhaseFromStatus(t *testing.T) {
	cases := map[enumspb.WorkflowExecutionStatus]struct {
		phase    string
		terminal bool
	}{
		enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED:      {"Pending", false},
		enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:          {"Running", false},
		enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:        {"Completed", true},
		enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:           {"Failed", true},
		enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:         {"Canceled", true},
		enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:       {"Terminated", true},
		enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW: {"ContinuedAsNew", true},
		enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:        {"TimedOut", true},
	}
	for st, want := range cases {
		if got := PhaseFromStatus(st); got != want.phase {
			t.Errorf("PhaseFromStatus(%v) = %q, want %q", st, got, want.phase)
		}
		if got := IsTerminalStatus(st); got != want.terminal {
			t.Errorf("IsTerminalStatus(%v) = %v, want %v", st, got, want.terminal)
		}
	}
}

func TestWorkflowFailureFromFailedEvent(t *testing.T) {
	ev := &historypb.HistoryEvent{
		Attributes: &historypb.HistoryEvent_WorkflowExecutionFailedEventAttributes{
			WorkflowExecutionFailedEventAttributes: &historypb.WorkflowExecutionFailedEventAttributes{
				Failure: &failurepb.Failure{
					Message: "boom",
					FailureInfo: &failurepb.Failure_ApplicationFailureInfo{
						ApplicationFailureInfo: &failurepb.ApplicationFailureInfo{Type: "MyError"},
					},
				},
			},
		},
	}
	f := workflowFailureFromEvent(ev)
	if f == nil || f.Message != "boom" || f.Type != "MyError" {
		t.Fatalf("unexpected failure: %+v", f)
	}
}

func TestWorkflowFailureFromTerminatedEvent(t *testing.T) {
	ev := &historypb.HistoryEvent{
		Attributes: &historypb.HistoryEvent_WorkflowExecutionTerminatedEventAttributes{
			WorkflowExecutionTerminatedEventAttributes: &historypb.WorkflowExecutionTerminatedEventAttributes{Reason: "stopped"},
		},
	}
	f := workflowFailureFromEvent(ev)
	if f == nil || f.Message != "stopped" {
		t.Fatalf("unexpected failure: %+v", f)
	}
}
