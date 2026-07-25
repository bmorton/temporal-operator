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
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
	"github.com/bmorton/temporal-operator/internal/status"
)

var _ = Describe("TemporalCluster upgrade reconciler", func() {
	ctx := context.Background()
	var counter int

	coreServices := []string{"frontend", "history", "matching", "worker"}

	reconciler := func() *TemporalClusterReconciler {
		return &TemporalClusterReconciler{
			Client:         k8sClient,
			Scheme:         k8sClient.Scheme(),
			BackendFactory: fakeBackendFactory(nil, map[string]string{"temporal": "1.12", "temporal_visibility": "1.12"}),
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
			Spec:       validClusterSpec("1.31.0"),
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })

		By("bringing the cluster to Ready at 1.31.0")
		reconcileOnce(name)
		markReadyAtVersion(name, "1.31.0")
		reconcileOnce(name)
		Expect(get(name).Status.Version).To(Equal("1.31.0"))
		Expect(meta.IsStatusConditionTrue(get(name).Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeTrue())

		By("requesting an upgrade to 1.31.1")
		cur := get(name)
		cur.Spec.Version = "1.31.1"
		Expect(k8sClient.Update(ctx, cur)).To(Succeed())

		By("driving the upgrade phase machine to completion")
		phases := map[string]bool{}
		var rollbackableDuringSchema *bool
		for i := 0; i < 20; i++ {
			reconcileOnce(name)
			markReadyAtVersion(name, "1.31.1")

			c := get(name)
			if c.Status.Upgrade != nil {
				phases[c.Status.Upgrade.Phase] = true
				if c.Status.Upgrade.Phase == "SchemaMigrating" {
					v := c.Status.Upgrade.Rollbackable
					rollbackableDuringSchema = &v
				}
			}
			if c.Status.Version == "1.31.1" && c.Status.Upgrade == nil {
				break
			}
		}

		final := get(name)
		Expect(final.Status.Version).To(Equal("1.31.1"))
		Expect(final.Status.Upgrade).To(BeNil())

		By("having passed through the ordered rolling phases")
		for _, p := range []string{"PreflightChecks", "SchemaMigrating", "RollingFrontend", "RollingHistory", "RollingMatching", "RollingWorker"} {
			Expect(phases).To(HaveKey(p), "expected to observe phase %s", p)
		}

		By("Rollbackable is true when entering schema migration (matches documented contract)")
		Expect(rollbackableDuringSchema).NotTo(BeNil())
		Expect(*rollbackableDuringSchema).To(BeTrue())
	})

	It("does not start an upgrade on a fresh install", func() {
		counter++
		name := fmt.Sprintf("fresh-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       validClusterSpec("1.31.1"),
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })

		reconcileOnce(name)
		markReadyAtVersion(name, "1.31.1")
		reconcileOnce(name)

		final := get(name)
		Expect(final.Status.Upgrade).To(BeNil())
		Expect(final.Status.Version).To(Equal("1.31.1"))
	})
})

func TestAdvanceRollingPhaseMarksStallAfterTimeout(t *testing.T) {
	origTimeout := upgradePhaseTimeout
	upgradePhaseTimeout = 50 * time.Millisecond
	defer func() { upgradePhaseTimeout = origTimeout }()

	cluster := &temporalv1alpha1.TemporalCluster{}
	cluster.Name = "tc"
	cluster.Namespace = testNamespace
	cluster.Spec.Version = testToVersion
	cluster.Status.Version = testFromVersion

	entered := metav1.NewTime(time.Now().Add(-time.Second))
	cluster.Status.Upgrade = &temporalv1alpha1.UpgradeStatus{
		FromVersion:    testFromVersion,
		ToVersion:      testToVersion,
		Phase:          upgradeRollingFrontend,
		PhaseStartedAt: &entered,
	}

	// No Deployment exists, so serviceRolledOut is false: the phase is stalled.
	r := &TemporalClusterReconciler{Client: fake.NewClientBuilder().WithScheme(testScheme(t)).Build()}
	r.advanceUpgrade(context.Background(), cluster)

	up := cluster.Status.Upgrade
	if up == nil {
		t.Fatal("upgrade status was cleared, want it retained while stalled")
	}
	if up.Phase != upgradeRollingFrontend {
		t.Errorf("phase = %q, want it to stay at %q", up.Phase, upgradeRollingFrontend)
	}
	if up.StalledService != resources.ServiceFrontend {
		t.Errorf("stalledService = %q, want %q", up.StalledService, resources.ServiceFrontend)
	}
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionUpgradeBlocked) {
		t.Error("UpgradeBlocked condition is not True")
	}
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionDegraded) {
		t.Error("Degraded condition is not True")
	}
	if !r.upgradeStalled(cluster) {
		t.Error("upgradeStalled reported false for a stalled upgrade")
	}
}

func TestAdvanceRollingPhaseDoesNotStallBeforeTimeout(t *testing.T) {
	origTimeout := upgradePhaseTimeout
	upgradePhaseTimeout = time.Hour
	defer func() { upgradePhaseTimeout = origTimeout }()

	cluster := &temporalv1alpha1.TemporalCluster{}
	cluster.Name = "tc"
	cluster.Namespace = testNamespace
	entered := metav1.NewTime(time.Now())
	cluster.Status.Upgrade = &temporalv1alpha1.UpgradeStatus{
		FromVersion:    testFromVersion,
		ToVersion:      testToVersion,
		Phase:          upgradeRollingFrontend,
		PhaseStartedAt: &entered,
	}

	r := &TemporalClusterReconciler{Client: fake.NewClientBuilder().WithScheme(testScheme(t)).Build()}
	r.advanceUpgrade(context.Background(), cluster)

	if meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionUpgradeBlocked) {
		t.Error("UpgradeBlocked set before the phase timeout elapsed")
	}
	if cluster.Status.Upgrade.StalledService != "" {
		t.Errorf("stalledService = %q, want empty before timeout", cluster.Status.Upgrade.StalledService)
	}
}

func TestStallClearsWhenServiceRollsOut(t *testing.T) {
	origTimeout := upgradePhaseTimeout
	upgradePhaseTimeout = 50 * time.Millisecond
	defer func() { upgradePhaseTimeout = origTimeout }()

	cluster := &temporalv1alpha1.TemporalCluster{}
	cluster.Name = "tc"
	cluster.Namespace = testNamespace
	entered := metav1.NewTime(time.Now().Add(-time.Second))
	cluster.Status.Upgrade = &temporalv1alpha1.UpgradeStatus{
		FromVersion:    testFromVersion,
		ToVersion:      testToVersion,
		Phase:          upgradeRollingFrontend,
		PhaseStartedAt: &entered,
		StalledService: resources.ServiceFrontend,
	}
	status.Set(cluster, temporalv1alpha1.ConditionUpgradeBlocked, metav1.ConditionTrue, temporalv1alpha1.ReasonUpgradeStalled, "stalled")

	dep := rolledOutDeployment(cluster, resources.ServiceFrontend, testToVersion)
	r := &TemporalClusterReconciler{Client: fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(dep).Build()}
	r.advanceUpgrade(context.Background(), cluster)

	if cluster.Status.Upgrade.Phase != upgradeRollingHistory {
		t.Errorf("phase = %q, want %q after rollout completed", cluster.Status.Upgrade.Phase, upgradeRollingHistory)
	}
	if cluster.Status.Upgrade.StalledService != "" {
		t.Errorf("stalledService = %q, want cleared", cluster.Status.Upgrade.StalledService)
	}
	if meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionUpgradeBlocked) {
		t.Error("UpgradeBlocked is still True after the service rolled out")
	}
}

// rolledOutDeployment builds a Deployment that serviceRolledOut reports as
// fully rolled out at the given version.
func rolledOutDeployment(cluster *temporalv1alpha1.TemporalCluster, component, version string) *appsv1.Deployment {
	one := int32(1)
	dep := &appsv1.Deployment{}
	dep.Name = resources.DeploymentName(cluster.Name, component)
	dep.Namespace = cluster.Namespace
	dep.Generation = 1
	dep.Spec.Replicas = &one
	dep.Spec.Template.Labels = map[string]string{resources.LabelVersion: version}
	dep.Status.ObservedGeneration = 1
	dep.Status.UpdatedReplicas = 1
	dep.Status.ReadyReplicas = 1
	return dep
}
