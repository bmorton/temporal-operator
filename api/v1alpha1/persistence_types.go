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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PersistenceSpec configures the default and visibility datastores.
type PersistenceSpec struct {
	// DefaultStore holds workflow execution state. Exactly one of sql or
	// cassandra must be set.
	// +kubebuilder:validation:XValidation:rule="has(self.sql) != has(self.cassandra)",message="exactly one of sql or cassandra must be set for defaultStore"
	DefaultStore DatastoreSpec `json:"defaultStore"`

	// VisibilityStore holds visibility records. One of sql, cassandra, or
	// elasticsearch must be set.
	VisibilityStore DatastoreSpec `json:"visibilityStore"`

	// SchemaJob customizes the schema setup/update Jobs the operator runs.
	// +optional
	SchemaJob *SchemaJobSpec `json:"schemaJob,omitempty"`
}

// SchemaJobSpec customizes the schema management Jobs (setup-schema /
// update-schema) the operator runs against SQL and Cassandra datastores.
type SchemaJobSpec struct {
	// PodTemplate overrides metadata and the pod spec of the schema Job pods.
	// Use it to attach a ServiceAccount, pod labels (e.g. Azure Workload
	// Identity), and a token initContainer so the Job can authenticate with a
	// passwordCommand instead of a static password.
	// +optional
	PodTemplate *PodTemplateOverride `json:"podTemplate,omitempty"`
}

// DatastoreSpec configures a single datastore. Exactly one backend should be set.
type DatastoreSpec struct {
	// +optional
	SQL *SQLDatastoreSpec `json:"sql,omitempty"`
	// +optional
	Cassandra *CassandraDatastoreSpec `json:"cassandra,omitempty"`
	// +optional
	Elasticsearch *ElasticsearchDatastoreSpec `json:"elasticsearch,omitempty"`

	// SchemaVersion is either "auto" (operator-managed migrations) or a pinned
	// schema version string.
	// +kubebuilder:default="auto"
	// +optional
	SchemaVersion string `json:"schemaVersion,omitempty"`
}

// SQLDatastoreSpec configures a SQL (Postgres/MySQL) datastore.
type SQLDatastoreSpec struct {
	// PluginName selects the SQL driver.
	// +kubebuilder:validation:Enum=postgres12;postgres12_pgx;mysql8
	// +kubebuilder:default=postgres12
	PluginName string `json:"pluginName"`

	Host string `json:"host"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=5432
	Port int32 `json:"port"`

	Database string `json:"database"`

	User string `json:"user"`

	// PasswordSecretRef references a secret containing the password. Required
	// for password authentication.
	// +optional
	PasswordSecretRef *SecretKeyReference `json:"passwordSecretRef,omitempty"`

	// PasswordCommandSecretRef references a secret holding a command that emits
	// a short-lived credential (Temporal 1.31+ IAM auth).
	// +optional
	PasswordCommandSecretRef *SecretKeyReference `json:"passwordCommandSecretRef,omitempty"`

	// +optional
	ConnectAttributes map[string]string `json:"connectAttributes,omitempty"`

	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxConns int32 `json:"maxConns,omitempty"`

	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxIdleConns int32 `json:"maxIdleConns,omitempty"`

	// +optional
	MaxConnLifetime *metav1.Duration `json:"maxConnLifetime,omitempty"`

	// +optional
	TLS *DatastoreTLSSpec `json:"tls,omitempty"`
}

// CassandraDatastoreSpec configures a Cassandra datastore.
type CassandraDatastoreSpec struct {
	// +kubebuilder:validation:MinItems=1
	Hosts []string `json:"hosts"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +kubebuilder:default=9042
	Port int32 `json:"port"`

	Keyspace string `json:"keyspace"`

	// +optional
	User string `json:"user,omitempty"`

	// +optional
	PasswordSecretRef *SecretKeyReference `json:"passwordSecretRef,omitempty"`

	// +optional
	Datacenter string `json:"datacenter,omitempty"`

	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicationFactor,omitempty"`

	// +optional
	TLS *DatastoreTLSSpec `json:"tls,omitempty"`
}

// ElasticsearchDatastoreSpec configures an Elasticsearch visibility store.
type ElasticsearchDatastoreSpec struct {
	URL string `json:"url"`

	// +kubebuilder:validation:Enum=v7;v8
	// +kubebuilder:default=v8
	Version string `json:"version"`

	// +optional
	Username string `json:"username,omitempty"`

	// +optional
	PasswordSecretRef *SecretKeyReference `json:"passwordSecretRef,omitempty"`

	// +optional
	Indices map[string]string `json:"indices,omitempty"`

	// +optional
	TLS *DatastoreTLSSpec `json:"tls,omitempty"`
}

// DatastoreTLSSpec configures TLS for a datastore connection.
type DatastoreTLSSpec struct {
	// +kubebuilder:default=true
	Enabled bool `json:"enabled"`

	// +optional
	CASecretRef *SecretKeyReference `json:"caSecretRef,omitempty"`
	// +optional
	CertSecretRef *SecretKeyReference `json:"certSecretRef,omitempty"`
	// +optional
	KeySecretRef *SecretKeyReference `json:"keySecretRef,omitempty"`

	// +optional
	EnableHostVerification bool `json:"enableHostVerification,omitempty"`
	// +optional
	ServerName string `json:"serverName,omitempty"`
}

// SecretKeyReference references a single key within a Secret in the same namespace.
type SecretKeyReference struct {
	Name string `json:"name"`
	// +kubebuilder:default=password
	// +optional
	Key string `json:"key,omitempty"`
}

// PersistenceStatus reports datastore reachability and schema state.
type PersistenceStatus struct {
	// SchemaVersions maps a store name to its observed schema version.
	// +optional
	SchemaVersions map[string]string `json:"schemaVersions,omitempty"`

	// History records schema upgrades applied by the operator.
	// +optional
	History []SchemaUpgradeRecord `json:"history,omitempty"`

	// Reachable indicates whether the datastores were reachable at last reconcile.
	// +optional
	Reachable bool `json:"reachable,omitempty"`
}

// SchemaUpgradeRecord records a single schema migration.
type SchemaUpgradeRecord struct {
	Store       string      `json:"store"`
	FromVersion string      `json:"fromVersion"`
	ToVersion   string      `json:"toVersion"`
	Time        metav1.Time `json:"time"`
}
