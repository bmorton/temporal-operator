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
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// Upgrade phases, ordered.
const (
	upgradePending                 = "Pending"
	upgradePreflight               = "PreflightChecks"
	upgradeSchemaMigrating         = "SchemaMigrating"
	upgradeRollingFrontend         = "RollingFrontend"
	upgradeRollingHistory          = "RollingHistory"
	upgradeRollingMatching         = "RollingMatching"
	upgradeRollingInternalFrontend = "RollingInternalFrontend"
	upgradeRollingWorker           = "RollingWorker"
	upgradePostUpgrade             = "PostUpgrade"
	upgradeComplete                = "Complete"
)

// rollingPhaseForService maps each rolling phase to the service it upgrades.
var rollingPhaseService = map[string]string{
	upgradeRollingFrontend:         resources.ServiceFrontend,
	upgradeRollingHistory:          resources.ServiceHistory,
	upgradeRollingMatching:         resources.ServiceMatching,
	upgradeRollingInternalFrontend: resources.ServiceInternalFrontend,
	upgradeRollingWorker:           resources.ServiceWorker,
}

// serviceUpgradeOrder is the order in which services adopt the new version.
var serviceUpgradeOrder = []string{
	resources.ServiceFrontend,
	resources.ServiceHistory,
	resources.ServiceMatching,
	resources.ServiceInternalFrontend,
	resources.ServiceWorker,
}

// reconcileUpgrade detects and advances a version upgrade. It returns the
// per-service target version map the service reconciler should apply.
func (r *TemporalClusterReconciler) reconcileUpgrade(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster) map[string]string {
	current := cluster.Status.Version
	target := cluster.Spec.Version

	// No prior version (fresh install) or already at target: no upgrade.
	if current == "" || current == target {
		cluster.Status.Upgrade = nil
		return r.uniformVersions(cluster, target)
	}

	if cluster.Status.Upgrade == nil || cluster.Status.Upgrade.ToVersion != target {
		now := metav1.Now()
		cluster.Status.Upgrade = &temporalv1alpha1.UpgradeStatus{
			FromVersion:  current,
			ToVersion:    target,
			Phase:        upgradePending,
			Rollbackable: true,
			StartedAt:    &now,
		}
		r.event(cluster, "UpgradeStarted", "upgrade from "+current+" to "+target+" started")
	}

	r.advanceUpgrade(ctx, cluster)
	if cluster.Status.Upgrade == nil {
		// Upgrade just completed during advance.
		return r.uniformVersions(cluster, target)
	}
	return r.upgradeServiceVersions(cluster)
}

func (r *TemporalClusterReconciler) uniformVersions(cluster *temporalv1alpha1.TemporalCluster, version string) map[string]string {
	out := map[string]string{}
	for _, svc := range resources.EnabledServices(cluster) {
		out[svc.Name] = version
	}
	return out
}

// upgradeServiceVersions returns the target version per service based on the
// current upgrade phase: services at or before the current rolling phase run the
// new version, the rest stay on the old version.
func (r *TemporalClusterReconciler) upgradeServiceVersions(cluster *temporalv1alpha1.TemporalCluster) map[string]string {
	up := cluster.Status.Upgrade
	out := map[string]string{}

	// Index of the service currently being rolled (or beyond).
	upgradedThrough := -1
	switch up.Phase {
	case upgradePostUpgrade, upgradeComplete:
		upgradedThrough = len(serviceUpgradeOrder) - 1
	default:
		if svc, ok := rollingPhaseService[up.Phase]; ok {
			for i, name := range serviceUpgradeOrder {
				if name == svc {
					upgradedThrough = i
				}
			}
		}
	}

	for _, svc := range resources.EnabledServices(cluster) {
		version := up.FromVersion
		for i, name := range serviceUpgradeOrder {
			if name == svc.Name && i <= upgradedThrough {
				version = up.ToVersion
			}
		}
		out[svc.Name] = version
	}
	return out
}

// advanceUpgrade moves the phase machine forward by at most one step per
// reconcile, guarded by rollout completion of the relevant service.
func (r *TemporalClusterReconciler) advanceUpgrade(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster) {
	up := cluster.Status.Upgrade
	reachable := meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionPersistenceReachable)
	schemaReady := meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionSchemaReady)

	advance := func(next string) {
		up.Phase = next
		r.event(cluster, "UpgradePhase", "upgrade entered phase "+next)
	}

	switch up.Phase {
	case upgradePending:
		advance(upgradePreflight)
	case upgradePreflight:
		if reachable {
			up.Rollbackable = false
			advance(upgradeSchemaMigrating)
		}
	case upgradeSchemaMigrating:
		up.Rollbackable = false
		if schemaReady {
			advance(upgradeRollingFrontend)
		}
	case upgradePostUpgrade:
		advance(upgradeComplete)
	case upgradeComplete:
		cluster.Status.Version = up.ToVersion
		r.event(cluster, "UpgradeComplete", "upgrade to "+up.ToVersion+" complete")
		cluster.Status.Upgrade = nil
	default:
		r.advanceRollingPhase(ctx, cluster, advance)
	}
}

// advanceRollingPhase advances a per-service rolling phase once the current
// service has fully rolled out at the target version.
func (r *TemporalClusterReconciler) advanceRollingPhase(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, advance func(string)) {
	up := cluster.Status.Upgrade
	svc, ok := rollingPhaseService[up.Phase]
	if !ok {
		return
	}
	if !r.serviceRolledOut(ctx, cluster, svc, up.ToVersion) {
		return
	}
	advance(r.nextRollingPhase(cluster, up.Phase))
}

func (r *TemporalClusterReconciler) nextRollingPhase(cluster *temporalv1alpha1.TemporalCluster, phase string) string {
	switch phase {
	case upgradeRollingFrontend:
		return upgradeRollingHistory
	case upgradeRollingHistory:
		return upgradeRollingMatching
	case upgradeRollingMatching:
		return r.afterMatchingPhase(cluster)
	case upgradeRollingInternalFrontend:
		return upgradeRollingWorker
	case upgradeRollingWorker:
		return upgradePostUpgrade
	default:
		return phase
	}
}

func (r *TemporalClusterReconciler) afterMatchingPhase(cluster *temporalv1alpha1.TemporalCluster) string {
	if cluster.Spec.Services.InternalFrontend != nil && cluster.Spec.Services.InternalFrontend.Enabled {
		return upgradeRollingInternalFrontend
	}
	return upgradeRollingWorker
}

// serviceRolledOut reports whether a service's Deployment is fully rolled out at
// the target version.
func (r *TemporalClusterReconciler) serviceRolledOut(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, component, version string) bool {
	var dep appsv1.Deployment
	name := resources.DeploymentName(cluster.Name, component)
	if err := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, &dep); err != nil {
		return false
	}
	if dep.Spec.Template.Labels[resources.LabelVersion] != version {
		return false
	}
	if dep.Generation != dep.Status.ObservedGeneration {
		return false
	}
	desired := int32(1)
	if dep.Spec.Replicas != nil {
		desired = *dep.Spec.Replicas
	}
	return dep.Status.UpdatedReplicas == desired && dep.Status.ReadyReplicas == desired
}

func (r *TemporalClusterReconciler) event(cluster *temporalv1alpha1.TemporalCluster, reason, message string) {
	if r.Recorder != nil {
		// The events.k8s.io recorder requires an action verb; reuse the reason,
		// which already reads as a machine-readable verb for our events.
		r.Recorder.Eventf(cluster, nil, "Normal", reason, reason, message)
	}
}

// upgradeInProgress reports whether the cluster is mid-upgrade.
func upgradeInProgress(cluster *temporalv1alpha1.TemporalCluster) bool {
	return cluster.Status.Upgrade != nil
}
