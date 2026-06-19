# Passwordless Flexible Server via Azure Workload Identity

Demonstrates fully passwordless authentication to Azure Database for PostgreSQL
Flexible Server using [Azure Workload Identity](https://azure.github.io/azure-workload-identity/)
and Microsoft Entra access tokens. The **Temporal server pods**, the **operator's
reachability probe**, and the **schema Job** all authenticate with short-lived
Entra tokens — no static passwords anywhere.

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
5. The operator itself is installed with `workloadIdentity.enable=true` so its
   reachability probe also obtains a token via `passwordCommand`.

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

## Apply

```sh
kubectl apply -f serviceaccount.yaml
kubectl apply -f temporalcluster.yaml
```

The server may restart a few times on first boot until the sidecar writes the
first token; the `passwordCommand` waits for the token file to avoid most of
this.
