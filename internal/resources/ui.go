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
	"strings"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

const (
	uiComponent     = "ui"
	uiPort          = 8080
	uiClientCertDir = "/etc/temporal/certs/ui-client"
)

// UIName returns the name of the UI Deployment/Service.
func UIName(clusterName string) string {
	return clusterName + "-ui"
}

// UIClientCertName returns the UI client Certificate/secret name.
func UIClientCertName(clusterName string) string {
	return clusterName + "-ui-client"
}

func uiImage(cluster *temporalv1alpha1.TemporalCluster) string {
	version := cluster.Spec.UI.Version
	if version == "" {
		version = temporal.DefaultUIVersion(cluster.Spec.Version)
	}
	return "temporalio/ui:" + version
}

func uiEnv(cluster *temporalv1alpha1.TemporalCluster) []corev1.EnvVar {
	frontend := fmt.Sprintf("%s.%s.svc:%d", FrontendServiceName(cluster.Name), cluster.Namespace,
		temporal.DefaultServicePorts()["frontend"].GRPCPort)
	env := []corev1.EnvVar{
		{Name: "TEMPORAL_ADDRESS", Value: frontend},
		{Name: "TEMPORAL_UI_PORT", Value: fmt.Sprintf("%d", uiPort)},
	}
	if mtlsEnabledSpec(cluster) {
		env = append(env,
			corev1.EnvVar{Name: "TEMPORAL_TLS_CA", Value: uiClientCertDir + "/ca.crt"},
			corev1.EnvVar{Name: "TEMPORAL_TLS_CERT", Value: uiClientCertDir + "/tls.crt"},
			corev1.EnvVar{Name: "TEMPORAL_TLS_KEY", Value: uiClientCertDir + "/tls.key"},
			corev1.EnvVar{Name: "TEMPORAL_TLS_ENABLE_HOST_VERIFICATION", Value: "true"},
			corev1.EnvVar{Name: "TEMPORAL_TLS_SERVER_NAME", Value: fmt.Sprintf("%s.%s.svc", FrontendServiceName(cluster.Name), cluster.Namespace)},
		)
	}
	if cluster.Spec.UI != nil && cluster.Spec.UI.CodecServer != nil {
		env = append(env, corev1.EnvVar{Name: "TEMPORAL_CODEC_ENDPOINT", Value: cluster.Spec.UI.CodecServer.Endpoint})
	}
	if cluster.Spec.UI != nil && cluster.Spec.UI.Auth != nil && cluster.Spec.UI.Auth.Enabled {
		a := cluster.Spec.UI.Auth
		providerURL := a.ProviderURL
		if a.Entra != nil {
			providerURL = fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", a.Entra.TenantID)
		}
		scopes := a.Scopes
		if len(scopes) == 0 {
			scopes = []string{"openid", "profile", "email"}
		}
		env = append(env,
			corev1.EnvVar{Name: "TEMPORAL_AUTH_ENABLED", Value: "true"},
			corev1.EnvVar{Name: "TEMPORAL_AUTH_TYPE", Value: "oidc"},
			corev1.EnvVar{Name: "TEMPORAL_AUTH_PROVIDER_URL", Value: providerURL},
			corev1.EnvVar{Name: "TEMPORAL_AUTH_CLIENT_ID", Value: a.ClientID},
			corev1.EnvVar{Name: "TEMPORAL_AUTH_SCOPES", Value: strings.Join(scopes, ",")},
			corev1.EnvVar{Name: "TEMPORAL_AUTH_CALLBACK_URL", Value: a.CallbackURL},
		)
		if a.ClientSecretRef != nil {
			key := a.ClientSecretRef.Key
			if key == "" {
				key = "password"
			}
			env = append(env, corev1.EnvVar{
				Name: "TEMPORAL_AUTH_CLIENT_SECRET",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: a.ClientSecretRef.Name},
						Key:                  key,
					},
				},
			})
		}
		if a.ExtraEnv != nil && len(a.ExtraEnv.Raw) > 0 {
			extra := map[string]string{}
			if err := yaml.Unmarshal(a.ExtraEnv.Raw, &extra); err == nil {
				for k, v := range extra {
					env = append(env, corev1.EnvVar{Name: k, Value: v})
				}
			}
		}
	}
	return env
}

func mtlsEnabledSpec(cluster *temporalv1alpha1.TemporalCluster) bool {
	return cluster.Spec.MTLS != nil && cluster.Spec.MTLS.Provider == "cert-manager"
}

// BuildUIClientCertificate builds the client Certificate the UI uses to connect
// to the frontend when mTLS is enabled.
func BuildUIClientCertificate(cluster *temporalv1alpha1.TemporalCluster) *certmanagerv1.Certificate {
	return &certmanagerv1.Certificate{
		TypeMeta: metav1.TypeMeta{APIVersion: "cert-manager.io/v1", Kind: "Certificate"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      UIClientCertName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    StandardLabels(cluster, uiComponent),
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: UIClientCertName(cluster.Name),
			CommonName: UIName(cluster.Name),
			IssuerRef:  issuerRef(cluster.Spec.MTLS.IssuerRef),
			Usages:     []certmanagerv1.KeyUsage{certmanagerv1.UsageClientAuth},
		},
	}
}

// BuildUIDeployment builds the temporal-ui Deployment.
func BuildUIDeployment(cluster *temporalv1alpha1.TemporalCluster) *appsv1.Deployment {
	labels := StandardLabels(cluster, uiComponent)
	replicas := int32(1)
	if cluster.Spec.UI != nil && cluster.Spec.UI.Replicas != nil {
		replicas = *cluster.Spec.UI.Replicas
	}

	httpProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{Path: "/", Port: intstrFromInt(uiPort)},
		},
	}

	container := corev1.Container{
		Name:           "ui",
		Image:          uiImage(cluster),
		Env:            uiEnv(cluster),
		Ports:          []corev1.ContainerPort{{Name: "http", ContainerPort: uiPort}},
		LivenessProbe:  httpProbe,
		ReadinessProbe: httpProbe,
	}

	var volumes []corev1.Volume
	if mtlsEnabledSpec(cluster) {
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
			Name: "ui-client-certs", MountPath: uiClientCertDir, ReadOnly: true,
		})
		volumes = append(volumes, corev1.Volume{
			Name: "ui-client-certs",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{SecretName: UIClientCertName(cluster.Name)},
			},
		})
	}

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      UIName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: SelectorLabels(cluster, uiComponent)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					ImagePullSecrets: cluster.Spec.ImagePullSecrets,
					Containers:       []corev1.Container{container},
					Volumes:          volumes,
				},
			},
		},
	}
}

// BuildUIService builds the ClusterIP Service for the UI.
func BuildUIService(cluster *temporalv1alpha1.TemporalCluster) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Service"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      UIName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    StandardLabels(cluster, uiComponent),
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: SelectorLabels(cluster, uiComponent),
			Ports:    []corev1.ServicePort{{Name: "http", Port: uiPort, TargetPort: intstrFromInt(uiPort)}},
		},
	}
}

// BuildUIIngress builds an Ingress for the UI when configured. It returns nil
// when ingress is not enabled.
func BuildUIIngress(cluster *temporalv1alpha1.TemporalCluster) *networkingv1.Ingress {
	if cluster.Spec.UI == nil || cluster.Spec.UI.Ingress == nil || !cluster.Spec.UI.Ingress.Enabled {
		return nil
	}
	ing := cluster.Spec.UI.Ingress
	pathType := networkingv1.PathTypePrefix
	rule := networkingv1.IngressRule{
		Host: ing.Host,
		IngressRuleValue: networkingv1.IngressRuleValue{
			HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: []networkingv1.HTTPIngressPath{
					{
						Path:     "/",
						PathType: &pathType,
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: UIName(cluster.Name),
								Port: networkingv1.ServiceBackendPort{Number: uiPort},
							},
						},
					},
				},
			},
		},
	}

	ingress := &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "Ingress"},
		ObjectMeta: metav1.ObjectMeta{
			Name:        UIName(cluster.Name),
			Namespace:   cluster.Namespace,
			Labels:      StandardLabels(cluster, uiComponent),
			Annotations: ing.Annotations,
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{rule},
		},
	}
	if ing.IngressClassName != "" {
		ingress.Spec.IngressClassName = &ing.IngressClassName
	}
	if ing.TLSSecretName != "" && ing.Host != "" {
		ingress.Spec.TLS = []networkingv1.IngressTLS{
			{Hosts: []string{ing.Host}, SecretName: ing.TLSSecretName},
		}
	}
	return ingress
}
