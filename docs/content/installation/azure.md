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

## Passwordless auth with Microsoft Entra + Workload Identity (preview)

The operator can drive the Temporal **server** pods passwordlessly using Azure
Workload Identity and Entra access tokens, via the `podTemplate` override
(`serviceAccountName`, the `azure.workload.identity/use` label, and a
token-refresher sidecar).

1. Enable the OIDC issuer and Workload Identity on AKS:
   `az aks update -g <rg> -n <cluster> --enable-oidc-issuer --enable-workload-identity`.
2. Create a managed identity and a federated credential bound to the
   `temporal-azure` ServiceAccount.
3. Enable Entra auth on the Flexible Server and map the identity to a Postgres
   role with `pgaadauth_create_principal`.

**Current limitation:** only the server pods are passwordless today. The
operator's reachability probe and the schema Job still need a password. Bootstrap
the schema with password auth first, or grant those identities temporary access.
Tracking issue:
[#47](https://github.com/bmorton/temporal-operator/issues/47).

Example:
[`examples/cluster-azure-workload-identity`](https://github.com/bmorton/temporal-operator/tree/main/examples/cluster-azure-workload-identity).
