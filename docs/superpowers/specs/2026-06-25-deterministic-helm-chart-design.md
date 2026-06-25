# Deterministic Helm chart generation (#82)

**Status:** Approved design — ready for implementation planning.
**Issue:** [#82](https://github.com/bmorton/temporal-operator/issues/82) — Make
Helm chart generation reproducible and add a CI "verify generated chart" check.

## Problem

`dist/chart` (the Helm chart used by e2e and end users) is maintained by hand
because `make helm-chart` shells out to `kubebuilder edit
--plugins=helm/v2-alpha` (kubebuilder pinned at `v4.10.1` in
`hack/tools/go.mod`), which is in-place, non-idempotent, and destructive:

1. Clobbers required manager customizations — deletes the `env:` block
   (`MIGRATION_PROXY_IMAGE`, `OPERATOR_IMAGE`) and drops the
   `{{ .Values.manager.image.tag | default .Chart.AppVersion }}` fallback.
2. Reformats every CRD template (2-space → 4-space) — huge spurious diffs.
3. Resurrects `.github/workflows/test-chart.yml`, intentionally deleted in
   commit `8137aad` and kept untracked.
4. Rewrites `config/manager/kustomization.yaml` and `dist/install.yaml`.
5. Wants a nested values contract (`rbac.helpers`, `networkPolicy`) that is
   incompatible with the committed flat `values.yaml` (`rbacHelpers.enable`).

Because the chart is hand-edited, it silently drifts from the API/RBAC/CRD
source of truth and there is **no CI guard** (unlike docs, which are protected
by the `Verify generated docs` job in `.github/workflows/docs.yml`). This was
hit again in #69, where `MIGRATION_PROXY_IMAGE` had to be added by hand.

## Goal

Stop hand-editing `dist/chart`. Make chart generation reproducible and add a CI
verify check so a stale or hand-edited chart fails the build — while still
allowing a small set of deliberately hand-made templates where kubebuilder
cannot produce what we need.

## Approach

Keep the supported `kubebuilder edit` generator and wrap it in a deterministic
post-processing step that we own. The **committed chart is the post-processed
output**, and the post-processor is a pure function of kubebuilder's raw output
plus a set of canonical override files — so re-running always lands on the same
bytes.

The post-processor only handles the parts kubebuilder gets *wrong*. For the
parts it gets *right* (CRDs, RBAC roles, webhook configs, cert-manager, metrics,
`NOTES.txt`, `_helpers.tpl`), we accept its output as canonical (a one-time
reformat diff, then stable).

### Sources of truth

`dist/chart` becomes a pure build artifact. The inputs are:

- **`config/`** (kustomize) — the *generated* files: CRDs, RBAC roles, webhook
  configs, etc. kubebuilder owns these.
- **`hack/helm/overrides/`** (new) — canonical copies of the *hand-owned* files,
  mirroring their final `dist/chart/` paths. The generator copies these over
  kubebuilder's output verbatim (wholesale ownership).

### Hand-owned set (`hack/helm/overrides/`)

| Override path (mirrors `dist/chart/`)        | Why hand-owned                                                                 |
|----------------------------------------------|--------------------------------------------------------------------------------|
| `templates/manager/manager.yaml`             | `env:` block (`MIGRATION_PROXY_IMAGE`/`OPERATOR_IMAGE`), `.Chart.AppVersion` image fallback |
| `values.yaml`                                | flat contract (`rbacHelpers.enable`), secret/1Password values, defaults        |
| `Chart.yaml`                                 | appVersion / metadata managed by release-please                                |
| `README.md`                                  | curated chart docs                                                             |
| `artifacthub-pkg.yaml`                       | curated chart metadata                                                          |

Everything else stays kubebuilder-generated.

## Components

### `hack/helmgen` (new Go tool, main module, run via `go run`)

A small, testable Go program that orchestrates the whole flow. Invoked by the
`helm-chart` Make target as `go run ./hack/helmgen --kubebuilder=$(KUBEBUILDER)`.

Responsibilities, in order:

1. **Snapshot** the files kubebuilder rewrites-but-shouldn't —
   `config/manager/kustomization.yaml` and `dist/install.yaml` — to a temp
   location, so a developer's uncommitted local edits are never clobbered.
2. **Run** `kubebuilder edit --plugins=helm/v2-alpha` (path from `--kubebuilder`).
3. **Restore** the snapshotted files from step 1.
4. **Delete** `.github/workflows/test-chart.yml` if kubebuilder recreated it.
5. **Copy** each file from `hack/helm/overrides/` to its corresponding
   `dist/chart/` path, verbatim, overwriting kubebuilder's version.

Determinism guarantee: output is a pure function of `config/` + `overrides/`, so
`make helm-chart` on a clean tree produces no diff.

### `make helm-chart` (Makefile)

Becomes a one-liner delegating to the Go tool:

```make
helm-chart: kubebuilder
	go run ./hack/helmgen --kubebuilder=$(KUBEBUILDER)
```

### CI: `Verify generated chart` job

A new job in CI mirroring `Verify generated docs` in `.github/workflows/docs.yml`:

1. Checkout, setup Go, `make install-tools` (provides pinned kubebuilder).
2. `make helm-chart`.
3. Fail if stale:
   ```sh
   if ! git diff --quiet -- dist/chart config/manager/kustomization.yaml dist/install.yaml; then
     echo "::error::Generated Helm chart is out of date. Run 'make helm-chart' and commit the result."
     git --no-pager diff -- dist/chart config/manager/kustomization.yaml dist/install.yaml
     exit 1
   fi
   ```
4. Assert `.github/workflows/test-chart.yml` is absent; fail if present.
5. Run `helm lint dist/chart` and a `helm template` smoke render so the values
   contract stays valid.

`.gitignore` gains `.github/workflows/test-chart.yml` as belt-and-suspenders so
it can never be accidentally committed.

## Testing

- **Go unit tests** for `hack/helmgen` against a fixture tree (not the real
  kubebuilder binary):
  - snapshot/restore preserves `config/manager/kustomization.yaml` and
    `dist/install.yaml`;
  - override files are copied to the correct `dist/chart/` paths verbatim;
  - `test-chart.yml` removal;
  - **idempotency** — running the post-process twice over the same input yields
    identical output.
- The **real kubebuilder round-trip** (generate → no diff) is covered by the new
  CI verify job, not by unit tests.

## Docs / cleanup

- Update `AGENTS.md`, `.github/copilot-instructions.md`, and `CONTRIBUTING.md`:
  remove "edit the chart by hand" guidance; document the
  generate-and-verify flow and the `hack/helm/overrides/` directory.
- This supersedes prior project guidance/memories that say "don't run
  `make helm-chart` / edit the chart by hand."

## Risks & mitigations

| Risk | Mitigation |
|------|------------|
| A kubebuilder upgrade shifts its output | CI verify catches drift immediately; wholesale overrides are unaffected. |
| Hand-owned files miss an upstream kustomize change (e.g. a new manager arg) | Accepted trade-off for the few parameterized files; reviewer catches via the `helm template` smoke render. |
| One-time large reformat diff (CRDs 2→4 space) when kubebuilder first owns them | Expected; lands in the same PR. |
| `go run` against the kubebuilder binary path differs in CI vs local | Always pass `--kubebuilder=$(KUBEBUILDER)` from the Makefile, which already resolves the pinned binary. |

## Acceptance criteria (from #82)

- `make helm-chart` on a clean tree produces **no diff** and does **not** create
  `.github/workflows/test-chart.yml`.
- CI fails when `dist/chart` (or `config/manager/kustomization.yaml` /
  `dist/install.yaml`) is stale or hand-edited.
- `helm template` / `helm lint` still pass and the existing flat values contract
  is unchanged.

## Delivery

Single PR (the acceptance criteria are interdependent): the `hack/helmgen` tool +
`hack/helm/overrides/`, the `make helm-chart` rewrite, the CI verify job +
`.gitignore` entry, unit tests, and the docs cleanup.
