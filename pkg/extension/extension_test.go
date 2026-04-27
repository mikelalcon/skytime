package extension

import (
	"context"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
)

// ============================================================================
// Compile-time interface assertion — *fakeExtension MUST satisfy Extension.
// If this fails to compile, Extension's method set drifted from the contract.
// ============================================================================

var _ Extension = (*fakeExtension)(nil)

// fakeExtension is a minimal test-only implementor of Extension. It exposes the
// Name/Initialize/Operations contract without any real I/O so we can verify the
// interface signature.
type fakeExtension struct {
	name string
	ops  map[string]*OperationSpec
}

func (e *fakeExtension) Name() string { return e.name }

func (e *fakeExtension) Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error) {
	// Phase 1: a minimal HasAttrs value is enough — the schema test in
	// task 3 will exercise the *starlarkstruct.Module path.
	return starlark.None, nil
}

func (e *fakeExtension) Operations() map[string]*OperationSpec { return e.ops }

func TestExtension_FakeImplementorExposesName(t *testing.T) {
	ext := &fakeExtension{name: "github", ops: map[string]*OperationSpec{}}
	assert.Equal(t, "github", ext.Name())
	assert.NotNil(t, ext.Operations())
}

func TestExtension_InitializeReturnsStarlarkValue(t *testing.T) {
	ext := &fakeExtension{name: "github"}
	v, err := ext.Initialize(&starlark.Thread{Name: "test"}, nil)
	require.NoError(t, err)
	assert.NotNil(t, v)
}

// TestNoTemporalImportsInExtensionPackage walks every Go source file in the
// pkg/extension directory and asserts none of them import any package under
// the go.temporal.io/ namespace. This is the firewall that EXT-03 requires:
// pkg/extension is the SDK contract; importing the Temporal SDK here would
// re-introduce the workflow.Context/activity coupling the project explicitly
// forbids (see PROJECT.md "no context bleed").
func TestNoTemporalImportsInExtensionPackage(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, parser.ImportsOnly)
	require.NoError(t, err)
	require.NotEmpty(t, pkgs, "expected at least one package in pkg/extension")

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
	t.Logf("checked %d import paths across pkg/extension; none contained go.temporal.io", checked)
}

// Compile-time test of OperationFunc's exact signature. If the assignment
// below stops compiling, the OperationFunc type drifted away from the
// (context.Context, args any, cred Credential) → (any, error) contract.
//
// EXT-03: OperationFunc takes context.Context (stdlib), NEVER workflow.Context.
func TestOperationFunc_SignatureCompiles(t *testing.T) {
	var fn OperationFunc = func(ctx context.Context, args any, cred Credential) (any, error) {
		_ = ctx
		_ = args
		_ = cred
		return nil, nil
	}
	require.NotNil(t, fn)
	out, err := fn(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Nil(t, out)
}
