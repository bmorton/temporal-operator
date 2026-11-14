# GitHub Copilot instructions

These instructions apply to GitHub Copilot (chat, code completion, and the
coding agent) when working in **temporal-operator**. They mirror
[`AGENTS.md`](../AGENTS.md); see that file and
[`CONTRIBUTING.md`](../CONTRIBUTING.md) for full detail.

## Project status

Pre-1.0 and early. Keep changes focused; avoid unrequested breaking changes.

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<optional scope>): <short summary>
```

Common types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`,
`build`, `ci`, `chore`, `revert`.

- **The PR title must be a Conventional Commit.** PRs are squash-merged and the
  PR title becomes the commit message on `main`. The `PR title` check is
  required to merge; a title without a type (e.g. "Fix zizmor reference")
  blocks the PR.
- The commitlint check on individual commits is **non-blocking** — it reports
  suggestions but never fails the build, so do not over-rotate on formatting.
- `release-please` uses the `type:` prefix to build the changelog and pick the
  next version (`feat:` → minor, `fix:` → patch; breaking changes stay within
  `0.x` while pre-1.0).

## Sign-off (DCO)

Sign off every commit (this check **is** enforced):

```sh
git commit -s -m "feat: add something"
```

## Build, test, lint

```sh
make install-tools       # install pinned dev tooling
make generate manifests  # regenerate deepcopy + CRD manifests
make build               # compile
make test                # unit + envtest suites
make lint                # golangci-lint
```

Run `make generate manifests` after changing API types and `make lint` before
opening a PR.

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
[`.claude/skills/temporal-version-bump/SKILL.md`](../.claude/skills/temporal-version-bump/SKILL.md).
Read it before editing `hack/version-matrix.yaml`. New upstream releases are
tracked as checklist tasks on a single open `temporal-release` issue,
maintained by `.github/workflows/temporal-releases.yml`.

## Releases

Versioning is automated by `release-please`. Do not hand-edit version numbers or
`CHANGELOG.md`.
