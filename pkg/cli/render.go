package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	charmlog "charm.land/log/v2"
	"github.com/charmbracelet/colorprofile"
	"golang.org/x/term"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// silentSDKLevel is the threshold the SDK-side slog logger runs at when
// --verbose is false. slog.LevelError is +8 (the highest standard
// level); +1 above it drops every record the SDK might emit at standard
// levels (Debug, Info, Warn, Error). The pkg/cli renderer routes
// `event=*` records BEFORE delegating to this handler, so Skytime
// progress events are unaffected — only raw SDK noise is suppressed.
const silentSDKLevel = slog.LevelError + 1

// setupLogging returns the *slog.Logger pkg/cli uses. Library packages
// log through slog.Default(); pkg/cli swaps the default to this handler
// at PersistentPreRunE so every Skytime-side log line renders through
// charm-log.
//
// TTY detection: charm-log auto-disables color when stderr is not a
// TTY, but explicit colorprofile.ASCII is defense-in-depth (Pitfall #6
// in 04-RESEARCH.md). golang.org/x/term.IsTerminal on stderr's fd is
// the canonical x/term check.
//
// ReportCaller is enabled only with --debug per D4-19 — keeps default
// output Starlark-focused.
//
// Note: charm-log was renamed upstream from github.com/charmbracelet/log/v2
// to charm.land/log/v2; same v2.0.0 source. The downsample profile lives
// in github.com/charmbracelet/colorprofile (a separate, indirect dep);
// charmlog.SetColorProfile takes a colorprofile.Profile.
func setupLogging(debug bool) *slog.Logger {
	level := charmlog.InfoLevel
	if debug {
		level = charmlog.DebugLevel
	}
	opts := charmlog.Options{
		Level:           level,
		ReportTimestamp: false,
		ReportCaller:    debug,
		TimeFormat:      time.Kitchen,
	}
	h := charmlog.NewWithOptions(os.Stderr, opts)
	if !term.IsTerminal(int(os.Stderr.Fd())) {
		h.SetColorProfile(colorprofile.ASCII)
	}
	logger := slog.New(h)
	slog.SetDefault(logger)
	return logger
}

// buildSDKSlogLogger returns the *slog.Logger handed to the Temporal
// SDK (worker, client). Behavior depends on cfg.Verbose:
//
//   - Verbose=false → wraps a slog.NewTextHandler at level=silentSDKLevel
//     pointing at io.Discard. SDK INFO/DEBUG records are dropped before
//     reaching any user-visible writer.
//
//   - Verbose=true → returns cfg.logger (the same charm-log handler used
//     for Skytime-side messages) so SDK lines render with the same
//     formatting as everything else.
//
// Skytime's interpreter `event=*` records are unaffected by this choice
// because the progressHandler in front of the SDK logger short-circuits
// them to the Bazel renderer before any level threshold applies (see
// progress.go Enabled).
func buildSDKSlogLogger(cfg *config) *slog.Logger {
	if cfg.Verbose {
		return cfg.logger
	}
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: silentSDKLevel}))
}

// buildRoutedSlogLogger returns the *slog.Logger handed to the worker
// + Temporal client. It wraps cfg.sdkLogger's handler with a
// *progressHandler that renders Skytime `event=*` records to
// progressOut (Bazel-style or live-block, per useLiveBlock()) and
// delegates everything else to the cfg.sdkLogger handler.
//
// progressOut is normally cmd.OutOrStdout() — the Bazel rendering goes
// to stdout, the SDK passthrough goes to stderr (via the wrapped
// charm-log handler). Tests pass a *bytes.Buffer for assertion.
//
// Phase 04.1-06: cfg.Verbose is threaded through so the live-block
// renderer (D4.1-17) deactivates when --verbose is set (D4.1-20).
func buildRoutedSlogLogger(cfg *config, progressOut io.Writer) *slog.Logger {
	wrapped := cfg.sdkLogger.Handler()
	routed := newProgressHandlerWithOptions(wrapped, progressOut, progressHandlerOptions{
		Verbose: cfg.Verbose,
	})
	return slog.New(routed)
}

// renderError writes a single error to out using D4-18 Starlark-first
// semantics. With debug=false: only the typed dag error message (or a
// bare err.Error() string when the chain has no typed dag error) is
// printed. With debug=true: the Wrapped chain is unwound and printed
// below the primary line per D4-19.
//
// CLI rendering goes to plain text (no ANSI color) so consultants can
// grep — charm-log is for slog-routed logs, not direct error output.
func renderError(out io.Writer, err error, debug bool) {
	if err == nil {
		return
	}

	// Resolve the Starlark-first message: typed dag errors first.
	var pe *dag.ParseError
	var ve *dag.ValidationError
	switch {
	case errors.As(err, &pe):
		fmt.Fprintln(out, pe.Error())
	case errors.As(err, &ve):
		fmt.Fprintln(out, ve.Error())
	default:
		// Untyped error: print its Error() string but NOT the chain
		// (D4-18: drop wrapped Go errors from default output). Debug
		// mode prints the chain below.
		fmt.Fprintln(out, err.Error())
	}

	if debug {
		renderWrappedChain(out, err)
	}
}

// renderWrappedChain walks err's Unwrap chain and prints each level
// indented under "cause:". Stops when Unwrap returns nil or the same
// error twice (defensive — a bad Unwrap implementation must not loop).
func renderWrappedChain(out io.Writer, err error) {
	seen := map[error]struct{}{err: {}}
	cause := errors.Unwrap(err)
	for cause != nil {
		if _, dup := seen[cause]; dup {
			break
		}
		seen[cause] = struct{}{}
		fmt.Fprintf(out, "  cause: %s\n", cause.Error())
		cause = errors.Unwrap(cause)
	}
}

// renderErrors is the multi-error helper for subcommands that get
// []error from validator.Validate. Renders each error one per line
// via renderError so the rendering rules stay in one place.
func renderErrors(out io.Writer, errs []error, debug bool) {
	for _, e := range errs {
		renderError(out, e, debug)
	}
}
