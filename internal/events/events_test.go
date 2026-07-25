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

package events_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	opevents "github.com/bmorton/temporal-operator/internal/events"
)

type recorded struct {
	eventtype string
	reason    string
	note      string
}

type fakeRecorder struct {
	got []recorded
}

func (f *fakeRecorder) Eventf(_ runtime.Object, _ runtime.Object, eventtype, reason, _, note string, _ ...interface{}) {
	f.got = append(f.got, recorded{eventtype: eventtype, reason: reason, note: note})
}

func newNamespace(name string) *temporalv1alpha1.TemporalNamespace {
	ns := &temporalv1alpha1.TemporalNamespace{}
	ns.Name = name
	ns.Namespace = "default"
	ns.UID = types.UID(name + "-uid")
	return ns
}

func TestNormalAndWarningPassThrough(t *testing.T) {
	f := &fakeRecorder{}
	r := opevents.New(f)
	ns := newNamespace("a")

	r.Normal(ns, "Registered", "namespace registered")
	r.Warning(ns, "CleanupAbandoned", "gave up")

	if len(f.got) != 2 {
		t.Fatalf("got %d events, want 2", len(f.got))
	}
	if f.got[0].eventtype != corev1.EventTypeNormal || f.got[0].reason != "Registered" {
		t.Errorf("first event = %+v, want Normal/Registered", f.got[0])
	}
	if f.got[1].eventtype != corev1.EventTypeWarning || f.got[1].note != "gave up" {
		t.Errorf("second event = %+v, want Warning with note %q", f.got[1], "gave up")
	}
}

func TestRepeatedIdenticalEventEmittedOnce(t *testing.T) {
	f := &fakeRecorder{}
	r := opevents.New(f)
	ns := newNamespace("a")

	for i := 0; i < 5; i++ {
		r.Warning(ns, "UpgradeStalled", "frontend not rolled out")
	}

	if len(f.got) != 1 {
		t.Fatalf("got %d events, want 1 (repeats deduplicated)", len(f.got))
	}
}

func TestChangedMessageEmitsAgain(t *testing.T) {
	f := &fakeRecorder{}
	r := opevents.New(f)
	ns := newNamespace("a")

	r.Warning(ns, "UpgradeStalled", "frontend not rolled out")
	r.Warning(ns, "UpgradeStalled", "history not rolled out")

	if len(f.got) != 2 {
		t.Fatalf("got %d events, want 2 (message changed)", len(f.got))
	}
}

func TestDifferentObjectsDoNotShareDedupeState(t *testing.T) {
	f := &fakeRecorder{}
	r := opevents.New(f)

	r.Warning(newNamespace("a"), "UpgradeStalled", "same message")
	r.Warning(newNamespace("b"), "UpgradeStalled", "same message")

	if len(f.got) != 2 {
		t.Fatalf("got %d events, want 2 (distinct objects)", len(f.got))
	}
}

func TestForgetAllowsReEmission(t *testing.T) {
	f := &fakeRecorder{}
	r := opevents.New(f)
	ns := newNamespace("a")

	r.Warning(ns, "UpgradeStalled", "stalled")
	r.Forget(ns)
	r.Warning(ns, "UpgradeStalled", "stalled")

	if len(f.got) != 2 {
		t.Fatalf("got %d events, want 2 (Forget clears dedupe state)", len(f.got))
	}
}

func TestForgetIsolationPreventsPrefixCollisions(t *testing.T) {
	// This test verifies that Forget(A) does not accidentally delete entries for
	// object B when A's UID is a leading substring of B's UID.
	// The dedupe key format is "<uid>|<eventtype>|<reason>", so using the
	// separator in the prefix match (key+"|" rather than bare key) prevents this.
	f := &fakeRecorder{}
	r := opevents.New(f)

	// Create two objects whose UIDs have a substring relationship.
	// newNamespace(name) constructs UID as name+"-uid".
	// So "a-uid" and "a-uid-extra" give us the substring case we're testing.
	a := newNamespace("a")           // UID = "a-uid"
	b := newNamespace("a-uid-extra") // UID = "a-uid-extra-uid"

	// Emit the same reason+message for both, so both get dedupe entries.
	const msg = "same message"
	r.Warning(a, "Event", msg)
	r.Warning(b, "Event", msg)

	if len(f.got) != 2 {
		t.Fatalf("initial emit: got %d events, want 2", len(f.got))
	}

	// Forget the shorter-UID object (a).
	r.Forget(a)

	// The shorter-UID object should emit again (its dedupe state was cleared).
	r.Warning(a, "Event", msg)
	if len(f.got) != 3 {
		t.Fatalf("after Forget(a): got %d events, want 3 (a should re-emit)", len(f.got))
	}

	// The longer-UID object should still deduplicate (its dedupe state was NOT cleared).
	r.Warning(b, "Event", msg)
	if len(f.got) != 3 {
		t.Fatalf("after Forget(a) + duplicate b: got %d events, want 3 (b should deduplicate)", len(f.got))
	}
}

func TestNilRecorderIsSafe(t *testing.T) {
	var r *opevents.Recorder
	r.Normal(newNamespace("a"), "Registered", "ok")    // must not panic
	r.Warning(newNamespace("a"), "Warning", "warning") // must not panic
	r.Forget(newNamespace("a"))                        // must not panic
	r.Warning(newNamespace("a"), "Warning", "warning") // must not panic
	r.Forget(newNamespace("a"))                        // must not panic
}
