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

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// reconcileUI deploys temporal-ui when enabled.
func (r *TemporalClusterReconciler) reconcileUI(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster) error {
	if cluster.Spec.UI == nil || !cluster.Spec.UI.Enabled {
		return nil
	}

	if mTLSEnabled(cluster) {
		if err := r.apply(ctx, cluster, resources.BuildUIClientCertificate(cluster)); err != nil {
			return err
		}
	}

	if err := r.apply(ctx, cluster, resources.BuildUIDeployment(cluster)); err != nil {
		return err
	}
	if err := r.apply(ctx, cluster, resources.BuildUIService(cluster)); err != nil {
		return err
	}
	if ingress := resources.BuildUIIngress(cluster); ingress != nil {
		if err := r.apply(ctx, cluster, ingress); err != nil {
			return err
		}
	}
	return nil
}
