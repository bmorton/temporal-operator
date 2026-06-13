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
	"k8s.io/apimachinery/pkg/types"
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

	var cluster temporalv1alpha1.TemporalCluster
	clusterKey := types.NamespacedName{Namespace: ns.Namespace, Name: ns.Spec.ClusterRef.Name}
	if err := r.Get(ctx, clusterKey, &cluster); err != nil {
		r.setReady(&ns, metav1.ConditionFalse, "ClusterNotFound", "referenced TemporalCluster not found")
		return ctrl.Result{RequeueAfter: namespaceDriftRequeue}, r.statusUpdate(ctx, &ns)
	}

	tc, err := r.clientFactory()(ctx, frontendAddress(&cluster), nil)
	if err != nil {
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

	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionReady) {
		r.setReady(&ns, metav1.ConditionFalse, "ClusterNotReady", "waiting for the TemporalCluster to become ready")
		return ctrl.Result{RequeueAfter: 15 * time.Second}, r.statusUpdate(ctx, &ns)
	}

	info, err := r.ensureRegistered(ctx, &ns, tc)
	if err != nil {
		return ctrl.Result{}, err
	}

	now := metav1.Now()
	ns.Status.Registered = true
	ns.Status.LastUpdated = &now
	if info != nil {
		ns.Status.NamespaceID = info.ID
	}
	r.setReady(&ns, metav1.ConditionTrue, "Registered", "namespace is registered")
	return ctrl.Result{RequeueAfter: namespaceDriftRequeue}, r.statusUpdate(ctx, &ns)
}

// ensureRegistered registers the namespace if missing, or reconciles drift.
func (r *TemporalNamespaceReconciler) ensureRegistered(ctx context.Context, ns *temporalv1alpha1.TemporalNamespace, tc temporal.NamespaceClient) (*temporal.NamespaceInfo, error) {
	log := logf.FromContext(ctx)
	params := namespaceParams(ns)

	info, err := tc.Describe(ctx, params.Name)
	switch {
	case errors.Is(err, temporal.ErrNamespaceNotFound):
		if err := tc.Register(ctx, params); err != nil {
			return nil, fmt.Errorf("registering namespace: %w", err)
		}
		log.Info("registered namespace", "namespace", params.Name)
		return tc.Describe(ctx, params.Name)
	case err != nil:
		return nil, fmt.Errorf("describing namespace: %w", err)
	}

	if ns.Spec.DriftDetection != "ignore" && namespaceDrifted(params, info) {
		if err := tc.Update(ctx, params); err != nil {
			return nil, fmt.Errorf("updating namespace: %w", err)
		}
		log.Info("updated namespace to resolve drift", "namespace", params.Name)
	}
	return info, nil
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
	}
}

func namespaceDrifted(params temporal.NamespaceParams, info *temporal.NamespaceInfo) bool {
	if info == nil {
		return true
	}
	return params.Description != info.Description ||
		params.OwnerEmail != info.OwnerEmail ||
		params.RetentionPeriod != info.RetentionPeriod
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
