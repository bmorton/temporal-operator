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
	"errors"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// ErrTargetNotFound indicates the referenced TemporalCluster or
// TemporalDevServer object does not exist.
var ErrTargetNotFound = errors.New("temporal target not found")

// ResolvedTarget describes how to reach the Temporal frontend named by a
// ClusterReference.
type ResolvedTarget struct {
	// Address is the gRPC frontend address (host:port).
	Address string
	// TLSConfig is the client TLS config, or nil for plaintext (dev server).
	TLSConfig *tls.Config
	// Ready reports whether the referenced target's Ready condition is true.
	Ready bool
}

// resolveTarget resolves a ClusterReference to a connectable Temporal frontend.
// It returns ErrTargetNotFound when the referenced object does not exist.
func resolveTarget(ctx context.Context, c client.Client, namespace string, ref temporalv1alpha1.ClusterReference) (*ResolvedTarget, error) {
	key := types.NamespacedName{Namespace: namespace, Name: ref.Name}

	switch ref.Kind {
	case "", temporalv1alpha1.ClusterKindTemporalCluster:
		var cluster temporalv1alpha1.TemporalCluster
		if err := c.Get(ctx, key, &cluster); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, ErrTargetNotFound
			}
			return nil, err
		}
		tlsConfig, err := clusterTLSConfig(ctx, c, &cluster)
		if err != nil {
			return nil, fmt.Errorf("building temporal client tls: %w", err)
		}
		return &ResolvedTarget{
			Address:   frontendAddress(&cluster),
			TLSConfig: tlsConfig,
			Ready:     meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionReady),
		}, nil

	case temporalv1alpha1.ClusterKindTemporalDevServer:
		var dev temporalv1alpha1.TemporalDevServer
		if err := c.Get(ctx, key, &dev); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, ErrTargetNotFound
			}
			return nil, err
		}
		return &ResolvedTarget{
			Address:   resources.DevServerFrontendEndpoint(&dev),
			TLSConfig: nil,
			Ready:     meta.IsStatusConditionTrue(dev.Status.Conditions, temporalv1alpha1.ConditionReady),
		}, nil

	default:
		return nil, fmt.Errorf("unknown cluster reference kind %q", ref.Kind)
	}
}
