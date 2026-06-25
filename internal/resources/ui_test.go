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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func uiAuthCluster() *temporalv1alpha1.TemporalCluster {
	return &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "entra", Namespace: "default"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Version: "1.31.1",
			UI: &temporalv1alpha1.UISpec{
				Enabled: true,
				Auth: &temporalv1alpha1.UIAuthSpec{
					Enabled:         true,
					Entra:           &temporalv1alpha1.EntraUIAuthSpec{TenantID: "tenant-abc"},
					ClientID:        "client-123",
					ClientSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "ui-oidc", Key: "client-secret"},
					CallbackURL:     "https://temporal.example.test/auth/sso/callback",
				},
			},
		},
	}
}

func envByName(env []corev1.EnvVar, name string) (corev1.EnvVar, bool) {
	for _, e := range env {
		if e.Name == name {
			return e, true
		}
	}
	return corev1.EnvVar{}, false
}

func TestBuildUIDeployment_EntraAuthEnv(t *testing.T) {
	dep := BuildUIDeployment(uiAuthCluster())
	env := dep.Spec.Template.Spec.Containers[0].Env

	cases := map[string]string{
		"TEMPORAL_AUTH_ENABLED":      "true",
		"TEMPORAL_AUTH_TYPE":         "oidc",
		"TEMPORAL_AUTH_PROVIDER_URL": "https://login.microsoftonline.com/tenant-abc/v2.0",
		"TEMPORAL_AUTH_CLIENT_ID":    "client-123",
		"TEMPORAL_AUTH_SCOPES":       "openid,profile,email",
		"TEMPORAL_AUTH_CALLBACK_URL": "https://temporal.example.test/auth/sso/callback",
	}
	for name, want := range cases {
		e, ok := envByName(env, name)
		if !ok {
			t.Fatalf("missing env %s", name)
		}
		if e.Value != want {
			t.Errorf("%s = %q, want %q", name, e.Value, want)
		}
	}

	secret, ok := envByName(env, "TEMPORAL_AUTH_CLIENT_SECRET")
	if !ok || secret.ValueFrom == nil || secret.ValueFrom.SecretKeyRef == nil {
		t.Fatalf("TEMPORAL_AUTH_CLIENT_SECRET should use a secretKeyRef")
	}
	if secret.ValueFrom.SecretKeyRef.Name != "ui-oidc" || secret.ValueFrom.SecretKeyRef.Key != "client-secret" {
		t.Errorf("secretKeyRef = %+v", secret.ValueFrom.SecretKeyRef)
	}
}

func TestBuildUIDeployment_NoAuthByDefault(t *testing.T) {
	c := uiAuthCluster()
	c.Spec.UI.Auth = nil
	dep := BuildUIDeployment(c)
	if _, ok := envByName(dep.Spec.Template.Spec.Containers[0].Env, "TEMPORAL_AUTH_ENABLED"); ok {
		t.Errorf("auth env should be absent when UI.Auth is nil")
	}
}

func TestBuildUIDeployment_AuthDisabled(t *testing.T) {
	c := uiAuthCluster()
	c.Spec.UI.Auth.Enabled = false
	dep := BuildUIDeployment(c)
	if _, ok := envByName(dep.Spec.Template.Spec.Containers[0].Env, "TEMPORAL_AUTH_ENABLED"); ok {
		t.Errorf("auth env should be absent when UI.Auth.Enabled is false")
	}
}

func TestBuildUIDeployment_DirectProviderURL(t *testing.T) {
	c := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "direct", Namespace: "default"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Version: "1.31.1",
			UI: &temporalv1alpha1.UISpec{
				Enabled: true,
				Auth: &temporalv1alpha1.UIAuthSpec{
					Enabled:         true,
					ProviderURL:     "https://issuer.example/v2.0",
					ClientID:        "client-456",
					ClientSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "ui-secret", Key: "secret"},
					CallbackURL:     "https://temporal.example.test/auth/sso/callback",
				},
			},
		},
	}
	dep := BuildUIDeployment(c)
	env := dep.Spec.Template.Spec.Containers[0].Env

	e, ok := envByName(env, "TEMPORAL_AUTH_PROVIDER_URL")
	if !ok {
		t.Fatalf("missing env TEMPORAL_AUTH_PROVIDER_URL")
	}
	if e.Value != "https://issuer.example/v2.0" {
		t.Errorf("TEMPORAL_AUTH_PROVIDER_URL = %q, want %q", e.Value, "https://issuer.example/v2.0")
	}
}
