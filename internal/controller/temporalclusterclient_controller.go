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

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

const clientFieldOwner = client.FieldOwner("temporal-operator/client")

// TemporalClusterClientReconciler reconciles TemporalClusterClient objects by
// issuing client certificates from the referenced cluster's mTLS issuer.
type TemporalClusterClientReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterclients,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterclients/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterclients/finalizers,verbs=update

// Reconcile issues a client certificate for the TemporalClusterClient.
func (r *TemporalClusterClientReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var cc temporalv1alpha1.TemporalClusterClient
	if err := r.Get(ctx, req.NamespacedName, &cc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	var cluster temporalv1alpha1.TemporalCluster
	clusterKey := types.NamespacedName{Namespace: cc.Namespace, Name: cc.Spec.ClusterRef.Name}
	if err := r.Get(ctx, clusterKey, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			r.setReady(&cc, metav1.ConditionFalse, "ClusterNotFound", "referenced TemporalCluster not found")
			return ctrl.Result{}, r.statusUpdate(ctx, &cc)
		}
		return ctrl.Result{}, err
	}

	if cluster.Spec.MTLS == nil || cluster.Spec.MTLS.IssuerRef == nil {
		r.setReady(&cc, metav1.ConditionFalse, "ClusterMTLSDisabled", "cluster does not have mTLS enabled")
		return ctrl.Result{}, r.statusUpdate(ctx, &cc)
	}

	cert := resources.BuildClientCertificate(&cc, &cluster)
	if err := controllerutil.SetControllerReference(&cc, cert, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.Patch(ctx, cert, client.Apply, clientFieldOwner, client.ForceOwnership); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("reconciled client certificate", "secret", resources.ClientSecretName(&cc))

	secretName := resources.ClientSecretName(&cc)
	cc.Status.SecretRef = &corev1.LocalObjectReference{Name: secretName}

	if r.certificateReady(ctx, &cc) {
		r.setReady(&cc, metav1.ConditionTrue, "CertificateReady", "client credentials are ready")
	} else {
		r.setReady(&cc, metav1.ConditionFalse, temporalv1alpha1.ReasonReconciling, "waiting for the client certificate to be issued")
	}
	return ctrl.Result{}, r.statusUpdate(ctx, &cc)
}

func (r *TemporalClusterClientReconciler) certificateReady(ctx context.Context, cc *temporalv1alpha1.TemporalClusterClient) bool {
	var cert certmanagerv1.Certificate
	if err := r.Get(ctx, types.NamespacedName{Namespace: cc.Namespace, Name: cc.Name}, &cert); err != nil {
		return false
	}
	for _, c := range cert.Status.Conditions {
		if c.Type == certmanagerv1.CertificateConditionReady && c.Status == cmmeta.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *TemporalClusterClientReconciler) setReady(cc *temporalv1alpha1.TemporalClusterClient, status metav1.ConditionStatus, reason, message string) {
	cc.Status.ObservedGeneration = cc.Generation
	meta.SetStatusCondition(&cc.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cc.Generation,
	})
}

func (r *TemporalClusterClientReconciler) statusUpdate(ctx context.Context, cc *temporalv1alpha1.TemporalClusterClient) error {
	if err := r.Status().Update(ctx, cc); err != nil {
		if apierrors.IsConflict(err) {
			return nil
		}
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalClusterClientReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalClusterClient{}).
		Owns(&certmanagerv1.Certificate{}).
		Named("temporalclusterclient").
		Complete(r)
}
