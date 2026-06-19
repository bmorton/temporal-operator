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
	"testing"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func sqlBackendSpec() *temporalv1alpha1.SQLDatastoreSpec {
	return &temporalv1alpha1.SQLDatastoreSpec{
		PluginName: "postgres12",
		Host:       "pg.example.com",
		Port:       5432,
		Database:   "temporal",
		User:       "temporal",
		TLS:        &temporalv1alpha1.DatastoreTLSSpec{Enabled: true},
	}
}

func TestResolvePasswordStatic(t *testing.T) {
	b := &sqlBackend{spec: sqlBackendSpec(), cred: ResolvedCredential{Password: "static"}, dbName: "temporal"}
	got, err := b.resolvePassword(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "static" {
		t.Errorf("expected static password, got %q", got)
	}
}

func TestResolvePasswordRunsCommandFresh(t *testing.T) {
	calls := 0
	b := &sqlBackend{
		spec:   sqlBackendSpec(),
		cred:   ResolvedCredential{PasswordCommand: "get-token"},
		dbName: "temporal",
		runner: func(_ context.Context, cmd string) (string, error) {
			calls++
			if cmd != "get-token" {
				t.Errorf("unexpected command %q", cmd)
			}
			return "token-fresh", nil
		},
	}
	for i := 0; i < 2; i++ {
		got, err := b.resolvePassword(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "token-fresh" {
			t.Errorf("expected token, got %q", got)
		}
	}
	if calls != 2 {
		t.Errorf("expected command run fresh per call (2), got %d", calls)
	}
}

func TestResolvePasswordCommandError(t *testing.T) {
	b := &sqlBackend{
		spec:   sqlBackendSpec(),
		cred:   ResolvedCredential{PasswordCommand: "boom"},
		dbName: "temporal",
		runner: func(_ context.Context, _ string) (string, error) { return "", errors.New("boom") },
	}
	if _, err := b.resolvePassword(context.Background()); err == nil {
		t.Fatal("expected error from failing command")
	}
}
