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

    syncHighlightTheme() {},
  };
}

window.previewApp = previewApp;
