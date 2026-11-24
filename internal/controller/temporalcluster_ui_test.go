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

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

var _ = Describe("TemporalCluster UI and monitoring", func() {
	ctx := context.Background()
	var counter int

	readyVersions := map[string]string{
		"temporal":            "1.19",
		"temporal_visibility": "1.14",
	}

	BeforeEach(func() {
		_ = k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "temporal-store", Namespace: "default"},
			Data:       map[string][]byte{"password": []byte("pw")},
		})
	})

	reconcileFor := func(name string) {
		r := &TemporalClusterReconciler{
			Client:         k8sClient,
			Scheme:         k8sClient.Scheme(),
			BackendFactory: fakeBackendFactory(nil, readyVersions),
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())
	}

	clusterFor := func(name string) *temporalv1alpha1.TemporalCluster {
		c := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, c)).To(Succeed())
		return c
	}

	create := func(mutate func(*temporalv1alpha1.TemporalCluster)) string {
		counter++
		name := fmt.Sprintf("ui-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       validClusterSpec("1.31.1"),
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

	uiKey := func(name string) types.NamespacedName {
		return types.NamespacedName{Name: resources.UIName(name), Namespace: "default"}
	}

	updateSpec := func(name string, mutate func(*temporalv1alpha1.TemporalCluster)) {
		c := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, c)).To(Succeed())
		mutate(c)
		Expect(k8sClient.Update(ctx, c)).To(Succeed())
	}

	// envtest runs no garbage collector, so a delete of an object without
	// finalizers is immediate and NotFound is the right assertion.
	expectGone := func(key types.NamespacedName, obj client.Object) {
		GinkgoHelper()
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, obj))).To(BeTrue(), "expected %T %s to be deleted", obj, key.Name)
	}

	It("removes the Ingress when ingress is disabled, and keeps the rest of the UI", func() {
		name := create(func(c *temporalv1alpha1.TemporalCluster) {
			c.Spec.UI = &temporalv1alpha1.UISpec{
				Enabled: true,
				Ingress: &temporalv1alpha1.UIIngressSpec{Enabled: true, Host: "temporal.example.com"},
			}
		})
		reconcileFor(name)
		Expect(k8sClient.Get(ctx, uiKey(name), &networkingv1.Ingress{})).To(Succeed())

		updateSpec(name, func(c *temporalv1alpha1.TemporalCluster) { c.Spec.UI.Ingress.Enabled = false })
		reconcileFor(name)

		expectGone(uiKey(name), &networkingv1.Ingress{})
		Expect(k8sClient.Get(ctx, uiKey(name), &appsv1.Deployment{})).To(Succeed())
		Expect(k8sClient.Get(ctx, uiKey(name), &corev1.Service{})).To(Succeed())
	})

	It("removes the Ingress when the ingress block is dropped from the spec", func() {
		name := create(func(c *temporalv1alpha1.TemporalCluster) {
			c.Spec.UI = &temporalv1alpha1.UISpec{
				Enabled: true,
				Ingress: &temporalv1alpha1.UIIngressSpec{Enabled: true, Host: "temporal.example.com"},
			}
		})
		reconcileFor(name)

		updateSpec(name, func(c *temporalv1alpha1.TemporalCluster) { c.Spec.UI.Ingress = nil })
		reconcileFor(name)

		expectGone(uiKey(name), &networkingv1.Ingress{})
	})

	It("removes every UI object when the UI is disabled", func() {
		name := create(func(c *temporalv1alpha1.TemporalCluster) {
			c.Spec.UI = &temporalv1alpha1.UISpec{
				Enabled: true,
				Ingress: &temporalv1alpha1.UIIngressSpec{Enabled: true, Host: "temporal.example.com"},
			}
		})
		reconcileFor(name)

		updateSpec(name, func(c *temporalv1alpha1.TemporalCluster) { c.Spec.UI.Enabled = false })
		reconcileFor(name)

		expectGone(uiKey(name), &appsv1.Deployment{})
		expectGone(uiKey(name), &corev1.Service{})
		expectGone(uiKey(name), &networkingv1.Ingress{})
	})

	It("leaves an Ingress it does not own alone", func() {
		name := create(func(c *temporalv1alpha1.TemporalCluster) {
			c.Spec.UI = &temporalv1alpha1.UISpec{Enabled: true}
		})
		pathType := networkingv1.PathTypePrefix
		foreign := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{Name: resources.UIName(name), Namespace: "default"},
			Spec: networkingv1.IngressSpec{Rules: []networkingv1.IngressRule{{
				Host: "handmade.example.com",
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{{
						Path: "/", PathType: &pathType,
						Backend: networkingv1.IngressBackend{Service: &networkingv1.IngressServiceBackend{
							Name: resources.UIName(name), Port: networkingv1.ServiceBackendPort{Number: 8080},
						}},
					}},
				}},
			}}},
		}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, foreign) })

		reconcileFor(name)

		Expect(k8sClient.Get(ctx, uiKey(name), &networkingv1.Ingress{})).To(Succeed())
	})

	It("removes internal-frontend when it is disabled", func() {
		name := create(func(c *temporalv1alpha1.TemporalCluster) {
			c.Spec.Services.InternalFrontend = &temporalv1alpha1.InternalFrontendSpec{Enabled: true}
		})
		reconcileFor(name)

		component := resources.ServiceInternalFrontend
		depKey := types.NamespacedName{Name: resources.DeploymentName(name, component), Namespace: "default"}
		svcKey := types.NamespacedName{Name: resources.HeadlessServiceName(name, component), Namespace: "default"}
		pdbKey := types.NamespacedName{Name: resources.PDBName(name, component), Namespace: "default"}
		Expect(k8sClient.Get(ctx, depKey, &appsv1.Deployment{})).To(Succeed())
		Expect(k8sClient.Get(ctx, svcKey, &corev1.Service{})).To(Succeed())
		Expect(k8sClient.Get(ctx, pdbKey, &policyv1.PodDisruptionBudget{})).To(Succeed())
		Expect(clusterFor(name).Status.Services).To(HaveKey(component))

		updateSpec(name, func(c *temporalv1alpha1.TemporalCluster) { c.Spec.Services.InternalFrontend.Enabled = false })
		reconcileFor(name)

		expectGone(depKey, &appsv1.Deployment{})
		expectGone(svcKey, &corev1.Service{})
		expectGone(pdbKey, &policyv1.PodDisruptionBudget{})
		Expect(clusterFor(name).Status.Services).NotTo(HaveKey(component))
		// The always-on services are untouched.
		Expect(k8sClient.Get(ctx, types.NamespacedName{
			Name: resources.DeploymentName(name, resources.ServiceFrontend), Namespace: "default",
		}, &appsv1.Deployment{})).To(Succeed())
	})

	It("removes the mTLS and UI client Certificates when mTLS is turned off", func() {
		name := create(func(c *temporalv1alpha1.TemporalCluster) {
			c.Spec.UI = &temporalv1alpha1.UISpec{Enabled: true}
			c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{
				Provider:  "cert-manager",
				IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca-issuer"},
			}
		})
		reconcileFor(name)

		certKeys := []types.NamespacedName{
			{Name: resources.InternodeCertName(name), Namespace: "default"},
			{Name: resources.FrontendCertName(name), Namespace: "default"},
			{Name: resources.UIClientCertName(name), Namespace: "default"},
		}
		for _, key := range certKeys {
			Expect(k8sClient.Get(ctx, key, &certmanagerv1.Certificate{})).To(Succeed())
		}
		Expect(meta.FindStatusCondition(clusterFor(name).Status.Conditions, temporalv1alpha1.ConditionMTLSReady)).NotTo(BeNil())

		updateSpec(name, func(c *temporalv1alpha1.TemporalCluster) { c.Spec.MTLS = nil })
		reconcileFor(name)

		for _, key := range certKeys {
			expectGone(key, &certmanagerv1.Certificate{})
		}
		Expect(meta.FindStatusCondition(clusterFor(name).Status.Conditions, temporalv1alpha1.ConditionMTLSReady)).To(BeNil())
		Expect(k8sClient.Get(ctx, uiKey(name), &appsv1.Deployment{})).To(Succeed())
	})

	It("removes the ServiceMonitor when it is disabled", func() {
		name := create(func(c *temporalv1alpha1.TemporalCluster) {
			c.Spec.Metrics = &temporalv1alpha1.MetricsSpec{
				Enabled:        true,
				ServiceMonitor: &temporalv1alpha1.ServiceMonitorSpec{Enabled: true},
			}
		})
		reconcileFor(name)

		sm := &unstructured.Unstructured{}
		sm.SetGroupVersionKind(resources.ServiceMonitorGVK)
		smKey := types.NamespacedName{Name: resources.ServiceMonitorName(name), Namespace: "default"}
		Expect(k8sClient.Get(ctx, smKey, sm)).To(Succeed())

		updateSpec(name, func(c *temporalv1alpha1.TemporalCluster) { c.Spec.Metrics.ServiceMonitor.Enabled = false })
		reconcileFor(name)

		expectGone(smKey, sm)
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
