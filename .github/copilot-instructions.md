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

- The commitlint CI check is **non-blocking** — it reports suggestions but never
  fails the build, so do not over-rotate on formatting.
- Still follow the convention: `release-please` uses the `type:` prefix to build
  the changelog and pick the next version (`feat:` → minor, `fix:` → patch;
  breaking changes stay within `0.x` while pre-1.0).

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

Generation is `kubebuilder edit` plus a post-processor (`hack/helmgen`). Files
that must be hand-maintained live in `hack/helm/overrides/` (mirroring their
`dist/chart/` paths) and are copied over the generated output; edit those, not
`dist/chart`. The `Verify generated chart` CI job fails if `dist/chart` is stale,
so always run `make helm-chart` and commit the result after changing API types,
RBAC markers, or chart overrides. Because the manager Deployment template is hand-owned, changes to `config/manager/manager.yaml` (new args, env vars, probes, volumes, resources) are **not** picked up automatically — mirror them into `hack/helm/overrides/templates/manager/manager.yaml` by hand.

## Releases

Versioning is automated by `release-please`. Do not hand-edit version numbers or
`CHANGELOG.md`.
