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

package recovery_test

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bmorton/temporal-operator/internal/recovery"
)

func TestPolicyNext(t *testing.T) {
	p := recovery.Policy{Delays: []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}}

	tests := []struct {
		name        string
		attempts    int
		wantRetry   bool
		wantAfter   time.Duration
		wantAttempt int
	}{
		{"first failure retries after 1m", 0, true, time.Minute, 1},
		{"second failure retries after 5m", 1, true, 5 * time.Minute, 2},
		{"third failure retries after 15m", 2, true, 15 * time.Minute, 3},
		{"fourth failure gives up", 3, false, 0, 0},
		{"beyond budget stays given up", 9, false, 0, 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := p.Next(tc.attempts)
			if got.Retry != tc.wantRetry {
				t.Errorf("Retry = %v, want %v", got.Retry, tc.wantRetry)
			}
			if got.After != tc.wantAfter {
				t.Errorf("After = %v, want %v", got.After, tc.wantAfter)
			}
			if got.Attempt != tc.wantAttempt {
				t.Errorf("Attempt = %v, want %v", got.Attempt, tc.wantAttempt)
			}
		})
	}
}

func TestPolicyNextNegativeAttemptsTreatedAsZero(t *testing.T) {
	p := recovery.Policy{Delays: []time.Duration{time.Minute}}
	got := p.Next(-1)
	if !got.Retry || got.After != time.Minute {
		t.Errorf("Next(-1) = %+v, want retry after 1m", got)
	}
	if got.Attempt != 1 {
		t.Errorf("Next(-1) Attempt = %v, want 1", got.Attempt)
	}
}

func TestDeadlineExceeded(t *testing.T) {
	start := metav1.NewTime(time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC))

	tests := []struct {
		name     string
		since    metav1.Time
		now      time.Time
		deadline time.Duration
		want     bool
	}{
		{"before deadline", start, start.Add(4 * time.Minute), 5 * time.Minute, false},
		{"exactly at deadline", start, start.Add(5 * time.Minute), 5 * time.Minute, true},
		{"past deadline", start, start.Add(6 * time.Minute), 5 * time.Minute, true},
		{"zero start never expires", metav1.Time{}, start.Add(4 * time.Minute), 5 * time.Minute, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := recovery.DeadlineExceeded(tc.since, tc.deadline, tc.now); got != tc.want {
				t.Errorf("DeadlineExceeded = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRemaining(t *testing.T) {
	start := metav1.NewTime(time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC))

	if got := recovery.Remaining(start, 5*time.Minute, start.Add(2*time.Minute)); got != 3*time.Minute {
		t.Errorf("Remaining = %v, want 3m", got)
	}
	if got := recovery.Remaining(start, 5*time.Minute, start.Add(9*time.Minute)); got != 0 {
		t.Errorf("Remaining past deadline = %v, want 0", got)
	}
	if got := recovery.Remaining(metav1.Time{}, 5*time.Minute, start.Add(2*time.Minute)); got != 5*time.Minute {
		t.Errorf("Remaining with zero since = %v, want 5m", got)
	}
}
