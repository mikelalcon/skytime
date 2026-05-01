package cli

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSlogProgress_RendersStepEvents verifies D4-06: records carrying
// the "flow_name" attribute are rendered as compact one-liner progress
// lines on the progress writer; records without it pass through to the
// wrapped handler unchanged.
func TestSlogProgress_RendersStepEvents(t *testing.T) {
	var progressOut bytes.Buffer
	var passthroughOut bytes.Buffer

	passthrough := slog.NewTextHandler(&passthroughOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := newProgressHandler(passthrough, &progressOut)
	logger := slog.New(handler)

	// 1. Record WITH flow_name → progress line on progressOut, nothing on passthroughOut
	logger.Info("dispatching",
		slog.String("flow_name", "approve_pr"),
		slog.String("step_kind", "Step"),
		slog.String("action_kind", "github.create_issue"),
		slog.String("pos", "flows/x.star:42"),
	)
	require.Contains(t, progressOut.String(), "[skytime]")
	require.Contains(t, progressOut.String(), "flow=approve_pr")
	require.Contains(t, progressOut.String(), "step=Step")
	require.Contains(t, progressOut.String(), "action=github.create_issue")
	require.Contains(t, progressOut.String(), "at flows/x.star:42")
	require.Contains(t, progressOut.String(), "dispatching")
	require.Empty(t, passthroughOut.String(), "non-Skytime handler must NOT receive Skytime-namespaced records")

	// 2. Record WITHOUT flow_name → passthrough sees it, progress empty
	progressOut.Reset()
	logger.Info("plain SDK log message")
	require.Empty(t, progressOut.String(), "progress writer must not receive non-Skytime records")
	require.Contains(t, passthroughOut.String(), "plain SDK log message")
}

// TestSlogProgress_PassthroughRespectsLevel verifies the wrapped
// handler's Enabled() drives whether records flow through. If the
// wrapped handler is configured at WARN, INFO records on the
// non-Skytime path are dropped.
func TestSlogProgress_PassthroughRespectsLevel(t *testing.T) {
	var progressOut, passthroughOut bytes.Buffer
	passthrough := slog.NewTextHandler(&passthroughOut, &slog.HandlerOptions{Level: slog.LevelWarn})
	handler := newProgressHandler(passthrough, &progressOut)
	logger := slog.New(handler)

	// INFO + no flow_name → wrapped is at WARN → both buffers empty
	logger.Info("hidden by level")
	require.Empty(t, progressOut.String())
	require.Empty(t, passthroughOut.String())

	// WARN + no flow_name → passthrough renders
	logger.Warn("visible warn")
	require.Contains(t, passthroughOut.String(), "visible warn")

	ctx := context.Background()
	require.True(t, handler.Enabled(ctx, slog.LevelWarn))
	require.False(t, handler.Enabled(ctx, slog.LevelInfo))
}
