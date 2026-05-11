package schedules

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	commonpb "go.temporal.io/api/common/v1"
	"go.temporal.io/api/serviceerror"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
	skycore "github.com/mikelalcon/skytime/pkg/extension/builtin/core"
	"github.com/mikelalcon/skytime/pkg/interpreter"
)

// ---------- test helpers ----------

// pStringPtr is declared in id_test.go (same package).

// newTestTrigger returns a *dag.Trigger whose Source is a *skycore.CronSource
// built via skycore.NewCronSourceForTest. Catchup is optional (nil ⇒ unset).
func newTestTrigger(flow, file string, line int32, schedule, tz, overlap string, catchup *time.Duration) *dag.Trigger {
	src := skycore.NewCronSourceForTest(schedule, tz, overlap, catchup)
	return &dag.Trigger{
		FlowName: flow,
		Pos:      syntax.MakePosition(pStringPtr(file), line, 1),
		Source:   src,
	}
}

// newTestFlowRegistry returns a registry with one (flowName, contentHash)
// pair registered + frozen. Most reconciler tests need only ContentHashFor
// to succeed; a stub *interpreter.ParsedFlow is sufficient because the
// reconciler never derefs Flow/Lambdas.
func newTestFlowRegistry(t *testing.T, flowName, contentHash string) *interpreter.FlowRegistry {
	t.Helper()
	reg := interpreter.NewRegistry()
	// Stub ParsedFlow — fields nilable per the registry contract (only
	// require non-nil parsed pointer). The reconciler only calls
	// ContentHashFor, which iterates the inner map; it never touches
	// the Flow/Lambdas fields.
	require.NoError(t, reg.Register(flowName, contentHash, &interpreter.ParsedFlow{}))
	reg.Freeze()
	return reg
}

// captureLogger returns an slog.Logger that writes JSON records to the
// returned buffer; tests assert log content (e.g., "already exists" Warn
// for Pitfall 5).
func captureLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), buf
}

// encodeMemoCanonical produces a *commonpb.Memo whose
// "skytime_canonical" Payload encodes the given canonical-config JSON
// string via the SDK default DataConverter — the exact wire format the
// reconciler's Create emits. Tests use this to populate
// FakeScheduleClient.ListEntries[i].Memo for the "actual state" side of
// the diff.
func encodeMemoCanonical(t *testing.T, canonical string) *commonpb.Memo {
	t.Helper()
	pld, err := converter.GetDefaultDataConverter().ToPayload(canonical)
	require.NoError(t, err)
	return &commonpb.Memo{Fields: map[string]*commonpb.Payload{
		skytimeCanonicalMemoKey: pld,
	}}
}

// canonicalBytes returns the JSON canonical-config bytes for a cron
// trigger — mirrors buildDesired's marshal path so tests can compare
// against actual-state memos byte-for-byte.
func canonicalBytes(t *testing.T, schedule, tz, overlap, contentHash string, catchupNs int64) string {
	t.Helper()
	cfg := canonicalConfig{
		Cron:        schedule,
		Timezone:    tz,
		Overlap:     overlap,
		ContentHash: contentHash,
	}
	if catchupNs != 0 {
		cfg.CatchupWindowNs = catchupNs
	}
	b, err := json.Marshal(cfg)
	require.NoError(t, err)
	return string(b)
}

// discardLogger returns an slog.Logger that swallows all output. Used
// by tests that don't assert log content (most diff tests).
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ---------- Diff matrix (8 tests) ----------

func TestDiff_CreatesOnly(t *testing.T) {
	flows := interpreter.NewRegistry()
	require.NoError(t, flows.Register("a", "hashA", &interpreter.ParsedFlow{}))
	require.NoError(t, flows.Register("b", "hashB", &interpreter.ParsedFlow{}))
	flows.Freeze()

	ta := newTestTrigger("a", "a.star", 1, "0 9 * * 1", "UTC", "skip", nil)
	tb := newTestTrigger("b", "b.star", 1, "0 10 * * 2", "UTC", "skip", nil)

	desired, err := buildDesired([]*dag.Trigger{ta, tb}, flows)
	require.NoError(t, err)
	plan := diff(desired, map[string]actualState{})

	require.Len(t, plan.Creates, 2)
	require.Empty(t, plan.Updates)
	require.Empty(t, plan.Deletes)

	// Sorted by Schedule ID (lexicographic). Two random hash prefixes,
	// so we verify the property "creates are sorted" rather than which
	// trigger comes first.
	idA := ComposeScheduleID(plan.Creates[0])
	idB := ComposeScheduleID(plan.Creates[1])
	require.LessOrEqual(t, idA, idB)
}

func TestDiff_UpdateOnCronChange(t *testing.T) {
	flows := newTestFlowRegistry(t, "f", "hashF")
	tr := newTestTrigger("f", "f.star", 1, "0 10 * * 1", "UTC", "skip", nil)

	desired, err := buildDesired([]*dag.Trigger{tr}, flows)
	require.NoError(t, err)

	id := ComposeScheduleID(tr)
	// Actual encodes a DIFFERENT cron expression — drift.
	actual := map[string]actualState{
		id: {id: id, canonical: canonicalBytes(t, "0 9 * * 1", "UTC", "skip", "hashF", 0)},
	}

	plan := diff(desired, actual)
	require.Empty(t, plan.Creates)
	require.Len(t, plan.Updates, 1)
	require.Empty(t, plan.Deletes)
	assert.Equal(t, id, plan.Updates[0].ScheduleID)
	assert.Contains(t, plan.Updates[0].Reason, "canonical config drift")
}

func TestDiff_NoChanges(t *testing.T) {
	flows := newTestFlowRegistry(t, "f", "hashF")
	tr := newTestTrigger("f", "f.star", 1, "0 10 * * 1", "UTC", "skip", nil)

	desired, err := buildDesired([]*dag.Trigger{tr}, flows)
	require.NoError(t, err)

	id := ComposeScheduleID(tr)
	actual := map[string]actualState{
		id: {id: id, canonical: canonicalBytes(t, "0 10 * * 1", "UTC", "skip", "hashF", 0)},
	}

	plan := diff(desired, actual)
	require.Empty(t, plan.Creates)
	require.Empty(t, plan.Updates)
	require.Empty(t, plan.Deletes)
}

func TestDiff_DeleteOrphan(t *testing.T) {
	desired := map[string]desiredState{}
	actual := map[string]actualState{
		"skytime/x/abc12345": {id: "skytime/x/abc12345", canonical: "{}"},
	}

	plan := diff(desired, actual)
	require.Empty(t, plan.Creates)
	require.Empty(t, plan.Updates)
	require.Equal(t, []string{"skytime/x/abc12345"}, plan.Deletes)
}

func TestDiff_UnmanagedScheduleIgnored(t *testing.T) {
	// The diff itself only sees skytime-prefixed entries from
	// listSkytimeManaged. Verify the full path via ReconcileCronSchedules
	// with apply=false: feeding ListEntries with a non-prefixed ID must
	// produce a Plan with all three buckets empty.
	fake := NewFakeScheduleClient()
	fake.ListEntries = []*client.ScheduleListEntry{
		{ID: "user-foo"},
	}
	flows := newTestFlowRegistry(t, "f", "hashF")

	plan, err := ReconcileCronSchedules(context.Background(), fake, nil, flows, "tq", false, discardLogger())
	require.NoError(t, err)
	require.Empty(t, plan.Creates)
	require.Empty(t, plan.Updates)
	require.Empty(t, plan.Deletes)
}

func TestDiff_Mixed(t *testing.T) {
	flows := interpreter.NewRegistry()
	require.NoError(t, flows.Register("create_me", "hashC", &interpreter.ParsedFlow{}))
	require.NoError(t, flows.Register("update_me", "hashU", &interpreter.ParsedFlow{}))
	flows.Freeze()

	tCreate := newTestTrigger("create_me", "c.star", 1, "0 9 * * 1", "UTC", "skip", nil)
	tUpdate := newTestTrigger("update_me", "u.star", 1, "0 10 * * 1", "UTC", "skip", nil)

	desired, err := buildDesired([]*dag.Trigger{tCreate, tUpdate}, flows)
	require.NoError(t, err)

	idUpdate := ComposeScheduleID(tUpdate)
	deleteID := "skytime/delete_me/zzzz1111"
	actual := map[string]actualState{
		idUpdate: {id: idUpdate, canonical: canonicalBytes(t, "0 5 * * 1", "UTC", "skip", "hashU", 0)},
		deleteID: {id: deleteID, canonical: "{}"},
	}

	plan := diff(desired, actual)
	require.Len(t, plan.Creates, 1)
	require.Len(t, plan.Updates, 1)
	require.Len(t, plan.Deletes, 1)
	assert.Equal(t, "create_me", plan.Creates[0].FlowName)
	assert.Equal(t, idUpdate, plan.Updates[0].ScheduleID)
	assert.Equal(t, deleteID, plan.Deletes[0])
}

func TestDiff_UpdateOnTimezoneChange(t *testing.T) {
	flows := newTestFlowRegistry(t, "f", "hashF")
	tr := newTestTrigger("f", "f.star", 1, "0 9 * * 1", "UTC", "skip", nil)
	desired, err := buildDesired([]*dag.Trigger{tr}, flows)
	require.NoError(t, err)

	id := ComposeScheduleID(tr)
	actual := map[string]actualState{
		id: {id: id, canonical: canonicalBytes(t, "0 9 * * 1", "America/New_York", "skip", "hashF", 0)},
	}

	plan := diff(desired, actual)
	require.Len(t, plan.Updates, 1)
	assert.Equal(t, id, plan.Updates[0].ScheduleID)
}

func TestDiff_UpdateOnContentHashChange(t *testing.T) {
	flows := newTestFlowRegistry(t, "f", "hashF_NEW")
	tr := newTestTrigger("f", "f.star", 1, "0 9 * * 1", "UTC", "skip", nil)
	desired, err := buildDesired([]*dag.Trigger{tr}, flows)
	require.NoError(t, err)

	id := ComposeScheduleID(tr)
	// Cron config byte-identical, ONLY ContentHash drifts (Pitfall 7).
	actual := map[string]actualState{
		id: {id: id, canonical: canonicalBytes(t, "0 9 * * 1", "UTC", "skip", "hashF_OLD", 0)},
	}

	plan := diff(desired, actual)
	require.Len(t, plan.Updates, 1, "ContentHash drift alone must emit an Update (Pitfall 7)")
}

// ---------- Apply matrix (6 tests including Memo-staleness limitation) ----------

func TestReconcile_AppliesAllBuckets(t *testing.T) {
	flows := interpreter.NewRegistry()
	require.NoError(t, flows.Register("create_me", "hashC", &interpreter.ParsedFlow{}))
	require.NoError(t, flows.Register("update_me", "hashU", &interpreter.ParsedFlow{}))
	flows.Freeze()

	tCreate := newTestTrigger("create_me", "c.star", 1, "0 9 * * 1", "UTC", "skip", nil)
	catchup := 5 * time.Minute
	tUpdate := newTestTrigger("update_me", "u.star", 1, "0 10 * * 1", "America/New_York", "allow", &catchup)

	idUpdate := ComposeScheduleID(tUpdate)
	deleteID := "skytime/delete_me/zzzz1111"

	fake := NewFakeScheduleClient()
	fake.ListEntries = []*client.ScheduleListEntry{
		// Update target — stale canonical to force drift.
		{ID: idUpdate, Memo: encodeMemoCanonical(t, canonicalBytes(t, "0 5 * * 1", "UTC", "skip", "hashU", 0))},
		// Delete orphan — no matching desired.
		{ID: deleteID, Memo: encodeMemoCanonical(t, "{}")},
		// Ignore — not skytime-managed.
		{ID: "user-thing"},
	}
	fake.DescribeResponses[idUpdate] = &client.ScheduleDescription{
		Schedule: client.Schedule{State: &client.ScheduleState{}},
	}

	logger := discardLogger()
	plan, err := ReconcileCronSchedules(context.Background(), fake, []*dag.Trigger{tCreate, tUpdate}, flows, "test-tq", true, logger)
	require.NoError(t, err)
	require.Len(t, plan.Creates, 1)
	require.Len(t, plan.Updates, 1)
	require.Len(t, plan.Deletes, 1)

	// Create call: exact shape verification.
	require.Len(t, fake.CreateCalls, 1)
	createOpts := fake.CreateCalls[0]
	assert.Equal(t, ComposeScheduleID(tCreate), createOpts.ID)
	assert.Equal(t, []string{"0 9 * * 1"}, createOpts.Spec.CronExpressions)
	assert.Equal(t, "UTC", createOpts.Spec.TimeZoneName)
	assert.Equal(t, enumspb.SCHEDULE_OVERLAP_POLICY_SKIP, createOpts.Overlap)
	require.NotNil(t, createOpts.Memo)
	assert.Contains(t, createOpts.Memo, skytimeCanonicalMemoKey)

	wfAction, ok := createOpts.Action.(*client.ScheduleWorkflowAction)
	require.True(t, ok, "Action must be *client.ScheduleWorkflowAction")
	assert.Equal(t, SkytimeWorkflowType, wfAction.Workflow)
	assert.Equal(t, "test-tq", wfAction.TaskQueue)
	// ID has NO timestamp suffix (Pitfall 2 — server appends).
	assert.Equal(t, "create_me/"+posHash(tCreate), wfAction.ID)
	require.Len(t, wfAction.Args, 1)
	wfInput, ok := wfAction.Args[0].(dag.WorkflowInput)
	require.True(t, ok, "Args[0] must be dag.WorkflowInput value (not pointer)")
	assert.Equal(t, "create_me", wfInput.FlowName)
	assert.Equal(t, "hashC", wfInput.ContentHash)
	assert.Nil(t, wfInput.InitState)

	// Update call: state preserved, Spec/Action/Policy reflect desired.
	require.Len(t, fake.UpdateCalls, 1)
	assert.Equal(t, idUpdate, fake.UpdateCalls[0].ScheduleID)
	require.NotNil(t, fake.UpdateCalls[0].Result)
	require.NotNil(t, fake.UpdateCalls[0].Result.Schedule)
	assert.NotNil(t, fake.UpdateCalls[0].Result.Schedule.Policy)
	assert.Equal(t, enumspb.SCHEDULE_OVERLAP_POLICY_ALLOW_ALL, fake.UpdateCalls[0].Result.Schedule.Policy.Overlap)
	assert.Equal(t, 5*time.Minute, fake.UpdateCalls[0].Result.Schedule.Policy.CatchupWindow)

	// Delete call.
	require.Equal(t, []string{deleteID}, fake.DeleteCalls)
}

func TestReconcile_DryRunIsReadOnly(t *testing.T) {
	flows := newTestFlowRegistry(t, "f", "hashF")
	tr := newTestTrigger("f", "f.star", 1, "0 9 * * 1", "UTC", "skip", nil)

	fake := NewFakeScheduleClient()
	fake.ListEntries = []*client.ScheduleListEntry{
		{ID: "skytime/orphan/xxxxxxxx", Memo: encodeMemoCanonical(t, "{}")},
	}

	plan, err := ReconcileCronSchedules(context.Background(), fake, []*dag.Trigger{tr}, flows, "tq", false, discardLogger())
	require.NoError(t, err)
	require.Len(t, plan.Creates, 1)
	require.Len(t, plan.Deletes, 1)

	// Zero mutating calls in dry-run mode.
	assert.Empty(t, fake.CreateCalls)
	assert.Empty(t, fake.UpdateCalls)
	assert.Empty(t, fake.DeleteCalls)
}

func TestReconcile_AccumulatesFailures(t *testing.T) {
	flows := interpreter.NewRegistry()
	require.NoError(t, flows.Register("a", "hashA", &interpreter.ParsedFlow{}))
	require.NoError(t, flows.Register("b", "hashB", &interpreter.ParsedFlow{}))
	flows.Freeze()

	ta := newTestTrigger("a", "a.star", 1, "0 9 * * 1", "UTC", "skip", nil)
	tb := newTestTrigger("b", "b.star", 1, "0 10 * * 1", "UTC", "skip", nil)

	fake := NewFakeScheduleClient()
	fake.CreateErr = fmt.Errorf("boom")

	_, err := ReconcileCronSchedules(context.Background(), fake, []*dag.Trigger{ta, tb}, flows, "tq", true, discardLogger())
	require.Error(t, err)
	// errors.Join joins multiple errors with "\n"; assert both wrapped
	// messages are present (no short-circuit).
	msg := err.Error()
	assert.Contains(t, msg, "boom")
	// Both Create attempts happened — count "create " occurrences in the
	// aggregated error.
	assert.Equal(t, 2, strings.Count(msg, "create "), "both Create attempts must run; errors.Join contains two wrapped errors")
}

func TestReconcile_AlreadyExistsIsNonFatal(t *testing.T) {
	flows := newTestFlowRegistry(t, "f", "hashF")
	tr := newTestTrigger("f", "f.star", 1, "0 9 * * 1", "UTC", "skip", nil)

	fake := NewFakeScheduleClient()
	fake.CreateErr = serviceerror.NewAlreadyExists("another reconciler beat us")

	logger, buf := captureLogger()
	plan, err := ReconcileCronSchedules(context.Background(), fake, []*dag.Trigger{tr}, flows, "tq", true, logger)
	require.NoError(t, err, "AlreadyExists must be non-fatal (Pitfall 5)")
	require.Len(t, plan.Creates, 1)

	logs := buf.String()
	assert.Contains(t, strings.ToLower(logs), "already exists")
}

func TestReconcile_UpdatePreservesState(t *testing.T) {
	flows := newTestFlowRegistry(t, "f", "hashF")
	tr := newTestTrigger("f", "f.star", 1, "0 10 * * 1", "UTC", "skip", nil)
	id := ComposeScheduleID(tr)

	fake := NewFakeScheduleClient()
	// Stale canonical → forces an Update.
	fake.ListEntries = []*client.ScheduleListEntry{
		{ID: id, Memo: encodeMemoCanonical(t, canonicalBytes(t, "0 5 * * 1", "UTC", "skip", "hashF", 0))},
	}
	// Operator paused this schedule via Temporal UI; the reconciler
	// MUST preserve that state.
	fake.DescribeResponses[id] = &client.ScheduleDescription{
		Schedule: client.Schedule{State: &client.ScheduleState{
			Paused: true,
			Note:   "operator paused",
		}},
	}

	_, err := ReconcileCronSchedules(context.Background(), fake, []*dag.Trigger{tr}, flows, "tq", true, discardLogger())
	require.NoError(t, err)

	require.Len(t, fake.UpdateCalls, 1)
	require.NotNil(t, fake.UpdateCalls[0].Result)
	require.NotNil(t, fake.UpdateCalls[0].Result.Schedule)
	require.NotNil(t, fake.UpdateCalls[0].Result.Schedule.State)
	assert.True(t, fake.UpdateCalls[0].Result.Schedule.State.Paused, "operator-set Paused must survive reconciliation")
	assert.Equal(t, "operator paused", fake.UpdateCalls[0].Result.Schedule.State.Note)
}

// TestReconcile_UpdateMemoStaysStale_DocumentedLimitation pins the SDK
// limitation: ScheduleHandle.Update does NOT include the Memo field in
// the outbound gRPC request. After an Update, the cluster-side Memo
// keeps the OLD canonical bytes. On the next boot, diff sees the same
// drift and emits the same Update again — idempotent thrash bounded to
// one redundant Update per boot per drifted schedule.
//
// This test models that scenario: after one Update cycle, set the
// cluster's actual.canonical to the STALE pre-Update value (since the
// Memo wasn't refreshed); re-run diff(desired, actual); assert another
// Update is emitted (NOT a no-op) AND that the Update payload is
// byte-identical across the two boots (idempotency proof).
func TestReconcile_UpdateMemoStaysStale_DocumentedLimitation(t *testing.T) {
	flows := newTestFlowRegistry(t, "f", "hashF_NEW")
	tr := newTestTrigger("f", "f.star", 1, "0 10 * * 1", "UTC", "skip", nil)
	id := ComposeScheduleID(tr)

	staleCanonical := canonicalBytes(t, "0 10 * * 1", "UTC", "skip", "hashF_OLD", 0)

	// First boot: drift detected (hashF_OLD on cluster, hashF_NEW
	// desired). Update fires.
	desired1, err := buildDesired([]*dag.Trigger{tr}, flows)
	require.NoError(t, err)
	actual1 := map[string]actualState{
		id: {id: id, canonical: staleCanonical},
	}
	plan1 := diff(desired1, actual1)
	require.Len(t, plan1.Updates, 1)

	// Second boot: the SDK didn't refresh the Memo on the cluster, so
	// actual.canonical is STILL staleCanonical. diff sees the same
	// drift and emits ANOTHER Update (idempotent thrash).
	desired2, err := buildDesired([]*dag.Trigger{tr}, flows)
	require.NoError(t, err)
	actual2 := map[string]actualState{
		id: {id: id, canonical: staleCanonical},
	}
	plan2 := diff(desired2, actual2)
	require.Len(t, plan2.Updates, 1, "Memo staleness produces idempotent thrash: each boot sees the same drift")

	// Idempotency proof: the two Updates carry byte-identical Reason
	// strings (same diff inputs ⇒ same Reason).
	assert.Equal(t, plan1.Updates[0].Reason, plan2.Updates[0].Reason)
	// And both target the same Schedule ID.
	assert.Equal(t, plan1.Updates[0].ScheduleID, plan2.Updates[0].ScheduleID)
}

// ---------- Overlap + action shape (2 tests) ----------

func TestMapOverlapToEnum(t *testing.T) {
	cases := map[string]enumspb.ScheduleOverlapPolicy{
		"skip":         enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
		"allow":        enumspb.SCHEDULE_OVERLAP_POLICY_ALLOW_ALL,
		"buffer_one":   enumspb.SCHEDULE_OVERLAP_POLICY_BUFFER_ONE,
		"cancel_other": enumspb.SCHEDULE_OVERLAP_POLICY_CANCEL_OTHER,
		"unknown":      enumspb.SCHEDULE_OVERLAP_POLICY_SKIP, // defensive fallback
		"":             enumspb.SCHEDULE_OVERLAP_POLICY_SKIP,
	}
	for in, want := range cases {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, want, mapOverlapToEnum(in))
		})
	}
}

func TestScheduleAction_WorkflowInputShape(t *testing.T) {
	flows := newTestFlowRegistry(t, "weekly_digest", "hashWD")
	tr := newTestTrigger("weekly_digest", "weekly_digest.star", 5, "0 9 * * 1", "UTC", "skip", nil)

	opts, err := scheduleOptionsFor(tr, flows, "demo-tq")
	require.NoError(t, err)

	wfAction, ok := opts.Action.(*client.ScheduleWorkflowAction)
	require.True(t, ok, "Action must be *client.ScheduleWorkflowAction")
	assert.Equal(t, SkytimeWorkflowType, wfAction.Workflow)
	assert.Equal(t, "demo-tq", wfAction.TaskQueue)

	// Pitfall 2: no timestamp suffix on the Action.ID — server adds it.
	expectedID := tr.FlowName + "/" + posHash(tr)
	assert.Equal(t, expectedID, wfAction.ID)
	// Just to be loud about it:
	assert.False(t, strings.Contains(wfAction.ID, "-"), "Pitfall 2: Action.ID must NOT contain a timestamp suffix; server adds one server-side")

	require.Len(t, wfAction.Args, 1)
	wfInput, ok := wfAction.Args[0].(dag.WorkflowInput)
	require.True(t, ok)
	assert.Equal(t, "weekly_digest", wfInput.FlowName)
	assert.Equal(t, "hashWD", wfInput.ContentHash)
	assert.Nil(t, wfInput.InitState)
}

// ---------- Sanity: buildDesired error paths ----------

// TestBuildDesired_NonCronTriggersSkipped — buildDesired filters out
// triggers whose Source.Kind() is not "core.cron". Non-cron triggers
// (HTTP, signal, future kinds) must not appear in the desired map.
func TestBuildDesired_NonCronTriggersSkipped(t *testing.T) {
	flows := newTestFlowRegistry(t, "f", "hashF")
	tr := newTestTrigger("f", "f.star", 1, "0 9 * * 1", "UTC", "skip", nil)
	otherTr := &dag.Trigger{FlowName: "g", Source: &fakeOtherSource{}}

	desired, err := buildDesired([]*dag.Trigger{tr, otherTr}, flows)
	require.NoError(t, err)
	require.Len(t, desired, 1, "non-cron trigger must be filtered out")
	require.Contains(t, desired, ComposeScheduleID(tr))
}

// fakeOtherSource is a minimal dag.TriggerSource that is NOT a cronSource.
// Used to test buildDesired's Kind() filter.
type fakeOtherSource struct{}

func (*fakeOtherSource) Kind() string                  { return "other.kind" }
func (*fakeOtherSource) MarshalJSON() ([]byte, error)  { return []byte(`{}`), nil }

// ---------- isAlreadyExists ----------

func TestIsAlreadyExists(t *testing.T) {
	assert.False(t, isAlreadyExists(nil))
	assert.False(t, isAlreadyExists(errors.New("ordinary error")))
	assert.True(t, isAlreadyExists(serviceerror.NewAlreadyExists("yo")))
	// Wrapped error still matches via errors.As.
	wrapped := fmt.Errorf("outer: %w", serviceerror.NewAlreadyExists("inner"))
	assert.True(t, isAlreadyExists(wrapped))
}
