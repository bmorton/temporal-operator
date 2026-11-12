# Agent & LLM Contributor Guide

This file gives AI coding agents (GitHub Copilot, Claude, Codex, etc.) the
conventions for contributing to **temporal-operator**. Human contributors
should read [`CONTRIBUTING.md`](./CONTRIBUTING.md) for the full guide.

> **Status: pre-1.0.** This project is early and the API may change. Keep
> changes focused and avoid introducing breaking behavior unless asked.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/). PRs are
squash-merged and **the PR title becomes the commit message on `main`**, so the
PR title must be a Conventional Commit. The `PR title` check enforces this and
is required to merge. Individual commit messages are linted too, but that check
is **non-blocking**. `release-please` derives the changelog and the next
version from the merged commit types.

Format:

```
<type>(<optional scope>): <short summary>
```

Common types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`,
`build`, `ci`, `chore`, `revert`.

- `feat:` → minor version bump (e.g. `0.1.0` → `0.2.0` while pre-1.0)
- `fix:` → patch version bump
- A `feat!:` / `fix!:` or a `BREAKING CHANGE:` footer is a **minor** bump while
  pre-1.0 (it will not jump to `1.0.0`).

Examples:

```
feat(controller): reconcile persistence schema
fix(webhook): reject immutable shard count change
docs: document mTLS configuration
```

## Sign-off (DCO)

Every commit must be signed off under the
[Developer Certificate of Origin](https://developercertificate.org/):

```sh
git commit -s -m "feat: add something"
```

This adds a `Signed-off-by:` trailer. The DCO check **is** enforced in CI.

## Build, test, and lint

```sh
make install-tools       # install pinned dev tooling
make generate manifests  # regenerate deepcopy + CRD manifests
make build               # compile
make test                # unit + envtest suites
make lint                # golangci-lint
```

Run `make generate manifests` after changing API types, and `make lint` before
opening a PR. The project targets the Go version pinned in `go.mod`.

Dev tools (controller-gen, kustomize, kind, chainsaw, crd-ref-docs,
golangci-lint, kubebuilder) are each pinned by a Go module in
`hack/tools/<tool>/go.mod` and bumped by Dependabot; the Makefile has no version
variables for them. The templ generator follows the `github.com/a-h/templ`
version in the root `go.mod`. After a controller-gen, kustomize, kubebuilder or
templ bump, run `make generate manifests helm-chart` and commit the result.

## Helm chart

`dist/chart` is generated — do **not** edit it by hand. Run:

```sh
make helm-chart   # regenerates dist/chart deterministically
```

Generation is `kubebuilder edit --force` plus a post-processor
(`hack/helmgen`), so `values.yaml`, the helpers and every template follow the
kubebuilder version pinned in `hack/tools/kubebuilder/go.mod`. Two things are
ours:

- **Patches** in `hack/helmgen/main.go` edit generated files (the default image
  repository, `manager.env`, and the `OPERATOR_IMAGE` env var on the manager
  container). A patch fails the run if the text it expects is gone.
- **Overrides** in `hack/helm/overrides/` (mirroring their `dist/chart/` paths)
  are copied over the generated output: `Chart.yaml`, `README.md`,
  `artifacthub-pkg.yaml` and `templates/NOTES.txt`. Edit those, not `dist/chart`.

The manager Deployment template is generated from `config/manager/manager.yaml`,
so changes there (args, probes, volumes, resources) reach the chart on the next
`make helm-chart`. The `Verify generated chart` CI job fails if `dist/chart` is
stale, so always run `make helm-chart` and commit the result after changing API
types, RBAC markers, `config/`, or chart overrides. Renaming or removing a chart
value is a breaking change for chart users: say so with a `BREAKING CHANGE:`
footer in the PR description.

## Temporal version bumps

Adding support for a new Temporal server, UI, CLI (dev server), or s2s-proxy
release is a recurring task with a dedicated playbook:
[`.claude/skills/temporal-version-bump/SKILL.md`](./.claude/skills/temporal-version-bump/SKILL.md).
Read it before editing `hack/version-matrix.yaml`. New upstream releases are
tracked as checklist tasks on a single open `temporal-release` issue,
maintained by `.github/workflows/temporal-releases.yml`.

## Releases

Releases are automated by `release-please`. Do **not** hand-edit version numbers
or `CHANGELOG.md`; let the release PR manage them. Versioning is configured in
`release-please-config.json` and `.release-please-manifest.json` to stay below
`1.0.0` until the project is ready.
