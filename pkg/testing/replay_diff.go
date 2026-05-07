package testing

// Plan 03 Task 2 — replay-determinism divergence reporter. Implements
// D5-D2 (first-divergent-event report with payload before/after) and
// D5-D3 (flow-callsite attribution from the originating step's
// step_dispatch event, which Task 0 extended with `pos` + `name` KV
// pairs).
//
// Diff scope (D5-D4): event records line-by-line via the same
// per-record serialization shape EventCapture.Serialize uses. Final
// workflow state is downstream of the event stream — if events are
// byte-equal, state is byte-equal by construction.

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/interpreter"
)

// Divergence is the D5-D2 first-divergent-event report. Returned by
// FirstDivergentEvent; nil when both captures serialize byte-equal.
//
// Index is the 0-indexed offset of the divergent record in run1.
// Kind is the EventRecord.Kind ("slog", "activity_started",
// "activity_completed", or "<structural>" for length mismatches).
// Before / After are the per-record serialized strings for diagnostic
// rendering.
//
// FlowCallsite is the originating step's syntax.Position read from
// the nearest preceding step_dispatch event's `pos` KV (added by Plan
// 03 Task 0); zero-valued for events that occur before any
// step_dispatch.
//
// StepName is the resolved step display name read from the same
// step_dispatch event's `name` KV.
//
// TestCallsite is the caller-supplied position (typically the .star
// file's `tester.run(...)` line); zero-valued when called from a
// non-Starlark unit test.
type Divergence struct {
	Index        int
	Kind         string
	Before       string
	After        string
	FlowCallsite syntax.Position
	StepName     string
	TestCallsite syntax.Position
}

// FirstDivergentEvent compares the serialized event streams of run1
// and run2 record-by-record. Returns the first inequality (or
// structural mismatch when one capture is shorter). Returns nil when
// both captures serialize byte-equal.
//
// SDK-internal DEBUG slog records (e.g. "handleActivityResult: ..."
// emitted by the Temporal Go SDK) are FILTERED before comparison —
// their ActivityID values are inherently non-deterministic for parallel
// branches (for_each_parallel goroutine scheduling) and would surface
// as false-positive divergences. Interpreter-emitted events are
// INFO/WARN/ERROR, so dropping DEBUG records preserves every event
// the harness actually cares about while eliminating SDK observability
// noise. (Phase 6 latent fix: surfaced when issue_triage_test.star's
// for_each_parallel exposed flaky replay-determinism reports.)
//
// testCallsite is the caller-supplied position; the Divergence will
// surface it under "test callsite:" in the formatted message.
func FirstDivergentEvent(run1, run2 *interpreter.EventCapture, testCallsite syntax.Position) *Divergence {
	if run1 == nil || run2 == nil {
		return nil
	}
	snap1 := filterDeterministicEvents(run1.Snapshot())
	snap2 := filterDeterministicEvents(run2.Snapshot())

	n := len(snap1)
	if len(snap2) < n {
		n = len(snap2)
	}

	for i := 0; i < n; i++ {
		a := serializeRecordForDiff(snap1[i])
		b := serializeRecordForDiff(snap2[i])
		if a != b {
			// Sequential mismatch — could still be a parallel-branch
			// reordering. Fall back to multiset equality on the full
			// filtered snapshots: if both runs produced the SAME bag
			// of events (just in different orders), there is no real
			// divergence, only goroutine-scheduling jitter. Real
			// divergences (different events, missing events, payload
			// mismatches) fail multiset equality and surface here.
			if multisetEqual(snap1, snap2) {
				return nil
			}
			d := &Divergence{
				Index:        i,
				Kind:         snap1[i].Kind,
				Before:       a,
				After:        b,
				TestCallsite: testCallsite,
			}
			if pos, name, ok := lookupOriginatingStep(snap1, i); ok {
				d.FlowCallsite = pos
				d.StepName = name
			}
			return d
		}
	}

	if len(snap1) != len(snap2) {
		// Length mismatch + sequential prefix-match → real divergence
		// (one run produced strictly more events than the other).
		// Multiset equality cannot rescue this — the bags differ by
		// definition.
		idx := n
		var before, after string
		if idx < len(snap1) {
			before = serializeRecordForDiff(snap1[idx])
		} else {
			before = "<missing>"
		}
		if idx < len(snap2) {
			after = serializeRecordForDiff(snap2[idx])
		} else {
			after = "<missing>"
		}
		d := &Divergence{
			Index:        idx,
			Kind:         "<structural>",
			Before:       before,
			After:        after,
			TestCallsite: testCallsite,
		}
		if pos, name, ok := lookupOriginatingStep(snap1, idx); ok {
			d.FlowCallsite = pos
			d.StepName = name
		}
		return d
	}

	return nil
}

// multisetEqual reports whether snap1 and snap2 contain the SAME bag of
// serialized records (same multiplicity per record). Used as a fallback
// when sequential comparison flags a difference but the underlying
// cause is parallel-branch reordering — siblings in a for_each_parallel
// can complete in different orders across two independent runs of the
// always-on D5-D1 replay even though the workflow is correct.
//
// True multisets matter: if a record appears 3× in run1 and 2× in run2,
// the runs are NOT equal even if the unique sets match. We use a
// map[string]int and check that every key has the same count.
func multisetEqual(snap1, snap2 []interpreter.EventRecord) bool {
	if len(snap1) != len(snap2) {
		return false
	}
	counts := make(map[string]int, len(snap1))
	for _, r := range snap1 {
		counts[serializeRecordForDiff(r)]++
	}
	for _, r := range snap2 {
		key := serializeRecordForDiff(r)
		counts[key]--
		if counts[key] < 0 {
			return false
		}
	}
	for _, c := range counts {
		if c != 0 {
			return false
		}
	}
	return true
}

// filterDeterministicEvents drops EventRecords that carry inherent
// non-determinism unrelated to interpreter behavior, then canonicalizes
// the order of parallel-branch events so siblings finishing in
// different orders across two runs do not surface as false divergences.
//
// Filters:
//
//   - slog records at DEBUG level — Temporal SDK internal observability
//     (handleActivityResult, ActivityID assignments, etc.). The
//     interpreter never emits DEBUG; every interpreter event is INFO
//     or higher (verified via `grep 'logger\.' pkg/interpreter/*.go`).
//
// Canonicalizes:
//
//   - Consecutive interpreter slog records emitted from sibling branches
//     of a for_each_parallel block are stable-sorted by their `path` KV.
//     The path attribute embeds the branch index (parent `3` → branches
//     `3.0`, `3.1`, ...); without this canonicalization, goroutine
//     scheduling decides which branch's events appear first across two
//     independent runs. The harness runs are NOT actual Temporal replays
//     — they are two independent TestWorkflowEnvironment executions
//     (D5-D1 always-on replay), so sibling completion order is genuine
//     non-determinism that the divergence detector should ignore.
//
// Kept as-is:
//   - slog INFO/WARN/ERROR (interpreter events: skytime, step_dispatch,
//     branch, flow_complete, ...).
//   - activity_started / activity_completed records — these surface
//     the actual batch payloads + result shapes the harness compares.
//
// Returns a fresh slice so the caller's snapshot is not mutated.
func filterDeterministicEvents(snap []interpreter.EventRecord) []interpreter.EventRecord {
	out := make([]interpreter.EventRecord, 0, len(snap))
	for _, r := range snap {
		if r.Kind == "slog" && r.Level == "DEBUG" {
			continue
		}
		out = append(out, r)
	}
	return canonicalizeParallelOrder(out)
}

// canonicalizeParallelOrder finds windows of consecutive interpreter
// slog records whose `path` attribute shares the same parent prefix
// (e.g. "3.0.x" and "3.1.x" both descend from "3") and stable-sorts
// the window by full path string. This eliminates false divergences
// from parallel-branch goroutine scheduling without altering the order
// of unambiguously-sequential events.
//
// Detection: a "parallel window" is a run of consecutive records whose
// paths are at depth >= 2 (i.e. inside at least one parent) and whose
// FIRST n-1 path components match exactly across all records in the
// run. Records without a `path` KV (e.g. flow_start/flow_complete)
// terminate any window.
func canonicalizeParallelOrder(snap []interpreter.EventRecord) []interpreter.EventRecord {
	if len(snap) <= 1 {
		return snap
	}
	out := make([]interpreter.EventRecord, len(snap))
	copy(out, snap)

	// Walk the slice; whenever we find a window that meets the
	// parallel-sibling criterion, sort it in place.
	i := 0
	for i < len(out) {
		path, ok := extractPathFromSlog(out[i])
		if !ok {
			i++
			continue
		}
		// Find the longest run starting at i where all records share
		// the parent prefix of path (path[:lastDot]) AND have at least
		// one trailing component beyond that prefix.
		parent := parentOfPath(path)
		j := i + 1
		for j < len(out) {
			p, ok := extractPathFromSlog(out[j])
			if !ok {
				break
			}
			if parentOfPath(p) != parent {
				break
			}
			j++
		}
		if j-i > 1 && parent != "" {
			// Sort the [i, j) window stable-by-path.
			window := out[i:j]
			sort.SliceStable(window, func(a, b int) bool {
				pa, _ := extractPathFromSlog(window[a])
				pb, _ := extractPathFromSlog(window[b])
				return pa < pb
			})
		}
		i = j
	}
	return out
}

// extractPathFromSlog returns the `path` KV value (string) from a slog
// EventRecord. Returns ("", false) for non-slog records or when no
// `path` KV is present.
func extractPathFromSlog(r interpreter.EventRecord) (string, bool) {
	if r.Kind != "slog" {
		return "", false
	}
	for k := 0; k+1 < len(r.KV); k += 2 {
		ks, ok := r.KV[k].(string)
		if !ok {
			continue
		}
		if ks != "path" {
			continue
		}
		if vs, ok := r.KV[k+1].(string); ok {
			return vs, true
		}
		return fmt.Sprintf("%v", r.KV[k+1]), true
	}
	return "", false
}

// parentOfPath returns everything up to (and including) the last dot
// in path. Returns "" when path has no dots (depth 1) — those records
// are not parallel siblings of anything.
func parentOfPath(path string) string {
	idx := strings.LastIndex(path, ".")
	if idx < 0 {
		return ""
	}
	return path[:idx+1]
}

// serializeRecordForDiff renders one EventRecord deterministically
// for line-by-line equality comparison. Mirrors the shape used by
// interpreter.EventCapture.Serialize so the diff granularity is
// identical (one EventRecord per diff "line").
func serializeRecordForDiff(r interpreter.EventRecord) string {
	var b strings.Builder
	b.WriteString(r.Kind)
	b.WriteByte('|')
	b.WriteString(r.Level)
	b.WriteByte('|')
	b.WriteString(r.Message)
	if len(r.KV) > 0 {
		b.WriteByte('|')
		// Sort KV by key for stability — Go map iteration is
		// randomized; sorting eliminates a class of false-positive
		// divergences. We don't use a map here because record-level
		// duplicate keys (rare; defensive) would collapse silently.
		pairs := make([]string, 0, len(r.KV)/2)
		for i := 0; i+1 < len(r.KV); i += 2 {
			ks, ok := r.KV[i].(string)
			if !ok {
				continue
			}
			pairs = append(pairs, fmt.Sprintf("%s=%v", ks, r.KV[i+1]))
		}
		sort.Strings(pairs)
		b.WriteString(strings.Join(pairs, ","))
	}
	if len(r.Args) > 0 {
		b.WriteByte('|')
		for i, ref := range r.Args {
			if i > 0 {
				b.WriteByte(',')
			}
			if ref == nil {
				b.WriteString("<nil>")
				continue
			}
			b.WriteString(ref.Kind_)
			b.WriteByte('(')
			if ref.Kwargs != nil {
				fmt.Fprintf(&b, "%v", ref.Kwargs)
			}
			b.WriteByte(')')
		}
	}
	if r.Err != "" {
		b.WriteString("|err=")
		b.WriteString(r.Err)
	}
	return b.String()
}

// lookupOriginatingStep walks backward from idx looking for the
// nearest step_dispatch event, then reads its `pos` and `name`
// attributes (Plan 03 Task 0 added both). Returns (zero, "", false)
// when no preceding step_dispatch exists (e.g., divergence at
// flow_start).
func lookupOriginatingStep(snap []interpreter.EventRecord, idx int) (syntax.Position, string, bool) {
	if idx >= len(snap) {
		idx = len(snap) - 1
	}
	for i := idx; i >= 0; i-- {
		r := snap[i]
		if r.Kind != "slog" {
			continue
		}
		if !kvEquals(r.KV, "event", "step_dispatch") {
			continue
		}
		pos, hasPos := extractPosKV(r.KV, "pos")
		name, _ := extractStringKV(r.KV, "name")
		if hasPos {
			return pos, name, true
		}
	}
	return syntax.Position{}, "", false
}

// kvEquals returns true when kv contains the (key, value) pair.
// The kv slice is the flat keyvals form passed to slog
// Logger.Info(msg, kv...).
func kvEquals(kv []any, key string, value any) bool {
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok || k != key {
			continue
		}
		return kv[i+1] == value
	}
	return false
}

// extractPosKV reads a syntax.Position value from kv under the given
// key. Returns the zero Position + false when the key is missing or
// the value's type is not syntax.Position.
func extractPosKV(kv []any, key string) (syntax.Position, bool) {
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok || k != key {
			continue
		}
		if pos, ok := kv[i+1].(syntax.Position); ok {
			return pos, true
		}
	}
	return syntax.Position{}, false
}

// extractStringKV reads a string value from kv under the given key.
// Returns "" + false when missing or the value's type is not string.
func extractStringKV(kv []any, key string) (string, bool) {
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok || k != key {
			continue
		}
		if s, ok := kv[i+1].(string); ok {
			return s, true
		}
	}
	return "", false
}

// Format renders the D5-D2 multi-line message. Both flow callsite
// and test callsite lines are emitted only when the corresponding
// position is non-zero (Line > 0).
//
// Verbatim shape:
//
//	replay diverged
//	  event N (KIND) diverged:
//	    run1: <serialized>
//	    run2: <serialized>
//	  flow callsite: <flow_file>:<line>:<col> (step "<name>")
//	  test callsite: <test_file>:<line>:<col> (tester.run)
func (d *Divergence) Format() string {
	var buf bytes.Buffer
	fmt.Fprintln(&buf, "replay diverged")
	fmt.Fprintf(&buf, "  event %d (%s) diverged:\n", d.Index, d.Kind)
	fmt.Fprintf(&buf, "    run1: %s\n", d.Before)
	fmt.Fprintf(&buf, "    run2: %s\n", d.After)
	if d.FlowCallsite.Line > 0 {
		fmt.Fprintf(&buf, "  flow callsite: %s (step %q)\n", d.FlowCallsite.String(), d.StepName)
	}
	if d.TestCallsite.Line > 0 {
		fmt.Fprintf(&buf, "  test callsite: %s (tester.run)\n", d.TestCallsite.String())
	}
	return buf.String()
}
