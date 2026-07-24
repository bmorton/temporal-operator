# Restart Temporal services on dynamicConfig change

**Status:** Approved design — ready for implementation planning.

## Problem

Changing `spec.dynamicConfig` on a `TemporalCluster` updates the rendered
dynamic-config `ConfigMap`, but the running service pods are **not** restarted,
so the new values do not take effect until an operator manually rolls the
Deployments.

The operator already solves the equivalent problem for **static** config and
for **mTLS certificates** by stamping a content hash onto the pod template: any
change to the hash mutates the pod template and triggers a rolling update.

- `internal/resources/labels.go` defines `ConfigHashAnnotation`
  (`temporal.bmor10.com/config-hash`) and `CertHashAnnotation`
  (`temporal.bmor10.com/cert-hash`).
- `internal/resources/deployment.go` (`BuildDeployment`, ~line 227) stamps
  `podAnnotations[ConfigHashAnnotation] = configHash` and, when mTLS is enabled,
  `podAnnotations[CertHashAnnotation] = mtls.CertHash`.
- The hash is computed by `resources.ConfigHash(content)`
  (`internal/resources/config.go:50`): `sha256` truncated to 16 hex chars.

The gap: the config hash is computed **only** from the rendered static config
(`resources.ConfigHash(rendered.config)` in
`internal/controller/temporalcluster_services.go:56` and
`internal/plan/plan.go:91`). The rendered dynamic config
(`rendered.dynamicConfig`) is threaded into the `ConfigMap` via
`plan.ServicesInput.RenderedDynamicConfig` but never contributes to any
pod-template annotation. Therefore dynamicConfig-only changes never roll the
pods.

## Goal

A change to `spec.dynamicConfig` automatically triggers a rolling restart of the
affected Temporal service pods, with no manual intervention — consistent with
how static-config and cert changes already behave.

## Approach

Mirror the existing `cert-hash` pattern with a **separate** dynamic-config hash
annotation (Approach A, chosen over folding it into the single `config-hash` so
`kubectl describe pod` clearly shows which input caused a roll).

1. **New annotation constant** in `internal/resources/labels.go`:

   ```go
   // DynamicConfigHashAnnotation stamps the rendered dynamic-config hash onto
   // pods so dynamic-config changes trigger a rolling restart.
   DynamicConfigHashAnnotation = "temporal.bmor10.com/dynamicconfig-hash"
   ```

2. **Reuse `resources.ConfigHash`** to hash the rendered dynamic config
   (sha256 → 16 hex chars). No new hashing helper.

3. **Thread the hash through the pure planning path** (keeps `PlanServices`
   pure and testable, matching the existing `ConfigHash` threading):

   - Add `DynamicConfigHash string` to `plan.ServicesInput`
     (`internal/plan/services.go`).
   - Populate it with `resources.ConfigHash(renderedDynamic)` in both call
     sites that already compute `ConfigHash`:
     - `internal/plan/plan.go` (preview path).
     - `internal/controller/temporalcluster_services.go`
       (`reconcileServices`).
   - `BuildDeployment(...)` gains a `dynamicConfigHash string` parameter and
     stamps `podAnnotations[DynamicConfigHashAnnotation] = dynamicConfigHash`
     alongside the existing config/cert hashes.

4. **Empty-value handling:** when `spec.dynamicConfig` is absent, the rendered
   dynamic-config string is empty. `ConfigHash("")` is a stable constant, so the
   annotation is always present and deterministic (no golden-test flakiness, no
   spurious rolls).

## Components touched

| File | Change |
|------|--------|
| `internal/resources/labels.go` | Add `DynamicConfigHashAnnotation` constant. |
| `internal/resources/deployment.go` | `BuildDeployment` takes `dynamicConfigHash`, stamps the annotation. |
| `internal/plan/services.go` | Add `DynamicConfigHash` to `ServicesInput`; pass to `BuildDeployment`. |
| `internal/plan/plan.go` | Compute and set `DynamicConfigHash`. |
| `internal/controller/temporalcluster_services.go` | Compute and set `DynamicConfigHash`. |

All `BuildDeployment` callers must be updated for the new parameter (compiler
enforces completeness).

## Data flow

```
spec.dynamicConfig
  → RenderDynamicConfig()        (already exists)
  → renderedDynamic (string)
      ├─ BuildDynamicConfigMap() → ConfigMap data          (already exists)
      └─ ConfigHash(renderedDynamic) → DynamicConfigHash    (NEW)
             → BuildDeployment → pod annotation
                 temporal.bmor10.com/dynamicconfig-hash     (NEW)
             → pod template mutates → rolling restart
```

## Error handling

No new failure modes. `ConfigHash` cannot fail; rendering errors are already
surfaced by the existing `renderConfig` / `RenderDynamicConfig` paths.

## Testing

- **Unit (`internal/resources`):** `BuildDeployment` stamps
  `DynamicConfigHashAnnotation`; two distinct dynamic configs yield different
  hashes; the same input is stable; empty dynamic config yields the stable
  empty-hash value.
- **Plan (`internal/plan`):** update `services_test.go` (and any golden output)
  for the new annotation; assert the hash on the planned Deployment matches
  `ConfigHash(RenderedDynamicConfig)`.
- **Envtest (`internal/controller`):** existing suites continue to pass;
  optionally assert the annotation appears on reconciled Deployments.

## Out of scope

- **Static config** already rolls pods correctly.
- **`TemporalDevServer`** is a separate all-in-one resource that does not use the
  `config-hash` pod-template pattern; no change.
- No CRD/API type change → no manifest, Helm chart, or API-doc regeneration.
