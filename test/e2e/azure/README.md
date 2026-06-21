# Azure passwordless end-to-end test

This suite proves the operator brings a `TemporalCluster` to `Ready` against a
**real** Azure Database for PostgreSQL Flexible Server using **passwordless**
Microsoft Entra (Azure AD) auth via Azure Workload Identity — with **zero static
passwords**. It validates the #47 changes end-to-end across every actor:

| Actor | How it authenticates |
|---|---|
| **Temporal server** pods | `passwordCommand` fed by an operator-generated one-shot token initContainer (seeds the token before startup) plus an `azure-token-refresher` sidecar (keeps it fresh) |
| **Schema Job** | `passwordCommand` fed by an operator-generated one-shot token initContainer |

Unlike the kind (`.github/workflows/e2e.yml`) and nsc (`hack/nsc-e2e.sh`) flows —
which never exercise Workload Identity, Entra auth, or Flexible Server TLS — this
suite stands up actual Azure infrastructure. It is **not** part of CI; it is run
on demand from a developer machine with an Azure subscription.

The operator generates **all** the Azure Workload Identity wiring (ServiceAccount,
sidecars, initContainers, passwordCommand, inspector Jobs) from a single
cluster-level `persistence.azureWorkloadIdentity.clientId` field. The suite
applies only the `TemporalCluster`; no hand-written ServiceAccount or Secret is
needed.

## What gets created

The harness (`hack/azure-e2e.sh`) provisions everything into **one tagged
resource group** so teardown is a single `az group delete`:

- an Azure Container Registry and a **remote** image build of the current branch
  (no local Docker needed);
- an AKS cluster (`--enable-oidc-issuer --enable-workload-identity`, 1 node);
- a user-assigned managed identity with a **single** federated credential bound
  to the **operator-generated** cluster ServiceAccount `azure-e2e-azure` in the
  `azure-e2e` namespace (the operator creates this ServiceAccount when the
  TemporalCluster is applied; the operator itself no longer needs Azure Workload
  Identity);
- an Azure Database for PostgreSQL Flexible Server (Entra auth, password auth
  disabled, TLS) with `temporal` and `temporal_visibility` databases, the
  `azure.extensions` allow-list set to `btree_gin,pg_trgm` (required by Temporal's
  SQL visibility schema), a Postgres role mapped to the managed identity via
  `pgaadauth_create_principal`, PostgreSQL 16 `public`-schema grants for that
  role, and a firewall rule for the runner's public IP (for the setup `psql`);
- **cert-manager** (required by the operator's webhook serving certificates);
- a Helm install of the operator (no operator Workload Identity Helm values).

The Chainsaw suite is pinned to the `azure-e2e` namespace (`--namespace`) so the
operator-generated ServiceAccount's subject matches the federated credential.

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

### Standing up a usable cluster (not just the test)

`azure-e2e-up` installs the **operator** but does not deploy a `TemporalCluster`,
and `azure-e2e-test` deploys one only to assert on it and then tears it down. To
get a **standing, usable** `TemporalCluster` you can connect to, deploy one
against the provisioned environment:

```sh
make azure-e2e-up-deploy   # provision everything, then leave a usable cluster, OR
make azure-e2e-deploy      # deploy against an environment a previous 'up' provisioned
```

This applies a `TemporalCluster` named `azure-e2e` in the `azure-e2e` namespace
(the name/namespace are fixed so the operator-generated ServiceAccount matches
the federated credential) and waits for it to become `Ready`. Reach it with:

```sh
kubectl -n azure-e2e port-forward svc/azure-e2e-frontend 7233:7233
```

Delete just the standing cluster with `kubectl delete namespace azure-e2e`, or
tear down everything with `make azure-e2e-down`.

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
| `AZURE_LOCATION` | `centralus` | Azure region (see note below). |
| `AZURE_RG` | `temporal-operator-e2e-<rand>` | Resource group (persisted in the env file). |
| `AKS_NODE_SIZE` | `Standard_B2s` | AKS node VM size. |
| `AKS_NODE_COUNT` | `1` | AKS node count. |
| `PG_SKU` | `Standard_B1ms` | Flexible Server compute. |
| `PG_TIER` | `Burstable` | Flexible Server tier. |
| `PG_VERSION` | `16` | PostgreSQL major version. |
| `ACR_NAME` | `tempope2e<rand>` | Container registry name. |
| `AZURE_TEST_NS` | `azure-e2e` | Namespace the Chainsaw suite runs in (must match the workload federated credential). |
| `E2E_TAG` | `app=temporal-operator-e2e` | Tag used by `azure-e2e-clean`. |

## Moving to CI

Every input is an env var, so a future GitHub Actions workflow can `azure/login`
with OIDC and call the same `make azure-e2e` target — no script changes needed.
No workflow is added here on purpose (the run is slow and incurs Azure cost).

## Choosing a region

Azure Database for PostgreSQL Flexible Server is **region-restricted on many
subscriptions** — Visual Studio / MSDN credit subscriptions, for example,
restrict popular regions like `eastus`, `eastus2`, and `westus2`, and creation
fails with `ERROR: The location is restricted from performing this operation`.
The default is `centralus`, which is broadly available. To find a region your
subscription allows, check which ones expose the Burstable SKU (the capabilities
API reflects per-subscription offer restrictions):

```sh
for r in centralus westus3 westus canadacentral eastus eastus2 westus2; do
  n=$(az postgres flexible-server list-skus -l "$r" -o json 2>/dev/null | grep -c Standard_B1ms)
  echo "$r: $n"   # non-zero => Standard_B1ms is available to you there
done
```

Then run with, e.g., `AZURE_LOCATION=westus3 make azure-e2e-up`.

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
