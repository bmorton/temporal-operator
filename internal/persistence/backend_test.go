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
	"testing"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func TestDefaultBackendFactoryDispatch(t *testing.T) {
	cases := []struct {
		name  string
		store temporalv1alpha1.DatastoreSpec
		kind  string
	}{
		{"sql", temporalv1alpha1.DatastoreSpec{SQL: &temporalv1alpha1.SQLDatastoreSpec{}}, "sql"},
		{"cassandra", temporalv1alpha1.DatastoreSpec{Cassandra: &temporalv1alpha1.CassandraDatastoreSpec{}}, "cassandra"},
		{"elasticsearch", temporalv1alpha1.DatastoreSpec{Elasticsearch: &temporalv1alpha1.ElasticsearchDatastoreSpec{}}, "elasticsearch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := DefaultBackendFactory(tc.store, ResolvedCredential{}, "db")
			if err != nil {
				t.Fatalf("factory error: %v", err)
			}
			if b.Kind() != tc.kind {
				t.Errorf("expected kind %q, got %q", tc.kind, b.Kind())
			}
		})
	}
	if _, err := DefaultBackendFactory(temporalv1alpha1.DatastoreSpec{}, ResolvedCredential{}, ""); err == nil {
		t.Errorf("expected error for empty datastore")
	}
}

func TestESBaseURL(t *testing.T) {
	b := &esBackend{spec: &temporalv1alpha1.ElasticsearchDatastoreSpec{URL: "es.default.svc:9200"}}
	if got := b.baseURL(); got != "http://es.default.svc:9200" {
		t.Errorf("unexpected base url %q", got)
	}
	b.spec.TLS = &temporalv1alpha1.DatastoreTLSSpec{Enabled: true}
	if got := b.baseURL(); got != "https://es.default.svc:9200" {
		t.Errorf("expected https with TLS, got %q", got)
	}
	b.spec.URL = "https://es.example.com/"
	if got := b.baseURL(); got != "https://es.example.com" {
		t.Errorf("expected scheme preserved and trailing slash trimmed, got %q", got)
	}
}
