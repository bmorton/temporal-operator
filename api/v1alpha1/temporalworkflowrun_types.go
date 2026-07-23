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

// Cancellation policies applied to a still-running workflow when its
// TemporalWorkflowRun is deleted.
const (
	// CancellationPolicyAbandon leaves the workflow running.
	CancellationPolicyAbandon = "Abandon"
	// CancellationPolicyCancel requests graceful cancellation of the workflow.
	CancellationPolicyCancel = "Cancel"
	// CancellationPolicyTerminate forcefully terminates the workflow.
	CancellationPolicyTerminate = "Terminate"
)

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
