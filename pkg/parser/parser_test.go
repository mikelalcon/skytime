package parser

import (
	"context"
	"errors"
	goparser "go/parser"
	"go/token"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/bridge"
	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// ============================================================================
// Test fixtures: reusable fake extensions
// ============================================================================

// noopOpFunc is an extension.OperationFunc that does nothing. Reused across
// fake extensions in this package's tests.
func noopOpFunc(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
	return nil, nil
}

// minimalExtension is a valid extension declaring one operation. Initialize
// returns starlark.None which is NOT attribute-bearing — used to verify
// the HasAttrs gate in newParseTimeGlobals.
type minimalExtension struct {
	name string
}

func (e *minimalExtension) Name() string { return e.name }
func (e *minimalExtension) Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}
func (e *minimalExtension) Operations() map[string]*extension.OperationSpec {
	return map[string]*extension.OperationSpec{
		"noop": {
			Name:       "noop",
			Idempotent: extension.Ptr(true),
			Func:       noopOpFunc,
			KwargsType: reflect.TypeOf(struct{}{}),
		},
	}
}

// nilIdempotentExtension declares an operation with Idempotent: nil so the
// registry rejects with ErrIdempotentRequired (D-12 enforcement).
type nilIdempotentExtension struct{}

func (*nilIdempotentExtension) Name() string { return "bad" }
func (*nilIdempotentExtension) Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error) {
	return starlark.None, nil
}
func (*nilIdempotentExtension) Operations() map[string]*extension.OperationSpec {
	return map[string]*extension.OperationSpec{
		"oops": {
			Name:       "oops",
			Idempotent: nil, // D-12 violation
			Func:       noopOpFunc,
			KwargsType: reflect.TypeOf(struct{}{}),
		},
	}
}

// ============================================================================
// TestNewParser_Defaults — NewParser produces a Parser with sensible defaults
// ============================================================================

func TestNewParser_Defaults(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, bridge.DefaultMaxExecutionSteps, p.maxExecSteps,
		"default maxExecSteps should equal bridge.DefaultMaxExecutionSteps (D-22 = 10_000_000)")
	assert.Empty(t, p.root, "default root should be empty (will fall back to .git ancestor at parse time)")
	require.NotNil(t, p.registry, "registry must be initialized so Register() works without panicking")
	assert.NotNil(t, p.loadCache, "loadCache must be initialized")
	assert.NotNil(t, p.fileBytes, "fileBytes must be initialized")
	assert.NotNil(t, p.flows, "flows must be initialized")
	assert.NotNil(t, p.lambdas, "lambdas must be initialized")
}

// ============================================================================
// TestNewParser_PropagatesRegistrationError — D-12 surfaced through NewParser
// ============================================================================

func TestNewParser_PropagatesRegistrationError(t *testing.T) {
	_, err := NewParser(WithExtensions(&nilIdempotentExtension{}))
	require.Error(t, err, "NewParser must surface ErrIdempotentRequired")
	assert.True(t, errors.Is(err, extension.ErrIdempotentRequired),
		"error must wrap extension.ErrIdempotentRequired (D-12 enforcement)")
}

// ============================================================================
// TestParse_NeverPanicsOnGarbage — PARSE-05 panic guard
// ============================================================================

func TestParse_NeverPanicsOnGarbage(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)

	cases := []struct {
		name string
		src  string
	}{
		{"random ascii noise", "@@@@@bad@@@@@"},
		{"unclosed paren", "flow(name='x', inputs={"},
		{"raw bytes", string([]byte{0xff, 0xfe, 0x00, 0x01, 0x02})},
		{"empty file", ""},
		{"only comment", "# just a comment\n"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// The whole point: must not panic. Empty and comment-only
			// files parse cleanly; the rest must surface *dag.ParseError.
			result, parseErr := p.ParseSource("garbage.star", []byte(tc.src))
			if parseErr != nil {
				var pe *dag.ParseError
				assert.True(t, errors.As(parseErr, &pe),
					"error must be *dag.ParseError, got %T: %v", parseErr, parseErr)
			} else {
				assert.NotNil(t, result, "successful parse must return a non-nil flows map")
			}
		})
	}
}

// ============================================================================
// TestParse_ErrorFormat — D-04 format <file>:<line>:<col>: <msg>
// ============================================================================

func TestParse_ErrorFormat(t *testing.T) {
	p, err := NewParser()
	require.NoError(t, err)

	src := []byte("flow(\n    name='x',\n    inputs={,\n    steps=[],\n)\n")
	_, parseErr := p.ParseSource("test.star", src)
	require.Error(t, parseErr)

	formatRe := regexp.MustCompile(`^[^:]+:\d+:\d+: `)
	assert.Regexp(t, formatRe, parseErr.Error(),
		"D-04: error must be formatted as <file>:<line>:<col>: <msg>")
}

// ============================================================================
// TestParse_UsesExecFileOptions — verify ExecFileOptions semantics are active
// ============================================================================

func TestParse_UsesExecFileOptions(t *testing.T) {
	// Indirect test: defaultFileOptions disables `while`. Try to parse a
	// `while` loop and assert it's rejected — proves ExecFileOptions
	// (not deprecated ExecFile) is in use.
	p, err := NewParser()
	require.NoError(t, err)

	src := []byte(`x = 0
while x < 3:
    x = x + 1
`)
	_, parseErr := p.ParseSource("test.star", src)
	require.Error(t, parseErr)
	assert.Contains(t, parseErr.Error(), "while",
		"FileOptions.While=false should reject while loops at parse time")
}

// ============================================================================
// TestParse_HasExecFileOptionsCallNotDeprecated — grep-style check on source
// ============================================================================

func TestParse_HasExecFileOptionsCallNotDeprecated(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := goparser.ParseDir(fset, ".", nil, goparser.ParseComments)
	require.NoError(t, err)

	foundOptions := false
	for _, pkg := range pkgs {
		for fname, file := range pkg.Files {
			if strings.HasSuffix(fname, "_test.go") {
				continue
			}
			data, err := os.ReadFile(fname)
			require.NoError(t, err, "read %s", fname)
			text := string(data)
			if strings.Contains(text, "starlark.ExecFileOptions") {
				foundOptions = true
			}
			// Reject the deprecated form: starlark.ExecFile( with open
			// paren distinguishes it from ExecFileOptions.
			if strings.Contains(text, "starlark.ExecFile(") {
				t.Errorf("file %s references deprecated starlark.ExecFile(...); use ExecFileOptions", fname)
			}
			_ = file
		}
	}
	assert.True(t, foundOptions, "at least one parser source file must reference starlark.ExecFileOptions")
}

// ============================================================================
// TestNoTemporalImportsInParserPackage — architectural firewall (PROJECT.md)
// ============================================================================

func TestNoTemporalImportsInParserPackage(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := goparser.ParseDir(fset, ".", nil, goparser.ImportsOnly)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs, "expected at least one package in pkg/parser")

	checked := 0
	for pkgName, pkg := range pkgs {
		for fname, file := range pkg.Files {
			for _, imp := range file.Imports {
				pathStr := strings.Trim(imp.Path.Value, "\"")
				require.NotContains(t, pathStr, "go.temporal.io",
					"package %s file %s imports forbidden Temporal package %s",
					pkgName, fname, pathStr)
				checked++
			}
		}
	}
	t.Logf("checked %d import paths across pkg/parser; none contained go.temporal.io", checked)
}

// ============================================================================
// TestWrapStarlarkError — typed conversion of starlark errors
// ============================================================================

func TestWrapStarlarkError_NilPassThrough(t *testing.T) {
	assert.NoError(t, wrapStarlarkError(nil))
}

func TestWrapStarlarkError_AlreadyParseError(t *testing.T) {
	// Direct ParseError: surfaces as itself (errors.As finds and returns).
	original := &dag.ParseError{Msg: "already wrapped"}
	got := wrapStarlarkError(original)
	var pe *dag.ParseError
	require.True(t, errors.As(got, &pe))
	assert.Equal(t, "already wrapped", pe.Msg)
}

func TestWrapStarlarkError_AlreadyValidationError(t *testing.T) {
	original := &dag.ValidationError{Msg: "already wrapped"}
	got := wrapStarlarkError(original)
	var ve *dag.ValidationError
	require.True(t, errors.As(got, &ve))
	assert.Equal(t, "already wrapped", ve.Msg)
}

func TestWrapStarlarkError_SyntaxError(t *testing.T) {
	// Force a real syntax.Error via the parser and verify wrapping.
	p, err := NewParser()
	require.NoError(t, err)

	_, parseErr := p.ParseSource("syntax.star", []byte("flow(\n    inputs={\n"))
	require.Error(t, parseErr)
	var pe *dag.ParseError
	require.True(t, errors.As(parseErr, &pe))
	assert.True(t, pe.Pos.IsValid(), "syntax errors must preserve their position")
}
