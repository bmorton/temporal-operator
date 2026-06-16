# OpenSearch advanced-visibility e2e coverage — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a CI-running Chainsaw e2e suite that exercises the operator's Elasticsearch/OpenSearch visibility-store code path against a single-node OpenSearch 2.x cluster, with a Postgres default store.

**Architecture:** A new `test/e2e/opensearch/` suite stands up a plain single-node OpenSearch Deployment + Service (no OpenSearch operator), reuses the existing CNPG Postgres fixture as the default store, and creates a `TemporalCluster` whose visibility store points at OpenSearch via the existing `elasticsearch` config (`version: v8`). It asserts the operator's `SchemaReady` and `Ready` conditions. No operator/Go code changes — Temporal's `olivere/elastic/v7` client (used for both `v7` and `v8`) is created with healthcheck/sniff disabled and is wire-compatible with OpenSearch 2.x.

**Tech Stack:** Kubernetes, Chainsaw (kyverno) e2e tests, CloudNativePG, OpenSearch 2.19.5, Temporal 1.31.1, GitHub Actions.

**Reference spec:** `docs/superpowers/specs/2026-06-15-opensearch-e2e-design.md`

---

## File Structure

All new files live under `test/e2e/opensearch/`, mirroring the existing
`test/e2e/elasticsearch/` suite layout:

- **Create** `test/e2e/opensearch/01-opensearch.yaml` — single-node OpenSearch
  `Deployment` + `Service` (`temporal-os`). Sole responsibility: provision the
  visibility backend.
- **Create** `test/e2e/opensearch/01-assert.yaml` — asserts the OpenSearch
  `Deployment` is available.
- **Create** `test/e2e/opensearch/02-temporalcluster.yaml` — the `TemporalCluster`
  with Postgres default store + OpenSearch visibility store.
- **Create** `test/e2e/opensearch/02-assert.yaml` — asserts the cluster's
  `SchemaReady` and `Ready` conditions are `True`.
- **Create** `test/e2e/opensearch/chainsaw-test.yaml` — orchestrates the steps.
- **Modify** `.github/workflows/e2e.yml` — add the `opensearch` combo to the
  scheduled (non-PR) matrix and side-load the OpenSearch image in the pre-pull
  step.

Reused as-is (not modified): `test/e2e/postgres/01-fixtures-cnpg.yaml`.

---

## Task 1: OpenSearch Deployment + Service fixture

**Files:**
- Create: `test/e2e/opensearch/01-opensearch.yaml`
- Create: `test/e2e/opensearch/01-assert.yaml`

- [ ] **Step 1: Create the OpenSearch Deployment + Service**

Create `test/e2e/opensearch/01-opensearch.yaml` with this exact content:

```yaml
# Single-node OpenSearch 2.x used as the Temporal visibility store. No OpenSearch
# Kubernetes operator is required: a plain Deployment + Service is enough for a
# disposable e2e backend.
#
# Security is fully disabled (plain HTTP, no auth, no TLS) so the operator's
# esBackend can probe /_cluster/health and apply the visibility index template
# without credentials. compatibility.override_main_response_version keeps
# Elasticsearch clients (Temporal uses olivere/elastic v7) from rejecting the
# version handshake. The mmap and disk-watermark settings keep single-node
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
            - name: plugins.security.disabled
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

- [ ] **Step 2: Create the readiness assertion**

Create `test/e2e/opensearch/01-assert.yaml` with this exact content:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: temporal-os
status:
  availableReplicas: 1
```

- [ ] **Step 3: Validate the manifests parse**

Run:
```bash
kubectl apply --dry-run=client -f test/e2e/opensearch/01-opensearch.yaml
```
Expected: prints `deployment.apps/temporal-os created (dry run)` and
`service/temporal-os created (dry run)`, exit 0. (No live cluster contact —
`--dry-run=client` only validates structure. A reachable cluster is not
required for this step, but a kube context must be configured; if none is, skip
this step and rely on Task 6's live run.)

- [ ] **Step 4: Commit**

```bash
git add test/e2e/opensearch/01-opensearch.yaml test/e2e/opensearch/01-assert.yaml
git commit -s -m "test(e2e): add single-node OpenSearch fixture for visibility suite"
```

---

## Task 2: TemporalCluster + assertion fixtures

**Files:**
- Create: `test/e2e/opensearch/02-temporalcluster.yaml`
- Create: `test/e2e/opensearch/02-assert.yaml`

- [ ] **Step 1: Create the TemporalCluster fixture**

Create `test/e2e/opensearch/02-temporalcluster.yaml` with this exact content:

```yaml
# Temporal cluster with a Postgres default store (CNPG) and an OpenSearch
# visibility store. The visibility store uses the operator's elasticsearch config
# with version v8; Temporal's olivere/elastic v7 client (used for both v7 and v8)
# is healthcheck/sniff-disabled and works against OpenSearch 2.x.
#
# The visibility URL is a namespace-qualified FQDN in host:port form (no scheme):
# the operator probes the store from the temporal-system namespace, so a bare
# service name would not resolve, and the absence of a tls block yields an http
# scheme in the rendered Temporal config.
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: temporal-os
spec:
  version: "1.31.1"
  numHistoryShards: 512
  persistence:
    defaultStore:
      sql:
        pluginName: postgres12
        host: (join('.', ['temporal-pg-rw', $namespace, 'svc.cluster.local']))
        port: 5432
        database: temporal
        user: temporal
        passwordSecretRef:
          name: temporal-pg-app
          key: password
    visibilityStore:
      elasticsearch:
        url: (join('.', ['temporal-os', $namespace, 'svc.cluster.local:9200']))
        version: v8
```

- [ ] **Step 2: Create the cluster assertion**

Create `test/e2e/opensearch/02-assert.yaml` with this exact content:

```yaml
apiVersion: temporal.bmor10.com/v1alpha1
kind: TemporalCluster
metadata:
  name: temporal-os
status:
  (conditions[?type == 'SchemaReady'].status | [0]): "True"
  (conditions[?type == 'Ready'].status | [0]): "True"
```

- [ ] **Step 3: Validate the TemporalCluster manifest parses**

The `(join(...))` expressions are Chainsaw bindings, not valid standalone YAML
values for `kubectl`, so validate YAML well-formedness instead:

Run:
```bash
python3 -c "import yaml,sys; list(yaml.safe_load_all(open('test/e2e/opensearch/02-temporalcluster.yaml'))); list(yaml.safe_load_all(open('test/e2e/opensearch/02-assert.yaml'))); print('ok')"
```
Expected: prints `ok`, exit 0.

- [ ] **Step 4: Commit**

```bash
git add test/e2e/opensearch/02-temporalcluster.yaml test/e2e/opensearch/02-assert.yaml
git commit -s -m "test(e2e): add TemporalCluster fixture with OpenSearch visibility"
```

---

## Task 3: Chainsaw test orchestration

**Files:**
- Create: `test/e2e/opensearch/chainsaw-test.yaml`

- [ ] **Step 1: Create the Chainsaw test**

Create `test/e2e/opensearch/chainsaw-test.yaml` with this exact content:

```yaml
# Chainsaw test: OpenSearch 2.x visibility store (plain single-node Deployment)
# with a Postgres default store from the postgres suite.
#
# Prerequisite: install the CNPG operator (postgresql.cnpg.io CRDs). No
# OpenSearch operator is needed. ../postgres/02-secrets.yaml is intentionally not
# applied: it only creates the temporal_visibility Postgres database, which is
# unused when visibility is served by OpenSearch.
apiVersion: chainsaw.kyverno.io/v1alpha1
kind: Test
metadata:
  name: opensearch
spec:
  timeouts:
    apply: 2m
    assert: 10m
  steps:
    - name: provision-deps
      try:
        - apply:
            file: ../postgres/01-fixtures-cnpg.yaml
        - apply:
            file: 01-opensearch.yaml
        - assert:
            file: 01-assert.yaml
    - name: cluster-os-visibility
      try:
        - apply:
            file: 02-temporalcluster.yaml
        - assert:
            file: 02-assert.yaml
```

- [ ] **Step 2: Validate the Chainsaw test YAML parses**

Run:
```bash
python3 -c "import yaml; yaml.safe_load(open('test/e2e/opensearch/chainsaw-test.yaml')); print('ok')"
```
Expected: prints `ok`, exit 0.

- [ ] **Step 3: Commit**

```bash
git add test/e2e/opensearch/chainsaw-test.yaml
git commit -s -m "test(e2e): wire OpenSearch visibility chainsaw test"
```

---

## Task 4: CI wiring — add OpenSearch to the scheduled e2e matrix

**Files:**
- Modify: `.github/workflows/e2e.yml` (matrix combos, ~line 25)

- [ ] **Step 1: Add the OpenSearch combo to the scheduled matrix**

In `.github/workflows/e2e.yml`, find the scheduled-matrix line (the `if [ ... = "schedule" ]` branch):

```yaml
            echo 'combos=[{"temporal":"1.31.1","persistence":"postgres","suite":"postgres/lifecycle"},{"temporal":"1.31.1","persistence":"mtls","suite":"mtls"},{"temporal":"1.30.0","persistence":"upgrade","suite":"upgrade"}]' >> "$GITHUB_OUTPUT"
```

Replace it with (adds the `opensearch` combo; the PR-only `else` branch is left
unchanged so PR runs stay fast):

```yaml
            echo 'combos=[{"temporal":"1.31.1","persistence":"postgres","suite":"postgres/lifecycle"},{"temporal":"1.31.1","persistence":"mtls","suite":"mtls"},{"temporal":"1.30.0","persistence":"upgrade","suite":"upgrade"},{"temporal":"1.31.1","persistence":"opensearch","suite":"opensearch"}]' >> "$GITHUB_OUTPUT"
```

- [ ] **Step 2: Verify the workflow YAML is still valid**

Run:
```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/e2e.yml')); print('ok')"
```
Expected: prints `ok`, exit 0.

- [ ] **Step 3: Verify both JSON combo arrays parse**

Run:
```bash
grep -oE "combos=\[.*\]" .github/workflows/e2e.yml | sed 's/^combos=//' | while read -r line; do echo "$line" | python3 -c "import json,sys; json.load(sys.stdin); print('valid json')"; done
```
Expected: prints `valid json` twice (once per branch), exit 0.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/e2e.yml
git commit -s -m "ci(e2e): run the OpenSearch visibility suite in the scheduled matrix"
```

---

## Task 5: CI wiring — side-load the OpenSearch image into kind

**Files:**
- Modify: `.github/workflows/e2e.yml` (the "Pre-pull and load Temporal images" step, ~lines 97-99)

The kind kubelet's anonymous Docker Hub pulls are rate-limited, so suite images
are pre-pulled on the runner and imported into the kind node. The existing step
only collects `temporalio/*` images; OpenSearch lives on Docker Hub too and must
be added.

- [ ] **Step 1: Add the OpenSearch image to the pre-pull image list**

In `.github/workflows/e2e.yml`, find this block inside the "Pre-pull and load
Temporal images" step:

```bash
          # Pick up any images referenced literally in the fixtures/scripts
          # (e.g. admin-tools health checks or pinned UI images).
          images="$images $(grep -rhoE 'temporalio/[a-zA-Z0-9-]+:[^ "}]+' "$root" || true)"
```

Replace it with (adds a second `grep` that picks up the pinned OpenSearch image
from the suite fixtures):

```bash
          # Pick up any images referenced literally in the fixtures/scripts
          # (e.g. admin-tools health checks or pinned UI images).
          images="$images $(grep -rhoE 'temporalio/[a-zA-Z0-9-]+:[^ "}]+' "$root" || true)"
          # OpenSearch (Docker Hub, rate-limited for the kind kubelet) used by
          # the opensearch suite's visibility store.
          images="$images $(grep -rhoE 'opensearchproject/[a-zA-Z0-9-]+:[^ "}]+' "$root" || true)"
```

- [ ] **Step 2: Verify the workflow YAML is still valid**

Run:
```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/e2e.yml')); print('ok')"
```
Expected: prints `ok`, exit 0.

- [ ] **Step 3: Verify the grep matches the fixture image**

Run:
```bash
grep -rhoE 'opensearchproject/[a-zA-Z0-9-]+:[^ "}]+' test/e2e/opensearch
```
Expected: prints `opensearchproject/opensearch:2.19.5`, exit 0.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/e2e.yml
git commit -s -m "ci(e2e): pre-load the OpenSearch image into the kind node"
```

---

## Task 6: End-to-end verification on a local kind cluster

This is the real integration test for the suite. It requires Docker and the
ability to create a kind cluster in the dev environment. If that is unavailable,
skip and rely on the scheduled CI run, but note that the suite is then unverified
locally.

**Files:** none (verification only)

- [ ] **Step 1: Create the kind cluster and load the operator image**

Run:
```bash
make install-tools
export PATH="$PWD/bin:$PATH"
make kind-up
make docker-build IMG=temporal-operator:e2e
make kind-load IMG=temporal-operator:e2e
```
Expected: kind cluster `temporal-operator-test-e2e` created; operator image
built and loaded; exit 0.

- [ ] **Step 2: Install cert-manager and CloudNativePG**

Run:
```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml
kubectl apply --server-side -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.24/releases/cnpg-1.24.0.yaml
kubectl -n cert-manager rollout status deploy/cert-manager-webhook --timeout=180s
kubectl -n cnpg-system rollout status deploy/cnpg-controller-manager --timeout=180s
```
Expected: both deployments report `successfully rolled out`, exit 0.

- [ ] **Step 3: Install the operator via Helm**

Run:
```bash
helm install temporal-operator dist/chart \
  --namespace temporal-system --create-namespace \
  --set manager.image.repository=temporal-operator \
  --set manager.image.tag=e2e \
  --set manager.image.pullPolicy=Never
kubectl -n temporal-system rollout status deploy/temporal-operator-controller-manager --timeout=180s
```
Expected: `temporal-operator-controller-manager` rolled out, exit 0.

- [ ] **Step 4: Side-load the OpenSearch and Temporal images into kind**

Run:
```bash
for img in opensearchproject/opensearch:2.19.5 temporalio/server:1.31.1 temporalio/admin-tools:1.31.1; do
  docker pull --platform linux/amd64 "$img"
  docker save "$img" | docker exec -i temporal-operator-test-e2e-control-plane \
    ctr --namespace=k8s.io images import --digests --snapshotter=overlayfs -
done
```
Expected: each image pulled and imported (`unpacking ...done`), exit 0.

- [ ] **Step 5: Run the OpenSearch Chainsaw suite**

Run:
```bash
./bin/chainsaw test --test-dir test/e2e/opensearch --config .chainsaw.yaml
```
Expected: the `opensearch` test passes — the OpenSearch Deployment becomes
available, and the `TemporalCluster` reaches `SchemaReady=True` and `Ready=True`.
Final output: `Tests: 1 passed, 0 failed`, exit 0.

- [ ] **Step 6 (on failure): Diagnose**

If the suite fails, gather diagnostics before changing fixtures:
```bash
kubectl -n temporal-system logs deploy/temporal-operator-controller-manager --tail=200
# Find the test namespace (chainsaw-<random>) and inspect the cluster + pods:
kubectl get temporalcluster -A
kubectl get pods -A | grep -E 'temporal-os|temporal-pg'
```
Common causes and fixes:
- `PersistenceUnreachable` on the visibility store → confirm the `url` is the
  namespace-qualified FQDN `temporal-os.<ns>.svc.cluster.local:9200` (the operator
  probes from `temporal-system`).
- OpenSearch pod `CrashLoopBackOff` / not ready → check it isn't OOMing (raise
  `OPENSEARCH_JAVA_OPTS`) or blocked by `vm.max_map_count` (the
  `node.store.allow_mmap=false` env must be present).
- Index write rejected / read-only → confirm
  `cluster.routing.allocation.disk.threshold_enabled=false` is set (kind node
  disk pressure).

- [ ] **Step 7: Tear down**

Run:
```bash
make kind-down
```
Expected: kind cluster deleted, exit 0.

---

## Self-Review Notes

- **Spec coverage:** Every spec section maps to a task — OpenSearch fixture
  (Task 1), TemporalCluster + assert (Task 2), chainsaw orchestration (Task 3),
  scheduled-matrix combo (Task 4), image pre-pull (Task 5), verification
  (Task 6). The "no operator code change" finding is reflected by the absence of
  any Go task. The decision to drop `02-secrets.yaml` is encoded in Task 3.
- **No placeholders:** every file's full content is inline; every command has
  expected output.
- **Name consistency:** the Deployment/Service/TemporalCluster are all named
  `temporal-os`; the visibility URL and the pre-pull grep both target
  `opensearchproject/opensearch:2.19.5`; the CNPG service `temporal-pg-rw` and
  secret `temporal-pg-app` match `test/e2e/postgres/01-fixtures-cnpg.yaml`.
