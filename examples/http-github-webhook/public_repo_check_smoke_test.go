// Package httpgithubwebhook_test — public_repo_check_smoke_test.go
//
// In-suite walkthrough smoke for the EX-04 README headline demo
// (public_repo_check.star). This test catches the same regression as
// pkg/parser/interpolation_collision_test.go but at the example tier:
// the .star file is the REAL one that ships in the repo and that CI's
// walkthrough_smoke.sh exercises end-to-end. If the parser-level fix
// (quick 260512-w7c) regresses, this test fails LOCALLY without needing
// a running Temporal server.
//
// Specifically: the if_cond.then[0] block step in public_repo_check.star
// contains:
//
//	gh.list_open_issues(owner = "${ctx.rp.owner}", repo = "${ctx.rp.repo}"),
//	gh.list_prs(owner = "${ctx.rp.owner}", repo = "${ctx.rp.repo}"),
//
// — four `${...}` kwargs total, two per ActionRef, all sharing the same
// `ar.Pos` per ActionRef. Before the fix, each ActionRef's owner+repo
// pair collided to a single lambda ID (p.lambdas last-wins) and the
// workflow's resolveKwargs (interpreter/resolve_kwargs.go ID-lookup)
// evaluated the SECOND-captured lambda for BOTH kwargs — the live
// request hit GET /repos/Hello-World/Hello-World/issues and 404'd
// (CI's walkthrough_smoke.sh exit 1, locally invisible to `go test`).
package httpgithubwebhook_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/bridge"
	"github.com/mikelalcon/skytime/pkg/dag"
)

// TestPublicRepoCheck_KwargLambdasResolveDistinctly is the regression
// backstop for quick 260512-w7c at the example tier. Parses the REAL
// public_repo_check.star, walks to the if_cond.then[0] block step, and
// asserts each ActionRef's owner + repo kwargs are *StarlarkLambda
// instances whose bodies evaluate to the CORRECT distinct values
// against a mocked rp state struct.
func TestPublicRepoCheck_KwargLambdasResolveDistinctly(t *testing.T) {
	p := newExampleParser(t)

	starPath, err := filepath.Abs("public_repo_check.star")
	require.NoError(t, err)

	flows, err := p.ParseFile(starPath)
	require.NoError(t, err, "ParseFile(%s)", starPath)
	require.Contains(t, flows, "public_repo_check")
	f := flows["public_repo_check"]
	require.NotNil(t, f)

	// f.Body is roughly: [script(parse_repo), step(action_fn=...),
	// script(popularity), if_cond(...)]. Walk to the if_cond, then to
	// its then-branch's first step (the block of two GETs).
	var ifc *dag.IfCond
	for _, node := range f.Body {
		if ic, ok := node.(*dag.IfCond); ok {
			ifc = ic
			break
		}
	}
	require.NotNil(t, ifc, "flow should contain an if_cond node")

	require.GreaterOrEqual(t, len(ifc.Then), 1)
	blockStep, ok := ifc.Then[0].(*dag.Step)
	require.True(t, ok, "if_cond.then[0] should be *dag.Step, got %T", ifc.Then[0])
	require.Len(t, blockStep.Actions, 2,
		"block must contain exactly two ActionRefs (list_open_issues, list_prs); got %d", len(blockStep.Actions))

	ctx := context.Background()
	// State mirrors what runs in CI's walkthrough_smoke.sh: input
	// repo="octocat/Hello-World" → parse_repo script populates rp.
	state := map[string]any{
		"repo": "octocat/Hello-World",
		"rp": map[string]any{
			"owner": "octocat",
			"repo":  "Hello-World",
		},
		"pop": map[string]any{"popular": true},
	}

	for i, ar := range blockStep.Actions {
		// Sanity: both owner and repo kwargs MUST be *StarlarkLambda
		// (proves the desugarer ran, not the bug where the literal
		// "${ctx.rp.owner}" string slipped through).
		for _, key := range []string{"owner", "repo"} {
			v, found, err := ar.Kwargs.Get(starlark.String(key))
			require.NoError(t, err, "action[%d].Kwargs.Get(%q)", i, key)
			require.True(t, found, "action[%d] missing kwarg %q", i, key)
			_, isLambda := dag.UnwrapStarlarkLambda(v)
			require.True(t, isLambda,
				"action[%d].kwargs[%q] should be *StarlarkLambda, got %T", i, key, v)
		}

		// The IDs of owner and repo MUST differ. This is the parser-
		// level invariant Task 1 guarantees via the kwarg-key
		// disambiguator. If the IDs collide here, the interpreter's
		// ID-keyed evalLambda(resolved.ID) lookup would return the
		// same captured lambda for both kwargs at runtime.
		ownerVal, _, _ := ar.Kwargs.Get(starlark.String("owner"))
		repoVal, _, _ := ar.Kwargs.Get(starlark.String("repo"))
		ownerLam, _ := dag.UnwrapStarlarkLambda(ownerVal)
		repoLam, _ := dag.UnwrapStarlarkLambda(repoVal)
		require.NotNil(t, ownerLam)
		require.NotNil(t, repoLam)
		assert.NotEqual(t, ownerLam.ID, repoLam.ID,
			"action[%d] owner+repo lambda IDs collide: owner=%q repo=%q — regression of quick 260512-w7c",
			i, ownerLam.ID, repoLam.ID)

		// Evaluate owner and repo and assert they land on the
		// CORRECT distinct values (not "Hello-World" for both — the
		// bug shape).
		ownerRes, err := bridge.CallLambda(ctx, ownerLam, state, bridge.CallOptions{})
		require.NoError(t, err, "action[%d] owner lambda eval", i)
		repoRes, err := bridge.CallLambda(ctx, repoLam, state, bridge.CallOptions{})
		require.NoError(t, err, "action[%d] repo lambda eval", i)

		assert.Equal(t, starlark.String("octocat"), ownerRes,
			"action[%d].owner should evaluate to 'octocat'; got %v (bug: both kwargs collapse to last-captured lambda)", i, ownerRes)
		assert.Equal(t, starlark.String("Hello-World"), repoRes,
			"action[%d].repo should evaluate to 'Hello-World'; got %v", i, repoRes)
		assert.NotEqual(t, ownerRes, repoRes,
			"action[%d] owner+repo collapse to the same value — regression of quick 260512-w7c", i)
	}
}
