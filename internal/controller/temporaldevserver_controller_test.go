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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

var _ = Describe("TemporalDevServer controller", func() {
	It("creates an owned Deployment and Service for a supported version", func() {
		dev := &temporalv1alpha1.TemporalDevServer{
			ObjectMeta: metav1.ObjectMeta{Name: "dev-ctrl", Namespace: "default"},
			Spec:       temporalv1alpha1.TemporalDevServerSpec{Version: "1.31.1"},
		}
		Expect(k8sClient.Create(ctx, dev)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, dev) })

		reconciler := &TemporalDevServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "dev-ctrl"}})
		Expect(err).NotTo(HaveOccurred())

		var deploy appsv1.Deployment
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "dev-ctrl"}, &deploy)).To(Succeed())
		Expect(*deploy.Spec.Replicas).To(Equal(int32(1)))
		Expect(deploy.Spec.Template.Spec.Containers[0].Image).To(Equal("temporalio/temporal:1.7.2"))

		var svc corev1.Service
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: resources.DevServerFrontendServiceName("dev-ctrl")}, &svc)).To(Succeed())

		var got temporalv1alpha1.TemporalDevServer
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "dev-ctrl"}, &got)).To(Succeed())
		Expect(got.Status.Version).To(Equal("1.31.1"))
	})

	It("fails an unsupported version without creating a Deployment", func() {
		dev := &temporalv1alpha1.TemporalDevServer{
			ObjectMeta: metav1.ObjectMeta{Name: "dev-bad", Namespace: "default"},
			Spec:       temporalv1alpha1.TemporalDevServerSpec{Version: "9.9.9"},
		}
		Expect(k8sClient.Create(ctx, dev)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, dev) })

		reconciler := &TemporalDevServerReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "dev-bad"}})
		Expect(err).NotTo(HaveOccurred())

		var got temporalv1alpha1.TemporalDevServer
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "dev-bad"}, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal("Failed"))
		cond := apimeta.FindStatusCondition(got.Status.Conditions, temporalv1alpha1.ConditionReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(temporalv1alpha1.ReasonVersionUnsupported))

		var deploy appsv1.Deployment
		err = k8sClient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "dev-bad"}, &deploy)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
})
