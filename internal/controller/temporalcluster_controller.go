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

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/persistence"
)

// TemporalClusterReconciler reconciles a TemporalCluster object.
type TemporalClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// Prober and SchemaInspector are injectable for testing; when nil the
	// default Postgres SQL implementation is used.
	Prober          persistence.Prober
	SchemaInspector persistence.SchemaInspector
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile drives the TemporalCluster toward its desired state. At this
// milestone it reconciles persistence (reachability + schema) and reports the
// remaining work as not-yet-implemented via the Ready condition.
func (r *TemporalClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var cluster temporalv1alpha1.TemporalCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		// Ignore not-found errors: the object was deleted.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("reconciling TemporalCluster", "version", cluster.Spec.Version)

	result, err := r.reconcilePersistence(ctx, &cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             temporalv1alpha1.ReasonNotImplemented,
		Message:            "service deployment not yet implemented",
		ObservedGeneration: cluster.Generation,
	})
	cluster.Status.ObservedGeneration = cluster.Generation

	if err := r.Status().Update(ctx, &cluster); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	return result, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalCluster{}).
		Owns(&batchv1.Job{}).
		Named("temporalcluster").
		Complete(r)
}
