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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/metrics"
	"github.com/bmorton/temporal-operator/internal/persistence"
	"github.com/bmorton/temporal-operator/internal/recovery"
	"github.com/bmorton/temporal-operator/internal/resources"
	"github.com/bmorton/temporal-operator/internal/status"
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
	storeResults := make([]storeResult, 0, len(targets))
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
		storeResults = append(storeResults, res)
		switch {
		case res.failed:
			r.setSchemaReady(cluster, metav1.ConditionFalse, temporalv1alpha1.ReasonSchemaMigrationFailed, res.message)
			return ctrl.Result{}, nil
		case !res.done:
			migrating = true
		}
	}

	if migrating {
		requeue := minRequeue(storeResults...)
		if requeue == 0 {
			// Normal migration in progress (no retry-after-failure pending).
			r.setSchemaReady(cluster, metav1.ConditionFalse, temporalv1alpha1.ReasonSchemaMigrating, "schema migration in progress")
			requeue = persistenceRequeueAfter
		}
		// When requeue > 0, handleFailedSchemaJob already set SchemaMigrationRetrying.
		return ctrl.Result{RequeueAfter: requeue}, nil
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
	done         bool
	failed       bool
	message      string
	requeueAfter time.Duration
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
			return r.handleFailedSchemaJob(ctx, cluster, t, resources.ActionSetup)
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
		return r.handleFailedSchemaJob(ctx, cluster, t, resources.ActionUpdate)
	}
	if update == jobSucceeded {
		resetSchemaAttempts(cluster, t.store)
		// The setup/update Jobs have finished, so the schema version is now current.
		// The schema version we read this pass came from an inspector Job that ran
		// before migration and reports the old (often empty) version. Delete that
		// stale inspector Job so the next reconcile re-probes the updated version
		// immediately, instead of waiting out the inspector Job's TTL.
		if err := r.deleteInspectorJob(ctx, cluster, t.store); err != nil {
			return storeResult{}, err
		}
	}
	return storeResult{}, nil
}

// handleFailedSchemaJob applies the bounded recreation policy to a schema Job
// that has exhausted its own BackoffLimit.
//
// The Job's BackoffLimit retries the pod within seconds, which does not cover
// the most common real failure: the Job was created while the database was
// still starting. Recreating the Job at minute-scale intervals covers that,
// while the attempt budget stops a genuinely broken migration from retrying
// forever.
func (r *TemporalClusterReconciler) handleFailedSchemaJob(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, t schemaTarget, action resources.SchemaAction) (storeResult, error) {
	key := string(t.store)
	if cluster.Status.Persistence.SchemaAttempts == nil {
		cluster.Status.Persistence.SchemaAttempts = map[string]temporalv1alpha1.SchemaAttemptStatus{}
	}
	attempt := cluster.Status.Persistence.SchemaAttempts[key]

	name := resources.SchemaJobName(cluster.Name, t.store, action)
	detail := r.schemaJobFailureDetail(ctx, cluster, name)

	decision := recovery.SchemaJobPolicy.Next(int(attempt.Count))
	if !decision.Retry {
		message := fmt.Sprintf("%s %s-schema job failed %d times and will not be retried: %s. The failed Job %q is retained; inspect its pod logs with: kubectl -n %s logs job/%s",
			t.store, action, attempt.Count, detail, name, cluster.Namespace, name)
		status.Set(cluster, temporalv1alpha1.ConditionSchemaReady, metav1.ConditionFalse,
			temporalv1alpha1.ReasonSchemaMigrationFailed, message)
		status.Set(cluster, temporalv1alpha1.ConditionDegraded, metav1.ConditionTrue,
			temporalv1alpha1.ReasonSchemaMigrationFailed, message)
		r.warnEvent(cluster, temporalv1alpha1.ReasonSchemaMigrationFailed, message)
		return storeResult{failed: true, message: message}, nil
	}

	now := metav1.Now()
	if attempt.FirstFailedAt == nil {
		attempt.FirstFailedAt = &now
	}
	attempt.Count++
	attempt.LastError = detail
	cluster.Status.Persistence.SchemaAttempts[key] = attempt
	metrics.SchemaJobAttempts.
		WithLabelValues(cluster.Namespace, cluster.Name, string(t.store)).
		Set(float64(attempt.Count))

	// Delete the failed Job so the next reconcile recreates it from scratch.
	// Safe to re-run: the schema tools are invoked without --overwrite.
	job := &batchv1.Job{}
	job.Name = name
	job.Namespace = cluster.Namespace
	policy := metav1.DeletePropagationBackground
	if err := r.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &policy}); err != nil && !apierrors.IsNotFound(err) {
		return storeResult{}, fmt.Errorf("deleting failed %s job: %w", action, err)
	}

	message := fmt.Sprintf("%s %s-schema job failed (attempt %d of %d): %s; retrying in %s",
		t.store, action, attempt.Count, len(recovery.SchemaJobPolicy.Delays), detail, decision.After)
	status.Set(cluster, temporalv1alpha1.ConditionSchemaReady, metav1.ConditionFalse,
		temporalv1alpha1.ReasonSchemaMigrationRetrying, message)
	r.warnEvent(cluster, temporalv1alpha1.ReasonSchemaMigrationRetrying, message)

	return storeResult{requeueAfter: decision.After}, nil
}

// schemaJobFailureDetail reports the Job's own failure reason, so the condition
// message names the cause rather than just the fact of failure.
func (r *TemporalClusterReconciler) schemaJobFailureDetail(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, name string) string {
	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, &job); err != nil {
		return "job not found"
	}
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return fmt.Sprintf("%s: %s", c.Reason, c.Message)
		}
	}
	return fmt.Sprintf("%d failed pods", job.Status.Failed)
}

// resetSchemaAttempts clears the failure record for a store after a successful
// migration, so a later unrelated failure gets a full retry budget.
func resetSchemaAttempts(cluster *temporalv1alpha1.TemporalCluster, store resources.SchemaStore) {
	delete(cluster.Status.Persistence.SchemaAttempts, string(store))
	metrics.SchemaJobAttempts.DeleteLabelValues(cluster.Namespace, cluster.Name, string(store))
}

// minRequeue returns the soonest non-zero requeue among results, or zero if none.
func minRequeue(results ...storeResult) time.Duration {
	var out time.Duration
	for _, res := range results {
		if res.requeueAfter == 0 {
			continue
		}
		if out == 0 || res.requeueAfter < out {
			out = res.requeueAfter
		}
	}
	return out
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

// deleteInspectorJob deletes the inspector Job for a store if it exists, so a
// subsequent probe creates a fresh one that reflects the current schema version.
// A missing Job is not an error.
func (r *TemporalClusterReconciler) deleteInspectorJob(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, store resources.SchemaStore) error {
	name := resources.InspectorJobName(cluster.Name, store)
	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, &job); err != nil {
		return client.IgnoreNotFound(err)
	}
	policy := metav1.DeletePropagationBackground
	if err := r.Delete(ctx, &job, &client.DeleteOptions{PropagationPolicy: &policy}); err != nil {
		return client.IgnoreNotFound(err)
	}
	return nil
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
	condStatus := metav1.ConditionTrue
	reason := "Reachable"
	if !reachable {
		condStatus = metav1.ConditionFalse
		reason = temporalv1alpha1.ReasonPersistenceUnreachable
	}
	cluster.Status.Persistence.Reachable = reachable
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionPersistenceReachable,
		Status:             condStatus,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: cluster.Generation,
	})
}

func (r *TemporalClusterReconciler) setSchemaReady(cluster *temporalv1alpha1.TemporalCluster, condStatus metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
		Type:               temporalv1alpha1.ConditionSchemaReady,
		Status:             condStatus,
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
