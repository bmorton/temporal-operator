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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// Versions used by the upgrade scenarios: both are in the support matrix and
// share a schema version, so the phase machine is exercised without Jobs.
const (
	upgradeFromVersion = "1.31.0"
	upgradeToVersion   = "1.31.1"
)

var _ = Describe("TemporalCluster upgrade reconciler", func() {
	ctx := context.Background()
	var counter int

	coreServices := []string{"frontend", "history", "matching", "worker"}

	reconciler := func() *TemporalClusterReconciler {
		return &TemporalClusterReconciler{
			Client:         k8sClient,
			Scheme:         k8sClient.Scheme(),
			BackendFactory: fakeBackendFactory(nil, map[string]string{"temporal": "1.19", "temporal_visibility": "1.14"}),
		}
	}

	reconcileOnce := func(name string) {
		_, err := reconciler().Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())
	}

	// markReadyAtVersion marks every core deployment currently templated at the
	// given version as fully rolled out.
	markReadyAtVersion := func(name, version string) {
		for _, svc := range coreServices {
			dep := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(name, svc), Namespace: "default"}, dep); err != nil {
				continue
			}
			if dep.Spec.Template.Labels[resources.LabelVersion] != version {
				continue
			}
			desired := int32(1)
			if dep.Spec.Replicas != nil {
				desired = *dep.Spec.Replicas
			}
			dep.Status.ObservedGeneration = dep.Generation
			dep.Status.Replicas = desired
			dep.Status.UpdatedReplicas = desired
			dep.Status.ReadyReplicas = desired
			Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())
		}
	}

	// expectServerImages asserts every core Deployment templates the server
	// container at image.
	expectServerImages := func(name, image string) {
		for _, svc := range coreServices {
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(name, svc), Namespace: "default"}, dep)).To(Succeed())
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal(image), "service %s", svc)
		}
	}

	get := func(name string) *temporalv1alpha1.TemporalCluster {
		c := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, c)).To(Succeed())
		return c
	}

	BeforeEach(func() {
		_ = k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "temporal-store", Namespace: "default"},
			Data:       map[string][]byte{"password": []byte("pw")},
		})
	})

	It("rolls services in order from 1.31.0 to 1.31.1 and records phases", func() {
		counter++
		name := fmt.Sprintf("upg-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       validClusterSpec(upgradeFromVersion),
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })

		By("bringing the cluster to Ready at 1.31.0")
		reconcileOnce(name)
		markReadyAtVersion(name, upgradeFromVersion)
		reconcileOnce(name)
		Expect(get(name).Status.Version).To(Equal(upgradeFromVersion))
		Expect(meta.IsStatusConditionTrue(get(name).Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeTrue())

		By("requesting an upgrade to 1.31.1")
		cur := get(name)
		cur.Spec.Version = upgradeToVersion
		Expect(k8sClient.Update(ctx, cur)).To(Succeed())

		By("driving the upgrade phase machine to completion")
		phases := map[string]bool{}
		var rollbackableDuringSchema *bool
		for i := 0; i < 20; i++ {
			reconcileOnce(name)
			markReadyAtVersion(name, upgradeToVersion)

			c := get(name)
			if c.Status.Upgrade != nil {
				phases[c.Status.Upgrade.Phase] = true
				if c.Status.Upgrade.Phase == "SchemaMigrating" {
					v := c.Status.Upgrade.Rollbackable
					rollbackableDuringSchema = &v
				}
			}
			if c.Status.Version == upgradeToVersion && c.Status.Upgrade == nil {
				break
			}
		}

		final := get(name)
		Expect(final.Status.Version).To(Equal(upgradeToVersion))
		Expect(final.Status.Upgrade).To(BeNil())

		By("having passed through the ordered rolling phases")
		for _, p := range []string{"PreflightChecks", "SchemaMigrating", "RollingFrontend", "RollingHistory", "RollingMatching", "RollingWorker"} {
			Expect(phases).To(HaveKey(p), "expected to observe phase %s", p)
		}

		By("marking the upgrade non-rollbackable once schema migration starts")
		Expect(rollbackableDuringSchema).NotTo(BeNil())
		Expect(*rollbackableDuringSchema).To(BeFalse())

		By("running the new server image in every service")
		expectServerImages(name, "temporalio/server:1.31.1")
	})

	It("does not let a persisted default spec.image pin the server image across an upgrade", func() {
		counter++
		name := fmt.Sprintf("pinned-%d", counter)
		spec := validClusterSpec(upgradeFromVersion)
		// What a pre-#223 defaulting webhook stored at creation time.
		spec.Image = "temporalio/server:1.31.0"
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })

		reconcileOnce(name)
		markReadyAtVersion(name, upgradeFromVersion)
		reconcileOnce(name)
		Expect(get(name).Status.Version).To(Equal(upgradeFromVersion))
		expectServerImages(name, "temporalio/server:1.31.0")

		cur := get(name)
		cur.Spec.Version = upgradeToVersion
		Expect(k8sClient.Update(ctx, cur)).To(Succeed())

		for i := 0; i < 20; i++ {
			reconcileOnce(name)
			markReadyAtVersion(name, upgradeToVersion)
			if c := get(name); c.Status.Version == upgradeToVersion && c.Status.Upgrade == nil {
				break
			}
		}
		Expect(get(name).Status.Version).To(Equal(upgradeToVersion))
		expectServerImages(name, "temporalio/server:1.31.1")
	})

	It("does not advance a rolling phase while the service still runs the old image", func() {
		counter++
		name := fmt.Sprintf("stale-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       validClusterSpec(upgradeFromVersion),
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })

		reconcileOnce(name)
		markReadyAtVersion(name, upgradeFromVersion)
		reconcileOnce(name)
		Expect(get(name).Status.Version).To(Equal(upgradeFromVersion))

		// Simulate the #223 failure mode directly: a frontend Deployment that
		// carries the new version label but still templates the old image.
		dep := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(name, "frontend"), Namespace: "default"}, dep)).To(Succeed())
		dep.Spec.Template.Labels[resources.LabelVersion] = upgradeToVersion
		dep.Spec.Template.Spec.Containers[0].Image = "temporalio/server:1.31.0"
		Expect(k8sClient.Update(ctx, dep)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: dep.Name, Namespace: "default"}, dep)).To(Succeed())
		dep.Status.ObservedGeneration = dep.Generation
		dep.Status.Replicas = *dep.Spec.Replicas
		dep.Status.UpdatedReplicas = *dep.Spec.Replicas
		dep.Status.ReadyReplicas = *dep.Spec.Replicas
		Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())

		Expect(reconciler().serviceRolledOut(ctx, get(name), "frontend", upgradeToVersion)).To(BeFalse())
	})

	It("does not start an upgrade on a fresh install", func() {
		counter++
		name := fmt.Sprintf("fresh-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       validClusterSpec(upgradeToVersion),
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })

		reconcileOnce(name)
		markReadyAtVersion(name, upgradeToVersion)
		reconcileOnce(name)

		final := get(name)
		Expect(final.Status.Upgrade).To(BeNil())
		Expect(final.Status.Version).To(Equal(upgradeToVersion))
	})
})
