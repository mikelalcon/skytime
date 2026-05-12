package cli

// Static-path renderers — extracted from pkg/cli/progress.go in
// Phase 04.1-06 Task 1. The methods here produce the Bazel-style
// line-per-event output used by:
//
//   - Non-TTY contexts (file redirect, pipe, CI). Detected by
//     progressHandler.isTTY() returning false (D4.1-21).
//   - --verbose mode, even on TTY. SDK INFO/DEBUG lines stream to stderr
//     and would constantly break the live region's redraw, so verbose
//     forces static line-per-event mode (D4.1-20).
//   - Windows builds. progress_live_windows.go is a no-op stub; on
//     Windows useLiveBlock() returns false, so all output uses these
//     renderers.
//
// The split is purely organizational — every method is still a
// receiver on *progressHandler, no signature changes. Tests assert the
// load-bearing properties (Bazel banner, [N/M] counter, kind label,
// ✓/✗ markers, ms duration, flow_failed renderer for err_count > 0)
// against the same struct instance.

import (
	"fmt"
	"io"
	"strings"
)

// renderFlowStart: `[skytime] flow <flow_name>  <step_count> steps  starting`
//
// Quick 260502-onc Fix C: clears lastErr so a long-lived handler
// doesn't carry the previous run's failure context into the next.
func (p *progressHandler) renderFlowStart(a attrMap) error {
	p.lastErr = nil
	flow := a.str("flow_name")
	count := a.int("step_count")
	line := fmt.Sprintf("%s flow %s  %d steps  starting",
		p.colorBanner("[skytime]"), flow, count)
	return p.println(line)
}

// renderStepDispatch dispatches by kind:
//   - kind == "if_cond": SUPPRESSED (D-RHY-01). The total attr is cached
//     so renderBranch can render the [N/M] counter on the header line.
//   - kind == "for_each_parallel": emit HEADER row (D-RHY-02 + D-RHY-11)
//     `<indent>[N/M] for_each_parallel  items=K  ▶ open`.
//   - leaf kinds (step / script / call_flow): existing dispatch shape
//     `<indent>[N/M] kind  label`.
//
// Indent is depth-based via pathDepth (4 spaces per level, D-RHY-07).
// computeCounter returns indent+counter; counter format still uses
// "[path/path]" for nested rows (D-RHY-13 leaves the counter format
// unchanged from qkk; only indent calculation moved to pathDepth).
func (p *progressHandler) renderStepDispatch(a attrMap) error {
	idx := a.int("idx")
	total := a.int("total")
	kind := a.str("kind")
	label := a.str("label")
	path := a.str("path")

	if kind == "if_cond" {
		// Suppressed (D-RHY-01) — header is delegated to renderBranch.
		// Cache total so renderBranch can emit [N/M] (the branch event
		// itself does NOT carry total — verified in walk_ifcond.go).
		if p.ifCondTotalByIdx == nil {
			p.ifCondTotalByIdx = make(map[int64]int64)
		}
		p.ifCondTotalByIdx[idx] = total
		return nil
	}

	// Phase 7.2.1 D-7.2.1-13: SUPPRESS kind=log step_dispatch. Log steps
	// are side-channels — the user-message record (msg "[skytime/log] ...",
	// no `event` attr) carries the payload via the wrapped charm-log
	// passthrough in progressHandler.Handle; the bookend dispatch/complete
	// frames are noise in human mode. JSON-log mode emits all three records
	// (the filter is only wired into the non-JSON server path via
	// setupServerLogging).
	if kind == "log" {
		return nil
	}

	indent, counter := p.computeCounter(idx, total, path)

	if kind == "for_each_parallel" {
		// D-RHY-02 + D-RHY-11 + D-RHY-12: header form
		// "<indent>[N/M] for_each_parallel  items=K  ▶ open".
		line := fmt.Sprintf("%s%s %s %s %s open",
			indent,
			p.colorCounter(counter),
			p.colorKind(padKind(kind)),
			label,
			p.colorArrow("▶"),
		)
		return p.println(line)
	}

	// Leaf kinds — existing dispatch shape.
	line := fmt.Sprintf("%s%s %s %s",
		indent,
		p.colorCounter(counter),
		p.colorKind(padKind(kind)),
		label,
	)
	return p.println(line)
}

// renderStepComplete: emits a row matching the dispatch column shape.
//
// For kind ∈ {if_cond, for_each_parallel} this is the scope FOOTER
// (D-RHY-04) — counter + kind + label + ✓/✗ + ms + summary, with NO
// branch suffix. Branch was already consumed by renderBranch (header).
//
// For leaf kinds {step, script, call_flow} this is the step's
// completion row — same shape, also no branch suffix (no leaf kind
// emits branch events today; D-RHY-05).
//
// Indent is depth-based via pathDepth (4 spaces per level, D-RHY-07).
func (p *progressHandler) renderStepComplete(a attrMap) error {
	status := a.str("status")
	dur := a.int("duration_ms")
	summary := a.str("summary")
	path := a.str("path")
	idx := a.int("idx")
	total := a.int("total")
	kind := a.str("kind")
	label := a.str("label")

	// Phase 7.2.1 D-7.2.1-13: SUPPRESS kind=log step_complete. Early-return
	// BEFORE the lastErr failureContext capture so any future log-step
	// errors (NonRetryableErr-driven validation failures from walk_log.go —
	// reserved keys / >32 attrs / bad shape) do NOT shadow the actual
	// workflow-failing step that renderFlowComplete attributes when
	// err_count > 0. The error itself surfaces via the activity-level
	// error path, not via the log step's complete frame.
	if kind == "log" {
		return nil
	}

	// Quick 260502-onc Fix C: capture failure context for
	// renderFlowComplete to attribute the failure when err_count > 0.
	if status == "err" {
		p.lastErr = &failureContext{
			idx:     idx,
			total:   total,
			summary: summary,
		}
	}

	indent, counter := p.computeCounter(idx, total, path)

	marker := p.colorOk("✓")
	if status == "err" {
		marker = p.colorErr("✗")
	}
	line := fmt.Sprintf("%s%s %s  %s  %s %dms  %s",
		indent,
		p.colorCounter(counter),
		p.colorKind(padKind(kind)),
		label,
		marker, dur, summary,
	)
	return p.println(line)
}

// renderBranch emits the if_cond scope HEADER (D-RHY-03 + D-RHY-10):
//
//	<indent>[N/M] if_cond  cond  ▶ <branch>
//
// Total comes from ifCondTotalByIdx (cached by the suppressed
// step_dispatch); the branch event itself doesn't carry total.
//
// kind=if_cond is hardcoded (only walk_ifcond emits branch events
// today). label "cond" is also literal — branch events don't carry
// "label", and walk_ifcond's dispatch always uses "cond" (verified in
// walk_ifcond.go).
//
// Empty-branch events are defensively a no-op.
func (p *progressHandler) renderBranch(a attrMap) error {
	branch := a.str("branch")
	if branch == "" {
		return nil // defensive: empty-branch event is a no-op
	}
	idx := a.int("idx")
	path := a.str("path")
	total := int64(0)
	if p.ifCondTotalByIdx != nil {
		total = p.ifCondTotalByIdx[idx]
		delete(p.ifCondTotalByIdx, idx)
	}
	indent, counter := p.computeCounter(idx, total, path)
	line := fmt.Sprintf("%s%s %s %s %s %s",
		indent,
		p.colorCounter(counter),
		p.colorKind(padKind("if_cond")),
		"cond",
		p.colorArrow("▶"),
		branch,
	)
	return p.println(line)
}

// renderResultBound emits the leaf row for an expression-mode if_cond
// result-binding (D4.2-15 + D4.2-16). The row appears INSIDE the active
// branch scope, between the branch header (rendered by renderBranch)
// and the if_cond's footer (rendered by renderStepComplete{kind=if_cond}).
//
// Format:
//
//	<indent>✓ → ctx.<alias>
//	<indent>✓ → ctx.<alias>  keys=[a b c]   // --verbose mode only
//
// Indent is depth-based via pathDepth (4 spaces per level, D-RHY-07).
// The path attr identifies the active branch path (e.g., "0a" for the
// then-branch of the top-level if_cond).
//
// Color: ✓ in green when TTY (via colorOk); the `→` arrow and alias are
// uncolored — visual emphasis is the alias name, and uncolored alias
// stays greppable for tooling.
func (p *progressHandler) renderResultBound(a attrMap) error {
	alias := a.str("alias")
	path := a.str("path")
	keys := a.strSlice("keys")

	indent := strings.Repeat(" ", 4*pathDepth(path))
	marker := p.colorOk("✓")

	line := fmt.Sprintf("%s%s → ctx.%s", indent, marker, alias)
	if p.verbose && len(keys) > 0 {
		line = fmt.Sprintf("%s  keys=%v", line, keys)
	}
	return p.println(line)
}

// renderFlowComplete: success → `[skytime] flow complete  <ok>/<total> steps  total <ms>ms`
//
// Quick 260502-onc Fix C: when err_count > 0, render the failure line
// instead, attributing the most recently captured step failure (or a
// placeholder when the renderer never saw a step_complete-with-err —
// defense against malformed event sequences):
//
//	[skytime] flow failed  step <I>/<M> (<reason>)  total <ms>ms
func (p *progressHandler) renderFlowComplete(a attrMap) error {
	ok := a.int("ok_count")
	errc := a.int("err_count")
	totalMs := a.int("total_ms")

	if errc > 0 {
		idx := int64(0)
		total := ok + errc
		summary := "(no per-step error captured)"
		if p.lastErr != nil {
			idx = p.lastErr.idx
			if p.lastErr.total > 0 {
				total = p.lastErr.total
			}
			if p.lastErr.summary != "" {
				summary = p.lastErr.summary
			}
		}
		line := fmt.Sprintf("%s flow %s  step %d/%d (%s)  total %dms",
			p.colorBanner("[skytime]"),
			p.colorErr("failed"),
			idx, total, summary, totalMs)
		return p.println(line)
	}

	line := fmt.Sprintf("%s flow complete  %d/%d steps  total %dms",
		p.colorBanner("[skytime]"), ok, ok+errc, totalMs)
	return p.println(line)
}

// computeCounter picks the right counter format and indent for a row.
//
// Indent is depth-based (4 spaces per pathDepth level, D-RHY-07) — the
// renderer uses path nesting to indent rows so children of a scope sit
// 4 spaces under the scope header. Top-level rows (depth 0) have no
// indent.
//
// Counter format choice (unchanged from qkk): nested rows render
// "[path/path]" because idx alone doesn't disambiguate a parallel
// for_each iteration; top-level rows render "[idx/total]".
func (p *progressHandler) computeCounter(idx, total int64, path string) (indent, counter string) {
	indent = strings.Repeat(" ", 4*pathDepth(path))
	if isNestedPath(idx, path) {
		counter = fmt.Sprintf("[%s/%s]", path, path)
	} else {
		counter = fmt.Sprintf("[%d/%d]", idx, total)
	}
	return indent, counter
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

// printlnTo writes line followed by a newline to w. Helper used by both
// the static path (writes to p.out) and the live path (writes to the
// live renderer's serialized goroutine).
func printlnTo(w io.Writer, line string) error {
	_, err := fmt.Fprintln(w, line)
	return err
}

// println writes line followed by a newline to p.out.
func (p *progressHandler) println(line string) error {
	return printlnTo(p.out, line)
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
	ansiReset       = "\x1b[0m"
	ansiDimCyan     = "\x1b[2;36m"
	ansiBrightCyan  = "\x1b[1;36m"
	ansiBrightWhite = "\x1b[1;37m"
	ansiGreen       = "\x1b[32m"
	ansiRed         = "\x1b[31m"
	ansiYellow      = "\x1b[33m"
)

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
