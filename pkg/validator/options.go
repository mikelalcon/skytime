package validator

import (
	"github.com/mikelalcon/skytime/pkg/extension"
)

// Option configures a Validate call. Mirrors pkg/parser's option style
// (functional options returning error so option-time validation can
// short-circuit). Validate accumulates options into a private *config and
// forwards them to parser.NewParser.
type Option func(*config) error

// config is the internal accumulator that Validate threads through Option
// closures and into parser.Option construction.
type config struct {
	root        string
	exts        []extension.Extension
	credHandler extension.CredentialHandler
}

// WithExtensions registers extensions with the underlying parser. Mirrors
// parser.WithExtensions; the Validate call constructs a parser.NewParser
// with each registered extension.
func WithExtensions(exts ...extension.Extension) Option {
	return func(c *config) error {
		c.exts = append(c.exts, exts...)
		return nil
	}
}

// WithCredentialHandler is accepted for API symmetry with pkg/cli's root
// command — the validator does NOT invoke the resolver itself (validate is
// parse-only), but pkg/cli plumbs the same option down to both
// validator.Validate and worker.NewWorker. Storing the handler here without
// using it keeps the option signatures uniform across the CLI surface.
func WithCredentialHandler(h extension.CredentialHandler) Option {
	return func(c *config) error {
		c.credHandler = h
		return nil
	}
}

// WithRoot sets the parser's load() sandbox root. Defaults to the file's
// directory (or its `.git` ancestor) when unset; pkg/cli passes this when
// --rootdir is supplied.
func WithRoot(root string) Option {
	return func(c *config) error {
		c.root = root
		return nil
	}
}
