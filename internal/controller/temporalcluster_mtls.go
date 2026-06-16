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
	"crypto/sha256"
	"encoding/hex"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/plan"
	"github.com/bmorton/temporal-operator/internal/resources"
)

// mTLSEnabled reports whether cert-manager-driven mTLS is configured.
func mTLSEnabled(cluster *temporalv1alpha1.TemporalCluster) bool {
	return resources.MTLSEnabled(cluster)
}

// reconcileMTLS applies the internode and frontend Certificates and sets the
// MTLSReady condition based on their issuance status.
func (r *TemporalClusterReconciler) reconcileMTLS(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster) error {
	if !mTLSEnabled(cluster) {
		return nil
	}

	for _, p := range plan.PlanMTLS(cluster) {
		if err := r.apply(ctx, cluster, p.Object); err != nil {
			return err
		}
	}

	ready, failed := r.certificatesStatus(ctx, cluster,
		resources.InternodeCertName(cluster.Name), resources.FrontendCertName(cluster.Name))
	switch {
	case failed:
		r.setMTLSReady(cluster, metav1.ConditionFalse, "CertificateIssuanceFailed", "one or more certificates failed to issue")
	case ready:
		r.setMTLSReady(cluster, metav1.ConditionTrue, "CertificatesReady", "all certificates are ready")
	default:
		r.setMTLSReady(cluster, metav1.ConditionFalse, temporalv1alpha1.ReasonReconciling, "waiting for certificates to be issued")
	}
	return nil
}

// certificatesStatus reports whether all named Certificates are Ready and
// whether any are in a Failed state.
func (r *TemporalClusterReconciler) certificatesStatus(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, names ...string) (allReady, anyFailed bool) {
	allReady = true
	for _, name := range names {
		var cert certmanagerv1.Certificate
		if err := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, &cert); err != nil {
			allReady = false
			continue
		}
		ready := false
		for _, c := range cert.Status.Conditions {
			if c.Type == certmanagerv1.CertificateConditionReady {
				switch c.Status {
				case cmmeta.ConditionTrue:
					ready = true
				case cmmeta.ConditionFalse:
					if c.Reason == "Failed" {
						anyFailed = true
					}
				}
			}
		}
		if !ready {
			allReady = false
		}
	}
	return allReady, anyFailed
}

// mtlsMounts returns the cert mount configuration for the service reconciler,
// including a cert hash derived from the cert secrets' resource versions so that
// rotation triggers a rolling restart.
func (r *TemporalClusterReconciler) mtlsMounts(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster) *resources.MTLSMounts {
	if !mTLSEnabled(cluster) {
		return nil
	}
	internodeSecret := resources.InternodeCertName(cluster.Name)
	frontendSecret := resources.FrontendCertName(cluster.Name)
	return &resources.MTLSMounts{
		Enabled:         true,
		InternodeSecret: internodeSecret,
		FrontendSecret:  frontendSecret,
		CertHash:        r.certHash(ctx, cluster, internodeSecret, frontendSecret),
	}
}

func (r *TemporalClusterReconciler) certHash(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, secretNames ...string) string {
	h := sha256.New()
	for _, name := range secretNames {
		var secret corev1.Secret
		if err := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, &secret); err != nil {
			if apierrors.IsNotFound(err) {
				continue
			}
			return ""
		}
		h.Write([]byte(name))
		h.Write([]byte(secret.ResourceVersion))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func (r *TemporalClusterReconciler) setMTLSReady(cluster *temporalv1alpha1.TemporalCluster, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionMTLSReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cluster.Generation,
	})
}
