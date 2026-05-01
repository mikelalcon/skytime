// Package dryrun is the test-only mock dispatch for the differential
// corpus test (VAL-02, D4-03 — Phase 4 CONTEXT.md). Internal-only:
// imported solely by pkg/validator's own tests and by tests/
// differential_test.go (placed at tests/ to side-step the temporal
// firewall in pkg/activity/firewall_test.go). NOT a public API and NOT
// a CLI flag — Phase 5's Starlark mock harness will reuse the
// dispatch-replacement seam with a different mock body.
//
// The "AlwaysOk" name is literal: every registered op's Func is
// replaced with one that returns (nil OperationOutput, nil error). The
// schema reflection (KwargsType) and Idempotent declaration are
// preserved verbatim — only I/O is bypassed.
package dryrun
