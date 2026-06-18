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

package proxy

import "testing"

func TestClassify(t *testing.T) {
	cases := map[string]MethodClass{
		"/temporal.api.workflowservice.v1.WorkflowService/StartWorkflowExecution":           ClassStart,
		"/temporal.api.workflowservice.v1.WorkflowService/SignalWithStartWorkflowExecution": ClassStart,
		"/temporal.api.workflowservice.v1.WorkflowService/SignalWorkflowExecution":          ClassExisting,
		"/temporal.api.workflowservice.v1.WorkflowService/QueryWorkflow":                    ClassExisting,
		"/temporal.api.workflowservice.v1.WorkflowService/DescribeWorkflowExecution":        ClassExisting,
		"/temporal.api.workflowservice.v1.WorkflowService/TerminateWorkflowExecution":       ClassExisting,
		"/temporal.api.workflowservice.v1.WorkflowService/RequestCancelWorkflowExecution":   ClassExisting,
		"/temporal.api.workflowservice.v1.WorkflowService/GetWorkflowExecutionHistory":      ClassExisting,
		"/temporal.api.workflowservice.v1.WorkflowService/PollWorkflowTaskQueue":            ClassPoll,
		"/temporal.api.workflowservice.v1.WorkflowService/PollActivityTaskQueue":            ClassPoll,
		"/temporal.api.workflowservice.v1.WorkflowService/RegisterNamespace":                ClassPassthrough,
		"/grpc.health.v1.Health/Check":                                                      ClassPassthrough,
	}
	for method, want := range cases {
		if got := Classify(method); got != want {
			t.Errorf("Classify(%q) = %v, want %v", method, got, want)
		}
	}
}
