// This file (and firewall_cli_test.go) live in package firewall_test —
// the external test package convention used in tests/. Cross-tree
// integration tests that need to import multiple pkg/ trees AND the
// temporal SDK live here because tests/ is outside pkg/, side-stepping
// the temporal-firewall in
// pkg/activity/firewall_test.go::TestNoTemporalImportsOutsideAllowList
// (which gates only pkg/*, not tests/).
package firewall_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/activity"
	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/interpreter"
	"github.com/mikelalcon/skytime/pkg/parser"
	"github.com/mikelalcon/skytime/pkg/validator"
	"github.com/mikelalcon/skytime/pkg/validator/dryrun"
)

// corpusExtensions returns the extension list the differential test
// injects into both static + dry-run sides. Currently empty — W4
// (plan 04-07) populates this with the baked-in HTTP extension once
// pkg/extension/builtin/http lands. Until then, the corpus directory
// is empty and the test t.Skip()s.
//
// Build-tag-free: extending this slice is the W4 wiring point.
func corpusExtensions(t *testing.T) []extension.Extension {
	t.Helper()
	return nil // W4 will append httpext.New() once the extension exists
}

// TestDifferentialCorpus walks examples/skeleton/ and asserts static
// validation and dry-run interpretation agree on accept/reject for
// every .star file (VAL-02).
//
// Skip semantics: when the directory does not exist or contains no .star
// files, the test t.Skip()s with a message indicating W4 will populate
// the corpus. This lets the test land in W2 (when the differential
// infrastructure is built) without failing CI before W4 lands the
// fixtures.
func TestDifferentialCorpus(t *testing.T) {
	moduleRoot := findModuleRootCLI(t)
	corpusDir := filepath.Join(moduleRoot, "examples", "skeleton")

	if _, err := os.Stat(corpusDir); errors.Is(err, fs.ErrNotExist) {
		t.Skipf("corpus dir %s does not exist yet — W4 will populate it", corpusDir)
		return
	}

	var starFiles []string
	require.NoError(t, filepath.WalkDir(corpusDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() && filepath.Ext(path) == ".star" {
			starFiles = append(starFiles, path)
		}
		return nil
	}))
	if len(starFiles) == 0 {
		t.Skipf("no .star files in %s yet — W4 will populate the corpus", corpusDir)
		return
	}

	exts := corpusExtensions(t)

	for _, file := range starFiles {
		file := file
		t.Run(filepath.Base(file), func(t *testing.T) {
			// Static side
			staticErrs := validator.Validate(file,
				validator.WithExtensions(exts...),
				validator.WithRoot(corpusDir),
			)
			staticPassed := len(staticErrs) == 0

			// Dry-run side: parse → build registry → run SkytimeWorkflow
			// with AlwaysOkDispatch.
			dryRunPassed, dryRunErr := runDryRun(t, file, corpusDir, exts)

			if staticPassed != dryRunPassed {
				t.Fatalf("DIVERGENCE in %s\n  static_passed=%v static_errs=%v\n  dryrun_passed=%v dryrun_err=%v",
					file, staticPassed, staticErrs, dryRunPassed, dryRunErr)
			}

			// Pitfall #7: when both paths fail, neither failure is a
			// runtime panic. Both must be user-error class
			// (*dag.ParseError, *dag.ValidationError, or wrapped
			// application errors from Temporal).
			if !staticPassed && !dryRunPassed {
				for _, e := range staticErrs {
					assertNotRuntimePanic(t, "static", e)
				}
				assertNotRuntimePanic(t, "dryrun", dryRunErr)
			}
		})
	}
}

// runDryRun parses the file, builds a FlowRegistry, and runs
// SkytimeWorkflow against testsuite.TestWorkflowEnvironment for every
// declared flow. Returns (passed, firstError). Empty error means all
// flows ran cleanly. The first failure short-circuits — matches the
// static side's first-error behavior.
//
// For the corpus this iterates every flow in the file because we don't
// know the consultant's intended entry flow at validation time. A single
// flow failing fails the dry-run side.
func runDryRun(t *testing.T, file, rootDir string, exts []extension.Extension) (bool, error) {
	t.Helper()

	// Parse with the same options the worker uses.
	parserOpts := []parser.Option{parser.WithRoot(rootDir)}
	if len(exts) > 0 {
		parserOpts = append(parserOpts, parser.WithExtensions(exts...))
	}
	p, err := parser.NewParser(parserOpts...)
	if err != nil {
		return false, err
	}
	flows, err := p.ParseFile(file)
	if err != nil {
		return false, err
	}

	// Build a FlowRegistry. Compute content_hash from the parser's
	// FileBytes cache so it matches what the runtime worker would compute
	// (sha256 of file bytes, hex-encoded).
	reg := interpreter.NewRegistry()
	lambdas := p.Lambdas()
	fileBytes := p.FileBytes()
	for name, f := range flows {
		owningPath := f.Pos.Filename()
		bytes, ok := fileBytes[owningPath]
		if !ok {
			return false, errors.New("FileBytes cache missing entry for " + owningPath)
		}
		contentHash := sha256Hex(bytes)
		if err := reg.Register(name, contentHash, &interpreter.ParsedFlow{
			Flow:    f,
			Lambdas: lambdas,
		}); err != nil {
			return false, err
		}
	}
	reg.Freeze()

	// Build the activity with AlwaysOkDispatch (no real I/O).
	dispatch := dryrun.AlwaysOkDispatch(exts)
	act, err := activity.New(dispatch, noopCredentialHandler{})
	if err != nil {
		return false, err
	}

	// Run every flow through TestWorkflowEnvironment. First failing flow
	// stops the loop — same first-error behavior as the static side.
	ts := &testsuite.WorkflowTestSuite{}
	for name := range flows {
		env := ts.NewTestWorkflowEnvironment()
		env.RegisterWorkflowWithOptions(
			interpreter.NewWorkflow(reg),
			workflow.RegisterOptions{Name: "SkytimeWorkflow"},
		)
		env.RegisterActivityWithOptions(
			act.ExecuteBatch,
			sdkactivity.RegisterOptions{Name: "ExecuteBatch"},
		)

		contentHash, ok := reg.ContentHashFor(name)
		if !ok {
			return false, errors.New("ContentHashFor: no unique hash for flow " + name)
		}
		env.ExecuteWorkflow("SkytimeWorkflow", dag.WorkflowInput{
			FlowName:    name,
			ContentHash: contentHash,
			InitState:   map[string]any{},
		})
		if !env.IsWorkflowCompleted() {
			return false, errors.New("workflow did not complete: " + name)
		}
		if werr := env.GetWorkflowError(); werr != nil {
			return false, werr
		}
	}
	return true, nil
}

// assertNotRuntimePanic checks that err's chain does not contain a
// runtime.Error (Pitfall #7 in 04-RESEARCH.md). User-error classes
// (*dag.ParseError, *dag.ValidationError, Temporal application errors)
// are acceptable; nil-deref / index-out-of-bounds / etc. are bugs.
func assertNotRuntimePanic(t *testing.T, side string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var re runtime.Error
	if errors.As(err, &re) {
		t.Errorf("%s side: error chain contains runtime.Error (panic-class), should be user-error: %v", side, err)
	}
}

// noopCredentialHandler is the no-op handler used by the dry-run path.
// AlwaysOkDispatch's wrapped Func never reaches the resolver, but
// activity.New requires a non-nil handler — so Resolve returns an
// explicit "should-not-be-called" error to make any unintended call
// surface loudly.
type noopCredentialHandler struct{}

func (noopCredentialHandler) Resolve(_ context.Context, _ string) (extension.Credential, error) {
	return nil, errors.New("noopCredentialHandler.Resolve called — dry-run should not require credentials")
}

// sha256Hex matches pkg/worker/boot.go's content_hash format.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// findModuleRootCLI is shared with firewall_cli_test.go in the same
// package — defined there.
