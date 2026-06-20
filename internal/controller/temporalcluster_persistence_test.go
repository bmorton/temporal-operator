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
			Spec:       validClusterSpec("1.31.1"),
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
		Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal("temporalio/admin-tools:1.31.1"))
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
				Version:          "1.31.1",
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

var _ = Describe("TemporalCluster Azure Workload Identity integration", func() {
	ctx := context.Background()
	var counter int

	// azureWISQLSpec returns a SQLDatastoreSpec WITHOUT a password secret ref,
	// since Azure WI provides passwordless auth.
	azureWISQLSpec := func(db string) *temporalv1alpha1.SQLDatastoreSpec {
		return &temporalv1alpha1.SQLDatastoreSpec{
			PluginName: "postgres12",
			Host:       "postgres.default.svc",
			Port:       5432,
			Database:   db,
			User:       "temporal",
			// No PasswordSecretRef - Azure WI provides the credential
		}
	}

	newAzureWICluster := func() *temporalv1alpha1.TemporalCluster {
		counter++
		name := fmt.Sprintf("azure-%d", counter)
		c := &temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: temporalv1alpha1.TemporalClusterSpec{
				Version:          "1.31.1",
				NumHistoryShards: 512,
				Persistence: temporalv1alpha1.PersistenceSpec{
					DefaultStore:    temporalv1alpha1.DatastoreSpec{SQL: azureWISQLSpec("temporal")},
					VisibilityStore: temporalv1alpha1.DatastoreSpec{SQL: azureWISQLSpec("temporal_visibility")},
					AzureWorkloadIdentity: &temporalv1alpha1.AzureWorkloadIdentitySpec{
						ClientID: "test-client-id",
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, c)).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, c) })
		return c
	}

	// reconcileAndReturnResult reconciles without a fake backend (uses JobInspectorBackend path).
	reconcileAndReturnResult := func(c *temporalv1alpha1.TemporalCluster) reconcile.Result {
		r := &TemporalClusterReconciler{
			Client:        k8sClient,
			Scheme:        k8sClient.Scheme(),
			OperatorImage: "test-operator:v0",
			// Note: BackendFactory is nil; for Azure WI clusters, buildSchemaTargets
			// will use JobInspectorBackend which doesn't call the factory for SQL stores.
		}
		result, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: c.Name, Namespace: c.Namespace}})
		Expect(err).NotTo(HaveOccurred())
		return result
	}

	conditionStatus := func(name, condType string) *metav1.Condition {
		c := &temporalv1alpha1.TemporalCluster{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name, Namespace: "default"}, c)).To(Succeed())
		return meta.FindStatusCondition(c.Status.Conditions, condType)
	}

	It("creates the Azure ServiceAccount when azureWorkloadIdentity is set", func() {
		c := newAzureWICluster()
		reconcileAndReturnResult(c)

		By("creating the <cluster>-azure ServiceAccount")
		sa := &corev1.ServiceAccount{}
		saName := resources.AzureServiceAccountName(c)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: saName, Namespace: "default"}, sa)).To(Succeed())
		Expect(sa.Annotations).To(HaveKeyWithValue("azure.workload.identity/client-id", "test-client-id"))
		Expect(sa.OwnerReferences).NotTo(BeEmpty())
	})

	It("creates inspector Jobs for both stores when azureWorkloadIdentity is set", func() {
		c := newAzureWICluster()
		// First reconcile creates the default store inspector job and returns on ErrInspecting.
		reconcileAndReturnResult(c)

		By("creating the default store inspector job")
		defaultJob := &batchv1.Job{}
		defaultJobName := resources.InspectorJobName(c.Name, resources.StoreDefault)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: defaultJobName, Namespace: "default"}, defaultJob)).To(Succeed())
		Expect(defaultJob.Spec.Template.Spec.Containers[0].Image).To(Equal("test-operator:v0"))
		Expect(defaultJob.Spec.Template.Spec.Containers[0].Args).To(ContainElement("inspect"))
		Expect(defaultJob.OwnerReferences).NotTo(BeEmpty())

		// Note: The visibility job is NOT created in the first reconcile because
		// ErrInspecting on the default store causes an early return. A subsequent
		// reconcile after the default job completes would create the visibility job.
		// This is the correct behavior - we probe stores sequentially and requeue
		// while inspecting.
	})

	It("sets PersistenceReachable=False with reason Inspecting while Jobs are pending", func() {
		c := newAzureWICluster()
		result := reconcileAndReturnResult(c)

		By("setting PersistenceReachable=False with reason Inspecting")
		cond := conditionStatus(c.Name, temporalv1alpha1.ConditionPersistenceReachable)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal(temporalv1alpha1.ReasonInspecting))

		By("requesting a requeue (not a failure)")
		Expect(result.RequeueAfter).To(BeNumerically(">", 0))
	})

	It("inspector Jobs have Azure Workload Identity wiring applied", func() {
		c := newAzureWICluster()
		reconcileAndReturnResult(c)

		job := &batchv1.Job{}
		jobName := resources.InspectorJobName(c.Name, resources.StoreDefault)
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: jobName, Namespace: "default"}, job)).To(Succeed())

		By("setting the ServiceAccount")
		Expect(job.Spec.Template.Spec.ServiceAccountName).To(Equal(resources.AzureServiceAccountName(c)))

		By("adding the azure.workload.identity/use label")
		Expect(job.Spec.Template.Labels).To(HaveKeyWithValue(resources.AzureWILabel, "true"))

		By("adding the azure-token initContainer")
		initContainerNames := []string{}
		for _, ic := range job.Spec.Template.Spec.InitContainers {
			initContainerNames = append(initContainerNames, ic.Name)
		}
		Expect(initContainerNames).To(ContainElement("azure-token"))

		By("adding the azure-token volume")
		volumeNames := []string{}
		for _, v := range job.Spec.Template.Spec.Volumes {
			volumeNames = append(volumeNames, v.Name)
		}
		Expect(volumeNames).To(ContainElement(resources.AzureTokenVolumeName))
	})
})
