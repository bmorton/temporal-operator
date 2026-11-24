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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/plan"
	"github.com/bmorton/temporal-operator/internal/resources"
)

var httpRouteGVK = schema.GroupVersionKind{Group: gatewayv1.GroupName, Version: "v1", Kind: "HTTPRoute"}

// reconcileUI applies the temporal-ui objects the spec asks for and deletes the
// ones it no longer asks for: everything when the UI is disabled, or just the
// Ingress, HTTPRoute or client Certificate when that part is turned off.
func (r *TemporalClusterReconciler) reconcileUI(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster) error {
	routeInstalled := r.kindInstalled(httpRouteGVK)

	desired := map[schema.GroupKind]bool{}
	for _, p := range plan.PlanUI(cluster) {
		gk := p.Object.GetObjectKind().GroupVersionKind().GroupKind()
		if gk == httpRouteGVK.GroupKind() && !routeInstalled {
			logf.FromContext(ctx).Info("Gateway API HTTPRoute CRD not installed; skipping UI HTTPRoute")
			r.warn(cluster, "GatewayAPINotInstalled",
				"spec.ui.httpRoute is enabled but the gateway.networking.k8s.io/v1 HTTPRoute CRD is not installed; no route was created")
			continue
		}
		if err := r.apply(ctx, cluster, p.Object); err != nil {
			return err
		}
		desired[gk] = true
	}

	meta := metav1.ObjectMeta{Name: resources.UIName(cluster.Name), Namespace: cluster.Namespace}
	candidates := []client.Object{
		&networkingv1.Ingress{ObjectMeta: meta},
		&corev1.Service{ObjectMeta: meta},
		&appsv1.Deployment{ObjectMeta: meta},
		&certmanagerv1.Certificate{ObjectMeta: metav1.ObjectMeta{
			Name: resources.UIClientCertName(cluster.Name), Namespace: cluster.Namespace,
		}},
	}
	if routeInstalled {
		candidates = append(candidates, &gatewayv1.HTTPRoute{ObjectMeta: meta})
	}
	for _, obj := range candidates {
		gvk, err := apiutil.GVKForObject(obj, r.Scheme)
		if err != nil {
			return err
		}
		if desired[gvk.GroupKind()] {
			continue
		}
		if err := r.prune(ctx, cluster, obj); err != nil {
			return err
		}
	}
	return nil
}

// prune deletes obj (identified by its name and namespace) when it exists and
// is controlled by cluster. Objects the operator did not create are left alone,
// even when they share a name with one it would have created.
func (r *TemporalClusterReconciler) prune(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, obj client.Object) error {
	if err := r.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
		return client.IgnoreNotFound(err)
	}
	if !metav1.IsControlledBy(obj, cluster) || obj.GetDeletionTimestamp() != nil {
		return nil
	}
	uid := obj.GetUID()
	if err := r.Delete(ctx, obj, client.Preconditions{UID: &uid}); err != nil {
		return client.IgnoreNotFound(err)
	}
	logf.FromContext(ctx).Info("deleted resource no longer requested by spec",
		"kind", obj.GetObjectKind().GroupVersionKind().Kind, "name", obj.GetName())
	return nil
}

// kindInstalled reports whether the API server serves gvk, according to the
// controller's RESTMapper. It is how optional APIs (Gateway API, Prometheus
// Operator) are detected.
func (r *TemporalClusterReconciler) kindInstalled(gvk schema.GroupVersionKind) bool {
	mapper := r.RESTMapper()
	if mapper == nil {
		return false
	}
	_, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	return err == nil
}

// warn records a Warning event on the cluster.
func (r *TemporalClusterReconciler) warn(cluster *temporalv1alpha1.TemporalCluster, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Eventf(cluster, nil, corev1.EventTypeWarning, reason, reason, message)
	}
}
