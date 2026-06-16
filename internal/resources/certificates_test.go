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
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func mtlsCluster() *temporalv1alpha1.TemporalCluster {
	return &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			MTLS: &temporalv1alpha1.MTLSSpec{
				Provider:  "cert-manager",
				IssuerRef: &temporalv1alpha1.IssuerReference{Name: "ca"},
			},
		},
	}
}

func TestBuildInternodeCertificateIncludesServerNameSAN(t *testing.T) {
	cert := BuildInternodeCertificate(mtlsCluster())
	if !slices.Contains(cert.Spec.DNSNames, "test-internode") {
		t.Fatalf("expected DNSNames to contain stable serverName %q, got %v", "test-internode", cert.Spec.DNSNames)
	}
}
