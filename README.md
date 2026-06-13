# temporal-operator

[//]: # (status-badges)
<!-- Badges: CI | Release | Go Report Card | License -->
![CI](https://img.shields.io/badge/ci-pending-lightgrey)
![Release](https://img.shields.io/badge/release-unreleased-lightgrey)
![License](https://img.shields.io/badge/license-Apache--2.0-blue)

A modern Kubernetes operator for [Temporal](https://temporal.io), written in Go
with the Operator SDK / Kubebuilder. It manages the full lifecycle of Temporal
clusters — persistence, schema management, mTLS, rollouts, and version upgrades —
through declarative custom resources.

> **Status:** early development. APIs are `v1alpha1` and subject to change.

## Custom Resources

| Kind | Short | Purpose |
|---|---|---|
| `TemporalCluster` | `tc` | A complete Temporal server deployment. |
| `TemporalNamespace` | `tns` | A namespace within a managed cluster. |
| `TemporalSearchAttribute` | `tsa` | A custom search attribute registration. |
| `TemporalClusterClient` | `tcc` | Generated client credentials for a cluster. |

## Documentation

See the documentation site (coming soon). Until then, the
[`examples/`](./examples) directory contains sample custom resources.

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md). This project follows the
[Contributor Covenant](./CODE_OF_CONDUCT.md) code of conduct, requires
[Conventional Commits](https://www.conventionalcommits.org/), and uses the
[Developer Certificate of Origin](https://developercertificate.org/) (DCO).

## License

Licensed under the [Apache License 2.0](./LICENSE).
