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
