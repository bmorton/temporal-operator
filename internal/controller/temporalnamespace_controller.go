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
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

const namespaceFinalizer = "temporal.bmor10.com/namespace"

// namespaceDriftRequeue is how often a registered namespace is re-checked for drift.
const namespaceDriftRequeue = 5 * time.Minute

// TemporalNamespaceReconciler reconciles TemporalNamespace objects against a
// running Temporal cluster.
type TemporalNamespaceReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// ClientFactory builds the Temporal namespace client; injectable for tests.
	ClientFactory temporal.NamespaceClientFactory
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalnamespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalnamespaces/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalnamespaces/finalizers,verbs=update

// Reconcile registers, updates, or deletes a Temporal namespace.
func (r *TemporalNamespaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var ns temporalv1alpha1.TemporalNamespace
	if err := r.Get(ctx, req.NamespacedName, &ns); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	target, err := resolveTarget(ctx, r.Client, ns.Namespace, ns.Spec.ClusterRef)
	if err != nil {
		if !ns.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.removeFinalizerAndForget(ctx, &ns)
		}
		if errors.Is(err, ErrTargetNotFound) {
			r.setReady(&ns, metav1.ConditionFalse, "ClusterNotFound", "referenced Temporal target not found")
			return ctrl.Result{RequeueAfter: namespaceDriftRequeue}, r.statusUpdate(ctx, &ns)
		}
		return ctrl.Result{}, err
	}

	tc, err := r.clientFactory()(ctx, target.Address, target.TLSConfig)
	if err != nil {
		if !ns.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.removeFinalizerAndForget(ctx, &ns)
		}
		return ctrl.Result{}, fmt.Errorf("building temporal client: %w", err)
	}
	defer func() { _ = tc.Close() }()

	// Handle deletion.
	if !ns.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, &ns, tc)
	}

	if !controllerutil.ContainsFinalizer(&ns, namespaceFinalizer) {
		controllerutil.AddFinalizer(&ns, namespaceFinalizer)
		if err := r.Update(ctx, &ns); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !target.Ready {
		r.setReady(&ns, metav1.ConditionFalse, "ClusterNotReady", "waiting for the Temporal target to become ready")
		return ctrl.Result{RequeueAfter: 15 * time.Second}, r.statusUpdate(ctx, &ns)
	}

	info, failover, err := r.ensureRegistered(ctx, &ns, tc)
	if err != nil {
		return ctrl.Result{}, err
	}

	now := metav1.Now()
	ns.Status.Registered = true
	ns.Status.LastUpdated = &now
	if info != nil {
		ns.Status.NamespaceID = info.ID
	}
	r.applyReplicationStatus(&ns, info, failover)
	r.setReady(&ns, metav1.ConditionTrue, "Registered", "namespace is registered")
	return ctrl.Result{RequeueAfter: namespaceDriftRequeue}, r.statusUpdate(ctx, &ns)
}

// applyReplicationStatus records the observed replication state of a global
// namespace, marking a failover as in progress while the observed active cluster
// has not yet caught up with the desired one.
func (r *TemporalNamespaceReconciler) applyReplicationStatus(ns *temporalv1alpha1.TemporalNamespace, info *temporal.NamespaceInfo, failoverIssued bool) {
	if !ns.Spec.IsGlobal || info == nil {
		return
	}
	repl := &temporalv1alpha1.NamespaceReplicationStatus{
		IsGlobal:           info.IsGlobal,
		ActiveCluster:      info.ActiveCluster,
		Clusters:           info.Clusters,
		FailoverInProgress: ns.Spec.ActiveCluster != "" && info.ActiveCluster != ns.Spec.ActiveCluster,
	}
	// Preserve a prior failover timestamp, then bump it when this reconcile
	// issued a failover.
	if ns.Status.Replication != nil {
		repl.LastFailoverTime = ns.Status.Replication.LastFailoverTime
	}
	if failoverIssued {
		now := metav1.Now()
		repl.LastFailoverTime = &now
	}
	ns.Status.Replication = repl
}

// ensureRegistered registers the namespace if missing, or reconciles drift. It
// returns the latest observed namespace info and whether a declarative failover
// (active-cluster change) was issued during this reconcile.
func (r *TemporalNamespaceReconciler) ensureRegistered(ctx context.Context, ns *temporalv1alpha1.TemporalNamespace, tc temporal.NamespaceClient) (*temporal.NamespaceInfo, bool, error) {
	log := logf.FromContext(ctx)
	params := namespaceParams(ns)

	info, err := tc.Describe(ctx, params.Name)
	switch {
	case errors.Is(err, temporal.ErrNamespaceNotFound):
		if err := tc.Register(ctx, params); err != nil {
			return nil, false, fmt.Errorf("registering namespace: %w", err)
		}
		log.Info("registered namespace", "namespace", params.Name)
		info, err := tc.Describe(ctx, params.Name)
		return info, false, err
	case err != nil:
		return nil, false, fmt.Errorf("describing namespace: %w", err)
	}

	failover := false
	if ns.Spec.DriftDetection != "ignore" && namespaceDrifted(params, info) {
		// Detect a declarative failover before issuing the update so the status
		// timestamp reflects this reconcile.
		failover = params.ActiveCluster != "" && params.ActiveCluster != info.ActiveCluster
		if err := tc.Update(ctx, params); err != nil {
			return nil, false, fmt.Errorf("updating namespace: %w", err)
		}
		if failover {
			log.Info("issued namespace failover", "namespace", params.Name, "activeCluster", params.ActiveCluster)
		} else {
			log.Info("updated namespace to resolve drift", "namespace", params.Name)
		}
		// Re-describe so the reported status reflects the post-update state.
		if refreshed, derr := tc.Describe(ctx, params.Name); derr == nil {
			info = refreshed
		}
	}
	return info, failover, nil
}

func (r *TemporalNamespaceReconciler) reconcileDelete(ctx context.Context, ns *temporalv1alpha1.TemporalNamespace, tc temporal.NamespaceClient) error {
	log := logf.FromContext(ctx)
	if controllerutil.ContainsFinalizer(ns, namespaceFinalizer) {
		if ns.Spec.AllowDeletion {
			if err := tc.Delete(ctx, namespaceParams(ns).Name); err != nil && !errors.Is(err, temporal.ErrNamespaceNotFound) {
				return fmt.Errorf("deleting namespace: %w", err)
			}
			log.Info("deleted temporal namespace", "namespace", namespaceParams(ns).Name)
		}
		controllerutil.RemoveFinalizer(ns, namespaceFinalizer)
		if err := r.Update(ctx, ns); err != nil {
			return err
		}
	}
	return nil
}

// removeFinalizerAndForget removes the namespace finalizer and returns a clean
// result. It is used when the TemporalCluster (or its TLS/client) is
// unreachable during deletion — there is nothing to clean up remotely, so we
// just unblock GC.
func (r *TemporalNamespaceReconciler) removeFinalizerAndForget(ctx context.Context, ns *temporalv1alpha1.TemporalNamespace) error {
	if controllerutil.ContainsFinalizer(ns, namespaceFinalizer) {
		controllerutil.RemoveFinalizer(ns, namespaceFinalizer)
		if err := r.Update(ctx, ns); err != nil {
			return err
		}
	}
	return nil
}

func (r *TemporalNamespaceReconciler) clientFactory() temporal.NamespaceClientFactory {
	if r.ClientFactory != nil {
		return r.ClientFactory
	}
	return temporal.NewNamespaceClient
}

func frontendAddress(cluster *temporalv1alpha1.TemporalCluster) string {
	return fmt.Sprintf("%s.%s.svc:%d",
		resources.FrontendServiceName(cluster.Name), cluster.Namespace,
		temporal.DefaultServicePorts()["frontend"].GRPCPort)
}

func namespaceParams(ns *temporalv1alpha1.TemporalNamespace) temporal.NamespaceParams {
	retention := 72 * time.Hour
	if ns.Spec.RetentionPeriod != nil {
		retention = ns.Spec.RetentionPeriod.Duration
	}
	return temporal.NamespaceParams{
		Name:            ns.Name,
		Description:     ns.Spec.Description,
		OwnerEmail:      ns.Spec.OwnerEmail,
		RetentionPeriod: retention,
		IsGlobal:        ns.Spec.IsGlobal,
		Clusters:        ns.Spec.Clusters,
		ActiveCluster:   ns.Spec.ActiveCluster,
	}
}

func namespaceDrifted(params temporal.NamespaceParams, info *temporal.NamespaceInfo) bool {
	if info == nil {
		return true
	}
	// A change to the desired active cluster (declarative failover) is drift.
	if params.ActiveCluster != "" && params.ActiveCluster != info.ActiveCluster {
		return true
	}
	// For a global namespace, a change to the replication clusters list is drift.
	// Temporal may return clusters in any order, so compare as sets. Only global
	// namespaces declare clusters, so guard on a non-empty desired list to avoid
	// spurious updates for local namespaces.
	if len(params.Clusters) > 0 && !equalStringSets(params.Clusters, info.Clusters) {
		return true
	}
	return params.Description != info.Description ||
		params.OwnerEmail != info.OwnerEmail ||
		params.RetentionPeriod != info.RetentionPeriod
}

// equalStringSets reports whether a and b contain the same strings, ignoring
// order but respecting multiplicity (multiset equality).
func equalStringSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		counts[s]--
		if counts[s] < 0 {
			return false
		}
	}
	return true
}

func (r *TemporalNamespaceReconciler) setReady(ns *temporalv1alpha1.TemporalNamespace, status metav1.ConditionStatus, reason, message string) {
	ns.Status.ObservedGeneration = ns.Generation
	meta.SetStatusCondition(&ns.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: ns.Generation,
	})
}

func (r *TemporalNamespaceReconciler) statusUpdate(ctx context.Context, ns *temporalv1alpha1.TemporalNamespace) error {
	return client.IgnoreNotFound(r.Status().Update(ctx, ns))
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalNamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalNamespace{}).
		Named("temporalnamespace").
		Complete(r)
}
