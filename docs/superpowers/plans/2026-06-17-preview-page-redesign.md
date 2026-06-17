# Resource Preview Page Redesign — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign the in-browser Resource Preview into a polished, responsive, kind-grouped card UI, and change the wasm export to return structured JSON — keeping it drop-in static on the existing Hugo/Pages flow.

**Architecture:** The pure Go planner (`internal/plan`) stays untouched; only `cmd/preview-wasm/main.go`'s return shape changes to a structured `{resources:[…], error}` JSON. The page moves from a hand-written standalone static file to a Hugo-rendered standalone layout (`content/preview/_index.md` + `layouts/preview/list.html`) styled with Tailwind v4 + daisyUI compiled through Hugo Pipes, with Alpine.js for reactivity and highlight.js for YAML. The wasm/`wasm_exec.js`/examples remain generated into `static/preview/`, cache-busted via a build-time hash.

**Tech Stack:** Go `GOOS=js GOARCH=wasm` (standard compiler), Hugo extended 0.140.2, Hugo Pipes `css.TailwindCSS`, Tailwind v4 + daisyUI v5, Alpine.js 3, highlight.js 11 + highlightjs-copy.

---

## File Structure

**Go (modify):**
- `cmd/preview-wasm/main.go` — change `previewObject`/`previewResult` to the structured contract; populate `apiVersion`/`namespace`; single `error`.

**Build (modify):**
- `hack/build-preview.sh` — write `docs/data/preview.json` with the wasm content hash.
- `.gitignore` — ignore `docs/data/preview.json`; drop the now-removed `docs/static/preview/index.html`/`app.js` from concern.
- `docs/hugo.toml` — no change required (kept for reference).

**Hugo page (create):**
- `docs/content/preview/_index.md` — front matter selecting the standalone layout, hidden from the section menu.
- `docs/layouts/preview/list.html` — full `<html>` document: input pane + output shell + Hugo Pipes asset wiring.
- `docs/assets/css/preview.css` — Tailwind v4 entry + daisyUI plugin + `@source` globs.
- `docs/assets/js/preview.js` — Alpine component: lazy wasm loader, grouped-card render, highlight, copy/download, filter, expand/collapse, dark mode.
- `docs/package.json` + `docs/package-lock.json` — pin `tailwindcss`, `@tailwindcss/cli`, `daisyui`.

**Hugo page (delete):**
- `docs/static/preview/index.html`, `docs/static/preview/app.js` — replaced by the Hugo-rendered page.

**Content (modify):**
- `docs/content/tools/resource-preview.md`, `docs/content/tools/_index.md` — drop "tabs" wording.

**CI (modify):**
- `.github/workflows/docs.yml` — add Node + `npm ci` (in `docs/`) before the Hugo build in `build-deploy`.

---

## Task 1: Structured-JSON wasm contract (Go)

**Files:**
- Modify: `cmd/preview-wasm/main.go`
- Test: `cmd/preview-wasm/wasm_build_test.go` (unchanged compile guard — run it)

- [ ] **Step 1: Update the result types and rename `objects`→`resources`**

In `cmd/preview-wasm/main.go`, replace the `previewObject`/`previewResult` types and the `result` helper (lines ~42-63) with:

```go
type previewResource struct {
	Kind       string `json:"kind"`
	APIVersion string `json:"apiVersion"`
	Name       string `json:"name"`
	Namespace  string `json:"namespace"`
	Phase      string `json:"phase"`
	YAML       string `json:"yaml"`
}

type previewResult struct {
	Resources []previewResource `json:"resources"`
	Error     string            `json:"error,omitempty"`
}

func ok(objs []previewResource) string {
	if objs == nil {
		objs = []previewResource{}
	}
	b, _ := json.Marshal(previewResult{Resources: objs})
	return string(b)
}

func fail(format string, args ...any) string {
	b, _ := json.Marshal(previewResult{
		Resources: []previewResource{},
		Error:     fmt.Sprintf(format, args...),
	})
	return string(b)
}
```

- [ ] **Step 2: Rewrite `previewTemporalCluster` to use the new helpers and populate apiVersion/namespace**

Replace `previewTemporalCluster` (lines ~67-106) with:

```go
func previewTemporalCluster(yamlSrc string) string {
	cluster, err := decodeTemporalCluster(yamlSrc)
	if err != nil {
		return fail("%s", err.Error())
	}
	if cluster.Namespace == "" {
		cluster.Namespace = "default"
	}

	ctx := context.Background()
	defaulter := &webhookv1alpha1.TemporalClusterCustomDefaulter{}
	if err := defaulter.Default(ctx, cluster); err != nil {
		return fail("defaulting failed: %v", err)
	}

	validator := &webhookv1alpha1.TemporalClusterCustomValidator{}
	if _, err := validator.ValidateCreate(ctx, cluster); err != nil {
		return fail("validation failed: %v", err)
	}

	planned, err := plan.PlanFromSpec(cluster)
	if err != nil {
		return fail("%s", err.Error())
	}

	objs := make([]previewResource, 0, len(planned))
	for _, p := range planned {
		rendered, err := renderObject(p.Object)
		if err != nil {
			return fail("rendering %s: %v", p.Object.GetName(), err)
		}
		gvk := p.Object.GetObjectKind().GroupVersionKind()
		objs = append(objs, previewResource{
			Kind:       gvk.Kind,
			APIVersion: gvk.GroupVersion().String(),
			Name:       p.Object.GetName(),
			Namespace:  p.Object.GetNamespace(),
			Phase:      string(p.Phase),
			YAML:       rendered,
		})
	}
	return ok(objs)
}
```

- [ ] **Step 3: Update the remaining `result(...)` call sites in `temporalPreview`**

Replace `temporalPreview` (lines ~156-168) with:

```go
func temporalPreview(_ js.Value, args []js.Value) any {
	if len(args) < 2 {
		return fail("temporalPreview(kind, yaml) requires two arguments")
	}
	kind := args[0].String()
	src := args[1].String()
	switch kind {
	case "TemporalCluster":
		return previewTemporalCluster(src)
	default:
		return fail("kind %q is not supported yet", kind)
	}
}
```

Also update the doc comment at the top of the file (line ~19-21) to read:

```go
// Command preview-wasm exposes the operator's object planner to the browser. It
// registers a global temporalPreview(kind, yaml) function that returns a JSON
// string {resources:[{kind,apiVersion,name,namespace,phase,yaml}], error}
// describing every object the operator would create.
```

- [ ] **Step 4: Build the js/wasm target and run the compile guard**

Run:
```bash
cd /workspaces/temporal-op2
go test ./cmd/preview-wasm/...
```
Expected: `ok  github.com/bmorton/temporal-operator/cmd/preview-wasm` (the test runs `GOOS=js GOARCH=wasm go build`).

- [ ] **Step 5: Sanity-check the JSON shape with a quick wasm run (optional but recommended)**

Run (uses the Node `wasm_exec.js` to invoke the export on the minimal example):
```bash
cd /workspaces/temporal-op2
make preview-wasm >/dev/null
node -e '
  const fs=require("fs"); globalThis.crypto=require("crypto").webcrypto;
  require("./docs/static/preview/wasm_exec.js");
  const go=new Go();
  WebAssembly.instantiate(fs.readFileSync("./docs/static/preview/temporal-operator-preview.wasm"), go.importObject).then(r=>{
    go.run(r.instance);
    const yaml=fs.readFileSync("./examples/cluster-postgres-minimal/temporalcluster.yaml","utf8");
    const out=JSON.parse(globalThis.temporalPreview("TemporalCluster", yaml));
    console.log("error:", out.error||"(none)");
    console.log("resources:", out.resources.length, "first:", JSON.stringify({kind:out.resources[0].kind, apiVersion:out.resources[0].apiVersion, name:out.resources[0].name, namespace:out.resources[0].namespace, phase:out.resources[0].phase}));
  });
'
```
Expected: `error: (none)` and a non-zero resource count whose first entry has populated `kind`, `apiVersion`, `name`, `phase` fields.

- [ ] **Step 6: Commit**

```bash
cd /workspaces/temporal-op2
git add cmd/preview-wasm/main.go
git commit -s -m "feat(preview): return structured resources JSON from wasm

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 2: Cache-bust hash in the build script

**Files:**
- Modify: `hack/build-preview.sh`
- Modify: `.gitignore`

- [ ] **Step 1: Write the wasm content hash to `docs/data/preview.json`**

In `hack/build-preview.sh`, after the `Copying wasm_exec.js...` block and before `Staging example...`, insert:

```bash
echo "Writing cache-busting hash..."
data_dir="${repo_root}/docs/data"
mkdir -p "${data_dir}"
wasm_hash="$(sha256sum "${out_dir}/temporal-operator-preview.wasm" | cut -c1-16)"
printf '{"wasmHash":"%s"}\n' "${wasm_hash}" > "${data_dir}/preview.json"
```

- [ ] **Step 2: Git-ignore the generated data file**

In `.gitignore`, under the existing `# Generated WebAssembly resource-preview assets` block, add:

```gitignore
docs/data/preview.json
```

- [ ] **Step 3: Run the build and verify the hash file**

Run:
```bash
cd /workspaces/temporal-op2
make preview-wasm
cat docs/data/preview.json
```
Expected: `{"wasmHash":"<16 hex chars>"}`.

- [ ] **Step 4: Commit**

```bash
cd /workspaces/temporal-op2
git add hack/build-preview.sh .gitignore
git commit -s -m "build(preview): emit wasm content hash for cache busting

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 3: Tailwind + daisyUI dependencies and CSS entry

**Files:**
- Create: `docs/package.json`, `docs/package-lock.json` (generated)
- Create: `docs/assets/css/preview.css`

- [ ] **Step 1: Create `docs/package.json`**

```json
{
  "name": "temporal-operator-docs",
  "private": true,
  "description": "Build-time CSS tooling for the Hugo docs site (Tailwind + daisyUI).",
  "devDependencies": {
    "@tailwindcss/cli": "4.3.1",
    "daisyui": "5.5.23",
    "tailwindcss": "4.3.1"
  }
}
```

- [ ] **Step 2: Install to generate the lockfile**

Run:
```bash
cd /workspaces/temporal-op2/docs
npm install
ls node_modules/.bin/tailwindcss
```
Expected: `npm install` succeeds and `node_modules/.bin/tailwindcss` exists (Hugo's `css.TailwindCSS` invokes this binary).

- [ ] **Step 3: Create `docs/assets/css/preview.css`**

```css
@import "tailwindcss";

/* Scan the templates and app JS that emit class names (Tailwind v4 purge). */
@source "../../layouts/**/*.html";
@source "../../assets/js/**/*.js";

@plugin "daisyui" {
  themes: light --default, dark --prefersdark;
}

/* Make code blocks scroll horizontally on mobile instead of overflowing. */
.preview-code {
  overflow-x: auto;
}
.preview-code code {
  white-space: pre;
}
```

- [ ] **Step 4: Ignore `docs/node_modules`**

In `.gitignore`, add:
```gitignore
docs/node_modules/
```

- [ ] **Step 5: Commit (lockfile included, node_modules ignored)**

```bash
cd /workspaces/temporal-op2
git add docs/package.json docs/package-lock.json docs/assets/css/preview.css .gitignore
git commit -s -m "build(docs): add Tailwind + daisyUI for Hugo Pipes CSS

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 4: Standalone Hugo page (layout + content) — structural shell

This task produces a working, styled page **shell** (input pane + empty output
pane) that compiles CSS through Hugo Pipes. The interactive JS lands in Task 5.

**Files:**
- Create: `docs/content/preview/_index.md`
- Create: `docs/layouts/preview/list.html`
- Create: `docs/assets/js/preview.js` (stub registering the Alpine component)

- [ ] **Step 1: Create `docs/content/preview/_index.md`**

```markdown
---
title: "Resource Preview"
type: "preview"
layout: "list"
_build:
  list: "never"
---
```

- [ ] **Step 2: Create the Alpine component stub `docs/assets/js/preview.js`**

```javascript
// Alpine component for the Resource Preview playground. The wasm module is
// fetched lazily on first interaction (see loadWasm). Filled in in Task 5.
function previewApp() {
  return {
    // --- state ---
    input: "",
    examples: [],
    selectedExample: "",
    wasmState: "idle", // idle | loading | ready | error
    wasmProgress: 0, // 0..100, -1 when total unknown
    generating: false,
    error: "",
    groups: [], // [{ kind, items: [{ id, name, namespace, phase, yaml, open, highlighted }] }]
    hiddenKinds: {}, // { [kind]: true } when filtered out
    theme: "light",

    async init() {
      this.theme = localStorage.getItem("preview-theme") || "light";
      document.documentElement.setAttribute("data-theme", this.theme);
      this.syncHighlightTheme();
      await this.loadExamples();
    },

    async loadExamples() {
      try {
        const res = await fetch("examples/index.json");
        this.examples = await res.json();
      } catch (_) {
        this.examples = [];
      }
    },
  };
}

window.previewApp = previewApp;
```

- [ ] **Step 3: Create `docs/layouts/preview/list.html` (full standalone document)**

```go-html-template
{{- $css := resources.Get "css/preview.css" | css.TailwindCSS | fingerprint -}}
{{- $js := resources.Get "js/preview.js" | fingerprint -}}
{{- $wasmHash := "" -}}
{{- with site.Data.preview }}{{ $wasmHash = .wasmHash }}{{ end -}}
<!DOCTYPE html>
<html lang="en" data-theme="light">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Resource Preview — temporal-operator</title>
  <link rel="stylesheet" href="{{ $css.RelPermalink }}" />
  <link id="hljs-light" rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/github.min.css" />
  <link id="hljs-dark" rel="stylesheet" href="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/styles/github-dark.min.css" disabled />
  <script defer src="https://cdnjs.cloudflare.com/ajax/libs/highlight.js/11.9.0/highlight.min.js"></script>
  <script defer src="{{ "preview/wasm_exec.js" | relURL }}"></script>
  <script defer src="{{ $js.RelPermalink }}"></script>
  <script defer src="https://cdn.jsdelivr.net/npm/alpinejs@3.15.12/dist/cdn.min.js"></script>
  <script>
    window.PREVIEW_WASM_URL = {{ printf "%s?v=%s" (relURL "preview/temporal-operator-preview.wasm") $wasmHash }};
  </script>
</head>
<body class="min-h-screen bg-base-100 text-base-content">
  <div x-data="previewApp()" class="flex flex-col min-h-screen">
    <!-- Header -->
    <header class="border-b border-base-300 px-4 py-3 flex items-center gap-3">
      <h1 class="text-lg font-semibold">Resource Preview <span class="badge badge-sm badge-warning align-middle">alpha</span></h1>
      <p class="hidden sm:block text-sm opacity-70">Paste a <code>TemporalCluster</code> and see every object the operator would create. Runs entirely in your browser.</p>
      <button class="btn btn-ghost btn-sm ml-auto" @click="toggleTheme()" :aria-label="theme === 'dark' ? 'Switch to light mode' : 'Switch to dark mode'">
        <span x-text="theme === 'dark' ? '☀️' : '🌙'"></span>
      </button>
    </header>

    <!-- Panes -->
    <main class="flex-1 grid grid-cols-1 lg:grid-cols-[minmax(0,420px)_1fr] gap-4 p-4">
      <!-- Input pane -->
      <section class="flex flex-col gap-3 min-h-0">
        <div class="flex gap-2">
          <select class="select select-bordered select-sm flex-1" x-model="selectedExample" @change="loadExample()">
            <option value="">Load example…</option>
            <template x-for="ex in examples" :key="ex.file">
              <option :value="ex.file" x-text="ex.name"></option>
            </template>
          </select>
          <button class="btn btn-primary btn-sm" @click="preview()" :disabled="generating || wasmState === 'loading'">
            <span x-show="generating || wasmState === 'loading'" class="loading loading-spinner loading-xs"></span>
            <span x-text="(generating || wasmState === 'loading') ? 'Working…' : 'Preview →'"></span>
          </button>
        </div>
        <textarea
          class="textarea textarea-bordered font-mono text-xs flex-1 min-h-[300px] resize-none"
          spellcheck="false"
          placeholder="apiVersion: temporal.bmor10.com/v1alpha1&#10;kind: TemporalCluster&#10;..."
          x-model="input"
          @focus.once="loadWasm()"></textarea>
        <!-- wasm loading indicator -->
        <div x-show="wasmState === 'loading'" class="text-xs opacity-70">
          <span>Loading WebAssembly…</span>
          <progress class="progress progress-primary w-full mt-1" :value="wasmProgress < 0 ? undefined : wasmProgress" max="100"></progress>
        </div>
      </section>

      <!-- Output pane -->
      <section class="flex flex-col gap-3 min-h-0">
        <!-- error banner -->
        <div x-show="error" x-cloak role="alert" class="alert alert-error text-sm">
          <span x-text="error"></span>
        </div>

        <!-- global controls -->
        <div x-show="groups.length" x-cloak class="flex flex-wrap items-center gap-2">
          <button class="btn btn-xs" @click="expandAll(true)">Expand all</button>
          <button class="btn btn-xs" @click="expandAll(false)">Collapse all</button>
          <div class="dropdown">
            <div tabindex="0" role="button" class="btn btn-xs">Kinds ▾</div>
            <ul tabindex="0" class="dropdown-content menu bg-base-200 rounded-box z-10 w-56 p-2 shadow">
              <template x-for="kind in allKinds()" :key="kind">
                <li>
                  <label class="label cursor-pointer justify-start gap-2">
                    <input type="checkbox" class="checkbox checkbox-xs" :checked="!hiddenKinds[kind]" @change="toggleKind(kind)" />
                    <span x-text="kind"></span>
                  </label>
                </li>
              </template>
            </ul>
          </div>
          <button class="btn btn-xs ml-auto" @click="copyAll()">Copy all</button>
          <button class="btn btn-xs" @click="downloadAll()">Download all</button>
        </div>

        <!-- empty state -->
        <div x-show="!groups.length && !error" x-cloak class="opacity-60 text-sm">
          Generated resources will appear here, grouped by kind.
        </div>

        <!-- grouped cards -->
        <div class="flex flex-col gap-4 overflow-auto min-h-0">
          <template x-for="group in visibleGroups()" :key="group.kind">
            <div>
              <h2 class="text-sm font-semibold mb-2">
                <span x-text="group.kind"></span>
                <span class="opacity-60" x-text="'(' + group.items.length + ')'"></span>
              </h2>
              <div class="flex flex-col gap-2">
                <template x-for="item in group.items" :key="item.id">
                  <div class="card card-compact bg-base-200 border border-base-300">
                    <div class="card-body p-0">
                      <button class="flex items-center gap-2 px-3 py-2 text-left" @click="toggleItem(item)">
                        <span class="badge badge-sm" :class="badgeClass(group.kind)" x-text="group.kind"></span>
                        <span class="font-mono text-sm" x-text="item.name"></span>
                        <span x-show="item.namespace" class="text-xs opacity-60" x-text="'ns: ' + item.namespace"></span>
                        <span class="ml-auto opacity-50" x-text="item.open ? '▾' : '▸'"></span>
                      </button>
                      <div x-show="item.open" x-cloak class="border-t border-base-300">
                        <div class="flex justify-end px-2 pt-2">
                          <button class="btn btn-ghost btn-xs" @click="copyOne(item)">Copy</button>
                        </div>
                        <pre class="preview-code px-3 pb-3 text-xs"><code class="language-yaml" :id="'code-' + item.id" x-text="item.yaml"></code></pre>
                      </div>
                    </div>
                  </div>
                </template>
              </div>
            </div>
          </template>
        </div>
      </section>
    </main>
  </div>
  <style>[x-cloak]{display:none !important;}</style>
</body>
</html>
```

- [ ] **Step 4: Build the site and confirm CSS compiles through Hugo Pipes**

Run:
```bash
cd /workspaces/temporal-op2
make preview-wasm
PATH="$PWD/docs/node_modules/.bin:$PATH" hugo --source docs --minify
ls docs/public/preview/index.html
grep -c "daisyui\|--color-base" docs/public/css/preview.*.css 2>/dev/null || find docs/public -name 'preview.*.css'
```
Expected: `hugo` exits 0, `docs/public/preview/index.html` exists, and a fingerprinted `preview.*.css` is emitted (Tailwind/daisyUI compiled, not the CDN runtime).

- [ ] **Step 5: Verify the page is excluded from the book section menu**

Run:
```bash
cd /workspaces/temporal-op2
grep -ri 'href="[^"]*preview/"' docs/public/index.html docs/public/tools/index.html | head
```
Expected: the only menu/nav link to `/preview/` comes from the Tools content page, not an auto-generated section entry (because `_build.list = "never"`).

- [ ] **Step 6: Commit**

```bash
cd /workspaces/temporal-op2
git add docs/content/preview docs/layouts/preview docs/assets/js/preview.js
git commit -s -m "feat(preview): standalone Hugo page shell with Tailwind/daisyUI

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 5: Interactive Alpine logic (wasm load, render, copy/download, filter, theme)

**Files:**
- Modify: `docs/assets/js/preview.js`

- [ ] **Step 1: Add example loading, lazy wasm loader with progress**

In `docs/assets/js/preview.js`, replace the `loadExamples()` method's closing brace region by appending these methods **inside** the returned object (after `loadExamples`):

```javascript
    async loadExample() {
      if (!this.selectedExample) return;
      try {
        this.input = await (await fetch(this.selectedExample)).text();
      } catch (e) {
        this.error = "Failed to load example: " + e;
      }
    },

    async loadWasm() {
      if (this.wasmState === "ready" || this.wasmState === "loading") return;
      this.wasmState = "loading";
      this.wasmProgress = 0;
      try {
        const go = new Go();
        const resp = await fetch(window.PREVIEW_WASM_URL);
        if (!resp.ok) throw new Error("HTTP " + resp.status);
        const buffer = await this.readWithProgress(resp);
        const { instance } = await WebAssembly.instantiate(buffer, go.importObject);
        go.run(instance); // registers window.temporalPreview, then blocks on select{}
        this.wasmState = "ready";
      } catch (e) {
        this.wasmState = "error";
        this.error = "Failed to load WebAssembly: " + e;
      }
    },

    async readWithProgress(resp) {
      const total = Number(resp.headers.get("Content-Length")) || 0;
      if (!total || !resp.body) {
        this.wasmProgress = -1; // indeterminate
        return await resp.arrayBuffer();
      }
      const reader = resp.body.getReader();
      const chunks = [];
      let received = 0;
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        chunks.push(value);
        received += value.length;
        this.wasmProgress = Math.round((received / total) * 100);
      }
      const out = new Uint8Array(received);
      let pos = 0;
      for (const c of chunks) { out.set(c, pos); pos += c.length; }
      return out.buffer;
    },
```

- [ ] **Step 2: Add the `preview()` generation method and grouping**

Append these methods inside the returned object:

```javascript
    async preview() {
      this.error = "";
      if (this.wasmState !== "ready") {
        await this.loadWasm();
        if (this.wasmState !== "ready") return;
      }
      this.generating = true;
      try {
        const raw = window.temporalPreview("TemporalCluster", this.input);
        const res = JSON.parse(raw);
        if (res.error) {
          this.error = res.error;
          this.groups = [];
          return;
        }
        this.groups = this.groupByKind(res.resources || []);
      } catch (e) {
        this.error = "Generation failed: " + e;
        this.groups = [];
      } finally {
        this.generating = false;
      }
    },

    groupByKind(resources) {
      const byKind = {};
      let seq = 0;
      for (const r of resources) {
        (byKind[r.kind] = byKind[r.kind] || []).push({
          id: seq++,
          name: r.name,
          namespace: r.namespace,
          phase: r.phase,
          yaml: r.yaml,
          open: false,
          highlighted: false,
        });
      }
      return Object.keys(byKind)
        .sort()
        .map((kind) => ({ kind, items: byKind[kind] }));
    },

    allKinds() {
      return this.groups.map((g) => g.kind);
    },

    visibleGroups() {
      return this.groups.filter((g) => !this.hiddenKinds[g.kind]);
    },
```

- [ ] **Step 3: Add expand/collapse, lazy highlight, and filter**

Append these methods inside the returned object:

```javascript
    toggleItem(item) {
      item.open = !item.open;
      if (item.open) this.highlight(item);
    },

    expandAll(open) {
      for (const g of this.groups) {
        if (this.hiddenKinds[g.kind]) continue;
        for (const item of g.items) {
          item.open = open;
          if (open) this.highlight(item);
        }
      }
    },

    highlight(item) {
      if (item.highlighted || !window.hljs) return;
      // Defer to next tick so the <code> element exists in the DOM.
      this.$nextTick(() => {
        const el = document.getElementById("code-" + item.id);
        if (el) {
          window.hljs.highlightElement(el);
          item.highlighted = true;
        }
      });
    },

    toggleKind(kind) {
      this.hiddenKinds[kind] = !this.hiddenKinds[kind];
    },
```

- [ ] **Step 4: Add copy/download and theme toggle**

Append these methods inside the returned object:

```javascript
    async copyOne(item) {
      await navigator.clipboard.writeText(item.yaml);
    },

    multiDocYAML() {
      const docs = [];
      for (const g of this.visibleGroups()) {
        for (const item of g.items) docs.push(item.yaml.trimEnd());
      }
      return docs.join("\n---\n") + "\n";
    },

    async copyAll() {
      await navigator.clipboard.writeText(this.multiDocYAML());
    },

    downloadAll() {
      const blob = new Blob([this.multiDocYAML()], { type: "application/yaml" });
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = "preview.yaml";
      a.click();
      URL.revokeObjectURL(url);
    },

    toggleTheme() {
      this.theme = this.theme === "dark" ? "light" : "dark";
      localStorage.setItem("preview-theme", this.theme);
      document.documentElement.setAttribute("data-theme", this.theme);
      this.syncHighlightTheme();
    },

    syncHighlightTheme() {
      const light = document.getElementById("hljs-light");
      const dark = document.getElementById("hljs-dark");
      if (!light || !dark) return;
      const isDark = this.theme === "dark";
      light.disabled = isDark;
      dark.disabled = !isDark;
    },
```

- [ ] **Step 5: Rebuild and verify the page renders end-to-end locally**

Run:
```bash
cd /workspaces/temporal-op2
make preview-wasm
PATH="$PWD/docs/node_modules/.bin:$PATH" hugo --source docs --minify
( cd docs/public && python3 -m http.server 8765 >/dev/null 2>&1 & echo $! > /tmp/preview_http.pid )
sleep 1
curl -sS -o /dev/null -w "preview page: %{http_code}\n" http://localhost:8765/preview/
curl -sS -o /dev/null -w "wasm: %{http_code} %{content_type}\n" "http://localhost:8765/preview/temporal-operator-preview.wasm"
curl -sS -o /dev/null -w "examples: %{http_code}\n" http://localhost:8765/preview/examples/index.json
kill "$(cat /tmp/preview_http.pid)"
```
Expected: `preview page: 200`, the wasm returns `200` (content-type may be `application/wasm` or `application/octet-stream` under python's server — GitHub Pages sends `application/wasm`), and `examples: 200`.

- [ ] **Step 6: Manual acceptance check (headless)**

Run this Puppeteer-free DOM smoke test via Node + jsdom is overkill; instead verify the compiled JS has no syntax errors and the app factory is intact:
```bash
cd /workspaces/temporal-op2
node --check docs/assets/js/preview.js && echo "preview.js syntax OK"
node -e 'const s=require("fs").readFileSync("docs/assets/js/preview.js","utf8"); for (const m of ["loadWasm","preview","groupByKind","expandAll","downloadAll","toggleTheme","highlight"]) { if (!s.includes(m+"(")) throw new Error("missing "+m); } console.log("all methods present");'
```
Expected: `preview.js syntax OK` and `all methods present`.

- [ ] **Step 7: Commit**

```bash
cd /workspaces/temporal-op2
git add docs/assets/js/preview.js
git commit -s -m "feat(preview): interactive grouped-card UI with lazy wasm + highlight

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 6: Remove the old static page and update content + CI

**Files:**
- Delete: `docs/static/preview/index.html`, `docs/static/preview/app.js`
- Modify: `docs/content/tools/resource-preview.md`, `docs/content/tools/_index.md`
- Modify: `.github/workflows/docs.yml`

- [ ] **Step 1: Delete the superseded static files**

Run:
```bash
cd /workspaces/temporal-op2
git rm docs/static/preview/index.html docs/static/preview/app.js
```

- [ ] **Step 2: Update `docs/content/tools/resource-preview.md`**

Replace the body paragraph that mentions "grouped into tabs by kind" with grouped-card wording. Change:

```markdown
resource and it shows every Kubernetes object the operator would create, grouped
into tabs by kind, after applying the same defaulting and validation the
operator's admission webhooks perform.
```

to:

```markdown
resource and it shows every Kubernetes object the operator would create, grouped
into collapsible cards by kind, after applying the same defaulting and validation
the operator's admission webhooks perform.
```

- [ ] **Step 3: Update `docs/content/tools/_index.md`**

No wording change is strictly required (it does not mention tabs), but confirm the relref link target still resolves. Run:
```bash
cd /workspaces/temporal-op2
grep -n "resource-preview" docs/content/tools/_index.md
```
Expected: the `{{< relref "resource-preview" >}}` line is present and unchanged.

- [ ] **Step 4: Add the Node/Tailwind step to the `build-deploy` job in `.github/workflows/docs.yml`**

In the `build-deploy` job, immediately **after** the `Setup Go` step and **before** `Setup Pages`, insert:

```yaml
      - name: Setup Node
        uses: actions/setup-node@a0853c24544627f65ddf259abe73b1d18a591444 # v5.0.0
        with:
          node-version: "22"
      - name: Install docs CSS tooling
        run: npm ci
        working-directory: docs
```

Then update the `Build site` step's `run` so the Tailwind binary is on `PATH`:

```yaml
      - name: Build site
        env:
          BASE_URL: ${{ steps.pages.outputs.base_url }}
        run: PATH="$PWD/docs/node_modules/.bin:$PATH" hugo --source docs --minify --baseURL "${BASE_URL}/"
```

- [ ] **Step 5: Confirm the action is hash-pinned per repo convention**

The repo requires hash-pinned actions with a `# vX` comment (zizmor). Verify the `setup-node` pin resolves to a real `v5.0.0` tag before committing:
```bash
cd /workspaces/temporal-op2
gh api repos/actions/setup-node/git/ref/tags/v5.0.0 --jq .object.sha 2>/dev/null || echo "verify the pinned SHA matches a real setup-node release tag"
```
Expected: a commit SHA is printed; ensure it matches the SHA used in the workflow (`a0853c24544627f65ddf259abe73b1d18a591444`). If it differs, replace the SHA in the workflow with the printed value, keeping the `# v5.0.0` comment.

- [ ] **Step 6: Build the full site once more to confirm nothing broke**

Run:
```bash
cd /workspaces/temporal-op2
make preview-wasm
PATH="$PWD/docs/node_modules/.bin:$PATH" hugo --source docs --minify
echo "hugo build OK"
```
Expected: `hugo build OK` with no missing-layout or Pipes errors.

- [ ] **Step 7: Commit**

```bash
cd /workspaces/temporal-op2
git add docs/static/preview docs/content/tools/resource-preview.md .github/workflows/docs.yml
git commit -s -m "feat(preview): retire static page, wire Tailwind build into docs CI

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 7: Final verification pass

**Files:** none (verification only)

- [ ] **Step 1: Go tests for the wasm package**

Run:
```bash
cd /workspaces/temporal-op2
go test ./cmd/preview-wasm/...
```
Expected: `ok`.

- [ ] **Step 2: Full preview build + Hugo build**

Run:
```bash
cd /workspaces/temporal-op2
make preview-wasm
PATH="$PWD/docs/node_modules/.bin:$PATH" hugo --source docs --minify
```
Expected: both succeed; `docs/public/preview/index.html` and a fingerprinted `docs/public/css/preview.*.css` exist.

- [ ] **Step 3: Measure the gzipped wasm size (sanity vs. the ~9.7 MB expectation)**

Run:
```bash
cd /workspaces/temporal-op2
gzip -c docs/static/preview/temporal-operator-preview.wasm | wc -c | awk '{printf "wasm gzipped: %.1f MB\n", $1/1048576}'
```
Expected: roughly 9–11 MB; confirms lazy-loading remains the right call.

- [ ] **Step 4: Markdown lint (repo convention before opening a PR)**

Run:
```bash
cd /workspaces/temporal-op2
npx -y markdownlint-cli2 "docs/content/tools/*.md" --config .markdownlint.yaml 2>&1 | tail -5 || true
```
Expected: no errors for the edited tools pages (the `docs/superpowers/**` specs/plans are excluded by CI config and need not pass).

- [ ] **Step 5: Confirm clean tree and review the diff**

Run:
```bash
cd /workspaces/temporal-op2
git status --porcelain
git --no-pager log --oneline -7
```
Expected: working tree clean (generated `docs/static/preview/*`, `docs/data/preview.json`, `docs/node_modules/` are git-ignored), and the seven feature commits are present.

---

## Self-Review Notes

- **Spec coverage:** structured JSON contract (Task 1), cache-bust hash (Task 2), Tailwind/daisyUI via Hugo Pipes (Task 3), standalone Hugo layout + input/output panes (Task 4), grouped collapsible cards / badges / expand-collapse-all / kind filter / copy+download all / lazy highlight / dark mode / lazy wasm with determinate progress / error banner (Tasks 4–5), retire old static page + content + CI Node step (Task 6), verification incl. size measurement (Task 7). Stages 2–3 explicitly deferred per spec.
- **No placeholders:** every code step shows complete code; every run step shows the command and expected output.
- **Naming consistency:** Go `previewResource`/`previewResult` with `resources`/`error`; JS methods (`loadWasm`, `preview`, `groupByKind`, `visibleGroups`, `expandAll`, `highlight`, `toggleKind`, `copyAll`, `downloadAll`, `toggleTheme`, `syncHighlightTheme`) are referenced identically in `layouts/preview/list.html`.
- **Known follow-up:** the `setup-node` SHA in Task 6 must be verified against a real release (Step 5 does this); if GitHub changes the tag SHA, swap it but keep the `# v5.0.0` comment.
