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
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
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

func markCertReady(ctx context.Context, name string) {
	cert := &certmanagerv1.Certificate{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, cert)).To(Succeed())
	cert.Status.Conditions = []certmanagerv1.CertificateCondition{
		{Type: certmanagerv1.CertificateConditionReady, Status: cmmeta.ConditionTrue},
	}
	Expect(k8sClient.Status().Update(ctx, cert)).To(Succeed())
}

var _ = Describe("TemporalCluster mTLS reconciler", func() {
	ctx := context.Background()
	var counter int

	readyVersions := map[string]string{
		"temporal":            "1.12",
		"temporal_visibility": "1.12",
	}

	BeforeEach(func() {
		_ = k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "temporal-store", Namespace: "default"},
			Data:       map[string][]byte{"password": []byte("pw")},
		})
	})

	newMTLSCluster := func() string {
		counter++
		name := fmt.Sprintf("mtls-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       validClusterSpec("1.31.1"),
		}
		c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{
			Provider:  "cert-manager",
			IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca-issuer"},
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		return name
	}

	reconcile := func(name string) {
		r := &TemporalClusterReconciler{
			Client:         k8sClient,
			Scheme:         k8sClient.Scheme(),
			BackendFactory: fakeBackendFactory(nil, readyVersions),
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())
	}

	It("creates internode and frontend Certificates", func() {
		name := newMTLSCluster()
		reconcile(name)

		internode := &certmanagerv1.Certificate{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.InternodeCertName(name), Namespace: "default"}, internode)).To(Succeed())
		Expect(internode.Spec.IssuerRef.Name).To(Equal("ca-issuer"))
		Expect(internode.Spec.DNSNames).NotTo(BeEmpty())

		frontend := &certmanagerv1.Certificate{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.FrontendCertName(name), Namespace: "default"}, frontend)).To(Succeed())

		By("reporting MTLSReady=False until certificates are issued")
		c := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, c)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(c.Status.Conditions, temporalv1alpha1.ConditionMTLSReady)).To(BeFalse())
	})

	It("mounts internode certs into service pods", func() {
		name := newMTLSCluster()
		reconcile(name)

		dep := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(name, "frontend"), Namespace: "default"}, dep)).To(Succeed())

		volNames := map[string]bool{}
		for _, v := range dep.Spec.Template.Spec.Volumes {
			volNames[v.Name] = true
		}
		Expect(volNames).To(HaveKey("internode-certs"))
		Expect(volNames).To(HaveKey("frontend-certs"))
	})

	It("reports MTLSReady=True and Ready=True once certificates are issued and services are up", func() {
		name := newMTLSCluster()
		reconcile(name)

		markCertReady(ctx, resources.InternodeCertName(name))
		markCertReady(ctx, resources.FrontendCertName(name))

		for _, svc := range []string{"frontend", "history", "matching", "worker"} {
			dep := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.DeploymentName(name, svc), Namespace: "default"}, dep)).To(Succeed())
			dep.Status.Replicas = 1
			dep.Status.ReadyReplicas = 1
			Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())
		}

		reconcile(name)

		c := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, c)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(c.Status.Conditions, temporalv1alpha1.ConditionMTLSReady)).To(BeTrue())
		Expect(meta.IsStatusConditionTrue(c.Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeTrue())
	})
})

var _ = Describe("TemporalClusterClient reconciler", func() {
	ctx := context.Background()
	var counter int

	newClusterWithMTLS := func() string {
		counter++
		name := fmt.Sprintf("cc-cluster-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       validClusterSpec("1.31.1"),
		}
		c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{
			Provider:  "cert-manager",
			IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca-issuer"},
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		return name
	}

	reconcileClient := func(name string) {
		r := &TemporalClusterClientReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())
	}

	It("issues a client certificate and reports readiness", func() {
		cluster := newClusterWithMTLS()
		ccName := fmt.Sprintf("client-%d", counter)
		cc := &temporalv1alpha1.TemporalClusterClient{
			ObjectMeta: metav1.ObjectMeta{Name: ccName, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalClusterClientSpec{
				ClusterRef: temporalv1alpha1.ClusterReference{Name: cluster},
				SecretName: ccName + "-creds",
			},
		}
		Expect(k8sClient.Create(ctx, cc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, cc) })

		reconcileClient(ccName)

		cert := &certmanagerv1.Certificate{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ccName, Namespace: "default"}, cert)).To(Succeed())
		Expect(cert.Spec.SecretName).To(Equal(ccName + "-creds"))

		got := &temporalv1alpha1.TemporalClusterClient{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ccName, Namespace: "default"}, got)).To(Succeed())
		Expect(got.Status.SecretRef).NotTo(BeNil())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeFalse())

		markCertReady(ctx, ccName)
		reconcileClient(ccName)

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ccName, Namespace: "default"}, got)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeTrue())
	})

	It("reports not-ready when the cluster has no mTLS", func() {
		counter++
		clusterName := fmt.Sprintf("nomtls-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: "default"},
			Spec:       validClusterSpec("1.31.1"),
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })

		ccName := fmt.Sprintf("client-nomtls-%d", counter)
		cc := &temporalv1alpha1.TemporalClusterClient{
			ObjectMeta: metav1.ObjectMeta{Name: ccName, Namespace: "default"},
			Spec:       temporalv1alpha1.TemporalClusterClientSpec{ClusterRef: temporalv1alpha1.ClusterReference{Name: clusterName}},
		}
		Expect(k8sClient.Create(ctx, cc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, cc) })

		reconcileClient(ccName)

		got := &temporalv1alpha1.TemporalClusterClient{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ccName, Namespace: "default"}, got)).To(Succeed())
		cond := meta.FindStatusCondition(got.Status.Conditions, temporalv1alpha1.ConditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("ClusterMTLSDisabled"))
	})

	It("reports not-ready when the clusterRef targets a TemporalDevServer", func() {
		counter++
		ccName := fmt.Sprintf("client-devserver-%d", counter)
		cc := &temporalv1alpha1.TemporalClusterClient{
			ObjectMeta: metav1.ObjectMeta{Name: ccName, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalClusterClientSpec{
				ClusterRef: temporalv1alpha1.ClusterReference{
					Name: "some-dev-server",
					Kind: temporalv1alpha1.ClusterKindTemporalDevServer,
				},
			},
		}
		Expect(k8sClient.Create(ctx, cc)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, cc) })

		reconcileClient(ccName)

		got := &temporalv1alpha1.TemporalClusterClient{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ccName, Namespace: "default"}, got)).To(Succeed())
		cond := meta.FindStatusCondition(got.Status.Conditions, temporalv1alpha1.ConditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("DevServerUnsupported"))
	})
})
