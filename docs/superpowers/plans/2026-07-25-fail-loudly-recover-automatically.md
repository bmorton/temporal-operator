# Fail Loudly, Recover Automatically — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every abnormal state in the operator named by a condition, bounded in its recovery, and observable in Prometheus — so no stall is silent and no retry is infinite.

**Architecture:** Four new shared packages under `internal/` (`status`, `recovery`, `events`, `metrics`) replace duplicated per-controller plumbing and provide the primitives. Five behavior changes then build on them: upgrade stall detection, a mid-upgrade webhook guard, bounded schema-Job recreation, a cleanup deadline, and gRPC dial deadlines. Finally a condition-derived Prometheus collector exports every condition uniformly, and a `PrometheusRule` alerts on exactly the states introduced.

**Tech Stack:** Go 1.26, controller-runtime v0.23, Kubebuilder v4, envtest, Ginkgo/Gomega, chainsaw (e2e), Prometheus client_golang, cert-manager.

**Spec:** `docs/superpowers/specs/2026-07-25-fail-loudly-recover-automatically-design.md`

## Global Constraints

- Module path is `github.com/bmorton/temporal-operator`; CRD group is `temporal.bmor10.com`.
- Every commit is signed off: `git commit -s`. The DCO check is enforced in CI.
- Commit messages follow Conventional Commits (`feat:`, `fix:`, `test:`, `refactor:`, `docs:`, `chore:`).
- Gates: `make build`, `make test`, `make lint`. All three must pass before any commit.
- After changing any type in `api/v1alpha1/`, run `make generate manifests` and commit the regenerated `zz_generated.deepcopy.go` and `config/crd/bases/*.yaml`.
- After changing RBAC markers or anything under `config/` that the chart mirrors, run `make helm-chart` and commit `dist/chart`. **Never hand-edit `dist/chart`** — edit `hack/helm/overrides/` instead.
- No new `spec` API surface in this project. Status fields only.
- Every new timeout is a package-level `var` (not `const`) so tests can shorten it.
- Apache 2.0 license header (copy the 15-line block from any existing `.go` file, e.g. `internal/controller/target.go:1-15`) at the top of every new Go file.

## File Structure

**New packages:**

| File | Responsibility |
| --- | --- |
| `api/v1alpha1/accessors.go` | `GetConditions`/`SetObservedGeneration` methods on all 8 CRD types |
| `internal/status/status.go` | `Object` interface, `Set`, `Update` with conflict retry |
| `internal/recovery/recovery.go` | Pure bounded-retry decisions: `Next`, `DeadlineExceeded` |
| `internal/events/events.go` | `Recorder` wrapper with deduplication and typed helpers |
| `internal/metrics/conditions.go` | `prometheus.Collector` over all 8 kinds |
| `internal/metrics/domain.go` | The 4 domain metrics |
| `config/prometheus/alerts.yaml` | `PrometheusRule` |
| `test/e2e/upgrade-stall/` | Chainsaw suite for a stalled upgrade |

**Modified:** the 7 satellite controllers (status migration), `temporalcluster_upgrade.go`, `temporalcluster_persistence.go`, `temporalcluster_webhook.go`, `target.go`, `internal/temporal/client.go`, `internal/temporal/schedule.go`, `internal/temporal/workflowrun.go`, `cmd/main.go`, `Makefile`.

---

# Phase 1 — Foundations

Pure refactor plus the 409 fix. No behavior change. Lands low-risk and shrinks every later phase.

### Task 1: CRD status accessors

**Files:**
- Create: `api/v1alpha1/accessors.go`
- Test: `api/v1alpha1/accessors_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: on each of `TemporalCluster`, `TemporalClusterClient`, `TemporalClusterConnection`, `TemporalDevServer`, `TemporalNamespace`, `TemporalSchedule`, `TemporalSearchAttribute`, `TemporalWorkflowRun`: `GetConditions() *[]metav1.Condition` and `SetObservedGeneration(int64)`.

- [ ] **Step 1: Write the failing test**

Create `api/v1alpha1/accessors_test.go` (license header, then):

```go
package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// conditionAccessor is the contract internal/status depends on.
type conditionAccessor interface {
	GetConditions() *[]metav1.Condition
	SetObservedGeneration(int64)
}

func TestAllTypesImplementConditionAccessor(t *testing.T) {
	objs := map[string]conditionAccessor{
		"TemporalCluster":           &TemporalCluster{},
		"TemporalClusterClient":     &TemporalClusterClient{},
		"TemporalClusterConnection": &TemporalClusterConnection{},
		"TemporalDevServer":         &TemporalDevServer{},
		"TemporalNamespace":         &TemporalNamespace{},
		"TemporalSchedule":          &TemporalSchedule{},
		"TemporalSearchAttribute":   &TemporalSearchAttribute{},
		"TemporalWorkflowRun":       &TemporalWorkflowRun{},
	}

	for name, obj := range objs {
		t.Run(name, func(t *testing.T) {
			conds := obj.GetConditions()
			if conds == nil {
				t.Fatal("GetConditions returned nil pointer")
			}
			*conds = append(*conds, metav1.Condition{Type: "Ready"})
			if len(*obj.GetConditions()) != 1 {
				t.Error("GetConditions does not return a live pointer into status")
			}
			obj.SetObservedGeneration(7)
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./api/v1alpha1/ -run TestAllTypesImplementConditionAccessor`
Expected: FAIL — compile error, `*TemporalCluster does not implement conditionAccessor (missing method GetConditions)`.

- [ ] **Step 3: Write minimal implementation**

Create `api/v1alpha1/accessors.go` (license header, then):

```go
package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// This file provides the uniform status accessors that internal/status uses to
// operate on any Temporal CRD without type switches. Keep one pair of methods
// per API type; there is intentionally no reflection here.

func (t *TemporalCluster) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalCluster) SetObservedGeneration(g int64)      { t.Status.ObservedGeneration = g }

func (t *TemporalClusterClient) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalClusterClient) SetObservedGeneration(g int64)      { t.Status.ObservedGeneration = g }

func (t *TemporalClusterConnection) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalClusterConnection) SetObservedGeneration(g int64) {
	t.Status.ObservedGeneration = g
}

func (t *TemporalDevServer) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalDevServer) SetObservedGeneration(g int64)      { t.Status.ObservedGeneration = g }

func (t *TemporalNamespace) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalNamespace) SetObservedGeneration(g int64)      { t.Status.ObservedGeneration = g }

func (t *TemporalSchedule) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalSchedule) SetObservedGeneration(g int64)      { t.Status.ObservedGeneration = g }

func (t *TemporalSearchAttribute) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalSearchAttribute) SetObservedGeneration(g int64) {
	t.Status.ObservedGeneration = g
}

func (t *TemporalWorkflowRun) GetConditions() *[]metav1.Condition { return &t.Status.Conditions }
func (t *TemporalWorkflowRun) SetObservedGeneration(g int64)      { t.Status.ObservedGeneration = g }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./api/v1alpha1/ -run TestAllTypesImplementConditionAccessor -v`
Expected: PASS, 8 subtests.

- [ ] **Step 5: Commit**

```bash
git add api/v1alpha1/accessors.go api/v1alpha1/accessors_test.go
git commit -s -m "refactor(api): add uniform condition accessors to all CRD types"
```

---

### Task 2: internal/status with conflict retry

**Files:**
- Create: `internal/status/status.go`
- Test: `internal/status/status_test.go`

**Interfaces:**
- Consumes: `GetConditions`/`SetObservedGeneration` from Task 1.
- Produces:
  - `type Object interface { client.Object; GetConditions() *[]metav1.Condition; SetObservedGeneration(int64) }`
  - `func Set(obj Object, condType string, s metav1.ConditionStatus, reason, message string)`
  - `func Update(ctx context.Context, c client.Client, obj Object) error`
  - `func IsTrue(obj Object, condType string) bool`

- [ ] **Step 1: Write the failing test**

Create `internal/status/status_test.go` (license header, then):

```go
package status_test

import (
	"context"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/status"
)

func scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := temporalv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}
	return s
}

func TestSetStampsObservedGeneration(t *testing.T) {
	ns := &temporalv1alpha1.TemporalNamespace{}
	ns.Generation = 4

	status.Set(ns, temporalv1alpha1.ConditionReady, metav1.ConditionTrue, "Registered", "ok")

	if ns.Status.ObservedGeneration != 4 {
		t.Errorf("status.observedGeneration = %d, want 4", ns.Status.ObservedGeneration)
	}
	conds := *ns.GetConditions()
	if len(conds) != 1 {
		t.Fatalf("got %d conditions, want 1", len(conds))
	}
	if conds[0].ObservedGeneration != 4 {
		t.Errorf("condition.observedGeneration = %d, want 4", conds[0].ObservedGeneration)
	}
	if conds[0].Reason != "Registered" {
		t.Errorf("condition.reason = %q, want %q", conds[0].Reason, "Registered")
	}
}

// conflictClient returns a Conflict error for the first n status updates.
type conflictClient struct {
	client.Client
	remaining int
	attempts  int
}

func (c *conflictClient) Status() client.SubResourceWriter {
	return &conflictWriter{parent: c, inner: c.Client.Status()}
}

type conflictWriter struct {
	parent *conflictClient
	inner  client.SubResourceWriter
}

func (w *conflictWriter) Create(ctx context.Context, obj client.Object, sub client.Object, opts ...client.SubResourceCreateOption) error {
	return w.inner.Create(ctx, obj, sub, opts...)
}

func (w *conflictWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.SubResourcePatchOption) error {
	return w.inner.Patch(ctx, obj, patch, opts...)
}

func (w *conflictWriter) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	w.parent.attempts++
	if w.parent.remaining > 0 {
		w.parent.remaining--
		return apierrors.NewConflict(schema.GroupResource{Group: "temporal.bmor10.com", Resource: "temporalnamespaces"}, obj.GetName(), nil)
	}
	return w.inner.Update(ctx, obj, opts...)
}

func TestUpdateRetriesOnConflict(t *testing.T) {
	ns := &temporalv1alpha1.TemporalNamespace{}
	ns.Name = "ns1"
	ns.Namespace = "default"
	ns.Generation = 1

	base := fake.NewClientBuilder().
		WithScheme(scheme(t)).
		WithObjects(ns).
		WithStatusSubresource(ns).
		Build()
	c := &conflictClient{Client: base, remaining: 2}

	status.Set(ns, temporalv1alpha1.ConditionReady, metav1.ConditionTrue, "Registered", "ok")
	if err := status.Update(context.Background(), c, ns); err != nil {
		t.Fatalf("Update returned %v, want nil", err)
	}
	if c.attempts != 3 {
		t.Errorf("status update attempts = %d, want 3 (2 conflicts then success)", c.attempts)
	}

	var got temporalv1alpha1.TemporalNamespace
	if err := base.Get(context.Background(), client.ObjectKeyFromObject(ns), &got); err != nil {
		t.Fatalf("re-reading namespace: %v", err)
	}
	if len(got.Status.Conditions) != 1 || got.Status.Conditions[0].Reason != "Registered" {
		t.Errorf("condition was not persisted after retry: %+v", got.Status.Conditions)
	}
}

func TestUpdateIgnoresNotFound(t *testing.T) {
	ns := &temporalv1alpha1.TemporalNamespace{}
	ns.Name = "gone"
	ns.Namespace = "default"

	c := fake.NewClientBuilder().WithScheme(scheme(t)).Build()
	if err := status.Update(context.Background(), c, ns); err != nil {
		t.Errorf("Update on a deleted object returned %v, want nil", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/status/`
Expected: FAIL — `no required module provides package github.com/bmorton/temporal-operator/internal/status`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/status/status.go` (license header, then):

```go
// Package status provides uniform condition and status-update handling for all
// Temporal CRDs, replacing the per-controller setReady/statusUpdate pairs.
package status

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Object is any Temporal CRD that reports conditions. All eight API types
// satisfy it via api/v1alpha1/accessors.go.
type Object interface {
	client.Object
	GetConditions() *[]metav1.Condition
	SetObservedGeneration(int64)
}

// Set records a condition, stamping the object's current generation on both the
// status and the condition. It only mutates the in-memory object; call Update to
// persist.
func Set(obj Object, condType string, s metav1.ConditionStatus, reason, message string) {
	gen := obj.GetGeneration()
	obj.SetObservedGeneration(gen)
	meta.SetStatusCondition(obj.GetConditions(), metav1.Condition{
		Type:               condType,
		Status:             s,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: gen,
	})
}

// IsTrue reports whether the named condition is currently True.
func IsTrue(obj Object, condType string) bool {
	return meta.IsStatusConditionTrue(*obj.GetConditions(), condType)
}

// Update persists the status subresource, retrying on conflict.
//
// On conflict we refresh only the resourceVersion from the API server and retry
// with our own status intact. We deliberately do not merge the server's status:
// each resource has exactly one controller writing its status, so a conflict
// means a stale read of our own earlier write, not a competing author.
//
// A NotFound error is returned as nil — the object was deleted while we
// reconciled, which is not a failure.
func Update(ctx context.Context, c client.Client, obj Object) error {
	key := client.ObjectKeyFromObject(obj)

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		updateErr := c.Status().Update(ctx, obj)
		if updateErr == nil || !apierrors.IsConflict(updateErr) {
			return updateErr
		}

		fresh, ok := obj.DeepCopyObject().(Object)
		if !ok {
			return updateErr
		}
		if getErr := c.Get(ctx, key, fresh); getErr != nil {
			return getErr
		}
		obj.SetResourceVersion(fresh.GetResourceVersion())
		return updateErr
	})

	return client.IgnoreNotFound(err)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/status/ -v`
Expected: PASS — `TestSetStampsObservedGeneration`, `TestUpdateRetriesOnConflict`, `TestUpdateIgnoresNotFound`.

- [ ] **Step 5: Commit**

```bash
git add internal/status/
git commit -s -m "feat(status): add shared condition helper with conflict retry"
```

---

### Task 3: internal/recovery bounded-retry primitive

**Files:**
- Create: `internal/recovery/recovery.go`
- Test: `internal/recovery/recovery_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Policy struct { Delays []time.Duration }`
  - `type Decision struct { Retry bool; After time.Duration; Attempt int }`
  - `func (p Policy) Next(attempts int) Decision`
  - `func DeadlineExceeded(since metav1.Time, deadline time.Duration, now time.Time) bool`
  - `func Remaining(since metav1.Time, deadline time.Duration, now time.Time) time.Duration`
  - `var SchemaJobPolicy = Policy{Delays: []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}}`

- [ ] **Step 1: Write the failing test**

Create `internal/recovery/recovery_test.go` (license header, then):

```go
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
		name      string
		attempts  int
		wantRetry bool
		wantAfter time.Duration
	}{
		{"first failure retries after 1m", 0, true, time.Minute},
		{"second failure retries after 5m", 1, true, 5 * time.Minute},
		{"third failure retries after 15m", 2, true, 15 * time.Minute},
		{"fourth failure gives up", 3, false, 0},
		{"beyond budget stays given up", 9, false, 0},
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
		})
	}
}

func TestPolicyNextNegativeAttemptsTreatedAsZero(t *testing.T) {
	p := recovery.Policy{Delays: []time.Duration{time.Minute}}
	got := p.Next(-1)
	if !got.Retry || got.After != time.Minute {
		t.Errorf("Next(-1) = %+v, want retry after 1m", got)
	}
}

func TestDeadlineExceeded(t *testing.T) {
	start := metav1.NewTime(time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC))

	tests := []struct {
		name     string
		now      time.Time
		deadline time.Duration
		want     bool
	}{
		{"before deadline", start.Add(4 * time.Minute), 5 * time.Minute, false},
		{"exactly at deadline", start.Add(5 * time.Minute), 5 * time.Minute, true},
		{"past deadline", start.Add(6 * time.Minute), 5 * time.Minute, true},
		{"zero start never expires", time.Time{}, 5 * time.Minute, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := start
			if tc.name == "zero start never expires" {
				s = metav1.Time{}
			}
			if got := recovery.DeadlineExceeded(s, tc.deadline, tc.now); got != tc.want {
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
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/recovery/`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write minimal implementation**

Create `internal/recovery/recovery.go` (license header, then):

```go
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
	return !now.Before(since.Time.Add(deadline))
}

// Remaining is the time left before the deadline elapses, floored at zero.
func Remaining(since metav1.Time, deadline time.Duration, now time.Time) time.Duration {
	if since.IsZero() {
		return deadline
	}
	left := since.Time.Add(deadline).Sub(now)
	if left < 0 {
		return 0
	}
	return left
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/recovery/ -v`
Expected: PASS — all subtests of `TestPolicyNext`, `TestDeadlineExceeded`, `TestRemaining`.

- [ ] **Step 5: Commit**

```bash
git add internal/recovery/
git commit -s -m "feat(recovery): add pure bounded-retry decision primitives"
```

---

### Task 4: internal/events recorder wrapper

**Files:**
- Create: `internal/events/events.go`
- Test: `internal/events/events_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type Recorder struct { ... }`
  - `func New(inner events.EventRecorder) *Recorder`
  - `func (r *Recorder) Normal(obj runtime.Object, reason, message string)`
  - `func (r *Recorder) Warning(obj runtime.Object, reason, message string)`
  - `func (r *Recorder) Forget(obj runtime.Object)`

**Background:** the existing recorder is `events.EventRecorder` from `k8s.io/client-go/tools/events`, obtained via `mgr.GetEventRecorder(name)` (`cmd/main.go:204`). Its `Eventf` takes `(regarding, related runtime.Object, eventtype, reason, action, note string, args ...any)`. The existing call site (`temporalcluster_upgrade.go:236`) passes `reason` twice, once as reason and once as action. This wrapper preserves that convention.

Deduplication matters here because stall and unreachable states are re-evaluated on every reconcile; without it a blocked upgrade would emit an Event per requeue for as long as it stays blocked.

- [ ] **Step 1: Write the failing test**

Create `internal/events/events_test.go` (license header, then):

```go
package events_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

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

func TestNilRecorderIsSafe(t *testing.T) {
	var r *opevents.Recorder
	r.Normal(newNamespace("a"), "Registered", "ok") // must not panic
}

var _ = metav1.Now
```

Add `"k8s.io/apimachinery/pkg/types"` to the test imports (used by `newNamespace`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/events/`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write minimal implementation**

Create `internal/events/events.go` (license header, then):

```go
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

// Forget clears the dedupe state for an object, so the next event is emitted
// even if it repeats the previous one. Call it when a resource leaves an
// abnormal state, so re-entering it is reported again.
func (r *Recorder) Forget(obj runtime.Object) {
	if r == nil {
		return
	}
	key, ok := objectKey(obj)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.last {
		if len(k) > len(key) && k[:len(key)] == key {
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/events/ -v`
Expected: PASS — six tests.

- [ ] **Step 5: Commit**

```bash
git add internal/events/
git commit -s -m "feat(events): add deduplicating event recorder wrapper"
```

---

### Task 5: Migrate the seven controllers onto internal/status

**Files:**
- Modify: `internal/controller/temporalnamespace_controller.go:298-311`
- Modify: `internal/controller/temporalschedule_controller.go:224-238`
- Modify: `internal/controller/temporalsearchattribute_controller.go:193-207`
- Modify: `internal/controller/temporalclusterclient_controller.go:116-135`
- Modify: `internal/controller/temporalclusterconnection_controller.go:416-430`
- Modify: `internal/controller/temporaldevserver_controller.go:121-135`
- Modify: `internal/controller/temporalworkflowrun_controller.go:257-270`
- Test: `internal/controller/status_migration_test.go` (create)

**Interfaces:**
- Consumes: `status.Set`, `status.Update` from Task 2.
- Produces: no new exported surface. Each controller keeps its existing `setReady(obj, status, reason, message)` and `statusUpdate(ctx, obj)` method names and signatures, so no call sites change — only the bodies are replaced.

**Why keep the method names:** every controller calls `r.setReady(...)` and `r.statusUpdate(...)` in dozens of places. Preserving the signatures makes this a body-only change with zero call-site churn, which keeps the diff reviewable and the behavior provably unchanged apart from the conflict fix.

- [ ] **Step 1: Write the failing test**

Create `internal/controller/status_migration_test.go` (license header, then):

```go
package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// TestSetReadyStampsObservedGeneration asserts every controller's setReady
// stamps observedGeneration on both the status and the condition. Before the
// migration to internal/status, TemporalClusterClient did not.
func TestSetReadyStampsObservedGeneration(t *testing.T) {
	const gen int64 = 11

	t.Run("TemporalNamespace", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalNamespace{}
		obj.Generation = gen
		(&TemporalNamespaceReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})

	t.Run("TemporalSchedule", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalSchedule{}
		obj.Generation = gen
		(&TemporalScheduleReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})

	t.Run("TemporalSearchAttribute", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalSearchAttribute{}
		obj.Generation = gen
		(&TemporalSearchAttributeReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})

	t.Run("TemporalClusterClient", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalClusterClient{}
		obj.Generation = gen
		(&TemporalClusterClientReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})

	t.Run("TemporalClusterConnection", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalClusterConnection{}
		obj.Generation = gen
		(&TemporalClusterConnectionReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})

	t.Run("TemporalDevServer", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalDevServer{}
		obj.Generation = gen
		(&TemporalDevServerReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})

	t.Run("TemporalWorkflowRun", func(t *testing.T) {
		obj := &temporalv1alpha1.TemporalWorkflowRun{}
		obj.Generation = gen
		(&TemporalWorkflowRunReconciler{}).setReady(obj, metav1.ConditionTrue, "R", "m")
		assertStamped(t, obj.Status.ObservedGeneration, obj.Status.Conditions, gen)
	})
}

func assertStamped(t *testing.T, observed int64, conds []metav1.Condition, want int64) {
	t.Helper()
	if observed != want {
		t.Errorf("status.observedGeneration = %d, want %d", observed, want)
	}
	if len(conds) != 1 {
		t.Fatalf("got %d conditions, want 1", len(conds))
	}
	if conds[0].ObservedGeneration != want {
		t.Errorf("condition.observedGeneration = %d, want %d", conds[0].ObservedGeneration, want)
	}
	if conds[0].Type != temporalv1alpha1.ConditionReady {
		t.Errorf("condition.type = %q, want %q", conds[0].Type, temporalv1alpha1.ConditionReady)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/controller/ -run TestSetReadyStampsObservedGeneration -v`
Expected: FAIL on the `TemporalClusterClient` subtest — `condition.observedGeneration = 0, want 11`. The other six already stamp, so they pass; this is the pre-existing inconsistency the migration removes.

- [ ] **Step 3: Write minimal implementation**

In each of the seven controller files, replace the `setReady` and `statusUpdate` bodies. Add `"github.com/bmorton/temporal-operator/internal/status"` to each file's import block, and remove now-unused `"k8s.io/apimachinery/pkg/api/meta"` imports where `meta` becomes unreferenced (the compiler will tell you which).

`internal/controller/temporalnamespace_controller.go` — replace lines 298-311:

```go
func (r *TemporalNamespaceReconciler) setReady(ns *temporalv1alpha1.TemporalNamespace, s metav1.ConditionStatus, reason, message string) {
	status.Set(ns, temporalv1alpha1.ConditionReady, s, reason, message)
}

func (r *TemporalNamespaceReconciler) statusUpdate(ctx context.Context, ns *temporalv1alpha1.TemporalNamespace) error {
	return status.Update(ctx, r.Client, ns)
}
```

Apply the identical shape to the other six, changing only the receiver type, parameter type, and variable name:

```go
// temporalschedule_controller.go (replaces lines 224-238)
func (r *TemporalScheduleReconciler) setReady(sched *temporalv1alpha1.TemporalSchedule, s metav1.ConditionStatus, reason, message string) {
	status.Set(sched, temporalv1alpha1.ConditionReady, s, reason, message)
}

func (r *TemporalScheduleReconciler) statusUpdate(ctx context.Context, sched *temporalv1alpha1.TemporalSchedule) error {
	return status.Update(ctx, r.Client, sched)
}

// temporalsearchattribute_controller.go (replaces lines 193-207)
func (r *TemporalSearchAttributeReconciler) setReady(sa *temporalv1alpha1.TemporalSearchAttribute, s metav1.ConditionStatus, reason, message string) {
	status.Set(sa, temporalv1alpha1.ConditionReady, s, reason, message)
}

func (r *TemporalSearchAttributeReconciler) statusUpdate(ctx context.Context, sa *temporalv1alpha1.TemporalSearchAttribute) error {
	return status.Update(ctx, r.Client, sa)
}

// temporalclusterclient_controller.go (replaces lines 116-135)
func (r *TemporalClusterClientReconciler) setReady(cc *temporalv1alpha1.TemporalClusterClient, s metav1.ConditionStatus, reason, message string) {
	status.Set(cc, temporalv1alpha1.ConditionReady, s, reason, message)
}

func (r *TemporalClusterClientReconciler) statusUpdate(ctx context.Context, cc *temporalv1alpha1.TemporalClusterClient) error {
	return status.Update(ctx, r.Client, cc)
}

// temporalclusterconnection_controller.go (replaces lines 416-430)
func (r *TemporalClusterConnectionReconciler) setReady(conn *temporalv1alpha1.TemporalClusterConnection, s metav1.ConditionStatus, reason, message string) {
	status.Set(conn, temporalv1alpha1.ConditionReady, s, reason, message)
}

func (r *TemporalClusterConnectionReconciler) statusUpdate(ctx context.Context, conn *temporalv1alpha1.TemporalClusterConnection) error {
	return status.Update(ctx, r.Client, conn)
}

// temporaldevserver_controller.go (replaces lines 121-135)
func (r *TemporalDevServerReconciler) setReady(dev *temporalv1alpha1.TemporalDevServer, s metav1.ConditionStatus, reason, message string) {
	status.Set(dev, temporalv1alpha1.ConditionReady, s, reason, message)
}

func (r *TemporalDevServerReconciler) statusUpdate(ctx context.Context, dev *temporalv1alpha1.TemporalDevServer) error {
	return status.Update(ctx, r.Client, dev)
}

// temporalworkflowrun_controller.go (replaces lines 257-270)
func (r *TemporalWorkflowRunReconciler) setReady(run *temporalv1alpha1.TemporalWorkflowRun, s metav1.ConditionStatus, reason, message string) {
	status.Set(run, temporalv1alpha1.ConditionReady, s, reason, message)
}

func (r *TemporalWorkflowRunReconciler) statusUpdate(ctx context.Context, run *temporalv1alpha1.TemporalWorkflowRun) error {
	return status.Update(ctx, r.Client, run)
}
```

- [ ] **Step 4: Run the full controller suite**

Run: `make test`
Expected: PASS. `TestSetReadyStampsObservedGeneration` passes all seven subtests, and every pre-existing controller test still passes — the migration must not change any observable behavior except the added conflict retry and the ClusterClient generation stamp.

- [ ] **Step 5: Verify no swallowed conflicts remain**

Run: `grep -rn "IsConflict" internal/controller/ | grep -v _test`
Expected: no output. Every conflict path now goes through `status.Update`.

- [ ] **Step 6: Commit**

```bash
git add internal/controller/
git commit -s -m "refactor(controller): migrate status handling to internal/status

Replaces seven near-identical setReady/statusUpdate pairs with the shared
helper. Fixes the swallowed 409 Conflict in the TemporalClusterClient
reconciler and the missing observedGeneration stamp that came with it."
```

---

### Task 6: Phase 1 gate

- [ ] **Step 1: Run every gate**

```bash
make build && make test && make lint
```

Expected: all three succeed. Coverage in `cover.out` should be at or above the 48.1% baseline — this phase adds tested code and removes untested duplication.

- [ ] **Step 2: Confirm no API or chart regeneration is outstanding**

```bash
make generate manifests && git status --short
```

Expected: no modified files. Phase 1 adds methods, not fields, so no CRD schema changes.

---

# Phase 2 — Recovery semantics

The substance. Each task makes one silent failure loud and bounded.

### Task 7: Upgrade stall detection

**Files:**
- Modify: `api/v1alpha1/temporalcluster_types.go:235-250` (`UpgradeStatus`)
- Modify: `api/v1alpha1/conditions.go` (new reasons)
- Modify: `internal/controller/temporalcluster_upgrade.go:139-185`
- Test: `internal/controller/temporalcluster_upgrade_test.go`

**Interfaces:**
- Consumes: `recovery.DeadlineExceeded` (Task 3), `status.Set` (Task 2).
- Produces:
  - `UpgradeStatus.PhaseStartedAt *metav1.Time`, `.StalledService string`, `.Message string`
  - `temporalv1alpha1.ReasonUpgradeStalled = "UpgradeStalled"`
  - `temporalv1alpha1.ReasonRolloutStalled = "RolloutStalled"`
  - `var upgradePhaseTimeout = 15 * time.Minute`
  - `var upgradeStallRecheck = 1 * time.Minute`
  - `func (r *TemporalClusterReconciler) upgradeStalled(cluster) bool`

- [ ] **Step 1: Write the failing test**

Append to `internal/controller/temporalcluster_upgrade_test.go`:

```go
func TestAdvanceRollingPhaseMarksStallAfterTimeout(t *testing.T) {
	origTimeout := upgradePhaseTimeout
	upgradePhaseTimeout = 50 * time.Millisecond
	defer func() { upgradePhaseTimeout = origTimeout }()

	cluster := &temporalv1alpha1.TemporalCluster{}
	cluster.Name = "tc"
	cluster.Namespace = "default"
	cluster.Spec.Version = "1.31.1"
	cluster.Status.Version = "1.30.4"

	entered := metav1.NewTime(time.Now().Add(-time.Second))
	cluster.Status.Upgrade = &temporalv1alpha1.UpgradeStatus{
		FromVersion:    "1.30.4",
		ToVersion:      "1.31.1",
		Phase:          upgradeRollingFrontend,
		PhaseStartedAt: &entered,
	}

	// No Deployment exists, so serviceRolledOut is false: the phase is stalled.
	r := &TemporalClusterReconciler{Client: fake.NewClientBuilder().WithScheme(testScheme(t)).Build()}
	r.advanceUpgrade(context.Background(), cluster)

	up := cluster.Status.Upgrade
	if up == nil {
		t.Fatal("upgrade status was cleared, want it retained while stalled")
	}
	if up.Phase != upgradeRollingFrontend {
		t.Errorf("phase = %q, want it to stay at %q", up.Phase, upgradeRollingFrontend)
	}
	if up.StalledService != resources.ServiceFrontend {
		t.Errorf("stalledService = %q, want %q", up.StalledService, resources.ServiceFrontend)
	}
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionUpgradeBlocked) {
		t.Error("UpgradeBlocked condition is not True")
	}
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionDegraded) {
		t.Error("Degraded condition is not True")
	}
	if !r.upgradeStalled(cluster) {
		t.Error("upgradeStalled reported false for a stalled upgrade")
	}
}

func TestAdvanceRollingPhaseDoesNotStallBeforeTimeout(t *testing.T) {
	origTimeout := upgradePhaseTimeout
	upgradePhaseTimeout = time.Hour
	defer func() { upgradePhaseTimeout = origTimeout }()

	cluster := &temporalv1alpha1.TemporalCluster{}
	cluster.Name = "tc"
	cluster.Namespace = "default"
	entered := metav1.NewTime(time.Now())
	cluster.Status.Upgrade = &temporalv1alpha1.UpgradeStatus{
		FromVersion:    "1.30.4",
		ToVersion:      "1.31.1",
		Phase:          upgradeRollingFrontend,
		PhaseStartedAt: &entered,
	}

	r := &TemporalClusterReconciler{Client: fake.NewClientBuilder().WithScheme(testScheme(t)).Build()}
	r.advanceUpgrade(context.Background(), cluster)

	if meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionUpgradeBlocked) {
		t.Error("UpgradeBlocked set before the phase timeout elapsed")
	}
	if cluster.Status.Upgrade.StalledService != "" {
		t.Errorf("stalledService = %q, want empty before timeout", cluster.Status.Upgrade.StalledService)
	}
}

func TestStallClearsWhenServiceRollsOut(t *testing.T) {
	origTimeout := upgradePhaseTimeout
	upgradePhaseTimeout = 50 * time.Millisecond
	defer func() { upgradePhaseTimeout = origTimeout }()

	cluster := &temporalv1alpha1.TemporalCluster{}
	cluster.Name = "tc"
	cluster.Namespace = "default"
	entered := metav1.NewTime(time.Now().Add(-time.Second))
	cluster.Status.Upgrade = &temporalv1alpha1.UpgradeStatus{
		FromVersion:    "1.30.4",
		ToVersion:      "1.31.1",
		Phase:          upgradeRollingFrontend,
		PhaseStartedAt: &entered,
		StalledService: resources.ServiceFrontend,
	}
	status.Set(cluster, temporalv1alpha1.ConditionUpgradeBlocked, metav1.ConditionTrue, temporalv1alpha1.ReasonUpgradeStalled, "stalled")

	dep := rolledOutDeployment(cluster, resources.ServiceFrontend, "1.31.1")
	r := &TemporalClusterReconciler{Client: fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(dep).Build()}
	r.advanceUpgrade(context.Background(), cluster)

	if cluster.Status.Upgrade.Phase != upgradeRollingHistory {
		t.Errorf("phase = %q, want %q after rollout completed", cluster.Status.Upgrade.Phase, upgradeRollingHistory)
	}
	if cluster.Status.Upgrade.StalledService != "" {
		t.Errorf("stalledService = %q, want cleared", cluster.Status.Upgrade.StalledService)
	}
	if meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionUpgradeBlocked) {
		t.Error("UpgradeBlocked is still True after the service rolled out")
	}
}

// rolledOutDeployment builds a Deployment that serviceRolledOut reports as
// fully rolled out at the given version.
func rolledOutDeployment(cluster *temporalv1alpha1.TemporalCluster, component, version string) *appsv1.Deployment {
	one := int32(1)
	dep := &appsv1.Deployment{}
	dep.Name = resources.DeploymentName(cluster.Name, component)
	dep.Namespace = cluster.Namespace
	dep.Generation = 1
	dep.Spec.Replicas = &one
	dep.Spec.Template.Labels = map[string]string{resources.LabelVersion: version}
	dep.Status.ObservedGeneration = 1
	dep.Status.UpdatedReplicas = 1
	dep.Status.ReadyReplicas = 1
	return dep
}
```

If `testScheme` does not already exist in the controller test package, add it to `fakes_test.go`:

```go
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := temporalv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("adding temporal scheme: %v", err)
	}
	if err := appsv1.AddToScheme(s); err != nil {
		t.Fatalf("adding apps scheme: %v", err)
	}
	if err := batchv1.AddToScheme(s); err != nil {
		t.Fatalf("adding batch scheme: %v", err)
	}
	if err := corev1.AddToScheme(s); err != nil {
		t.Fatalf("adding core scheme: %v", err)
	}
	return s
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/controller/ -run 'TestAdvanceRollingPhase|TestStallClears'`
Expected: FAIL — compile error, `up.PhaseStartedAt undefined` and `upgradePhaseTimeout undefined`.

- [ ] **Step 3: Add the status fields**

In `api/v1alpha1/temporalcluster_types.go`, replace the `UpgradeStatus` struct (lines 235-250) with:

```go
// UpgradeStatus reports the state of an in-progress version upgrade.
type UpgradeStatus struct {
	// +optional
	FromVersion string `json:"fromVersion,omitempty"`
	// +optional
	ToVersion string `json:"toVersion,omitempty"`
	// +optional
	Phase string `json:"phase,omitempty"`
	// Rollbackable is true until schema migration begins, after which a
	// rollback is no longer safe.
	// +optional
	Rollbackable bool `json:"rollbackable,omitempty"`
	// +optional
	StartedAt *metav1.Time `json:"startedAt,omitempty"`
	// PhaseStartedAt is when the current phase was entered. It is the basis for
	// stall detection.
	// +optional
	PhaseStartedAt *metav1.Time `json:"phaseStartedAt,omitempty"`
	// StalledService names the service whose rollout has exceeded the phase
	// timeout, or is empty when the upgrade is progressing normally.
	// +optional
	StalledService string `json:"stalledService,omitempty"`
	// Message explains why the upgrade is not progressing, when it is not.
	// +optional
	Message string `json:"message,omitempty"`
}
```

In `api/v1alpha1/conditions.go`, add to the reasons block:

```go
	// ReasonUpgradeStalled indicates an upgrade phase exceeded its timeout.
	ReasonUpgradeStalled = "UpgradeStalled"
	// ReasonRolloutStalled indicates a service Deployment did not roll out in time.
	ReasonRolloutStalled = "RolloutStalled"
	// ReasonUpgradeProgressing indicates an upgrade is advancing normally.
	ReasonUpgradeProgressing = "UpgradeProgressing"
```

- [ ] **Step 4: Implement stall detection**

In `internal/controller/temporalcluster_upgrade.go`, add `"time"` and the `status`/`recovery` packages to the imports, then add below the phase constants:

```go
// upgradePhaseTimeout is how long a single upgrade phase may run before it is
// reported as stalled. It is a var so tests can shorten it.
var upgradePhaseTimeout = 15 * time.Minute

// upgradeStallRecheck is how often a stalled upgrade is re-examined. An
// explicit requeue is required because a stall whose cause is external (an
// unreachable registry, a pending PVC) may produce no watched-object event at
// all, and the blocked state would otherwise itself be a stall.
var upgradeStallRecheck = 1 * time.Minute
```

Replace `advanceUpgrade`'s phase-entry bookkeeping and `advanceRollingPhase` (lines 139-185) with:

```go
func (r *TemporalClusterReconciler) advanceUpgrade(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster) {
	up := cluster.Status.Upgrade
	reachable := meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionPersistenceReachable)
	schemaReady := meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionSchemaReady)

	advance := func(next string) {
		up.Phase = next
		now := metav1.Now()
		up.PhaseStartedAt = &now
		up.StalledService = ""
		up.Message = ""
		r.clearUpgradeStall(cluster)
		r.event(cluster, "UpgradePhase", "upgrade entered phase "+next)
	}

	// An in-flight upgrade is work in progress. Progressing is declared in
	// conditions.go but was never set by any controller before this change.
	status.Set(cluster, temporalv1alpha1.ConditionProgressing, metav1.ConditionTrue,
		temporalv1alpha1.ReasonUpgradeProgressing,
		fmt.Sprintf("upgrading from %s to %s (phase %s)", up.FromVersion, up.ToVersion, up.Phase))

	if up.PhaseStartedAt == nil {
		now := metav1.Now()
		up.PhaseStartedAt = &now
	}

	switch up.Phase {
	case upgradePending:
		advance(upgradePreflight)
	case upgradePreflight:
		if reachable {
			advance(upgradeSchemaMigrating)
		}
	case upgradeSchemaMigrating:
		// Rollbackable goes false here, not at preflight: this is the first
		// phase that can apply an irreversible schema change, which is what the
		// field's documented contract says.
		up.Rollbackable = false
		if schemaReady {
			advance(upgradeRollingFrontend)
		}
	case upgradePostUpgrade:
		advance(upgradeComplete)
	case upgradeComplete:
		cluster.Status.Version = up.ToVersion
		r.event(cluster, "UpgradeComplete", "upgrade to "+up.ToVersion+" complete")
		r.clearUpgradeStall(cluster)
		status.Set(cluster, temporalv1alpha1.ConditionProgressing, metav1.ConditionFalse,
			temporalv1alpha1.ReasonAllServicesReady, "upgrade complete")
		cluster.Status.Upgrade = nil
	default:
		r.advanceRollingPhase(ctx, cluster, advance)
	}
}

// advanceRollingPhase advances a per-service rolling phase once the current
// service has fully rolled out at the target version, or marks the upgrade
// stalled once the phase exceeds upgradePhaseTimeout.
func (r *TemporalClusterReconciler) advanceRollingPhase(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, advance func(string)) {
	up := cluster.Status.Upgrade
	svc, ok := rollingPhaseService[up.Phase]
	if !ok {
		return
	}

	if r.serviceRolledOut(ctx, cluster, svc, up.ToVersion) {
		advance(r.nextRollingPhase(cluster, up.Phase))
		return
	}

	if up.PhaseStartedAt == nil || !recovery.DeadlineExceeded(*up.PhaseStartedAt, upgradePhaseTimeout, time.Now()) {
		return
	}

	message := fmt.Sprintf("service %q has not rolled out to %s within %s: %s",
		svc, up.ToVersion, upgradePhaseTimeout, r.rolloutDetail(ctx, cluster, svc))
	up.StalledService = svc
	up.Message = message
	status.Set(cluster, temporalv1alpha1.ConditionUpgradeBlocked, metav1.ConditionTrue,
		temporalv1alpha1.ReasonUpgradeStalled, message)
	status.Set(cluster, temporalv1alpha1.ConditionDegraded, metav1.ConditionTrue,
		temporalv1alpha1.ReasonRolloutStalled, message)
	r.warnEvent(cluster, temporalv1alpha1.ReasonUpgradeStalled, message)
}

// clearUpgradeStall returns the blocked/degraded conditions to False. The stall
// is a report, not a latch: it clears as soon as the rollout completes.
func (r *TemporalClusterReconciler) clearUpgradeStall(cluster *temporalv1alpha1.TemporalCluster) {
	status.Set(cluster, temporalv1alpha1.ConditionUpgradeBlocked, metav1.ConditionFalse,
		temporalv1alpha1.ReasonUpgradeProgressing, "upgrade is progressing")
	status.Set(cluster, temporalv1alpha1.ConditionDegraded, metav1.ConditionFalse,
		temporalv1alpha1.ReasonUpgradeProgressing, "upgrade is progressing")
}

// rolloutDetail reports the Deployment's own reason for not being rolled out,
// so the condition message names the actual cause rather than just the symptom.
func (r *TemporalClusterReconciler) rolloutDetail(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, component string) string {
	var dep appsv1.Deployment
	name := resources.DeploymentName(cluster.Name, component)
	if err := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, &dep); err != nil {
		return "deployment not found"
	}
	for _, c := range dep.Status.Conditions {
		if c.Type == appsv1.DeploymentProgressing && c.Status == corev1.ConditionFalse {
			return fmt.Sprintf("%s: %s", c.Reason, c.Message)
		}
		if c.Type == appsv1.DeploymentAvailable && c.Status == corev1.ConditionFalse {
			return fmt.Sprintf("%s: %s", c.Reason, c.Message)
		}
	}
	return fmt.Sprintf("%d/%d replicas ready", dep.Status.ReadyReplicas, dep.Status.UpdatedReplicas)
}

// upgradeStalled reports whether an upgrade is currently blocked, so the
// reconciler can requeue to re-examine it.
func (r *TemporalClusterReconciler) upgradeStalled(cluster *temporalv1alpha1.TemporalCluster) bool {
	return cluster.Status.Upgrade != nil && cluster.Status.Upgrade.StalledService != ""
}
```

Add a warning-event helper next to the existing `event` method (which stays as-is for Normal events):

```go
func (r *TemporalClusterReconciler) warnEvent(cluster *temporalv1alpha1.TemporalCluster, reason, message string) {
	if r.Recorder != nil {
		r.Recorder.Eventf(cluster, nil, corev1.EventTypeWarning, reason, reason, message)
	}
}
```

Add `corev1 "k8s.io/api/core/v1"` and `"fmt"` to the imports.

- [ ] **Step 5: Wire the requeue**

In `internal/controller/temporalcluster_controller.go`, find the final `return ctrl.Result{...}, nil` of `Reconcile` and ensure a stalled upgrade requeues. Insert immediately before it:

```go
	if r.upgradeStalled(&cluster) {
		return ctrl.Result{RequeueAfter: upgradeStallRecheck}, nil
	}
```

- [ ] **Step 6: Regenerate and run tests**

```bash
make generate manifests
go test ./internal/controller/ -run 'TestAdvanceRollingPhase|TestStallClears' -v
```

Expected: PASS — three tests. `git status --short` shows modified `zz_generated.deepcopy.go` and `config/crd/bases/temporal.bmor10.com_temporalclusters.yaml`.

- [ ] **Step 7: Run the full suite**

Run: `make test`
Expected: PASS. Existing upgrade tests must still pass; if one asserts `Rollbackable == false` immediately after preflight, update it to assert `true` — the old assertion encoded the bug, and the field's own doc comment (`temporalcluster_types.go`) documents the corrected behavior.

- [ ] **Step 8: Commit**

```bash
git add api/ config/crd/ internal/controller/
git commit -s -m "feat(controller): detect and report stalled upgrade phases

A rolling phase that exceeds upgradePhaseTimeout now sets UpgradeBlocked
and Degraded with the Deployment's own failure reason, emits a warning
event, and requeues. The condition clears automatically when the rollout
completes. Also moves Rollbackable=false to the schema-migrating phase,
matching the field's documented contract."
```

---

### Task 8: Mid-upgrade version guard

**Files:**
- Modify: `internal/webhook/v1alpha1/temporalcluster_webhook.go:272-313`
- Test: `internal/webhook/v1alpha1/temporalcluster_webhook_test.go`

**Interfaces:**
- Consumes: `UpgradeStatus.FromVersion`, `.Rollbackable` (Task 7).
- Produces:
  - `func validateVersionChangeDuringUpgrade(oldCluster, newCluster *temporalv1alpha1.TemporalCluster, specPath *field.Path) field.ErrorList`
  - `func upgradeRevertWarnings(oldCluster, newCluster *temporalv1alpha1.TemporalCluster) admission.Warnings`
  - `validateUpgradePath` gains an upgrade-aware baseline.

**Background:** `ValidateUpdate` receives the full old object including `status`, so `oldCluster.Status.Upgrade` is the in-flight upgrade. Mid-upgrade, `oldCluster.Spec.Version` is already the *target*, which is why the existing `validateUpgradePath` validates from the wrong baseline.

- [ ] **Step 1: Write the failing test**

Append to `internal/webhook/v1alpha1/temporalcluster_webhook_test.go`:

```go
func upgradingCluster(from, to string, rollbackable bool) *temporalv1alpha1.TemporalCluster {
	c := validCluster() // existing helper in this test file
	c.Spec.Version = to
	c.Status.Version = from
	c.Status.Upgrade = &temporalv1alpha1.UpgradeStatus{
		FromVersion:  from,
		ToVersion:    to,
		Phase:        "RollingFrontend",
		Rollbackable: rollbackable,
	}
	return c
}

func TestValidateUpdateRejectsThirdVersionMidUpgrade(t *testing.T) {
	oldCluster := upgradingCluster("1.30.4", "1.31.1", false)
	newCluster := oldCluster.DeepCopy()
	newCluster.Spec.Version = "1.31.2"

	v := &TemporalClusterCustomValidator{}
	_, err := v.ValidateUpdate(context.Background(), oldCluster, newCluster)
	if err == nil {
		t.Fatal("ValidateUpdate accepted a third version mid-upgrade, want rejection")
	}
	if !strings.Contains(err.Error(), "upgrade in progress") {
		t.Errorf("error = %q, want it to mention the in-progress upgrade", err.Error())
	}
}

func TestValidateUpdateAllowsRevertToFromVersion(t *testing.T) {
	oldCluster := upgradingCluster("1.30.4", "1.31.1", true)
	newCluster := oldCluster.DeepCopy()
	newCluster.Spec.Version = "1.30.4"

	v := &TemporalClusterCustomValidator{}
	warnings, err := v.ValidateUpdate(context.Background(), oldCluster, newCluster)
	if err != nil {
		t.Fatalf("ValidateUpdate rejected a revert to fromVersion: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("got warnings %v, want none while still rollbackable", warnings)
	}
}

func TestValidateUpdateWarnsOnRevertAfterSchemaMigration(t *testing.T) {
	oldCluster := upgradingCluster("1.30.4", "1.31.1", false)
	newCluster := oldCluster.DeepCopy()
	newCluster.Spec.Version = "1.30.4"

	v := &TemporalClusterCustomValidator{}
	warnings, err := v.ValidateUpdate(context.Background(), oldCluster, newCluster)
	if err != nil {
		t.Fatalf("ValidateUpdate rejected the documented escape hatch: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("got no warnings, want one about the migrated schema")
	}
	if !strings.Contains(warnings[0], "schema") {
		t.Errorf("warning = %q, want it to mention the migrated schema", warnings[0])
	}
}

func TestValidateUpgradePathUsesFromVersionMidUpgrade(t *testing.T) {
	// A cluster mid-upgrade from 1.30.4 to 1.31.1. Reverting is validated
	// against fromVersion, not against spec.Version (which is already 1.31.1).
	oldCluster := upgradingCluster("1.30.4", "1.31.1", true)
	newCluster := oldCluster.DeepCopy()
	newCluster.Spec.Version = "1.30.4"

	errs := validateUpgradePath(oldCluster, newCluster, field.NewPath("spec"))
	if len(errs) != 0 {
		t.Errorf("validateUpgradePath returned %v for a revert to fromVersion, want none", errs)
	}
}

func TestValidateUpdateAllowsVersionChangeWhenNotUpgrading(t *testing.T) {
	oldCluster := validCluster()
	oldCluster.Spec.Version = "1.30.4"
	oldCluster.Status.Version = "1.30.4"
	newCluster := oldCluster.DeepCopy()
	newCluster.Spec.Version = "1.31.1"

	v := &TemporalClusterCustomValidator{}
	if _, err := v.ValidateUpdate(context.Background(), oldCluster, newCluster); err != nil {
		t.Fatalf("ValidateUpdate rejected a normal upgrade start: %v", err)
	}
}
```

Add `"strings"` to the test imports if absent. If the existing test file has no `validCluster()` helper, use whatever fixture builder it already defines and adapt these three lines accordingly.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/webhook/v1alpha1/ -run 'TestValidateUpdate|TestValidateUpgradePath'`
Expected: FAIL — `ValidateUpdate accepted a third version mid-upgrade, want rejection`.

- [ ] **Step 3: Write minimal implementation**

In `internal/webhook/v1alpha1/temporalcluster_webhook.go`, replace `ValidateUpdate` (lines 272-288) with:

```go
// ValidateUpdate implements admission.Validator.
func (v *TemporalClusterCustomValidator) ValidateUpdate(_ context.Context, oldCluster, newCluster *temporalv1alpha1.TemporalCluster) (admission.Warnings, error) {
	temporalclusterlog.Info("Validation for TemporalCluster upon update", "name", newCluster.GetName())

	errs := v.validateSpec(newCluster)
	specPath := field.NewPath("spec")

	errs = append(errs, validateShardCountImmutable(oldCluster, newCluster, specPath)...)
	errs = append(errs, validateVersionChangeDuringUpgrade(oldCluster, newCluster, specPath)...)
	errs = append(errs, validateUpgradePath(oldCluster, newCluster, specPath)...)
	errs = append(errs, validateStoreDriverImmutable(oldCluster, newCluster, specPath)...)
	errs = append(errs, validateClusterMetadataImmutable(oldCluster, newCluster, specPath)...)

	if len(errs) > 0 {
		return nil, errs.ToAggregate()
	}
	return upgradeRevertWarnings(oldCluster, newCluster), nil
}
```

Replace `validateUpgradePath` (lines 304-313) and add the two new functions:

```go
// upgradeBaseline is the version the cluster's oldest services are actually
// running. Mid-upgrade that is status.upgrade.fromVersion, not spec.version —
// spec.version is already the target, so validating from it would check a path
// the cluster is not taking.
func upgradeBaseline(oldCluster *temporalv1alpha1.TemporalCluster) string {
	if up := oldCluster.Status.Upgrade; up != nil && up.FromVersion != "" {
		return up.FromVersion
	}
	return oldCluster.Spec.Version
}

func validateUpgradePath(oldCluster, newCluster *temporalv1alpha1.TemporalCluster, specPath *field.Path) field.ErrorList {
	baseline := upgradeBaseline(oldCluster)
	if newCluster.Spec.Version == baseline || newCluster.Spec.Version == oldCluster.Spec.Version {
		return nil
	}
	allowed, err := temporal.CanUpgrade(baseline, newCluster.Spec.Version)
	if err != nil || !allowed {
		return field.ErrorList{field.Invalid(specPath.Child("version"), newCluster.Spec.Version,
			fmt.Sprintf("%s: cannot upgrade from %s to %s", temporalv1alpha1.ReasonUpgradePathInvalid, baseline, newCluster.Spec.Version))}
	}
	return nil
}

// validateVersionChangeDuringUpgrade rejects retargeting an in-flight upgrade.
// The single exception is a revert to status.upgrade.fromVersion, which cancels
// the upgrade and is the documented way out of a stalled one.
func validateVersionChangeDuringUpgrade(oldCluster, newCluster *temporalv1alpha1.TemporalCluster, specPath *field.Path) field.ErrorList {
	up := oldCluster.Status.Upgrade
	if up == nil || newCluster.Spec.Version == oldCluster.Spec.Version {
		return nil
	}
	if newCluster.Spec.Version == up.FromVersion {
		return nil
	}
	return field.ErrorList{field.Invalid(specPath.Child("version"), newCluster.Spec.Version,
		fmt.Sprintf("%s: an upgrade from %s to %s is in progress (phase %s); set version back to %s to cancel it, or wait for the upgrade to finish",
			temporalv1alpha1.ReasonUpgradePathInvalid, up.FromVersion, up.ToVersion, up.Phase, up.FromVersion))}
}

// upgradeRevertWarnings warns when a revert is accepted after the schema has
// already migrated. The revert is still allowed: it is a human decision made
// with information the operator does not have.
func upgradeRevertWarnings(oldCluster, newCluster *temporalv1alpha1.TemporalCluster) admission.Warnings {
	up := oldCluster.Status.Upgrade
	if up == nil || up.Rollbackable || newCluster.Spec.Version != up.FromVersion {
		return nil
	}
	return admission.Warnings{fmt.Sprintf(
		"reverting to %s after the persistence schema has already migrated for %s: Temporal schema migrations are forward-only, so the older server binaries will run against the newer schema; verify compatibility before proceeding",
		up.FromVersion, up.ToVersion)}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/webhook/v1alpha1/ -run 'TestValidateUpdate|TestValidateUpgradePath' -v`
Expected: PASS — five tests.

- [ ] **Step 5: Run the full suite and commit**

```bash
make test
git add internal/webhook/
git commit -s -m "feat(webhook): guard spec.version changes during an in-flight upgrade

Rejects retargeting a running upgrade, allows a revert to fromVersion as
the documented escape hatch, and warns when that revert happens after the
schema has migrated. Also fixes validateUpgradePath, which validated from
spec.version -- already the target mid-upgrade -- instead of fromVersion."
```

---

### Task 9: Schema Job bounded recreation

**Files:**
- Modify: `api/v1alpha1/persistence_types.go:228-248` (`PersistenceStatus`)
- Modify: `api/v1alpha1/conditions.go` (new reason)
- Modify: `internal/controller/temporalcluster_persistence.go:243-275`
- Test: `internal/controller/temporalcluster_persistence_test.go`

**Interfaces:**
- Consumes: `recovery.SchemaJobPolicy`, `recovery.Policy.Next` (Task 3); `status.Set` (Task 2).
- Produces:
  - `PersistenceStatus.SchemaAttempts map[string]SchemaAttemptStatus`
  - `type SchemaAttemptStatus struct { Count int32; FirstFailedAt *metav1.Time; LastError string }`
  - `temporalv1alpha1.ReasonSchemaMigrationFailed = "SchemaMigrationFailed"`
  - `func (r *TemporalClusterReconciler) handleFailedSchemaJob(ctx, cluster, t schemaTarget, action resources.SchemaAction) (storeResult, error)`
  - `storeResult` gains `requeueAfter time.Duration`

**Safety note for the implementer:** recreating these Jobs is safe because they run `setup-schema -v 0.0` and `update-schema -d <dir>` with **no `--overwrite`** (`internal/resources/schemajob.go:151,153`). `update-schema` applies only migrations above the current version. Do not add `--overwrite` under any circumstances.

- [ ] **Step 1: Write the failing test**

Append to `internal/controller/temporalcluster_persistence_test.go`:

```go
func TestHandleFailedSchemaJobRecreatesWithinBudget(t *testing.T) {
	cluster := clusterWithSQLPersistence() // existing helper in this test file
	failed := failedSchemaJob(cluster, temporalv1alpha1.StoreDefault, resources.ActionUpdate)

	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(cluster, failed).Build()
	r := &TemporalClusterReconciler{Client: c, Scheme: c.Scheme()}

	target := schemaTarget{store: temporalv1alpha1.StoreDefault}
	res, err := r.handleFailedSchemaJob(context.Background(), cluster, target, resources.ActionUpdate)
	if err != nil {
		t.Fatalf("handleFailedSchemaJob returned %v, want nil", err)
	}
	if res.failed {
		t.Error("result marked terminal on the first failure, want a retry")
	}
	if res.requeueAfter != time.Minute {
		t.Errorf("requeueAfter = %v, want 1m for the first attempt", res.requeueAfter)
	}

	attempts := cluster.Status.Persistence.SchemaAttempts[string(temporalv1alpha1.StoreDefault)]
	if attempts.Count != 1 {
		t.Errorf("attempt count = %d, want 1", attempts.Count)
	}
	if attempts.FirstFailedAt == nil {
		t.Error("firstFailedAt was not recorded")
	}

	var job batchv1.Job
	err = c.Get(context.Background(), client.ObjectKeyFromObject(failed), &job)
	if !apierrors.IsNotFound(err) {
		t.Errorf("failed Job still exists (err=%v), want it deleted so the next reconcile recreates it", err)
	}
}

func TestHandleFailedSchemaJobGivesUpAfterBudget(t *testing.T) {
	cluster := clusterWithSQLPersistence()
	first := metav1.NewTime(time.Now().Add(-time.Hour))
	cluster.Status.Persistence.SchemaAttempts = map[string]temporalv1alpha1.SchemaAttemptStatus{
		string(temporalv1alpha1.StoreDefault): {Count: 3, FirstFailedAt: &first},
	}
	failed := failedSchemaJob(cluster, temporalv1alpha1.StoreDefault, resources.ActionUpdate)

	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(cluster, failed).Build()
	r := &TemporalClusterReconciler{Client: c, Scheme: c.Scheme()}

	target := schemaTarget{store: temporalv1alpha1.StoreDefault}
	res, err := r.handleFailedSchemaJob(context.Background(), cluster, target, resources.ActionUpdate)
	if err != nil {
		t.Fatalf("handleFailedSchemaJob returned %v, want nil", err)
	}
	if !res.failed {
		t.Error("result is not terminal after the attempt budget is exhausted")
	}
	if res.requeueAfter != 0 {
		t.Errorf("requeueAfter = %v, want 0 once given up", res.requeueAfter)
	}
	if !meta.IsStatusConditionTrue(cluster.Status.Conditions, temporalv1alpha1.ConditionDegraded) {
		t.Error("Degraded condition is not True after giving up")
	}

	// The Job is retained on give-up so its pod logs remain inspectable.
	var job batchv1.Job
	if err := c.Get(context.Background(), client.ObjectKeyFromObject(failed), &job); err != nil {
		t.Errorf("failed Job was deleted on give-up (%v), want it retained for debugging", err)
	}
}

func TestSchemaAttemptsResetOnSuccess(t *testing.T) {
	cluster := clusterWithSQLPersistence()
	first := metav1.NewTime(time.Now())
	cluster.Status.Persistence.SchemaAttempts = map[string]temporalv1alpha1.SchemaAttemptStatus{
		string(temporalv1alpha1.StoreDefault): {Count: 2, FirstFailedAt: &first},
	}

	resetSchemaAttempts(cluster, temporalv1alpha1.StoreDefault)

	if _, present := cluster.Status.Persistence.SchemaAttempts[string(temporalv1alpha1.StoreDefault)]; present {
		t.Error("attempt record survived a success, want it cleared")
	}
}

// failedSchemaJob builds a Job in the state classifyJob reports as jobFailed.
func failedSchemaJob(cluster *temporalv1alpha1.TemporalCluster, store temporalv1alpha1.StoreName, action resources.SchemaAction) *batchv1.Job {
	job := &batchv1.Job{}
	job.Name = resources.SchemaJobName(cluster.Name, store, action)
	job.Namespace = cluster.Namespace
	job.Status.Failed = 4
	job.Status.Conditions = []batchv1.JobCondition{{
		Type:    batchv1.JobFailed,
		Status:  corev1.ConditionTrue,
		Reason:  "BackoffLimitExceeded",
		Message: "Job has reached the specified backoff limit",
	}}
	return job
}
```

Adapt `clusterWithSQLPersistence()` and the `schemaTarget` literal to whatever the existing test file already provides — read `temporalcluster_persistence_test.go` first and reuse its fixtures rather than duplicating them.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/controller/ -run 'TestHandleFailedSchemaJob|TestSchemaAttemptsReset'`
Expected: FAIL — `undefined: r.handleFailedSchemaJob` and `undefined: temporalv1alpha1.SchemaAttemptStatus`.

- [ ] **Step 3: Add the status fields**

In `api/v1alpha1/persistence_types.go`, extend `PersistenceStatus` and add the new type:

```go
// PersistenceStatus reports datastore reachability and schema state.
type PersistenceStatus struct {
	// SchemaVersions maps a store name to its observed schema version.
	// +optional
	SchemaVersions map[string]string `json:"schemaVersions,omitempty"`

	// History records schema upgrades applied by the operator.
	// +optional
	History []SchemaUpgradeRecord `json:"history,omitempty"`

	// Reachable indicates whether the datastores were reachable at last reconcile.
	// +optional
	Reachable bool `json:"reachable,omitempty"`

	// SchemaAttempts records failed schema-migration attempts per store, so
	// recreation is bounded across operator restarts and leader-election
	// failover. Entries are removed when a store's migration succeeds.
	// +optional
	SchemaAttempts map[string]SchemaAttemptStatus `json:"schemaAttempts,omitempty"`
}

// SchemaAttemptStatus counts consecutive failed schema-migration attempts for a
// single store.
type SchemaAttemptStatus struct {
	// Count is the number of attempts that have failed.
	// +optional
	Count int32 `json:"count,omitempty"`
	// FirstFailedAt is when the first of the current run of failures occurred.
	// +optional
	FirstFailedAt *metav1.Time `json:"firstFailedAt,omitempty"`
	// LastError is the failure reason reported by the most recent Job.
	// +optional
	LastError string `json:"lastError,omitempty"`
}
```

In `api/v1alpha1/conditions.go`, add:

```go
	// ReasonSchemaMigrationFailed indicates schema migration failed and the
	// retry budget is exhausted.
	ReasonSchemaMigrationFailed = "SchemaMigrationFailed"
	// ReasonSchemaMigrationRetrying indicates a failed schema migration will be retried.
	ReasonSchemaMigrationRetrying = "SchemaMigrationRetrying"
```

- [ ] **Step 4: Implement the recovery path**

In `internal/controller/temporalcluster_persistence.go`, add `requeueAfter` to `storeResult`:

```go
type storeResult struct {
	done         bool
	failed       bool
	message      string
	requeueAfter time.Duration
}
```

Replace the two `jobFailed` branches in `reconcileJobSchema` (lines 243-275) so both delegate to the new handler:

```go
func (r *TemporalClusterReconciler) reconcileJobSchema(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, t schemaTarget, current string) (storeResult, error) {
	if current == "" {
		setup, err := r.ensureSchemaJob(ctx, cluster, t, resources.ActionSetup)
		if err != nil {
			return storeResult{}, err
		}
		if setup == jobFailed {
			return r.handleFailedSchemaJob(ctx, cluster, t, resources.ActionSetup)
		}
		if setup != jobSucceeded {
			return storeResult{}, nil
		}
	}

	update, err := r.ensureSchemaJob(ctx, cluster, t, resources.ActionUpdate)
	if err != nil {
		return storeResult{}, err
	}
	if update == jobFailed {
		return r.handleFailedSchemaJob(ctx, cluster, t, resources.ActionUpdate)
	}
	if update == jobSucceeded {
		resetSchemaAttempts(cluster, t.store)
		// The setup/update Jobs have finished, so the schema version is now current.
		// The schema version we read this pass came from an inspector Job that ran
		// before migration and reports the old (often empty) version. Delete that
		// stale inspector Job so the next reconcile re-probes the updated version
		// immediately, instead of waiting out the inspector Job's TTL.
		if err := r.deleteInspectorJob(ctx, cluster, t.store); err != nil {
			return storeResult{}, err
		}
	}
	return storeResult{}, nil
}

// handleFailedSchemaJob applies the bounded recreation policy to a schema Job
// that has exhausted its own BackoffLimit.
//
// The Job's BackoffLimit retries the pod within seconds, which does not cover
// the most common real failure: the Job was created while the database was
// still starting. Recreating the Job at minute-scale intervals covers that,
// while the attempt budget stops a genuinely broken migration from retrying
// forever.
func (r *TemporalClusterReconciler) handleFailedSchemaJob(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, t schemaTarget, action resources.SchemaAction) (storeResult, error) {
	key := string(t.store)
	if cluster.Status.Persistence.SchemaAttempts == nil {
		cluster.Status.Persistence.SchemaAttempts = map[string]temporalv1alpha1.SchemaAttemptStatus{}
	}
	attempt := cluster.Status.Persistence.SchemaAttempts[key]

	name := resources.SchemaJobName(cluster.Name, t.store, action)
	detail := r.schemaJobFailureDetail(ctx, cluster, name)

	decision := recovery.SchemaJobPolicy.Next(int(attempt.Count))
	if !decision.Retry {
		message := fmt.Sprintf("%s %s-schema job failed %d times and will not be retried: %s. The failed Job %q is retained; inspect its pod logs with: kubectl -n %s logs job/%s",
			t.store, action, attempt.Count, detail, name, cluster.Namespace, name)
		status.Set(cluster, temporalv1alpha1.ConditionSchemaReady, metav1.ConditionFalse,
			temporalv1alpha1.ReasonSchemaMigrationFailed, message)
		status.Set(cluster, temporalv1alpha1.ConditionDegraded, metav1.ConditionTrue,
			temporalv1alpha1.ReasonSchemaMigrationFailed, message)
		r.warnEvent(cluster, temporalv1alpha1.ReasonSchemaMigrationFailed, message)
		return storeResult{failed: true, message: message}, nil
	}

	now := metav1.Now()
	if attempt.FirstFailedAt == nil {
		attempt.FirstFailedAt = &now
	}
	attempt.Count++
	attempt.LastError = detail
	cluster.Status.Persistence.SchemaAttempts[key] = attempt

	// Delete the failed Job so the next reconcile recreates it from scratch.
	// Safe to re-run: the schema tools are invoked without --overwrite.
	job := &batchv1.Job{}
	job.Name = name
	job.Namespace = cluster.Namespace
	policy := metav1.DeletePropagationBackground
	if err := r.Delete(ctx, job, &client.DeleteOptions{PropagationPolicy: &policy}); err != nil && !apierrors.IsNotFound(err) {
		return storeResult{}, fmt.Errorf("deleting failed %s job: %w", action, err)
	}

	message := fmt.Sprintf("%s %s-schema job failed (attempt %d of %d): %s; retrying in %s",
		t.store, action, attempt.Count, len(recovery.SchemaJobPolicy.Delays), detail, decision.After)
	status.Set(cluster, temporalv1alpha1.ConditionSchemaReady, metav1.ConditionFalse,
		temporalv1alpha1.ReasonSchemaMigrationRetrying, message)
	r.warnEvent(cluster, temporalv1alpha1.ReasonSchemaMigrationRetrying, message)

	return storeResult{requeueAfter: decision.After}, nil
}

// schemaJobFailureDetail reports the Job's own failure reason, so the condition
// message names the cause rather than just the fact of failure.
func (r *TemporalClusterReconciler) schemaJobFailureDetail(ctx context.Context, cluster *temporalv1alpha1.TemporalCluster, name string) string {
	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Namespace: cluster.Namespace, Name: name}, &job); err != nil {
		return "job not found"
	}
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return fmt.Sprintf("%s: %s", c.Reason, c.Message)
		}
	}
	return fmt.Sprintf("%d failed pods", job.Status.Failed)
}

// resetSchemaAttempts clears the failure record for a store after a successful
// migration, so a later unrelated failure gets a full retry budget.
func resetSchemaAttempts(cluster *temporalv1alpha1.TemporalCluster, store temporalv1alpha1.StoreName) {
	delete(cluster.Status.Persistence.SchemaAttempts, string(store))
}
```

- [ ] **Step 5: Propagate requeueAfter**

Find where `reconcileJobSchema`'s `storeResult` is consumed (the persistence sub-reconciler's aggregation of per-store results) and carry the shortest non-zero `requeueAfter` into the returned `ctrl.Result`. Read the aggregation code first; it must take the **minimum** non-zero value across stores, so the earliest scheduled retry is honored:

```go
// minRequeue returns the soonest non-zero requeue among results, or zero if none.
func minRequeue(results ...storeResult) time.Duration {
	var out time.Duration
	for _, res := range results {
		if res.requeueAfter == 0 {
			continue
		}
		if out == 0 || res.requeueAfter < out {
			out = res.requeueAfter
		}
	}
	return out
}
```

- [ ] **Step 6: Regenerate, test, and commit**

```bash
make generate manifests
go test ./internal/controller/ -run 'TestHandleFailedSchemaJob|TestSchemaAttemptsReset' -v
make test
git add api/ config/crd/ internal/controller/
git commit -s -m "feat(controller): recreate failed schema jobs on a bounded schedule

A schema Job that exhausts its BackoffLimit is now deleted and recreated
after 1m, 5m, then 15m before the operator gives up with a terminal
SchemaReady=False and Degraded=True. The failed Job is retained on
give-up so its pod logs stay inspectable. Recreation is safe: the schema
tools run without --overwrite."
```

---

### Task 10: Cleanup deadline

**Files:**
- Create: `internal/controller/cleanup.go`
- Test: `internal/controller/cleanup_test.go`
- Modify: `internal/controller/temporalnamespace_controller.go:68-85`
- Modify: `internal/controller/temporalschedule_controller.go:70-87`
- Modify: `internal/controller/temporalsearchattribute_controller.go:63-80`
- Modify: `internal/controller/temporalclusterconnection_controller.go:373-381`
- Modify: `api/v1alpha1/conditions.go`

**Interfaces:**
- Consumes: `recovery.DeadlineExceeded`, `recovery.Remaining` (Task 3).
- Produces:
  - `type cleanupAction int` with `cleanupForget`, `cleanupRetry`, `cleanupAbandon`
  - `func decideCleanup(obj client.Object, err error, now time.Time) (cleanupAction, time.Duration)`
  - `var cleanupDeadline = 5 * time.Minute`
  - `var cleanupRetryInterval = 15 * time.Second`
  - `temporalv1alpha1.ReasonCleanupAbandoned = "CleanupAbandoned"`

**Critical constraint:** issue #58 (spec `docs/superpowers/specs/2026-06-18-stranded-finalizer-fix-design.md`) fixed objects being stuck in `Terminating` forever. Its guarantee — **deletion always terminates** — must survive this change. The existing regression tests ("removes the finalizer when the cluster is deleted before the ...") must pass untouched. The only change is that a *transient* failure now retries before giving up, where previously it gave up instantly.

No new status field is needed: `metadata.deletionTimestamp` already records when deletion began.

- [ ] **Step 1: Write the failing test**

Create `internal/controller/cleanup_test.go` (license header, then):

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/controller/ -run TestDecideCleanup`
Expected: FAIL — `undefined: decideCleanup`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/controller/cleanup.go` (license header, then):

```go
package controller

import (
	"errors"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

var _ = metav1.Now
```

Remove the unused `metav1` alias line once the file compiles without it.

In `api/v1alpha1/conditions.go`, add:

```go
	// ReasonCleanupAbandoned indicates remote cleanup was abandoned after the
	// cleanup deadline elapsed with the target unreachable.
	ReasonCleanupAbandoned = "CleanupAbandoned"
	// ReasonCleanupPending indicates remote cleanup is being retried.
	ReasonCleanupPending = "CleanupPending"
```

- [ ] **Step 4: Apply the decision in each controller**

In `internal/controller/temporalnamespace_controller.go`, replace the two deletion branches (lines 68-85) with:

```go
	target, err := resolveTarget(ctx, r.Client, ns.Namespace, ns.Spec.ClusterRef)
	if err != nil {
		if !ns.DeletionTimestamp.IsZero() {
			return r.cleanupUnreachable(ctx, &ns, err)
		}
		if errors.Is(err, ErrTargetNotFound) {
			r.setReady(&ns, metav1.ConditionFalse, "ClusterNotFound", "referenced Temporal target not found")
			return ctrl.Result{RequeueAfter: namespaceDriftRequeue}, r.statusUpdate(ctx, &ns)
		}
		return ctrl.Result{}, err
	}

	tc, err := r.clientFactory()(ctx, target.Address, target.TLSConfig)
	if err != nil {
		if !ns.DeletionTimestamp.IsZero() {
			return r.cleanupUnreachable(ctx, &ns, err)
		}
		return ctrl.Result{}, fmt.Errorf("building temporal client: %w", err)
	}
	defer func() { _ = tc.Close() }()
```

And add, next to `removeFinalizerAndForget` (which stays exactly as it is):

```go
// cleanupUnreachable applies the cleanup deadline when the target cannot be
// reached during deletion.
func (r *TemporalNamespaceReconciler) cleanupUnreachable(ctx context.Context, ns *temporalv1alpha1.TemporalNamespace, cause error) (ctrl.Result, error) {
	action, after := decideCleanup(ns, cause, time.Now())
	switch action {
	case cleanupForget:
		return ctrl.Result{}, r.removeFinalizerAndForget(ctx, ns)
	case cleanupRetry:
		status.Set(ns, temporalv1alpha1.ConditionProgressing, metav1.ConditionTrue,
			temporalv1alpha1.ReasonCleanupPending,
			fmt.Sprintf("waiting for the target to become reachable before cleaning up: %v", cause))
		return ctrl.Result{RequeueAfter: after}, r.statusUpdate(ctx, ns)
	default:
		message := fmt.Sprintf("abandoned cleanup of temporal namespace %q after %s: %v",
			namespaceParams(ns).Name, cleanupDeadline, cause)
		r.Events.Warning(ns, temporalv1alpha1.ReasonCleanupAbandoned, message)
		// Task 13 adds the metrics.CleanupAbandoned counter increment here.
		return ctrl.Result{}, r.removeFinalizerAndForget(ctx, ns)
	}
}
```

Add an `Events *events.Recorder` field to `TemporalNamespaceReconciler` and populate it in `cmd/main.go` (Task 13 wires all of them; for now add the field and leave it nil-safe — `*events.Recorder` is nil-safe by construction).

Apply the same shape to the other three controllers, changing only the receiver, type, kind label, and the description of the orphaned object. Each `cleanupRetry` branch sets `Progressing=True` with `ReasonCleanupPending` exactly as above, so a resource waiting on cleanup is distinguishable from one that is simply idle:

```go
// temporalschedule_controller.go — replaces the branches at lines 70-87
func (r *TemporalScheduleReconciler) cleanupUnreachable(ctx context.Context, sched *temporalv1alpha1.TemporalSchedule, cause error) (ctrl.Result, error) {
	action, after := decideCleanup(sched, cause, time.Now())
	switch action {
	case cleanupForget:
		return ctrl.Result{}, r.removeFinalizerAndForget(ctx, sched)
	case cleanupRetry:
		status.Set(sched, temporalv1alpha1.ConditionProgressing, metav1.ConditionTrue,
			temporalv1alpha1.ReasonCleanupPending,
			fmt.Sprintf("waiting for the target to become reachable before cleaning up: %v", cause))
		return ctrl.Result{RequeueAfter: after}, r.statusUpdate(ctx, sched)
	default:
		message := fmt.Sprintf("abandoned cleanup of temporal schedule %q in namespace %q after %s: %v",
			sched.Spec.ScheduleID, sched.Spec.Namespace, cleanupDeadline, cause)
		r.Events.Warning(sched, temporalv1alpha1.ReasonCleanupAbandoned, message)
		// Task 13 adds the metrics.CleanupAbandoned counter increment here.
		return ctrl.Result{}, r.removeFinalizerAndForget(ctx, sched)
	}
}

// temporalsearchattribute_controller.go — replaces the branches at lines 63-80
func (r *TemporalSearchAttributeReconciler) cleanupUnreachable(ctx context.Context, sa *temporalv1alpha1.TemporalSearchAttribute, cause error) (ctrl.Result, error) {
	action, after := decideCleanup(sa, cause, time.Now())
	switch action {
	case cleanupForget:
		return ctrl.Result{}, r.removeFinalizerAndForget(ctx, sa)
	case cleanupRetry:
		status.Set(sa, temporalv1alpha1.ConditionProgressing, metav1.ConditionTrue,
			temporalv1alpha1.ReasonCleanupPending,
			fmt.Sprintf("waiting for the target to become reachable before cleaning up: %v", cause))
		return ctrl.Result{RequeueAfter: after}, r.statusUpdate(ctx, sa)
	default:
		message := fmt.Sprintf("abandoned cleanup of search attribute %q in namespace %q after %s: %v",
			sa.Spec.Name, sa.Spec.Namespace, cleanupDeadline, cause)
		r.Events.Warning(sa, temporalv1alpha1.ReasonCleanupAbandoned, message)
		// Task 13 adds the metrics.CleanupAbandoned counter increment here.
		return ctrl.Result{}, r.removeFinalizerAndForget(ctx, sa)
	}
}
```

For `internal/controller/temporalclusterconnection_controller.go:373-381`, the current code logs and ignores every `RemoveRemoteCluster` error, then drops the finalizer. Change it to collect the errors and route them through the same decision:

```go
	var removeErr error
	for _, peer := range peers {
		if err := peer.client.RemoveRemoteCluster(ctx, peer.name); err != nil {
			log.Error(err, "removing remote cluster", "peer", peer.name)
			removeErr = errors.Join(removeErr, fmt.Errorf("peer %s: %w", peer.name, err))
		}
	}
	if removeErr != nil {
		action, after := decideCleanup(conn, removeErr, time.Now())
		switch action {
		case cleanupRetry:
			return ctrl.Result{RequeueAfter: after}, nil
		case cleanupAbandon:
			message := fmt.Sprintf("abandoned removal of remote cluster registrations after %s: %v", cleanupDeadline, removeErr)
			r.Events.Warning(conn, temporalv1alpha1.ReasonCleanupAbandoned, message)
			// Task 13 adds the metrics.CleanupAbandoned counter increment here.
		}
	}
```

Adapt the peer-loop variable names to whatever the file actually uses — read lines 360-390 before editing.

- [ ] **Step 5: Write the envtest regression tests**

Append to `internal/controller/temporalnamespace_controller_test.go`, inside the existing Ginkgo describe block:

```go
It("retries cleanup while the cluster exists but is unreachable, then abandons", func() {
	origDeadline := cleanupDeadline
	origInterval := cleanupRetryInterval
	cleanupDeadline = 200 * time.Millisecond
	cleanupRetryInterval = 20 * time.Millisecond
	defer func() {
		cleanupDeadline = origDeadline
		cleanupRetryInterval = origInterval
	}()

	// A ready cluster exists, so resolveTarget succeeds, but the client
	// factory fails: the target is present-but-unreachable.
	cluster := makeReadyCluster("tc-unreachable")
	Expect(k8sClient.Create(ctx, cluster)).To(Succeed())

	ns := makeNamespace("ns-unreachable", "tc-unreachable")
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())

	reconciler := &TemporalNamespaceReconciler{
		Client: k8sClient,
		Scheme: k8sClient.Scheme(),
		ClientFactory: func(context.Context, string, *tls.Config) (temporal.NamespaceClient, error) {
			return nil, errors.New("connection refused")
		},
	}
	key := client.ObjectKeyFromObject(ns)

	// First reconcile registers the finalizer via the happy path helper.
	Expect(k8sClient.Get(ctx, key, ns)).To(Succeed())
	controllerutil.AddFinalizer(ns, namespaceFinalizer)
	Expect(k8sClient.Update(ctx, ns)).To(Succeed())
	Expect(k8sClient.Delete(ctx, ns)).To(Succeed())

	// Within the deadline the finalizer is retained and a retry is scheduled.
	res, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
	Expect(err).NotTo(HaveOccurred())
	Expect(res.RequeueAfter).To(BeNumerically(">", 0))
	Expect(k8sClient.Get(ctx, key, ns)).To(Succeed())
	Expect(ns.Finalizers).To(ContainElement(namespaceFinalizer))

	// Past the deadline the finalizer is released so deletion terminates.
	time.Sleep(250 * time.Millisecond)
	_, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: key})
	Expect(err).NotTo(HaveOccurred())
	Eventually(func() bool {
		return apierrors.IsNotFound(k8sClient.Get(ctx, key, ns))
	}, time.Second, 20*time.Millisecond).Should(BeTrue())
})
```

Reuse the file's existing `makeReadyCluster`/`makeNamespace` fixtures if present; otherwise build the objects inline following the patterns already in that file.

- [ ] **Step 6: Verify the #58 guarantee still holds**

Run: `go test ./internal/controller/ -run TestTemporalNamespace -v 2>&1 | grep -i "finalizer"`
Expected: the pre-existing test "removes the finalizer when the cluster is deleted before the namespace" still passes, unmodified. If it fails, `ErrTargetNotFound` is no longer taking the `cleanupForget` branch — fix that before continuing.

- [ ] **Step 7: Run everything and commit**

```bash
make test
git add api/ internal/controller/
git commit -s -m "feat(controller): bound cleanup retries instead of orphaning silently

A target that is absent still forgets immediately, preserving the issue
#58 guarantee that deletion always terminates. A target that exists but
is unreachable now retries for up to cleanupDeadline before abandoning
with a CleanupAbandoned warning event and a counter increment, rather
than orphaning the Temporal object on the first transient error."
```

---

### Task 11: gRPC dial and RPC deadlines

**Files:**
- Modify: `internal/temporal/client.go:81-100`
- Modify: `internal/temporal/schedule.go:344`
- Modify: `internal/temporal/workflowrun.go:90`
- Test: `internal/temporal/client_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `var DialTimeout = 30 * time.Second`; `func DialContext(ctx context.Context) (context.Context, context.CancelFunc)`

**Background:** all three call sites use `grpc.NewClient`, which is lazy and does not block — so the hang is not in dialing but in the first RPC on a half-open connection. With controller-runtime's default `MaxConcurrentReconciles: 1`, one such reconcile stalls its entire controller. Bounding the context is what actually fixes it.

- [ ] **Step 1: Write the failing test**

Create or append to `internal/temporal/client_test.go`:

```go
func TestDialContextAppliesTimeout(t *testing.T) {
	orig := temporal.DialTimeout
	temporal.DialTimeout = 40 * time.Millisecond
	defer func() { temporal.DialTimeout = orig }()

	ctx, cancel := temporal.DialContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("DialContext returned a context with no deadline")
	}
	if until := time.Until(deadline); until > 50*time.Millisecond {
		t.Errorf("deadline is %v away, want <= 50ms", until)
	}

	select {
	case <-ctx.Done():
		t.Fatal("context expired immediately")
	default:
	}

	time.Sleep(60 * time.Millisecond)
	if ctx.Err() == nil {
		t.Error("context did not expire after the timeout elapsed")
	}
}

func TestDialContextPreservesShorterParentDeadline(t *testing.T) {
	orig := temporal.DialTimeout
	temporal.DialTimeout = time.Hour
	defer func() { temporal.DialTimeout = orig }()

	parent, cancelParent := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancelParent()

	ctx, cancel := temporal.DialContext(parent)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("no deadline on the derived context")
	}
	if time.Until(deadline) > time.Minute {
		t.Error("the parent's shorter deadline was discarded")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/temporal/ -run TestDialContext`
Expected: FAIL — `undefined: temporal.DialContext`.

- [ ] **Step 3: Write minimal implementation**

In `internal/temporal/client.go`, add near the top of the file:

```go
// DialTimeout bounds how long any single Temporal RPC issued by a reconciler
// may take. Without it a half-open connection blocks a reconcile indefinitely,
// which -- at controller-runtime's default MaxConcurrentReconciles of 1 --
// stalls that controller entirely. It is a var so tests can shorten it.
var DialTimeout = 30 * time.Second

// DialContext derives a bounded context for Temporal RPCs. A parent deadline
// that is already sooner than DialTimeout wins, since context.WithTimeout keeps
// the earlier of the two.
func DialContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, DialTimeout)
}
```

Then wrap the RPC-issuing paths in each client. In `client.go`, `schedule.go`, and `workflowrun.go`, each exported method that performs an RPC gains the same two opening lines. For example, in `client.go`:

```go
func (c *namespaceClient) Describe(ctx context.Context, name string) (*NamespaceInfo, error) {
	ctx, cancel := DialContext(ctx)
	defer cancel()
	// ... existing body unchanged
}
```

Apply this to every method on `namespaceClient`, `scheduleClient`, and `workflowRunClient` that calls the gRPC stub. Do not wrap `Close()`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/temporal/ -run TestDialContext -v`
Expected: PASS — two tests.

- [ ] **Step 5: Verify no RPC method was missed**

Run: `grep -n "func (c \*" internal/temporal/client.go internal/temporal/schedule.go internal/temporal/workflowrun.go`
Expected: every listed method except `Close` contains `DialContext` in its body. Check each one.

- [ ] **Step 6: Commit**

```bash
make test
git add internal/temporal/
git commit -s -m "fix(temporal): bound gRPC RPC deadlines in reconcile paths

grpc.NewClient is lazy, so a half-open connection hangs on the first RPC
rather than at dial. Every client method now derives a bounded context,
so an unreachable frontend can no longer stall a controller indefinitely."
```

---

### Task 12: Phase 2 gate

- [ ] **Step 1: Run every gate**

```bash
make build && make test && make lint
```

Expected: all three pass.

- [ ] **Step 2: Verify generated artifacts are current**

```bash
make generate manifests && make helm-chart && git status --short
```

Expected: no unstaged modifications. If `dist/chart` changed, commit it — do not edit it by hand.

- [ ] **Step 3: Commit any regeneration**

```bash
git add -A && git commit -s -m "chore: regenerate manifests and chart after status field additions"
```

---

# Phase 3 — Visibility

Conditions from Phase 2 become queryable and alertable.

### Task 13: Domain metrics

**Files:**
- Create: `internal/metrics/domain.go`
- Test: `internal/metrics/domain_test.go`
- Modify: the four `cleanupUnreachable` methods from Task 10
- Modify: `internal/controller/temporalcluster_upgrade.go`
- Modify: `internal/controller/temporalcluster_persistence.go`
- Modify: `internal/controller/target.go`

**Interfaces:**
- Consumes: the `cleanupAbandon` branches (Task 10), `UpgradeStatus.PhaseStartedAt` (Task 7), `SchemaAttemptStatus` (Task 9).
- Produces:
  - `metrics.CleanupAbandoned *prometheus.CounterVec` — labels `kind`, `namespace`
  - `metrics.TargetUnreachable *prometheus.CounterVec` — labels `kind`, `namespace`
  - `metrics.UpgradePhaseSeconds *prometheus.GaugeVec` — labels `namespace`, `name`, `phase`
  - `metrics.SchemaJobAttempts *prometheus.GaugeVec` — labels `namespace`, `name`, `store`
  - `func Register()` — idempotent registration into controller-runtime's registry

**Why controller-runtime's registry:** it is already served on the existing authenticated metrics endpoint and scraped by the existing ServiceMonitor, so no new port, Service, or RBAC is needed.

- [ ] **Step 1: Write the failing test**

Create `internal/metrics/domain_test.go` (license header, then):

```go
package metrics_test

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/bmorton/temporal-operator/internal/metrics"
)

func TestCleanupAbandonedCounter(t *testing.T) {
	metrics.CleanupAbandoned.Reset()
	metrics.CleanupAbandoned.WithLabelValues("TemporalNamespace", "default").Inc()
	metrics.CleanupAbandoned.WithLabelValues("TemporalNamespace", "default").Inc()

	expected := `
# HELP temporal_operator_cleanup_abandoned_total Number of times remote cleanup was abandoned after the cleanup deadline elapsed.
# TYPE temporal_operator_cleanup_abandoned_total counter
temporal_operator_cleanup_abandoned_total{kind="TemporalNamespace",namespace="default"} 2
`
	if err := testutil.CollectAndCompare(metrics.CleanupAbandoned, strings.NewReader(expected)); err != nil {
		t.Error(err)
	}
}

func TestUpgradePhaseSecondsGauge(t *testing.T) {
	metrics.UpgradePhaseSeconds.Reset()
	metrics.UpgradePhaseSeconds.WithLabelValues("default", "tc", "RollingFrontend").Set(120)

	expected := `
# HELP temporal_operator_upgrade_phase_seconds Seconds the current upgrade phase has been active.
# TYPE temporal_operator_upgrade_phase_seconds gauge
temporal_operator_upgrade_phase_seconds{name="tc",namespace="default",phase="RollingFrontend"} 120
`
	if err := testutil.CollectAndCompare(metrics.UpgradePhaseSeconds, strings.NewReader(expected)); err != nil {
		t.Error(err)
	}
}

func TestRegisterIsIdempotent(t *testing.T) {
	metrics.Register()
	metrics.Register() // must not panic with AlreadyRegisteredError
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metrics/`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write minimal implementation**

Create `internal/metrics/domain.go` (license header, then):

```go
// Package metrics exposes the operator's domain metrics. Reconcile rate,
// latency, and error metrics already come from controller-runtime; only state
// that conditions cannot express lives here.
package metrics

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const namespacePrefix = "temporal_operator"

var (
	// CleanupAbandoned counts give-ups on remote cleanup. Any increase means a
	// Temporal-side object was orphaned and needs manual attention.
	CleanupAbandoned = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: namespacePrefix + "_cleanup_abandoned_total",
		Help: "Number of times remote cleanup was abandoned after the cleanup deadline elapsed.",
	}, []string{"kind", "namespace"})

	// TargetUnreachable counts failures to resolve or dial a Temporal target
	// that the operator believes exists. Rising counts show a flapping frontend
	// before it causes an abandonment.
	TargetUnreachable = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: namespacePrefix + "_target_unreachable_total",
		Help: "Number of failures to reach a Temporal target that is believed to exist.",
	}, []string{"kind", "namespace"})

	// UpgradePhaseSeconds reports how long the current upgrade phase has run,
	// which is what an alert compares against the stall timeout.
	UpgradePhaseSeconds = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: namespacePrefix + "_upgrade_phase_seconds",
		Help: "Seconds the current upgrade phase has been active.",
	}, []string{"namespace", "name", "phase"})

	// SchemaJobAttempts reports consecutive failed schema-migration attempts.
	SchemaJobAttempts = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: namespacePrefix + "_schema_job_attempts",
		Help: "Consecutive failed schema migration attempts for a store.",
	}, []string{"namespace", "name", "store"})
)

var registerOnce sync.Once

// Register adds the domain metrics to controller-runtime's registry, which is
// already served on the manager's authenticated metrics endpoint. Safe to call
// more than once.
func Register() {
	registerOnce.Do(func() {
		ctrlmetrics.Registry.MustRegister(
			CleanupAbandoned,
			TargetUnreachable,
			UpgradePhaseSeconds,
			SchemaJobAttempts,
		)
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metrics/ -v`
Expected: PASS — three tests.

- [ ] **Step 5: Wire the increments**

Add `"github.com/bmorton/temporal-operator/internal/metrics"` to each file and replace the four `// Task 13 adds the metrics.CleanupAbandoned counter increment here.` comments from Task 10 with the real calls:

```go
metrics.CleanupAbandoned.WithLabelValues("TemporalNamespace", ns.Namespace).Inc()
metrics.CleanupAbandoned.WithLabelValues("TemporalSchedule", sched.Namespace).Inc()
metrics.CleanupAbandoned.WithLabelValues("TemporalSearchAttribute", sa.Namespace).Inc()
metrics.CleanupAbandoned.WithLabelValues("TemporalClusterConnection", conn.Namespace).Inc()
```

In each `cleanupUnreachable`, also increment the unreachable counter on the `cleanupRetry` branch, using the same kind label.

In `internal/controller/temporalcluster_upgrade.go`, set the phase gauge whenever the upgrade status is evaluated. Add at the end of `reconcileUpgrade`, before it returns:

```go
	if up := cluster.Status.Upgrade; up != nil && up.PhaseStartedAt != nil {
		metrics.UpgradePhaseSeconds.
			WithLabelValues(cluster.Namespace, cluster.Name, up.Phase).
			Set(time.Since(up.PhaseStartedAt.Time).Seconds())
	} else {
		metrics.UpgradePhaseSeconds.DeletePartialMatch(prometheus.Labels{
			"namespace": cluster.Namespace, "name": cluster.Name,
		})
	}
```

The `DeletePartialMatch` on the else branch is what stops a completed upgrade leaving a stale series behind forever.

In `internal/controller/temporalcluster_persistence.go`, inside `handleFailedSchemaJob` after updating the attempt record:

```go
	metrics.SchemaJobAttempts.
		WithLabelValues(cluster.Namespace, cluster.Name, string(t.store)).
		Set(float64(attempt.Count))
```

And in `resetSchemaAttempts`, clear it:

```go
func resetSchemaAttempts(cluster *temporalv1alpha1.TemporalCluster, store temporalv1alpha1.StoreName) {
	delete(cluster.Status.Persistence.SchemaAttempts, string(store))
	metrics.SchemaJobAttempts.DeleteLabelValues(cluster.Namespace, cluster.Name, string(store))
}
```

- [ ] **Step 6: Commit**

```bash
make test
git add internal/metrics/ internal/controller/
git commit -s -m "feat(metrics): add domain metrics for stalls, retries, and orphans"
```

---

### Task 14: Condition-derived collector

**Files:**
- Create: `internal/metrics/conditions.go`
- Test: `internal/metrics/conditions_test.go`

**Interfaces:**
- Consumes: `status.Object` (Task 2).
- Produces:
  - `func NewConditionCollector(c client.Client) *ConditionCollector`
  - `ConditionCollector` implements `prometheus.Collector`
  - Metric `temporal_operator_resource_condition{kind,namespace,name,type,status,reason}`

**Design note for the implementer:** this collects on scrape rather than writing gauges during reconcile. Per-reconcile gauges leak series for deleted objects and force every controller to remember cleanup. Collecting from the cache means deleted resources simply stop appearing, and every condition introduced anywhere becomes queryable with no further plumbing.

- [ ] **Step 1: Write the failing test**

Create `internal/metrics/conditions_test.go` (license header, then):

```go
package metrics_test

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	"github.com/bmorton/temporal-operator/internal/metrics"
)

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := temporalv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("adding scheme: %v", err)
	}
	return s
}

func TestConditionCollectorEmitsOnePerCondition(t *testing.T) {
	cluster := &temporalv1alpha1.TemporalCluster{}
	cluster.Name = "tc"
	cluster.Namespace = "default"
	cluster.Status.Conditions = []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllServicesReady"},
		{Type: "UpgradeBlocked", Status: metav1.ConditionFalse, Reason: "UpgradeProgressing"},
	}

	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(cluster).Build()
	collector := metrics.NewConditionCollector(c)

	expected := `
# HELP temporal_operator_resource_condition Current status of each condition on each Temporal resource (1 when the condition is True).
# TYPE temporal_operator_resource_condition gauge
temporal_operator_resource_condition{kind="TemporalCluster",name="tc",namespace="default",reason="AllServicesReady",status="True",type="Ready"} 1
temporal_operator_resource_condition{kind="TemporalCluster",name="tc",namespace="default",reason="UpgradeProgressing",status="False",type="UpgradeBlocked"} 0
`
	if err := testutil.CollectAndCompare(collector, strings.NewReader(expected)); err != nil {
		t.Error(err)
	}
}

func TestConditionCollectorDropsDeletedResources(t *testing.T) {
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	collector := metrics.NewConditionCollector(c)

	if got := testutil.CollectAndCount(collector); got != 0 {
		t.Errorf("collected %d series with no resources present, want 0", got)
	}
}

func TestConditionCollectorCoversAllKinds(t *testing.T) {
	objs := []client.Object{
		withReady(&temporalv1alpha1.TemporalCluster{}),
		withReady(&temporalv1alpha1.TemporalClusterClient{}),
		withReady(&temporalv1alpha1.TemporalClusterConnection{}),
		withReady(&temporalv1alpha1.TemporalDevServer{}),
		withReady(&temporalv1alpha1.TemporalNamespace{}),
		withReady(&temporalv1alpha1.TemporalSchedule{}),
		withReady(&temporalv1alpha1.TemporalSearchAttribute{}),
		withReady(&temporalv1alpha1.TemporalWorkflowRun{}),
	}
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(objs...).Build()

	if got := testutil.CollectAndCount(metrics.NewConditionCollector(c)); got != 8 {
		t.Errorf("collected %d series, want 8 (one Ready condition per kind)", got)
	}
}

// withReady names an object and gives it a single Ready condition.
func withReady[T interface {
	client.Object
	GetConditions() *[]metav1.Condition
}](obj T) client.Object {
	obj.SetName("x")
	obj.SetNamespace("default")
	*obj.GetConditions() = []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Ok"}}
	return obj
}
```

Add `"sigs.k8s.io/controller-runtime/pkg/client"` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metrics/ -run TestConditionCollector`
Expected: FAIL — `undefined: metrics.NewConditionCollector`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/metrics/conditions.go` (license header, then):

```go
package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
	opstatus "github.com/bmorton/temporal-operator/internal/status"
)

var conditionDesc = prometheus.NewDesc(
	namespacePrefix+"_resource_condition",
	"Current status of each condition on each Temporal resource (1 when the condition is True).",
	[]string{"kind", "namespace", "name", "type", "status", "reason"},
	nil,
)

// listers maps each kind name to a factory for its List type. Adding a CRD means
// adding one line here; nothing else in the metrics layer changes.
var listers = map[string]func() client.ObjectList{
	"TemporalCluster":           func() client.ObjectList { return &temporalv1alpha1.TemporalClusterList{} },
	"TemporalClusterClient":     func() client.ObjectList { return &temporalv1alpha1.TemporalClusterClientList{} },
	"TemporalClusterConnection": func() client.ObjectList { return &temporalv1alpha1.TemporalClusterConnectionList{} },
	"TemporalDevServer":         func() client.ObjectList { return &temporalv1alpha1.TemporalDevServerList{} },
	"TemporalNamespace":         func() client.ObjectList { return &temporalv1alpha1.TemporalNamespaceList{} },
	"TemporalSchedule":          func() client.ObjectList { return &temporalv1alpha1.TemporalScheduleList{} },
	"TemporalSearchAttribute":   func() client.ObjectList { return &temporalv1alpha1.TemporalSearchAttributeList{} },
	"TemporalWorkflowRun":       func() client.ObjectList { return &temporalv1alpha1.TemporalWorkflowRunList{} },
}

// ConditionCollector exports every condition on every Temporal resource.
//
// It collects on scrape from the manager's cache rather than writing gauges
// during reconcile. That avoids stale series for deleted objects and means new
// conditions become queryable without touching this file.
type ConditionCollector struct {
	client client.Client
}

// NewConditionCollector builds a collector reading through the given client.
func NewConditionCollector(c client.Client) *ConditionCollector {
	return &ConditionCollector{client: c}
}

// Describe implements prometheus.Collector.
func (c *ConditionCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- conditionDesc
}

// Collect implements prometheus.Collector. Errors listing a kind are skipped
// rather than failing the whole scrape: a partial view beats none.
func (c *ConditionCollector) Collect(ch chan<- prometheus.Metric) {
	ctx := context.Background()

	for kind, newList := range listers {
		list := newList()
		if err := c.client.List(ctx, list); err != nil {
			continue
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			continue
		}
		for _, item := range items {
			obj, ok := item.(opstatus.Object)
			if !ok {
				continue
			}
			for _, cond := range *obj.GetConditions() {
				value := 0.0
				if cond.Status == metav1.ConditionTrue {
					value = 1.0
				}
				ch <- prometheus.MustNewConstMetric(
					conditionDesc, prometheus.GaugeValue, value,
					kind, obj.GetNamespace(), obj.GetName(),
					cond.Type, string(cond.Status), cond.Reason,
				)
			}
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metrics/ -run TestConditionCollector -v`
Expected: PASS — three tests.

- [ ] **Step 5: Commit**

```bash
make test
git add internal/metrics/
git commit -s -m "feat(metrics): export every resource condition as a gauge"
```

---

### Task 15: Wire recorders and collectors into the manager

**Files:**
- Modify: `cmd/main.go:196-260`

**Interfaces:**
- Consumes: `events.New` (Task 4), `metrics.Register`, `metrics.NewConditionCollector` (Tasks 13-14).
- Produces: every reconciler receives an `Events *events.Recorder`.

- [ ] **Step 1: Register metrics and the collector**

In `cmd/main.go`, immediately after the manager is created and before the reconcilers are set up, add:

```go
	metrics.Register()
	ctrlmetrics.Registry.MustRegister(metrics.NewConditionCollector(mgr.GetClient()))
```

Import `ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"` and `"github.com/bmorton/temporal-operator/internal/metrics"`.

- [ ] **Step 2: Give every reconciler a recorder**

Add an `Events *events.Recorder` field to each of the eight reconciler structs, then populate it. For example:

```go
	if err := (&controller.TemporalNamespaceReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Events: opevents.New(mgr.GetEventRecorder("temporalnamespace-controller")),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "TemporalNamespace")
		os.Exit(1)
	}
```

Do the same for `TemporalClusterClientReconciler` (`temporalclusterclient-controller`), `TemporalSearchAttributeReconciler` (`temporalsearchattribute-controller`), `TemporalScheduleReconciler` (`temporalschedule-controller`), `TemporalClusterConnectionReconciler` (`temporalclusterconnection-controller`), `TemporalDevServerReconciler` (`temporaldevserver-controller`), and `TemporalWorkflowRunReconciler` (`temporalworkflowrun-controller`). `TemporalClusterReconciler` keeps its existing `Recorder` field and gains `Events` alongside it.

Import the package as `opevents "github.com/bmorton/temporal-operator/internal/events"` to avoid colliding with `k8s.io/client-go/tools/events`.

- [ ] **Step 3: Verify RBAC is unchanged**

Run: `make manifests && git diff --stat config/rbac/`
Expected: no changes. The collector reads Temporal CRDs the controllers already watch, so no new permission is required. If `role.yaml` changed, something granted more than intended — investigate before proceeding.

- [ ] **Step 4: Verify the endpoint serves the new metrics**

```bash
go run ./cmd --metrics-bind-address=:8080 --metrics-secure=false &
sleep 5
curl -s localhost:8080/metrics | grep -c temporal_operator_
kill %1
```

Expected: a non-zero count. If the operator cannot reach a cluster in your environment, skip this step and rely on the e2e suite in Task 17.

- [ ] **Step 5: Commit**

```bash
make build && make test && make lint
git add cmd/main.go internal/controller/
git commit -s -m "feat(manager): register domain metrics, condition collector, and per-controller recorders"
```

---

### Task 16: Alert rules

**Files:**
- Create: `config/prometheus/alerts.yaml`
- Modify: `config/prometheus/kustomization.yaml`
- Create: `hack/helm/overrides/templates/prometheus/alerts.yaml`
- Test: `internal/metrics/alerts_test.go`

**Interfaces:**
- Consumes: metric and condition names from Tasks 7, 9, 10, 13, 14.
- Produces: a `PrometheusRule` named `temporal-operator-alerts`.

**Constraint:** `dist/chart` is generated. Hand-maintained chart files live in `hack/helm/overrides/` mirroring their `dist/chart/` path. Never edit `dist/chart` directly.

- [ ] **Step 1: Write the failing test**

Create `internal/metrics/alerts_test.go` (license header, then):

```go
package metrics_test

import (
	"os"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// TestAlertsReferenceRealMetrics guards against the classic drift where an
// alert outlives the metric or condition it queries.
func TestAlertsReferenceRealMetrics(t *testing.T) {
	raw, err := os.ReadFile("../../config/prometheus/alerts.yaml")
	if err != nil {
		t.Fatalf("reading alerts: %v", err)
	}

	var rule struct {
		Spec struct {
			Groups []struct {
				Name  string `json:"name"`
				Rules []struct {
					Alert string `json:"alert"`
					Expr  string `json:"expr"`
					For   string `json:"for"`
				} `json:"rules"`
			} `json:"groups"`
		} `json:"spec"`
	}
	if err := yaml.Unmarshal(raw, &rule); err != nil {
		t.Fatalf("parsing alerts: %v", err)
	}

	wantAlerts := map[string]bool{
		"TemporalClusterUpgradeStalled": false,
		"TemporalSchemaMigrationFailed": false,
		"TemporalResourceDegraded":      false,
		"TemporalCleanupAbandoned":      false,
	}

	knownSeries := []string{
		"temporal_operator_resource_condition",
		"temporal_operator_cleanup_abandoned_total",
		"temporal_operator_upgrade_phase_seconds",
		"temporal_operator_schema_job_attempts",
		"temporal_operator_target_unreachable_total",
	}

	for _, group := range rule.Spec.Groups {
		for _, r := range group.Rules {
			if _, expected := wantAlerts[r.Alert]; !expected {
				t.Errorf("unexpected alert %q; add it to wantAlerts if intentional", r.Alert)
				continue
			}
			wantAlerts[r.Alert] = true

			found := false
			for _, series := range knownSeries {
				if strings.Contains(r.Expr, series) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("alert %q queries no known metric: %s", r.Alert, r.Expr)
			}
			if r.For == "" {
				t.Errorf("alert %q has no 'for' duration; it would fire on a single scrape", r.Alert)
			}
		}
	}

	for name, seen := range wantAlerts {
		if !seen {
			t.Errorf("alert %q is missing from alerts.yaml", name)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metrics/ -run TestAlertsReferenceRealMetrics`
Expected: FAIL — `reading alerts: open ../../config/prometheus/alerts.yaml: no such file or directory`.

- [ ] **Step 3: Write the PrometheusRule**

Create `config/prometheus/alerts.yaml`:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  labels:
    app.kubernetes.io/name: temporal-operator
    app.kubernetes.io/managed-by: kustomize
  name: temporal-operator-alerts
  namespace: system
spec:
  groups:
    - name: temporal-operator
      rules:
        - alert: TemporalClusterUpgradeStalled
          expr: temporal_operator_resource_condition{kind="TemporalCluster",type="UpgradeBlocked",status="True"} == 1
          for: 5m
          labels:
            severity: warning
          annotations:
            summary: Temporal cluster upgrade is stalled
            description: >-
              The upgrade of TemporalCluster {{ $labels.namespace }}/{{ $labels.name }}
              has been blocked for more than 5 minutes ({{ $labels.reason }}). The
              cluster is running mixed server versions. Inspect
              status.upgrade.message for the failing service.
        - alert: TemporalSchemaMigrationFailed
          expr: temporal_operator_resource_condition{kind="TemporalCluster",type="SchemaReady",status="False",reason="SchemaMigrationFailed"} == 1
          for: 1m
          labels:
            severity: critical
          annotations:
            summary: Temporal schema migration failed permanently
            description: >-
              Schema migration for TemporalCluster {{ $labels.namespace }}/{{ $labels.name }}
              exhausted its retry budget. The cluster cannot start until this is
              resolved. The failed Job is retained; check its pod logs.
        - alert: TemporalResourceDegraded
          expr: temporal_operator_resource_condition{type="Degraded",status="True"} == 1
          for: 15m
          labels:
            severity: warning
          annotations:
            summary: Temporal resource is degraded
            description: >-
              {{ $labels.kind }} {{ $labels.namespace }}/{{ $labels.name }} has
              been Degraded for 15 minutes ({{ $labels.reason }}).
        - alert: TemporalCleanupAbandoned
          expr: increase(temporal_operator_cleanup_abandoned_total[1h]) > 0
          for: 1m
          labels:
            severity: warning
          annotations:
            summary: Temporal remote cleanup was abandoned
            description: >-
              The operator gave up cleaning up a {{ $labels.kind }} in namespace
              {{ $labels.namespace }} because its target stayed unreachable past
              the cleanup deadline. A Temporal-side object was orphaned and needs
              manual removal.
```

Add `alerts.yaml` to the `resources` list in `config/prometheus/kustomization.yaml`.

- [ ] **Step 4: Mirror into the chart overrides**

Copy the same manifest to `hack/helm/overrides/templates/prometheus/alerts.yaml`, wrapping it so it is opt-in, matching the existing ServiceMonitor gate in `dist/chart/templates/monitoring/`:

```yaml
{{- if .Values.prometheus.enable }}
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: {{ .Chart.Name }}-alerts
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "chart.labels" . | nindent 4 }}
spec:
  # ... identical groups block as above ...
{{- end }}
```

Read `dist/chart/templates/monitoring/servicemonitor.yaml` first and copy its exact guard condition and label helper names — they must match the chart's conventions.

- [ ] **Step 5: Verify and regenerate**

```bash
go test ./internal/metrics/ -run TestAlertsReferenceRealMetrics -v
kubectl apply --dry-run=client -f config/prometheus/alerts.yaml
make helm-chart
git status --short
```

Expected: test PASS; dry-run accepted (requires the PrometheusRule CRD; if absent, use `--validate=false`); `dist/chart` shows the new template.

- [ ] **Step 6: Commit**

```bash
git add config/prometheus/ hack/helm/overrides/ dist/chart/ internal/metrics/
git commit -s -m "feat(prometheus): add alert rules for stalls, failures, and orphans"
```

---

# Phase 4 — Verification

### Task 17: End-to-end stall suite

**Files:**
- Create: `test/e2e/upgrade-stall/chainsaw-test.yaml`
- Create: `test/e2e/upgrade-stall/01-temporalcluster.yaml`
- Create: `test/e2e/upgrade-stall/01-assert.yaml`
- Create: `test/e2e/upgrade-stall/02-temporalcluster-broken.yaml`
- Create: `test/e2e/upgrade-stall/02-assert-stalled.yaml`
- Create: `test/e2e/upgrade-stall/03-temporalcluster-repaired.yaml`
- Create: `test/e2e/upgrade-stall/03-assert-recovered.yaml`
- Modify: `.github/workflows/e2e.yml`

**Interfaces:**
- Consumes: the `UpgradeBlocked` condition (Task 7) and the version guard (Task 8).
- Produces: an e2e suite named `upgrade-stall`.

**Approach:** stall the upgrade with an unpullable image override rather than a corrupt version, so the failure is deterministic and fast (`ImagePullBackOff` needs no timeout to manifest) and the repair is a single field edit. The suite reuses the CNPG fixtures the existing `upgrade` suite uses.

Because `upgradePhaseTimeout` defaults to 15 minutes and e2e cannot patch a Go variable, the manager under test is started with the timeout shortened. Add a `--upgrade-phase-timeout` flag in Task 7 if it does not already exist, defaulting to `15m` and assigning `upgradePhaseTimeout`; the e2e deployment sets `--upgrade-phase-timeout=1m`.

- [ ] **Step 1: Add the flag**

In `cmd/main.go`, alongside the existing flags:

```go
	flag.DurationVar(&upgradePhaseTimeout, "upgrade-phase-timeout", 15*time.Minute,
		"How long a single upgrade phase may run before it is reported as stalled.")
```

Export a setter from the controller package rather than reaching into the variable directly:

```go
// SetUpgradePhaseTimeout overrides the stall threshold. Called once from main
// before the manager starts.
func SetUpgradePhaseTimeout(d time.Duration) { upgradePhaseTimeout = d }
```

and in `main.go`:

```go
	var upgradePhaseTimeout time.Duration
	flag.DurationVar(&upgradePhaseTimeout, "upgrade-phase-timeout", 15*time.Minute,
		"How long a single upgrade phase may run before it is reported as stalled.")
	// ... after flag.Parse():
	controller.SetUpgradePhaseTimeout(upgradePhaseTimeout)
```

- [ ] **Step 2: Write the test fixtures**

`test/e2e/upgrade-stall/01-temporalcluster.yaml` — copy `test/e2e/upgrade/01-temporalcluster-1.30.yaml` verbatim, changing only `metadata.name` to `stall-test`.

`test/e2e/upgrade-stall/01-assert.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: stall-test
status:
  conditions:
    - type: Ready
      status: "True"
```

`test/e2e/upgrade-stall/02-temporalcluster-broken.yaml` — the same cluster at the newer version with an image that cannot be pulled:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: stall-test
spec:
  version: 1.31.1
  image: ghcr.io/bmorton/temporal-operator-e2e/does-not-exist:0.0.0
```

Merge this into the full spec from `01-temporalcluster.yaml` — chainsaw `apply` replaces the object, so the file must be complete. Copy the whole spec and change `version` plus add `image`.

`test/e2e/upgrade-stall/02-assert-stalled.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: stall-test
status:
  upgrade:
    fromVersion: 1.30.4
    toVersion: 1.31.1
    stalledService: frontend
  conditions:
    - type: UpgradeBlocked
      status: "True"
      reason: UpgradeStalled
    - type: Degraded
      status: "True"
```

`test/e2e/upgrade-stall/03-temporalcluster-repaired.yaml` — identical to `01-temporalcluster.yaml` (reverting `spec.version` to 1.30.4 and dropping the bad image), which exercises the webhook's revert escape hatch.

`test/e2e/upgrade-stall/03-assert-recovered.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: stall-test
status:
  conditions:
    - type: UpgradeBlocked
      status: "False"
    - type: Ready
      status: "True"
```

- [ ] **Step 3: Write the chainsaw test**

`test/e2e/upgrade-stall/chainsaw-test.yaml`:

```yaml
# Chainsaw upgrade-stall test: bring up a cluster at 1.30.4, start an upgrade
# that cannot roll out (unpullable image), assert the operator reports
# UpgradeBlocked rather than hanging silently, then revert spec.version and
# assert the cluster recovers. Requires the CNPG operator and a manager started
# with --upgrade-phase-timeout=1m.
apiVersion: chainsaw.kyverno.io/v1alpha1
kind: Test
metadata:
  name: upgrade-stall
spec:
  timeouts:
    apply: 1m
    assert: 10m
  steps:
    - name: provision-postgres
      try:
        - apply:
            file: ../postgres/01-fixtures-cnpg.yaml
        - assert:
            resource:
              apiVersion: postgresql.cnpg.io/v1
              kind: Cluster
              metadata:
                name: temporal-pg
              status:
                readyInstances: 1
        - apply:
            file: ../postgres/02-secrets.yaml
        - assert:
            resource:
              apiVersion: batch/v1
              kind: Job
              metadata:
                name: create-visibility-db
              status:
                succeeded: 1
    - name: deploy-healthy-cluster
      try:
        - apply:
            file: 01-temporalcluster.yaml
        - assert:
            file: 01-assert.yaml
    - name: start-upgrade-that-cannot-roll-out
      try:
        - apply:
            file: 02-temporalcluster-broken.yaml
        - assert:
            file: 02-assert-stalled.yaml
        - script:
            content: |
              set -euo pipefail
              # The stall must be reported as an event, not only a condition.
              kubectl -n $NAMESPACE get events \
                --field-selector reason=UpgradeStalled \
                -o jsonpath='{.items[0].message}' | grep -q 'frontend'
    - name: reject-retarget-while-stalled
      try:
        - script:
            content: |
              set -euo pipefail
              # A third version must be rejected by the webhook.
              if kubectl -n $NAMESPACE patch temporalcluster stall-test \
                   --type=merge -p '{"spec":{"version":"1.31.2"}}' 2>/dev/null; then
                echo "webhook accepted a third version mid-upgrade" >&2
                exit 1
              fi
    - name: revert-and-recover
      try:
        - apply:
            file: 03-temporalcluster-repaired.yaml
        - assert:
            file: 03-assert-recovered.yaml
```

- [ ] **Step 4: Register the suite in CI**

In `.github/workflows/e2e.yml`, add `upgrade-stall` to the workflow-dispatch `options` list (line 13), define the combo alongside the others (near line 38):

```bash
          upgradestall='{"temporal":"1.30.4","persistence":"upgrade","suite":"upgrade-stall"}'
```

add it to the `all` and default combo arrays, and add a dispatch case:

```bash
              upgrade-stall) echo "combos=[$upgradestall]" >> "$GITHUB_OUTPUT" ;;
```

In the same workflow, the Helm install step must pass the shortened timeout. Add to the `helm install`/`helm upgrade` arguments:

```sh
--set-string 'manager.args[0]=--leader-elect' \
--set-string 'manager.args[1]=--upgrade-phase-timeout=1m'
```

Read the existing install step first and extend its argument list rather than replacing it.

- [ ] **Step 5: Validate the chainsaw definition parses**

Run: `bin/chainsaw lint test --file test/e2e/upgrade-stall/chainsaw-test.yaml`
Expected: no errors. If `bin/chainsaw` is absent, run `make chainsaw` first (check the Makefile for the exact target name).

- [ ] **Step 6: Commit**

```bash
git add test/e2e/upgrade-stall/ .github/workflows/e2e.yml cmd/main.go internal/controller/
git commit -s -m "test(e2e): add upgrade-stall suite

Covers the failure mode with no prior e2e coverage: an upgrade that
cannot roll out must report UpgradeBlocked, reject retargeting, and
recover when spec.version is reverted."
```

---

### Task 18: Race detection in CI

**Files:**
- Modify: `Makefile:64-66`

- [ ] **Step 1: Run the suite under the race detector to find existing races**

Run:

```bash
KUBEBUILDER_ASSETS="$(bin/setup-envtest use 1.34.0 --bin-dir bin -p path)" \
  go test -race $(go list ./... | grep -v /e2e)
```

Expected: PASS. If the detector reports a race, **stop and fix it before changing the Makefile** — a race found here is a real bug and belongs in Phase 2's scope, not hidden behind a disabled flag. The most likely candidate is the dedupe map in `internal/events`, which is why it carries a mutex.

- [ ] **Step 2: Enable it permanently**

In `Makefile`, change the `test` target:

```make
.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test -race $$(go list ./... | grep -v /e2e) -coverprofile cover.out
```

- [ ] **Step 3: Verify**

Run: `make test`
Expected: PASS. Note the wall-clock increase; the race detector typically costs 2-3x. If this pushes CI past its timeout, raise the job timeout rather than dropping the flag.

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -s -m "test: enable the race detector in make test"
```

---

### Task 19: Document the new failure states

**Files:**
- Modify: `docs/content/docs/troubleshooting/_index.md`
- Modify: `docs/content/docs/architecture/_index.md`
- Modify: `docs/content/docs/operations/_index.md`

**Rationale:** these conditions are the interface operators will meet at 3am. An alert that fires with no documented response is only marginally better than silence.

- [ ] **Step 1: Add a troubleshooting section**

Append to `docs/content/docs/troubleshooting/_index.md`:

````markdown
## Stalled upgrades

When a service does not roll out to the new version within the upgrade phase
timeout (15 minutes by default, `--upgrade-phase-timeout`), the operator sets:

```
UpgradeBlocked=True  reason=UpgradeStalled
Degraded=True        reason=RolloutStalled
```

`status.upgrade.stalledService` names the service and `status.upgrade.message`
carries the Deployment's own reason — usually an image pull failure, a
crashlooping pod, or unschedulable replicas.

```sh
kubectl get temporalcluster my-cluster -o jsonpath='{.status.upgrade}' | jq
kubectl describe deployment my-cluster-frontend
```

The cluster keeps running mixed versions while blocked. The condition clears on
its own as soon as the rollout completes, so fixing the underlying cause is
usually all that is required.

To abandon the upgrade instead, set `spec.version` back to
`status.upgrade.fromVersion`. That is the only version change accepted while an
upgrade is in flight; any other value is rejected by the webhook. If the schema
has already migrated (`status.upgrade.rollbackable: false`) the revert is still
accepted, but the API server returns a warning: Temporal schema migrations are
forward-only, so the older binaries will run against the newer schema. Confirm
that combination is supported before proceeding.

## Failed schema migrations

A schema Job that fails is deleted and recreated after 1m, then 5m, then 15m.
While retrying:

```
SchemaReady=False  reason=SchemaMigrationRetrying
```

After the third failure the operator stops and reports:

```
SchemaReady=False  reason=SchemaMigrationFailed
Degraded=True      reason=SchemaMigrationFailed
```

The final failed Job is deliberately retained so its logs stay available:

```sh
kubectl -n <namespace> logs job/<cluster>-<store>-update-schema
```

`status.persistence.schemaAttempts` records the attempt count and the last
error. Resolve the cause (most often database credentials or connectivity), then
delete the failed Job to restart the cycle with a fresh budget.

## Abandoned cleanup

If a `TemporalNamespace`, `TemporalSchedule`, `TemporalSearchAttribute`, or
`TemporalClusterConnection` is deleted while its cluster is unreachable, the
operator retries for five minutes before releasing the finalizer so deletion can
complete. When that happens it emits a warning event:

```sh
kubectl get events --field-selector reason=CleanupAbandoned
```

The Kubernetes object is gone but the Temporal-side object was **not** removed.
Delete it manually with `temporal operator namespace delete` or the equivalent
for the resource type. The `temporal_operator_cleanup_abandoned_total` metric
counts these, and the `TemporalCleanupAbandoned` alert fires on any increase.

Deletion always terminates. The operator never leaves a resource stuck in
`Terminating` because a cluster is unreachable.
````

- [ ] **Step 2: Document the observability surface**

Append to `docs/content/docs/operations/_index.md`:

````markdown
## Metrics and alerts

Beyond controller-runtime's standard reconcile metrics, the operator exports:

| Metric | Type | Meaning |
| --- | --- | --- |
| `temporal_operator_resource_condition` | gauge | 1 when a condition is True. Labelled by `kind`, `namespace`, `name`, `type`, `status`, `reason`. Covers every condition on every Temporal resource. |
| `temporal_operator_upgrade_phase_seconds` | gauge | How long the current upgrade phase has been active. |
| `temporal_operator_schema_job_attempts` | gauge | Consecutive failed schema migration attempts per store. |
| `temporal_operator_cleanup_abandoned_total` | counter | Remote cleanups abandoned after the deadline. Any increase means an orphaned Temporal object. |
| `temporal_operator_target_unreachable_total` | counter | Failures to reach a Temporal target believed to exist. |

Because the condition metric is derived from resource status rather than written
by hand, any condition the operator sets is queryable without further
configuration. To find everything currently unhealthy:

```promql
temporal_operator_resource_condition{type="Degraded",status="True"} == 1
```

Alert rules ship in the chart and are enabled with the ServiceMonitor:

```sh
helm upgrade --install temporal-operator oci://ghcr.io/bmorton/charts/temporal-operator \
  --set prometheus.enable=true
```
````

- [ ] **Step 3: Update the architecture page**

In `docs/content/docs/architecture/_index.md`, append to the `TemporalCluster reconciliation` section:

````markdown
### Failure handling

Every sub-reconciler reports failures through conditions rather than logs alone:

- A rolling upgrade phase that exceeds `--upgrade-phase-timeout` sets
  `UpgradeBlocked` and `Degraded`, names the stalled service in
  `status.upgrade.stalledService`, and stops advancing. It resumes automatically
  when the rollout completes — the condition is a report, not a latch.
- A schema Job that exhausts its `BackoffLimit` is deleted and recreated on a
  bounded schedule (1m, 5m, 15m) before the operator gives up. Recreation is
  safe because the schema tools run without `--overwrite`.
- Deleting a satellite resource whose cluster is unreachable retries for
  `cleanupDeadline` before releasing the finalizer, so a transient outage does
  not orphan the Temporal-side object. A cluster that no longer exists is
  forgotten immediately, since there is nothing left to clean up.

Every one of these states is exported as
`temporal_operator_resource_condition` and covered by a shipped alert rule.
````

- [ ] **Step 4: Lint the docs**

Run: `npx markdownlint-cli2 "docs/content/**/*.md"`
Expected: 0 issues. Note the nested code fences in the troubleshooting section — use four-backtick outer fences if markdownlint objects.

- [ ] **Step 5: Commit**

```bash
git add docs/content/
git commit -s -m "docs: document stalled upgrades, failed migrations, and abandoned cleanup"
```

---

### Task 20: Final gate

- [ ] **Step 1: Full local verification**

```bash
make build && make test && make lint
```

Expected: all pass, with the race detector active.

- [ ] **Step 2: Confirm coverage improved**

```bash
go tool cover -func=cover.out | tail -1
```

Expected: above the 48.1% baseline recorded when this project started. The new failure paths are all tested, and Phase 1 removed untested duplication.

- [ ] **Step 3: Confirm generated artifacts are current**

```bash
make generate manifests helm-chart && git status --short
```

Expected: empty. Anything modified here means a generated file was not committed alongside its source change.

- [ ] **Step 4: Verify the spec's claims hold**

Walk the spec's behavior section and confirm each is now true:

```bash
# 1. Stalled upgrades are detected
grep -q "upgradePhaseTimeout" internal/controller/temporalcluster_upgrade.go && echo "stall detection: ok"
# 2. Mid-upgrade version changes are guarded
grep -q "validateVersionChangeDuringUpgrade" internal/webhook/v1alpha1/temporalcluster_webhook.go && echo "version guard: ok"
# 3. Schema jobs are recreated
grep -q "handleFailedSchemaJob" internal/controller/temporalcluster_persistence.go && echo "schema recovery: ok"
# 4. Cleanup is bounded
grep -q "decideCleanup" internal/controller/cleanup.go && echo "cleanup deadline: ok"
# 5. RPCs are bounded
grep -q "DialContext" internal/temporal/client.go && echo "grpc deadlines: ok"
# 6. Degraded and Progressing are used
grep -rq "ConditionDegraded" internal/controller/ && echo "degraded in use: ok"
grep -rq "ConditionProgressing" internal/controller/ && echo "progressing in use: ok"
# 7. Alerts exist
test -f config/prometheus/alerts.yaml && echo "alerts: ok"
```

Expected: eight `ok` lines.

- [ ] **Step 5: Open the pull request**

```bash
gh pr create \
  --title "feat: fail loudly, recover automatically" \
  --body "Implements docs/superpowers/specs/2026-07-25-fail-loudly-recover-automatically-design.md

Makes every abnormal state named by a condition, bounded in its recovery, and
observable in Prometheus.

- Stalled upgrade phases set UpgradeBlocked/Degraded instead of hanging silently
- Failed schema Jobs are recreated on a bounded schedule before giving up
- Cleanup retries a transient outage instead of orphaning Temporal objects,
  while preserving the issue #58 guarantee that deletion always terminates
- Temporal RPCs are bounded, so an unreachable frontend cannot stall a controller
- Every condition on every resource is exported as a metric, with shipped alerts
- Race detector enabled in make test"
```

---

## Notes for the implementer

**Read before editing.** Several tasks say "adapt to the existing fixtures" — that is deliberate. This codebase has established test helpers and naming conventions, and matching them matters more than matching the exact identifiers written here. Read the file you are about to change first.

**The #58 guarantee is load-bearing.** `docs/superpowers/specs/2026-06-18-stranded-finalizer-fix-design.md` fixed resources being stuck in `Terminating` forever. Task 10 refines that fix; it must not revert it. If any existing "removes the finalizer when the cluster is deleted before the ..." test fails, stop and fix the cause rather than the test.

**Never add `--overwrite` to a schema tool invocation.** The entire schema-Job recovery design rests on re-running being non-destructive.

**`dist/chart` is generated.** Edit `hack/helm/overrides/` and run `make helm-chart`. The `Verify generated chart` CI job fails on stale output.

**Timeouts are `var`, not `const`.** Every one exists as a variable specifically so tests can shorten it. Do not convert them to constants for tidiness.
