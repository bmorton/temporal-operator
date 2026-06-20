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

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"k8s.io/client-go/tools/events"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/persistence"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
)

// TemporalClusterReconciler reconciles a TemporalCluster object.
type TemporalClusterReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder

	// BackendFactory builds datastore backends and is injectable for testing;
	// when nil the default real implementation is used.
	BackendFactory persistence.BackendFactory

	// OperatorImage is the operator's own image, used for inspector Jobs.
	// Populated from OPERATOR_IMAGE env by cmd/main.go.
	OperatorImage string
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates;issuers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

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

	if err := r.reconcileMTLS(ctx, &cluster); err != nil {
		return ctrl.Result{}, err
	}

	serviceVersions := r.reconcileUpgrade(ctx, &cluster)

	if err := r.reconcileServices(ctx, &cluster, serviceVersions); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileUI(ctx, &cluster); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileMonitoring(ctx, &cluster); err != nil {
		return ctrl.Result{}, err
	}

	r.computeReadyAndPhase(&cluster)
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
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&certmanagerv1.Certificate{}).
		Named("temporalcluster").
		Complete(r)
}
