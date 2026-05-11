package parser_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	skycore "github.com/mikelalcon/skytime/pkg/extension/builtin/core"
	"github.com/mikelalcon/skytime/pkg/parser"
)

// =============================================================================
// Test extension that exposes fake.webhook(req_fields=[...]) → TriggerSource
// =============================================================================
//
// fakeWebhookExt is a parser-side test extension (Name="fake") with a single
// attribute `webhook` — a builtin that constructs a fakeTriggerStarlarkValue
// (which embeds *extension.FakeTriggerSource so the unexported
// triggerSourceMarker() seal is satisfied by promotion). The wrapper also
// implements the starlark.Value contract so the value flows through Starlark.
//
// We pin the kind to "skytime.test.webhook" inside the builtin so all
// trigger fixtures share that discriminator regardless of how the test
// fixture spelt the call.

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
// unexported triggerSourceMarker() seal (defined in package extension)
// is satisfied by method promotion. Kind() / ReqSchema() / MarshalJSON()
// are also promoted, so no delegation methods are needed for the
// extension.TriggerSource interface. Only the starlark.Value methods
// need declaring on the wrapper.
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

// parseFixture builds a parser rooted at testdata/triggers and parses the
// given relative fixture path. Convenience for the per-fixture tests.
func parseFixture(t *testing.T, relPath string) (*parser.Parser, error) {
	t.Helper()
	p, err := parser.NewParser(
		parser.WithRoot("testdata/triggers"),
		parser.WithExtensions(fakeWebhookExt{}),
	)
	require.NoError(t, err)
	_, err = p.ParseFile("testdata/triggers/" + relPath)
	return p, err
}

// =============================================================================
// TRIG-01: trigger(...) parses without I/O; fields captured correctly
// =============================================================================

// TestBuiltinTrigger covers TRIG-01: parse a clean trigger fixture and
// assert the *dag.Trigger surface ends up in p.Triggers().
func TestBuiltinTrigger(t *testing.T) {
	p, err := parseFixture(t, "valid.star")
	require.NoError(t, err)

	trigs := p.Triggers()
	require.Len(t, trigs, 1)

	trig := trigs[0]
	assert.Equal(t, "check_user", trig.FlowName)
	assert.Equal(t, "skytime.test.webhook", trig.Source.Kind())
	assert.NotNil(t, trig.MapLambda, "MapLambda must be captured")
	assert.NotNil(t, trig.IdempotencyLambda, "IdempotencyLambda must be captured")
	assert.Equal(t, "github-app-prod", trig.CredentialID)
}

// TestBuiltinTrigger_Fields drills into the trigger's Pos and lambda IDs
// to confirm position-attribution is intact and D-18 IDs are non-empty.
func TestBuiltinTrigger_Fields(t *testing.T) {
	p, err := parseFixture(t, "valid.star")
	require.NoError(t, err)

	trigs := p.Triggers()
	require.Len(t, trigs, 1)
	trig := trigs[0]

	assert.True(t, trig.Pos.IsValid(), "trigger Pos must be valid")
	assert.Contains(t, trig.Pos.Filename(), "valid.star",
		"trigger Pos.Filename must reference the fixture file")
	assert.NotZero(t, trig.Pos.Line, "trigger Pos.Line must be non-zero")
	assert.NotZero(t, trig.Pos.Col, "trigger Pos.Col must be non-zero")

	require.NotNil(t, trig.MapLambda)
	assert.NotEmpty(t, trig.MapLambda.ID, "MapLambda.ID must be a non-empty D-18 hash")
	require.NotNil(t, trig.IdempotencyLambda)
	assert.NotEmpty(t, trig.IdempotencyLambda.ID, "IdempotencyLambda.ID must be a non-empty D-18 hash")
}

// =============================================================================
// TRIG-04: parse-time validation surfaces position-aware errors
// =============================================================================

// TestTrigger_UnknownFlow exercises validateTriggerFlowNames (D-07-12).
func TestTrigger_UnknownFlow(t *testing.T) {
	_, err := parseFixture(t, "unknown_flow.star")
	require.Error(t, err)

	var pe *dag.ParseError
	require.ErrorAs(t, err, &pe, "expected *dag.ParseError, got %T", err)
	assert.Regexp(t,
		regexp.MustCompile(`trigger references unknown flow "missing"; known flows: \[check_user\]`),
		err.Error())
}

// TestTrigger_BadSource exercises the source type-assert in builtinTrigger.
func TestTrigger_BadSource(t *testing.T) {
	_, err := parseFixture(t, "not_a_source.star")
	require.Error(t, err)

	var pe *dag.ParseError
	require.ErrorAs(t, err, &pe, "expected *dag.ParseError, got %T", err)
	assert.Regexp(t,
		regexp.MustCompile(`trigger\.source: expected TriggerSource, got string`),
		err.Error())
}

// TestTrigger_ReqAttrTypo exercises validateTriggerReqAccesses (D-07-05).
func TestTrigger_ReqAttrTypo(t *testing.T) {
	_, err := parseFixture(t, "typo.star")
	require.Error(t, err)

	var ve *dag.ValidationError
	require.ErrorAs(t, err, &ve, "expected *dag.ValidationError, got %T", err)
	assert.Regexp(t,
		regexp.MustCompile(`trigger map lambda: req has no attribute "payloud"; available: \[headers payload\] \(declared by source kind "skytime\.test\.webhook"\)`),
		err.Error())
}

// TestTrigger_CronReqAttrTypo exercises validateTriggerReqAccesses against
// a *core.cronSource trigger source. Confirms the walker integrates with
// the new Phase 7.2 extension by validating that req.payload (NOT in
// core.cron's ReqSchema) surfaces a position-aware ValidationError listing
// the valid set ["actual_time", "scheduled_time"] (sorted at render time)
// and the source kind "core.cron".
//
// The render-time alphabetical-sort assertion guards the D-7.2-14 erratum:
// ReqSchema() returns semantic priority order [scheduled_time, actual_time],
// but the walker's error formatter MUST sort suggestions alphabetically so
// error messages are deterministic regardless of ReqSchema() ordering.
func TestTrigger_CronReqAttrTypo(t *testing.T) {
	p, err := parser.NewParser(
		parser.WithRoot("testdata/triggers"),
		parser.WithExtensions(skycore.New(), fakeWebhookExt{}),
	)
	require.NoError(t, err)

	// ParseFile runs finalize internally (per pkg/parser/parser.go); the
	// validateTriggerReqAccesses pass surfaces the typo here.
	_, err = p.ParseFile("testdata/triggers/cron_req_attr_typo.star")
	require.Error(t, err, "ParseFile must return the req-walker validation error")

	var ve *dag.ValidationError
	require.ErrorAs(t, err, &ve, "expected *dag.ValidationError; got %T: %v", err, err)
	msg := err.Error()
	require.Contains(t, msg, `"payload"`, "error must name the invalid attribute")
	require.Contains(t, msg, `core.cron`, "error must name the source kind")
	require.Contains(t, msg, "actual_time", "error must list actual_time as a valid attribute")
	require.Contains(t, msg, "scheduled_time", "error must list scheduled_time as a valid attribute")

	// Determinism: the walker's error-render code MUST sort the suggested-
	// attribute list at render time so messages are stable regardless of
	// ReqSchema()'s semantic-priority return order (D-7.2-14 erratum).
	// Assert "actual_time" appears BEFORE "scheduled_time" in the rendered
	// message (alphabetical at render). validateTriggerReqAccesses already
	// sorts via sortedKeysTrigger; this assertion locks that contract.
	require.Less(t,
		strings.Index(msg, "actual_time"),
		strings.Index(msg, "scheduled_time"),
		"walker error message must list suggested attributes in alphabetical order at render time: %s", msg)
}

// TestTrigger_BadArity exercises captureLambdaWithArity layer 2.
func TestTrigger_BadArity(t *testing.T) {
	_, err := parseFixture(t, "bad_arity.star")
	require.Error(t, err)

	var pe *dag.ParseError
	require.ErrorAs(t, err, &pe, "expected *dag.ParseError, got %T", err)
	assert.Regexp(t,
		regexp.MustCompile(`kwarg "map" lambda must accept exactly 1 positional parameter\(s\) \(convention: req\); got 2`),
		err.Error())
}

// TestTrigger_MutableClosure exercises the Phase 1 free-var lint when a
// trigger lambda closes over a mutable module-level value. The exact error
// wording belongs to validateFreeVars; we only assert that the error
// references the offending name.
func TestTrigger_MutableClosure(t *testing.T) {
	_, err := parseFixture(t, "mutable_closure.star")
	require.Error(t, err, "mutable closure must be rejected")

	msg := err.Error()
	matches := false
	for _, sub := range []string{"counter", "free var", "module-level", "non-module-level"} {
		if regexp.MustCompile(sub).MatchString(msg) {
			matches = true
			break
		}
	}
	assert.True(t, matches,
		"expected error to mention the closure variable or free-var lint terminology; got: %s", msg)
}

// =============================================================================
// D-07-12: cross-file trigger.FlowName resolution
// =============================================================================

// TestTrigger_CrossFileFlow exercises the cross-file resolution: the trigger
// file load()s the flow file, and finalize resolves the FlowName against
// the merged session state.
func TestTrigger_CrossFileFlow(t *testing.T) {
	p, err := parser.NewParser(
		parser.WithRoot("testdata/triggers"),
		parser.WithExtensions(fakeWebhookExt{}),
	)
	require.NoError(t, err)

	_, err = p.ParseFile("testdata/triggers/cross_file_trigger.star")
	require.NoError(t, err, "cross-file trigger should parse cleanly via load()")

	flows := p.Flows()
	_, ok := flows["check_user"]
	assert.True(t, ok, "flow check_user should be loaded from cross_file_flow.star")

	trigs := p.Triggers()
	require.Len(t, trigs, 1)
	assert.Equal(t, "check_user", trigs[0].FlowName)
}

// =============================================================================
// D-07-13: byte-identical duplicates → warning, not error
// =============================================================================

// TestTrigger_DuplicateWarn exercises warnDuplicateTriggers.
func TestTrigger_DuplicateWarn(t *testing.T) {
	p, err := parseFixture(t, "duplicate_warn.star")
	require.NoError(t, err, "duplicate triggers must be ACCEPTED per D-07-13")

	trigs := p.Triggers()
	require.Len(t, trigs, 2, "both triggers must be registered")

	warnings := p.TriggerWarnings()
	require.Len(t, warnings, 1, "exactly one warning expected for the byte-identical pair")
	assert.Regexp(t,
		regexp.MustCompile(`duplicate trigger \(byte-identical to .*\) — accepted but flagged`),
		warnings[0])
}
