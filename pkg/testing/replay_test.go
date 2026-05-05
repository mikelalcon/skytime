package testing

// Plan 03 Task 2 — pkg/testing.RunOnceCapturing wrapper smoke +
// helperParseProductionFlow concrete implementation. The helper drives
// a real parser session over a .star source string and returns
// *interpreter.ParsedFlow + the content hash; mirrors the pattern used
// by pkg/interpreter/test_helpers_test.go::parseSrcAsFlow and
// tests/differential_test.go.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/interpreter"
	"github.com/mikelalcon/skytime/pkg/parser"
)

// TestReplay_RunOnceCapturing_NoActivities — smoke test that the
// pkg/testing wrapper threads through to interpreter.RunOnceCapturing
// for a script-only flow (no activities; mock callback never fires).
func TestReplay_RunOnceCapturing_NoActivities(t *testing.T) {
	src := `flow(
    name="x",
    inputs={},
    steps=[
        script(id="s", fn=lambda ctx: {"k": "v"}, output_alias="o"),
    ],
)`
	parsed, hash := helperParseProductionFlow(t, src, "x")
	reg := NewMockRegistry()
	cap, out, err := RunOnceCapturing(parsed, hash, map[string]any{}, reg, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.NotEmpty(t, cap.Serialize(),
		"capture buffer must contain at least one slog event")
}

// TestReplay_RunOnceCapturing_TwoRunsByteEqual — D5-D1 always-on
// replay determinism: two consecutive RunOnceCapturing calls with
// identical inputs produce byte-equal serialized event streams.
func TestReplay_RunOnceCapturing_TwoRunsByteEqual(t *testing.T) {
	src := `flow(
    name="x",
    inputs={"n": "int"},
    steps=[
        if_cond(
            output_alias="r",
            cond=lambda ctx: ctx.n > 0,
            then=[result(value={"k": "v", "n": ctx.n})],
            else_=[result(value={"k": "v", "n": ctx.n})],
        ),
    ],
)`
	parsed, hash := helperParseProductionFlow(t, src, "x")
	reg := NewMockRegistry()
	cap1, _, err1 := RunOnceCapturing(parsed, hash, map[string]any{"n": int64(3)}, reg, nil, nil)
	require.NoError(t, err1)
	cap2, _, err2 := RunOnceCapturing(parsed, hash, map[string]any{"n": int64(3)}, reg, nil, nil)
	require.NoError(t, err2)
	assert.Equal(t, cap1.Serialize(), cap2.Serialize(),
		"two consecutive RunOnceCapturing calls must produce byte-equal event streams (D5-D1)")
}

// helperParseProductionFlow drives a real parser session over a
// .star source string and returns *interpreter.ParsedFlow + the
// content hash (sha256-hex of the source bytes). Mirrors the pattern
// in pkg/interpreter/test_helpers_test.go::parseSrcAsFlow and
// tests/differential_test.go's worker-bootstrap convention.
//
// pkg/parser does NOT export a ParsedFlow shape; the interpreter owns
// it. The helper assembles {Flow, Lambdas} from the parser session's
// accessors per the convention used by the interpreter test suite.
func helperParseProductionFlow(t *testing.T, src, flowName string) (*interpreter.ParsedFlow, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.star")
	require.NoError(t, os.WriteFile(path, []byte(src), 0o644))

	p, err := parser.NewParser(parser.WithRoot(dir))
	require.NoError(t, err)

	flows, err := p.ParseFile(path)
	require.NoError(t, err)
	flow, ok := flows[flowName]
	require.True(t, ok, "helperParseProductionFlow: flow %q not found (got %d flows)", flowName, len(flows))

	parsed := &interpreter.ParsedFlow{
		Flow:    flow,
		Lambdas: p.Lambdas(),
	}
	sum := sha256.Sum256([]byte(src))
	return parsed, hex.EncodeToString(sum[:])
}
