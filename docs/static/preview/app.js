// Loads the operator's WebAssembly planner and renders its output as tabs by Kind.
const els = {
  input: document.getElementById("input"),
  status: document.getElementById("status"),
  errors: document.getElementById("errors"),
  tabs: document.getElementById("tabs"),
  objects: document.getElementById("objects"),
  example: document.getElementById("example"),
  render: document.getElementById("render"),
};

let ready = false;
let current = { objects: [], activeKind: null };

async function initWasm() {
  const go = new Go();
  const resp = await fetch("temporal-operator-preview.wasm");
  const { instance } = await WebAssembly.instantiateStreaming(resp, go.importObject);
  go.run(instance); // registers window.temporalPreview, then blocks on select{}
  ready = true;
  els.status.textContent = "Ready. Paste a TemporalCluster and click Preview.";
}

async function loadExamples() {
  try {
    const list = await (await fetch("examples/index.json")).json();
    for (const ex of list) {
      const opt = document.createElement("option");
      opt.value = ex.file;
      opt.textContent = ex.name;
      els.example.appendChild(opt);
    }
  } catch (_) { /* examples are optional */ }
}

els.example.addEventListener("change", async () => {
  if (!els.example.value) return;
  els.input.value = await (await fetch(els.example.value)).text();
});

els.render.addEventListener("click", () => {
  if (!ready) { els.status.textContent = "Still loading WebAssembly…"; return; }
  const raw = window.temporalPreview("TemporalCluster", els.input.value);
  const res = JSON.parse(raw);
  els.errors.textContent = (res.errors || []).join("\n");
  current.objects = res.objects || [];
  current.activeKind = null;
  renderTabs();
});

function renderTabs() {
  const kinds = [...new Set(current.objects.map((o) => o.kind))].sort();
  els.tabs.innerHTML = "";
  if (!current.activeKind && kinds.length) current.activeKind = kinds[0];
  for (const kind of kinds) {
    const count = current.objects.filter((o) => o.kind === kind).length;
    const tab = document.createElement("div");
    tab.className = "tab" + (kind === current.activeKind ? " active" : "");
    tab.textContent = `${kind} (${count})`;
    tab.addEventListener("click", () => { current.activeKind = kind; renderTabs(); });
    els.tabs.appendChild(tab);
  }
  renderObjects();
}

function renderObjects() {
  els.objects.innerHTML = "";
  for (const obj of current.objects.filter((o) => o.kind === current.activeKind)) {
    const d = document.createElement("details");
    d.className = "object";
    const s = document.createElement("summary");
    s.innerHTML = `<strong>${obj.name}</strong><span class="badge">${obj.phase}</span>`;
    const copy = document.createElement("button");
    copy.className = "copy";
    copy.textContent = "Copy";
    copy.addEventListener("click", (e) => { e.preventDefault(); navigator.clipboard.writeText(obj.yaml); });
    s.appendChild(copy);
    const pre = document.createElement("pre");
    pre.textContent = obj.yaml;
    d.appendChild(s);
    d.appendChild(pre);
    els.objects.appendChild(d);
  }
}

loadExamples();
initWasm().catch((e) => { els.status.textContent = "Failed to load WebAssembly: " + e; });
