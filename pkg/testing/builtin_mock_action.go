package testing

import (
	"fmt"
	"regexp"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// makeBuiltinTesterMockAction returns the Starlark builtin closure for
// tester.mock_action(extension, op, mock_fn, match). The registered
// entry lands in reg's active frame (file frame at parse time;
// per-test frame inside def test_*() once Plan 04's runner pushes
// frames). D5-B1 + D5-B5 + D5-B6.
func makeBuiltinTesterMockAction(reg *MockRegistry) func(*starlark.Thread, *starlark.Builtin, starlark.Tuple, []starlark.Tuple) (starlark.Value, error) {
	return func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("tester.mock_action: positional args not supported (use kwargs)")
		}
		var (
			extension string
			op        string
			mockFn    *starlark.Function
			match     starlark.Value = starlark.None
		)
		if err := starlark.UnpackArgs("tester.mock_action", args, kwargs,
			"extension", &extension,
			"op", &op,
			"mock_fn", &mockFn,
			"match?", &match,
		); err != nil {
			return nil, err
		}

		// D5-B3: extension="*" cross-extension wildcard is forbidden in
		// v1. Surface a clear error mentioning D5-B3 so consultants can
		// look up the decision; route through reg.Add as well so the
		// existing ErrCrossExtensionWildcard sentinel still pins the
		// registry-level invariant.
		if extension == "*" {
			return nil, fmt.Errorf("tester.mock_action: extension=\"*\" not supported in v1 (D5-B3); name a specific extension")
		}
		if extension == "" {
			return nil, fmt.Errorf("tester.mock_action: extension must be non-empty")
		}
		if op == "" {
			return nil, fmt.Errorf("tester.mock_action: op must be non-empty (use op=\"*\" for wildcard)")
		}
		if mockFn == nil {
			return nil, fmt.Errorf("tester.mock_action: mock_fn is required")
		}
		// Mock-fn signature should be (kwargs, attempt). Validate arity
		// at registration so bugs surface at parse time, not at activity
		// dispatch deep inside TestWorkflowEnvironment.
		if mockFn.NumParams() != 2 {
			return nil, fmt.Errorf("tester.mock_action: mock_fn must accept exactly 2 positional args (kwargs, attempt), got %d", mockFn.NumParams())
		}

		// Compile match map. D5-B5: regex compile-at-registration. D5-B6: values must be strings.
		compiled := map[string]*regexp.Regexp{}
		if match != nil && match != starlark.None {
			md, ok := match.(*starlark.Dict)
			if !ok {
				return nil, fmt.Errorf("tester.mock_action: match must be a dict of {key: regex_string}, got %s", match.Type())
			}
			for _, item := range md.Items() {
				k, isStr := item[0].(starlark.String)
				if !isStr {
					return nil, fmt.Errorf("tester.mock_action: match keys must be strings (D5-B6); got %s", item[0].Type())
				}
				v, isStrV := item[1].(starlark.String)
				if !isStrV {
					return nil, fmt.Errorf("tester.mock_action: match values must be strings (D5-B6); got %s for key %q", item[1].Type(), string(k))
				}
				re, err := CompileMatchRegex(string(k), string(v))
				if err != nil {
					return nil, err
				}
				compiled[string(k)] = re
			}
		}

		// Capture the mock function as dag.CapturedLambda so the router
		// can evaluate it via starlark.Call later. The ID format here is
		// stable per-position (file:line:col); Plan 04 may swap to a
		// content-hash ID once parser plumbing is wired through, but
		// router-side dispatch only reads captured.Fn (and captured.ID
		// for thread-naming), so the position-derived ID is sufficient.
		captured := &dag.CapturedLambda{
			Fn:      mockFn,
			Pos:     mockFn.Position(),
			BodyPos: mockFn.Position(),
			ID: fmt.Sprintf("mock:%s:%d:%d",
				mockFn.Position().Filename(),
				mockFn.Position().Line,
				mockFn.Position().Col),
		}

		entry := MockEntry{
			Extension:   extension,
			Op:          op,
			Match:       compiled,
			Lambda:      captured,
			RegisterPos: callerPosFromThread(thread),
		}
		if err := reg.Add(entry); err != nil {
			return nil, err
		}
		return starlark.None, nil
	}
}

// callerPosFromThread extracts the deepest user-frame position from
// thread.CallStack so the result points at the .star file's tester.*
// callsite, not the builtin internals. Mirrors Phase 04.1's
// fail()-callsite preservation pattern.
//
// Skips any frame whose name starts with "tester." (workflow,
// mock_action, run) and any <builtin> frame; the first remaining
// frame is the user's .star line.
func callerPosFromThread(thread *starlark.Thread) syntax.Position {
	depth := thread.CallStackDepth()
	// Walk innermost (depth-1) toward outermost (0); skip <builtin>
	// and any tester.* frames so the position lands on the user's
	// .star line. CallFrame indexing matches Starlark's convention:
	// CallFrame(0) is the innermost frame.
	for i := 0; i < depth; i++ {
		fr := thread.CallFrame(i)
		fname := fr.Pos.Filename()
		if fname == "" || fname == "<builtin>" {
			continue
		}
		if strings.HasPrefix(fr.Name, "tester.") {
			continue
		}
		return fr.Pos
	}
	return syntax.Position{}
}
