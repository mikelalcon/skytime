package cli

import (
	"context"
	"fmt"

	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/interpreter"
	"github.com/mikelalcon/skytime/pkg/worker"
)

// loadRegistries bootstraps the parser-side registries (flows + triggers)
// from a rootdir of .star files WITHOUT requiring a Temporal client.
// This is the read-only boot path used by:
//
//   - skytime cron-plan (dry-run; no cluster mutations; uses the
//     registries to compute the desired-state Plan, then List vs that)
//   - future read-only subcommands that need to parse `.star` files
//     without dialing Temporal
//
// Per D-7.2-21 (test seam discretion): this is the parse-side seam
// (the cluster-side seam is WithScheduleClientFactory in options.go).
// Together they make cron-plan unit-testable without a real cluster.
//
// The ctx parameter is reserved for future use (cancellation during
// large rootdir walks); the underlying worker.LoadRegistries is
// synchronous in the current implementation.
func loadRegistries(
	_ context.Context,
	rootdir string,
	exts []extension.Extension,
) (*interpreter.FlowRegistry, *interpreter.TriggerRegistry, error) {
	if rootdir == "" {
		return nil, nil, fmt.Errorf("loadRegistries: rootdir is required")
	}
	flowReg, trigReg, err := worker.LoadRegistries(rootdir, exts)
	if err != nil {
		return nil, nil, fmt.Errorf("load registries from %s: %w", rootdir, err)
	}
	return flowReg, trigReg, nil
}
