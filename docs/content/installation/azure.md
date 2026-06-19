+++
title = "Running on Azure"
weight = 30
+++

# Running on Azure

This guide covers running the Temporal operator on Azure: AKS, Azure Database for
PostgreSQL Flexible Server, Application Gateway ingress, and passwordless
Microsoft Entra authentication.

## AKS prerequisites

- An AKS cluster (`az aks create ...`).
- The operator installed (see the [installation guide](./_index.md)).

## Persistence: Flexible Server

Azure Database for PostgreSQL Flexible Server is the recommended SQL backend.

- Create the `temporal` and `temporal_visibility` databases up front — the
  operator runs `setup-schema` but does not create databases.
- Raise `max_connections` (~200) to avoid pool exhaustion on smaller SKUs.
- TLS is required; set `tls.enabled: true` on each store. Azure chains to a
  public root, so no CA secret is needed.

Example:
[`examples/cluster-azure-postgres-flexible`](https://github.com/bmorton/temporal-operator/tree/main/examples/cluster-azure-postgres-flexible).

## Exposing the UI with Application Gateway (AGIC)

Set `ui.ingress.ingressClassName: azure-application-gateway` and AGIC
annotations.

Example:
[`examples/cluster-azure-aks-ingress`](https://github.com/bmorton/temporal-operator/tree/main/examples/cluster-azure-aks-ingress).

## Passwordless auth with Microsoft Entra + Workload Identity

The operator drives the Temporal **server pods**, its own **reachability probe**,
and the **schema Job** passwordlessly using Azure Workload Identity and Entra
access tokens, via the `podTemplate` override (`serviceAccountName`, the
`azure.workload.identity/use` label, and a token-refresher sidecar) and the new
`schemaJob.podTemplate` field.

1. Enable the OIDC issuer and Workload Identity on AKS:
   `az aks update -g <rg> -n <cluster> --enable-oidc-issuer --enable-workload-identity`.
2. Create a managed identity and a federated credential bound to the
   `temporal-azure` ServiceAccount (for cluster pods and schema Jobs) and another
   bound to the `temporal-operator-controller-manager` ServiceAccount in
   `temporal-system` (for the operator's probe). These can be the same identity or
   separate ones; each needs a federated credential and a Postgres role.
3. Enable Entra auth on the Flexible Server and map each identity to a Postgres
   role with `pgaadauth_create_principal`.
4. Install the operator with Workload Identity enabled:
   `helm install temporal-operator oci://ghcr.io/bmorton/charts/temporal-operator --set workloadIdentity.enable=true --set workloadIdentity.clientId=<client-id>`.

Full passwordless support (operator probe + schema Job + server pods) is available
as of this release. Issue
[#47](https://github.com/bmorton/temporal-operator/issues/47) tracked this work.

Example:
[`examples/cluster-azure-workload-identity`](https://github.com/bmorton/temporal-operator/tree/main/examples/cluster-azure-workload-identity).
