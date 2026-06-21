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

// Package plan computes the desired set of Kubernetes objects for a Temporal
// custom resource. It is pure (no client, no IO) so it can be shared by the
// operator's controllers and by the WebAssembly preview tool.
package plan

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// Phase labels which operator concern produces an object. It is surfaced in the
// preview UI as a badge and documents why each object exists.
type Phase string

const (
	PhasePersistenceSchema Phase = "Persistence & Schema"
	PhaseCoreServices      Phase = "Core Services"
	PhaseMTLS              Phase = "mTLS"
	PhaseUI                Phase = "UI"
	PhaseMonitoring        Phase = "Monitoring"
)

// PlannedObject is a desired object tagged with the phase that produces it.
type PlannedObject struct {
	Object client.Object
	Phase  Phase
}

func tag(phase Phase, objs ...client.Object) []PlannedObject {
	out := make([]PlannedObject, 0, len(objs))
	for _, o := range objs {
		if o == nil {
			continue
		}
		out = append(out, PlannedObject{Object: o, Phase: phase})
	}
	return out
}

// PlanFromSpec computes the full desired object set for a cluster using only its
// spec, with placeholder credentials. It is the entry point for the WebAssembly
// preview. The cluster is expected to already be defaulted by the caller.
func PlanFromSpec(cluster *temporalv1alpha1.TemporalCluster) ([]PlannedObject, error) {
	opts := temporal.BuildOptions{
		PublicClientHostPort: fmt.Sprintf("%s.%s.svc:%d",
			resources.FrontendServiceName(cluster.Name), cluster.Namespace,
			temporal.DefaultServicePorts()["frontend"].GRPCPort),
	}
	renderedConfig, err := temporal.RenderClusterConfig(cluster, opts)
	if err != nil {
		return nil, fmt.Errorf("rendering config: %w", err)
	}
	renderedDynamic, _, err := temporal.RenderDynamicConfig(cluster.Spec.DynamicConfig, cluster.Spec.Version)
	if err != nil {
		return nil, fmt.Errorf("rendering dynamic config: %w", err)
	}

	var mtls *resources.MTLSMounts
	if resources.MTLSEnabled(cluster) {
		mtls = &resources.MTLSMounts{
			Enabled:         true,
			InternodeSecret: resources.InternodeCertName(cluster.Name),
			FrontendSecret:  resources.FrontendCertName(cluster.Name),
		}
	}

	servicesInput := ServicesInput{
		RenderedConfig:        renderedConfig,
		RenderedDynamicConfig: renderedDynamic,
		ConfigHash:            resources.ConfigHash(renderedConfig),
		MTLS:                  mtls,
	}

	var out []PlannedObject
	schemaJobs, err := PlanSchemaJobs(cluster)
	if err != nil {
		return nil, err
	}
	out = append(out, schemaJobs...)
	out = append(out, PlanMTLS(cluster)...)
	services, err := PlanServices(cluster, servicesInput)
	if err != nil {
		return nil, err
	}
	out = append(out, services...)
	out = append(out, PlanUI(cluster)...)
	out = append(out, PlanMonitoring(cluster)...)
	return out, nil
}
