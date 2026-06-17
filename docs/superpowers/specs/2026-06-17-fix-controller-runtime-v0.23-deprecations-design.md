# Fix controller-runtime v0.23 deprecations (replace `//nolint:staticcheck`)

**Date:** 2026-06-17
**Status:** Approved

## Problem

Commit `cd2e361` (the controller-runtime v0.23 dependency bump) silenced three
`SA1019` deprecation warnings with `//nolint:staticcheck` directives instead of
migrating to the replacement APIs. One justification was also factually wrong:
it claimed server-side apply configurations "are not available for these
unstructured objects", but controller-runtime v0.23 ships
`client.ApplyConfigurationFromUnstructured`.

The three suppressed deprecations:

| # | Location | Deprecated API |
|---|----------|----------------|
| 1 | `cmd/main.go` | `mgr.GetEventRecorderFor` → `record.EventRecorder` |
| 2 | `internal/controller/temporalcluster_services.go` | `client.Apply` patch |
| 3 | `internal/controller/temporalclusterclient_controller.go` | `client.Apply` patch |

## Goal

Remove all three `//nolint:staticcheck` directives by migrating to the
non-deprecated v0.23 APIs. `make lint` must report zero `SA1019` for these
sites.

## Design

### #2 & #3 — `client.Apply` → `Client.Apply`

Add one shared helper in the `controller` package:

```go
func serverSideApply(ctx context.Context, c client.Client, scheme *runtime.Scheme,
    obj client.Object, owner client.FieldOwner) error {
    gvk, err := apiutil.GVKForObject(obj, scheme) // typed objects lack TypeMeta
    if err != nil { return err }
    raw, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
    if err != nil { return err }
    u := &unstructured.Unstructured{Object: raw}
    u.GetObjectKind().SetGroupVersionKind(gvk)
    return c.Apply(ctx, client.ApplyConfigurationFromUnstructured(u),
        owner, client.ForceOwnership)
}
```

- `temporalcluster_services.go` `apply()` and the client-cert apply in
  `temporalclusterclient_controller.go` call this helper.
- Both `//nolint:staticcheck` directives removed.
- `client.FieldOwner` and `client.ForceOwnership` both implement `ApplyToApply`
  (verified), so they pass through unchanged.

### #1 — event recorder → new events API

The new `events.EventRecorder.Eventf` signature is
`Eventf(regarding, related runtime.Object, eventtype, reason, action, note string, ...)`.

- `temporalcluster_controller.go`: field `Recorder record.EventRecorder` →
  `events.EventRecorder`; import swap `k8s.io/client-go/tools/record` →
  `k8s.io/client-go/tools/events`.
- `cmd/main.go`: `Recorder: mgr.GetEventRecorder("temporalcluster-controller")`;
  remove the `//nolint`.
- `event()` helper becomes
  `r.Recorder.Eventf(cluster, nil, eventType, reason, reason, message)` —
  `reason` doubles as the machine-readable `action`; `related` is `nil`.
- The inline `ClusterReady` emission in `temporalcluster_services.go` routes
  through the `event()` helper for consistency (centralizes the nil-check).
- Call-site signatures (`UpgradeStarted`, `UpgradePhase`, `UpgradeComplete`,
  `ClusterReady`) stay unchanged.

## Trade-offs (accepted)

- Emitted events change from `core/v1` to `events.k8s.io/v1` objects (different
  `kubectl get events` rendering). Cosmetic; acceptable for an early,
  single-user project.
- No event-asserting tests exist, so test breakage risk is low.

## Verification

`make generate manifests` (expected no-op — no API type change), `make build`,
`make lint` (zero `SA1019`), `make test`.
