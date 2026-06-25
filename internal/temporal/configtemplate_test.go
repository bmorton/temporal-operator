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

package temporal

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func ptrInt32(v int32) *int32 { return &v }

var update = flag.Bool("update", false, "update golden files")

func sqlStore(db string) *temporalv1alpha1.SQLDatastoreSpec {
	return &temporalv1alpha1.SQLDatastoreSpec{
		PluginName: "postgres12",
		Host:       "postgres.default.svc",
		Port:       5432,
		Database:   db,
		User:       "temporal",
		PasswordSecretRef: &temporalv1alpha1.SecretKeyReference{
			Name: "store",
			Key:  "password",
		},
	}
}

func baseCluster() *temporalv1alpha1.TemporalCluster {
	return &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Version:          "1.31.1",
			NumHistoryShards: 512,
			Persistence: temporalv1alpha1.PersistenceSpec{
				DefaultStore:    temporalv1alpha1.DatastoreSpec{SQL: sqlStore("temporal")},
				VisibilityStore: temporalv1alpha1.DatastoreSpec{SQL: sqlStore("temporal_visibility")},
			},
		},
	}
}

func TestRenderConfigGolden(t *testing.T) {
	opts := BuildOptions{
		BindOnIP:                "0.0.0.0",
		BroadcastAddress:        "10.0.0.1",
		DefaultStorePassword:    "default-pw",
		VisibilityStorePassword: "visibility-pw",
	}

	cases := map[string]func() *temporalv1alpha1.TemporalCluster{
		"postgres-no-mtls": baseCluster,
		"postgres-mtls": func() *temporalv1alpha1.TemporalCluster {
			c := baseCluster()
			c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{
				Provider:        "cert-manager",
				IssuerRef:       &temporalv1alpha1.IssuerReference{Name: "ca"},
				RefreshInterval: &metav1.Duration{Duration: 720 * 60 * 60 * 1e9},
			}
			return c
		},
		"cassandra": func() *temporalv1alpha1.TemporalCluster {
			c := baseCluster()
			cass := &temporalv1alpha1.CassandraDatastoreSpec{
				Hosts:      []string{"cass-0", "cass-1"},
				Port:       9042,
				Keyspace:   "temporal",
				User:       "temporal",
				Datacenter: "dc1",
			}
			c.Spec.Persistence.DefaultStore = temporalv1alpha1.DatastoreSpec{Cassandra: cass}
			visCass := *cass
			visCass.Keyspace = "temporal_visibility"
			c.Spec.Persistence.VisibilityStore = temporalv1alpha1.DatastoreSpec{Cassandra: &visCass}
			return c
		},
		"es-visibility": func() *temporalv1alpha1.TemporalCluster {
			c := baseCluster()
			c.Spec.Persistence.VisibilityStore = temporalv1alpha1.DatastoreSpec{
				Elasticsearch: &temporalv1alpha1.ElasticsearchDatastoreSpec{
					URL:     "elasticsearch.default.svc:9200",
					Version: "v8",
				},
			}
			return c
		},
		"archival": func() *temporalv1alpha1.TemporalCluster {
			c := baseCluster()
			c.Spec.Archival = &temporalv1alpha1.ArchivalSpec{}
			return c
		},
		"internal-frontend": func() *temporalv1alpha1.TemporalCluster {
			c := baseCluster()
			c.Spec.Services.InternalFrontend = &temporalv1alpha1.InternalFrontendSpec{Enabled: true}
			return c
		},
		"multi-cluster": func() *temporalv1alpha1.TemporalCluster {
			c := baseCluster()
			c.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
				EnableGlobalNamespace:    true,
				FailoverVersionIncrement: ptrInt32(100),
				CurrentClusterName:       "clusterA",
				InitialFailoverVersion:   ptrInt32(1),
				MasterClusterName:        "clusterA",
			}
			return c
		},
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			out, err := RenderClusterConfig(build(), opts)
			if err != nil {
				t.Fatalf("render: %v", err)
			}

			// The rendered output must be valid YAML.
			var parsed map[string]interface{}
			if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
				t.Fatalf("rendered config is not valid YAML: %v\n%s", err, out)
			}

			golden := filepath.Join("testdata", "golden", "1.31", name+".yaml")
			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, []byte(out), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("reading golden (run `make test-golden-update`): %v", err)
			}
			if string(want) != out {
				t.Errorf("rendered config does not match golden %s\n--- got ---\n%s", golden, out)
			}
		})
	}
}

// TestRenderConfigPasswordCommand verifies that passwordCommand renders as
// Temporal's structured config.PasswordCommandConfig ({command, args}) rather
// than a YAML scalar string. Temporal fails to load a scalar with:
// "cannot unmarshal !!str into config.PasswordCommandConfig".
func TestRenderConfigPasswordCommand(t *testing.T) {
	const cmd = "until [ -s /azure/pgpass ]; do sleep 1; done; cat /azure/pgpass"
	out, err := RenderClusterConfig(baseCluster(), BuildOptions{
		BindOnIP:                       "0.0.0.0",
		BroadcastAddress:               "10.0.0.1",
		DefaultStorePasswordCommand:    cmd,
		VisibilityStorePasswordCommand: cmd,
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	// A scalar passwordCommand is the bug we are guarding against.
	if strings.Contains(out, `passwordCommand: "`) {
		t.Errorf("passwordCommand rendered as a scalar string\n%s", out)
	}

	// The rendered YAML must shape passwordCommand as {command, args:[-c, cmd]}.
	type sqlStoreCfg struct {
		PasswordCommand *struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"passwordCommand"`
		Password string `json:"password"`
	}
	var parsed struct {
		Persistence struct {
			Datastores map[string]struct {
				SQL sqlStoreCfg `json:"sql"`
			} `json:"datastores"`
		} `json:"persistence"`
	}
	if err := yaml.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("rendered config is not valid YAML: %v\n%s", err, out)
	}

	for name, ds := range parsed.Persistence.Datastores {
		pc := ds.SQL.PasswordCommand
		if pc == nil {
			t.Errorf("datastore %q: passwordCommand missing", name)
			continue
		}
		if ds.SQL.Password != "" {
			t.Errorf("datastore %q: password and passwordCommand are mutually exclusive, got password %q", name, ds.SQL.Password)
		}
		if pc.Command != "sh" {
			t.Errorf("datastore %q: command = %q, want %q", name, pc.Command, "sh")
		}
		if len(pc.Args) != 2 || pc.Args[0] != "-c" || pc.Args[1] != cmd {
			t.Errorf("datastore %q: args = %v, want [-c %q]", name, pc.Args, cmd)
		}
	}
}

func TestBuildConfigDataClusterMetadata(t *testing.T) {
	c := baseCluster()
	c.Spec.ClusterMetadata = &temporalv1alpha1.ClusterMetadataSpec{
		EnableGlobalNamespace:    true,
		FailoverVersionIncrement: ptrInt32(100),
		CurrentClusterName:       "clusterA",
		InitialFailoverVersion:   ptrInt32(1),
		MasterClusterName:        "clusterA",
	}
	data, err := BuildConfigData(c, BuildOptions{
		DefaultStorePassword:    "p",
		VisibilityStorePassword: "p",
	})
	if err != nil {
		t.Fatalf("BuildConfigData: %v", err)
	}
	if !data.EnableGlobalNamespace {
		t.Error("EnableGlobalNamespace should be true")
	}
	if data.FailoverVersionIncrement != 100 {
		t.Errorf("FailoverVersionIncrement = %d, want 100", data.FailoverVersionIncrement)
	}
	if data.CurrentClusterName != "clusterA" {
		t.Errorf("CurrentClusterName = %q, want %q", data.CurrentClusterName, "clusterA")
	}
	if data.InitialFailoverVersion != 1 {
		t.Errorf("InitialFailoverVersion = %d, want 1", data.InitialFailoverVersion)
	}
	if data.MasterClusterName != "clusterA" {
		t.Errorf("MasterClusterName = %q, want %q", data.MasterClusterName, "clusterA")
	}
}

func TestBuildConfigDataClusterMetadataDefaults(t *testing.T) {
	data, err := BuildConfigData(baseCluster(), BuildOptions{
		DefaultStorePassword:    "p",
		VisibilityStorePassword: "p",
	})
	if err != nil {
		t.Fatalf("BuildConfigData: %v", err)
	}
	if data.EnableGlobalNamespace != false {
		t.Error("default EnableGlobalNamespace should be false")
	}
	if data.FailoverVersionIncrement != 10 {
		t.Errorf("default FailoverVersionIncrement = %d, want 10", data.FailoverVersionIncrement)
	}
	if data.CurrentClusterName != "active" {
		t.Errorf("default CurrentClusterName = %q, want %q", data.CurrentClusterName, "active")
	}
	if data.InitialFailoverVersion != 1 {
		t.Errorf("default InitialFailoverVersion = %d, want 1", data.InitialFailoverVersion)
	}
	if data.MasterClusterName != "active" {
		t.Errorf("default MasterClusterName = %q, want %q", data.MasterClusterName, "active")
	}
}

func TestRenderConfigMTLSServerNames(t *testing.T) {
	c := baseCluster()
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{
		Provider:  "cert-manager",
		IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"},
	}
	out, err := RenderClusterConfig(c, BuildOptions{
		BindOnIP:                "0.0.0.0",
		BroadcastAddress:        "10.0.0.1",
		DefaultStorePassword:    "default-pw",
		VisibilityStorePassword: "visibility-pw",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		`serverName: "test-internode"`,
		`serverName: "test-frontend.default.svc.cluster.local"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q\n%s", want, out)
		}
	}
}

func TestRenderConfigMTLSSystemWorker(t *testing.T) {
	c := baseCluster()
	c.Spec.MTLS = &temporalv1alpha1.MTLSSpec{
		Provider:  "cert-manager",
		IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"},
	}
	out, err := RenderClusterConfig(c, BuildOptions{
		BindOnIP:                "0.0.0.0",
		BroadcastAddress:        "10.0.0.1",
		DefaultStorePassword:    "default-pw",
		VisibilityStorePassword: "visibility-pw",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"systemWorker:",
		"certFile: /etc/temporal/certs/internode/tls.crt",
		"keyFile: /etc/temporal/certs/internode/tls.key",
		`serverName: "test-frontend.default.svc.cluster.local"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q\n%s", want, out)
		}
	}

	// Without mTLS there must be no systemWorker block.
	plain, err := RenderClusterConfig(baseCluster(), BuildOptions{
		BindOnIP: "0.0.0.0", BroadcastAddress: "10.0.0.1",
		DefaultStorePassword: "p", VisibilityStorePassword: "p",
	})
	if err != nil {
		t.Fatalf("render plain: %v", err)
	}
	if strings.Contains(plain, "systemWorker:") {
		t.Errorf("non-mtls config must not contain systemWorker block\n%s", plain)
	}
}

func TestRenderConfig_AuthorizationEntra(t *testing.T) {
	cluster := baseCluster()
	cluster.Spec.Authorization = &temporalv1alpha1.AuthorizationSpec{
		Entra: &temporalv1alpha1.EntraAuthSpec{TenantID: "11111111-2222-3333-4444-555555555555"},
	}

	out, err := RenderClusterConfig(cluster, BuildOptions{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, want := range []string{
		"authorization:",
		`authorizer: "default"`,
		`claimMapper: "default"`,
		`permissionsClaimName: "roles"`,
		"jwtKeyProvider:",
		"https://login.microsoftonline.com/11111111-2222-3333-4444-555555555555/discovery/v2.0/keys",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q\n---\n%s", want, out)
		}
	}
}

func TestRenderConfig_AuthorizationExplicitAndPassthrough(t *testing.T) {
	cluster := baseCluster()
	cluster.Spec.Authorization = &temporalv1alpha1.AuthorizationSpec{
		JWTKeyProvider: &temporalv1alpha1.JWTKeyProviderSpec{
			KeySourceURIs:   []string{"https://example.test/jwks"},
			RefreshInterval: &metav1.Duration{Duration: 2 * time.Minute},
		},
		Config: &runtime.RawExtension{Raw: []byte(`{"permissionsClaimName":"perms"}`)},
	}

	out, err := RenderClusterConfig(cluster, BuildOptions{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"https://example.test/jwks",
		`refreshInterval: "2m0s"`,
		"permissionsClaimName: perms",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q\n---\n%s", want, out)
		}
	}
	if strings.Contains(out, `permissionsClaimName: "permissions"`) {
		t.Errorf("passthrough should override the modeled permissionsClaimName, got duplicate\n---\n%s", out)
	}
}

func TestRenderConfig_AuthorizationEntraJWTProviderPassthrough(t *testing.T) {
	// TDD: Entra + passthrough jwtKeyProvider — the passthrough must win and
	// there must be exactly one jwtKeyProvider: key in the rendered YAML.
	cluster := baseCluster()
	cluster.Spec.Authorization = &temporalv1alpha1.AuthorizationSpec{
		Entra: &temporalv1alpha1.EntraAuthSpec{TenantID: "11111111-2222-3333-4444-555555555555"},
		Config: &runtime.RawExtension{
			Raw: []byte(`{"jwtKeyProvider":{"keySourceURIs":["https://override/jwks"]}}`),
		},
	}

	out, err := RenderClusterConfig(cluster, BuildOptions{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	// The passthrough URI must be present.
	if !strings.Contains(out, "https://override/jwks") {
		t.Errorf("rendered config missing override URI\n---\n%s", out)
	}
	// The entra-derived JWKS URI must NOT appear.
	if strings.Contains(out, "discovery/v2.0/keys") {
		t.Errorf("rendered config must not contain entra-derived JWKS URI when passthrough jwtKeyProvider is set\n---\n%s", out)
	}
	// Exactly one jwtKeyProvider: occurrence.
	if count := strings.Count(out, "jwtKeyProvider:"); count != 1 {
		t.Errorf("expected exactly 1 'jwtKeyProvider:' line, got %d\n---\n%s", count, out)
	}
}

func TestRenderConfig_AuthorizationAuthenticateOnly(t *testing.T) {
	cluster := baseCluster()
	cluster.Spec.Authorization = &temporalv1alpha1.AuthorizationSpec{
		Entra:      &temporalv1alpha1.EntraAuthSpec{TenantID: "tid"},
		Authorizer: ptr[string](""),
	}

	out, err := RenderClusterConfig(cluster, BuildOptions{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	// Explicit empty string must emit authorizer: "" (no-op), never "default".
	if !strings.Contains(out, `authorizer: ""`) {
		t.Errorf(`rendered config missing authorizer: ""\n---\n%s`, out)
	}
	if strings.Contains(out, `authorizer: "default"`) {
		t.Errorf(`rendered config must not contain authorizer: "default" when explicit "" is set\n---\n%s`, out)
	}
	// JWT / Entra config must still be present.
	if !strings.Contains(out, "https://login.microsoftonline.com/tid/discovery/v2.0/keys") {
		t.Errorf("rendered config missing Entra JWKS URL\n---\n%s", out)
	}
	if !strings.Contains(out, `permissionsClaimName: "roles"`) {
		t.Errorf(`rendered config missing permissionsClaimName: "roles"\n---\n%s`, out)
	}
}
