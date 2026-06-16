# nsc-based Chainsaw e2e runner Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a script + Makefile targets that provision an ephemeral Namespace (nsc) Kubernetes cluster, run a Chainsaw e2e suite against it, and always destroy the cluster afterward — so chainsaw suites can be validated from a devcontainer where kind cannot run.

**Architecture:** A single bash orchestrator (`hack/nsc-e2e.sh`) owns the full lifecycle (remote image build/push → ephemeral cluster create → dependency + operator install → chainsaw run → guaranteed teardown via `trap`). Thin Makefile wrappers (`chainsaw-test-nsc`, `nsc-clean`) are the entrypoints. Billing safety is layered: `--ephemeral --duration` auto-expiry (crash-proof backstop), a `trap` that destroys on exit, and a label-scoped purge command.

**Tech Stack:** bash, GNU make, `nsc` CLI (namespace.so), Helm, kubectl, jq, kyverno/chainsaw.

**Spec:** `docs/superpowers/specs/2026-06-16-nsc-chainsaw-runner-design.md`

---

## Notes on testing approach

This feature is infrastructure glue (a shell script + make targets), not application
code with unit tests. "Tests" are therefore: (1) `bash -n` syntax validation, (2)
`make -n` dry-run inspection of the new targets, and (3) one real end-to-end run
against nsc that exercises build → create → install → chainsaw → destroy plus the
purge command. There is no unit-test harness for shell in this repo, so classic
red/green TDD does not apply; each task still ends with an explicit verification step.

## File Structure

- **Create `hack/nsc-e2e.sh`** — the orchestrator. Single responsibility: run one
  chainsaw suite against a freshly provisioned, guaranteed-destroyed nsc cluster.
- **Modify `Makefile`** — add `chainsaw-test-nsc` (run the script) and `nsc-clean`
  (label-scoped purge) targets near the existing `chainsaw-test` / `kind-*` targets.
- **Modify `CONTRIBUTING.md`** — add a "Run e2e on Namespace (nsc)" subsection.

Reference facts (verified against the live `nsc` CLI and namespace.so docs):
- Registry URL comes from `nsc workspace describe` line `Registry URL: nscr.io/<tenant>`.
- `nsc build -f Dockerfile -t <ref> --push` builds on a remote builder (no local Docker).
- `nsc create --enable=kubernetes:<ver> --ephemeral --duration <d> --label k=v --wait_kube_system --cidfile <file>` creates the cluster and writes its instance id to `<file>`.
- `nsc kubeconfig write <id> --output_to <file>` writes the **path** of the produced kubeconfig into `<file>`; then `export KUBECONFIG="$(cat <file>)"`.
- `nsc destroy <id> --force` destroys without confirmation.
- `nsc list --all -o json` returns objects with a `labels` map (e.g. `{"app":"temporal-operator-e2e"}`). The remote builder instance carries `labels."nsc.purpose" == "builder"` and must never be destroyed by the purge.
- The CI e2e flow (`.github/workflows/e2e.yml`) installs cert-manager (`latest`) and CloudNativePG (`release-1.24` / `cnpg-1.24.0.yaml`), then Helm-installs the operator from `dist/chart` with `manager.image.repository`/`manager.image.tag`/`manager.image.pullPolicy`. The operator Deployment is `temporal-operator-controller-manager` in namespace `temporal-system`; the Helm release name is `temporal-operator`.

---

### Task 1: Create the orchestrator script `hack/nsc-e2e.sh`

**Files:**
- Create: `hack/nsc-e2e.sh`

- [ ] **Step 1: Write the script**

Create `hack/nsc-e2e.sh` with exactly this content:

```bash
#!/usr/bin/env bash
# Provision an ephemeral Namespace (nsc) Kubernetes cluster, run a Chainsaw
# e2e suite against it, and ALWAYS destroy the cluster afterward.
#
# This is an alternative to the kind-based e2e flow for environments where kind
# cannot run (e.g. devcontainers). CI continues to use kind; see
# .github/workflows/e2e.yml.
#
# Usage:
#   hack/nsc-e2e.sh
#   SUITE=mtls hack/nsc-e2e.sh
#
# Environment variables (with defaults):
#   SUITE            Chainsaw suite under test/e2e/ to run (default: postgres/lifecycle)
#   NSC_DURATION     Ephemeral cluster lifetime / billing backstop (default: 30m)
#   NSC_K8S_VERSION  Kubernetes version to provision (default: 1.33)
#   NSC_LABEL        Label applied to created clusters, used by 'make nsc-clean'
#                    (default: app=temporal-operator-e2e)
#   TAG              Operator image tag (default: derived git short-sha + timestamp)
set -euo pipefail

SUITE="${SUITE:-postgres/lifecycle}"
NSC_DURATION="${NSC_DURATION:-30m}"
NSC_K8S_VERSION="${NSC_K8S_VERSION:-1.33}"
NSC_LABEL="${NSC_LABEL:-app=temporal-operator-e2e}"
TAG="${TAG:-$(git rev-parse --short HEAD 2>/dev/null || echo dev)-$(date +%s)}"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
CHAINSAW="${CHAINSAW:-$REPO_ROOT/bin/chainsaw}"

WORKDIR="$(mktemp -d)"
CIDFILE="$WORKDIR/instance.cid"
KCPATH="$WORKDIR/kubeconfig.path"

log() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
err() { printf '\033[1;31mERROR: %s\033[0m\n' "$*" >&2; }

cleanup() {
  local code=$?
  if [ -s "$CIDFILE" ]; then
    log "Destroying nsc cluster $(cat "$CIDFILE")"
    nsc destroy "$(cat "$CIDFILE")" --force \
      || err "nsc destroy failed; run 'make nsc-clean' to purge orphaned clusters"
  fi
  rm -rf "$WORKDIR"
  exit "$code"
}
# Arm cleanup BEFORE creating anything so a partially-created cluster is destroyed.
trap cleanup EXIT INT TERM

# --- Preflight ---------------------------------------------------------------
log "Preflight checks"
nsc auth check-login || { err "Not logged in to Namespace. Run 'nsc login'."; exit 1; }
for bin in kubectl helm jq; do
  command -v "$bin" >/dev/null 2>&1 || { err "Required tool '$bin' not found in PATH."; exit 1; }
done
if [ ! -x "$CHAINSAW" ]; then
  log "chainsaw not found at $CHAINSAW; installing via 'make chainsaw'"
  make chainsaw
fi

# --- Resolve registry & build/push operator image (remote builder) ----------
REG="$(nsc workspace describe 2>/dev/null | awk -F': ' '/Registry URL/ {print $2; exit}')"
[ -n "$REG" ] || { err "Could not determine nscr.io registry from 'nsc workspace describe'."; exit 1; }
IMAGE="$REG/temporal-operator:$TAG"
log "Building and pushing operator image: $IMAGE"
nsc build -f Dockerfile -t "$IMAGE" --push

# --- Create ephemeral Kubernetes cluster ------------------------------------
log "Creating ephemeral Kubernetes $NSC_K8S_VERSION cluster (auto-expires after $NSC_DURATION)"
nsc create \
  --enable="kubernetes:$NSC_K8S_VERSION" \
  --ephemeral \
  --duration "$NSC_DURATION" \
  --label "$NSC_LABEL" \
  --label "run=$TAG" \
  --wait_kube_system \
  --cidfile "$CIDFILE"
INSTANCE_ID="$(cat "$CIDFILE")"

log "Writing kubeconfig for $INSTANCE_ID"
nsc kubeconfig write "$INSTANCE_ID" --output_to "$KCPATH"
KUBECONFIG="$(cat "$KCPATH")"
export KUBECONFIG
kubectl cluster-info

# --- Install dependencies (matches .github/workflows/e2e.yml) ----------------
log "Installing cert-manager and CloudNativePG"
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml
kubectl apply --server-side -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.24/releases/cnpg-1.24.0.yaml
kubectl -n cert-manager rollout status deploy/cert-manager-webhook --timeout=180s
kubectl -n cnpg-system rollout status deploy/cnpg-controller-manager --timeout=180s

# --- Install operator via Helm (also verifies nscr.io pull access) ----------
log "Installing operator via Helm from dist/chart"
helm install temporal-operator dist/chart \
  --namespace temporal-system --create-namespace \
  --set manager.image.repository="$REG/temporal-operator" \
  --set manager.image.tag="$TAG" \
  --set manager.image.pullPolicy=IfNotPresent
kubectl -n temporal-system rollout status deploy/temporal-operator-controller-manager --timeout=180s

# --- Run the Chainsaw suite --------------------------------------------------
log "Running Chainsaw suite: $SUITE"
if ! "$CHAINSAW" test --test-dir "test/e2e/$SUITE" --config .chainsaw.yaml; then
  err "Chainsaw suite '$SUITE' failed; collecting diagnostics into ./artifacts"
  mkdir -p artifacts
  kubectl get all -A > artifacts/all.txt 2>&1 || true
  kubectl get temporalcluster -A -o yaml > artifacts/temporalclusters.yaml 2>&1 || true
  kubectl -n temporal-system logs deploy/temporal-operator-controller-manager --tail=2000 \
    > artifacts/operator.log 2>&1 || true
  exit 1
fi

log "Chainsaw suite '$SUITE' passed"
```

- [ ] **Step 2: Make it executable**

Run:
```bash
chmod +x hack/nsc-e2e.sh
```

- [ ] **Step 3: Syntax-check the script**

Run:
```bash
bash -n hack/nsc-e2e.sh && echo SYNTAX_OK
```
Expected: prints `SYNTAX_OK` with exit code 0 (no parse errors).

- [ ] **Step 4: Verify preflight fails fast on a bogus tool requirement (no cluster created)**

This proves the trap/preflight wiring is sound without spending money. Temporarily
force a missing-binary failure and confirm the script exits before `nsc create`:
```bash
PATH=/nonexistent bash hack/nsc-e2e.sh; echo "exit=$?"
```
Expected: an `ERROR:` line about a missing tool (e.g. `kubectl`/`jq`) or `nsc`, and
`exit=1`. No `nsc create` is attempted (no cluster is provisioned).

- [ ] **Step 5: Commit**

```bash
git add hack/nsc-e2e.sh
git commit -s -m "feat(e2e): add nsc-based chainsaw runner script

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 2: Add Makefile targets `chainsaw-test-nsc` and `nsc-clean`

**Files:**
- Modify: `Makefile` (add targets immediately after the existing `chainsaw-test` target, around line 271)

- [ ] **Step 1: Add the targets**

Find this block in `Makefile`:
```make
.PHONY: chainsaw-test
chainsaw-test: chainsaw ## Run Chainsaw e2e tests against the current kube context.
	"$(CHAINSAW)" test --test-dir test/e2e --config .chainsaw.yaml
```

Insert the following immediately AFTER it (keep a blank line before and after):
```make
.PHONY: chainsaw-test-nsc
chainsaw-test-nsc: chainsaw ## Run a Chainsaw suite on an ephemeral nsc cluster. Override with SUITE=, NSC_DURATION=, NSC_K8S_VERSION=.
	CHAINSAW="$(CHAINSAW)" ./hack/nsc-e2e.sh

.PHONY: nsc-clean
nsc-clean: ## Destroy ALL nsc clusters labeled app=temporal-operator-e2e (leaves the builder instance untouched).
	@ids="$$(nsc list --all -o json | jq -r '.[] | select(.labels.app == "temporal-operator-e2e") | .cluster_id')"; \
	if [ -z "$$ids" ]; then \
		echo "No temporal-operator-e2e clusters to clean up."; \
	else \
		for id in $$ids; do \
			echo "Destroying $$id"; \
			nsc destroy "$$id" --force; \
		done; \
	fi
```

Note: `SUITE`, `NSC_DURATION`, etc. are read from the environment by the script, so
`make chainsaw-test-nsc SUITE=mtls` works because make exports command-line variables
to recipe subprocesses.

- [ ] **Step 2: Verify the targets parse and appear in help**

Run:
```bash
make -n chainsaw-test-nsc
```
Expected: prints `CHAINSAW=.../bin/chainsaw ./hack/nsc-e2e.sh` (plus possibly the
chainsaw install recipe). No "No rule to make target" error.

Run:
```bash
make help 2>/dev/null | grep -E 'chainsaw-test-nsc|nsc-clean'
```
Expected: both targets listed with their `##` descriptions.

- [ ] **Step 3: Verify `nsc-clean` is a safe no-op right now**

Run:
```bash
make nsc-clean
```
Expected: `No temporal-operator-e2e clusters to clean up.` (There are currently no
such clusters; the pre-existing `nsc.purpose: builder` instance is NOT matched.)

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -s -m "feat(e2e): add chainsaw-test-nsc and nsc-clean make targets

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 3: Document the nsc runner in CONTRIBUTING.md

**Files:**
- Modify: `CONTRIBUTING.md` (add a subsection after the existing `### Run against a local cluster` block, before `## Conventional Commits`)

- [ ] **Step 1: Add the documentation**

Find this block in `CONTRIBUTING.md`:
```markdown
### Run against a local cluster

```sh
make kind-up
make install           # install CRDs
make run               # run the controller locally
```
```

Insert the following immediately AFTER it (and before `## Conventional Commits`):
```markdown
### Run e2e (Chainsaw) on Namespace (nsc)

When [kind](https://kind.sigs.k8s.io/) cannot run locally (for example inside a
devcontainer), you can run the Chainsaw e2e suites against an ephemeral
[Namespace](https://cloud.namespace.so/) cluster instead. CI still uses kind;
this is an alternate path for local/agent validation.

Prerequisites: the `nsc` CLI installed and authenticated (`nsc login`), plus
`kubectl`, `helm`, and `jq` on your `PATH`.

```sh
make chainsaw-test-nsc                 # runs the postgres/lifecycle suite
make chainsaw-test-nsc SUITE=mtls      # run a different suite under test/e2e/
```

The runner builds and pushes the operator image with `nsc build` (no local
Docker needed), provisions an ephemeral cluster, installs cert-manager,
CloudNativePG, and the operator, runs the suite, then destroys the cluster.

Billing safety is layered: the cluster is created `--ephemeral` with a 30m
`--duration` (override with `NSC_DURATION=`) so it auto-expires even if the
process is killed, and the script destroys it on exit. To purge any leftover
clusters from interrupted runs:

```sh
make nsc-clean
```

`nsc-clean` only destroys clusters labeled `app=temporal-operator-e2e`; it never
touches the shared Namespace build cluster.
```

- [ ] **Step 2: Verify the section renders and is internally consistent**

Run:
```bash
grep -n 'chainsaw-test-nsc\|nsc-clean\|Run e2e (Chainsaw) on Namespace' CONTRIBUTING.md
```
Expected: matches for the new heading and both make targets, confirming the block
was inserted and references the same target names defined in Task 2.

- [ ] **Step 3: Commit**

```bash
git add CONTRIBUTING.md
git commit -s -m "docs: document make chainsaw-test-nsc e2e runner

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

### Task 4: End-to-end validation against a real nsc cluster

This is the real integration test. It spends a small amount of money (one ~30m
ephemeral cluster, usually destroyed within minutes) and validates the whole flow
plus the cleanup guarantees. It also surfaces the risks called out in the spec
(nscr.io pull access, Docker Hub rate limits, k8s version availability).

**Files:** none (verification only; may produce small follow-up fixes).

- [ ] **Step 1: Run the full suite end-to-end**

Run:
```bash
make chainsaw-test-nsc 2>&1 | tee /tmp/nsc-e2e.log
```
Expected: the log shows, in order, `Building and pushing operator image`,
`Creating ephemeral Kubernetes ... cluster`, cert-manager/CNPG rollouts, the
operator deployment rollout succeeding (this confirms **nscr.io pull access**),
the Chainsaw suite output ending in a pass, then `Destroying nsc cluster <id>`.
Final exit code 0.

If the operator pod fails to pull the image (`ImagePullBackOff`), the nscr.io
cluster needs a pull secret — see spec risk #1; wire a registry pull secret into
the `temporal-system` namespace before retrying. If `temporalio/server` /
`temporalio/admin-tools` pods hit Docker Hub rate limits, see spec risk #2.

- [ ] **Step 2: Confirm the cluster was destroyed (no orphans, builder intact)**

Run:
```bash
nsc list --all -o json | jq -r '.[] | "\(.cluster_id)\t\(.labels)"'
```
Expected: NO entry with label `app=temporal-operator-e2e`. The
`nsc.purpose: builder` instance is still present (it must not have been destroyed).

- [ ] **Step 3: Verify `nsc-clean` purge against a deliberately orphaned cluster**

Create a throwaway labeled cluster, confirm `nsc-clean` destroys it, and confirm
the builder survives:
```bash
nsc create --enable=kubernetes:1.33 --ephemeral --duration 10m \
  --label app=temporal-operator-e2e --cidfile /tmp/orphan.cid
make nsc-clean
nsc list --all -o json | jq -r '.[] | select(.labels.app == "temporal-operator-e2e") | .cluster_id'
```
Expected: `nsc-clean` prints `Destroying <id>` for the orphan, and the final
`nsc list` query prints nothing (orphan gone). Separately confirm the builder
remains:
```bash
nsc list --all -o json | jq -r '.[] | select(.labels."nsc.purpose" == "builder") | .cluster_id'
```
Expected: the builder cluster id is still listed.

- [ ] **Step 4: Clean up any test artifacts**

Run:
```bash
rm -f /tmp/orphan.cid /tmp/nsc-e2e.log
rm -rf artifacts   # only present if a suite failed and produced diagnostics
```

- [ ] **Step 5: Commit any fixes discovered during validation**

If Steps 1-3 required edits to `hack/nsc-e2e.sh` or the Makefile (e.g. a pull
secret, a flag tweak), commit them:
```bash
git add -A
git commit -s -m "fix(e2e): adjust nsc runner after end-to-end validation

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```
If no fixes were needed, skip this step.

---

## Self-Review

- **Spec coverage:**
  - Files/interface (`hack/nsc-e2e.sh`, `chainsaw-test-nsc`, `nsc-clean`) → Tasks 1, 2.
  - Env-var config (SUITE/NSC_DURATION/NSC_K8S_VERSION/NSC_LABEL/TAG) → Task 1 Step 1.
  - Registry derived dynamically from `nsc workspace describe` → Task 1 Step 1.
  - Remote image build/push (no local Docker) → Task 1 Step 1.
  - Ephemeral cluster with 30m duration + cidfile + trap-before-create → Task 1 Step 1.
  - Three cleanup layers (duration backstop, trap, label-scoped purge) → Task 1 (trap + duration), Task 2 (`nsc-clean`), validated in Task 4 Steps 2-3.
  - Kubeconfig wiring via `--output_to` → Task 1 Step 1.
  - cert-manager + CNPG install matching CI → Task 1 Step 1.
  - Helm install verifying nscr.io pull access → Task 1 Step 1, validated Task 4 Step 1.
  - Chainsaw run + failure diagnostics into ./artifacts → Task 1 Step 1.
  - Risks verified on first real run → Task 4 Step 1 notes.
  - Validation (real run, no orphans, builder intact, purge works) → Task 4.
  - Docs in CONTRIBUTING.md → Task 3.
  No spec gaps found.
- **Placeholder scan:** No TBD/TODO/"handle edge cases"; all code shown in full.
- **Type/name consistency:** Target names (`chainsaw-test-nsc`, `nsc-clean`), label
  (`app=temporal-operator-e2e`), env vars, deployment name
  (`temporal-operator-controller-manager`), namespace (`temporal-system`), and Helm
  release (`temporal-operator`) are identical across the script, Makefile, docs, and
  validation tasks.
