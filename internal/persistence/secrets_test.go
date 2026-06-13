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
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func newClient(objs ...runtime.Object) *fake.ClientBuilder {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...)
}

func secret(name string, data map[string]string) *corev1.Secret {
	d := map[string][]byte{}
	for k, v := range data {
		d[k] = []byte(v)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "ns"},
		Data:       d,
	}
}

func TestResolveSQLPassword(t *testing.T) {
	c := newClient(secret("store", map[string]string{"password": "s3cret"})).Build()
	r := NewSecretResolver(c, "ns")

	cred, err := r.ResolveSQL(context.Background(), &temporalv1alpha1.SQLDatastoreSpec{
		PasswordSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "store"},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred.Password != "s3cret" || cred.PasswordCommand != "" {
		t.Errorf("unexpected credential: %+v", cred)
	}
}

func TestResolveSQLPasswordCommandTakesPrecedence(t *testing.T) {
	c := newClient(
		secret("store", map[string]string{"password": "s3cret"}),
		secret("cmd", map[string]string{"password": "aws rds generate-token"}),
	).Build()
	r := NewSecretResolver(c, "ns")

	cred, err := r.ResolveSQL(context.Background(), &temporalv1alpha1.SQLDatastoreSpec{
		PasswordSecretRef:        &temporalv1alpha1.SecretKeyReference{Name: "store"},
		PasswordCommandSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "cmd"},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred.PasswordCommand != "aws rds generate-token" || cred.Password != "" {
		t.Errorf("expected passwordCommand to win, got %+v", cred)
	}
}

func TestResolveCustomKey(t *testing.T) {
	c := newClient(secret("store", map[string]string{"pw": "abc"})).Build()
	r := NewSecretResolver(c, "ns")

	cred, err := r.ResolveSQL(context.Background(), &temporalv1alpha1.SQLDatastoreSpec{
		PasswordSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "store", Key: "pw"},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred.Password != "abc" {
		t.Errorf("expected abc, got %q", cred.Password)
	}
}

func TestResolveMissingSecret(t *testing.T) {
	c := newClient().Build()
	r := NewSecretResolver(c, "ns")

	_, err := r.ResolveSQL(context.Background(), &temporalv1alpha1.SQLDatastoreSpec{
		PasswordSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "nope"},
	})
	if err == nil {
		t.Errorf("expected error for missing secret")
	}
}

func TestResolveMissingKey(t *testing.T) {
	c := newClient(secret("store", map[string]string{"other": "x"})).Build()
	r := NewSecretResolver(c, "ns")

	_, err := r.ResolveSQL(context.Background(), &temporalv1alpha1.SQLDatastoreSpec{
		PasswordSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "store"},
	})
	if err == nil {
		t.Errorf("expected error for missing key")
	}
}

func TestResolveStoreDispatch(t *testing.T) {
	c := newClient(secret("store", map[string]string{"password": "p"})).Build()
	r := NewSecretResolver(c, "ns")

	cred, err := r.ResolveStore(context.Background(), temporalv1alpha1.DatastoreSpec{
		Cassandra: &temporalv1alpha1.CassandraDatastoreSpec{
			PasswordSecretRef: &temporalv1alpha1.SecretKeyReference{Name: "store"},
		},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if cred.Password != "p" {
		t.Errorf("expected p, got %q", cred.Password)
	}

	if _, err := r.ResolveStore(context.Background(), temporalv1alpha1.DatastoreSpec{}); err == nil {
		t.Errorf("expected error for empty datastore")
	}
}
