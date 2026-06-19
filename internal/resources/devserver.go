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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// Dev server ports (Temporal CLI start-dev defaults).
const (
	DevServerFrontendPort int32 = 7233
	DevServerUIPort       int32 = 8233
)

const (
	devServerComponent  = "devserver"
	devServerDataPath   = "/data"
	devServerVolumeName = "data"
)

// DevServerFrontendServiceName returns the Service name for a dev server.
func DevServerFrontendServiceName(name string) string {
	return name + "-devserver"
}

// DevServerPVCName returns the PVC name for a persistent dev server.
func DevServerPVCName(name string) string {
	return name + "-devserver-data"
}

// devServerSelectorLabels returns the stable selector labels for a dev server.
func devServerSelectorLabels(name string) map[string]string {
	return map[string]string{
		LabelName:      nameValue,
		LabelInstance:  name,
		LabelComponent: devServerComponent,
	}
}

// devServerLabels returns the full label set for a dev server's resources.
func devServerLabels(name string) map[string]string {
	labels := devServerSelectorLabels(name)
	labels[LabelManagedBy] = managedByValue
	return labels
}

// DevServerImage returns the container image for a dev server. When Image is set
// it is used verbatim; otherwise the Temporal server Version (or the latest
// supported version when empty) is mapped to the matching temporalio/temporal
// CLI image. It returns an error when the server version is unsupported.
func DevServerImage(dev *temporalv1alpha1.TemporalDevServer) (string, error) {
	if dev.Spec.Image != "" {
		return dev.Spec.Image, nil
	}
	version := dev.Spec.Version
	if version == "" {
		version = temporal.LatestSupportedVersion()
	}
	cli := temporal.DevServerCLIVersion(version)
	if cli == "" {
		return "", fmt.Errorf("unsupported dev server version %q: not in the supported matrix %v",
			version, temporal.SupportedVersions())
	}
	return "temporalio/temporal:" + cli, nil
}

// DevServerServerVersion returns the Temporal server version reported in status:
// the explicit Version, the latest supported version when Version is empty, or
// an empty string when a raw Image override is used.
func DevServerServerVersion(dev *temporalv1alpha1.TemporalDevServer) string {
	if dev.Spec.Image != "" {
		return ""
	}
	if dev.Spec.Version != "" {
		return dev.Spec.Version
	}
	return temporal.LatestSupportedVersion()
}

func devServerArgs(dev *temporalv1alpha1.TemporalDevServer) []string {
	parts := []string{
		"server", "start-dev",
		"--ip", "0.0.0.0",
		"--port", fmt.Sprintf("%d", DevServerFrontendPort),
		"--db-filename", devServerDataPath + "/temporal.db",
	}
	uiEnabled := dev.Spec.UI == nil || dev.Spec.UI.Enabled
	if uiEnabled {
		parts = append(parts, "--ui-ip", "0.0.0.0", "--ui-port", fmt.Sprintf("%d", DevServerUIPort))
	} else {
		parts = append(parts, "--headless")
	}
	for _, ns := range dev.Spec.Namespaces {
		parts = append(parts, "--namespace", ns)
	}
	return parts
}

func devServerVolume(dev *temporalv1alpha1.TemporalDevServer) corev1.Volume {
	if dev.Spec.Storage != nil && dev.Spec.Storage.Type == "Persistent" {
		return corev1.Volume{
			Name: devServerVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: DevServerPVCName(dev.Name),
				},
			},
		}
	}
	return corev1.Volume{
		Name:         devServerVolumeName,
		VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
	}
}

func devServerFSGroup() *int64 { v := int64(1000); return &v }

// BuildDevServerDeployment builds the single-replica Deployment that runs
// `temporal server start-dev`.
func BuildDevServerDeployment(dev *temporalv1alpha1.TemporalDevServer, image string) *appsv1.Deployment {
	replicas := int32(1)
	labels := devServerLabels(dev.Name)

	ports := []corev1.ContainerPort{{Name: "grpc", ContainerPort: DevServerFrontendPort}}
	if dev.Spec.UI == nil || dev.Spec.UI.Enabled {
		ports = append(ports, corev1.ContainerPort{Name: "ui", ContainerPort: DevServerUIPort})
	}

	container := corev1.Container{
		Name:      "temporal",
		Image:     image,
		Command:   []string{"temporal"},
		Args:      devServerArgs(dev),
		Ports:     ports,
		Resources: dev.Spec.Resources,
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{Port: intstrFromInt(DevServerFrontendPort)},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: devServerVolumeName, MountPath: devServerDataPath},
		},
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      dev.Name,
			Namespace: dev.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: devServerSelectorLabels(dev.Name)},
			// SQLite cannot be shared across pods; never roll two pods at once.
			Strategy: appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					SecurityContext:  &corev1.PodSecurityContext{FSGroup: devServerFSGroup()},
					ImagePullSecrets: dev.Spec.ImagePullSecrets,
					NodeSelector:     dev.Spec.NodeSelector,
					Tolerations:      dev.Spec.Tolerations,
					Affinity:         dev.Spec.Affinity,
					Containers:       []corev1.Container{container},
					Volumes:          []corev1.Volume{devServerVolume(dev)},
				},
			},
		},
	}
}

// BuildDevServerService builds the Service exposing the frontend and UI ports.
func BuildDevServerService(dev *temporalv1alpha1.TemporalDevServer) *corev1.Service {
	svcType := corev1.ServiceTypeClusterIP
	var annotations map[string]string
	if dev.Spec.Service != nil {
		if dev.Spec.Service.Type != "" {
			svcType = dev.Spec.Service.Type
		}
		annotations = dev.Spec.Service.Annotations
	}

	ports := []corev1.ServicePort{
		{Name: "grpc", Port: DevServerFrontendPort, TargetPort: intstrFromInt(DevServerFrontendPort)},
	}
	if dev.Spec.UI == nil || dev.Spec.UI.Enabled {
		ports = append(ports, corev1.ServicePort{
			Name: "ui", Port: DevServerUIPort, TargetPort: intstrFromInt(DevServerUIPort),
		})
	}

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        DevServerFrontendServiceName(dev.Name),
			Namespace:   dev.Namespace,
			Labels:      devServerLabels(dev.Name),
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     svcType,
			Selector: devServerSelectorLabels(dev.Name),
			Ports:    ports,
		},
	}
}

// BuildDevServerPVC builds the PVC for a persistent dev server. Returns nil when
// the dev server uses ephemeral storage.
func BuildDevServerPVC(dev *temporalv1alpha1.TemporalDevServer) *corev1.PersistentVolumeClaim {
	if dev.Spec.Storage == nil || dev.Spec.Storage.Type != "Persistent" {
		return nil
	}
	size := resource.MustParse("1Gi")
	if dev.Spec.Storage.Size != nil {
		size = *dev.Spec.Storage.Size
	}
	return &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DevServerPVCName(dev.Name),
			Namespace: dev.Namespace,
			Labels:    devServerLabels(dev.Name),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			StorageClassName: dev.Spec.Storage.StorageClassName,
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: size},
			},
		},
	}
}

// devServerEndpoint formats an in-cluster endpoint host:port for a dev server.
func devServerEndpoint(dev *temporalv1alpha1.TemporalDevServer, port int32) string {
	return fmt.Sprintf("%s.%s.svc:%d", DevServerFrontendServiceName(dev.Name), dev.Namespace, port)
}

// DevServerFrontendEndpoint returns the in-cluster gRPC frontend endpoint.
func DevServerFrontendEndpoint(dev *temporalv1alpha1.TemporalDevServer) string {
	return devServerEndpoint(dev, DevServerFrontendPort)
}

// DevServerUIEndpoint returns the in-cluster Web UI endpoint.
func DevServerUIEndpoint(dev *temporalv1alpha1.TemporalDevServer) string {
	return devServerEndpoint(dev, DevServerUIPort)
}
