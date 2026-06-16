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

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// Certificate / secret naming.
const (
	// InternodeCertMountPath is where the internode cert is mounted in every pod.
	InternodeCertMountPath = "/etc/temporal/certs/internode"
	// FrontendCertMountPath is where the frontend cert is mounted in frontend pods.
	FrontendCertMountPath = "/etc/temporal/certs/frontend"
)

// InternodeCertName returns the internode Certificate (and its secret) name.
func InternodeCertName(clusterName string) string {
	return clusterName + "-internode"
}

// FrontendCertName returns the frontend server Certificate (and its secret) name.
func FrontendCertName(clusterName string) string {
	return clusterName + "-frontend-tls"
}

// issuerRef converts the CR's IssuerReference into a cert-manager ObjectReference.
func issuerRef(ref *temporalv1alpha1.IssuerReference) cmmeta.ObjectReference {
	kind := ref.Kind
	if kind == "" {
		kind = "Issuer"
	}
	group := ref.Group
	if group == "" {
		group = "cert-manager.io"
	}
	return cmmeta.ObjectReference{Name: ref.Name, Kind: kind, Group: group}
}

// serviceDNSNames returns the in-cluster DNS names for a service's headless and
// (for frontend) client Services.
func serviceDNSNames(cluster *temporalv1alpha1.TemporalCluster, component string) []string {
	ns := cluster.Namespace
	headless := HeadlessServiceName(cluster.Name, component)
	return []string{
		fmt.Sprintf("%s.%s.svc.cluster.local", headless, ns),
		fmt.Sprintf("%s.%s.svc", headless, ns),
		fmt.Sprintf("*.%s.%s.svc.cluster.local", headless, ns),
	}
}

// BuildInternodeCertificate builds the shared internode mTLS Certificate covering
// every service's membership DNS names. It is used by all services for both
// server and client auth within the cluster.
func BuildInternodeCertificate(cluster *temporalv1alpha1.TemporalCluster) *certmanagerv1.Certificate {
	var dnsNames []string
	// Stable serverName used by internode TLS clients; pod IPs are dynamic and
	// not in the cert, so clients verify against this name instead.
	dnsNames = append(dnsNames, InternodeCertName(cluster.Name))
	for _, svc := range EnabledServices(cluster) {
		dnsNames = append(dnsNames, serviceDNSNames(cluster, svc.Name)...)
	}
	return &certmanagerv1.Certificate{
		TypeMeta: metav1.TypeMeta{APIVersion: "cert-manager.io/v1", Kind: "Certificate"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      InternodeCertName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    StandardLabels(cluster, "internode"),
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName:  InternodeCertName(cluster.Name),
			CommonName:  fmt.Sprintf("%s-internode", cluster.Name),
			DNSNames:    dnsNames,
			Duration:    certDuration(cluster),
			RenewBefore: certRenewBefore(cluster),
			IssuerRef:   issuerRef(cluster.Spec.MTLS.IssuerRef),
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageServerAuth,
				certmanagerv1.UsageClientAuth,
			},
		},
	}
}

// BuildFrontendCertificate builds the frontend server Certificate, covering the
// frontend client Service DNS plus any user-supplied DNS names.
func BuildFrontendCertificate(cluster *temporalv1alpha1.TemporalCluster) *certmanagerv1.Certificate {
	ns := cluster.Namespace
	dnsNames := []string{
		fmt.Sprintf("%s.%s.svc.cluster.local", FrontendServiceName(cluster.Name), ns),
		fmt.Sprintf("%s.%s.svc", FrontendServiceName(cluster.Name), ns),
	}
	if cluster.Spec.MTLS.Frontend != nil {
		dnsNames = append(dnsNames, cluster.Spec.MTLS.Frontend.DNSNames...)
	}
	return &certmanagerv1.Certificate{
		TypeMeta: metav1.TypeMeta{APIVersion: "cert-manager.io/v1", Kind: "Certificate"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      FrontendCertName(cluster.Name),
			Namespace: cluster.Namespace,
			Labels:    StandardLabels(cluster, "frontend"),
		},
		Spec: certmanagerv1.CertificateSpec{
			SecretName:  FrontendCertName(cluster.Name),
			CommonName:  fmt.Sprintf("%s-frontend", cluster.Name),
			DNSNames:    dnsNames,
			Duration:    certDuration(cluster),
			RenewBefore: certRenewBefore(cluster),
			IssuerRef:   issuerRef(cluster.Spec.MTLS.IssuerRef),
			Usages: []certmanagerv1.KeyUsage{
				certmanagerv1.UsageServerAuth,
				certmanagerv1.UsageClientAuth,
			},
		},
	}
}

func certDuration(cluster *temporalv1alpha1.TemporalCluster) *metav1.Duration {
	if cluster.Spec.MTLS != nil && cluster.Spec.MTLS.RefreshInterval != nil {
		return cluster.Spec.MTLS.RefreshInterval
	}
	return nil
}

func certRenewBefore(cluster *temporalv1alpha1.TemporalCluster) *metav1.Duration {
	if cluster.Spec.MTLS != nil && cluster.Spec.MTLS.RenewBefore != nil {
		return cluster.Spec.MTLS.RenewBefore
	}
	return nil
}
