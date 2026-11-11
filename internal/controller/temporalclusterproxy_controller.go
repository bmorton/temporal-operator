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
	"errors"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
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
	"github.com/bmorton/temporal-operator/internal/temporal"
)

const (
	clusterProxyFinalizer   = "temporal.bmor10.com/clusterproxy"
	proxyServicesFieldOwner = client.FieldOwner("temporal-operator-clusterproxy")
	proxyTLSProviderSecret  = "secret"
)

// TemporalClusterProxyReconciler deploys an s2s-proxy for one local cluster and
// registers the peer as a remote cluster via the local proxy.
type TemporalClusterProxyReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	ClientFactory temporal.RemoteClusterClientFactory
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterproxies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterproxies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporalclusterproxies/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps;secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=get;list;watch;create;update;patch;delete

func (r *TemporalClusterProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var cr temporalv1alpha1.TemporalClusterProxy
	if err := r.Get(ctx, req.NamespacedName, &cr); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !cr.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, r.reconcileDelete(ctx, &cr)
	}
	if !controllerutil.ContainsFinalizer(&cr, clusterProxyFinalizer) {
		controllerutil.AddFinalizer(&cr, clusterProxyFinalizer)
		if err := r.Update(ctx, &cr); err != nil {
			return ctrl.Result{}, err
		}
	}

	target, err := resolveTarget(ctx, r.Client, cr.Namespace, cr.Spec.LocalClusterRef)
	if err != nil {
		if errors.Is(err, ErrTargetNotFound) {
			r.setReady(&cr, metav1.ConditionFalse, temporalv1alpha1.ReasonClusterNotReady, "local cluster not found")
			return ctrl.Result{RequeueAfter: namespaceDriftRequeue}, r.statusUpdate(ctx, &cr)
		}
		return ctrl.Result{}, err
	}

	// Render + apply proxy resources.
	if err := r.applyResources(ctx, &cr, target.Address); err != nil {
		return ctrl.Result{}, err
	}

	deployReady := r.deploymentAvailable(ctx, &cr)
	r.setProxyDeployedCondition(&cr, deployReady)

	mtlsReady, mtlsMsg := r.checkMTLS(ctx, &cr)
	r.setMTLSCondition(&cr, mtlsReady, mtlsMsg)

	// Publish endpoint for server role.
	if cr.Spec.Mux.Role == temporalv1alpha1.ProxyRoleServer && cr.Spec.Mux.Server != nil {
		cr.Status.ProxyEndpoint = r.serverEndpoint(ctx, &cr)
	}

	// Register the peer via the local proxy once the proxy and local cluster are ready.
	registered := r.reconcileRegistration(ctx, &cr, deployReady, target.Ready)

	switch {
	case !mtlsReady:
		r.setReady(&cr, metav1.ConditionFalse, temporalv1alpha1.ReasonMTLSNotReady, mtlsMsg)
	case deployReady && registered:
		r.setReady(&cr, metav1.ConditionTrue, temporalv1alpha1.ReasonProxyReady, "proxy deployed and peer registered")
	default:
		r.setReady(&cr, metav1.ConditionFalse, temporalv1alpha1.ReasonProxyNotReady, "proxy not fully converged")
	}
	return ctrl.Result{RequeueAfter: namespaceDriftRequeue}, r.statusUpdate(ctx, &cr)
}

// checkMTLS reports whether the mux TLS material is present and complete. For a
// cert-manager provider the Secret appears once the Certificate is issued; for a
// BYO secret provider it must already exist with the certificate, key, and CA
// keys. It returns false with a diagnostic message while the material is absent
// or incomplete so a missing/misconfigured secret surfaces an explicit reason
// instead of a silently stuck Deployment.
func (r *TemporalClusterProxyReconciler) checkMTLS(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy) (bool, string) {
	secretName := resources.ClusterProxyTLSSecretName(cr)
	var sec corev1.Secret
	if err := r.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: secretName}, &sec); err != nil {
		if apierrors.IsNotFound(err) {
			if cr.Spec.Mux.TLS.Provider == proxyTLSProviderSecret {
				return false, fmt.Sprintf("mux TLS secret %q not found", secretName)
			}
			return false, fmt.Sprintf("mux TLS certificate secret %q not yet issued", secretName)
		}
		return false, fmt.Sprintf("reading mux TLS secret %q: %v", secretName, err)
	}

	certKey, keyKey, caKey := "tls.crt", "tls.key", "ca.crt"
	if cr.Spec.Mux.TLS.Provider == proxyTLSProviderSecret && cr.Spec.Mux.TLS.SecretRef != nil {
		ref := cr.Spec.Mux.TLS.SecretRef
		if ref.CertKey != "" {
			certKey = ref.CertKey
		}
		if ref.KeyKey != "" {
			keyKey = ref.KeyKey
		}
		if ref.CAKey != "" {
			caKey = ref.CAKey
		}
	}
	for _, k := range []string{certKey, keyKey, caKey} {
		if len(sec.Data[k]) == 0 {
			return false, fmt.Sprintf("mux TLS secret %q is missing key %q", secretName, k)
		}
	}
	return true, "mux TLS material present"
}

func (r *TemporalClusterProxyReconciler) setMTLSCondition(cr *temporalv1alpha1.TemporalClusterProxy, ready bool, message string) {
	status := metav1.ConditionFalse
	reason := temporalv1alpha1.ReasonMTLSNotReady
	if ready {
		status = metav1.ConditionTrue
		reason = temporalv1alpha1.ReasonProxyReady
	}
	meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionMTLSReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cr.Generation,
	})
}

// applyResources renders and server-side-applies the proxy's owned resources.
func (r *TemporalClusterProxyReconciler) applyResources(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy, localFrontendAddress string) error {
	configYAML, err := resources.BuildClusterProxyConfig(cr, localFrontendAddress)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(configYAML))
	configHash := hex.EncodeToString(sum[:])

	objs := []client.Object{
		resources.BuildClusterProxyConfigMap(cr, configYAML),
		resources.BuildClusterProxyService(cr),
		resources.BuildClusterProxyDeployment(cr, configHash),
	}
	if cr.Spec.Mux.TLS.Provider != proxyTLSProviderSecret && cr.Spec.Mux.TLS.IssuerRef != nil {
		objs = append([]client.Object{resources.BuildClusterProxyCertificate(cr)}, objs...)
	}
	for _, obj := range objs {
		if err := r.apply(ctx, cr, obj); err != nil {
			return err
		}
	}
	return nil
}

func (r *TemporalClusterProxyReconciler) setProxyDeployedCondition(cr *temporalv1alpha1.TemporalClusterProxy, deployReady bool) {
	if deployReady {
		meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{Type: temporalv1alpha1.ConditionProxyDeployed, Status: metav1.ConditionTrue, Reason: temporalv1alpha1.ReasonProxyReady, Message: "proxy deployment available", ObservedGeneration: cr.Generation})
		return
	}
	meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{Type: temporalv1alpha1.ConditionProxyDeployed, Status: metav1.ConditionFalse, Reason: temporalv1alpha1.ReasonProxyNotReady, Message: "proxy deployment not yet available", ObservedGeneration: cr.Generation})
}

// reconcileRegistration registers the peer via the local proxy when both the
// proxy Deployment and the local cluster are ready, and records the result as a
// condition. It returns whether the peer is registered.
func (r *TemporalClusterProxyReconciler) reconcileRegistration(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy, deployReady, clusterReady bool) bool {
	if !deployReady || !clusterReady {
		meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{Type: temporalv1alpha1.ConditionRemoteClusterRegistered, Status: metav1.ConditionFalse, Reason: temporalv1alpha1.ReasonClusterNotReady, Message: "waiting for proxy and local cluster", ObservedGeneration: cr.Generation})
		return false
	}
	if err := r.registerPeer(ctx, cr); err != nil {
		logf.FromContext(ctx).Error(err, "registering peer via proxy")
		meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{Type: temporalv1alpha1.ConditionRemoteClusterRegistered, Status: metav1.ConditionFalse, Reason: temporalv1alpha1.ReasonRegistrationFailed, Message: err.Error(), ObservedGeneration: cr.Generation})
		return false
	}
	meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{Type: temporalv1alpha1.ConditionRemoteClusterRegistered, Status: metav1.ConditionTrue, Reason: temporalv1alpha1.ReasonProxyReady, Message: "peer registered via local proxy", ObservedGeneration: cr.Generation})
	return true
}

// registerPeer dials the local Temporal frontend and registers the peer with the
// local proxy tcpServer address as its frontend.
func (r *TemporalClusterProxyReconciler) registerPeer(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy) error {
	target, err := resolveTarget(ctx, r.Client, cr.Namespace, cr.Spec.LocalClusterRef)
	if err != nil {
		return err
	}
	c, err := r.clientFactory()(ctx, target.Address, target.TLSConfig)
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	proxyAddr := fmt.Sprintf("%s.%s.svc.cluster.local:%d", resources.ClusterProxyServiceName(cr), cr.Namespace, resources.ProxyTCPServerPort)
	enable := cr.Spec.Peer.EnableConnection == nil || *cr.Spec.Peer.EnableConnection
	return c.UpsertRemoteCluster(ctx, proxyAddr, enable)
}

func (r *TemporalClusterProxyReconciler) reconcileDelete(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy) error {
	if !controllerutil.ContainsFinalizer(cr, clusterProxyFinalizer) {
		return nil
	}
	target, err := resolveTarget(ctx, r.Client, cr.Namespace, cr.Spec.LocalClusterRef)
	if err == nil && target.Ready {
		if c, derr := r.clientFactory()(ctx, target.Address, target.TLSConfig); derr == nil {
			_ = c.RemoveRemoteCluster(ctx, cr.Spec.Peer.Name)
			_ = c.Close()
		}
	}
	// Owned resources are GC'd via owner references.
	controllerutil.RemoveFinalizer(cr, clusterProxyFinalizer)
	return r.Update(ctx, cr)
}

func (r *TemporalClusterProxyReconciler) deploymentAvailable(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy) bool {
	var dep appsv1.Deployment
	key := types.NamespacedName{Namespace: cr.Namespace, Name: resources.ClusterProxyName(cr)}
	if err := r.Get(ctx, key, &dep); err != nil {
		return false
	}
	return dep.Status.AvailableReplicas > 0
}

func (r *TemporalClusterProxyReconciler) serverEndpoint(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy) string {
	// For LoadBalancer, surface the assigned ingress; otherwise the in-cluster DNS name.
	var svc corev1.Service
	if err := r.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: resources.ClusterProxyServiceName(cr)}, &svc); err == nil {
		for _, ing := range svc.Status.LoadBalancer.Ingress {
			host := ing.Hostname
			if host == "" {
				host = ing.IP
			}
			if host != "" {
				return fmt.Sprintf("%s:%d", host, cr.Spec.Mux.Server.ListenPort)
			}
		}
	}
	return fmt.Sprintf("%s.%s.svc.cluster.local:%d", resources.ClusterProxyServiceName(cr), cr.Namespace, cr.Spec.Mux.Server.ListenPort)
}

func (r *TemporalClusterProxyReconciler) apply(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy, obj client.Object) error {
	if err := controllerutil.SetControllerReference(cr, obj, r.Scheme); err != nil {
		return err
	}
	return serverSideApply(ctx, r.Client, r.Scheme, obj, proxyServicesFieldOwner)
}

func (r *TemporalClusterProxyReconciler) clientFactory() temporal.RemoteClusterClientFactory {
	if r.ClientFactory != nil {
		return r.ClientFactory
	}
	return temporal.NewRemoteClusterClient
}

func (r *TemporalClusterProxyReconciler) setReady(cr *temporalv1alpha1.TemporalClusterProxy, status metav1.ConditionStatus, reason, message string) {
	cr.Status.ObservedGeneration = cr.Generation
	meta.SetStatusCondition(&cr.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cr.Generation,
	})
}

func (r *TemporalClusterProxyReconciler) statusUpdate(ctx context.Context, cr *temporalv1alpha1.TemporalClusterProxy) error {
	return client.IgnoreNotFound(r.Status().Update(ctx, cr))
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalClusterProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalClusterProxy{}).
		Owns(&appsv1.Deployment{}).
		Named("temporalclusterproxy").
		Complete(r)
}
