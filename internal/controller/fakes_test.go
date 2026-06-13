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

package controller

import (
	"context"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/persistence"
)

// fakeBackend is a test double for persistence.Backend.
type fakeBackend struct {
	kind     string
	probeErr error
	version  string
}

func (f *fakeBackend) Probe(_ context.Context) error                   { return f.probeErr }
func (f *fakeBackend) SchemaVersion(_ context.Context) (string, error) { return f.version, nil }
func (f *fakeBackend) EnsureSchema(_ context.Context, _ string) (bool, error) {
	if f.kind == kindElasticsearch {
		// Simulate the index template being applied: the version now satisfies
		// any minimum.
		f.version = "v99"
		return true, nil
	}
	return false, nil
}
func (f *fakeBackend) Kind() string { return f.kind }

func backendKind(store temporalv1alpha1.DatastoreSpec) string {
	switch {
	case store.Cassandra != nil:
		return kindCassandra
	case store.Elasticsearch != nil:
		return kindElasticsearch
	default:
		return "sql"
	}
}

// fakeBackendFactory builds backends whose probe returns probeErr and whose
// schema version is looked up by the resolved store dbName.
func fakeBackendFactory(probeErr error, versions map[string]string) persistence.BackendFactory {
	return func(store temporalv1alpha1.DatastoreSpec, _ persistence.ResolvedCredential, dbName string) (persistence.Backend, error) {
		return &fakeBackend{
			kind:     backendKind(store),
			probeErr: probeErr,
			version:  versions[dbName],
		}, nil
	}
}
