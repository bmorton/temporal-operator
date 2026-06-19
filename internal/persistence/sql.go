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
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	// Register the pgx stdlib driver under the name "pgx".
	_ "github.com/jackc/pgx/v5/stdlib"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// Prober verifies that a datastore is reachable.
type Prober interface {
	// Probe opens a connection described by dsn, performs a trivial liveness
	// query, and returns an error if the datastore is unreachable.
	Probe(ctx context.Context, dsn string) error
}

// SchemaInspector reports the current schema version of a datastore.
type SchemaInspector interface {
	// CurrentSchemaVersion returns the current schema version recorded for the
	// given logical database. An empty string means no schema is present yet
	// (bootstrap required).
	CurrentSchemaVersion(ctx context.Context, dsn, dbName string) (string, error)
}

// SQLProber is the default database/sql-backed Prober and SchemaInspector for
// Postgres.
type SQLProber struct {
	// Timeout bounds each probe/query. Defaults to 5s when zero.
	Timeout time.Duration
}

func (p SQLProber) timeout() time.Duration {
	if p.Timeout == 0 {
		return 5 * time.Second
	}
	return p.Timeout
}

func openPostgres(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	return db, nil
}

// Probe implements Prober for Postgres.
func (p SQLProber) Probe(ctx context.Context, dsn string) error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()

	db, err := openPostgres(dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("pinging database: %w", err)
	}
	var one int
	if err := db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil {
		return fmt.Errorf("running liveness query: %w", err)
	}
	return nil
}

// CurrentSchemaVersion implements SchemaInspector for Postgres. It reads the
// Temporal schema_version table; if that table does not exist, it returns an
// empty version to signal that a bootstrap (setup-schema) is required.
func (p SQLProber) CurrentSchemaVersion(ctx context.Context, dsn, dbName string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout())
	defer cancel()

	db, err := openPostgres(dsn)
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	var version string
	err = db.QueryRowContext(ctx,
		"SELECT curr_version FROM schema_version WHERE db_name = $1", dbName).Scan(&version)
	switch {
	case err == sql.ErrNoRows:
		return "", nil
	case err != nil && isUndefinedTable(err):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("querying schema version: %w", err)
	}
	return version, nil
}

// isUndefinedTable reports whether the error indicates a missing table
// (Postgres SQLSTATE 42P01), which the operator interprets as "needs bootstrap".
func isUndefinedTable(err error) bool {
	return strings.Contains(err.Error(), "42P01") ||
		strings.Contains(strings.ToLower(err.Error()), "does not exist")
}

// BuildPostgresDSN builds a Postgres connection string from a SQL datastore spec
// and a resolved password. The database name can be overridden (e.g. to target
// the visibility database) via dbName; when empty, spec.Database is used.
func BuildPostgresDSN(spec *temporalv1alpha1.SQLDatastoreSpec, password, dbName string) string {
	if dbName == "" {
		dbName = spec.Database
	}
	sslmode := "disable"
	if spec.TLS != nil && spec.TLS.Enabled {
		if spec.TLS.EnableHostVerification {
			sslmode = "verify-full"
		} else {
			sslmode = "require"
		}
	}
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(spec.User, password),
		Host:   fmt.Sprintf("%s:%d", spec.Host, spec.Port),
		Path:   "/" + dbName,
	}
	q := url.Values{}
	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()
	return u.String()
}

// sqlBackend adapts the SQL prober to the Backend interface.
type sqlBackend struct {
	spec   *temporalv1alpha1.SQLDatastoreSpec
	cred   ResolvedCredential
	dbName string
	// runner resolves a passwordCommand credential. Defaults to
	// DefaultCommandRunner when nil.
	runner CommandRunner
}

// resolvePassword returns the static password, or the fresh output of the
// configured passwordCommand when one is set (re-run on every call so an
// expiring token is always current).
func (b *sqlBackend) resolvePassword(ctx context.Context) (string, error) {
	if b.cred.PasswordCommand == "" {
		return b.cred.Password, nil
	}
	run := b.runner
	if run == nil {
		run = DefaultCommandRunner
	}
	return run(ctx, b.cred.PasswordCommand)
}

func (b *sqlBackend) dsn(password string) string {
	return BuildPostgresDSN(b.spec, password, b.dbName)
}

func (b *sqlBackend) Probe(ctx context.Context) error {
	password, err := b.resolvePassword(ctx)
	if err != nil {
		return err
	}
	return SQLProber{}.Probe(ctx, b.dsn(password))
}

func (b *sqlBackend) SchemaVersion(ctx context.Context) (string, error) {
	password, err := b.resolvePassword(ctx)
	if err != nil {
		return "", err
	}
	return SQLProber{}.CurrentSchemaVersion(ctx, b.dsn(password), b.dbName)
}

func (b *sqlBackend) EnsureSchema(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (b *sqlBackend) Kind() string { return "sql" }
