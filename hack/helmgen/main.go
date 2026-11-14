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
		// Everything under templates/ is either regenerated or copied from
		// the overrides, so start from an empty directory.
		ResetDirs: []string{
			"dist/chart/templates",
		},
		Patches: []Patch{
			{
				File: "values.yaml",
				Apply: ReplaceOnce(
					"    repository: controller\n",
					"    repository: ghcr.io/bmorton/temporal-operator\n",
				),
			},
			{
				File: "values.yaml",
				Apply: ReplaceOnce(
					"  args:\n    - --leader-elect\n",
					"  args:\n    - --leader-elect\n\n"+
						"  ## Extra environment variables for the manager container\n"+
						"  ## (OPERATOR_IMAGE is always set by the chart)\n"+
						"  ##\n"+
						"  env: []\n",
				),
			},
			{
				File:  "templates/manager/manager.yaml",
				Apply: InjectManagerEnv,
			},
		},
		RunKubebuilder: func() error {
			// --force regenerates values.yaml, _helpers.tpl and the other
			// files the plugin otherwise leaves alone once they exist, so the
			// chart always matches the pinned kubebuilder version.
			cmd := exec.Command(*kubebuilder, "edit", "--plugins=helm/v2-alpha", "--force")
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
