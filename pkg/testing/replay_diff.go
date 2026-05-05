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
// testCallsite is the caller-supplied position; the Divergence will
// surface it under "test callsite:" in the formatted message.
func FirstDivergentEvent(run1, run2 *interpreter.EventCapture, testCallsite syntax.Position) *Divergence {
	if run1 == nil || run2 == nil {
		return nil
	}
	snap1 := run1.Snapshot()
	snap2 := run2.Snapshot()

	n := len(snap1)
	if len(snap2) < n {
		n = len(snap2)
	}

	for i := 0; i < n; i++ {
		a := serializeRecordForDiff(snap1[i])
		b := serializeRecordForDiff(snap2[i])
		if a != b {
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
