//go:build tools

// Package tools pins the controller-gen version used by the Makefile. Dependabot
// updates the requirement in go.mod; `make controller-gen` builds it into ./bin.
package tools

import _ "sigs.k8s.io/controller-tools/cmd/controller-gen"
