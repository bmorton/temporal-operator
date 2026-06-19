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

package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/proxy"
)

const (
	testMigrationNamespace = "temporal-system"
	testMigrationName      = "mig"
	testClusterName        = "newcluster"
	testSourceAddress      = "old:7233"
)

func TestRenderProxyConfigPassthrough(t *testing.T) {
	m := &temporalv1alpha1.TemporalMigration{}
	m.Name = testMigrationName
	m.Namespace = testMigrationNamespace
	m.Spec.Source.Address = testSourceAddress
	m.Spec.Cutover = false
	cluster := &temporalv1alpha1.TemporalCluster{}
	cluster.Name = testClusterName
	cluster.Namespace = testMigrationNamespace

	cfg, mounts, err := renderProxyConfig(m, cluster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != proxy.ModePassthrough {
		t.Errorf("mode = %q, want passthrough", cfg.Mode)
	}
	if cfg.Source.Address != testSourceAddress {
		t.Errorf("source = %q", cfg.Source.Address)
	}
	if cfg.Target.Address == "" {
		t.Errorf("target address empty")
	}
	if len(mounts) != 0 {
		t.Errorf("expected no secret mounts for non-mTLS, got %d", len(mounts))
	}
}

func TestRenderProxyConfigCutoverWithSourceTLS(t *testing.T) {
	m := &temporalv1alpha1.TemporalMigration{}
	m.Name = testMigrationName
	m.Namespace = testMigrationNamespace
	m.Spec.Source.Address = testSourceAddress
	m.Spec.Source.TLS = &temporalv1alpha1.SourceTLSSpec{
		Enabled:   true,
		SecretRef: &corev1.LocalObjectReference{Name: "old-tls"},
	}
	m.Spec.Cutover = true
	cluster := &temporalv1alpha1.TemporalCluster{}
	cluster.Name = testClusterName
	cluster.Namespace = testMigrationNamespace

	cfg, mounts, err := renderProxyConfig(m, cluster, &sourceTLSFiles{HasCA: true, HasClientCert: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != proxy.ModeCutover {
		t.Errorf("mode = %q, want cutover", cfg.Mode)
	}
	if cfg.Source.TLS == nil || cfg.Source.TLS.CAFile == "" {
		t.Errorf("source TLS not rendered: %+v", cfg.Source.TLS)
	}
	if cfg.Source.TLS.CertFile == "" {
		t.Errorf("source client cert not rendered: %+v", cfg.Source.TLS)
	}
	if len(mounts) != 1 || mounts[0].SecretName != "old-tls" {
		t.Errorf("expected one source-tls mount, got %+v", mounts)
	}
}

func TestRenderProxyConfigSourceTLSCAOnly(t *testing.T) {
	m := &temporalv1alpha1.TemporalMigration{}
	m.Name = testMigrationName
	m.Namespace = testMigrationNamespace
	m.Spec.Source.Address = testSourceAddress
	m.Spec.Source.TLS = &temporalv1alpha1.SourceTLSSpec{
		Enabled:   true,
		SecretRef: &corev1.LocalObjectReference{Name: "old-tls"},
	}
	cluster := &temporalv1alpha1.TemporalCluster{}
	cluster.Name = testClusterName
	cluster.Namespace = testMigrationNamespace

	cfg, mounts, err := renderProxyConfig(m, cluster, &sourceTLSFiles{HasCA: true, HasClientCert: false})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Source.TLS == nil || cfg.Source.TLS.CAFile == "" {
		t.Errorf("expected CAFile set, got %+v", cfg.Source.TLS)
	}
	if cfg.Source.TLS.CertFile != "" || cfg.Source.TLS.KeyFile != "" {
		t.Errorf("expected no client cert for CA-only secret, got CertFile=%q KeyFile=%q", cfg.Source.TLS.CertFile, cfg.Source.TLS.KeyFile)
	}
	if len(mounts) != 1 {
		t.Errorf("expected one mount, got %d", len(mounts))
	}
}
