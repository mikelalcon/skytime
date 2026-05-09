// Package httpgithubwebhook_test contains the parse + coverage tests for the
// rich example project's .star flows.
//
// TestFlows_ParseAll asserts every non-test .star file under this directory
// parses cleanly against the example's registered extension set
// (HTTP + GitHub + Webhook).
//
// TestFlows_CoverageMatrix asserts that the DAG produced for each flow uses
// the primitives claimed in CONTEXT.md D-FLOWS-COVERAGE-MATRIX. The expected
// map below is the source-of-truth that drives the README's coverage table —
// keep them in sync.
package httpgithubwebhook_test

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
	skyhttp "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
	"github.com/mikelalcon/skytime/pkg/parser"

	skygh "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/github"
	skyweb "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/webhook"
)

// exampleDir is the directory containing the .star flow files relative to
// this test file. Tests run with the test file's directory as cwd, so "."
// resolves to examples/http-github-webhook/.
const exampleDir = "."

// newExampleParser registers the example's three extensions and returns a
// fresh parser. Mirrors the wiring done at runtime by cmd/extbin (Plan 06-05).
func newExampleParser(t *testing.T) *parser.Parser {
	t.Helper()
	p, err := parser.NewParser(
		parser.WithExtensions(skyhttp.New(), skygh.New(), skyweb.New()),
	)
	require.NoError(t, err)
	return p
}

// findFlowStarFiles returns every *.star file under exampleDir EXCEPT those
// matching *_test.star (Tier-3 test fixtures land in 06-07).
func findFlowStarFiles(t *testing.T) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(exampleDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".star") {
			return nil
		}
		if strings.HasSuffix(path, "_test.star") {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no flow .star files found under %s", exampleDir)
	sort.Strings(paths)
	return paths
}

// TestFlows_ParseAll asserts every flow .star file parses without error
// against the example's registered extension set.
func TestFlows_ParseAll(t *testing.T) {
	paths := findFlowStarFiles(t)
	for _, path := range paths {
		path := path
		t.Run(filepath.Base(path), func(t *testing.T) {
			p := newExampleParser(t)
			parsed, err := p.ParseFile(path)
			require.NoError(t, err, "parse %s", path)
			require.NotEmpty(t, parsed, "%s declared no flows", path)
		})
	}
}

// TestFlows_CoverageMatrix asserts each flow's DAG contains the primitives
// claimed in CONTEXT.md D-FLOWS-COVERAGE-MATRIX. The expected maps below are
// the source-of-truth that drives the README's coverage table — when a flow's
// coverage changes, update BOTH this test AND the README.
func TestFlows_CoverageMatrix(t *testing.T) {
	// Per-flow expected primitive set. The kind names are the discriminators
	// produced by primitiveSet:
	//   - step_seq:      *dag.Step with len(Actions) == 1 (or ActionFn set)
	//   - step_block:    *dag.Step with len(Actions) > 1 (static block)
	//   - step_block_fn: *dag.Step with BlockFn set (dynamic batch)
	//   - if_cond:       *dag.IfCond
	//   - script:        *dag.Script
	//   - for_each_parallel: *dag.ForEachParallel
	//   - call_flow:     *dag.CallFlow
	expected := map[string]map[string]bool{
		"public_repo_check": {
			"step_seq":   true,
			"step_block": true,
			"if_cond":    true,
			"script":     true,
		},
		"pr_to_webhook": {
			"step_seq":          true,
			"for_each_parallel": true,
			"script":            true,
		},
		"issue_triage": {
			"step_seq":          true,
			"script":            true,
			"for_each_parallel": true,
			"call_flow":         true,
		},
		"triage_issue": {
			"step_seq":      true,
			"step_block_fn": true,
			"if_cond":       true,
			"script":        true,
		},
		"batch_label_issues": {
			"step_seq":      true,
			"step_block_fn": true,
			"script":        true,
		},
		"weekly_digest": {
			"step_seq":          true,
			"for_each_parallel": true,
			"script":            true,
		},
	}

	// Aggregate coverage across all flows MUST cover every DSL primitive
	// (D-FLOWS-COVERAGE-MATRIX success criterion: every primitive exercised).
	aggregateRequired := []string{
		"step_seq", "step_block", "step_block_fn",
		"if_cond", "script", "for_each_parallel", "call_flow",
	}

	allFlows := make(map[string]*dag.Flow)
	for _, path := range findFlowStarFiles(t) {
		p := newExampleParser(t)
		// ParseFile returns (map[string]*dag.Flow, error); both top-level
		// and any sub-flows declared in a multi-flow .star file land in this
		// map keyed by flow name.
		parsed, err := p.ParseFile(path)
		require.NoError(t, err, "parse %s", path)
		for name, fl := range parsed {
			allFlows[name] = fl
		}
	}

	for flowName, want := range expected {
		flowName := flowName
		want := want
		t.Run(flowName, func(t *testing.T) {
			fl, ok := allFlows[flowName]
			require.True(t, ok, "flow %q not found; available: %v", flowName, mapKeys(allFlows))
			got := primitiveSet(fl)
			for primitive := range want {
				assert.True(t, got[primitive],
					"flow %q expected primitive %q; got set: %v",
					flowName, primitive, sortedKeys(got))
			}
		})
	}

	agg := make(map[string]bool)
	for _, fl := range allFlows {
		for k, v := range primitiveSet(fl) {
			if v {
				agg[k] = true
			}
		}
	}
	for _, primitive := range aggregateRequired {
		assert.True(t, agg[primitive],
			"aggregate coverage missing primitive %q (across all flows); set: %v",
			primitive, sortedKeys(agg))
	}
}

// TestWebhookDemo_BootsCleanly is the EX-05 / Plan 07 D-7.1-16 acceptance
// gate for the crash-recovery demo. It asserts that webhook_demo.star:
//
//   - parses without error against the example's three-extension parser
//     session (HTTP + GitHub + Webhook);
//   - registers a flow named "webhook_demo";
//   - registers at least one trigger pointing at "webhook_demo" whose
//     Source.Kind() is "github.webhook" (i.e. the github.webhook(...)
//     factory call resolved to the concrete *githubWebhookSource type
//     shipped in Plan 03).
//
// This is the parses-and-boots gate the walkthrough docs (Plan 08)
// reference by name. It is intentionally a separate test (not folded
// into TestFlows_ParseAll) so a future regression that drops the trigger
// from the demo flow surfaces with the precise failure message rather
// than as a generic parse failure.
func TestWebhookDemo_BootsCleanly(t *testing.T) {
	const path = "webhook_demo.star"
	p := newExampleParser(t)
	parsed, err := p.ParseFile(path)
	require.NoError(t, err, "parse %s", path)

	// Flow must be registered under its declared name.
	_, ok := parsed["webhook_demo"]
	require.True(t, ok,
		"expected flow %q in parsed map; got: %v",
		"webhook_demo", mapKeys(parsed))

	// Trigger registry must contain at least one entry whose FlowName
	// is "webhook_demo" AND whose Source.Kind() is "github.webhook".
	// Both halves matter: Plan 08's walkthrough copy promises the demo
	// fires from a GitHub webhook delivery, so the kind must match.
	triggers := p.Triggers()
	require.NotEmpty(t, triggers, "expected at least one trigger registered")

	var matched bool
	for _, trig := range triggers {
		if trig.FlowName == "webhook_demo" && trig.Source.Kind() == "github.webhook" {
			matched = true
			break
		}
	}
	assert.True(t, matched,
		"expected a trigger with FlowName=%q and Source.Kind()=%q; got triggers=%+v",
		"webhook_demo", "github.webhook", triggers)
}

// primitiveSet walks a flow's Body (recursively into IfCond.Then/Else,
// ForEachParallel.Steps) and returns the set of primitive kinds present.
func primitiveSet(fl *dag.Flow) map[string]bool {
	out := map[string]bool{}
	var walk func(nodes []dag.Node)
	walk = func(nodes []dag.Node) {
		for _, n := range nodes {
			switch v := n.(type) {
			case *dag.Step:
				switch {
				case v.BlockFn != nil:
					out["step_block_fn"] = true
				case len(v.Actions) > 1:
					out["step_block"] = true
				default:
					// len(Actions) == 1 OR ActionFn != nil.
					out["step_seq"] = true
				}
			case *dag.IfCond:
				out["if_cond"] = true
				walk(v.Then)
				walk(v.Else)
			case *dag.Script:
				out["script"] = true
			case *dag.ForEachParallel:
				out["for_each_parallel"] = true
				walk(v.Steps)
			case *dag.CallFlow:
				out["call_flow"] = true
			}
		}
	}
	walk(fl.Body)
	return out
}

// mapKeys returns sorted keys of a flow map for stable error output.
func mapKeys(m map[string]*dag.Flow) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedKeys returns the truthy keys of a primitive set, sorted, for stable
// error output.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}
