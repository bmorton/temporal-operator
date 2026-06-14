# temporal-operator

A Helm chart for the [temporal-operator](https://github.com/bmorton/temporal-operator),
a Kubernetes operator for managing Temporal clusters, namespaces, search
attributes, and client credentials.

## Prerequisites

- Kubernetes 1.30+
- [cert-manager](https://cert-manager.io/) (when `webhook.enable=true`, the default)
- [prometheus-operator](https://github.com/prometheus-operator/prometheus-operator)
  (only when `prometheus.enable=true`)

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
| `manager.resources` | Controller resource requests/limits | see `values.yaml` |
| `crd.enable` | Install the CRDs with the chart | `true` |
| `crd.keep` | Keep CRDs on chart uninstall | `true` |
| `webhook.enable` | Enable admission webhooks (requires cert-manager) | `true` |
| `certManager.enable` | Provision webhook/metrics certs via cert-manager | `true` |
| `prometheus.enable` | Create a ServiceMonitor for the controller metrics | `false` |
| `rbac.enable` | Install RBAC | `true` |
| `metrics.enable` | Expose the controller metrics service | `true` |

See [`values.yaml`](./values.yaml) for the full, commented set of values.
