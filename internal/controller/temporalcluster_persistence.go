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
	"errors"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/persistence"
	"github.com/bmorton/temporal-operator/internal/resources"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// persistenceRequeueAfter is how long to wait before re-probing an unreachable
// or migrating datastore.
const persistenceRequeueAfter = 30 * time.Second

// Datastore backend kinds.
const (
	kindCassandra     = "cassandra"
	kindElasticsearch = "elasticsearch"
)

// backendFactory returns the configured datastore backend factory, defaulting to
// the real implementation.
func (r *TemporalClusterReconciler) backendFactory() persistence.BackendFactory {
	if r.BackendFactory != nil {
		return r.BackendFactory
	}
	return persistence.DefaultBackendFactory
}

func storeDBName(store temporalv1alpha1.DatastoreSpec) string {
	switch {
	case store.SQL != nil:
		return store.SQL.Database
	case store.Cassandra != nil:
		return store.Cassandra.Keyspace
	default:
		return ""
	}
}

// minSchemaFor returns the required minimum schema version for a store given the
// backend kind.
func minSchemaFor(info *temporal.VersionInfo, kind string) string {
	switch kind {
	case kindCassandra:
		return info.MinSchemaCassandra
	case kindElasticsearch:
		return info.MinSchemaES
	default:
		return info.MinSchemaSQL
	}
}

type schemaTarget struct {
	store   resources.SchemaStore
	spec    temporalv1alpha1.DatastoreSpec
	backend persistence.Backend
	cred    persistence.ResolvedCredential
}

// reconcilePersistence probes the datastore(s) and drives schema setup/migration
// via Jobs (SQL, Cassandra) or inline (Elasticsearch).
// ensureAzureServiceAccount creates the ServiceAccount for Azure Workload Identity if enabled.
func (r *TemporalClusterReconciler) ensureAzureServiceAccount(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster) error {
	if !resources.AzureWorkloadIdentityEnabled(cluster) {
		return nil
	}
	sa := resources.BuildAzureServiceAccount(cluster)
	if err := controllerutil.SetControllerReference(cluster, sa, r.Scheme); err != nil {
		return err
	}
	if err := r.Create(ctx, sa); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (r *TemporalClusterReconciler) reconcilePersistence(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	factory := r.backendFactory()
	resolver := persistence.NewSecretResolver(r.Client, cluster.Namespace)

	// Ensure the Azure WI ServiceAccount exists when WI is enabled.
	if err := r.ensureAzureServiceAccount(ctx, cluster); err != nil {
		return ctrl.Result{}, err
	}

	targets, err := r.buildSchemaTargets(ctx, cluster, factory, resolver)
	if err != nil {
		r.setReachable(cluster, false, err.Error())
		return ctrl.Result{RequeueAfter: persistenceRequeueAfter}, nil
	}

	for _, t := range targets {
		if err := t.backend.Probe(ctx); err != nil {
			// ErrInspecting is transient (Job not yet finished); requeue silently.
			if errors.Is(err, persistence.ErrInspecting) {
				r.setInspecting(cluster, fmt.Sprintf("%s store: inspection in progress", t.store))
				return ctrl.Result{RequeueAfter: persistenceRequeueAfter}, nil
			}
			log.Info("persistence unreachable", "store", t.store, "error", err.Error())
			r.setReachable(cluster, false, fmt.Sprintf("%s store: %v", t.store, err))
			return ctrl.Result{RequeueAfter: persistenceRequeueAfter}, nil
		}
	}
	r.setReachable(cluster, true, "datastore is reachable")
	cluster.Status.Persistence.Reachable = true

	info, err := temporal.LookupVersion(cluster.Spec.Version)
	if err != nil {
		return ctrl.Result{}, err
	}
	if cluster.Status.Persistence.SchemaVersions == nil {
		cluster.Status.Persistence.SchemaVersions = map[string]string{}
	}

	migrating := false
	for _, t := range targets {
		res, err := r.reconcileStoreSchema(ctx, cluster, t, minSchemaFor(info, t.backend.Kind()))
		if err != nil {
			// ErrInspecting during schema version lookup is transient; requeue.
			if errors.Is(err, persistence.ErrInspecting) {
				migrating = true
				continue
			}
			return ctrl.Result{}, err
		}
		switch {
		case res.failed:
			r.setSchemaReady(cluster, metav1.ConditionFalse, "SchemaMigrationFailed", res.message)
			return ctrl.Result{}, nil
		case !res.done:
			migrating = true
		}
	}

	if migrating {
		r.setSchemaReady(cluster, metav1.ConditionFalse, temporalv1alpha1.ReasonSchemaMigrating, "schema migration in progress")
		return ctrl.Result{RequeueAfter: persistenceRequeueAfter}, nil
	}

	r.setSchemaReady(cluster, metav1.ConditionTrue, "SchemaReady", "all schemas are at the required version")
	return ctrl.Result{}, nil
}

func (r *TemporalClusterReconciler) buildSchemaTargets(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, factory persistence.BackendFactory, resolver *persistence.SecretResolver) ([]schemaTarget, error) {
	build := func(store temporalv1alpha1.DatastoreSpec, name resources.SchemaStore) (schemaTarget, error) {
		cred, err := resolver.ResolveStore(ctx, store)
		if err != nil {
			return schemaTarget{}, fmt.Errorf("resolving %s store credential: %w", name, err)
		}

		var backend persistence.Backend
		// When Azure Workload Identity is enabled for a SQL store, use the
		// Job-based inspector backend instead of a direct connection.
		if resources.AzureWorkloadIdentityEnabled(cluster) && store.SQL != nil {
			backend = persistence.NewJobInspectorBackend(r.Client, storeDBName(store), func(ctx context.Context) (*batchv1.Job, error) {
				return r.ensureInspectorJob(ctx, cluster, store.SQL, name)
			})
		} else {
			backend, err = factory(store, cred, storeDBName(store))
			if err != nil {
				return schemaTarget{}, fmt.Errorf("building %s backend: %w", name, err)
			}
		}
		return schemaTarget{store: name, spec: store, backend: backend, cred: cred}, nil
	}

	defTarget, err := build(cluster.Spec.Persistence.DefaultStore, resources.StoreDefault)
	if err != nil {
		return nil, err
	}
	visTarget, err := build(cluster.Spec.Persistence.VisibilityStore, resources.StoreVisibility)
	if err != nil {
		return nil, err
	}
	return []schemaTarget{defTarget, visTarget}, nil
}

type storeResult struct {
	done    bool
	failed  bool
	message string
}

// reconcileStoreSchema ensures a single store's schema reaches minSchema.
func (r *TemporalClusterReconciler) reconcileStoreSchema(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, t schemaTarget, minSchema string) (storeResult, error) {
	current, err := t.backend.SchemaVersion(ctx)
	if err != nil {
		return storeResult{}, fmt.Errorf("inspecting %s schema: %w", t.store, err)
	}
	cluster.Status.Persistence.SchemaVersions[string(t.store)] = current

	if persistence.SchemaSatisfies(current, minSchema) {
		return storeResult{done: true}, nil
	}

	// Elasticsearch manages schema inline (index templates) rather than via Jobs.
	if inline, err := t.backend.EnsureSchema(ctx, minSchema); err != nil {
		return storeResult{}, fmt.Errorf("applying %s schema: %w", t.store, err)
	} else if inline {
		current, err = t.backend.SchemaVersion(ctx)
		if err != nil {
			return storeResult{}, err
		}
		cluster.Status.Persistence.SchemaVersions[string(t.store)] = current
		return storeResult{done: persistence.SchemaSatisfies(current, minSchema)}, nil
	}

	return r.reconcileJobSchema(ctx, cluster, t, current)
}

// reconcileJobSchema runs setup/update Jobs for SQL and Cassandra stores.
func (r *TemporalClusterReconciler) reconcileJobSchema(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, t schemaTarget, current string) (storeResult, error) {
	if current == "" {
		setup, err := r.ensureSchemaJob(ctx, cluster, t, resources.ActionSetup)
		if err != nil {
			return storeResult{}, err
		}
		if setup == jobFailed {
			return storeResult{failed: true, message: fmt.Sprintf("%s setup-schema job failed", t.store)}, nil
		}
		if setup != jobSucceeded {
			return storeResult{}, nil
		}
	}

	update, err := r.ensureSchemaJob(ctx, cluster, t, resources.ActionUpdate)
	if err != nil {
		return storeResult{}, err
	}
	if update == jobFailed {
		return storeResult{failed: true, message: fmt.Sprintf("%s update-schema job failed", t.store)}, nil
	}
	return storeResult{}, nil
}

type jobPhase int

const (
	jobPending jobPhase = iota
	jobRunning
	jobSucceeded
	jobFailed
)

// ensureSchemaJob creates the schema Job if absent and reports its phase.
func (r *TemporalClusterReconciler) ensureSchemaJob(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, t schemaTarget, action resources.SchemaAction) (jobPhase, error) {
	name := resources.SchemaJobName(cluster.Name, t.store, action)
	var job batchv1.Job
	err := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, &job)
	if apierrors.IsNotFound(err) {
		// Determine password command and pod template based on Azure WI setting.
		passwordCommand := t.cred.PasswordCommand
		var podTemplate *temporalv1alpha1.PodTemplateOverride
		if resources.AzureWorkloadIdentityEnabled(cluster) {
			// Azure WI uses the password command that reads from the token file.
			passwordCommand = resources.AzurePasswordCommand()
			// Do NOT use user-provided podTemplate when Azure WI is enabled;
			// Azure generates the necessary wiring.
		} else {
			podTemplate = schemaJobPodTemplate(cluster)
		}

		built, buildErr := resources.BuildSchemaJob(resources.SchemaJobParams{
			Cluster:          cluster,
			SQLSpec:          t.spec.SQL,
			CassandraSpec:    t.spec.Cassandra,
			Store:            t.store,
			Action:           action,
			SchemaVersionDir: resources.PostgresSchemaDir,
			PasswordCommand:  passwordCommand,
			PodTemplate:      podTemplate,
		})
		if buildErr != nil {
			return jobPending, buildErr
		}

		// Apply Azure Workload Identity wiring to the schema Job when enabled.
		if resources.AzureWorkloadIdentityEnabled(cluster) {
			resources.ApplyAzureSchemaWorkloadIdentity(
				&built.Spec.Template.ObjectMeta,
				&built.Spec.Template.Spec,
				cluster,
				"schema",
			)
		}

		if err := controllerutil.SetControllerReference(cluster, built, r.Scheme); err != nil {
			return jobPending, err
		}
		if err := r.Create(ctx, built); err != nil && !apierrors.IsAlreadyExists(err) {
			return jobPending, err
		}
		return jobPending, nil
	}
	if err != nil {
		return jobPending, err
	}
	return classifyJob(&job), nil
}

// ensureInspectorJob creates the inspector Job if absent and returns it.
func (r *TemporalClusterReconciler) ensureInspectorJob(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, sqlSpec *temporalv1alpha1.SQLDatastoreSpec, store resources.SchemaStore) (*batchv1.Job, error) {
	name := resources.InspectorJobName(cluster.Name, store)
	var job batchv1.Job
	err := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, &job)
	if apierrors.IsNotFound(err) {
		built := resources.BuildInspectorJob(resources.InspectorJobParams{
			Cluster:       cluster,
			Store:         store,
			SQLSpec:       sqlSpec,
			OperatorImage: r.OperatorImage,
		})
		if err := controllerutil.SetControllerReference(cluster, built, r.Scheme); err != nil {
			return nil, err
		}
		if err := r.Create(ctx, built); err != nil && !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
		return built, nil
	}
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// schemaJobPodTemplate returns the configured schema Job podTemplate override,
// or nil when none is set.
func schemaJobPodTemplate(cluster *temporalv1alpha1.TemporalCluster) *temporalv1alpha1.PodTemplateOverride {
	if cluster.Spec.Persistence.SchemaJob == nil {
		return nil
	}
	return cluster.Spec.Persistence.SchemaJob.PodTemplate
}

func classifyJob(job *batchv1.Job) jobPhase {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == "True" {
			return jobSucceeded
		}
		if c.Type == batchv1.JobFailed && c.Status == "True" {
			return jobFailed
		}
	}
	if job.Status.Active > 0 {
		return jobRunning
	}
	return jobPending
}

func (r *TemporalClusterReconciler) setReachable(cluster *temporalv1alpha1.TemporalCluster, reachable bool, message string) {
	status := metav1.ConditionTrue
	reason := "Reachable"
	if !reachable {
		status = metav1.ConditionFalse
		reason = temporalv1alpha1.ReasonPersistenceUnreachable
	}
	cluster.Status.Persistence.Reachable = reachable
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionPersistenceReachable,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cluster.Generation,
	})
}

func (r *TemporalClusterReconciler) setSchemaReady(cluster *temporalv1alpha1.TemporalCluster, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionSchemaReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cluster.Generation,
	})
}

// setInspecting sets the PersistenceReachable condition to False with reason Inspecting.
func (r *TemporalClusterReconciler) setInspecting(cluster *temporalv1alpha1.TemporalCluster, message string) {
	cluster.Status.Persistence.Reachable = false
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionPersistenceReachable,
		Status:             metav1.ConditionFalse,
		Reason:             temporalv1alpha1.ReasonInspecting,
		Message:            message,
		ObservedGeneration: cluster.Generation,
	})
}
