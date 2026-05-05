package testing

import (
	"errors"
	"fmt"
	"regexp"

	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// ErrCrossExtensionWildcard is returned by MockRegistry.Add when an
// entry uses Extension="*" (D5-B3 forbids cross-extension wildcards
// for v1).
var ErrCrossExtensionWildcard = errors.New("tester.mock_action: extension=\"*\" not supported in v1 (D5-B3)")

// MockEntry is one (extension, op) → mock_fn binding registered via
// tester.mock_action(...). Match is the optional kwargs-regex filter
// (D5-B5: compiled once at registration); empty map means "match any
// call to (extension, op)".
//
// Lambda is the captured mock function body — Phase 5 stores
// *dag.CapturedLambda so the runner can evaluate via
// bridge.CallLambda using the mock-lambda env (built in Plan 02).
type MockEntry struct {
	Extension   string
	Op          string                    // "*" for tier-3 wildcard
	Match       map[string]*regexp.Regexp // nil/empty = no kwargs filter
	Lambda      *dag.CapturedLambda       // nil permitted at construction; Plan 02 populates
	RegisterPos syntax.Position           // file:line:col of tester.mock_action call (for D5-D2 attribution)
}

// Frame is one scoping layer of the MockRegistry stack. The file frame
// is the bottom; PushTestFrame stacks per-test frames on top
// (D5-A4). Recency within a frame = entries slice index (later =
// more recent).
type Frame struct {
	Entries []MockEntry
}

// MockRegistry holds the per-test-file mock state. It owns one file
// frame plus a stack of per-test frames managed by PushTestFrame /
// PopTestFrame. Match implements the D5-B4 3-tier ladder.
//
// MockRegistry is NOT goroutine-safe. The harness drives one test at a
// time per file (D5-E5 sequential within a file); the mock callback
// runs in the activity goroutine while the workflow goroutine is
// blocked on ExecuteActivity.Get, so there is no concurrent access.
type MockRegistry struct {
	file    Frame
	perTest []Frame
}

// NewMockRegistry returns a fresh registry with an empty file frame
// and no per-test frames.
func NewMockRegistry() *MockRegistry {
	return &MockRegistry{}
}

// PushTestFrame stacks a new per-test frame (D5-A4). Called by the
// runner before invoking each def test_*().
func (r *MockRegistry) PushTestFrame() {
	r.perTest = append(r.perTest, Frame{})
}

// PopTestFrame removes the top per-test frame. Panics if the stack is
// empty (programming error in the harness).
func (r *MockRegistry) PopTestFrame() {
	if len(r.perTest) == 0 {
		panic("MockRegistry.PopTestFrame: stack empty")
	}
	r.perTest = r.perTest[:len(r.perTest)-1]
}

// Add records a new MockEntry into the active frame (per-test if any,
// else file). Returns ErrCrossExtensionWildcard if e.Extension == "*"
// (D5-B3).
func (r *MockRegistry) Add(e MockEntry) error {
	if e.Extension == "*" {
		return ErrCrossExtensionWildcard
	}
	if len(r.perTest) > 0 {
		top := &r.perTest[len(r.perTest)-1]
		top.Entries = append(top.Entries, e)
		return nil
	}
	r.file.Entries = append(r.file.Entries, e)
	return nil
}

// Match implements the D5-B4 3-tier ladder over (ref.Kind_, ref.Kwargs):
//
//	Tier 1: (ext, op) exact + non-empty Match map; ALL match keys
//	        regex-match the corresponding kwargs string value
//	Tier 2: (ext, op) exact + empty Match map
//	Tier 3: (ext, "*") wildcard, with optional Match
//
// Within a tier, entries are walked top-of-stack first (per-test
// frames over file frame), and within each frame most-recent-first
// (entries slice walked end→start). The first match wins.
//
// kwargsAsString is the string-form view of ref.Kwargs (passed in by
// the router; only string-valued kwargs participate in regex matching
// per D5-B6). Non-string kwargs are simply absent from the map; a
// match key keyed to a non-string kwarg fails the tier-1 test for
// that entry.
func (r *MockRegistry) Match(ref *dag.ActionRef, kwargsAsString map[string]string) (MockEntry, bool) {
	ext, op, ok := splitExtOp(ref.Kind_)
	if !ok {
		return MockEntry{}, false
	}

	// Build frames in walk order: per-test top-down, then file last.
	frames := make([]Frame, 0, len(r.perTest)+1)
	for i := len(r.perTest) - 1; i >= 0; i-- {
		frames = append(frames, r.perTest[i])
	}
	frames = append(frames, r.file)

	for tier := 1; tier <= 3; tier++ {
		for _, frame := range frames {
			for i := len(frame.Entries) - 1; i >= 0; i-- {
				e := frame.Entries[i]
				switch tier {
				case 1:
					if e.Extension == ext && e.Op == op && len(e.Match) > 0 && matchKwargs(e.Match, kwargsAsString) {
						return e, true
					}
				case 2:
					if e.Extension == ext && e.Op == op && len(e.Match) == 0 {
						return e, true
					}
				case 3:
					if e.Extension == ext && e.Op == "*" && matchKwargs(e.Match, kwargsAsString) {
						return e, true
					}
				}
			}
		}
	}
	return MockEntry{}, false
}

// splitExtOp parses "gh.get" into ("gh", "get", true). Returns
// ("", "", false) for malformed kinds (no dot, leading/trailing dot).
func splitExtOp(kind string) (ext, op string, ok bool) {
	for i := 0; i < len(kind); i++ {
		if kind[i] == '.' {
			if i == 0 || i == len(kind)-1 {
				return "", "", false
			}
			return kind[:i], kind[i+1:], true
		}
	}
	return "", "", false
}

// matchKwargs returns true iff every key in match has a corresponding
// string in kwargsAsString whose value is matched by the compiled
// regex (partial-match per D5-B5). An empty match map matches
// unconditionally (used by tier-3 wildcard with no kwargs filter).
func matchKwargs(match map[string]*regexp.Regexp, kwargsAsString map[string]string) bool {
	for k, re := range match {
		v, present := kwargsAsString[k]
		if !present {
			return false
		}
		if !re.MatchString(v) {
			return false
		}
	}
	return true
}

// CompileMatchRegex is the helper Plan 02's tester.mock_action calls
// at registration time per D5-B5. Surfaces a descriptive error so a
// bad pattern fails parse-time rather than at activity dispatch.
func CompileMatchRegex(key, pattern string) (*regexp.Regexp, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("tester.mock_action: match[%q] regex compile failed: %w", key, err)
	}
	return re, nil
}
