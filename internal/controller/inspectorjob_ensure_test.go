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
	"context"
	"errors"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/persistence"
	"github.com/bmorton/temporal-operator/internal/resources"
)

func inspectorTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("corev1: %v", err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatalf("batchv1: %v", err)
	}
	if err := temporalv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("temporalv1alpha1: %v", err)
	}
	return scheme
}

func inspectorTestCluster() *temporalv1alpha1.TemporalCluster {
	// Azure Workload Identity, because that is the only configuration in which
	// the Job-based inspector is used at all: with a direct connection the
	// controller talks to the datastore itself.
	spec := validClusterSpec("1.31.1")
	spec.Persistence.AzureWorkloadIdentity = &temporalv1alpha1.AzureWorkloadIdentitySpec{
		ClientID:           "00000000-0000-0000-0000-000000000000",
		ServiceAccountName: "temporal-azure",
	}
	return &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tc", Namespace: "default"},
		Spec:       spec,
	}
}

func inspectorSQLSpec() *temporalv1alpha1.SQLDatastoreSpec {
	return inspectorTestCluster().Spec.Persistence.DefaultStore.SQL
}

// A Job that is being deleted cannot be waited on: it will not run again, and
// its pods are on their way out. Returning it makes the inspector wait for an
// answer that can never come, which is one shape of the stall where a cluster
// sits at "inspection in progress" indefinitely.
func TestEnsureInspectorJob_TerminatingJobIsNotReturned(t *testing.T) {
	scheme := inspectorTestScheme(t)
	cluster := inspectorTestCluster()
	name := resources.InspectorJobName(cluster.Name, resources.StoreDefault)

	deleting := metav1.NewTime(time.Now())
	existing := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         cluster.Namespace,
			DeletionTimestamp: &deleting,
			Finalizers:        []string{"keep/for-test"},
		},
	}

	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster, existing).Build()
	r := &TemporalClusterReconciler{Client: c, Scheme: scheme}

	_, err := r.ensureInspectorJob(context.Background(), cluster, inspectorSQLSpec(), resources.StoreDefault)
	if !errors.Is(err, persistence.ErrInspecting) {
		t.Fatalf("a terminating Job must requeue rather than be treated as usable; got %v", err)
	}
}

// When the Job already exists -- typically because a previous one is still
// being garbage-collected -- swallowing AlreadyExists and returning the object
// that was built in memory hands back something with no status and no UID. The
// inspector then reads a Job that does not exist as the API sees it.
func TestEnsureInspectorJob_AlreadyExistsReturnsTheLiveJob(t *testing.T) {
	scheme := inspectorTestScheme(t)
	cluster := inspectorTestCluster()
	name := resources.InspectorJobName(cluster.Name, resources.StoreDefault)

	live := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: cluster.Namespace,
			UID:       "live-uid",
		},
		Status: batchv1.JobStatus{Active: 1},
	}

	// Get misses but Create collides: the interceptor reproduces the window in
	// which the object is present to the writer but not yet to the reader.
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(cluster).Build()
	r := &TemporalClusterReconciler{Client: &alreadyExistsClient{Client: c, existing: live}, Scheme: scheme}

	got, err := r.ensureInspectorJob(context.Background(), cluster, inspectorSQLSpec(), resources.StoreDefault)
	if err != nil {
		if !errors.Is(err, persistence.ErrInspecting) {
			t.Fatalf("unexpected error: %v", err)
		}
		return // requeueing is an acceptable answer; returning a phantom is not
	}
	if got.UID != "live-uid" {
		t.Fatalf("expected the live Job from the API, got one with UID %q", got.UID)
	}
}

// alreadyExistsClient reproduces the window where a Job is absent to a reader
// but present to a writer: Get misses until a Create has collided, after which
// the live object is served. That is what happens while a previous inspector
// Job is still being garbage-collected.
type alreadyExistsClient struct {
	client.Client
	existing *batchv1.Job
	collided bool
}

func (c *alreadyExistsClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if job, ok := obj.(*batchv1.Job); ok && key.Name == c.existing.Name {
		if !c.collided {
			return apierrors.NewNotFound(schema.GroupResource{Group: "batch", Resource: "jobs"}, key.Name)
		}
		c.existing.DeepCopyInto(job)
		return nil
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func (c *alreadyExistsClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if job, ok := obj.(*batchv1.Job); ok && job.Name == c.existing.Name {
		c.collided = true
		return apierrors.NewAlreadyExists(schema.GroupResource{Group: "batch", Resource: "jobs"}, job.Name)
	}
	return c.Client.Create(ctx, obj, opts...)
}
