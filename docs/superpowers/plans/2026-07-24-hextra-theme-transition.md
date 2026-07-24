# Hextra Theme Transition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the docs site's `hugo-book` theme with **Hextra** (installed as a Hugo Module), add a landing page and a `content/docs/` section, enable built-in search, and preserve all live URLs — without disturbing the Resource Preview WASM tool.

**Architecture:** The Hugo site in `docs/` becomes a Hugo Module that imports `github.com/imfing/hextra`. Documentation content moves under `content/docs/` behind a Hextra home landing page at `/`. Old URLs are preserved with front-matter `aliases`. Generated content (CRD reference, examples) and the standalone `/preview/` tool are repointed/left intact.

**Tech Stack:** Hugo 0.140.2 (extended), Hugo Modules (Go), Hextra theme, Node 22 + Tailwind v4/daisyUI (preview tool only), GitHub Actions → GitHub Pages.

## Global Constraints

- Hugo **0.164.0 extended** — current Hextra (>=0.10) requires Hugo >=0.146.0 (uses the `try` function), so the previous 0.140.2 pin was bumped to 0.164.0. Keep CI and local at >=0.146.0.
- Hextra theme pinned to **v0.12.3** in `docs/go.mod`.
- The FlexSearch index file emitted by Hextra is `en.search-data.json` (verify this, not `index.json`).
- Do not run bare `hugo mod get` in CI (it upgrades to `latest`); let Hugo auto-download modules from `go.mod` on build, or pin the exact version.
- `baseURL = "https://temporal-operator.bmorton.dev/"` and `title = "temporal-operator"` — unchanged.
- Hextra theme version MUST be pinned to a specific released tag in `docs/go.mod` (no floating `latest`).
- `[markup.goldmark.renderer] unsafe = true` MUST be retained (content uses raw HTML).
- The `/preview/` tool (`docs/layouts/preview/`, `docs/assets/`, `docs/static/preview/`, `docs/data/preview.json`, `content/preview/_index.md`) MUST remain unchanged and reachable at `/preview/`.
- Generated content is never hand-edited: CRD reference via `make api-docs docs-crd-reference`; examples via `make docs-examples`. The `Verify generated docs` CI job must still pass.
- Commits: Conventional Commits, DCO sign-off (`git commit -s`), and the trailer `Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>`.
- Section sidebar weights (keep): getting-started=10, installation=20, architecture=30, operations=40, upgrades=50, troubleshooting=60, reference=70, examples=75, contributing=80, tools=80.
- Commit commands below omit `-s`/trailer for brevity — always add them.

---

## File Structure

- `docs/go.mod`, `docs/go.sum` — **create** (Hugo module; imports Hextra).
- `.gitmodules` — **modify** (remove the hugo-book submodule entry).
- `docs/themes/hugo-book` — **remove** (submodule).
- `docs/hugo.toml` — **rewrite** (Hextra config).
- `docs/content/_index.md` — **rewrite** (Hextra landing page).
- `docs/content/docs/_index.md` — **create** (docs section root; from old root index).
- `docs/content/docs/<section>/…` — **move** (all existing sections via `git mv`).
- `docs/content/preview/_index.md` — **unchanged** (stays at root).
- `docs/layouts/shortcodes/button.html` — **create** (self-contained CTA button).
- `hack/build-examples-docs.sh` — **modify** (output path + drop hugo-book front matter/H1).
- `Makefile` — **modify** (`docs-crd-reference` output path).
- `.gitignore` — **modify** (repoint generated paths).
- `.github/workflows/docs.yml` — **modify** (module cache, drop theme submodule, lint globs).

---

## Task 1: Repoint generators & .gitignore to `content/docs/`

Move generated-content output ahead of the theme swap so the reference/examples land in their new home.

**Files:**
- Modify: `Makefile` (`docs-crd-reference` target, ~lines 354-361)
- Modify: `hack/build-examples-docs.sh` (out_dir + front matter)
- Modify: `.gitignore` (lines 34-35)

**Interfaces:**
- Produces: generated CRD reference at `docs/content/docs/reference/_index.md`; examples at `docs/content/docs/examples/`.

- [ ] **Step 1: Show current output paths (baseline)**

Run: `grep -n "content/reference\|content/examples" Makefile hack/build-examples-docs.sh .gitignore`
Expected: matches showing the old `docs/content/reference` and `docs/content/examples` paths.

- [ ] **Step 2: Repoint the `docs-crd-reference` Makefile target**

In `Makefile`, replace every `docs/content/reference` with `docs/content/docs/reference` inside the `docs-crd-reference` target:

```makefile
.PHONY: docs-crd-reference
docs-crd-reference: crd-ref-docs ## Generate the Hugo CRD reference page (docs/content/docs/reference/_index.md).
	@mkdir -p docs/content/docs/reference
	@printf '+++\ntitle = "CRD Reference"\nweight = 70\n+++\n\n' > docs/content/docs/reference/_index.md
	"$(CRD_REF_DOCS)" --source-path=./api/v1alpha1 --config=hack/crd-ref-docs-config.yaml --renderer=markdown --output-path=docs/content/docs/reference/.crd-reference-body.md
	@cat docs/content/docs/reference/.crd-reference-body.md >> docs/content/docs/reference/_index.md
	@rm -f docs/content/docs/reference/.crd-reference-body.md docs/content/docs/reference/crds.md
	@echo "Generated docs/content/docs/reference/_index.md"
```

- [ ] **Step 3: Repoint the examples generator and drop hugo-book front matter**

In `hack/build-examples-docs.sh`, change the output dir and remove the hugo-book-only `bookCollapseSection` key and the body `# Examples` H1 (Hextra renders the title from front matter). Replace:

```bash
out_dir="${repo_root}/docs/content/examples"
```
with:
```bash
out_dir="${repo_root}/docs/content/docs/examples"
```

And in the index heredoc, replace the front-matter/heading block:
```bash
  printf '+++\n'
  printf 'title = "Examples"\n'
  printf 'weight = 75\n'
  printf 'bookCollapseSection = true\n'
  printf '+++\n\n'
  printf '# Examples\n\n'
  printf 'Curated `TemporalCluster` (and related) custom resources for common\n'
```
with:
```bash
  printf '+++\n'
  printf 'title = "Examples"\n'
  printf 'weight = 75\n'
  printf '+++\n\n'
  printf 'Curated `TemporalCluster` (and related) custom resources for common\n'
```

- [ ] **Step 4: Repoint `.gitignore`**

In `.gitignore`, replace:
```
docs/content/reference/.crd-reference-body.md
docs/content/examples/
```
with:
```
docs/content/docs/reference/.crd-reference-body.md
docs/content/docs/examples/
```

- [ ] **Step 5: Run the generators and verify new output locations**

Run: `make docs-crd-reference docs-examples && ls docs/content/docs/reference/_index.md docs/content/docs/examples/_index.md`
Expected: both files exist under `docs/content/docs/`; command exits 0. (The old `docs/content/reference/_index.md` still exists from git — it is moved in Task 2.)

- [ ] **Step 6: Commit**

```bash
git add Makefile hack/build-examples-docs.sh .gitignore
git commit -m "build(docs): repoint generated docs output under content/docs"
```

---

## Task 2: Install Hextra as a Hugo Module and swap the theme (green build)

The anchor task: remove hugo-book, install Hextra via Hugo Modules, rewrite the config, move content under `content/docs/`, add the landing page, the custom button shortcode, and strip hugo-book-only front matter — ending with a clean `hugo` build.

**Files:**
- Remove: `docs/themes/hugo-book` (submodule); Modify: `.gitmodules`
- Create: `docs/go.mod`, `docs/go.sum`
- Rewrite: `docs/hugo.toml`
- Move: `docs/content/{getting-started,installation,architecture,operations,upgrades,troubleshooting,reference,contributing,tools}` → under `docs/content/docs/`
- Create: `docs/content/docs/_index.md`, `docs/content/_index.md` (rewrite), `docs/layouts/shortcodes/button.html`

**Interfaces:**
- Consumes: generated paths from Task 1 (`content/docs/reference`, `content/docs/examples`).
- Produces: a Hextra-themed site that builds with zero errors; docs served under `/docs/…`; landing at `/`.

- [ ] **Step 1: Verify current build works (baseline), then confirm it will break after theme removal**

Run: `hugo --source docs --quiet && echo BUILD_OK`
Expected: `BUILD_OK` (current hugo-book build). This confirms the starting point before we swap.

- [ ] **Step 2: Remove the hugo-book submodule**

```bash
git submodule deinit -f docs/themes/hugo-book
git rm -f docs/themes/hugo-book
rm -rf .git/modules/docs/themes/hugo-book
```

Then remove the submodule stanza from `.gitmodules`:
```
[submodule "docs/themes/hugo-book"]
	path = docs/themes/hugo-book
	url = https://github.com/alex-shpak/hugo-book
```
If `.gitmodules` is now empty, delete it: `git rm -f .gitmodules` (only if empty).

- [ ] **Step 3: Initialize the Hugo module and add Hextra**

```bash
cd docs
hugo mod init github.com/bmorton/temporal-operator/docs
hugo mod get github.com/imfing/hextra@v0.11.0
cd ..
```

Note: pin to the latest stable Hextra tag available (verify with `hugo mod get github.com/imfing/hextra@<tag>`; `v0.11.0` shown as example — use the newest released tag). This creates `docs/go.mod` and `docs/go.sum`.

- [ ] **Step 4: Rewrite `docs/hugo.toml` for Hextra**

Replace the entire file with:

```toml
baseURL = "https://temporal-operator.bmorton.dev/"
title = "temporal-operator"
languageCode = "en-us"

enableInlineShortcodes = true
enableGitInfo = true

[module]
  [[module.imports]]
    path = "github.com/imfing/hextra"

[markup]
  [markup.goldmark.renderer]
    unsafe = true
  [markup.highlight]
    noClasses = false

[menu]
  [[menu.main]]
    name = "Documentation"
    pageRef = "/docs"
    weight = 1
  [[menu.main]]
    name = "Search"
    weight = 2
    [menu.main.params]
      type = "search"
  [[menu.main]]
    name = "GitHub"
    weight = 3
    url = "https://github.com/bmorton/temporal-operator"
    [menu.main.params]
      icon = "github"

[params]
  [params.navbar]
    displayTitle = true
    displayLogo = false
  [params.theme]
    default = "system"
    displayToggle = true
  [params.footer]
    enable = true
    displayCopyright = true
    displayPoweredBy = true
  [params.editURL]
    enable = true
    base = "https://github.com/bmorton/temporal-operator/edit/main/docs/content"
  [params.search]
    enable = true
    type = "flexsearch"
    [params.search.flexsearch]
      index = "content"
      tokenize = "forward"
```

- [ ] **Step 5: Move all doc sections under `content/docs/`**

```bash
cd docs
mkdir -p content/docs
for d in getting-started installation architecture operations upgrades troubleshooting reference contributing tools; do
  git mv "content/$d" "content/docs/$d"
done
cd ..
```

(The generated `content/docs/reference/_index.md` from Task 1 and the git-tracked `content/reference/_index.md` may collide — if `git mv content/reference` fails because `content/docs/reference` exists, run `git rm -r content/reference` and keep the generated copy: `rm -rf content/reference` was already avoided; instead `git rm -r content/reference` then `make docs-crd-reference`.)

- [ ] **Step 6: Create the docs section root `content/docs/_index.md`**

```markdown
---
title: Documentation
weight: 1
---

A Kubernetes operator that manages the full lifecycle of [Temporal](https://temporal.io)
clusters — persistence and schema management, mTLS, the UI, monitoring,
controlled version upgrades, and declarative namespaces, search attributes,
schedules, and client credentials — plus disposable dev servers for local
development and CI.

{{< cards >}}
  {{< card link="getting-started" title="Getting Started" subtitle="Zero to a running cluster." >}}
  {{< card link="installation" title="Installation" subtitle="Helm, OLM, or kustomize." >}}
  {{< card link="architecture" title="Architecture" subtitle="How the operator and CRDs fit together." >}}
  {{< card link="operations" title="Operations" subtitle="Status, conditions, day-2." >}}
  {{< card link="upgrades" title="Upgrades" subtitle="Moving between Temporal versions." >}}
  {{< card link="troubleshooting" title="Troubleshooting" subtitle="Common failures." >}}
  {{< card link="reference" title="CRD Reference" subtitle="The full API." >}}
{{< /cards >}}
```

- [ ] **Step 7: Rewrite `content/_index.md` as a Hextra landing page**

```markdown
---
title: temporal-operator
layout: hextra-home
---

<div class="hx-mt-6 hx-mb-6">
{{< hextra/hero-headline >}}
  Kubernetes operator for Temporal
{{< /hextra/hero-headline >}}
</div>

<div class="hx-mb-12">
{{< hextra/hero-subtitle >}}
  Manage the full lifecycle of Temporal clusters — persistence, mTLS, the UI,
  version upgrades, and declarative resources — the Kubernetes way.
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx-mb-6">
{{< hextra/hero-button text="Get Started" link="docs/getting-started" >}}
</div>

{{< hextra/feature-grid >}}
  {{< hextra/feature-card title="Getting Started" subtitle="Zero to a running Temporal cluster on kind." link="docs/getting-started" >}}
  {{< hextra/feature-card title="Installation" subtitle="Install via Helm, OLM, or kustomize." link="docs/installation" >}}
  {{< hextra/feature-card title="Architecture" subtitle="How the operator and CRDs fit together." link="docs/architecture" >}}
  {{< hextra/feature-card title="Operations" subtitle="Status, conditions, and day-2 operations." link="docs/operations" >}}
  {{< hextra/feature-card title="CRD Reference" subtitle="The full API reference." link="docs/reference" >}}
  {{< hextra/feature-card title="Resource Preview" subtitle="See what the operator would create, in your browser." link="preview" >}}
{{< /hextra/feature-grid >}}
```

- [ ] **Step 8: Add a self-contained `button` shortcode**

Hextra has no `button` shortcode; the tools page uses one. Create `docs/layouts/shortcodes/button.html`:

```html
{{- $href := .Get "href" | relURL -}}
<a href="{{ $href }}" class="hx-inline-flex hx-items-center hx-rounded-md hx-border hx-border-transparent hx-bg-primary-600 hx-px-4 hx-py-2 hx-text-sm hx-font-semibold hx-text-white hover:hx-bg-primary-700" style="text-decoration:none;background:#3b82f6;color:#fff;padding:0.5rem 1rem;border-radius:0.375rem;display:inline-block;">
  {{- .Inner -}}
</a>
```

- [ ] **Step 9: Strip hugo-book-only front matter and duplicate H1s from moved pages**

For every moved page under `content/docs/`, remove any `bookCollapseSection = true` line, and remove the leading level-1 `# Heading` line from the body (Hextra renders the title from front matter, so the body H1 is a duplicate). Files needing the H1 removed include (non-exhaustive — check each): `content/docs/getting-started/_index.md`, `content/docs/installation/_index.md`, `content/docs/architecture/_index.md`, `content/docs/operations/_index.md`, `content/docs/upgrades/_index.md`, `content/docs/troubleshooting/_index.md`, `content/docs/contributing/_index.md`, `content/docs/tools/_index.md`, `content/docs/tools/resource-preview.md`, and any leaf pages (`installation/azure.md`, `installation/verifying-releases.md`, `operations/authentication.md`).

Verify remaining hugo-book keys are gone:
Run: `grep -rn "bookCollapseSection\|BookToC\|BookSection" docs/content || echo NONE_LEFT`
Expected: `NONE_LEFT`.

- [ ] **Step 10: Reconcile the reference page (generated vs moved)**

Ensure the CRD reference lives only at `content/docs/reference/_index.md`:

```bash
rm -rf docs/content/reference
make docs-crd-reference docs-examples
ls docs/content/docs/reference/_index.md docs/content/docs/examples/_index.md
```
Expected: both files present.

- [ ] **Step 11: Build the site and verify zero errors**

Run: `hugo mod get && hugo --source docs --minify 2>&1 | tail -20`
Expected: build completes, no `ERROR`/`WARN` about missing shortcodes or layouts; `docs/public/` regenerated.

- [ ] **Step 12: Spot-check rendered output**

Run: `ls docs/public/index.html docs/public/docs/getting-started/index.html docs/public/preview/index.html docs/public/index.json`
Expected: all exist (`index.json` is the FlexSearch index — confirms search is enabled). 

Run: `grep -l "temporal-operator-preview.wasm" docs/public/preview/index.html`
Expected: match (preview tool intact).

- [ ] **Step 13: Commit**

```bash
git add -A
git commit -m "feat(docs): migrate documentation site to the Hextra theme"
```

---

## Task 3: Preserve old URLs with aliases

Moving sections under `/docs/` changes their URLs. Add `aliases` so every previous URL redirects to its new location.

**Files:**
- Modify: each moved section/page `_index.md` / `.md` under `docs/content/docs/`

**Interfaces:**
- Consumes: the `content/docs/…` structure from Task 2.
- Produces: redirect stubs in `docs/public/` at every old path.

- [ ] **Step 1: List the old URLs that must keep working (baseline)**

The pre-migration top-level URLs were:
`/getting-started/`, `/installation/`, `/installation/azure/`, `/installation/verifying-releases/`, `/architecture/`, `/operations/`, `/operations/authentication/`, `/upgrades/`, `/troubleshooting/`, `/reference/`, `/contributing/`, `/tools/`, `/tools/resource-preview/`.
(`/preview/` is unchanged — no alias.)

- [ ] **Step 2: Add `aliases` to each section index**

Add an `aliases` key to the front matter of each moved `_index.md`. Example for `content/docs/installation/_index.md` (TOML front matter):

```toml
+++
title = "Installation"
weight = 20
aliases = ["/installation/"]
+++
```

Apply the analogous single-entry alias to: getting-started (`/getting-started/`), architecture (`/architecture/`), operations (`/operations/`), upgrades (`/upgrades/`), troubleshooting (`/troubleshooting/`), reference (`/reference/`), contributing (`/contributing/`), tools (`/tools/`).

- [ ] **Step 3: Add `aliases` to leaf pages**

- `content/docs/installation/azure.md` → `aliases = ["/installation/azure/"]`
- `content/docs/installation/verifying-releases.md` → `aliases = ["/installation/verifying-releases/"]`
- `content/docs/operations/authentication.md` → `aliases = ["/operations/authentication/"]`
- `content/docs/tools/resource-preview.md` → `aliases = ["/tools/resource-preview/"]`

- [ ] **Step 4: Rebuild and verify redirect stubs exist**

Run: `hugo --source docs --quiet && for u in getting-started installation installation/azure installation/verifying-releases architecture operations operations/authentication upgrades troubleshooting reference contributing tools tools/resource-preview; do test -f "docs/public/$u/index.html" && grep -q 'http-equiv="refresh"' "docs/public/$u/index.html" && echo "OK $u" || echo "MISSING $u"; done`
Expected: `OK` for every path, no `MISSING`.

- [ ] **Step 5: Commit**

```bash
git add docs/content/docs
git commit -m "docs: preserve pre-Hextra URLs with aliases"
```

---

## Task 4: Light content polish (callouts)

Convert note/warning blockquotes to Hextra `callout`s where they exist. Keep this focused — no prose rewrites.

**Files:**
- Modify: `docs/content/docs/tools/resource-preview.md` and any page with a `>` note/warning block.

**Interfaces:**
- Consumes: migrated content.
- Produces: rendered callouts; build stays green.

- [ ] **Step 1: Find candidate note/warning blocks (baseline)**

Run: `grep -rn "^> \*\*" docs/content/docs`
Expected: at least the "**Alpha.**" note in `tools/resource-preview.md`; note each match.

- [ ] **Step 2: Convert the Alpha note to a callout**

In `content/docs/tools/resource-preview.md`, replace:
```markdown
> **Alpha.** The tool currently supports `TemporalCluster`. It uses placeholder
> credentials when rendering configuration, so secret values shown are not real.
```
with:
```markdown
{{< callout type="warning" >}}
**Alpha.** The tool currently supports `TemporalCluster`. It uses placeholder
credentials when rendering configuration, so secret values shown are not real.
{{< /callout >}}
```

Convert any other blockquote notes found in Step 1 analogously (`type="info"` for neutral notes, `type="warning"` for cautions).

- [ ] **Step 3: Rebuild and verify**

Run: `hugo --source docs --quiet && echo BUILD_OK`
Expected: `BUILD_OK`, no shortcode errors.

- [ ] **Step 4: Commit**

```bash
git add docs/content/docs
git commit -m "docs: convert notes to Hextra callouts"
```

---

## Task 5: Update CI workflow and final verification

Adjust `docs.yml` for Hugo Modules (no theme submodule, add module cache) and update lint globs; then run the full local verification the CI job performs.

**Files:**
- Modify: `.github/workflows/docs.yml`

**Interfaces:**
- Consumes: everything above.
- Produces: a CI workflow that builds the Hextra site; a fully verified local build.

- [ ] **Step 1: Update lint ignore globs**

In the `lint` job of `.github/workflows/docs.yml`, update the markdownlint `globs` and lychee `args` to the new generated paths and drop the vendored-theme exclusion. Replace `!docs/content/reference/**` with `!docs/content/docs/reference/**`, `!docs/content/examples/**` with `!docs/content/docs/examples/**`, and remove the `!docs/themes/**` line (markdownlint) / `--exclude-path docs/themes` (lychee). Keep `!docs/superpowers/**` / `--exclude-path docs/superpowers`.

- [ ] **Step 1b: Repoint the `verify-generated` job's generated-docs diff path**

In the `verify-generated` job, the staleness check diffs the generated CRD reference. Update the moved path: replace `docs/content/reference/_index.md` with `docs/content/docs/reference/_index.md` in both the `git diff --quiet -- …` line and the `git --no-pager diff -- …` line.

- [ ] **Step 1c: Bump the Hugo version in both build jobs**

In the `Setup Hugo` step of BOTH the `preview-wasm` and `build-deploy` jobs (`peaceiris/actions-hugo`), change `hugo-version: "0.140.2"` to `hugo-version: "0.164.0"` (current Hextra >=0.10 requires Hugo >=0.146.0). Keep `extended: true`.

- [ ] **Step 2: Remove theme submodule checkout and add Hugo module cache**

In the `preview-wasm` and `build-deploy` jobs: on the `actions/checkout` step, remove `submodules: recursive` (it existed only for the vendored theme). Keep `fetch-depth: 0` on `build-deploy` for `enableGitInfo`. After the "Setup Hugo" step in each build job, add a module cache before the build:

```yaml
      - name: Cache Hugo modules
        uses: actions/cache@v4
        with:
          path: /home/runner/.cache/hugo_mod
          key: hugo-mod-${{ hashFiles('docs/go.sum') }}
          restore-keys: hugo-mod-
```

Do NOT add a bare `hugo mod get` step — it upgrades the theme to `latest`, which may be incompatible with the pinned Hugo. Hugo automatically downloads the modules pinned in `docs/go.mod` during the build (`hugo --source docs`), so no explicit fetch step is needed. (Go is already set up in these jobs.)

- [ ] **Step 3: Verify the workflow references only new paths**

Run: `grep -n "content/reference\|content/examples\|themes\|submodules: recursive\|0.140.2" .github/workflows/docs.yml || echo CLEAN`
Expected: no references to old `content/reference`, `content/examples`, `themes`, `submodules: recursive`, or the old Hugo version `0.140.2`. Adjust until `CLEAN`.

- [ ] **Step 4: Full local verification (mirrors CI)**

```bash
make preview-wasm docs-examples docs-crd-reference
cd docs && PATH="$PWD/node_modules/.bin:$PATH" hugo --minify 2>&1 | tail -15 && cd ..
npx --yes markdownlint-cli2 "docs/content/docs/**/*.md" "#docs/content/docs/reference/**" "#docs/content/docs/examples/**" 2>&1 | tail -5 || true
```
Expected: WASM builds, Hugo build completes with no errors (a `.Site.Data` deprecation WARN from Hextra's own theme code is expected and harmless), markdownlint reports no violations on hand-written docs.

- [ ] **Step 5: Verify search index, landing, docs, preview, and an alias in the built site**

Run: `ls docs/public/en.search-data.json docs/public/index.html docs/public/docs/installation/index.html && grep -q 'http-equiv="refresh"' docs/public/installation/index.html && grep -q 'preview.wasm' docs/public/preview/index.html && echo ALL_OK`
Expected: `ALL_OK`. (The Hextra FlexSearch index is `en.search-data.json`.)

- [ ] **Step 6: Commit**

```bash
git add .github/workflows/docs.yml
git commit -m "ci(docs): build Hextra site via Hugo modules on Hugo 0.164"
```

---

## Self-Review (completed)

- **Spec coverage:** Modules install (T2), remove submodule (T2), landing + `content/docs/` (T2), aliases (T3), search enabled (T2 config, verified via `index.json`), config/front-matter/shortcode migration (T2), button shortcode (T2), callouts (T4), generator repointing (T1), `.gitignore` (T1), CI updates (T5), preview tool untouched (constraints + T2/T5 verification). All spec sections map to a task.
- **Placeholder scan:** No TBD/TODO; all created files show full content; Hextra tag pinned (verify newest at execution).
- **Type/path consistency:** `content/docs/reference` and `content/docs/examples` used consistently across T1/T2/T5; `hugo mod get` used in T2/T5/CI; alias paths match the enumerated old URLs.
