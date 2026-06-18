# Fix stranded finalizer in TemporalNamespace / TemporalSearchAttribute controllers

**Issue:** [#58](https://github.com/bmorton/temporal-operator/issues/58)
**Date:** 2026-06-18

## Problem

The `TemporalNamespace` and `TemporalSearchAttribute` reconcilers fetch the
referenced `TemporalCluster`, build the TLS config, and build the Temporal
client **before** checking `DeletionTimestamp`. Each of those steps returns
early on error.

If a user deletes the `TemporalCluster` first and then deletes the CR, the
cluster `Get` (or TLS / client build) fails and `Reconcile` returns early — so
`reconcileDelete` is never reached and the finalizer is never removed. The
object is stuck in `Terminating` forever and needs a manual `kubectl patch` to
strip the finalizer.

The sibling `TemporalSchedule` controller already fixed this in #57; the same
latent bug remains in the two other controllers.

## Affected code

- `internal/controller/temporalnamespace_controller.go`
- `internal/controller/temporalsearchattribute_controller.go`

## Fix

Mirror `internal/controller/temporalschedule_controller.go`. In each of the
three early-return branches — cluster `Get` error, `clusterTLSConfig` error,
and client-factory error — first check `!obj.DeletionTimestamp.IsZero()`. If the
object is being deleted, the remote cluster is unreachable and there is nothing
to clean up, so remove the finalizer and return a clean result.

Add a per-controller `removeFinalizerAndForget` helper with the same shape as
`TemporalScheduleReconciler.removeFinalizerAndForget` (lines 200–212 of the
schedule controller):

```go
func (r *...Reconciler) removeFinalizerAndForget(ctx context.Context, obj *...) error {
	if controllerutil.ContainsFinalizer(obj, <finalizer>) {
		controllerutil.RemoveFinalizer(obj, <finalizer>)
		if err := r.Update(ctx, obj); err != nil {
			return err
		}
	}
	return nil
}
```

The cluster `Get` branch keeps its existing non-deletion behaviour
(`ClusterNotFound` condition + requeue). Only the deletion path changes.

## Approach choice: per-controller helper (not a shared generic)

The issue floats factoring out the shared deletion-when-cluster-gone handling.
Each controller has a distinct finalizer constant and a distinct typed object,
and the existing convention is a per-controller `removeFinalizerAndForget`. We
mirror that per-controller pattern rather than introduce a generic abstraction —
it matches existing code, needs no generics, and keeps the change minimal
(YAGNI). The three controllers stay consistent by construction.

## Testing

Add one regression test per controller, mirroring the schedule test at
`temporalschedule_controller_test.go:309–337` ("removes the finalizer when the
cluster is deleted before the schedule"):

1. Create cluster + CR; reconcile so the finalizer is added.
2. Delete the `TemporalCluster`.
3. Delete the CR (it becomes `Terminating` due to the finalizer).
4. Reconcile once; assert the CR is fully gone (finalizer removed, `Get` errors).

## Verification

- `make build` — compiles clean.
- `make lint` — passes.
- `make test` (envtest) — the controller suites pass, including the two new
  regression tests.
