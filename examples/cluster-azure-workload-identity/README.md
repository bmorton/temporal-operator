# PREVIEW: passwordless Flexible Server via Azure Workload Identity

Demonstrates passwordless authentication to Azure Database for PostgreSQL
Flexible Server using [Azure Workload Identity](https://azure.github.io/azure-workload-identity/)
and Microsoft Entra access tokens. It exercises the operator's `podTemplate`
override to inject a `serviceAccountName`, the Workload Identity pod label, and a
token-refresher sidecar.

## Status: preview

This makes the **Temporal server pods** passwordless. Two actors are **not** yet
passwordless, so a cluster applied as-is will not reach Ready without extra
setup:

- The **operator's** reachability probe + schema inspection still build the DSN
  from a static password.
- The **schema Job** (`setup-schema`) still reads a static `SQL_PASSWORD`.

Tracking issue: [#47](https://github.com/bmorton/temporal-operator/issues/47).
Until it lands, bootstrap the schema with password auth (apply
`cluster-azure-postgres-flexible` once to create the schema), or grant the
operator/job identity temporary password access.

## How it works

1. The `ServiceAccount` (`serviceaccount.yaml`) is annotated with the managed
   identity's `client-id`.
2. `podTemplate.labels` sets `azure.workload.identity/use: "true"`, so the
   Workload Identity webhook injects `AZURE_CLIENT_ID`, `AZURE_TENANT_ID`,
   `AZURE_FEDERATED_TOKEN_FILE`, and `AZURE_AUTHORITY_HOST` into every container.
3. `podTemplate.spec` adds the `azure-token-refresher` sidecar, which performs a
   federated `az login` and writes a fresh Entra access token to a shared
   `emptyDir` (`/azure/pgpass`) every 30 minutes.
4. `passwordCommandSecretRef` tells Temporal to read the token file per
   connection, so expiring tokens are picked up automatically.

## Prerequisites

- AKS with the OIDC issuer and Workload Identity enabled
  (`az aks update --enable-oidc-issuer --enable-workload-identity`).
- A managed identity (or app registration) with a federated credential bound to
  this ServiceAccount.
- The identity mapped to a Postgres role on the Flexible Server:
  `SELECT pgaadauth_create_principal('temporal-identity', false, false);`
- Microsoft Entra authentication enabled on the Flexible Server.

## Apply

```sh
kubectl apply -f serviceaccount.yaml
kubectl apply -f temporalcluster.yaml
```

The server may restart a few times on first boot until the sidecar writes the
first token; the `passwordCommand` waits for the token file to avoid most of
this.
