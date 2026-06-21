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

package resources

import (
	"slices"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// wiLabelValue is the value of the Azure Workload Identity use label.
const wiLabelValue = "true"

func testCluster() *temporalv1alpha1.TemporalCluster {
	return &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tc", Namespace: "ns"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Version: "1.31.1",
		},
	}
}

func sqlSpec() *temporalv1alpha1.SQLDatastoreSpec {
	return &temporalv1alpha1.SQLDatastoreSpec{
		PluginName: "postgres12",
		Host:       "pg",
		Port:       5432,
		Database:   "temporal",
		User:       "temporal",
		PasswordSecretRef: &temporalv1alpha1.SecretKeyReference{
			Name: "store",
			Key:  "password",
		},
	}
}

func TestSchemaJobName(t *testing.T) {
	got := SchemaJobName("tc", StoreDefault, ActionSetup)
	if got != "tc-schema-default-setup" {
		t.Errorf("unexpected job name %q", got)
	}
}

func TestBuildSchemaJobSetup(t *testing.T) {
	job, err := BuildSchemaJob(SchemaJobParams{
		Cluster:          testCluster(),
		SQLSpec:          sqlSpec(),
		Store:            StoreDefault,
		Action:           ActionSetup,
		SchemaVersionDir: "v12",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job.Name != "tc-schema-default-setup" || job.Namespace != "ns" {
		t.Errorf("unexpected metadata: %s/%s", job.Namespace, job.Name)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if c.Image != "temporalio/admin-tools:1.31.1" {
		t.Errorf("unexpected image %q", c.Image)
	}
	if c.Command[0] != "temporal-sql-tool" {
		t.Errorf("unexpected command %v", c.Command)
	}
	if !slices.Contains(c.Args, "setup-schema") || !slices.Contains(c.Args, "0.0") {
		t.Errorf("expected setup-schema args, got %v", c.Args)
	}
	if !slices.Contains(c.Args, "--plugin") || !slices.Contains(c.Args, "postgres12") {
		t.Errorf("expected plugin args, got %v", c.Args)
	}
	if len(c.Env) != 1 || c.Env[0].Name != "SQL_PASSWORD" {
		t.Errorf("expected SQL_PASSWORD env from secret, got %v", c.Env)
	}
	if *job.Spec.BackoffLimit != 3 {
		t.Errorf("expected backoffLimit 3, got %d", *job.Spec.BackoffLimit)
	}
	if *job.Spec.TTLSecondsAfterFinished != 600 {
		t.Errorf("expected ttl 600, got %d", *job.Spec.TTLSecondsAfterFinished)
	}
}

func TestBuildSchemaJobUpdateVisibility(t *testing.T) {
	job, err := BuildSchemaJob(SchemaJobParams{
		Cluster:          testCluster(),
		SQLSpec:          sqlSpec(),
		Store:            StoreVisibility,
		Action:           ActionUpdate,
		SchemaVersionDir: "v12",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if !slices.Contains(c.Args, "update-schema") {
		t.Errorf("expected update-schema, got %v", c.Args)
	}
	wantDir := "/etc/temporal/schema/postgresql/v12/visibility/versioned"
	if !slices.Contains(c.Args, wantDir) {
		t.Errorf("expected schema dir %q in %v", wantDir, c.Args)
	}
}

func cassandraSpec() *temporalv1alpha1.CassandraDatastoreSpec {
	return &temporalv1alpha1.CassandraDatastoreSpec{
		Hosts:    []string{"cass-0", "cass-1"},
		Port:     9042,
		Keyspace: "temporal",
		User:     "temporal",
	}
}

func TestBuildCassandraSchemaJob(t *testing.T) {
	job, err := BuildSchemaJob(SchemaJobParams{
		Cluster:       testCluster(),
		CassandraSpec: cassandraSpec(),
		Store:         StoreDefault,
		Action:        ActionUpdate,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if c.Command[0] != "temporal-cassandra-tool" {
		t.Errorf("expected cassandra tool, got %v", c.Command)
	}
	if !slices.Contains(c.Args, "--keyspace") || !slices.Contains(c.Args, "temporal") {
		t.Errorf("expected keyspace arg, got %v", c.Args)
	}
	if !slices.Contains(c.Args, "--endpoint") || !slices.Contains(c.Args, "cass-0") {
		t.Errorf("expected endpoint to be first host, got %v", c.Args)
	}
	wantDir := "/etc/temporal/schema/cassandra/temporal/versioned"
	if !slices.Contains(c.Args, wantDir) {
		t.Errorf("expected cassandra schema dir, got %v", c.Args)
	}
}

func TestBuildSchemaJobPasswordCommand(t *testing.T) {
	spec := sqlSpec()
	spec.PasswordSecretRef = nil
	job, err := BuildSchemaJob(SchemaJobParams{
		Cluster:          testCluster(),
		SQLSpec:          spec,
		PasswordCommand:  "cat /azure/pgpass",
		Store:            StoreDefault,
		Action:           ActionSetup,
		SchemaVersionDir: "v12",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if len(c.Command) != 2 || c.Command[0] != "sh" || c.Command[1] != "-c" {
		t.Fatalf("expected sh -c wrapper, got command %v", c.Command)
	}
	if len(c.Args) != 1 || !strings.Contains(c.Args[0], `SQL_PASSWORD="$(cat /azure/pgpass)"`) {
		t.Errorf("expected SQL_PASSWORD exported from command, got args %v", c.Args)
	}
	if !strings.Contains(c.Args[0], "exec temporal-sql-tool") {
		t.Errorf("expected exec of temporal-sql-tool, got %v", c.Args)
	}
	for _, e := range c.Env {
		if e.Name == "SQL_PASSWORD" {
			t.Errorf("did not expect static SQL_PASSWORD env, got %+v", c.Env)
		}
	}
}

func TestBuildSchemaJobPodTemplate(t *testing.T) {
	raw := []byte(`{"serviceAccountName":"temporal-wi","initContainers":[{"name":"token","image":"mcr.microsoft.com/azure-cli"}]}`)
	job, err := BuildSchemaJob(SchemaJobParams{
		Cluster:          testCluster(),
		SQLSpec:          sqlSpec(),
		Store:            StoreDefault,
		Action:           ActionSetup,
		SchemaVersionDir: "v12",
		PodTemplate: &temporalv1alpha1.PodTemplateOverride{
			Labels: map[string]string{"azure.workload.identity/use": wiLabelValue},
			Spec:   &runtime.RawExtension{Raw: raw},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tpl := job.Spec.Template
	if tpl.Labels["azure.workload.identity/use"] != wiLabelValue {
		t.Errorf("expected WI label, got %v", tpl.Labels)
	}
	if tpl.Spec.ServiceAccountName != "temporal-wi" {
		t.Errorf("expected serviceAccountName from podTemplate, got %q", tpl.Spec.ServiceAccountName)
	}
	if len(tpl.Spec.InitContainers) != 1 || tpl.Spec.InitContainers[0].Name != "token" {
		t.Errorf("expected token initContainer, got %v", tpl.Spec.InitContainers)
	}
	if len(tpl.Spec.Containers) != 1 || tpl.Spec.Containers[0].Name != "schema" {
		t.Errorf("expected generated schema container preserved, got %v", tpl.Spec.Containers)
	}
}

func TestBuildSchemaJobPasswordCommandQuotesArgs(t *testing.T) {
	spec := sqlSpec()
	spec.PasswordSecretRef = nil
	spec.Database = "temp'oral" // arg with an embedded single quote
	job, err := BuildSchemaJob(SchemaJobParams{
		Cluster:          testCluster(),
		SQLSpec:          spec,
		PasswordCommand:  "cat /azure/pgpass",
		Store:            StoreDefault,
		Action:           ActionSetup,
		SchemaVersionDir: "v12",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	script := job.Spec.Template.Spec.Containers[0].Args[0]
	if !strings.Contains(script, `'temp'\''oral'`) {
		t.Errorf("expected single-quote-escaped database arg, got %q", script)
	}
}

func TestBuildSchemaJobCassandraIgnoresPasswordCommand(t *testing.T) {
	job, err := BuildSchemaJob(SchemaJobParams{
		Cluster:          testCluster(),
		CassandraSpec:    cassandraSpec(),
		PasswordCommand:  "cat /azure/pgpass",
		Store:            StoreDefault,
		Action:           ActionSetup,
		SchemaVersionDir: "v12",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if c.Command[0] == "sh" {
		t.Errorf("cassandra must not use sh -c wrapper, got command %v", c.Command)
	}
}
