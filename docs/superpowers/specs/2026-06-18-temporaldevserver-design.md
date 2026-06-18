# TemporalDevServer CRD — Design

**Date:** 2026-06-18
**Status:** Approved (pending implementation plan)

## Summary

Add a new `TemporalDevServer` CRD that runs a single-pod, disposable Temporal
**dev server** (`temporal server start-dev`) backed by SQLite. It is explicitly
**not for production** — it targets local development and testing scenarios
where standing up a full `TemporalCluster` (Postgres/visibility/schema jobs/
mTLS) is overkill.

The dev server integrates with the operator's existing ecosystem: the
`clusterRef`-based CRDs (`TemporalNamespace`, `TemporalSchedule`,
`TemporalSearchAttribute`, `TemporalClusterClient`) can target a
`TemporalDevServer` via a new, backward-compatible `kind` discriminator on the
reference. A shared `clusterRef` resolver is extracted so all four controllers
gain DevServer support at once.

## Goals

- A throwaway, end-user-facing Temporal instance runnable inside a cluster for
  app development and CI.
- Single binary, SQLite, bundled Web UI, auto-created `default` namespace.
- Reuse the existing data-plane CRDs against the dev server (namespaces,
  schedules, search attributes) via polymorphic `clusterRef`.
- Stay deliberately simpler than `TemporalCluster` — no schema jobs, no mTLS,
  no upgrade machinery.

## Non-Goals

- Production use, HA, or multi-replica scaling (SQLite forbids it).
- mTLS / client-auth on the dev server.
- A validating/defaulting webhook for v1 (no immutable or dangerous fields).

## Architecture

### Container

- Image: `temporalio/temporal:<Version>` (the Temporal CLI image; default tag
  `latest`, overridable via `image`).
- Command: `temporal server start-dev --ip 0.0.0.0` plus `--namespace <name>`
  for each inline namespace, and UI flags when the UI is disabled.
- Ports: gRPC frontend `7233`, Web UI `8233`.
- Single replica (SQLite). SQLite file on an `emptyDir` by default, optional
  PVC.

### Cross-cutting: shared `clusterRef` resolver

Today `TemporalNamespace`, `TemporalSchedule`, `TemporalSearchAttribute`, and
`TemporalClusterClient` each inline the same logic: `Get(TemporalCluster)` →
derive `frontendAddress` + `clusterTLSConfig` + check the `Ready` condition.

Extract a single resolver (e.g. `internal/controller` helper or a small
`internal/temporal`/`internal/resources` seam):

```go
// ResolveTarget resolves a ClusterReference to a connectable Temporal frontend.
func ResolveTarget(ctx context.Context, c client.Client, namespace string, ref ClusterReference) (
    frontendAddr string,
    tlsConfig *tls.Config,
    ready bool,
    reason string, // condition reason when not ready / not found
    err error,
)
```

It switches on `ref.Kind`:

- `TemporalCluster` (default): existing behavior — `FrontendServiceName`,
  `clusterTLSConfig`, `Status.Conditions[Ready]`.
- `TemporalDevServer`: `DevServerFrontendServiceName` (plaintext, `tls = nil`),
  `Status.Conditions[Ready]`.

All four controllers replace their inlined resolution with a call to
`ResolveTarget`, shrinking per-controller code and centralizing the kind switch.

### `ClusterReference` type (backward-compatible)

In `api/v1alpha1/shared_types.go`:

```go
// ClusterReference points at a Temporal frontend, either a TemporalCluster
// (default) or a TemporalDevServer, in the same Kubernetes namespace.
type ClusterReference struct {
    Name string `json:"name"`
    // +kubebuilder:validation:Enum=TemporalCluster;TemporalDevServer
    // +kubebuilder:default=TemporalCluster
    // +optional
    Kind string `json:"kind,omitempty"`
}
```

`ClusterRef` on `TemporalNamespaceSpec`, `TemporalScheduleSpec`,
`TemporalSearchAttributeSpec`, and `TemporalClusterClientSpec` changes from
`corev1.LocalObjectReference` to `ClusterReference`. Because `name` is retained
and `kind` defaults to `TemporalCluster`, **all existing manifests remain valid
unchanged** — an additive schema change.

## CRD: `TemporalDevServer`

Group `temporal.bmor10.com/v1alpha1`, scope `Namespaced`, shortName `tds`,
storage version, status subresource.

### Spec

```go
type TemporalDevServerSpec struct {
    // Version is the temporalio/temporal CLI image tag. Default "latest".
    // +optional
    Version string `json:"version,omitempty"`

    // Image overrides the full image. Default temporalio/temporal:<Version>.
    // +optional
    Image string `json:"image,omitempty"`

    // +optional
    ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

    // Namespaces are extra namespaces created at startup (besides "default").
    // Created once at boot with no drift management; use TemporalNamespace CRs
    // for richer, managed namespaces.
    // +optional
    Namespaces []string `json:"namespaces,omitempty"`

    // UI controls the bundled Web UI (port 8233).
    // +optional
    UI *DevServerUISpec `json:"ui,omitempty"`

    // Storage selects ephemeral (default) or a PVC for the SQLite file.
    // +optional
    Storage *DevServerStorageSpec `json:"storage,omitempty"`

    // Service configures how the frontend/UI are exposed (ClusterIP default).
    // +optional
    Service *ServiceExposureSpec `json:"service,omitempty"`

    // Resources sets the dev server container resource requirements.
    // +optional
    Resources corev1.ResourceRequirements `json:"resources,omitempty"`

    // Standard pod placement knobs (reusing existing patterns).
    // +optional
    NodeSelector map[string]string `json:"nodeSelector,omitempty"`
    // +optional
    Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
    // +optional
    Affinity *corev1.Affinity `json:"affinity,omitempty"`
}

type DevServerUISpec struct {
    // +kubebuilder:default=true
    Enabled bool `json:"enabled"`
}

type DevServerStorageSpec struct {
    // +kubebuilder:validation:Enum=Ephemeral;Persistent
    // +kubebuilder:default=Ephemeral
    Type string `json:"type,omitempty"`
    // Size for the PVC when Type=Persistent. Default "1Gi".
    // +optional
    Size *resource.Quantity `json:"size,omitempty"`
    // +optional
    StorageClassName *string `json:"storageClassName,omitempty"`
}
```

### Status

```go
type TemporalDevServerStatus struct {
    // Phase is a coarse human-friendly summary.
    // +optional
    Phase string `json:"phase,omitempty"`
    // +optional
    ObservedGeneration int64 `json:"observedGeneration,omitempty"`
    // +listType=map
    // +listMapKey=type
    // +optional
    Conditions []metav1.Condition `json:"conditions,omitempty"` // Ready, Available
    // +optional
    Endpoints DevServerEndpoints `json:"endpoints,omitempty"` // frontend, ui
    // +optional
    Version string `json:"version,omitempty"`
}

type DevServerEndpoints struct {
    // +optional
    Frontend string `json:"frontend,omitempty"` // host:7233
    // +optional
    UI string `json:"ui,omitempty"` // host:8233
}
```

Print columns: Version, Ready, UI, Age. `Ready=True` once the Deployment's
single replica is available — this is what `ResolveTarget` checks for dependent
CRs.

## Namespaces: inline vs. CRDs

Both paths are supported by design:

- **Inline `namespaces` list** — quick startup convenience mapped to
  `--namespace <name>`. Created once at boot, no drift management.
- **`TemporalNamespace` / `TemporalSearchAttribute` / `TemporalSchedule` CRs**
  with `clusterRef.kind: TemporalDevServer` — full managed lifecycle (retention,
  drift, search attributes, schedules), identical to targeting a real cluster.

`start-dev` always auto-creates the `default` namespace, a nice ergonomic
difference from `TemporalCluster` (which runs plain `temporal-server start` and
creates no namespaces).

## Controller

`internal/controller/temporaldevserver_controller.go` — a small reconcile loop,
**no finalizer**. Deleting the CR garbage-collects the owned Deployment,
Service, and (ephemeral) PVC via owner references; the SQLite state dies with
the pod, which is the whole point of "disposable".

Reconcile steps:
1. Apply the Deployment (`server start-dev --ip 0.0.0.0` + per-namespace flags;
   UI disabled flags when `ui.enabled=false`).
2. Apply the frontend/UI Service (`ServiceExposureSpec`, ClusterIP default).
3. Apply a PVC when `storage.type=Persistent`.
4. Update status, conditions, and endpoints; requeue until the replica is ready.

Reuse the existing `apply.go` helper and owner-reference conventions.

## Resources (pure builders)

`internal/resources/devserver.go`, following the established pure-builder pattern
(controllers do all Kubernetes IO; builders are pure and WASM-buildable):

- `BuildDevServerDeployment`
- `BuildDevServerService`
- `BuildDevServerPVC`
- `DevServerFrontendServiceName(name)` — used by `ResolveTarget` to compute the
  frontend address.

## Wiring & RBAC

- Register `TemporalDevServerReconciler` in `cmd/main.go` alongside the others.
- Add `+kubebuilder:rbac` markers for `temporaldevservers` (+ `/status`,
  `/finalizers`). Deployment/Service/PVC permissions already exist for the
  cluster controller.
- Run `make generate manifests` to regenerate deepcopy + CRD manifests.

## Webhook

None for v1 (YAGNI). The dev server has no immutable or dangerous fields (no
shard count, no schema). A defaulting/validating webhook can be added later if
needed.

## Testing

Following repo conventions (`make test` via envtest; table-driven builder tests):

- **`internal/resources` builder unit tests** — pure and fast; assert the
  Deployment command/args, ports, Service, PVC, names.
- **Resolver unit tests** — both kinds, default-kind backward compatibility,
  not-found and not-ready paths.
- **Controller envtest** — create the CR, assert Deployment/Service exist and
  status transitions to Ready.
- **One e2e/chainsaw scenario** — a `TemporalDevServer` plus a
  `TemporalNamespace{clusterRef.kind: TemporalDevServer}` registering against it.

## Backward Compatibility

- `clusterRef` gains an optional `kind` that defaults to `TemporalCluster`;
  existing manifests are unaffected.
- No changes to `TemporalCluster` behavior.

## Conventions to honor

- Conventional Commits + DCO sign-off (`git commit -s`).
- `make generate manifests` after API changes; `make build`; `make test`;
  `make lint` before PR.
- Do not hand-edit version numbers / `CHANGELOG.md` (release-please).
