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
| `TemporalMigration` | `tm` | Gradually migrate traffic from an external Temporal cluster to a managed one. |
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

## Operator UI

The operator ships an optional, read-only web UI that shows an overview of every
TemporalCluster it manages and a per-cluster detail view (services, persistence,
mTLS, endpoints, conditions, in-flight upgrades, and related namespaces, clients
and search attributes).

The views refresh live; the header shows a "last updated" timestamp and a
Pause/Resume toggle so you can freeze the auto-refresh while inspecting data.

It is **disabled by default**. Enable it by setting `--ui-bind-address` (for
example `:8082`) on the manager and front it with a forward-auth proxy (e.g.
Authelia) — the operator does not authenticate users itself.

### Enabling it

The UI flags are manager arguments. With the Helm chart, add them to
`manager.args` in your values:

```yaml
manager:
  args:
    - --leader-elect
    - --ui-bind-address=:8082
    # Recommended whenever the UI is reachable in-cluster (fail closed):
    - --ui-require-auth
```

```sh
helm upgrade --install temporal-operator dist/chart -n temporal-operator-system \
  --set-string 'manager.args[0]=--leader-elect' \
  --set-string 'manager.args[1]=--ui-bind-address=:8082'
```

(Using raw kustomize instead? Add the same flags to the manager container args
and apply the `config/ui` overlay for the Service.)

### Take a quick look

To browse the UI without setting up a proxy first, port-forward the manager and
open it locally (leave `--ui-require-auth` off for this, or send the header
yourself):

```sh
kubectl -n temporal-operator-system port-forward \
  deploy/temporal-operator-controller-manager 8082:8082
# then open http://localhost:8082
```

### Exposing it for real

To expose the UI to users, apply the `config/ui` Service and front it with a
forward-auth proxy. See [`examples/ui/`](./examples/ui/) for a worked Authelia
Ingress, a NetworkPolicy, and the security notes below.

Because the Service routes directly to the pod, set `--ui-require-auth` (and
optionally a NetworkPolicy; see `examples/ui/`) so direct in-cluster access
cannot bypass the proxy.

| Flag | Default | Description |
| --- | --- | --- |
| `--ui-bind-address` | _(empty)_ | UI listen address (e.g. `:8082`). Empty disables the UI. |
| `--ui-refresh-interval` | `5s` | htmx auto-refresh interval. |
| `--ui-base-path` | `/` | Serve the UI under a sub-path. |
| `--ui-require-auth` | `false` | Return 401 when no trusted user header is present. |

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
