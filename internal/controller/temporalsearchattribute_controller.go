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
	"github.com/bmorton/temporal-operator/internal/temporal"
)

const searchAttributeFinalizer = "temporal.bmor10.com/search-attribute"

// TemporalSearchAttributeReconciler reconciles TemporalSearchAttribute objects.
type TemporalSearchAttributeReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// ClientFactory builds the Temporal search-attribute client; injectable for tests.
	ClientFactory temporal.SearchAttributeClientFactory
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalsearchattributes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalsearchattributes/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalsearchattributes/finalizers,verbs=update

// Reconcile registers or removes a custom search attribute.
func (r *TemporalSearchAttributeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var sa temporalv1alpha1.TemporalSearchAttribute
	if err := r.Get(ctx, req.NamespacedName, &sa); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var cluster temporalv1alpha1.TemporalCluster
	clusterKey := types.NamespacedName{Namespace: sa.Namespace, Name: sa.Spec.ClusterRef.Name}
	if err := r.Get(ctx, clusterKey, &cluster); err != nil {
		if !sa.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.removeFinalizerAndForget(ctx, &sa)
		}
		r.setReady(&sa, metav1.ConditionFalse, "ClusterNotFound", "referenced TemporalCluster not found")
		return ctrl.Result{RequeueAfter: time.Minute}, r.statusUpdate(ctx, &sa)
	}

	tlsConfig, err := clusterTLSConfig(ctx, r.Client, &cluster)
	if err != nil {
		if !sa.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.removeFinalizerAndForget(ctx, &sa)
		}
		return ctrl.Result{}, fmt.Errorf("building temporal client tls: %w", err)
	}
	sac, err := r.clientFactory()(ctx, frontendAddress(&cluster), tlsConfig)
	if err != nil {
		if !sa.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, r.removeFinalizerAndForget(ctx, &sa)
		}
		return ctrl.Result{}, fmt.Errorf("building temporal client: %w", err)
	}
	defer func() { _ = sac.Close() }()

	if !sa.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, &sa, sac)
	}

	if !controllerutil.ContainsFinalizer(&sa, searchAttributeFinalizer) {
		controllerutil.AddFinalizer(&sa, searchAttributeFinalizer)
		if err := r.Update(ctx, &sa); err != nil {
			return ctrl.Result{}, err
		}
	}

	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionReady) {
		r.setReady(&sa, metav1.ConditionFalse, "ClusterNotReady", "waiting for the TemporalCluster to become ready")
		return ctrl.Result{RequeueAfter: 15 * time.Second}, r.statusUpdate(ctx, &sa)
	}

	registered, err := r.ensureRegistered(ctx, &sa, sac)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !registered {
		r.setReady(&sa, metav1.ConditionFalse, temporalv1alpha1.ReasonReconciling, "waiting for the search attribute to become visible")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, r.statusUpdate(ctx, &sa)
	}

	now := metav1.Now()
	sa.Status.Registered = true
	if sa.Status.RegisteredAt == nil {
		sa.Status.RegisteredAt = &now
	}
	r.setReady(&sa, metav1.ConditionTrue, "Registered", "search attribute is registered")
	return ctrl.Result{}, r.statusUpdate(ctx, &sa)
}

// ensureRegistered adds the search attribute if it is not already present and
// reports whether it is visible yet. Registration triggers a system workflow,
// so a freshly added attribute may not be immediately visible; when it is not,
// the caller should requeue.
func (r *TemporalSearchAttributeReconciler) ensureRegistered(ctx context.Context, sa *temporalv1alpha1.TemporalSearchAttribute, sac temporal.SearchAttributeClient) (bool, error) {
	existing, err := sac.List(ctx, sa.Spec.Namespace)
	if err != nil {
		return false, fmt.Errorf("listing search attributes: %w", err)
	}
	if _, ok := existing[sa.Spec.Name]; ok {
		return true, nil
	}

	if err := sac.Add(ctx, sa.Spec.Namespace, sa.Spec.Name, sa.Spec.Type); err != nil {
		return false, fmt.Errorf("adding search attribute: %w", err)
	}
	logf.FromContext(ctx).Info("registered search attribute", "namespace", sa.Spec.Namespace, "name", sa.Spec.Name)

	// Registration triggers a system workflow; the attribute may not be
	// immediately visible. The caller requeues to confirm.
	return r.attributeVisible(ctx, sac, sa), nil
}

func (r *TemporalSearchAttributeReconciler) attributeVisible(ctx context.Context, sac temporal.SearchAttributeClient, sa *temporalv1alpha1.TemporalSearchAttribute) bool {
	attrs, err := sac.List(ctx, sa.Spec.Namespace)
	if err != nil {
		return false
	}
	_, ok := attrs[sa.Spec.Name]
	return ok
}

func (r *TemporalSearchAttributeReconciler) reconcileDelete(ctx context.Context, sa *temporalv1alpha1.TemporalSearchAttribute, sac temporal.SearchAttributeClient) error {
	log := logf.FromContext(ctx)
	if controllerutil.ContainsFinalizer(sa, searchAttributeFinalizer) {
		if sa.Spec.AllowDeletion {
			if err := sac.Remove(ctx, sa.Spec.Namespace, sa.Spec.Name); err != nil {
				return fmt.Errorf("removing search attribute: %w", err)
			}
			log.Info("removed search attribute", "namespace", sa.Spec.Namespace, "name", sa.Spec.Name)
		}
		controllerutil.RemoveFinalizer(sa, searchAttributeFinalizer)
		if err := r.Update(ctx, sa); err != nil {
			return err
		}
	}
	return nil
}

// removeFinalizerAndForget removes the search-attribute finalizer and returns a
// clean result. It is used when the TemporalCluster (or its TLS/client) is
// unreachable during deletion — there is nothing to clean up remotely, so we
// just unblock GC.
func (r *TemporalSearchAttributeReconciler) removeFinalizerAndForget(ctx context.Context, sa *temporalv1alpha1.TemporalSearchAttribute) error {
	if controllerutil.ContainsFinalizer(sa, searchAttributeFinalizer) {
		controllerutil.RemoveFinalizer(sa, searchAttributeFinalizer)
		if err := r.Update(ctx, sa); err != nil {
			return err
		}
	}
	return nil
}

func (r *TemporalSearchAttributeReconciler) clientFactory() temporal.SearchAttributeClientFactory {
	if r.ClientFactory != nil {
		return r.ClientFactory
	}
	return temporal.NewSearchAttributeClient
}

func (r *TemporalSearchAttributeReconciler) setReady(sa *temporalv1alpha1.TemporalSearchAttribute, status metav1.ConditionStatus, reason, message string) {
	sa.Status.ObservedGeneration = sa.Generation
	meta.SetStatusCondition(&sa.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: sa.Generation,
	})
}

func (r *TemporalSearchAttributeReconciler) statusUpdate(ctx context.Context, sa *temporalv1alpha1.TemporalSearchAttribute) error {
	return client.IgnoreNotFound(r.Status().Update(ctx, sa))
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalSearchAttributeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalSearchAttribute{}).
		Named("temporalsearchattribute").
		Complete(r)
}
