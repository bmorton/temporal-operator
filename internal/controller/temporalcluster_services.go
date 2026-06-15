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

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/persistence"
	"github.com/bmorton/temporal-operator/internal/resources"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

const servicesFieldOwner = client.FieldOwner("temporal-operator/services")

// reconcileServices renders configuration and applies the Deployments, Services,
// ConfigMaps, and PodDisruptionBudgets for the cluster's Temporal services. It
// only runs once the schema is ready.
func (r *TemporalClusterReconciler) reconcileServices(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, serviceVersions map[string]string) error {
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionSchemaReady) {
		// Schema is not ready yet; defer service deployment.
		return nil
	}

	rendered, err := r.renderConfig(ctx, cluster)
	if err != nil {
		return err
	}
	configHash := resources.ConfigHash(rendered.config)
	mtls := r.mtlsMounts(ctx, cluster)

	dynamicCM := resources.BuildDynamicConfigMap(cluster, rendered.dynamicConfig)
	configSecret := resources.BuildConfigSecret(cluster, rendered.config)
	if err := r.apply(ctx, cluster, configSecret); err != nil {
		return err
	}
	if err := r.apply(ctx, cluster, dynamicCM); err != nil {
		return err
	}

	services := resources.EnabledServices(cluster)
	for _, svc := range services {
		version := serviceVersions[svc.Name]
		if err := r.apply(ctx, cluster, resources.BuildDeployment(cluster, svc, configHash, version, mtls)); err != nil {
			return err
		}
		if err := r.apply(ctx, cluster, resources.BuildHeadlessService(cluster, svc)); err != nil {
			return err
		}
		if err := r.apply(ctx, cluster, resources.BuildPodDisruptionBudget(cluster, svc)); err != nil {
			return err
		}
		if svc.Name == resources.ServiceFrontend {
			if err := r.apply(ctx, cluster, resources.BuildFrontendService(cluster, svc)); err != nil {
				return err
			}
		}
	}

	return r.rollupServiceStatus(ctx, cluster, services)
}

type renderedConfig struct {
	config        string
	dynamicConfig string
}

func (r *TemporalClusterReconciler) renderConfig(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster) (renderedConfig, error) {
	resolver := persistence.NewSecretResolver(r.Client, cluster.Namespace)
	defCred, err := resolver.ResolveStore(ctx, cluster.Spec.Persistence.DefaultStore)
	if err != nil {
		return renderedConfig{}, fmt.Errorf("resolving default store credential: %w", err)
	}
	visCred, err := resolver.ResolveStore(ctx, cluster.Spec.Persistence.VisibilityStore)
	if err != nil {
		return renderedConfig{}, fmt.Errorf("resolving visibility store credential: %w", err)
	}

	opts := temporal.BuildOptions{
		DefaultStorePassword:           defCred.Password,
		DefaultStorePasswordCommand:    defCred.PasswordCommand,
		VisibilityStorePassword:        visCred.Password,
		VisibilityStorePasswordCommand: visCred.PasswordCommand,
		PublicClientHostPort: fmt.Sprintf("%s.%s.svc:%d",
			resources.FrontendServiceName(cluster.Name), cluster.Namespace, temporal.DefaultServicePorts()["frontend"].GRPCPort),
	}
	cfg, err := temporal.RenderClusterConfig(cluster, opts)
	if err != nil {
		return renderedConfig{}, fmt.Errorf("rendering config: %w", err)
	}
	dyn, _, err := temporal.RenderDynamicConfig(cluster.Spec.DynamicConfig, cluster.Spec.Version)
	if err != nil {
		return renderedConfig{}, fmt.Errorf("rendering dynamic config: %w", err)
	}
	return renderedConfig{config: cfg, dynamicConfig: dyn}, nil
}

// apply performs a server-side apply of a managed object, setting the cluster as
// its controller owner.
func (r *TemporalClusterReconciler) apply(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, obj client.Object) error {
	if err := controllerutil.SetControllerReference(cluster, obj, r.Scheme); err != nil {
		return err
	}
	// client.Apply (the legacy server-side apply patch type) is deprecated in
	// controller-runtime v0.23 in favor of Client.Apply with generated apply
	// configurations, which are not available for these unstructured objects.
	//nolint:staticcheck // SA1019: legacy server-side apply patch retained intentionally.
	return r.Patch(ctx, obj, client.Apply, servicesFieldOwner, client.ForceOwnership)
}

// rollupServiceStatus reads Deployment readiness and updates status.services and
// the Available condition.
func (r *TemporalClusterReconciler) rollupServiceStatus(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, services []resources.ServiceInfo) error {
	if cluster.Status.Services == nil {
		cluster.Status.Services = map[string]temporalv1alpha1.ServiceStatus{}
	}

	allReady := true
	for _, svc := range services {
		var dep appsv1.Deployment
		name := resources.DeploymentName(cluster.Name, svc.Name)
		if err := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, &dep); err != nil {
			return err
		}
		desired := int32(1)
		if dep.Spec.Replicas != nil {
			desired = *dep.Spec.Replicas
		}
		cluster.Status.Services[svc.Name] = temporalv1alpha1.ServiceStatus{
			Ready:   dep.Status.ReadyReplicas,
			Desired: desired,
			Version: cluster.Spec.Version,
		}
		if dep.Status.ReadyReplicas < 1 {
			allReady = false
		}
	}

	status := metav1.ConditionFalse
	reason := temporalv1alpha1.ReasonRolloutInProgress
	message := "waiting for all services to become ready"
	if allReady {
		status = metav1.ConditionTrue
		reason = temporalv1alpha1.ReasonAllServicesReady
		message = "all services are ready"
	}
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionAvailable,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cluster.Generation,
	})
	return nil
}

// computeReadyAndPhase derives the Ready condition and status.phase from the
// component conditions.
func (r *TemporalClusterReconciler) computeReadyAndPhase(cluster *temporalv1alpha1.TemporalCluster) {
	conds := cluster.Status.Conditions
	reachable := meta.IsStatusConditionTrue(conds, temporalv1alpha1.ConditionPersistenceReachable)
	schemaReady := meta.IsStatusConditionTrue(conds, temporalv1alpha1.ConditionSchemaReady)
	available := meta.IsStatusConditionTrue(conds, temporalv1alpha1.ConditionAvailable)
	mtlsReady := !mTLSEnabled(cluster) || meta.IsStatusConditionTrue(conds, temporalv1alpha1.ConditionMTLSReady)

	ready := reachable && schemaReady && available && mtlsReady
	wasReady := meta.IsStatusConditionTrue(conds, temporalv1alpha1.ConditionReady)
	upgrading := upgradeInProgress(cluster)
	switch {
	case !reachable:
		cluster.Status.Phase = "Pending"
	case !schemaReady:
		cluster.Status.Phase = "ProvisioningSchema"
	case upgrading:
		cluster.Status.Phase = "Upgrading"
	case !available:
		cluster.Status.Phase = "DeployingServices"
	default:
		cluster.Status.Phase = "Ready"
	}

	readyStatus := metav1.ConditionFalse
	reason := temporalv1alpha1.ReasonReconciling
	message := "cluster is not yet ready"
	if ready {
		readyStatus = metav1.ConditionTrue
		reason = temporalv1alpha1.ReasonAllServicesReady
		message = "cluster is ready"
		// Only stamp the running version on a fresh install; during an upgrade
		// the upgrade reconciler owns status.version until the rollout completes.
		if !upgrading {
			cluster.Status.Version = cluster.Spec.Version
		}
	}
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionReady,
		Status:             readyStatus,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cluster.Generation,
	})
	if ready && !wasReady && r.Recorder != nil {
		r.Recorder.Event(cluster, "Normal", "ClusterReady", "TemporalCluster is ready")
	}
}
