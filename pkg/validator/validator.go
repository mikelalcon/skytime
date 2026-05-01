package validator

import (
	"github.com/mikelalcon/skytime/pkg/parser"
)

// Validate parses file through pkg/parser and returns the typed errors
// (*dag.ParseError or *dag.ValidationError) as a slice.
//
// Returns:
//   - empty []error on success (NOT nil — the empty slice signals "no
//     errors" unambiguously to callers iterating len(errs))
//   - one-element slice on parser construction failure (e.g., extension
//     ErrIdempotentRequired)
//   - one-element slice on parse failure (the parser short-circuits on
//     first error per finalize-pass convention)
//
// Future enhancement: collect-then-return-all errors when the parser
// grows multi-error reporting. For v1, single-error short-circuit
// matches the runtime parser's behavior 1:1, which is exactly what
// VAL-02's "static + runtime parser share the same code path" demands.
func Validate(file string, opts ...Option) []error {
	cfg := &config{}
	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return []error{err}
		}
	}

	parserOpts := []parser.Option{}
	if cfg.root != "" {
		parserOpts = append(parserOpts, parser.WithRoot(cfg.root))
	}
	if len(cfg.exts) > 0 {
		parserOpts = append(parserOpts, parser.WithExtensions(cfg.exts...))
	}

	p, err := parser.NewParser(parserOpts...)
	if err != nil {
		return []error{err}
	}
	if _, err := p.ParseFile(file); err != nil {
		return []error{err}
	}
	return []error{} // no errors — empty (non-nil) slice
}
