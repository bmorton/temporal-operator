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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// fakeSearchAttributeClient records search-attribute operations in memory.
type fakeSearchAttributeClient struct {
	attrs          map[string]string // name -> type
	added, removed []string
}

func (f *fakeSearchAttributeClient) List(_ context.Context, _ string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range f.attrs {
		out[k] = v
	}
	return out, nil
}

func (f *fakeSearchAttributeClient) Add(_ context.Context, _, name, attrType string) error {
	f.added = append(f.added, name)
	f.attrs[name] = attrType
	return nil
}

func (f *fakeSearchAttributeClient) Remove(_ context.Context, _, name string) error {
	f.removed = append(f.removed, name)
	delete(f.attrs, name)
	return nil
}

func (f *fakeSearchAttributeClient) Close() error { return nil }

var _ = Describe("TemporalSearchAttribute reconciler", func() {
	ctx := context.Background()
	var counter int
	var fake *fakeSearchAttributeClient

	var factory temporal.SearchAttributeClientFactory = func(_ context.Context, _ string, _ *tls.Config) (temporal.SearchAttributeClient, error) {
		return fake, nil
	}

	reconcileSA := func(name string) {
		r := &TemporalSearchAttributeReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ClientFactory: factory}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())
	}

	readyCluster := func() string {
		counter++
		name := fmt.Sprintf("sa-cluster-%d", counter)
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
		fake = &fakeSearchAttributeClient{attrs: map[string]string{}}
	})

	It("registers a search attribute once the cluster is ready", func() {
		cluster := readyCluster()
		saName := fmt.Sprintf("attr-%d", counter)
		sa := &temporalv1alpha1.TemporalSearchAttribute{
			ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalSearchAttributeSpec{
				ClusterRef: temporalv1alpha1.ClusterReference{Name: cluster},
				Namespace:  "default",
				Name:       "CustomerId",
				Type:       "Keyword",
			},
		}
		Expect(k8sClient.Create(ctx, sa)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, sa) })

		reconcileSA(saName) // finalizer
		reconcileSA(saName) // add + visible

		Expect(fake.added).To(ContainElement("CustomerId"))

		got := &temporalv1alpha1.TemporalSearchAttribute{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: saName, Namespace: "default"}, got)).To(Succeed())
		Expect(got.Status.Registered).To(BeTrue())
		Expect(got.Status.RegisteredAt).NotTo(BeNil())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeTrue())
	})

	It("removes the attribute on deletion when allowDeletion is set", func() {
		cluster := readyCluster()
		saName := fmt.Sprintf("delattr-%d", counter)
		sa := &temporalv1alpha1.TemporalSearchAttribute{
			ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalSearchAttributeSpec{
				ClusterRef:    temporalv1alpha1.ClusterReference{Name: cluster},
				Namespace:     "default",
				Name:          "OrderId",
				Type:          "Keyword",
				AllowDeletion: true,
			},
		}
		Expect(k8sClient.Create(ctx, sa)).To(Succeed())

		reconcileSA(saName)
		reconcileSA(saName)
		Expect(fake.added).To(ContainElement("OrderId"))

		Expect(k8sClient.Delete(ctx, sa)).To(Succeed())
		reconcileSA(saName)

		Expect(fake.removed).To(ContainElement("OrderId"))
		err := k8sClient.Get(ctx, types.NamespacedName{Name: saName, Namespace: "default"}, &temporalv1alpha1.TemporalSearchAttribute{})
		Expect(err).To(HaveOccurred())
	})

	It("removes the finalizer when the cluster is deleted before the search attribute", func() {
		cluster := readyCluster()
		saName := fmt.Sprintf("strandedattr-%d", counter)
		sa := &temporalv1alpha1.TemporalSearchAttribute{
			ObjectMeta: metav1.ObjectMeta{Name: saName, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalSearchAttributeSpec{
				ClusterRef: temporalv1alpha1.ClusterReference{Name: cluster},
				Namespace:  "default",
				Name:       "StrandedId",
				Type:       "Keyword",
			},
		}
		Expect(k8sClient.Create(ctx, sa)).To(Succeed())

		reconcileSA(saName) // adds finalizer
		reconcileSA(saName) // add + visible

		// Simulate the user deleting the TemporalCluster first.
		c := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cluster, Namespace: "default"}, c)).To(Succeed())
		Expect(k8sClient.Delete(ctx, c)).To(Succeed())

		// Now delete the search attribute — the finalizer keeps it Terminating.
		Expect(k8sClient.Delete(ctx, sa)).To(Succeed())

		// Reconcile: the cluster is gone, so the finalizer must be removed and
		// the object fully released (no stuck Terminating state).
		reconcileSA(saName)

		err := k8sClient.Get(ctx, types.NamespacedName{Name: saName, Namespace: "default"}, &temporalv1alpha1.TemporalSearchAttribute{})
		Expect(err).To(HaveOccurred(), "search attribute should be gone after finalizer is removed")
	})
})
