package github_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	skygh "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/github"
	"github.com/mikelalcon/skytime/pkg/dag"
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
