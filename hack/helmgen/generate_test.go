package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		ResetDirs:     []string{"dist/chart/templates"},
		Patches: []Patch{
			{File: "templates/crd/example.yaml", Apply: ReplaceOnce("GENERATED", "PATCHED")},
		},
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

	// Patches are applied to generated files.
	if got := readFile(t, root, "dist/chart/templates/crd/example.yaml"); got != "PATCHED-CRD\n" {
		t.Errorf("generated CRD not patched: %q", got)
	}
}

func TestGenerateRemovesStaleTemplates(t *testing.T) {
	root, opts := fixture(t)
	writeFile(t, root, "dist/chart/templates/monitoring/stale.yaml", "STALE\n")

	if err := Generate(opts); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "dist/chart/templates/monitoring/stale.yaml")); !os.IsNotExist(err) {
		t.Errorf("stale template should have been removed, stat err=%v", err)
	}
}

func TestGenerateFailsWhenPatchDoesNotApply(t *testing.T) {
	_, opts := fixture(t)
	opts.Patches = []Patch{
		{File: "values.yaml", Apply: ReplaceOnce("NOT-IN-THE-FILE", "x")},
	}
	if err := Generate(opts); err == nil {
		t.Fatal("Generate should fail when a patch finds nothing to replace")
	}
}

func TestReplaceOnceRejectsAmbiguousMatch(t *testing.T) {
	if _, err := ReplaceOnce("a", "b")("a a"); err == nil {
		t.Fatal("ReplaceOnce should fail on two occurrences")
	}
}

func TestInjectManagerEnv(t *testing.T) {
	in := "      containers:\n" +
		"      - args:\n" +
		"        command:\n" +
		"        - /manager\n" +
		"        image: \"{{ .Values.manager.image.repository }}\"\n" +
		"        name: manager\n"
	want := "      containers:\n" +
		"      - args:\n" +
		"        command:\n" +
		"        - /manager\n" +
		"        env:\n" +
		"        - name: OPERATOR_IMAGE\n" +
		"          value: \"{{ .Values.manager.image.repository }}\"\n" +
		"        {{- with .Values.manager.env }}\n" +
		"        {{- toYaml . | nindent 8 }}\n" +
		"        {{- end }}\n" +
		"        image: \"{{ .Values.manager.image.repository }}\"\n" +
		"        name: manager\n"
	got, err := InjectManagerEnv(in)
	if err != nil {
		t.Fatalf("InjectManagerEnv: %v", err)
	}
	if got != want {
		t.Errorf("unexpected output:\n%s", got)
	}

	withEnv := strings.Replace(in, "        image:", "        env: []\n        image:", 1)
	if _, err := InjectManagerEnv(withEnv); err == nil {
		t.Error("InjectManagerEnv should refuse a template that already has an env block")
	}
	if _, err := InjectManagerEnv("no image here\n"); err == nil {
		t.Error("InjectManagerEnv should fail without an image line")
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

func TestGenerateRestoresPreservedFilesOnKubebuilderError(t *testing.T) {
	root, opts := fixture(t)

	// Replace RunKubebuilder to clobber a preserved file then error.
	opts.RunKubebuilder = func() error {
		writeFile(t, root, "config/manager/kustomization.yaml", "CLOBBERED-KUSTOMIZATION\n")
		return fmt.Errorf("simulated kubebuilder failure")
	}

	// Generate should return an error.
	err := Generate(opts)
	if err == nil {
		t.Fatal("Generate should have returned an error")
	}

	// Preserved file must be restored to original content despite the error.
	if got := readFile(t, root, "config/manager/kustomization.yaml"); got != "ORIGINAL-KUSTOMIZATION\n" {
		t.Errorf("kustomization not restored on error: got %q, want %q", got, "ORIGINAL-KUSTOMIZATION\n")
	}
}
