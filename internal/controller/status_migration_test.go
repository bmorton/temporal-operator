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

package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// TestSetReadyStampsObservedGeneration asserts every controller's setReady
// stamps observedGeneration on both the status and the condition. Before the
// migration to internal/status, TemporalClusterClient did not.
func TestSetReadyStampsObservedGeneration(t *testing.T) {
	const gen int64 = 11

	t.Run("TemporalNamespace", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalNamespace{}
		obj.Generation = gen
		(&TemporalNamespaceReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})

	t.Run("TemporalSchedule", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalSchedule{}
		obj.Generation = gen
		(&TemporalScheduleReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})

	t.Run("TemporalSearchAttribute", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalSearchAttribute{}
		obj.Generation = gen
		(&TemporalSearchAttributeReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})

	t.Run("TemporalClusterClient", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalClusterClient{}
		obj.Generation = gen
		(&TemporalClusterClientReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})

	t.Run("TemporalClusterConnection", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalClusterConnection{}
		obj.Generation = gen
		(&TemporalClusterConnectionReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})

	t.Run("TemporalDevServer", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalDevServer{}
		obj.Generation = gen
		(&TemporalDevServerReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})

	t.Run("TemporalWorkflowRun", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalWorkflowRun{}
		obj.Generation = gen
		(&TemporalWorkflowRunReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})
}

func assertStamped(t *testing.T, observed int64, conds []metav1.Condition, want int64) {
	t.Helper()
	if observed != want {
		t.Errorf("status.observedGeneration = %d, want %d", observed, want)
	}
	if len(conds) != 1 {
		t.Fatalf("got %d conditions, want 1", len(conds))
	}
	if conds[0].ObservedGeneration != want {
		t.Errorf("condition.observedGeneration = %d, want %d", conds[0].ObservedGeneration, want)
	}
	if conds[0].Type != temporalv1alpha1.ConditionReady {
		t.Errorf("condition.type = %q, want %q", conds[0].Type, temporalv1alpha1.ConditionReady)
	}
}
