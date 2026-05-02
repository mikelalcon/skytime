package cli

// White-box: package cli so the run-time wiring tests can reach
// connectClientWithFactory + the clientFactory seam + buildSDKSlogLogger
// without exporting them.

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	sdkclient "go.temporal.io/sdk/client"
	sdklog "go.temporal.io/sdk/log"

	"github.com/mikelalcon/skytime/pkg/worker"
)

// TestRun_VerboseFlagWiresSDKLogger — Fix B (quick 260502-guu): the
// --verbose flag must change which slog handler the SDK Logger writes
// through. With --verbose=false the SDK Logger is wrapped around a
// near-silent handler (level=ErrorLevel+1 → INFO records dropped); with
// --verbose=true the SDK Logger writes through the same charm-log
// handler the rest of the CLI uses.
//
// We test the seam directly: buildSDKSlogLogger(cfg) is what run.go
// hands to worker.NewWorker / connect. Asserting on it (rather than on
// the captured WorkerOptions/client.Options) keeps the test scope tight
// and deterministic.
func TestRun_VerboseFlagWiresSDKLogger(t *testing.T) {
	t.Run("verbose=false drops INFO records", func(t *testing.T) {
		var buf bytes.Buffer
		// Build a config whose charm-log-side handler writes to buf.
		// In production setupLogging owns this; the test passes its own
		// wrapped handler so we can observe what gets through.
		wrapped := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
		cfg := &config{
			Verbose:   false,
			logger:    slog.New(wrapped),
			sdkLogger: nil, // populated below
		}
		cfg.sdkLogger = buildSDKSlogLogger(cfg)

		cfg.sdkLogger.Info("plain SDK info")
		cfg.sdkLogger.Warn("plain SDK warn")
		require.Empty(t, buf.String(),
			"verbose=false: SDK INFO/WARN records must be dropped before reaching the wrapped handler (got: %q)", buf.String())
	})

	t.Run("verbose=true allows INFO records through", func(t *testing.T) {
		var buf bytes.Buffer
		wrapped := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
		cfg := &config{
			Verbose: true,
			logger:  slog.New(wrapped),
		}
		cfg.sdkLogger = buildSDKSlogLogger(cfg)

		cfg.sdkLogger.Info("plain SDK info")
		require.Contains(t, buf.String(), "plain SDK info",
			"verbose=true: SDK INFO records must reach the wrapped handler")
	})
}

// TestProgressHandler_WrapsWorkerLogger — Fix B: the routedSlog handed
// to worker.NewWorker MUST be a *progressHandler that intercepts records
// carrying `event` attributes. Without this routing, the interpreter's
// `event=step_dispatch` records would either get dropped (--verbose=false
// → wrapped handler at LevelError+1) or render as raw text (--verbose=true
// → charm-log) instead of as Bazel-style step lines.
//
// The test builds a routedSlog the same way newRunCommand does, then
// fires an event-bearing record through it and asserts the bytes flow
// to the progress writer (Bazel renderer) NOT the wrapped charm-log
// handler.
func TestProgressHandler_WrapsWorkerLogger(t *testing.T) {
	var progressOut, charmOut bytes.Buffer
	wrapped := slog.NewTextHandler(&charmOut, &slog.HandlerOptions{Level: slog.LevelInfo})
	cfg := &config{
		Verbose: true,
		logger:  slog.New(wrapped),
	}
	cfg.sdkLogger = buildSDKSlogLogger(cfg)

	routed := buildRoutedSlogLogger(cfg, &progressOut)
	routed.Info("skytime",
		slog.String("event", "step_dispatch"),
		slog.Int("idx", 1), slog.Int("total", 1),
		slog.String("kind", "step"),
		slog.String("label", "gh.get(/x)"),
		slog.String("path", "1"),
	)

	require.Contains(t, progressOut.String(), "[1/1]",
		"event=step_dispatch must render through the Bazel renderer to the progress writer")
	require.NotContains(t, charmOut.String(), "step_dispatch",
		"Skytime event records must NOT reach the wrapped charm-log handler")
}

// TestSDKLoggerRoundTripPreservesEventAttr — behavior gate: the Temporal
// SDK's NewStructuredLogger is the round-trip layer between
// workflow.GetLogger inside the workflow and our slog handler outside.
// If the adapter concatenates keyvals into the message instead of
// preserving them as slog Attrs, the renderer's `event` discriminator
// breaks. This test pins that the adapter forwards keyvals as Attrs.
func TestSDKLoggerRoundTripPreservesEventAttr(t *testing.T) {
	var sawEvent bool
	captureHandler := captureFn(func(_ context.Context, r slog.Record) error {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "event" && a.Value.String() == "step_dispatch" {
				sawEvent = true
			}
			return true
		})
		return nil
	})
	slogger := slog.New(captureHandler)
	sdkLogger := sdklog.NewStructuredLogger(slogger)

	sdkLogger.Info("msg", "event", "step_dispatch", "idx", 1)
	require.True(t, sawEvent,
		"SDK NewStructuredLogger must preserve `event` as a slog.Attr (not concat into msg)")
}

// TestConnectClient_ThreadsLoggerIntoOptions — Fix B: the chosen
// connection variant (cloud / self-hosted / dev) must thread cfg.sdkLogger
// into the corresponding {Cloud,SelfHosted,Dev}ClientOptions.Logger
// field. Without this, the SDK client's gRPC-side INFO logs bypass the
// progressHandler and surface as raw text.
func TestConnectClient_ThreadsLoggerIntoOptions(t *testing.T) {
	captured := struct {
		dev   *slog.Logger
		cloud *slog.Logger
		self  *slog.Logger
	}{}
	factory := clientFactory{
		NewCloud: func(opts worker.CloudOptions) (sdkclient.Client, error) {
			captured.cloud = opts.Logger
			return fakeTemporalClient{}, nil
		},
		NewSelfHosted: func(opts worker.SelfHostedOptions) (sdkclient.Client, error) {
			captured.self = opts.Logger
			return fakeTemporalClient{}, nil
		},
		NewDev: func(opts worker.DevClientOptions) (sdkclient.Client, error) {
			captured.dev = opts.Logger
			return fakeTemporalClient{}, nil
		},
	}

	cfg := &config{address: "host:7233", namespace: "default"}
	cfg.sdkLogger = slog.New(slog.NewTextHandler(new(bytes.Buffer), nil))
	_, err := connectClientWithFactory(cfg, factory)
	require.NoError(t, err)
	require.NotNil(t, captured.dev,
		"dev client constructor must receive cfg.sdkLogger via DevClientOptions.Logger")
	require.Same(t, cfg.sdkLogger, captured.dev,
		"DevClientOptions.Logger must be the cfg.sdkLogger value")

	cfg.apiKey = "k"
	_, err = connectClientWithFactory(cfg, factory)
	require.NoError(t, err)
	require.Same(t, cfg.sdkLogger, captured.cloud,
		"CloudOptions.Logger must be the cfg.sdkLogger value")
}

// captureFn is a tiny slog.Handler that calls fn for every Handle. Used
// to inspect record attrs without re-implementing the full handler interface.
type captureFn func(context.Context, slog.Record) error

func (c captureFn) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (c captureFn) Handle(ctx context.Context, r slog.Record) error {
	return c(ctx, r)
}
func (c captureFn) WithAttrs(_ []slog.Attr) slog.Handler { return c }
func (c captureFn) WithGroup(_ string) slog.Handler      { return c }
