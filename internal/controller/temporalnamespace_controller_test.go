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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// fakeNamespaceClient records operations against an in-memory namespace store.
type fakeNamespaceClient struct {
	store                        map[string]*temporal.NamespaceInfo
	registered, updated, deleted []string
	registerParams, updateParams []temporal.NamespaceParams
	failovers                    []failoverCall
	registerErr                  error
}

// failoverCall records a single Failover invocation.
type failoverCall struct {
	name          string
	activeCluster string
}

func (f *fakeNamespaceClient) Describe(_ context.Context, name string) (*temporal.NamespaceInfo, error) {
	info, ok := f.store[name]
	if !ok {
		return nil, temporal.ErrNamespaceNotFound
	}
	// Return a copy so callers never alias the stored object (mirrors the gRPC
	// client returning a freshly decoded response per call).
	cp := *info
	cp.Clusters = append([]string(nil), info.Clusters...)
	return &cp, nil
}

func (f *fakeNamespaceClient) Register(_ context.Context, p temporal.NamespaceParams) error {
	if f.registerErr != nil {
		return f.registerErr
	}
	f.registered = append(f.registered, p.Name)
	f.registerParams = append(f.registerParams, p)
	f.store[p.Name] = &temporal.NamespaceInfo{
		ID:              "id-" + p.Name,
		Description:     p.Description,
		OwnerEmail:      p.OwnerEmail,
		RetentionPeriod: p.RetentionPeriod,
		IsGlobal:        p.IsGlobal,
		ActiveCluster:   p.ActiveCluster,
		Clusters:        append([]string(nil), p.Clusters...),
	}
	return nil
}

func (f *fakeNamespaceClient) Update(_ context.Context, p temporal.NamespaceParams) error {
	f.updated = append(f.updated, p.Name)
	f.updateParams = append(f.updateParams, p)
	if info, ok := f.store[p.Name]; ok {
		info.Description = p.Description
		info.OwnerEmail = p.OwnerEmail
		info.RetentionPeriod = p.RetentionPeriod
		// Update never changes the active cluster (that is a standalone Failover).
		if len(p.Clusters) > 0 {
			info.Clusters = append([]string(nil), p.Clusters...)
		}
	}
	return nil
}

func (f *fakeNamespaceClient) Failover(_ context.Context, name, activeCluster string) error {
	f.failovers = append(f.failovers, failoverCall{name: name, activeCluster: activeCluster})
	if info, ok := f.store[name]; ok {
		info.ActiveCluster = activeCluster
	}
	return nil
}

func (f *fakeNamespaceClient) Delete(_ context.Context, name string) error {
	f.deleted = append(f.deleted, name)
	delete(f.store, name)
	return nil
}

func (f *fakeNamespaceClient) Close() error { return nil }

var _ = Describe("TemporalNamespace reconciler", func() {
	ctx := context.Background()
	var counter int
	var fake *fakeNamespaceClient

	var factory temporal.NamespaceClientFactory = func(_ context.Context, _ string, _ *tls.Config) (temporal.NamespaceClient, error) {
		return fake, nil
	}

	reconciler := func() *TemporalNamespaceReconciler {
		return &TemporalNamespaceReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ClientFactory: factory}
	}

	reconcileNS := func(name string) {
		_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())
	}

	// readyCluster creates a TemporalCluster and marks it Ready in status.
	readyCluster := func() string {
		counter++
		name := fmt.Sprintf("ns-cluster-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       validClusterSpec("1.31.1"),
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		meta.SetStatusCondition(&c.Status.Conditions, metav1.Condition{
			Type: temporalv1alpha1.ConditionReady, Status: metav1.ConditionTrue, Reason: "Ready", Message: "ready",
		})
		Expect(k8sClient.Status().Update(ctx, c)).To(Succeed())
		return name
	}

	BeforeEach(func() {
		fake = &fakeNamespaceClient{store: map[string]*temporal.NamespaceInfo{}}
	})

	It("registers a namespace once the cluster is ready", func() {
		cluster := readyCluster()
		nsName := fmt.Sprintf("orders-%d", counter)
		ns := &temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalNamespaceSpec{
				ClusterRef:  temporalv1alpha1.ClusterReference{Name: cluster},
				Description: "orders namespace",
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		reconcileNS(nsName) // adds finalizer
		reconcileNS(nsName) // registers

		Expect(fake.registered).To(ContainElement(nsName))

		got := &temporalv1alpha1.TemporalNamespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName, Namespace: "default"}, got)).To(Succeed())
		Expect(got.Status.Registered).To(BeTrue())
		Expect(got.Status.NamespaceID).To(Equal("id-" + nsName))
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeTrue())
	})

	It("updates the namespace when the spec drifts", func() {
		cluster := readyCluster()
		nsName := fmt.Sprintf("drift-%d", counter)
		ns := &temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName, Namespace: "default"},
			Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: temporalv1alpha1.ClusterReference{Name: cluster}, Description: "v1"},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		reconcileNS(nsName)
		reconcileNS(nsName)
		Expect(fake.registered).To(ContainElement(nsName))

		// Change description -> drift on next reconcile.
		got := &temporalv1alpha1.TemporalNamespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName, Namespace: "default"}, got)).To(Succeed())
		got.Spec.Description = "v2"
		Expect(k8sClient.Update(ctx, got)).To(Succeed())

		reconcileNS(nsName)
		Expect(fake.updated).To(ContainElement(nsName))
	})

	It("deletes the namespace on CR deletion when allowDeletion is set", func() {
		cluster := readyCluster()
		nsName := fmt.Sprintf("del-%d", counter)
		ns := &temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName, Namespace: "default"},
			Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: temporalv1alpha1.ClusterReference{Name: cluster}, AllowDeletion: true},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		reconcileNS(nsName)
		reconcileNS(nsName)

		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		reconcileNS(nsName)

		Expect(fake.deleted).To(ContainElement(nsName))
		// Finalizer removed -> object gone.
		err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName, Namespace: "default"}, &temporalv1alpha1.TemporalNamespace{})
		Expect(err).To(HaveOccurred())
	})

	It("does not delete the namespace when allowDeletion is false", func() {
		cluster := readyCluster()
		nsName := fmt.Sprintf("keep-%d", counter)
		ns := &temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName, Namespace: "default"},
			Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: temporalv1alpha1.ClusterReference{Name: cluster}},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		reconcileNS(nsName)
		reconcileNS(nsName)
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		reconcileNS(nsName)

		Expect(fake.deleted).NotTo(ContainElement(nsName))
	})

	It("removes the finalizer when the cluster is deleted before the namespace", func() {
		cluster := readyCluster()
		nsName := fmt.Sprintf("stranded-%d", counter)
		ns := &temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName, Namespace: "default"},
			Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: temporalv1alpha1.ClusterReference{Name: cluster}},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		reconcileNS(nsName) // adds finalizer
		reconcileNS(nsName) // registers

		// Simulate the user deleting the TemporalCluster first.
		c := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cluster, Namespace: "default"}, c)).To(Succeed())
		Expect(k8sClient.Delete(ctx, c)).To(Succeed())

		// Now delete the namespace — the finalizer keeps it Terminating.
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())

		// Reconcile: the cluster is gone, so the finalizer must be removed and
		// the object fully released (no stuck Terminating state).
		reconcileNS(nsName)

		err := k8sClient.Get(ctx, types.NamespacedName{Name: nsName, Namespace: "default"}, &temporalv1alpha1.TemporalNamespace{})
		Expect(err).To(HaveOccurred(), "namespace should be gone after finalizer is removed")
	})

	It("registers a global namespace and performs a declarative failover", func() {
		cluster := readyCluster()
		nsName := fmt.Sprintf("global-%d", counter)
		ns := &temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalNamespaceSpec{
				ClusterRef:    temporalv1alpha1.ClusterReference{Name: cluster},
				IsGlobal:      true,
				Clusters:      []string{"a", "b"},
				ActiveCluster: "a",
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		reconcileNS(nsName) // adds finalizer
		reconcileNS(nsName) // registers

		Expect(fake.registered).To(ContainElement(nsName))
		// Register params carried the global replication settings.
		var reg *temporal.NamespaceParams
		for i := range fake.registerParams {
			if fake.registerParams[i].Name == nsName {
				reg = &fake.registerParams[i]
			}
		}
		Expect(reg).NotTo(BeNil())
		Expect(reg.IsGlobal).To(BeTrue())
		Expect(reg.ActiveCluster).To(Equal("a"))
		Expect(reg.Clusters).To(Equal([]string{"a", "b"}))

		got := &temporalv1alpha1.TemporalNamespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName, Namespace: "default"}, got)).To(Succeed())
		Expect(got.Status.Replication).NotTo(BeNil())
		Expect(got.Status.Replication.IsGlobal).To(BeTrue())
		Expect(got.Status.Replication.ActiveCluster).To(Equal("a"))

		// Trigger a declarative failover: switch the active cluster to b.
		got.Spec.ActiveCluster = "b"
		Expect(k8sClient.Update(ctx, got)).To(Succeed())

		reconcileNS(nsName) // detects active-cluster drift -> standalone failover

		// Failover must be a standalone call (not bundled into a general Update),
		// because Temporal rejects an active-cluster change combined with other
		// update parameters.
		Expect(fake.failovers).To(HaveLen(1))
		Expect(fake.failovers[0].name).To(Equal(nsName))
		Expect(fake.failovers[0].activeCluster).To(Equal("b"))

		got = &temporalv1alpha1.TemporalNamespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName, Namespace: "default"}, got)).To(Succeed())
		Expect(got.Status.Replication).NotTo(BeNil())
		Expect(got.Status.Replication.ActiveCluster).To(Equal("b"))
		Expect(got.Status.Replication.LastFailoverTime).NotTo(BeNil())
	})

	It("updates a global namespace when the replication clusters list changes", func() {
		cluster := readyCluster()
		nsName := fmt.Sprintf("repl-clusters-%d", counter)
		ns := &temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalNamespaceSpec{
				ClusterRef:    temporalv1alpha1.ClusterReference{Name: cluster},
				IsGlobal:      true,
				Clusters:      []string{"a", "b"},
				ActiveCluster: "a",
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, ns) })

		reconcileNS(nsName) // adds finalizer
		reconcileNS(nsName) // registers with clusters [a, b]
		Expect(fake.registered).To(ContainElement(nsName))

		// Add cluster "c" to the replication group; active cluster unchanged.
		got := &temporalv1alpha1.TemporalNamespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName, Namespace: "default"}, got)).To(Succeed())
		got.Spec.Clusters = []string{"a", "b", "c"}
		Expect(k8sClient.Update(ctx, got)).To(Succeed())

		reconcileNS(nsName) // should detect clusters-list drift -> Update

		Expect(fake.updated).To(ContainElement(nsName))
		var upd *temporal.NamespaceParams
		for i := range fake.updateParams {
			if fake.updateParams[i].Name == nsName {
				upd = &fake.updateParams[i]
			}
		}
		Expect(upd).NotTo(BeNil())
		Expect(upd.Clusters).To(ConsistOf("a", "b", "c"))
	})

	It("requeues without error when the frontend is transiently unavailable", func() {
		clusterName := readyCluster()
		nsName := fmt.Sprintf("ns-transient-%d", counter)
		ns := &temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalNamespaceSpec{
				ClusterRef: temporalv1alpha1.ClusterReference{Name: clusterName},
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		fake = &fakeNamespaceClient{
			store:       map[string]*temporal.NamespaceInfo{},
			registerErr: status.Error(codes.Unavailable, "frontend starting"),
		}

		res, err := reconciler().Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: nsName, Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(clusterUnavailableRequeue))

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName, Namespace: "default"}, ns)).To(Succeed())
		cond := meta.FindStatusCondition(ns.Status.Conditions, temporalv1alpha1.ConditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal(temporalv1alpha1.ReasonFrontendUnavailable))
	})
})
