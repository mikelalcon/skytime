package worker

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nexus-rpc/sdk-go/nexus"
	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	sdkworker "go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
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
