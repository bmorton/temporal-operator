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
