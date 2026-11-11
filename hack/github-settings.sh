#!/usr/bin/env bash
# github-settings applies the repository settings that CI relies on but that
# live outside the repo: merge methods, the "main" ruleset (required PR,
# linear history, squash only, required status checks), and the security
# features (secret scanning, push protection, Dependabot alerts and security
# updates, CodeQL default setup). It is idempotent: run it whenever a required
# check is renamed or a new gate is added, so the settings stay reviewable here
# instead of drifting in the GitHub UI.
#
# Requires `gh` authenticated as a repository admin.
#
# Usage:
#   hack/github-settings.sh            # apply
#   DRY_RUN=1 hack/github-settings.sh  # print what would be sent
set -euo pipefail

repo="${GH_REPO:-bmorton/temporal-operator}"
ruleset_name="main"
dry_run="${DRY_RUN:-0}"

# Required status checks: the job *names* (not ids) of the gates every PR to
# main must pass. Jobs skipped by hack/ci-changes.sh report "skipped", which
# GitHub counts as passing. Workflows gated by trigger `paths:` filters (Docs,
# zizmor) never report on unrelated PRs and must not be listed here.
required_checks=(
  "Lint"                    # ci.yml
  "Test"                    # ci.yml
  "Build image"             # ci.yml
  "govulncheck"             # ci.yml
  "Verify generated chart"  # ci.yml
  "DCO check"               # dco.yml
  "E2E result"              # e2e.yml (aggregate of the matrix)
  "PR title"                # pr-title.yml
)
github_actions_app_id=15368

api() {
  local method="$1" path="$2"
  shift 2
  if [ "$dry_run" = "1" ]; then
    echo "DRY RUN: gh api -X $method $path $*" >&2
    if [ -n "${body:-}" ]; then echo "$body" | jq . >&2; fi
    return 0
  fi
  if [ -n "${body:-}" ]; then
    gh api -X "$method" "$path" "$@" --input - <<<"$body"
  else
    gh api -X "$method" "$path" "$@"
  fi
}

echo "== Merge methods and secret scanning"
body='{
  "allow_merge_commit": false,
  "allow_rebase_merge": false,
  "allow_squash_merge": true,
  "allow_auto_merge": true,
  "delete_branch_on_merge": true,
  "squash_merge_commit_title": "PR_TITLE",
  "squash_merge_commit_message": "PR_BODY",
  "security_and_analysis": {
    "secret_scanning": {"status": "enabled"},
    "secret_scanning_push_protection": {"status": "enabled"}
  }
}'
api PATCH "repos/$repo" --jq '{allow_merge_commit,allow_rebase_merge,allow_auto_merge,squash_merge_commit_title,squash_merge_commit_message,secret_scanning:.security_and_analysis.secret_scanning.status,push_protection:.security_and_analysis.secret_scanning_push_protection.status}'

echo "== Dependabot alerts and security updates"
body=
api PUT "repos/$repo/vulnerability-alerts" && echo "vulnerability alerts: enabled"
api PUT "repos/$repo/automated-security-fixes" && echo "dependabot security updates: enabled"

echo "== CodeQL default setup"
body='{"state":"configured","query_suite":"default","languages":["go","actions","javascript-typescript"]}'
# Returns 409 when it is already configured identically; that's fine.
api PATCH "repos/$repo/code-scanning/default-setup" >/dev/null || true
body=
api GET "repos/$repo/code-scanning/default-setup" --jq '{state,languages}'

echo "== Ruleset: $ruleset_name"
checks_json="$(printf '%s\n' "${required_checks[@]}" |
  jq -R --argjson app "$github_actions_app_id" '{context: ., integration_id: $app}' | jq -s .)"
body="$(jq -n --arg name "$ruleset_name" --argjson checks "$checks_json" '{
  name: $name,
  target: "branch",
  enforcement: "active",
  bypass_actors: [
    {actor_id: 5, actor_type: "RepositoryRole", bypass_mode: "always"}
  ],
  conditions: {ref_name: {include: ["~DEFAULT_BRANCH"], exclude: []}},
  rules: [
    {type: "deletion"},
    {type: "non_fast_forward"},
    {type: "required_linear_history"},
    {type: "pull_request", parameters: {
      required_approving_review_count: 0,
      dismiss_stale_reviews_on_push: false,
      require_code_owner_review: false,
      require_last_push_approval: false,
      required_review_thread_resolution: false,
      allowed_merge_methods: ["squash"]
    }},
    {type: "required_status_checks", parameters: {
      strict_required_status_checks_policy: false,
      do_not_enforce_on_create: false,
      required_status_checks: $checks
    }}
  ]
}')"
existing_id="$(gh api "repos/$repo/rulesets" --jq ".[] | select(.name == \"$ruleset_name\") | .id" | head -n1)"
if [ -n "$existing_id" ]; then
  api PUT "repos/$repo/rulesets/$existing_id" --jq '{id,name,enforcement,rules:[.rules[].type]}'
else
  api POST "repos/$repo/rulesets" --jq '{id,name,enforcement,rules:[.rules[].type]}'
fi

if [ "$dry_run" != "1" ]; then
  echo "== Required checks now:"
  gh api "repos/$repo/rulesets" --jq ".[] | select(.name == \"$ruleset_name\") | .id" | head -n1 |
    xargs -I{} gh api "repos/$repo/rulesets/{}" \
      --jq '.rules[] | select(.type=="required_status_checks") | .parameters.required_status_checks[].context'
fi

# Not applied here on purpose:
#   actions/permissions sha_pinning_required=true would also apply to the
#   reusable SLSA generator workflow, which must be referenced by tag
#   (see release.yml), and would break releases.
