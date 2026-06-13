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
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

var _ = Describe("TemporalCluster services reconciler", func() {
	ctx := context.Background()
	var counter int

	readyVersions := map[string]string{
		"temporal":            "1.12",
		"temporal_visibility": "1.12",
	}

	reconcileFor := func(name string) {
		r := &TemporalClusterReconciler{
			Client:         k8sClient,
			Scheme:         k8sClient.Scheme(),
			BackendFactory: fakeBackendFactory(nil, readyVersions),
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())
	}

	BeforeEach(func() {
		_ = k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "temporal-store", Namespace: "default"},
			Data:       map[string][]byte{"password": []byte("pw")},
		})
	})

	newCluster := func() string {
		counter++
		name := fmt.Sprintf("svc-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       validClusterSpec("1.31.2"),
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		return name
	}

	get := func(name string) *temporalv1alpha1.TemporalCluster {
		c := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, c)).To(Succeed())
		return c
	}

	It("deploys all service objects once the schema is ready", func() {
		name := newCluster()
		reconcileFor(name)

		By("creating the config Secret and dynamic-config ConfigMap")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.ConfigSecretName(name), Namespace: "default"}, &corev1.Secret{})).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DynamicConfigMapName(name), Namespace: "default"}, &corev1.ConfigMap{})).To(Succeed())

		By("creating a Deployment, headless Service, and PDB per service")
		for _, svc := range []string{"frontend", "history", "matching", "worker"} {
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(name, svc), Namespace: "default"}, &appsv1.Deployment{})).To(Succeed(), svc)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.HeadlessServiceName(name, svc), Namespace: "default"}, &corev1.Service{})).To(Succeed(), svc)
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.PDBName(name, svc), Namespace: "default"}, &policyv1.PodDisruptionBudget{})).To(Succeed(), svc)
		}

		By("creating the frontend client Service")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.FrontendServiceName(name), Namespace: "default"}, &corev1.Service{})).To(Succeed())

		By("reporting Available=False while deployments are not ready")
		c := get(name)
		Expect(meta.IsStatusConditionTrue(c.Status.Conditions, temporalv1alpha1.ConditionAvailable)).To(BeFalse())
		Expect(c.Status.Phase).To(Equal("DeployingServices"))
	})

	It("becomes Ready when all deployments report ready replicas", func() {
		name := newCluster()
		reconcileFor(name)

		By("simulating ready deployments")
		for _, svc := range []string{"frontend", "history", "matching", "worker"} {
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(name, svc), Namespace: "default"}, dep)).To(Succeed())
			dep.Status.Replicas = 1
			dep.Status.ReadyReplicas = 1
			Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())
		}

		reconcileFor(name)

		c := get(name)
		Expect(meta.IsStatusConditionTrue(c.Status.Conditions, temporalv1alpha1.ConditionAvailable)).To(BeTrue())
		Expect(meta.IsStatusConditionTrue(c.Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeTrue())
		Expect(c.Status.Phase).To(Equal("Ready"))
		Expect(c.Status.Version).To(Equal("1.31.2"))
		Expect(c.Status.Services).To(HaveKey("frontend"))
	})
})
