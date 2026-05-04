package cli

// Wave 0 RED scaffolding for the new D4.2 progress events that the
// renderer will format (plan 05 fills). Two tests pin:
//   - result_bound: emitted when an expression-mode if_cond binds its
//     resolved dict into ctx.<alias>; renderer shows `→ ctx.<alias>`
//     under the branch row
//   - step_complete with kind=fail: emitted when an expression-mode
//     branch fail() raises; renderer shows ✗ + the failure reason
//
// Until plan 05 wires these cases into the static + live renderers,
// the captured output won't contain the expected tokens — RED state.

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestProgressHandler_ResultBound: an `event=result_bound` slog record
// produces output containing `→ ctx.<alias>` and the bound key list.
// RED until plan 05 adds the result_bound case to the renderer.
func TestProgressHandler_ResultBound(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandler(passthrough, &progressOut)
	logger := slog.New(handler)

	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "result_bound"),
		slog.Int("idx", 2),
		slog.String("path", "2"),
		slog.String("alias", "result_dict"),
		slog.String("keys", "sign,magnitude"),
	)

	got := progressOut.String()
	require.Contains(t, got, "→ ctx.result_dict",
		"RED until plan 05 adds result_bound rendering: %q", got)
	require.Contains(t, got, "sign",
		"RED until plan 05: keys list must appear in output; got %q", got)
}

// TestProgressHandler_FailLeaf: an expression-mode branch fail()
// surfaces as `event=step_complete, kind=fail` with status=err. The
// renderer shows ✗ and the failure reason. RED until plan 05 wires
// the kind=fail case (today's renderer hardcodes the kind="step"
// label per quick 260503-qx1).
func TestProgressHandler_FailLeaf(t *testing.T) {
	var progressOut, passOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandler(passthrough, &progressOut)
	logger := slog.New(handler)

	logger.LogAttrs(context.Background(), slog.LevelInfo, "skytime",
		slog.String("event", "step_complete"),
		slog.Int("idx", 1), slog.Int("total", 1),
		slog.String("kind", "fail"),
		slog.String("label", "fail(\"missing repo\")"),
		slog.String("path", "1"),
		slog.String("status", "err"),
		slog.Int64("duration_ms", 5),
		slog.String("summary", "missing repo"),
	)

	got := progressOut.String()
	require.Contains(t, got, "✗",
		"RED until plan 05: fail kind must render with ✗; got %q", got)
	require.Contains(t, got, "missing repo",
		"RED until plan 05: failure reason must appear; got %q", got)
	require.Contains(t, got, "fail",
		"RED until plan 05: kind=fail label must appear; got %q", got)
}
