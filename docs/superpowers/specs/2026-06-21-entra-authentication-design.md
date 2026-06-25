# Microsoft Entra authentication for Temporal — design

Date: 2026-06-21
Status: Approved (not yet implemented), branch `feat/entra-authentication`

## Context

PR #85 gave the operator passwordless **persistence** auth (the cluster's
Temporal pods, schema Jobs, and operator inspector Jobs all reach Azure Database
for PostgreSQL with short-lived Microsoft Entra tokens, no static passwords). It
did **not** touch how *clients* and *humans* authenticate **to Temporal itself**.

Temporal supports JWT-based authentication and authorization at the frontend:

- A **JWT key provider** validates inbound bearer tokens against a JWKS endpoint.
- A **claim mapper** turns token claims into Temporal roles; the stock **default**
  claim mapper reads a configurable permissions claim whose values are
  `<namespace>:<role>` (e.g. `default:write`, `temporal-system:admin`).
- An **authorizer** (stock **default**) enforces those roles per request.

Two surfaces need wiring so Microsoft Entra can be the identity provider:

1. **Service-to-service / client → frontend gRPC**: validate Entra-issued JWTs
   and (optionally) enforce per-namespace roles.
2. **temporal-ui → human login**: OIDC sign-in against Entra.

The relevant config already exists in the API but is **dead**:

- `AuthorizationSpec.Config` (`*runtime.RawExtension`) is defined but **never
  rendered** into the generated server config
  (`internal/temporal/configtemplate.go`, `templates/config_template.yaml` only
  emit `authorizer` + `claimMapper`).
- `UISpec.Auth` (`*runtime.RawExtension`) is defined but **never consumed** by the
  UI deployment (`internal/resources/ui.go`).

This design wires both surfaces with typed, Entra-aware ergonomics that mirror
the single-field `spec.persistence.azureWorkloadIdentity` pattern from PR #85,
keeps passthrough escape hatches, and ships docs plus layered tests including a
documented longstanding Entra app registration.

We use **stock** `temporalio/server` and `temporalio/ui` images throughout — no
custom claim mapper plugin (that would require building a custom server image).

## Goals

- A `TemporalCluster` can validate Entra-issued JWTs on the frontend with a
  single high-level field (`spec.authorization.entra.tenantID`), and optionally
  enforce per-namespace roles via the stock default authorizer/claim mapper.
- The bundled UI can require Entra OIDC login with a single high-level field
  (`spec.ui.auth.entra.tenantID`) plus a `clientID` and a **secret reference**
  for the client secret (never inlined in the CR).
- Both surfaces keep a raw passthrough for any knob we don't model.
- CI-runnable proof that the operator's authz wiring is enforced, **without** a
  cloud dependency, plus golden/unit coverage of the exact rendered config.
- A reproducible **longstanding Entra app registration** (script + docs) for the
  real, manual Entra integration that cannot run unattended in CI.
- A discoverable authentication guide documenting both authenticate-only and
  full-RBAC setups, UI OIDC, and security caveats.

## Non-goals

- Custom claim mappers / custom server images.
- Automated, interactive UI OIDC login in CI (verified manually via checklist).
- mTLS changes (orthogonal; already supported).
- Per-store or per-namespace *identity* changes to persistence auth (PR #85 scope).

## Design

### 1. API changes (`api/v1alpha1`)

#### 1a. Server authorization — extend `AuthorizationSpec` (`shared_types.go`)

```go
type AuthorizationSpec struct {
    // Authorizer defaults to "default" when JWT/Entra is configured.
    Authorizer  string `json:"authorizer,omitempty"`
    // ClaimMapper defaults to "default" when JWT/Entra is configured.
    ClaimMapper string `json:"claimMapper,omitempty"`

    // PermissionsClaimName maps to global.authorization.permissionsClaimName.
    // Defaults to "roles" when Entra is set (Entra app roles land in the
    // "roles" claim), otherwise "permissions".
    // +optional
    PermissionsClaimName string `json:"permissionsClaimName,omitempty"`

    // JWTKeyProvider configures JWKS-based token validation.
    // +optional
    JWTKeyProvider *JWTKeyProviderSpec `json:"jwtKeyProvider,omitempty"`

    // Entra is a convenience that derives the Entra JWKS keySourceURI from a
    // tenant ID and sets sensible JWT defaults.
    // +optional
    Entra *EntraAuthSpec `json:"entra,omitempty"`

    // Config is a passthrough merged into the authorization block for any knob
    // not modeled above. Now RENDERED (previously dead).
    // +kubebuilder:pruning:PreserveUnknownFields
    // +optional
    Config *runtime.RawExtension `json:"config,omitempty"`
}

type JWTKeyProviderSpec struct {
    // KeySourceURIs are JWKS endpoints used to validate token signatures.
    // +optional
    KeySourceURIs []string `json:"keySourceURIs,omitempty"`
    // RefreshInterval controls how often keys are refreshed (e.g. "1m").
    // +optional
    RefreshInterval *metav1.Duration `json:"refreshInterval,omitempty"`
}

type EntraAuthSpec struct {
    // TenantID is the Entra (Azure AD) tenant. Derives the JWKS keySourceURI
    // https://login.microsoftonline.com/{tenantID}/discovery/v2.0/keys.
    // +kubebuilder:validation:MinLength=1
    TenantID string `json:"tenantID"`
}
```

Resolution (`buildAuth`):

- If `Entra != nil`: append the derived JWKS URI to the effective
  `keySourceURIs`, default `permissionsClaimName` to `roles`, and default
  `authorizer`/`claimMapper` to `default`.
- Else if `JWTKeyProvider != nil`: default `permissionsClaimName` to
  `permissions`, default `authorizer`/`claimMapper` to `default`.
- Explicit fields always win over derived defaults.
- The `Config` passthrough is merged last so power users can set anything.

**Authenticate-only vs full RBAC** fall out of the same config (decision C:
expose the knobs, document both modes):

- **Full RBAC:** `authorizer: "default"` + `claimMapper: "default"`. Tokens are
  validated against the JWKS, and the default authorizer enforces the
  `<namespace>:<role>` values from the permissions claim. A token with no
  matching role is denied.
- **Authenticate-only:** still `claimMapper: "default"` (so invalid-signature or
  expired tokens are rejected — authentication), but `authorizer: ""` (Temporal's
  no-op authorizer, which allows any successfully-authenticated caller). This is
  validated during implementation against the target Temporal version, since the
  exact no-op authorizer key must be confirmed before documenting it.

#### 1b. UI OIDC — replace dead `UISpec.Auth` with typed `*UIAuthSpec`

```go
// UISpec.Auth changes type from *runtime.RawExtension to *UIAuthSpec.
type UIAuthSpec struct {
    // +kubebuilder:default=false
    Enabled bool `json:"enabled"`

    // Entra derives ProviderURL from a tenant ID.
    // +optional
    Entra *EntraUIAuthSpec `json:"entra,omitempty"`

    // ProviderURL is the OIDC issuer URL (set directly or via Entra).
    // +optional
    ProviderURL string `json:"providerURL,omitempty"`

    // +optional
    ClientID string `json:"clientID,omitempty"`

    // ClientSecretRef references an existing Secret key holding the OIDC client
    // secret. Injected via secretKeyRef env — never inlined in the CR.
    // +optional
    ClientSecretRef *SecretKeyReference `json:"clientSecretRef,omitempty"`

    // Scopes default to ["openid", "profile", "email"].
    // +optional
    Scopes []string `json:"scopes,omitempty"`

    // +optional
    CallbackURL string `json:"callbackURL,omitempty"`

    // ExtraEnv is a passthrough for any other temporal-ui auth env knob.
    // +kubebuilder:pruning:PreserveUnknownFields
    // +optional
    ExtraEnv *runtime.RawExtension `json:"extraEnv,omitempty"`
}

type EntraUIAuthSpec struct {
    // +kubebuilder:validation:MinLength=1
    TenantID string `json:"tenantID"`
}

// SecretKeyReference: reuse an existing repo type if present, else define
// { Name string; Key string }.
```

Changing `UISpec.Auth`'s type is a **breaking** schema change, but the field is
currently dead (never rendered), the project is pre-1.0, and the change is
documented in the changelog (a `feat!:`/`BREAKING CHANGE:` footer, which is a
**minor** bump pre-1.0).

### 2. Server config rendering (`internal/temporal`)

- Extend `AuthConfig` with `KeySourceURIs []string`, `RefreshInterval string`,
  `PermissionsClaimName string`, and a rendered passthrough.
- `buildAuth(cluster)` implements the resolution above (Entra URI derivation,
  defaults). Return non-nil whenever any of authorizer/claimMapper/jwtKeyProvider/
  entra/config is set.
- `config_template.yaml` `authorization:` block emits:

```yaml
authorization:
    authorizer: "default"
    claimMapper: "default"
    permissionsClaimName: "roles"
    jwtKeyProvider:
        keySourceURIs:
            - https://login.microsoftonline.com/<tenant>/discovery/v2.0/keys
        refreshInterval: "1m"
    # + merged passthrough
```

### 3. UI rendering (`internal/resources/ui.go`)

Extend `uiEnv()` (and add a volume/secret-free path) to emit, when
`ui.auth.enabled`:

- `TEMPORAL_AUTH_ENABLED=true`
- `TEMPORAL_AUTH_TYPE=oidc`
- `TEMPORAL_AUTH_PROVIDER_URL=<providerURL | derived from Entra tenant>`
- `TEMPORAL_AUTH_CLIENT_ID=<clientID>`
- `TEMPORAL_AUTH_CLIENT_SECRET` via `valueFrom.secretKeyRef` (from
  `clientSecretRef`)
- `TEMPORAL_AUTH_SCOPES=<comma-joined scopes>`
- `TEMPORAL_AUTH_CALLBACK_URL=<callbackURL>`
- plus any `extraEnv` passthrough entries.

Entra `providerURL` derives to
`https://login.microsoftonline.com/{tenantID}/v2.0`.

### 4. Webhook validation (`internal/webhook/v1alpha1`)

- When `ui.auth.enabled`: require `clientID`, `clientSecretRef`, and
  `callbackURL`; require exactly one of `entra.tenantID` or `providerURL`.
- When `authorization.entra` set: validate non-empty `tenantID`.
- These are validation-only; defaults applied in `buildAuth`/`uiEnv`.

### 5. Example (`examples/cluster-entra-auth/`)

- `temporalcluster.yaml`: `authorization.entra.tenantID` + `ui.auth.entra` with a
  `clientSecretRef` to a Secret.
- `onepassworditem.yaml`: a `OnePasswordItem` providing the UI client secret
  (the maintainer's 1Password convention).
- `README.md`: prerequisites (app registration), apply steps, verification.

### 6. Tests

**Golden / unit:**

- `configtemplate_test.go`: authorization block for (a) Entra shortcut, (b)
  explicit `jwtKeyProvider`, (c) passthrough merge, (d) absent (unchanged).
- `ui` env test: asserts `TEMPORAL_AUTH_*` env incl. `secretKeyRef`.
- webhook validation tests for the rules in §4.
- `zz_generated.deepcopy.go` regenerated for the new types.

**Hermetic server-JWT e2e** (`test/e2e/entra-auth/`, Chainsaw on kind):

- A **static JWKS** served by an `nginx` Deployment from a ConfigMap, built from
  an **offline RSA keypair** committed as test fixtures.
- Pre-baked JWTs as fixtures: one **valid** (correct signature, `roles` claim
  `default:read` / `temporal-system:admin`, far-future `exp`) and one
  **invalid** (wrong-signature). Baking tokens offline avoids running a minter
  in-cluster and keeps the suite deterministic.
- A `TemporalCluster` whose `authorization.jwtKeyProvider.keySourceURIs` points
  at the in-cluster JWKS Service, `authorizer`/`claimMapper` `default`.
- Assertions via a `temporal` CLI pod using
  `--grpc-meta authorization="Bearer <jwt>"`:
  - valid token → an authorized operation **succeeds**;
  - missing / invalid token → operation is **rejected** (PermissionDenied /
    Unauthenticated).
- Wired into `.chainsaw.yaml` / `make chainsaw-test` like the other suites; runs
  on kind, **no Azure dependency**.

### 7. Real Entra path — longstanding app (`hack/entra-e2e.sh`)

A script (mirroring `hack/azure-e2e.sh` subcommand style) that **creates and
manages a longstanding Entra app registration** used for real, manual
verification:

- `up`: create the app registration + service principal; define **app roles**
  with values `default:read`, `default:write`, `temporal-system:admin`; create a
  client secret; print `tenantID`, `clientID`, and secret.
- `token`: acquire a token via client-credentials for the gRPC test.
- `down`: delete the app registration.
- `make entra-e2e-up` / `-token` / `-down` targets.

The app's **purpose, ownership, and recreation** are documented (it is shared and
long-lived because app registrations are tenant-scoped and rate-limited to
create). UI OIDC is verified **manually** via a documented checklist (interactive
browser login).

### 8. Docs & generated artifacts

- New `docs/content/operations/authentication.md`:
  "Authentication & Authorization with Microsoft Entra" — concepts (authn vs
  authz, claim mapper, JWKS), server JWT authenticate-only **and** full RBAC via
  Entra **app roles** (`<namespace>:<role>` values, `roles` claim), UI OIDC
  setup, the longstanding-app setup, and security caveats (audience/issuer
  validation, token lifetime, role-value format).
- Cross-link from `docs/content/installation/azure.md`.
- Regenerate and commit: `make generate manifests api-docs docs-crd-reference`
  (updates `config/crd/...`, `docs/api/v1alpha1.md`,
  `docs/content/reference/_index.md`).
- Propagate CRD schema changes into **`dist/chart/templates/crd/`** (kind/CI e2e
  installs via Helm from `dist/chart`); hand-edit `dist/chart` (do **not** run
  `make helm-chart`).

## Risks & caveats

- **Entra claim shape:** app roles emit the **`roles`** claim, and Temporal's
  default mapper expects `<namespace>:<role>` values — both are easy to
  misconfigure. The default (`permissionsClaimName: roles`) plus explicit docs
  mitigate this.
- **Audience/issuer validation:** Temporal's default JWT key provider validates
  signature + expiry; audience/issuer expectations are documented so tokens
  aren't accepted too broadly.
- **Breaking `UISpec.Auth` type change:** dead field, pre-1.0, flagged via
  conventional-commit breaking footer and changelog.
- **CI determinism:** offline keypair + pre-baked far-future tokens avoid a live
  minter and clock flakiness.

## Validation

`make generate manifests` (no drift), `make build`, `make test`, `make lint`,
the hermetic `entra-auth` Chainsaw suite on kind, and a documented manual real
Entra run (`hack/entra-e2e.sh` + UI OIDC checklist).

## Conventions

DCO sign-off on every commit, Conventional Commits (`feat!:` for the breaking UI
field), feature branch + PR (not main), and `make build/test/lint` before
opening the PR.
