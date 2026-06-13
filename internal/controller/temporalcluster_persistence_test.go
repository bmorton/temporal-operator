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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/resources"
)

var _ = Describe("TemporalCluster persistence reconciler", func() {
	ctx := context.Background()
	var counter int

	newCluster := func() *temporalv1alpha1.TemporalCluster {
		counter++
		name := fmt.Sprintf("pg-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec:       validClusterSpec("1.31.2"),
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		return c
	}

	BeforeEach(func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "temporal-store", Namespace: "default"},
			Data:       map[string][]byte{"password": []byte("pw")},
		}
		_ = k8sClient.Create(ctx, secret)
	})

	reconcileWith := func(c *temporalv1alpha1.TemporalCluster, probeErr error, versions map[string]string) {
		r := &TemporalClusterReconciler{
			Client:         k8sClient,
			Scheme:         k8sClient.Scheme(),
			BackendFactory: fakeBackendFactory(probeErr, versions),
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: c.Name, Namespace: c.Namespace}})
		Expect(err).NotTo(HaveOccurred())
	}

	conditionStatus := func(name, condType string) *metav1.Condition {
		c := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, c)).To(Succeed())
		return meta.FindStatusCondition(c.Status.Conditions, condType)
	}

	It("marks persistence unreachable when the probe fails", func() {
		c := newCluster()
		reconcileWith(c, fmt.Errorf("connection refused"), nil)

		cond := conditionStatus(c.Name, temporalv1alpha1.ConditionPersistenceReachable)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(temporalv1alpha1.ReasonPersistenceUnreachable))
	})

	It("creates setup jobs and reports schema migrating on a fresh database", func() {
		c := newCluster()
		reconcileWith(c, nil, map[string]string{})

		By("setting PersistenceReachable=True")
		Expect(conditionStatus(c.Name, temporalv1alpha1.ConditionPersistenceReachable).Status).To(Equal(metav1.ConditionTrue))

		By("creating the default setup-schema job with the admin-tools image")
		job := &batchv1.Job{}
		jobName := resources.SchemaJobName(c.Name, resources.StoreDefault, resources.ActionSetup)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, job)).To(Succeed())
		Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("temporalio/admin-tools:1.31.2"))
		Expect(job.Spec.Template.Spec.Containers[0].Args).To(ContainElement("setup-schema"))
		Expect(job.OwnerReferences).NotTo(BeEmpty())

		By("reporting SchemaReady=False/SchemaMigrating")
		cond := conditionStatus(c.Name, temporalv1alpha1.ConditionSchemaReady)
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(temporalv1alpha1.ReasonSchemaMigrating))
	})

	It("reports SchemaReady=True when the schema already satisfies the minimum", func() {
		c := newCluster()
		reconcileWith(c, nil, map[string]string{
			"temporal":            "1.12",
			"temporal_visibility": "1.12",
		})

		cond := conditionStatus(c.Name, temporalv1alpha1.ConditionSchemaReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))

		By("not creating any schema jobs")
		jobName := resources.SchemaJobName(c.Name, resources.StoreDefault, resources.ActionSetup)
		err := k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &batchv1.Job{})
		Expect(err).To(HaveOccurred())
	})

	It("reports SchemaReady=False when a schema job fails", func() {
		c := newCluster()
		// First pass creates the setup job.
		reconcileWith(c, nil, map[string]string{})

		jobName := resources.SchemaJobName(c.Name, resources.StoreDefault, resources.ActionSetup)
		job := &batchv1.Job{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, job)).To(Succeed())

		By("marking the setup job as failed")
		now := metav1.Now()
		job.Status.StartTime = &now
		job.Status.Conditions = []batchv1.JobCondition{
			{Type: batchv1.JobFailureTarget, Status: corev1.ConditionTrue},
			{Type: batchv1.JobFailed, Status: corev1.ConditionTrue},
		}
		Expect(k8sClient.Status().Update(ctx, job)).To(Succeed())

		reconcileWith(c, nil, map[string]string{})

		cond := conditionStatus(c.Name, temporalv1alpha1.ConditionSchemaReady)
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("SchemaMigrationFailed"))
	})
})

var _ = Describe("TemporalCluster Cassandra and Elasticsearch backends", func() {
	ctx := context.Background()
	var counter int

	cassandraESCluster := func() *temporalv1alpha1.TemporalCluster {
		counter++
		name := fmt.Sprintf("cass-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalClusterSpec{
				Version:          "1.31.2",
				NumHistoryShards: 512,
				Persistence: temporalv1alpha1.PersistenceSpec{
					DefaultStore: temporalv1alpha1.DatastoreSpec{
						Cassandra: &temporalv1alpha1.CassandraDatastoreSpec{
							Hosts:    []string{"cass-0.cass", "cass-1.cass"},
							Port:     9042,
							Keyspace: "temporal",
							User:     "temporal",
						},
					},
					VisibilityStore: temporalv1alpha1.DatastoreSpec{
						Elasticsearch: &temporalv1alpha1.ElasticsearchDatastoreSpec{
							URL:     "elasticsearch.default.svc:9200",
							Version: "v8",
						},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		return c
	}

	reconcileWith := func(c *temporalv1alpha1.TemporalCluster, versions map[string]string) {
		r := &TemporalClusterReconciler{
			Client:         k8sClient,
			Scheme:         k8sClient.Scheme(),
			BackendFactory: fakeBackendFactory(nil, versions),
		}
		_, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: c.Name, Namespace: "default"}})
		Expect(err).NotTo(HaveOccurred())
	}

	It("creates a Cassandra setup-schema job using temporal-cassandra-tool", func() {
		c := cassandraESCluster()
		reconcileWith(c, map[string]string{}) // empty -> fresh schema

		job := &batchv1.Job{}
		jobName := resources.SchemaJobName(c.Name, resources.StoreDefault, resources.ActionSetup)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, job)).To(Succeed())
		Expect(job.Spec.Template.Spec.Containers[0].Command).To(ContainElement("temporal-cassandra-tool"))
		Expect(job.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--keyspace"))
	})

	It("applies the Elasticsearch visibility schema inline without a Job", func() {
		c := cassandraESCluster()
		// Mark the cassandra default store satisfied so only the ES path is exercised.
		reconcileWith(c, map[string]string{"temporal": "1.9"})

		By("not creating a visibility schema Job for Elasticsearch")
		jobName := resources.SchemaJobName(c.Name, resources.StoreVisibility, resources.ActionSetup)
		err := k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, &batchv1.Job{})
		Expect(err).To(HaveOccurred())

		By("reporting SchemaReady=True once ES applies inline and cassandra is satisfied")
		got := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: c.Name, Namespace: "default"}, got)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, temporalv1alpha1.ConditionSchemaReady)).To(BeTrue())
	})
})
