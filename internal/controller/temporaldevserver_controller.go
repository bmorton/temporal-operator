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

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

const devServerFieldOwner = client.FieldOwner("temporal-operator/devserver")

// TemporalDevServerReconciler reconciles TemporalDevServer objects into a
// single-pod dev server Deployment + Service (+ optional PVC).
type TemporalDevServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporaldevservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporaldevservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=temporal.bmor10.com,resources=temporaldevservers/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

// Reconcile applies the dev server resources and updates status.
func (r *TemporalDevServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var dev temporalv1alpha1.TemporalDevServer
	if err := r.Get(ctx, req.NamespacedName, &dev); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if pvc := resources.BuildDevServerPVC(&dev); pvc != nil {
		if err := r.apply(ctx, &dev, pvc); err != nil {
			return ctrl.Result{}, err
		}
	}
	if err := r.apply(ctx, &dev, resources.BuildDevServerService(&dev)); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.apply(ctx, &dev, resources.BuildDevServerDeployment(&dev)); err != nil {
		return ctrl.Result{}, err
	}

	ready, err := r.deploymentReady(ctx, &dev)
	if err != nil {
		return ctrl.Result{}, err
	}

	dev.Status.ObservedGeneration = dev.Generation
	dev.Status.Version = resources.DevServerImage(&dev)
	dev.Status.Endpoints = temporalv1alpha1.DevServerEndpoints{
		Frontend: resources.DevServerFrontendEndpoint(&dev),
	}
	if dev.Spec.UI == nil || dev.Spec.UI.Enabled {
		dev.Status.Endpoints.UI = resources.DevServerUIEndpoint(&dev)
	}

	if ready {
		dev.Status.Phase = "Ready"
		r.setReady(&dev, metav1.ConditionTrue, "Available", "dev server is ready")
	} else {
		dev.Status.Phase = "Pending"
		r.setReady(&dev, metav1.ConditionFalse, temporalv1alpha1.ReasonReconciling, "waiting for the dev server pod to become ready")
	}
	return ctrl.Result{}, r.statusUpdate(ctx, &dev)
}

func (r *TemporalDevServerReconciler) apply(ctx context.Context, dev *temporalv1alpha1.TemporalDevServer, obj client.Object) error {
	if err := controllerutil.SetControllerReference(dev, obj, r.Scheme); err != nil {
		return err
	}
	return serverSideApply(ctx, r.Client, r.Scheme, obj, devServerFieldOwner)
}

func (r *TemporalDevServerReconciler) deploymentReady(ctx context.Context, dev *temporalv1alpha1.TemporalDevServer) (bool, error) {
	var deploy appsv1.Deployment
	key := types.NamespacedName{Namespace: dev.Namespace, Name: dev.Name}
	if err := r.Get(ctx, key, &deploy); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return deploy.Status.ReadyReplicas >= 1, nil
}

func (r *TemporalDevServerReconciler) setReady(dev *temporalv1alpha1.TemporalDevServer, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&dev.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: dev.Generation,
	})
}

func (r *TemporalDevServerReconciler) statusUpdate(ctx context.Context, dev *temporalv1alpha1.TemporalDevServer) error {
	if err := r.Status().Update(ctx, dev); err != nil {
		if apierrors.IsConflict(err) {
			return nil
		}
		return client.IgnoreNotFound(err)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *TemporalDevServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalDevServer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Named("temporaldevserver").
		Complete(r)
}
