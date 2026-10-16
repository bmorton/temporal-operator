#!/usr/bin/env bash
# Assert both SQL stores are at the schema version the given server version
# requires (hack/version-matrix.yaml, via hack/schema-min.go), both as the
# operator reports it in status.persistence.schemaVersions and as recorded in
# the database itself. A Ready cluster alone cannot tell a migrated schema from
# an unmigrated one (#222).
#
# Usage: check-schema.sh <namespace> <server-version> [cluster-name]
set -euo pipefail

ns="$1"
version="$2"
cluster="${3:-temporal-upgrade}"
root="$(git rev-parse --show-toplevel)"

# kubectl with a few retries: a transient API-server hiccup must not read as a
# schema mismatch.
k() {
  local attempt out
  for attempt in 1 2 3 4 5; do
    if out="$(kubectl --request-timeout=30s "$@" 2>&1)"; then
      printf '%s' "$out"
      return 0
    fi
    echo "kubectl $* failed (attempt $attempt): $out" >&2
    sleep 5
  done
  return 1
}

rc=0
for spec in default:sql:temporal visibility:visibility:temporal_visibility; do
  IFS=: read -r store kind db <<<"$spec"
  want="$(go run "$root/hack/schema-min.go" "$version" "$kind")"
  status="$(k -n "$ns" get temporalcluster "$cluster" \
    -o jsonpath="{.status.persistence.schemaVersions.$store}")"
  actual="$(k -n "$ns" exec temporal-pg-1 -c postgres -- \
    psql -U postgres -d "$db" -Atc "SELECT curr_version FROM schema_version WHERE db_name = '$db'")"
  echo "$store store: required=$want status=$status database=$actual"
  if [ "$status" != "$want" ] || [ "$actual" != "$want" ]; then
    echo "FAIL: $store store is not at the schema version $version requires"
    rc=1
  fi
done
exit $rc
