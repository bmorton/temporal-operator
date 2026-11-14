# temporal-operator

A Helm chart for the [temporal-operator](https://github.com/bmorton/temporal-operator),
a Kubernetes operator for managing Temporal clusters, namespaces, search
attributes, and client credentials.

## Prerequisites

- Kubernetes 1.30+
- [cert-manager](https://cert-manager.io/) (when `webhook.enabled=true`, the default)
- [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator)
  (only when `prometheus.enabled=true`)

## Installing

```sh
helm install temporal-operator ./dist/chart \
  --namespace temporal-system --create-namespace
```

## Uninstalling

```sh
helm uninstall temporal-operator --namespace temporal-system
```

## Values

| Key | Description | Default |
|---|---|---|
| `manager.replicas` | Controller manager replica count | `1` |
| `manager.image.repository` | Operator image repository | `ghcr.io/bmorton/temporal-operator` |
| `manager.image.tag` | Operator image tag | chart `appVersion` |
| `manager.args` | Extra arguments for the manager | `[--leader-elect]` |
| `manager.env` | Extra environment variables for the manager | `[]` |
| `manager.resources` | Controller resource requests/limits | see `values.yaml` |
| `crd.enabled` | Install the CRDs with the chart | `true` |
| `crd.keep` | Keep CRDs on chart uninstall | `true` |
| `webhook.enabled` | Enable admission webhooks (requires cert-manager) | `true` |
| `certManager.enabled` | Provision webhook/metrics certs via cert-manager | `true` |
| `metrics.enabled` | Expose the controller metrics service | `true` |
| `metrics.secure` | Serve metrics over HTTPS with authn/authz | `true` |
| `prometheus.enabled` | Create a ServiceMonitor for the controller metrics | `false` |
| `networkPolicy.enabled` | Restrict ingress to the manager with NetworkPolicies | `false` |
| `rbac.helpers.enabled` | Install admin/editor/viewer ClusterRoles for the CRDs | `false` |
| `rbac.namespaced` | Use Role/RoleBinding in the release namespace instead of cluster-wide RBAC | `false` |
| `serviceAccount.enabled` | Create the manager ServiceAccount | `true` |
| `nameOverride`, `fullnameOverride` | Override the names of the chart's resources | unset |

See [`values.yaml`](./values.yaml) for the full, commented set of values.

## Upgrading from 0.14 or earlier

The chart layout follows a newer kubebuilder Helm plugin and several values
were renamed. Update your values before upgrading:

| Old | New |
|---|---|
| `crd.enable` | `crd.enabled` |
| `metrics.enable` | `metrics.enabled` |
| `certManager.enable` | `certManager.enabled` |
| `webhook.enable` | `webhook.enabled` |
| `prometheus.enable` | `prometheus.enabled` |
| `rbacHelpers.enable` | `rbac.helpers.enabled` |

An old key is ignored without an error, so a value such as
`webhook.enable=false` silently stops having any effect.

Resource names now start with the release's full name instead of a fixed
`temporal-operator-` prefix. A release named `temporal-operator` keeps every
name it had. A release with any other name gets renamed resources on upgrade
(for example `<release>-temporal-operator-controller-manager`); set
`fullnameOverride: temporal-operator` to keep the old names.

CRDs now carry `helm.sh/resource-policy: keep` when `crd.keep` is true (the
default), so `helm uninstall` leaves them, and the custom resources, in place.
