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

package controller

import (
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func TestIsTransientClusterErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unavailable", status.Error(codes.Unavailable, "connecting"), true},
		{"deadline", status.Error(codes.DeadlineExceeded, "timeout"), true},
		{"canceled", status.Error(codes.Canceled, "canceled"), true},
		{"fmt-wrapped unavailable", fmt.Errorf("registering namespace: %w", status.Error(codes.Unavailable, "x")), true},
		{"invalid argument", status.Error(codes.InvalidArgument, "bad"), false},
		{"permission denied", status.Error(codes.PermissionDenied, "no"), false},
		{"plain error", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientClusterErr(tc.err); got != tc.want {
				t.Fatalf("isTransientClusterErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestRefTargets(t *testing.T) {
	cases := []struct {
		name string
		ref  temporalv1alpha1.ClusterReference
		kind string
		obj  string
		want bool
	}{
		{"cluster match", temporalv1alpha1.ClusterReference{Name: "c1"}, temporalv1alpha1.ClusterKindTemporalCluster, "c1", true},
		{"empty kind defaults to cluster", temporalv1alpha1.ClusterReference{Name: "c1", Kind: ""}, temporalv1alpha1.ClusterKindTemporalCluster, "c1", true},
		{"name mismatch", temporalv1alpha1.ClusterReference{Name: "c1"}, temporalv1alpha1.ClusterKindTemporalCluster, "c2", false},
		{"kind mismatch", temporalv1alpha1.ClusterReference{Name: "c1", Kind: temporalv1alpha1.ClusterKindTemporalDevServer}, temporalv1alpha1.ClusterKindTemporalCluster, "c1", false},
		{"devserver match", temporalv1alpha1.ClusterReference{Name: "d1", Kind: temporalv1alpha1.ClusterKindTemporalDevServer}, temporalv1alpha1.ClusterKindTemporalDevServer, "d1", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := refTargets(tc.ref, tc.kind, tc.obj); got != tc.want {
				t.Fatalf("refTargets = %v, want %v", got, tc.want)
			}
		})
	}
}

func clusterWithReady(gen int64, ready *metav1.ConditionStatus) *temporalv1alpha1.TemporalCluster {
	c := &temporalv1alpha1.TemporalCluster{}
	c.Generation = gen
	if ready != nil {
		c.Status.Conditions = []metav1.Condition{{
			Type:   temporalv1alpha1.ConditionReady,
			Status: *ready,
		}}
	}
	return c
}

func TestClusterReadinessChangedUpdate(t *testing.T) {
	readyTrue := metav1.ConditionTrue
	readyFalse := metav1.ConditionFalse

	t.Run("ready transition fires", func(t *testing.T) {
		oldObj := clusterWithReady(1, &readyFalse)
		newObj := clusterWithReady(1, &readyTrue)
		if !clusterReadinessChanged.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
			t.Fatal("expected update to fire on Ready transition")
		}
	})
	t.Run("no change does not fire", func(t *testing.T) {
		oldObj := clusterWithReady(1, &readyTrue)
		newObj := clusterWithReady(1, &readyTrue)
		if clusterReadinessChanged.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
			t.Fatal("expected update NOT to fire when nothing changed")
		}
	})
	t.Run("generation bump fires", func(t *testing.T) {
		oldObj := clusterWithReady(1, &readyTrue)
		newObj := clusterWithReady(2, &readyTrue)
		if !clusterReadinessChanged.Update(event.UpdateEvent{ObjectOld: oldObj, ObjectNew: newObj}) {
			t.Fatal("expected update to fire on generation change")
		}
	})
	t.Run("create always fires", func(t *testing.T) {
		if !clusterReadinessChanged.Create(event.CreateEvent{Object: clusterWithReady(1, &readyTrue)}) {
			t.Fatal("expected create to fire")
		}
	})
}
