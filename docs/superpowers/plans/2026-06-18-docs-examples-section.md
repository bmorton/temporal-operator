# Auto-generated Examples Docs Section — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish the `examples/` directory on the Hugo docs site as an auto-generated "Examples" section, rebuilt on every docs deploy so it can never drift and needs no manual maintenance.

**Architecture:** A dependency-free bash generator (`hack/build-examples-docs.sh`, mirroring `hack/build-preview.sh`) walks `examples/*/`, and for each directory containing YAML writes a git-ignored Hugo content page (`docs/content/examples/<dir>.md`) made of the dir's README body plus each manifest in a fenced ```yaml block. A generated `_index.md` provides the section landing page and index table. A `make docs-examples` target runs the generator; `docs-serve`/`docs-build` depend on it, and the two docs CI jobs run it before `hugo`.

**Tech Stack:** Bash, GNU coreutils/find, GNU Make, Hugo Extended (hugo-book theme), GitHub Actions.

---

## File Structure

- **Create** `hack/build-examples-docs.sh` — the generator. Reads `examples/`, writes `docs/content/examples/`.
- **Modify** `Makefile` — add the `docs-examples` target; make `docs-serve` and `docs-build` depend on it.
- **Modify** `.gitignore` — ignore `docs/content/examples/`.
- **Modify** `.github/workflows/docs.yml` — run `make docs-examples` before the Hugo build in both site-building jobs; add the generated path to the markdownlint/lychee excludes.

Generated output (never committed):
- `docs/content/examples/_index.md` — section landing + index table.
- `docs/content/examples/<dir>.md` — one page per example directory.

Notes / known limitations (acceptable for v1, out of scope to fix here):
- Relative links inside an example README (e.g. `./temporalcluster.yaml`) are emitted as-is and may not resolve on the rendered site.
- Generated pages are not in git/PR diffs (same trade-off as the existing preview tool).

---

## Task 1: Create the generator script

**Files:**
- Create: `hack/build-examples-docs.sh`

- [ ] **Step 1: Write the generator script**

Create `hack/build-examples-docs.sh` with exactly this content:

```bash
#!/usr/bin/env bash
# Generates Hugo content pages for each example under examples/ into
# docs/content/examples/ (git-ignored). Rebuilt on every docs deploy so the
# published examples can never drift from the examples/ directory.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
examples_root="${repo_root}/examples"
out_dir="${repo_root}/docs/content/examples"

# Start clean so output is deterministic and deleted examples disappear.
rm -rf "${out_dir}"
mkdir -p "${out_dir}"

index="${out_dir}/_index.md"
{
  printf '+++\n'
  printf 'title = "Examples"\n'
  printf 'weight = 75\n'
  printf 'bookCollapseSection = true\n'
  printf '+++\n\n'
  printf '# Examples\n\n'
  printf 'Curated `TemporalCluster` (and related) custom resources for common\n'
  printf 'scenarios. Each page renders the example README and its manifests.\n\n'
  printf 'These pages are generated from the\n'
  printf '[`examples/`](https://github.com/bmorton/temporal-operator/tree/main/examples)\n'
  printf 'directory; edit the examples there, not these pages.\n\n'
  printf '| Example | Manifests |\n'
  printf '| --- | --- |\n'
} > "${index}"

weight=10
while IFS= read -r dir; do
  name="$(basename "${dir}")"

  mapfile -t yaml_files < <(find "${dir}" -maxdepth 1 -name '*.yaml' -printf '%f\n' | sort)
  if [ "${#yaml_files[@]}" -eq 0 ]; then
    continue
  fi

  readme="${dir}/README.md"
  title="${name}"
  if [ -f "${readme}" ]; then
    h1="$(grep -m1 '^# ' "${readme}" || true)"
    if [ -n "${h1}" ]; then
      title="${h1#\# }"
    fi
  fi

  page="${out_dir}/${name}.md"
  {
    printf '+++\n'
    printf 'title = "%s"\n' "${title}"
    printf 'weight = %d\n' "${weight}"
    printf '+++\n\n'

    if [ -f "${readme}" ]; then
      # Drop the first level-1 heading; the title comes from front matter.
      awk 'skipped!=1 && /^# /{skipped=1; next} {print}' "${readme}"
      printf '\n'
    fi

    printf '## Manifests\n\n'
    for yf in "${yaml_files[@]}"; do
      printf '### %s\n\n' "${yf}"
      printf '```yaml\n'
      cat "${dir}/${yf}"
      printf '```\n\n'
    done
  } > "${page}"

  manifest_list="$(printf '`%s` ' "${yaml_files[@]}")"
  printf '| [%s](%s) | %s |\n' "${title}" "${name}" "${manifest_list% }" >> "${index}"

  weight=$((weight + 10))
done < <(find "${examples_root}" -mindepth 1 -maxdepth 1 -type d | sort)

echo "Generated examples docs in ${out_dir}"
```

- [ ] **Step 2: Make it executable**

Run: `chmod +x hack/build-examples-docs.sh`

- [ ] **Step 3: Run the generator and verify it fails-safe / produces output**

Run: `./hack/build-examples-docs.sh && ls docs/content/examples/`
Expected: prints `Generated examples docs in .../docs/content/examples` and the listing includes `_index.md` plus one `.md` per example dir that has YAML (e.g. `cluster-postgres-minimal.md`, `cluster-cnpg-integrated.md`, `cluster-with-namespaces-and-search-attributes.md`).

- [ ] **Step 4: Verify a single-file example page renders README + manifest**

Run: `sed -n '1,12p' docs/content/examples/cluster-postgres-minimal.md`
Expected: TOML front matter with `title = ...` and `weight = ...`, followed (after README body) by a `## Manifests` section and a ` ```yaml ` block. Confirm the page contains the manifest:
Run: `grep -c '^kind: TemporalCluster' docs/content/examples/cluster-postgres-minimal.md`
Expected: `1` or greater.

- [ ] **Step 5: Verify a multi-file example lists every manifest in sorted order**

Run: `grep -n '^### ' docs/content/examples/cluster-upgrade.md`
Expected: two headings in order — `### 01-temporalcluster-1.30.yaml` then `### 02-temporalcluster-1.31.yaml`.

- [ ] **Step 6: Verify the index table has a row per example**

Run: `grep -c '^| \[' docs/content/examples/_index.md`
Expected: equals the number of example dirs containing YAML (currently 11).

- [ ] **Step 7: Verify determinism (re-running yields identical output)**

Run:
```bash
find docs/content/examples -type f | sort | xargs sha256sum > /tmp/ex1.sha
./hack/build-examples-docs.sh >/dev/null
find docs/content/examples -type f | sort | xargs sha256sum > /tmp/ex2.sha
diff /tmp/ex1.sha /tmp/ex2.sha && echo DETERMINISTIC
```
Expected: prints `DETERMINISTIC` (no diff).

- [ ] **Step 8: Commit**

```bash
git add hack/build-examples-docs.sh
git commit -s -m "docs: add examples-to-Hugo generator script"
```

---

## Task 2: Wire the generator into Make and git-ignore the output

**Files:**
- Modify: `Makefile` (add `docs-examples`; add it as a prerequisite of `docs-serve` and `docs-build`)
- Modify: `.gitignore`

- [ ] **Step 1: Add the `docs-examples` target**

In `Makefile`, immediately before the existing `.PHONY: docs-serve` line, insert:

```make
.PHONY: docs-examples
docs-examples: ## Generate the Hugo examples pages from examples/ (git-ignored).
	./hack/build-examples-docs.sh

```

- [ ] **Step 2: Make `docs-serve` and `docs-build` depend on it**

In `Makefile`, change:

```make
docs-serve: ## Serve the documentation site locally at http://localhost:1313 (requires Hugo Extended).
	hugo server --source docs
```
to:
```make
docs-serve: docs-examples ## Serve the documentation site locally at http://localhost:1313 (requires Hugo Extended).
	hugo server --source docs
```

and change:

```make
docs-build: ## Build the documentation site into docs/public (requires Hugo Extended).
	hugo --source docs --minify
```
to:
```make
docs-build: docs-examples ## Build the documentation site into docs/public (requires Hugo Extended).
	hugo --source docs --minify
```

- [ ] **Step 3: Git-ignore the generated output**

In `.gitignore`, under the `# Hugo docs site` section, add this line after `docs/content/reference/.crd-reference-body.md`:

```gitignore
docs/content/examples/
```

- [ ] **Step 4: Verify the target works and output is ignored**

Run: `rm -rf docs/content/examples && make docs-examples && git status --porcelain docs/content/examples`
Expected: the generator runs and `git status --porcelain` prints **nothing** for that path (it is ignored).

- [ ] **Step 5: Commit**

```bash
git add Makefile .gitignore
git commit -s -m "docs: add docs-examples make target and ignore generated pages"
```

---

## Task 3: Run the generator in CI before the Hugo build

**Files:**
- Modify: `.github/workflows/docs.yml`

- [ ] **Step 1: Add a generate step to the PR build job**

In `.github/workflows/docs.yml`, in the `preview-wasm` job, insert a step immediately before the `- name: Build site (no deploy)` step:

```yaml
      - name: Generate examples docs
        run: make docs-examples
```

- [ ] **Step 2: Add a generate step to the deploy job**

In the `build-deploy` job, insert a step immediately before the `- name: Build site` step:

```yaml
      - name: Generate examples docs
        run: make docs-examples
```

- [ ] **Step 3: Exclude the generated path from the docs lint job**

In the `lint` job's "Lint Markdown" step, add `!docs/content/examples/**` to the `globs` block so it reads:

```yaml
          globs: |
            docs/**/*.md
            examples/**/*.md
            !docs/content/reference/**
            !docs/content/examples/**
            !docs/api/**
            !docs/themes/**
            !docs/superpowers/**
```

In the "Check links" step, add `--exclude-path docs/content/examples` to the `args` so the lychee exclude list also covers the generated pages:

```yaml
          args: --no-progress --accept 200,206,429 docs/**/*.md examples/**/*.md --exclude-path docs/content/reference --exclude-path docs/content/examples --exclude-path docs/api --exclude-path docs/themes --exclude-path docs/superpowers
```

- [ ] **Step 4: Validate the workflow YAML parses**

Run: `python3 -c "import yaml; yaml.safe_load(open('.github/workflows/docs.yml')); print('ok')"`
Expected: prints `ok`.

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/docs.yml
git commit -s -m "ci(docs): generate examples pages before building the site"
```

---

## Task 4: End-to-end verification with Hugo

**Files:** none (verification only)

- [ ] **Step 1: Generate and build the full site**

Run: `make docs-build`
Expected: the generator runs (`Generated examples docs in ...`) and Hugo finishes with `EN ... pages ... total in ... ms` and a non-error exit. No `ERROR` lines.

- [ ] **Step 2: Confirm the Examples section was rendered to HTML**

Run: `ls docs/public/examples/ && test -f docs/public/examples/index.html && echo SECTION_OK`
Expected: a directory listing that includes `index.html` and one subdirectory per example page (e.g. `cluster-postgres-minimal/`), then `SECTION_OK`.

- [ ] **Step 3: Confirm a manifest was rendered with syntax highlighting**

Run: `grep -rl 'language-yaml\|chroma' docs/public/examples/cluster-postgres-minimal/index.html && echo HIGHLIGHT_OK`
Expected: the file matches (Hugo's highlighter emits `chroma`/`language-yaml` markup), then `HIGHLIGHT_OK`.

- [ ] **Step 4: Confirm the build left no tracked changes**

Run: `git status --porcelain docs/content/examples docs/public`
Expected: prints nothing (both paths are git-ignored).

- [ ] **Step 5: Clean the local build artifacts**

Run: `rm -rf docs/public docs/content/examples`
Expected: no error. (These are regenerated on demand; nothing to commit.)

---

## Self-Review notes

- **Spec coverage:** generator (Task 1), make target + `docs-serve`/`docs-build` prereqs + gitignore (Task 2), CI wiring + lint excludes (Task 3), Hugo verification (Task 4). README-less dirs and dirs-without-YAML handled in the Task 1 script. All spec sections covered.
- **No formal unit-test framework:** the repo has no bats/shell test harness and the sibling `hack/build-preview.sh` has none; the executable `grep`/`diff`/Hugo-build checks in each task are the acceptance tests.
- **Determinism** is explicitly verified (Task 1 Step 7).
- **Weight `75`** places the section between `CRD Reference` (70) and `Tools` (80).
