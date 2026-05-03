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

	// Quick 260502-onc Fix C: capture failure context for
	// renderFlowComplete to attribute the failure when err_count > 0.
	// step_complete carries idx but the renderer also wants total —
	// best-effort fallback to a.int("total") which the dispatch event
	// for the same step set; missing total falls back to 0 (renderer
	// prints "step 2/0", uglier but never crashes).
	if status == "err" {
		p.lastErr = &failureContext{
			idx:     idx,
			total:   a.int("total"),
			summary: summary,
		}
	}

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

	// Quick 260503-qkk: if a `branch` event for this idx was buffered,
	// read+delete it and append ` <colorArrow(→)> <branch>` to the line.
	// The standalone branch line is no longer emitted by renderBranch.
	if p.branchByIdx != nil {
		if branch, ok := p.branchByIdx[idx]; ok {
			delete(p.branchByIdx, idx)
			if branch != "" {
				line = fmt.Sprintf("%s %s %s", line, p.colorArrow("→"), branch)
			}
		}
	}

	return p.println(line)
}

// renderBranch buffers the branch name keyed by idx; renderStepComplete
// for the same idx reads+deletes the buffer and appends a colored
// ` → <branch>` suffix. Returns nil with NO output. Quick 260503-qkk:
// previously emitted a standalone `     → <branch>` line; that line is
// now inlined onto the if_cond's step_complete row.
func (p *progressHandler) renderBranch(a attrMap) error {
	branch := a.str("branch")
	idx := a.int("idx")
	if branch == "" {
		return nil // defensive: empty-branch event is a no-op
	}
	if p.branchByIdx == nil {
		p.branchByIdx = make(map[int64]string)
	}
	p.branchByIdx[idx] = branch
	return nil
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
