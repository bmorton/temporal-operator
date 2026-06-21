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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// requireInitContainerByName fails the test if the initContainer is not found.
func requireInitContainerByName(t *testing.T, job *batchv1.Job, name string) {
	t.Helper()
	for _, ic := range job.Spec.Template.Spec.InitContainers {
		if ic.Name == name {
			return
		}
	}
	t.Errorf("%s initContainer not found", name)
}

// requireJobVolume fails the test if the volume is not found.
func requireJobVolume(t *testing.T, job *batchv1.Job, name string) {
	t.Helper()
	for _, vol := range job.Spec.Template.Spec.Volumes {
		if vol.Name == name {
			return
		}
	}
	t.Errorf("%s volume not found", name)
}

// requireJobVolumeMount fails the test if the container doesn't have the mount.
func requireJobVolumeMount(t *testing.T, container *corev1.Container, volumeName, mountPath string) {
	t.Helper()
	for _, mount := range container.VolumeMounts {
		if mount.Name == volumeName && mount.MountPath == mountPath {
			return
		}
	}
	t.Errorf("%s volume mount not found on container", volumeName)
}

// requireArgsContain fails the test if the args don't contain the expected values.
func requireArgsContain(t *testing.T, args []string, key, value string) {
	t.Helper()
	if !slices.Contains(args, key) {
		t.Errorf("args missing %s", key)
		return
	}
	if value != "" && !slices.Contains(args, value) {
		t.Errorf("args missing %s %s", key, value)
	}
}

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
	wiLabel := job.Spec.Template.Labels[AzureWILabel]
	if wiLabel != wiLabelValue {
		t.Errorf("Azure WI label = %q, want %q", wiLabel, wiLabelValue)
	}

	requireInitContainerByName(t, job, azureTokenInitName)
	requireJobVolume(t, job, AzureTokenVolumeName)

	verifyInspectorJobContainer(t, job)
}

func verifyInspectorJobContainer(t *testing.T, job *batchv1.Job) {
	t.Helper()
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
	idx := slices.Index(main.Args, "--password-file")
	if idx == -1 || idx+1 >= len(main.Args) || main.Args[idx+1] != AzureTokenFile {
		t.Errorf("--password-file value = %v, want %q", main.Args, AzureTokenFile)
	}

	requireArgsContain(t, main.Args, "--host", "pg.example.com")
	requireArgsContain(t, main.Args, "--db", "temporal")
	requireArgsContain(t, main.Args, "--user", "temporal")

	// Verify TerminationMessagePolicy
	if main.TerminationMessagePolicy != corev1.TerminationMessageFallbackToLogsOnError {
		t.Errorf("TerminationMessagePolicy = %q, want FallbackToLogsOnError", main.TerminationMessagePolicy)
	}

	requireJobVolumeMount(t, &main, AzureTokenVolumeName, AzureTokenMountPath)
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
