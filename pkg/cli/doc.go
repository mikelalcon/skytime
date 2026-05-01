// Package cli is the reusable cobra command tree for Skytime.
//
// pkg/cli is the ONLY library-side package permitted to import
// github.com/spf13/cobra, github.com/spf13/pflag, and
// charm.land/log/v2. The AST firewall in
// tests/firewall_cli_test.go gates this — see D4-13 (Phase 4 CONTEXT.md).
//
// The package is empty in Wave 0; root command, subcommands, and
// renderer land in Waves 3 and 4. Doc-only file ensures the package
// exists so the firewall allow-list is non-vacuous from the moment
// it ships.
//
// (Charm-log was renamed upstream from github.com/charmbracelet/log/v2
// to charm.land/log/v2; the firewall forbidden list and this doc track
// the new module path. See tools.go for the anchor.)
package cli
