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

### 1. Operator probe & schema inspection execute `passwordCommand`

Files: `internal/persistence/sql.go`,
`internal/controller/temporalcluster_persistence.go`.

- Today `sqlBackend.dsn()` builds the DSN from `b.cred.Password`. Change: when
  `cred.PasswordCommand` is set (and `Password` is empty), **execute the command
  and use its trimmed stdout as the password**, rebuilding the DSN on **every**
  probe and schema-version inspection so an expiring Entra token is always
  refreshed.
- Introduce a small injectable runner to keep this unit-testable:

  ```go
  // CommandRunner executes a shell command and returns its trimmed stdout.
  type CommandRunner func(ctx context.Context, command string) (string, error)
  ```

  Default implementation runs `sh -c <command>` via `os/exec` and trims trailing
  whitespace/newlines. The runner is a field on `SQLProber` / `sqlBackend` and
  defaults to the real implementation when nil.
- The static-password path is **byte-for-byte unchanged** (no command set →
  existing behavior).
- Security / trust boundary: the operator only ever executes the command the
  user explicitly supplied via `passwordCommandSecretRef`. This is the same
  command the server pods already run. Documented as an explicit opt-in.

**Tests** (`internal/persistence/sql_test.go`):
- When `cred.PasswordCommand` is set, the backend executes it and uses the
  output as the DSN password (inject a fake `CommandRunner`).
- The command is executed **fresh** on each `Probe` / `SchemaVersion` call
  (token rotation).
- Command execution error propagates as a probe error.
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

- Add **opt-in** values to set, on the operator controller Deployment:
  - the ServiceAccount name + `azure.workload.identity/client-id` annotation,
  - the `azure.workload.identity/use: "true"` pod label,
  - a token-refresher sidecar + shared `emptyDir` (so the operator pod's
    `cat /azure/pgpass` `passwordCommand` works).
- All default **off**, so non-Azure installs are unchanged. Follow the chart's
  existing flat `values.yaml` contract (do not introduce the nested
  kubebuilder-generated contract).

### 4. Examples & docs

- `examples/cluster-azure-workload-identity`: remove the "operator probe + schema
  Job are not passwordless" caveat; add the operator Helm WI values and the
  `persistence.schemaJob.podTemplate` token initContainer; promote out of
  "preview" once verified.
- `docs/content/installation/azure.md`: update the Entra/Workload Identity
  section to state full passwordless now works and document the operator chart
  values + schema-Job `podTemplate`.
- Close #47, referencing the PR.

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
