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

// postgresSchemaDir mirrors the controller's on-image schema directory for the
// postgres12 plugin.
const postgresSchemaDir = "v12"

// PlanSchemaJobs returns the initial setup-schema Job for the default and
// visibility stores. The operator additionally runs update-schema Jobs based on
// live schema introspection; the preview shows the from-scratch setup case,
// which is the representative "what gets created" view.
func PlanSchemaJobs(cluster *temporalv1alpha1.TemporalCluster) []PlannedObject {
	stores := []struct {
		name  resources.SchemaStore
		store temporalv1alpha1.DatastoreSpec
	}{
		{resources.StoreDefault, cluster.Spec.Persistence.DefaultStore},
		{resources.StoreVisibility, cluster.Spec.Persistence.VisibilityStore},
	}
	objs := make([]client.Object, 0, len(stores))
	for _, s := range stores {
		objs = append(objs, resources.BuildSchemaJob(resources.SchemaJobParams{
			Cluster:          cluster,
			SQLSpec:          s.store.SQL,
			CassandraSpec:    s.store.Cassandra,
			Store:            s.name,
			Action:           resources.ActionSetup,
			SchemaVersionDir: postgresSchemaDir,
		}))
	}
	return tag(PhasePersistenceSchema, objs...)
}
