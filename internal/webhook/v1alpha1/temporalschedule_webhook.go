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
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

var temporalschedulelog = logf.Log.WithName("temporalschedule-resource")

// SetupTemporalScheduleWebhookWithManager registers the webhook for TemporalSchedule.
func SetupTemporalScheduleWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &temporalv1alpha1.TemporalSchedule{}).
		WithValidator(&TemporalScheduleCustomValidator{}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-temporal-bmor10-com-v1alpha1-temporalschedule,mutating=false,failurePolicy=fail,sideEffects=None,groups=temporal.bmor10.com,resources=temporalschedules,verbs=create;update,versions=v1alpha1,name=vtemporalschedule-v1alpha1.kb.io,admissionReviewVersions=v1

// TemporalScheduleCustomValidator validates TemporalSchedule resources.
type TemporalScheduleCustomValidator struct{}

var _ admission.Validator[*temporalv1alpha1.TemporalSchedule] = &TemporalScheduleCustomValidator{}

func (v *TemporalScheduleCustomValidator) ValidateCreate(_ context.Context, sched *temporalv1alpha1.TemporalSchedule) (admission.Warnings, error) {
	temporalschedulelog.Info("Validation for TemporalSchedule upon creation", "name", sched.GetName())
	return nil, validateSchedule(sched)
}

func (v *TemporalScheduleCustomValidator) ValidateUpdate(_ context.Context, oldSched, newSched *temporalv1alpha1.TemporalSchedule) (admission.Warnings, error) {
	temporalschedulelog.Info("Validation for TemporalSchedule upon update", "name", newSched.GetName())
	if resolvedScheduleID(oldSched) != resolvedScheduleID(newSched) {
		return nil, fmt.Errorf("spec.scheduleID is immutable (was %q)", resolvedScheduleID(oldSched))
	}
	if newSched.Spec.Namespace != oldSched.Spec.Namespace {
		return nil, fmt.Errorf("spec.namespace is immutable (was %q)", oldSched.Spec.Namespace)
	}
	return nil, validateSchedule(newSched)
}

func (v *TemporalScheduleCustomValidator) ValidateDelete(_ context.Context, _ *temporalv1alpha1.TemporalSchedule) (admission.Warnings, error) {
	return nil, nil
}

var validOverlapPolicies = map[string]struct{}{
	"": {}, "Skip": {}, "BufferOne": {}, "BufferAll": {},
	"CancelOther": {}, "TerminateOther": {}, "AllowAll": {},
}

var validReusePolicies = map[string]struct{}{
	"": {}, "AllowDuplicate": {}, "AllowDuplicateFailedOnly": {},
	"RejectDuplicate": {}, "TerminateIfRunning": {},
}

func resolvedScheduleID(sched *temporalv1alpha1.TemporalSchedule) string {
	if sched.Spec.ScheduleID != "" {
		return sched.Spec.ScheduleID
	}
	return sched.Name
}

func validateSchedule(sched *temporalv1alpha1.TemporalSchedule) error {
	if err := validateScheduleRequiredFields(sched); err != nil {
		return err
	}
	if err := validateScheduleTimeSource(sched); err != nil {
		return err
	}
	if err := validateSchedulePolicies(sched); err != nil {
		return err
	}

	a := sched.Spec.Action.StartWorkflow
	if err := validateJSONList("spec.action.startWorkflow.args", a.Args); err != nil {
		return err
	}
	if err := validateJSONMap("spec.action.startWorkflow.memo", a.Memo); err != nil {
		return err
	}
	if err := validateJSONMap("spec.action.startWorkflow.searchAttributes", a.SearchAttributes); err != nil {
		return err
	}
	return nil
}

func validateScheduleRequiredFields(sched *temporalv1alpha1.TemporalSchedule) error {
	if sched.Spec.ClusterRef.Name == "" {
		return fmt.Errorf("spec.clusterRef.name must not be empty")
	}
	if sched.Spec.Namespace == "" {
		return fmt.Errorf("spec.namespace must not be empty")
	}
	a := sched.Spec.Action.StartWorkflow
	if a.WorkflowType == "" {
		return fmt.Errorf("spec.action.startWorkflow.workflowType must not be empty")
	}
	if a.TaskQueue == "" {
		return fmt.Errorf("spec.action.startWorkflow.taskQueue must not be empty")
	}
	return nil
}

func validateScheduleTimeSource(sched *temporalv1alpha1.TemporalSchedule) error {
	paused := sched.Spec.State != nil && sched.Spec.State.Paused
	sp := sched.Spec.Schedule
	hasTime := len(sp.Calendars) > 0 || len(sp.Intervals) > 0 || len(sp.StructuredCalendar) > 0
	if !hasTime && !paused {
		return fmt.Errorf("spec.schedule must specify at least one of calendars, intervals, or structuredCalendar (unless spec.state.paused is true)")
	}
	return nil
}

func validateSchedulePolicies(sched *temporalv1alpha1.TemporalSchedule) error {
	if sched.Spec.Policies != nil {
		if _, ok := validOverlapPolicies[sched.Spec.Policies.OverlapPolicy]; !ok {
			return fmt.Errorf("spec.policies.overlapPolicy %q is not valid", sched.Spec.Policies.OverlapPolicy)
		}
	}
	a := sched.Spec.Action.StartWorkflow
	if _, ok := validReusePolicies[a.WorkflowIDReusePolicy]; !ok {
		return fmt.Errorf("spec.action.startWorkflow.workflowIDReusePolicy %q is not valid", a.WorkflowIDReusePolicy)
	}
	if a.RetryPolicy != nil && a.RetryPolicy.BackoffCoefficient != "" {
		f, err := strconv.ParseFloat(a.RetryPolicy.BackoffCoefficient, 64)
		if err != nil || f < 1 {
			return fmt.Errorf("spec.action.startWorkflow.retryPolicy.backoffCoefficient must be a number >= 1")
		}
	}
	return nil
}

func validateJSONList(field string, values []runtime.RawExtension) error {
	for i, raw := range values {
		if len(raw.Raw) > 0 && !json.Valid(raw.Raw) {
			return fmt.Errorf("%s[%d] is not valid JSON", field, i)
		}
	}
	return nil
}

func validateJSONMap(field string, values map[string]runtime.RawExtension) error {
	for k, raw := range values {
		if len(raw.Raw) > 0 && !json.Valid(raw.Raw) {
			return fmt.Errorf("%s[%q] is not valid JSON", field, k)
		}
	}
	return nil
}
