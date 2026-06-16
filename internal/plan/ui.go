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

package plan

import (
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// PlanUI returns the temporal-ui objects when the UI is enabled: an optional
// client certificate (under mTLS), the Deployment, the Service, and an optional
// Ingress. It mirrors the controller's reconcileUI ordering.
func PlanUI(cluster *temporalv1alpha1.TemporalCluster) []PlannedObject {
	if cluster.Spec.UI == nil || !cluster.Spec.UI.Enabled {
		return nil
	}
	objs := make([]client.Object, 0, 4)
	if resources.MTLSEnabled(cluster) {
		objs = append(objs, resources.BuildUIClientCertificate(cluster))
	}
	objs = append(objs, resources.BuildUIDeployment(cluster), resources.BuildUIService(cluster))
	if ingress := resources.BuildUIIngress(cluster); ingress != nil {
		objs = append(objs, ingress)
	}
	return tag(PhaseUI, objs...)
}
