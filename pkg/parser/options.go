package parser

import (
	"fmt"

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
