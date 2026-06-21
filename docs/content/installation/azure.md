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

Each actor in the system obtains an Entra token through a different mechanism:

| Actor | Token mechanism |
|---|---|
| **Temporal server pods** | `azure-token-refresher` sidecar writes a token to a shared `emptyDir`; Temporal reads it via `passwordCommand` (`podTemplate` override) |
| **Schema Job** | one-shot `initContainer` writes a token before the schema container starts; schema container reads it via `passwordCommand` (`schemaJob.podTemplate`) |
| **Operator** (probe + schema inspection) | obtains an Entra token **natively in-process** via the Go Azure Workload Identity SDK — no sidecar; enabled by setting `sql.azureWorkloadIdentity: {}` on each datastore |

The operator runs on a distroless image with no shell, so it cannot use
`passwordCommand`. Instead, set `sql.azureWorkloadIdentity: {}` on each store and
install the operator with Workload Identity enabled so the Go SDK picks up the
projected OIDC token automatically — no token-refresher sidecar on the operator pod.

1. Enable the OIDC issuer and Workload Identity on AKS:
   `az aks update -g <rg> -n <cluster> --enable-oidc-issuer --enable-workload-identity`.
2. Create a managed identity and a federated credential bound to the
   `temporal-azure` ServiceAccount (for cluster pods and schema Jobs) and another
   bound to the `temporal-operator-controller-manager` ServiceAccount in
   `temporal-system` (for the operator's native in-process token). These can be the
   same identity or separate ones; each needs a federated credential and a Postgres
   role.
3. Enable Entra auth on the Flexible Server and map each identity to a Postgres
   role with `pgaadauth_create_principal`.
4. Install the operator with Workload Identity enabled:
   `helm install temporal-operator oci://ghcr.io/bmorton/charts/temporal-operator --set workloadIdentity.enable=true --set workloadIdentity.clientId=<client-id>`.
   This adds the WI label and SA annotation to the operator pod; no sidecar is
   added to the operator.

Full passwordless support (operator probe + schema Job + server pods) is available
as of this release. Issue
[#47](https://github.com/bmorton/temporal-operator/issues/47) tracked this work.
The operator's own native token currently supports Azure Workload Identity only;
[#84](https://github.com/bmorton/temporal-operator/issues/84) tracks generic /
multi-provider support (e.g. AWS RDS IAM).

Example:
[`examples/cluster-azure-workload-identity`](https://github.com/bmorton/temporal-operator/tree/main/examples/cluster-azure-workload-identity).
