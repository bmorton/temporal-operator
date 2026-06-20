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

package persistence

import (
	"context"
	"errors"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestJobInspectorBackend_PodNotTerminated(t *testing.T) {
	// Test 1: Pod not terminated yet (or no message) → Probe returns ErrInspecting
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inspector-job",
			Namespace: "test-ns",
		},
	}

	// Pod exists but not terminated
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inspector-pod",
			Namespace: "test-ns",
			Labels: map[string]string{
				"job-name": "inspector-job",
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:  "inspect",
					State: corev1.ContainerState{},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job, pod).Build()

	ensureCalls := 0
	ensureJob := func(ctx context.Context) (*batchv1.Job, error) {
		ensureCalls++
		return job, nil
	}

	backend := NewJobInspectorBackend(fakeClient, "test-db", ensureJob)

	err := backend.Probe(context.Background())
	if !errors.Is(err, ErrInspecting) {
		t.Errorf("Probe() error = %v, want ErrInspecting", err)
	}

	if ensureCalls != 1 {
		t.Errorf("ensureJob called %d times, want 1", ensureCalls)
	}

	// Prove ErrInspecting is not cached: call Probe() again and verify ensureJob is invoked again
	err = backend.Probe(context.Background())
	if !errors.Is(err, ErrInspecting) {
		t.Errorf("Probe() second call error = %v, want ErrInspecting", err)
	}

	if ensureCalls != 2 {
		t.Errorf("ensureJob called %d times after second Probe(), want 2", ensureCalls)
	}
}

func TestJobInspectorBackend_PodTerminatedReachable(t *testing.T) {
	// Test 2: Pod terminated with message {"reachable":true,"version":"1.13"} →
	// Probe returns nil AND SchemaVersion returns "1.13"; assert ensureJob called at most once
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inspector-job",
			Namespace: "test-ns",
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inspector-pod",
			Namespace: "test-ns",
			Labels: map[string]string{
				"job-name": "inspector-job",
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "inspect",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Message: `{"reachable":true,"version":"1.13"}`,
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job, pod).Build()

	ensureCalls := 0
	ensureJob := func(ctx context.Context) (*batchv1.Job, error) {
		ensureCalls++
		return job, nil
	}

	backend := NewJobInspectorBackend(fakeClient, "test-db", ensureJob)

	err := backend.Probe(context.Background())
	if err != nil {
		t.Errorf("Probe() error = %v, want nil", err)
	}

	version, err := backend.SchemaVersion(context.Background())
	if err != nil {
		t.Errorf("SchemaVersion() error = %v, want nil", err)
	}
	if version != "1.13" {
		t.Errorf("SchemaVersion() = %q, want %q", version, "1.13")
	}

	// ensureJob should be called at most once due to caching
	if ensureCalls > 1 {
		t.Errorf("ensureJob called %d times, want at most 1", ensureCalls)
	}
}

func TestJobInspectorBackend_PodTerminatedUnreachable(t *testing.T) {
	// Test 3: Message {"reachable":false,"error":"timeout"} → Probe returns error containing "timeout"
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inspector-job",
			Namespace: "test-ns",
		},
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "inspector-pod",
			Namespace: "test-ns",
			Labels: map[string]string{
				"job-name": "inspector-job",
			},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name: "inspect",
					State: corev1.ContainerState{
						Terminated: &corev1.ContainerStateTerminated{
							Message: `{"reachable":false,"error":"timeout"}`,
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(job, pod).Build()

	ensureJob := func(ctx context.Context) (*batchv1.Job, error) {
		return job, nil
	}

	backend := NewJobInspectorBackend(fakeClient, "test-db", ensureJob)

	err := backend.Probe(context.Background())
	if err == nil {
		t.Error("Probe() error = nil, want error containing 'timeout'")
	}
	if err != nil && !containsString(err.Error(), "timeout") {
		t.Errorf("Probe() error = %v, want error containing 'timeout'", err)
	}
}

func TestJobInspectorBackend_Kind(t *testing.T) {
	// Test 4: Kind() == "sql"
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = batchv1.AddToScheme(scheme)

	fakeClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	ensureJob := func(ctx context.Context) (*batchv1.Job, error) {
		return nil, nil
	}

	backend := NewJobInspectorBackend(fakeClient, "test-db", ensureJob)

	if got := backend.Kind(); got != "sql" {
		t.Errorf("Kind() = %q, want %q", got, "sql")
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
