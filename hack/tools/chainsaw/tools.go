//go:build tools

// Package tools pins the chainsaw version used by the Makefile. Dependabot
// updates the requirement in go.mod; `make chainsaw` builds it into ./bin.
package tools

import _ "github.com/kyverno/chainsaw"
