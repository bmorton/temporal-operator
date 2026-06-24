#!/usr/bin/env bash
# Provision AKS + Azure Database for PostgreSQL Flexible Server, run the Azure
# passwordless Chainsaw e2e against them, and tear everything down. All
# resources live in one tagged resource group so teardown is a single
# `az group delete`. Does NOT touch the kind (.github/workflows/e2e.yml) or nsc
# (hack/nsc-e2e.sh) e2e flows.
#
# Usage:
#   hack/azure-e2e.sh up         # provision everything, write hack/.azure-e2e.env
#   hack/azure-e2e.sh test       # run the chainsaw suite against the standing cluster
#   hack/azure-e2e.sh deploy     # apply a standing, usable TemporalCluster (no teardown)
#   hack/azure-e2e.sh up-deploy  # up -> deploy (provision, then leave a usable cluster)
#   hack/azure-e2e.sh down       # delete the resource group (everything)
#   hack/azure-e2e.sh all        # up -> test -> down (always tears down via trap)
#   hack/azure-e2e.sh clean      # delete ANY resource group with our tag (leak backstop)
#
# Env (defaults):
#   AZURE_LOCATION      centralus
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

# Azure Database for PostgreSQL Flexible Server is region-restricted on many
# subscriptions (e.g. Visual Studio / MSDN credit subscriptions restrict eastus,
# eastus2, westus2). centralus is broadly available; override AZURE_LOCATION if
# your subscription restricts it (see README for how to find an allowed region).
AZURE_LOCATION="${AZURE_LOCATION:-centralus}"
AKS_NODE_SIZE="${AKS_NODE_SIZE:-Standard_B2s}"
AKS_NODE_COUNT="${AKS_NODE_COUNT:-1}"
PG_SKU="${PG_SKU:-Standard_B1ms}"
PG_TIER="${PG_TIER:-Burstable}"
PG_VERSION="${PG_VERSION:-16}"
E2E_TAG="${E2E_TAG:-app=temporal-operator-e2e}"
# Namespace the Chainsaw suite runs in. Must match the federated credential
# subject created in 'up' (see cmd_test).
AZURE_TEST_NS="${AZURE_TEST_NS:-azure-e2e}"

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
  local SA_NAME="azure-e2e-azure" NS="$AZURE_TEST_NS"

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
  # Federated credential for the operator-generated cluster SA (Temporal server
  # pods + schema Job). The operator no longer needs Workload Identity.
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
  # Temporal's SQL visibility schema needs btree_gin (and pg_trgm). Azure
  # Flexible Server requires extensions to be allow-listed before CREATE
  # EXTENSION works (else: "extension ... is not allow-listed"). Dynamic param,
  # no restart.
  az postgres flexible-server parameter set -g "$AZURE_RG" -s "$PG_NAME" \
    --name azure.extensions --value btree_gin,pg_trgm >/dev/null

  log "Creating databases and mapping the managed identity to a Postgres role"
  az postgres flexible-server db create -g "$AZURE_RG" -s "$PG_NAME" -n temporal >/dev/null
  az postgres flexible-server db create -g "$AZURE_RG" -s "$PG_NAME" -n temporal_visibility >/dev/null
  # The psql calls below run from THIS host (outside Azure); the 0.0.0.0 rule
  # only admits Azure-internal traffic (the operator/pods), so add a firewall
  # rule for the runner's public IP.
  local MYIP; MYIP="$(curl -fsS --max-time 10 https://api.ipify.org || curl -fsS --max-time 10 https://ifconfig.me)"
  az postgres flexible-server firewall-rule create -g "$AZURE_RG" -s "$PG_NAME" \
    -n runner-host --start-ip-address "$MYIP" --end-ip-address "$MYIP" >/dev/null
  # Map the workload identity as an Entra principal so it can log in to Postgres.
  # The admin authenticates with a fresh oss-rdbms Entra token.
  local PG_TOKEN; PG_TOKEN="$(az account get-access-token --resource-type oss-rdbms --query accessToken -o tsv)"
  PGPASSWORD="$PG_TOKEN" psql "host=$PG_HOST port=5432 dbname=postgres user=$PG_ADMIN sslmode=require" \
    -v ON_ERROR_STOP=1 \
    -c "select * from pgaadauth_create_principal_with_oid('$UAMI_NAME', '$CLIENT_ID', 'service', false, false);" || \
    err "principal mapping failed (may already exist); continuing"
  # PostgreSQL 16 no longer grants CREATE on the public schema to all roles, so
  # the schema Job (running as the managed-identity role) needs explicit grants
  # to create Temporal's tables.
  local db
  for db in temporal temporal_visibility; do
    PGPASSWORD="$PG_TOKEN" psql "host=$PG_HOST port=5432 dbname=$db user=$PG_ADMIN sslmode=require" \
      -v ON_ERROR_STOP=1 \
      -c "GRANT ALL PRIVILEGES ON DATABASE \"$db\" TO \"$UAMI_NAME\";" \
      -c "GRANT ALL ON SCHEMA public TO \"$UAMI_NAME\";" \
      -c "ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT ALL ON TABLES TO \"$UAMI_NAME\";" >/dev/null
  done

  log "Installing cert-manager (required by the operator webhook certs)"
  kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml >/dev/null
  kubectl -n cert-manager rollout status deploy/cert-manager-webhook --timeout=180s

  log "Installing operator via Helm"
  helm install temporal-operator dist/chart \
    --namespace temporal-system --create-namespace \
    --set manager.image.repository="$ACR_LOGIN/temporal-operator" \
    --set manager.image.tag="$sfx" >/dev/null
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
  # Pin the namespace to azure-e2e so the test ServiceAccount's subject matches
  # the federated credential created in 'up' (chainsaw otherwise generates a
  # random namespace, which would fail Workload Identity token exchange).
  "$CHAINSAW" test --test-dir test/e2e/azure --config .chainsaw.yaml \
    --namespace "$AZURE_TEST_NS" --values "$VALUES_FILE"
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

# cmd_deploy applies a STANDING TemporalCluster (named azure-e2e, in the
# AZURE_TEST_NS namespace) against an already-provisioned environment, so you can
# actually use Temporal instead of having Chainsaw create and immediately tear it
# down. The cluster name/namespace are fixed because the federated credential is
# bound to system:serviceaccount:$AZURE_TEST_NS:azure-e2e-azure.
cmd_deploy() {
  command -v kubectl >/dev/null || { err "kubectl not found"; exit 1; }
  [ -f "$ENV_FILE" ] || { err "No $ENV_FILE; run 'azure-e2e.sh up' first."; exit 1; }
  # shellcheck source=/dev/null
  . "$ENV_FILE"
  : "${PG_HOST:?missing PG_HOST in $ENV_FILE}" \
    "${PG_USER:?missing PG_USER in $ENV_FILE}" \
    "${CLIENT_ID:?missing CLIENT_ID in $ENV_FILE}"

  local manifest="$REPO_ROOT/test/e2e/azure/03-temporalcluster.yaml"
  log "Deploying standing TemporalCluster 'azure-e2e' in namespace $AZURE_TEST_NS"
  kubectl create namespace "$AZURE_TEST_NS" >/dev/null 2>&1 || true
  # Substitute the Chainsaw ($values.*) bindings with the provisioned values.
  sed -e "s|(\$values.clientId)|$CLIENT_ID|g" \
      -e "s|(\$values.pgHost)|$PG_HOST|g" \
      -e "s|(\$values.pgUser)|$PG_USER|g" \
      "$manifest" | kubectl -n "$AZURE_TEST_NS" apply -f -

  log "Waiting for the cluster to become Ready (up to 10m)"
  if ! kubectl -n "$AZURE_TEST_NS" wait --for=condition=Ready --timeout=600s \
      temporalcluster/azure-e2e; then
    log "Not Ready yet — inspect with: kubectl -n $AZURE_TEST_NS get temporalcluster azure-e2e -o wide"
  fi
  cat <<EOF

Standing TemporalCluster is up in namespace $AZURE_TEST_NS.
  Frontend (in-cluster):  azure-e2e-frontend.$AZURE_TEST_NS.svc:7233
  Port-forward locally:   kubectl -n $AZURE_TEST_NS port-forward svc/azure-e2e-frontend 7233:7233
  Web UI (enabled):       kubectl -n $AZURE_TEST_NS port-forward svc/azure-e2e-ui 8080:8080  # http://localhost:8080
  Remove just this cluster: kubectl delete namespace $AZURE_TEST_NS
  Tear down everything:   make azure-e2e-down
EOF
}

cmd_up_deploy() {
  cmd_up
  cmd_deploy
}

case "${1:-}" in
  up)        cmd_up ;;
  test)      cmd_test ;;
  deploy)    cmd_deploy ;;
  up-deploy) cmd_up_deploy ;;
  down)      cmd_down ;;
  clean)     cmd_clean ;;
  all)       cmd_all ;;
  *) err "usage: $0 {up|test|deploy|up-deploy|down|clean|all}"; exit 1 ;;
esac
