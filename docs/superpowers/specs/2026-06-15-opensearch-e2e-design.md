# OpenSearch advanced-visibility e2e coverage

**Date:** 2026-06-15
**Status:** Approved design

## Problem

The e2e suite only meaningfully exercises Postgres in CI. The operator supports
an Elasticsearch/OpenSearch-style visibility store (`persistence.visibilityStore.elasticsearch`),
but there is no advanced-visibility suite running in CI:

- The existing `test/e2e/elasticsearch/` suite (ECK operator + Postgres default
  store) is not wired into either the `e2e.yml` matrix or the nightly workflow,
  so it only runs locally. Its visibility URL (`https://temporal-es-es-http:9200`)
  is also **not** namespace-qualified, and the operator probes the store from the
  `temporal-system` namespace, so that bare hostname cannot resolve cross-namespace.

We want advanced-visibility (ES-family) e2e coverage that actually runs in CI, via
the cheapest route. The chosen target is **OpenSearch 2.x**.

## Goal

Add a new `test/e2e/opensearch/` Chainsaw suite that stands up a single-node
OpenSearch 2.x visibility store alongside a Postgres default store, drives it
through the operator's existing `elasticsearch` visibility config, and runs in the
non-PR e2e matrix. No operator code changes.

## Key compatibility findings (why no code change is needed)

- Temporal's ES client factory (`common/persistence/visibility/store/elasticsearch/client/client_factory.go`)
  accepts only `"v7"`, `"v8"`, or `""`. **There is no `os2` value.** Both `v7` and
  `v8` map to the same `olivere/elastic/v7` client implementation.
- That client is created with `elastic.SetHealthcheck(false)` and sniff disabled,
  which avoids the strict Elasticsearch-8 product/version handshake that would
  otherwise reject OpenSearch. This is what makes **OpenSearch 2.x work through the
  v7/v8 client**.
- The operator's `esBackend` (`internal/persistence/elasticsearch.go`) only calls
  `GET /_cluster/health` and `GET/PUT /_index_template/...` — both supported by
  OpenSearch 2.x. It also supports plain HTTP with no auth and no TLS.
- The operator emits `version: <v7|v8>` straight into Temporal's config
  (`internal/temporal/templates/config_template.yaml`). The `ElasticsearchDatastoreSpec.Version`
  enum already allows `v8` (`api/v1alpha1/persistence_types.go`), so
  **OpenSearch 2.x + `version: v8` requires no operator change.**
- Official Temporal OpenSearch 2.x support starts at server **v1.30.1+**; the suite
  pins **Temporal 1.31.1**, which satisfies this.

## Design

### Topology

```
namespace (per-test)
├── CNPG Postgres cluster   (default store)   ← reuse ../postgres fixtures
└── OpenSearch single node  (visibility store) ← new, plain Deployment + Service
        ▲
        │ operator probes/applies template from temporal-system (FQDN required)
TemporalCluster: defaultStore=sql(postgres), visibilityStore=elasticsearch(v8)
```

No OpenSearch Kubernetes operator is used. OpenSearch is a plain `Deployment`
plus `Service` — this is the main reason this route is cheaper than ECK: nothing
extra to install or wait on in CI.

### Files (`test/e2e/opensearch/`)

1. **`chainsaw-test.yaml`** — Steps:
   - `provision-deps`: apply `../postgres/01-fixtures-cnpg.yaml` and
     `01-opensearch.yaml`; assert the OpenSearch Deployment is available.
     (Note: `../postgres/02-secrets.yaml` is intentionally **not** reused — it
     only creates the `temporal_visibility` Postgres database, which is unused
     when visibility is OpenSearch. Only the default `temporal` database, created
     by CNPG initdb, is needed.)
   - `cluster-os-visibility`: apply `02-temporalcluster.yaml`; assert
     `02-assert.yaml`.
   - Timeouts mirror the ES suite (`apply: 2m`, `assert: 10m`).

2. **`01-opensearch.yaml`** — `Deployment` (replicas: 1) + `Service`
   (`temporal-os`, port 9200). Container `opensearchproject/opensearch:2.19.5`
   with env:
   - `discovery.type=single-node`
   - `DISABLE_SECURITY_PLUGIN=true`
   - `DISABLE_INSTALL_DEMO_CONFIG=true`
   - `plugins.security.disabled=true`
   - `compatibility.override_main_response_version=true`
   - `node.store.allow_mmap=false` (kind nodes don't raise `vm.max_map_count`)
   - `cluster.routing.allocation.disk.threshold_enabled=false` (kind nodes often
     trip the 85% disk watermark, which flips indices read-only)
   - `OPENSEARCH_JAVA_OPTS=-Xms512m -Xmx512m`
   - Readiness/liveness probe: HTTP `GET /_cluster/health` on 9200.
   - `emptyDir` volume for `/usr/share/opensearch/data`.

3. **`02-temporalcluster.yaml`** — `TemporalCluster`:
   - `version: "1.31.1"`, `numHistoryShards: 512`
   - `persistence.defaultStore.sql`: postgres12, host
     `(join('.', ['temporal-pg-rw', $namespace, 'svc.cluster.local']))`, db
     `temporal`, user `temporal`, `passwordSecretRef` → `temporal-pg-app`.
   - `persistence.visibilityStore.elasticsearch`:
     - `version: v8`
     - `url: (join('.', ['temporal-os', $namespace, 'svc.cluster.local:9200']))`
       — namespace-qualified FQDN as `host:port` **without** a scheme. The
       operator passes this straight into Temporal's `url.host`, and the absence
       of `tls` yields `scheme: http` (confirmed by golden `es-visibility.yaml`).
       The FQDN is required because the operator probes from `temporal-system`.
     - `tls` omitted (plain HTTP), no username/password.

4. **`02-assert.yaml`** — TemporalCluster status:
   - `conditions[?type == 'SchemaReady'].status == "True"`
   - `conditions[?type == 'Ready'].status == "True"`
   Mirrors the ES suite. The operator applies the visibility index template; the
   index auto-creates on Temporal's first visibility write (same model as ES).

### CI wiring (`.github/workflows/e2e.yml`)

- Add to the **full / non-PR** matrix combos only (keep PR runs fast):
  `{"temporal":"1.31.1","persistence":"opensearch","suite":"opensearch"}`.
  Do **not** add it to the PR-only combo list.
- Extend the "Pre-pull and load Temporal images" step to also pull and side-load
  `opensearchproject/opensearch:2.19.5` into the kind node (Docker Hub images are
  rate-limited for the kind kubelet's anonymous pulls, like the temporalio images).
  The version is pinned, so a literal addition to the image list is sufficient.

## Decisions (confirmed)

- OpenSearch target: **2.x**, pinned to **2.19.5**, single node.
- Temporal `version`: **v8** (works with OpenSearch 2.x via the olivere v7 client).
- Default store: **Postgres** via CNPG, reusing the existing postgres fixtures.
- PR matrix: OpenSearch runs in the **full/non-PR matrix only**.
- Scope: **new OpenSearch suite only.** The existing ECK Elasticsearch suite is
  left untouched in this effort.

## Out of scope

- Fixing or wiring in the existing `test/e2e/elasticsearch/` (ECK) suite.
- OpenSearch with the security plugin enabled / TLS / auth.
- A dedicated `os2` version value or distinct `opensearch` config kind (not needed;
  Temporal has no `os2` config value).
- OpenSearch 1.x.
- Functional visibility queries (workflow search) — the suite asserts operator
  reconciliation conditions, consistent with the existing ES suite.

## Testing / verification

- Local: `./bin/chainsaw test --test-dir test/e2e/opensearch --config .chainsaw.yaml`
  against a kind cluster with the operator installed (per `e2e.yml` setup).
- CI: the new matrix combo runs the suite; the aggregate e2e status check covers it.
