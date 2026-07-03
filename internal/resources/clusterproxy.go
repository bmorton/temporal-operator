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
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// DefaultClusterProxyImage is the pinned s2s-proxy image.
const DefaultClusterProxyImage = "temporalio/s2s-proxy:v0.2.1"

// ClusterProxyName returns the proxy Deployment (and base) name.
func ClusterProxyName(cr *temporalv1alpha1.TemporalClusterProxy) string {
	return cr.Name + "-s2s-proxy"
}

// ClusterProxyConfigMapName returns the rendered-config ConfigMap name.
func ClusterProxyConfigMapName(cr *temporalv1alpha1.TemporalClusterProxy) string {
	return ClusterProxyName(cr) + "-config"
}

// ClusterProxyServiceName returns the proxy Service name.
func ClusterProxyServiceName(cr *temporalv1alpha1.TemporalClusterProxy) string {
	return ClusterProxyName(cr)
}

// ClusterProxyCertName returns the cert-manager Certificate name.
func ClusterProxyCertName(cr *temporalv1alpha1.TemporalClusterProxy) string {
	return ClusterProxyName(cr) + "-tls"
}

// ClusterProxyTLSSecretName returns the mux TLS Secret name (own material).
func ClusterProxyTLSSecretName(cr *temporalv1alpha1.TemporalClusterProxy) string {
	if cr.Spec.Mux.TLS.Provider == "secret" && cr.Spec.Mux.TLS.SecretRef != nil {
		return cr.Spec.Mux.TLS.SecretRef.Name
	}
	return ClusterProxyCertName(cr)
}

// ClusterProxyLabels returns the standard label set for proxy resources.
func ClusterProxyLabels(cr *temporalv1alpha1.TemporalClusterProxy) map[string]string {
	return map[string]string{
		LabelName:      "s2s-proxy",
		LabelInstance:  cr.Name,
		LabelComponent: "s2s-proxy",
		LabelManagedBy: managedByValue,
	}
}

func clusterProxyImage(cr *temporalv1alpha1.TemporalClusterProxy) string {
	if cr.Spec.Image != "" {
		return cr.Spec.Image
	}
	return DefaultClusterProxyImage
}

// BuildClusterProxyConfigMap wraps the rendered config YAML in a ConfigMap.
func BuildClusterProxyConfigMap(cr *temporalv1alpha1.TemporalClusterProxy, configYAML string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{Name: ClusterProxyConfigMapName(cr), Namespace: cr.Namespace, Labels: ClusterProxyLabels(cr)},
		Data:       map[string]string{ProxyConfigFileName: configYAML},
	}
}

// BuildClusterProxyCertificate builds the mux mTLS Certificate. Only call when
// mux.tls.provider is cert-manager (IssuerRef set).
func BuildClusterProxyCertificate(cr *temporalv1alpha1.TemporalClusterProxy) *certmanagerv1.Certificate {
	var dnsNames []string
	svc := ClusterProxyServiceName(cr)
	dnsNames = append(dnsNames,
		svc,
		svc+"."+cr.Namespace+".svc",
		svc+"."+cr.Namespace+".svc.cluster.local",
	)
	return &certmanagerv1.Certificate{
		TypeMeta:   metav1.TypeMeta{APIVersion: "cert-manager.io/v1", Kind: "Certificate"},
		ObjectMeta: metav1.ObjectMeta{Name: ClusterProxyCertName(cr), Namespace: cr.Namespace, Labels: ClusterProxyLabels(cr)},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: ClusterProxyTLSSecretName(cr),
			CommonName: ClusterProxyName(cr),
			DNSNames:   dnsNames,
			IssuerRef:  issuerRef(cr.Spec.Mux.TLS.IssuerRef),
			Usages:     []certmanagerv1.KeyUsage{certmanagerv1.UsageServerAuth, certmanagerv1.UsageClientAuth},
		},
	}
}

// BuildClusterProxyService builds the proxy Service. It always exposes the
// tcpServer port (ClusterIP, for the local Temporal) and, for the server role,
// the mux listen port using the configured exposure.
func BuildClusterProxyService(cr *temporalv1alpha1.TemporalClusterProxy) *corev1.Service {
	ports := []corev1.ServicePort{
		{Name: "tcp-server", Port: ProxyTCPServerPort, TargetPort: intstrFromInt(ProxyTCPServerPort)},
	}
	svcType := corev1.ServiceTypeClusterIP
	var annotations map[string]string
	if cr.Spec.Mux.Role == temporalv1alpha1.ProxyRoleServer && cr.Spec.Mux.Server != nil {
		ports = append(ports, corev1.ServicePort{
			Name: "mux", Port: cr.Spec.Mux.Server.ListenPort, TargetPort: intstrFromInt(cr.Spec.Mux.Server.ListenPort),
		})
		if ex := cr.Spec.Mux.Server.Exposure; ex != nil {
			if ex.Type != "" {
				svcType = ex.Type
			}
			annotations = ex.Annotations
		}
	}
	return &corev1.Service{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{Name: ClusterProxyServiceName(cr), Namespace: cr.Namespace, Labels: ClusterProxyLabels(cr), Annotations: annotations},
		Spec: corev1.ServiceSpec{
			Type:     svcType,
			Selector: ClusterProxyLabels(cr),
			Ports:    ports,
		},
	}
}

// BuildClusterProxyDeployment builds the s2s-proxy Deployment. configHash stamps
// the pod template so config/cert changes trigger a rollout.
func BuildClusterProxyDeployment(cr *temporalv1alpha1.TemporalClusterProxy, configHash string) *appsv1.Deployment {
	volumes := []corev1.Volume{
		{Name: "config", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: ClusterProxyConfigMapName(cr)},
		}}},
		{Name: "tls", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{
			SecretName: ClusterProxyTLSSecretName(cr),
		}}},
	}
	mounts := []corev1.VolumeMount{
		{Name: "config", MountPath: ProxyConfigMountPath, ReadOnly: true},
		{Name: "tls", MountPath: ProxyTLSMountPath, ReadOnly: true},
	}
	if ref := cr.Spec.Mux.TLS.PeerCARef; ref != nil {
		volumes = append(volumes, corev1.Volume{Name: "peer-ca", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: ref.Name}}})
		mounts = append(mounts, corev1.VolumeMount{Name: "peer-ca", MountPath: ProxyPeerCAMountPath, ReadOnly: true})
	}

	replicas := int32(1)
	labels := ClusterProxyLabels(cr)
	return &appsv1.Deployment{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{Name: ClusterProxyName(cr), Namespace: cr.Namespace, Labels: labels},
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
						Name:         "s2s-proxy",
						Image:        clusterProxyImage(cr),
						Env:          []corev1.EnvVar{{Name: "CONFIG_YML", Value: ProxyConfigMountPath + "/" + ProxyConfigFileName}},
						VolumeMounts: mounts,
					}},
					Volumes: volumes,
				},
			},
		},
	}
}
