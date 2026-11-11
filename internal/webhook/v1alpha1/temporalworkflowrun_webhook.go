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
