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
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

const clusterConnectionFinalizer = "temporal.bmor10.com/clusterconnection"

// TemporalClusterConnectionReconciler automates remote-cluster connection
// registration between the peers of a TemporalClusterConnection. For every
// local, ready peer it dials the Temporal operator API and upserts the other
// peers as remote clusters, driving the replication group toward the declared
// topology.
type TemporalClusterConnectionReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// ClientFactory builds the Temporal remote-cluster client; injectable for tests.
	ClientFactory temporal.RemoteClusterClientFactory
}

// resolvedPeer is a peer resolved to a connectable frontend.
type resolvedPeer struct {
	name    string
	address string
	tls     *tls.Config
	// local is true when the peer references an in-cluster TemporalCluster the
	// operator can dial to manage its remote-cluster connections.
	local bool
	// ready is true when a local peer's TemporalCluster is Ready, or for
	// external peers (which the operator assumes are reachable).
	ready bool
	// enable is the desired connection-enabled state (default true).
	enable bool
}

// localPeerState holds a dialed local peer's operator client and its current
// view of registered remote clusters (keyed by remote cluster name).
type localPeerState struct {
	peer    resolvedPeer
	client  temporal.RemoteClusterClient
	remotes map[string]temporal.RemoteClusterInfo
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterconnections,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterconnections/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterconnections/finalizers,verbs=update

// Reconcile registers each local, ready peer as a remote cluster on the others.
func (r *TemporalClusterConnectionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var conn temporalv1alpha1.TemporalClusterConnection
	if err := r.Get(ctx, req.NamespacedName, &conn); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	peers, err := r.resolvePeers(ctx, &conn)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Handle deletion: best-effort de-registration, then unblock GC.
	if !conn.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, &conn, peers)
	}

	if !controllerutil.ContainsFinalizer(&conn, clusterConnectionFinalizer) {
		controllerutil.AddFinalizer(&conn, clusterConnectionFinalizer)
		if err := r.Update(ctx, &conn); err != nil {
			return ctrl.Result{}, err
		}
	}

	statuses, allReady, reason, message := r.reconcilePeers(ctx, peers)
	conn.Status.Peers = statuses
	if allReady {
		r.setReady(&conn, metav1.ConditionTrue, temporalv1alpha1.ReasonPeersConnected, "all replication peers are connected")
	} else {
		r.setReady(&conn, metav1.ConditionFalse, reason, message)
	}
	return ctrl.Result{RequeueAfter: namespaceDriftRequeue}, r.statusUpdate(ctx, &conn)
}

// resolvePeers resolves each spec peer to a connectable frontend. Local peers
// (ClusterRef set) are resolved via resolveTarget; a missing local target is
// reported as not-ready rather than failing the reconcile (eventual
// consistency). External peers (FrontendAddress set) are taken at face value.
func (r *TemporalClusterConnectionReconciler) resolvePeers(ctx context.Context, conn *temporalv1alpha1.TemporalClusterConnection) ([]resolvedPeer, error) {
	out := make([]resolvedPeer, 0, len(conn.Spec.Peers))
	for _, p := range conn.Spec.Peers {
		rp := resolvedPeer{
			name:   p.Name,
			enable: p.EnableConnection == nil || *p.EnableConnection,
		}
		if p.ClusterRef != nil {
			rp.local = true
			target, err := resolveTarget(ctx, r.Client, conn.Namespace, *p.ClusterRef)
			if err != nil {
				if errors.Is(err, ErrTargetNotFound) {
					// Local peer not yet present: leave address empty / not ready.
					out = append(out, rp)
					continue
				}
				return nil, err
			}
			rp.address = target.Address
			rp.tls = target.TLSConfig
			rp.ready = target.Ready
		} else {
			// External peer. TLS-from-secret is deferred to a follow-up.
			// TODO(external mTLS): load p.TLSSecretRef into *tls.Config and pass
			// it to the factory; treated as plaintext for now.
			rp.address = p.FrontendAddress
			rp.tls = nil
			rp.ready = true
		}
		out = append(out, rp)
	}
	return out, nil
}

// dialLocals dials every local, ready peer that has a resolved address and
// loads its current remote-cluster view. Peers that cannot be dialed are
// omitted from the result (they are reported unreachable by the caller). The
// caller is responsible for closing the returned clients.
func (r *TemporalClusterConnectionReconciler) dialLocals(ctx context.Context, peers []resolvedPeer) map[string]*localPeerState {
	log := logf.FromContext(ctx)
	locals := map[string]*localPeerState{}
	for _, p := range peers {
		if !p.local || !p.ready || p.address == "" {
			continue
		}
		c, err := r.clientFactory()(ctx, p.address, p.tls)
		if err != nil {
			log.Error(err, "dialing peer frontend", "peer", p.name, "address", p.address)
			continue
		}
		list, err := c.ListRemoteClusters(ctx)
		if err != nil {
			log.Error(err, "listing remote clusters", "peer", p.name)
			_ = c.Close()
			continue
		}
		remotes := make(map[string]temporal.RemoteClusterInfo, len(list))
		for _, info := range list {
			remotes[info.Name] = info
		}
		locals[p.name] = &localPeerState{peer: p, client: c, remotes: remotes}
	}
	return locals
}

// reconcilePeers performs the upsert loop and computes per-peer status plus the
// top-level readiness verdict.
//
// Definition of "Connected": a peer P is Connected when there is at least one
// other reachable local peer and P appears as an enabled remote cluster on
// every other reachable local peer. "Reachable" means: for a local peer, the
// operator could dial its frontend; for an external peer, it appears as a
// remote on at least one local peer. The top-level Ready condition is true when
// every peer that is not intentionally disabled (EnableConnection=false) is
// both reachable and connected.
func (r *TemporalClusterConnectionReconciler) reconcilePeers(ctx context.Context, peers []resolvedPeer) ([]temporalv1alpha1.PeerConnectionStatus, bool, string, string) {
	locals := r.dialLocals(ctx, peers)
	defer func() {
		for _, l := range locals {
			_ = l.client.Close()
		}
	}()

	r.upsertRemotes(ctx, peers, locals)
	reachable := computeReachable(peers, locals)
	return r.buildStatuses(peers, locals, reachable)
}

// upsertRemotes registers every other peer as a remote on each dialed local
// peer when it is missing or its enabled-state differs from the desired state.
func (r *TemporalClusterConnectionReconciler) upsertRemotes(ctx context.Context, peers []resolvedPeer, locals map[string]*localPeerState) {
	log := logf.FromContext(ctx)
	for name, l := range locals {
		for _, other := range peers {
			if other.name == name || other.address == "" {
				continue
			}
			existing, ok := l.remotes[other.name]
			if ok && existing.ConnectionEnabled == other.enable {
				continue
			}
			if err := l.client.UpsertRemoteCluster(ctx, other.address, other.enable); err != nil {
				log.Error(err, "upserting remote cluster", "on", name, "remote", other.name)
				continue
			}
			l.remotes[other.name] = temporal.RemoteClusterInfo{
				Name:              other.name,
				Address:           other.address,
				ConnectionEnabled: other.enable,
			}
		}
	}
}

// computeReachable reports per-peer reachability: a local peer is reachable when
// it was dialed; an external peer is reachable when it appears as a remote on
// at least one dialed local peer.
func computeReachable(peers []resolvedPeer, locals map[string]*localPeerState) map[string]bool {
	reachable := map[string]bool{}
	for _, p := range peers {
		if p.local {
			_, reachable[p.name] = locals[p.name]
			continue
		}
		for _, l := range locals {
			if _, ok := l.remotes[p.name]; ok {
				reachable[p.name] = true
				break
			}
		}
	}
	return reachable
}

// buildStatuses computes connectivity and the top-level readiness verdict.
func (r *TemporalClusterConnectionReconciler) buildStatuses(peers []resolvedPeer, locals map[string]*localPeerState, reachable map[string]bool) ([]temporalv1alpha1.PeerConnectionStatus, bool, string, string) {
	statuses := make([]temporalv1alpha1.PeerConnectionStatus, 0, len(peers))
	allReady := true
	reason := temporalv1alpha1.ReasonPeersConnected
	message := "all replication peers are connected"
	for _, p := range peers {
		connected := peerConnected(p, locals)
		st := temporalv1alpha1.PeerConnectionStatus{
			Name:      p.name,
			Reachable: reachable[p.name],
			Connected: connected,
		}
		switch {
		case !reachable[p.name]:
			st.Message = "peer frontend is unreachable or not ready"
			if p.enable {
				allReady = false
				reason = temporalv1alpha1.ReasonPeerUnreachable
				message = fmt.Sprintf("peer %q is unreachable", p.name)
			}
		case !p.enable:
			st.Message = "connection intentionally disabled"
		case !connected:
			st.Message = "peer is not yet registered as an enabled remote on all peers"
			allReady = false
			if reason == temporalv1alpha1.ReasonPeersConnected {
				reason = temporalv1alpha1.ReasonReplicationDrift
				message = fmt.Sprintf("peer %q is not connected on all peers", p.name)
			}
		default:
			st.Message = "connected"
		}
		statuses = append(statuses, st)
	}
	return statuses, allReady, reason, message
}

// peerConnected reports whether p appears as an enabled remote cluster on every
// other reachable local peer (see reconcilePeers for the full definition).
func peerConnected(p resolvedPeer, locals map[string]*localPeerState) bool {
	others := 0
	for _, l := range locals {
		if l.peer.name == p.name {
			continue
		}
		others++
		info, ok := l.remotes[p.name]
		if !ok || !info.ConnectionEnabled {
			return false
		}
	}
	return others > 0
}

// reconcileDelete best-effort removes this connection's peers as remote
// clusters from every reachable local peer, then removes the finalizer. If
// peers are unreachable, the finalizer is still removed to unblock GC.
func (r *TemporalClusterConnectionReconciler) reconcileDelete(ctx context.Context, conn *temporalv1alpha1.TemporalClusterConnection, peers []resolvedPeer) error {
	log := logf.FromContext(ctx)
	if !controllerutil.ContainsFinalizer(conn, clusterConnectionFinalizer) {
		return nil
	}

	locals := r.dialLocals(ctx, peers)
	for name, l := range locals {
		for _, other := range peers {
			if other.name == name {
				continue
			}
			if err := l.client.RemoveRemoteCluster(ctx, other.name); err != nil {
				log.Error(err, "removing remote cluster", "on", name, "remote", other.name)
			}
		}
		_ = l.client.Close()
	}

	controllerutil.RemoveFinalizer(conn, clusterConnectionFinalizer)
	return r.Update(ctx, conn)
}

func (r *TemporalClusterConnectionReconciler) clientFactory() temporal.RemoteClusterClientFactory {
	if r.ClientFactory != nil {
		return r.ClientFactory
	}
	return temporal.NewRemoteClusterClient
}

func (r *TemporalClusterConnectionReconciler) setReady(conn *temporalv1alpha1.TemporalClusterConnection, status metav1.ConditionStatus, reason, message string) {
	conn.Status.ObservedGeneration = conn.Generation
	meta.SetStatusCondition(&conn.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: conn.Generation,
	})
}

func (r *TemporalClusterConnectionReconciler) statusUpdate(ctx context.Context, conn *temporalv1alpha1.TemporalClusterConnection) error {
	return client.IgnoreNotFound(r.Status().Update(ctx, conn))
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalClusterConnectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalClusterConnection{}).
		Named("temporalclusterconnection").
		Complete(r)
}
