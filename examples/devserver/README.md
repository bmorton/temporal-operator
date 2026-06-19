# TemporalDevServer examples

`TemporalDevServer` runs a single-pod `temporal server start-dev` instance backed
by embedded SQLite. It is designed for local development and CI — **not for
production**. Data is ephemeral by default (wiped on pod restart); a Persistent
variant keeps data across restarts via a PVC.

| File | Description |
| --- | --- |
| [`minimal.yaml`](./minimal.yaml) | Minimal ephemeral dev server (`dev`) — SQLite, UI enabled, no extra setup. |
| [`persistent.yaml`](./persistent.yaml) | Dev server with a PVC (`dev-persistent`) and two extra namespaces (`orders`, `billing`). |
| [`namespace-on-devserver.yaml`](./namespace-on-devserver.yaml) | `TemporalNamespace` targeting the `dev` server via the polymorphic `clusterRef`. |

## Apply

```sh
# Minimal (ephemeral) dev server:
kubectl apply -f minimal.yaml

# Persistent variant with pre-created namespaces:
kubectl apply -f persistent.yaml

# Managed namespace pointing at the dev server:
kubectl apply -f namespace-on-devserver.yaml

# Check status (short name: tds):
kubectl get temporaldevservers
kubectl get tds
```

## Accessing the dev server

The operator creates a Service named `<name>-devserver` in the same namespace:

| Endpoint | Use |
| --- | --- |
| `dev-devserver:7233` | Temporal frontend gRPC (SDK / CLI connections) |
| `dev-devserver:8233` | Temporal Web UI |

From inside the cluster (or via `kubectl port-forward`):

```sh
# Forward the UI to localhost:
kubectl port-forward svc/dev-devserver 8233:8233

# Run the Temporal CLI against the dev server:
temporal namespace list --address dev-devserver:7233
```

## Namespaces

The `default` Temporal namespace is created automatically at startup. Additional
namespaces can be pre-created two ways:

1. **`spec.namespaces`** — list them in the `TemporalDevServer` spec (see
   `persistent.yaml`); they exist as soon as the pod is ready.
2. **`TemporalNamespace` CRs** — create managed namespace objects with a
   `clusterRef` pointing at the dev server (see `namespace-on-devserver.yaml`);
   the operator reconciles them like it would against a full `TemporalCluster`.

## Polymorphic `clusterRef`

`TemporalNamespace`, `TemporalSchedule`, and `TemporalSearchAttribute` each
accept a `clusterRef` that can target either a `TemporalCluster` or a
`TemporalDevServer`. The `kind` field defaults to `TemporalCluster`, so existing
manifests are unaffected. To target a dev server, set `kind: TemporalDevServer`:

```yaml
clusterRef:
  name: dev
  kind: TemporalDevServer
```

## Storage

| `storage.type` | Behaviour |
| --- | --- |
| `Ephemeral` (default) | `emptyDir` volume — data is wiped when the pod restarts. |
| `Persistent` | PVC provisioned with `storage.size` (default `1Gi`) and optional `storage.storageClassName`. |

## Version field

`spec.version` is the **Temporal server version** (e.g. `1.31.1`), the same value
`TemporalCluster.spec.version` takes. The operator maps it to the matching
`temporalio/temporal` CLI image (which runs `temporal server start-dev`) using the
supported version matrix. When `version` is omitted, the latest supported server
version is used.

To pin a specific CLI image directly instead, set `spec.image` (e.g.
`temporalio/temporal:1.7.2`); it takes precedence over `version`.
