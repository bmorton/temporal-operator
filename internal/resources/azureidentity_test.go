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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func TestAzureWorkloadIdentityEnabled(t *testing.T) {
	tests := []struct {
		name     string
		cluster  *temporalv1alpha1.TemporalCluster
		expected bool
	}{
		{
			name: "enabled when azure workload identity is set",
			cluster: &temporalv1alpha1.TemporalCluster{
				Spec: temporalv1alpha1.TemporalClusterSpec{
					Persistence: temporalv1alpha1.PersistenceSpec{
						AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
							ClientID: "test-client-id",
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "disabled when azure workload identity is nil",
			cluster: &temporalv1alpha1.TemporalCluster{
				Spec: temporalv1alpha1.TemporalClusterSpec{
					Persistence: temporalv1alpha1.PersistenceSpec{},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AzureWorkloadIdentityEnabled(tt.cluster)
			if got != tt.expected {
				t.Errorf("AzureWorkloadIdentityEnabled() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestAzureServiceAccountName(t *testing.T) {
	tests := []struct {
		name     string
		cluster  *temporalv1alpha1.TemporalCluster
		expected string
	}{
		{
			name: "default service account name",
			cluster: &temporalv1alpha1.TemporalCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cluster"},
				Spec: temporalv1alpha1.TemporalClusterSpec{
					Persistence: temporalv1alpha1.PersistenceSpec{
						AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
							ClientID: "test-client-id",
						},
					},
				},
			},
			expected: "my-cluster-azure",
		},
		{
			name: "custom service account name",
			cluster: &temporalv1alpha1.TemporalCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "my-cluster"},
				Spec: temporalv1alpha1.TemporalClusterSpec{
					Persistence: temporalv1alpha1.PersistenceSpec{
						AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
							ClientID:           "test-client-id",
							ServiceAccountName: "custom-sa",
						},
					},
				},
			},
			expected: "custom-sa",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AzureServiceAccountName(tt.cluster)
			if got != tt.expected {
				t.Errorf("AzureServiceAccountName() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestAzurePasswordCommand(t *testing.T) {
	expected := "sh -c 'until [ -s /azure/pgpass ]; do sleep 1; done; cat /azure/pgpass'"
	got := AzurePasswordCommand()
	if got != expected {
		t.Errorf("AzurePasswordCommand() = %q, want %q", got, expected)
	}
}

func TestBuildAzureServiceAccount(t *testing.T) {
	cluster := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "azure-e2e",
			Namespace: "test-ns",
		},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Persistence: temporalv1alpha1.PersistenceSpec{
				AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
					ClientID: "test-client-id",
				},
			},
		},
	}

	sa := BuildAzureServiceAccount(cluster)

	if sa == nil {
		t.Fatal("BuildAzureServiceAccount() returned nil")
	}

	if sa.Name != "azure-e2e-azure" {
		t.Errorf("sa.Name = %q, want %q", sa.Name, "azure-e2e-azure")
	}

	if sa.Namespace != "test-ns" {
		t.Errorf("sa.Namespace = %q, want %q", sa.Namespace, "test-ns")
	}

	clientIDAnnotation := "azure.workload.identity/client-id"
	if sa.Annotations[clientIDAnnotation] != "test-client-id" {
		t.Errorf("sa.Annotations[%q] = %q, want %q", clientIDAnnotation, sa.Annotations[clientIDAnnotation], "test-client-id")
	}
}

func TestAzureTokenInitContainer(t *testing.T) {
	cluster := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Persistence: temporalv1alpha1.PersistenceSpec{
				AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
					ClientID: "test-client-id",
				},
			},
		},
	}

	container := AzureTokenInitContainer(cluster)

	if container.Name != "azure-token" {
		t.Errorf("container.Name = %q, want %q", container.Name, "azure-token")
	}

	if container.Image != DefaultAzureCLIImage {
		t.Errorf("container.Image = %q, want %q", container.Image, DefaultAzureCLIImage)
	}

	// Check that it has the expected volume mount
	found := false
	for _, mount := range container.VolumeMounts {
		if mount.Name == AzureTokenVolumeName && mount.MountPath == AzureTokenMountPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("container does not have expected volume mount %q at %q", AzureTokenVolumeName, AzureTokenMountPath)
	}
}

func TestAzureTokenRefresherSidecar(t *testing.T) {
	refreshInterval := &metav1.Duration{Duration: 10 * time.Minute}
	cluster := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Persistence: temporalv1alpha1.PersistenceSpec{
				AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
					ClientID:        "test-client-id",
					RefreshInterval: refreshInterval,
				},
			},
		},
	}

	container := AzureTokenRefresherSidecar(cluster)

	if container.Name != "azure-token-refresher" {
		t.Errorf("container.Name = %q, want %q", container.Name, "azure-token-refresher")
	}

	if container.Image != DefaultAzureCLIImage {
		t.Errorf("container.Image = %q, want %q", container.Image, DefaultAzureCLIImage)
	}

	// Check that it has the expected volume mount
	found := false
	for _, mount := range container.VolumeMounts {
		if mount.Name == AzureTokenVolumeName && mount.MountPath == AzureTokenMountPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("container does not have expected volume mount %q at %q", AzureTokenVolumeName, AzureTokenMountPath)
	}
}

func TestApplyAzureServerWorkloadIdentity(t *testing.T) {
	cluster := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "azure-e2e",
			Namespace: "test-ns",
		},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Persistence: temporalv1alpha1.PersistenceSpec{
				AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
					ClientID: "test-client-id",
				},
			},
		},
	}

	var meta metav1.ObjectMeta
	spec := corev1.PodSpec{
		Containers: []corev1.Container{{Name: "temporal"}},
	}

	ApplyAzureServerWorkloadIdentity(&meta, &spec, cluster, "temporal")

	// Check WI label
	if meta.Labels[AzureWILabel] != "true" {
		t.Errorf("meta.Labels[%q] = %q, want %q", AzureWILabel, meta.Labels[AzureWILabel], "true")
	}

	// Check ServiceAccount
	if spec.ServiceAccountName != "azure-e2e-azure" {
		t.Errorf("spec.ServiceAccountName = %q, want %q", spec.ServiceAccountName, "azure-e2e-azure")
	}

	// Check volume
	foundVolume := false
	for _, vol := range spec.Volumes {
		if vol.Name == AzureTokenVolumeName && vol.EmptyDir != nil {
			foundVolume = true
			break
		}
	}
	if !foundVolume {
		t.Error("azure-token emptyDir volume not found")
	}

	// Check mount on main container
	foundMount := false
	for _, container := range spec.Containers {
		if container.Name == "temporal" {
			for _, mount := range container.VolumeMounts {
				if mount.Name == AzureTokenVolumeName && mount.MountPath == AzureTokenMountPath {
					foundMount = true
					break
				}
			}
			break
		}
	}
	if !foundMount {
		t.Error("azure-token volume mount not found on temporal container")
	}

	// Check sidecar
	foundSidecar := false
	for _, container := range spec.Containers {
		if container.Name == "azure-token-refresher" {
			foundSidecar = true
			break
		}
	}
	if !foundSidecar {
		t.Error("azure-token-refresher sidecar not found")
	}
}

func TestApplyAzureServerWorkloadIdentityIdempotent(t *testing.T) {
	cluster := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "azure-e2e",
			Namespace: "test-ns",
		},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Persistence: temporalv1alpha1.PersistenceSpec{
				AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
					ClientID: "test-client-id",
				},
			},
		},
	}

	var meta metav1.ObjectMeta
	spec := corev1.PodSpec{
		Containers: []corev1.Container{{Name: "temporal"}},
	}

	// Apply twice
	ApplyAzureServerWorkloadIdentity(&meta, &spec, cluster, "temporal")
	ApplyAzureServerWorkloadIdentity(&meta, &spec, cluster, "temporal")

	// Should have exactly one volume
	volumeCount := 0
	for _, vol := range spec.Volumes {
		if vol.Name == AzureTokenVolumeName {
			volumeCount++
		}
	}
	if volumeCount != 1 {
		t.Errorf("found %d azure-token volumes, want 1", volumeCount)
	}

	// Should have exactly one sidecar
	sidecarCount := 0
	for _, container := range spec.Containers {
		if container.Name == "azure-token-refresher" {
			sidecarCount++
		}
	}
	if sidecarCount != 1 {
		t.Errorf("found %d azure-token-refresher sidecars, want 1", sidecarCount)
	}
}

func TestApplyAzureSchemaWorkloadIdentity(t *testing.T) {
	cluster := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "azure-e2e",
			Namespace: "test-ns",
		},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Persistence: temporalv1alpha1.PersistenceSpec{
				AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
					ClientID: "test-client-id",
				},
			},
		},
	}

	var meta metav1.ObjectMeta
	spec := corev1.PodSpec{
		Containers: []corev1.Container{{Name: "schema"}},
	}

	ApplyAzureSchemaWorkloadIdentity(&meta, &spec, cluster, "schema")

	// Check WI label
	if meta.Labels[AzureWILabel] != "true" {
		t.Errorf("meta.Labels[%q] = %q, want %q", AzureWILabel, meta.Labels[AzureWILabel], "true")
	}

	// Check ServiceAccount
	if spec.ServiceAccountName != "azure-e2e-azure" {
		t.Errorf("spec.ServiceAccountName = %q, want %q", spec.ServiceAccountName, "azure-e2e-azure")
	}

	// Check volume
	foundVolume := false
	for _, vol := range spec.Volumes {
		if vol.Name == AzureTokenVolumeName && vol.EmptyDir != nil {
			foundVolume = true
			break
		}
	}
	if !foundVolume {
		t.Error("azure-token emptyDir volume not found")
	}

	// Check mount on main container
	foundMount := false
	for _, container := range spec.Containers {
		if container.Name == "schema" {
			for _, mount := range container.VolumeMounts {
				if mount.Name == AzureTokenVolumeName && mount.MountPath == AzureTokenMountPath {
					foundMount = true
					break
				}
			}
			break
		}
	}
	if !foundMount {
		t.Error("azure-token volume mount not found on schema container")
	}

	// Check initContainer
	foundInit := false
	for _, container := range spec.InitContainers {
		if container.Name == "azure-token" {
			foundInit = true
			break
		}
	}
	if !foundInit {
		t.Error("azure-token initContainer not found")
	}

	// Should NOT have sidecar
	for _, container := range spec.Containers {
		if container.Name == "azure-token-refresher" {
			t.Error("azure-token-refresher sidecar should not be present for schema workload")
		}
	}
}

func TestApplyAzureSchemaWorkloadIdentityIdempotent(t *testing.T) {
	cluster := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "azure-e2e",
			Namespace: "test-ns",
		},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Persistence: temporalv1alpha1.PersistenceSpec{
				AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
					ClientID: "test-client-id",
				},
			},
		},
	}

	var meta metav1.ObjectMeta
	spec := corev1.PodSpec{
		Containers: []corev1.Container{{Name: "schema"}},
	}

	// Apply twice
	ApplyAzureSchemaWorkloadIdentity(&meta, &spec, cluster, "schema")
	ApplyAzureSchemaWorkloadIdentity(&meta, &spec, cluster, "schema")

	// Should have exactly one volume
	volumeCount := 0
	for _, vol := range spec.Volumes {
		if vol.Name == AzureTokenVolumeName {
			volumeCount++
		}
	}
	if volumeCount != 1 {
		t.Errorf("found %d azure-token volumes, want 1", volumeCount)
	}

	// Should have exactly one initContainer
	initCount := 0
	for _, container := range spec.InitContainers {
		if container.Name == "azure-token" {
			initCount++
		}
	}
	if initCount != 1 {
		t.Errorf("found %d azure-token initContainers, want 1", initCount)
	}
}
