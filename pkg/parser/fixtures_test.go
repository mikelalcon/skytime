package parser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// =============================================================================
// fakeGithubExtension — D-08 module-attribute factory pattern (test 6)
// =============================================================================

// fakeGithubExtension exercises the user authoring example from CONTEXT.md
// D-08: `gh = github.endpoint("admin")` returns a credential-aware sub-Module
// whose attributes (`create_issue`, etc.) close over the credential ID and
// produce *dag.ActionRef intents with CredentialID populated.
//
// This is distinct from the simpler `fakeExtension` (in builtins_test.go)
// which only exposes a top-level `echo` op. The Github fake captures the
// FULL D-08 contract — Module → endpoint factory → sub-Module → op factory
// → *dag.ActionRef carrying the credential ID — end-to-end.
type fakeGithubExtension struct{}

func (*fakeGithubExtension) Name() string { return "github" }

func (*fakeGithubExtension) Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error) {
	endpointFn := starlark.NewBuiltin("endpoint", func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kw []starlark.Tuple) (starlark.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("endpoint requires one positional credential id, got %d args", len(args))
		}
		credIDStr, ok := args[0].(starlark.String)
		if !ok {
			return nil, fmt.Errorf("endpoint credential id must be string, got %s", args[0].Type())
		}
		credID := string(credIDStr)
		// Closure: createIssue captures credID by value so every
		// *dag.ActionRef built from THIS endpoint carries the right ID.
		createIssue := starlark.NewBuiltin("create_issue", func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kw []starlark.Tuple) (starlark.Value, error) {
			kwDict := starlark.NewDict(len(kw))
			for _, pair := range kw {
				_ = kwDict.SetKey(pair[0], pair[1])
			}
			return &dag.ActionRef{
				Pos:          callerPositionOrZero(thread),
				Kind_:        "github.create_issue",
				Kwargs:       kwDict,
				CredentialID: credID,
			}, nil
		})
		return &starlarkstruct.Module{
			Name:    "github_endpoint:" + credID,
			Members: starlark.StringDict{"create_issue": createIssue},
		}, nil
	})
	return &starlarkstruct.Module{
		Name:    "github",
		Members: starlark.StringDict{"endpoint": endpointFn},
	}, nil
}

func (*fakeGithubExtension) Operations() map[string]*extension.OperationSpec {
	return map[string]*extension.OperationSpec{
		"create_issue": {
			Name:       "create_issue",
			Idempotent: extension.Ptr(false),
			Func: func(ctx context.Context, args any, cred extension.Credential) (any, error) {
				return nil, nil
			},
			KwargsType: reflect.TypeOf(struct {
				Repo  string `star:"repo,required"`
				Title string `star:"title,required"`
			}{}),
		},
	}
}

// =============================================================================
// TestExtensionEndpointFactory_PropagatesCredentialID — D-08 verification
// =============================================================================

func TestExtensionEndpointFactory_PropagatesCredentialID(t *testing.T) {
	p, err := NewParser(WithExtensions(&fakeGithubExtension{}))
	require.NoError(t, err)

	flows, err := p.ParseFile("../../tests/fixtures/valid/07-extension-endpoint-credential.star")
	require.NoError(t, err, "D-08 user authoring pattern must parse cleanly")

	flow, ok := flows["issue_creation"]
	require.True(t, ok, `expected flow "issue_creation" to be present`)
	require.Len(t, flow.Body, 1)

	step, ok := flow.Body[0].(*dag.Step)
	require.True(t, ok, "first body element must be *dag.Step")
	require.Len(t, step.Actions, 1)

	action := step.Actions[0]
	assert.Equal(t, "github.create_issue", action.Kind_,
		"operation identity must be preserved through the closure")
	assert.Equal(t, "admin", action.CredentialID,
		`D-08: credential ID from github.endpoint("admin") must propagate into *dag.ActionRef`)
}

// =============================================================================
// TestValidFixtures — every .star under tests/fixtures/valid parses
// =============================================================================

// goldenShape wraps the parser's flow map under a "flows" key so the
// golden output structure is stable against future top-level additions.
// (E.g., if we later add a "lambda_ids" sibling field at the top level,
// existing goldens still validate.)
type goldenShape struct {
	Flows map[string]*dag.Flow `json:"flows"`
}

func TestValidFixtures(t *testing.T) {
	fixtures, err := filepath.Glob("../../tests/fixtures/valid/*.star")
	require.NoError(t, err)
	require.NotEmpty(t, fixtures)

	// Filter out helper / target files.
	var top []string
	for _, f := range fixtures {
		base := filepath.Base(f)
		if strings.Contains(base, "-target") || strings.Contains(base, "-helper") {
			continue
		}
		top = append(top, f)
	}
	require.NotEmpty(t, top, "expected at least one top-level valid fixture")

	for _, f := range top {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			p, err := NewParser(
				WithRoot("../../tests/fixtures/valid"),
				WithExtensions(&fakeExtension{}, &fakeGithubExtension{}),
			)
			require.NoError(t, err)

			flows, parseErr := p.ParseFile(f)
			require.NoError(t, parseErr, "fixture %s must parse cleanly", f)
			require.NotEmpty(t, flows, "fixture %s must produce at least one flow", f)

			goldenPath := strings.TrimSuffix(f, ".star") + ".golden.json"
			if _, statErr := os.Stat(goldenPath); statErr != nil {
				return // no golden — skip comparison
			}

			got, err := json.MarshalIndent(goldenShape{Flows: flows}, "", "  ")
			require.NoError(t, err)

			if os.Getenv("UPDATE_GOLDEN") != "" {
				require.NoError(t, os.WriteFile(goldenPath, append(got, '\n'), 0o644))
				t.Logf("updated golden file: %s", goldenPath)
				return
			}

			want, err := os.ReadFile(goldenPath)
			require.NoError(t, err)
			assert.JSONEq(t, string(want), string(got),
				"golden mismatch for %s\nrun `UPDATE_GOLDEN=1 go test ./pkg/parser/... -run TestValidFixtures` to regenerate",
				f)
		})
	}
}

// =============================================================================
// TestInvalidFixtures — every .star under tests/fixtures/invalid produces
// an error matching its `# expects: <substring>` header
// =============================================================================

var posFormatRe = regexp.MustCompile(`^[^:]+:\d+:\d+: `)

func TestInvalidFixtures(t *testing.T) {
	fixtures, err := filepath.Glob("../../tests/fixtures/invalid/*.star")
	require.NoError(t, err)
	require.NotEmpty(t, fixtures)

	// Sort for stable test order.
	sort.Strings(fixtures)

	for _, f := range fixtures {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			data, err := os.ReadFile(f)
			require.NoError(t, err)
			firstLine := strings.SplitN(string(data), "\n", 2)[0]
			require.True(t, strings.HasPrefix(firstLine, "# expects:"),
				"invalid fixture %s must start with `# expects: <substring>`", f)
			expectSub := strings.TrimSpace(strings.TrimPrefix(firstLine, "# expects:"))
			require.NotEmpty(t, expectSub, "expects substring must be non-empty")

			p, err := NewParser(
				WithRoot("../../tests/fixtures/invalid"),
				WithExtensions(&fakeExtension{}, &fakeGithubExtension{}),
			)
			require.NoError(t, err)

			_, parseErr := p.ParseFile(f)
			require.Error(t, parseErr, "fixture %s must produce an error containing %q", f, expectSub)

			var pe *dag.ParseError
			var ve *dag.ValidationError
			isParseErr := errors.As(parseErr, &pe)
			isValErr := errors.As(parseErr, &ve)
			require.True(t, isParseErr || isValErr,
				"error must be *dag.ParseError or *dag.ValidationError, got %T: %v", parseErr, parseErr)

			assert.Contains(t, parseErr.Error(), expectSub,
				"fixture %s: error must contain %q\nfull error: %v", f, expectSub, parseErr)

			// D-04 format check (skipped when no position is attached —
			// some failures originate before file I/O completes).
			hasValidPos := (isParseErr && pe.Pos.IsValid()) || (isValErr && ve.Pos.IsValid())
			if hasValidPos {
				assert.Regexp(t, posFormatRe, parseErr.Error(),
					"D-04: error must format as <file>:<line>:<col>: <msg>")
			}
		})
	}
}

// =============================================================================
// TestRegistration_StaticAndDynamic — EXT-06 alias
// =============================================================================

func TestRegistration_StaticAndDynamic(t *testing.T) {
	// Static path: WithExtensions at construction.
	p1, err := NewParser(WithExtensions(&fakeExtension{}))
	require.NoError(t, err)
	_, ok := p1.registry.Get("fake_ext")
	assert.True(t, ok, "WithExtensions registers fake_ext at construction time")

	// Dynamic path: Register after NewParser.
	p2, err := NewParser()
	require.NoError(t, err)
	require.NoError(t, p2.Register(&fakeExtension{}))
	_, ok = p2.registry.Get("fake_ext")
	assert.True(t, ok, "p.Register registers fake_ext post-construction (EXT-06 dynamic)")
}

// =============================================================================
// TestLoad_SandboxedResolution — alias touching all three D-13/14/17 paths
// =============================================================================

func TestLoad_SandboxedResolution(t *testing.T) {
	// Run a quick smoke test of the load() surface. The detailed coverage
	// lives in load_test.go; this test exists so VALIDATION.md's per-task
	// command list resolves to a single test name.
	t.Run("relative", TestLoad_Relative)
	t.Run("absolute", TestLoad_Absolute)
	t.Run("traversal-rejected", TestLoad_TraversalRejected)
}
