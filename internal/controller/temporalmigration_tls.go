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
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

type tlsConfig = tls.Config

// defaultProxyImage resolves the proxy image from the environment, defaulting
// to the operator image at a well-known tag.
func defaultProxyImage() string {
	if v := os.Getenv(migrationProxyImageEnv); v != "" {
		return v
	}
	return "ghcr.io/bmorton/temporal-operator:latest"
}

// buildSourceTLSConfig builds a *tls.Config from the source TLS secret.
func buildSourceTLSConfig(ctx context.Context, c client.Client, namespace string, t *temporalv1alpha1.SourceTLSSpec) (*tls.Config, error) {
	var secret corev1.Secret
	key := types.NamespacedName{Namespace: namespace, Name: t.SecretRef.Name}
	if err := c.Get(ctx, key, &secret); err != nil {
		return nil, fmt.Errorf("reading source tls secret %s: %w", key, err)
	}
	cfg := &tls.Config{ServerName: t.ServerName, MinVersion: tls.VersionTLS12}
	if ca := secret.Data["ca.crt"]; len(ca) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("invalid ca.crt in %s", key)
		}
		cfg.RootCAs = pool
	}
	if crt, k := secret.Data["tls.crt"], secret.Data["tls.key"]; len(crt) > 0 && len(k) > 0 {
		cert, err := tls.X509KeyPair(crt, k)
		if err != nil {
			return nil, fmt.Errorf("parsing source client cert: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}
