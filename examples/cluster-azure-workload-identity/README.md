# Passwordless Flexible Server via Azure Workload Identity

Demonstrates fully passwordless authentication to Azure Database for PostgreSQL
Flexible Server using [Azure Workload Identity](https://azure.github.io/azure-workload-identity/)
and Microsoft Entra access tokens. No static passwords anywhere.

| Actor | Token mechanism |
|---|---|
| **Temporal server pods** | `azure-token-refresher` sidecar writes `/azure/pgpass`; Temporal reads it via `passwordCommand` on every connection |
| **Schema Job** | one-shot `initContainer` writes `/azure/pgpass`; schema container reads it via `passwordCommand` |
| **Operator** (probe + schema inspection) | obtains an Entra token **natively in-process** via the Go Azure Workload Identity SDK — no sidecar, no shell required on the distroless image |

This resolved [#47](https://github.com/bmorton/temporal-operator/issues/47).

## How it works

The operator generates **all** the Azure Workload Identity wiring (ServiceAccount,
sidecars, initContainers, passwordCommand, inspector Jobs) from a single
cluster-level `persistence.azureWorkloadIdentity.clientId` field. You provide the
managed identity's client ID; the operator synthesizes the rest.

## Prerequisites

- AKS with the OIDC issuer and Workload Identity enabled
  (`az aks update --enable-oidc-issuer --enable-workload-identity`).
- A managed identity (or app registration) with a federated credential bound to
  the **operator-generated** ServiceAccount `<cluster-name>-azure` in the
  cluster's namespace (e.g., `azure-wi-azure` in namespace `default`).
- The identity mapped to a Postgres role on the Flexible Server:
  `SELECT pgaadauth_create_principal('temporal-identity', false, false);`
- Microsoft Entra authentication enabled on the Flexible Server.
- The `azure.extensions` allow-list set to `btree_gin,pg_trgm` (required by
  Temporal's SQL visibility schema).

## Apply

```sh
kubectl apply -f temporalcluster.yaml
```

The operator creates the ServiceAccount (`<cluster-name>-azure`) and wires the
server pods, schema Jobs, and its own probe/schema-inspection Jobs. Ensure your
managed identity's federated credential is bound to this generated ServiceAccount
before applying.
