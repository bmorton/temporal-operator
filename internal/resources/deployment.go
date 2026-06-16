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
	"encoding/json"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/strategicpatch"

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

// tcpProbe checks that a service's gRPC port accepts TCP connections.
func tcpProbe(port int32) *corev1.Probe {
	return &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			TCPSocket: &corev1.TCPSocketAction{Port: intstrFromInt(port)},
		},
	}
}

// serviceProbe returns the health probe for a request-serving Temporal service.
// With mTLS enabled the gRPC port enforces client auth, which Kubernetes' native
// gRPC prober cannot satisfy (it dials without a client certificate, so the
// mutual-TLS handshake never completes and the probe times out). In that case
// we fall back to a TCP probe, which succeeds once the listener is up.
//
// A TCP probe is a weaker signal than the gRPC health check (it only confirms
// the port accepts connections). This is documented as a known limitation; see
// docs/content/operations/_index.md ("mTLS health probes"). A future change may
// adopt a grpc-health-probe exec probe or a service mesh instead.
func serviceProbe(port int32, mtlsEnabled bool) *corev1.Probe {
	if mtlsEnabled {
		return tcpProbe(port)
	}
	return grpcProbe(port)
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
	// The config Secret is mounted read-only. Temporal's config loader does
	// not perform env-var substitution for broadcastAddress in 1.31+, so we
	// use a shell wrapper: copy config to a writable emptyDir, substitute
	// ${POD_IP} with the real pod IP, then exec the server binary.
	container := corev1.Container{
		Name:    "temporal",
		Image:   serverImage(cluster, version),
		Command: []string{"/bin/sh", "-c"},
		Args: []string{
			`cp -r /etc/temporal/config /tmp/temporal/temporal-config && ` +
				`sed -i "s/\${POD_IP}/$POD_IP/g" /tmp/temporal/temporal-config/*.yaml && ` +
				`exec temporal-server ` +
				`--root /tmp/temporal --config temporal-config --env config ` +
				`start --service ` + svc.Name,
		},
		Env: []corev1.EnvVar{
			{
				Name: "POD_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
				},
			},
		},
		Ports:     containerPorts(svc),
		Resources: resources,
		VolumeMounts: []corev1.VolumeMount{
			{Name: "config", MountPath: configMountPath, ReadOnly: true},
			{Name: "dynamicconfig", MountPath: dynamicConfigMountPath, ReadOnly: true},
			{Name: "processed-config", MountPath: "/tmp/temporal"},
		},
	}

	// The worker service does not expose a client-facing gRPC server, so it
	// does not answer gRPC health checks (this is unreliable before Temporal
	// 1.31 and the upstream Temporal Helm chart omits worker probes entirely).
	// Probing it would fail its startup probe forever and the cluster would
	// never report Ready. Only the request-serving services get probes.
	if svc.Name != ServiceWorker {
		mtlsEnabled := mtls != nil && mtls.Enabled

		startup := serviceProbe(svc.Ports.GRPCPort, mtlsEnabled)
		startup.FailureThreshold = 30
		startup.PeriodSeconds = 5

		readiness := serviceProbe(svc.Ports.GRPCPort, mtlsEnabled)
		readiness.TimeoutSeconds = 3

		container.LivenessProbe = serviceProbe(svc.Ports.GRPCPort, mtlsEnabled)
		container.ReadinessProbe = readiness
		container.StartupProbe = startup
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
		{
			Name:         "processed-config",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
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

// applyPodTemplate layers a PodTemplateOverride onto a generated pod template.
// Labels and annotations are merged (override wins), and the override's partial
// PodSpec is strategic-merge patched onto the generated PodSpec (containers and
// volumes merge by name). Selector labels are re-asserted afterward so an
// override can never drop a label the Deployment selector depends on. A nil
// override is a no-op.
func applyPodTemplate(tmpl corev1.PodTemplateSpec, override *temporalv1alpha1.PodTemplateOverride, selectorLabels map[string]string) (corev1.PodTemplateSpec, error) {
	if override == nil {
		return tmpl, nil
	}

	labels := map[string]string{}
	for k, v := range tmpl.Labels {
		labels[k] = v
	}
	for k, v := range override.Labels {
		labels[k] = v
	}

	annotations := map[string]string{}
	for k, v := range tmpl.Annotations {
		annotations[k] = v
	}
	for k, v := range override.Annotations {
		annotations[k] = v
	}

	if override.Spec != nil && len(override.Spec.Raw) > 0 {
		original, err := json.Marshal(tmpl.Spec)
		if err != nil {
			return tmpl, fmt.Errorf("marshaling generated pod spec: %w", err)
		}
		patched, err := strategicpatch.StrategicMergePatch(original, override.Spec.Raw, corev1.PodSpec{})
		if err != nil {
			return tmpl, fmt.Errorf("applying podTemplate spec patch: %w", err)
		}
		var merged corev1.PodSpec
		if err := json.Unmarshal(patched, &merged); err != nil {
			return tmpl, fmt.Errorf("unmarshaling patched pod spec: %w", err)
		}
		tmpl.Spec = merged
	}

	// Re-assert selector labels last so an override cannot drop one.
	for k, v := range selectorLabels {
		labels[k] = v
	}

	tmpl.Labels = labels
	tmpl.Annotations = annotations
	return tmpl, nil
}

// intstrFromInt is a small helper for service target ports.
func intstrFromInt(i int32) intstr.IntOrString {
	return intstr.FromInt32(i)
}
