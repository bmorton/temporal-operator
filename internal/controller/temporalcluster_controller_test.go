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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

var _ = Describe("TemporalCluster Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		temporalcluster := &temporalv1alpha1.TemporalCluster{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind TemporalCluster")
			err := k8sClient.Get(ctx, typeNamespacedName, temporalcluster)
			if err != nil && errors.IsNotFound(err) {
				resource := &temporalv1alpha1.TemporalCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: validClusterSpec("1.31.1"),
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &temporalv1alpha1.TemporalCluster{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance TemporalCluster")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should set a Ready=False condition while persistence is unreachable", func() {
			By("Reconciling the created resource")
			controllerReconciler := &TemporalClusterReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the Ready condition")
			Eventually(func(g Gomega) {
				resource := &temporalv1alpha1.TemporalCluster{}
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, resource)).To(Succeed())

				cond := meta.FindStatusCondition(resource.Status.Conditions, temporalv1alpha1.ConditionReady)
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(resource.Status.Phase).To(Equal("Pending"))
				g.Expect(resource.Status.ObservedGeneration).To(Equal(resource.Generation))
			}, 5*time.Second, 200*time.Millisecond).Should(Succeed())
		})
	})
})

// validClusterSpec returns a minimal valid TemporalClusterSpec backed by a
// SQL (Postgres) default and visibility store.
func validClusterSpec(version string) temporalv1alpha1.TemporalClusterSpec {
	sql := func(db string) *temporalv1alpha1.SQLDatastoreSpec {
		return &temporalv1alpha1.SQLDatastoreSpec{
			PluginName: "postgres12",
			Host:       "postgres.default.svc",
			Port:       5432,
			Database:   db,
			User:       "temporal",
			PasswordSecretRef: &temporalv1alpha1.SecretKeyReference{
				Name: "temporal-store",
				Key:  "password",
			},
		}
	}
	return temporalv1alpha1.TemporalClusterSpec{
		Version:          version,
		NumHistoryShards: 512,
		Persistence: temporalv1alpha1.PersistenceSpec{
			DefaultStore:    temporalv1alpha1.DatastoreSpec{SQL: sql("temporal")},
			VisibilityStore: temporalv1alpha1.DatastoreSpec{SQL: sql("temporal_visibility")},
		},
	}
}

var _ = Describe("TemporalCluster CRD validation", func() {
	ctx := context.Background()

	It("rejects setting both sql and cassandra on a store (CEL)", func() {
		resource := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "cel-both-stores", Namespace: "default"},
			Spec:       validClusterSpec("1.31.1"),
		}
		resource.Spec.Persistence.DefaultStore.Cassandra = &temporalv1alpha1.CassandraDatastoreSpec{
			Hosts:    []string{"cassandra.default.svc"},
			Port:     9042,
			Keyspace: "temporal",
		}
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects an invalid version pattern", func() {
		resource := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "bad-version", Namespace: "default"},
			Spec:       validClusterSpec("not-a-version"),
		}
		Expect(k8sClient.Create(ctx, resource)).NotTo(Succeed())
	})

	It("rejects changing the immutable numHistoryShards (CEL transition rule)", func() {
		name := "immutable-shards"
		resource := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       validClusterSpec("1.31.1"),
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, resource)
		})

		fetched := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, fetched)).To(Succeed())
		fetched.Spec.NumHistoryShards = 1024
		Expect(k8sClient.Update(ctx, fetched)).NotTo(Succeed())
	})

	It("accepts scalar dynamicConfig values (bool, number, string)", func() {
		resource := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "scalar-dynamic-config", Namespace: "default"},
			Spec:       validClusterSpec("1.31.1"),
		}
		resource.Spec.DynamicConfig = &temporalv1alpha1.DynamicConfigSpec{
			Values: map[string][]temporalv1alpha1.DynamicConfigValue{
				"history.enableTransitionHistory": {{Value: apiextensionsv1.JSON{Raw: []byte("true")}}},
				"history.enableChasm":             {{Value: apiextensionsv1.JSON{Raw: []byte("false")}}},
				"activity.enableStandalone":       {{Value: apiextensionsv1.JSON{Raw: []byte("true")}}},
				"limit.maxIDLength":               {{Value: apiextensionsv1.JSON{Raw: []byte("3000")}}},
				"system.someString":               {{Value: apiextensionsv1.JSON{Raw: []byte(`"hello"`)}}},
			},
		}
		Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		DeferCleanup(func() {
			_ = k8sClient.Delete(ctx, resource)
		})
	})
})
