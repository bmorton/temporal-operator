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

// Package recovery provides pure, time-injected decisions for bounded retry.
//
// Nothing here holds state. Callers persist the attempt count and first-failure
// timestamp in the resource's status, so recovery survives operator restarts and
// leader-election failover.
package recovery

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Policy is an ordered list of delays. The Nth failure waits Delays[N] before
// the next attempt; running off the end means give up.
type Policy struct {
	Delays []time.Duration
}

// Decision is the outcome of consulting a Policy.
type Decision struct {
	// Retry is false once the attempt budget is exhausted.
	Retry bool
	// After is how long to wait before the next attempt. Zero when Retry is false.
	After time.Duration
	// Attempt is the 1-based number of the attempt this decision authorises.
	Attempt int
}

// SchemaJobPolicy governs schema Job recreation. The minute-scale spacing is
// deliberate: the Job's own BackoffLimit already retries the pod within seconds,
// which does not cover the common failure of a database that is still starting.
var SchemaJobPolicy = Policy{Delays: []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
}}

// Next reports whether a further attempt is authorised after the given number of
// prior failed attempts.
func (p Policy) Next(attempts int) Decision {
	if attempts < 0 {
		attempts = 0
	}
	if attempts >= len(p.Delays) {
		return Decision{Retry: false}
	}
	return Decision{Retry: true, After: p.Delays[attempts], Attempt: attempts + 1}
}

// DeadlineExceeded reports whether deadline has elapsed since the given time.
// A zero since never expires — callers that have not yet recorded a start time
// should keep waiting rather than immediately give up.
func DeadlineExceeded(since metav1.Time, deadline time.Duration, now time.Time) bool {
	if since.IsZero() {
		return false
	}
	return !now.Before(since.Add(deadline))
}

// Remaining is the time left before the deadline elapses, floored at zero.
func Remaining(since metav1.Time, deadline time.Duration, now time.Time) time.Duration {
	if since.IsZero() {
		return deadline
	}
	left := since.Add(deadline).Sub(now)
	if left < 0 {
		return 0
	}
	return left
}
