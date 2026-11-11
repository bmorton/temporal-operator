# Cluster Readiness Watch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make cluster-dependent CRs reconcile the instant their referenced Temporal cluster becomes Ready, and stop transient frontend-not-serving errors from triggering exponential backoff.

**Architecture:** Add a shared readiness-change predicate, per-controller `Watches` on `TemporalCluster`/`TemporalDevServer` that map a changed target to the dependent CRs referencing it, and a shared transient-error classifier so the four RPC-registering controllers requeue on a short fixed interval instead of returning errors. `TemporalClusterConnection` gets the watch only (it already swallows transient per-peer errors).

**Tech Stack:** Go, controller-runtime (v0.23), gRPC (v1.82), Ginkgo/Gomega + envtest, controller-runtime fake client, chainsaw (e2e).

## Global Constraints

- Pre-1.0 project; keep changes focused, no unrequested breaking changes.
- No API/CRD schema changes (adding a plain Go const to `conditions.go` is allowed; it is not a schema field). No `make generate manifests` / `make helm-chart` needed.
- Conventional Commits; every commit signed off with `-s` (DCO enforced).
- Include commit trailers on every commit:
  `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>`
  and `Copilot-Session: 136e3309-64e9-4f7a-8a1d-4bab174e96e1`.
- `resolveTarget` semantics: a `ClusterReference` always points at a target in the CR's **own** namespace; empty `ClusterRef.Kind` defaults to `TemporalCluster`.
- Reason string constants live in `api/v1alpha1/conditions.go`.
- Run `make lint` and targeted `make test` (envtest) before the PR.

---

### Task 1: Shared watch predicate, target matcher, and transient-error classifier

**Files:**
- Create: `internal/controller/cluster_watch.go`
- Modify: `api/v1alpha1/conditions.go` (add one reason const, before the closing `)` at line 87)
- Test: `internal/controller/cluster_watch_test.go`

**Interfaces:**
- Consumes: `temporalv1alpha1.{TemporalCluster,TemporalDevServer,ClusterReference,ConditionReady,ClusterKindTemporalCluster,ClusterKindTemporalDevServer}`.
- Produces (used by Tasks 2-6):
  - `clusterUnavailableRequeue time.Duration` (= 5s)
  - `func isTransientClusterErr(err error) bool`
  - `func refTargets(ref temporalv1alpha1.ClusterReference, kind, name string) bool`
  - `func targetReadyStatus(obj client.Object) metav1.ConditionStatus`
  - `var clusterReadinessChanged predicate.Predicate`
  - `temporalv1alpha1.ReasonFrontendUnavailable` (= `"FrontendUnavailable"`)

- [ ] **Step 1: Write the failing test**

Create `internal/controller/cluster_watch_test.go`:

```go
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
```

Note: `errors` is imported only for the `"plain error"` case in `TestIsTransientClusterErr`; keep it. `fmt` is used by the `"fmt-wrapped unavailable"` case.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/controller/ -run 'TestIsTransientClusterErr|TestRefTargets|TestClusterReadinessChanged' -v`
Expected: FAIL to compile — `undefined: isTransientClusterErr`, `refTargets`, `clusterReadinessChanged`.

- [ ] **Step 3: Add the reason constant**

In `api/v1alpha1/conditions.go`, add before the closing `)` (currently line 87):

```go
	// ReasonFrontendUnavailable indicates the Temporal frontend is reachable
	// but not yet accepting RPCs (transient startup window).
	ReasonFrontendUnavailable = "FrontendUnavailable"
```

- [ ] **Step 4: Create `internal/controller/cluster_watch.go`**

```go
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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	temporalv1alpha1 "github.com/bmorton/temporal-operator/api/v1alpha1"
)

// clusterUnavailableRequeue is the fixed, short delay used when a Temporal
// frontend is reachable but not yet accepting RPCs. Requeuing on a fixed
// interval (rather than returning an error) keeps the dependent object off
// controller-runtime's exponential backoff queue, so it registers promptly once
// the frontend finishes starting.
const clusterUnavailableRequeue = 5 * time.Second

// isTransientClusterErr reports whether err is a transient connectivity error
// from a Temporal frontend that is up but not yet serving RPCs. Such errors
// should be retried on a short fixed interval rather than triggering
// exponential backoff. Terminal errors (invalid arguments, permission denied,
// bad TLS material, etc.) return false so they surface as real reconcile
// errors. status.Code unwraps fmt-wrapped errors via errors.As.
func isTransientClusterErr(err error) bool {
	if err == nil {
		return false
	}
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
		return true
	default:
		return false
	}
}

// refTargets reports whether ref points at the target named name of the given
// kind. An empty ref.Kind defaults to TemporalCluster (matching resolveTarget).
func refTargets(ref temporalv1alpha1.ClusterReference, kind, name string) bool {
	refKind := ref.Kind
	if refKind == "" {
		refKind = temporalv1alpha1.ClusterKindTemporalCluster
	}
	return refKind == kind && ref.Name == name
}

// targetReadyStatus returns the Ready condition status of a watched Temporal
// target (TemporalCluster or TemporalDevServer), or ConditionUnknown if the
// object is neither type or has no Ready condition.
func targetReadyStatus(obj client.Object) metav1.ConditionStatus {
	var conds []metav1.Condition
	switch o := obj.(type) {
	case *temporalv1alpha1.TemporalCluster:
		conds = o.Status.Conditions
	case *temporalv1alpha1.TemporalDevServer:
		conds = o.Status.Conditions
	default:
		return metav1.ConditionUnknown
	}
	if c := meta.FindStatusCondition(conds, temporalv1alpha1.ConditionReady); c != nil {
		return c.Status
	}
	return metav1.ConditionUnknown
}

// clusterReadinessChanged limits watch-driven enqueues of dependent CRs to
// meaningful target changes: a create (so an already-Ready target still triggers
// dependents created afterward), a generation change, or a transition of the
// Ready condition. Routine status writes that do not move Ready are ignored to
// avoid re-reconciling every dependent on each cluster status update.
var clusterReadinessChanged predicate.Predicate = predicate.Funcs{
	CreateFunc: func(event.CreateEvent) bool { return true },
	DeleteFunc: func(event.DeleteEvent) bool { return false },
	UpdateFunc: func(e event.UpdateEvent) bool {
		if e.ObjectOld == nil || e.ObjectNew == nil {
			return false
		}
		if e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() {
			return true
		}
		return targetReadyStatus(e.ObjectOld) != targetReadyStatus(e.ObjectNew)
	},
	GenericFunc: func(event.GenericEvent) bool { return false },
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/controller/ -run 'TestIsTransientClusterErr|TestRefTargets|TestClusterReadinessChanged' -v`
Expected: PASS (all subtests).

- [ ] **Step 6: Commit**

```bash
git add internal/controller/cluster_watch.go internal/controller/cluster_watch_test.go api/v1alpha1/conditions.go
git commit -s -m "feat(controller): add shared cluster-readiness watch helpers" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 136e3309-64e9-4f7a-8a1d-4bab174e96e1"
```

---

### Task 2: TemporalNamespace — watch cluster readiness + avoid backoff

**Files:**
- Modify: `internal/controller/temporalnamespace_controller.go` (Reconcile ~lines 102-105; add `mapClusterToNamespaces`; SetupWithManager lines 305-310)
- Modify: `internal/controller/temporalnamespace_controller_test.go` (add `registerErr` to `fakeNamespaceClient`, add transient test)

**Interfaces:**
- Consumes (from Task 1): `isTransientClusterErr`, `clusterUnavailableRequeue`, `refTargets`, `clusterReadinessChanged`, `temporalv1alpha1.ReasonFrontendUnavailable`.
- Produces: `func (r *TemporalNamespaceReconciler) mapClusterToNamespaces(kind string) handler.MapFunc`.

- [ ] **Step 1: Write the failing test**

In `internal/controller/temporalnamespace_controller_test.go`, add a `registerErr` field to the fake (in the struct at lines 36-41) and honor it in `Register`:

```go
// add to fakeNamespaceClient struct:
	registerErr error
```

```go
// modify Register to fail transiently when set:
func (f *fakeNamespaceClient) Register(_ context.Context, p temporal.NamespaceParams) error {
	if f.registerErr != nil {
		return f.registerErr
	}
	f.registered = append(f.registered, p.Name)
	// ... existing body unchanged ...
}
```

Add a new spec inside the existing `Describe("TemporalNamespace reconciler", ...)` block (uses the file's existing `readyCluster`, `reconciler`, `ctx`, and `k8sClient`):

```go
It("requeues without error when the frontend is transiently unavailable", func() {
	clusterName := readyCluster()
	nsName := fmt.Sprintf("ns-transient-%d", counter)
	ns := &temporalv1alpha1.TemporalNamespace{
		ObjectMeta: metav1.ObjectMeta{Name: nsName, Namespace: "default"},
		Spec: temporalv1alpha1.TemporalNamespaceSpec{
			ClusterRef: temporalv1alpha1.ClusterReference{Name: clusterName},
		},
	}
	Expect(k8sClient.Create(ctx, ns)).To(Succeed())

	fake = &fakeNamespaceClient{
		store:       map[string]*temporal.NamespaceInfo{},
		registerErr: status.Error(codes.Unavailable, "frontend starting"),
	}

	res, err := reconciler().Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: nsName, Namespace: "default"},
	})
	Expect(err).NotTo(HaveOccurred())
	Expect(res.RequeueAfter).To(Equal(clusterUnavailableRequeue))

	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: nsName, Namespace: "default"}, ns)).To(Succeed())
	cond := meta.FindStatusCondition(ns.Status.Conditions, temporalv1alpha1.ConditionReady)
	Expect(cond).NotTo(BeNil())
	Expect(cond.Reason).To(Equal(temporalv1alpha1.ReasonFrontendUnavailable))
})
```

Add these imports to the test file's import block:

```go
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
```

(`meta`, `metav1`, `types`, `reconcile`, `temporalv1alpha1`, `temporal`, `fmt` are already imported.)

- [ ] **Step 2: Run test to verify it fails**

Run: `make test` is heavy; run just this package's envtest suite:
`KUBEBUILDER_ASSETS="$(setup-envtest use -p path)" go test ./internal/controller/ -run TestControllers -v 2>&1 | tail -40`
(If `setup-envtest` is not on PATH, run `make setup-envtest` first, or use `make test`.)
Expected: FAIL — the current code returns a wrapped error (`return ctrl.Result{}, err`), so `Reconcile` returns an error and `res.RequeueAfter` is zero.

- [ ] **Step 3: Wrap the transient error in Reconcile**

In `temporalnamespace_controller.go`, replace the block at lines 102-105:

```go
	info, failover, err := r.ensureRegistered(ctx, &ns, tc)
	if err != nil {
		return ctrl.Result{}, err
	}
```

with:

```go
	info, failover, err := r.ensureRegistered(ctx, &ns, tc)
	if err != nil {
		if isTransientClusterErr(err) {
			r.setReady(&ns, metav1.ConditionFalse, temporalv1alpha1.ReasonFrontendUnavailable,
				"waiting for the Temporal frontend to become available")
			return ctrl.Result{RequeueAfter: clusterUnavailableRequeue}, r.statusUpdate(ctx, &ns)
		}
		return ctrl.Result{}, err
	}
```

- [ ] **Step 4: Add the map function and wire the watches**

Add this method (near `SetupWithManager`):

```go
// mapClusterToNamespaces enqueues every TemporalNamespace in the changed
// target's namespace whose ClusterRef points at it, so a namespace reconciles
// immediately when its cluster becomes Ready instead of waiting for a requeue.
func (r *TemporalNamespaceReconciler) mapClusterToNamespaces(kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var list temporalv1alpha1.TemporalNamespaceList
		if err := r.List(ctx, &list, client.InNamespace(obj.GetNamespace())); err != nil {
			return nil
		}
		var reqs []reconcile.Request
		for i := range list.Items {
			item := &list.Items[i]
			if refTargets(item.Spec.ClusterRef, kind, obj.GetName()) {
				reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{
					Namespace: item.Namespace, Name: item.Name,
				}})
			}
		}
		return reqs
	}
}
```

Replace `SetupWithManager` (lines 305-310) with:

```go
// SetupWithManager sets up the controller with the Manager.
func (r *TemporalNamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalNamespace{}).
		Watches(&temporalv1alpha1.TemporalCluster{},
			handler.EnqueueRequestsFromMapFunc(r.mapClusterToNamespaces(temporalv1alpha1.ClusterKindTemporalCluster)),
			builder.WithPredicates(clusterReadinessChanged)).
		Watches(&temporalv1alpha1.TemporalDevServer{},
			handler.EnqueueRequestsFromMapFunc(r.mapClusterToNamespaces(temporalv1alpha1.ClusterKindTemporalDevServer)),
			builder.WithPredicates(clusterReadinessChanged)).
		Named("temporalnamespace").
		Complete(r)
}
```

Add to the imports block of `temporalnamespace_controller.go`:

```go
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
```

- [ ] **Step 5: Add a map-function unit test (fake client)**

Append to `internal/controller/cluster_watch_test.go` (this test compiles only after the map func from Step 4 exists). It verifies same-namespace filtering, ref matching, and empty-Kind defaulting without needing envtest:

```go
func TestMapClusterToNamespaces(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := temporalv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	mkNS := func(name, ns, refName, refKind string) *temporalv1alpha1.TemporalNamespace {
		return &temporalv1alpha1.TemporalNamespace{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: temporalv1alpha1.TemporalNamespaceSpec{
				ClusterRef: temporalv1alpha1.ClusterReference{Name: refName, Kind: refKind},
			},
		}
	}

	match := mkNS("match", "team-a", "c1", "")                                             // empty kind -> cluster
	otherNS := mkNS("other-ns", "team-b", "c1", "")                                        // different k8s namespace
	otherCluster := mkNS("other-cluster", "team-a", "c2", "")                              // different cluster
	devRef := mkNS("dev-ref", "team-a", "c1", temporalv1alpha1.ClusterKindTemporalDevServer) // wrong kind

	c := fake.NewClientBuilder().WithScheme(scheme).
		WithObjects(match, otherNS, otherCluster, devRef).Build()
	r := &TemporalNamespaceReconciler{Client: c, Scheme: scheme}

	cluster := &temporalv1alpha1.TemporalCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "c1", Namespace: "team-a"},
	}
	reqs := r.mapClusterToNamespaces(temporalv1alpha1.ClusterKindTemporalCluster)(context.Background(), cluster)

	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d: %v", len(reqs), reqs)
	}
	if reqs[0].Name != "match" || reqs[0].Namespace != "team-a" {
		t.Fatalf("unexpected request: %v", reqs[0])
	}
}
```

Add these imports to `cluster_watch_test.go`:

```go
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `make test` (or the targeted envtest command from Step 2), plus `go test ./internal/controller/ -run TestMapClusterToNamespaces -v`.
Expected: PASS, including the new transient-unavailable spec and the map-function test.

- [ ] **Step 7: Commit**

```bash
git add internal/controller/temporalnamespace_controller.go internal/controller/temporalnamespace_controller_test.go internal/controller/cluster_watch_test.go
git commit -s -m "feat(controller): reconcile namespaces on cluster readiness" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 136e3309-64e9-4f7a-8a1d-4bab174e96e1"
```

---

### Task 3: TemporalSchedule — watch cluster readiness + avoid backoff

**Files:**
- Modify: `internal/controller/temporalschedule_controller.go` (Reconcile lines 103-105; add map func; SetupWithManager lines 231-237)

**Interfaces:**
- Consumes (Task 1): `isTransientClusterErr`, `clusterUnavailableRequeue`, `refTargets`, `clusterReadinessChanged`, `temporalv1alpha1.ReasonFrontendUnavailable`.
- Produces: `func (r *TemporalScheduleReconciler) mapClusterToSchedules(kind string) handler.MapFunc`.

- [ ] **Step 1: Wrap the transient error in Reconcile**

Replace lines 103-105:

```go
	if err := r.reconcileSchedule(ctx, &sched, sc); err != nil {
		return ctrl.Result{}, err
	}
```

with:

```go
	if err := r.reconcileSchedule(ctx, &sched, sc); err != nil {
		if isTransientClusterErr(err) {
			r.setReady(&sched, metav1.ConditionFalse, temporalv1alpha1.ReasonFrontendUnavailable,
				"waiting for the Temporal frontend to become available")
			return ctrl.Result{RequeueAfter: clusterUnavailableRequeue}, r.statusUpdate(ctx, &sched)
		}
		return ctrl.Result{}, err
	}
```

- [ ] **Step 2: Add the map function and wire the watches**

Add near `SetupWithManager`:

```go
// mapClusterToSchedules enqueues every TemporalSchedule in the changed target's
// namespace whose ClusterRef points at it.
func (r *TemporalScheduleReconciler) mapClusterToSchedules(kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var list temporalv1alpha1.TemporalScheduleList
		if err := r.List(ctx, &list, client.InNamespace(obj.GetNamespace())); err != nil {
			return nil
		}
		var reqs []reconcile.Request
		for i := range list.Items {
			item := &list.Items[i]
			if refTargets(item.Spec.ClusterRef, kind, obj.GetName()) {
				reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{
					Namespace: item.Namespace, Name: item.Name,
				}})
			}
		}
		return reqs
	}
}
```

Replace `SetupWithManager`:

```go
// SetupWithManager sets up the controller with the Manager.
func (r *TemporalScheduleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalSchedule{}).
		Watches(&temporalv1alpha1.TemporalCluster{},
			handler.EnqueueRequestsFromMapFunc(r.mapClusterToSchedules(temporalv1alpha1.ClusterKindTemporalCluster)),
			builder.WithPredicates(clusterReadinessChanged)).
		Watches(&temporalv1alpha1.TemporalDevServer{},
			handler.EnqueueRequestsFromMapFunc(r.mapClusterToSchedules(temporalv1alpha1.ClusterKindTemporalDevServer)),
			builder.WithPredicates(clusterReadinessChanged)).
		Named("temporalschedule").
		Complete(r)
}
```

Add to imports:

```go
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
```

- [ ] **Step 3: Build and run existing schedule tests**

Run: `go build ./... && make test`
Expected: PASS. Existing schedule specs still pass; code compiles with the new imports.

- [ ] **Step 4: Commit**

```bash
git add internal/controller/temporalschedule_controller.go
git commit -s -m "feat(controller): reconcile schedules on cluster readiness" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 136e3309-64e9-4f7a-8a1d-4bab174e96e1"
```

---

### Task 4: TemporalSearchAttribute — watch cluster readiness + avoid backoff

**Files:**
- Modify: `internal/controller/temporalsearchattribute_controller.go` (Reconcile lines 96-99; add map func; SetupWithManager lines 200-206)

**Interfaces:**
- Consumes (Task 1): same helpers as Task 3.
- Produces: `func (r *TemporalSearchAttributeReconciler) mapClusterToSearchAttributes(kind string) handler.MapFunc`.

- [ ] **Step 1: Wrap the transient error in Reconcile**

Replace lines 96-99:

```go
	registered, err := r.ensureRegistered(ctx, &sa, sac)
	if err != nil {
		return ctrl.Result{}, err
	}
```

with:

```go
	registered, err := r.ensureRegistered(ctx, &sa, sac)
	if err != nil {
		if isTransientClusterErr(err) {
			r.setReady(&sa, metav1.ConditionFalse, temporalv1alpha1.ReasonFrontendUnavailable,
				"waiting for the Temporal frontend to become available")
			return ctrl.Result{RequeueAfter: clusterUnavailableRequeue}, r.statusUpdate(ctx, &sa)
		}
		return ctrl.Result{}, err
	}
```

- [ ] **Step 2: Add the map function and wire the watches**

Add near `SetupWithManager`:

```go
// mapClusterToSearchAttributes enqueues every TemporalSearchAttribute in the
// changed target's namespace whose ClusterRef points at it.
func (r *TemporalSearchAttributeReconciler) mapClusterToSearchAttributes(kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var list temporalv1alpha1.TemporalSearchAttributeList
		if err := r.List(ctx, &list, client.InNamespace(obj.GetNamespace())); err != nil {
			return nil
		}
		var reqs []reconcile.Request
		for i := range list.Items {
			item := &list.Items[i]
			if refTargets(item.Spec.ClusterRef, kind, obj.GetName()) {
				reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{
					Namespace: item.Namespace, Name: item.Name,
				}})
			}
		}
		return reqs
	}
}
```

Replace `SetupWithManager`:

```go
// SetupWithManager sets up the controller with the Manager.
func (r *TemporalSearchAttributeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalSearchAttribute{}).
		Watches(&temporalv1alpha1.TemporalCluster{},
			handler.EnqueueRequestsFromMapFunc(r.mapClusterToSearchAttributes(temporalv1alpha1.ClusterKindTemporalCluster)),
			builder.WithPredicates(clusterReadinessChanged)).
		Watches(&temporalv1alpha1.TemporalDevServer{},
			handler.EnqueueRequestsFromMapFunc(r.mapClusterToSearchAttributes(temporalv1alpha1.ClusterKindTemporalDevServer)),
			builder.WithPredicates(clusterReadinessChanged)).
		Named("temporalsearchattribute").
		Complete(r)
}
```

Add to imports:

```go
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
```

- [ ] **Step 3: Build and run existing tests**

Run: `go build ./... && make test`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/controller/temporalsearchattribute_controller.go
git commit -s -m "feat(controller): reconcile search attributes on cluster readiness" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 136e3309-64e9-4f7a-8a1d-4bab174e96e1"
```

---

### Task 5: TemporalWorkflowRun — watch cluster readiness + avoid backoff

**Files:**
- Modify: `internal/controller/temporalworkflowrun_controller.go` (Reconcile line 110; add map func; SetupWithManager lines 260-266)

**Interfaces:**
- Consumes (Task 1): same helpers as Task 3.
- Produces: `func (r *TemporalWorkflowRunReconciler) mapClusterToWorkflowRuns(kind string) handler.MapFunc`.

Note: `reconcileRun` returns `(ctrl.Result, error)`, so wrap at the call site (line 110) by capturing both values.

- [ ] **Step 1: Wrap the transient error in Reconcile**

Replace line 110:

```go
	return r.reconcileRun(ctx, &run, wc, target.WorkflowRunPolicy)
```

with:

```go
	res, err := r.reconcileRun(ctx, &run, wc, target.WorkflowRunPolicy)
	if err != nil && isTransientClusterErr(err) {
		r.setReady(&run, metav1.ConditionFalse, temporalv1alpha1.ReasonFrontendUnavailable,
			"waiting for the Temporal frontend to become available")
		return ctrl.Result{RequeueAfter: clusterUnavailableRequeue}, r.statusUpdate(ctx, &run)
	}
	return res, err
```

- [ ] **Step 2: Add the map function and wire the watches**

Add near `SetupWithManager`:

```go
// mapClusterToWorkflowRuns enqueues every TemporalWorkflowRun in the changed
// target's namespace whose ClusterRef points at it.
func (r *TemporalWorkflowRunReconciler) mapClusterToWorkflowRuns(kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var list temporalv1alpha1.TemporalWorkflowRunList
		if err := r.List(ctx, &list, client.InNamespace(obj.GetNamespace())); err != nil {
			return nil
		}
		var reqs []reconcile.Request
		for i := range list.Items {
			item := &list.Items[i]
			if refTargets(item.Spec.ClusterRef, kind, obj.GetName()) {
				reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{
					Namespace: item.Namespace, Name: item.Name,
				}})
			}
		}
		return reqs
	}
}
```

Replace `SetupWithManager`:

```go
// SetupWithManager sets up the controller with the Manager.
func (r *TemporalWorkflowRunReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalWorkflowRun{}).
		Watches(&temporalv1alpha1.TemporalCluster{},
			handler.EnqueueRequestsFromMapFunc(r.mapClusterToWorkflowRuns(temporalv1alpha1.ClusterKindTemporalCluster)),
			builder.WithPredicates(clusterReadinessChanged)).
		Watches(&temporalv1alpha1.TemporalDevServer{},
			handler.EnqueueRequestsFromMapFunc(r.mapClusterToWorkflowRuns(temporalv1alpha1.ClusterKindTemporalDevServer)),
			builder.WithPredicates(clusterReadinessChanged)).
		Named("temporalworkflowrun").
		Complete(r)
}
```

Add to imports (some may already be present — only add missing ones):

```go
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
```

- [ ] **Step 3: Build and run existing tests**

Run: `go build ./... && make test`
Expected: PASS. (Verify `types`/`reconcile` are not double-imported; the test file imports `reconcile` but the controller file may not — check `goimports`.)

- [ ] **Step 4: Commit**

```bash
git add internal/controller/temporalworkflowrun_controller.go
git commit -s -m "feat(controller): reconcile workflow runs on cluster readiness" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 136e3309-64e9-4f7a-8a1d-4bab174e96e1"
```

---

### Task 6: TemporalClusterConnection — watch cluster readiness (watch only)

**Files:**
- Modify: `internal/controller/temporalclusterconnection_controller.go` (add map func; SetupWithManager lines 403-409)

**Interfaces:**
- Consumes (Task 1): `refTargets`, `clusterReadinessChanged`.
- Produces: `func (r *TemporalClusterConnectionReconciler) mapClusterToConnections(kind string) handler.MapFunc`.

Note: no transient-error wrapping — this controller already treats per-peer failures as not-ready and never returns them as errors. A connection references clusters via `Spec.Peers[].ClusterRef` (a `*ClusterReference`), so match when **any** local peer targets the changed cluster.

- [ ] **Step 1: Add the map function**

```go
// mapClusterToConnections enqueues every TemporalClusterConnection in the
// changed target's namespace that has a local peer whose ClusterRef points at
// it, so replication peers reconnect promptly when a cluster becomes Ready.
func (r *TemporalClusterConnectionReconciler) mapClusterToConnections(kind string) handler.MapFunc {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		var list temporalv1alpha1.TemporalClusterConnectionList
		if err := r.List(ctx, &list, client.InNamespace(obj.GetNamespace())); err != nil {
			return nil
		}
		var reqs []reconcile.Request
		for i := range list.Items {
			item := &list.Items[i]
			for _, p := range item.Spec.Peers {
				if p.ClusterRef != nil && refTargets(*p.ClusterRef, kind, obj.GetName()) {
					reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{
						Namespace: item.Namespace, Name: item.Name,
					}})
					break
				}
			}
		}
		return reqs
	}
}
```

- [ ] **Step 2: Wire the watches**

Replace `SetupWithManager` (lines 403-409):

```go
// SetupWithManager sets up the controller with the Manager.
func (r *TemporalClusterConnectionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&temporalv1alpha1.TemporalClusterConnection{}).
		Watches(&temporalv1alpha1.TemporalCluster{},
			handler.EnqueueRequestsFromMapFunc(r.mapClusterToConnections(temporalv1alpha1.ClusterKindTemporalCluster)),
			builder.WithPredicates(clusterReadinessChanged)).
		Watches(&temporalv1alpha1.TemporalDevServer{},
			handler.EnqueueRequestsFromMapFunc(r.mapClusterToConnections(temporalv1alpha1.ClusterKindTemporalDevServer)),
			builder.WithPredicates(clusterReadinessChanged)).
		Named("temporalclusterconnection").
		Complete(r)
}
```

Add to imports:

```go
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
```

Confirm the peer field name: `Spec.Peers[].ClusterRef` is `*temporalv1alpha1.ClusterReference` (see `resolvePeers` at `temporalclusterconnection_controller.go:130`, which does `if p.ClusterRef != nil`). If the field/type differs, adjust the nil-check and deref accordingly.

- [ ] **Step 3: Build and run existing tests**

Run: `go build ./... && make test`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/controller/temporalclusterconnection_controller.go
git commit -s -m "feat(controller): reconcile cluster connections on cluster readiness" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 136e3309-64e9-4f7a-8a1d-4bab174e96e1"
```

---

### Task 7: e2e — create cluster and namespace concurrently

**Files:**
- Modify: `test/e2e/namespace/chainsaw-test.yaml`

**Interfaces:**
- Consumes: the watch behavior from Task 2 (namespace registers promptly once the cluster is Ready).

Goal: prove the fix end-to-end by applying the cluster and the namespace in the **same** step (concurrently) instead of asserting the cluster is Ready first, then asserting the namespace registers. This also trims wall-clock time.

- [ ] **Step 1: Inspect the current test**

Run: `sed -n '1,80p' test/e2e/namespace/chainsaw-test.yaml`
Expected: current steps are `provision-postgres` → `cluster` (apply + assert cluster ready) → `register-namespace` (apply + assert namespace).

- [ ] **Step 2: Merge cluster + namespace application into one step**

Edit `test/e2e/namespace/chainsaw-test.yaml` so the `cluster` and `register-namespace` steps become a single step that applies both the cluster manifest and `01-namespace.yaml` **before** any assert, then asserts both. Concretely, replace the separate `cluster` and `register-namespace` steps with:

```yaml
    - name: cluster-and-namespace
      try:
        - apply:
            file: ../postgres/lifecycle/01-temporalcluster.yaml
        - apply:
            file: 01-namespace.yaml
        - assert:
            file: ../postgres/lifecycle/01-assert.yaml
        - assert:
            file: 01-assert.yaml
        - script:
            content: |
              kubectl run -n $NAMESPACE tctl-list --rm -i --restart=Never \
                --image=temporalio/admin-tools:1.31.1 -- \
                temporal operator namespace describe \
                --address temporal-lifecycle-frontend:7233 orders
```

Keep the trailing part of the original `register-namespace` step (any `catch`/`finally`/additional script lines) intact — only the step boundary and ordering change. Do not modify `01-namespace.yaml` or `01-assert.yaml`.

- [ ] **Step 3: Validate the chainsaw manifest parses**

Run: `chainsaw lint --test-file test/e2e/namespace/chainsaw-test.yaml` (if `chainsaw` is installed) or `yq '.' test/e2e/namespace/chainsaw-test.yaml >/dev/null` to confirm valid YAML.
Expected: no parse/lint errors. (Full e2e requires a live cluster + CNPG operator and runs in CI; do not attempt to run the suite locally unless that environment is available.)

- [ ] **Step 4: Commit**

```bash
git add test/e2e/namespace/chainsaw-test.yaml
git commit -s -m "test(e2e): create cluster and namespace concurrently" \
  -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>" \
  -m "Copilot-Session: 136e3309-64e9-4f7a-8a1d-4bab174e96e1"
```

---

### Task 8: Final verification

**Files:** none (verification only).

- [ ] **Step 1: Lint**

Run: `make lint`
Expected: no new findings. Fix any `goimports`/ordering issues introduced by the new imports.

- [ ] **Step 2: Full test suite**

Run: `make test`
Expected: PASS (unit + envtest).

- [ ] **Step 3: Confirm no generated artifacts are stale**

Run: `git status --porcelain`
Expected: clean (only the intended source/test/e2e/doc changes were committed; no `dist/chart` or `config/crd` diffs — there were no API schema changes).
