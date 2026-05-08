package worker

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/interpreter"
)

// =============================================================================
// fakeWebhookExt — duplicate of pkg/parser/trigger_test.go's helper
//
// TODO(phase-7.1): factor fakeWebhookExt to pkg/extension/testing so
// pkg/parser, pkg/worker, and any future consumer share one source. Sub-
// packages of pkg/extension cannot satisfy the unexported triggerSourceMarker
// seal directly, but they CAN re-export the FakeTriggerSource value; a
// thin wrapper around extension.FakeTriggerSource that adds the starlark.Value
// surface lives equally well in any package and is the right home for it.
// =============================================================================

type fakeWebhookExt struct{}

func (fakeWebhookExt) Name() string { return "fake" }

func (fakeWebhookExt) Operations() map[string]*extension.OperationSpec { return nil }

func (fakeWebhookExt) Initialize(thread *starlark.Thread, _ []starlark.Tuple) (starlark.Value, error) {
	webhook := starlark.NewBuiltin("webhook", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var reqFieldsList *starlark.List
		if err := starlark.UnpackArgs("webhook", args, kwargs, "req_fields?", &reqFieldsList); err != nil {
			return nil, err
		}
		var reqFields []string
		if reqFieldsList != nil {
			iter := reqFieldsList.Iterate()
			defer iter.Done()
			var v starlark.Value
			for iter.Next(&v) {
				s, ok := starlark.AsString(v)
				if !ok {
					return nil, fmt.Errorf("req_fields must be list[string]")
				}
				reqFields = append(reqFields, s)
			}
		}
		return &fakeTriggerStarlarkValue{
			FakeTriggerSource: &extension.FakeTriggerSource{
				KindName:  "skytime.test.webhook",
				ReqFields: reqFields,
			},
		}, nil
	})
	return starlarkstruct.FromStringDict(starlark.String("fake"), starlark.StringDict{
		"webhook": webhook,
	}), nil
}

var _ extension.Extension = fakeWebhookExt{}

// fakeTriggerStarlarkValue embeds *extension.FakeTriggerSource so the
// unexported triggerSourceMarker() seal (defined in package extension) is
// satisfied by method promotion. Only the starlark.Value methods need
// declaring on the wrapper.
type fakeTriggerStarlarkValue struct {
	*extension.FakeTriggerSource
}

func (f *fakeTriggerStarlarkValue) String() string        { return "fake.webhook" }
func (f *fakeTriggerStarlarkValue) Type() string          { return "fake.webhook" }
func (f *fakeTriggerStarlarkValue) Freeze()               {}
func (f *fakeTriggerStarlarkValue) Truth() starlark.Bool  { return starlark.True }
func (f *fakeTriggerStarlarkValue) Hash() (uint32, error) { return 0, fmt.Errorf("not hashable") }

var _ starlark.Value = (*fakeTriggerStarlarkValue)(nil)
var _ extension.TriggerSource = (*fakeTriggerStarlarkValue)(nil)

// trivialFlowSrc is a parse-clean .star file with one flow + one inline
// script (no extensions required). Used across boot tests.
const trivialFlowSrc = `flow(
    name="trivial",
    steps=[
        script(id="bump", fn=lambda ctx: {"x": 1}, output_alias="x_plus_one"),
    ],
)
`

const trivialFlowSrc2 = `flow(
    name="trivial2",
    steps=[
        script(id="noop", fn=lambda ctx: {"y": 2}, output_alias="y_doubled"),
    ],
)
`

// writeStarFile writes content to dir/name and returns the absolute path.
func writeStarFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	abs, err := filepath.Abs(path)
	require.NoError(t, err)
	return abs
}

// ---------------------------------------------------------------------------
// bootRegistry
// ---------------------------------------------------------------------------

func TestBootRegistry_ParsesAllStarFiles(t *testing.T) {
	dir := t.TempDir()
	writeStarFile(t, dir, "a.star", trivialFlowSrc)
	writeStarFile(t, dir, "b.star", trivialFlowSrc2)

	reg, _, err := bootRegistry(dir, nil)
	require.NoError(t, err)
	require.NotNil(t, reg)

	// Both flows should be registered with their own content_hash.
	hash1, ok := reg.ContentHashFor("trivial")
	require.True(t, ok, "trivial flow must be registered")
	require.NotEmpty(t, hash1)

	hash2, ok := reg.ContentHashFor("trivial2")
	require.True(t, ok, "trivial2 flow must be registered")
	require.NotEmpty(t, hash2)

	assert.NotEqual(t, hash1, hash2, "different .star files have different content_hashes")
}

func TestBootRegistry_ContentHashIsSha256OfFileBytes(t *testing.T) {
	dir := t.TempDir()
	writeStarFile(t, dir, "a.star", trivialFlowSrc)

	reg, _, err := bootRegistry(dir, nil)
	require.NoError(t, err)

	gotHash, ok := reg.ContentHashFor("trivial")
	require.True(t, ok)

	expectedSum := sha256.Sum256([]byte(trivialFlowSrc))
	expectedHash := hex.EncodeToString(expectedSum[:])
	assert.Equal(t, expectedHash, gotHash, "content_hash must equal sha256(fileBytes)")
}

func TestBootRegistry_RegistryFrozenAfterBoot(t *testing.T) {
	dir := t.TempDir()
	writeStarFile(t, dir, "a.star", trivialFlowSrc)

	reg, _, err := bootRegistry(dir, nil)
	require.NoError(t, err)

	err = reg.Register("anything", "h", &interpreter.ParsedFlow{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, interpreter.ErrRegistryFrozen), "registry must be frozen after boot")
}

func TestBootRegistry_FailsOnUnparseable(t *testing.T) {
	dir := t.TempDir()
	writeStarFile(t, dir, "a.star", trivialFlowSrc)
	writeStarFile(t, dir, "z_broken.star", `this is not valid starlark @@@@@@`)

	_, _, err := bootRegistry(dir, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "z_broken.star", "error must mention the offending file")
}

func TestBootRegistry_EmptyDirReturnsEmptyRegistry(t *testing.T) {
	dir := t.TempDir()
	// No .star files written.

	reg, _, err := bootRegistry(dir, nil)
	require.NoError(t, err)
	require.NotNil(t, reg)

	// Frozen + empty: any Lookup miss + Register fails with ErrRegistryFrozen.
	_, ok := reg.Lookup("nonexistent", "h")
	assert.False(t, ok)
	err = reg.Register("late", "h", &interpreter.ParsedFlow{})
	assert.True(t, errors.Is(err, interpreter.ErrRegistryFrozen))
}

func TestBootRegistry_IgnoresNonStarFiles(t *testing.T) {
	dir := t.TempDir()
	writeStarFile(t, dir, "a.star", trivialFlowSrc)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("docs"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("notes"), 0644))

	reg, _, err := bootRegistry(dir, nil)
	require.NoError(t, err)
	_, ok := reg.ContentHashFor("trivial")
	assert.True(t, ok)
}

func TestBootRegistry_RecursesSubdirectories(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "nested")
	require.NoError(t, os.MkdirAll(sub, 0755))
	writeStarFile(t, sub, "sub.star", trivialFlowSrc)

	reg, _, err := bootRegistry(dir, nil)
	require.NoError(t, err)
	_, ok := reg.ContentHashFor("trivial")
	assert.True(t, ok, "bootRegistry must walk subdirectories")
}

func TestBootRegistry_RootDirRequired(t *testing.T) {
	_, _, err := bootRegistry("", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rootDir")
}

func TestBootRegistry_RootDirMissing(t *testing.T) {
	_, _, err := bootRegistry("/nonexistent/dir/that/should/not/exist", nil)
	require.Error(t, err)
}

// =============================================================================
// Phase 7 Plan 04 — TRIG-05: bootRegistry registers triggers
// =============================================================================

// TestBootRegistry_RegistersTriggers proves bootRegistry walks rootDir and
// registers triggers from production .star files alongside their flows.
func TestBootRegistry_RegistersTriggers(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "flows.star"), []byte(`
flow(name = "check_user", steps = [])
trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
    credential = "github-app",
)
`), 0o644))

	flowReg, trigReg, err := bootRegistry(dir, []extension.Extension{fakeWebhookExt{}})
	require.NoError(t, err)
	require.NotNil(t, flowReg)
	require.NotNil(t, trigReg)

	// Flow side: existing semantics.
	hash, ok := flowReg.ContentHashFor("check_user")
	require.True(t, ok)
	require.NotEmpty(t, hash)

	// Trigger side: TRIG-05 proof.
	trigs := trigReg.All()
	require.Len(t, trigs, 1, "expected exactly 1 trigger from flows.star")
	assert.Equal(t, "check_user", trigs[0].FlowName)
	assert.Equal(t, "skytime.test.webhook", trigs[0].Source.Kind())
	assert.Equal(t, "github-app", trigs[0].CredentialID)
}

// TestBootRegistry_SkipsTestFiles proves the *_test.star skip rule applies
// uniformly to BOTH flows and triggers — neither registers from a test file.
func TestBootRegistry_SkipsTestFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "flows.star"), []byte(`
flow(name = "prod", steps = [])
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "flows_test.star"), []byte(`
flow(name = "testonly", steps = [])
trigger(
    flow = "testonly",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
)
`), 0o644))

	flowReg, trigReg, err := bootRegistry(dir, []extension.Extension{fakeWebhookExt{}})
	require.NoError(t, err, "the *_test.star file is skipped, so its trigger never reaches finalize and the prod flow can't conflict")

	// Production flow is registered.
	_, ok := flowReg.ContentHashFor("prod")
	assert.True(t, ok)
	// Test-only flow is NOT registered.
	_, ok = flowReg.ContentHashFor("testonly")
	assert.False(t, ok)
	// Test-only trigger is NOT registered.
	assert.Empty(t, trigReg.All(), "trigger from *_test.star must be skipped")
}

// TestBootRegistry_DrainsParserWarnings proves byte-identical duplicate
// triggers (D-07-13) accumulate as parser warnings and surface at boot via
// slog.Default().Warn — the registry stores both copies; only the warning
// flags the duplication.
func TestBootRegistry_DrainsParserWarnings(t *testing.T) {
	// Capture slog default's output via a custom handler.
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "dup.star"), []byte(`
flow(name = "check_user", steps = [])
trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
    credential = "github-app",
)
trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
    credential = "github-app",
)
`), 0o644))

	_, trigReg, err := bootRegistry(dir, []extension.Extension{fakeWebhookExt{}})
	require.NoError(t, err)
	require.Len(t, trigReg.All(), 2, "duplicate triggers accepted; both register (D-07-13)")
	assert.Contains(t, buf.String(), "duplicate trigger", "duplicate-warn must surface at boot via slog")
}

// TestBootRegistry_TriggerInLoadedFile proves a trigger declared in a file
// other than the one that owns its referenced flow still registers, as long
// as both files are inside rootDir.
func TestBootRegistry_TriggerInLoadedFile(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "prod.star"), []byte(`
flow(name = "check_user", steps = [])
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "triggers.star"), []byte(`
trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
)
`), 0o644))

	flowReg, trigReg, err := bootRegistry(dir, []extension.Extension{fakeWebhookExt{}})
	require.NoError(t, err)
	_, ok := flowReg.ContentHashFor("check_user")
	assert.True(t, ok, "flow registers from prod.star")
	assert.Len(t, trigReg.All(), 1, "trigger registers from triggers.star")
}

// TestBootRegistry_NoTriggers proves a directory with only flows produces an
// empty (non-nil) trigger registry — the worker still boots cleanly.
func TestBootRegistry_NoTriggers(t *testing.T) {
	dir := t.TempDir()
	writeStarFile(t, dir, "a.star", trivialFlowSrc)

	_, trigReg, err := bootRegistry(dir, nil)
	require.NoError(t, err)
	require.NotNil(t, trigReg)
	assert.Empty(t, trigReg.All(), "no triggers declared → empty registry")
}
