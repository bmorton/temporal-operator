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

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// This file provides the uniform status accessors that internal/status uses to
// operate on any Temporal CRD without type switches. Keep one pair of methods
// per API type; there is intentionally no reflection here.

func (t *TemporalCluster) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalCluster) SetObservedGeneration(g int64)      { t.Status.ObservedGeneration = g }

func (t *TemporalClusterClient) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalClusterClient) SetObservedGeneration(g int64)      { t.Status.ObservedGeneration = g }

func (t *TemporalClusterConnection) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalClusterConnection) SetObservedGeneration(g int64) {
	t.Status.ObservedGeneration = g
}

func (t *TemporalDevServer) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalDevServer) SetObservedGeneration(g int64)      { t.Status.ObservedGeneration = g }

func (t *TemporalNamespace) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalNamespace) SetObservedGeneration(g int64)      { t.Status.ObservedGeneration = g }

func (t *TemporalSchedule) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalSchedule) SetObservedGeneration(g int64)      { t.Status.ObservedGeneration = g }

func (t *TemporalSearchAttribute) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalSearchAttribute) SetObservedGeneration(g int64) {
	t.Status.ObservedGeneration = g
}

func (t *TemporalWorkflowRun) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalWorkflowRun) SetObservedGeneration(g int64)      { t.Status.ObservedGeneration = g }
