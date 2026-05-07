package credfile

import "log/slog"

// config carries the resolved options. Internal — not exported.
type config struct {
	path   string
	strict bool
	logger *slog.Logger
}

// Option mutates a config. Construct via the With* helpers.
type Option func(*config)

// WithPath overrides the default $HOME/.skytime-credentials.
// Empty string is treated as "use default" (caller convenience for
// env-var → option translation: empty env → fallback).
func WithPath(p string) Option {
	return func(c *config) {
		if p != "" {
			c.path = p
		}
	}
}

// WithStrictMode refuses to load a credfile whose mode bits include
// group/other read (mode & 0o044 != 0). Default behavior is warn-only
// via slog (D-CREDS-FORMAT). On Windows the file-mode check is skipped
// entirely (POSIX semantics do not apply); see resolver.go.
func WithStrictMode() Option {
	return func(c *config) { c.strict = true }
}

// WithLogger overrides slog.Default() for the file-mode warning and
// any other diagnostic output. Pass slog.New(slog.DiscardHandler) to
// silence completely.
func WithLogger(l *slog.Logger) Option {
	return func(c *config) {
		if l != nil {
			c.logger = l
		}
	}
}
