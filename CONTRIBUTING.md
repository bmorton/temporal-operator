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

### Run against a local cluster

```sh
make kind-up
make install           # install CRDs
make run               # run the controller locally
```

## Conventional Commits

All commit messages **must** follow the
[Conventional Commits](https://www.conventionalcommits.org/) specification, e.g.:

```
feat(controller): reconcile persistence schema
fix(webhook): reject immutable shard count change
docs: document mTLS configuration
```

Allowed types include `feat`, `fix`, `docs`, `style`, `refactor`, `perf`,
`test`, `build`, `ci`, `chore`, and `revert`. This is enforced by CI.

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
