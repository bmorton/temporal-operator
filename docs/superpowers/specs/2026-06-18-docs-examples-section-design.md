# Auto-generated Examples section in the docs site

**Date:** 2026-06-18

## Problem

The `examples/` directory holds curated `TemporalCluster` and related custom
resources (clusters, namespaces, search attributes, schedules), each in its own
directory with a `README.md` and one or more YAML manifests. None of this is
surfaced on the Hugo docs site. We want the examples published there, formatted
nicely, with **zero ongoing maintenance** — adding or editing an example should
update the site automatically with no manual regeneration or committed
duplication.

## Constraints and precedent

The repo already has the exact pattern we want for this:
`hack/build-preview.sh` (run via `make preview-wasm` in `.github/workflows/docs.yml`)
scans `examples/`, copies manifests, and writes a **git-ignored**
`docs/data/preview.json` consumed by a Hugo layout — "rebuilt on every docs
deploy, so the tool can never drift." We mirror that approach.

The docs site is Hugo (hugo-book theme), built in CI with
`hugo --source docs --minify`. Plain-markdown content under `docs/content/`
automatically gets the theme's sidebar, table of contents, and syntax
highlighting.

## Approach (chosen)

Generate git-ignored markdown content pages at build time from `examples/**`,
wired into the docs CI build. Rejected alternatives: generate-and-commit
(mirrors the CRD reference but can drift and isn't "free"); Hugo data + custom
layout (most code, more brittle than plain markdown).

## Components

### 1. Generator: `hack/build-examples-docs.sh`

A dependency-free bash script (consistent with `hack/build-preview.sh`).

- Resolves the repo root and writes into `docs/content/examples/` (git-ignored).
  The output directory is removed and recreated on each run so it is
  deterministic and safe to re-run.
- Writes a generated section index `docs/content/examples/_index.md` with front
  matter (`title = "Examples"`, a `weight`) and a table linking each example
  page. Each row's label comes from the example's title (see below).
- For every `examples/<dir>/` that contains at least one `*.yaml` file, writes
  `docs/content/examples/<dir>.md` containing:
  - Front matter: `title` derived from the example `README.md`'s first level-1
    (`# `) heading, falling back to the directory name; plus a `weight` assigned
    by sorted directory order (first dir = 10, then 20, 30, …) so the sidebar
    order is stable and matches the index table.
  - The README body with its leading H1 removed (the H1 becomes the page title
    via front matter, avoiding a duplicate heading).
  - A `## Manifests` heading, then for each `*.yaml` file (sorted by filename, so
    `00-shared.yaml` leads): a `### <filename>` heading followed by the file
    contents in a fenced ```yaml code block.
- Ordering is deterministic (directories and files sorted by name).

Edge cases:

- A directory with no `*.yaml` files is skipped.
- A directory with YAML but no `README.md` still renders; its title falls back
  to the directory name and the body is just the Manifests section.
- The top-level `examples/README.md` is not an example directory; it is ignored
  (the generated `_index.md` is the site's index for this section).

### 2. Make target: `docs-examples`

```make
.PHONY: docs-examples
docs-examples: ## Generate the Hugo examples pages from examples/ (git-ignored).
	./hack/build-examples-docs.sh
```

`docs-serve` and `docs-build` gain `docs-examples` as a prerequisite so local
preview always reflects the current `examples/` tree.

### 3. CI wiring: `.github/workflows/docs.yml`

Add a "Generate examples docs" step that runs `make docs-examples` before the
`hugo` build in both jobs that build the site:

- `preview-wasm` (runs on pull requests and pushes), and
- `build-deploy` (runs on push to `main`).

The step needs Go only if the script requires it; this script is pure bash, so
no extra toolchain setup is required beyond a checkout. It must run after
checkout and before the "Build site" step.

### 4. Git and lint hygiene

- `.gitignore`: add `docs/content/examples/` next to the other generated docs
  artifacts.
- Markdownlint in the `lint` job does not run the generator, so the generated
  files will not exist there and won't be linted. To keep local
  `markdownlint-cli2` runs clean, add `!docs/content/examples/**` to the lint
  globs (the workflow already excludes `docs/content/reference/**` and similar
  generated paths).

## Data flow

```
examples/<dir>/README.md + *.yaml
        │  (make docs-examples → hack/build-examples-docs.sh)
        ▼
docs/content/examples/_index.md + docs/content/examples/<dir>.md   (git-ignored)
        │  (hugo --source docs --minify)
        ▼
Rendered Examples section on the docs site (sidebar + ToC + highlighted YAML)
```

## Testing / verification

- Run `make docs-examples` and confirm `docs/content/examples/` contains an
  `_index.md` plus one page per example directory, with README prose and fenced
  YAML blocks for every manifest.
- Run `make docs-build` (Hugo Extended) and confirm the build succeeds and the
  Examples section renders, including the multi-file `schedules` example with
  `00-shared.yaml` listed first.
- Confirm `git status` shows no tracked changes under `docs/content/examples/`
  (the directory is git-ignored).
- Re-run the generator twice and confirm identical output (deterministic).

## Out of scope

- Search (site search is intentionally disabled).
- Per-example rendering beyond README + manifests (e.g., live preview embedding).
- Committing generated pages.
