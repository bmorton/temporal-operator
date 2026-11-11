# Design: Migrate the documentation site to the Hextra Hugo theme

## Status

Approved (2026-07-24).

## Problem

The documentation site under `docs/` currently uses the **`hugo-book`** theme
(vendored as a git submodule at `docs/themes/hugo-book`). `hugo-book` is
functional but visually dated and limited: no built-in search (it is
intentionally disabled today), a plain docs-only landing page, and a modest
component set. We want a cleaner, more modern documentation experience modeled
after [Hextra](https://imfing.github.io/hextra/docs/) — a marketing-style
landing page, a polished sidebar, built-in full-text search, and richer
shortcodes (cards, callouts, steps, tabs) — while preserving the site's live
URLs, the custom Resource Preview tool, and the existing generated content.

References:

- Hextra docs: <https://imfing.github.io/hextra/docs/>
- Hextra guide: <https://imfing.github.io/hextra/docs/guide/>
- Hextra source: <https://github.com/imfing/hextra>
- Starter template: <https://github.com/imfing/hextra-starter-template>

## Constraints & context

- **Live site.** The site is deployed to GitHub Pages on the custom domain
  `temporal-operator.bmorton.dev` (see `docs/static/CNAME`). Existing inbound
  URLs must keep working after the migration.
- **Custom Resource Preview tool.** `/preview/` is a standalone interactive
  WebAssembly tool. It is theme-independent: it uses its own full-page layout
  (`docs/layouts/preview/list.html`), its own Tailwind v4 + daisyUI stylesheet
  (`docs/assets/css/preview.css`, compiled via Hugo's `css.TailwindCSS`), Alpine.js,
  highlight.js, and a Go-compiled `.wasm` binary built by `hack/build-preview.sh`
  (`make preview-wasm`). It must continue to work unchanged, at the same URL.
- **Generated content, never hand-edited:**
  - CRD reference — `make api-docs docs-crd-reference` writes
    `docs/api/v1alpha1.md` and the Hugo reference page (currently
    `docs/content/reference/_index.md`).
  - Examples — `make docs-examples` (`hack/build-examples-docs.sh`) generates
    example pages (currently under `docs/content/examples/`, git-ignored).
  - The `Verify generated docs` CI job fails if these are stale.
- **Build toolchain.** Hugo `0.140.2` **extended**, Go (already set up in CI for
  the WASM build), Node 22 (for the preview tool's Tailwind/daisyUI CSS).
- **Pre-1.0 project.** Keep the change focused; this is a docs-only migration and
  must not touch operator behavior.

## Chosen approach

Swap `hugo-book` for **Hextra**, installed the idiomatic way as a **Hugo
Module**, restructure content into a landing page plus a `content/docs/`
section, enable Hextra's built-in search, and preserve all existing URLs with
`aliases`. Do a focused re-theme with light polish (landing/section cards,
callouts) rather than a wholesale content rewrite. Ship as a single PR.

### Decisions (with alternatives considered)

1. **Theme install: Hugo Modules** (chosen) over git submodule or vendoring.
   Matches the Hextra starter, enables clean upgrades (`hugo mod get -u`), keeps
   the theme out of the tree. CI already has Go. Adds a dedicated `docs/go.mod`
   (separate from the operator's root module).
2. **Structure: full Hextra layout** (chosen) — a styled landing page at `/`
   plus docs under `content/docs/`, over keeping docs at the root. Most
   idiomatic Hextra experience; URL churn mitigated with aliases.
3. **Search: enabled** (chosen) — Hextra's built-in FlexSearch indexes at build
   time, needs no external service, works on GitHub Pages.
4. **Migration depth: re-theme + light polish** (chosen) — migrate content
   as-is (front matter, one shortcode), add landing + section `cards`, convert
   note/warning blocks to `callout`s. No open-ended prose rewrite.

## Detailed design

### 1. Theme installation & build (Hugo Modules)

- Remove the hugo-book submodule: `git rm docs/themes/hugo-book`, delete its
  entry from `.gitmodules` (removing the file if it becomes empty), and remove
  the now-empty `docs/themes/` directory.
- Initialize a Hugo module for the docs site:
  `hugo mod init github.com/bmorton/temporal-operator/docs`, producing
  `docs/go.mod` (and `docs/go.sum` once the theme is fetched). This module is
  independent of the operator's root `go.mod` and exists only for theme
  resolution.
- Import Hextra in `docs/hugo.toml`:

  ```toml
  [module]
    [[module.imports]]
      path = "github.com/imfing/hextra"
  ```

- Pin Hextra to a specific released tag in `docs/go.mod` (no floating version),
  so builds are reproducible.
- Hugo stays at `0.140.2` extended (satisfies Hextra's minimum).

### 2. Content restructuring & URL preservation

- Create `content/docs/` and move existing sections into it: `getting-started/`,
  `installation/`, `architecture/`, `operations/`, `upgrades/`,
  `troubleshooting/`, `reference/`, `contributing/`, `tools/`.
- New **landing page** `content/_index.md` using Hextra's home layout: hero
  (title, tagline, "Get Started" CTA) plus feature `cards` linking into the docs
  (mirroring the current index's bullet list).
- `content/docs/_index.md` becomes the docs section root (adapted from the old
  root `_index.md`).
- **`/preview/` stays at root**, unchanged: `content/preview/_index.md`,
  `layouts/preview/list.html`, `assets/`, and `static/preview/` are untouched, so
  the tool's URL and behavior are preserved. Its `content/tools/resource-preview.md`
  landing page moves under `content/docs/tools/` with the rest of the docs.
- **URL preservation via `aliases`.** Moving sections under `/docs/` changes URLs
  (e.g. `/installation/` → `/docs/installation/`). Every moved section/page gets
  an `aliases` front-matter entry for its previous path so Hugo emits redirect
  stubs. The full list of current top-level URLs to alias:
  `/getting-started/`, `/installation/` (+ `azure`, `verifying-releases`),
  `/architecture/`, `/operations/` (+ `authentication`), `/upgrades/`,
  `/troubleshooting/`, `/reference/`, `/contributing/`, `/tools/`
  (+ `resource-preview`). `/preview/` is unchanged (no alias needed).

### 3. Generated-content repointing

- `make docs-crd-reference` (Makefile): write to
  `docs/content/docs/reference/_index.md` instead of
  `docs/content/reference/_index.md`.
- `make docs-examples` (`hack/build-examples-docs.sh`): generate under
  `docs/content/docs/examples/` instead of `docs/content/examples/`.
- `.gitignore`: repoint the generated paths
  (`docs/content/docs/reference/.crd-reference-body.md`,
  `docs/content/docs/examples/`). The preview tool's git-ignored generated paths
  (`docs/static/preview/*`, `docs/data/preview.json`) are unchanged.

### 4. Config, front matter & shortcodes

- **Rewrite `docs/hugo.toml`** for Hextra. Keep `baseURL` and `title`. Replace
  all `Book*` params with Hextra config:
  - `[module]` import (section 1).
  - `enableInlineShortcodes = true`, `[markup.goldmark.renderer] unsafe = true`
    (kept), Hextra-compatible `[markup.highlight]` settings.
  - Navbar: display title/logo and a GitHub link to the repo.
  - `editURL` pointing at `edit/main/docs/content` (Hextra's edit-this-page).
  - Search enabled (FlexSearch) via Hextra's `[params.search]`.
  - `[params.footer]` with a simple footer line.
- **Front matter migration.** hugo-book used `type = "docs"` + `weight`. Hextra
  uses `weight` for sidebar ordering (kept) and does not need `type = "docs"`.
  Normalize each page's front matter; add `sidebar`/`title` where Hextra needs
  it. Section `_index.md` files get appropriate titles/weights.
- **Shortcode migration:**
  - Replace the single hugo-book `{{< button href="/preview/" >}}` on the tools
    page with a small **custom `button` shortcode** at
    `docs/layouts/shortcodes/button.html`, styled to match Hextra, so the CTA
    survives.
  - `{{< relref >}}` is a Hugo built-in — unchanged.
  - Convert existing note/warning blockquotes (e.g. the "Alpha" note on the
    preview tools page) to Hextra `{{< callout >}}`.
  - Use Hextra `{{< cards >}}` / `{{< card >}}` on the landing page and, where it
    reads well, on section index pages.
- **Tailwind/daisyUI npm tooling stays.** `docs/package.json` and the
  `css.TailwindCSS` pipeline are used only by the preview tool. Hextra ships its
  own precompiled CSS, so there is no Tailwind conflict.

### 5. CI & tooling (`.github/workflows/docs.yml`, Makefile)

- **`lint` job:** update markdownlint/lychee ignore globs to the new generated
  paths (`docs/content/docs/reference/**`, `docs/content/docs/examples/**`) and
  drop the `!docs/themes/**` exclusion (no vendored theme anymore).
- **`preview-wasm` and `build-deploy` jobs:** remove `submodules: recursive`
  where it existed only for the theme (keep `fetch-depth: 0` on deploy for
  git-info/lastmod); add a Hugo module cache (`~/.cache/hugo_mod` via
  `actions/cache`) and a `hugo mod get` step before the build. Keep the existing
  `preview-wasm`, `docs-examples`, and `docs-crd-reference` steps.
- **Makefile:** `docs-serve` / `docs-build` keep running the generators; ensure
  they work with the new paths. `.gitmodules` loses the hugo-book entry.
- **Docs-on-docs:** update `docs/preview-environments.md` only if it references
  theme paths. `AGENTS.md`/Copilot instructions contain no hugo-book references
  (verified) and need no change.

## Testing & verification

1. `make preview-wasm docs-examples docs-crd-reference` — generators succeed and
   write to the new `content/docs/**` paths.
2. From `docs/`: `hugo mod get` then `hugo --source docs --minify` — a clean
   build with **zero** template errors.
3. Inspect built `docs/public/`:
   - Landing page renders (hero + cards).
   - Sidebar navigation and the search box are present.
   - `/preview/` loads and the WASM tool initializes.
   - A sample of aliased old URLs (`/installation/index.html`, etc.) emit
     redirect HTML to the new `/docs/...` locations.
4. `markdownlint` + `lychee` link check pass locally with updated globs.

## Rollout

Single PR (big-bang theme swap). The existing PR docs-build job validates the
build; merging to `main` deploys to GitHub Pages on the custom domain. Aliases
prevent broken inbound links. If the module fetch proves flaky in CI, the
fallback is `hugo mod vendor` (commit `_vendor/`) — not adopted now.

## Out of scope

- Wholesale rewriting of documentation prose or converting every guide to
  `steps`/`tabs`/`filetree` (can be done incrementally later).
- Changes to the Resource Preview tool's internals or the WASM build.
- Multi-language (i18n) support and docs versioning.
- Any operator/runtime code changes.
