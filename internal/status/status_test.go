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

package status_test

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/status"
)

func scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := temporalv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}
	return s
}

func TestSetStampsObservedGeneration(t *testing.T) {
	ns := &temporalv1alpha1.TemporalNamespace{}
	ns.Generation = 4

	status.Set(ns, temporalv1alpha1.ConditionReady, metav1.ConditionTrue, "Registered", "ok")

	if ns.Status.ObservedGeneration != 4 {
		t.Errorf("status.observedGeneration = %d, want 4", ns.Status.ObservedGeneration)
	}
	conds := *ns.GetConditions()
	if len(conds) != 1 {
		t.Fatalf("got %d conditions, want 1", len(conds))
	}
	if conds[0].ObservedGeneration != 4 {
		t.Errorf("condition.observedGeneration = %d, want 4", conds[0].ObservedGeneration)
	}
	if conds[0].Reason != "Registered" {
		t.Errorf("condition.reason = %q, want %q", conds[0].Reason, "Registered")
	}
}

// conflictClient returns a Conflict error for the first n status updates.
type conflictClient struct {
	client.Client
	remaining int
	attempts  int
}

func (c *conflictClient) Status() client.SubResourceWriter {
	return &conflictWriter{parent: c, inner: c.Client.Status()}
}

type conflictWriter struct {
	parent *conflictClient
	inner  client.SubResourceWriter
}

func (w *conflictWriter) Create(ctx context.Context, obj client.Object, sub client.Object, opts ...client.SubResourceCreateOption) error {
	return w.inner.Create(ctx, obj, sub, opts...)
}

func (w *conflictWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return w.inner.Patch(ctx, obj, patch, opts...)
}

func (w *conflictWriter) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.SubResourceApplyOption) error {
	return w.inner.Apply(ctx, obj, opts...)
}

func (w *conflictWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	w.parent.attempts++
	if w.parent.remaining > 0 {
		w.parent.remaining--
		return apierrors.NewConflict(schema.GroupResource{Group: "temporal.bmor10.com", Resource: "temporalnamespaces"}, obj.GetName(), nil)
	}
	return w.inner.Update(ctx, obj, opts...)
}

func TestUpdateRetriesOnConflict(t *testing.T) {
	ns := &temporalv1alpha1.TemporalNamespace{}
	ns.Name = "ns1"
	ns.Namespace = "default"
	ns.Generation = 1

	base := fake.NewClientBuilder().
		WithScheme(scheme(t)).
		WithObjects(ns).
		WithStatusSubresource(ns).
		Build()
	c := &conflictClient{Client: base, remaining: 2}

	status.Set(ns, temporalv1alpha1.ConditionReady, metav1.ConditionTrue, "Registered", "ok")
	if err := status.Update(context.Background(), c, ns); err != nil {
		t.Fatalf("Update returned %v, want nil", err)
	}
	if c.attempts != 3 {
		t.Errorf("status update attempts = %d, want 3 (2 conflicts then success)", c.attempts)
	}

	var got temporalv1alpha1.TemporalNamespace
	if err := base.Get(context.Background(), client.ObjectKeyFromObject(ns), &got); err != nil {
		t.Fatalf("re-reading namespace: %v", err)
	}
	if len(got.Status.Conditions) != 1 || got.Status.Conditions[0].Reason != "Registered" {
		t.Errorf("condition was not persisted after retry: %+v", got.Status.Conditions)
	}
}

func TestUpdateIgnoresNotFound(t *testing.T) {
	ns := &temporalv1alpha1.TemporalNamespace{}
	ns.Name = "gone"
	ns.Namespace = "default"

	c := fake.NewClientBuilder().WithScheme(scheme(t)).Build()
	if err := status.Update(context.Background(), c, ns); err != nil {
		t.Errorf("Update on a deleted object returned %v, want nil", err)
	}
}
