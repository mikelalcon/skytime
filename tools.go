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
//
// Phase 4 CLI anchors: Plan 04-01 (Phase 4 Wave 0) lands cobra,
// charm.land/log/v2, and golang.org/x/term ahead of pkg/cli (Plan 04-W3)
// importing them. The CLI firewall test (tests/firewall_cli_test.go) is
// load-bearing from Wave 0 and must run against go.mod entries that are
// guaranteed to exist; without these anchors, `go mod tidy` would prune
// the requires before any pkg/cli code lands.
//
// NOTE: charm-log was renamed upstream from `github.com/charmbracelet/log/v2`
// to `charm.land/log/v2` (the GitHub repo still hosts the source; the module
// path moved). Plan 04-01's anchor and the firewall forbidden list both use
// the new path. Documented as Rule 3 deviation in 04-01-SUMMARY.md.
//
// Remove the Phase 4 anchors once pkg/cli ships and `go mod tidy` is happy
// without manual help.
package tools

import (
	// Phase 2 Wave 1 / Wave 2 (pkg/activity) will import these directly.
	// Importing one sub-package here is enough to anchor the whole module
	// in go.mod — `activity` is the SDK surface pkg/activity will consume.
	_ "go.temporal.io/sdk/activity"

	// Phase 4 Wave 3 (pkg/cli) will import these directly. Each anchor
	// touches one symbol in the module so `go mod tidy` retains the
	// require line.
	_ "charm.land/log/v2"
	_ "github.com/spf13/cobra"
	_ "golang.org/x/term"
)
