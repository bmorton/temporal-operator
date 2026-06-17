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
	RunningWorkflows int32 `json:"runningWorkflows,omitempty"`
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
