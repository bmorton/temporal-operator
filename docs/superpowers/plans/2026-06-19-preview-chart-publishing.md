# Preview Helm Chart Publishing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the `Preview Image` workflow so one manual dispatch publishes both a preview operator image and a matching preview Helm chart (pinned to that image) that ArgoCD can pull by a prerelease version.

**Architecture:** A single workflow computes one version string `V = <base>-pr.<short-sha>` (prerelease SemVer), tags the image with it, and `helm package --version V --app-version V` + `helm push` to `oci://ghcr.io/bmorton/charts`. Because the chart's `manager.image.tag` defaults to `.Chart.AppVersion`, the chart pulls exactly the preview image with no values override. Same OCI repo as releases, so ArgoCD only changes `targetRevision`.

**Tech Stack:** GitHub Actions (`workflow_dispatch`), Helm 3 OCI, GHCR, `gh` CLI.

**Spec:** `docs/superpowers/specs/2026-06-19-preview-chart-publishing-design.md`

## Conventions
- Commits: Conventional Commits, signed off (`git commit -s`), with trailer `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>`.
- GitHub Actions MUST be pinned to a full 40-char commit SHA with a trailing `# vX` comment (repo convention).
- All work happens in worktree `/workspaces/temporal-operator/.worktrees/migration-proxy` (branch `feat/migration-proxy`).
- This plan edits ONE workflow file plus docs. There is no Go code and no unit test; verification is `helm lint`/`helm package` dry-runs and a workflow-syntax lint.

## File structure

| File | Responsibility |
|---|---|
| `.github/workflows/preview-image.yml` (modify) | Inputs, version resolution + SemVer guard, image tag = `V`, new chart publish step, updated run summary + header comment |
| `docs/preview-environments.md` (create) | Maintainer guide: dispatch → ArgoCD `targetRevision: V` → test → flip back after merge |

---

## Task 1: Replace inputs and add version resolution + SemVer guard

**Files:**
- Modify: `.github/workflows/preview-image.yml`

- [ ] **Step 1: Update the header comment and inputs block**

Replace the header comment (lines 1-18, from `name:` through the `platforms` input) so the comment mentions the chart and the inputs become `baseVersion`/`version`/`platforms`:

```yaml
name: Preview Image

# Builds and pushes a one-off preview container image AND a matching preview
# Helm chart (pinned to that image) for a branch or PR, so reviewers can try the
# operator on a cluster (e.g. via ArgoCD) before merging. Triggered manually
# from the Actions tab against the branch you want to preview.
on:
  workflow_dispatch:
    inputs:
      baseVersion:
        description: "Base SemVer for the preview (default: latest release, minor bumped, e.g. 0.7.0)"
        required: false
        type: string
      version:
        description: "Full preview version override (must be valid SemVer; wins over baseVersion)"
        required: false
        type: string
      platforms:
        description: "Target platforms"
        required: false
        default: "linux/amd64,linux/arm64"
        type: string
```

- [ ] **Step 2: Replace the "Resolve image reference" step with version resolution**

Replace the existing `- name: Resolve image reference` step (currently lines ~34-44) with a step that computes `V` and exposes both the version and the full image ref as outputs:

```yaml
      - name: Resolve preview version
        id: ref
        env:
          INPUT_VERSION: ${{ inputs.version }}
          INPUT_BASE: ${{ inputs.baseVersion }}
          SHA: ${{ github.sha }}
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          REPO: ${{ github.repository }}
        run: |
          set -euo pipefail
          short="${SHA::7}"

          if [ -n "$INPUT_VERSION" ]; then
            version="$INPUT_VERSION"
          else
            base="$INPUT_BASE"
            if [ -z "$base" ]; then
              # Latest published (non-prerelease, non-draft) release tag; fall
              # back to 0.0.0 when the repo has no releases yet.
              latest="$(gh api "repos/${REPO}/releases/latest" -q .tag_name 2>/dev/null || echo "v0.0.0")"
              latest="${latest#v}"
              major="${latest%%.*}"
              rest="${latest#*.}"
              minor="${rest%%.*}"
              base="${major}.$((minor + 1)).0"
            fi
            version="${base}-pr.${short}"
          fi

          # Validate SemVer so chart packaging cannot fail late.
          semver='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$'
          if ! printf '%s' "$version" | grep -Eq "$semver"; then
            echo "::error::Resolved version '$version' is not valid SemVer" >&2
            exit 1
          fi

          echo "version=${version}" >> "$GITHUB_OUTPUT"
          echo "image=ghcr.io/bmorton/temporal-operator:${version}" >> "$GITHUB_OUTPUT"
          echo "Resolved preview version: ${version}"
```

- [ ] **Step 3: Verify the workflow is still valid YAML and lints**

Run (in the worktree):
```bash
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/preview-image.yml'))" && echo YAML_OK
command -v actionlint >/dev/null 2>&1 && actionlint .github/workflows/preview-image.yml || echo "actionlint not installed; skipping"
```
Expected: `YAML_OK`. If `actionlint` is installed, it reports no errors; if not, the skip message is acceptable.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/preview-image.yml
git commit -s -m "ci(preview): compute prerelease version for image and chart

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 2: Tag the image with `V` and publish the matching preview chart

**Files:**
- Modify: `.github/workflows/preview-image.yml`

Context: the existing steps after version resolution are, in order: `Log in to GHCR` (docker/login-action), `setup-qemu`, `setup-buildx`, `Build and push` (docker/build-push-action, `tags: ${{ steps.ref.outputs.image }}`), `setup-go`, `Generate installer pinned to the preview image` (`make build-installer IMG=...`), `Upload install.yaml`, `Summary`. The `Build and push` step already uses `steps.ref.outputs.image`, which now resolves to `…:V` — no change needed there.

- [ ] **Step 1: Add a Helm setup + chart publish step**

Insert a new step immediately AFTER the `- name: Upload install.yaml` step and BEFORE the final `- name: Summary` step:

```yaml
      - name: Set up Helm
        uses: azure/setup-helm@dda3372f752e03dde6b3237bc9431cdc2f7a02a2 # v5.0.0

      - name: Package and push preview chart to GHCR (OCI)
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          REGISTRY_USER: ${{ github.actor }}
          VERSION: ${{ steps.ref.outputs.version }}
        run: |
          set -euo pipefail
          helm package dist/chart --version "$VERSION" --app-version "$VERSION"
          echo "$GITHUB_TOKEN" | helm registry login ghcr.io -u "$REGISTRY_USER" --password-stdin
          helm push "temporal-operator-${VERSION}.tgz" oci://ghcr.io/bmorton/charts
```

- [ ] **Step 2: Update the final Summary step to include the chart + ArgoCD snippet**

Replace the existing `- name: Summary` step (the last step) with one that prints both artifacts and a copy-paste ArgoCD source:

```yaml
      - name: Summary
        env:
          IMAGE: ${{ steps.ref.outputs.image }}
          VERSION: ${{ steps.ref.outputs.version }}
        run: |
          {
            echo "### Preview artifacts published"
            echo
            echo "**Image**"
            echo '```'
            echo "$IMAGE"
            echo '```'
            echo
            echo "**Helm chart** (OCI)"
            echo '```'
            echo "oci://ghcr.io/bmorton/charts/temporal-operator:${VERSION}"
            echo '```'
            echo
            echo "**ArgoCD source** (set targetRevision to the preview version; flip back to your mainline version after merge):"
            echo '```yaml'
            echo "source:"
            echo "  repoURL: ghcr.io/bmorton/charts"
            echo "  chart: temporal-operator"
            echo "  targetRevision: ${VERSION}"
            echo "  helm:"
            echo "    releaseName: temporal-operator"
            echo '```'
            echo
            echo "Or with raw manifests: download the **install-yaml** artifact and \`kubectl apply -f install.yaml\`."
            echo
            echo "The chart/installer expect cert-manager (for the validating webhook). To skip the webhook, set ENABLE_WEBHOOKS=false on the manager Deployment."
          } >> "$GITHUB_STEP_SUMMARY"
```

- [ ] **Step 3: Verify YAML + chart packaging works with a prerelease version**

Run (in the worktree):
```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/preview-image.yml'))" && echo YAML_OK
helm lint dist/chart
helm package dist/chart --version 0.7.0-pr.testsha --app-version 0.7.0-pr.testsha --destination /tmp
ls /tmp/temporal-operator-0.7.0-pr.testsha.tgz
rm -f /tmp/temporal-operator-0.7.0-pr.testsha.tgz
```
Expected: `YAML_OK`; `helm lint` passes (info/warnings OK, no errors); the `.tgz` is produced (proving the prerelease version packages cleanly); then cleaned up.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/preview-image.yml
git commit -s -m "ci(preview): publish matching preview Helm chart to GHCR

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 3: Maintainer documentation

**Files:**
- Create: `docs/preview-environments.md`

- [ ] **Step 1: Write the guide**

Create `docs/preview-environments.md`:

```markdown
# Preview Environments

The **Preview Image** workflow (`.github/workflows/preview-image.yml`) lets you
try an unmerged branch — the operator image *and* the Helm chart (including any
new CRDs/RBAC/templates the branch adds) — on a real cluster before merging.

One manual dispatch publishes two coupled artifacts, both tagged with the same
prerelease version `V` (for example `0.7.0-pr.a1b2c3d`):

- Image: `ghcr.io/bmorton/temporal-operator:V`
- Helm chart: `oci://ghcr.io/bmorton/charts/temporal-operator` at version `V`

Because the chart's `manager.image.tag` defaults to the chart `appVersion` (also
`V`), the chart pulls the matching preview image with no extra configuration.

## Publishing a preview

1. In the GitHub **Actions** tab, open **Preview Image** and click **Run
   workflow**. Select the branch you want to preview.
2. Optional inputs:
   - `baseVersion` — base SemVer (default: the latest release with its minor
     bumped, e.g. `0.7.0`). The published version becomes `<baseVersion>-pr.<short-sha>`.
   - `version` — a full SemVer override (wins over `baseVersion`).
   - `platforms` — image target platforms.
3. When the run finishes, copy the version `V` and the ArgoCD snippet from the
   run **Summary**.

## Testing with ArgoCD

Point your Application at the preview chart version. Only `targetRevision`
changes from your normal install:

\`\`\`yaml
source:
  repoURL: ghcr.io/bmorton/charts
  chart: temporal-operator
  targetRevision: 0.7.0-pr.a1b2c3d   # the V from the run summary
  helm:
    releaseName: temporal-operator
\`\`\`

The chart expects cert-manager (for the validating webhook). To skip the
webhook, set `ENABLE_WEBHOOKS=false` on the manager Deployment.

## After merge

Set `targetRevision` back to your mainline/release version (e.g. `0.7.0` once
released). Preview versions are prereleases, so ArgoCD never selects them unless
you target them explicitly.

## Cleanup

Preview images and chart versions accumulate in GHCR. Delete old ones manually
from the package settings for `temporal-operator` (image) and `charts/temporal-operator`
(chart) under the org's **Packages**. (Automated retention is future work.)
```

Note: in the file above, replace the `\`\`\`yaml` / `\`\`\`` escaped fences with real triple-backtick fences — the backslashes are only to show them inside this plan.

- [ ] **Step 2: Verify the doc renders as valid Markdown**

Run:
```bash
test -f docs/preview-environments.md && grep -q "targetRevision" docs/preview-environments.md && echo DOC_OK
```
Expected: `DOC_OK`.

- [ ] **Step 3: Commit**

```bash
git add docs/preview-environments.md
git commit -s -m "docs: add preview environments (ArgoCD) guide

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Final verification

- [ ] **Workflow lints / parses:** `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/preview-image.yml'))" && echo OK` → `OK`. If `actionlint` is available, it reports no errors.
- [ ] **Chart still packages with a prerelease version** (proves the core mechanism): `helm package dist/chart --version 0.7.0-pr.x --app-version 0.7.0-pr.x --destination /tmp && ls /tmp/temporal-operator-0.7.0-pr.x.tgz` then remove it.
- [ ] **Chart renders:** `helm template dist/chart >/dev/null && echo OK`.
- [ ] **No stray changes:** `git status --short` is clean after commits.

## Self-review notes (verified while writing)
- **Spec coverage:** combined workflow (Tasks 1-2), prerelease `<base>-pr.<sha>` + SemVer guard (Task 1), unified `V` for image/version/appVersion (Tasks 1-2, image ref + `--app-version V`), same OCI repo `ghcr.io/bmorton/charts` (Task 2), run summary + ArgoCD snippet (Task 2), docs incl. cleanup note (Task 3), security (job already scoped `packages: write`/`persist-credentials: false`; `helm registry login` via `GITHUB_TOKEN`; pinned `azure/setup-helm` SHA). All spec sections map to a task.
- **Identifier consistency:** `steps.ref.outputs.version` (= `V`) and `steps.ref.outputs.image` (= `…:V`) are defined in Task 1 and consumed in Task 2; `--version`/`--app-version` both use `V`.
- **Note on `cache: false`:** the existing `setup-go` step in this job already sets `cache: false`; the new Helm step adds no Go cache, so no change needed.
