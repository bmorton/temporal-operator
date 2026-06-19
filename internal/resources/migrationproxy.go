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
)

// MigrationProxyComponent is the component label value for proxy resources.
const MigrationProxyComponent = "migration-proxy"

// proxyFrontendPort is the gRPC port the proxy listens on (Temporal default).
const proxyFrontendPort int32 = 7233

// MigrationProxyName returns the proxy resource name for a migration.
func MigrationProxyName(m *temporalv1alpha1.TemporalMigration) string {
	return m.Name + "-proxy"
}

func migrationProxyLabels(m *temporalv1alpha1.TemporalMigration) map[string]string {
	return map[string]string{
		LabelName:      nameValue,
		LabelInstance:  m.Name,
		LabelComponent: MigrationProxyComponent,
		LabelManagedBy: managedByValue,
	}
}

// BuildMigrationProxyConfigMap renders the proxy config file into a ConfigMap.
func BuildMigrationProxyConfigMap(m *temporalv1alpha1.TemporalMigration, renderedConfig string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MigrationProxyName(m),
			Namespace: m.Namespace,
			Labels:    migrationProxyLabels(m),
		},
		Data: map[string]string{"config.yaml": renderedConfig},
	}
}

// BuildMigrationProxyService exposes the proxy frontend port.
func BuildMigrationProxyService(m *temporalv1alpha1.TemporalMigration) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MigrationProxyName(m),
			Namespace: m.Namespace,
			Labels:    migrationProxyLabels(m),
		},
		Spec: corev1.ServiceSpec{
			Selector: migrationProxyLabels(m),
			Ports: []corev1.ServicePort{{
				Name:       "grpc",
				Port:       proxyFrontendPort,
				TargetPort: intstr.FromInt32(proxyFrontendPort),
				Protocol:   corev1.ProtocolTCP,
			}},
		},
	}
}

// BuildMigrationProxyDeployment builds the proxy Deployment. configHash is
// stamped on the pod template so config changes (e.g. cutover) trigger a rollout.
func BuildMigrationProxyDeployment(m *temporalv1alpha1.TemporalMigration, image, configHash string) *appsv1.Deployment {
	replicas := int32(1)
	if m.Spec.Proxy != nil && m.Spec.Proxy.Replicas != nil {
		replicas = *m.Spec.Proxy.Replicas
	}
	if m.Spec.Proxy != nil && m.Spec.Proxy.Image != "" {
		image = m.Spec.Proxy.Image
	}
	var resources corev1.ResourceRequirements
	if m.Spec.Proxy != nil {
		resources = m.Spec.Proxy.Resources
	}
	labels := migrationProxyLabels(m)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MigrationProxyName(m),
			Namespace: m.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: map[string]string{ConfigHashAnnotation: configHash},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "migration-proxy",
						Image:   image,
						Command: []string{"/migration-proxy"},
						Args:    []string{"--config=/etc/migration-proxy/config.yaml"},
						Ports: []corev1.ContainerPort{{
							Name:          "grpc",
							ContainerPort: proxyFrontendPort,
							Protocol:      corev1.ProtocolTCP,
						}},
						Resources: resources,
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "config",
							MountPath: "/etc/migration-proxy",
							ReadOnly:  true,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "config",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: MigrationProxyName(m)},
							},
						},
					}},
				},
			},
		},
	}
}
