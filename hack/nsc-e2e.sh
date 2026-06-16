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
