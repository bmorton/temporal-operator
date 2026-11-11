# TemporalWorkflowRun CRD Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `TemporalWorkflowRun` CRD that triggers one-off Temporal workflow executions declaratively (a `Job` analog), tracks the execution in status, supports TTL cleanup, and is gated by a closed-by-default cluster opt-in policy.

**Architecture:** A new namespaced CRD plus a satellite controller that reuses the existing `resolveTarget`/`clusterTLSConfig` (mTLS) path. The controller starts the workflow via a new raw-gRPC `WorkflowRunClient`, polls `DescribeWorkflowExecution` to keep status current, enforces a `WorkflowRunPolicy` declared on the target `TemporalCluster`/`TemporalDevServer`, and deletes the CR after `ttlSecondsAfterFinished`. A finalizer applies a `cancellationPolicy` to in-flight workflows on delete.

**Tech Stack:** Go, kubebuilder/controller-runtime v0.23 (typed webhooks), `go.temporal.io/api` raw gRPC clients, Ginkgo/Gomega + envtest, Chainsaw e2e.

## Global Constraints

- Module `github.com/bmorton/temporal-operator`; API group domain `temporal.bmor10.com`; copyright owner "Brian Morton". Every new `.go` file starts with the Apache 2.0 license header copied verbatim from a neighbouring file (e.g. `api/v1alpha1/temporalschedule_types.go` lines 1-15).
- Go 1.26.4 (per `go.mod`).
- Conventional Commits; **every** commit signed off: `git commit -s`.
- Never hand-edit `dist/chart` — regenerate with `make helm-chart`.
- After API changes run `make generate manifests`; after API doc-affecting changes run `make api-docs docs-crd-reference` and commit `docs/api/v1alpha1.md` + `docs/content/reference/_index.md`.
- controller-runtime v0.23 typed webhooks: implement `admission.Validator[*T]`, register via `ctrl.NewWebhookManagedBy(mgr, &T{}).WithValidator(...)`.
- Satellite controllers resolve `clusterRef` only within the CR's own Kubernetes namespace (security boundary — do not change this).
- Add the `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>` trailer to commit messages.

---

## File Structure

**Created:**
- `api/v1alpha1/temporalworkflowrun_types.go` — CRD spec/status/`WorkflowRunFailure`, plus the shared `WorkflowRunPolicy` type and kubebuilder markers.
- `internal/temporal/workflowrun.go` — `WorkflowRunClient` interface, factory, raw-gRPC impl, status→phase mapping, failure extraction.
- `internal/temporal/workflowrun_test.go` — pure mapping/encoding tests.
- `internal/controller/temporalworkflowrun_controller.go` — reconciler.
- `internal/controller/temporalworkflowrun_controller_test.go` — envtest/Ginkgo tests + fake client.
- `internal/webhook/v1alpha1/temporalworkflowrun_webhook.go` — validating webhook.
- `internal/webhook/v1alpha1/temporalworkflowrun_webhook_test.go` — webhook tests.
- `config/samples/temporal_v1alpha1_temporalworkflowrun.yaml` — sample CR.
- `test/e2e/workflowrun/{chainsaw-test.yaml,01-devserver.yaml,01-assert.yaml,02-namespace.yaml,02-assert.yaml,03-workflowrun.yaml,03-assert.yaml,05-deny.yaml,05-assert.yaml}` — e2e suite.

**Modified:**
- `api/v1alpha1/temporalcluster_types.go` — add `WorkflowRunPolicy *WorkflowRunPolicy` to `TemporalClusterSpec`.
- `api/v1alpha1/temporaldevserver_types.go` — add `WorkflowRunPolicy *WorkflowRunPolicy` to `TemporalDevServerSpec`.
- `api/v1alpha1/conditions.go` — add reason constants.
- `internal/controller/target.go` — surface effective `WorkflowRunPolicy` on `ResolvedTarget`.
- `internal/controller/target_test.go` — policy-default tests.
- `cmd/main.go` — register reconciler + webhook.
- `PROJECT` — add resource/webhook entries.
- `.github/workflows/e2e.yml` — add the `workflowrun` combo.
- Generated: `config/crd/bases/*`, `config/rbac/*`, `dist/chart/*`, `docs/api/v1alpha1.md`, `docs/content/reference/_index.md`, `api/v1alpha1/zz_generated.deepcopy.go`.

---

### Task 1: API types — `TemporalWorkflowRun` + `WorkflowRunPolicy`

**Files:**
- Create: `api/v1alpha1/temporalworkflowrun_types.go`
- Modify: `api/v1alpha1/temporalcluster_types.go` (add field to `TemporalClusterSpec`), `api/v1alpha1/temporaldevserver_types.go` (add field to `TemporalDevServerSpec`)
- Modify: `api/v1alpha1/conditions.go` (add reason constants)

**Interfaces:**
- Produces: types `TemporalWorkflowRun`, `TemporalWorkflowRunSpec`, `TemporalWorkflowRunStatus`, `WorkflowRunFailure`, `WorkflowRunPolicy`; constants `ReasonWorkflowRunNotPermitted`, `ReasonWorkflowFinished`, `ReasonWorkflowRunning`, `ReasonClusterNotFound`, `ReasonClusterNotReady`. `TemporalWorkflowRunSpec.Workflow` is of the **existing** type `StartWorkflowAction`.

- [ ] **Step 1: Create the types file.** Copy the license header (lines 1-15) from `api/v1alpha1/temporalschedule_types.go`, then add:

```go
package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkflowRunPolicy is a per-target opt-in gate controlling operator-initiated
// workflow runs. It is declared on a TemporalCluster or TemporalDevServer by the
// cluster owner. Defaults differ per kind and are applied by the controller's
// target resolver, not by kubebuilder defaults, so an absent policy is
// meaningful: disabled for TemporalCluster, enabled (no allowlist) for
// TemporalDevServer.
type WorkflowRunPolicy struct {
	// Enabled permits operator-initiated workflow runs against this target.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// AllowedNamespaces optionally restricts which Temporal namespaces runs may
	// target. Empty means any namespace is allowed.
	// +optional
	AllowedNamespaces []string `json:"allowedNamespaces,omitempty"`
	// AllowedTaskQueues optionally restricts which task queues runs may use.
	// Empty means any task queue is allowed.
	// +optional
	AllowedTaskQueues []string `json:"allowedTaskQueues,omitempty"`
}

// TemporalWorkflowRunSpec defines the desired state of a one-off workflow run.
type TemporalWorkflowRunSpec struct {
	// ClusterRef references the TemporalCluster or TemporalDevServer that runs
	// the workflow. Resolved in the same Kubernetes namespace as this CR.
	ClusterRef ClusterReference `json:"clusterRef"`

	// Namespace is the Temporal namespace to start the workflow in.
	Namespace string `json:"namespace"`

	// Workflow describes the one-off workflow to start. Immutable after create.
	Workflow StartWorkflowAction `json:"workflow"`

	// TTLSecondsAfterFinished, when set, deletes this CR that many seconds after
	// the workflow reaches a terminal state. Unset keeps the CR indefinitely.
	// +optional
	TTLSecondsAfterFinished *int32 `json:"ttlSecondsAfterFinished,omitempty"`

	// CancellationPolicy controls what happens to a still-running workflow when
	// this CR is deleted.
	// +kubebuilder:validation:Enum=Abandon;Cancel;Terminate
	// +kubebuilder:default=Abandon
	// +optional
	CancellationPolicy string `json:"cancellationPolicy,omitempty"`
}

// WorkflowRunFailure carries failure detail for non-success terminal states.
type WorkflowRunFailure struct {
	// +optional
	Message string `json:"message,omitempty"`
	// +optional
	Type string `json:"type,omitempty"`
}

// TemporalWorkflowRunStatus is the observed state of a workflow run.
type TemporalWorkflowRunStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Phase is a friendly lifecycle summary: Pending, Running, Completed,
	// Failed, Terminated, Canceled, TimedOut, or ContinuedAsNew.
	// +optional
	Phase string `json:"phase,omitempty"`
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

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=twr
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.clusterRef.name`
// +kubebuilder:printcolumn:name="Namespace",type=string,JSONPath=`.spec.namespace`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// TemporalWorkflowRun is the Schema for the temporalworkflowruns API.
type TemporalWorkflowRun struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`
	// +required
	Spec TemporalWorkflowRunSpec `json:"spec"`
	// +optional
	Status TemporalWorkflowRunStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TemporalWorkflowRunList contains a list of TemporalWorkflowRun.
type TemporalWorkflowRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TemporalWorkflowRun `json:"items"`
}

func init() {
	registerType(&TemporalWorkflowRun{}, &TemporalWorkflowRunList{})
}
```

- [ ] **Step 2: Add the policy field to both targets.** In `api/v1alpha1/temporalcluster_types.go`, inside `TemporalClusterSpec` (after the `PreventDeletion` field), add:

```go
	// WorkflowRunPolicy gates operator-initiated TemporalWorkflowRun executions
	// against this cluster. Absent means disabled (closed by default).
	// +optional
	WorkflowRunPolicy *WorkflowRunPolicy `json:"workflowRunPolicy,omitempty"`
```

In `api/v1alpha1/temporaldevserver_types.go`, inside `TemporalDevServerSpec` (after `Affinity`), add:

```go
	// WorkflowRunPolicy gates operator-initiated TemporalWorkflowRun executions
	// against this dev server. Absent means enabled with no allowlist.
	// +optional
	WorkflowRunPolicy *WorkflowRunPolicy `json:"workflowRunPolicy,omitempty"`
```

- [ ] **Step 3: Add reason constants.** In `api/v1alpha1/conditions.go`, append inside the reasons `const` block:

```go
	// ReasonWorkflowRunNotPermitted indicates the target's WorkflowRunPolicy denied the run.
	ReasonWorkflowRunNotPermitted = "WorkflowRunNotPermitted"
	// ReasonWorkflowRunning indicates the workflow is currently running.
	ReasonWorkflowRunning = "WorkflowRunning"
	// ReasonWorkflowFinished indicates the workflow reached a terminal state.
	ReasonWorkflowFinished = "WorkflowFinished"
	// ReasonClusterNotFound indicates the referenced Temporal target was not found.
	ReasonClusterNotFound = "ClusterNotFound"
	// ReasonClusterNotReady indicates the referenced Temporal target is not ready.
	ReasonClusterNotReady = "ClusterNotReady"
```

- [ ] **Step 4: Regenerate deepcopy + manifests.**

Run: `make generate manifests`
Expected: succeeds; `git status` shows new `config/crd/bases/temporal.bmor10.com_temporalworkflowruns.yaml`, updates to the cluster/devserver CRDs, and `zz_generated.deepcopy.go` changes.

- [ ] **Step 5: Build to confirm it compiles.**

Run: `go build ./api/...`
Expected: exit 0.

- [ ] **Step 6: Commit.**

```bash
git add api/ config/crd config/rbac
git commit -s -m "feat(api): add TemporalWorkflowRun CRD and WorkflowRunPolicy types"
```

---

### Task 2: Surface the effective `WorkflowRunPolicy` from `resolveTarget`

**Files:**
- Modify: `internal/controller/target.go`
- Test: `internal/controller/target_test.go`

**Interfaces:**
- Consumes: `temporalv1alpha1.WorkflowRunPolicy` (Task 1), existing `resolveTarget`/`ResolvedTarget`.
- Produces: `ResolvedTarget.WorkflowRunPolicy temporalv1alpha1.WorkflowRunPolicy` (effective, defaults applied); helper `effectiveWorkflowRunPolicy(kind string, p *temporalv1alpha1.WorkflowRunPolicy) temporalv1alpha1.WorkflowRunPolicy`.

- [ ] **Step 1: Write the failing test.** Append to `internal/controller/target_test.go` (create the file with the standard header + `package controller` and ginkgo imports if it does not yet exist; otherwise add a `Describe`):

```go
var _ = Describe("effectiveWorkflowRunPolicy", func() {
	It("defaults TemporalCluster to disabled when nil", func() {
		p := effectiveWorkflowRunPolicy(temporalv1alpha1.ClusterKindTemporalCluster, nil)
		Expect(p.Enabled).To(BeFalse())
	})
	It("defaults TemporalDevServer to enabled when nil", func() {
		p := effectiveWorkflowRunPolicy(temporalv1alpha1.ClusterKindTemporalDevServer, nil)
		Expect(p.Enabled).To(BeTrue())
		Expect(p.AllowedNamespaces).To(BeEmpty())
	})
	It("passes an explicit policy through unchanged", func() {
		in := &temporalv1alpha1.WorkflowRunPolicy{Enabled: true, AllowedTaskQueues: []string{"q"}}
		p := effectiveWorkflowRunPolicy(temporalv1alpha1.ClusterKindTemporalCluster, in)
		Expect(p.Enabled).To(BeTrue())
		Expect(p.AllowedTaskQueues).To(Equal([]string{"q"}))
	})
})
```

- [ ] **Step 2: Run it to verify it fails.**

Run: `go test ./internal/controller/ -run TestControllers -v` (the Ginkgo suite entrypoint)
Expected: compile error `undefined: effectiveWorkflowRunPolicy`.

- [ ] **Step 3: Implement.** In `internal/controller/target.go`, add the field to `ResolvedTarget`:

```go
	// WorkflowRunPolicy is the effective (defaults-applied) policy for
	// operator-initiated workflow runs against this target.
	WorkflowRunPolicy temporalv1alpha1.WorkflowRunPolicy
```

Add the helper and set the field in both branches of `resolveTarget`:

```go
// effectiveWorkflowRunPolicy applies per-kind defaults to a possibly-nil policy.
// A nil policy means disabled for TemporalCluster (closed by default) and
// enabled with no allowlist for TemporalDevServer (throwaway dev environments).
func effectiveWorkflowRunPolicy(kind string, p *temporalv1alpha1.WorkflowRunPolicy) temporalv1alpha1.WorkflowRunPolicy {
	if p != nil {
		return *p
	}
	switch kind {
	case temporalv1alpha1.ClusterKindTemporalDevServer:
		return temporalv1alpha1.WorkflowRunPolicy{Enabled: true}
	default:
		return temporalv1alpha1.WorkflowRunPolicy{Enabled: false}
	}
}
```

In the `ClusterKindTemporalCluster` branch set `WorkflowRunPolicy: effectiveWorkflowRunPolicy(temporalv1alpha1.ClusterKindTemporalCluster, cluster.Spec.WorkflowRunPolicy)` on the returned `ResolvedTarget`; in the `ClusterKindTemporalDevServer` branch set `WorkflowRunPolicy: effectiveWorkflowRunPolicy(temporalv1alpha1.ClusterKindTemporalDevServer, dev.Spec.WorkflowRunPolicy)`.

- [ ] **Step 4: Run tests to verify they pass.**

Run: `go test ./internal/controller/ -run TestControllers`
Expected: PASS.

- [ ] **Step 5: Commit.**

```bash
git add internal/controller/target.go internal/controller/target_test.go
git commit -s -m "feat(controller): surface effective WorkflowRunPolicy from resolveTarget"
```

---

### Task 3: `WorkflowRunClient` (raw-gRPC Temporal client)

**Files:**
- Create: `internal/temporal/workflowrun.go`
- Test: `internal/temporal/workflowrun_test.go`

**Interfaces:**
- Consumes (same `temporal` package, from `schedule.go`): `StartWorkflowParams`, `RetryParams`, `encodeJSONPayloads`, `encodeJSONFields`, `optDuration`, `reusePolicies`.
- Produces: `WorkflowRunClient` interface; `WorkflowRunClientFactory`; `NewWorkflowRunClient`; structs `WorkflowExecutionInfo`, `WorkflowFailure`; pure helpers `PhaseFromStatus(enumspb.WorkflowExecutionStatus) string`, `IsTerminalStatus(enumspb.WorkflowExecutionStatus) bool`, `workflowFailureFromEvent(*historypb.HistoryEvent) *WorkflowFailure`. Phase strings: `Pending|Running|Completed|Failed|Terminated|Canceled|TimedOut|ContinuedAsNew`.

- [ ] **Step 1: Write the failing test.** Create `internal/temporal/workflowrun_test.go` (license header + `package temporal`):

```go
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
```

- [ ] **Step 2: Run it to verify it fails.**

Run: `go test ./internal/temporal/ -run 'TestPhaseFromStatus|TestWorkflowFailure'`
Expected: compile error (undefined `PhaseFromStatus`, etc.).

- [ ] **Step 3: Implement the client.** Create `internal/temporal/workflowrun.go` (license header + `package temporal`):

```go
package temporal

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"

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
	if err != nil && status.Code(err) == codes.NotFound {
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
	if err != nil && status.Code(err) == codes.NotFound {
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
```

Add `"time"` to the import block (used by `WorkflowExecutionInfo`).

- [ ] **Step 4: Run tests to verify they pass.**

Run: `go test ./internal/temporal/ -run 'TestPhaseFromStatus|TestWorkflowFailure'`
Expected: PASS.

- [ ] **Step 5: Build the package.**

Run: `go build ./internal/temporal/`
Expected: exit 0. (If a proto getter name differs — e.g. `GetType()` on the failed event — fix per the compiler and the `go.temporal.io/api` version in `go.mod`.)

- [ ] **Step 6: Commit.**

```bash
git add internal/temporal/workflowrun.go internal/temporal/workflowrun_test.go
git commit -s -m "feat(temporal): add WorkflowRunClient for one-off workflow executions"
```

---

### Task 4: `TemporalWorkflowRun` controller

**Files:**
- Create: `internal/controller/temporalworkflowrun_controller.go`
- Test: `internal/controller/temporalworkflowrun_controller_test.go`

**Interfaces:**
- Consumes: `resolveTarget`, `ResolvedTarget.WorkflowRunPolicy` (Task 2), `temporal.WorkflowRunClient`/`WorkflowRunClientFactory`/`NewWorkflowRunClient`/`StartWorkflowParams`/`PhaseFromStatus`/`IsTerminalStatus`/`ErrTargetNotFound`, and the package-level helpers `durPtr`, `rawList`, `rawMap` (from `temporalschedule_controller.go`).
- Produces: `TemporalWorkflowRunReconciler{client.Client, Scheme, ClientFactory temporal.WorkflowRunClientFactory}` with `Reconcile` and `SetupWithManager`.

- [ ] **Step 1: Implement the controller.** Create `internal/controller/temporalworkflowrun_controller.go` (license header + `package controller`):

```go
package controller

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

const workflowRunFinalizer = "temporal.bmor10.com/workflowrun"

// workflowRunPollInterval is how often a running workflow's status is refreshed.
const workflowRunPollInterval = 10 * time.Second

// TemporalWorkflowRunReconciler reconciles TemporalWorkflowRun objects.
type TemporalWorkflowRunReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// ClientFactory builds the Temporal workflow-run client; injectable for tests.
	ClientFactory temporal.WorkflowRunClientFactory
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalworkflowruns,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalworkflowruns/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalworkflowruns/finalizers,verbs=update

// Reconcile starts a one-off workflow, tracks its status, and cleans up via TTL.
func (r *TemporalWorkflowRunReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var run temporalv1alpha1.TemporalWorkflowRun
	if err := r.Get(ctx, req.NamespacedName, &run); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	target, err := resolveTarget(ctx, r.Client, run.Namespace, run.Spec.ClusterRef)
	if err != nil {
		if !run.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.removeFinalizer(ctx, &run)
		}
		if err == ErrTargetNotFound {
			r.setReady(&run, metav1.ConditionFalse, temporalv1alpha1.ReasonClusterNotFound, "referenced Temporal target not found")
			return ctrl.Result{RequeueAfter: 15 * time.Second}, r.statusUpdate(ctx, &run)
		}
		return ctrl.Result{}, err
	}

	wc, err := r.clientFactory()(ctx, target.Address, target.TLSConfig)
	if err != nil {
		if !run.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.removeFinalizer(ctx, &run)
		}
		return ctrl.Result{}, fmt.Errorf("building temporal client: %w", err)
	}
	defer func() { _ = wc.Close() }()

	if !run.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, &run, wc)
	}

	if !controllerutil.ContainsFinalizer(&run, workflowRunFinalizer) {
		controllerutil.AddFinalizer(&run, workflowRunFinalizer)
		if err := r.Update(ctx, &run); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !target.Ready {
		r.setReady(&run, metav1.ConditionFalse, temporalv1alpha1.ReasonClusterNotReady, "waiting for the Temporal target to become ready")
		return ctrl.Result{RequeueAfter: 15 * time.Second}, r.statusUpdate(ctx, &run)
	}

	return r.reconcileRun(ctx, &run, wc, target.WorkflowRunPolicy)
}

func (r *TemporalWorkflowRunReconciler) reconcileRun(ctx context.Context, run *temporalv1alpha1.TemporalWorkflowRun, wc temporal.WorkflowRunClient, policy temporalv1alpha1.WorkflowRunPolicy) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	wfID := resolveWorkflowID(run)
	taskQueue := run.Spec.Workflow.TaskQueue

	// Start the workflow once.
	if run.Status.RunID == "" {
		if err := checkWorkflowRunPolicy(policy, run.Spec.Namespace, taskQueue); err != nil {
			r.setReady(run, metav1.ConditionFalse, temporalv1alpha1.ReasonWorkflowRunNotPermitted, err.Error())
			return ctrl.Result{}, r.statusUpdate(ctx, run)
		}
		params, err := workflowRunParams(run)
		if err != nil {
			r.setReady(run, metav1.ConditionFalse, "InvalidSpec", err.Error())
			return ctrl.Result{}, r.statusUpdate(ctx, run)
		}
		runID, err := wc.Start(ctx, run.Spec.Namespace, string(run.UID), params)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("starting workflow: %w", err)
		}
		log.Info("started workflow", "workflowID", wfID, "runID", runID)
		run.Status.WorkflowID = wfID
		run.Status.RunID = runID
		run.Status.WorkflowType = run.Spec.Workflow.WorkflowType
		run.Status.TaskQueue = taskQueue
		run.Status.Phase = "Running"
		r.setReady(run, metav1.ConditionTrue, temporalv1alpha1.ReasonWorkflowRunning, "workflow started")
	}

	// Refresh observed state.
	info, err := wc.Describe(ctx, run.Spec.Namespace, wfID, run.Status.RunID)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("describing workflow: %w", err)
	}
	run.Status.Phase = temporal.PhaseFromStatus(info.Status)
	run.Status.HistoryLength = info.HistoryLength
	if info.StartTime != nil {
		t := metav1.NewTime(*info.StartTime)
		run.Status.StartTime = &t
	}
	if info.CloseTime != nil {
		t := metav1.NewTime(*info.CloseTime)
		run.Status.CloseTime = &t
	}

	if !temporal.IsTerminalStatus(info.Status) {
		r.setReady(run, metav1.ConditionTrue, temporalv1alpha1.ReasonWorkflowRunning, "workflow is running")
		return ctrl.Result{RequeueAfter: workflowRunPollInterval}, r.statusUpdate(ctx, run)
	}

	// Terminal: record completion, failure, and Finished condition once.
	if run.Status.CompletionTime == nil {
		now := metav1.Now()
		run.Status.CompletionTime = &now
	}
	if info.Failure != nil {
		run.Status.Failure = &temporalv1alpha1.WorkflowRunFailure{Message: info.Failure.Message, Type: info.Failure.Type}
	}
	meta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
		Type: "Finished", Status: metav1.ConditionTrue,
		Reason: temporalv1alpha1.ReasonWorkflowFinished, Message: "workflow reached a terminal state: " + run.Status.Phase,
		ObservedGeneration: run.Generation,
	})
	r.setReady(run, metav1.ConditionTrue, temporalv1alpha1.ReasonWorkflowFinished, "workflow finished: "+run.Status.Phase)
	if err := r.statusUpdate(ctx, run); err != nil {
		return ctrl.Result{}, err
	}

	// TTL cleanup.
	if run.Spec.TTLSecondsAfterFinished != nil {
		deadline := run.Status.CompletionTime.Add(time.Duration(*run.Spec.TTLSecondsAfterFinished) * time.Second)
		if remaining := time.Until(deadline); remaining > 0 {
			return ctrl.Result{RequeueAfter: remaining}, nil
		}
		log.Info("deleting workflow run after TTL", "name", run.Name)
		return ctrl.Result{}, client.IgnoreNotFound(r.Delete(ctx, run))
	}
	return ctrl.Result{}, nil
}

func (r *TemporalWorkflowRunReconciler) reconcileDelete(ctx context.Context, run *temporalv1alpha1.TemporalWorkflowRun, wc temporal.WorkflowRunClient) error {
	log := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(run, workflowRunFinalizer) {
		return nil
	}
	// Apply the cancellation policy only if the workflow is still running.
	if run.Status.RunID != "" && run.Status.CompletionTime == nil {
		wfID := resolveWorkflowID(run)
		switch run.Spec.CancellationPolicy {
		case "Cancel":
			if err := wc.Cancel(ctx, run.Spec.Namespace, wfID, run.Status.RunID, "TemporalWorkflowRun deleted"); err != nil && err != temporal.ErrWorkflowNotFound {
				return fmt.Errorf("cancelling workflow: %w", err)
			}
			log.Info("requested workflow cancellation on delete", "workflowID", wfID)
		case "Terminate":
			if err := wc.Terminate(ctx, run.Spec.Namespace, wfID, run.Status.RunID, "TemporalWorkflowRun deleted"); err != nil && err != temporal.ErrWorkflowNotFound {
				return fmt.Errorf("terminating workflow: %w", err)
			}
			log.Info("terminated workflow on delete", "workflowID", wfID)
		}
	}
	return r.removeFinalizer(ctx, run)
}

func (r *TemporalWorkflowRunReconciler) removeFinalizer(ctx context.Context, run *temporalv1alpha1.TemporalWorkflowRun) error {
	if controllerutil.ContainsFinalizer(run, workflowRunFinalizer) {
		controllerutil.RemoveFinalizer(run, workflowRunFinalizer)
		if err := r.Update(ctx, run); err != nil {
			return err
		}
	}
	return nil
}

func (r *TemporalWorkflowRunReconciler) clientFactory() temporal.WorkflowRunClientFactory {
	if r.ClientFactory != nil {
		return r.ClientFactory
	}
	return temporal.NewWorkflowRunClient
}

func (r *TemporalWorkflowRunReconciler) setReady(run *temporalv1alpha1.TemporalWorkflowRun, status metav1.ConditionStatus, reason, message string) {
	run.Status.ObservedGeneration = run.Generation
	meta.SetStatusCondition(&run.Status.Conditions, metav1.Condition{
		Type: temporalv1alpha1.ConditionReady, Status: status,
		Reason: reason, Message: message, ObservedGeneration: run.Generation,
	})
}

func (r *TemporalWorkflowRunReconciler) statusUpdate(ctx context.Context, run *temporalv1alpha1.TemporalWorkflowRun) error {
	return client.IgnoreNotFound(r.Status().Update(ctx, run))
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalWorkflowRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalWorkflowRun{}).
		Named("temporalworkflowrun").
		Complete(r)
}

func resolveWorkflowID(run *temporalv1alpha1.TemporalWorkflowRun) string {
	if run.Spec.Workflow.WorkflowID != "" {
		return run.Spec.Workflow.WorkflowID
	}
	return run.Name
}

// checkWorkflowRunPolicy enforces the target's effective WorkflowRunPolicy.
func checkWorkflowRunPolicy(p temporalv1alpha1.WorkflowRunPolicy, namespace, taskQueue string) error {
	if !p.Enabled {
		return fmt.Errorf("workflow runs are not enabled on the referenced Temporal target")
	}
	if len(p.AllowedNamespaces) > 0 && !contains(p.AllowedNamespaces, namespace) {
		return fmt.Errorf("Temporal namespace %q is not in the target's allowedNamespaces", namespace)
	}
	if len(p.AllowedTaskQueues) > 0 && !contains(p.AllowedTaskQueues, taskQueue) {
		return fmt.Errorf("task queue %q is not in the target's allowedTaskQueues", taskQueue)
	}
	return nil
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// workflowRunParams maps a TemporalWorkflowRun's workflow spec to StartWorkflowParams.
func workflowRunParams(run *temporalv1alpha1.TemporalWorkflowRun) (temporal.StartWorkflowParams, error) {
	a := run.Spec.Workflow
	params := temporal.StartWorkflowParams{
		WorkflowType:     a.WorkflowType,
		TaskQueue:        a.TaskQueue,
		WorkflowID:       resolveWorkflowID(run),
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
				return temporal.StartWorkflowParams{}, fmt.Errorf("invalid retryPolicy.backoffCoefficient: %w", err)
			}
			backoff = f
		}
		params.Retry = &temporal.RetryParams{
			InitialInterval:        durPtr(a.RetryPolicy.InitialInterval),
			BackoffCoefficient:     backoff,
			MaximumInterval:        durPtr(a.RetryPolicy.MaximumInterval),
			MaximumAttempts:        a.RetryPolicy.MaximumAttempts,
			NonRetryableErrorTypes: a.RetryPolicy.NonRetryableErrorTypes,
		}
	}
	return params, nil
}
```

Note: `resolveTarget` returns the sentinel `ErrTargetNotFound`; the existing schedule controller compares with `errors.Is`. Using `err == ErrTargetNotFound` is acceptable here since it is returned directly, but prefer `errors.Is(err, ErrTargetNotFound)` and add `"errors"` to imports to match house style. Same for `temporal.ErrWorkflowNotFound`.

- [ ] **Step 2: Write the controller tests.** Create `internal/controller/temporalworkflowrun_controller_test.go` (license header + `package controller`), with a fake client and tests:

```go
package controller

import (
	"context"
	"crypto/tls"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	enumspb "go.temporal.io/api/enums/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// fakeWorkflowRunClient is an in-memory WorkflowRunClient. status drives Describe.
type fakeWorkflowRunClient struct {
	started              []string
	canceled, terminated []string
	status               enumspb.WorkflowExecutionStatus
	failure              *temporal.WorkflowFailure
}

func (f *fakeWorkflowRunClient) Start(_ context.Context, _, _ string, p temporal.StartWorkflowParams) (string, error) {
	f.started = append(f.started, p.WorkflowID)
	if f.status == enumspb.WORKFLOW_EXECUTION_STATUS_UNSPECIFIED {
		f.status = enumspb.WORKFLOW_EXECUTION_STATUS_RUNNING
	}
	return "run-" + p.WorkflowID, nil
}

func (f *fakeWorkflowRunClient) Describe(_ context.Context, _, _, _ string) (*temporal.WorkflowExecutionInfo, error) {
	return &temporal.WorkflowExecutionInfo{Status: f.status, RunID: "run", Failure: f.failure}, nil
}

func (f *fakeWorkflowRunClient) Cancel(_ context.Context, _, wfID, _, _ string) error {
	f.canceled = append(f.canceled, wfID)
	return nil
}

func (f *fakeWorkflowRunClient) Terminate(_ context.Context, _, wfID, _, _ string) error {
	f.terminated = append(f.terminated, wfID)
	return nil
}

func (f *fakeWorkflowRunClient) Close() error { return nil }

var _ = Describe("TemporalWorkflowRun reconciler", func() {
	const testNamespace = "default"
	ctx := context.Background()
	var counter int
	var fake *fakeWorkflowRunClient

	var factory temporal.WorkflowRunClientFactory = func(_ context.Context, _ string, _ *tls.Config) (temporal.WorkflowRunClient, error) {
		return fake, nil
	}
	reconciler := func() *TemporalWorkflowRunReconciler {
		return &TemporalWorkflowRunReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ClientFactory: factory}
	}

	newReadyCluster := func(name string, policy *temporalv1alpha1.WorkflowRunPolicy) *temporalv1alpha1.TemporalCluster {
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       validClusterSpec("1.31.1"),
		}
		c.Spec.WorkflowRunPolicy = policy
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		meta.SetStatusCondition(&c.Status.Conditions, metav1.Condition{
			Type: temporalv1alpha1.ConditionReady, Status: metav1.ConditionTrue, Reason: "Ready", Message: "ready",
		})
		Expect(k8sClient.Status().Update(ctx, c)).To(Succeed())
		return c
	}

	newRun := func(name, cluster string) *temporalv1alpha1.TemporalWorkflowRun {
		return &temporalv1alpha1.TemporalWorkflowRun{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec: temporalv1alpha1.TemporalWorkflowRunSpec{
				ClusterRef: temporalv1alpha1.ClusterReference{Name: cluster},
				Namespace:  "orders",
				Workflow:   temporalv1alpha1.StartWorkflowAction{WorkflowType: "Greet", TaskQueue: "tq"},
			},
		}
	}

	BeforeEach(func() {
		counter++
		fake = &fakeWorkflowRunClient{}
	})

	It("starts the workflow and records Running status", func() {
		c := newReadyCluster(fmt.Sprintf("cluster-%d", counter), &temporalv1alpha1.WorkflowRunPolicy{Enabled: true})
		run := newRun(fmt.Sprintf("run-%d", counter), c.Name)
		Expect(k8sClient.Create(ctx, run)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, run) })

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: run.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, err = reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		Expect(fake.started).To(ContainElement(run.Name))
		var got temporalv1alpha1.TemporalWorkflowRun
		Expect(k8sClient.Get(ctx, req.NamespacedName, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal("Running"))
		Expect(got.Status.RunID).NotTo(BeEmpty())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeTrue())
	})

	It("denies the run when policy is disabled and starts nothing", func() {
		c := newReadyCluster(fmt.Sprintf("cluster-%d", counter), &temporalv1alpha1.WorkflowRunPolicy{Enabled: false})
		run := newRun(fmt.Sprintf("run-%d", counter), c.Name)
		Expect(k8sClient.Create(ctx, run)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, run) })

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: run.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		Expect(fake.started).To(BeEmpty())
		var got temporalv1alpha1.TemporalWorkflowRun
		Expect(k8sClient.Get(ctx, req.NamespacedName, &got)).To(Succeed())
		Expect(got.Status.RunID).To(BeEmpty())
		cond := meta.FindStatusCondition(got.Status.Conditions, temporalv1alpha1.ConditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal(temporalv1alpha1.ReasonWorkflowRunNotPermitted))
	})

	It("captures failure and sets Finished when the workflow fails", func() {
		c := newReadyCluster(fmt.Sprintf("cluster-%d", counter), &temporalv1alpha1.WorkflowRunPolicy{Enabled: true})
		fake.status = enumspb.WORKFLOW_EXECUTION_STATUS_FAILED
		fake.failure = &temporal.WorkflowFailure{Message: "boom", Type: "MyError"}
		run := newRun(fmt.Sprintf("run-%d", counter), c.Name)
		Expect(k8sClient.Create(ctx, run)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, run) })

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: run.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		var got temporalv1alpha1.TemporalWorkflowRun
		Expect(k8sClient.Get(ctx, req.NamespacedName, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal("Failed"))
		Expect(got.Status.Failure).NotTo(BeNil())
		Expect(got.Status.Failure.Message).To(Equal("boom"))
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, "Finished")).To(BeTrue())
	})

	It("terminates a running workflow on delete with cancellationPolicy=Terminate", func() {
		c := newReadyCluster(fmt.Sprintf("cluster-%d", counter), &temporalv1alpha1.WorkflowRunPolicy{Enabled: true})
		run := newRun(fmt.Sprintf("run-%d", counter), c.Name)
		run.Spec.CancellationPolicy = "Terminate"
		Expect(k8sClient.Create(ctx, run)).To(Succeed())

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: run.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req) // adds finalizer + starts
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Delete(ctx, run)).To(Succeed())
		_, err = reconciler().Reconcile(ctx, req) // handles deletion
		Expect(err).NotTo(HaveOccurred())
		Expect(fake.terminated).NotTo(BeEmpty())
	})
})
```

- [ ] **Step 3: Run the suite.**

Run: `make test` (or, faster: `KUBEBUILDER_ASSETS="$(bin/setup-envtest use 1.33.0 --bin-dir bin -p path)" go test ./internal/controller/ -run TestControllers`)
Expected: PASS. Fix compile/logic issues the loop surfaces (e.g. the terminal-status branch sets `CompletionTime`, so the Terminate test deletes while `CompletionTime==nil` only if the workflow was still Running — keep `fake.status` defaulting to RUNNING in that test).

- [ ] **Step 4: Commit.**

```bash
git add internal/controller/temporalworkflowrun_controller.go internal/controller/temporalworkflowrun_controller_test.go
git commit -s -m "feat(controller): reconcile TemporalWorkflowRun (start, status, TTL, policy)"
```

---

### Task 5: Validating webhook

**Files:**
- Create: `internal/webhook/v1alpha1/temporalworkflowrun_webhook.go`
- Test: `internal/webhook/v1alpha1/temporalworkflowrun_webhook_test.go`

**Interfaces:**
- Consumes: `temporalv1alpha1.TemporalWorkflowRun`, the existing `validateJSONList`/`validateJSONMap`/`validReusePolicies` helpers in package `v1alpha1` (webhook package), `apiequality.Semantic.DeepEqual`.
- Produces: `SetupTemporalWorkflowRunWebhookWithManager(mgr) error`; `TemporalWorkflowRunCustomValidator`.

- [ ] **Step 1: Write the webhook.** Create `internal/webhook/v1alpha1/temporalworkflowrun_webhook.go` (license header + `package v1alpha1`):

```go
package v1alpha1

import (
	"context"
	"fmt"

	apiequality "k8s.io/apimachinery/pkg/api/equality"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

var temporalworkflowrunlog = logf.Log.WithName("temporalworkflowrun-resource")

// SetupTemporalWorkflowRunWebhookWithManager registers the webhook.
func SetupTemporalWorkflowRunWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &temporalv1alpha1.TemporalWorkflowRun{}).
		WithValidator(&TemporalWorkflowRunCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-temporal-bmor10-com-v1alpha1-temporalworkflowrun,mutating=false,failurePolicy=fail,sideEffects=None,groups=temporal.bmor10.com,resources=temporalworkflowruns,verbs=create;update,versions=v1alpha1,name=vtemporalworkflowrun-v1alpha1.kb.io,admissionReviewVersions=v1

// TemporalWorkflowRunCustomValidator validates TemporalWorkflowRun resources.
type TemporalWorkflowRunCustomValidator struct{}

var _ admission.Validator[*temporalv1alpha1.TemporalWorkflowRun] = &TemporalWorkflowRunCustomValidator{}

func (v *TemporalWorkflowRunCustomValidator) ValidateCreate(_ context.Context, run *temporalv1alpha1.TemporalWorkflowRun) (admission.Warnings, error) {
	temporalworkflowrunlog.Info("Validation for TemporalWorkflowRun upon creation", "name", run.GetName())
	return nil, validateWorkflowRun(run)
}

func (v *TemporalWorkflowRunCustomValidator) ValidateUpdate(_ context.Context, oldRun, newRun *temporalv1alpha1.TemporalWorkflowRun) (admission.Warnings, error) {
	temporalworkflowrunlog.Info("Validation for TemporalWorkflowRun upon update", "name", newRun.GetName())
	if newRun.Spec.ClusterRef != oldRun.Spec.ClusterRef {
		return nil, fmt.Errorf("spec.clusterRef is immutable")
	}
	if newRun.Spec.Namespace != oldRun.Spec.Namespace {
		return nil, fmt.Errorf("spec.namespace is immutable (was %q)", oldRun.Spec.Namespace)
	}
	if !apiequality.Semantic.DeepEqual(oldRun.Spec.Workflow, newRun.Spec.Workflow) {
		return nil, fmt.Errorf("spec.workflow is immutable; create a new TemporalWorkflowRun to run again")
	}
	return nil, validateWorkflowRun(newRun)
}

func (v *TemporalWorkflowRunCustomValidator) ValidateDelete(_ context.Context, _ *temporalv1alpha1.TemporalWorkflowRun) (admission.Warnings, error) {
	return nil, nil
}

func validateWorkflowRun(run *temporalv1alpha1.TemporalWorkflowRun) error {
	if run.Spec.ClusterRef.Name == "" {
		return fmt.Errorf("spec.clusterRef.name must not be empty")
	}
	if run.Spec.Namespace == "" {
		return fmt.Errorf("spec.namespace must not be empty")
	}
	w := run.Spec.Workflow
	if w.WorkflowType == "" {
		return fmt.Errorf("spec.workflow.workflowType must not be empty")
	}
	if w.TaskQueue == "" {
		return fmt.Errorf("spec.workflow.taskQueue must not be empty")
	}
	if _, ok := validReusePolicies[w.WorkflowIDReusePolicy]; !ok {
		return fmt.Errorf("spec.workflow.workflowIDReusePolicy %q is not valid", w.WorkflowIDReusePolicy)
	}
	if err := validateJSONList("spec.workflow.args", w.Args); err != nil {
		return err
	}
	if err := validateJSONMap("spec.workflow.memo", w.Memo); err != nil {
		return err
	}
	return validateJSONMap("spec.workflow.searchAttributes", w.SearchAttributes)
}
```

- [ ] **Step 2: Write the test.** Create `internal/webhook/v1alpha1/temporalworkflowrun_webhook_test.go` (license header + `package v1alpha1`). Mirror `temporalschedule_webhook_test.go` structure:

```go
package v1alpha1

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func validRun() *temporalv1alpha1.TemporalWorkflowRun {
	return &temporalv1alpha1.TemporalWorkflowRun{
		ObjectMeta: metav1.ObjectMeta{Name: "r"},
		Spec: temporalv1alpha1.TemporalWorkflowRunSpec{
			ClusterRef: temporalv1alpha1.ClusterReference{Name: "c"},
			Namespace:  "orders",
			Workflow:   temporalv1alpha1.StartWorkflowAction{WorkflowType: "Greet", TaskQueue: "tq"},
		},
	}
}

func TestValidateWorkflowRunCreate(t *testing.T) {
	v := &TemporalWorkflowRunCustomValidator{}
	if _, err := v.ValidateCreate(context.Background(), validRun()); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
	bad := validRun()
	bad.Spec.Workflow.TaskQueue = ""
	if _, err := v.ValidateCreate(context.Background(), bad); err == nil {
		t.Fatal("expected error for empty taskQueue")
	}
}

func TestValidateWorkflowRunImmutability(t *testing.T) {
	v := &TemporalWorkflowRunCustomValidator{}
	old := validRun()
	newRun := validRun()
	newRun.Spec.Workflow.WorkflowType = "Other"
	if _, err := v.ValidateUpdate(context.Background(), old, newRun); err == nil {
		t.Fatal("expected error mutating spec.workflow")
	}

	// Mutable fields are allowed.
	ttl := int32(60)
	mutable := validRun()
	mutable.Spec.TTLSecondsAfterFinished = &ttl
	mutable.Spec.CancellationPolicy = "Terminate"
	if _, err := v.ValidateUpdate(context.Background(), old, mutable); err != nil {
		t.Fatalf("expected mutable update to pass, got %v", err)
	}
}

func TestValidateWorkflowRunInvalidJSON(t *testing.T) {
	v := &TemporalWorkflowRunCustomValidator{}
	bad := validRun()
	bad.Spec.Workflow.Args = []runtime.RawExtension{{Raw: []byte("{not json")}}
	if _, err := v.ValidateCreate(context.Background(), bad); err == nil {
		t.Fatal("expected error for invalid JSON args")
	}
}
```

- [ ] **Step 3: Run tests.**

Run: `go test ./internal/webhook/v1alpha1/ -run 'TestValidateWorkflowRun'`
Expected: PASS. (If `validReusePolicies`/`validateJSONList`/`validateJSONMap` are unexported in another file of the same package, they are accessible — same package `v1alpha1`.)

- [ ] **Step 4: Commit.**

```bash
git add internal/webhook/v1alpha1/temporalworkflowrun_webhook.go internal/webhook/v1alpha1/temporalworkflowrun_webhook_test.go
git commit -s -m "feat(webhook): validate TemporalWorkflowRun (required fields, immutability)"
```

---

### Task 6: Wire into the manager, PROJECT, sample, and regenerate artifacts

**Files:**
- Modify: `cmd/main.go`, `PROJECT`
- Create: `config/samples/temporal_v1alpha1_temporalworkflowrun.yaml`
- Regenerated: `config/crd/*`, `config/rbac/*`, `dist/chart/*`, `docs/api/v1alpha1.md`, `docs/content/reference/_index.md`

**Interfaces:**
- Consumes: `controller.TemporalWorkflowRunReconciler` (Task 4), `webhookv1alpha1.SetupTemporalWorkflowRunWebhookWithManager` (Task 5).

- [ ] **Step 1: Register the controller in `cmd/main.go`.** After the `TemporalClusterConnectionReconciler` setup block, add:

```go
	if err := (&controller.TemporalWorkflowRunReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TemporalWorkflowRun")
		os.Exit(1)
	}
```

- [ ] **Step 2: Register the webhook in `cmd/main.go`.** After the last `if webhooksEnabled { ... SetupTemporalClusterConnectionWebhookWithManager ... }` block, add:

```go
	if webhooksEnabled {
		if err := webhookv1alpha1.SetupTemporalWorkflowRunWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "TemporalWorkflowRun")
			os.Exit(1)
		}
	}
```

- [ ] **Step 3: Add the PROJECT entry.** In `PROJECT`, add a resource block alongside the other satellite CRDs (before the trailing `version: "3"`), matching the existing indentation:

```yaml
- api:
    crdVersion: v1
    namespaced: true
  domain: bmor10.com
  group: temporal
  kind: TemporalWorkflowRun
  path: github.com/bmorton/temporal-operator/api/v1alpha1
  version: v1alpha1
  webhooks:
    validation: true
    webhookVersion: v1
```

- [ ] **Step 4: Create the sample CR.** Create `config/samples/temporal_v1alpha1_temporalworkflowrun.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalWorkflowRun
metadata:
  name: greet-once
spec:
  clusterRef:
    name: temporal-sample
  namespace: default
  ttlSecondsAfterFinished: 300
  cancellationPolicy: Abandon
  workflow:
    workflowType: GreetingWorkflow
    taskQueue: greeting-tq
    args:
      - '"world"'
---
# The referenced TemporalCluster must opt in to operator-initiated runs:
# apiVersion: temporal.bmor10.com/v1alpha1
# kind: TemporalCluster
# metadata:
#   name: temporal-sample
# spec:
#   workflowRunPolicy:
#     enabled: true
#     allowedNamespaces: ["default"]
#     allowedTaskQueues: ["greeting-tq"]
```

- [ ] **Step 5: Regenerate manifests, chart, and docs.**

Run: `make manifests helm-chart api-docs docs-crd-reference`
Expected: succeeds. `git status` shows the new CRD under `config/crd/bases/`, RBAC additions, `dist/chart` updates (new CRD template + RBAC), and updated `docs/api/v1alpha1.md` + `docs/content/reference/_index.md`.

- [ ] **Step 6: Build the whole project.**

Run: `make build`
Expected: exit 0.

- [ ] **Step 7: Commit.**

```bash
git add cmd/main.go PROJECT config docs dist
git commit -s -m "feat: wire TemporalWorkflowRun controller, webhook, and generated manifests"
```

---

### Task 7: Chainsaw e2e suite + CI wiring

**Files:**
- Create: `test/e2e/workflowrun/chainsaw-test.yaml`, `01-devserver.yaml`, `01-assert.yaml`, `02-namespace.yaml`, `02-assert.yaml`, `03-workflowrun.yaml`, `03-assert.yaml`, `04-assert-terminated.yaml`, `05-deny.yaml`, `05-assert.yaml`
- Modify: `.github/workflows/e2e.yml`

**Interfaces:** None (declarative YAML + CI). Drives terminal state by terminating the workflow with `admin-tools`, so no custom worker image is needed.

- [ ] **Step 1: Dev server fixture.** Create `test/e2e/workflowrun/01-devserver.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalDevServer
metadata:
  name: wfr
spec:
  version: "1.31.1"
  namespaces: ["orders"]
  workflowRunPolicy:
    enabled: true
    allowedTaskQueues: ["greeting-tq"]
```

And `test/e2e/workflowrun/01-assert.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalDevServer
metadata:
  name: wfr
status:
  (conditions[?type == 'Ready'].status | [0]): "True"
```

- [ ] **Step 2: Namespace fixture.** Create `test/e2e/workflowrun/02-namespace.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalNamespace
metadata:
  name: orders
spec:
  clusterRef:
    name: wfr
    kind: TemporalDevServer
  retentionPeriod: "24h"
```

And `test/e2e/workflowrun/02-assert.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalNamespace
metadata:
  name: orders
status:
  registered: true
  (conditions[?type == 'Ready'].status | [0]): "True"
```

- [ ] **Step 3: WorkflowRun fixtures.** Create `test/e2e/workflowrun/03-workflowrun.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalWorkflowRun
metadata:
  name: greet-once
spec:
  clusterRef:
    name: wfr
    kind: TemporalDevServer
  namespace: orders
  ttlSecondsAfterFinished: 30
  workflow:
    workflowType: GreetingWorkflow
    taskQueue: greeting-tq
```

`test/e2e/workflowrun/03-assert.yaml` (started + Running, no worker so it stays Running):

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalWorkflowRun
metadata:
  name: greet-once
status:
  phase: Running
  (workflowID != null && workflowID != ''): true
  (runID != null && runID != ''): true
  (conditions[?type == 'Ready'].status | [0]): "True"
```

`test/e2e/workflowrun/04-assert-terminated.yaml` (after external terminate):

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalWorkflowRun
metadata:
  name: greet-once
status:
  phase: Terminated
  (conditions[?type == 'Finished'].status | [0]): "True"
```

- [ ] **Step 4: Deny fixtures.** Create `test/e2e/workflowrun/05-deny.yaml` (task queue not in allowlist):

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalWorkflowRun
metadata:
  name: denied-run
spec:
  clusterRef:
    name: wfr
    kind: TemporalDevServer
  namespace: orders
  workflow:
    workflowType: GreetingWorkflow
    taskQueue: not-allowed-tq
```

`test/e2e/workflowrun/05-assert.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalWorkflowRun
metadata:
  name: denied-run
status:
  (conditions[?type == 'Ready'].status | [0]): "False"
  (conditions[?type == 'Ready'].reason | [0]): WorkflowRunNotPermitted
```

- [ ] **Step 5: Chainsaw test orchestration.** Create `test/e2e/workflowrun/chainsaw-test.yaml`:

```yaml
# Chainsaw test: stand up a disposable TemporalDevServer, start a one-off
# workflow via TemporalWorkflowRun, drive it to a terminal state by terminating
# it with admin-tools, verify TTL deletes the CR, and verify the
# WorkflowRunPolicy denies a run using a disallowed task queue. No external DB
# and no custom worker image are required.
apiVersion: chainsaw.kyverno.io/v1alpha1
kind: Test
metadata:
  name: workflowrun
spec:
  timeouts:
    apply: 1m
    assert: 5m
    exec: 2m
  steps:
    - name: dev-server
      try:
        - apply:
            file: 01-devserver.yaml
        - assert:
            file: 01-assert.yaml
    - name: register-namespace
      try:
        - apply:
            file: 02-namespace.yaml
        - assert:
            file: 02-assert.yaml
    - name: start-workflow
      try:
        - apply:
            file: 03-workflowrun.yaml
        - assert:
            file: 03-assert.yaml
    - name: terminate-and-ttl
      try:
        - script:
            content: |
              kubectl run -n $NAMESPACE wf-terminate --rm -i --restart=Never \
                --image=temporalio/admin-tools:1.31.1 -- \
                temporal workflow terminate \
                --address dev-wfr:7233 --namespace orders \
                --workflow-id greet-once --reason e2e
        - assert:
            file: 04-assert-terminated.yaml
        - error:
            # After TTL (30s) the operator deletes the CR.
            file: 04-assert-terminated.yaml
    - name: policy-deny
      try:
        - apply:
            file: 05-deny.yaml
        - assert:
            file: 05-assert.yaml
```

Note: confirm the dev server in-cluster frontend Service DNS name. `DevServerFrontendEndpoint` (in `internal/resources/devserver.go`) and the existing `devserver` suite use `dev-<name>:7233`, hence `dev-wfr:7233`. If the helper differs, match it.

- [ ] **Step 6: Wire into CI.** In `.github/workflows/e2e.yml`, in the matrix-compute step, add the combo definition next to `devserver`:

```bash
          workflowrun='{"suite":"workflowrun"}'
```

Add `$workflowrun` to the `schedule` combos list and the `all` list, and add a `workflow_dispatch` case:

```bash
              workflowrun)  echo "combos=[$workflowrun]" >> "$GITHUB_OUTPUT" ;;
```

Finally, in the "Pre-pull and load Temporal images" step, ensure the `workflowrun` suite pulls the dev-server + admin-tools images the same way the `devserver` suite does (extend the `grep -q "^devserver"` branch condition to also match `^workflowrun`, or add an equivalent branch). Inspect that step (`.github/workflows/e2e.yml` around the devserver handling) and mirror it.

- [ ] **Step 7: Validate the suite YAML locally (lint only — full run needs a cluster).**

Run: `./bin/chainsaw lint test --test-dir test/e2e/workflowrun || true` (install chainsaw with `make chainsaw` first if absent)
Expected: no schema errors. Full execution happens via `make chainsaw-test-nsc SUITE=workflowrun` in Task 8.

- [ ] **Step 8: Commit.**

```bash
git add test/e2e/workflowrun .github/workflows/e2e.yml
git commit -s -m "test(e2e): add workflowrun Chainsaw suite and CI wiring"
```

---

### Task 8: Full verification & PR

**Files:** none (verification + delivery).

- [ ] **Step 1: Run the full local checks.**

Run: `make generate manifests build test lint`
Expected: all succeed; `git status` is clean (no stale generated artifacts). If `make manifests`/`helm-chart`/`api-docs` produce diffs, commit them (`fix: regenerate manifests`), because CI enforces drift checks.

- [ ] **Step 2: Run the e2e suite on an ephemeral nsc cluster (the real validation signal).**

Run: `make chainsaw-test-nsc SUITE=workflowrun`
Expected: the suite passes — dev server Ready, workflow started (phase Running, workflowID/runID populated), terminate → Terminated + Finished, CR auto-deleted after TTL, and the policy-deny run reports `Ready=False`/`WorkflowRunNotPermitted`. If anything fails, fix and re-run before opening the PR.

- [ ] **Step 3: Push the branch and open the PR.**

```bash
git push -u origin feat/temporalworkflowrun-crd
gh pr create --fill --title "feat: add TemporalWorkflowRun CRD for one-off workflow runs" \
  --body "Implements a Job-like TemporalWorkflowRun CRD: starts one-off workflows, tracks execution status, TTL cleanup, finalizer-based cancellation policy, and a closed-by-default WorkflowRunPolicy opt-in on TemporalCluster/TemporalDevServer. mTLS reuses the existing target-resolution path. Includes unit/envtest coverage and a Chainsaw e2e suite (dispatchable + nightly). See docs/superpowers/specs/2026-06-25-temporalworkflowrun-design.md."
```

Expected: PR created. Verify CI (build, test, lint, DCO, generated-chart/docs drift) is green.

---

## Self-Review Notes

- **Spec coverage:** one-shot/immutable (Task 1 spec + Task 5 webhook); TTL deletes CR (Task 4 reconcileRun); cancellationPolicy via finalizer (Task 4 reconcileDelete); status detail + failure-only (Task 1 status, Task 3 failure extraction, Task 4 mapping); cluster opt-in policy closed-by-default (Task 1 fields, Task 2 effective defaults, Task 4 enforcement); mTLS reuse (Task 4 via resolveTarget — unchanged); e2e nsc + CI (Task 7); generated-artifact + DCO discipline (Tasks 6, 8). All spec sections map to a task.
- **Type consistency:** `WorkflowRunPolicy`, `StartWorkflowParams`, `WorkflowExecutionInfo`, `WorkflowFailure`, `PhaseFromStatus`/`IsTerminalStatus`, `effectiveWorkflowRunPolicy`, `workflowRunParams`, `checkWorkflowRunPolicy`, `resolveWorkflowID` are referenced consistently across tasks. `spec.workflow` is the existing `StartWorkflowAction` type throughout.
- **Known follow-the-compiler points (flagged inline):** exact `go.temporal.io/api` proto getter names (failure type, timed-out attrs), dev-server frontend DNS in e2e, and the e2e.yml image pre-pull branch — each task tells the implementer to verify against the live code/version.
