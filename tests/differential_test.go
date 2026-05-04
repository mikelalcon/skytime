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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	sdkactivity "go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/activity"
	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	httpext "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
	"github.com/mikelalcon/skytime/pkg/interpreter"
	"github.com/mikelalcon/skytime/pkg/parser"
	"github.com/mikelalcon/skytime/pkg/validator"
	"github.com/mikelalcon/skytime/pkg/validator/dryrun"
)

// expectedErrFlows enumerates flow names whose dry-run under
// stubInitState is expected to raise a *temporal.ApplicationError
// (top-level fail() exercised by stub seeds). Phase 04.2 introduced
// the first two via examples/skeleton/expression_if.star; future
// fixtures with similar shapes extend this map.
//
// Flows NOT listed here keep the strict NoError assertion — the
// regression-protection net for every fixture that should dry-run
// cleanly under stubInitState.
var expectedErrFlows = map[string]bool{
	"procedural_demo": true, // examples/skeleton/expression_if.star — repo="" → fail("repo input is required")
	"check_user":      true, // examples/skeleton/expression_if.star — user_id="" → fail("invalid user_id: ...")
}

// corpusExtensions returns the extension list the differential test
// injects into both static + dry-run sides. Wave 4 (plan 04-07)
// populated this with the baked-in HTTP extension; the examples/skeleton/
// fixtures use http.endpoint(...) exclusively, so this list is the
// single registration point that makes the test exercise the corpus.
//
// To extend the corpus with new extensions, append them here.
func corpusExtensions(t *testing.T) []extension.Extension {
	t.Helper()
	return []extension.Extension{httpext.New()}
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
			InitState:   stubInitState(flows[name]),
		})
		if !env.IsWorkflowCompleted() {
			return false, errors.New("workflow did not complete: " + name)
		}
		werr := env.GetWorkflowError()
		if expectedErrFlows[name] {
			// Phase 04.2 D4.2-05/D4.2-15: flows whose stubInitState
			// path exercises top-level fail() raise a
			// NonRetryableApplicationError by design. The corpus's
			// agreement check is parse + dry-run structural
			// agreement; a deterministic *temporal.ApplicationError
			// is a valid outcome. Existing fixtures (parallel_fanout.star,
			// simple_check.star, etc.) are NOT in expectedErrFlows so
			// their strict NoError assertion is preserved.
			if werr == nil {
				return false, fmt.Errorf("flow %q expected to fail (in expectedErrFlows) but completed without error", name)
			}
			var appErr *temporal.ApplicationError
			if !errors.As(werr, &appErr) {
				return false, fmt.Errorf("flow %q: expected *temporal.ApplicationError, got %T: %w", name, werr, werr)
			}
			// Acceptable — deterministic Application error.
		} else if werr != nil {
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
//
// Empty IDs (anonymous endpoints — fixtures that use http.endpoint
// without a credential= arg) return (nil, nil) so the activity's
// runAction proceeds with cred=nil. Non-empty IDs return an explicit
// "should-not-be-called" error: real credentials should never reach
// the dry-run path because AlwaysOkDispatch's wrapped Func ignores
// them, but activity-side credential resolution happens BEFORE Func
// dispatch, so a non-empty ID surfacing here means the corpus has
// drifted away from the "anonymous endpoints only" assumption — fail
// loudly rather than silently succeeding.
type noopCredentialHandler struct{}

func (noopCredentialHandler) Resolve(_ context.Context, id string) (extension.Credential, error) {
	if id == "" {
		return nil, nil
	}
	return nil, errors.New("noopCredentialHandler.Resolve called with non-empty id — corpus uses a credential the dry-run path cannot resolve: " + id)
}

// sha256Hex matches pkg/worker/boot.go's content_hash format.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// stubInitState seeds InitState from a flow's declared inputs so the
// dry-run path's lambdas see a populated ctx. Maps each declared input
// name to a stub value chosen by the type-hint string. The static
// validator (D4-02) accepts any ctx.<input_name> reference; the dry-run
// path needs the keys to exist on the runtime ctx struct so the lambdas
// don't fail with "struct has no .<name> attribute".
//
// This mirrors what `skytime run --input=<json>` would pass at the
// CLI; the differential test substitutes a deterministic stub so the
// corpus does not need to ship per-flow input fixtures.
func stubInitState(f *dag.Flow) map[string]any {
	if f == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(f.Inputs))
	for name, hint := range f.Inputs {
		switch hint {
		case "int":
			out[name] = int64(0)
		case "bool":
			out[name] = false
		case "list":
			out[name] = []any{}
		case "dict":
			out[name] = map[string]any{}
		default: // "string" or unknown → empty string
			out[name] = ""
		}
	}
	return out
}

// findModuleRootCLI is shared with firewall_cli_test.go in the same
// package — defined there.
