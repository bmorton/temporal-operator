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
