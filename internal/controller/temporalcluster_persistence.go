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

// postgresSchemaDir is the on-image schema directory for the postgres12 plugin.
const postgresSchemaDir = "v12"

// reconcilePersistence probes the datastore and drives schema setup/migration
// via Jobs. It is Postgres-only at this milestone; non-SQL stores are skipped.
func (r *TemporalClusterReconciler) reconcilePersistence(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	defStore := cluster.Spec.Persistence.DefaultStore
	if defStore.SQL == nil {
		// Only SQL (Postgres) persistence is implemented at this milestone.
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:               temporalv1alpha1.ConditionPersistenceReachable,
			Status:             metav1.ConditionUnknown,
			Reason:             temporalv1alpha1.ReasonNotImplemented,
			Message:            "only SQL persistence is implemented",
			ObservedGeneration: cluster.Generation,
		})
		return ctrl.Result{}, nil
	}

	resolver := persistence.NewSecretResolver(r.Client, cluster.Namespace)
	defCred, err := resolver.ResolveSQL(ctx, defStore.SQL)
	if err != nil {
		r.setReachable(cluster, false, fmt.Sprintf("resolving default store password: %v", err))
		return ctrl.Result{RequeueAfter: persistenceRequeueAfter}, nil
	}
	defDSN := persistence.BuildPostgresDSN(defStore.SQL, defCred.Password, defStore.SQL.Database)

	if err := r.prober().Probe(ctx, defDSN); err != nil {
		log.Info("persistence unreachable", "error", err.Error())
		r.setReachable(cluster, false, err.Error())
		return ctrl.Result{RequeueAfter: persistenceRequeueAfter}, nil
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

	targets := []schemaTarget{
		{store: resources.StoreDefault, spec: defStore.SQL, dsn: defDSN},
	}
	if vis := cluster.Spec.Persistence.VisibilityStore; vis.SQL != nil {
		visCred, err := resolver.ResolveSQL(ctx, vis.SQL)
		if err != nil {
			r.setReachable(cluster, false, fmt.Sprintf("resolving visibility store password: %v", err))
			return ctrl.Result{RequeueAfter: persistenceRequeueAfter}, nil
		}
		visDSN := persistence.BuildPostgresDSN(vis.SQL, visCred.Password, vis.SQL.Database)
		targets = append(targets, schemaTarget{store: resources.StoreVisibility, spec: vis.SQL, dsn: visDSN})
	}

	migrating := false
	for _, t := range targets {
		res, err := r.reconcileStoreSchema(ctx, cluster, t, info.MinSchemaSQL)
		if err != nil {
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

type schemaTarget struct {
	store resources.SchemaStore
	spec  *temporalv1alpha1.SQLDatastoreSpec
	dsn   string
}

type storeResult struct {
	done    bool
	failed  bool
	message string
}

// reconcileStoreSchema ensures a single store's schema reaches minSchema by
// running setup and/or update Jobs as needed.
func (r *TemporalClusterReconciler) reconcileStoreSchema(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, t schemaTarget, minSchema string) (storeResult, error) {
	current, err := r.schemaInspector().CurrentSchemaVersion(ctx, t.dsn, t.spec.Database)
	if err != nil {
		return storeResult{}, fmt.Errorf("inspecting %s schema: %w", t.store, err)
	}
	cluster.Status.Persistence.SchemaVersions[string(t.store)] = current

	if persistence.SchemaSatisfies(current, minSchema) {
		return storeResult{done: true}, nil
	}

	// A fresh database needs the schema_version bookkeeping created first.
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
	// Even on success we report not-done until a subsequent inspect confirms the
	// schema satisfies the minimum.
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
		built := resources.BuildSchemaJob(resources.SchemaJobParams{
			Cluster:          cluster,
			SQLSpec:          t.spec,
			Store:            t.store,
			Action:           action,
			SchemaVersionDir: postgresSchemaDir,
		})
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

// prober returns the configured Prober, defaulting to the SQL prober.
func (r *TemporalClusterReconciler) prober() persistence.Prober {
	if r.Prober != nil {
		return r.Prober
	}
	return persistence.SQLProber{}
}

// schemaInspector returns the configured SchemaInspector, defaulting to the SQL prober.
func (r *TemporalClusterReconciler) schemaInspector() persistence.SchemaInspector {
	if r.SchemaInspector != nil {
		return r.SchemaInspector
	}
	return persistence.SQLProber{}
}
