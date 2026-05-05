package parser

import (
	"fmt"

	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// Option mutates a Parser at construction time. Returning an error lets
// option application fail fast on bad input (e.g. registering an extension
// whose Idempotent declaration is missing — D-12 / ErrIdempotentRequired).
//
// Functional options are the canonical Go pattern for this kind of
// configuration; the per-parser registry (D-07) is the stake in the ground
// against package-global state.
type Option func(*Parser) error

// WithRoot sets the sandbox root for load() resolution (D-14). Absolute
// load paths (`/shared/util.star`) resolve relative to this root; relative
// paths (`./util.star`) resolve relative to the loading file. When unset, a
// `.git` ancestor of the file being parsed becomes the implicit root.
func WithRoot(root string) Option {
	return func(p *Parser) error {
		p.root = root
		return nil
	}
}

// WithExtensions registers one or more extensions on the parser's per-parser
// registry (D-07). Returns the first registration error (e.g.
// ErrIdempotentRequired) so NewParser surfaces it to the caller rather than
// silently swallowing.
func WithExtensions(exts ...extension.Extension) Option {
	return func(p *Parser) error {
		for _, e := range exts {
			if err := p.registry.Register(e); err != nil {
				return err
			}
		}
		return nil
	}
}

// WithMaxExecutionSteps overrides the default Starlark step budget per parse
// (and load()). D-22 default is 10_000_000.
func WithMaxExecutionSteps(n uint64) Option {
	return func(p *Parser) error {
		p.maxExecSteps = n
		return nil
	}
}

// WithMaxBlockSize overrides the default block-size cap (50) for
// step(block=[...]) per D2-07. Values < 1 return an error at parser
// construction time; the cap is enforced by the lintBlockSize pass at
// parse-finalize time.
//
// The activity (pkg/activity, Phase 2) defensively re-enforces this at
// runtime — the parser cap exists primarily for fast-fail UX, not safety;
// the activity is the safety boundary. Tightening the cap only restricts
// what consultant `.star` files can declare; loosening it past the
// activity's defensive limit (also 50 by default) would let parse-time
// blocks pass through, hit the activity limit, and fail at execute time.
// Coordinate any change with the activity-side cap.
func WithMaxBlockSize(n int) Option {
	return func(p *Parser) error {
		if n < 1 {
			return fmt.Errorf("WithMaxBlockSize: invalid max block size %d: must be >= 1", n)
		}
		p.maxBlockSize = n
		return nil
	}
}

// WithTestMode enables Phase 5's test-file parse path. When set, the
// parser's parse-time globals include the `tester`
// *starlarkstruct.Module and the assert.* globals from
// go.starlark.net/starlarktest. The production parse path
// (`parser.NewParser()` with no test options) is unchanged.
//
// Phase 5 D5-A1..A4 + TEST-01.
func WithTestMode() Option {
	return func(p *Parser) error {
		p.testMode = true
		return nil
	}
}

// WithTestModule wires the function that builds the `tester`
// *starlarkstruct.Module value bound under the global name "tester"
// in test-mode parses. Splitting this from WithTestMode breaks the
// parser→pkg/testing import cycle: pkg/cli/test.go (Phase 5 Plan 06)
// imports pkg/testing, constructs the builder, and supplies it via
// this option.
//
// Returns an error if builderFn is nil — defensive against the
// programming error of calling WithTestMode() but forgetting
// WithTestModule.
func WithTestModule(builderFn func(p *Parser, thread *starlark.Thread) starlark.Value) Option {
	return func(p *Parser) error {
		if builderFn == nil {
			return fmt.Errorf("WithTestModule: builder must not be nil")
		}
		p.testModuleBuilder = builderFn
		return nil
	}
}
