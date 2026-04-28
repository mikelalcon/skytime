//go:build tools
// +build tools

// Package tools anchors module-level dependencies that are not yet imported
// by any production package but are required by upcoming work. Build
// constraint `tools` ensures these imports never compile into a real binary
// — they exist solely to keep `go mod tidy` from pruning the entries from
// go.mod / go.sum.
//
// go.temporal.io/sdk anchor: Plan 02-01 (Phase 2 Wave 0) lands the Temporal
// SDK dependency at v1.42.0 ahead of pkg/activity (Plans 02-02 / 02-03)
// importing it. Without this anchor, `go mod tidy` would remove the
// require line, breaking the type-spine handoff between waves.
//
// Remove the temporal anchor below once pkg/activity ships and `go mod tidy`
// is happy without manual help.
package tools

import (
	// Phase 2 Wave 1 / Wave 2 (pkg/activity) will import these directly.
	// Importing one sub-package here is enough to anchor the whole module
	// in go.mod — `activity` is the SDK surface pkg/activity will consume.
	_ "go.temporal.io/sdk/activity"
)
