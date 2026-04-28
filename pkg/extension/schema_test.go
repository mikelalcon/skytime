package extension

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// sampleArgs is the test-corpus operation parameter struct exercising every
// `star:` tag form: required vs. optional, the full type matrix supported by
// assignStarlarkToGo (string/int/bool/float64/[]string/map[string]string),
// the `-` opt-out, and an untagged field which must be ignored.
type sampleArgs struct {
	Repo        string            `star:"repo,required"`
	Title       string            `star:"title,required"`
	Body        string            `star:"body"`
	Count       int               `star:"count"`
	Open        bool              `star:"open"`
	Score       float64           `star:"score"`
	Tags        []string          `star:"tags"`
	Meta        map[string]string `star:"meta"`
	Internal    string            `star:"-"` // explicitly opted out
	Untagged    string            // no `star:` tag — must be ignored
}

// emptyTagArgs is a malformed-by-design struct used by TestParseSchema_RejectsEmptyStarName.
type emptyTagArgs struct {
	Bad string `star:",required"` // empty name + required — must be rejected
}

// dummyPos returns a syntax.Position usable in tests.
func dummyPos(line, col int32) syntax.Position {
	return syntax.MakePosition(refStr("test.star"), line, col)
}

// refStr is a tiny helper around &"x" used for syntax.MakePosition.
func refStr(s string) *string { return &s }

// ----------------------------------------------------------------------------
// ParseSchema tests
// ----------------------------------------------------------------------------

// TestParseSchema_ExtractsFieldSpecs — happy path. Verifies every `star:`-
// tagged field becomes a FieldSpec with the right name, GoType, FieldIdx,
// and Required flag.
func TestParseSchema_ExtractsFieldSpecs(t *testing.T) {
	specs, err := ParseSchema(reflect.TypeOf(sampleArgs{}))
	require.NoError(t, err)

	// Build an index by StarName so the test does not depend on field order.
	byName := make(map[string]FieldSpec, len(specs))
	for _, s := range specs {
		byName[s.StarName] = s
	}

	// Every `star:`-tagged field except `-` and untagged is present.
	expected := map[string]bool{
		"repo": true, "title": true, "body": false, "count": false,
		"open": false, "score": false, "tags": false, "meta": false,
	}
	require.Len(t, specs, len(expected),
		"got %d specs, want %d", len(specs), len(expected))

	for name, wantRequired := range expected {
		spec, ok := byName[name]
		require.True(t, ok, "missing FieldSpec for %q", name)
		assert.Equal(t, wantRequired, spec.Required, "Required mismatch for %q", name)
	}

	// Validate field types are recorded.
	assert.Equal(t, reflect.String, byName["repo"].GoType.Kind())
	assert.Equal(t, reflect.Int, byName["count"].GoType.Kind())
	assert.Equal(t, reflect.Bool, byName["open"].GoType.Kind())
	assert.Equal(t, reflect.Float64, byName["score"].GoType.Kind())
	assert.Equal(t, reflect.Slice, byName["tags"].GoType.Kind())
	assert.Equal(t, reflect.Map, byName["meta"].GoType.Kind())
}

// TestParseSchema_AcceptsPointerToStruct — many extension authors will declare
// `KwargsType: reflect.TypeOf(&CreateIssueArgs{})`. ParseSchema unwraps the
// pointer.
func TestParseSchema_AcceptsPointerToStruct(t *testing.T) {
	specs, err := ParseSchema(reflect.TypeOf(&sampleArgs{}))
	require.NoError(t, err)
	assert.NotEmpty(t, specs)
}

// TestParseSchema_RejectsNonStruct — passing a non-struct type is a caller
// bug; ParseSchema reports it instead of silently returning nil.
func TestParseSchema_RejectsNonStruct(t *testing.T) {
	_, err := ParseSchema(reflect.TypeOf(42))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a struct")
}

// TestParseSchema_RejectsEmptyStarName — `star:",required"` (no name) is a
// caller bug.
func TestParseSchema_RejectsEmptyStarName(t *testing.T) {
	_, err := ParseSchema(reflect.TypeOf(emptyTagArgs{}))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty star: name")
}

// TestParseSchema_IgnoresDashAndUntagged — `star:"-"` and untagged fields
// are ignored.
func TestParseSchema_IgnoresDashAndUntagged(t *testing.T) {
	specs, err := ParseSchema(reflect.TypeOf(sampleArgs{}))
	require.NoError(t, err)
	for _, s := range specs {
		assert.NotEqual(t, "-", s.StarName)
		assert.NotEqual(t, "Internal", s.GoName, "field with star:\"-\" leaked into specs")
		assert.NotEqual(t, "Untagged", s.GoName, "untagged field leaked into specs")
	}
}

// ----------------------------------------------------------------------------
// UnpackOperationKwargs tests
// ----------------------------------------------------------------------------

// kwargsOf is a convenience for building Starlark kwargs as []starlark.Tuple.
func kwargsOf(pairs ...any) []starlark.Tuple {
	if len(pairs)%2 != 0 {
		panic("kwargsOf: odd number of pairs")
	}
	out := make([]starlark.Tuple, 0, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			panic("kwargsOf: keys must be string")
		}
		val, ok := pairs[i+1].(starlark.Value)
		if !ok {
			panic(fmt.Sprintf("kwargsOf: value at idx %d must be starlark.Value, got %T", i+1, pairs[i+1]))
		}
		out = append(out, starlark.Tuple{starlark.String(key), val})
	}
	return out
}

// TestUnpackOperationKwargs_HappyPath — valid kwargs populate the target struct.
func TestUnpackOperationKwargs_HappyPath(t *testing.T) {
	specs, err := ParseSchema(reflect.TypeOf(sampleArgs{}))
	require.NoError(t, err)

	tagsList := starlark.NewList([]starlark.Value{
		starlark.String("bug"), starlark.String("urgent"),
	})
	metaDict := starlark.NewDict(1)
	require.NoError(t, metaDict.SetKey(starlark.String("priority"), starlark.String("high")))

	kwargs := kwargsOf(
		"repo", starlark.String("owner/name"),
		"title", starlark.String("Crash on startup"),
		"body", starlark.String("Stack trace inside"),
		"count", starlark.MakeInt(42),
		"open", starlark.Bool(true),
		"score", starlark.Float(3.14),
		"tags", tagsList,
		"meta", metaDict,
	)

	var target sampleArgs
	err = UnpackOperationKwargs("create_issue", dummyPos(10, 5), specs, kwargs, &target)
	require.NoError(t, err)
	assert.Equal(t, "owner/name", target.Repo)
	assert.Equal(t, "Crash on startup", target.Title)
	assert.Equal(t, "Stack trace inside", target.Body)
	assert.Equal(t, 42, target.Count)
	assert.True(t, target.Open)
	assert.InDelta(t, 3.14, target.Score, 1e-9)
	assert.Equal(t, []string{"bug", "urgent"}, target.Tags)
	assert.Equal(t, map[string]string{"priority": "high"}, target.Meta)
}

// TestUnpackOperationKwargs_MissingRequired — D-04 ValidationError with
// position attached and message naming the missing field.
func TestUnpackOperationKwargs_MissingRequired(t *testing.T) {
	specs, err := ParseSchema(reflect.TypeOf(sampleArgs{}))
	require.NoError(t, err)

	pos := dummyPos(10, 5)
	kwargs := kwargsOf("title", starlark.String("hi")) // repo missing
	var target sampleArgs

	err = UnpackOperationKwargs("create_issue", pos, specs, kwargs, &target)
	require.Error(t, err)

	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T", err)
	assert.Contains(t, ve.Msg, "repo")
	assert.Contains(t, ve.Msg, "missing required")
	assert.Equal(t, pos, ve.Pos)
}

// TestUnpackOperationKwargs_UnknownKwarg — passing an unknown kwarg is a
// parse-time error mentioning the bad key.
func TestUnpackOperationKwargs_UnknownKwarg(t *testing.T) {
	specs, err := ParseSchema(reflect.TypeOf(sampleArgs{}))
	require.NoError(t, err)

	kwargs := kwargsOf(
		"repo", starlark.String("o/n"),
		"title", starlark.String("hi"),
		"reviewer", starlark.String("alice"), // unknown
	)
	var target sampleArgs

	err = UnpackOperationKwargs("create_issue", dummyPos(10, 5), specs, kwargs, &target)
	require.Error(t, err)

	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T", err)
	assert.Contains(t, ve.Msg, "reviewer")
	assert.Contains(t, ve.Msg, "unknown")
}

// TestUnpackOperationKwargs_WrongType — int passed for a string field becomes
// a typed error mentioning the field and expected type.
func TestUnpackOperationKwargs_WrongType(t *testing.T) {
	specs, err := ParseSchema(reflect.TypeOf(sampleArgs{}))
	require.NoError(t, err)

	kwargs := kwargsOf(
		"repo", starlark.MakeInt(42), // wrong type — should be string
		"title", starlark.String("hi"),
	)
	var target sampleArgs

	err = UnpackOperationKwargs("create_issue", dummyPos(10, 5), specs, kwargs, &target)
	require.Error(t, err)

	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T", err)
	assert.Contains(t, ve.Msg, "repo")
	assert.Contains(t, ve.Msg, "string")
}

// TestUnpackOperationKwargs_NoneIsZeroValue — Starlark None for an optional
// field leaves the Go zero-value in place (matches the "absent" semantics).
func TestUnpackOperationKwargs_NoneIsZeroValue(t *testing.T) {
	specs, err := ParseSchema(reflect.TypeOf(sampleArgs{}))
	require.NoError(t, err)

	kwargs := kwargsOf(
		"repo", starlark.String("o/n"),
		"title", starlark.String("hi"),
		"body", starlark.None, // None for optional → field stays zero
	)
	var target sampleArgs

	err = UnpackOperationKwargs("create_issue", dummyPos(10, 5), specs, kwargs, &target)
	require.NoError(t, err)
	assert.Equal(t, "", target.Body)
}

// TestUnpackOperationKwargs_NonStringKey — a kwarg key that is not a Starlark
// string (rare; comes from misuse of starlark.Tuple construction) is rejected.
func TestUnpackOperationKwargs_NonStringKey(t *testing.T) {
	specs, err := ParseSchema(reflect.TypeOf(sampleArgs{}))
	require.NoError(t, err)

	kwargs := []starlark.Tuple{
		{starlark.MakeInt(1), starlark.String("v")},
	}
	var target sampleArgs

	err = UnpackOperationKwargs("create_issue", dummyPos(10, 5), specs, kwargs, &target)
	require.Error(t, err)

	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T", err)
	assert.Contains(t, ve.Msg, "kwarg key must be string")
}

// ----------------------------------------------------------------------------
// assignStarlarkToGo direct tests — covers all supported types.
// ----------------------------------------------------------------------------

func TestAssignStarlarkToGo_String(t *testing.T) {
	var dst string
	require.NoError(t, assignStarlarkToGo(reflect.ValueOf(&dst).Elem(), starlark.String("hello")))
	assert.Equal(t, "hello", dst)

	require.Error(t, assignStarlarkToGo(reflect.ValueOf(&dst).Elem(), starlark.MakeInt(1)))
}

func TestAssignStarlarkToGo_IntFamily(t *testing.T) {
	var i int
	require.NoError(t, assignStarlarkToGo(reflect.ValueOf(&i).Elem(), starlark.MakeInt(42)))
	assert.Equal(t, 42, i)

	var i64 int64
	require.NoError(t, assignStarlarkToGo(reflect.ValueOf(&i64).Elem(), starlark.MakeInt(9001)))
	assert.Equal(t, int64(9001), i64)

	var i8 int8
	require.NoError(t, assignStarlarkToGo(reflect.ValueOf(&i8).Elem(), starlark.MakeInt(7)))
	assert.Equal(t, int8(7), i8)
}

func TestAssignStarlarkToGo_Bool(t *testing.T) {
	var b bool
	require.NoError(t, assignStarlarkToGo(reflect.ValueOf(&b).Elem(), starlark.Bool(true)))
	assert.True(t, b)

	require.Error(t, assignStarlarkToGo(reflect.ValueOf(&b).Elem(), starlark.String("nope")))
}

func TestAssignStarlarkToGo_Float64(t *testing.T) {
	var f float64
	require.NoError(t, assignStarlarkToGo(reflect.ValueOf(&f).Elem(), starlark.Float(2.5)))
	assert.InDelta(t, 2.5, f, 1e-9)
}

func TestAssignStarlarkToGo_StringSlice(t *testing.T) {
	var s []string
	lst := starlark.NewList([]starlark.Value{starlark.String("a"), starlark.String("b")})
	require.NoError(t, assignStarlarkToGo(reflect.ValueOf(&s).Elem(), lst))
	assert.Equal(t, []string{"a", "b"}, s)

	// Wrong element type rejected.
	mixed := starlark.NewList([]starlark.Value{starlark.String("a"), starlark.MakeInt(1)})
	var s2 []string
	require.Error(t, assignStarlarkToGo(reflect.ValueOf(&s2).Elem(), mixed))
}

func TestAssignStarlarkToGo_StringMap(t *testing.T) {
	d := starlark.NewDict(2)
	require.NoError(t, d.SetKey(starlark.String("k1"), starlark.String("v1")))
	require.NoError(t, d.SetKey(starlark.String("k2"), starlark.String("v2")))

	var m map[string]string
	require.NoError(t, assignStarlarkToGo(reflect.ValueOf(&m).Elem(), d))
	assert.Equal(t, map[string]string{"k1": "v1", "k2": "v2"}, m)
}

func TestAssignStarlarkToGo_NoneIsNoOp(t *testing.T) {
	var s string
	require.NoError(t, assignStarlarkToGo(reflect.ValueOf(&s).Elem(), starlark.None))
	assert.Equal(t, "", s)
}

func TestAssignStarlarkToGo_UnsupportedKind(t *testing.T) {
	var c complex128 // not in the supported set
	err := assignStarlarkToGo(reflect.ValueOf(&c).Elem(), starlark.Float(1.0))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

// ----------------------------------------------------------------------------
// Test 18 — EXT-02 / D-08 module-attribute factory pattern.
// ----------------------------------------------------------------------------

// TestExtensionFactory_ReturnsActionRef walks the full D-08 user-authoring
// pattern:
//
//	gh = github.endpoint("admin")
//	gh.create_issue(repo="x", title="y")
//
// at the Go level. The fakeFactoryExtension returns a *starlarkstruct.Module
// from Initialize; that module has an `endpoint` attribute that is a
// *starlark.Builtin returning a credential-aware sub-Module; that sub-Module
// has a `create_issue` attribute that returns a *dag.ActionRef carrying the
// CredentialID.
func TestExtensionFactory_ReturnsActionRef(t *testing.T) {
	ext := &fakeFactoryExtension{}
	thread := &starlark.Thread{Name: "test"}

	// Step 1: Initialize returns the github module — must be HasAttrs.
	githubVal, err := ext.Initialize(thread, nil)
	require.NoError(t, err)
	github, ok := githubVal.(starlark.HasAttrs)
	require.True(t, ok, "Initialize must return a value with attributes (typically *starlarkstruct.Module), got %T", githubVal)

	// Step 2: github.endpoint("admin") → sub-module.
	endpointAttr, err := github.Attr("endpoint")
	require.NoError(t, err)
	require.NotNil(t, endpointAttr, "github.endpoint must exist")

	ghVal, err := starlark.Call(thread, endpointAttr, starlark.Tuple{starlark.String("admin")}, nil)
	require.NoError(t, err)
	gh, ok := ghVal.(starlark.HasAttrs)
	require.True(t, ok, "endpoint() must return a value with attributes, got %T", ghVal)

	// Step 3: gh.create_issue(repo="x", title="y") → *dag.ActionRef.
	ciAttr, err := gh.Attr("create_issue")
	require.NoError(t, err)
	require.NotNil(t, ciAttr, "gh.create_issue must exist")

	result, err := starlark.Call(thread, ciAttr, nil, []starlark.Tuple{
		{starlark.String("repo"), starlark.String("x")},
		{starlark.String("title"), starlark.String("y")},
	})
	require.NoError(t, err)

	ar, ok := result.(*dag.ActionRef)
	require.True(t, ok, "create_issue must return *dag.ActionRef, got %T", result)
	assert.Equal(t, "fake.create_issue", ar.ActionKind())
	assert.Equal(t, "admin", ar.CredentialID,
		"credential ID from endpoint(\"admin\") must propagate to ActionRef.CredentialID per D-08")
}

// fakeFactoryExtension implements the D-08 pattern: Initialize returns a
// *starlarkstruct.Module with an "endpoint" attribute that creates a
// credential-aware sub-Module whose operation attributes return *dag.ActionRef.
type fakeFactoryExtension struct{}

func (e *fakeFactoryExtension) Name() string { return "github" }

func (e *fakeFactoryExtension) Operations() map[string]*OperationSpec {
	return map[string]*OperationSpec{
		"create_issue": {
			Name:       "create_issue",
			Idempotent: Ptr(false),
			Func: func(ctx context.Context, args any, cred Credential) (dag.OperationOutput, error) {
				return nil, nil
			},
			KwargsType: reflect.TypeOf(struct{}{}),
		},
	}
}

func (e *fakeFactoryExtension) Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error) {
	endpointFn := starlark.NewBuiltin("endpoint",
		func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kw []starlark.Tuple) (starlark.Value, error) {
			if len(args) != 1 {
				return nil, fmt.Errorf("endpoint requires exactly one positional argument (credential id)")
			}
			credIDStr, ok := args[0].(starlark.String)
			if !ok {
				return nil, fmt.Errorf("endpoint credential id must be string, got %s", args[0].Type())
			}
			return makeCredentialModule(string(credIDStr)), nil
		})
	return &starlarkstruct.Module{
		Name: "github",
		Members: starlark.StringDict{
			"endpoint": endpointFn,
		},
	}, nil
}

// makeCredentialModule returns a sub-module whose operation attributes close
// over the credential ID and produce *dag.ActionRef on call.
func makeCredentialModule(credID string) *starlarkstruct.Module {
	createIssue := starlark.NewBuiltin("create_issue",
		func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kw []starlark.Tuple) (starlark.Value, error) {
			// Phase-1 fake: collect kwargs into a dict without strict
			// schema validation (UnpackOperationKwargs is exercised
			// in tests 9-17 above). Just produce a recognizable
			// *dag.ActionRef.
			kwDict := starlark.NewDict(len(kw))
			for _, pair := range kw {
				if err := kwDict.SetKey(pair[0], pair[1]); err != nil {
					return nil, err
				}
			}
			// Phase 1 fake doesn't need a real call-site position;
			// real extensions in Phase 6 will pull this from
			// thread.CallStack at the appropriate depth.
			return &dag.ActionRef{
				Pos:          syntax.Position{},
				Kind_:        "fake.create_issue",
				Kwargs:       kwDict,
				CredentialID: credID,
			}, nil
		})
	return &starlarkstruct.Module{
		Name: "github_endpoint:" + credID,
		Members: starlark.StringDict{
			"create_issue": createIssue,
		},
	}
}

// ----------------------------------------------------------------------------
// DecodeKwargsFromDict tests (Plan 02-01 Task 4 — runtime-path companion to
// UnpackOperationKwargs)
// ----------------------------------------------------------------------------

// decodeArgs is the parameter struct exercised by DecodeKwargsFromDict
// tests. Mirrors the shape ActionRef.Kwargs would carry for a typical
// extension op (one required string + optional int + optional list).
type decodeArgs struct {
	Repo     string   `star:"repo,required"`
	IssueNum int      `star:"issue_num"`
	Labels   []string `star:"labels"`
}

// TestDecodeKwargsFromDict_RoundTrip — happy path: build a *starlark.Dict
// matching the schema, freeze it (mirroring parse-time freeze on
// ActionRef.Kwargs), decode into the target, assert each field populated.
func TestDecodeKwargsFromDict_RoundTrip(t *testing.T) {
	dict := starlark.NewDict(3)
	require.NoError(t, dict.SetKey(starlark.String("repo"), starlark.String("octocat/hello")))
	require.NoError(t, dict.SetKey(starlark.String("issue_num"), starlark.MakeInt(42)))
	labels := starlark.NewList([]starlark.Value{starlark.String("bug"), starlark.String("p1")})
	require.NoError(t, dict.SetKey(starlark.String("labels"), labels))
	dict.Freeze() // mirror parse-time freeze on ActionRef.Kwargs

	var got decodeArgs
	err := DecodeKwargsFromDict("github.create_comment", dict, &got)
	require.NoError(t, err)
	assert.Equal(t, "octocat/hello", got.Repo)
	assert.Equal(t, 42, got.IssueNum)
	assert.Equal(t, []string{"bug", "p1"}, got.Labels)
}

// TestDecodeKwargsFromDict_MissingRequired verifies a Dict missing the
// `repo,required` field surfaces a *dag.ValidationError matching the
// exact "missing required kwarg" wording UnpackOperationKwargs emits.
func TestDecodeKwargsFromDict_MissingRequired(t *testing.T) {
	dict := starlark.NewDict(0) // missing required "repo"
	dict.Freeze()
	var got decodeArgs
	err := DecodeKwargsFromDict("github.create_comment", dict, &got)
	require.Error(t, err)

	var vErr *dag.ValidationError
	require.True(t, errors.As(err, &vErr), "expected *dag.ValidationError, got %T", err)
	assert.Contains(t, vErr.Msg, `missing required kwarg "repo"`)
}

// TestDecodeKwargsFromDict_UnknownKwarg verifies an extra kwarg not declared
// on the struct surfaces the canonical "unknown kwarg" wording.
func TestDecodeKwargsFromDict_UnknownKwarg(t *testing.T) {
	dict := starlark.NewDict(2)
	require.NoError(t, dict.SetKey(starlark.String("repo"), starlark.String("x/y")))
	require.NoError(t, dict.SetKey(starlark.String("extra_arg"), starlark.String("oops")))
	dict.Freeze()

	var got decodeArgs
	err := DecodeKwargsFromDict("github.create_comment", dict, &got)
	require.Error(t, err)

	var vErr *dag.ValidationError
	require.True(t, errors.As(err, &vErr))
	assert.Contains(t, vErr.Msg, `unknown kwarg "extra_arg"`)
}

// TestDecodeKwargsFromDict_TypeMismatch verifies a type-mismatched value
// (string for int) produces a *dag.ValidationError naming the offending
// kwarg.
func TestDecodeKwargsFromDict_TypeMismatch(t *testing.T) {
	dict := starlark.NewDict(2)
	require.NoError(t, dict.SetKey(starlark.String("repo"), starlark.String("x/y")))
	require.NoError(t, dict.SetKey(starlark.String("issue_num"), starlark.String("not a number")))
	dict.Freeze()

	var got decodeArgs
	err := DecodeKwargsFromDict("github.create_comment", dict, &got)
	require.Error(t, err)

	var vErr *dag.ValidationError
	require.True(t, errors.As(err, &vErr))
	assert.Contains(t, vErr.Msg, `issue_num`)
}

// TestDecodeKwargsFromDict_NilTarget verifies a nil target returns a
// non-panicking error mentioning the op name.
func TestDecodeKwargsFromDict_NilTarget(t *testing.T) {
	dict := starlark.NewDict(0)
	err := DecodeKwargsFromDict("op", dict, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "op")
	assert.Contains(t, err.Error(), "target is nil")
}

// TestDecodeKwargsFromDict_NonStructTarget verifies a target that is a
// pointer to a non-struct (e.g., *int) is rejected gracefully via
// ParseSchema's existing not-a-struct error path.
func TestDecodeKwargsFromDict_NonStructTarget(t *testing.T) {
	dict := starlark.NewDict(0)
	var x int
	err := DecodeKwargsFromDict("op", dict, &x)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a struct")
}

// TestDecodeKwargsFromDict_NonPointerTarget verifies a non-pointer target
// is rejected with a useful error (NOT a panic via reflect.Value.Elem on
// a non-pointer).
func TestDecodeKwargsFromDict_NonPointerTarget(t *testing.T) {
	dict := starlark.NewDict(0)
	var s decodeArgs
	err := DecodeKwargsFromDict("op", dict, s) // pass by value, not by pointer
	require.Error(t, err)
	assert.Contains(t, err.Error(), "non-nil pointer to struct")
}

// TestDecodeKwargsFromDict_FrozenDict_AllowedToIterate is the regression
// test for the parse-time freeze constraint: ActionRef.Kwargs is frozen
// before the activity runs, and DecodeKwargsFromDict must work against
// frozen dicts (i.e., it must not attempt to mutate the dict during
// iteration). This is true today via Items() returning a snapshot slice;
// the test pins the contract so a future refactor can't regress it.
func TestDecodeKwargsFromDict_FrozenDict_AllowedToIterate(t *testing.T) {
	dict := starlark.NewDict(1)
	require.NoError(t, dict.SetKey(starlark.String("repo"), starlark.String("a/b")))
	dict.Freeze() // critical: ActionRef.Kwargs is frozen at parse time

	var got decodeArgs
	err := DecodeKwargsFromDict("op", dict, &got)
	require.NoError(t, err)
	assert.Equal(t, "a/b", got.Repo)
}

// TestDecodeKwargsFromDict_NilDict — a nil *starlark.Dict (e.g., if Phase 2
// somehow constructs an ActionRef with no kwargs) must not panic and must
// surface as a missing-required error for required fields.
func TestDecodeKwargsFromDict_NilDict(t *testing.T) {
	var got decodeArgs
	err := DecodeKwargsFromDict("op", nil, &got)
	require.Error(t, err, "nil Dict + required field must produce a clean error, not a panic")
	var vErr *dag.ValidationError
	require.True(t, errors.As(err, &vErr))
	assert.Contains(t, vErr.Msg, `missing required kwarg "repo"`)
}
