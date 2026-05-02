package worker

import (
	"fmt"
	"sync"

	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	sdklog "go.temporal.io/sdk/log"
	sdkworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	skyactivity "github.com/mikelalcon/skytime/pkg/activity"
	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/interpreter"
)

// sdkWorkerNew is the test seam — production assigns sdkworker.New; tests
// override to capture worker.Options + register calls without a real SDK
// worker.
var sdkWorkerNew = sdkworker.New

// Worker is the Skytime worker. Wraps the SDK worker with registry boot,
// sync.Once-protected Stop, and a Start that returns immediately (D3-18).
type Worker struct {
	sdk      sdkworker.Worker
	registry *interpreter.FlowRegistry
	opts     WorkerOptions

	stopOnce sync.Once
}

// NewWorker constructs a Skytime worker. Boots the flow registry from
// opts.RootDir, registers SkytimeWorkflow + ExecuteBatch with the SDK
// worker, and returns the *Worker ready to Start.
//
// WORK-01: registers exactly one workflow ("SkytimeWorkflow") + one activity
// ("ExecuteBatch").
func NewWorker(c client.Client, opts WorkerOptions) (*Worker, error) {
	if err := opts.applyDefaults(); err != nil {
		return nil, err
	}

	registry, err := bootRegistry(opts.RootDir, opts.Extensions)
	if err != nil {
		return nil, fmt.Errorf("NewWorker: %w", err)
	}

	// Build the activity. activity.OperationDispatch is a map keyed by
	// "<extName>.<opName>" matching dag.ActionRef.Kind_ verbatim.
	dispatch := buildDispatch(opts.Extensions)
	actOpts := []skyactivity.Option{}
	act, err := skyactivity.New(dispatch, opts.CredentialHandler, actOpts...)
	if err != nil {
		return nil, fmt.Errorf("NewWorker: activity init: %w", err)
	}

	sdkOpts := sdkworker.Options{
		BuildID:                 opts.BuildID,
		UseBuildIDForVersioning: opts.UseBuildIDVersioning,
		Identity:                "skytime/" + opts.BuildID,
	}
	if opts.MaxConcurrentActivities > 0 {
		sdkOpts.MaxConcurrentActivityExecutionSize = opts.MaxConcurrentActivities
	}
	// Quick 260502-guu Fix B: the SDK's worker.Options struct does NOT
	// expose a Logger field (verified against go.temporal.io/sdk@v1.42.0
	// internal/worker.go WorkerOptions). The worker INHERITS the
	// client's Logger (set via client.Options.Logger). Wiring lives in
	// pkg/worker/client.go's three constructors which thread
	// {Cloud,SelfHosted,Dev}ClientOptions.Logger into client.Options.Logger
	// via sdklog.NewStructuredLogger.
	//
	// WorkerOptions.Logger is preserved as a field for API symmetry with
	// the client option structs and as a future seam (the SDK may add
	// worker-level Logger override in a later version), but in v1.42.0
	// it is informational only — opts.Logger is not consumed here.
	_ = sdklog.NewStructuredLogger // import retained for future Logger-on-worker hook
	_ = opts.Logger
	sdkW := sdkWorkerNew(c, opts.TaskQueue, sdkOpts)

	// Register the single interpreter workflow.
	wf := interpreter.NewWorkflow(registry)
	sdkW.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})

	// Register the single ExecuteBatch activity.
	sdkW.RegisterActivityWithOptions(act.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})

	return &Worker{
		sdk:      sdkW,
		registry: registry,
		opts:     opts,
	}, nil
}

// Start begins polling for tasks. Non-blocking (D3-18). Caller is responsible
// for lifecycle (signal handling, graceful drain).
func (w *Worker) Start() error { return w.sdk.Start() }

// Stop shuts down the worker. sync.Once-wrapped to prevent panic on
// double-call (RESEARCH §Pitfall 5; sdkworker.Worker.Stop docs:
// "This may panic if called a second time").
func (w *Worker) Stop() {
	w.stopOnce.Do(w.sdk.Stop)
}

// Registry returns the frozen flow registry. Useful for advanced consumers
// (e.g., Phase 4 CLI's `skytime validate`) and for the WORK-03 library-embed
// integration test, which uses ContentHashFor to build the workflow input.
func (w *Worker) Registry() *interpreter.FlowRegistry { return w.registry }

// buildDispatch flattens an []extension.Extension into the
// activity.OperationDispatch map keyed by "<extName>.<opName>".
func buildDispatch(exts []extension.Extension) skyactivity.OperationDispatch {
	d := skyactivity.OperationDispatch{}
	for _, e := range exts {
		extName := e.Name()
		for opName, spec := range e.Operations() {
			if spec == nil {
				continue
			}
			d[extName+"."+opName] = *spec
		}
	}
	return d
}
