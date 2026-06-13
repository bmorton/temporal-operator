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
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// PDBName returns the PodDisruptionBudget name for a service.
func PDBName(clusterName, component string) string {
	return clusterName + "-" + component
}

// BuildPodDisruptionBudget builds a PDB allowing at most one unavailable pod for
// the given service.
func BuildPodDisruptionBudget(cluster *temporalv1alpha1.TemporalCluster, svc ServiceInfo) *policyv1.PodDisruptionBudget {
	maxUnavailable := intstr.FromInt32(1)
	return &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{APIVersion: "policy/v1", Kind: "PodDisruptionBudget"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      PDBName(cluster.Name, svc.Name),
			Namespace: cluster.Namespace,
			Labels:    StandardLabels(cluster, svc.Name),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailable,
			Selector:       &metav1.LabelSelector{MatchLabels: SelectorLabels(cluster, svc.Name)},
		},
	}
}
