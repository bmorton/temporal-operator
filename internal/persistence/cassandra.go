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

package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gocql/gocql"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// cassandraBackend probes and inspects a Cassandra datastore via gocql.
type cassandraBackend struct {
	spec *temporalv1alpha1.CassandraDatastoreSpec
	cred ResolvedCredential
}

func (b *cassandraBackend) clusterConfig(keyspace string) *gocql.ClusterConfig {
	c := gocql.NewCluster(b.spec.Hosts...)
	c.Port = int(b.spec.Port)
	c.Keyspace = keyspace
	c.Timeout = 5 * time.Second
	c.ConnectTimeout = 5 * time.Second
	c.ProtoVersion = 4
	if b.spec.User != "" {
		c.Authenticator = gocql.PasswordAuthenticator{Username: b.spec.User, Password: b.cred.Password}
	}
	if b.spec.Datacenter != "" {
		c.PoolConfig.HostSelectionPolicy = gocql.DCAwareRoundRobinPolicy(b.spec.Datacenter)
	}
	return c
}

func (b *cassandraBackend) Probe(ctx context.Context) error {
	session, err := b.clusterConfig("system").CreateSession()
	if err != nil {
		return fmt.Errorf("connecting to cassandra: %w", err)
	}
	defer session.Close()
	if err := session.Query("SELECT now() FROM system.local").WithContext(ctx).Exec(); err != nil {
		return fmt.Errorf("cassandra liveness query: %w", err)
	}
	return nil
}

func (b *cassandraBackend) SchemaVersion(ctx context.Context) (string, error) {
	// The keyspace may not exist yet on a fresh cluster; treat any connection or
	// query failure as "needs bootstrap" rather than a hard error.
	session, err := b.clusterConfig(b.spec.Keyspace).CreateSession()
	if err != nil {
		return "", nil
	}
	defer session.Close()

	var version string
	err = session.Query(
		"SELECT curr_version FROM schema_version WHERE version_partition = ? AND db_name = ?",
		0, b.spec.Keyspace,
	).WithContext(ctx).Scan(&version)
	switch {
	case errors.Is(err, gocql.ErrNotFound):
		return "", nil
	case err != nil:
		// schema_version table missing -> bootstrap required.
		return "", nil
	}
	return version, nil
}

func (b *cassandraBackend) EnsureSchema(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (b *cassandraBackend) Kind() string { return "cassandra" }
