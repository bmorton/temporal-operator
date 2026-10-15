---
name: temporal-version-bump
description: Add support for a new upstream Temporal release in temporal-operator — a Temporal server patch or minor version, a Temporal UI release, a Temporal CLI (dev server) release, or an s2s-proxy release. Use when asked to "support Temporal 1.x.y", "bump the UI", or when working an issue labeled `temporal-release`.
---

# Temporal version bump

Use this playbook to add an upstream Temporal release to the operator. Most
bumps change just a few lines. Getting one wrong ships a default image that
can't be pulled, or allows an upgrade path Temporal doesn't support. So check
every value against upstream and never guess one.

Issues labeled `temporal-release` are opened by
`.github/workflows/temporal-releases.yml` (script:
`hack/check-temporal-releases.sh`). The issue names the component, the version,
and the release notes link. Use them.

## Ground rules

- **Single source of truth:** `hack/version-matrix.yaml`. Never hand-edit
  `internal/temporal/versions_gen.go`; regenerate it with
  `make gen-version-matrix`.
- **Read the upstream release notes first.** Look for schema changes, removed
  or renamed dynamic config keys, changed defaults, and breaking changes, and
  note them in the PR description.
- **Verify images exist** before referencing them:
  `curl -fsS https://hub.docker.com/v2/namespaces/temporalio/repositories/<repo>/tags/<tag>`.
  The server needs both `server:<v>` and `admin-tools:<v>`.
- **Keep one release per PR.** Don't also refactor or bump unrelated deps.
- Follow `AGENTS.md`: Conventional Commits, `git commit -s` (DCO is enforced),
  and run `make helm-chart` only if API types/RBAC changed (a version bump
  normally does not).
- Commit type: use `feat(versions):` for a new minor version or a new default
  UI/CLI/proxy version, and `fix(versions):` for a patch release that only adds
  `patchVersions` entries. If unsure, use `feat(versions):`.

## Patch release (e.g. server 1.31.3 → 1.31.4)

1. In `hack/version-matrix.yaml`, append the version to that minor's
   `patchVersions` (keep them ascending). Keep existing patches unless the
   release notes say one is broken or unsafe, and if you drop one, say why in
   the PR.
2. Leave the `minSchema*` values alone. If the release notes or a new
   directory under upstream `schema/*/temporal/versioned/` show a schema
   change in a patch release (rare), don't edit the values. Call it out in
   the PR description for a human to review.
3. `make gen-version-matrix`.
4. If the nightly upgrade matrix (`.github/workflows/nightly.yml`) or
   `test/e2e/upgrade/` uses an older patch of this minor as an upgrade
   endpoint, you may move it to the new patch. That's optional; skip it if the
   release is very new.
5. Validate (see below).

A patch release for an **older** supported minor (e.g. 1.30.8 when 1.32 is
newest) is handled the same way. It's still worth supporting because those
releases are usually security fixes.

## New minor release (e.g. server 1.33.0)

Use commit `9252008` ("feat: add Temporal 1.32 upgrade path") as the
reference implementation and mirror its shape.

1. **Matrix entry**: add a new block to `hack/version-matrix.yaml` after the
   previous minor:
   - `version`, `patchVersions`
   - `minSchemaSQL`, `minSchemaCassandra`, `minSchemaES`: copy from the
     previous minor. If upstream added a new directory under
     `schema/*/temporal/versioned/` between the two release tags, list it in
     the PR description for human review instead of guessing new values.
     (The operator's numbering doesn't map 1:1 to upstream directory names.)
   - `allowedFromVersions`: normally just the previous minor (Temporal only
     supports upgrading one minor at a time)
   - `uiSeries` / `defaultUIVersion`: the UI release paired with this server
     release (the UI release published alongside it; check its notes)
   - `devServerCLIVersion`: a `temporalio/temporal` CLI tag that bundles this
     server version (see "CLI release")
   - `cassandraVisibilitySupported`
   - `removedDynamicConfig` / `addedDynamicConfig`: from the release notes.
     Removed keys are stripped from user config by
     `internal/temporal/dynamicconfig.go`, so getting this list wrong breaks
     clusters.
2. `make gen-version-matrix`.
3. **Unit tests** in `internal/temporal/versions_test.go`: move the
   "latest" assertions to the new version, and add an "adjacent minor"
   upgrade case plus a "minor skip" rejection case for it.
4. **Golden files**: in `internal/temporal/configtemplate_test.go`, point the
   golden version and directory at the new minor, then run
   `make test-golden-update` to write `internal/temporal/testdata/golden/<minor>/`.
   Diff the new directory against the previous minor's. Any difference must
   be explained by the release notes. Remove the oldest golden directory only
   if no test references it any more.
5. **Upgrade e2e**: rename and update the files in `test/e2e/upgrade/` (and
   `chainsaw-test.yaml`, `01-assert.yaml`, `02-assert.yaml`) so the test
   upgrades from the previous minor's latest patch to the new minor.
6. **Nightly**: add a `from`/`to` row to `.github/workflows/nightly.yml` for
   previous-minor-latest → new-minor.
7. **Examples/docs**: update `examples/cluster-upgrade/` and the
   `examples/README.md` row to the new pair. Search the docs for the old
   "latest" version (`grep -rn '<old>' docs/content examples`) and update any
   statement about what's newest. Leave pinned versions in unrelated e2e
   suites (e.g. `admin-tools:1.31.1`) alone unless asked.
8. Validate (see below).

## UI release

The UI follows its own version scheme. Each server minor pairs with a UI
series (`uiSeries`) and a known-good `defaultUIVersion`.

1. Read the UI release notes and decide which server minor(s) it supports.
   Usually a new UI release targets only the newest server minor.
2. For each such minor, set `defaultUIVersion` (and `uiSeries` if the UI minor
   changed) in `hack/version-matrix.yaml`. Don't move older server minors to a
   UI release that requires newer server APIs.
3. `make gen-version-matrix`, then update tests that pin the UI image
   (`grep -rn 'temporalio/ui:' internal`). Some pin an older minor's default
   on purpose, so only change pins whose minor you changed.
4. Validate.

## CLI release (dev server)

`devServerCLIVersion` maps each server minor to the `temporalio/temporal`
CLI image that runs `TemporalDevServer` for it. A new CLI release matters only
if it bundles a server version in a minor we support.

1. Find the bundled server version. Check the CLI release notes, or the
   `go.temporal.io/server` version in the CLI's `go.mod` at that tag:
   `gh api repos/temporalio/cli/contents/go.mod?ref=v<cli> --jq .content | base64 -d | grep go.temporal.io/server`.
2. Set `devServerCLIVersion` for that server minor. Prefer a plain stable tag
   (`1.9.1`) over a suffixed preview build (`1.8.3-server-1.32.0-162.0`) when
   both bundle the same minor.
3. `make gen-version-matrix`, and update
   `internal/controller/temporaldevserver_controller_test.go` and any
   `devserver-image` mapping tests if their expected tag changed.
4. If the CLI bundles a server minor newer than anything in the matrix, don't
   add it. Note in the issue that it's blocked on the server minor bump.
5. Validate.

## s2s-proxy release

The default proxy image lives in `internal/resources/clusterproxy.go`
(`DefaultClusterProxyImage`) and the TemporalClusterProxy webhook. Update both
to the new `vX.Y.Z` tag, update tests that assert the image, and check the
release notes for config format changes to the generated proxy config.

## Validate

Run all of these. Every one must pass before you open the PR:

```sh
make gen-version-matrix   # versions_gen.go must be regenerated and committed
make test                 # unit + envtest, including golden files
make lint
govulncheck ./...  # optional, CI runs it
```

Then confirm:

- `git grep -n '<new version>'` shows the matrix, `versions_gen.go`, and
  (for minors) tests, golden files, e2e, nightly, and examples. Nothing else
  should be missing.
- The PR description lists: the version(s) added, links to the upstream release
  notes, any schema / dynamic-config / breaking notes, and which validation
  you ran. Reference the issue with `Closes #<n>`.
