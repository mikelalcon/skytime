// Package validator is the thin façade in front of pkg/parser for the
// skytime validate CLI subcommand and the differential-corpus dry-run
// test (D4-01, D4-03 — Phase 4 CONTEXT.md).
//
// The validator does NOT implement new checks — every check (kwarg
// cross-validate, ctx.<name> walk, free-vars-reference-state, lambda
// global subset) lives in pkg/parser/finalize.go. The facade exists to
// give the CLI a single non-cobra entry point and to host the
// differential-corpus test harness (tests/differential_test.go) via the
// internal/dryrun mock dispatch.
//
// Architecture firewall: this package MUST NOT import cobra, pflag, or
// charm.land/log/v2. The cross-tree firewall in
// tests/firewall_cli_test.go::TestNoCobraImportsOutsideAllowList enforces
// the rule.
package validator
