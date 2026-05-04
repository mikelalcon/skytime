package parser

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// TestStateSchema_AccumulatesScopes pins the stateSchema semantics: add/has,
// clone-isolation, and sortedKeys ordering. After D4.2-13 widening the API
// keeps the same shape (visibility-only) but values carry TypeInfo —
// addUntyped() is the visibility-only shortcut.
func TestStateSchema_AccumulatesScopes(t *testing.T) {
	s := newStateSchema()
	s.addUntyped("repo")
	require.True(t, s.has("repo"))
	require.False(t, s.has("missing"))

	branch := s.clone()
	branch.addUntyped("only_in_branch")
	require.True(t, branch.has("only_in_branch"))
	require.False(t, s.has("only_in_branch"), "clone must not leak into parent")

	require.Equal(t, []string{"repo"}, s.sortedKeys())
	require.Equal(t, []string{"only_in_branch", "repo"}, branch.sortedKeys())
}

// TestStateSchema_VisibilityAPIPreserved pins that newStateSchema /
// has / addUntyped / clone / sortedKeys cover the D4-02 walker's
// pre-widening surface — the regression net for the stateSet → stateSchema
// rename. Every call this test exercises is mirrored verbatim inside
// walkBodyForCtxValidation.
func TestStateSchema_VisibilityAPIPreserved(t *testing.T) {
	initial := newStateSchema()
	initial.addUntyped("repo_name")
	require.True(t, initial.has("repo_name"))
	require.False(t, initial.has("missing"))

	// Mimic the if_cond branch fork: each branch sees a clone of pre-branch
	// state; an add inside one branch must not leak.
	thenBranch := initial.clone()
	thenBranch.addUntyped("from_then")
	elseBranch := initial.clone()
	require.True(t, thenBranch.has("from_then"))
	require.False(t, elseBranch.has("from_then"), "branches must be isolated post-clone")
	require.False(t, initial.has("from_then"), "parent must not see branch-local additions")

	// sortedKeys for deterministic error output.
	require.Equal(t, []string{"repo_name"}, initial.sortedKeys())
}

// TestStateSchema_TypedAdd: D4.2-13 — typed add() preserves the TypeInfo
// for plan 03's get() consumer.
func TestStateSchema_TypedAdd(t *testing.T) {
	s := newStateSchema()
	s.add("x", TypeScalar{Kind: "int"})
	require.True(t, s.has("x"))
	got, ok := s.get("x")
	require.True(t, ok)
	require.True(t, Equal(got, TypeScalar{Kind: "int"}))
}

// TestStateSchema_AddUntyped: addUntyped() stores TypeOpaque{} so the
// branch-equality validator can detect "untyped state read here, defer
// the strict check".
func TestStateSchema_AddUntyped(t *testing.T) {
	s := newStateSchema()
	s.addUntyped("y")
	require.True(t, s.has("y"))
	got, ok := s.get("y")
	require.True(t, ok)
	require.True(t, Equal(got, TypeOpaque{}))
}

// TestStateSchema_CloneIsDeep: clone returns a fresh map; adds in either
// side stay local. (The TypeInfo values are immutable structs — a shallow
// copy of the map is sufficient.)
func TestStateSchema_CloneIsDeep(t *testing.T) {
	a := newStateSchema()
	a.add("x", TypeScalar{Kind: "int"})

	b := a.clone()
	b.add("y", TypeScalar{Kind: "string"})

	require.False(t, a.has("y"), "a must not see b's additions")
	require.True(t, b.has("x"), "b must inherit a's pre-clone entries")
}

// TestStateSchema_SortedKeys: deterministic ordering for error messages.
func TestStateSchema_SortedKeys(t *testing.T) {
	s := newStateSchema()
	s.addUntyped("c")
	s.addUntyped("a")
	s.addUntyped("b")
	require.Equal(t, []string{"a", "b", "c"}, s.sortedKeys())
}

// TestTypeFromHint: D4.2-13 flow.Inputs hint → TypeInfo mapping.
func TestTypeFromHint(t *testing.T) {
	cases := []struct {
		hint string
		want TypeInfo
	}{
		{"int", TypeScalar{Kind: "int"}},
		{"integer", TypeScalar{Kind: "int"}},
		{"float", TypeScalar{Kind: "float"}},
		{"number", TypeScalar{Kind: "float"}},
		{"bool", TypeScalar{Kind: "bool"}},
		{"boolean", TypeScalar{Kind: "bool"}},
		{"string", TypeScalar{Kind: "string"}},
		{"str", TypeScalar{Kind: "string"}},
		{"list", TypeList{Element: TypeOpaque{}}},
		{"array", TypeList{Element: TypeOpaque{}}},
		{"dict", TypeDict{Fields: nil}},
		{"object", TypeDict{Fields: nil}},
		{"map", TypeDict{Fields: nil}},
		{"", TypeOpaque{}},
		{"unknown_thing", TypeOpaque{}},
	}
	for _, c := range cases {
		got := typeFromHint(c.hint)
		require.True(t, Equal(got, c.want), "hint %q: got %v want %v", c.hint, got, c.want)
	}
}

// TestFinalize_CtxAccess_Valid is the positive case: a flow whose scripts
// only reference declared inputs and prior output_aliases parses cleanly.
// Stacking rules:
//   - flow inputs at entry: {repo_name}
//   - script(output_alias="echo_out"): state += echo_out before next node
//   - script(output_alias="x") sees both repo_name and echo_out
func TestFinalize_CtxAccess_Valid(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(
    name="ok",
    inputs={"repo_name": "string"},
    steps=[
        script(id="echo", fn=lambda ctx: {"out": ctx.repo_name}, output_alias="echo_out"),
        script(id="echo2", fn=lambda ctx: {"x": ctx.echo_out}, output_alias="x"),
    ],
)`)
	_, err := p.ParseSource("ok.star", src)
	require.NoError(t, err, "expected no error for valid ctx access")
}

// TestFinalize_CtxAccess_RejectsUnknown is the negative case: a typo on a
// ctx access that does not match any state-schema name surfaces as a
// *dag.ValidationError naming the unknown attribute.
func TestFinalize_CtxAccess_RejectsUnknown(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(
    name="bad",
    inputs={"repo_name": "string"},
    steps=[
        script(id="typo", fn=lambda ctx: {"x": ctx.repo_nme}, output_alias="x"),
    ],
)`)
	_, err := p.ParseSource("bad.star", src)
	require.Error(t, err)

	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T: %v", err, err)
	require.Contains(t, ve.Msg, "ctx.repo_nme")
	require.Contains(t, ve.Msg, "not in declared state")
	require.Equal(t, "bad", ve.Flow)
}

// TestFinalize_CtxAccess_ForEachItemVar covers the for_each_parallel
// stacking rule: state += ItemVar inside the fan-out body. The inner
// script's lambda sees `row` even though `row` is not a flow input.
func TestFinalize_CtxAccess_ForEachItemVar(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(
    name="loop",
    inputs={"repos": "list"},
    steps=[
        for_each_parallel(
            items=lambda ctx: ctx.repos,
            item="row",
            steps=[
                script(id="x", fn=lambda ctx: {"out": ctx.row}, output_alias="o"),
            ],
        ),
    ],
)`)
	_, err := p.ParseSource("loop.star", src)
	require.NoError(t, err, "row should be visible inside for_each body")
}

// TestFinalize_CtxAccess_IfCondBranchesIsolated covers the if_cond branching
// rule: outputs from `then` are NOT visible in `else_` and vice versa. The
// fixture below puts a script in `then` whose output_alias is "from_then",
// then references `ctx.from_then` from inside the `else_` branch — should
// reject because state forks at if_cond and `from_then` is set only in the
// then-branch's clone.
func TestFinalize_CtxAccess_IfCondBranchesIsolated(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(
    name="branchy",
    inputs={"flag": "bool"},
    steps=[
        if_cond(
            cond=lambda ctx: ctx.flag,
            then=[
                script(id="t", fn=lambda ctx: {"v": ctx.flag}, output_alias="from_then"),
            ],
            else_=[
                script(id="e", fn=lambda ctx: {"v": ctx.from_then}, output_alias="from_else"),
            ],
        ),
    ],
)`)
	_, err := p.ParseSource("branchy.star", src)
	require.Error(t, err)
	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T: %v", err, err)
	require.Contains(t, ve.Msg, "ctx.from_then")
	require.Contains(t, ve.Msg, "not in declared state")
}

// TestFinalize_CtxAccess_ForEachItemsLambdaPreLoop covers the items-producer
// scope: the lambda passed as `items=` sees ONLY pre-loop state (it cannot
// reference its own item-var). Here `ctx.repos` is a flow input so the
// items lambda parses; the test is positive (no error) — it pins that the
// items lambda is validated, not skipped.
func TestFinalize_CtxAccess_ForEachItemsLambdaPreLoop(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(
    name="items_ok",
    inputs={"repos": "list"},
    steps=[
        for_each_parallel(
            items=lambda ctx: ctx.repos,
            item="row",
            steps=[],
        ),
    ],
)`)
	_, err := p.ParseSource("items_ok.star", src)
	require.NoError(t, err)
}

// TestFinalize_CtxAccess_ForEachItemsLambdaCannotSeeItem is the negative
// half: the items lambda tries to reference its own item-var. Should
// reject because the item-var is added to state only *inside* Steps.
func TestFinalize_CtxAccess_ForEachItemsLambdaCannotSeeItem(t *testing.T) {
	p := newTestParser(t)
	src := []byte(`flow(
    name="items_bad",
    inputs={},
    steps=[
        for_each_parallel(
            items=lambda ctx: ctx.row,
            item="row",
            steps=[],
        ),
    ],
)`)
	_, err := p.ParseSource("items_bad.star", src)
	require.Error(t, err)
	var ve *dag.ValidationError
	require.True(t, errors.As(err, &ve), "expected *dag.ValidationError, got %T: %v", err, err)
	require.Contains(t, ve.Msg, "ctx.row")
	require.Contains(t, ve.Msg, "not in declared state")
}
