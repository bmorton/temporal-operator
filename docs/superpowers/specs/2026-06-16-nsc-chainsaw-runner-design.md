# Design: nsc-based Chainsaw e2e runner

**Date:** 2026-06-16
**Status:** Approved (pending implementation)

## Problem

Chainsaw e2e suites under `test/e2e/` require a real Kubernetes cluster. CI
runs them on a local [kind](https://kind.sigs.k8s.io/) cluster
(`.github/workflows/e2e.yml`). kind cannot run inside this devcontainer, so
coding agents (and developers) have no way to validate chainsaw suites before
pushing to CI.

The [`nsc`](https://cloud.namespace.so/) CLI (namespace.so) is installed and
authenticated in the devcontainer and can provision ephemeral Kubernetes
clusters on demand. This design adds an alternate chainsaw runner that
provisions an nsc cluster, runs a suite against it, and reliably destroys the
cluster afterward so the account is not billed for idle infrastructure.

## Goals

- Run any `test/e2e/<suite>` chainsaw suite from the devcontainer without kind.
- Build and distribute the operator image to the remote cluster without a local
  Docker daemon.
- Guarantee cluster teardown (billing safety) even on crash, kill, or Ctrl-C.
- Provide a label-scoped purge command to clean up any leftover clusters.

## Non-goals

- Replacing kind in CI. CI stays on kind as-is. The runner is structured so CI
  *could* adopt nsc later with minimal change, but that migration is out of
  scope here.
- Changing the chainsaw suites themselves or the operator's behavior.

## Relationship to CI

Local-first, CI-migration-ready (option B from brainstorming). The nsc runner
is a parallel path used by agents/developers. The existing kind-based
`.github/workflows/e2e.yml` is unchanged. The orchestration logic is packaged so
a future CI job could call the same script.

## Approach

Single orchestration shell script plus thin Makefile wrappers. The
cleanup-critical logic (traps, instance-id capture, error handling) lives in
bash, where `trap` actually works reliably; the Makefile remains the familiar
entrypoint.

## Components

### Files & interface

- **`hack/nsc-e2e.sh`** — the orchestrator. Owns the full lifecycle and trap
  cleanup.
- **Makefile targets** (thin wrappers):
  - `chainsaw-test-nsc` — runs `hack/nsc-e2e.sh`, honoring `SUITE=` (default
    `postgres/lifecycle`).
  - `nsc-clean` — label-filtered purge of all of our clusters.
- **Config via env vars** (with defaults):
  - `SUITE=postgres/lifecycle`
  - `NSC_DURATION=30m`
  - `NSC_K8S_VERSION=1.33`
  - `NSC_LABEL=app=temporal-operator-e2e`
  - `TAG` — unique per run (e.g. `git rev-parse --short HEAD` + timestamp) to
    avoid stale image caching.

### Registry

The nscr.io workspace registry is derived dynamically from
`nsc workspace describe` (e.g. `nscr.io/<tenant>`); it is **not** hardcoded. The
nsc Kubernetes cluster has native pull access to nscr.io (expected: no
imagePullSecrets required).

## Script lifecycle (`hack/nsc-e2e.sh`)

1. `set -euo pipefail`. **Preflight**: `nsc auth check-login`; ensure
   `chainsaw` (via `make chainsaw`), `kubectl`, and `helm` are present. Fail
   fast with clear messages.
2. Derive registry `REG=nscr.io/<tenant>` from `nsc workspace describe`.
3. **Build + push operator image remotely**:
   `nsc build -f Dockerfile -t $REG/temporal-operator:$TAG --push`. Runs the
   build on Namespace's remote builder — no local Docker daemon needed (the key
   enabler for this devcontainer).
4. **Create cluster**:
   `nsc create --enable=kubernetes:$NSC_K8S_VERSION --ephemeral
   --duration $NSC_DURATION --label $NSC_LABEL --label run=<id>
   --wait_kube_system --cidfile $CIDFILE`. The cleanup trap is armed **before**
   create so a partially-created instance is still destroyed.
5. **Arm cleanup**: `trap cleanup EXIT INT TERM`. `cleanup` destroys the
   instance via `$CIDFILE` if non-empty.
6. **Kubeconfig**: `nsc kubeconfig write <id>` into a temp file; export
   `KUBECONFIG`.
7. **Install dependencies**: cert-manager + CloudNativePG, using the same
   manifests and version pins as `.github/workflows/e2e.yml`; wait for rollouts.
8. **Helm-install operator** with
   `manager.image.repository=$REG/temporal-operator` and
   `manager.image.tag=$TAG`. A successful deployment rollout is the
   **verification that nscr.io pull access works** from the cluster.
9. **Run chainsaw**:
   `chainsaw test --test-dir test/e2e/$SUITE --config .chainsaw.yaml`.
10. On any exit, the trap destroys the cluster.

## Cleanup / billing safety (three layers)

- **Layer 1 — auto-expiry (crash-proof backstop):** `--ephemeral --duration
  30m`. Even if the devcontainer dies or the agent process is killed, the
  platform destroys the cluster when the duration elapses.
- **Layer 2 — trap cleanup (fast path):** `trap cleanup EXIT INT TERM` calls
  `nsc destroy "$(cat $CIDFILE)" --force` on success, failure, or interrupt, so
  no idle time is billed after a run.
- **Layer 3 — purge command:** `make nsc-clean` runs
  `nsc list --all -o json | jq -r '.[] | select(.labels.app=="temporal-operator-e2e") | .cluster_id'`
  and `nsc destroy --force`s each. Strictly label-scoped, so it never touches
  the `nsc.purpose: builder` instance (the remote build cluster) or any
  unrelated instance.

## Error handling & diagnostics

- `set -euo pipefail` throughout; preflight fails fast.
- On install/chainsaw **failure**, before teardown, collect diagnostics
  (mirroring CI): `kubectl get all -A`, `kubectl get temporalcluster -A -o yaml`,
  and operator deployment logs, written to `./artifacts/`, so failures can be
  debugged after the cluster is gone.
- The trap guarantees teardown regardless of where a failure occurs.

## Risks to verify on first real run

1. **nscr.io pull access** from a fresh nsc Kubernetes cluster without
   imagePullSecrets. Expected to work natively; fallback is to wire a registry
   pull secret into the operator namespace.
2. **Docker Hub rate limits** for `temporalio/server` / `temporalio/admin-tools`
   on nsc node egress. The kind side-load workaround is dropped; verify the
   cluster's kubelet can pull these directly.
3. **Kubernetes version** `1.33` is offered by `nsc create --enable`.
4. **cert-manager `latest` manifest + CNPG pin** behave the same on nsc as on
   kind.

## Validation

This is infrastructure glue, so validation is one real end-to-end run during
implementation:

1. Run `make chainsaw-test-nsc` and confirm the `postgres/lifecycle` suite
   passes.
2. Confirm `nsc list --all` shows no leftover cluster afterward (trap cleanup
   worked).
3. Confirm `make nsc-clean` is a no-op when nothing is leftover, and correctly
   destroys a deliberately orphaned cluster while leaving the builder instance
   intact.

That single run exercises build → create → install → test → destroy and all
cleanup paths.

## Documentation

Add a short "Running e2e on Namespace (nsc)" section to `CONTRIBUTING.md`
documenting `make chainsaw-test-nsc`, the `SUITE=`/duration env vars, and
`make nsc-clean`.
