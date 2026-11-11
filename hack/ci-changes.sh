#!/usr/bin/env bash
# ci-changes decides which expensive CI suites a pull request needs, from the
# list of files it changes (one path per line on stdin). It prints
# GITHUB_OUTPUT-style lines:
#
#   go=true|false    lint, unit/envtest, image build, govulncheck, chart check
#   e2e=true|false   kind + chainsaw e2e suites
#
# A suite is skipped only when *every* changed file is known not to affect it,
# so anything unrecognized (new directories, build files, ...) runs everything.
# A workflow file always triggers its own suite, so action bumps (e.g. from
# Dependabot) are still exercised by the workflows that use them.
#
# Release PRs (HEAD_REF=release-please--*) always run everything: they only
# touch the changelog and manifest, but they are the gate for the release and
# e2e doesn't run on pushes to main, so they are the only e2e run against the
# combined state being released.
#
# Usage:
#   gh pr diff 123 --name-only | HEAD_REF=my-branch hack/ci-changes.sh
set -euo pipefail

if [[ "${HEAD_REF:-}" == release-please--* ]]; then
  cat >/dev/null
  echo "go=true"
  echo "e2e=true"
  exit 0
fi

# Files that affect neither the Go build nor e2e.
inert='^(docs/|examples/|\.claude/|\.devcontainer/)'
inert+='|\.md$'
inert+='|^(LICENSE|\.gitignore|release-please-config\.json|\.release-please-manifest\.json)$'
inert+='|^\.github/(ISSUE_TEMPLATE/|CODEOWNERS$|PULL_REQUEST_TEMPLATE\.md$|copilot-instructions\.md$|dependabot\.yml$)'
inert+='|^hack/(check-temporal-releases|github-settings)\.sh$'

# Workflow files are inert except for the suite they define.
go_self='^\.github/workflows/ci\.yml$'
e2e_self='^\.github/workflows/e2e\.yml$'
workflows='^\.github/workflows/'

# Dev tooling (golangci-lint, controller-gen, ...) matters to the Go checks
# but not to e2e, which builds the operator image from the main module.
e2e_inert='^hack/tools/'

go=false
e2e=false
while IFS= read -r f; do
  [ -z "$f" ] && continue
  if [[ "$f" =~ $inert ]]; then
    continue
  fi
  if [[ "$f" =~ $workflows ]]; then
    [[ "$f" =~ $go_self ]] && go=true
    [[ "$f" =~ $e2e_self ]] && e2e=true
    continue
  fi
  go=true
  [[ "$f" =~ $e2e_inert ]] || e2e=true
done

echo "go=$go"
echo "e2e=$e2e"
