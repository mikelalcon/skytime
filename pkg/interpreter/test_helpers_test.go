package interpreter

// Test helpers for plan 04.2-04 (and later) — parseSrcAsFlow drives a
// real parser session over an in-memory .star source and returns the
// interpreter-level *ParsedFlow. Other helpers in this file (slog
// capture, env wiring) are reused across walk_ifcond_expression_test.go
// and replay_determinism_test.go.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/parser"
)

// parseSrcAsFlow writes src to a temp .star file, runs parser.NewParser
// + ParseFile, and returns the *ParsedFlow keyed by flowName. The
// returned *ParsedFlow is the INTERPRETER's shape (pkg/interpreter
// owns ParsedFlow; pkg/parser does not export a ParsedFlow type — it
// returns map[string]*dag.Flow + Lambdas() + FileBytes() accessors that
// we splice together here, mirroring tests/differential_test.go).
//
// Tests pass a flowName that matches the `flow(name="...", ...)`
// declaration in src.
//
// On any parse error, t.Fatalf via require.NoError surfaces the exact
// dag.ParseError or dag.ValidationError so the test stack trace points
// at the misspelled fixture, not at this helper.
func parseSrcAsFlow(t *testing.T, src, flowName string) *ParsedFlow {
	t.Helper()

	dir := t.TempDir()
	path := filepath.Join(dir, "t.star")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	p, err := parser.NewParser(parser.WithRoot(dir))
	require.NoError(t, err)

	flows, err := p.ParseFile(path)
	require.NoError(t, err)

	flow, ok := flows[flowName]
	require.True(t, ok, "parseSrcAsFlow: flow %q not found in parsed file (got %d flows)", flowName, len(flows))

	// pkg/parser does not export a ParsedFlow shape; the interpreter
	// constructs one from {Flow, Lambdas} per the convention used in
	// tests/differential_test.go.
	return &ParsedFlow{
		Flow:    flow,
		Lambdas: p.Lambdas(),
	}
}

// contentHashForSrc returns the sha256-hex of src — matches what the
// worker bootstrap computes for the parser's FileBytes cache. Used by
// tests to seed registry.Register with a stable hash. Two calls on the
// same source produce the same hash (used in replay tests).
func contentHashForSrc(src string) string {
	h := sha256.Sum256([]byte(src))
	return hex.EncodeToString(h[:])
}

// ----------------------------------------------------------------------
// Slog capture infrastructure (used by tests that assert slog event
// sequences — TestWalkIfCond_ResultBoundToCtx_SlogEvent and the
// replay-determinism tests).
// ----------------------------------------------------------------------

// capturedRecord is a minimal snapshot of a slog.Record's fields that
// matter for replay determinism: level, message, and resolved attrs.
type capturedRecord struct {
	Level slog.Level
	Msg   string
	Attrs map[string]any
}

// capturingHandler is a slog.Handler that appends every record it sees
// to a goroutine-safe slice. Temporal's replay machinery may invoke
// logger.Info from multiple goroutines; the mutex makes -race happy.
type capturingHandler struct {
	mu      sync.Mutex
	records []capturedRecord
}

// newCapturingHandler returns a fresh handler with an empty record list.
func newCapturingHandler() *capturingHandler {
	return &capturingHandler{}
}

// Enabled accepts every level — tests want full visibility.
func (h *capturingHandler) Enabled(context.Context, slog.Level) bool { return true }

// Handle copies the record's level/msg/attrs into the records slice
// under lock. attrs are resolved (LogValuer chains evaluated) so the
// captured value is a stable snapshot, not a lazy reference.
func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make(map[string]any, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Resolve().Any()
		return true
	})
	rec := capturedRecord{Level: r.Level, Msg: r.Message, Attrs: attrs}
	h.mu.Lock()
	h.records = append(h.records, rec)
	h.mu.Unlock()
	return nil
}

// WithAttrs returns the same handler — we don't want to fork into a
// child handler with bound attrs because that would split the record
// stream across handlers and break the replay-equality assertion.
func (h *capturingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }

// WithGroup returns the same handler for the same reason.
func (h *capturingHandler) WithGroup(string) slog.Handler { return h }

// snapshot returns a defensive copy of the captured records under lock.
// Tests use the returned slice without holding the handler's lock.
func (h *capturingHandler) snapshot() []capturedRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]capturedRecord, len(h.records))
	copy(out, h.records)
	return out
}

// findEventRecords returns the subset of records whose Attrs["event"]
// matches eventName (after string conversion). Used by tests asserting
// e.g. exactly one "result_bound" event was emitted.
func findEventRecords(recs []capturedRecord, eventName string) []capturedRecord {
	var out []capturedRecord
	for _, r := range recs {
		if v, ok := r.Attrs["event"]; ok {
			if s, ok2 := v.(string); ok2 && s == eventName {
				out = append(out, r)
			}
		}
	}
	return out
}

// serializeRecords returns a stable string representation of the
// captured records for byte-equal comparison across replay runs. Caller
// passes a snapshot (from .snapshot()) so the iteration is mutex-free.
//
// Attr keys are sorted alphabetically — Go map iteration order is
// randomized, so without the sort, two runs can produce identical
// records but different serialized forms.
func serializeRecords(recs []capturedRecord) string {
	var b strings.Builder
	for _, r := range recs {
		b.WriteString(r.Level.String())
		b.WriteByte(' ')
		b.WriteString(r.Msg)
		keys := make([]string, 0, len(r.Attrs))
		for k := range r.Attrs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, " %s=%v", k, r.Attrs[k])
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// TestParseSrcAsFlow_Smoke verifies the helper produces a non-nil
// ParsedFlow with a populated Flow.Name. Catches regressions in the
// helper itself or the parser's contract (NewParser → ParseFile →
// Flows()/Lambdas()).
func TestParseSrcAsFlow_Smoke(t *testing.T) {
	parsed := parseSrcAsFlow(t, `flow(name="t", inputs={}, steps=[])`, "t")
	require.NotNil(t, parsed)
	require.Equal(t, "t", parsed.Flow.Name)
	require.NotNil(t, parsed.Lambdas, "Lambdas map must be non-nil even when empty")
}
