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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// clusterTLSConfig builds the client TLS config the operator uses to reach a
// cluster's frontend. It returns (nil, nil) when the cluster has no mTLS, in
// which case callers dial insecurely. When mTLS is enabled it reuses the
// cluster's internode certificate (clientAuth-capable and CA-trusted by the
// frontend) and verifies the frontend against its stable serverName.
func clusterTLSConfig(ctx context.Context, c client.Client, cluster *temporalv1alpha1.TemporalCluster) (*tls.Config, error) {
	if cluster.Spec.MTLS == nil {
		return nil, nil
	}

	var secret corev1.Secret
	key := types.NamespacedName{Namespace: cluster.Namespace, Name: resources.InternodeCertName(cluster.Name)}
	if err := c.Get(ctx, key, &secret); err != nil {
		return nil, fmt.Errorf("reading internode cert secret %s: %w", key, err)
	}

	cert, err := tls.X509KeyPair(secret.Data["tls.crt"], secret.Data["tls.key"])
	if err != nil {
		return nil, fmt.Errorf("parsing client key pair: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(secret.Data["ca.crt"]) {
		return nil, fmt.Errorf("no CA certificates found in %s ca.crt", key)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   fmt.Sprintf("%s.%s.svc.cluster.local", resources.FrontendServiceName(cluster.Name), cluster.Namespace),
		MinVersion:   tls.VersionTLS12,
	}, nil
}
