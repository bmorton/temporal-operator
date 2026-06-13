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
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

var _ = Describe("TemporalCluster UI and monitoring", func() {
	ctx := context.Background()
	var counter int

	readySchema := fakeInspector{versions: map[string]string{
		"temporal":            "1.12",
		"temporal_visibility": "1.12",
	}}

	BeforeEach(func() {
		_ = k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "temporal-store", Namespace: "default"},
			Data:       map[string][]byte{"password": []byte("pw")},
		})
	})

	reconcileFor := func(name string) {
		r := &TemporalClusterReconciler{
			Client:          k8sClient,
			Scheme:          k8sClient.Scheme(),
			Prober:          fakeProber{},
			SchemaInspector: readySchema,
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())
	}

	create := func(mutate func(*temporalv1alpha1.TemporalCluster)) string {
		counter++
		name := fmt.Sprintf("ui-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       validClusterSpec("1.31.2"),
		}
		mutate(c)
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		return name
	}

	It("deploys the UI Deployment, Service, and Ingress when enabled", func() {
		name := create(func(c *temporalv1alpha1.TemporalCluster) {
			c.Spec.UI = &temporalv1alpha1.UISpec{
				Enabled: true,
				Version: "2.34.0",
				Ingress: &temporalv1alpha1.UIIngressSpec{Enabled: true, Host: "temporal.example.com"},
			}
		})
		reconcileFor(name)

		dep := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.UIName(name), Namespace: "default"}, dep)).To(Succeed())
		Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("temporalio/ui:2.34.0"))

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.UIName(name), Namespace: "default"}, &corev1.Service{})).To(Succeed())

		ing := &networkingv1.Ingress{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.UIName(name), Namespace: "default"}, ing)).To(Succeed())
		Expect(ing.Spec.Rules[0].Host).To(Equal("temporal.example.com"))
	})

	It("does not deploy the UI when disabled", func() {
		name := create(func(c *temporalv1alpha1.TemporalCluster) {})
		reconcileFor(name)

		err := k8sClient.Get(ctx, types.NamespacedName{Name: resources.UIName(name), Namespace: "default"}, &appsv1.Deployment{})
		Expect(err).To(HaveOccurred())
	})

	It("creates a ServiceMonitor when the CRD is installed and requested", func() {
		name := create(func(c *temporalv1alpha1.TemporalCluster) {
			c.Spec.Metrics = &temporalv1alpha1.MetricsSpec{
				Enabled:        true,
				ServiceMonitor: &temporalv1alpha1.ServiceMonitorSpec{Enabled: true, Labels: map[string]string{"release": "kps"}},
			}
		})
		reconcileFor(name)

		sm := &unstructured.Unstructured{}
		sm.SetGroupVersionKind(resources.ServiceMonitorGVK)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.ServiceMonitorName(name), Namespace: "default"}, sm)).To(Succeed())
		Expect(sm.GetLabels()).To(HaveKeyWithValue("release", "kps"))
	})
})
