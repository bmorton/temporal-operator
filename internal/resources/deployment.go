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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

const (
	configMountPath        = "/etc/temporal/config"
	dynamicConfigMountPath = "/etc/temporal/dynamicconfig"
	metricsPort            = 9090
)

// DeploymentName returns the Deployment name for a service.
func DeploymentName(clusterName, component string) string {
	return clusterName + "-" + component
}

func int32OrDefault(p *int32, def int32) int32 {
	if p == nil {
		return def
	}
	return *p
}

func serverImage(cluster *temporalv1alpha1.TemporalCluster, version string) string {
	if cluster.Spec.Image != "" {
		return cluster.Spec.Image
	}
	if version == "" {
		version = cluster.Spec.Version
	}
	return temporal.ServerImage(version)
}

func grpcProbe(port int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			GRPC: &corev1.GRPCAction{Port: port},
		},
	}
}

func defaultTopologySpread(cluster *temporalv1alpha1.TemporalCluster, component string) []corev1.TopologySpreadConstraint {
	return []corev1.TopologySpreadConstraint{
		{
			MaxSkew:           1,
			TopologyKey:       "topology.kubernetes.io/zone",
			WhenUnsatisfiable: corev1.ScheduleAnyway,
			LabelSelector:     &metav1.LabelSelector{MatchLabels: SelectorLabels(cluster, component)},
		},
	}
}

func containerPorts(svc ServiceInfo) []corev1.ContainerPort {
	ports := []corev1.ContainerPort{
		{Name: "grpc", ContainerPort: svc.Ports.GRPCPort},
		{Name: "membership", ContainerPort: svc.Ports.MembershipPort},
		{Name: "metrics", ContainerPort: metricsPort},
	}
	if svc.HasHTTP && svc.Ports.HTTPPort != 0 {
		ports = append(ports, corev1.ContainerPort{Name: "http", ContainerPort: svc.Ports.HTTPPort})
	}
	return ports
}

// MTLSMounts describes the cert secrets to mount when mTLS is enabled.
type MTLSMounts struct {
	Enabled         bool
	InternodeSecret string
	FrontendSecret  string
	// CertHash, when set, is stamped on the pod template to trigger a rolling
	// restart on certificate rotation.
	CertHash string
}

// BuildDeployment builds the Deployment for a single Temporal service. The
// version overrides the server image tag (used for per-service rollout during
// upgrades); when empty the cluster's spec version is used.
func BuildDeployment(cluster *temporalv1alpha1.TemporalCluster, svc ServiceInfo, configHash, version string, mtls *MTLSMounts) *appsv1.Deployment {
	replicas := int32(1)
	var resources corev1.ResourceRequirements
	var nodeSelector map[string]string
	var tolerations []corev1.Toleration
	var affinity *corev1.Affinity
	topologySpread := defaultTopologySpread(cluster, svc.Name)

	if svc.Spec != nil {
		replicas = int32OrDefault(svc.Spec.Replicas, 1)
		resources = svc.Spec.Resources
		nodeSelector = svc.Spec.NodeSelector
		tolerations = svc.Spec.Tolerations
		affinity = svc.Spec.Affinity
		if len(svc.Spec.TopologySpreadConstraints) > 0 {
			topologySpread = svc.Spec.TopologySpreadConstraints
		}
	}

	podLabels := StandardLabels(cluster, svc.Name)
	if version != "" {
		podLabels[LabelVersion] = version
	}
	startup := grpcProbe(svc.Ports.GRPCPort)
	startup.FailureThreshold = 30
	startup.PeriodSeconds = 5

	readiness := grpcProbe(svc.Ports.GRPCPort)
	readiness.TimeoutSeconds = 3

	container := corev1.Container{
		Name:  "temporal",
		Image: serverImage(cluster, version),
		Command: []string{
			"temporal-server",
			"--root", "/etc/temporal",
			"--config", "config",
			"--env", "config",
			"start",
			"--service", svc.Name,
		},
		Ports:          containerPorts(svc),
		Resources:      resources,
		LivenessProbe:  grpcProbe(svc.Ports.GRPCPort),
		ReadinessProbe: readiness,
		StartupProbe:   startup,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "config", MountPath: configMountPath, ReadOnly: true},
			{Name: "dynamicconfig", MountPath: dynamicConfigMountPath, ReadOnly: true},
		},
	}

	volumes := []corev1.Volume{
		{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: ConfigSecretName(cluster.Name)},
			},
		},
		{
			Name: "dynamicconfig",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: DynamicConfigMapName(cluster.Name)},
				},
			},
		},
	}

	podAnnotations := map[string]string{ConfigHashAnnotation: configHash}

	if mtls != nil && mtls.Enabled {
		if mtls.CertHash != "" {
			podAnnotations[CertHashAnnotation] = mtls.CertHash
		}
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name: "internode-certs", MountPath: InternodeCertMountPath, ReadOnly: true,
		})
		volumes = append(volumes, corev1.Volume{
			Name: "internode-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: mtls.InternodeSecret},
			},
		})
		if svc.Name == ServiceFrontend || svc.Name == ServiceInternalFrontend {
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
				Name: "frontend-certs", MountPath: FrontendCertMountPath, ReadOnly: true,
			})
			volumes = append(volumes, corev1.Volume{
				Name: "frontend-certs",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: mtls.FrontendSecret},
				},
			})
		}
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeploymentName(cluster.Name, svc.Name),
			Namespace: cluster.Namespace,
			Labels:    podLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: SelectorLabels(cluster, svc.Name)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets:          cluster.Spec.ImagePullSecrets,
					NodeSelector:              nodeSelector,
					Tolerations:               tolerations,
					Affinity:                  affinity,
					TopologySpreadConstraints: topologySpread,
					Containers:                []corev1.Container{container},
					Volumes:                   volumes,
				},
			},
		},
	}
}

// intstrFromInt is a small helper for service target ports.
func intstrFromInt(i int32) intstr.IntOrString {
	return intstr.FromInt32(i)
}
