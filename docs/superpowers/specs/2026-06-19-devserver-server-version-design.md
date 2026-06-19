# TemporalDevServer: accept a Temporal server version

**Date:** 2026-06-19

## Problem

`TemporalDevServer.spec.version` currently means the **`temporalio/temporal` CLI
image tag** (`DevServerImage` → `temporalio/temporal:<version>`). The CLI image
is versioned on its own cadence (latest is `1.7.x`), independent of the Temporal
**server** version. Users naturally expect to ask for a server version — e.g.
`1.31.1`, the same value `TemporalCluster.spec.version` takes — and get an
`ImagePullBackOff` instead:

```
failed to resolve reference "docker.io/temporalio/temporal:1.31.1": not found
```

(`1.31.1` is a server version; the CLI image has no such tag.)

We want `TemporalDevServer.spec.version` to accept a **Temporal server version**
and have the operator map it to the correct `temporalio/temporal` CLI image,
using the existing version matrix as the single source of truth.

## Feasibility (confirmed)

Each `temporalio/cli` release (the `temporalio/temporal` image) bundles exactly
one Temporal server version, discoverable from that tag's `go.mod`
(`go.temporal.io/server`). The mapping for every currently-supported server line:

| Server line | `temporalio/temporal` CLI tag | bundled server |
| --- | --- | --- |
| 1.31 | 1.7.2 | 1.31.1 |
| 1.30 | 1.6.2 | 1.30.2 |
| 1.29 | 1.5.1 | 1.29.1 |
| 1.28 | 1.4.1 | 1.28.0 |
| 1.27 | 1.3.0 | 1.27.1 |

It is a **lookup table keyed by server minor line**, not a formula. The CLI
bundles a line's specific patch, which may differ from the matrix's
`patchVersions` (e.g. matrix `1.30.4` vs the CLI's bundled `1.30.2`). For a
disposable dev server this approximation is acceptable: requesting any patch of a
line yields the CLI image for that line.

## Approach (decisions from brainstorming)

- **Repurpose `spec.version`** to mean the Temporal server version (option A),
  matching `TemporalCluster.spec.version`. This is a breaking change to the
  field's meaning, acceptable because `TemporalDevServer` is new (shipped in
  0.6.0) and pre-1.0.
- **`version` stays optional**; when omitted the controller resolves to the
  **highest supported server version** in the matrix (computed at runtime — no
  brittle static default). The existing `+kubebuilder:default="latest"` is
  removed (`latest` is not a valid server version).
- **`spec.image`** remains the full-image escape hatch (unchanged). A user who
  wants a specific raw CLI image sets `image: temporalio/temporal:1.7.2`.
- **Validation is controller-side** (no webhook): `TemporalDevServer` has no
  webhook today, and a dev server is low-stakes, so an unsupported/unmappable
  version is surfaced as a failed status condition rather than rejected at
  admission.

## Component 1: Mapping data + generated matrix

- Add a per-entry field `devServerCLIVersion` to `hack/version-matrix.yaml`,
  holding the `temporalio/temporal` CLI tag for that server line:
  - `1.31` → `"1.7.2"`, `1.30` → `"1.6.2"`, `1.29` → `"1.5.1"`,
    `1.28` → `"1.4.1"`, `1.27` → `"1.3.0"`.
- Extend `hack/gen-version-matrix.go`: add `DevServerCLIVersion string` to the
  `versionInfo` struct and the `VersionInfo` output template, then regenerate
  `internal/temporal/versions_gen.go` via `make gen-version-matrix`.
- Add `DevServerCLIVersion string` to the `VersionInfo` struct definition in
  `internal/temporal/versions.go` (the hand-written struct the generated slice
  populates).

## Component 2: Resolvers (`internal/temporal/versions.go`)

- `DevServerCLIVersion(serverVersion string) string` — mirrors
  `DefaultUIVersion`: `minorOf(serverVersion)` → matching entry's
  `DevServerCLIVersion`; returns `""` if the version is unknown or the line has
  no CLI mapping.
- `LatestSupportedVersion() string` — returns the highest server version in the
  matrix (used for the omitted-`version` case). `SupportedVersions()` already
  returns the patch versions sorted ascending by semver, so this is its last
  element (guard against an empty slice).

## Component 3: Image resolution (`internal/resources/devserver.go`)

Rewrite `DevServerImage(dev)` to be matrix-aware. Because resolution can now
fail (unsupported version), change the signature to return an error:

```
func DevServerImage(dev *TemporalDevServer) (string, error)
```

Logic:
1. `spec.image != ""` → return it verbatim, nil.
2. `v := spec.version`; if `v == ""` → `v = temporal.LatestSupportedVersion()`.
3. `cli := temporal.DevServerCLIVersion(v)`; if `cli == ""` → return an error
   identifying the unsupported version (include `temporal.SupportedVersions()`).
4. return `"temporalio/temporal:" + cli`, nil.

Update all callers (`devserver.go` Deployment builder, the controller) for the
new signature.

## Component 4: Controller validation + status (`internal/controller/temporaldevserver_controller.go`)

- Before building resources, resolve the image. On error, set
  `Status.Phase = "Failed"`, `Ready=False` with reason
  `ReasonVersionUnsupported` (reuse the existing constant used by the cluster
  webhook) and the error message, persist status, and return without creating
  the Deployment/Service/PVC.
- On success, set `Status.Version` to the **resolved server version** (the value
  used for the lookup — `spec.version` or the latest-supported fallback), not the
  image string. This matches `TemporalCluster.status.version` and the
  `Version` print column. (Today the controller sets
  `Status.Version = DevServerImage(...)`, i.e. the image string — that changes.)

## Component 5: Update merged examples, sample, e2e, and docs

PR #73 added dev server collateral using the old "CLI image tag" semantics; all
of it must be updated to the new server-version meaning:

- `examples/devserver/minimal.yaml`: `version: "1.7.2"` → `version: "1.31.1"`
  (drop the "CLI image tag" comment).
- `examples/devserver/persistent.yaml`: `version: "1.7.2"` → `version: "1.31.1"`.
- `examples/devserver/README.md`: rewrite the `## Version field` section to
  describe server-version semantics, the latest-supported default when omitted,
  and the `spec.image` escape hatch.
- `config/samples/temporal_v1alpha1_temporaldevserver.yaml`: `version: "latest"`
  → `version: "1.31.1"` (or omit to use the default).
- `test/e2e/devserver/01-devserver.yaml`: `version: "1.7.2"` → `version: "1.31.1"`.
- `test/e2e/devserver/01-assert.yaml` / `02-assert.yaml`: these assert on status
  (Ready/Phase), not the image, so they likely need no change — but if any
  assertion checks `status.version`, update it from the old image string to the
  resolved server version (`1.31.1`).

## Testing

- `internal/temporal/versions_test.go` (or equivalent): `DevServerCLIVersion`
  for each line, unknown version → `""`, and `LatestSupportedVersion()`.
- `internal/resources/devserver_test.go`: update `DevServerImage` expectations
  for the new signature — server version → CLI image (`1.31.1` →
  `temporalio/temporal:1.7.2`), empty version → latest-supported mapping,
  `spec.image` escape hatch, and unsupported version → error.
- `internal/controller/temporaldevserver_controller_test.go`: a supported
  version reconciles and reports `status.version` = the server version; an
  unsupported version sets `Phase=Failed` + `Ready=False`
  (`ReasonVersionUnsupported`) and creates no Deployment.

## Out of scope (future work)

- Extending the release-watch automation to also watch `temporalio/cli` and keep
  `devServerCLIVersion` current as new server lines land.
- Allowing the dev server to run an exact server patch (the CLI only ever bundles
  one patch per line).

## Acceptance criteria

- `make generate manifests`, `make build`, `make lint`, and `make test` pass.
- A `TemporalDevServer` with `spec.version: "1.31.1"` runs
  `temporalio/temporal:1.7.2`; with `version` omitted it runs the
  latest-supported line's CLI image; with `spec.image` set it uses that image.
- An unsupported `spec.version` yields `Phase=Failed` /
  `Ready=False (ReasonVersionUnsupported)` and no Deployment.
- All merged dev server examples, the sample, the e2e test, and the README
  reflect server-version semantics.
