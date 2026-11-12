//go:build tools

// Package tools pins the golangci-lint version used by the Makefile. Dependabot
// updates the requirement in go.mod; `make golangci-lint` builds it into ./bin.
package tools

import _ "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
