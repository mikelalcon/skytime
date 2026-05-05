// Package parser regenerates docs/reference/builtins.md from this
// package's globals.go and builtins.go via cmd/skytime-docgen.
//
// Refresh after changing any builtin signature or // skytime:doc marker:
//
//	go generate ./pkg/parser/
//
// CI verifies drift via tests/docgen_drift_test.go.
package parser

//go:generate go run github.com/mikelalcon/skytime/cmd/skytime-docgen --pkg . --out ../../docs/reference/builtins.md
