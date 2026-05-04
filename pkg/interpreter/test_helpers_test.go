package interpreter

// Test helpers for plan 04.2-04 (and later) — parseSrcAsFlow drives a
// real parser session over an in-memory .star source and returns the
// interpreter-level *ParsedFlow. Other helpers in this file (event
// capture via Temporal's log.Logger interface, env wiring) are reused
// across walk_ifcond_expression_test.go and replay_determinism_test.go.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/log"

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
// Event capture infrastructure (used by tests that assert event
// sequences — TestWalkIfCond_ResultBoundToCtx_SlogEvent and the
// replay-determinism tests).
//
// We use the same shape as walk_step_namefn_test.go's captureLogger
// (Temporal's log.Logger interface) since workflow.GetLogger(ctx)
// routes through ts.SetLogger, NOT slog.SetDefault. The records here
// extend that pattern with snapshot()/serialize() seams for byte-equal
// replay comparison.
// ----------------------------------------------------------------------

// capturedRecord is a snapshot of one logger.Info call. Msg holds the
// log message ("skytime"); Attrs is a flat map of resolved
// k1=v1, k2=v2 pairs.
type capturedRecord struct {
	Msg   string
	Attrs map[string]any
}

// eventCapturingLogger implements go.temporal.io/sdk/log.Logger and
// records every Info call. Mutex-guarded so -race accepts concurrent
// emission from Temporal's replay machinery (workflow goroutines may
// invoke logger.Info via deterministic-runner scheduling).
type eventCapturingLogger struct {
	mu      sync.Mutex
	records []capturedRecord
}

// newEventCapturingLogger returns a fresh logger with an empty record list.
func newEventCapturingLogger() *eventCapturingLogger { return &eventCapturingLogger{} }

// appendInfo converts a flat keyvals slice into a capturedRecord and
// stores it under lock.
func (c *eventCapturingLogger) appendInfo(msg string, kvs []any) {
	attrs := make(map[string]any, len(kvs)/2)
	for k := 0; k+1 < len(kvs); k += 2 {
		ks, ok := kvs[k].(string)
		if !ok {
			continue
		}
		attrs[ks] = kvs[k+1]
	}
	rec := capturedRecord{Msg: msg, Attrs: attrs}
	c.mu.Lock()
	c.records = append(c.records, rec)
	c.mu.Unlock()
}

func (c *eventCapturingLogger) Debug(msg string, kvs ...any) { c.appendInfo(msg, kvs) }
func (c *eventCapturingLogger) Info(msg string, kvs ...any)  { c.appendInfo(msg, kvs) }
func (c *eventCapturingLogger) Warn(msg string, kvs ...any)  { c.appendInfo(msg, kvs) }
func (c *eventCapturingLogger) Error(msg string, kvs ...any) { c.appendInfo(msg, kvs) }

// Compile-time guarantee: *eventCapturingLogger satisfies log.Logger.
var _ log.Logger = (*eventCapturingLogger)(nil)

// snapshot returns a defensive copy of the captured records under lock.
// Tests use the returned slice without holding the logger's lock.
func (c *eventCapturingLogger) snapshot() []capturedRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capturedRecord, len(c.records))
	copy(out, c.records)
	return out
}

// findEventRecords returns the subset of records whose Attrs["event"]
// equals eventName. Used by tests asserting e.g. exactly one
// "result_bound" event was emitted.
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
// Attr keys are sorted alphabetically per record — Go map iteration
// order is randomized, so without the sort, two runs can produce
// identical records but different serialized forms (false-positive
// non-determinism).
//
// Slice values render via reflect-based Sprintf(%v) which is stable.
func serializeRecords(recs []capturedRecord) string {
	var b strings.Builder
	for _, r := range recs {
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
