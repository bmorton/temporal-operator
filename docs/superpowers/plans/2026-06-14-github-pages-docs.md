# GitHub Pages Documentation Site Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and publish the `docs/` content as a Hugo site on GitHub Pages at `https://bmorton.github.io/temporal-operator/`.

**Architecture:** Hugo (Extended) with the `hugo-book` theme renders the existing `docs/content/` tree. A `build-deploy` job in `.github/workflows/docs.yml` builds the site and publishes it via the official GitHub Pages Actions (`configure-pages` → `upload-pages-artifact` → `deploy-pages`) on every push to `main`. The CRD reference page is generated (never hand-edited) by a Makefile target.

**Tech Stack:** Hugo Extended, `hugo-book` theme (git submodule), GitHub Actions, GitHub Pages, `crd-ref-docs`.

**Reference spec:** `docs/superpowers/specs/2026-06-14-github-pages-docs-design.md`

---

## File Structure

| Path | Responsibility | Action |
|---|---|---|
| `docs/hugo.toml` | Hugo site config (baseURL, theme, Book params) | Create |
| `docs/themes/hugo-book` | Theme (git submodule, pinned tag) | Create |
| `.gitmodules` | Submodule registration | Create |
| `docs/content/_index.md` | Home landing page front matter | Modify |
| `docs/content/getting-started/_index.md` | Section front matter | Modify |
| `docs/content/installation/_index.md` | Section front matter | Modify |
| `docs/content/install/verifying-releases.md` | Leaf page front matter | Modify |
| `docs/content/architecture/_index.md` | Section front matter | Modify |
| `docs/content/operations/_index.md` | Section front matter | Modify |
| `docs/content/upgrades/_index.md` | Section front matter | Modify |
| `docs/content/troubleshooting/_index.md` | Section front matter | Modify |
| `docs/content/contributing/_index.md` | Section front matter | Modify |
| `docs/content/reference/_index.md` | Generated CRD reference section page | Create (generated) |
| `docs/content/reference/crds.md` | Old generated reference page | Delete |
| `Makefile` | `docs-crd-reference`, `docs-serve`, `docs-build` targets | Modify |
| `.gitignore` | Ignore Hugo build artifacts | Modify |
| `.github/workflows/docs.yml` | Add `build-deploy` job | Modify |

---

## Task 1: Local Hugo toolchain + theme + site config

**Files:**
- Create: `docs/hugo.toml`
- Create: `docs/themes/hugo-book` (submodule), `.gitmodules`
- Modify: `.gitignore`

- [ ] **Step 1: Install Hugo Extended locally (for verification)**

Run:
```bash
HUGO_VERSION=0.140.2
curl -fsSL "https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_extended_${HUGO_VERSION}_linux-amd64.tar.gz" | tar -xz -C /tmp hugo
sudo mv /tmp/hugo /usr/local/bin/hugo
hugo version
```
Expected: a line containing `hugo v0.140.2` and the word `extended`.

- [ ] **Step 2: Add the hugo-book theme as a pinned submodule**

Run:
```bash
cd /workspaces/temporal-operator
git submodule add https://github.com/alex-shpak/hugo-book docs/themes/hugo-book
# Pin to the latest release tag for reproducibility:
LATEST_TAG=$(git -C docs/themes/hugo-book tag --list 'v*' --sort=-v:refname | head -1)
echo "Pinning hugo-book to ${LATEST_TAG}"
git -C docs/themes/hugo-book checkout "${LATEST_TAG}"
git add .gitmodules docs/themes/hugo-book
```
Expected: `.gitmodules` created; `docs/themes/hugo-book` staged at a tagged commit. Record `${LATEST_TAG}` in the commit message.

- [ ] **Step 3: Create the Hugo site config**

Create `docs/hugo.toml`:
```toml
baseURL = "https://bmorton.github.io/temporal-operator/"
title = "temporal-operator"
theme = "hugo-book"
languageCode = "en-us"

# hugo-book renders the section/page tree as the left sidebar.
disablePathToLower = true

[params]
  BookTheme = "auto"
  BookToC = true
  BookRepo = "https://github.com/bmorton/temporal-operator"
  BookEditPath = "edit/main/docs"
  # Search is intentionally disabled for now (see spec "Out of scope").
  BookSearch = false

[markup.goldmark.renderer]
  unsafe = true
```

- [ ] **Step 4: Ignore Hugo build artifacts**

Append to `.gitignore`:
```gitignore

# Hugo docs site
docs/public/
docs/resources/
.hugo_build.lock
```

- [ ] **Step 5: Verify the site builds**

Run:
```bash
cd /workspaces/temporal-operator
hugo --source docs --minify
```
Expected: build succeeds (exit 0), prints a "Pages" count > 0, and creates `docs/public/index.html`. Warnings about missing pages are acceptable at this stage.

Run:
```bash
test -f docs/public/index.html && echo OK
```
Expected: `OK`

- [ ] **Step 6: Commit**

```bash
cd /workspaces/temporal-operator
git add .gitmodules docs/themes/hugo-book docs/hugo.toml .gitignore
git commit -s -m "docs: add Hugo site config and hugo-book theme submodule

Pin hugo-book to <LATEST_TAG>.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 2: Add navigation front matter to content pages

**Files:**
- Modify: `docs/content/_index.md`, `docs/content/getting-started/_index.md`, `docs/content/installation/_index.md`, `docs/content/install/verifying-releases.md`, `docs/content/architecture/_index.md`, `docs/content/operations/_index.md`, `docs/content/upgrades/_index.md`, `docs/content/troubleshooting/_index.md`, `docs/content/contributing/_index.md`

> For each file, **prepend** the TOML front matter block (including both `+++` fences and a trailing blank line) to the very top of the existing file. Do not modify the existing body.

- [ ] **Step 1: Home page** — prepend to `docs/content/_index.md`:
```toml
+++
title = "temporal-operator"
type = "docs"
+++

```

- [ ] **Step 2: Getting Started** — prepend to `docs/content/getting-started/_index.md`:
```toml
+++
title = "Getting Started"
weight = 10
+++

```

- [ ] **Step 3: Installation** — prepend to `docs/content/installation/_index.md`:
```toml
+++
title = "Installation"
weight = 20
+++

```

- [ ] **Step 4: Verifying Releases** — prepend to `docs/content/install/verifying-releases.md`:
```toml
+++
title = "Verifying Releases"
weight = 25
+++

```

- [ ] **Step 5: Architecture** — prepend to `docs/content/architecture/_index.md`:
```toml
+++
title = "Architecture"
weight = 30
+++

```

- [ ] **Step 6: Operations** — prepend to `docs/content/operations/_index.md`:
```toml
+++
title = "Operations"
weight = 40
+++

```

- [ ] **Step 7: Upgrades** — prepend to `docs/content/upgrades/_index.md`:
```toml
+++
title = "Upgrades"
weight = 50
+++

```

- [ ] **Step 8: Troubleshooting** — prepend to `docs/content/troubleshooting/_index.md`:
```toml
+++
title = "Troubleshooting"
weight = 60
+++

```

- [ ] **Step 9: Contributing** — prepend to `docs/content/contributing/_index.md`:
```toml
+++
title = "Contributing"
weight = 80
+++

```

- [ ] **Step 10: Verify titles and ordering**

Run:
```bash
cd /workspaces/temporal-operator
hugo --source docs list all | cut -d, -f1,8
```
Expected: rows listing each content page; the site renders (no build error).

Run:
```bash
hugo --source docs --minify && grep -o 'Getting Started' docs/public/index.html | head -1
```
Expected: build succeeds and prints `Getting Started` (sidebar contains the section).

- [ ] **Step 11: Commit**

```bash
cd /workspaces/temporal-operator
git add docs/content
git commit -s -m "docs: add Hugo front matter for nav titles and ordering

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 3: Generate the CRD reference as a section page

**Files:**
- Modify: `Makefile` (add `docs-crd-reference` target)
- Create (generated): `docs/content/reference/_index.md`
- Delete: `docs/content/reference/crds.md`

- [ ] **Step 1: Add the Makefile target**

Add to `Makefile` immediately after the existing `api-docs` target (after the line that outputs `docs/api/v1alpha1.md`):
```makefile

.PHONY: docs-crd-reference
docs-crd-reference: crd-ref-docs ## Generate the Hugo CRD reference page (docs/content/reference/_index.md).
	@mkdir -p docs/content/reference
	@printf '+++\ntitle = "CRD Reference"\nweight = 70\n+++\n\n' > docs/content/reference/_index.md
	"$(CRD_REF_DOCS)" --source-path=./api/v1alpha1 --config=hack/crd-ref-docs-config.yaml --renderer=markdown --output-path=/tmp/crd-reference-body.md
	@cat /tmp/crd-reference-body.md >> docs/content/reference/_index.md
	@rm -f /tmp/crd-reference-body.md docs/content/reference/crds.md
	@echo "Generated docs/content/reference/_index.md"
```

- [ ] **Step 2: Generate the reference page**

Run:
```bash
cd /workspaces/temporal-operator
make docs-crd-reference
```
Expected: prints `Generated docs/content/reference/_index.md`; `docs/content/reference/crds.md` is removed.

- [ ] **Step 3: Verify front matter + content**

Run:
```bash
head -6 docs/content/reference/_index.md
```
Expected:
```
+++
title = "CRD Reference"
weight = 70
+++

# API Reference
```

Run:
```bash
test ! -e docs/content/reference/crds.md && echo "crds.md removed"
```
Expected: `crds.md removed`

- [ ] **Step 4: Verify the site still builds with the reference page**

Run:
```bash
cd /workspaces/temporal-operator
hugo --source docs --minify && grep -o 'CRD Reference' docs/public/index.html | head -1
```
Expected: build succeeds and prints `CRD Reference`.

- [ ] **Step 5: Commit**

```bash
cd /workspaces/temporal-operator
git add Makefile docs/content/reference
git commit -s -m "docs: generate CRD reference as a Hugo section page

Replace docs/content/reference/crds.md with a generated _index.md that
carries Hugo front matter, via a new 'make docs-crd-reference' target.

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 4: Local-dev Makefile targets

**Files:**
- Modify: `Makefile` (add `docs-serve`, `docs-build`)

- [ ] **Step 1: Add the targets**

Add to `Makefile` immediately after the `docs-crd-reference` target:
```makefile

.PHONY: docs-serve
docs-serve: ## Serve the documentation site locally at http://localhost:1313 (requires Hugo Extended).
	hugo server --source docs

.PHONY: docs-build
docs-build: ## Build the documentation site into docs/public (requires Hugo Extended).
	hugo --source docs --minify
```

- [ ] **Step 2: Verify `make docs-build`**

Run:
```bash
cd /workspaces/temporal-operator
make docs-build
test -f docs/public/index.html && echo OK
```
Expected: build succeeds and prints `OK`.

- [ ] **Step 3: Commit**

```bash
cd /workspaces/temporal-operator
git add Makefile
git commit -s -m "docs: add docs-serve and docs-build Makefile targets

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 5: GitHub Actions build + deploy job

**Files:**
- Modify: `.github/workflows/docs.yml`

- [ ] **Step 1: Replace the trailing comment with a deploy job**

In `.github/workflows/docs.yml`, delete the trailing comment block:
```yaml

  # NOTE: production deployment (e.g. Cloudflare Pages) requires repository
  # secrets and a configured Hugo/Docsy site; wire it here when available.
```
and append this job (sibling of `lint`, under `jobs:`):
```yaml

  build-deploy:
    name: Build and deploy site
    if: github.event_name == 'push' && github.ref == 'refs/heads/main'
    runs-on: namespace-profile-temporal-operator
    permissions:
      contents: read
      pages: write
      id-token: write
    concurrency:
      group: pages
      cancel-in-progress: false
    environment:
      name: github-pages
      url: ${{ steps.deployment.outputs.page_url }}
    steps:
      - uses: actions/checkout@v4
        with:
          submodules: recursive
          fetch-depth: 0
      - name: Setup Pages
        id: pages
        uses: actions/configure-pages@v5
      - name: Setup Hugo
        uses: peaceiris/actions-hugo@v3
        with:
          hugo-version: "0.140.2"
          extended: true
      - name: Build site
        run: hugo --source docs --minify --baseURL "${{ steps.pages.outputs.base_url }}/"
      - name: Upload artifact
        uses: actions/upload-pages-artifact@v3
        with:
          path: docs/public
      - name: Deploy to GitHub Pages
        id: deployment
        uses: actions/deploy-pages@v4
```

- [ ] **Step 2: Validate workflow YAML**

Run:
```bash
cd /workspaces/temporal-operator
python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/docs.yml')); print('YAML OK')"
```
Expected: `YAML OK`

- [ ] **Step 3: Commit**

```bash
cd /workspaces/temporal-operator
git add .github/workflows/docs.yml
git commit -s -m "ci(docs): build and deploy the Hugo site to GitHub Pages

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 6: Enable GitHub Pages (one-time, manual)

**Files:** none (repository setting via API)

- [ ] **Step 1: Set the Pages build source to GitHub Actions**

Run:
```bash
gh api --method POST repos/bmorton/temporal-operator/pages -f build_type=workflow || \
gh api --method PUT  repos/bmorton/temporal-operator/pages -f build_type=workflow
```
Expected: a JSON response (POST) or no error (PUT). If POST returns HTTP 409 (already exists), the PUT fallback updates it.

- [ ] **Step 2: Confirm the setting**

Run:
```bash
gh api repos/bmorton/temporal-operator/pages -q '.build_type'
```
Expected: `workflow`

---

## Task 7: Open PR, merge, and verify the live site

**Files:** none

- [ ] **Step 1: Push the branch and open a PR**

Run:
```bash
cd /workspaces/temporal-operator
git push -u origin feat/docs-github-pages
gh pr create --title "feat(docs): publish documentation to GitHub Pages with Hugo" \
  --body "Implements docs/superpowers/specs/2026-06-14-github-pages-docs-design.md: Hugo + hugo-book site deployed to GitHub Pages on push to main."
```
Expected: PR URL printed.

- [ ] **Step 2: After merge, watch the deploy**

Run (after merging to `main`):
```bash
cd /workspaces/temporal-operator
gh run list --workflow=docs.yml --limit 1 --json databaseId,status,conclusion
```
Expected: a run that completes with `conclusion: success`.

- [ ] **Step 3: Verify the site is live**

Run:
```bash
curl -sfI https://bmorton.github.io/temporal-operator/ | head -1
```
Expected: `HTTP/2 200`.

---

## Notes for the implementer

- **Hugo Extended is mandatory** — `hugo-book` compiles SCSS; the plain Hugo build will fail.
- **Generated files are never hand-edited.** `docs/content/reference/_index.md` and `docs/api/v1alpha1.md` come from `crd-ref-docs`; both directories are excluded from markdownlint/lychee in `docs.yml`.
- **Submodule checkout in CI** requires `submodules: recursive` on `actions/checkout` (already in the deploy job).
- **Pages on GitHub Pro from a private repo** publishes a publicly reachable site at its URL; viewer access control is Enterprise-only. This is expected and acceptable per the spec.
