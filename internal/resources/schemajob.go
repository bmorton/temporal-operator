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

// Package resources contains builders for the Kubernetes objects the operator
// manages (Jobs, Deployments, Services, ConfigMaps, etc.).
package resources

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/temporal"
)

// SchemaStore identifies which logical store a schema Job targets.
type SchemaStore string

const (
	// StoreDefault is the default (history/execution) store.
	StoreDefault SchemaStore = "default"
	// StoreVisibility is the visibility store.
	StoreVisibility SchemaStore = "visibility"
)

// SchemaAction identifies the schema operation a Job performs.
type SchemaAction string

const (
	// ActionSetup creates the schema_version bookkeeping (setup-schema -v 0.0).
	ActionSetup SchemaAction = "setup"
	// ActionUpdate applies versioned migrations (update-schema -d <dir>).
	ActionUpdate SchemaAction = "update"
)

const (
	schemaJobBackoffLimit   int32 = 3
	schemaJobTTLAfterFinish int32 = 600

	// passwordEnvVar is the env var temporal-sql-tool reads the password from.
	passwordEnvVar = "SQL_PASSWORD"
)

// SchemaJobParams describes a single schema Job to build.
type SchemaJobParams struct {
	// Cluster is the owning TemporalCluster.
	Cluster *temporalv1alpha1.TemporalCluster
	// SQLSpec is the resolved SQL datastore spec for the target store.
	SQLSpec *temporalv1alpha1.SQLDatastoreSpec
	// Store and Action select the operation.
	Store  SchemaStore
	Action SchemaAction
	// SchemaVersionDir is the on-image schema version directory, e.g. "v12".
	SchemaVersionDir string
}

// SchemaJobName returns the deterministic name for a schema Job.
func SchemaJobName(clusterName string, store SchemaStore, action SchemaAction) string {
	return fmt.Sprintf("%s-schema-%s-%s", clusterName, store, action)
}

func schemaDir(version string, store SchemaStore) string {
	sub := "temporal"
	if store == StoreVisibility {
		sub = "visibility"
	}
	return fmt.Sprintf("/etc/temporal/schema/postgresql/%s/%s/versioned", version, sub)
}

func schemaToolArgs(p SchemaJobParams) []string {
	spec := p.SQLSpec
	plugin := spec.PluginName
	if plugin == "" {
		plugin = "postgres12"
	}
	args := []string{
		"--plugin", plugin,
		"--endpoint", spec.Host,
		"--port", fmt.Sprintf("%d", spec.Port),
		"--user", spec.User,
		"--database", databaseName(p),
	}
	if spec.TLS != nil && spec.TLS.Enabled {
		args = append(args, "--tls")
		if !spec.TLS.EnableHostVerification {
			args = append(args, "--tls-disable-host-verification")
		}
	}
	switch p.Action {
	case ActionSetup:
		args = append(args, "setup-schema", "-v", "0.0")
	case ActionUpdate:
		args = append(args, "update-schema", "-d", schemaDir(p.SchemaVersionDir, p.Store))
	}
	return args
}

func databaseName(p SchemaJobParams) string {
	return p.SQLSpec.Database
}

func passwordEnv(spec *temporalv1alpha1.SQLDatastoreSpec) []corev1.EnvVar {
	if spec.PasswordSecretRef == nil {
		return nil
	}
	key := spec.PasswordSecretRef.Key
	if key == "" {
		key = "password"
	}
	return []corev1.EnvVar{
		{
			Name: passwordEnvVar,
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: spec.PasswordSecretRef.Name},
					Key:                  key,
				},
			},
		},
	}
}

// BuildSchemaJob builds a batch/v1 Job that runs temporal-sql-tool for the given
// store and action. The caller is responsible for setting the controller owner
// reference on the returned Job.
func BuildSchemaJob(p SchemaJobParams) *batchv1.Job {
	name := SchemaJobName(p.Cluster.Name, p.Store, p.Action)
	backoff := schemaJobBackoffLimit
	ttl := schemaJobTTLAfterFinish

	labels := map[string]string{
		"app.kubernetes.io/name":       "temporal",
		"app.kubernetes.io/instance":   p.Cluster.Name,
		"app.kubernetes.io/component":  "schema",
		"app.kubernetes.io/managed-by": "temporal-operator",
		"temporal.bmor10.com/store":    string(p.Store),
		"temporal.bmor10.com/action":   string(p.Action),
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: p.Cluster.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					RestartPolicy:    corev1.RestartPolicyNever,
					ImagePullSecrets: p.Cluster.Spec.ImagePullSecrets,
					Containers: []corev1.Container{
						{
							Name:    "schema",
							Image:   temporal.AdminToolsImage(p.Cluster.Spec.Version),
							Command: []string{"temporal-sql-tool"},
							Args:    schemaToolArgs(p),
							Env:     passwordEnv(p.SQLSpec),
						},
					},
				},
			},
		},
	}
}
