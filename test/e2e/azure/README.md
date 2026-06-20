# Azure passwordless end-to-end test

This suite proves the operator brings a `TemporalCluster` to `Ready` against a
**real** Azure Database for PostgreSQL Flexible Server using **passwordless**
Microsoft Entra (Azure AD) auth via Azure Workload Identity — with **zero static
passwords**. It validates the #47 changes end-to-end across every actor:

| Actor | How it authenticates |
|---|---|
| **Operator** probe + schema inspection | native in-process Entra token (`sql.azureWorkloadIdentity`); the operator runs on a distroless image with no shell, so it cannot run a `passwordCommand` |
| **Temporal server** pods | `passwordCommand` fed by an `azure-token-refresher` sidecar |
| **Schema Job** | `passwordCommand` fed by a one-shot token initContainer |

Unlike the kind (`.github/workflows/e2e.yml`) and nsc (`hack/nsc-e2e.sh`) flows —
which never exercise Workload Identity, Entra auth, or Flexible Server TLS — this
suite stands up actual Azure infrastructure. It is **not** part of CI; it is run
on demand from a developer machine with an Azure subscription.

## What gets created

The harness (`hack/azure-e2e.sh`) provisions everything into **one tagged
resource group** so teardown is a single `az group delete`:

- an Azure Container Registry and a **remote** image build of the current branch
  (no local Docker needed);
- an AKS cluster (`--enable-oidc-issuer --enable-workload-identity`, 1 node);
- a user-assigned managed identity + **federated credential** bound to the test
  ServiceAccount (`azure-e2e:temporal-workload-identity`);
- an Azure Database for PostgreSQL Flexible Server (Entra auth, password auth
  disabled, TLS) with `temporal` and `temporal_visibility` databases, and a
  Postgres role mapped to the managed identity via `pgaadauth_create_principal`;
- a Helm install of the operator with `workloadIdentity.enable=true`.

## Prerequisites

- `az` CLI, logged in with an active subscription that has quota for a
  `Standard_B2s` AKS node and a Burstable Flexible Server:

  ```sh
  az login
  az account set --subscription <SUBSCRIPTION_ID>
  ```
- `kubectl`, `helm`, and `psql` on `PATH` (`psql` performs the identity → role
  mapping). `chainsaw` is installed automatically via `make chainsaw`.

## Running it

Separable phases (recommended while iterating — `test` is fast and re-runnable):

```sh
make azure-e2e-up      # provision AKS + Flexible Server, install the operator
make azure-e2e-test    # run the Chainsaw suite against the standing cluster
make azure-e2e-down    # delete the resource group (everything)
```

All-in-one, unattended (a `trap` **always** tears down, even on failure):

```sh
make azure-e2e
```

Leak backstop — delete **any** resource group carrying our tag, in case a run was
interrupted before teardown:

```sh
make azure-e2e-clean
```

`azure-e2e-up` writes two **gitignored** files:

- `hack/.azure-e2e.env` — resource group + connection facts used by `down`.
- `test/e2e/azure/.values.local.yaml` — `pgHost`, `pgUser`, `clientId` consumed
  by Chainsaw via `--values`.

## Cost

A short-lived run costs **cents** — a `Standard_B2s` AKS node and a `Standard_B1ms`
Burstable Flexible Server are billed by the minute. **Always tear down** when
finished (`make azure-e2e-down`), and run `make azure-e2e-clean` if anything was
left behind.

## Configuration (env vars, with defaults)

| Variable | Default | Notes |
|---|---|---|
| `AZURE_LOCATION` | `eastus` | Azure region. |
| `AZURE_RG` | `temporal-operator-e2e-<rand>` | Resource group (persisted in the env file). |
| `AKS_NODE_SIZE` | `Standard_B2s` | AKS node VM size. |
| `AKS_NODE_COUNT` | `1` | AKS node count. |
| `PG_SKU` | `Standard_B1ms` | Flexible Server compute. |
| `PG_TIER` | `Burstable` | Flexible Server tier. |
| `PG_VERSION` | `16` | PostgreSQL major version. |
| `ACR_NAME` | `tempope2e<rand>` | Container registry name. |
| `E2E_TAG` | `app=temporal-operator-e2e` | Tag used by `azure-e2e-clean`. |

## Moving to CI

Every input is an env var, so a future GitHub Actions workflow can `azure/login`
with OIDC and call the same `make azure-e2e` target — no script changes needed.
No workflow is added here on purpose (the run is slow and incurs Azure cost).

## First-run notes

The exact `az postgres flexible-server create` flags and the
`pgaadauth_create_principal*` signature vary by `az` / `pgaadauth` version. The
script targets **az CLI 2.87.0**, which uses `--microsoft-entra-auth Enabled`,
`--admin-display-name`, `--admin-object-id`, and `--admin-type User` (older docs
showed `--active-directory-auth` / `--microsoft-entra-admin`). If
`make azure-e2e-up` fails at Flexible Server creation or the principal-mapping
step on a different `az` version, reconcile against
`az postgres flexible-server create --help` and the server's installed
`pgaadauth` functions, then commit the fix (`fix(e2e): ...`) and re-run.

> A failed `up` leaves a partially-provisioned, tagged resource group behind.
> Run `make azure-e2e-clean` to delete any leftover
> `app=temporal-operator-e2e` resource groups before retrying.
