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

// Package events wraps the controller-runtime event recorder with
// deduplication, so states that are re-evaluated on every reconcile (a stalled
// upgrade, an unreachable target) emit one Event per state change rather than
// one per requeue.
package events

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Recorder emits deduplicated Kubernetes Events.
//
// A nil *Recorder is valid and drops every event, which keeps controllers
// constructed without a recorder (as several tests do) working unchanged.
type Recorder struct {
	inner events.EventRecorder

	mu   sync.Mutex
	last map[string]string
}

// New wraps an event recorder. A nil inner recorder yields a no-op Recorder.
func New(inner events.EventRecorder) *Recorder {
	if inner == nil {
		return nil
	}
	return &Recorder{inner: inner, last: map[string]string{}}
}

// Normal emits an informational event.
func (r *Recorder) Normal(obj runtime.Object, reason, message string) {
	r.emit(obj, corev1.EventTypeNormal, reason, message)
}

// Warning emits a warning event.
func (r *Recorder) Warning(obj runtime.Object, reason, message string) {
	r.emit(obj, corev1.EventTypeWarning, reason, message)
}

// Forget clears the dedupe state for an object so the next event is emitted
// even if it repeats the previous one. Call it when a resource leaves an
// abnormal state, so re-entering that state is reported again.
func (r *Recorder) Forget(obj runtime.Object) {
	if r == nil {
		return
	}
	key, ok := objectKey(obj)
	if !ok {
		return
	}
	// Dedupe map keys have the form "<uid>|<eventtype>|<reason>".
	// We delete all entries for this object by matching the "<uid>|" prefix.
	// Using the separator in the prefix prevents accidentally matching a
	// different object whose UID happens to share a leading substring.
	prefix := key + "|"
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.last {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			delete(r.last, k)
		}
	}
}

func (r *Recorder) emit(obj runtime.Object, eventtype, reason, message string) {
	if r == nil || r.inner == nil {
		return
	}
	if key, ok := objectKey(obj); ok {
		dedupeKey := key + "|" + eventtype + "|" + reason
		r.mu.Lock()
		if prev, seen := r.last[dedupeKey]; seen && prev == message {
			r.mu.Unlock()
			return
		}
		r.last[dedupeKey] = message
		r.mu.Unlock()
	}
	// The events.k8s.io recorder requires an action verb; reuse the reason,
	// which already reads as a machine-readable verb for our events.
	r.inner.Eventf(obj, nil, eventtype, reason, reason, message)
}

func objectKey(obj runtime.Object) (string, bool) {
	o, ok := obj.(client.Object)
	if !ok {
		return "", false
	}
	return string(o.GetUID()), true
}
