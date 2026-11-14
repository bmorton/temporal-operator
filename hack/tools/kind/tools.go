//go:build tools

// Package tools pins the kind version used by the Makefile. Dependabot
// updates the requirement in go.mod; `make kind` builds it into ./bin.
package tools

import _ "sigs.k8s.io/kind"
