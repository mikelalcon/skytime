package worker

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nexus-rpc/sdk-go/nexus"
	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	sdkworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/interpreter"
)

// fakeSDKWorker is a stub sdkworker.Worker that records register calls and
// counts Stop invocations. The compile-time assertion below ensures it
// continues to satisfy the interface as the SDK evolves.
type fakeSDKWorker struct {
	registeredWorkflows  []string // Names from RegisterWorkflowWithOptions
	registeredActivities []string // Names from RegisterActivityWithOptions
	started              bool
	stopCount            int
	panicOnSecondStop    bool
}

func (w *fakeSDKWorker) RegisterWorkflow(_ interface{})                                           {}
func (w *fakeSDKWorker) RegisterDynamicWorkflow(_ interface{}, _ workflow.DynamicRegisterOptions) {}
func (w *fakeSDKWorker) RegisterActivity(_ interface{})                                           {}
func (w *fakeSDKWorker) RegisterDynamicActivity(_ interface{}, _ sdkactivity.DynamicRegisterOptions) {
}
func (w *fakeSDKWorker) RegisterNexusService(_ *nexus.Service) {}

func (w *fakeSDKWorker) RegisterWorkflowWithOptions(_ interface{}, opts workflow.RegisterOptions) {
	w.registeredWorkflows = append(w.registeredWorkflows, opts.Name)
}

func (w *fakeSDKWorker) RegisterActivityWithOptions(_ interface{}, opts sdkactivity.RegisterOptions) {
	w.registeredActivities = append(w.registeredActivities, opts.Name)
}

func (w *fakeSDKWorker) Start() error                          { w.started = true; return nil }
func (w *fakeSDKWorker) Run(_ <-chan interface{}) error        { w.started = true; return nil }
func (w *fakeSDKWorker) Stop() {
	w.stopCount++
	if w.panicOnSecondStop && w.stopCount > 1 {
		panic("Stop called twice — sync.Once should have prevented this")
	}
}

// Compile-time type assertion. If sdkworker.Worker grows new methods, this
// line FAILS AT BUILD TIME — the fake stub set must be updated to match.
var _ sdkworker.Worker = (*fakeSDKWorker)(nil)

// withFakeSDKWorker swaps sdkWorkerNew for the duration of the test, returning
// the captured fake + options + cleanup.
func withFakeSDKWorker(t *testing.T) (*fakeSDKWorker, *sdkworker.Options, *string, func()) {
	t.Helper()
	fake := &fakeSDKWorker{}
	capturedOpts := &sdkworker.Options{}
	capturedTaskQueue := new(string)
	orig := sdkWorkerNew
	sdkWorkerNew = func(_ client.Client, taskQueue string, opts sdkworker.Options) sdkworker.Worker {
		*capturedOpts = opts
		*capturedTaskQueue = taskQueue
		return fake
	}
	return fake, capturedOpts, capturedTaskQueue, func() { sdkWorkerNew = orig }
}

// makeFlowsDir returns a tempdir containing a single trivial .star file.
func makeFlowsDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "trivial.star"), []byte(trivialFlowSrc), 0644))
	return dir
}

// ---------------------------------------------------------------------------
// NewWorker — registration + options threading
// ---------------------------------------------------------------------------

func TestNewWorker_RegistersWorkflowAndActivity(t *testing.T) {
	fake, _, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	dir := makeFlowsDir(t)
	w, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		CredentialHandler: noopHandler{},
	})
	require.NoError(t, err)
	require.NotNil(t, w)

	assert.Equal(t, []string{"SkytimeWorkflow"}, fake.registeredWorkflows,
		"NewWorker must register exactly one workflow named 'SkytimeWorkflow'")
	assert.Equal(t, []string{"ExecuteBatch"}, fake.registeredActivities,
		"NewWorker must register exactly one activity named 'ExecuteBatch'")
}

func TestNewWorker_PassesBuildIDToSDK(t *testing.T) {
	_, capturedOpts, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	dir := makeFlowsDir(t)
	_, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		BuildID:           "v42",
		CredentialHandler: noopHandler{},
	})
	require.NoError(t, err)
	assert.Equal(t, "v42", capturedOpts.BuildID)
	assert.False(t, capturedOpts.UseBuildIDForVersioning,
		"UseBuildIDForVersioning is opt-in at the SDK boundary too — setting BuildID alone must NOT auto-enable versioning")
}

func TestNewWorker_OptInVersioningPropagatesToSDK(t *testing.T) {
	_, capturedOpts, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	dir := makeFlowsDir(t)
	_, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:              dir,
		BuildID:              "v42",
		UseBuildIDVersioning: true,
		CredentialHandler:    noopHandler{},
	})
	require.NoError(t, err)
	assert.Equal(t, "v42", capturedOpts.BuildID)
	assert.True(t, capturedOpts.UseBuildIDForVersioning,
		"explicit UseBuildIDVersioning=true must be propagated to sdkworker.Options.UseBuildIDForVersioning")
}

func TestNewWorker_PassesTaskQueueToSDK(t *testing.T) {
	_, _, capturedTaskQueue, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	dir := makeFlowsDir(t)
	_, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		TaskQueue:         "critical",
		CredentialHandler: noopHandler{},
	})
	require.NoError(t, err)
	assert.Equal(t, "critical", *capturedTaskQueue)
}

func TestNewWorker_DefaultBuildIDFallback(t *testing.T) {
	_, capturedOpts, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	dir := makeFlowsDir(t)
	_, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		CredentialHandler: noopHandler{},
	})
	require.NoError(t, err)
	assert.Equal(t, "dev", capturedOpts.BuildID, "default BuildID must be 'dev'")
}

func TestNewWorker_DefaultTaskQueueFallback(t *testing.T) {
	_, _, capturedTaskQueue, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	dir := makeFlowsDir(t)
	_, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		CredentialHandler: noopHandler{},
	})
	require.NoError(t, err)
	assert.Equal(t, "skytime", *capturedTaskQueue, "default TaskQueue must be 'skytime'")
}

func TestNewWorker_RootDirRequired(t *testing.T) {
	_, _, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	_, err := NewWorker(&fakeClient{}, WorkerOptions{CredentialHandler: noopHandler{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RootDir")
}

func TestNewWorker_CredentialHandlerRequired(t *testing.T) {
	_, _, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	_, err := NewWorker(&fakeClient{}, WorkerOptions{RootDir: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CredentialHandler")
}

// ---------------------------------------------------------------------------
// Worker lifecycle — Start non-blocking, Stop idempotent
// ---------------------------------------------------------------------------

func TestWorker_StartReturnsImmediately(t *testing.T) {
	fake, _, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	dir := makeFlowsDir(t)
	w, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		CredentialHandler: noopHandler{},
	})
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		require.NoError(t, w.Start())
		close(done)
	}()
	select {
	case <-done:
		// expected: returned within deadline
		assert.True(t, fake.started)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2s — must be non-blocking (D3-18)")
	}
}

func TestWorker_StopIsIdempotent(t *testing.T) {
	fake, _, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()
	fake.panicOnSecondStop = true

	dir := makeFlowsDir(t)
	w, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		CredentialHandler: noopHandler{},
	})
	require.NoError(t, err)

	require.NotPanics(t, func() {
		w.Stop()
		w.Stop()
		w.Stop()
	}, "Stop must be sync.Once-wrapped (RESEARCH §Pitfall 5)")

	assert.Equal(t, 1, fake.stopCount, "underlying SDK Stop must be called exactly once")
}

// ---------------------------------------------------------------------------
// Registry accessor
// ---------------------------------------------------------------------------

func TestWorker_RegistryAccessor(t *testing.T) {
	_, _, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	dir := makeFlowsDir(t)
	w, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		CredentialHandler: noopHandler{},
	})
	require.NoError(t, err)
	require.NotNil(t, w.Registry())

	hash, ok := w.Registry().ContentHashFor("trivial")
	require.True(t, ok)
	assert.NotEmpty(t, hash)
}

// =============================================================================
// Phase 7 Plan 04 — Worker.Triggers() + WorkerStopTimeout threading
// =============================================================================

// makeFlowsDirWithTrigger writes a single .star file declaring one flow and
// one trigger. Used by Worker.Triggers() integration tests below.
func makeFlowsDirWithTrigger(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "flows.star"), []byte(`
flow(name = "check_user", steps = [])
trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
)
`), 0644))
	return dir
}

// TestNewWorker_RegistersTriggers proves Worker.Triggers() exposes the
// trigger registry built by bootRegistry.
func TestNewWorker_RegistersTriggers(t *testing.T) {
	_, _, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	dir := makeFlowsDirWithTrigger(t)
	w, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		Extensions:        []extension.Extension{fakeWebhookExt{}},
		CredentialHandler: noopHandler{},
	})
	require.NoError(t, err)
	require.NotNil(t, w.Triggers())
	assert.Len(t, w.Triggers().All(), 1, "trigger from flows.star registered")
	assert.NotNil(t, w.Registry(), "flow registry preserved")
}

// TestNewWorker_WorkerStopTimeoutDefault proves applyDefaults supplies
// 30s when WorkerStopTimeout is zero, and that the default flows into
// sdkworker.Options.
func TestNewWorker_WorkerStopTimeoutDefault(t *testing.T) {
	_, capturedOpts, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	dir := makeFlowsDir(t)
	_, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		CredentialHandler: noopHandler{},
		// WorkerStopTimeout intentionally zero — applyDefaults supplies 30s.
	})
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, capturedOpts.WorkerStopTimeout,
		"default WorkerStopTimeout (30s) must propagate to sdkworker.Options")
}

// TestNewWorker_WorkerStopTimeoutCustom proves an explicit
// WorkerStopTimeout flows through to sdkworker.Options.
func TestNewWorker_WorkerStopTimeoutCustom(t *testing.T) {
	_, capturedOpts, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	dir := makeFlowsDir(t)
	_, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		CredentialHandler: noopHandler{},
		WorkerStopTimeout: 5 * time.Second,
	})
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, capturedOpts.WorkerStopTimeout,
		"explicit WorkerStopTimeout must propagate to sdkworker.Options")
}

// =============================================================================
// Phase 7 Plan 05 — Worker.FlowNames + NewWorkerForTest
// =============================================================================

// TestWorker_FlowNames: Worker.FlowNames is a sorted-slice pass-through to
// the flow registry. Phase 7 Plan 05's startup banner depends on this
// being deterministic.
func TestWorker_FlowNames(t *testing.T) {
	_, _, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "flows.star"), []byte(`
flow(name = "z", steps = [])
flow(name = "a", steps = [])
flow(name = "m", steps = [])
`), 0o644))

	w, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		CredentialHandler: noopHandler{},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "m", "z"}, w.FlowNames())
}

// TestNewWorkerForTest: NewWorkerForTest builds a Worker from pre-built
// registries WITHOUT running boot. Used by pkg/cli's banner test which
// is a black-box consumer of pkg/worker and cannot reach sdkWorkerNew.
func TestNewWorkerForTest(t *testing.T) {
	flowReg := interpreter.NewRegistry()
	require.NoError(t, flowReg.Register("foo", "h1", &interpreter.ParsedFlow{Flow: &dag.Flow{Name: "foo"}}))
	flowReg.Freeze()
	trigReg := interpreter.NewTriggerRegistry()
	trigReg.Freeze()

	w := NewWorkerForTest(flowReg, trigReg)
	require.NotNil(t, w)
	assert.Equal(t, []string{"foo"}, w.FlowNames())
	assert.Empty(t, w.Triggers().All())
}

// =============================================================================
// Phase 7.1 Plan 05 — WithSDKFactory functional option
// =============================================================================

// TestNewWorker_WithSDKFactoryThreadsThrough: WithSDKFactory injects a fake
// SDK worker constructor for ONE NewWorker call. The captured (client,
// taskQueue, sdkOpts) tuple matches the production sdkWorkerNew(...)
// invocation that would have happened, and the returned *Worker wraps the
// fake worker.
func TestNewWorker_WithSDKFactoryThreadsThrough(t *testing.T) {
	// Do NOT use withFakeSDKWorker here — we want the package-level
	// sdkWorkerNew to remain UNTOUCHED. The Option scope is per-call only.
	var capturedClient client.Client
	var capturedTaskQueue string
	var capturedOpts sdkworker.Options
	fake := &fakeSDKWorker{}
	factory := func(c client.Client, taskQueue string, opts sdkworker.Options) sdkworker.Worker {
		capturedClient = c
		capturedTaskQueue = taskQueue
		capturedOpts = opts
		return fake
	}

	dir := makeFlowsDir(t)
	c := &fakeClient{}
	w, err := NewWorker(c, WorkerOptions{
		RootDir:           dir,
		TaskQueue:         "wsk-tq",
		BuildID:           "wsk-build",
		CredentialHandler: noopHandler{},
		WorkerStopTimeout: 7 * time.Second,
	}, WithSDKFactory(factory))
	require.NoError(t, err)
	require.NotNil(t, w)

	assert.Equal(t, c, capturedClient, "client passed verbatim to factory")
	assert.Equal(t, "wsk-tq", capturedTaskQueue, "task queue passed verbatim to factory")
	assert.Equal(t, "wsk-build", capturedOpts.BuildID)
	assert.Equal(t, 7*time.Second, capturedOpts.WorkerStopTimeout)

	// Fake worker is the underlying SDK worker; registration calls landed
	// on it (one workflow + one activity).
	assert.Equal(t, []string{"SkytimeWorkflow"}, fake.registeredWorkflows)
	assert.Equal(t, []string{"ExecuteBatch"}, fake.registeredActivities)
}

// TestNewWorker_NoOptionUsesDefault: WITHOUT WithSDKFactory, NewWorker
// falls back to the package-level sdkWorkerNew var. Indirectly verified
// by all existing tests; the explicit assertion here pins the contract.
func TestNewWorker_NoOptionUsesDefault(t *testing.T) {
	fake, _, _, cleanup := withFakeSDKWorker(t)
	defer cleanup()

	dir := makeFlowsDir(t)
	w, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		CredentialHandler: noopHandler{},
	})
	require.NoError(t, err)
	require.NotNil(t, w)

	// Default sdkWorkerNew (overridden via withFakeSDKWorker) was used —
	// the fake captured the registration calls.
	assert.Equal(t, []string{"SkytimeWorkflow"}, fake.registeredWorkflows)
	assert.Equal(t, []string{"ExecuteBatch"}, fake.registeredActivities)
}

// TestNewWorker_OptionDoesNotAffectGlobalSdkWorkerNew: a NewWorker call
// WITH WithSDKFactory does NOT mutate the package-level sdkWorkerNew var.
// A subsequent NewWorker call WITHOUT the option uses the original
// sdkWorkerNew. This isolation is what makes the option safe to use in
// parallel test runs.
func TestNewWorker_OptionDoesNotAffectGlobalSdkWorkerNew(t *testing.T) {
	beforePtr := reflect.ValueOf(sdkWorkerNew).Pointer()

	dir := makeFlowsDir(t)

	// First call WITH the option.
	fakeA := &fakeSDKWorker{}
	factory := func(_ client.Client, _ string, _ sdkworker.Options) sdkworker.Worker {
		return fakeA
	}
	_, err := NewWorker(&fakeClient{}, WorkerOptions{
		RootDir:           dir,
		CredentialHandler: noopHandler{},
	}, WithSDKFactory(factory))
	require.NoError(t, err)

	afterPtr := reflect.ValueOf(sdkWorkerNew).Pointer()
	assert.Equal(t, beforePtr, afterPtr,
		"package-level sdkWorkerNew var must NOT be mutated by WithSDKFactory option (per-call scope)")

	// Second call WITHOUT the option uses the original sdkWorkerNew. We
	// can't directly observe the call without overriding sdkWorkerNew,
	// but the pointer-equality assertion above is the load-bearing
	// guarantee — a subsequent NewWorker call would reach the same
	// function pointer.
}
