package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	// ResetDirs are deleted before generation, so a template the generator
	// stops emitting does not linger in the chart.
	ResetDirs []string
	// Patches edit generated files after generation and before overrides are
	// copied. A patch that cannot be applied fails the run.
	Patches []Patch
	// RunKubebuilder performs the upstream generation step.
	RunKubebuilder func() error
}

// Patch rewrites one generated file. File is relative to Options.ChartDir.
type Patch struct {
	File  string
	Apply func(content string) (string, error)
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

	for _, rel := range opts.ResetDirs {
		if err := os.RemoveAll(filepath.Join(opts.Root, rel)); err != nil {
			return fmt.Errorf("reset %s: %w", rel, err)
		}
	}

	if err := opts.RunKubebuilder(); err != nil {
		_ = restore(snaps)
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

	for _, p := range opts.Patches {
		if err := applyPatch(opts.ChartDir, p); err != nil {
			return fmt.Errorf("patch %s: %w", p.File, err)
		}
	}

	if err := copyOverrides(opts.OverridesDir, opts.ChartDir); err != nil {
		return fmt.Errorf("copy overrides: %w", err)
	}
	return nil
}

func snapshot(root string, rels []string) ([]snapshotEntry, error) {
	entries := make([]snapshotEntry, 0, len(rels))
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
			_ = tmp.Close()
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

func applyPatch(chartDir string, p Patch) error {
	path := filepath.Join(chartDir, p.File)
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out, err := p.Apply(string(data))
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

// ReplaceOnce returns a patch function that replaces the single occurrence of
// old with replacement. It fails when old is absent or ambiguous, so a change
// in the generator's output is noticed instead of silently skipped.
func ReplaceOnce(old, replacement string) func(string) (string, error) {
	return func(content string) (string, error) {
		if n := strings.Count(content, old); n != 1 {
			return "", fmt.Errorf("expected exactly one occurrence of %q, found %d", old, n)
		}
		return strings.Replace(content, old, replacement, 1), nil
	}
}

var managerImageLine = regexp.MustCompile(`(?m)^( +)image: (.+)\n`)

// InjectManagerEnv adds an env block to the generated manager Deployment
// template. OPERATOR_IMAGE repeats the container's own image expression, so
// the operator can start Jobs from the image it is running. Entries from
// .Values.manager.env follow it.
func InjectManagerEnv(content string) (string, error) {
	matches := managerImageLine.FindAllStringSubmatchIndex(content, -1)
	if len(matches) != 1 {
		return "", fmt.Errorf("expected exactly one container image line, found %d", len(matches))
	}
	m := matches[0]
	indent := content[m[2]:m[3]]
	if strings.Contains(content, "\n"+indent+"env:") {
		return "", fmt.Errorf("generated template already has an env block; merge it with the injected one")
	}
	image := content[m[4]:m[5]]
	env := indent + "env:\n" +
		indent + "- name: OPERATOR_IMAGE\n" +
		indent + "  value: " + image + "\n" +
		indent + "{{- with .Values.manager.env }}\n" +
		indent + "{{- toYaml . | nindent " + fmt.Sprint(len(indent)) + " }}\n" +
		indent + "{{- end }}\n"
	return content[:m[0]] + env + content[m[0]:], nil
}
