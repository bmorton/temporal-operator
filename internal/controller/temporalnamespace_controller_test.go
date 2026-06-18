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

// fakeNamespaceClient records operations against an in-memory namespace store.
type fakeNamespaceClient struct {
	store                        map[string]*temporal.NamespaceInfo
	registered, updated, deleted []string
}

func (f *fakeNamespaceClient) Describe(_ context.Context, name string) (*temporal.NamespaceInfo, error) {
	info, ok := f.store[name]
	if !ok {
		return nil, temporal.ErrNamespaceNotFound
	}
	return info, nil
}

func (f *fakeNamespaceClient) Register(_ context.Context, p temporal.NamespaceParams) error {
	f.registered = append(f.registered, p.Name)
	f.store[p.Name] = &temporal.NamespaceInfo{
		ID:              "id-" + p.Name,
		Description:     p.Description,
		OwnerEmail:      p.OwnerEmail,
		RetentionPeriod: p.RetentionPeriod,
	}
	return nil
}

func (f *fakeNamespaceClient) Update(_ context.Context, p temporal.NamespaceParams) error {
	f.updated = append(f.updated, p.Name)
	if info, ok := f.store[p.Name]; ok {
		info.Description = p.Description
		info.OwnerEmail = p.OwnerEmail
		info.RetentionPeriod = p.RetentionPeriod
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
				ClusterRef:  corev1.LocalObjectReference{Name: cluster},
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
			Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: corev1.LocalObjectReference{Name: cluster}, Description: "v1"},
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
			Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: corev1.LocalObjectReference{Name: cluster}, AllowDeletion: true},
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
			Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: corev1.LocalObjectReference{Name: cluster}},
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
			Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: corev1.LocalObjectReference{Name: cluster}},
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
})
