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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

const (
	// AzureTokenVolumeName is the name of the shared emptyDir volume for Azure tokens.
	AzureTokenVolumeName = "azure-token"
	// AzureTokenMountPath is the mount path for the Azure token volume.
	AzureTokenMountPath = "/azure"
	// AzureTokenFile is the full path to the token file.
	AzureTokenFile = "/azure/pgpass"
	// DefaultAzureCLIImage is the default azure-cli image for token containers.
	DefaultAzureCLIImage = "mcr.microsoft.com/azure-cli:2.87.0"
	// DefaultAzureScope is the default Entra token scope for Azure Database for PostgreSQL.
	DefaultAzureScope = "https://ossrdbms-aad.database.windows.net/.default"
	// AzureWILabel is the Azure Workload Identity pod label.
	AzureWILabel = "azure.workload.identity/use"
	// AzureWILabelValue is the value for the Azure Workload Identity pod label.
	AzureWILabelValue = "true"
	// azureTokenRefresherName is the sidecar container name.
	azureTokenRefresherName = "azure-token-refresher"
	// azureTokenInitName is the initContainer name.
	azureTokenInitName = "azure-token"
)

const (
	// Default refresh interval for the token refresher sidecar (30 minutes).
	defaultRefreshIntervalSeconds = 1800
)

// AzureWorkloadIdentityEnabled returns true when Azure Workload Identity is configured.
func AzureWorkloadIdentityEnabled(cluster *temporalv1alpha1.TemporalCluster) bool {
	return cluster.Spec.Persistence.AzureWorkloadIdentity != nil
}

// AzureServiceAccountName returns the ServiceAccount name to use for Azure Workload Identity.
func AzureServiceAccountName(cluster *temporalv1alpha1.TemporalCluster) string {
	if cluster.Spec.Persistence.AzureWorkloadIdentity != nil &&
		cluster.Spec.Persistence.AzureWorkloadIdentity.ServiceAccountName != "" {
		return cluster.Spec.Persistence.AzureWorkloadIdentity.ServiceAccountName
	}
	return cluster.Name + "-azure"
}

// AzurePasswordCommand returns the passwordCommand shell snippet that waits for
// the token file and reads it. It is a snippet (not a full "sh -c '...'" string)
// because both consumers wrap it: the schema Job embeds it in "$(...)" and the
// Temporal server config renders it as command "sh" with args ["-c", <snippet>].
func AzurePasswordCommand() string {
	return "until [ -s /azure/pgpass ]; do sleep 1; done; cat /azure/pgpass"
}

// BuildAzureServiceAccount builds the ServiceAccount for Azure Workload Identity.
func BuildAzureServiceAccount(cluster *temporalv1alpha1.TemporalCluster) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      AzureServiceAccountName(cluster),
			Namespace: cluster.Namespace,
			Annotations: map[string]string{
				"azure.workload.identity/client-id": cluster.Spec.Persistence.AzureWorkloadIdentity.ClientID,
			},
		},
	}
}

// AzureTokenInitContainer returns the initContainer that obtains an Azure token once.
func AzureTokenInitContainer(cluster *temporalv1alpha1.TemporalCluster) corev1.Container {
	spec := cluster.Spec.Persistence.AzureWorkloadIdentity
	image := DefaultAzureCLIImage
	if spec.Image != "" {
		image = spec.Image
	}

	scope := DefaultAzureScope
	if spec.Scope != "" {
		scope = spec.Scope
	}

	script := fmt.Sprintf(`set -e
az login --service-principal \
  -u "$AZURE_CLIENT_ID" \
  --tenant "$AZURE_TENANT_ID" \
  --federated-token "$(cat "$AZURE_FEDERATED_TOKEN_FILE")" \
  --allow-no-subscriptions
az account get-access-token --scope %s \
  --query accessToken -o tsv > /azure/pgpass`, scope)

	return corev1.Container{
		Name:    azureTokenInitName,
		Image:   image,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{script},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      AzureTokenVolumeName,
				MountPath: AzureTokenMountPath,
			},
		},
	}
}

// AzureTokenRefresherSidecar returns the sidecar that refreshes the Azure token periodically.
func AzureTokenRefresherSidecar(cluster *temporalv1alpha1.TemporalCluster) corev1.Container {
	spec := cluster.Spec.Persistence.AzureWorkloadIdentity
	image := DefaultAzureCLIImage
	if spec.Image != "" {
		image = spec.Image
	}

	scope := DefaultAzureScope
	if spec.Scope != "" {
		scope = spec.Scope
	}

	refreshInterval := defaultRefreshIntervalSeconds
	if spec.RefreshInterval != nil {
		refreshInterval = int(spec.RefreshInterval.Seconds())
	}

	script := fmt.Sprintf(`set -e
az login --service-principal \
  -u "$AZURE_CLIENT_ID" \
  --tenant "$AZURE_TENANT_ID" \
  --federated-token "$(cat "$AZURE_FEDERATED_TOKEN_FILE")" \
  --allow-no-subscriptions
while true; do
  az account get-access-token --scope %s \
    --query accessToken -o tsv > /azure/pgpass.tmp
  mv /azure/pgpass.tmp /azure/pgpass
  sleep %d
done`, scope, refreshInterval)

	return corev1.Container{
		Name:    azureTokenRefresherName,
		Image:   image,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{script},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      AzureTokenVolumeName,
				MountPath: AzureTokenMountPath,
			},
		},
	}
}

// ApplyAzureServerWorkloadIdentity applies Azure Workload Identity configuration to a server pod.
// It sets the ServiceAccount, adds the WI label, adds the token volume, mounts the volume on the
// main container, and appends the token refresher sidecar. This function is idempotent.
func ApplyAzureServerWorkloadIdentity(podMeta *metav1.ObjectMeta, podSpec *corev1.PodSpec, cluster *temporalv1alpha1.TemporalCluster, mainContainerName string) {
	// Set ServiceAccount
	podSpec.ServiceAccountName = AzureServiceAccountName(cluster)

	// Add WI label
	if podMeta.Labels == nil {
		podMeta.Labels = make(map[string]string)
	}
	podMeta.Labels[AzureWILabel] = AzureWILabelValue

	// Add token volume if not present
	volumeExists := false
	for _, vol := range podSpec.Volumes {
		if vol.Name == AzureTokenVolumeName {
			volumeExists = true
			break
		}
	}
	if !volumeExists {
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: AzureTokenVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	// Add volume mount to main container if not present
	for i := range podSpec.Containers {
		if podSpec.Containers[i].Name == mainContainerName {
			mountExists := false
			for _, mount := range podSpec.Containers[i].VolumeMounts {
				if mount.Name == AzureTokenVolumeName {
					mountExists = true
					break
				}
			}
			if !mountExists {
				podSpec.Containers[i].VolumeMounts = append(
					podSpec.Containers[i].VolumeMounts,
					corev1.VolumeMount{
						Name:      AzureTokenVolumeName,
						MountPath: AzureTokenMountPath,
					},
				)
			}
			break
		}
	}

	// Add sidecar if not present
	sidecarExists := false
	for _, container := range podSpec.Containers {
		if container.Name == azureTokenRefresherName {
			sidecarExists = true
			break
		}
	}
	if !sidecarExists {
		podSpec.Containers = append(podSpec.Containers, AzureTokenRefresherSidecar(cluster))
	}
}

// ApplyAzureSchemaWorkloadIdentity applies Azure Workload Identity configuration to a schema Job pod.
// It sets the ServiceAccount, adds the WI label, adds the token volume, mounts the volume on the
// main container, and appends the token init container. This function is idempotent.
func ApplyAzureSchemaWorkloadIdentity(podMeta *metav1.ObjectMeta, podSpec *corev1.PodSpec, cluster *temporalv1alpha1.TemporalCluster, mainContainerName string) {
	// Set ServiceAccount
	podSpec.ServiceAccountName = AzureServiceAccountName(cluster)

	// Add WI label
	if podMeta.Labels == nil {
		podMeta.Labels = make(map[string]string)
	}
	podMeta.Labels[AzureWILabel] = AzureWILabelValue

	// Add token volume if not present
	volumeExists := false
	for _, vol := range podSpec.Volumes {
		if vol.Name == AzureTokenVolumeName {
			volumeExists = true
			break
		}
	}
	if !volumeExists {
		podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
			Name: AzureTokenVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	// Add volume mount to main container if not present
	for i := range podSpec.Containers {
		if podSpec.Containers[i].Name == mainContainerName {
			mountExists := false
			for _, mount := range podSpec.Containers[i].VolumeMounts {
				if mount.Name == AzureTokenVolumeName {
					mountExists = true
					break
				}
			}
			if !mountExists {
				podSpec.Containers[i].VolumeMounts = append(
					podSpec.Containers[i].VolumeMounts,
					corev1.VolumeMount{
						Name:      AzureTokenVolumeName,
						MountPath: AzureTokenMountPath,
					},
				)
			}
			break
		}
	}

	// Add init container if not present
	initExists := false
	for _, container := range podSpec.InitContainers {
		if container.Name == azureTokenInitName {
			initExists = true
			break
		}
	}
	if !initExists {
		podSpec.InitContainers = append(podSpec.InitContainers, AzureTokenInitContainer(cluster))
	}
}
