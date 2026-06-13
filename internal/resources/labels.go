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
	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// Label keys shared across all managed resources.
const (
	LabelName      = "app.kubernetes.io/name"
	LabelInstance  = "app.kubernetes.io/instance"
	LabelComponent = "app.kubernetes.io/component"
	LabelManagedBy = "app.kubernetes.io/managed-by"
	LabelCluster   = "temporal.bmor10.com/cluster"
	LabelVersion   = "temporal.bmor10.com/version"

	// ConfigHashAnnotation stamps the rendered-config hash onto pods so config
	// changes trigger a rollout.
	ConfigHashAnnotation = "temporal.bmor10.com/config-hash"
	// CertHashAnnotation stamps the mTLS cert hash onto pods so certificate
	// rotation triggers a rollout.
	CertHashAnnotation = "temporal.bmor10.com/cert-hash"

	managedByValue = "temporal-operator"
	nameValue      = "temporal"
)

// SelectorLabels returns the stable selector labels for a cluster component.
// These never include the version so that selectors remain stable across
// upgrades.
func SelectorLabels(cluster *temporalv1alpha1.TemporalCluster, component string) map[string]string {
	return map[string]string{
		LabelName:      nameValue,
		LabelInstance:  cluster.Name,
		LabelComponent: component,
		LabelCluster:   cluster.Name,
	}
}

// StandardLabels returns the full label set for a cluster component, including
// the managed-by and version labels.
func StandardLabels(cluster *temporalv1alpha1.TemporalCluster, component string) map[string]string {
	labels := SelectorLabels(cluster, component)
	labels[LabelManagedBy] = managedByValue
	if cluster.Spec.Version != "" {
		labels[LabelVersion] = cluster.Spec.Version
	}
	return labels
}
