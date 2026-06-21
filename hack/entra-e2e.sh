#!/usr/bin/env bash
# Create and manage a longstanding Microsoft Entra app registration for manual
# Entra JWT integration testing with Temporal. The registration includes three
# app roles whose values are Temporal <namespace>:<role> strings, a service
# principal, and a client secret. Use the subcommands below to provision the
# registration, mint a client-credentials JWT for gRPC testing, or tear
# everything down.
#
# Usage:
#   hack/entra-e2e.sh up     # create app registration, app roles, SP, secret
#   hack/entra-e2e.sh token  # print a client-credentials access token
#   hack/entra-e2e.sh down   # delete the app registration + env file
#
# Env (overridable):
#   ENTRA_APP_NAME   temporal-operator-e2e   (display name for the app registration)
#   ENTRA_TENANT_ID  <inferred from 'az account show' if not set>
#
# Credentials are persisted in hack/.entra-e2e.env (gitignored). Source it
# before 'token' or 'down' if you are in a fresh shell:
#   source hack/.entra-e2e.env
#
# Prerequisites: az CLI, curl, jq, an active 'az login' with tenant-admin
# privileges (Application.ReadWrite.All or a delegated app-admin role).
#
# CAUTION: This script mutates a real Microsoft Entra tenant. Do NOT run in CI.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"
ENV_FILE="$REPO_ROOT/hack/.entra-e2e.env"
# Temporary roles file written during 'up' and removed after use.
ROLES_FILE="$REPO_ROOT/hack/.entra-app-roles.json"

log() { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
err() { printf '\033[1;31mERROR: %s\033[0m\n' "$*" >&2; }

APP_NAME="${ENTRA_APP_NAME:-temporal-operator-e2e}"

preflight() {
  command -v az   >/dev/null || { err "az CLI not found";  exit 1; }
  command -v curl >/dev/null || { err "curl not found";    exit 1; }
  command -v jq   >/dev/null || { err "jq not found";     exit 1; }
  az account show >/dev/null 2>&1 || { err "Run 'az login' and select your tenant."; exit 1; }
}

cmd_up() {
  preflight

  # Finding 3: guard against re-running 'up' on an existing registration.
  # 'az ad app credential reset' revokes prior secrets, so a second 'up' would
  # silently invalidate any live credential.
  if [ -f "$ENV_FILE" ]; then
    err "Env file $ENV_FILE already exists — app may already be registered. Run 'hack/entra-e2e.sh down' first."
    exit 1
  fi

  local ENTRA_TENANT_ID
  ENTRA_TENANT_ID="${ENTRA_TENANT_ID:-$(az account show --query tenantId -o tsv)}"

  log "Writing app-roles JSON to $ROLES_FILE"
  # Three app roles whose 'value' fields are Temporal <namespace>:<role> strings.
  # The GUIDs below are stable identifiers for the roles within this registration;
  # they were generated offline and are not secret.
  cat > "$ROLES_FILE" <<'ROLES_EOF'
[
  {
    "id": "1a2b3c4d-5e6f-7a8b-9c0d-1e2f3a4b5c6d",
    "displayName": "Temporal default namespace read",
    "description": "Read access to the Temporal default namespace.",
    "value": "default:read",
    "allowedMemberTypes": ["Application", "User"],
    "isEnabled": true
  },
  {
    "id": "2b3c4d5e-6f7a-8b9c-0d1e-2f3a4b5c6d7e",
    "displayName": "Temporal default namespace write",
    "description": "Write access to the Temporal default namespace.",
    "value": "default:write",
    "allowedMemberTypes": ["Application", "User"],
    "isEnabled": true
  },
  {
    "id": "3c4d5e6f-7a8b-9c0d-1e2f-3a4b5c6d7e8f",
    "displayName": "Temporal system namespace admin",
    "description": "Admin access to the Temporal temporal-system namespace.",
    "value": "temporal-system:admin",
    "allowedMemberTypes": ["Application", "User"],
    "isEnabled": true
  }
]
ROLES_EOF
  # Finding 2: always clean up the temp roles file, even if az commands fail.
  trap 'rm -f "$ROLES_FILE"' EXIT

  log "Creating app registration '$APP_NAME'"
  local APP_ID
  APP_ID="$(az ad app create --display-name "$APP_NAME" --query appId -o tsv)"

  log "Setting identifier URI and app roles on $APP_ID"
  # Use 'api://<appId>' as the identifier URI (the standard pattern for custom
  # APIs). The client-credentials scope is then 'api://<appId>/.default'.
  az ad app update --id "$APP_ID" \
    --identifier-uris "api://$APP_ID" \
    --app-roles "@$ROLES_FILE"

  log "Creating service principal for app $APP_ID"
  az ad sp create --id "$APP_ID" >/dev/null

  log "Creating client secret"
  local CLIENT_SECRET
  CLIENT_SECRET="$(az ad app credential reset \
    --id "$APP_ID" \
    --display-name "entra-e2e" \
    --query password -o tsv)"

  local JWKS_URL="https://login.microsoftonline.com/$ENTRA_TENANT_ID/discovery/v2.0/keys"

  log "Writing $ENV_FILE"
  # Finding 1: use printf %q for the secret so special characters ($, backticks,
  # spaces, quotes) are shell-escaped and survive `. "$ENV_FILE"` correctly.
  {
    printf 'ENTRA_TENANT_ID=%s\n'     "$ENTRA_TENANT_ID"
    printf 'ENTRA_APP_ID=%s\n'        "$APP_ID"
    printf 'ENTRA_CLIENT_ID=%s\n'     "$APP_ID"
    printf 'ENTRA_CLIENT_SECRET=%q\n' "$CLIENT_SECRET"
    printf 'ENTRA_APP_NAME=%s\n'      "$APP_NAME"
  } > "$ENV_FILE"

  cat <<EOF

  Tenant ID:     $ENTRA_TENANT_ID
  Client ID:     $APP_ID
  Client secret: $CLIENT_SECRET
  JWKS URL:      $JWKS_URL

  Credentials saved to $ENV_FILE.
  Run 'make entra-e2e-token' to mint an access token.
  Run 'make entra-e2e-down' to delete the registration when done.
EOF
}

cmd_token() {
  # Token acquisition approach: OAuth 2.0 client-credentials grant via curl
  # against the standard OIDC token endpoint. We use curl rather than
  # 'az account get-access-token' because the az command returns a token for
  # the interactively signed-in user, not a daemon/application credential.
  # The scope 'api://<clientId>/.default' requests all app roles assigned to
  # the calling service principal (i.e. the roles defined in the registration).
  preflight
  [ -f "$ENV_FILE" ] || { err "No $ENV_FILE; run 'entra-e2e.sh up' first."; exit 1; }
  # shellcheck source=/dev/null
  . "$ENV_FILE"
  : "${ENTRA_TENANT_ID:?missing ENTRA_TENANT_ID in $ENV_FILE}"
  : "${ENTRA_CLIENT_ID:?missing ENTRA_CLIENT_ID in $ENV_FILE}"
  : "${ENTRA_CLIENT_SECRET:?missing ENTRA_CLIENT_SECRET in $ENV_FILE}"

  log "Requesting client-credentials token (scope: api://$ENTRA_CLIENT_ID/.default)"
  local response
  response="$(curl -fsS -X POST \
    "https://login.microsoftonline.com/$ENTRA_TENANT_ID/oauth2/v2.0/token" \
    -H "Content-Type: application/x-www-form-urlencoded" \
    --data-urlencode "client_id=$ENTRA_CLIENT_ID" \
    --data-urlencode "client_secret=$ENTRA_CLIENT_SECRET" \
    --data-urlencode "scope=api://$ENTRA_CLIENT_ID/.default" \
    --data-urlencode "grant_type=client_credentials")"

  local token
  token="$(printf '%s' "$response" | jq -r '.access_token')"
  if [ "$token" = "null" ] || [ -z "$token" ]; then
    err "Token acquisition failed: $(printf '%s' "$response" | jq -r '.error_description // .')"
    exit 1
  fi
  printf '%s\n' "$token"
}

cmd_down() {
  [ -f "$ENV_FILE" ] || { err "No $ENV_FILE; nothing to delete."; exit 1; }
  # shellcheck source=/dev/null
  . "$ENV_FILE"
  : "${ENTRA_APP_ID:?missing ENTRA_APP_ID in $ENV_FILE}"
  log "Deleting app registration $ENTRA_APP_ID ($APP_NAME)"
  az ad app delete --id "$ENTRA_APP_ID"
  rm -f "$ENV_FILE"
  log "App registration deleted and $ENV_FILE removed."
}

usage() {
  cat <<EOF
Usage: hack/entra-e2e.sh <subcommand>

Subcommands:
  up     Create the Entra app registration, app roles, service principal, and
         client secret. Writes credentials to hack/.entra-e2e.env.
  token  Mint a client-credentials access token for gRPC auth testing.
  down   Delete the Entra app registration and remove hack/.entra-e2e.env.
EOF
}

case "${1:-}" in
  up)    cmd_up    ;;
  token) cmd_token ;;
  down)  cmd_down  ;;
  *)     usage; exit 1 ;;
esac
