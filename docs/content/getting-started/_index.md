# Getting Started

This guide takes you from an empty kind cluster to a running Temporal cluster in
about 15 minutes.

## 1. Prerequisites

- A Kubernetes cluster (e.g. `kind create cluster`).
- [cert-manager](https://cert-manager.io/docs/installation/) installed.
- A Postgres database. The quickest path is [CloudNativePG](https://cloudnative-pg.io/).

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

## 3. Provision Postgres (CloudNativePG)

```sh
kubectl apply --server-side -f \
  https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.24/releases/cnpg-1.24.0.yaml
kubectl apply -f examples/cluster-cnpg-integrated/01-cnpg.yaml
```

## 4. Create a TemporalCluster

```sh
kubectl apply -f examples/cluster-cnpg-integrated/02-temporalcluster.yaml
kubectl get temporalcluster -w
```

When `READY` is `True`, the cluster is serving on
`temporal-lifecycle-frontend:7233`.

## 5. Run a workflow

```sh
kubectl run tctl --rm -it --image=temporalio/admin-tools:1.31.2 -- \
  temporal operator cluster health --address temporal-lifecycle-frontend:7233
```
