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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

func ctrlRequest(name, namespace string) ctrl.Request {
	return ctrl.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: namespace}}
}

type fakeMigClient struct {
	namespaces []string
	running    map[string]int64
}

func (f *fakeMigClient) ListNamespaces(context.Context) ([]string, error) { return f.namespaces, nil }
func (f *fakeMigClient) CountRunningWorkflows(_ context.Context, ns string) (int64, error) {
	return f.running[ns], nil
}
func (f *fakeMigClient) Close() error { return nil }

var _ = Describe("TemporalMigration controller", func() {
	ctx := context.Background()

	It("provisions the proxy and reports Passthrough", func() {
		cluster := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "newc", Namespace: "default"},
			Spec:       validClusterSpec("1.31.1"),
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

		mig := &temporalv1alpha1.TemporalMigration{
			ObjectMeta: metav1.ObjectMeta{Name: "mig", Namespace: "default"},
			Spec: temporalv1alpha1.TemporalMigrationSpec{
				Source:    temporalv1alpha1.SourceClusterSpec{Address: "old:7233"},
				TargetRef: corev1.LocalObjectReference{Name: "newc"},
			},
		}
		Expect(k8sClient.Create(ctx, mig)).To(Succeed())

		r := &TemporalMigrationReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
			MigrationClientFactory: func(context.Context, string, *tls.Config) (temporal.MigrationClient, error) {
				return &fakeMigClient{namespaces: []string{"orders"}, running: map[string]int64{"orders": 5}}, nil
			},
		}
		_, err := r.Reconcile(ctx, ctrlRequest("mig", "default"))
		Expect(err).NotTo(HaveOccurred())

		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "mig-proxy", Namespace: "default"}, &dep)).To(Succeed())
		Expect(dep.OwnerReferences).NotTo(BeEmpty())

		var got temporalv1alpha1.TemporalMigration
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "mig", Namespace: "default"}, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(temporalv1alpha1.MigrationPhasePassthrough))
		Expect(got.Status.ProxyEndpoint).NotTo(BeEmpty())
	})

	It("enters Cutover then Complete when source has drained", func() {
		cluster := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "newc2", Namespace: "default"},
			Spec:       validClusterSpec("1.31.1"),
		}
		Expect(k8sClient.Create(ctx, cluster)).To(Succeed())
		mig := &temporalv1alpha1.TemporalMigration{
			ObjectMeta: metav1.ObjectMeta{Name: "mig2", Namespace: "default"},
			Spec: temporalv1alpha1.TemporalMigrationSpec{
				Source:    temporalv1alpha1.SourceClusterSpec{Address: "old:7233"},
				TargetRef: corev1.LocalObjectReference{Name: "newc2"},
				Cutover:   true,
			},
		}
		Expect(k8sClient.Create(ctx, mig)).To(Succeed())

		drained := &fakeMigClient{namespaces: []string{"orders"}, running: map[string]int64{"orders": 0}}
		r := &TemporalMigrationReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
			DrainStableThreshold: 1,
			MigrationClientFactory: func(context.Context, string, *tls.Config) (temporal.MigrationClient, error) {
				return drained, nil
			},
		}
		for i := 0; i < 3; i++ {
			_, err := r.Reconcile(ctx, ctrlRequest("mig2", "default"))
			Expect(err).NotTo(HaveOccurred())
			time.Sleep(10 * time.Millisecond)
		}
		var got temporalv1alpha1.TemporalMigration
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "mig2", Namespace: "default"}, &got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(temporalv1alpha1.MigrationPhaseComplete))
		Expect(got.Status.CutoverTime).NotTo(BeNil())
	})
})
