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
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func testCluster() *temporalv1alpha1.TemporalCluster {
	return &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tc", Namespace: "ns"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Version: "1.31.2",
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
	job := BuildSchemaJob(SchemaJobParams{
		Cluster:          testCluster(),
		SQLSpec:          sqlSpec(),
		Store:            StoreDefault,
		Action:           ActionSetup,
		SchemaVersionDir: "v12",
	})

	if job.Name != "tc-schema-default-setup" || job.Namespace != "ns" {
		t.Errorf("unexpected metadata: %s/%s", job.Namespace, job.Name)
	}
	c := job.Spec.Template.Spec.Containers[0]
	if c.Image != "temporalio/admin-tools:1.31.2" {
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
	job := BuildSchemaJob(SchemaJobParams{
		Cluster:          testCluster(),
		SQLSpec:          sqlSpec(),
		Store:            StoreVisibility,
		Action:           ActionUpdate,
		SchemaVersionDir: "v12",
	})
	c := job.Spec.Template.Spec.Containers[0]
	if !slices.Contains(c.Args, "update-schema") {
		t.Errorf("expected update-schema, got %v", c.Args)
	}
	wantDir := "/etc/temporal/schema/postgresql/v12/visibility/versioned"
	if !slices.Contains(c.Args, wantDir) {
		t.Errorf("expected schema dir %q in %v", wantDir, c.Args)
	}
}
