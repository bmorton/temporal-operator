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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

var _ = Describe("TemporalClusterProxy reconciler", func() {
	ctx := context.Background()
	var counter int
	var fakes map[string]*fakeRemoteClient // key: dialed address

	var factory temporal.RemoteClusterClientFactory = func(_ context.Context, address string, _ *tls.Config) (temporal.RemoteClusterClient, error) {
		f := fakes[address]
		if f == nil {
			f = &fakeRemoteClient{view: map[string]temporal.RemoteClusterInfo{}, addrName: map[string]string{}}
			fakes[address] = f
		}
		return f, nil
	}

	reconcileProxy := func(name string) {
		r := &TemporalClusterProxyReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ClientFactory: factory}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())
	}

	// readyProxyCluster creates a Ready TemporalCluster, returning its k8s name.
	readyProxyCluster := func(clusterName string) string {
		counter++
		name := fmt.Sprintf("proxy-cluster-%d", counter)
		spec := validClusterSpec("1.31.1")
		spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
			EnableGlobalNamespace: true,
			CurrentClusterName:    clusterName,
		}
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       spec,
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
		fakes = map[string]*fakeRemoteClient{}
	})

	It("deploys the proxy and sets conditions", func() {
		localCluster := readyProxyCluster("cluster-a")

		tlsSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "mux-tls", Namespace: "default"},
			Data:       map[string][]byte{"tls.crt": []byte("x"), "tls.key": []byte("y"), "ca.crt": []byte("z")},
		}
		Expect(k8sClient.Create(ctx, tlsSecret)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, tlsSecret) })

		proxyName := fmt.Sprintf("proxy-%d", counter)
		proxy := &temporalv1alpha1.TemporalClusterProxy{
			ObjectMeta: metav1.ObjectMeta{Name: proxyName, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalClusterProxySpec{
				LocalClusterRef: temporalv1alpha1.ClusterReference{Name: localCluster},
				Peer: temporalv1alpha1.ProxyPeer{
					Name: "cluster-b",
				},
				Mux: temporalv1alpha1.ProxyMux{
					Role:   temporalv1alpha1.ProxyRoleServer,
					Server: &temporalv1alpha1.ProxyMuxServer{ListenPort: 6334},
					TLS: temporalv1alpha1.ProxyMuxTLS{
						Provider:  "secret",
						SecretRef: &temporalv1alpha1.SecretReference{Name: "mux-tls"},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, proxy)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, proxy) })

		reconcileProxy(proxyName) // adds finalizer
		reconcileProxy(proxyName) // renders + applies resources

		got := &temporalv1alpha1.TemporalClusterProxy{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: proxyName, Namespace: "default"}, got)).To(Succeed())

		// Deployment, ConfigMap and Service exist, owned by the proxy CR.
		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.ClusterProxyName(got), Namespace: "default"}, &dep)).To(Succeed())
		Expect(dep.OwnerReferences).To(ContainElement(HaveField("UID", got.UID)))

		var cm corev1.ConfigMap
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.ClusterProxyConfigMapName(got), Namespace: "default"}, &cm)).To(Succeed())
		Expect(cm.OwnerReferences).To(ContainElement(HaveField("UID", got.UID)))

		var svc corev1.Service
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: resources.ClusterProxyServiceName(got), Namespace: "default"}, &svc)).To(Succeed())
		Expect(svc.OwnerReferences).To(ContainElement(HaveField("UID", got.UID)))

		// The ProxyDeployed condition is present.
		cond := meta.FindStatusCondition(got.Status.Conditions, temporalv1alpha1.ConditionProxyDeployed)
		Expect(cond).NotTo(BeNil())
	})
})
