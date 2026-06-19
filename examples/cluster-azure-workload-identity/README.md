# Passwordless Flexible Server via Azure Workload Identity

Demonstrates fully passwordless authentication to Azure Database for PostgreSQL
Flexible Server using [Azure Workload Identity](https://azure.github.io/azure-workload-identity/)
and Microsoft Entra access tokens. No static passwords anywhere.

| Actor | Token mechanism |
|---|---|
| **Temporal server pods** | `azure-token-refresher` sidecar writes `/azure/pgpass`; Temporal reads it via `passwordCommand` on every connection |
| **Schema Job** | one-shot `initContainer` writes `/azure/pgpass`; schema container reads it via `passwordCommand` |
| **Operator** (probe + schema inspection) | obtains an Entra token **natively in-process** via the Go Azure Workload Identity SDK — no sidecar, no shell required on the distroless image; enabled by `sql.azureWorkloadIdentity: {}` on each store |

This resolved [#47](https://github.com/bmorton/temporal-operator/issues/47).

## How it works

1. The `ServiceAccount` (`serviceaccount.yaml`) is annotated with the managed
   identity's `client-id`.
2. `podTemplate.labels` sets `azure.workload.identity/use: "true"`, so the
   Workload Identity webhook injects `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`,
   `AZURE_FEDERATED_TOKEN_FILE`, and `AZURE_AUTHORITY_HOST` into every container.
3. `podTemplate.spec` adds the `azure-token-refresher` sidecar, which performs a
   federated `az login` and writes a fresh Entra access token to a shared
   `emptyDir` (`/azure/pgpass`) every 30 minutes. Server pods read the token via
   `passwordCommandSecretRef` on every connection.
4. `schemaJob.podTemplate` gives the schema setup/update Jobs the same
   ServiceAccount and WI label, plus a one-shot `initContainer` that writes an
   Entra token to `/azure/pgpass` before the schema container starts.
5. The operator itself obtains its Entra token **natively in-process** (Go Azure
   Workload Identity SDK) — it runs on a distroless image that has no shell, so it
   cannot use `passwordCommand`. Set `sql.azureWorkloadIdentity: {}` on each store
   to enable this. The operator pod needs only the WI label + SA annotation (no
   sidecar on the operator pod).

## Prerequisites

- AKS with the OIDC issuer and Workload Identity enabled
  (`az aks update --enable-oidc-issuer --enable-workload-identity`).
- A managed identity (or app registration) with a federated credential bound to
  the `temporal-azure` ServiceAccount in the cluster's namespace.
- The identity mapped to a Postgres role on the Flexible Server:
  `SELECT pgaadauth_create_principal('temporal-identity', false, false);`
- Microsoft Entra authentication enabled on the Flexible Server.
- The **operator's** managed identity must also be federated to the
  `temporal-operator-controller-manager` ServiceAccount in the `temporal-system`
  namespace, and mapped to a Postgres role via `pgaadauth_create_principal`. The
  operator and the Temporal cluster can share a single managed identity or use
  separate ones; if separate, each needs its own federated credential and Postgres
  role.

## Install the operator

```sh
helm install temporal-operator oci://ghcr.io/bmorton/charts/temporal-operator \
  --namespace temporal-system --create-namespace \
  --set workloadIdentity.enable=true \
  --set workloadIdentity.clientId=<operator-managed-identity-client-id>
```

`workloadIdentity.enable=true` adds the `azure.workload.identity/use: "true"`
label and the `azure.workload.identity/client-id` annotation to the operator's
ServiceAccount. The operator needs **no token-refresher sidecar** — it fetches
the Entra token natively in-process.

## Apply

```sh
kubectl apply -f serviceaccount.yaml
kubectl apply -f temporalcluster.yaml
```

The server may restart a few times on first boot until the sidecar writes the
first token; the `passwordCommand` waits for the token file to avoid most of
this.
