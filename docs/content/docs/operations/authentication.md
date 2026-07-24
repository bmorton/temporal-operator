+++
title = "Authentication & Authorization"
weight = 10
aliases = ["/operations/authentication/"]
+++

This guide covers configuring Microsoft Entra (Azure AD) authentication and
authorization for temporal-operator — both **server-side JWT validation** for
programmatic callers and **UI OIDC login** for human users.

## Concepts

### Authentication vs. authorization

- **Authentication** — verifying that an incoming JWT was issued by a trusted
  identity provider (Microsoft Entra) and has a valid signature. Temporal
  performs this by fetching public keys from a JWKS endpoint and validating
  the token's signature, issuer, and audience.
- **Authorization** — deciding whether an authenticated caller may perform a
  specific operation on a specific Temporal namespace. Temporal's built-in
  `default` authorizer reads a permissions claim from the JWT and maps it to
  per-namespace roles.

### JWKS / JWT key provider

Temporal's frontend service fetches public keys from one or more JWKS
(`keySourceURIs`) to validate incoming token signatures. The operator
configures this through `spec.authorization.jwtKeyProvider`. When you set
`spec.authorization.entra.tenantID`, the operator automatically derives the
correct JWKS URL:

```text
https://login.microsoftonline.com/<tenantID>/discovery/v2.0/keys
```

Keys are refreshed periodically; set `spec.authorization.jwtKeyProvider.refreshInterval`
(e.g. `"1m"`) to control the interval. If omitted, Temporal's server default applies.

### Default claim mapper and the `<namespace>:<role>` format

Temporal's default JWT claim mapper reads a named claim from the JWT payload
and interprets each entry as `<namespace>:<role>`. Valid roles are:

| Role value | Access |
|---|---|
| `read` | Read-only namespace access |
| `write` | Workflow submit / signal / cancel |
| `worker` | Worker poll |
| `admin` | Namespace admin operations |

A token with `["default:write", "temporal-system:admin"]` grants write access
to the `default` namespace and admin access to `temporal-system`.

When `spec.authorization.entra` is set, the operator sets
`permissionsClaimName` to `roles` automatically, matching where Microsoft
Entra places app-role assignments in the token.

### Stock images — no custom claim mapper required

temporal-operator uses the stock `temporalio/server` and `temporalio/ui`
images. No custom claim mapper or authorizer binary is needed; the operator
wires Temporal's built-in JWT claim mapper and `default` authorizer via
server configuration.

---

## Server JWT — authenticate-only

In this mode Temporal validates that every incoming gRPC call carries a
properly signed Entra JWT, but does **not** enforce per-namespace role checks.
Any caller with a valid token is allowed.

This is controlled by `spec.authorization.authorizer`. The CRD documents:

> `Authorizer` defaults to `"default"` when JWT validation is configured.
> Set `authorizer: ""` for the no-op (allow-all) authorizer.

### Minimal example (Entra shortcut)

```yaml
spec:
  authorization:
    entra:
      tenantID: "00000000-0000-0000-0000-000000000000"
    authorizer: ""   # allow any authenticated caller; no per-namespace RBAC
```

### Alternative — explicit JWKS URI

```yaml
spec:
  authorization:
    jwtKeyProvider:
      keySourceURIs:
        - "https://login.microsoftonline.com/<tenantID>/discovery/v2.0/keys"
      refreshInterval: "1m"
    authorizer: ""
```

### Attaching a Bearer token (gRPC metadata)

Temporal clients must pass the token in the `authorization` gRPC header:

```text
authorization: Bearer <access_token>
```

**Go SDK example:**

```go
import (
    "context"

    "go.temporal.io/sdk/client"
)

// authHeadersProvider attaches a bearer token to every gRPC call.
type authHeadersProvider struct{ token string }

func (a authHeadersProvider) GetHeaders(context.Context) (map[string]string, error) {
    return map[string]string{"authorization": "Bearer " + a.token}, nil
}

c, err := client.Dial(client.Options{
    HostPort:        "temporal.example.com:7233",
    HeadersProvider: authHeadersProvider{token: token},
})
```

**tctl / temporal CLI:**

```sh
temporal workflow list \
  --address temporal.example.com:7233 \
  --tls \
  --grpc-meta "authorization=Bearer $(make entra-e2e-token)"
```

---

## Server JWT — full RBAC via Entra app roles

In this mode Temporal validates JWTs **and** enforces the `<namespace>:<role>`
permissions from the `roles` claim. The `default` authorizer rejects calls
where the caller's token does not include the required role for the target
namespace.

### 1. Define app roles in Entra

In the Entra app registration, define **App Roles** whose `value` fields are
exactly the `<namespace>:<role>` strings Temporal expects:

| Display name | Value | Access |
|---|---|---|
| Temporal default namespace read | `default:read` | Read-only |
| Temporal default namespace write | `default:write` | Workflow submit |
| Temporal system namespace admin | `temporal-system:admin` | Namespace admin |

{{< callout type="important" >}}
**Important:** The `value` field is what ends up in the JWT's `roles` claim.
Any mismatch in namespace name or role name (e.g. `Default:Write` instead of
`default:write`) will result in authorization denials.
{{< /callout >}}

### 2. Assign app roles to callers

- **Service-to-service / daemon apps:** add an app role assignment on the
  calling app's service principal in Entra (Entra portal → Enterprise
  Applications → select the client app → App role assignments).
- **Human users:** add role assignments via Entra groups or direct user
  assignment (Entra portal → the API app → App roles → Assign users and
  groups).

### 3. Configure the operator

```yaml
spec:
  authorization:
    entra:
      tenantID: "00000000-0000-0000-0000-000000000000"
    # authorizer defaults to "default" when entra is set — no need to set it.
    # permissionsClaimName defaults to "roles" when entra is set.
```

Omitting `authorizer` when `entra` is set means it defaults to `"default"`,
which enforces per-namespace role checks. Setting `permissionsClaimName:
roles` explicitly is also valid but not required.

### 4. Provision the test app registration

Use `hack/entra-e2e.sh` (or its `make` wrappers) to create a longstanding
app registration with the three canonical app roles:

```sh
make entra-e2e-up     # create registration, roles, service principal, secret
make entra-e2e-token  # mint a client-credentials access token
make entra-e2e-down   # delete the registration when done
```

`entra-e2e-up` writes credentials to `hack/.entra-e2e.env`. The token
subcommand performs an OAuth 2.0 client-credentials grant and prints the
resulting JWT. Because the calling service principal is assigned all three
roles, the token's `roles` claim will contain
`["default:read", "default:write", "temporal-system:admin"]`.

---

## UI OIDC login

Configure `spec.ui.auth` to enable human login via the Temporal Web UI using
Entra as the OIDC provider:

```yaml
spec:
  ui:
    enabled: true
    auth:
      enabled: true
      entra:
        tenantID: "00000000-0000-0000-0000-000000000000"
      clientID: "11111111-1111-1111-1111-111111111111"
      clientSecretRef:
        name: temporal-ui-oidc
        key: client-secret
      callbackURL: https://temporal.example.com/auth/sso/callback
      # scopes defaults to ["openid", "profile", "email"]
```

### Field reference

| Field | Description |
|---|---|
| `auth.enabled` | Enable OIDC authentication on the UI |
| `auth.entra.tenantID` | Derives `providerURL` as `https://login.microsoftonline.com/<tenantID>/v2.0` |
| `auth.clientID` | The OIDC client ID of the Entra app registration for the UI |
| `auth.clientSecretRef.name` | Name of the Kubernetes `Secret` holding the client secret |
| `auth.clientSecretRef.key` | Key within the Secret (default: `password`) |
| `auth.callbackURL` | Must **exactly** match a redirect URI registered in the Entra app |
| `auth.scopes` | Override OIDC scopes (default: `["openid", "profile", "email"]`) |
| `auth.extraEnv` | Passthrough map of additional `temporal-ui` auth environment variables |

### The `callbackURL` must match Entra's registered redirect URI

In the Entra app registration for the UI, add the callback URL under
**Authentication → Redirect URIs**. A mismatch (including trailing slashes or
HTTP vs HTTPS differences) causes a `redirect_uri_mismatch` error at login.

### Providing the client secret

The operator reads the OIDC client secret from a Kubernetes `Secret` at
runtime. **Never inline the secret value in the `TemporalCluster` manifest.**

**Manual secret creation:**

```sh
kubectl create secret generic temporal-ui-oidc \
  --from-literal=client-secret=<value>
```

**1Password Kubernetes Operator pattern** (as used in
`examples/cluster-entra-auth/`):

The 1Password operator watches `OnePasswordItem` resources and syncs them to
native Kubernetes `Secret` objects automatically. The synced Secret name
matches the `OnePasswordItem`'s `metadata.name`, so it aligns with
`clientSecretRef.name` without any extra wiring:

```yaml
apiVersion: onepassword.com/v1
kind: OnePasswordItem
metadata:
  name: temporal-ui-oidc
spec:
  itemPath: "vaults/Kubernetes/items/temporal-ui-oidc"
```

The 1Password item must expose the client secret under a field named
`client-secret` to match `clientSecretRef.key: client-secret`.

---

## The longstanding Entra app registration

### Purpose

Entra app registrations are tenant-scoped and shared across environments. A
single long-lived app registration is used for all Entra JWT integration tests
rather than creating and deleting one per test run. This avoids Entra
propagation delays and keeps the registration stable enough for repeated
testing.

### Lifecycle

```sh
# Provision once (writes hack/.entra-e2e.env)
make entra-e2e-up

# Mint a token for manual gRPC testing
make entra-e2e-token

# Tear down when no longer needed
make entra-e2e-down
```

Running `entra-e2e-up` a second time without first running `entra-e2e-down`
is an error — the script guards against overwriting an existing registration.

### Ownership and re-creation

`hack/.entra-e2e.env` stores the tenant ID, client ID, and client secret. If
the file is lost or the registration is deleted from the Entra portal, run
`make entra-e2e-up` again. The `APP_NAME` (default: `temporal-operator-e2e`)
is configurable via the `ENTRA_APP_NAME` environment variable.

### Manual UI OIDC verification checklist

Automated CI cannot complete an interactive browser login. Use this checklist
to verify the end-to-end UI flow manually:

1. Deploy a `TemporalCluster` with `spec.ui.auth` configured (see example
   below).
2. Open the UI URL (e.g. `https://temporal.example.com`) in a browser.
3. Confirm you are redirected to the Entra login page.
4. Sign in with a user or service account that has been assigned one or more
   of the Temporal app roles.
5. After successful login, confirm you land on the Temporal UI namespace list.
6. Attempt a role-gated operation (e.g. triggering a workflow in the
   `default` namespace) and verify it succeeds with a `default:write`-assigned
   user but fails (403) with a `default:read`-only user.

---

## Security caveats

### Audience and issuer validation

Temporal's JWT key provider validates the token **signature** using the JWKS
endpoint. Audience (`aud`) and issuer (`iss`) validation follow Temporal's
standard JWT handling. For Entra-issued client-credentials tokens, the
audience is `api://<appId>` and the issuer is
`https://login.microsoftonline.com/<tenantID>/v2.0`. Ensure these match your
Entra app registration configuration. Mismatches result in
`Unauthorized` errors even with a valid signature.

### Token lifetime

Entra access tokens typically expire after 1 hour (configurable in Entra).
Clients must refresh tokens before expiry. gRPC clients should implement a
token supplier that re-acquires tokens transparently (for example via the
OAuth 2.0 client-credentials grant on each call, or by caching with early
refresh).

### Role value format pitfall

The `roles` claim value must be an exact `<namespace>:<role>` string.
Common mistakes:

- Capitalisation: `Default:Write` is **not** the same as `default:write`.
- Extra spaces: `default: write` will not match.
- Wrong role name: `viewer` instead of `read`.

When creating app roles in Entra, set the **Value** field exactly (copy-paste
from the table above).

### Never inline secrets

The OIDC client secret must always be provided via `clientSecretRef`
referencing a Kubernetes `Secret`. Do not place the raw secret value anywhere
in the `TemporalCluster` manifest or in version control.

---

## Complete example

See
[`examples/cluster-entra-auth/`](https://github.com/bmorton/temporal-operator/tree/main/examples/cluster-entra-auth)
for a full `TemporalCluster` manifest combining server JWT RBAC and UI OIDC
login, along with a `OnePasswordItem` for secret management.
