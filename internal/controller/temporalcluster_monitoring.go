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

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// reconcileMonitoring creates a ServiceMonitor when requested and the Prometheus
// Operator ServiceMonitor CRD is installed in the cluster.
func (r *TemporalClusterReconciler) reconcileMonitoring(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster) error {
	log := logf.FromContext(ctx)

	if cluster.Spec.Metrics == nil || cluster.Spec.Metrics.ServiceMonitor == nil || !cluster.Spec.Metrics.ServiceMonitor.Enabled {
		return nil
	}

	if !r.serviceMonitorCRDInstalled() {
		log.Info("ServiceMonitor CRD not installed; skipping ServiceMonitor creation")
		return nil
	}

	return r.apply(ctx, cluster, resources.BuildServiceMonitor(cluster))
}

// serviceMonitorCRDInstalled reports whether the ServiceMonitor kind is known to
// the API server via the controller's RESTMapper.
func (r *TemporalClusterReconciler) serviceMonitorCRDInstalled() bool {
	mapper := r.RESTMapper()
	if mapper == nil {
		return false
	}
	_, err := mapper.RESTMapping(resources.ServiceMonitorGVK.GroupKind(), resources.ServiceMonitorGVK.Version)
	return err == nil
}
