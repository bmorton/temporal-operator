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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// conditionAccessor is the contract internal/status depends on.
type conditionAccessor interface {
	GetConditions() *[]metav1.Condition
	SetObservedGeneration(int64)
}

func TestAllTypesImplementConditionAccessor(t *testing.T) {
	objs := map[string]conditionAccessor{
		"TemporalCluster":           &TemporalCluster{},
		"TemporalClusterClient":     &TemporalClusterClient{},
		"TemporalClusterConnection": &TemporalClusterConnection{},
		"TemporalDevServer":         &TemporalDevServer{},
		"TemporalNamespace":         &TemporalNamespace{},
		"TemporalSchedule":          &TemporalSchedule{},
		"TemporalSearchAttribute":   &TemporalSearchAttribute{},
		"TemporalWorkflowRun":       &TemporalWorkflowRun{},
	}

	for name, obj := range objs {
		t.Run(name, func(t *testing.T) {
			conds := obj.GetConditions()
			if conds == nil {
				t.Fatal("GetConditions returned nil pointer")
			}
			*conds = append(*conds, metav1.Condition{Type: "Ready"})
			if len(*obj.GetConditions()) != 1 {
				t.Error("GetConditions does not return a live pointer into status")
			}
			obj.SetObservedGeneration(7)
		})
	}
}
