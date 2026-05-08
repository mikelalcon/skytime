package worker

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mikelalcon/skytime/pkg/extension"
)

const (
	defaultTaskQueue         = "skytime"
	defaultDevHostPort       = "localhost:7233"
	defaultDevNamespace      = "default"
	defaultWorkerStopTimeout = 30 * time.Second
)

// CloudOptions configures a connection to Temporal Cloud (D3-17).
//
// v1.39+ behavior: providing APIKey auto-enables TLS — do NOT set TLS
// alongside it.
//
// Logger (quick 260502-guu Fix B): optional *slog.Logger threaded into
// client.Options.Logger via go.temporal.io/sdk/log.NewStructuredLogger.
// nil → SDK default. The CLI (pkg/cli) constructs a routedSlog whose
// handler is a *progressHandler so SDK lines either render through
// charm-log (--verbose) or get dropped (--verbose=false).
type CloudOptions struct {
	HostPort  string // e.g., "us-west-2.aws.api.temporal.io:7233"
	Namespace string
	APIKey    string // required
	Identity  string // optional; defaults to "skytime/<build_id>" when empty
	Logger    *slog.Logger
}

// SelfHostedOptions configures a connection to a self-hosted Temporal cluster
// with mTLS (D3-17).
//
// Logger: see CloudOptions.Logger.
type SelfHostedOptions struct {
	HostPort   string
	Namespace  string
	ClientCert tls.Certificate // mTLS client identity
	RootCAs    *x509.CertPool  // server cert verification; nil = system pool
	ServerName string          // SNI / verify name
	Identity   string
	Logger     *slog.Logger
}

// DevClientOptions configures a connection to a local dev server (no TLS).
//
// Logger: see CloudOptions.Logger.
type DevClientOptions struct {
	HostPort  string // default "localhost:7233"
	Namespace string // default "default"
	Identity  string
	Logger    *slog.Logger
}

// WorkerOptions configures a Skytime worker.
type WorkerOptions struct {
	// RootDir is the filesystem directory containing .star files (D3-07).
	// Required. The worker walks this directory at boot, parses every .star
	// file, computes content_hash for each, builds the registry, and freezes
	// it.
	RootDir string

	// BuildID is the Temporal Build ID this worker tags itself with (D3-20).
	// Empty → defaults to defaultBuildID ("dev" out of the box).
	BuildID string

	// TaskQueue is the worker's task queue name. Empty → "skytime".
	TaskQueue string

	// UseBuildIDVersioning toggles worker-level Build ID versioning.
	//
	// Opt-in. When true, Temporal pins workflows to compatible Build IDs
	// — the caller must register a Build ID compatibility set on the
	// task queue FIRST via `temporal task-queue
	// update-worker-build-id-compatibility` (or the equivalent SDK
	// call). A versioned worker against a task queue with no
	// compatibility set registered will receive zero task dispatches and
	// workflows will hang.
	//
	// Default false: dev workers and one-shot CLI runs (`skytime run`,
	// `skytime dev-server`) work out of the box. Production long-running
	// workers explicitly set this to true once Build ID sets are
	// configured for the task queue.
	UseBuildIDVersioning bool

	// Extensions is the static extension list for parser registration at
	// boot. Each extension is registered with the parser before any .star
	// file is read.
	Extensions []extension.Extension

	// CredentialHandler is the JIT credential resolver passed to the
	// ExecuteBatch activity (Phase 2). Required.
	CredentialHandler extension.CredentialHandler

	// MaxConcurrentActivities tunes the SDK worker's activity pool. Zero =
	// SDK default.
	MaxConcurrentActivities int

	// Logger (quick 260502-guu Fix B) is optional. When non-nil, the
	// SDK worker's options.Logger is wired to
	// go.temporal.io/sdk/log.NewStructuredLogger(Logger). nil → SDK
	// default (raw stdlib log lines on stderr). The CLI's run subcommand
	// supplies a routedSlog whose handler is a *progressHandler — that
	// is what makes interpreter `event=*` records render as Bazel-style
	// step lines instead of raw log noise.
	Logger *slog.Logger

	// WorkerStopTimeout is the SDK's graceful-stop duration (D-07-16).
	// Worker.Stop() closes the poll channel and blocks up to this duration
	// waiting for in-flight tasks to complete; uncompleted workflow tasks
	// are NOT ACK'd back to the server, so Temporal redispatches them on
	// the next worker start (this IS the durability story). Default 30s
	// applied in applyDefaults; matches Kubernetes terminationGracePeriodSeconds
	// default per D-07-17.
	//
	// Phase 5 / Phase 4 paths (skytime run, embedded transient workers) can
	// leave this zero — applyDefaults supplies 30s, which is a safe upper
	// bound for one-shot workflows. Phase 7's skytime server explicitly
	// sets this from --drain-timeout.
	WorkerStopTimeout time.Duration
}

// applyDefaults fills in defaults on an in-place WorkerOptions copy and
// returns an error if required fields are missing.
func (o *WorkerOptions) applyDefaults() error {
	if o.RootDir == "" {
		return errors.New("WorkerOptions: RootDir is required")
	}
	if o.CredentialHandler == nil {
		return errors.New("WorkerOptions: CredentialHandler is required (use a no-op handler if your flows don't use credentials)")
	}
	if o.BuildID == "" {
		o.BuildID = defaultBuildID
	}
	if o.TaskQueue == "" {
		o.TaskQueue = defaultTaskQueue
	}
	if o.WorkerStopTimeout == 0 {
		o.WorkerStopTimeout = defaultWorkerStopTimeout
	}
	return nil
}

// coalesce returns the first non-empty string in s, or "" if all are empty.
func coalesce(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

func (o CloudOptions) validate() error {
	switch {
	case o.HostPort == "":
		return fmt.Errorf("CloudOptions: HostPort required")
	case o.Namespace == "":
		return fmt.Errorf("CloudOptions: Namespace required")
	case o.APIKey == "":
		return fmt.Errorf("CloudOptions: APIKey required")
	}
	return nil
}

func (o SelfHostedOptions) validate() error {
	switch {
	case o.HostPort == "":
		return fmt.Errorf("SelfHostedOptions: HostPort required")
	case o.Namespace == "":
		return fmt.Errorf("SelfHostedOptions: Namespace required")
	}
	return nil
}
