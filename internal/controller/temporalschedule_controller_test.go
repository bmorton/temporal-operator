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
	"crypto/tls"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// fakeScheduleClient records operations against an in-memory schedule store.
type fakeScheduleClient struct {
	store                     map[string]*temporal.ScheduleInfo
	created, updated, deleted []string
	paused, unpaused          []string
}

func (f *fakeScheduleClient) key(ns, id string) string { return ns + "/" + id }

func (f *fakeScheduleClient) Describe(_ context.Context, ns, id string) (*temporal.ScheduleInfo, error) {
	info, ok := f.store[f.key(ns, id)]
	if !ok {
		return nil, temporal.ErrScheduleNotFound
	}
	return info, nil
}

func (f *fakeScheduleClient) Create(_ context.Context, p temporal.ScheduleParams) error {
	f.created = append(f.created, p.ScheduleID)
	f.store[f.key(p.Namespace, p.ScheduleID)] = &temporal.ScheduleInfo{Paused: p.State.Paused, Notes: p.State.Notes}
	return nil
}

func (f *fakeScheduleClient) Update(_ context.Context, p temporal.ScheduleParams) error {
	f.updated = append(f.updated, p.ScheduleID)
	if info, ok := f.store[f.key(p.Namespace, p.ScheduleID)]; ok {
		info.Paused = p.State.Paused
	}
	return nil
}

func (f *fakeScheduleClient) Pause(_ context.Context, ns, id, _ string) error {
	f.paused = append(f.paused, id)
	if info, ok := f.store[f.key(ns, id)]; ok {
		info.Paused = true
	}
	return nil
}

func (f *fakeScheduleClient) Unpause(_ context.Context, ns, id, _ string) error {
	f.unpaused = append(f.unpaused, id)
	if info, ok := f.store[f.key(ns, id)]; ok {
		info.Paused = false
	}
	return nil
}

func (f *fakeScheduleClient) Delete(_ context.Context, ns, id string) error {
	f.deleted = append(f.deleted, id)
	delete(f.store, f.key(ns, id))
	return nil
}

func (f *fakeScheduleClient) Close() error { return nil }

var _ = Describe("TemporalSchedule reconciler", func() {
	const testNamespace = "default"
	ctx := context.Background()
	var counter int
	var fake *fakeScheduleClient

	var factory temporal.ScheduleClientFactory = func(_ context.Context, _ string, _ *tls.Config) (temporal.ScheduleClient, error) {
		return fake, nil
	}

	reconciler := func() *TemporalScheduleReconciler {
		return &TemporalScheduleReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ClientFactory: factory}
	}

	// newCluster creates a TemporalCluster with a valid spec. Call markClusterReady
	// separately to set Ready=True in status.
	newCluster := func(name string) *temporalv1alpha1.TemporalCluster {
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec:       validClusterSpec("1.31.1"),
		}
		return c
	}

	newSchedule := func(name, cluster string) *temporalv1alpha1.TemporalSchedule {
		return &temporalv1alpha1.TemporalSchedule{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace},
			Spec: temporalv1alpha1.TemporalScheduleSpec{
				ClusterRef: corev1.LocalObjectReference{Name: cluster},
				Namespace:  "orders",
				Schedule:   temporalv1alpha1.ScheduleSpec{Calendars: []string{"0 9 * * *"}},
				Action: temporalv1alpha1.ScheduleActionSpec{
					StartWorkflow: temporalv1alpha1.StartWorkflowAction{
						WorkflowType: "ProcessOrders",
						TaskQueue:    "orders-tq",
					},
				},
			},
		}
	}

	markClusterReady := func(c *temporalv1alpha1.TemporalCluster) {
		meta.SetStatusCondition(&c.Status.Conditions, metav1.Condition{
			Type: temporalv1alpha1.ConditionReady, Status: metav1.ConditionTrue,
			Reason: "Ready", Message: "ready",
		})
		Expect(k8sClient.Status().Update(ctx, c)).To(Succeed())
	}

	BeforeEach(func() {
		counter++
		fake = &fakeScheduleClient{store: map[string]*temporal.ScheduleInfo{}}
	})

	It("creates a schedule when missing", func() {
		c := newCluster(fmt.Sprintf("cluster-%d", counter))
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		markClusterReady(c)

		s := newSchedule(fmt.Sprintf("sched-%d", counter), c.Name)
		Expect(k8sClient.Create(ctx, s)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, s) })

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: s.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		// Second reconcile ensures the schedule is created even if the first reconcile
		// also ran reconcileSchedule (pattern mirrors namespace controller tests).
		_, err = reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		Expect(fake.created).To(ContainElement(s.Name))

		var got temporalv1alpha1.TemporalSchedule
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: s.Name, Namespace: testNamespace}, &got)).To(Succeed())
		Expect(got.Status.Created).To(BeTrue())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeTrue())
	})

	It("updates the schedule when the spec hash changes", func() {
		c := newCluster(fmt.Sprintf("cluster-%d", counter))
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		markClusterReady(c)

		s := newSchedule(fmt.Sprintf("sched-%d", counter), c.Name)
		Expect(k8sClient.Create(ctx, s)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, s) })

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: s.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, err = reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(fake.created).To(ContainElement(s.Name))

		// Change the cron expression — this changes the spec hash.
		var got temporalv1alpha1.TemporalSchedule
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: s.Name, Namespace: testNamespace}, &got)).To(Succeed())
		got.Spec.Schedule.Calendars = []string{"0 10 * * *"}
		Expect(k8sClient.Update(ctx, &got)).To(Succeed())

		_, err = reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		Expect(fake.updated).To(ContainElement(s.Name))
	})

	It("pauses the schedule when spec.state.paused becomes true", func() {
		c := newCluster(fmt.Sprintf("cluster-%d", counter))
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		markClusterReady(c)

		s := newSchedule(fmt.Sprintf("sched-%d", counter), c.Name)
		Expect(k8sClient.Create(ctx, s)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, s) })

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: s.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, err = reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(fake.created).To(ContainElement(s.Name))

		// Setting Paused=true changes the spec hash → flows through Update path.
		var got temporalv1alpha1.TemporalSchedule
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: s.Name, Namespace: testNamespace}, &got)).To(Succeed())
		got.Spec.State = &temporalv1alpha1.ScheduleStateSpec{Paused: true}
		Expect(k8sClient.Update(ctx, &got)).To(Succeed())

		_, err = reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		// Update sets Paused on the store entry; confirm via fake store.
		Expect(fake.store["orders/"+s.Name].Paused).To(BeTrue())
		Expect(fake.updated).To(ContainElement(s.Name))
	})

	It("deletes the schedule when AllowDeletion is true and the CR is deleted", func() {
		c := newCluster(fmt.Sprintf("cluster-%d", counter))
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		markClusterReady(c)

		s := newSchedule(fmt.Sprintf("sched-%d", counter), c.Name)
		s.Spec.AllowDeletion = true
		Expect(k8sClient.Create(ctx, s)).To(Succeed())

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: s.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, err = reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(fake.created).To(ContainElement(s.Name))

		Expect(k8sClient.Delete(ctx, s)).To(Succeed())
		_, err = reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		Expect(fake.deleted).To(ContainElement(s.Name))
		// Finalizer removed → object fully gone.
		err = k8sClient.Get(ctx, types.NamespacedName{Name: s.Name, Namespace: testNamespace}, &temporalv1alpha1.TemporalSchedule{})
		Expect(err).To(HaveOccurred())
	})

	It("does not delete the schedule when AllowDeletion is false", func() {
		c := newCluster(fmt.Sprintf("cluster-%d", counter))
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		markClusterReady(c)

		s := newSchedule(fmt.Sprintf("sched-%d", counter), c.Name)
		Expect(k8sClient.Create(ctx, s)).To(Succeed())

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: s.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, err = reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		Expect(fake.created).To(ContainElement(s.Name))

		Expect(k8sClient.Delete(ctx, s)).To(Succeed())
		_, err = reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		Expect(fake.deleted).NotTo(ContainElement(s.Name))
	})

	It("sets ClusterNotReady when the cluster is not ready", func() {
		c := newCluster(fmt.Sprintf("cluster-%d", counter))
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		// Cluster is intentionally NOT marked ready.

		s := newSchedule(fmt.Sprintf("sched-%d", counter), c.Name)
		Expect(k8sClient.Create(ctx, s)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, s) })

		req := reconcile.Request{NamespacedName: types.NamespacedName{Name: s.Name, Namespace: testNamespace}}
		_, err := reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())
		_, err = reconciler().Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		var got temporalv1alpha1.TemporalSchedule
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: s.Name, Namespace: testNamespace}, &got)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeFalse())
		cond := meta.FindStatusCondition(got.Status.Conditions, temporalv1alpha1.ConditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("ClusterNotReady"))
		Expect(fake.created).To(BeEmpty())
	})
})
