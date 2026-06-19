# Cassandra + OpenSearch e2e coverage

**Date:** 2026-06-19
**Status:** Approved design

## Problem

The e2e suite has solid coverage for Postgres (default + Postgres visibility) and
for OpenSearch advanced visibility (Postgres default + OpenSearch visibility), but
Cassandra has no working coverage:

- The existing `test/e2e/cassandra/` suite is **not wired into CI** — it is absent
  from both the `e2e.yml` matrix and the `workflow_dispatch` choices, and the
  workflow never installs the K8ssandra operator its fixture requires (CI installs
  only cert-manager + CNPG).
- The existing fixture (`02-temporalcluster.yaml`) uses **Cassandra for the
  visibility store** on Temporal `1.31.1`. But `cassandraVisibilitySupported` is
  `false` for **every** version in `hack/version-matrix.yaml`, and the validating
  webhook (`internal/webhook/v1alpha1/temporalcluster_webhook.go:153-157`) rejects
  Cassandra visibility on those versions. So the fixture **cannot pass as written**.
- There is no coverage for the realistic production layout the fixtures themselves
  describe as "the typical production layout": **Cassandra default store +
  Elasticsearch/OpenSearch visibility store**.

We want Cassandra default-store e2e coverage that actually runs in CI, paired with
OpenSearch visibility (reusing the established OpenSearch backend pattern), via the
cheapest route and with no operator code changes.

## Goal

Repurpose the existing `test/e2e/cassandra/` Chainsaw suite into a correct,
CI-wired suite that stands up:

- a single-node **Cassandra 4.1** default store (plain StatefulSet, no operator),
- a single-node **OpenSearch 2.x** visibility store (same pattern as the
  `opensearch` suite),

drives them through the operator's existing `cassandra` default-store config and
`elasticsearch` visibility config, and runs in the non-PR (nightly /
`workflow_dispatch`) e2e matrix. No operator code changes expected.

## Key findings (why no code change is needed)

- **No-auth Cassandra works cleanly.** When the spec's `user` is empty, the
  operator's Cassandra backend skips the authenticator
  (`internal/persistence/cassandra.go:43-45`) and the schema Job omits `--user`
  (`internal/resources/schemajob.go:158-160`). A plain Cassandra container with the
  default `AllowAllAuthenticator` therefore needs no secret and no `user`/
  `passwordSecretRef` in the TemporalCluster spec — mirroring the OpenSearch
  "security disabled" approach already used in the repo.
- **The keyspace must be pre-created.** The operator's schema Job runs
  `setup-schema`/`update-schema` but **never `create-keyspace`**
  (`internal/resources/schemajob.go:164-169`), there is no keyspace-creation logic
  anywhere in the operator, and the official `cassandra` image does **not**
  auto-run init CQL (unlike `postgres`/`mysql`). So the suite must create the
  `temporal` keyspace itself before applying the TemporalCluster. (The visibility
  store is OpenSearch, so no second keyspace is needed.)
- **OpenSearch already works through the operator unchanged.** The `opensearch`
  suite proves Temporal's v7/v8 ES client (healthcheck/sniff disabled) and the
  operator's `esBackend` work against single-node OpenSearch 2.x over plain HTTP.

## Architecture

All fixtures live in `test/e2e/cassandra/` (suite is self-contained so the CI
image-prepull harvester, which scans only the suite's own directory, sees every
image it must side-load into kind).

### 1. Cassandra backend (replaces `01-k8ssandra.yaml`)

A plain single-node **StatefulSet + Service**:

- Image `cassandra:4.1`, CQL port `9042`, Service `temporal-cass`.
- Default `AllowAllAuthenticator` (no auth, no TLS) — disposable backend.
- JVM heap capped via `MAX_HEAP_SIZE` / `HEAP_NEWSIZE` (and a single-node-friendly
  config) to stay healthy on kind nodes, echoing the OpenSearch heap tuning.
- Readiness probe using `cqlsh -e "describe keyspaces"` (or `nodetool status`) so
  later steps only run once Cassandra accepts CQL.
- Data on `emptyDir` (disposable; no persistence needed for e2e).

### 2. Keyspace bootstrap

A small **Job** (or Chainsaw `script` step) that runs:

```
cqlsh temporal-cass 9042 -e "CREATE KEYSPACE IF NOT EXISTS temporal \
  WITH replication = {'class':'SimpleStrategy','replication_factor':1};"
```

Gated to run after Cassandra is ready and before the TemporalCluster is applied.

### 3. OpenSearch backend (visibility)

A single-node OpenSearch 2.x **Deployment + Service**, **copied into the cassandra
suite dir** (not cross-referenced from `../opensearch`), identical in spirit to
`test/e2e/opensearch/01-opensearch.yaml`: security disabled, plain HTTP,
`compatibility.override_main_response_version=true`, mmap/disk-watermark settings
for kind. Copying (rather than `apply: ../opensearch/...`) keeps the CI image
harvest correct.

### 4. TemporalCluster (`02-temporalcluster.yaml`)

```yaml
spec:
  version: "1.31.1"
  numHistoryShards: 512
  persistence:
    defaultStore:
      cassandra:
        hosts: [<namespace-qualified FQDN of temporal-cass>]
        port: 9042
        keyspace: temporal
    visibilityStore:
      elasticsearch:
        url: <namespace-qualified FQDN of the OpenSearch service>:9200
        version: v8
```

The Cassandra host and OpenSearch URL use **namespace-qualified FQDNs** because the
operator probes both stores from the `temporal-system` namespace (bare service
names would not resolve cross-namespace — the established repo convention). No
`user`/`passwordSecretRef` on the Cassandra store (no-auth). No `tls` block on the
visibility store (yields an `http` scheme).

### 5. Asserts (`02-assert.yaml`)

Unchanged in spirit: TemporalCluster `SchemaReady` and `Ready` conditions both
`True`.

### 6. Chainsaw test (`chainsaw-test.yaml`)

Step ordering with generous (~10m) assert timeouts (Cassandra cold-start on kind is
slow):

1. Provision Cassandra (StatefulSet + Service) and OpenSearch (Deployment +
   Service); assert both ready.
2. Run the keyspace-bootstrap Job; assert completion.
3. Apply the TemporalCluster; assert `02-assert.yaml`.
4. `catch`: `describe` the TemporalCluster on failure.

## CI wiring (`.github/workflows/e2e.yml`)

- Add a `cassandra` combo:
  `cassandra='{"temporal":"1.31.1","persistence":"cassandra","suite":"cassandra"}'`
  and include it in the `schedule` and `all` combo lists.
- Add `cassandra` to the `workflow_dispatch` `suite` `options` and a matching
  `case` branch.
- Keep Cassandra **out of the fast PR combo** (it is slow to boot) — nightly /
  dispatch only.
- **No new operator install step** — the plain StatefulSet needs nothing beyond the
  cluster; cert-manager + CNPG remain installed (CNPG simply unused here).
- Extend the image-prepull harvest to also pull `cassandra:*` images (Docker Hub,
  rate-limited for kind's anonymous kubelet) and side-load them into the kind node,
  alongside the existing `temporalio/*` and `opensearchproject/*` harvesting.

## Removals

- Delete `test/e2e/cassandra/01-k8ssandra.yaml` and all `K8ssandraCluster` /
  superuser-secret references. No `k8ssandra.io` / `cassandra.datastax.com` CRD
  dependency remains.

## Testing / validation

- Local: `./bin/chainsaw test --test-dir test/e2e/cassandra --config .chainsaw.yaml`
  against a kind cluster with the operator installed (per `hack/nsc-e2e.sh` / the
  e2e workflow).
- `make build` / `make lint` only if any Go changes prove necessary (none expected;
  the operator already supports Cassandra default + ES visibility). No unit-test
  changes anticipated since this is fixtures + workflow only.

## Out of scope

- Cassandra visibility coverage (unsupported across the current version matrix).
- Multi-node / production-grade Cassandra (K8ssandra operator) — deliberately
  avoided in favor of the lightweight disposable-backend pattern.
- A Cassandra + Elasticsearch (ECK) variant — OpenSearch is the chosen, cheaper
  advanced-visibility backend, consistent with the `opensearch` suite.
