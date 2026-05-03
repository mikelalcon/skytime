package cli

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"

	"golang.org/x/term"
)

// progressHandler wraps another slog.Handler and intercepts records
// carrying an "event" attribute, rendering them either as Bazel-style
// progress lines (static path) or as a multi-line redrawing live block
// (live path) on its output writer.
//
// Records without "event" pass through to the wrapped handler unchanged
// (e.g., raw SDK INFO/DEBUG lines, which then either render via
// charm-log when --verbose is set, or get dropped when --verbose is off
// and the wrapped handler is at LevelError+1).
//
// Mode selection (D4.1-17..21):
//   - useLiveBlock() == true  → multi-line live block (TTY + non-verbose,
//     non-Windows). Implemented in pkg/cli/progress_live.go.
//   - useLiveBlock() == false → static line-per-event Bazel-style output.
//     Implemented in pkg/cli/progress_static.go.
//
// Bazel-style format (quick 260502-guu Fix B):
//
//	[skytime] flow simple_check  3 steps  starting
//	[1/3] step                gh.get(/repos/example/repo)
//	     ✓ 234ms  status=200
//	[3/3] if_cond             ctx.health  → then
//	[skytime] flow complete  3/3 steps  total 433ms
//
// The renderer dispatches on the `event` attribute value:
//   - flow_start    → renderFlowStart(r)
//   - step_dispatch → renderStepDispatch(r)
//   - step_complete → renderStepComplete(r)
//   - branch        → renderBranch(r)
//   - flow_complete → renderFlowComplete(r)
//
// Color is applied only when the output writer is a TTY; non-TTY drops
// to plain ASCII (greppable by tooling).
type progressHandler struct {
	wrapped slog.Handler
	out     io.Writer

	// ttyKnown caches the TTY check on `out`. We compute it once at
	// first useLiveBlock()/wrap() call rather than at construction so
	// callers can swap out (rare in production, common in tests) and
	// the cache resets per-handler.
	ttyKnown bool
	tty      bool

	// verbose mirrors the --verbose persistent flag. When true, the live
	// block is disabled (D4.1-20) — SDK INFO/DEBUG lines stream to
	// stderr and would constantly break the live region's redraw.
	verbose bool

	// lastErr captures the most recent step_complete-with-err record so
	// renderFlowComplete can attribute the failure when err_count > 0.
	// Reset to nil on every flow_start so a long-lived handler (one
	// process, multiple workflow executions) does not leak failure
	// state across runs. Quick 260502-onc Fix C.
	lastErr *failureContext

	// live is set lazily on the first Handle() call when useLiveBlock()
	// returns true. nil for static-path handlers. Owned by this
	// handler — Close() cleans up.
	live     *liveRenderer
	liveOnce sync.Once
}

// failureContext is the per-handler record of the most recent
// step_complete-with-err event. Captured by renderStepComplete and
// consumed by renderFlowComplete on the err_count > 0 branch.
type failureContext struct {
	idx     int64
	total   int64
	summary string
}

// progressEvent is the immutable value the slog handler ships to the
// live renderer. Defined here (no build tag) so both Unix and Windows
// liveRenderer variants share an identical struct shape — otherwise
// buildProgressEvent's field accesses would compile on Unix but fail
// on Windows. See progress_live.go (!windows) and
// progress_live_windows.go for the platform-specific renderer.
type progressEvent struct {
	Kind       string // "flow_start" | "step_dispatch" | "step_complete" | "branch" | "flow_complete" | "raw"
	FlowName   string
	StepCount  int64
	Idx        int64
	Total      int64
	KindAttr   string // event["kind"] — "step", "if_cond", etc.
	Label      string
	Status     string
	Summary    string
	DurationMs int64
	OkCount    int64
	ErrCount   int64
	TotalMs    int64
	Branch     string
	Path       string
	Raw        string // pre-rendered line for "raw" events
}

// progressHandlerOptions configures progressHandler at construction.
// Used when callers need to inject the verbose flag or override TTY
// detection (tests). The bare newProgressHandler(wrapped, out) signature
// stays for backward compatibility — it constructs with Verbose=false
// and auto-TTY.
type progressHandlerOptions struct {
	Verbose  bool
	ForceTTY *bool // nil = auto-detect via term.IsTerminal; non-nil = test override
}

// newProgressHandler returns a handler that writes Bazel-style progress
// lines to out and delegates everything else to wrapped. Constructed
// with Verbose=false and auto-TTY detection.
func newProgressHandler(wrapped slog.Handler, out io.Writer) *progressHandler {
	return &progressHandler{wrapped: wrapped, out: out}
}

// newProgressHandlerWithOptions constructs a progressHandler with
// explicit verbose / TTY-override settings. Callers (run subcommand)
// thread cfg.Verbose; tests inject ForceTTY to exercise the live path
// without an actual terminal.
func newProgressHandlerWithOptions(wrapped slog.Handler, out io.Writer, opts progressHandlerOptions) *progressHandler {
	p := newProgressHandler(wrapped, out)
	p.verbose = opts.Verbose
	if opts.ForceTTY != nil {
		p.ttyKnown = true
		p.tty = *opts.ForceTTY
	}
	return p
}

// Enabled returns true unconditionally so SDK-level Enabled() pre-checks
// never gate Skytime progress records before they reach Handle. The
// wrapped handler still controls passthrough records via its own
// Enabled() inside Handle's delegation path.
//
// Why unconditional: when --verbose is false the wrapped charm-log
// handler runs at LevelError+1 and would drop INFO records — including
// our progress events — before Handle ever runs. The progress events
// are routed by attribute, not severity; Enabled must let them through.
func (p *progressHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

// Handle routes the record to the static Bazel renderer or to the live
// renderer when it carries the `event` attribute, or to the wrapped
// handler. The wrapped handler's Enabled() filters passthrough records
// by level.
//
// Phase 04.1-06 Task 3: dispatch honors useLiveBlock(). On TTY +
// non-verbose, the lazy-constructed liveRenderer owns all writes (its
// own goroutine writes to p.out). On non-TTY OR --verbose OR Windows,
// the static Bazel renderer writes directly to p.out.
func (p *progressHandler) Handle(ctx context.Context, r slog.Record) error {
	if !hasAttr(r, "event") {
		// Passthrough — but respect the wrapped handler's level.
		if !p.wrapped.Enabled(ctx, r.Level) {
			return nil
		}
		return p.wrapped.Handle(ctx, r)
	}
	if !p.useLiveBlock() {
		return p.renderBazelLine(r)
	}
	// Lazily construct the live renderer on the first event-bearing
	// record. liveOnce ensures only one goroutine ever runs even if
	// Handle is invoked concurrently from multiple workflow workers.
	p.liveOnce.Do(func() {
		p.live = newLiveRenderer(p.out)
	})
	p.live.submit(buildProgressEvent(r))
	return nil
}

// Close shuts down the background render goroutine if a live renderer
// was activated. Safe to call multiple times; safe to call on
// static-path handlers (no-op). The cli.Run subcommand should defer
// this after the workflow completes so the redraw region drains
// cleanly.
func (p *progressHandler) Close() {
	if p.live != nil {
		p.live.Close()
	}
}

// WithAttrs returns a new progressHandler whose wrapped handler has the
// attrs applied. The progress writer is unchanged — pre-applied attrs
// are part of the wrapped handler's state. lastErr is shallow-copied
// (both handlers share the same *failureContext pointer); for the v1
// usage pattern (one workflow, serial events) this is correct.
//
// Note: live + liveOnce are NOT propagated. The first Handle() on the
// returned handler will lazily construct its own renderer if needed.
// This is correct for slog's WithAttrs/WithGroup contract (the returned
// handler is logically a sibling, not an alias).
func (p *progressHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &progressHandler{
		wrapped:  p.wrapped.WithAttrs(attrs),
		out:      p.out,
		ttyKnown: p.ttyKnown,
		tty:      p.tty,
		verbose:  p.verbose,
		lastErr:  p.lastErr,
	}
}

// WithGroup returns a new progressHandler whose wrapped handler is
// scoped under the named group. The progress writer is unchanged.
// lastErr is shallow-copied per the WithAttrs note above.
func (p *progressHandler) WithGroup(name string) slog.Handler {
	return &progressHandler{
		wrapped:  p.wrapped.WithGroup(name),
		out:      p.out,
		ttyKnown: p.ttyKnown,
		tty:      p.tty,
		verbose:  p.verbose,
		lastErr:  p.lastErr,
	}
}

// useLiveBlock returns true when the handler should activate the
// multi-line live-block renderer (D4.1-17). False on Windows (the
// stub in progress_live_windows.go ensures live renderer is a no-op),
// non-TTY (D4.1-21), or --verbose (D4.1-20).
func (p *progressHandler) useLiveBlock() bool {
	if p.verbose {
		return false
	}
	return p.isTTY()
}

// isTTY memoizes the term.IsTerminal check on p.out. When p.out is a
// *os.File whose fd is a terminal, color and live block are enabled;
// otherwise (bytes buffer, pipe, redirect, non-file Writer) ASCII
// fallback applies. Tests inject ForceTTY via progressHandlerOptions to
// bypass the auto-detection.
func (p *progressHandler) isTTY() bool {
	if p.ttyKnown {
		return p.tty
	}
	p.ttyKnown = true
	if f, ok := p.out.(*os.File); ok {
		p.tty = term.IsTerminal(int(f.Fd()))
	} else {
		p.tty = false
	}
	return p.tty
}

// buildProgressEvent translates a slog.Record into a progressEvent
// value the live renderer consumes. Pure function — no shared state,
// safe to call from Handle() under concurrent invocations.
func buildProgressEvent(r slog.Record) progressEvent {
	attrs := collectAttrs(r)
	return progressEvent{
		Kind:       attrs.str("event"),
		FlowName:   attrs.str("flow_name"),
		StepCount:  attrs.int("step_count"),
		Idx:        attrs.int("idx"),
		Total:      attrs.int("total"),
		KindAttr:   attrs.str("kind"),
		Label:      attrs.str("label"),
		Status:     attrs.str("status"),
		Summary:    attrs.str("summary"),
		DurationMs: attrs.int("duration_ms"),
		OkCount:    attrs.int("ok_count"),
		ErrCount:   attrs.int("err_count"),
		TotalMs:    attrs.int("total_ms"),
		Branch:     attrs.str("branch"),
		Path:       attrs.str("path"),
	}
}

// renderBazelLine inspects the `event` attribute value and dispatches
// to the per-event STATIC renderer. Unknown event values fall back to
// a no-op (defense in depth — the interpreter is the only known
// producer and the schema is closed).
func (p *progressHandler) renderBazelLine(r slog.Record) error {
	attrs := collectAttrs(r)
	switch attrs.str("event") {
	case "flow_start":
		return p.renderFlowStart(attrs)
	case "step_dispatch":
		return p.renderStepDispatch(attrs)
	case "step_complete":
		return p.renderStepComplete(attrs)
	case "branch":
		return p.renderBranch(attrs)
	case "flow_complete":
		return p.renderFlowComplete(attrs)
	}
	return nil
}

// attrMap is a parsed view of a slog.Record's attributes — string keyed
// for easy lookup with type-aware accessors. Building this map once per
// record (instead of iterating r.Attrs in every renderer) keeps the
// dispatch-and-format split clean.
type attrMap map[string]slog.Value

func (m attrMap) str(k string) string {
	v, ok := m[k]
	if !ok {
		return ""
	}
	return v.String()
}

func (m attrMap) int(k string) int64 {
	v, ok := m[k]
	if !ok {
		return 0
	}
	return v.Int64()
}

// collectAttrs walks r.Attrs once and builds an attrMap. slog.Record
// stores attrs internally so we can't iterate them as a slice without
// allocating; the visitor pattern is the documented API.
func collectAttrs(r slog.Record) attrMap {
	m := make(attrMap, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		m[a.Key] = a.Value
		return true
	})
	return m
}

// hasAttr returns true when r contains an attribute with the given key.
func hasAttr(r slog.Record, key string) bool {
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			found = true
			return false // stop iteration
		}
		return true
	})
	return found
}
