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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// ClientSecretName returns the Secret name for a client's credentials.
func ClientSecretName(clusterClient *temporalv1alpha1.TemporalClusterClient) string {
	if clusterClient.Spec.SecretName != "" {
		return clusterClient.Spec.SecretName
	}
	return clusterClient.Name
}

// BuildClientCertificate builds a cert-manager Certificate for a
// TemporalClusterClient, signed by the cluster's mTLS issuer. The resulting
// Secret carries tls.crt, tls.key, and ca.crt for an application worker.
func BuildClientCertificate(clusterClient *temporalv1alpha1.TemporalClusterClient, cluster *temporalv1alpha1.TemporalCluster) *certmanagerv1.Certificate {
	return &certmanagerv1.Certificate{
		TypeMeta: metav1.TypeMeta{APIVersion: "cert-manager.io/v1", Kind: "Certificate"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      clusterClient.Name,
			Namespace: clusterClient.Namespace,
			Labels: map[string]string{
				LabelName:      nameValue,
				LabelInstance:  cluster.Name,
				LabelComponent: "client",
				LabelManagedBy: managedByValue,
				LabelCluster:   cluster.Name,
			},
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName: ClientSecretName(clusterClient),
			CommonName: clusterClient.Name,
			IssuerRef:  issuerRef(cluster.Spec.MTLS.IssuerRef),
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageClientAuth,
			},
		},
	}
}
