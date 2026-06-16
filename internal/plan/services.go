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

// ServicesInput carries the inputs the controller derives from IO (rendered
// config, config hash, per-service image versions, and mTLS mounts) so that
// PlanServices itself stays pure. The preview supplies placeholder-credentialed
// renders and an empty cert hash.
type ServicesInput struct {
	RenderedConfig        string
	RenderedDynamicConfig string
	ConfigHash            string
	ServiceVersions       map[string]string
	MTLS                  *resources.MTLSMounts
}

// PlanServices returns the config Secret, dynamic-config ConfigMap, and the
// Deployment/headless Service/PodDisruptionBudget for each enabled service
// (plus the frontend Service). Ordering matches the controller's
// reconcileServices so golden comparisons stay stable.
func PlanServices(cluster *temporalv1alpha1.TemporalCluster, in ServicesInput) ([]PlannedObject, error) {
	objs := []client.Object{
		resources.BuildConfigSecret(cluster, in.RenderedConfig),
		resources.BuildDynamicConfigMap(cluster, in.RenderedDynamicConfig),
	}
	for _, svc := range resources.EnabledServices(cluster) {
		version := in.ServiceVersions[svc.Name]
		deployment, err := resources.BuildDeployment(cluster, svc, in.ConfigHash, version, in.MTLS)
		if err != nil {
			return nil, err
		}
		objs = append(objs,
			deployment,
			resources.BuildHeadlessService(cluster, svc),
			resources.BuildPodDisruptionBudget(cluster, svc),
		)
		if svc.Name == resources.ServiceFrontend {
			objs = append(objs, resources.BuildFrontendService(cluster, svc))
		}
	}
	return tag(PhaseCoreServices, objs...), nil
}
