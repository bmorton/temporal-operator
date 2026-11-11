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
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	commonpb "go.temporal.io/api/common/v1"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	taskqueuepb "go.temporal.io/api/taskqueue/v1"
	workflowservice "go.temporal.io/api/workflowservice/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const workflowRunIdentity = "temporal-operator"

// ErrWorkflowNotFound is returned by Describe when the execution does not exist.
var ErrWorkflowNotFound = errors.New("workflow execution not found")

// WorkflowFailure is the failure detail for a non-success terminal workflow.
type WorkflowFailure struct {
	Message string
	Type    string
}

// WorkflowExecutionInfo is the observed state of a workflow execution.
type WorkflowExecutionInfo struct {
	Status        enumspb.WorkflowExecutionStatus
	RunID         string
	WorkflowType  string
	TaskQueue     string
	StartTime     *time.Time
	CloseTime     *time.Time
	HistoryLength int64
	Failure       *WorkflowFailure
}

// WorkflowRunClient starts and observes one-off workflow executions.
type WorkflowRunClient interface {
	// Start starts the workflow and returns its runID. requestID makes Start
	// idempotent: a retried call with the same requestID is de-duplicated by
	// Temporal. On AlreadyExists it resolves and returns the open run's runID.
	Start(ctx context.Context, namespace, requestID string, params StartWorkflowParams) (string, error)
	// Describe returns the execution state. For non-success terminal states it
	// reads the close event to populate Failure. runID may be empty to address
	// the latest run.
	Describe(ctx context.Context, namespace, workflowID, runID string) (*WorkflowExecutionInfo, error)
	Cancel(ctx context.Context, namespace, workflowID, runID, reason string) error
	Terminate(ctx context.Context, namespace, workflowID, runID, reason string) error
	Close() error
}

// WorkflowRunClientFactory builds a WorkflowRunClient. nil tlsConfig = insecure.
type WorkflowRunClientFactory func(ctx context.Context, address string, tlsConfig *tls.Config) (WorkflowRunClient, error)

type grpcWorkflowRunClient struct {
	conn     *grpc.ClientConn
	workflow workflowservice.WorkflowServiceClient
}

// NewWorkflowRunClient dials the frontend and returns a WorkflowRunClient.
func NewWorkflowRunClient(_ context.Context, address string, tlsConfig *tls.Config) (WorkflowRunClient, error) {
	creds := insecure.NewCredentials()
	if tlsConfig != nil {
		creds = credentials.NewTLS(tlsConfig)
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, err
	}
	return &grpcWorkflowRunClient{conn: conn, workflow: workflowservice.NewWorkflowServiceClient(conn)}, nil
}

func (c *grpcWorkflowRunClient) Start(ctx context.Context, namespace, requestID string, p StartWorkflowParams) (string, error) {
	reuse, ok := reusePolicies[p.IDReusePolicy]
	if !ok {
		return "", fmt.Errorf("unknown workflow id reuse policy %q", p.IDReusePolicy)
	}
	req := &workflowservice.StartWorkflowExecutionRequest{
		Namespace:                namespace,
		WorkflowId:               p.WorkflowID,
		WorkflowType:             &commonpb.WorkflowType{Name: p.WorkflowType},
		TaskQueue:                &taskqueuepb.TaskQueue{Name: p.TaskQueue, Kind: enumspb.TASK_QUEUE_KIND_NORMAL},
		Input:                    encodeJSONPayloads(p.Args),
		WorkflowExecutionTimeout: optDuration(p.ExecutionTimeout),
		WorkflowRunTimeout:       optDuration(p.RunTimeout),
		WorkflowTaskTimeout:      optDuration(p.TaskTimeout),
		Identity:                 workflowRunIdentity,
		RequestId:                requestID,
		WorkflowIdReusePolicy:    reuse,
	}
	if m := encodeJSONFields(p.Memo); m != nil {
		req.Memo = &commonpb.Memo{Fields: m}
	}
	if sa := encodeJSONFields(p.SearchAttributes); sa != nil {
		req.SearchAttributes = &commonpb.SearchAttributes{IndexedFields: sa}
	}
	if r := p.Retry; r != nil {
		req.RetryPolicy = &commonpb.RetryPolicy{
			InitialInterval:        optDuration(r.InitialInterval),
			BackoffCoefficient:     r.BackoffCoefficient,
			MaximumInterval:        optDuration(r.MaximumInterval),
			MaximumAttempts:        r.MaximumAttempts,
			NonRetryableErrorTypes: r.NonRetryableErrorTypes,
		}
	}
	resp, err := c.workflow.StartWorkflowExecution(ctx, req)
	if err != nil {
		if status.Code(err) == codes.AlreadyExists {
			info, derr := c.Describe(ctx, namespace, p.WorkflowID, "")
			if derr != nil {
				return "", derr
			}
			return info.RunID, nil
		}
		return "", err
	}
	return resp.GetRunId(), nil
}

func (c *grpcWorkflowRunClient) Describe(ctx context.Context, namespace, workflowID, runID string) (*WorkflowExecutionInfo, error) {
	resp, err := c.workflow.DescribeWorkflowExecution(ctx, &workflowservice.DescribeWorkflowExecutionRequest{
		Namespace: namespace,
		Execution: &commonpb.WorkflowExecution{WorkflowId: workflowID, RunId: runID},
	})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, ErrWorkflowNotFound
		}
		return nil, err
	}
	wi := resp.GetWorkflowExecutionInfo()
	info := &WorkflowExecutionInfo{
		Status:        wi.GetStatus(),
		RunID:         wi.GetExecution().GetRunId(),
		WorkflowType:  wi.GetType().GetName(),
		TaskQueue:     wi.GetTaskQueue(),
		HistoryLength: wi.GetHistoryLength(),
	}
	if wi.GetStartTime() != nil {
		t := wi.GetStartTime().AsTime()
		info.StartTime = &t
	}
	if wi.GetCloseTime() != nil {
		t := wi.GetCloseTime().AsTime()
		info.CloseTime = &t
	}
	if IsTerminalStatus(info.Status) && info.Status != enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED &&
		info.Status != enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED {
		info.Failure = c.closeFailure(ctx, namespace, workflowID, info.RunID)
	}
	return info, nil
}

// closeFailure reads the single close event and extracts a failure, best-effort.
func (c *grpcWorkflowRunClient) closeFailure(ctx context.Context, namespace, workflowID, runID string) *WorkflowFailure {
	resp, err := c.workflow.GetWorkflowExecutionHistory(ctx, &workflowservice.GetWorkflowExecutionHistoryRequest{
		Namespace:              namespace,
		Execution:              &commonpb.WorkflowExecution{WorkflowId: workflowID, RunId: runID},
		HistoryEventFilterType: enumspb.HISTORY_EVENT_FILTER_TYPE_CLOSE_EVENT,
	})
	if err != nil {
		return nil
	}
	for _, ev := range resp.GetHistory().GetEvents() {
		if f := workflowFailureFromEvent(ev); f != nil {
			return f
		}
	}
	return nil
}

func (c *grpcWorkflowRunClient) Cancel(ctx context.Context, namespace, workflowID, runID, reason string) error {
	_, err := c.workflow.RequestCancelWorkflowExecution(ctx, &workflowservice.RequestCancelWorkflowExecutionRequest{
		Namespace:         namespace,
		WorkflowExecution: &commonpb.WorkflowExecution{WorkflowId: workflowID, RunId: runID},
		Identity:          workflowRunIdentity,
		Reason:            reason,
	})
	code := status.Code(err)
	if err != nil && (code == codes.NotFound || code == codes.FailedPrecondition) {
		return ErrWorkflowNotFound
	}
	return err
}

func (c *grpcWorkflowRunClient) Terminate(ctx context.Context, namespace, workflowID, runID, reason string) error {
	_, err := c.workflow.TerminateWorkflowExecution(ctx, &workflowservice.TerminateWorkflowExecutionRequest{
		Namespace:         namespace,
		WorkflowExecution: &commonpb.WorkflowExecution{WorkflowId: workflowID, RunId: runID},
		Identity:          workflowRunIdentity,
		Reason:            reason,
	})
	code := status.Code(err)
	if err != nil && (code == codes.NotFound || code == codes.FailedPrecondition) {
		return ErrWorkflowNotFound
	}
	return err
}

func (c *grpcWorkflowRunClient) Close() error { return c.conn.Close() }

// PhaseFromStatus maps a Temporal execution status to a friendly phase string.
func PhaseFromStatus(s enumspb.WorkflowExecutionStatus) string {
	switch s {
	case enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		return "Running"
	case enumspb.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return "Completed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_FAILED:
		return "Failed"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CANCELED:
		return "Canceled"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TERMINATED:
		return "Terminated"
	case enumspb.WORKFLOW_EXECUTION_STATUS_CONTINUED_AS_NEW:
		return "ContinuedAsNew"
	case enumspb.WORKFLOW_EXECUTION_STATUS_TIMED_OUT:
		return "TimedOut"
	default:
		return "Pending"
	}
}

// IsTerminalStatus reports whether the status is a closed (terminal) state.
func IsTerminalStatus(s enumspb.WorkflowExecutionStatus) bool {
	switch s {
	case enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED, enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING:
		return false
	default:
		return true
	}
}

// workflowFailureFromEvent extracts failure detail from a close history event.
func workflowFailureFromEvent(ev *historypb.HistoryEvent) *WorkflowFailure {
	if a := ev.GetWorkflowExecutionFailedEventAttributes(); a != nil {
		f := a.GetFailure()
		return &WorkflowFailure{Message: f.GetMessage(), Type: f.GetApplicationFailureInfo().GetType()}
	}
	if a := ev.GetWorkflowExecutionTerminatedEventAttributes(); a != nil {
		return &WorkflowFailure{Message: a.GetReason(), Type: "Terminated"}
	}
	if a := ev.GetWorkflowExecutionTimedOutEventAttributes(); a != nil {
		return &WorkflowFailure{Message: "workflow execution timed out", Type: "TimedOut"}
	}
	return nil
}
