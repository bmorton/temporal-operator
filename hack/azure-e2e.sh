#!/usr/bin/env bash
# Provision AKS + Azure Database for PostgreSQL Flexible Server, run the Azure
# passwordless Chainsaw e2e against them, and tear everything down. All
# resources live in one tagged resource group so teardown is a single
# `az group delete`. Does NOT touch the kind (.github/workflows/e2e.yml) or nsc
# (hack/nsc-e2e.sh) e2e flows.
#
# Usage:
#   hack/azure-e2e.sh up        # provision everything, write hack/.azure-e2e.env
#   hack/azure-e2e.sh test      # run the chainsaw suite against the standing cluster
#   hack/azure-e2e.sh down      # delete the resource group (everything)
#   hack/azure-e2e.sh all       # up -> test -> down (always tears down via trap)
#   hack/azure-e2e.sh clean     # delete ANY resource group with our tag (leak backstop)
#
# Env (defaults):
#   AZURE_LOCATION      eastus
#   AZURE_RG            temporal-operator-e2e-<rand>   (persisted in env file)
#   AKS_NODE_SIZE       Standard_B2s
#   AKS_NODE_COUNT      1
#   PG_SKU              Standard_B1ms
#   PG_TIER             Burstable
#   PG_VERSION          16
#   ACR_NAME            tempope2e<rand>
#   E2E_TAG             app=temporal-operator-e2e
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
ENV_FILE="$REPO_ROOT/hack/.azure-e2e.env"
VALUES_FILE="$REPO_ROOT/test/e2e/azure/.values.local.yaml"
CHAINSAW="${CHAINSAW:-$REPO_ROOT/bin/chainsaw}"

log() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
err() { printf '\033[1;31mERROR: %s\033[0m\n' "$*" >&2; }

AZURE_LOCATION="${AZURE_LOCATION:-eastus}"
AKS_NODE_SIZE="${AKS_NODE_SIZE:-Standard_B2s}"
AKS_NODE_COUNT="${AKS_NODE_COUNT:-1}"
PG_SKU="${PG_SKU:-Standard_B1ms}"
PG_TIER="${PG_TIER:-Burstable}"
PG_VERSION="${PG_VERSION:-16}"
E2E_TAG="${E2E_TAG:-app=temporal-operator-e2e}"

preflight() {
  command -v az >/dev/null || { err "az CLI not found"; exit 1; }
  command -v kubectl >/dev/null || { err "kubectl not found"; exit 1; }
  command -v helm >/dev/null || { err "helm not found"; exit 1; }
  command -v psql >/dev/null || { err "psql not found (needed for the principal mapping)"; exit 1; }
  az account show >/dev/null 2>&1 || { err "Run 'az login' and select your subscription."; exit 1; }
  [ -x "$CHAINSAW" ] || { log "chainsaw missing; running 'make chainsaw'"; make chainsaw; }
}

rand_suffix() {
  # Read a bounded chunk so `tr` reaches EOF cleanly and slice with bash. A
  # trailing `| head -c 6` would SIGPIPE `tr` (unbounded /dev/urandom reader),
  # which under `set -o pipefail` surfaces as exit 141 and aborts the script.
  local raw
  raw="$(head -c 1024 /dev/urandom | LC_ALL=C tr -dc 'a-z0-9')"
  printf '%s' "${raw:0:6}"
}

cmd_up() {
  preflight
  local sfx; sfx="$(rand_suffix)"
  AZURE_RG="${AZURE_RG:-temporal-operator-e2e-$sfx}"
  ACR_NAME="${ACR_NAME:-tempope2e$sfx}"
  local AKS_NAME="aks-$sfx" PG_NAME="pg-$sfx" UAMI_NAME="id-$sfx"
  local SA_NAME="temporal-workload-identity" NS="azure-e2e"

  log "Creating resource group $AZURE_RG ($AZURE_LOCATION)"
  az group create -n "$AZURE_RG" -l "$AZURE_LOCATION" \
    --tags "$E2E_TAG" run="$sfx" >/dev/null

  log "Building operator image in ACR (remote build from the current branch)"
  az acr create -g "$AZURE_RG" -n "$ACR_NAME" --sku Basic >/dev/null
  az acr build -r "$ACR_NAME" -t "temporal-operator:$sfx" -f Dockerfile . >/dev/null
  local ACR_LOGIN; ACR_LOGIN="$(az acr show -g "$AZURE_RG" -n "$ACR_NAME" --query loginServer -o tsv)"

  log "Creating AKS ($AKS_NODE_COUNT x $AKS_NODE_SIZE, OIDC + Workload Identity)"
  az aks create -g "$AZURE_RG" -n "$AKS_NAME" \
    --node-count "$AKS_NODE_COUNT" --node-vm-size "$AKS_NODE_SIZE" \
    --enable-oidc-issuer --enable-workload-identity \
    --attach-acr "$ACR_NAME" --generate-ssh-keys >/dev/null
  az aks get-credentials -g "$AZURE_RG" -n "$AKS_NAME" --overwrite-existing
  local OIDC_ISSUER; OIDC_ISSUER="$(az aks show -g "$AZURE_RG" -n "$AKS_NAME" --query oidcIssuerProfile.issuerUrl -o tsv)"

  log "Creating user-assigned managed identity + federated credential"
  az identity create -g "$AZURE_RG" -n "$UAMI_NAME" >/dev/null
  local CLIENT_ID; CLIENT_ID="$(az identity show -g "$AZURE_RG" -n "$UAMI_NAME" --query clientId -o tsv)"
  az identity federated-credential create -g "$AZURE_RG" -n "fc-$sfx" \
    --identity-name "$UAMI_NAME" --issuer "$OIDC_ISSUER" \
    --subject "system:serviceaccount:$NS:$SA_NAME" \
    --audience api://AzureADTokenExchange >/dev/null

  log "Creating Flexible Server ($PG_SKU, Entra auth, password auth disabled)"
  local PG_ADMIN; PG_ADMIN="$(az account show --query user.name -o tsv)"
  local PG_ADMIN_OID; PG_ADMIN_OID="$(az ad signed-in-user show --query id -o tsv)"
  az postgres flexible-server create -g "$AZURE_RG" -n "$PG_NAME" \
    --location "$AZURE_LOCATION" --tier "$PG_TIER" --sku-name "$PG_SKU" \
    --version "$PG_VERSION" --storage-size 32 \
    --microsoft-entra-auth Enabled --password-auth Disabled \
    --admin-display-name "$PG_ADMIN" --admin-object-id "$PG_ADMIN_OID" \
    --admin-type User --public-access 0.0.0.0 --yes >/dev/null
  local PG_HOST; PG_HOST="$(az postgres flexible-server show -g "$AZURE_RG" -n "$PG_NAME" --query fullyQualifiedDomainName -o tsv)"

  log "Creating databases and mapping the managed identity to a Postgres role"
  az postgres flexible-server db create -g "$AZURE_RG" -s "$PG_NAME" -d temporal >/dev/null
  az postgres flexible-server db create -g "$AZURE_RG" -s "$PG_NAME" -d temporal_visibility >/dev/null
  # Map the workload identity as an Entra principal so it can log in to Postgres.
  # The admin authenticates with a fresh oss-rdbms Entra token.
  local PG_TOKEN; PG_TOKEN="$(az account get-access-token --resource-type oss-rdbms --query accessToken -o tsv)"
  PGPASSWORD="$PG_TOKEN" psql "host=$PG_HOST port=5432 dbname=postgres user=$PG_ADMIN sslmode=require" \
    -v ON_ERROR_STOP=1 \
    -c "select * from pgaadauth_create_principal_with_oid('$UAMI_NAME', '$CLIENT_ID', 'service', false, false);" || \
    err "principal mapping failed (may already exist); continuing"

  log "Installing operator via Helm (Workload Identity enabled)"
  helm install temporal-operator dist/chart \
    --namespace temporal-system --create-namespace \
    --set manager.image.repository="$ACR_LOGIN/temporal-operator" \
    --set manager.image.tag="$sfx" \
    --set workloadIdentity.enable=true \
    --set workloadIdentity.clientId="$CLIENT_ID" >/dev/null
  kubectl -n temporal-system rollout status deploy/temporal-operator-controller-manager --timeout=180s

  log "Writing $ENV_FILE and $VALUES_FILE"
  cat > "$ENV_FILE" <<EOF
AZURE_RG=$AZURE_RG
PG_HOST=$PG_HOST
CLIENT_ID=$CLIENT_ID
PG_USER=$UAMI_NAME
EOF
  cat > "$VALUES_FILE" <<EOF
pgHost: $PG_HOST
pgUser: $UAMI_NAME
clientId: $CLIENT_ID
EOF
  log "Provisioning complete. Run 'make azure-e2e-test', then 'make azure-e2e-down'."
}

cmd_test() {
  preflight
  [ -f "$VALUES_FILE" ] || { err "No $VALUES_FILE; run 'azure-e2e.sh up' first."; exit 1; }
  log "Running Chainsaw Azure suite"
  "$CHAINSAW" test --test-dir test/e2e/azure --config .chainsaw.yaml --values "$VALUES_FILE"
}

cmd_down() {
  # shellcheck source=/dev/null
  [ -f "$ENV_FILE" ] && . "$ENV_FILE"
  : "${AZURE_RG:?set AZURE_RG or provide $ENV_FILE}"
  log "Deleting resource group $AZURE_RG"
  az group delete -n "$AZURE_RG" --yes --no-wait
  rm -f "$ENV_FILE" "$VALUES_FILE"
}

cmd_clean() {
  local key="${E2E_TAG%%=*}" val="${E2E_TAG##*=}"
  log "Deleting resource groups tagged $E2E_TAG or named temporal-operator-e2e-*"
  # Match by tag (covers custom AZURE_RG names) and by the default name prefix
  # (a backstop that also catches groups created before the tag was correct).
  local rgs
  rgs="$( { az group list --tag "$key=$val" --query '[].name' -o tsv;
            az group list --query "[?starts_with(name, 'temporal-operator-e2e-')].name" -o tsv; } \
          | sort -u)"
  [ -n "$rgs" ] || { log "No matching resource groups found."; return 0; }
  for rg in $rgs; do log "Deleting $rg"; az group delete -n "$rg" --yes --no-wait; done
}

cmd_all() {
  trap 'cmd_down || true' EXIT INT TERM
  cmd_up
  cmd_test
}

case "${1:-}" in
  up)    cmd_up ;;
  test)  cmd_test ;;
  down)  cmd_down ;;
  clean) cmd_clean ;;
  all)   cmd_all ;;
  *) err "usage: $0 {up|test|down|clean|all}"; exit 1 ;;
esac
