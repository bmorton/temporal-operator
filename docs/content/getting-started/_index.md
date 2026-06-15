+++
title = "Getting Started"
weight = 10
+++

# Getting Started

This guide takes you from an empty kind cluster to a running Temporal cluster in
about 15 minutes.

## 1. Prerequisites

- A Kubernetes cluster (e.g. `kind create cluster`).
- [cert-manager](https://cert-manager.io/docs/installation/) installed.
- The [CloudNativePG](https://cloudnative-pg.io/) operator installed:

  ```sh
  kubectl apply --server-side -f \
    https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.24/releases/cnpg-1.24.0.yaml
  ```

## 2. Install the operator

```sh
helm install temporal-operator oci://ghcr.io/bmorton/charts/temporal-operator \
  --namespace temporal-system --create-namespace
```

Or from a checkout:

```sh
helm install temporal-operator ./dist/chart \
  --namespace temporal-system --create-namespace
```

## 3. Provision Postgres

```sh
kubectl apply -f examples/cluster-cnpg-integrated/01-cnpg.yaml
```

This creates a `temporal` namespace and a single-instance CNPG Postgres. Two
details matter and are easy to miss:

- **Temporal needs two databases.** The operator only runs schema setup; it does
  not create databases. `initdb` creates `temporal`, and `postInitSQL` creates
  the separate `temporal_visibility` database.
- **Raise `max_connections`.** Every Temporal service pod opens a connection
  pool to both stores, which exceeds Postgres's default of 100. The example sets
  `max_connections: "200"`.

Wait for Postgres to be ready:

```sh
kubectl -n temporal get cluster temporal-pg -w
```

## 4. Create a TemporalCluster

```sh
kubectl apply -f examples/cluster-cnpg-integrated/02-temporalcluster.yaml
kubectl -n temporal get temporalcluster temporal -w
```

The persistence `host` is a namespace-qualified FQDN
(`temporal-pg-rw.temporal.svc.cluster.local`) because the operator pings the
database from its own pod in `temporal-system`; a bare service name would not
resolve there.

When `READY` is `True`, the cluster is serving on
`temporal-frontend.temporal.svc:7233`.

## 5. Run a workflow

```sh
kubectl -n temporal run tctl --rm -it --image=temporalio/admin-tools:1.31.1 -- \
  temporal operator cluster health --address temporal-frontend:7233
```

## 6. Open the Web UI

```sh
kubectl -n temporal port-forward svc/temporal-ui 8080:8080
```

Then browse to `http://localhost:8080`. To expose the UI through an Ingress
instead, uncomment the `ui.ingress` block in
`examples/cluster-cnpg-integrated/02-temporalcluster.yaml`.
