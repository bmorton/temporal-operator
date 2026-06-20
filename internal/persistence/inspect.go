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

package persistence

import (
	"context"
	"encoding/json"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// InspectResult is the JSON the inspector subcommand writes to the pod
// termination message: reachability + current schema version for one store.
type InspectResult struct {
	Reachable bool   `json:"reachable"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

func (r InspectResult) JSON() string {
	b, _ := json.Marshal(r)
	return string(b)
}

// InspectSQL probes a SQL store with the given password and reads its schema
// version, returning a structured result (never an error — failures are encoded
// in the result so the subcommand can emit them).
func InspectSQL(ctx context.Context, spec *temporalv1alpha1.SQLDatastoreSpec, password, dbName string) InspectResult {
	dsn := BuildPostgresDSN(spec, password, dbName)
	if err := (SQLProber{}).Probe(ctx, dsn); err != nil {
		return InspectResult{Reachable: false, Error: err.Error()}
	}
	version, err := (SQLProber{}).CurrentSchemaVersion(ctx, dsn, dbName)
	if err != nil {
		return InspectResult{Reachable: true, Error: err.Error()}
	}
	return InspectResult{Reachable: true, Version: version}
}
