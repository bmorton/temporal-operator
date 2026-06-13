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
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// Service component names.
const (
	ServiceFrontend         = "frontend"
	ServiceInternalFrontend = "internal-frontend"
	ServiceHistory          = "history"
	ServiceMatching         = "matching"
	ServiceWorker           = "worker"
)

// ServiceInfo describes a single Temporal service to be deployed.
type ServiceInfo struct {
	// Name is the component name (e.g. "frontend").
	Name string
	// Spec is the per-service configuration from the CR; may be nil.
	Spec *temporalv1alpha1.ServiceSpec
	// Ports holds the resolved ports for the service.
	Ports temporal.ServicePort
	// HasHTTP reports whether the service exposes an HTTP port.
	HasHTTP bool
}

// EnabledServices returns the services that should be deployed for a cluster,
// including internal-frontend only when enabled.
func EnabledServices(cluster *temporalv1alpha1.TemporalCluster) []ServiceInfo {
	ports := temporal.DefaultServicePorts()
	svcs := cluster.Spec.Services

	infos := []ServiceInfo{
		{Name: ServiceFrontend, Spec: svcs.Frontend, Ports: ports[ServiceFrontend], HasHTTP: true},
		{Name: ServiceHistory, Spec: svcs.History, Ports: ports[ServiceHistory]},
		{Name: ServiceMatching, Spec: svcs.Matching, Ports: ports[ServiceMatching]},
		{Name: ServiceWorker, Spec: svcs.Worker, Ports: ports[ServiceWorker]},
	}

	if svcs.InternalFrontend != nil && svcs.InternalFrontend.Enabled {
		var spec *temporalv1alpha1.ServiceSpec
		if svcs.InternalFrontend.Replicas != nil {
			spec = &temporalv1alpha1.ServiceSpec{
				Replicas:  svcs.InternalFrontend.Replicas,
				Resources: svcs.InternalFrontend.Resources,
			}
		}
		infos = append(infos, ServiceInfo{
			Name:    ServiceInternalFrontend,
			Spec:    spec,
			Ports:   ports[ServiceInternalFrontend],
			HasHTTP: true,
		})
	}

	return infos
}
