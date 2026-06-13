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
	"fmt"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// Backend abstracts a Temporal datastore (SQL, Cassandra, or Elasticsearch) for
// reachability probing and schema management.
type Backend interface {
	// Probe verifies the datastore is reachable.
	Probe(ctx context.Context) error
	// SchemaVersion returns the current schema (or index-template) version. An
	// empty string means no schema is present yet.
	SchemaVersion(ctx context.Context) (string, error)
	// EnsureSchema applies schema inline when the backend manages schema itself
	// (Elasticsearch index templates). It returns inline=true when it handled
	// the schema; Job-based backends (SQL, Cassandra) return inline=false and do
	// nothing, leaving Job orchestration to the caller.
	EnsureSchema(ctx context.Context, minVersion string) (inline bool, err error)
	// Kind returns "sql", "cassandra", or "elasticsearch".
	Kind() string
}

// BackendFactory builds a Backend for a datastore spec and resolved credential.
type BackendFactory func(store temporalv1alpha1.DatastoreSpec, cred ResolvedCredential, dbName string) (Backend, error)

// DefaultBackendFactory dispatches on the configured datastore backend.
func DefaultBackendFactory(store temporalv1alpha1.DatastoreSpec, cred ResolvedCredential, dbName string) (Backend, error) {
	switch {
	case store.SQL != nil:
		return &sqlBackend{spec: store.SQL, cred: cred, dbName: dbName}, nil
	case store.Cassandra != nil:
		return &cassandraBackend{spec: store.Cassandra, cred: cred}, nil
	case store.Elasticsearch != nil:
		return &esBackend{spec: store.Elasticsearch, cred: cred}, nil
	default:
		return nil, fmt.Errorf("datastore has no backend configured")
	}
}
