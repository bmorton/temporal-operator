# AKS passwordless e2e + full Workload Identity auth (#47) — design

Date: 2026-06-19
Status: Approved

## Problem

The operator ships Azure examples and a *preview* `cluster-azure-workload-identity`
example that makes **server pods** authenticate to Azure Database for PostgreSQL
Flexible Server passwordlessly (Workload Identity → Microsoft Entra token →
`passwordCommand`). But two actors still require a static password, so a
passwordless cluster never reaches `Ready` (issue
[#47](https://github.com/bmorton/temporal-operator/issues/47)):

- **Operator probe + schema inspection** (`internal/persistence/sql.go`,
  `internal/controller/temporalcluster_persistence.go`) build the DSN from
  `cred.Password` only and never execute a configured `passwordCommand`, so the
  reachability probe fails with `PersistenceUnreachable`.
- **Schema Job** (`internal/resources/schemajob.go`) only reads a static
  `SQL_PASSWORD` env var and has no `podTemplate`, so it cannot acquire a token.

Separately, there is no end-to-end test that proves the operator works on a real
AKS cluster against a real Flexible Server — all current e2e suites run on
kind (CI) or ephemeral Namespace.so clusters (`hack/nsc-e2e.sh`), neither of
which exercises Azure Workload Identity, Entra auth, or Flexible Server TLS.

## Goals

- Close #47: make the operator's own DB probe/schema-inspection **and** the
  schema Job authenticate via `passwordCommand`, so a Workload-Identity cluster
  reaches `Ready` with **zero static passwords**.
- Add a real **AKS + Flexible Server + passwordless** e2e that provisions
  everything, runs a Chainsaw suite, and tears everything down — runnable today
  from a developer/coder instance and easy to move to CI later.
- Keep the kind and nsc e2e flows fully intact.

## Non-goals (YAGNI)

- A Temporal **workflow smoke test** in the e2e (condition-based assertions are
  enough for the first cut).
- A **GitHub Actions workflow** for the Azure e2e now (the harness is
  parameterized so one can be added later with an Azure OIDC login).
- Native Azure SDK token acquisition in the operator (we use the generic,
  cloud-agnostic `passwordCommand` mechanism).
- Any change to the kind/nsc/CI e2e flows beyond what is strictly additive.

## Rollout: two PRs

This effort ships as **two sequential PRs** on separate branches:

1. **PR1 — close #47** (operator probe + schema Job passwordless; reviewable with
   fast unit/envtest feedback).
2. **PR2 — AKS passwordless e2e** (slow/expensive; exercises the PR1 fix
   end-to-end). Lands after PR1 merges.

---

## PR1 — Full passwordless auth (close #47)

### 1. Operator probe & schema inspection: native Entra token

Files: `internal/persistence/sql.go`, `internal/persistence/azure.go` (new),
`internal/persistence/secrets.go`, `api/v1alpha1/persistence_types.go`.

> **Design pivot (2026-06-19).** The operator runs on `gcr.io/distroless/static:nonroot`,
> which has **no shell** (`sh`, `cat`). Executing a `passwordCommand` via
> `os/exec` (the originally-approved approach) therefore cannot work in the
> operator container. The token-refresher sidecar pattern works for the server
> and schema-Job pods (their images have shells), but **not** for the operator's
> own in-process probe. We pivoted the operator to obtain a Microsoft Entra
> access token **natively in Go** via Azure Workload Identity, with no shelling
> out. The generic `passwordCommand` model is kept for the workload pods; making
> the operator's own auth generic/multi-provider is tracked in a follow-up issue.

- New optional API field `sql.azureWorkloadIdentity` (`AzureWorkloadIdentitySpec`,
  pointer = enabled) on `SQLDatastoreSpec`, with an optional `scope` (default
  `https://ossrdbms-aad.database.windows.net/.default`). Its presence tells the
  operator to authenticate its **own** probe/schema-inspection with an Entra
  token.
- New `internal/persistence/azure.go`: a small `tokenProvider` interface
  (`Token(ctx, scope) (string, error)`) with a default implementation backed by
  `github.com/Azure/azure-sdk-for-go/sdk/azidentity` `WorkloadIdentityCredential`
  (reads the `AZURE_*` env + projected federated token that the Azure Workload
  Identity webhook injects when the operator pod carries the
  `azure.workload.identity/use: "true"` label and SA client-id annotation). The
  SDK caches/refreshes tokens internally, so calling it per probe is cheap.
- `ResolvedCredential` gains an `AzureWorkloadIdentity *AzureWorkloadIdentityCredential`
  field (carrying the resolved scope). `ResolveSQL` populates it from the spec
  field; it is **additive** to `Password`/`PasswordCommand` (the same store can
  set `passwordCommandSecretRef` for the server/schema Job *and*
  `azureWorkloadIdentity` for the operator).
- `sqlBackend.resolvePassword(ctx)`: when `cred.AzureWorkloadIdentity != nil`,
  fetch a token via the provider (bounded by its own timeout so a slow token
  fetch can't hang the reconcile worker) and use it as the DSN password; else use
  the static `Password`. The operator **does not** execute `passwordCommand`
  (distroless). The static-password path is unchanged.
- The injectable `CommandRunner`/shell-out approach (`internal/persistence/command.go`)
  is **removed** as dead code, since the operator no longer shells out and the
  schema-Job wrapper builds its own `sh -c` script string in `schemajob.go`.

**Tests** (`internal/persistence/azure_test.go`, `sql_test.go`):
- With `cred.AzureWorkloadIdentity` set, the backend uses the injected fake
  `tokenProvider`'s token as the DSN password; a fresh token is requested per
  probe; provider errors propagate as probe errors.
- Static password path produces the identical DSN it does today.

### 2. Schema Job passwordless + `podTemplate`

Files: `internal/resources/schemajob.go`,
`internal/controller/temporalcluster_persistence.go`,
`api/v1alpha1/persistence_types.go`.

- **Thread the resolved credential** into `SchemaJobParams`. The controller
  already resolves `ResolvedCredential` per store in `buildSchemaTargets`; carry
  it onto `schemaTarget` and pass `PasswordCommand` into `BuildSchemaJob`.
- When `PasswordCommand` is set, wrap the container command so the token is
  fetched at start:

  ```sh
  sh -c 'export SQL_PASSWORD="$(<passwordCommand>)"; exec temporal-sql-tool …'
  ```

  When only a static password is set, keep the current `SQL_PASSWORD` env
  (`passwordEnv`) and direct `temporal-sql-tool` invocation — unchanged.
- **New API field `spec.persistence.schemaJob.podTemplate`** (additive, pre-1.0)
  of the existing `PodTemplateOverride` type, so users can attach the
  ServiceAccount, the `azure.workload.identity/use: "true"` label, and a
  one-shot token **initContainer** (federated `az login` → write
  `/azure/pgpass`) to the Job pod. Applied with the existing `applyPodTemplate`
  helper introduced in the phase-1 Azure work. A short-lived Job only needs a
  single token, so an initContainer (not a refresher sidecar) is sufficient.

**Tests** (`internal/resources/schemajob_test.go`):
- With `PasswordCommand` set, the Job command is the `sh -c` wrapper that
  populates `SQL_PASSWORD` from the command; no static `SQL_PASSWORD` env.
- `schemaJob.podTemplate` attaches the ServiceAccount, WI label, and an
  initContainer + shared `emptyDir`, preserving the generated `schema` container.
- Nil `podTemplate` and static-password path leave the Job byte-for-byte
  unchanged.

### 3. Operator Helm chart Workload Identity support

Files: `dist/chart/` (edited **by hand** — never run `make helm-chart`).

- Add **opt-in** values (`workloadIdentity.enable`, `workloadIdentity.clientId`)
  that set, on the operator controller Deployment:
  - the ServiceAccount `azure.workload.identity/client-id` annotation,
  - the `azure.workload.identity/use: "true"` pod label.
- That is **all** that is needed: the Azure Workload Identity webhook then
  injects the `AZURE_*` env and projects the federated token, which the operator
  consumes in-process via `azidentity` (section 1). **No token-refresher sidecar
  or shared volume is added to the operator pod** (the operator does not shell
  out) — this keeps the operator distroless.
- All default **off**, so non-Azure installs are unchanged. Follow the chart's
  existing flat `values.yaml` contract (do not introduce the nested
  kubebuilder-generated contract).

### 4. Examples & docs

- `examples/cluster-azure-workload-identity`: remove the "operator probe + schema
  Job are not passwordless" caveat; add `sql.azureWorkloadIdentity: {}` to the
  store specs (operator auth) alongside `passwordCommandSecretRef` (server +
  schema Job); install the operator with `workloadIdentity.enable=true` +
  `clientId` (label/annotation only, no sidecar); keep the schema-Job
  `podTemplate` token initContainer; promote out of "preview" once verified.
- `docs/content/installation/azure.md`: update the Entra/Workload Identity
  section to state full passwordless now works, document that the operator
  obtains its own token natively (distroless-friendly), and document the operator
  chart values + the per-actor auth (operator: native Entra; server + schema Job:
  `passwordCommand`).
- Close #47, referencing the PR. Open a follow-up issue to make the operator's
  own DB token auth generic / multi-provider (beyond Azure Workload Identity).

### PR1 validation

`make generate manifests` (new `schemaJob.podTemplate` field in the CRD),
`make build`, `make test` (new unit tests above), `make lint`.

---

## PR2 — AKS passwordless e2e

### 1. Provisioning script + Make targets

File: `hack/azure-e2e.sh` (style mirrors `hack/nsc-e2e.sh`: `set -euo pipefail`,
`trap` cleanup, structured logging).

All resources live in **one tagged resource group** (tags
`app=temporal-operator-e2e`, `run=<id>`) so teardown is a single
`az group delete`.

Make targets:

- `make azure-e2e-up` — `az group create` → `az acr create` + `az acr build`
  (operator image built **remotely from the branch**, no local docker) →
  `az aks create --enable-oidc-issuer --enable-workload-identity --attach-acr`
  (1 small node) → `az postgres flexible-server create` (burstable SKU,
  `--active-directory-auth Enabled`, TLS) + create `temporal` and
  `temporal_visibility` databases → create a user-assigned managed identity +
  **federated credential** bound to the test ServiceAccount →
  `pgaadauth_create_principal` mapping the identity to a Postgres role →
  `helm install` the operator with the WI values + ACR image → write outputs to
  a gitignored `.env` (`AZURE_PG_HOST`, `AZURE_CLIENT_ID`, etc.).
- `make azure-e2e-test` — source `.env`, run the Chainsaw `test/e2e/azure` suite
  against the standing cluster (fast, re-runnable while iterating).
- `make azure-e2e-down` — `az group delete` (removes everything).
- `make azure-e2e` — `up` → `test` → `down` with a `trap` that **always** tears
  down, for unattended runs.
- `make azure-e2e-clean` — delete **any** resource group carrying our tag
  (leaked-billing backstop, analogous to `make nsc-clean`).

### 2. Chainsaw suite

Directory: `test/e2e/azure/` (static, committed; no generated YAML).

- Provisioning outputs are consumed via Chainsaw's `env(...)` function (e.g.
  `AZURE_PG_HOST`, `AZURE_CLIENT_ID`), so the committed manifests stay generic.
- Steps:
  1. Create the namespace, the Workload-Identity-annotated ServiceAccount, and
     the `passwordCommandSecretRef` Secret (`cat /azure/pgpass`).
  2. Apply the `TemporalCluster`: TLS enabled, `passwordCommandSecretRef`,
     `overrides.podTemplate` WI label + token-refresher sidecar, and
     `persistence.schemaJob.podTemplate` one-shot token initContainer.
  3. **Assert** the cluster reaches `PersistenceReachable=True`,
     `SchemaReady=True`, and the frontend Deployment reports available replicas.
     These conditions only flip true if **every** passwordless actor (operator
     probe, schema Job, server pods) authenticated to Flexible Server.
- `catch` blocks `describe` the `TemporalCluster` and dump `events` on failure.

### 3. Docs & CI-readiness

- `test/e2e/azure/README.md`: prerequisites (`az login`, subscription
  selection), the up/test/down/clean steps, expected cost, and the leak-clean
  backstop.
- Every input is an env var (region, SKUs, RG name have defaults but are
  override-able), so a future GitHub Actions workflow can invoke the same Make
  targets with an Azure OIDC login. No workflow is added in this PR.

### Defaults (override via env)

- Region: `eastus`.
- AKS: 1 × `Standard_B2s`.
- Flexible Server: `Standard_B1ms` (burstable; cheapest SKU that supports Entra
  auth), PostgreSQL 16.

Short-lived runs cost cents against the available credit; the all-in-one `trap`
plus the tag-based `azure-e2e-clean` target guard against leaked billing.

## Risks and mitigations

- **Leaked billable resources.** Single tagged RG; `trap`-based teardown in the
  all-in-one target; `azure-e2e-clean` backstop that deletes by tag.
- **Operator executing a Secret-sourced command.** Gated behind the explicit
  `passwordCommandSecretRef` opt-in; it is the same command the server pods run;
  documented trust boundary.
- **Token staleness in the operator probe.** The command is executed fresh on
  every probe/inspection, so a rotated token is always picked up.
- **Schema Job one-shot token expiry.** Job is short-lived; the initContainer
  fetches a fresh token at start, well within the Entra token lifetime.
- **Flaky/slow Azure provisioning.** Separable `up`/`test`/`down` targets let a
  cluster stand while the Chainsaw suite is iterated; `test` is independently
  re-runnable.

## Testing

- PR1: `make generate manifests`, `make build`, `make test`, `make lint`.
- PR2: `make azure-e2e` (full provision → assert → teardown) from a coder
  instance with `az login`; `make azure-e2e-clean` verified to remove a leaked
  RG.
