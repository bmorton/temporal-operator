# Microsoft Entra Authentication

Demonstrates a `TemporalCluster` configured for Microsoft Entra (Azure AD)
authentication on two layers:

| Layer | Mechanism |
|---|---|
| **Service-to-service** | Entra-issued JWTs validated server-side; `roles` claim enforces per-namespace access via Temporal's authorizer |
| **Temporal UI** | Human login via Entra OIDC; client secret synced from 1Password |

## Prerequisites

- An **Entra app registration** for the Temporal server with:
  - **App Roles** defined (e.g. `temporal-namespace-admin`,
    `temporal-namespace-writer`, `temporal-namespace-reader`).
  - The tenant ID noted as `<TENANT_ID>`.
- A **second Entra app registration** (or the same one) for the Temporal UI
  OIDC flow with:
  - A client secret created and stored in 1Password under vault `Kubernetes`,
    item `temporal-ui-oidc`, field name **`client-secret`**.
  - A redirect URI pointing to `https://temporal.example.com/auth/sso/callback`.
  - The client ID noted as `<CLIENT_ID>`.
- The [1Password Kubernetes Operator](https://developer.1password.com/docs/k8s/k8s-operator/)
  installed in the cluster so that `OnePasswordItem` resources sync secrets
  automatically.
- A running PostgreSQL instance accessible as `temporal-pg-rw:5432`, with a
  `temporal-pg-app` secret containing a `password` key.

## Customise

Replace the placeholder values before applying:

| Placeholder | Where |
|---|---|
| `00000000-0000-0000-0000-000000000000` | `spec.authorization.entra.tenantID` and `spec.ui.auth.entra.tenantID` |
| `11111111-1111-1111-1111-111111111111` | `spec.ui.auth.clientID` |
| `temporal.example.com` | `spec.ui.ingress.host` and `spec.ui.auth.callbackURL` |
| `vaults/Kubernetes/items/temporal-ui-oidc` | `onepassworditem.yaml` `spec.itemPath` (vault / item name in 1Password) |

## Secret linkage

The `OnePasswordItem` syncs to a Kubernetes `Secret` whose name matches the
`OnePasswordItem`'s `metadata.name` (`temporal-ui-oidc`). The 1Password item
must expose the client secret under the field name **`client-secret`**; the
operator reads it from the synced secret at key `client-secret`
(`spec.ui.auth.clientSecretRef.key`).

## Apply

```sh
# Sync the UI OIDC client secret from 1Password
kubectl apply -f onepassworditem.yaml

# Deploy the cluster
kubectl apply -f temporalcluster.yaml
```

The operator configures the Temporal server's JWT authorizer and wires the
Temporal UI's OIDC provider automatically.

## Further reading

- `docs/content/operations/authentication.md` — full authentication reference
