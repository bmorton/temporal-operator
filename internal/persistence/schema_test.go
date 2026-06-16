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
	"strings"
	"testing"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

func TestBuildPostgresDSN(t *testing.T) {
	spec := &temporalv1alpha1.SQLDatastoreSpec{
		Host: "pg.default.svc", Port: 5432, Database: "temporal", User: "temporal",
	}
	dsn := BuildPostgresDSN(spec, "s3cret", "")
	if !strings.HasPrefix(dsn, "postgres://temporal:s3cret@pg.default.svc:5432/temporal") {
		t.Errorf("unexpected dsn: %s", dsn)
	}
	if !strings.Contains(dsn, "sslmode=disable") {
		t.Errorf("expected sslmode=disable, got %s", dsn)
	}

	// dbName override targets the visibility database.
	if got := BuildPostgresDSN(spec, "x", "temporal_visibility"); !strings.Contains(got, "/temporal_visibility") {
		t.Errorf("expected visibility db, got %s", got)
	}

	// TLS enabled flips sslmode.
	spec.TLS = &temporalv1alpha1.DatastoreTLSSpec{Enabled: true, EnableHostVerification: true}
	if got := BuildPostgresDSN(spec, "x", ""); !strings.Contains(got, "sslmode=verify-full") {
		t.Errorf("expected verify-full, got %s", got)
	}
}

func TestCompareSchemaVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.12", "1.12", 0},
		{"v1.12", "1.12", 0},
		{"1.11", "1.12", -1},
		{"1.12", "1.11", 1},
		{"1.9", "1.12", -1},
		{"2.0", "1.99", 1},
		{"", "1.0", -1},
	}
	for _, tc := range cases {
		if got := CompareSchemaVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareSchemaVersions(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSchemaSatisfies(t *testing.T) {
	if SchemaSatisfies("", "1.12") {
		t.Errorf("empty version should not satisfy")
	}
	if !SchemaSatisfies("1.12", "1.12") {
		t.Errorf("equal version should satisfy")
	}
	if !SchemaSatisfies("v1.13", "1.12") {
		t.Errorf("higher version should satisfy")
	}
	if SchemaSatisfies("1.11", "1.12") {
		t.Errorf("lower version should not satisfy")
	}
}

func TestNormalizeSchemaVersion(t *testing.T) {
	if NormalizeSchemaVersion(" v1.12 ") != "1.12" {
		t.Errorf("unexpected normalization")
	}
}

// TestMinSchemaESSatisfiableByAllowedVersions guards against an unreachable
// Elasticsearch schema floor. The ElasticsearchDatastoreSpec.Version CRD enum
// allows only v7 and v8; if any supported Temporal version's MinSchemaES exceeds
// those (e.g. a stray "v9"), no OpenSearch/Elasticsearch visibility cluster can
// ever reach SchemaReady. esBackend.SchemaVersion reports the configured ES
// version, so MinSchemaES must be satisfiable by every allowed enum value.
func TestMinSchemaESSatisfiableByAllowedVersions(t *testing.T) {
	allowedESVersions := []string{"v7", "v8"}
	for _, v := range temporal.SupportedVersions() {
		info, err := temporal.LookupVersion(v)
		if err != nil {
			t.Fatalf("LookupVersion(%q): %v", v, err)
		}
		for _, esVersion := range allowedESVersions {
			if !SchemaSatisfies(esVersion, info.MinSchemaES) {
				t.Errorf("Temporal %s: allowed ES version %q does not satisfy MinSchemaES %q",
					v, esVersion, info.MinSchemaES)
			}
		}
	}
}
