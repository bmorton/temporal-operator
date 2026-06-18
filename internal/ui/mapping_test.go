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
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func cond(t, status, reason string) metav1.Condition {
	return metav1.Condition{Type: t, Status: metav1.ConditionStatus(status), Reason: reason}
}

func TestBadgeForCondition(t *testing.T) {
	conds := []metav1.Condition{
		cond("Ready", "True", "AllServicesReady"),
		cond("PersistenceReachable", "False", "PersistenceUnreachable"),
		cond("SchemaReady", "Unknown", "SchemaMigrating"),
	}
	if got := badgeForCondition(conds, "Ready"); got != BadgeOK {
		t.Errorf("Ready badge = %v, want ok", got)
	}
	if got := badgeForCondition(conds, "PersistenceReachable"); got != BadgeError {
		t.Errorf("Persistence badge = %v, want error", got)
	}
	if got := badgeForCondition(conds, "SchemaReady"); got != BadgePending {
		t.Errorf("Schema badge = %v, want pending", got)
	}
	if got := badgeForCondition(conds, "MTLSReady"); got != BadgeUnknown {
		t.Errorf("missing badge = %v, want unknown", got)
	}
}

func TestSummaryFromCluster(t *testing.T) {
	c := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "demo", Namespace: "team-a"},
		Spec: temporalv1alpha1.TemporalClusterSpec{
			Version:          "1.31.1",
			NumHistoryShards: 512,
			MTLS:             &temporalv1alpha1.MTLSSpec{},
		},
		Status: temporalv1alpha1.TemporalClusterStatus{
			Phase: "Running",
			Conditions: []metav1.Condition{
				cond("Ready", "True", "AllServicesReady"),
				cond("PersistenceReachable", "True", ""),
				cond("MTLSReady", "True", ""),
			},
		},
	}
	s := SummaryFromCluster(c)
	if s.Name != "demo" || s.Namespace != "team-a" {
		t.Fatalf("identity wrong: %+v", s)
	}
	if s.Version != "1.31.1" || s.Shards != 512 {
		t.Errorf("spec fields wrong: %+v", s)
	}
	if s.Ready != BadgeOK || s.Persistence != BadgeOK {
		t.Errorf("badges wrong: %+v", s)
	}
	if !s.MTLSEnabled || s.MTLS != BadgeOK {
		t.Errorf("mtls wrong: %+v", s)
	}
	if s.Upgrading {
		t.Errorf("should not be upgrading: %+v", s)
	}
}

func TestServiceRowsOrdered(t *testing.T) {
	c := &temporalv1alpha1.TemporalCluster{
		Status: temporalv1alpha1.TemporalClusterStatus{
			Services: map[string]temporalv1alpha1.ServiceStatus{
				"worker":   {Ready: 1, Desired: 1},
				"frontend": {Ready: 0, Desired: 2},
				"history":  {Ready: 3, Desired: 3},
			},
		},
	}
	rows := serviceRows(c)
	if len(rows) != 3 {
		t.Fatalf("len = %d, want 3", len(rows))
	}
	if rows[0].Name != "frontend" || rows[1].Name != "history" || rows[2].Name != "worker" {
		t.Errorf("order wrong: %+v", rows)
	}
	if rows[0].State != BadgeError { // 0/2 ready
		t.Errorf("frontend state = %v, want error", rows[0].State)
	}
	if rows[1].State != BadgeOK { // 3/3 ready
		t.Errorf("history state = %v, want ok", rows[1].State)
	}
}

func TestServiceStateWarn(t *testing.T) {
	c := &temporalv1alpha1.TemporalCluster{
		Status: temporalv1alpha1.TemporalClusterStatus{
			Services: map[string]temporalv1alpha1.ServiceStatus{
				"matching": {Ready: 1, Desired: 2},
			},
		},
	}
	rows := serviceRows(c)
	if len(rows) != 1 || rows[0].State != BadgeWarn {
		t.Errorf("want one warn row, got %+v", rows)
	}
}

func TestDurationFormat(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second: "<1m",
		5 * time.Minute:  "5m",
		3 * time.Hour:    "3h",
		50 * time.Hour:   "2d",
	}
	for d, want := range cases {
		if got := duration(d); got != want {
			t.Errorf("duration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestUpgradeInfo(t *testing.T) {
	c := &temporalv1alpha1.TemporalCluster{
		Status: temporalv1alpha1.TemporalClusterStatus{
			Upgrade: &temporalv1alpha1.UpgradeStatus{
				FromVersion: "1.30.4", ToVersion: "1.31.1", Phase: "Migrating", Rollbackable: false,
			},
		},
	}
	u := upgradeInfo(c)
	if !u.Active || u.FromVersion != "1.30.4" || u.ToVersion != "1.31.1" {
		t.Errorf("upgrade wrong: %+v", u)
	}
	if SummaryFromCluster(c).Upgrading != true {
		t.Error("summary should report upgrading")
	}
}

func TestRelatedFromSatellites(t *testing.T) {
	ns := []temporalv1alpha1.TemporalNamespace{{
		ObjectMeta: metav1.ObjectMeta{Name: "orders", Namespace: "team-a"},
		Spec:       temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: corev1.LocalObjectReference{Name: "demo"}},
		Status: temporalv1alpha1.TemporalNamespaceStatus{
			Conditions: []metav1.Condition{cond("Ready", "True", "")},
		},
	}}
	other := []temporalv1alpha1.TemporalNamespace{{
		Spec: temporalv1alpha1.TemporalNamespaceSpec{ClusterRef: corev1.LocalObjectReference{Name: "elsewhere"}},
	}}
	got := relatedNamespaces(append(ns, other...), "demo")
	if len(got) != 1 || got[0].Name != "orders" || got[0].Ready != BadgeOK {
		t.Errorf("related wrong: %+v", got)
	}
}
