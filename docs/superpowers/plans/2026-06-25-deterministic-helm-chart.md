# Deterministic Helm Chart Generation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `make helm-chart` deterministic and non-destructive by wrapping `kubebuilder edit` in a Go post-processor, and add a CI job that fails when `dist/chart` is stale or hand-edited.

**Architecture:** A new Go tool (`hack/helmgen`) orchestrates chart generation: it snapshots files kubebuilder must not permanently change, runs `kubebuilder edit --plugins=helm/v2-alpha`, restores the snapshots, deletes the resurrected `test-chart.yml`, then copies a small set of hand-owned files from `hack/helm/overrides/` over kubebuilder's output. The committed `dist/chart` is the post-processed output; the post-processor is a pure function of `config/` + `overrides/`, so re-runs are byte-stable. A new CI job runs `make helm-chart` and `git diff --quiet`.

**Tech Stack:** Go 1.26.4 (stdlib only for the tool), Make, kubebuilder v4.10.1 (pinned in `hack/tools/go.mod`), GitHub Actions, Helm.

## Global Constraints

- Module path: `github.com/bmorton/temporal-operator`; Go version `1.26.4` (from `go.mod`).
- The Go tool uses **stdlib only** — no new dependencies.
- Commit messages: Conventional Commits, and **every commit must be signed off** (`git commit -s`). DCO is enforced in CI.
- CRD API group domain is `temporal.bmor10.com`; copyright owner "Brian Morton".
- The flat `values.yaml` contract (`rbacHelpers.enable`, not nested `rbac.helpers`) must be preserved unchanged.
- CI runners use `runs-on: namespace-profile-temporal-operator`.
- Do **not** run `kubebuilder edit` by hand outside the new tool; the tool is the only supported entry point.

---

### Task 1: Create `hack/helm/overrides/` canonical source tree

Establish the canonical hand-owned files. These are copied verbatim from the current (hand-maintained) `dist/chart` and become the single source of truth for the hand-owned subset.

**Files:**
- Create: `hack/helm/overrides/templates/manager/manager.yaml` (copy of `dist/chart/templates/manager/manager.yaml`)
- Create: `hack/helm/overrides/values.yaml` (copy of `dist/chart/values.yaml`)
- Create: `hack/helm/overrides/Chart.yaml` (copy of `dist/chart/Chart.yaml`)
- Create: `hack/helm/overrides/README.md` (copy of `dist/chart/README.md`)
- Create: `hack/helm/overrides/artifacthub-pkg.yaml` (copy of `dist/chart/artifacthub-pkg.yaml`)

**Interfaces:**
- Produces: the `hack/helm/overrides/` directory whose layout mirrors `dist/chart/` paths. `hack/helmgen` (Task 2/3) walks this directory and copies each file to the matching `dist/chart/` path.

- [ ] **Step 1: Copy the five hand-owned files into the overrides tree**

```bash
mkdir -p hack/helm/overrides/templates/manager
cp dist/chart/templates/manager/manager.yaml hack/helm/overrides/templates/manager/manager.yaml
cp dist/chart/values.yaml                     hack/helm/overrides/values.yaml
cp dist/chart/Chart.yaml                       hack/helm/overrides/Chart.yaml
cp dist/chart/README.md                        hack/helm/overrides/README.md
cp dist/chart/artifacthub-pkg.yaml             hack/helm/overrides/artifacthub-pkg.yaml
```

- [ ] **Step 2: Verify the copies are byte-identical to their chart counterparts**

Run:
```bash
diff hack/helm/overrides/templates/manager/manager.yaml dist/chart/templates/manager/manager.yaml \
  && diff hack/helm/overrides/values.yaml dist/chart/values.yaml \
  && diff hack/helm/overrides/Chart.yaml dist/chart/Chart.yaml \
  && diff hack/helm/overrides/README.md dist/chart/README.md \
  && diff hack/helm/overrides/artifacthub-pkg.yaml dist/chart/artifacthub-pkg.yaml \
  && echo OK
```
Expected: `OK` (no diff output).

- [ ] **Step 3: Commit**

```bash
git add hack/helm/overrides
git commit -s -m "build(helm): add canonical overrides source tree for chart generation"
```

---

### Task 2: Implement `hack/helmgen` core pipeline (TDD)

Write the deterministic `Generate` pipeline with an injectable kubebuilder runner so it is unit-testable without the real binary.

**Files:**
- Create: `hack/helmgen/generate.go`
- Test: `hack/helmgen/generate_test.go`

**Interfaces:**
- Produces:
  - `type Options struct { Root, OverridesDir, ChartDir string; PreserveFiles, RemoveFiles []string; RunKubebuilder func() error }`
  - `func Generate(opts Options) error` — snapshots `PreserveFiles`, calls `opts.RunKubebuilder()`, restores the snapshots, removes `RemoveFiles`, then copies every file under `OverridesDir` to the mirrored path under `ChartDir`.
- Consumes (Task 3): `main.go` constructs `Options` with a real `RunKubebuilder` and calls `Generate`.

- [ ] **Step 1: Write the failing test**

Create `hack/helmgen/generate_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile writes data to root/rel, creating parent dirs.
func writeFile(t *testing.T, root, rel, data string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fixture builds a fake repo root and returns Options wired to a fake
// kubebuilder that clobbers preserved files, resurrects test-chart.yml,
// and overwrites the generated manager template.
func fixture(t *testing.T) (string, Options) {
	t.Helper()
	root := t.TempDir()

	// Original (canonical) preserved files.
	writeFile(t, root, "config/manager/kustomization.yaml", "ORIGINAL-KUSTOMIZATION\n")
	writeFile(t, root, "dist/install.yaml", "ORIGINAL-INSTALL\n")

	// Hand-owned override.
	writeFile(t, root, "hack/helm/overrides/templates/manager/manager.yaml", "HAND-OWNED-MANAGER\n")
	writeFile(t, root, "hack/helm/overrides/values.yaml", "HAND-OWNED-VALUES\n")

	opts := Options{
		Root:          root,
		OverridesDir:  filepath.Join(root, "hack", "helm", "overrides"),
		ChartDir:      filepath.Join(root, "dist", "chart"),
		PreserveFiles: []string{"config/manager/kustomization.yaml", "dist/install.yaml"},
		RemoveFiles:   []string{".github/workflows/test-chart.yml"},
		RunKubebuilder: func() error {
			// Simulate kubebuilder's destructive behavior.
			writeFile(t, root, "config/manager/kustomization.yaml", "CLOBBERED-KUSTOMIZATION\n")
			writeFile(t, root, "dist/install.yaml", "CLOBBERED-INSTALL\n")
			writeFile(t, root, ".github/workflows/test-chart.yml", "RESURRECTED\n")
			writeFile(t, root, "dist/chart/templates/manager/manager.yaml", "GENERATED-MANAGER\n")
			writeFile(t, root, "dist/chart/values.yaml", "GENERATED-VALUES\n")
			writeFile(t, root, "dist/chart/templates/crd/example.yaml", "GENERATED-CRD\n")
			return nil
		},
	}
	return root, opts
}

func TestGeneratePostProcessing(t *testing.T) {
	root, opts := fixture(t)

	if err := Generate(opts); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Preserved files restored to original content.
	if got := readFile(t, root, "config/manager/kustomization.yaml"); got != "ORIGINAL-KUSTOMIZATION\n" {
		t.Errorf("kustomization not restored: %q", got)
	}
	if got := readFile(t, root, "dist/install.yaml"); got != "ORIGINAL-INSTALL\n" {
		t.Errorf("install.yaml not restored: %q", got)
	}

	// Resurrected workflow removed.
	if _, err := os.Stat(filepath.Join(root, ".github/workflows/test-chart.yml")); !os.IsNotExist(err) {
		t.Errorf("test-chart.yml should have been removed, stat err=%v", err)
	}

	// Hand-owned overrides win over generated output.
	if got := readFile(t, root, "dist/chart/templates/manager/manager.yaml"); got != "HAND-OWNED-MANAGER\n" {
		t.Errorf("manager.yaml not overridden: %q", got)
	}
	if got := readFile(t, root, "dist/chart/values.yaml"); got != "HAND-OWNED-VALUES\n" {
		t.Errorf("values.yaml not overridden: %q", got)
	}

	// Non-overridden generated files are left as kubebuilder produced them.
	if got := readFile(t, root, "dist/chart/templates/crd/example.yaml"); got != "GENERATED-CRD\n" {
		t.Errorf("generated CRD altered: %q", got)
	}
}

func TestGenerateIsIdempotent(t *testing.T) {
	root, opts := fixture(t)

	if err := Generate(opts); err != nil {
		t.Fatalf("first Generate: %v", err)
	}
	first := readFile(t, root, "dist/chart/templates/manager/manager.yaml")
	firstKust := readFile(t, root, "config/manager/kustomization.yaml")

	if err := Generate(opts); err != nil {
		t.Fatalf("second Generate: %v", err)
	}
	if second := readFile(t, root, "dist/chart/templates/manager/manager.yaml"); second != first {
		t.Errorf("manager.yaml not stable: %q != %q", second, first)
	}
	if secondKust := readFile(t, root, "config/manager/kustomization.yaml"); secondKust != firstKust {
		t.Errorf("kustomization not stable: %q != %q", secondKust, firstKust)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./hack/helmgen/ -run TestGenerate -v`
Expected: FAIL (compile error: `undefined: Generate` / `undefined: Options`).

- [ ] **Step 3: Write the minimal implementation**

Create `hack/helmgen/generate.go`:

```go
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Options configures a single deterministic chart-generation run.
type Options struct {
	// Root is the repository root; PreserveFiles and RemoveFiles are relative to it.
	Root string
	// OverridesDir holds canonical hand-owned files mirroring ChartDir paths.
	OverridesDir string
	// ChartDir is the generated chart output directory.
	ChartDir string
	// PreserveFiles are snapshotted before, and restored after, generation.
	PreserveFiles []string
	// RemoveFiles are deleted after generation if present.
	RemoveFiles []string
	// RunKubebuilder performs the upstream generation step.
	RunKubebuilder func() error
}

type snapshotEntry struct {
	dest string
	temp string
}

// Generate runs the deterministic chart-generation pipeline.
func Generate(opts Options) error {
	snaps, err := snapshot(opts.Root, opts.PreserveFiles)
	defer cleanup(snaps)
	if err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}

	if err := opts.RunKubebuilder(); err != nil {
		return fmt.Errorf("run kubebuilder: %w", err)
	}

	if err := restore(snaps); err != nil {
		return fmt.Errorf("restore: %w", err)
	}

	for _, rel := range opts.RemoveFiles {
		if err := os.Remove(filepath.Join(opts.Root, rel)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove %s: %w", rel, err)
		}
	}

	if err := copyOverrides(opts.OverridesDir, opts.ChartDir); err != nil {
		return fmt.Errorf("copy overrides: %w", err)
	}
	return nil
}

func snapshot(root string, rels []string) ([]snapshotEntry, error) {
	var entries []snapshotEntry
	for _, rel := range rels {
		src := filepath.Join(root, rel)
		data, err := os.ReadFile(src)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return entries, err
		}
		tmp, err := os.CreateTemp("", "helmgen-*")
		if err != nil {
			return entries, err
		}
		if _, err := tmp.Write(data); err != nil {
			tmp.Close()
			return entries, err
		}
		if err := tmp.Close(); err != nil {
			return entries, err
		}
		entries = append(entries, snapshotEntry{dest: src, temp: tmp.Name()})
	}
	return entries, nil
}

func restore(entries []snapshotEntry) error {
	for _, e := range entries {
		data, err := os.ReadFile(e.temp)
		if err != nil {
			return err
		}
		if err := os.WriteFile(e.dest, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func cleanup(entries []snapshotEntry) {
	for _, e := range entries {
		_ = os.Remove(e.temp)
	}
}

func copyOverrides(overridesDir, chartDir string) error {
	return filepath.WalkDir(overridesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(overridesDir, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(chartDir, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dest, data, 0o644)
	})
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./hack/helmgen/ -run TestGenerate -v`
Expected: PASS (both `TestGeneratePostProcessing` and `TestGenerateIsIdempotent`).

- [ ] **Step 5: Commit**

```bash
git add hack/helmgen/generate.go hack/helmgen/generate_test.go
git commit -s -m "build(helm): add deterministic chart post-processing pipeline"
```

---

### Task 3: Wire `main.go` and rewrite the `helm-chart` Make target

Add the executable entry point and make `make helm-chart` delegate to it, then regenerate the chart once and commit the (one-time) result.

**Files:**
- Create: `hack/helmgen/main.go`
- Modify: `Makefile` (the `helm-chart` target, currently lines 157-159)

**Interfaces:**
- Consumes: `Generate(Options)` and `Options` from Task 2.
- Produces: a `main` that parses `--kubebuilder`, builds the real `Options` (preserving `config/manager/kustomization.yaml` + `dist/install.yaml`, removing `.github/workflows/test-chart.yml`, overrides at `hack/helm/overrides`, chart at `dist/chart`) and runs `kubebuilder edit --plugins=helm/v2-alpha` from the repo root.

- [ ] **Step 1: Write `hack/helmgen/main.go`**

```go
package main

import (
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
)

func main() {
	kubebuilder := flag.String("kubebuilder", "kubebuilder", "path to the kubebuilder binary")
	flag.Parse()

	root, err := os.Getwd()
	if err != nil {
		log.Fatalf("helmgen: getwd: %v", err)
	}

	opts := Options{
		Root:         root,
		OverridesDir: filepath.Join(root, "hack", "helm", "overrides"),
		ChartDir:     filepath.Join(root, "dist", "chart"),
		PreserveFiles: []string{
			"config/manager/kustomization.yaml",
			"dist/install.yaml",
		},
		RemoveFiles: []string{
			".github/workflows/test-chart.yml",
		},
		RunKubebuilder: func() error {
			cmd := exec.Command(*kubebuilder, "edit", "--plugins=helm/v2-alpha")
			cmd.Dir = root
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run()
		},
	}

	if err := Generate(opts); err != nil {
		log.Fatalf("helmgen: %v", err)
	}
}
```

- [ ] **Step 2: Verify the package builds**

Run: `go build ./hack/helmgen/`
Expected: no output, exit 0.

- [ ] **Step 3: Rewrite the `helm-chart` Make target**

In `Makefile`, replace:

```make
.PHONY: helm-chart
helm-chart: kubebuilder ## (Re)generate the Helm chart under dist/chart from kustomize output.
	$(KUBEBUILDER) edit --plugins=helm/v2-alpha
```

with:

```make
.PHONY: helm-chart
helm-chart: kubebuilder ## (Re)generate the Helm chart under dist/chart deterministically.
	go run ./hack/helmgen --kubebuilder=$(KUBEBUILDER)
```

- [ ] **Step 4: Regenerate the chart and commit the one-time result**

Run:
```bash
make helm-chart
git --no-pager diff --stat -- dist/chart config/manager/kustomization.yaml dist/install.yaml
```
Expected: a (potentially large) one-time diff — kubebuilder reformats CRD templates and may touch generated RBAC/webhook files. The hand-owned files (`manager.yaml`, `values.yaml`, `Chart.yaml`, `README.md`, `artifacthub-pkg.yaml`) must be **unchanged** because the overrides restored them. Confirm:
```bash
git --no-pager diff -- dist/chart/templates/manager/manager.yaml dist/chart/values.yaml \
  dist/chart/Chart.yaml dist/chart/README.md dist/chart/artifacthub-pkg.yaml
```
Expected: no diff for those five files. `config/manager/kustomization.yaml` and `dist/install.yaml` must also show **no diff** (restored from snapshot).

If a non-override generated file lost a customization that is actually required (review the diff), add that file to `hack/helm/overrides/` (mirroring its path) and re-run `make helm-chart` before committing.

- [ ] **Step 5: Verify determinism — second run is a no-op**

Run:
```bash
make helm-chart
git --no-pager diff --quiet -- dist/chart config/manager/kustomization.yaml dist/install.yaml && echo "NO-OP OK"
test ! -f .github/workflows/test-chart.yml && echo "NO TEST-CHART OK"
```
Expected: `NO-OP OK` and `NO TEST-CHART OK`.

- [ ] **Step 6: Verify the chart still renders and lints**

Run:
```bash
helm lint dist/chart
helm template temporal-operator dist/chart --namespace temporal-system >/dev/null && echo "TEMPLATE OK"
```
Expected: `helm lint` passes (0 chart(s) failed) and `TEMPLATE OK`. The flat values contract (`rbacHelpers.enable`) must still render.

- [ ] **Step 7: Commit**

```bash
git add Makefile hack/helmgen/main.go dist/chart config/manager/kustomization.yaml dist/install.yaml
git commit -s -m "build(helm): generate dist/chart deterministically via helmgen"
```

---

### Task 4: Add the `Verify generated chart` CI job and ignore `test-chart.yml`

Add a CI guard that regenerates the chart and fails if it differs from what is committed, mirroring `Verify generated docs` in `.github/workflows/docs.yml`.

**Files:**
- Modify: `.github/workflows/ci.yml` (add a new `verify-chart` job under `jobs:`)
- Modify: `.gitignore` (append the workflow path)

**Interfaces:**
- Consumes: the `make helm-chart` target from Task 3.

- [ ] **Step 1: Ignore the resurrected workflow**

Append to `.gitignore`:

```gitignore
# kubebuilder's helm plugin resurrects this workflow; helmgen deletes it and
# it must never be committed. See issue #82.
.github/workflows/test-chart.yml
```

- [ ] **Step 2: Add the `verify-chart` job to `.github/workflows/ci.yml`**

Add this job under `jobs:` (sibling of `lint`, `test`, `build`, `govulncheck`):

```yaml
  verify-chart:
    name: Verify generated chart
    runs-on: namespace-profile-temporal-operator
    steps:
      - uses: actions/checkout@df4cb1c069e1874edd31b4311f1884172cec0e10 # v6
        with:
          persist-credentials: false
      - uses: actions/setup-go@4a3601121dd01d1626a1e23e37211e3254c1c06c # v6.4.0
        with:
          go-version-file: go.mod
      - uses: actions/cache@27d5ce7f107fe9357f9df03efb73ab90386fccae # v5.0.5
        with:
          path: |
            ~/go/pkg/mod
            ~/.cache/go-build
          key: ${{ runner.os }}-chart-${{ hashFiles('**/go.sum') }}
          restore-keys: ${{ runner.os }}-chart-
      - name: Set up Helm
        uses: azure/setup-helm@dda3372f752e03dde6b3237bc9431cdc2f7a02a2 # v5.0.0
      - name: Regenerate Helm chart
        run: make helm-chart
      - name: Fail if generated chart is stale
        run: |
          if ! git diff --quiet -- dist/chart config/manager/kustomization.yaml dist/install.yaml; then
            echo "::error::Generated Helm chart is out of date. Run 'make helm-chart' and commit the result."
            git --no-pager diff -- dist/chart config/manager/kustomization.yaml dist/install.yaml
            exit 1
          fi
      - name: Fail if test-chart.yml was resurrected
        run: |
          if [ -f .github/workflows/test-chart.yml ]; then
            echo "::error::.github/workflows/test-chart.yml must not exist; helmgen should delete it."
            exit 1
          fi
      - name: Lint and render chart
        run: |
          helm lint dist/chart
          helm template temporal-operator dist/chart --namespace temporal-system >/dev/null
```

- [ ] **Step 3: Validate the workflow YAML syntax**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo "YAML OK"`
Expected: `YAML OK`.

- [ ] **Step 4: Sanity-check the verify logic locally**

Run:
```bash
make helm-chart
git diff --quiet -- dist/chart config/manager/kustomization.yaml dist/install.yaml && echo "CLEAN" || echo "STALE"
test ! -f .github/workflows/test-chart.yml && echo "NO TEST-CHART"
```
Expected: `CLEAN` and `NO TEST-CHART` (the chart was already regenerated and committed in Task 3).

- [ ] **Step 5: Commit**

```bash
git add .github/workflows/ci.yml .gitignore
git commit -s -m "ci: add Verify generated chart job"
```

---

### Task 5: Update contributor docs to remove hand-editing guidance

Replace the "edit the chart by hand" guidance with the new generate-and-verify workflow.

**Files:**
- Modify: `AGENTS.md`
- Modify: `.github/copilot-instructions.md`
- Modify: `CONTRIBUTING.md`

**Interfaces:** none (documentation only).

- [ ] **Step 1: Find the existing chart guidance to replace**

Run:
```bash
grep -rn "helm-chart\|dist/chart\|by hand\|hand-edit\|hand edit\|test-chart" AGENTS.md .github/copilot-instructions.md CONTRIBUTING.md
```
Expected: locates the passages describing manual chart edits and/or "don't run `make helm-chart`".

- [ ] **Step 2: Rewrite the guidance in each file**

In each of `AGENTS.md`, `.github/copilot-instructions.md`, and `CONTRIBUTING.md`, replace any "edit `dist/chart` by hand / don't run `make helm-chart`" guidance with the following (adapt wording to match each file's surrounding style and heading level):

```markdown
### Helm chart

`dist/chart` is generated — do **not** edit it by hand. Run:

```sh
make helm-chart   # regenerates dist/chart deterministically
```

Generation is `kubebuilder edit` plus a post-processor (`hack/helmgen`). Files
that must be hand-maintained live in `hack/helm/overrides/` (mirroring their
`dist/chart/` paths) and are copied over the generated output; edit those, not
`dist/chart`. The `Verify generated chart` CI job fails if `dist/chart` is stale,
so always run `make helm-chart` and commit the result after changing API types,
RBAC markers, or chart overrides.
```

- [ ] **Step 3: Verify no stale "by hand" chart guidance remains**

Run:
```bash
grep -rn "edit the chart by hand\|hand-edited\|don't run .*helm-chart\|do not run .*helm-chart\|maintained by hand" AGENTS.md .github/copilot-instructions.md CONTRIBUTING.md || echo "CLEAN"
```
Expected: `CLEAN` (no remaining hand-edit chart guidance).

- [ ] **Step 4: Commit**

```bash
git add AGENTS.md .github/copilot-instructions.md CONTRIBUTING.md
git commit -s -m "docs: document deterministic helm-chart generation workflow"
```

---

### Task 6: Final verification and PR

**Files:** none (verification only).

- [ ] **Step 1: Full deterministic round-trip from a clean tree**

Run:
```bash
git status --porcelain   # expect empty
make helm-chart
git status --porcelain   # expect empty (no diff, no new files)
test ! -f .github/workflows/test-chart.yml && echo "OK"
```
Expected: both `git status` outputs are empty, and `OK` is printed. This is the core acceptance criterion from #82.

- [ ] **Step 2: Run the unit tests and lint**

Run:
```bash
go test ./hack/helmgen/ -v
make lint
```
Expected: tests PASS; `make lint` passes (no new findings in `hack/helmgen`).

- [ ] **Step 3: Push the branch and open the PR**

```bash
git push -u origin helm-chart-determinism
gh pr create --fill --base main \
  --title "build(helm): make chart generation deterministic + add CI verify (#82)" \
  --body "Closes #82. Wraps \`kubebuilder edit\` in a deterministic Go post-processor (\`hack/helmgen\`), moves hand-owned templates to \`hack/helm/overrides/\`, and adds a \`Verify generated chart\` CI job."
```
Expected: PR created against `main`.

---

## Notes for the implementer

- **One-time reformat diff is expected** in Task 3 (CRDs 2-space → 4-space, possibly reordered RBAC). That is acceptable and lands in this PR. What must *not* change are the five override files and the two preserved files.
- **Memory follow-up (post-merge):** the prior repository guidance/memories stating "don't run `make helm-chart`" and "edit `dist/chart` by hand" are superseded by this work and should be down-voted/updated once merged.
- If `helm` or `kubebuilder` is missing locally, install via `make install-tools` (kubebuilder) and your platform's Helm install; CI provides both.
