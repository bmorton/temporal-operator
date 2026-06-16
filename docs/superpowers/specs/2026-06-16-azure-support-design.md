# Azure-friendly operator (phase 1) — design

Date: 2026-06-16
Status: Approved

## Problem

The operator works on Azure today in principle, but there is no Azure-flavored
on-ramp: no examples for Azure Database for PostgreSQL Flexible Server, no AKS
ingress guidance, and no path toward passwordless Microsoft Entra
authentication. Azure users have to reverse-engineer the generic Postgres/mTLS
examples and discover Azure specifics (TLS requirement, FQDN host, Entra
tokens, Workload Identity) on their own.

A deeper gap blocks the most compelling Azure story — **passwordless** database
auth via Azure Workload Identity → Entra → Flexible Server:

- The API defines `PodTemplateOverride` (labels, annotations, strategic-merge
  pod `spec`) on `ServiceSpec` and the shared `ServiceOverrides`
  (`api/v1alpha1/temporalcluster_types.go`), but `BuildDeployment`
  (`internal/resources/deployment.go`) **never applies it**. There is currently
  no way to set `serviceAccountName`, add the `azure.workload.identity/use:
  "true"` pod label, or inject a token-refresher sidecar.

During design we confirmed that **three** actors connect to Postgres, and only
one is passwordless-ready:

| Actor | Auth today | Passwordless-ready |
| --- | --- | --- |
| Server pods | Temporal `passwordCommand` (re-run per connection) | Yes — sidecar + `cat token` works |
| Schema Job (`setup-schema`) | static `SQL_PASSWORD` env from a Secret | No `passwordCommand` support; pod has no `podTemplate` field |
| Operator (reachability probe + schema inspection) | builds DSN from `cred.Password` only (`internal/persistence/sql.go`) | Never executes `passwordCommand` → empty password |

Wiring `podTemplate` into the server Deployments alone makes the **server** pods
passwordless, but the operator's own probe and the schema Job would still fail,
so the cluster would never reach Ready (`PersistenceUnreachable`). Full
passwordless therefore requires schema-Job and operator changes that are out of
scope for phase 1.

## Goals

- Wire up the existing `podTemplate` override so users can set
  `serviceAccountName`, pod labels/annotations, sidecars, and volumes on the
  Temporal service Deployments.
- Ship Azure-specific examples and documentation so Azure users have a clear,
  copy-pasteable on-ramp (Flexible Server password auth over TLS; AGIC ingress).
- Provide a **preview** Workload Identity passwordless example that demonstrates
  the pod-level wiring, with honest notes about the remaining limitations.
- Track the remaining passwordless work (schema Job + operator) in a GitHub
  issue linked from the example and docs.

## Non-goals (YAGNI)

- First-class Entra / Workload Identity fields in the CRD.
- Temporal UI OIDC / Entra SSO.
- Schema-Job and operator token auth (deferred to the follow-up issue).
- Changes to the UI Deployment or the schema Job in phase 1.

## Design

### 1. Wire up `podTemplate` overrides (Go)

Apply `PodTemplateOverride` in `internal/resources/deployment.go` when building
each service Deployment's pod template:

- **Labels / annotations:** merge onto the pod template `ObjectMeta`. On key
  conflict the **override wins**, so a user can set
  `azure.workload.identity/use: "true"` and other operational labels. The
  operator's own required labels (selector labels) must remain intact — apply
  overrides as an additive merge and never let an override drop a selector
  label (selector labels are re-asserted after the merge).
- **`spec` (RawExtension):** strategic-merge patch the partial `PodSpec` onto
  the generated `corev1.PodSpec` using
  `k8s.io/apimachinery/pkg/util/strategicpatch` (already in the module cache,
  apimachinery v0.34.x). Marshal the generated `PodSpec` to JSON, take the
  override's raw JSON as the patch, call `StrategicMergePatch` with
  `corev1.PodSpec{}` as the data struct, unmarshal back. Strategic merge keys
  mean containers merge by `name` (so a sidecar with a new name is appended and
  the `temporal` container can be patched by name) and volumes merge by `name`.
- **Precedence:** apply the shared `Overrides.PodTemplate` first, then the
  per-service `ServiceSpec.PodTemplate` on top (per-service wins).
- **Empty / nil overrides:** when no `podTemplate` is set, behavior is
  byte-for-byte unchanged from today.

Implementation detail: the controller resolves user `ServiceSpec` into the
internal service config passed to `BuildDeployment`; the resolved struct must
carry the merged `PodTemplateOverride` (shared + per-service) so the resources
package can apply it. Keep the strategic-merge helper small and unit-testable
(e.g. `applyPodTemplate(base corev1.PodTemplateSpec, tmpl *PodTemplateOverride)
(corev1.PodTemplateSpec, error)`).

**Tests** (`internal/resources/deployment_test.go`):
- Labels and annotations from `podTemplate` appear on the pod template; selector
  labels are preserved.
- `podTemplate.spec` sets `serviceAccountName`.
- `podTemplate.spec` appends a sidecar container and a shared `emptyDir` volume
  while preserving the generated `temporal` container and its volumes.
- Shared `Overrides.PodTemplate` is applied and overridden by per-service
  `podTemplate`.
- Nil `podTemplate` leaves the Deployment unchanged.

### 2. Examples

Three new directories under `examples/`, each with a short `README.md`.

**`cluster-azure-postgres-flexible`** — Azure Database for PostgreSQL Flexible
Server with **password auth** over TLS (works today, yields a healthy cluster):
- `persistence.*.sql.host: <server>.postgres.database.azure.com`, `port: 5432`,
  `tls.enabled: true`, `passwordSecretRef`.
- README notes: pre-create the `temporal` and `temporal_visibility` databases
  (the operator only runs `setup-schema`), raise `max_connections` (~200) to
  avoid pool exhaustion, and configure firewall / VNet / private access.

**`cluster-azure-aks-ingress`** — Temporal UI exposed via the Application
Gateway Ingress Controller (AGIC):
- `ui.ingress.enabled: true`, `ingressClassName: azure-application-gateway`,
  plus representative `appgw.ingress.kubernetes.io/*` annotations and `host`.

**`cluster-azure-workload-identity`** *(preview)* — Flexible Server passwordless
via Azure Workload Identity, exercising the new `podTemplate` wiring:
- A `ServiceAccount` annotated `azure.workload.identity/client-id: <client-id>`.
- `overrides.podTemplate.labels: { azure.workload.identity/use: "true" }`.
- `overrides.podTemplate.spec` sets `serviceAccountName` and adds an
  `mcr.microsoft.com/azure-cli` token-refresher sidecar that performs a
  Workload Identity federated `az login` and loops
  `az account get-access-token --resource-type oss-rdbms` into a shared
  `emptyDir` file (`/azure/pgpass`) on a refresh margin (< token lifetime).
- `persistence.*.sql.passwordCommandSecretRef` → a Secret holding
  `cat /azure/pgpass`; `user:` is the Entra role created in Postgres via
  `pgaadauth_create_principal`; `tls.enabled: true`.
- **Prominent note** in the README: this makes the **server pods** passwordless,
  but the operator's reachability probe and the schema Job are not yet
  passwordless (link the follow-up issue). Documents the bootstrap path:
  provision the schema once with password auth (or a temporary Entra admin
  token), then run the cluster passwordless; or grant the operator/job identity
  appropriate access until the follow-up lands.

### 3. Documentation

**`docs/content/installation/azure.md`** (Hugo page, consistent with the
existing `docs/content/installation/` structure):
- AKS prerequisites and installing the operator.
- Provisioning Flexible Server: creating the two databases, TLS requirement,
  `max_connections`, firewall / private networking.
- Password-auth quickstart linking `cluster-azure-postgres-flexible`.
- Exposing the UI via AGIC linking `cluster-azure-aks-ingress`.
- Microsoft Entra + Workload Identity (preview): enabling the OIDC issuer on
  AKS, creating the managed identity + federated credential with `az`, mapping
  the Entra principal to a Postgres role, and the current limitations with a
  link to the follow-up issue. Links `cluster-azure-workload-identity`.

**`examples/README.md`** — add three rows for the new examples.

### 4. Follow-up GitHub issue

Open an issue with `gh` titled
*"Full Azure Workload Identity passwordless auth (operator probe + schema Job)"*
capturing the deferred work:
- Wire `podTemplate` into the schema Job and populate `SQL_PASSWORD` from the
  `passwordCommand` / token file.
- Make the operator execute `passwordCommand` for its own reachability probe and
  schema-version inspection (operator pod runs with Workload Identity).

Link the issue from the preview example README and the Azure docs page.

## Risks and mitigations

- **Strategic-merge correctness / accidental clobbering of generated pod spec.**
  Mitigate with focused unit tests (sidecar append, volume append, scalar set)
  and by re-asserting selector labels after the merge.
- **Preview example doesn't yield a fully healthy cluster on its own.** Mitigate
  with explicit README bootstrap instructions and the linked issue; the
  password-auth example remains the "just works" path.
- **Token staleness in the sidecar.** Refresh on a margin shorter than the Entra
  token lifetime; documented in the example.
- **Pre-1.0 stability.** No CRD schema changes (the `podTemplate` fields already
  exist); behavior is unchanged when `podTemplate` is unset.

## Testing

- `make generate manifests` (no API change expected, but verify the CRD is
  unchanged), `make build`, `make test` (new `deployment_test.go` cases),
  `make lint`.
- Manual: `kubectl apply` the example YAML to confirm the rendered Deployment
  carries `serviceAccountName`, the WI label, and the sidecar (rendering only;
  full Azure validation is out of band).
