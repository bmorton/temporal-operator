//go:build tools

// Package tools pins the crd-ref-docs version used by the Makefile. Dependabot
// updates the requirement in go.mod; `make crd-ref-docs` builds it into ./bin.
package tools

import _ "github.com/elastic/crd-ref-docs"
