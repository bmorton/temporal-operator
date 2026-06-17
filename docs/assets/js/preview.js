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
      try {
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          chunks.push(value);
          received += value.length;
          this.wasmProgress = Math.round((received / total) * 100);
        }
      } catch (e) {
        await reader.cancel().catch(() => {});
        throw e;
      } finally {
        reader.releaseLock();
      }
      const out = new Uint8Array(received);
      let pos = 0;
      for (const c of chunks) { out.set(c, pos); pos += c.length; }
      return out.buffer;
    },

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

    badgeClass(kind) {
      const map = {
        Deployment: "badge-primary",
        Service: "badge-secondary",
        ConfigMap: "badge-accent",
        Secret: "badge-warning",
        ServiceAccount: "badge-info",
        Role: "badge-info",
        RoleBinding: "badge-info",
        Job: "badge-success",
        PodDisruptionBudget: "badge-neutral",
        Certificate: "badge-error",
        Issuer: "badge-error",
        ServiceMonitor: "badge-success",
      };
      if (map[kind]) return map[kind];
      const palette = [
        "badge-primary", "badge-secondary", "badge-accent", "badge-info",
        "badge-success", "badge-warning", "badge-error", "badge-neutral",
      ];
      let h = 0;
      for (let i = 0; i < kind.length; i++) h = (h * 31 + kind.charCodeAt(i)) >>> 0;
      return palette[h % palette.length];
    },

    async copyOne(item) {
      try {
        await navigator.clipboard.writeText(item.yaml);
      } catch (e) {
        this.error = "Copy failed: " + e;
      }
    },

    multiDocYAML() {
      const docs = [];
      for (const g of this.groups) {
        for (const item of g.items) docs.push(item.yaml.trimEnd());
      }
      return docs.join("\n---\n") + "\n";
    },

    async copyAll() {
      try {
        await navigator.clipboard.writeText(this.multiDocYAML());
      } catch (e) {
        this.error = "Copy failed: " + e;
      }
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
  };
}

window.previewApp = previewApp;
