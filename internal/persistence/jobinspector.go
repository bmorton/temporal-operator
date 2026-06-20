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
	"encoding/json"
	"errors"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ErrInspecting is returned when schema inspection is still in progress.
var ErrInspecting = errors.New("schema inspection in progress")

// JobInspectorBackend is a Backend implementation that ensures an inspector Job
// runs in the cluster and reads reachability + schema version from the Job's pod
// termination message.
type JobInspectorBackend struct {
	client    client.Client
	dbName    string
	ensureJob func(ctx context.Context) (*batchv1.Job, error)

	// cached result from the first successful inspectOnce call
	cached       *InspectResult
	cachedCalled bool
}

// NewJobInspectorBackend creates a Backend that uses a Job to inspect the datastore.
// The ensureJob closure is provided by the controller to avoid an import cycle.
func NewJobInspectorBackend(c client.Client, dbName string, ensureJob func(ctx context.Context) (*batchv1.Job, error)) *JobInspectorBackend {
	return &JobInspectorBackend{
		client:    c,
		dbName:    dbName,
		ensureJob: ensureJob,
	}
}

// inspectOnce calls ensureJob to get the Job, finds its pod, and reads the termination message.
// Returns ErrInspecting if the pod hasn't completed yet. Caches the first successful result.
func (b *JobInspectorBackend) inspectOnce(ctx context.Context) (InspectResult, error) {
	if b.cachedCalled {
		return *b.cached, nil
	}

	job, err := b.ensureJob(ctx)
	if err != nil {
		return InspectResult{}, err
	}

	// Find the job's pod via label job-name=<job.Name>
	podList := &corev1.PodList{}
	err = b.client.List(ctx, podList, client.InNamespace(job.Namespace), client.MatchingLabels{"job-name": job.Name})
	if err != nil {
		return InspectResult{}, err
	}

	// Look for a pod with a terminated inspect container that has a termination message
	for _, pod := range podList.Items {
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name == "inspect" && cs.State.Terminated != nil && cs.State.Terminated.Message != "" {
				var result InspectResult
				if err := json.Unmarshal([]byte(cs.State.Terminated.Message), &result); err != nil {
					return InspectResult{}, fmt.Errorf("failed to unmarshal termination message: %w", err)
				}
				// Cache the result
				b.cached = &result
				b.cachedCalled = true
				return result, nil
			}
		}
	}

	return InspectResult{}, ErrInspecting
}

// Probe verifies the datastore is reachable.
func (b *JobInspectorBackend) Probe(ctx context.Context) error {
	result, err := b.inspectOnce(ctx)
	if err != nil {
		return err
	}
	if !result.Reachable {
		return fmt.Errorf("datastore unreachable: %s", result.Error)
	}
	return nil
}

// SchemaVersion returns the current schema version.
func (b *JobInspectorBackend) SchemaVersion(ctx context.Context) (string, error) {
	result, err := b.inspectOnce(ctx)
	if err != nil {
		return "", err
	}
	return result.Version, nil
}

// EnsureSchema returns (false, nil) for Job-based backends; the controller orchestrates setup/update.
func (b *JobInspectorBackend) EnsureSchema(ctx context.Context, minVersion string) (bool, error) {
	return false, nil
}

// Kind returns "sql".
func (b *JobInspectorBackend) Kind() string {
	return KindSQL
}
