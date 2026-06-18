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

package ui

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := temporalv1alpha1.AddToScheme(s); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestListClusters(t *testing.T) {
	s := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		&temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "ns2"},
			Spec:       temporalv1alpha1.TemporalClusterSpec{Version: "1.31.1", NumHistoryShards: 512},
		},
		&temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns1"},
			Spec:       temporalv1alpha1.TemporalClusterSpec{Version: "1.31.1", NumHistoryShards: 512},
		},
	).Build()

	ds := &CachedDataSource{Reader: c}
	got, err := ds.ListClusters(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Namespace != "ns1" || got[1].Namespace != "ns2" {
		t.Errorf("not sorted by ns/name: %+v", got)
	}
}

func TestGetClusterWithRelated(t *testing.T) {
	s := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		&temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "team-a"},
			Spec:       temporalv1alpha1.TemporalClusterSpec{Version: "1.31.1", NumHistoryShards: 512},
		},
		&temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "team-a"},
			Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: corev1.LocalObjectReference{Name: "demo"}},
		},
		&temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "team-a"},
			Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: corev1.LocalObjectReference{Name: "nope"}},
		},
	).Build()

	ds := &CachedDataSource{Reader: c}
	got, err := ds.GetCluster(context.Background(), "team-a", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "demo" {
		t.Fatalf("name = %q", got.Name)
	}
	if len(got.Related) != 1 || got.Related[0].Name != "orders" {
		t.Errorf("related wrong: %+v", got.Related)
	}
}

func TestGetClusterExcludesOtherNamespaces(t *testing.T) {
	s := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		&temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "team-a"},
			Spec:       temporalv1alpha1.TemporalClusterSpec{Version: "1.31.1", NumHistoryShards: 512},
		},
		// Same cluster name, DIFFERENT namespace — must NOT appear as related.
		&temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: "intruder", Namespace: "team-b"},
			Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: corev1.LocalObjectReference{Name: "demo"}},
		},
	).Build()

	ds := &CachedDataSource{Reader: c}
	got, err := ds.GetCluster(context.Background(), "team-a", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Related) != 0 {
		t.Errorf("expected no related from other namespaces, got %+v", got.Related)
	}
}

func TestGetClusterRelatedClientsAndAttributes(t *testing.T) {
	s := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(s).WithObjects(
		&temporalv1alpha1.TemporalCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "team-a"},
			Spec:       temporalv1alpha1.TemporalClusterSpec{Version: "1.31.1", NumHistoryShards: 512},
		},
		&temporalv1alpha1.TemporalClusterClient{
			ObjectMeta: metav1.ObjectMeta{Name: "cli", Namespace: "team-a"},
			Spec:       temporalv1alpha1.TemporalClusterClientSpec{ClusterRef: corev1.LocalObjectReference{Name: "demo"}},
		},
		&temporalv1alpha1.TemporalSearchAttribute{
			ObjectMeta: metav1.ObjectMeta{Name: "attr", Namespace: "team-a"},
			Spec:       temporalv1alpha1.TemporalSearchAttributeSpec{ClusterRef: corev1.LocalObjectReference{Name: "demo"}},
		},
	).Build()

	ds := &CachedDataSource{Reader: c}
	got, err := ds.GetCluster(context.Background(), "team-a", "demo")
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]string, 0, len(got.Related))
	for _, r := range got.Related {
		kinds = append(kinds, r.Kind)
	}
	if len(got.Related) != 2 {
		t.Fatalf("want 2 related, got %d (%v)", len(got.Related), kinds)
	}
	hasClient, hasAttr := false, false
	for _, r := range got.Related {
		if r.Kind == "TemporalClusterClient" && r.Name == "cli" {
			hasClient = true
		}
		if r.Kind == "TemporalSearchAttribute" && r.Name == "attr" {
			hasAttr = true
		}
	}
	if !hasClient || !hasAttr {
		t.Errorf("missing related kinds: %v", kinds)
	}
}

func TestGetClusterNotFound(t *testing.T) {
	ds := &CachedDataSource{Reader: fake.NewClientBuilder().WithScheme(testScheme(t)).Build()}
	if _, err := ds.GetCluster(context.Background(), "x", "y"); err == nil {
		t.Error("expected error for missing cluster")
	}
}
