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

type fakeTokenProvider struct {
	calls int
	scope string
	token string
	err   error
}

func (f *fakeTokenProvider) Token(_ context.Context, scope string) (string, error) {
	f.calls++
	f.scope = scope
	return f.token, f.err
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

func TestResolvePasswordAzureToken(t *testing.T) {
	fake := &fakeTokenProvider{token: "entra-token"}
	b := &sqlBackend{
		spec:   sqlBackendSpec(),
		cred:   ResolvedCredential{AzureWorkloadIdentity: &AzureWorkloadIdentityCredential{Scope: DefaultAzureOSSRDBMSScope}},
		dbName: "temporal",
		tokens: fake,
	}
	for i := 0; i < 2; i++ {
		got, err := b.resolvePassword(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "entra-token" {
			t.Errorf("expected token, got %q", got)
		}
	}
	if fake.calls != 2 {
		t.Errorf("expected a fresh token per call (2), got %d", fake.calls)
	}
	if fake.scope != DefaultAzureOSSRDBMSScope {
		t.Errorf("expected scope %q passed to provider, got %q", DefaultAzureOSSRDBMSScope, fake.scope)
	}
}

func TestResolvePasswordAzureTokenError(t *testing.T) {
	fake := &fakeTokenProvider{err: errors.New("boom")}
	b := &sqlBackend{
		spec:   sqlBackendSpec(),
		cred:   ResolvedCredential{AzureWorkloadIdentity: &AzureWorkloadIdentityCredential{Scope: DefaultAzureOSSRDBMSScope}},
		dbName: "temporal",
		tokens: fake,
	}
	if _, err := b.resolvePassword(context.Background()); err == nil {
		t.Fatal("expected error from token provider")
	}
}
