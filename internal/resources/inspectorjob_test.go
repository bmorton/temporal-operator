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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func TestInspectorJobName(t *testing.T) {
	got := InspectorJobName("tc", StoreDefault)
	want := "tc-inspect-default"
	if got != want {
		t.Errorf("InspectorJobName() = %q, want %q", got, want)
	}
}

func TestBuildInspectorJob(t *testing.T) {
	cluster := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tc", Namespace: "ns"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Version: "1.31.1",
			Persistence: temporalv1alpha1.PersistenceSpec{
				AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
					ClientID: "test-client-id",
				},
			},
		},
	}

	spec := &temporalv1alpha1.SQLDatastoreSpec{
		PluginName: "postgres12",
		Host:       "pg.example.com",
		Port:       5432,
		Database:   "temporal",
		User:       "temporal",
	}

	job := BuildInspectorJob(InspectorJobParams{
		Cluster:       cluster,
		Store:         StoreDefault,
		SQLSpec:       spec,
		OperatorImage: "operator:v1.0.0",
	})

	// Verify Job metadata
	if job.Name != "tc-inspect-default" {
		t.Errorf("Job.Name = %q, want %q", job.Name, "tc-inspect-default")
	}
	if job.Namespace != "ns" {
		t.Errorf("Job.Namespace = %q, want %q", job.Namespace, "ns")
	}

	// Verify BackoffLimit
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 0 {
		t.Errorf("Job.Spec.BackoffLimit = %v, want 0", job.Spec.BackoffLimit)
	}

	// Verify TTLSecondsAfterFinished
	if job.Spec.TTLSecondsAfterFinished == nil {
		t.Error("Job.Spec.TTLSecondsAfterFinished is nil, want ~300")
	} else if *job.Spec.TTLSecondsAfterFinished < 250 || *job.Spec.TTLSecondsAfterFinished > 350 {
		t.Errorf("Job.Spec.TTLSecondsAfterFinished = %d, want ~300", *job.Spec.TTLSecondsAfterFinished)
	}

	// Verify RestartPolicy
	if job.Spec.Template.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("RestartPolicy = %q, want Never", job.Spec.Template.Spec.RestartPolicy)
	}

	// Verify ServiceAccount
	expectedSA := AzureServiceAccountName(cluster)
	if job.Spec.Template.Spec.ServiceAccountName != expectedSA {
		t.Errorf("ServiceAccountName = %q, want %q", job.Spec.Template.Spec.ServiceAccountName, expectedSA)
	}

	// Verify WI pod label
	wiLabel := job.Spec.Template.ObjectMeta.Labels[AzureWILabel]
	if wiLabel != "true" {
		t.Errorf("Azure WI label = %q, want %q", wiLabel, "true")
	}

	// Verify azure-token initContainer
	if len(job.Spec.Template.Spec.InitContainers) == 0 {
		t.Fatal("No initContainers found, expected azure-token")
	}
	foundAzureInit := false
	for _, ic := range job.Spec.Template.Spec.InitContainers {
		if ic.Name == "azure-token" {
			foundAzureInit = true
			break
		}
	}
	if !foundAzureInit {
		t.Error("azure-token initContainer not found")
	}

	// Verify /azure volume
	foundAzureVolume := false
	for _, vol := range job.Spec.Template.Spec.Volumes {
		if vol.Name == AzureTokenVolumeName {
			foundAzureVolume = true
			break
		}
	}
	if !foundAzureVolume {
		t.Error("azure-token volume not found")
	}

	// Verify main container
	if len(job.Spec.Template.Spec.Containers) == 0 {
		t.Fatal("No containers found")
	}
	main := job.Spec.Template.Spec.Containers[0]
	if main.Name != "inspect" {
		t.Errorf("main container name = %q, want %q", main.Name, "inspect")
	}

	// Verify operator image
	if main.Image != "operator:v1.0.0" {
		t.Errorf("main container image = %q, want %q", main.Image, "operator:v1.0.0")
	}

	// Verify args
	if len(main.Args) == 0 || main.Args[0] != "inspect" {
		t.Errorf("args[0] = %v, want %q", main.Args, "inspect")
	}

	// Verify --password-file /azure/pgpass
	if !slices.Contains(main.Args, "--password-file") {
		t.Error("args missing --password-file")
	}
	idx := slices.Index(main.Args, "--password-file")
	if idx == -1 || idx+1 >= len(main.Args) || main.Args[idx+1] != AzureTokenFile {
		t.Errorf("--password-file value = %v, want %q", main.Args, AzureTokenFile)
	}

	// Verify --host, --db, --user
	if !slices.Contains(main.Args, "--host") || !slices.Contains(main.Args, "pg.example.com") {
		t.Error("args missing --host pg.example.com")
	}
	if !slices.Contains(main.Args, "--db") || !slices.Contains(main.Args, "temporal") {
		t.Error("args missing --db temporal")
	}
	if !slices.Contains(main.Args, "--user") || !slices.Contains(main.Args, "temporal") {
		t.Error("args missing --user temporal")
	}

	// Verify TerminationMessagePolicy
	if main.TerminationMessagePolicy != corev1.TerminationMessageFallbackToLogsOnError {
		t.Errorf("TerminationMessagePolicy = %q, want FallbackToLogsOnError", main.TerminationMessagePolicy)
	}

	// Verify /azure volume mount on main container
	foundMount := false
	for _, mount := range main.VolumeMounts {
		if mount.Name == AzureTokenVolumeName && mount.MountPath == AzureTokenMountPath {
			foundMount = true
			break
		}
	}
	if !foundMount {
		t.Error("azure-token volume mount not found on main container")
	}
}

func TestBuildInspectorJobWithTLS(t *testing.T) {
	cluster := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tc", Namespace: "ns"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Version: "1.31.1",
			Persistence: temporalv1alpha1.PersistenceSpec{
				AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
					ClientID: "test-client-id",
				},
			},
		},
	}

	spec := &temporalv1alpha1.SQLDatastoreSpec{
		PluginName: "postgres12",
		Host:       "pg.example.com",
		Port:       5432,
		Database:   "temporal",
		User:       "temporal",
		TLS: &temporalv1alpha1.DatastoreTLSSpec{
			Enabled: true,
		},
	}

	job := BuildInspectorJob(InspectorJobParams{
		Cluster:       cluster,
		Store:         StoreDefault,
		SQLSpec:       spec,
		OperatorImage: "operator:v1.0.0",
	})

	main := job.Spec.Template.Spec.Containers[0]

	// Verify --tls is present when TLS enabled
	if !slices.Contains(main.Args, "--tls") {
		t.Error("args missing --tls when TLS enabled")
	}
}

func TestBuildInspectorJobWithoutTLS(t *testing.T) {
	cluster := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "tc", Namespace: "ns"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Version: "1.31.1",
			Persistence: temporalv1alpha1.PersistenceSpec{
				AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
					ClientID: "test-client-id",
				},
			},
		},
	}

	spec := &temporalv1alpha1.SQLDatastoreSpec{
		PluginName: "postgres12",
		Host:       "pg.example.com",
		Port:       5432,
		Database:   "temporal",
		User:       "temporal",
	}

	job := BuildInspectorJob(InspectorJobParams{
		Cluster:       cluster,
		Store:         StoreDefault,
		SQLSpec:       spec,
		OperatorImage: "operator:v1.0.0",
	})

	main := job.Spec.Template.Spec.Containers[0]

	// Verify --tls is not present when TLS is nil
	if slices.Contains(main.Args, "--tls") {
		t.Error("args contains --tls when TLS is nil")
	}
}
