//go:build tools

// Package tools pins the kustomize version used by the Makefile. Dependabot
// updates the requirement in go.mod; `make kustomize` builds it into ./bin.
package tools

import _ "sigs.k8s.io/kustomize/kustomize/v5"
