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
	"sigs.k8s.io/controller-runtime/pkg/client"
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
