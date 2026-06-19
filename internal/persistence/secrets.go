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

// Package persistence contains helpers for resolving Temporal persistence
// configuration, including datastore credentials sourced from Secrets.
package persistence

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// ResolvedCredential is the resolved authentication material for a datastore.
// Password or PasswordCommand may be set for the server / schema Job;
// AzureWorkloadIdentity may additionally be set for operator-side token auth.
type ResolvedCredential struct {
	// Password is a static password (password-auth).
	Password string
	// PasswordCommand is a command that emits a short-lived credential
	// (Temporal 1.31+ IAM auth). When set, Password is empty.
	PasswordCommand string
	// AzureWorkloadIdentity, when non-nil, tells the operator to obtain a
	// Microsoft Entra access token via Azure Workload Identity for its own
	// database connections (probe + schema inspection). It is additive: the same
	// store may also carry Password/PasswordCommand for the server / schema Job.
	AzureWorkloadIdentity *AzureWorkloadIdentityCredential
}

// AzureWorkloadIdentityCredential carries the resolved Entra token scope for
// operator-side Azure Workload Identity auth.
type AzureWorkloadIdentityCredential struct {
	// Scope is the Entra token scope.
	Scope string
}

// DefaultAzureOSSRDBMSScope is the default Entra token scope for Azure Database
// for PostgreSQL / MySQL Flexible Server.
const DefaultAzureOSSRDBMSScope = "https://ossrdbms-aad.database.windows.net/.default"

// SecretResolver resolves datastore password references from Secrets in the
// cluster's namespace.
type SecretResolver struct {
	Client    client.Client
	Namespace string
}

// NewSecretResolver returns a SecretResolver bound to a namespace.
func NewSecretResolver(c client.Client, namespace string) *SecretResolver {
	return &SecretResolver{Client: c, Namespace: namespace}
}

func defaultKey(key string) string {
	if key == "" {
		return "password"
	}
	return key
}

// getSecretValue reads a single key from a Secret in the resolver's namespace.
func (r *SecretResolver) getSecretValue(ctx context.Context, ref *temporalv1alpha1.SecretKeyReference) (string, error) {
	var secret corev1.Secret
	nn := types.NamespacedName{Namespace: r.Namespace, Name: ref.Name}
	if err := r.Client.Get(ctx, nn, &secret); err != nil {
		return "", fmt.Errorf("getting secret %s: %w", nn, err)
	}
	key := defaultKey(ref.Key)
	value, ok := secret.Data[key]
	if !ok {
		return "", fmt.Errorf("secret %s has no key %q", nn, key)
	}
	return string(value), nil
}

// ResolveSQL resolves the credential for a SQL datastore. AzureWorkloadIdentity
// is additive and set whenever the spec enables it. PasswordCommand takes
// precedence over a static password when both password refs are set.
func (r *SecretResolver) ResolveSQL(ctx context.Context, spec *temporalv1alpha1.SQLDatastoreSpec) (ResolvedCredential, error) {
	var cred ResolvedCredential
	if spec.AzureWorkloadIdentity != nil {
		scope := spec.AzureWorkloadIdentity.Scope
		if scope == "" {
			scope = DefaultAzureOSSRDBMSScope
		}
		cred.AzureWorkloadIdentity = &AzureWorkloadIdentityCredential{Scope: scope}
	}
	switch {
	case spec.PasswordCommandSecretRef != nil:
		cmd, err := r.getSecretValue(ctx, spec.PasswordCommandSecretRef)
		if err != nil {
			return ResolvedCredential{}, err
		}
		cred.PasswordCommand = cmd
	case spec.PasswordSecretRef != nil:
		pw, err := r.getSecretValue(ctx, spec.PasswordSecretRef)
		if err != nil {
			return ResolvedCredential{}, err
		}
		cred.Password = pw
	}
	return cred, nil
}

// ResolveCassandra resolves the credential for a Cassandra datastore.
func (r *SecretResolver) ResolveCassandra(ctx context.Context, spec *temporalv1alpha1.CassandraDatastoreSpec) (ResolvedCredential, error) {
	if spec.PasswordSecretRef == nil {
		return ResolvedCredential{}, nil
	}
	pw, err := r.getSecretValue(ctx, spec.PasswordSecretRef)
	if err != nil {
		return ResolvedCredential{}, err
	}
	return ResolvedCredential{Password: pw}, nil
}

// ResolveElasticsearch resolves the credential for an Elasticsearch datastore.
func (r *SecretResolver) ResolveElasticsearch(ctx context.Context, spec *temporalv1alpha1.ElasticsearchDatastoreSpec) (ResolvedCredential, error) {
	if spec.PasswordSecretRef == nil {
		return ResolvedCredential{}, nil
	}
	pw, err := r.getSecretValue(ctx, spec.PasswordSecretRef)
	if err != nil {
		return ResolvedCredential{}, err
	}
	return ResolvedCredential{Password: pw}, nil
}

// ResolveStore resolves the credential for whichever backend a DatastoreSpec uses.
func (r *SecretResolver) ResolveStore(ctx context.Context, store temporalv1alpha1.DatastoreSpec) (ResolvedCredential, error) {
	switch {
	case store.SQL != nil:
		return r.ResolveSQL(ctx, store.SQL)
	case store.Cassandra != nil:
		return r.ResolveCassandra(ctx, store.Cassandra)
	case store.Elasticsearch != nil:
		return r.ResolveElasticsearch(ctx, store.Elasticsearch)
	default:
		return ResolvedCredential{}, fmt.Errorf("datastore has no backend configured")
	}
}
