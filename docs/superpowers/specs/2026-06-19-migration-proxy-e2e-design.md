# Migration proxy e2e: dev-server → cluster migration

**Status:** Approved (design)
**Date:** 2026-06-19
**Related:** PR #69 (TemporalMigration upgrade proxy, alpha); issue #82 (chart generation)

## Problem

The `TemporalMigration` upgrade proxy (PR #69) has unit coverage for routing
classification, config rendering, and TLS, but no end-to-end test that actually
brings up two Temporal clusters, provisions the proxy, and proves that workflow
traffic transitions from the source to the target across a cutover. We want a
"super lightweight" e2e suite that exercises the real reconcile → proxy →
workflow path.

## Constraints discovered

1. **`targetRef` must be a `TemporalCluster`.** The controller resolves the
   target via `var cluster temporalv1alpha1.TemporalCluster` and
   `frontendAddress(cluster)` (`internal/controller/temporalmigration_controller.go:81-82`,
   `temporalmigration_config.go:58`). The **source** is just an `address`
   string, so a `TemporalDevServer` works as the source. A literal
   dev-server → dev-server migration is therefore not supported today.
2. **Workers cannot poll the target through the proxy.** `Director.Route`
   sends `ClassPoll` (`PollWorkflowTaskQueue`/`PollActivityTaskQueue`) to the
   **source** in both passthrough and cutover modes
   (`internal/proxy/director.go:58-59`). Workers must connect **directly** to
   each cluster's frontend; only the client/starter goes through the proxy.
3. **Routing semantics under cutover** (`director.go:48-63`):
   passthrough → everything to source; cutover → `Start`=target (no fallback),
   `Existing`=target then source fallback, `Poll`=source, other=target.
4. The operator Go module depends on `go.temporal.io/api` but **not** the
   worker SDK `go.temporal.io/sdk`. To avoid pulling the heavy worker SDK into
   the operator module, the runner is a **separate Go module** (mirroring
   `hack/tools/go.mod`).

## Decisions

- **Topology (Option A):** source = `TemporalDevServer` (embedded SQLite),
  target = `TemporalCluster` on CNPG Postgres.
- **Assertion depth (Option A):** routing transition only — a workflow started
  via the proxy before cutover executes on the **source**; a workflow started
  after cutover executes on the **target**. No drain/`status.phase: Complete`
  or existing-workflow fallback in this iteration.
- **Runner packaging (Option A):** dedicated runner image built from a separate
  Go module under `test/e2e/migration/runner/`, `kind load`ed via a
  migration-suite-specific step in `e2e.yml`.
- **CI cadence (Option A):** nightly (`schedule`) + manual dispatch only; not in
  the per-PR default combo.
- **No product-code changes.** Test-only. Do **not** run `make helm-chart`
  (see issue #82); the chart is hand-maintained.

## Architecture

All resources live in one ephemeral chainsaw namespace.

```
                         ┌───────────────────────────┐
        client Job  ───► │  migration-proxy (Deploy)  │
   (through proxy)       │  proxyEndpoint :7233       │
                         └──────────┬───────┬─────────┘
                   passthrough/poll │       │ cutover: Start→target
                                    ▼       ▼
        ┌──────────────────────────────┐  ┌──────────────────────────────┐
        │ source: TemporalDevServer    │  │ target: TemporalCluster       │
        │ source-devserver:7233        │  │ <name>-frontend:7233          │
        └──────────────┬───────────────┘  └───────────────┬──────────────┘
                       │ direct                            │ direct
              ┌────────▼────────┐                 ┌────────▼────────┐
              │ source-worker    │                 │ target-worker    │
              │ (Deployment)     │                 │ (Deployment)     │
              │ cluster="source" │                 │ cluster="target" │
              └──────────────────┘                 └──────────────────┘
```

### Components

**Runner (`test/e2e/migration/runner/`, own Go module + Dockerfile)**

A single binary `migration-runner` (image `migration-runner:e2e`) with two
subcommands:

- `worker --address <frontend> --namespace <ns> --task-queue <tq> --cluster <label>`
  Connects **directly** to the given frontend, registers a trivial `Echo`
  workflow that returns its configured `--cluster` label, and runs the worker
  until terminated. One Deployment per cluster (`cluster=source`,
  `cluster=target`).

- `client --address <proxyEndpoint> --namespace <ns> --task-queue <tq> --workflow-id <id> --expect <label>`
  Connects through the **proxy**, starts the `Echo` workflow, waits for the
  result, and asserts `result == expect`. Exits non-zero on mismatch or
  timeout. One Job per phase.

The workflow returning the executing worker's cluster label is how the client
proves *where* the workflow ran, without querying each cluster directly.

Notes:
- Both subcommands use the same `--task-queue` and `--namespace` (`default`,
  which the dev server and cluster provide out of the box).
- Connection retries/backoff are built into the runner so worker Deployments
  and client Jobs tolerate frontends that are still warming up.

### Test flow (`test/e2e/migration/chainsaw-test.yaml`)

Mirrors the `postgres/lifecycle` and `devserver` suites' chainsaw style.

1. **Provision Postgres:** apply `../postgres/01-fixtures-cnpg.yaml` and
   `../postgres/02-secrets.yaml`; assert CNPG `Cluster` ready and the
   visibility-DB Job succeeded. (Reuse the existing shared fixtures.)
2. **Bring up clusters:** apply the source `TemporalDevServer` and the target
   `TemporalCluster` (Postgres host templated with `$namespace`); assert both
   reach `Ready=True`.
3. **Provision proxy:** apply the `TemporalMigration` (`cutover: false`);
   assert `status.proxyEndpoint` is populated and `status.phase: Passthrough`.
4. **Deploy workers:** apply source-worker + target-worker Deployments
   (direct frontends); assert both `availableReplicas: 1`.
5. **Passthrough assertion:** apply the client Job with `--expect source`;
   assert Job `status.succeeded: 1`.
6. **Cutover:** `kubectl patch temporalmigration <name> --type merge
   -p '{"spec":{"cutover":true}}'`; assert `status.phase: Cutover`.
7. **Cutover assertion:** apply the client Job with `--expect target`;
   assert Job `status.succeeded: 1`.

`catch` blocks: `describe` the `TemporalMigration`, Deployments, and Jobs, and
`podLogs` for the proxy and worker pods.

Chainsaw timeouts follow the cluster suites (apply 1m, assert 5m); the client
Jobs get a generous deadline to allow image pulls and frontend warm-up.

### CI wiring (`.github/workflows/e2e.yml`)

- Add to the matrix step:
  `migration='{"temporal":"1.31.1","persistence":"postgres","suite":"migration"}'`.
- Add `migration` to the `schedule` combos list, to the `workflow_dispatch`
  `case` options, and to the `all` list. **Not** added to the PR default
  (`[$postgres,$devserver]`).
- Add a suite-conditional build/load step before running chainsaw: when
  `matrix.combo.suite == migration`, `docker build` the runner image
  (`migration-runner:e2e`) from `test/e2e/migration/runner/` and `kind load`
  it, mirroring the existing operator-image build/load step. The Temporal
  server/admin-tools/devserver images are already harvested and side-loaded by
  the existing "Pre-pull and load Temporal images" step (it scans the suite
  directory for `version:` and `temporalio/...` references).

## Files

Added:
- `test/e2e/migration/chainsaw-test.yaml`
- `test/e2e/migration/01-source-devserver.yaml`
- `test/e2e/migration/02-target-temporalcluster.yaml` (+ assert)
- `test/e2e/migration/03-temporalmigration.yaml` (+ assert)
- `test/e2e/migration/04-workers.yaml` (+ assert)
- `test/e2e/migration/05-client-passthrough-job.yaml`
- `test/e2e/migration/06-client-cutover-job.yaml`
- `test/e2e/migration/runner/go.mod`
- `test/e2e/migration/runner/go.sum`
- `test/e2e/migration/runner/main.go`
- `test/e2e/migration/runner/Dockerfile`

Edited:
- `.github/workflows/e2e.yml`

(Exact fixture file numbering/splitting may be adjusted during implementation to
match chainsaw step ergonomics; the set of resources is fixed.)

## Testing

- The suite itself is the test. Locally it can be run against an existing kube
  context via `make chainsaw-test` (scoped to `test/e2e/migration`) once the
  runner image is built and loaded into the cluster.
- The runner module builds independently (`cd test/e2e/migration/runner && go
  build ./...`). It is excluded from the operator module's `go test ./...` by
  virtue of being a separate module.

## Out of scope (future iterations)

- Existing-workflow fallback assertions (`ClassExisting` → target/source).
- Drain status / `status.phase: Draining` / `Complete` and
  `status.draining` counts.
- TLS/mTLS source connections.
- Running the suite on every PR.
```
