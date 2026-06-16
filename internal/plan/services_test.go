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
	"testing"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func TestPlanServicesObjects(t *testing.T) {
	c := testCluster()
	in := ServicesInput{
		RenderedConfig:        "config: yes",
		RenderedDynamicConfig: "{}\n",
		ConfigHash:            "deadbeef",
		ServiceVersions:       nil,
		MTLS:                  nil,
	}
	got, err := PlanServices(c, in)
	if err != nil {
		t.Fatalf("PlanServices error: %v", err)
	}
	// config Secret + dynamic ConfigMap + 4 services * (Deployment+Service+PDB)
	// + 1 frontend Service = 2 + 12 + 1 = 15.
	if len(got) != 15 {
		t.Fatalf("expected 15 objects, got %d (%v)", len(got), kinds(got))
	}
	for _, o := range got {
		if o.Phase != PhaseCoreServices {
			t.Errorf("expected PhaseCoreServices, got %s", o.Phase)
		}
	}
}

func TestPlanSchemaJobs(t *testing.T) {
	c := testCluster()
	c.Spec.Persistence = temporalv1alpha1.PersistenceSpec{
		DefaultStore:    temporalv1alpha1.DatastoreSpec{SQL: &temporalv1alpha1.SQLDatastoreSpec{PluginName: "postgres12"}},
		VisibilityStore: temporalv1alpha1.DatastoreSpec{SQL: &temporalv1alpha1.SQLDatastoreSpec{PluginName: "postgres12"}},
	}
	got := PlanSchemaJobs(c)
	if len(got) != 2 {
		t.Fatalf("expected one setup Job per store, got %d (%v)", len(got), kinds(got))
	}
	for _, o := range got {
		if o.Phase != PhasePersistenceSchema {
			t.Errorf("expected PhasePersistenceSchema, got %s", o.Phase)
		}
	}
}

func TestPlanFromSpecCoversAllPhases(t *testing.T) {
	c := testCluster()
	c.Spec.Persistence = temporalv1alpha1.PersistenceSpec{
		DefaultStore:    temporalv1alpha1.DatastoreSpec{SQL: &temporalv1alpha1.SQLDatastoreSpec{PluginName: "postgres12"}},
		VisibilityStore: temporalv1alpha1.DatastoreSpec{SQL: &temporalv1alpha1.SQLDatastoreSpec{PluginName: "postgres12"}},
	}
	c.Spec.UI = &temporalv1alpha1.UISpec{Enabled: true, Version: "2.34.0"}
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{Provider: "cert-manager", IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"}}

	got, err := PlanFromSpec(c)
	if err != nil {
		t.Fatalf("PlanFromSpec error: %v", err)
	}
	seen := map[Phase]bool{}
	for _, o := range got {
		seen[o.Phase] = true
	}
	for _, p := range []Phase{PhasePersistenceSchema, PhaseCoreServices, PhaseMTLS, PhaseUI} {
		if !seen[p] {
			t.Errorf("expected objects for phase %s", p)
		}
	}
}
