package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"golang.org/x/term"
)

// progressHandler wraps another slog.Handler and intercepts records
// carrying an "event" attribute, rendering them as Bazel-style progress
// lines on its output writer. Records without "event" pass through to
// the wrapped handler unchanged (e.g., raw SDK INFO/DEBUG lines, which
// then either render via charm-log when --verbose is set, or get dropped
// when --verbose is off and the wrapped handler is at LevelError+1).
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
	// first Handle() rather than at construction so callers can swap
	// out (rare in production, common in tests) and the cache resets
	// per-handler.
	ttyKnown bool
	tty      bool
}

// newProgressHandler returns a handler that writes Bazel-style progress
// lines to out and delegates everything else to wrapped.
func newProgressHandler(wrapped slog.Handler, out io.Writer) *progressHandler {
	return &progressHandler{wrapped: wrapped, out: out}
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

// Handle routes the record to the Bazel renderer when it carries the
// `event` attribute, or to the wrapped handler otherwise. The wrapped
// handler's Enabled() filters passthrough records by level.
func (p *progressHandler) Handle(ctx context.Context, r slog.Record) error {
	if !hasAttr(r, "event") {
		// Passthrough — but respect the wrapped handler's level.
		if !p.wrapped.Enabled(ctx, r.Level) {
			return nil
		}
		return p.wrapped.Handle(ctx, r)
	}
	return p.renderBazelLine(r)
}

// WithAttrs returns a new progressHandler whose wrapped handler has the
// attrs applied. The progress writer is unchanged — pre-applied attrs
// are part of the wrapped handler's state.
func (p *progressHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &progressHandler{wrapped: p.wrapped.WithAttrs(attrs), out: p.out}
}

// WithGroup returns a new progressHandler whose wrapped handler is
// scoped under the named group. The progress writer is unchanged.
func (p *progressHandler) WithGroup(name string) slog.Handler {
	return &progressHandler{wrapped: p.wrapped.WithGroup(name), out: p.out}
}

// renderBazelLine inspects the `event` attribute value and dispatches
// to the per-event renderer. Unknown event values fall back to a no-op
// (defense in depth — the interpreter is the only known producer and
// the schema is closed).
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

// renderFlowStart: `[skytime] flow <flow_name>  <step_count> steps  starting`
func (p *progressHandler) renderFlowStart(a attrMap) error {
	flow := a.str("flow_name")
	count := a.int("step_count")
	line := fmt.Sprintf("%s flow %s  %d steps  starting",
		p.colorBanner("[skytime]"), flow, count)
	return p.println(line)
}

// renderStepDispatch: `[N/M] kind                label`
//
// When path indicates nested context (anything other than the bare
// numeric idx), the row is indented 2 spaces and the counter uses
// `[path/path]`.
func (p *progressHandler) renderStepDispatch(a attrMap) error {
	idx := a.int("idx")
	total := a.int("total")
	kind := a.str("kind")
	label := a.str("label")
	path := a.str("path")

	indent, counter := p.computeCounter(idx, total, path)
	line := fmt.Sprintf("%s%s %s %s",
		indent,
		p.colorCounter(counter),
		p.colorKind(padKind(kind)),
		label,
	)
	return p.println(line)
}

// renderStepComplete:
//   - on status=ok: `     ✓ <duration_ms>ms  <summary>`
//   - on status=err: `     ✗ <duration_ms>ms  <summary>`
//
// The completion line is indented 5 spaces from column 0 so the marker
// column-aligns roughly under the counter.
func (p *progressHandler) renderStepComplete(a attrMap) error {
	status := a.str("status")
	dur := a.int("duration_ms")
	summary := a.str("summary")
	path := a.str("path")
	idx := a.int("idx")

	// Nested rows already had their dispatch indented 2 spaces; their
	// completion row indents an additional 2 (total 4) so the marker
	// sits under the nested counter. Top-level rows indent 5 from col 0.
	indent := "     "
	if isNestedPath(idx, path) {
		indent = "       "
	}

	marker := p.colorOk("✓")
	if status == "err" {
		marker = p.colorErr("✗")
	}
	line := fmt.Sprintf("%s%s %dms  %s", indent, marker, dur, summary)
	return p.println(line)
}

// renderBranch: `<indent>→ <branch>` — the arrow indicates an if_cond
// took the named branch (then|else).
func (p *progressHandler) renderBranch(a attrMap) error {
	branch := a.str("branch")
	path := a.str("path")
	idx := a.int("idx")

	indent := "     "
	if isNestedPath(idx, path) {
		indent = "       "
	}

	line := fmt.Sprintf("%s%s %s", indent, p.colorArrow("→"), branch)
	return p.println(line)
}

// renderFlowComplete: `[skytime] flow complete  <ok>/<total> steps  total <ms>ms`
func (p *progressHandler) renderFlowComplete(a attrMap) error {
	ok := a.int("ok_count")
	errc := a.int("err_count")
	totalMs := a.int("total_ms")
	line := fmt.Sprintf("%s flow complete  %d/%d steps  total %dms",
		p.colorBanner("[skytime]"), ok, ok+errc, totalMs)
	return p.println(line)
}

// computeCounter picks the right counter format and indent for a row.
// Top-level rows render `[idx/total]` and have no indent; nested rows
// render `[path/path]` and indent 2 spaces.
func (p *progressHandler) computeCounter(idx, total int64, path string) (indent, counter string) {
	if isNestedPath(idx, path) {
		return "  ", fmt.Sprintf("[%s/%s]", path, path)
	}
	return "", fmt.Sprintf("[%d/%d]", idx, total)
}

// isNestedPath returns true when path indicates a nested step (anything
// other than the bare decimal representation of idx, or empty).
//
// Top-level path conventions: "1", "2", "3", ... (matches fmt.Sprintf("%d", idx)).
// Nested conventions: "3a", "3b" (if_cond branches), "3.0.0", "3.1.0" (for_each).
//
// Empty path is treated as top-level (defensive — early callers may
// emit dispatch events before the stepPath is initialized).
func isNestedPath(idx int64, path string) bool {
	if path == "" {
		return false
	}
	return path != fmt.Sprintf("%d", idx)
}

// padKind right-pads a kind label so kind columns align across rows.
// Width 19 is just over the longest known kind ("for_each_parallel" = 17).
func padKind(kind string) string {
	const width = 19
	if len(kind) >= width {
		return kind
	}
	return kind + strings.Repeat(" ", width-len(kind))
}

// println writes line followed by a newline to p.out.
func (p *progressHandler) println(line string) error {
	_, err := fmt.Fprintln(p.out, line)
	return err
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

// ---------------------------------------------------------------------------
// Color helpers
// ---------------------------------------------------------------------------
//
// We intentionally use raw ANSI escapes rather than pulling in lipgloss —
// the cli firewall keeps charmbracelet deps to charm-log only, and these
// six escape sequences are well-known and trivially testable.
//
// When out is not a TTY (test buffers, file redirects), the helpers
// return the input unchanged → plain ASCII output that's greppable.

const (
	ansiReset      = "\x1b[0m"
	ansiDimCyan    = "\x1b[2;36m"
	ansiBrightCyan = "\x1b[1;36m"
	ansiBrightWhite = "\x1b[1;37m"
	ansiGreen      = "\x1b[32m"
	ansiRed        = "\x1b[31m"
	ansiYellow     = "\x1b[33m"
)

// isTTY memoizes the term.IsTerminal check on p.out. When p.out is a
// *os.File whose fd is a terminal, color is enabled; otherwise (bytes
// buffer, pipe, redirect, non-file Writer) ASCII fallback applies.
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

func (p *progressHandler) wrap(s, color string) string {
	if !p.isTTY() {
		return s
	}
	return color + s + ansiReset
}

func (p *progressHandler) colorBanner(s string) string  { return p.wrap(s, ansiDimCyan) }
func (p *progressHandler) colorCounter(s string) string { return p.wrap(s, ansiBrightCyan) }
func (p *progressHandler) colorKind(s string) string    { return p.wrap(s, ansiBrightWhite) }
func (p *progressHandler) colorOk(s string) string      { return p.wrap(s, ansiGreen) }
func (p *progressHandler) colorErr(s string) string     { return p.wrap(s, ansiRed) }
func (p *progressHandler) colorArrow(s string) string   { return p.wrap(s, ansiYellow) }
