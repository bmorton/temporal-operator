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

> The **Run workflow** button appears once this workflow is on the repository's
> default branch. After that you can dispatch it against any branch, and it uses
> the workflow definition and `dist/chart` from the branch you select.

## Publishing a preview

1. In the GitHub **Actions** tab, open **Preview Image** and click **Run
   workflow**. Select the branch you want to preview.
2. Optional inputs:
   - `baseVersion` — base SemVer (default: the latest release with its minor
     bumped, e.g. `0.7.0`). The published version becomes
     `<baseVersion>-pr.<short-sha>`.
   - `version` — a full SemVer override (wins over `baseVersion`).
   - `platforms` — image target platforms.
3. When the run finishes, copy the version `V` and the ArgoCD snippet from the
   run **Summary**.

## Testing with ArgoCD

Point your Application at the preview chart version. Only `targetRevision`
changes from your normal install:

```yaml
source:
  repoURL: ghcr.io/bmorton/charts
  chart: temporal-operator
  targetRevision: 0.7.0-pr.a1b2c3d   # the V from the run summary
  helm:
    releaseName: temporal-operator
```

The chart expects cert-manager (for the validating webhook). To skip the
webhook, set `ENABLE_WEBHOOKS=false` on the manager Deployment.

## After merge

Set `targetRevision` back to your mainline/release version (e.g. `0.7.0` once
released). Preview versions are prereleases, so ArgoCD never selects them unless
you target them explicitly.

## Cleanup

Preview images and chart versions accumulate in GHCR. Delete old ones manually
from the package settings for `temporal-operator` (image) and
`charts/temporal-operator` (chart) under the org's **Packages**. (Automated
retention is future work.)
