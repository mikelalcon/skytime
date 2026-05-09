package github_test

import (
	"testing"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	skygh "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/github"
	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// TestExtension_RegistersWithoutError verifies the extension constructs
// cleanly and every OperationSpec has Idempotent != nil. A nil
// Idempotent on any op would cause Registry.Register to return an error
// wrapping extension.ErrIdempotentRequired (D-12). This test pins that
// every op is registration-eligible.
func TestExtension_RegistersWithoutError(t *testing.T) {
	ext := skygh.New()
	require.NotNil(t, ext)
	assert.Equal(t, "github", ext.Name())

	ops := ext.Operations()
	require.NotEmpty(t, ops)
	for name, spec := range ops {
		require.NotNil(t, spec, "op %q has nil spec", name)
		require.NotNil(t, spec.Idempotent,
			"op %q is missing Idempotent *bool — would trigger extension.ErrIdempotentRequired at registration", name)
		require.NotNil(t, spec.Func, "op %q has nil Func", name)
		require.NotNil(t, spec.KwargsType, "op %q has nil KwargsType", name)
	}
}

// TestExtension_OperationsIdempotenceMatchesEndpoints pins the locked
// GitHub REST + RFC-7231 + application semantics matrix from
// RESEARCH.md § 1a. Any change here must flow through CONTEXT.md /
// RESEARCH.md amendments first. Mirrors the canonical
// TestExtension_OperationsIdempotenceMatchesD4_14 in the HTTP extension.
func TestExtension_OperationsIdempotenceMatchesEndpoints(t *testing.T) {
	ext := skygh.New()
	ops := ext.Operations()

	cases := map[string]bool{
		"get_repo":               true,
		"get_issue":              true,
		"list_open_issues":       true,
		"add_comment":            false,
		"add_label":              false,
		"list_prs":               true,
		"list_recent_merged_prs": true,
	}

	for op, want := range cases {
		t.Run(op, func(t *testing.T) {
			spec, ok := ops[op]
			require.True(t, ok, "op %q not in Operations() map", op)
			require.NotNil(t, spec.Idempotent, "op %q missing Idempotent *bool", op)
			assert.Equal(t, want, *spec.Idempotent, "op %q idempotence mismatch", op)
		})
	}

	assert.Equal(t, len(cases), len(ops),
		"unexpected number of operations: want %d, got %d", len(cases), len(ops))
}

// TestExtension_OutputsImplementOperationOutput pins the
// dag.OperationOutput marker on every top-level output type.
// Compile-time assertions catch accidental marker-method deletion;
// runtime calls catch accidental marker-method renaming or visibility
// changes.
func TestExtension_OutputsImplementOperationOutput(t *testing.T) {
	// Compile-time interface assertions.
	var _ dag.OperationOutput = skygh.GitHubRepoOutput{}
	var _ dag.OperationOutput = skygh.GitHubIssueOutput{}
	var _ dag.OperationOutput = skygh.GitHubIssueListOutput{}
	var _ dag.OperationOutput = skygh.GitHubCommentOutput{}
	var _ dag.OperationOutput = skygh.GitHubLabelsOutput{}
	var _ dag.OperationOutput = skygh.GitHubPRListOutput{}

	// Runtime sanity: each must support IsOperationOutput() (the marker
	// is a method, so calling it never panics on a value receiver).
	skygh.GitHubRepoOutput{}.IsOperationOutput()
	skygh.GitHubIssueOutput{}.IsOperationOutput()
	skygh.GitHubIssueListOutput{}.IsOperationOutput()
	skygh.GitHubCommentOutput{}.IsOperationOutput()
	skygh.GitHubLabelsOutput{}.IsOperationOutput()
	skygh.GitHubPRListOutput{}.IsOperationOutput()
}

// TestExtension_InitializeIncludesWebhook verifies Phase 7.1's wiring
// (D-7.1-02): the github extension's Initialize() Members map exposes
// `webhook` as a *starlark.Builtin whose .Name() == "github.webhook".
// This is the entry point .star authors call as
// `github.webhook(events=..., secret_credential=...)` to construct an
// inbound webhook trigger source.
func TestExtension_InitializeIncludesWebhook(t *testing.T) {
	ext := skygh.New()
	thread := &starlark.Thread{Name: "test-init-webhook"}
	val, err := ext.Initialize(thread, nil)
	require.NoError(t, err)

	mod, ok := val.(*starlarkstruct.Module)
	require.True(t, ok, "Initialize should return a *starlarkstruct.Module, got %T", val)

	webhookEntry, ok := mod.Members["webhook"]
	require.True(t, ok, "Members must contain a \"webhook\" entry")

	b, ok := webhookEntry.(*starlark.Builtin)
	require.True(t, ok, "Members[\"webhook\"] must be a *starlark.Builtin, got %T", webhookEntry)
	assert.Equal(t, "github.webhook", b.Name())
}

// TestExtension_InitializeIncludesClient_Regression verifies that
// adding the new `webhook` attribute did not displace the pre-existing
// `client` attribute (Phase 6 surface). Anchors the Initialize Members
// map invariant: BOTH attributes must be present after Phase 7.1.
func TestExtension_InitializeIncludesClient_Regression(t *testing.T) {
	ext := skygh.New()
	thread := &starlark.Thread{Name: "test-init-client"}
	val, err := ext.Initialize(thread, nil)
	require.NoError(t, err)

	mod, ok := val.(*starlarkstruct.Module)
	require.True(t, ok)

	clientEntry, ok := mod.Members["client"]
	require.True(t, ok, "Members must still contain the \"client\" entry (Phase 6 regression)")

	b, ok := clientEntry.(*starlark.Builtin)
	require.True(t, ok, "Members[\"client\"] must be a *starlark.Builtin")
	assert.Equal(t, "github.client", b.Name())
}

// TestExtension_StarFileCallsGithubWebhook is the load-bearing check
// that a `.star`-shaped invocation `github.webhook(...)` flowing
// through the Initialize-installed builtin actually constructs a
// value satisfying extension.TriggerSource. End-to-end proof that
// the wiring + factory + seal all line up.
func TestExtension_StarFileCallsGithubWebhook(t *testing.T) {
	ext := skygh.New()
	thread := &starlark.Thread{Name: "test-star-call-webhook"}
	mod, err := ext.Initialize(thread, nil)
	require.NoError(t, err)

	predeclared := starlark.StringDict{"github": mod}
	globals, err := starlark.ExecFile(thread, "test_init.star",
		`src = github.webhook(events=["issues"])`, predeclared)
	require.NoError(t, err)

	v, ok := globals["src"]
	require.True(t, ok, "src global should be set")

	src, ok := v.(extension.TriggerSource)
	require.True(t, ok, ".star-produced value must satisfy extension.TriggerSource")
	assert.Equal(t, "github.webhook", src.Kind())
}
