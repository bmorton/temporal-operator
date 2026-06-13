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

package resources

import (
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// ServiceMonitorGVK is the GroupVersionKind of the Prometheus Operator
// ServiceMonitor resource.
var ServiceMonitorGVK = schema.GroupVersionKind{
	Group:   "monitoring.coreos.com",
	Version: "v1",
	Kind:    "ServiceMonitor",
}

// ServiceMonitorName returns the cluster's ServiceMonitor name.
func ServiceMonitorName(clusterName string) string {
	return clusterName
}

// BuildServiceMonitor builds a Prometheus Operator ServiceMonitor (as an
// unstructured object to avoid a hard dependency on the prometheus-operator
// API) that scrapes the metrics port of every Temporal service in the cluster.
func BuildServiceMonitor(cluster *temporalv1alpha1.TemporalCluster) *unstructured.Unstructured {
	labels := map[string]string{
		LabelName:      nameValue,
		LabelInstance:  cluster.Name,
		LabelManagedBy: managedByValue,
		LabelCluster:   cluster.Name,
	}
	// Merge user-supplied labels for Prometheus selection.
	if cluster.Spec.Metrics != nil && cluster.Spec.Metrics.ServiceMonitor != nil {
		for k, v := range cluster.Spec.Metrics.ServiceMonitor.Labels {
			labels[k] = v
		}
	}

	selector := map[string]interface{}{
		LabelName:     nameValue,
		LabelInstance: cluster.Name,
	}

	sm := &unstructured.Unstructured{}
	sm.SetGroupVersionKind(ServiceMonitorGVK)
	sm.SetName(ServiceMonitorName(cluster.Name))
	sm.SetNamespace(cluster.Namespace)
	sm.SetLabels(labels)
	_ = unstructured.SetNestedMap(sm.Object, map[string]interface{}{
		"selector": map[string]interface{}{
			"matchLabels": selector,
		},
		"namespaceSelector": map[string]interface{}{
			"matchNames": []interface{}{cluster.Namespace},
		},
		"endpoints": []interface{}{
			map[string]interface{}{
				"port":     "metrics",
				"path":     "/metrics",
				"interval": "30s",
			},
		},
	}, "spec")
	return sm
}
