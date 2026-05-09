package receiver

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestRecord_Emit(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	rec := &requestRecord{
		method:          "POST",
		path:            "/webhook/github",
		status:          200,
		start:           time.Now().Add(-5 * time.Millisecond),
		sourceKind:      "github.webhook",
		event:           "issues",
		flowsDispatched: []string{"webhook_demo"},
		workflowIDs:     []string{"webhook_demo/aGcRb2Q8/72c89c70"},
		errorClass:      errorClassOK,
	}
	rec.emit(logger)

	out := buf.Bytes()
	require.NotEmpty(t, out)
	// Multi-line possible if any future change; we expect exactly one line.
	lines := bytes.Split(bytes.TrimRight(out, "\n"), []byte("\n"))
	require.Len(t, lines, 1, "emit must produce exactly one log line per request")

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &parsed))

	assert.Equal(t, "webhook delivery", parsed["msg"])
	assert.Equal(t, "POST", parsed["method"])
	assert.Equal(t, "/webhook/github", parsed["path"])
	assert.EqualValues(t, 200, parsed["status"])
	// duration_ms: JSON numbers decode as float64; expect > 0.
	durRaw, hasDur := parsed["duration_ms"]
	require.True(t, hasDur, "duration_ms must be present")
	durVal, ok := durRaw.(float64)
	require.True(t, ok)
	assert.True(t, durVal >= 0, "duration_ms must be non-negative, got %v", durVal)
	assert.Equal(t, "github.webhook", parsed["source_kind"])
	assert.Equal(t, "issues", parsed["event"])
	assert.Equal(t, errorClassOK, parsed["error_class"])

	flows, ok := parsed["flows_dispatched"].([]any)
	require.True(t, ok, "flows_dispatched must decode to a list")
	assert.Equal(t, []any{"webhook_demo"}, flows)

	wfs, ok := parsed["workflow_ids"].([]any)
	require.True(t, ok, "workflow_ids must decode to a list")
	assert.Equal(t, []any{"webhook_demo/aGcRb2Q8/72c89c70"}, wfs)
}

// TestRequestRecord_NoSecretLeak asserts that a mismatched-signature
// record contains NO accidentally-serialized secret-bearing payload.
// Defense-in-depth: even if a future bug ever stuffed a Secret-bearing
// struct into a field, we'd see the redactedString marker (or worse,
// a raw secret) in the emitted output. Today, the requestRecord struct
// has no string fields that could plausibly carry a Secret — this test
// pins that property.
func TestRequestRecord_NoSecretLeak(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	rec := &requestRecord{
		method:     "POST",
		path:       "/webhook/github",
		status:     401,
		start:      time.Now(),
		sourceKind: "github.webhook",
		errorClass: errorClassSignatureMismatch,
	}
	rec.emit(logger)

	emitted := buf.String()
	assert.NotContains(t, emitted, "<redacted>", "a properly-shaped requestRecord must never carry any value containing the redacted-marker string — its presence would imply a Secret-bearing struct slipped through")
	// Sanity: the error_class is in there.
	assert.Contains(t, emitted, errorClassSignatureMismatch)
	// Sanity: the line really was emitted at level INFO.
	assert.True(t, strings.Contains(emitted, `"level":"INFO"`))
}
