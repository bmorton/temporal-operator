//go:build tools

// Package tools pins the kubebuilder version used by the Makefile. Dependabot
// updates the requirement in go.mod; `make kubebuilder` builds it into ./bin.
package tools

import _ "sigs.k8s.io/kubebuilder/v4"
