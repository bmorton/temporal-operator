# WASM Resource Preview — Design

**Date:** 2026-06-16
**Status:** Approved (design); ready for implementation planning
**Quality bar:** Tool may be **alpha**. The operator **must remain high quality** —
any operator-side change must be behavior-preserving and fully covered by tests.

## Problem

We want a page on the project's GitHub Pages site where a user can paste a custom
resource (a `TemporalCluster`, with the other CRD kinds addable later) and see
**all the Kubernetes objects the operator would create** for it, on a simple
tabbed page.

The hard constraint: this must be **highly maintainable** by **reusing the
operator's own code** so the preview can never drift from reality, **without
making the operator worse** and **without creating ongoing manual syncing work**.

## Why this is feasible (exploration findings)

- The operator already separates *building* objects from *applying* them.
  `internal/resources` exposes pure `Build*` functions (`BuildDeployment`,
  `BuildFrontendService`, `BuildConfigSecret`, `BuildSchemaJob`,
  `BuildUIDeployment`, certificates, PDBs, `BuildServiceMonitor`, ...) that take a
  typed `*TemporalCluster` and return Kubernetes objects with **no client, no
  network, no IO**.
- Config rendering (`temporal.RenderClusterConfig`) is a pure `go:embed`
  template.
- The admission webhooks (`internal/webhook/v1alpha1`) expose pure
  defaulting/validation logic over typed objects.
- These packages **already compile to WebAssembly**: building
  `./internal/resources`, `./internal/temporal`, and `./api/...` with
  `GOOS=js GOARCH=wasm` succeeds with zero changes. A minimal binary calling
  `BuildDeployment` links and runs.
- Binary size with standard Go WASM: ~55 MB raw, **~9.7 MB gzipped**. Acceptable
  for an opt-in, lazy-loaded tool. TinyGo is **not** viable (k8s reflection), so
  it is explicitly out of scope.
- The site is **Hugo → GitHub Pages** (`docs/`, `.github/workflows/docs.yml`); a
  WASM page drops in naturally.

The only thing not yet reusable is the **decision logic** for *which* objects get
created (conditionals on mTLS / UI / internal-frontend / enabled services). Today
that logic is interleaved with `r.apply()` calls across the controller's reconcile
files. Centralizing it into a pure planner is the change that delivers
"no manual syncing" — and it makes the operator more testable.

## Architecture

```
api/ + internal/resources + internal/temporal + internal/webhook   (existing, pure)
                      │  reused verbatim — zero duplication
        ┌─────────────┴──────────────┐
   operator controller          internal/plan  (new, pure planner)
   (applies objects,                  │
    keeps phase gating)        cmd/preview-wasm  (new, js/wasm shim)
                                      │
                              docs/ Hugo page + JS  (paste CR → tabs)
```

**Single source of truth:** the planner is consumed by *both* the operator
controller and the WASM tool. The web page links the operator's own packages, so
any operator change is reflected automatically on the next docs deploy.

## Components

### 1. `internal/plan` — pure planner (operator-side refactor)

New package with one pure function per concern, each returning the desired
objects with **no IO**:

- `PlanMTLS(cluster)` — internode + frontend certificates.
- `PlanServices(cluster, opts)` — config Secret, dynamic-config ConfigMap, and per
  enabled service: Deployment, headless Service, PDB, and the frontend Service.
- `PlanUI(cluster)` — UI client cert, Deployment, Service, optional Ingress.
- `PlanMonitoring(cluster)` — ServiceMonitor.
- `PlanSchemaJobs(cluster, ...)` — schema setup Job(s).

A top-level composer returns the full set, each item tagged with metadata for the
UI:

```go
type PlannedObject struct {
    Object client.Object
    Phase  Phase   // PersistenceSchema | CoreServices | MTLS | UI | Monitoring
}

func PlanAll(cluster *v1alpha1.TemporalCluster, opts Options) ([]PlannedObject, error)
```

`Options` carries the inputs that are IO-derived in the operator but stubbed in
the preview (datastore passwords/commands, public client host:port). In the
preview these are deterministic placeholders.

**Safety / behavior preservation:** the controller's reconcile files keep their
existing **phase gating and ordering** (e.g. services only after `SchemaReady`,
certificates before deployments). They call the corresponding `Plan*` function
instead of the current inline `Build*` calls and loop over its output. **No phase
logic moves into the planner.** Behavior is identical, guaranteed by the existing
controller/envtest suite plus new pure planner tests.

### 2. `cmd/preview-wasm` — js/wasm shim

Builds with `GOOS=js GOARCH=wasm`. Registers a single JS-callable function:

```
temporalPreview(kind: string, yaml: string)
  -> { objects: [{kind, name, phase, yaml}], errors: [string] }
```

Flow:

1. Parse YAML → typed CR (via `sigs.k8s.io/yaml`, already a dependency).
2. Run the operator's **webhook defaulter** on the CR (applies real defaults).
3. Run the operator's **webhook validator**; validation errors are returned in
   `errors` and rendering of objects is skipped on hard failures.
4. Call `PlanAll` with placeholder `Options`.
5. Marshal each object to YAML. **Decode `Secret` `data` entries to readable
   text** (the rendered Temporal config lives in a Secret as `[]byte`), so the
   preview shows real config instead of base64.

A small registry maps `kind → { default, validate, plan }`. TemporalCluster is
the only fully wired entry at launch; the other three kinds are registered as
stubs so adding them later is a few lines (satisfies the "design for extension"
requirement).

### 3. Web page (Hugo, `docs/`)

A new content page:

- **Left:** textarea to paste a CR, a "load example" dropdown sourced from the
  repo's `examples/` clusters, and a kind selector.
- **Right:** **tabs by Kind** — Deployments | Services | Secrets/ConfigMaps |
  Certificates | PDBs | Job | ServiceMonitor | Ingress. Each object is expandable
  to syntax-highlighted YAML, with a copy button and a small **phase badge**
  (teaching value: why each object exists).
- Validation errors render in a banner above the tabs.
- All client-side: loads `wasm_exec.js` and **lazy-loads** the `.wasm` on first
  use.

### 4. Build & maintenance — the "no manual syncing" guarantee

- The `.wasm` is **built in CI from the same source**: add a Go setup +
  `GOOS=js GOARCH=wasm go build` step to `.github/workflows/docs.yml` before the
  Hugo build, emitting the artifact and Go's `wasm_exec.js` into `docs/static/`.
- The `.wasm` is **never committed** (large, generated) — add it to
  `.gitignore`.
- Because the page links the operator's own `resources` / `temporal` / `webhook`
  / `plan` packages, drift is impossible: every operator change flows to the next
  docs deploy automatically. A CI compile guard builds the wasm on PRs that touch
  those packages so any incompatibility fails fast.

## Data flow

```
paste CR YAML ──► temporalPreview(kind, yaml) [wasm]
                    │ sigs.k8s.io/yaml decode → typed CR
                    │ webhook Default(cluster)
                    │ webhook Validate(cluster) ─► errors? ─► banner
                    │ plan.PlanAll(cluster, placeholderOpts)
                    │ marshal objects to YAML, decode Secret data
                    ▼
              JSON {objects, errors} ──► JS renders tabs by Kind
```

## Error handling

- **YAML parse errors / wrong kind:** returned in `errors`, shown in the banner; no
  tabs rendered.
- **Validation errors:** surfaced inline; the tool doubles as an in-browser CR
  linter.
- **Planner errors** (e.g. config render failure): returned in `errors`.
- The WASM panics are guarded; the shim recovers and reports a generic error
  rather than crashing the page.

## Testing

- `internal/plan`: pure table tests producing golden `PlanAll` object sets for
  each `examples/` cluster (the primary correctness net for the preview).
- `cmd/preview-wasm`: a CI compile guard that builds the command for `js/wasm`.
- Existing controller / envtest suite: **unchanged**, proving the refactor is
  behavior-preserving.
- `make test` and `make lint` must pass; `make generate manifests` only if API
  types change (they should not).

## Scope (alpha)

**In scope**

- Full `TemporalCluster` preview: defaulting + validation + planned objects.
- Tabs by Kind with phase badges, copy buttons, example presets.
- Extensible `kind` registry (other CRDs stubbed, addable with no rework).
- CI-built, never-committed wasm with a drift compile guard.

**Out of scope (YAGNI for alpha)**

- Content previews for `TemporalNamespace`, `TemporalClusterClient`,
  `TemporalSearchAttribute` (registry stubs only — they mostly drive Temporal API
  calls, not k8s objects).
- Object diffing, live-cluster connection, editing, applying.
- Persistence reachability / DB pinging (preview uses placeholder credentials).
- TinyGo / aggressive size optimization.

## Open risks & mitigations

- **Binary size (~9.7 MB gzip):** acceptable for an opt-in tool; mitigated by
  lazy-loading and Pages' gzip. Revisit only if it becomes a real complaint.
- **Refactor risk:** contained by keeping all phase gating in the controller and
  leaning on the existing envtest suite as the safety net; planner is pure and
  independently tested.
- **Webhook signature coupling:** the shim calls the same defaulter/validator the
  operator registers; the CI compile guard catches any signature drift.
