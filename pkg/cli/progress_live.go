//go:build !windows

package cli

// Live-block renderer (Phase 04.1-06 Task 2, D4.1-17/18/19).
//
// Architecture:
//   - Single render goroutine + buffered events channel (size 64).
//     The slog Handle() pushes immutable progressEvent values; the
//     goroutine owns ALL writes to r.out. This makes redraw atomic
//     and isolates ANSI escape sequencing — no mutex on Handle().
//
//   - 100 ms ticker drives the spinner cadence (D4.1-18) AND debounces
//     rapid event bursts: events arriving between ticks are batched
//     into one redraw on the next tick or applyEvent.
//
//   - State (active list, drawnLines, spinIdx) is owned exclusively by
//     the render goroutine; Handle() never reads it.
//
//   - ANSI strategy: cursor-up (\x1b[1A) + line-clear (\x1b[2K). NOT
//     alternate-screen-buffer — preserves scrollback per RESEARCH §4.
//
//   - Truncation cap = 10 active rows; "... and N more" line when
//     total > 10 (D4.1-19).
//
//   - On flow_complete OR Close(): clearRedrawRegion + emit final
//     summary as a static line + drain events channel + show cursor.
//     Pitfall 6 — race with final flow output is mitigated by the
//     fact that Close() drains the channel before exiting.

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// braille frames for the spinner — npm/cargo de-facto standard set.
// Locked at 10 frames per D4.1-18.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// activeStep is one in-progress row in the live block's redraw region.
// Created on step_dispatch, removed on step_complete (the completion
// row prints as a static line above the redraw region before removal).
//
// Quick 260503-rhy: only LEAF kinds (step / script / call_flow) get an
// activeStep entry — if_cond and for_each_parallel are scopes whose
// header+footer print as static lines and never appear as in-progress
// rows (D-RHY-08). Path is captured so redraw can indent in-flight
// rows by depth.
type activeStep struct {
	Idx       int64
	Total     int64
	Kind      string
	Label     string
	Path      string
	StartedAt time.Time
}

// liveRenderer owns all writes to its output writer. Slog Handle()
// invocations push events through eventsCh; the single goroutine
// running run() pops, applies, and redraws. The 100 ms ticker drives
// the spinner cadence (D4.1-18) and acts as a debouncer for rapid
// event bursts (Pitfall 7 from RESEARCH).
type liveRenderer struct {
	out    io.Writer
	events chan progressEvent
	closed chan struct{}
	once   sync.Once
	wg     sync.WaitGroup

	// State owned by the render goroutine ONLY.
	active     []*activeStep
	cap        int
	drawnLines int
	spinIdx    int

	// ifCondTotalByIdx caches the parent total from the suppressed
	// step_dispatch event for kind=if_cond. Read+deleted by case "branch"
	// when rendering the [N/M] header counter (the branch event itself
	// does not carry total — verified in walk_ifcond.go). Owned by the
	// render goroutine. Quick 260503-rhy: replaces branchByIdx; scope
	// rendering means renderBranch emits the header inline (no
	// post-buffered consumption from step_complete).
	ifCondTotalByIdx map[int64]int64
}

// newLiveRenderer constructs a live renderer, hides the cursor, and
// spawns the single render goroutine. Caller is responsible for
// invoking Close() when the workflow ends — Close drains pending
// events, clears the redraw region, and restores the cursor.
func newLiveRenderer(out io.Writer) *liveRenderer {
	r := &liveRenderer{
		out:              out,
		events:           make(chan progressEvent, 64),
		closed:           make(chan struct{}),
		cap:              10, // D4.1-19
		ifCondTotalByIdx: make(map[int64]int64),
	}
	// Hide cursor on activation; restored on Close().
	fmt.Fprint(out, "\x1b[?25l")
	r.wg.Add(1)
	go r.run()
	return r
}

// submit ships an event to the render goroutine. Non-blocking when the
// renderer is alive (channel buffer = 64). After Close has been called
// the select picks the closed-channel case (always ready) and the
// event is dropped — protects callers from panicking on a stale
// handler. The closed channel is checked FIRST every poll so a sender
// observing "closed" never reaches the send case (which would panic
// on a closed events channel).
func (r *liveRenderer) submit(ev progressEvent) {
	// Fast path: closed already → drop.
	select {
	case <-r.closed:
		return
	default:
	}
	// Try to send, but bail if Close happens concurrently.
	defer func() {
		// If the events channel was closed between the fast-path check
		// and the send, recover from the panic — the event is dropped,
		// just like the fast path. This is the documented Go idiom for
		// "send on possibly-closed channel" in libraries that prefer
		// not to add an explicit mutex around every submit.
		_ = recover()
	}()
	select {
	case <-r.closed:
		return
	case r.events <- ev:
	}
}

// Close drains pending events, clears the redraw region, restores the
// cursor, and waits for the render goroutine to exit. Idempotent
// (sync.Once-guarded). Safe to call multiple times.
//
// Order matters: signal closed FIRST so concurrent submit() calls
// observe the close and bail out, then close the events channel which
// causes the render goroutine's range-receive to exit after draining.
func (r *liveRenderer) Close() {
	r.once.Do(func() {
		close(r.closed)
		close(r.events)
		r.wg.Wait()
		// Show cursor.
		fmt.Fprint(r.out, "\x1b[?25h")
	})
}

// run is the single render goroutine. Pops events from the channel,
// applies state changes, redraws on every event AND every 100 ms tick.
func (r *liveRenderer) run() {
	defer r.wg.Done()
	tick := time.NewTicker(100 * time.Millisecond) // D4.1-18
	defer tick.Stop()
	for {
		select {
		case ev, ok := <-r.events:
			if !ok {
				r.flushFinal()
				return
			}
			r.applyEvent(ev)
			r.redraw()
		case <-tick.C:
			r.spinIdx = (r.spinIdx + 1) % len(spinnerFrames)
			r.redraw()
		}
	}
}

// applyEvent updates the active list based on event kind. For
// flow_start and flow_complete, applies the printed-line semantics
// (header / footer accumulate above the redraw region as static lines).
func (r *liveRenderer) applyEvent(ev progressEvent) {
	switch ev.Kind {
	case "flow_start":
		// Clear current redraw region (none expected at flow start)
		// and emit the header as a STATIC line above the future
		// redraw region. Color-wrapped per quick 260503-q9p — banner
		// in dim cyan to mirror the static path's colorBanner choice.
		r.clearRedrawRegion()
		fmt.Fprintf(r.out, "%s[skytime]%s flow %s  %d steps  starting\n",
			ansiDimCyan, ansiReset, ev.FlowName, ev.StepCount)
	case "step_dispatch":
		// Quick 260503-rhy: dispatch by kind.
		switch ev.KindAttr {
		case "if_cond":
			// D-RHY-01: header is delegated to case "branch". Capture
			// total so the upcoming branch event can render the [N/M]
			// counter (the branch event itself doesn't carry total —
			// verified in walk_ifcond.go). No active row, no static
			// line, no redraw mutation.
			r.ifCondTotalByIdx[ev.Idx] = ev.Total
		case "for_each_parallel":
			// D-RHY-02 + D-RHY-11: emit HEADER as a static line above
			// the redraw region. Color parity with static path:
			// counter ansiBrightCyan, kind ansiBrightWhite, ▶ ansiYellow.
			r.clearRedrawRegion()
			indent := strings.Repeat(" ", 4*pathDepth(ev.Path))
			fmt.Fprintf(r.out, "%s%s[%d/%d]%s %s%s%s %s %s▶%s open\n",
				indent,
				ansiBrightCyan, ev.Idx, ev.Total, ansiReset,
				ansiBrightWhite, padKind("for_each_parallel"), ansiReset,
				ev.Label,
				ansiYellow, ansiReset)
		default:
			// Leaf kinds — append to the active list (D-RHY-08:
			// scopes never go in active so the redraw region only
			// renders leaves).
			r.active = append(r.active, &activeStep{
				Idx: ev.Idx, Total: ev.Total, Kind: ev.KindAttr, Label: ev.Label,
				Path:      ev.Path,
				StartedAt: time.Now(),
			})
		}
	case "step_complete":
		r.clearRedrawRegion()
		// Quick 260503-rhy: scope kinds emit a FOOTER static line; leaf
		// kinds emit the qx1 completion shape with depth-based indent.
		indent := strings.Repeat(" ", 4*pathDepth(ev.Path))
		marker := ansiGreen + "✓" + ansiReset
		if ev.Status == "err" {
			marker = ansiRed + "✗" + ansiReset
		}
		switch ev.KindAttr {
		case "if_cond", "for_each_parallel":
			// D-RHY-04: scope FOOTER. NO active-list mutation (these
			// never went into active). NO branch suffix (qkk path
			// consumed by case "branch" above as the header).
			fmt.Fprintf(r.out, "%s%s[%d/%d]%s %s%s%s  %s  %s %dms  %s\n",
				indent,
				ansiBrightCyan, ev.Idx, ev.Total, ansiReset,
				ansiBrightWhite, padKind(ev.KindAttr), ansiReset,
				ev.Label,
				marker, ev.DurationMs, ev.Summary)
		default:
			// Leaf kind — find+remove the matching active row, emit
			// completion line with depth-based indent. NO branch
			// suffix (only if_cond ever emitted branch events; that
			// suffix lives on the if_cond header now).
			for i, s := range r.active {
				if s.Idx == ev.Idx {
					r.active = append(r.active[:i], r.active[i+1:]...)
					break
				}
			}
			// Quick 260503-qx1: kind from ev.KindAttr, label between
			// the kind column and the marker. Quick 260503-rhy: indent
			// derived from path depth.
			fmt.Fprintf(r.out, "%s%s[%d/%d]%s %s%s%s  %s  %s %dms  %s\n",
				indent,
				ansiBrightCyan, ev.Idx, ev.Total, ansiReset,
				ansiBrightWhite, padKind(ev.KindAttr), ansiReset,
				ev.Label,
				marker, ev.DurationMs, ev.Summary)
		}
	case "result_bound":
		// Phase 04.2-05 (D4.2-15 + D4.2-16): emit the result-binding leaf
		// as a static line above the redraw region. Mirrors case "branch"
		// — clear the redraw region, write the line, then the next event
		// (typically step_complete{kind=if_cond}) will redraw on top.
		//
		// NO active-list mutation: result_bound is a tail-leaf inside
		// the if_cond scope, not a freestanding leaf step (D-RHY-08:
		// scopes/leaves use the active list; tail-leaves do not).
		//
		// Live path always wraps the ✓ marker in green (TTY is implicit
		// — useLiveBlock is true). Verbose mode is disabled in the live
		// path (useLiveBlock returns false when verbose is true), so we
		// never render the keys list here — keys=[...] is a static-path
		// feature only.
		if ev.Alias == "" {
			return // defensive — empty alias = malformed event
		}
		r.clearRedrawRegion()
		indent := strings.Repeat(" ", 4*pathDepth(ev.Path))
		fmt.Fprintf(r.out, "%s%s✓%s → ctx.%s\n",
			indent,
			ansiGreen, ansiReset,
			ev.Alias,
		)
	case "branch":
		// Quick 260503-rhy: emit if_cond HEADER as a static line above
		// the redraw region (D-RHY-03 + D-RHY-10). Total comes from
		// ifCondTotalByIdx (cached by the suppressed step_dispatch).
		if ev.Branch == "" {
			return
		}
		total := r.ifCondTotalByIdx[ev.Idx]
		delete(r.ifCondTotalByIdx, ev.Idx)
		r.clearRedrawRegion()
		indent := strings.Repeat(" ", 4*pathDepth(ev.Path))
		fmt.Fprintf(r.out, "%s%s[%d/%d]%s %s%s%s %s %s▶%s %s\n",
			indent,
			ansiBrightCyan, ev.Idx, total, ansiReset,
			ansiBrightWhite, padKind("if_cond"), ansiReset,
			"cond",
			ansiYellow, ansiReset, ev.Branch)
	case "flow_complete":
		// Quick 260503-q9p: wrap [skytime] banner in dim cyan; on the
		// failure path also wrap the word "failed" in red — mirrors
		// the static path's colorBanner + colorErr choices.
		r.clearRedrawRegion()
		if ev.ErrCount > 0 {
			fmt.Fprintf(r.out, "%s[skytime]%s flow %sfailed%s  total %dms\n",
				ansiDimCyan, ansiReset, ansiRed, ansiReset, ev.TotalMs)
		} else {
			fmt.Fprintf(r.out, "%s[skytime]%s flow complete  %d/%d steps  total %dms\n",
				ansiDimCyan, ansiReset, ev.OkCount, ev.OkCount+ev.ErrCount, ev.TotalMs)
		}
	case "raw":
		r.clearRedrawRegion()
		fmt.Fprintln(r.out, ev.Raw)
	}
}

// clearRedrawRegion moves the cursor up r.drawnLines and erases each
// line. Resets r.drawnLines to 0. Idempotent — calling on an
// already-empty region is a no-op.
func (r *liveRenderer) clearRedrawRegion() {
	for i := 0; i < r.drawnLines; i++ {
		fmt.Fprint(r.out, "\x1b[1A\x1b[2K")
	}
	r.drawnLines = 0
}

// redraw paints the current active list as the redraw region. Called
// on every tick AND after applyEvent. Idempotent: clearRedrawRegion +
// emit-rows.
func (r *liveRenderer) redraw() {
	r.clearRedrawRegion()
	n := len(r.active)
	if n == 0 {
		return
	}
	visible := n
	if visible > r.cap {
		visible = r.cap
	}
	fmt.Fprintf(r.out, "[skytime] in-progress  %d active\n", n)
	drawn := 1
	for i := 0; i < visible; i++ {
		s := r.active[i]
		elapsed := time.Since(s.StartedAt).Seconds()
		// Truncate label to keep line under typical 80-col width.
		label := s.Label
		if len(label) > 60 {
			label = label[:57] + "..."
		}
		// Quick 260503-rhy: indent in-flight rows by path depth (4
		// spaces per level). The previous fixed 2-space indent is
		// replaced by the depth-based indent so leaf rows inside an
		// if_cond branch or for_each iteration align with their static
		// completion rows.
		indent := strings.Repeat(" ", 4*pathDepth(s.Path))
		fmt.Fprintf(r.out, "%s[%d/%d] %s  %s  %s %.1fs\n",
			indent, s.Idx, s.Total, padKind(s.Kind), label, spinnerFrames[r.spinIdx], elapsed)
		drawn++
	}
	if n > r.cap {
		fmt.Fprintf(r.out, "  ... and %d more\n", n-r.cap)
		drawn++
	}
	r.drawnLines = drawn
}

// flushFinal clears the redraw region one last time on goroutine exit.
// Final summary lines (flow_complete) were already emitted as static
// lines via applyEvent before the channel close arrived; flushFinal
// just removes any in-flight redraw rows that didn't get cleared.
func (r *liveRenderer) flushFinal() {
	r.clearRedrawRegion()
}
