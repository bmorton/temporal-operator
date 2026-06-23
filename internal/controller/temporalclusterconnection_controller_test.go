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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// fakeRemoteClient records remote-cluster operations against an in-memory view.
// It is keyed (by the test factory) on the frontend address it was dialed with,
// i.e. it represents one local peer's operator API. addrNames maps a dialed
// frontend address to the replication cluster name that lives behind it so
// ListRemoteClusters can return realistic Names (as the real Temporal API does,
// having discovered the remote's identity during UpsertRemoteCluster).
type fakeRemoteClient struct {
	upserts  []string
	removes  []string
	view     map[string]temporal.RemoteClusterInfo // key: address
	addrName map[string]string                     // address -> cluster name
}

func (f *fakeRemoteClient) ListRemoteClusters(_ context.Context) ([]temporal.RemoteClusterInfo, error) {
	out := make([]temporal.RemoteClusterInfo, 0, len(f.view))
	for _, info := range f.view {
		out = append(out, info)
	}
	return out, nil
}

func (f *fakeRemoteClient) UpsertRemoteCluster(_ context.Context, frontendAddress string, enableConnection bool) error {
	f.upserts = append(f.upserts, frontendAddress)
	if f.view == nil {
		f.view = map[string]temporal.RemoteClusterInfo{}
	}
	f.view[frontendAddress] = temporal.RemoteClusterInfo{
		Name:              f.addrName[frontendAddress],
		Address:           frontendAddress,
		ConnectionEnabled: enableConnection,
	}
	return nil
}

func (f *fakeRemoteClient) RemoveRemoteCluster(_ context.Context, name string) error {
	f.removes = append(f.removes, name)
	for addr, info := range f.view {
		if info.Name == name {
			delete(f.view, addr)
		}
	}
	return nil
}

func (f *fakeRemoteClient) Close() error { return nil }

var _ = Describe("TemporalClusterConnection reconciler", func() {
	ctx := context.Background()
	var counter int
	var fakes map[string]*fakeRemoteClient // key: dialed address
	var addrName map[string]string         // dialed address -> cluster name

	var factory temporal.RemoteClusterClientFactory = func(_ context.Context, address string, _ *tls.Config) (temporal.RemoteClusterClient, error) {
		f := fakes[address]
		if f == nil {
			f = &fakeRemoteClient{view: map[string]temporal.RemoteClusterInfo{}, addrName: addrName}
			fakes[address] = f
		}
		return f, nil
	}

	reconcileConn := func(name string) {
		r := &TemporalClusterConnectionReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), ClientFactory: factory}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())
	}

	// readyConnCluster creates a Ready TemporalCluster whose
	// clusterMetadata.currentClusterName is set, returning the k8s object name.
	readyConnCluster := func(clusterName string) string {
		counter++
		name := fmt.Sprintf("conn-cluster-%d", counter)
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
		// Record the address -> replication name mapping for the fakes.
		addrName[frontendAddress(c)] = clusterName
		return name
	}

	BeforeEach(func() {
		fakes = map[string]*fakeRemoteClient{}
		addrName = map[string]string{}
	})

	It("registers each local peer as a remote on the other", func() {
		clusterA := readyConnCluster("cluster-a")
		clusterB := readyConnCluster("cluster-b")

		// Frontend addresses we expect the fakes to be keyed on / upserted with.
		cA := &temporalv1alpha1.TemporalCluster{}
		cB := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterA, Namespace: "default"}, cA)).To(Succeed())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterB, Namespace: "default"}, cB)).To(Succeed())
		addrA := frontendAddress(cA)
		addrB := frontendAddress(cB)

		connName := fmt.Sprintf("conn-%d", counter)
		conn := &temporalv1alpha1.TemporalClusterConnection{
			ObjectMeta: metav1.ObjectMeta{Name: connName, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalClusterConnectionSpec{
				Peers: []temporalv1alpha1.ClusterConnectionPeer{
					{Name: "cluster-a", ClusterRef: &temporalv1alpha1.ClusterReference{Name: clusterA}},
					{Name: "cluster-b", ClusterRef: &temporalv1alpha1.ClusterReference{Name: clusterB}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, conn)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, conn) })

		reconcileConn(connName) // adds finalizer
		reconcileConn(connName) // registers remotes

		// cluster-a's operator client should have upserted cluster-b's frontend.
		Expect(fakes).To(HaveKey(addrA))
		Expect(fakes[addrA].upserts).To(ContainElement(addrB))
		// cluster-b's operator client should have upserted cluster-a's frontend.
		Expect(fakes).To(HaveKey(addrB))
		Expect(fakes[addrB].upserts).To(ContainElement(addrA))

		got := &temporalv1alpha1.TemporalClusterConnection{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: connName, Namespace: "default"}, got)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeTrue())
		Expect(got.Status.Peers).To(HaveLen(2))
	})

	It("does not re-upsert when the remote already matches desired state", func() {
		clusterA := readyConnCluster("cluster-a")
		clusterB := readyConnCluster("cluster-b")
		connName := fmt.Sprintf("conn-idem-%d", counter)
		conn := &temporalv1alpha1.TemporalClusterConnection{
			ObjectMeta: metav1.ObjectMeta{Name: connName, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalClusterConnectionSpec{
				Peers: []temporalv1alpha1.ClusterConnectionPeer{
					{Name: "cluster-a", ClusterRef: &temporalv1alpha1.ClusterReference{Name: clusterA}},
					{Name: "cluster-b", ClusterRef: &temporalv1alpha1.ClusterReference{Name: clusterB}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, conn)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, conn) })

		reconcileConn(connName) // finalizer
		reconcileConn(connName) // first registration

		cA := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterA, Namespace: "default"}, cA)).To(Succeed())
		addrA := frontendAddress(cA)
		countAfterFirst := len(fakes[addrA].upserts)

		reconcileConn(connName) // second registration -> no new upserts
		Expect(fakes[addrA].upserts).To(HaveLen(countAfterFirst))
	})

	It("connects a single local peer paired with an external peer", func() {
		clusterA := readyConnCluster("cluster-a")
		cA := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: clusterA, Namespace: "default"}, cA)).To(Succeed())
		addrA := frontendAddress(cA)

		// External peer reachable at a fixed frontend address. Teach the fakes
		// the address -> cluster name mapping so the local peer's
		// ListRemoteClusters view reports the external remote by name once it
		// has been upserted (mirroring the real Temporal API).
		extAddr := "cluster-ext.example.com:7233"
		addrName[extAddr] = "cluster-ext"

		connName := fmt.Sprintf("conn-ext-%d", counter)
		conn := &temporalv1alpha1.TemporalClusterConnection{
			ObjectMeta: metav1.ObjectMeta{Name: connName, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalClusterConnectionSpec{
				Peers: []temporalv1alpha1.ClusterConnectionPeer{
					{Name: "cluster-a", ClusterRef: &temporalv1alpha1.ClusterReference{Name: clusterA}},
					{Name: "cluster-ext", FrontendAddress: extAddr},
				},
			},
		}
		Expect(k8sClient.Create(ctx, conn)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, conn) })

		reconcileConn(connName) // adds finalizer
		reconcileConn(connName) // registers the external remote on the local peer

		// The local peer's operator client registered the external frontend.
		Expect(fakes).To(HaveKey(addrA))
		Expect(fakes[addrA].upserts).To(ContainElement(extAddr))

		got := &temporalv1alpha1.TemporalClusterConnection{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: connName, Namespace: "default"}, got)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, temporalv1alpha1.ConditionReady)).To(BeTrue())

		// The local peer must be Connected based on its own registration, even
		// though no other local peer can confirm it.
		var localStatus *temporalv1alpha1.PeerConnectionStatus
		for i := range got.Status.Peers {
			if got.Status.Peers[i].Name == "cluster-a" {
				localStatus = &got.Status.Peers[i]
			}
		}
		Expect(localStatus).NotTo(BeNil())
		Expect(localStatus.Connected).To(BeTrue())
	})

	It("removes the finalizer on deletion even when a peer cannot be resolved", func() {
		// A local peer whose TLS material cannot be resolved (mTLS enabled but
		// the internode cert secret is absent) makes resolveTarget fail with a
		// non-NotFound error. Deletion must still proceed.
		counter++
		clusterName := fmt.Sprintf("conn-cluster-%d", counter)
		spec := validClusterSpec("1.31.1")
		spec.MTLS = &temporalv1alpha1.MTLSSpec{
			Provider:  "cert-manager",
			IssuerRef: &temporalv1alpha1.IssuerReference{Name: "test-issuer"},
		}
		cluster := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: clusterName, Namespace: "default"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, cluster) })

		connName := fmt.Sprintf("conn-del-%d", counter)
		conn := &temporalv1alpha1.TemporalClusterConnection{
			ObjectMeta: metav1.ObjectMeta{
				Name:       connName,
				Namespace:  "default",
				Finalizers: []string{clusterConnectionFinalizer},
			},
			Spec: temporalv1alpha1.TemporalClusterConnectionSpec{
				Peers: []temporalv1alpha1.ClusterConnectionPeer{
					{Name: "cluster-a", ClusterRef: &temporalv1alpha1.ClusterReference{Name: clusterName}},
					{Name: "cluster-ext", FrontendAddress: "cluster-ext.example.com:7233"},
				},
			},
		}
		Expect(k8sClient.Create(ctx, conn)).To(Succeed())

		// Trigger deletion: the finalizer keeps the object alive.
		Expect(k8sClient.Delete(ctx, conn)).To(Succeed())

		reconcileConn(connName) // must remove the finalizer without erroring

		got := &temporalv1alpha1.TemporalClusterConnection{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: connName, Namespace: "default"}, got)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
})
