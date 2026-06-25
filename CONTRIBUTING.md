# Contributing to temporal-operator

Thanks for your interest in contributing! This document explains how to get set
up, the conventions we follow, and how to submit changes.

## Code of Conduct

This project adheres to the [Contributor Covenant](./CODE_OF_CONDUCT.md). By
participating, you are expected to uphold this code.

## Quickstart

### Prerequisites

- Go **1.26.3** (see `go.mod` for the authoritative version).
- `make`.
- Docker (for building images and running kind).
- [kind](https://kind.sigs.k8s.io/) for local clusters.
- [kubebuilder](https://book.kubebuilder.io/) and the project tooling, installed
  via `make install-tools`.

### Build and test

```sh
make install-tools     # install pinned dev tooling
make generate manifests # regenerate deepcopy + CRD manifests
make build             # compile
make test              # unit + envtest suites
make lint              # golangci-lint
```

### Helm chart

`dist/chart` is generated — do **not** edit it by hand. Run:

```sh
make helm-chart   # regenerates dist/chart deterministically
```

Generation is `kubebuilder edit` plus a post-processor (`hack/helmgen`). Files
that must be hand-maintained live in `hack/helm/overrides/` (mirroring their
`dist/chart/` paths) and are copied over the generated output; edit those, not
`dist/chart`. The `Verify generated chart` CI job fails if `dist/chart` is stale,
so always run `make helm-chart` and commit the result after changing API types,
RBAC markers, or chart overrides.

### Run against a local cluster

```sh
make kind-up
make install           # install CRDs
make run               # run the controller locally
```

### Run e2e (Chainsaw) on Namespace (nsc)

When [kind](https://kind.sigs.k8s.io/) cannot run locally (for example inside a
devcontainer), you can run the Chainsaw e2e suites against an ephemeral
[Namespace](https://cloud.namespace.so/) cluster instead. CI still uses kind;
this is an alternate path for local/agent validation.

Prerequisites: the `nsc` CLI installed and authenticated (`nsc login`), plus
`kubectl`, `helm`, and `jq` on your `PATH`.

```sh
make chainsaw-test-nsc                 # runs the postgres/lifecycle suite
make chainsaw-test-nsc SUITE=mtls      # run a different suite under test/e2e/
```

The runner builds and pushes the operator image with `nsc build` (no local
Docker needed), provisions an ephemeral cluster, installs cert-manager,
CloudNativePG, and the operator, runs the suite, then destroys the cluster.

Billing safety is layered: the cluster is created `--ephemeral` with a 30m
`--duration` (override with `NSC_DURATION=`) so it auto-expires even if the
process is killed, and the script destroys it on exit. To purge any leftover
clusters from interrupted runs:

```sh
make nsc-clean
```

`nsc-clean` only destroys clusters labeled `app=temporal-operator-e2e`; it never
touches the shared Namespace build cluster.

## Conventional Commits

We **prefer** commit messages that follow the
[Conventional Commits](https://www.conventionalcommits.org/) specification, e.g.:

```
feat(controller): reconcile persistence schema
fix(webhook): reject immutable shard count change
docs: document mTLS configuration
```

Common types are `feat`, `fix`, `docs`, `style`, `refactor`, `perf`,
`test`, `build`, `ci`, `chore`, and `revert`. CI checks commit messages and
reports suggestions, but **the check is non-blocking** — it will not fail your
PR. The leading `type:` matters most: while the project is pre-1.0,
[release-please](https://github.com/googleapis/release-please) uses it to build
the changelog and choose the next version, so `feat:` and `fix:` are
especially helpful.

## Developer Certificate of Origin (DCO)

We require all commits to be signed off under the
[Developer Certificate of Origin](https://developercertificate.org/). Add the
`Signed-off-by` trailer to every commit:

```sh
git commit -s -m "feat: add something"
```

CI enforces the DCO on every pull request.

## Pull requests

1. Fork the repository and create a topic branch.
2. Make your change with accompanying tests (see the testing pyramid in the
   build plan: envtest-heavy, with unit and Chainsaw e2e as appropriate).
3. Ensure `make test lint manifests generate` all pass and produce no diff.
4. Open a pull request and fill out the template.
5. A maintainer will review per the [governance process](./GOVERNANCE.md).

## Reporting bugs and requesting features

Use the issue templates under **New Issue**. For questions, use
[Discussions](https://github.com/bmorton/temporal-operator/discussions).
