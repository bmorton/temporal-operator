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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/plan"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// reconcileMonitoring creates a ServiceMonitor when requested and the Prometheus
// Operator ServiceMonitor CRD is installed in the cluster, and deletes it again
// when it is no longer requested.
func (r *TemporalClusterReconciler) reconcileMonitoring(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster) error {
	log := logf.FromContext(ctx)

	installed := r.kindInstalled(resources.ServiceMonitorGVK)
	planned := plan.PlanMonitoring(cluster)

	if len(planned) == 0 {
		if !installed {
			return nil
		}
		sm := &unstructured.Unstructured{}
		sm.SetGroupVersionKind(resources.ServiceMonitorGVK)
		sm.SetName(resources.ServiceMonitorName(cluster.Name))
		sm.SetNamespace(cluster.Namespace)
		return r.prune(ctx, cluster, sm)
	}

	if !installed {
		log.Info("ServiceMonitor CRD not installed; skipping ServiceMonitor creation")
		return nil
	}

	for _, p := range planned {
		if err := r.apply(ctx, cluster, p.Object); err != nil {
			return err
		}
	}
	return nil
}
