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
	"fmt"

	"sigs.k8s.io/yaml"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/proxy"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// secretMount describes a Secret the proxy pod must mount.
type secretMount struct {
	SecretName string
	MountPath  string
}

// sourceTLSFiles reports which TLS materials the source secret actually
// contains, so the proxy only references files that will exist on disk.
type sourceTLSFiles struct {
	HasCA         bool
	HasClientCert bool
}

const (
	sourceTLSMountPath = "/etc/migration-proxy/source-tls"
	targetTLSMountPath = "/etc/migration-proxy/target-tls"
)

// renderProxyConfig builds the proxy config and required secret mounts.
func renderProxyConfig(m *temporalv1alpha1.TemporalMigration, cluster *temporalv1alpha1.TemporalCluster, srcFiles *sourceTLSFiles) (*proxy.Config, []secretMount, error) {
	mode := proxy.ModePassthrough
	if m.Spec.Cutover {
		mode = proxy.ModeCutover
	}

	cfg := &proxy.Config{
		Mode:   mode,
		Listen: ":7233",
		Source: proxy.BackendConfig{Address: m.Spec.Source.Address},
		Target: proxy.BackendConfig{Address: frontendAddress(cluster)},
	}

	var mounts []secretMount

	if t := m.Spec.Source.TLS; t != nil && t.Enabled {
		if t.SecretRef == nil {
			return nil, nil, fmt.Errorf("source.tls.enabled requires source.tls.secretRef")
		}
		backend := &proxy.BackendTLS{ServerName: t.ServerName}
		if srcFiles != nil && srcFiles.HasCA {
			backend.CAFile = sourceTLSMountPath + "/ca.crt"
		}
		if srcFiles != nil && srcFiles.HasClientCert {
			backend.CertFile = sourceTLSMountPath + "/tls.crt"
			backend.KeyFile = sourceTLSMountPath + "/tls.key"
		}
		cfg.Source.TLS = backend
		mounts = append(mounts, secretMount{SecretName: t.SecretRef.Name, MountPath: sourceTLSMountPath})
	}

	if cluster.Spec.MTLS != nil {
		cfg.Target.TLS = &proxy.BackendTLS{
			CAFile:     targetTLSMountPath + "/ca.crt",
			CertFile:   targetTLSMountPath + "/tls.crt",
			KeyFile:    targetTLSMountPath + "/tls.key",
			ServerName: fmt.Sprintf("%s.%s.svc.cluster.local", resources.FrontendServiceName(cluster.Name), cluster.Namespace),
		}
		mounts = append(mounts, secretMount{SecretName: resources.InternodeCertName(cluster.Name), MountPath: targetTLSMountPath})
	}

	return cfg, mounts, nil
}

// marshalProxyConfig renders the config to YAML for the ConfigMap.
func marshalProxyConfig(cfg *proxy.Config) (string, error) {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
