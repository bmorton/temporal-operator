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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

const migrationProxyImageEnv = "MIGRATION_PROXY_IMAGE"

// migrationRequeue is the steady-state reconcile cadence.
const migrationRequeue = 30 * time.Second

// TemporalMigrationReconciler reconciles TemporalMigration objects.
type TemporalMigrationReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// ProxyImage is the image used for the proxy Deployment (operator image).
	ProxyImage string
	// MigrationClientFactory builds visibility clients; injectable for tests.
	MigrationClientFactory temporal.MigrationClientFactory
	// DrainStableThreshold is the number of consecutive all-zero observations
	// required before declaring Complete. Defaults to 3.
	DrainStableThreshold int

	zeroStreak map[types.NamespacedName]int
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalmigrations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalmigrations/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalmigrations/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps,verbs=get;list;watch;create;update;patch;delete

// Reconcile provisions the proxy and advances the migration phase machine.
func (r *TemporalMigrationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var mig temporalv1alpha1.TemporalMigration
	if err := r.Get(ctx, req.NamespacedName, &mig); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var cluster temporalv1alpha1.TemporalCluster
	clusterKey := types.NamespacedName{Namespace: mig.Namespace, Name: mig.Spec.TargetRef.Name}
	if err := r.Get(ctx, clusterKey, &cluster); err != nil {
		meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
			Type: temporalv1alpha1.ConditionTargetReachable, Status: metav1.ConditionFalse,
			Reason: "ClusterNotFound", Message: "referenced TemporalCluster not found",
		})
		mig.Status.Phase = temporalv1alpha1.MigrationPhasePending
		return ctrl.Result{RequeueAfter: migrationRequeue}, r.statusUpdate(ctx, &mig)
	}
	meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
		Type: temporalv1alpha1.ConditionTargetReachable, Status: metav1.ConditionTrue,
		Reason: "Found", Message: "target TemporalCluster found",
	})

	if err := r.provisionProxy(ctx, &mig, &cluster); err != nil {
		return ctrl.Result{}, err
	}
	mig.Status.ProxyEndpoint = fmt.Sprintf("%s.%s.svc.cluster.local:7233", resources.MigrationProxyName(&mig), mig.Namespace)
	r.setProxyReadyCondition(ctx, &mig)

	if !mig.Spec.Cutover {
		mig.Status.Phase = temporalv1alpha1.MigrationPhasePassthrough
		meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
			Type: temporalv1alpha1.ConditionMigrationComplete, Status: metav1.ConditionFalse,
			Reason: temporalv1alpha1.ReasonPassthrough, Message: "proxy forwarding all traffic to source",
		})
		mig.Status.ObservedGeneration = mig.Generation
		return ctrl.Result{RequeueAfter: migrationRequeue}, r.statusUpdate(ctx, &mig)
	}

	if mig.Status.CutoverTime == nil {
		now := metav1.Now()
		mig.Status.CutoverTime = &now
		log.Info("cutover enabled", "migration", req.NamespacedName)
	}

	complete, err := r.reconcileDrain(ctx, &mig)
	if err != nil {
		return ctrl.Result{}, err
	}
	if complete {
		mig.Status.Phase = temporalv1alpha1.MigrationPhaseComplete
	} else {
		mig.Status.Phase = temporalv1alpha1.MigrationPhaseDraining
	}
	mig.Status.ObservedGeneration = mig.Generation
	return ctrl.Result{RequeueAfter: migrationRequeue}, r.statusUpdate(ctx, &mig)
}

// setProxyReadyCondition reflects the proxy Deployment's availability.
func (r *TemporalMigrationReconciler) setProxyReadyCondition(ctx context.Context, mig *temporalv1alpha1.TemporalMigration) {
	var dep appsv1.Deployment
	key := types.NamespacedName{Namespace: mig.Namespace, Name: resources.MigrationProxyName(mig)}
	status := metav1.ConditionFalse
	reason := temporalv1alpha1.ReasonProxyProvisioning
	msg := "proxy Deployment not yet available"
	if err := r.Get(ctx, key, &dep); err == nil && dep.Status.AvailableReplicas > 0 {
		status = metav1.ConditionTrue
		reason = "Available"
		msg = "proxy is serving"
	}
	meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
		Type: temporalv1alpha1.ConditionProxyReady, Status: status, Reason: reason, Message: msg,
	})
}

// provisionProxy renders config and applies the ConfigMap, Service, Deployment.
func (r *TemporalMigrationReconciler) provisionProxy(ctx context.Context, mig *temporalv1alpha1.TemporalMigration, cluster *temporalv1alpha1.TemporalCluster) error {
	cfg, mounts, err := renderProxyConfig(mig, cluster)
	if err != nil {
		return err
	}
	rendered, err := marshalProxyConfig(cfg)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(rendered))
	configHash := hex.EncodeToString(sum[:])

	cm := resources.BuildMigrationProxyConfigMap(mig, rendered)
	svc := resources.BuildMigrationProxyService(mig)
	dep := resources.BuildMigrationProxyDeployment(mig, r.proxyImage(), configHash)
	applyProxyMounts(dep, mounts)

	for _, obj := range []client.Object{cm, svc, dep} {
		if err := controllerutil.SetControllerReference(mig, obj, r.Scheme); err != nil {
			return err
		}
		if err := serverSideApply(ctx, r.Client, r.Scheme, obj, client.FieldOwner("temporal-operator")); err != nil {
			return fmt.Errorf("applying %T: %w", obj, err)
		}
	}
	return nil
}

// applyProxyMounts appends secret volumes/mounts to the proxy Deployment.
func applyProxyMounts(dep *appsv1.Deployment, mounts []secretMount) {
	c := &dep.Spec.Template.Spec.Containers[0]
	for i, m := range mounts {
		volName := fmt.Sprintf("tls-%d", i)
		dep.Spec.Template.Spec.Volumes = append(dep.Spec.Template.Spec.Volumes, corev1.Volume{
			Name:         volName,
			VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: m.SecretName}},
		})
		c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
			Name: volName, MountPath: m.MountPath, ReadOnly: true,
		})
	}
}

// reconcileDrain queries the source running-workflow counts and reports when
// the source has fully drained for DrainStableThreshold consecutive checks.
func (r *TemporalMigrationReconciler) reconcileDrain(ctx context.Context, mig *temporalv1alpha1.TemporalMigration) (bool, error) {
	srcTLS, err := r.sourceTLS(ctx, mig)
	if err != nil {
		return false, err
	}
	mc, err := r.MigrationClientFactory(ctx, mig.Spec.Source.Address, srcTLS)
	if err != nil {
		meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
			Type: temporalv1alpha1.ConditionSourceReachable, Status: metav1.ConditionFalse,
			Reason: temporalv1alpha1.ReasonUnreachable, Message: err.Error(),
		})
		return false, nil
	}
	defer func() { _ = mc.Close() }()
	meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
		Type: temporalv1alpha1.ConditionSourceReachable, Status: metav1.ConditionTrue,
		Reason: "Reachable", Message: "source frontend reachable",
	})

	namespaces, err := r.migratedNamespaces(ctx, mig, mc)
	if err != nil {
		return false, err
	}

	var total int64
	drainStatus := make([]temporalv1alpha1.NamespaceDrainStatus, 0, len(namespaces))
	for _, ns := range namespaces {
		count, err := mc.CountRunningWorkflows(ctx, ns)
		if err != nil {
			return false, err
		}
		total += count
		drainStatus = append(drainStatus, temporalv1alpha1.NamespaceDrainStatus{
			Namespace: ns, SourceRunningWorkflows: count, Drained: count == 0,
		})
	}
	mig.Status.Draining = drainStatus

	key := types.NamespacedName{Namespace: mig.Namespace, Name: mig.Name}
	if r.zeroStreak == nil {
		r.zeroStreak = map[types.NamespacedName]int{}
	}
	if total == 0 {
		r.zeroStreak[key]++
	} else {
		r.zeroStreak[key] = 0
	}

	threshold := r.DrainStableThreshold
	if threshold <= 0 {
		threshold = 3
	}
	complete := total == 0 && r.zeroStreak[key] >= threshold

	drainingStatus := metav1.ConditionTrue
	if complete {
		drainingStatus = metav1.ConditionFalse
	}
	meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
		Type: temporalv1alpha1.ConditionMigrationDraining, Status: drainingStatus,
		Reason: temporalv1alpha1.ReasonDraining, Message: fmt.Sprintf("%d running workflows on source", total),
	})

	completeStatus := metav1.ConditionFalse
	completeReason := temporalv1alpha1.ReasonDraining
	if complete {
		completeStatus = metav1.ConditionTrue
		completeReason = temporalv1alpha1.ReasonDrained
	}
	meta.SetStatusCondition(&mig.Status.Conditions, metav1.Condition{
		Type: temporalv1alpha1.ConditionMigrationComplete, Status: completeStatus,
		Reason: completeReason, Message: fmt.Sprintf("%d running workflows on source", total),
	})
	return complete, nil
}

// migratedNamespaces returns the configured namespaces or all source namespaces.
func (r *TemporalMigrationReconciler) migratedNamespaces(ctx context.Context, mig *temporalv1alpha1.TemporalMigration, mc temporal.MigrationClient) ([]string, error) {
	if len(mig.Spec.Namespaces) > 0 {
		out := make([]string, 0, len(mig.Spec.Namespaces))
		for _, m := range mig.Spec.Namespaces {
			out = append(out, m.Source)
		}
		return out, nil
	}
	return mc.ListNamespaces(ctx)
}

// sourceTLS builds the control-plane TLS config for the source frontend.
func (r *TemporalMigrationReconciler) sourceTLS(ctx context.Context, mig *temporalv1alpha1.TemporalMigration) (*tlsConfig, error) {
	t := mig.Spec.Source.TLS
	if t == nil || !t.Enabled || t.SecretRef == nil {
		return nil, nil
	}
	return buildSourceTLSConfig(ctx, r.Client, mig.Namespace, t)
}

func (r *TemporalMigrationReconciler) proxyImage() string {
	if r.ProxyImage != "" {
		return r.ProxyImage
	}
	return defaultProxyImage()
}

func (r *TemporalMigrationReconciler) statusUpdate(ctx context.Context, mig *temporalv1alpha1.TemporalMigration) error {
	return r.Status().Update(ctx, mig)
}

// SetupWithManager registers the controller and owned resources.
func (r *TemporalMigrationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.MigrationClientFactory == nil {
		r.MigrationClientFactory = temporal.NewMigrationClient
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalMigration{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Named("temporalmigration").
		Complete(r)
}
