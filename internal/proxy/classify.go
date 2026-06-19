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

import "strings"

// MethodClass is the routing category of a gRPC method.
type MethodClass int

const (
	// ClassPassthrough routes to the active default backend with no fallback
	// (cluster-wide reads/writes, namespace ops, health, OperatorService).
	ClassPassthrough MethodClass = iota
	// ClassStart begins a new workflow and routes to the target on cutover.
	ClassStart
	// ClassExisting operates on an existing workflow: try target, fall back to source.
	ClassExisting
	// ClassPoll is a worker task-queue poll: routes to the source for the alpha.
	ClassPoll
)

// startMethods begin a new workflow execution.
var startMethods = map[string]struct{}{
	"StartWorkflowExecution":           {},
	"SignalWithStartWorkflowExecution": {},
	"ExecuteMultiOperation":            {},
}

// existingMethods operate on an already-running workflow execution.
var existingMethods = map[string]struct{}{
	"SignalWorkflowExecution":            {},
	"QueryWorkflow":                      {},
	"DescribeWorkflowExecution":          {},
	"TerminateWorkflowExecution":         {},
	"RequestCancelWorkflowExecution":     {},
	"ResetWorkflowExecution":             {},
	"GetWorkflowExecutionHistory":        {},
	"GetWorkflowExecutionHistoryReverse": {},
	"UpdateWorkflowExecution":            {},
	"PollWorkflowExecutionUpdate":        {},
}

// pollMethods are worker long-poll task-queue reads.
var pollMethods = map[string]struct{}{
	"PollWorkflowTaskQueue": {},
	"PollActivityTaskQueue": {},
	"PollNexusTaskQueue":    {},
}

// Classify maps a gRPC full-method name to its routing class. Only the
// WorkflowService is specially handled; everything else is passthrough.
func Classify(fullMethod string) MethodClass {
	const wf = "/temporal.api.workflowservice.v1.WorkflowService/"
	if !strings.HasPrefix(fullMethod, wf) {
		return ClassPassthrough
	}
	name := strings.TrimPrefix(fullMethod, wf)
	if _, ok := startMethods[name]; ok {
		return ClassStart
	}
	if _, ok := existingMethods[name]; ok {
		return ClassExisting
	}
	if _, ok := pollMethods[name]; ok {
		return ClassPoll
	}
	return ClassPassthrough
}
