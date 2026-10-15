#!/usr/bin/env bash
# check-temporal-releases reports upstream Temporal releases the operator does
# not yet support, and (unless DRY_RUN=1) opens one GitHub issue per release so
# it can be assigned to an agent. The issue body points at the version-bump
# playbook in .claude/skills/temporal-version-bump/SKILL.md.
#
# Tracked components and where the operator's current version comes from:
#
#   server     temporalio/temporal   hack/version-matrix.yaml patchVersions
#   ui         temporalio/ui         hack/version-matrix.yaml defaultUIVersion
#   cli        temporalio/cli        hack/version-matrix.yaml devServerCLIVersion
#   s2s-proxy  temporalio/s2s-proxy  DefaultClusterProxyImage in internal/resources
#
# A release only counts once its container image(s) are on Docker Hub, so an
# issue is never opened for a version that cannot be pulled yet.
#
# Usage:
#   DRY_RUN=1 hack/check-temporal-releases.sh   # print, don't open issues
#   GH_TOKEN=... hack/check-temporal-releases.sh
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
matrix="$root/hack/version-matrix.yaml"
playbook=".claude/skills/temporal-version-bump/SKILL.md"
label="temporal-release"
dry_run="${DRY_RUN:-0}"

# stable_releases <repo> prints X.Y.Z for non-draft, non-prerelease releases
# whose tag is exactly vX.Y.Z (skips suffixed builds like v0.2.1-1.32.0).
stable_releases() {
  gh api --paginate "repos/$1/releases?per_page=100" \
    --jq '.[] | select(.draft|not) | select(.prerelease|not) | .tag_name' |
    grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sed 's/^v//' | sort -uV
}

# max_version prints the highest version read from stdin.
max_version() { sort -V | tail -n1; }

# gt <a> <b> succeeds when version a > version b.
gt() { [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | max_version)" = "$1" ]; }

# image_exists <repo> <tag> checks Docker Hub for temporalio/<repo>:<tag>.
image_exists() {
  curl -fsS -o /dev/null 2>/dev/null "https://hub.docker.com/v2/namespaces/temporalio/repositories/$1/tags/$2"
}

# matrix_values <key> prints every quoted value of a scalar key in the matrix.
matrix_values() {
  grep -E "^\s+$1:" "$matrix" | sed -E 's/.*"([^"]+)".*/\1/'
}

found=0

# report <component> <version> <release-url> <images> <guidance>
report() {
  local component="$1" version="$2" url="$3" images="$4" guidance="$5"
  local title="Support ${component} ${version}"
  found=$((found + 1))
  echo "new: ${title} (${url})"
  [ "$dry_run" = "1" ] && return 0

  if gh issue list --state all --label "$label" --search "\"${title}\" in:title" \
    --json title --jq '.[].title' | grep -Fxq "$title"; then
    echo "  issue already exists, skipping"
    return 0
  fi

  gh issue create --label "$label" --title "$title" --body "$(cat <<EOF
A new upstream release is available that temporal-operator does not support yet.

- **Component:** ${component}
- **Version:** \`${version}\`
- **Release notes:** ${url}
- **Images (verified on Docker Hub):** ${images}

## What to do

${guidance}

Follow the version-bump playbook in [\`${playbook}\`](../blob/main/${playbook}).
It lists every file to change, the generators to run, and the checks that must
pass. Read the upstream release notes before editing anything. If they mention
schema changes, removed dynamic config, or breaking changes, apply what the
playbook says for that case and summarize it in the PR description.

_Opened automatically by \`.github/workflows/temporal-releases.yml\`._
EOF
)"
}

check_server() {
  local releases minors max_minor min_minor
  releases="$(stable_releases temporalio/temporal)"
  minors="$(grep -E '^\s+- version:' "$matrix" | sed -E 's/.*"([^"]+)".*/\1/' | sort -V)"
  min_minor="$(head -n1 <<<"$minors")"
  max_minor="$(tail -n1 <<<"$minors")"

  # Newest release per minor at or above the oldest supported minor.
  local minor latest current
  for minor in $(sed -E 's/^([0-9]+\.[0-9]+)\..*/\1/' <<<"$releases" | sort -uV); do
    gt "$min_minor" "$minor" && continue
    latest="$(grep -E "^${minor//./\\.}\." <<<"$releases" | max_version)"
    current="$(awk -v m="\"$minor\"" '
      $0 ~ "- version: " m {f=1; next}
      f && /- version:/ {f=0}
      f && /patchVersions:/ {print}' "$matrix" |
      grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | max_version || true)"
    if [ -n "$current" ] && ! gt "$latest" "$current"; then
      continue
    fi
    image_exists server "$latest" && image_exists admin-tools "$latest" || {
      echo "skip: server $latest images not published yet"
      continue
    }
    local guidance
    if [ -n "$current" ]; then
      guidance="This is a **patch release** of the supported ${minor} line (currently up to ${current}). Add it to that line's \`patchVersions\` (see \"Patch release\" in the playbook)."
    elif gt "$minor" "$max_minor"; then
      guidance="This is a **new minor version** (newest supported is ${max_minor}). Add a new matrix entry, upgrade path, golden files, and e2e/nightly coverage (see \"New minor release\" in the playbook)."
    else
      continue
    fi
    report "Temporal server" "$latest" "https://github.com/temporalio/temporal/releases/tag/v${latest}" \
      "\`temporalio/server:${latest}\`, \`temporalio/admin-tools:${latest}\`" "$guidance"
  done
}

check_ui() {
  local latest current
  latest="$(stable_releases temporalio/ui | max_version)"
  current="$(matrix_values defaultUIVersion | max_version)"
  gt "$latest" "$current" || return 0
  image_exists ui "$latest" || { echo "skip: ui $latest image not published yet"; return 0; }
  report "Temporal UI" "$latest" "https://github.com/temporalio/ui/releases/tag/v${latest}" \
    "\`temporalio/ui:${latest}\`" \
    "The newest \`defaultUIVersion\` in the matrix is ${current}. Decide which server minor(s) this UI release should pair with and update \`uiSeries\`/\`defaultUIVersion\` (see \"UI release\" in the playbook)."
}

check_cli() {
  local latest current
  latest="$(stable_releases temporalio/cli | max_version)"
  # Matrix values may be suffixed builds (1.8.3-server-1.32.0-162.0); compare cores.
  current="$(matrix_values devServerCLIVersion | sed -E 's/-.*//' | max_version)"
  gt "$latest" "$current" || return 0
  image_exists temporal "$latest" || { echo "skip: cli $latest image not published yet"; return 0; }
  report "Temporal CLI" "$latest" "https://github.com/temporalio/cli/releases/tag/v${latest}" \
    "\`temporalio/temporal:${latest}\`" \
    "The newest \`devServerCLIVersion\` in the matrix is based on ${current}. Find which server version this CLI bundles and update \`devServerCLIVersion\` for that minor (see \"CLI release\" in the playbook)."
}

check_s2s_proxy() {
  local current latest
  current="$(grep -rhoE 'DefaultClusterProxyImage = "temporalio/s2s-proxy:v[0-9.]+"' "$root/internal" 2>/dev/null |
    grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n1 || true)"
  if [ -z "$current" ]; then
    echo "skip: s2s-proxy is not pinned in this checkout"
    return 0
  fi
  latest="$(stable_releases temporalio/s2s-proxy | max_version)"
  gt "$latest" "$current" || return 0
  image_exists s2s-proxy "v$latest" || { echo "skip: s2s-proxy v$latest image not published yet"; return 0; }
  report "Temporal s2s-proxy" "$latest" "https://github.com/temporalio/s2s-proxy/releases/tag/v${latest}" \
    "\`temporalio/s2s-proxy:v${latest}\`" \
    "The operator currently defaults to s2s-proxy v${current}. Update the default proxy image (see \"s2s-proxy release\" in the playbook)."
}

if [ "$dry_run" != "1" ]; then
  gh label create "$label" --color 5319e7 \
    --description "New upstream Temporal release to support" 2>/dev/null || true
fi

check_server
check_ui
check_cli
check_s2s_proxy

echo "${found} unsupported release(s) found"
