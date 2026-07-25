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
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

func deletingNamespace(deletedAgo time.Duration) *temporalv1alpha1.TemporalNamespace {
	ns := &temporalv1alpha1.TemporalNamespace{}
	ns.Name = "ns"
	ns.Namespace = "default"
	ts := metav1.NewTime(time.Now().Add(-deletedAgo))
	ns.DeletionTimestamp = &ts
	return ns
}

func TestDecideCleanupTargetNotFoundForgetsImmediately(t *testing.T) {
	// Issue #58: a cluster that no longer exists has nothing to clean up, so
	// the finalizer must come off at once regardless of the deadline.
	obj := deletingNamespace(0)
	action, _ := decideCleanup(obj, ErrTargetNotFound, time.Now())
	if action != cleanupForget {
		t.Errorf("action = %v, want cleanupForget for ErrTargetNotFound", action)
	}
}

func TestDecideCleanupTransientErrorRetriesWithinDeadline(t *testing.T) {
	obj := deletingNamespace(time.Minute)
	action, after := decideCleanup(obj, errors.New("connection refused"), time.Now())
	if action != cleanupRetry {
		t.Errorf("action = %v, want cleanupRetry one minute into a 5m deadline", action)
	}
	if after != cleanupRetryInterval {
		t.Errorf("requeue = %v, want %v", after, cleanupRetryInterval)
	}
}

func TestDecideCleanupAbandonsAfterDeadline(t *testing.T) {
	obj := deletingNamespace(6 * time.Minute)
	action, after := decideCleanup(obj, errors.New("connection refused"), time.Now())
	if action != cleanupAbandon {
		t.Errorf("action = %v, want cleanupAbandon past the 5m deadline", action)
	}
	if after != 0 {
		t.Errorf("requeue = %v, want 0 once abandoning", after)
	}
}

func TestDecideCleanupNeverExceedsDeadlineOnLastRetry(t *testing.T) {
	// Four minutes 55 seconds in, the next retry would land past the deadline;
	// it must be clamped so the abandon decision is not delayed a full interval.
	obj := deletingNamespace(4*time.Minute + 55*time.Second)
	action, after := decideCleanup(obj, errors.New("connection refused"), time.Now())
	if action != cleanupRetry {
		t.Fatalf("action = %v, want cleanupRetry just before the deadline", action)
	}
	if after > 5*time.Second+time.Second {
		t.Errorf("requeue = %v, want it clamped to the remaining ~5s", after)
	}
}

func TestDecideCleanupWithoutDeletionTimestampRetries(t *testing.T) {
	ns := &temporalv1alpha1.TemporalNamespace{}
	action, _ := decideCleanup(ns, errors.New("boom"), time.Now())
	if action != cleanupRetry {
		t.Errorf("action = %v, want cleanupRetry when no deletionTimestamp is set", action)
	}
}
