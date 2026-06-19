# temporal-operator

[//]: # (status-badges)
<!-- Badges: CI | Release | Go Report Card | License -->
[![CI](https://github.com/bmorton/temporal-operator/actions/workflows/ci.yml/badge.svg)](https://github.com/bmorton/temporal-operator/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/bmorton/temporal-operator)](https://github.com/bmorton/temporal-operator/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/bmorton/temporal-operator)](https://goreportcard.com/report/github.com/bmorton/temporal-operator)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE)

A modern Kubernetes operator for [Temporal](https://temporal.io), written in Go
with the Operator SDK / Kubebuilder. It manages the full lifecycle of Temporal
clusters — persistence, schema management, mTLS, rollouts, and version upgrades —
through declarative custom resources.

> **Status:** early development. APIs are `v1alpha1` and subject to change.

## Installation

The operator is published as a Helm chart (OCI) and a multi-arch container
image on GitHub Container Registry.

**Prerequisites:**

- A Kubernetes cluster (the operator image is built for `linux/amd64` and
  `linux/arm64`).
- [cert-manager](https://cert-manager.io/docs/installation/) installed in the
  cluster — the chart provisions webhook and metrics certificates through it.

**Install:**

```sh
helm install temporal-operator \
  oci://ghcr.io/bmorton/charts/temporal-operator \
  --namespace temporal-operator-system --create-namespace
```

Omitting `--version` installs the latest release; pin a specific version (for
example `--version 0.2.1`) for reproducible deploys.

The chart defaults to the matching operator image
(`ghcr.io/bmorton/temporal-operator`) at the chart's `appVersion`, so no extra
configuration is required. See [`dist/chart/README.md`](./dist/chart/README.md)
for configurable values.

## Custom Resources

| Kind | Short | Purpose |
|---|---|---|
| `TemporalCluster` | `tc` | A complete Temporal server deployment. |
| `TemporalDevServer` | `tds` | A disposable single-pod dev server (SQLite); for development/CI, not production. |
| `TemporalNamespace` | `tns` | A namespace within a managed cluster. |
| `TemporalSearchAttribute` | `tsa` | A custom search attribute registration. |
| `TemporalSchedule` | `tsch` | A declarative Temporal schedule (cron or interval). |
| `TemporalClusterClient` | `tcc` | Generated mTLS client credentials for a cluster. |

## Documentation

See the [documentation site](https://bmorton.github.io/temporal-operator/). The
[`examples/`](./examples) directory also contains sample custom resources.

The [Resource Preview](https://bmorton.github.io/temporal-operator/tools/resource-preview/)
tool runs in your browser (no install) and shows every Kubernetes object the
operator would create for a pasted `TemporalCluster`.

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md). This project follows the
[Contributor Covenant](./CODE_OF_CONDUCT.md) code of conduct, requires
[Conventional Commits](https://www.conventionalcommits.org/), and uses the
[Developer Certificate of Origin](https://developercertificate.org/) (DCO).

## Acknowledgments

This project is heavily inspired by and indebted to
[alexandrevilain/temporal-operator](https://github.com/alexandrevilain/temporal-operator) —
it simply wouldn't exist without that work.

temporal-operator was built from the ground up as a personal project to explore
building a Kubernetes operator end to end, to automate as much of a project's
lifecycle as is reasonable and helpful, and to sharpen my skills working with
agentic tooling.

## License

Licensed under the [Apache License 2.0](./LICENSE).
