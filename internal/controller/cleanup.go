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
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/bmorton/temporal-operator/internal/recovery"
)

// cleanupDeadline bounds how long deletion waits for an unreachable target
// before abandoning remote cleanup. It is a var so tests can shorten it.
var cleanupDeadline = 5 * time.Minute

// cleanupRetryInterval is how often an unreachable target is retried during
// deletion.
var cleanupRetryInterval = 15 * time.Second

type cleanupAction int

const (
	// cleanupForget removes the finalizer immediately: the target is gone, so
	// there is nothing to clean up remotely. This is the issue #58 path.
	cleanupForget cleanupAction = iota
	// cleanupRetry waits and tries again: the target exists but is unreachable.
	cleanupRetry
	// cleanupAbandon gives up remote cleanup after the deadline, emitting a
	// warning first so the orphan is recorded rather than silent.
	cleanupAbandon
)

// decideCleanup chooses how to handle a failure to reach the Temporal target
// while a resource is being deleted.
//
// The distinction that matters: a target that does not exist can never be
// cleaned up, so waiting is pointless and we forget at once. A target that
// exists but is temporarily unreachable — a frontend mid-restart — deserves a
// bounded wait, because forgetting orphans a live Temporal object.
//
// Deletion always terminates either way, which is the guarantee issue #58
// established.
func decideCleanup(obj client.Object, err error, now time.Time) (cleanupAction, time.Duration) {
	if errors.Is(err, ErrTargetNotFound) {
		return cleanupForget, 0
	}

	deletedAt := obj.GetDeletionTimestamp()
	if deletedAt == nil {
		return cleanupRetry, cleanupRetryInterval
	}

	if recovery.DeadlineExceeded(*deletedAt, cleanupDeadline, now) {
		return cleanupAbandon, 0
	}

	// Clamp the wait so the deadline is honored promptly rather than overshot
	// by up to a full retry interval.
	remaining := recovery.Remaining(*deletedAt, cleanupDeadline, now)
	if remaining < cleanupRetryInterval {
		return cleanupRetry, remaining
	}
	return cleanupRetry, cleanupRetryInterval
}
