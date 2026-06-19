# Preview Helm Chart Publishing — Design

Status: Approved (brainstorm)
Date: 2026-06-19
Target: CI/tooling enhancement (no operator code change)

## Goal

Let a maintainer test an entire branch — the operator **image** *and* the Helm
**chart** (including any new CRDs/RBAC/templates the branch adds) — on a real
cluster via **ArgoCD**, before merging. Today the `Preview Image` workflow
publishes a one-off image plus a raw `install.yaml`, which suits `kubectl apply`
but not an ArgoCD/Helm GitOps flow, and it never produces a chart containing the
branch's new templates. This design closes that gap by publishing a matching
**preview chart** alongside the preview image from a single manual dispatch.

The intended loop: dispatch the workflow on the branch → ArgoCD points at the
published preview chart version → test → after merge, flip the ArgoCD
`targetRevision` back to the mainline/release version.

## Decisions (from brainstorming)

1. **Combined workflow.** Extend the existing `.github/workflows/preview-image.yml`
   so one `workflow_dispatch` builds the image **and** packages + pushes a
   matching preview chart. Image and chart stay in sync (chart pushed only after
   the image push succeeds), and there is one thing to trigger.
2. **Prerelease SemVer, not build metadata.** The preview version is
   `<base>-pr.<short-sha>` (e.g. `0.7.0-pr.a1b2c3d`). Rationale:
   - Distinct SemVer precedence (sorts **below** the eventual `<base>` release
     and above the prior release) — correct ordering, no collisions.
   - `-` is a legal OCI tag character, so the OCI chart tag is exactly the
     version (no Helm `+`→`_` rewrite) — clean ArgoCD `targetRevision`.
   - Build metadata (`0.7.0+pr-…`) was rejected: SemVer ignores it for
     precedence (ambiguous/colliding with a real `0.7.0`) and OCI/ArgoCD handle
     `+` poorly.
3. **Unified identifier for image, chart version, and appVersion.** A single
   string `V = <base>-pr.<short-sha>` is used for **all three**:
   - Image tag: `ghcr.io/bmorton/temporal-operator:V`.
   - `helm package --version V --app-version V`.
   - Because `manager.yaml` renders `image.tag | default .Chart.AppVersion` and
     `manager.image.tag` defaults to empty, appVersion = V makes the chart pull
     exactly `…:V` with **no values override**.
   `V` is valid for both Docker tags and OCI Helm tags. This replaces the old
   `pr-<sha>` image-tag default.
4. **Same OCI repo as releases.** Push to `oci://ghcr.io/bmorton/charts`
   (chart `temporal-operator`), as prerelease versions of the normal chart. In
   ArgoCD the `repoURL` + chart stay constant; only `targetRevision` changes
   between the preview `V` and the mainline version — one knob to flip back.
   ArgoCD does not auto-select prereleases unless explicitly targeted, so
   released installs are unaffected.
5. **Cleanup is documented, not automated** (alpha). An automated GHCR
   retention/prune workflow is future work.

## Workflow changes (`.github/workflows/preview-image.yml`)

### Inputs (`workflow_dispatch`)

- `baseVersion` *(optional)* — base SemVer such as `0.7.0`. **Default: auto** =
  the latest release tag with its minor bumped and patch zeroed
  (e.g. `v0.6.0` → `0.7.0`). Matches the repo's pre-1.0 rule that `feat:` → minor
  bump, so a preview of unreleased work is a prerelease of the next minor.
- `version` *(optional)* — full override of the entire preview version; must be
  valid SemVer. Wins over `baseVersion` when set. Escape hatch for patch-only or
  custom previews.
- `platforms` *(existing, unchanged)* — default `linux/amd64,linux/arm64`.

The previous `tag` input (default `pr-<sha>`, arbitrary string) is removed in
favor of `version`/`baseVersion`.

### Version resolution + guard

```
if version provided:        V = version
elif baseVersion provided:  V = "${baseVersion}-pr.${SHA::7}"
else:                       base = <latest release tag, minor-bumped, patch 0>
                            V = "${base}-pr.${SHA::7}"
```
Then validate `V` against a SemVer pattern and fail fast with a clear message if
it is not valid (so chart packaging cannot fail late). Latest release tag is
resolved from the repo's tags (e.g. highest `v[0-9]+.[0-9]+.[0-9]+`).

### Image step

Unchanged build/push, except the resolved tag is `V` (not `pr-<sha>`):
`ghcr.io/bmorton/temporal-operator:V`. The existing `install.yaml` artifact step
remains and pins to `V`.

### New chart step (after the image push)

Mirrors `release.yml`'s `publish-chart` job:
```sh
helm package dist/chart --version "$V" --app-version "$V"
echo "$GITHUB_TOKEN" | helm registry login ghcr.io -u "$REGISTRY_USER" --password-stdin
helm push "temporal-operator-$V.tgz" oci://ghcr.io/bmorton/charts
```
- Uses the pinned `azure/setup-helm` action (SHA + `# vX` comment).
- Runs in the same job, **after** the image push, so the chart never references
  a missing image.
- The chart is packaged from the **branch's** `dist/chart`, so new CRDs/RBAC/
  templates on the branch are included.

### Run summary (`$GITHUB_STEP_SUMMARY`)

Extend the existing summary to print:
- The image ref and the chart ref (`oci://ghcr.io/bmorton/charts/temporal-operator` @ `V`).
- A copy-paste ArgoCD `Application` source snippet:
  ```yaml
  source:
    repoURL: ghcr.io/bmorton/charts
    chart: temporal-operator
    targetRevision: <V>   # flip back to your mainline version after merge
    helm:
      releaseName: temporal-operator
  ```
- The existing note: cert-manager is required for the webhook; set
  `ENABLE_WEBHOOKS=false` on the manager Deployment to skip it.

## Security / conventions

- Job permissions scoped to `contents: read, packages: write`.
- `persist-credentials: false` on checkout; `helm registry login` via
  `GITHUB_TOKEN`.
- `cache: false` on the publishing job (avoid restoring a poisoned build cache
  into a job that pushes artifacts — matches `release.yml`).
- All actions pinned to a full commit SHA with a trailing `# vX` comment
  (repo convention).

## Docs

Add `docs/preview-environments.md` (or a section in the contributing/preview
docs) describing the loop: dispatch the workflow → copy `V` from the run summary
→ set ArgoCD `targetRevision: V` → test → after merge, set `targetRevision` back
to the mainline/release version. Include the ArgoCD `Application` snippet and the
cert-manager / `ENABLE_WEBHOOKS` notes. Add a one-line manual-cleanup note for
preview package versions in GHCR.

## Out of scope (future)

- Automated GHCR retention/pruning of old preview images and chart versions.
- Auto-dispatch on PR open/update (kept manual, matching the current workflow).
- Any change to the operator code or the release chart's publishing flow.

## Testing / verification

CI workflows are not unit-testable here; validation is:
- `helm lint dist/chart` — chart is well-formed.
- `helm package dist/chart --version 0.7.0-pr.test --app-version 0.7.0-pr.test`
  (local dry run) — proves a prerelease version packages cleanly.
- `helm template dist/chart` — renders, including the new CRD/RBAC entries.
- A workflow-syntax check (e.g. `actionlint`/`zizmor` already in the repo) on the
  edited workflow.

## Conventions

- Module `github.com/bmorton/temporal-operator`; CRD group `temporal.bmor10.com`;
  copyright "Brian Morton".
- Conventional Commits, signed off (`git commit -s`), with the Copilot
  co-author trailer.
