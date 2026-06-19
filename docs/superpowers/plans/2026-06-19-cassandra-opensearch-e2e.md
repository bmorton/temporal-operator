# Cassandra + OpenSearch e2e Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Repurpose `test/e2e/cassandra/` into a working, CI-wired Chainsaw suite that runs a Cassandra default store (plain StatefulSet, no auth) + OpenSearch visibility store, and add it to the nightly e2e matrix.

**Architecture:** Replace the broken K8ssandra/Cassandra-visibility fixtures with a self-contained suite: a single-node Cassandra StatefulSet+Service, a keyspace-bootstrap Job, a single-node OpenSearch Deployment+Service (copied in-dir), and a TemporalCluster using `cassandra` default + `elasticsearch` (OpenSearch) visibility. Wire the suite into `.github/workflows/e2e.yml` nightly/dispatch matrix and extend image pre-pull to harvest `cassandra:*`. No operator (Go) code changes.

**Tech Stack:** Kyverno Chainsaw, Kubernetes (kind), Cassandra 4.1, OpenSearch 2.x, Temporal 1.31.1, GitHub Actions.

---

## Context for the implementer

- **Spec:** `docs/superpowers/specs/2026-06-19-cassandra-opensearch-e2e-design.md`. Read it first.
- **This is a test-fixtures + CI change.** There is no Go unit test to write; the "tests" are the Chainsaw e2e suite itself, validated by running it (or by static validation where a live cluster is unavailable).
- **Reference fixtures to mirror exactly:**
  - OpenSearch backend: `test/e2e/opensearch/01-opensearch.yaml` (Deployment+Service), `test/e2e/opensearch/01-assert.yaml` (`availableReplicas: 1`).
  - Bootstrap-Job pattern: `test/e2e/postgres/02-secrets.yaml` (one-shot Job) + the `succeeded: 1` assert in `test/e2e/postgres/chainsaw-test.yaml:31-38`.
  - FQDN convention for store hosts: `test/e2e/opensearch/02-temporalcluster.yaml:21` and `:30` use
    `(join('.', ['temporal-pg-rw', $namespace, 'svc.cluster.local']))` /
    `(join('.', ['temporal-os', $namespace, 'svc.cluster.local:9200']))`.
- **Key operator facts (already verified, do not re-derive):**
  - Empty `user` ⇒ no Cassandra auth (`internal/persistence/cassandra.go:43-45`) and schema Job omits `--user` (`internal/resources/schemajob.go:158-160`).
  - The operator never runs `create-keyspace` (`internal/resources/schemajob.go:164-169`); the `temporal` keyspace must be pre-created by the suite.
  - Cassandra visibility is unsupported on all matrix versions; the visibility store here is OpenSearch via `elasticsearch` config.
- **Local run command** (requires a kind cluster with cert-manager, CNPG, and the operator installed — see `.github/workflows/e2e.yml`):
  `./bin/chainsaw test --test-dir test/e2e/cassandra --config .chainsaw.yaml`
- **Branch:** Work is on branch `cassandra-opensearch-e2e` (already created). Sign off every commit with `git commit -s`. Use Conventional Commit prefixes.

---

## File Structure

- **Delete:** `test/e2e/cassandra/01-k8ssandra.yaml` (K8ssandraCluster — unused, requires uninstalled operator).
- **Create:** `test/e2e/cassandra/01-cassandra.yaml` — Cassandra StatefulSet + Service.
- **Create:** `test/e2e/cassandra/01-assert.yaml` — assert Cassandra StatefulSet ready.
- **Create:** `test/e2e/cassandra/02-keyspace.yaml` — keyspace-bootstrap Job.
- **Create:** `test/e2e/cassandra/03-opensearch.yaml` — OpenSearch Deployment + Service (copied from opensearch suite).
- **Create:** `test/e2e/cassandra/03-opensearch-assert.yaml` — assert OpenSearch Deployment ready.
- **Modify:** `test/e2e/cassandra/02-temporalcluster.yaml` — rewrite to Cassandra default + OpenSearch visibility. (Renumber: this becomes step 04; see Task 6.) Final name: `04-temporalcluster.yaml`.
- **Modify/replace:** `test/e2e/cassandra/02-assert.yaml` → `04-assert.yaml` (content unchanged: SchemaReady + Ready True).
- **Rewrite:** `test/e2e/cassandra/chainsaw-test.yaml` — new step ordering.
- **Modify:** `.github/workflows/e2e.yml` — add `cassandra` combo, dispatch option, and `cassandra:*` image harvesting.

> **Naming note:** the final fixtures use numeric prefixes matching their Chainsaw step order: `01-cassandra`, `02-keyspace`, `03-opensearch`, `04-temporalcluster`. Tasks below create them under those names directly to avoid a later rename.

---

## Task 1: Replace the Cassandra backend fixture

**Files:**
- Delete: `test/e2e/cassandra/01-k8ssandra.yaml`
- Create: `test/e2e/cassandra/01-cassandra.yaml`
- Create: `test/e2e/cassandra/01-assert.yaml`

- [ ] **Step 1: Delete the K8ssandra fixture**

```bash
git rm test/e2e/cassandra/01-k8ssandra.yaml
```

- [ ] **Step 2: Create the Cassandra StatefulSet + Service**

Create `test/e2e/cassandra/01-cassandra.yaml`:

```yaml
# Single-node Cassandra 4.1 used as the Temporal default (persistence) store. No
# Cassandra Kubernetes operator (K8ssandra/cass-operator) is required: a plain
# StatefulSet + headless Service is enough for a disposable e2e backend.
#
# Auth is left at Cassandra's default AllowAllAuthenticator (no username, no
# password, no TLS) so the operator and the schema-setup Job connect without
# credentials. When the TemporalCluster's cassandra store omits `user`, the
# operator skips the authenticator entirely. The JVM heap is capped so a
# single-node Cassandra stays healthy on kind nodes. Data lives on emptyDir
# (disposable). The readiness probe runs a trivial CQL query so dependent steps
# only proceed once Cassandra accepts CQL on 9042.
apiVersion: v1
kind: Service
metadata:
  name: temporal-cass
  labels:
    app: temporal-cass
spec:
  clusterIP: None
  selector:
    app: temporal-cass
  ports:
    - name: cql
      port: 9042
      targetPort: 9042
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: temporal-cass
  labels:
    app: temporal-cass
spec:
  serviceName: temporal-cass
  replicas: 1
  selector:
    matchLabels:
      app: temporal-cass
  template:
    metadata:
      labels:
        app: temporal-cass
    spec:
      containers:
        - name: cassandra
          image: cassandra:4.1
          env:
            - name: CASSANDRA_CLUSTER_NAME
              value: temporal-e2e
            - name: CASSANDRA_DC
              value: dc1
            - name: CASSANDRA_ENDPOINT_SNITCH
              value: GossipingPropertyFileSnitch
            - name: MAX_HEAP_SIZE
              value: "512M"
            - name: HEAP_NEWSIZE
              value: "128M"
            - name: JVM_OPTS
              value: "-Dcassandra.skip_wait_for_gossip_to_settle=0"
          ports:
            - name: cql
              containerPort: 9042
          readinessProbe:
            exec:
              command:
                ["/bin/sh", "-c", "cqlsh -e 'SELECT now() FROM system.local'"]
            initialDelaySeconds: 30
            periodSeconds: 10
            timeoutSeconds: 10
            failureThreshold: 60
          volumeMounts:
            - name: data
              mountPath: /var/lib/cassandra
      volumes:
        - name: data
          emptyDir: {}
```

- [ ] **Step 3: Create the readiness assert**

Create `test/e2e/cassandra/01-assert.yaml`:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: temporal-cass
status:
  readyReplicas: 1
```

- [ ] **Step 4: Validate the YAML parses**

Run: `./bin/chainsaw lint test --file test/e2e/cassandra/01-cassandra.yaml` (if `chainsaw lint` is unavailable, run `kubectl apply --dry-run=client -f test/e2e/cassandra/01-cassandra.yaml -f test/e2e/cassandra/01-assert.yaml`).
Expected: no parse/schema errors.

- [ ] **Step 5: Commit**

```bash
git add test/e2e/cassandra/01-cassandra.yaml test/e2e/cassandra/01-assert.yaml
git commit -s -m "test(e2e): replace K8ssandra fixture with plain Cassandra StatefulSet"
```

---

## Task 2: Add the keyspace-bootstrap Job

**Files:**
- Create: `test/e2e/cassandra/02-keyspace.yaml`

The operator runs `setup-schema`/`update-schema` but never `create-keyspace`, and the `cassandra` image does not auto-run init CQL. This Job creates the `temporal` keyspace before the TemporalCluster is applied. (Visibility is OpenSearch, so no second keyspace.)

- [ ] **Step 1: Create the keyspace Job**

Create `test/e2e/cassandra/02-keyspace.yaml`:

```yaml
# The operator's schema Job runs setup-schema/update-schema but never
# create-keyspace, and the official cassandra image does not auto-run init CQL.
# This one-shot Job creates the `temporal` keyspace (the default store) before the
# TemporalCluster is applied. The visibility store is OpenSearch, so no second
# keyspace is needed. cqlsh ships in the cassandra image; we reuse it here. The
# loop waits for CQL to come up before issuing the CREATE KEYSPACE.
apiVersion: batch/v1
kind: Job
metadata:
  name: create-cassandra-keyspace
spec:
  backoffLimit: 10
  template:
    spec:
      restartPolicy: OnFailure
      containers:
        - name: createkeyspace
          image: cassandra:4.1
          command: ["/bin/sh", "-c"]
          args:
            - >-
              until cqlsh temporal-cass 9042 -e "SELECT now() FROM system.local";
              do echo "waiting for cassandra..."; sleep 5; done;
              cqlsh temporal-cass 9042 -e
              "CREATE KEYSPACE IF NOT EXISTS temporal WITH replication =
              {'class':'SimpleStrategy','replication_factor':1};"
```

- [ ] **Step 2: Validate the YAML parses**

Run: `kubectl apply --dry-run=client -f test/e2e/cassandra/02-keyspace.yaml`
Expected: `job.batch/create-cassandra-keyspace created (dry run)`.

- [ ] **Step 3: Commit**

```bash
git add test/e2e/cassandra/02-keyspace.yaml
git commit -s -m "test(e2e): add Cassandra keyspace bootstrap Job"
```

---

## Task 3: Add the OpenSearch visibility backend (copied in-dir)

**Files:**
- Create: `test/e2e/cassandra/03-opensearch.yaml`
- Create: `test/e2e/cassandra/03-opensearch-assert.yaml`

Copy the OpenSearch backend into the cassandra suite dir (rather than referencing `../opensearch`) so the CI image-prepull harvester — which scans only the suite's own directory — side-loads the OpenSearch image into kind.

- [ ] **Step 1: Create the OpenSearch Deployment + Service**

Create `test/e2e/cassandra/03-opensearch.yaml` (identical to `test/e2e/opensearch/01-opensearch.yaml`):

```yaml
# Single-node OpenSearch 2.x used as the Temporal visibility store. No OpenSearch
# Kubernetes operator is required: a plain Deployment + Service is enough for a
# disposable e2e backend.
#
# Security is fully disabled (plain HTTP, no auth, no TLS) so the operator's
# esBackend can probe /_cluster/health and apply the visibility index template
# without credentials. DISABLE_SECURITY_PLUGIN is the image entrypoint's
# mechanism for this and writes plugins.security.disabled into opensearch.yml;
# do NOT also set a plugins.security.disabled env var or OpenSearch aborts with
# "setting [plugins.security.disabled] already set". compatibility.override_main_response_version
# keeps Elasticsearch clients (Temporal uses olivere/elastic v7) from rejecting
# the version handshake. The mmap and disk-watermark settings keep single-node
# OpenSearch healthy on kind nodes (vm.max_map_count is not raised, and the node
# disk often sits above the default 85% flood-stage watermark).
apiVersion: apps/v1
kind: Deployment
metadata:
  name: temporal-os
  labels:
    app: temporal-os
spec:
  replicas: 1
  selector:
    matchLabels:
      app: temporal-os
  template:
    metadata:
      labels:
        app: temporal-os
    spec:
      containers:
        - name: opensearch
          image: opensearchproject/opensearch:2.19.5
          env:
            - name: discovery.type
              value: single-node
            - name: DISABLE_SECURITY_PLUGIN
              value: "true"
            - name: DISABLE_INSTALL_DEMO_CONFIG
              value: "true"
            - name: compatibility.override_main_response_version
              value: "true"
            - name: node.store.allow_mmap
              value: "false"
            - name: cluster.routing.allocation.disk.threshold_enabled
              value: "false"
            - name: bootstrap.memory_lock
              value: "false"
            - name: OPENSEARCH_JAVA_OPTS
              value: "-Xms512m -Xmx512m"
          ports:
            - name: http
              containerPort: 9200
          readinessProbe:
            httpGet:
              path: /_cluster/health
              port: 9200
            initialDelaySeconds: 20
            periodSeconds: 5
            timeoutSeconds: 3
            failureThreshold: 60
          volumeMounts:
            - name: data
              mountPath: /usr/share/opensearch/data
      volumes:
        - name: data
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: temporal-os
spec:
  selector:
    app: temporal-os
  ports:
    - name: http
      port: 9200
      targetPort: 9200
```

- [ ] **Step 2: Create the OpenSearch readiness assert**

Create `test/e2e/cassandra/03-opensearch-assert.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: temporal-os
status:
  availableReplicas: 1
```

- [ ] **Step 3: Validate the YAML parses**

Run: `kubectl apply --dry-run=client -f test/e2e/cassandra/03-opensearch.yaml -f test/e2e/cassandra/03-opensearch-assert.yaml`
Expected: no parse/schema errors.

- [ ] **Step 4: Commit**

```bash
git add test/e2e/cassandra/03-opensearch.yaml test/e2e/cassandra/03-opensearch-assert.yaml
git commit -s -m "test(e2e): add OpenSearch visibility backend to cassandra suite"
```

---

## Task 4: Rewrite the TemporalCluster fixture (Cassandra default + OpenSearch visibility)

**Files:**
- Create: `test/e2e/cassandra/04-temporalcluster.yaml`
- Create: `test/e2e/cassandra/04-assert.yaml`
- Delete: `test/e2e/cassandra/02-temporalcluster.yaml`, `test/e2e/cassandra/02-assert.yaml`

- [ ] **Step 1: Remove the old (broken) cluster + assert fixtures**

```bash
git rm test/e2e/cassandra/02-temporalcluster.yaml test/e2e/cassandra/02-assert.yaml
```

- [ ] **Step 2: Create the new TemporalCluster fixture**

Create `test/e2e/cassandra/04-temporalcluster.yaml`:

```yaml
# A TemporalCluster using Cassandra for the default (persistence) store and
# OpenSearch for the visibility store -- the typical production layout. Cassandra
# visibility is unsupported on current Temporal versions (see the version matrix
# / validating webhook), so visibility is served by OpenSearch via the operator's
# elasticsearch config (v7/v8 client, healthcheck/sniff disabled, works against
# OpenSearch 2.x).
#
# Hosts/URLs are namespace-qualified FQDNs: the operator probes both stores from
# the temporal-system namespace, so a bare service name would not resolve
# cross-namespace. The cassandra store omits user/passwordSecretRef because the
# backend runs with AllowAllAuthenticator; the visibility store omits a tls block,
# which yields an http scheme in the rendered Temporal config.
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: temporal-cass
spec:
  version: "1.31.1"
  numHistoryShards: 512
  persistence:
    defaultStore:
      cassandra:
        hosts:
          - (join('.', ['temporal-cass', $namespace, 'svc.cluster.local']))
        port: 9042
        keyspace: temporal
    visibilityStore:
      elasticsearch:
        url: (join('.', ['temporal-os', $namespace, 'svc.cluster.local:9200']))
        version: v8
```

- [ ] **Step 3: Create the cluster assert**

Create `test/e2e/cassandra/04-assert.yaml`:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: temporal-cass
status:
  (conditions[?type == 'SchemaReady'].status | [0]): "True"
  (conditions[?type == 'Ready'].status | [0]): "True"
```

- [ ] **Step 4: Commit**

```bash
git add test/e2e/cassandra/04-temporalcluster.yaml test/e2e/cassandra/04-assert.yaml
git commit -s -m "test(e2e): use Cassandra default + OpenSearch visibility for cluster fixture"
```

---

## Task 5: Rewrite the Chainsaw test orchestration

**Files:**
- Modify (full rewrite): `test/e2e/cassandra/chainsaw-test.yaml`

- [ ] **Step 1: Rewrite the Chainsaw test**

Replace the entire contents of `test/e2e/cassandra/chainsaw-test.yaml` with:

```yaml
# Chainsaw test: Cassandra default (persistence) store + OpenSearch visibility
# store -- the typical production layout.
#
# Prerequisites (installed by the e2e workflow): the temporal-operator and its
# CRDs. No Cassandra or OpenSearch Kubernetes operator is required; both backends
# are plain workloads provisioned by this suite. Cassandra is slow to cold-start
# on kind, so the assert timeout is generous.
apiVersion: chainsaw.kyverno.io/v1alpha1
kind: Test
metadata:
  name: cassandra
spec:
  timeouts:
    apply: 2m
    assert: 10m
  steps:
    - name: provision-backends
      try:
        - apply:
            file: 01-cassandra.yaml
        - apply:
            file: 03-opensearch.yaml
        - assert:
            file: 01-assert.yaml
        - assert:
            file: 03-opensearch-assert.yaml
    - name: create-keyspace
      try:
        - apply:
            file: 02-keyspace.yaml
        - assert:
            resource:
              apiVersion: batch/v1
              kind: Job
              metadata:
                name: create-cassandra-keyspace
              status:
                succeeded: 1
    - name: cluster-cassandra-opensearch
      try:
        - apply:
            file: 04-temporalcluster.yaml
        - assert:
            file: 04-assert.yaml
      catch:
        - describe:
            apiVersion: temporal.bmor10.com/v1alpha1
            kind: TemporalCluster
        - events: {}
```

- [ ] **Step 2: Validate the Chainsaw test parses**

Run: `./bin/chainsaw lint test --file test/e2e/cassandra/chainsaw-test.yaml`
Expected: no errors. (If `chainsaw lint` subcommand differs in this version, run `./bin/chainsaw lint --help` to find the correct form, or skip — the live run in Task 7 covers validation.)

- [ ] **Step 3: Commit**

```bash
git add test/e2e/cassandra/chainsaw-test.yaml
git commit -s -m "test(e2e): orchestrate Cassandra + OpenSearch chainsaw steps"
```

---

## Task 6: Wire the suite into CI

**Files:**
- Modify: `.github/workflows/e2e.yml`

The `matrix` job builds a JSON combos list; the image-prepull step harvests images from the suite dir. Add a `cassandra` combo and `cassandra:*` harvesting. Cassandra stays nightly/dispatch-only (not in the fast PR combo).

- [ ] **Step 1: Add the `cassandra` combo definition and include it in schedule/all**

In `.github/workflows/e2e.yml`, in the `matrix` job's `run:` block, locate:

```bash
          opensearch='{"temporal":"1.31.1","persistence":"opensearch","suite":"opensearch"}'
          if [ "$EVENT" = "schedule" ]; then
            echo "combos=[$postgres,$devserver,$mtls,$upgrade,$opensearch]" >> "$GITHUB_OUTPUT"
```

Replace with:

```bash
          opensearch='{"temporal":"1.31.1","persistence":"opensearch","suite":"opensearch"}'
          cassandra='{"temporal":"1.31.1","persistence":"cassandra","suite":"cassandra"}'
          if [ "$EVENT" = "schedule" ]; then
            echo "combos=[$postgres,$devserver,$mtls,$upgrade,$opensearch,$cassandra]" >> "$GITHUB_OUTPUT"
```

- [ ] **Step 2: Add the dispatch case branch and the `all` branch**

In the same block, locate the `workflow_dispatch` case statement:

```bash
              opensearch) echo "combos=[$opensearch]" >> "$GITHUB_OUTPUT" ;;
              all)        echo "combos=[$postgres,$devserver,$mtls,$upgrade,$opensearch]" >> "$GITHUB_OUTPUT" ;;
```

Replace with:

```bash
              opensearch) echo "combos=[$opensearch]" >> "$GITHUB_OUTPUT" ;;
              cassandra)  echo "combos=[$cassandra]" >> "$GITHUB_OUTPUT" ;;
              all)        echo "combos=[$postgres,$devserver,$mtls,$upgrade,$opensearch,$cassandra]" >> "$GITHUB_OUTPUT" ;;
```

- [ ] **Step 3: Add `cassandra` to the dispatch input options**

Near the top of the file, locate:

```yaml
        options: [default, devserver, mtls, upgrade, opensearch, all]
```

Replace with:

```yaml
        options: [default, devserver, mtls, upgrade, opensearch, cassandra, all]
```

- [ ] **Step 4: Harvest the Cassandra image for kind pre-pull**

In the "Pre-pull and load Temporal images" step, locate:

```bash
          # OpenSearch (Docker Hub, rate-limited for the kind kubelet) used by
          # the opensearch suite's visibility store.
          images="$images $(grep -rhoE 'opensearchproject/[a-zA-Z0-9-]+:[^ "}]+' "$root" || true)"
```

Insert immediately after it:

```bash
          # Cassandra (Docker Hub, rate-limited for the kind kubelet) used by the
          # cassandra suite's default store and keyspace-bootstrap Job.
          images="$images $(grep -rhoE 'cassandra:[0-9][^ "}]*' "$root" || true)"
```

- [ ] **Step 5: Validate the workflow YAML**

Run: `python -c "import yaml,sys; yaml.safe_load(open('.github/workflows/e2e.yml'))" && echo OK`
Expected: `OK`.

- [ ] **Step 6: Sanity-check the matrix shell logic locally**

Run:
```bash
EVENT=schedule bash -c '
postgres=p; devserver=d; mtls=m; upgrade=u; opensearch=o; cassandra=c
if [ "$EVENT" = "schedule" ]; then echo "combos=[$postgres,$devserver,$mtls,$upgrade,$opensearch,$cassandra]"; fi'
```
Expected: `combos=[p,d,m,u,o,c]` (confirms the cassandra var is wired into the schedule list).

- [ ] **Step 7: Commit**

```bash
git add .github/workflows/e2e.yml
git commit -s -m "ci: add cassandra suite to nightly e2e matrix"
```

---

## Task 7: End-to-end validation

**Files:** none (validation only)

- [ ] **Step 1: Confirm the suite directory is self-contained and consistent**

Run:
```bash
ls test/e2e/cassandra
grep -rn "k8ssandra\|K8ssandra\|01-k8ssandra" test/e2e/cassandra || echo "no k8ssandra refs"
```
Expected files: `01-cassandra.yaml`, `01-assert.yaml`, `02-keyspace.yaml`, `03-opensearch.yaml`, `03-opensearch-assert.yaml`, `04-temporalcluster.yaml`, `04-assert.yaml`, `chainsaw-test.yaml`. Expected: `no k8ssandra refs`.

- [ ] **Step 2: Run the suite against a live cluster (if available)**

Prereq: a kind cluster with cert-manager, CNPG, and the operator installed (mirror `.github/workflows/e2e.yml`, or use `hack/nsc-e2e.sh` if applicable).

Run: `./bin/chainsaw test --test-dir test/e2e/cassandra --config .chainsaw.yaml`
Expected: all three steps pass; the TemporalCluster reaches `SchemaReady=True` and `Ready=True`.

If no live cluster is available in this environment, document that the suite was statically validated (YAML parses, dry-run applies clean) and will be exercised by the nightly CI run / a manual `workflow_dispatch` with `suite: cassandra`.

- [ ] **Step 3: Final commit (if Step 2 surfaced fixes)**

```bash
git add -A
git commit -s -m "test(e2e): finalize Cassandra + OpenSearch suite"
```

---

## Self-review checklist (completed during planning)

- **Spec coverage:** Cassandra StatefulSet (Task 1), keyspace bootstrap (Task 2), OpenSearch backend copied in-dir (Task 3), Cassandra+OpenSearch TemporalCluster (Task 4), step orchestration with generous timeouts (Task 5), CI matrix + dispatch + `cassandra:*` harvest, no extra operator install (Task 6), removal of K8ssandra fixture (Task 1 Step 1; verified Task 7), namespace-qualified FQDNs (Task 4), no-auth Cassandra (Task 1/Task 4). All spec sections map to a task.
- **Placeholder scan:** no TBD/TODO; every fixture and workflow edit shows full content.
- **Name consistency:** Cassandra Service/StatefulSet `temporal-cass`; keyspace Job `create-cassandra-keyspace` (asserted by exact name in Task 5); OpenSearch Deployment/Service `temporal-os`; TemporalCluster `temporal-cass`; fixture numeric prefixes match Chainsaw step order.
