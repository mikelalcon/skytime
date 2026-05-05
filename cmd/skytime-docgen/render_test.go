package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderMarkdown_BuiltinSection(t *testing.T) {
	b := []Builtin{{
		Name:     "flow",
		Function: "builtinFlow",
		Params:   []Param{{Name: "name", Required: true, Target: "name"}},
		Markers: map[string][]string{
			"summary": {"Declares a workflow."},
			"returns": {"None (parse-time side effect)"},
			"since":   {"phase-01"},
			"example": {"flow(name=\"f\", inputs={}, steps=[])"},
			"see":     {"step, script"},
		},
	}}
	out, err := RenderMarkdownString(b)
	if err != nil {
		t.Fatalf("RenderMarkdownString: %v", err)
	}
	for _, want := range []string{
		"## flow",
		"**Signature:** `flow(name)`",
		"| Param | Type | Required | Description |",
		"| `name` | string | yes |",
		"**Returns:** None (parse-time side effect)",
		"**Example:**",
		"```python",
		"**See also:** step, script",
		"**Since:** phase-01",
		"---",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q;\nfull:\n%s", want, out)
		}
	}
}

func TestRenderMarkdown_MultilineSummary(t *testing.T) {
	b := []Builtin{{
		Name: "x",
		Markers: map[string][]string{
			"summary": {"line one", "line two"},
			"returns": {"r"},
			"since":   {"phase-01"},
			"example": {"e"},
		},
	}}
	out, err := RenderMarkdownString(b)
	if err != nil {
		t.Fatalf("RenderMarkdownString: %v", err)
	}
	// Multi-line summaries are joined with "\n\n" (paragraph break).
	want := "line one\n\nline two"
	if !strings.Contains(out, want) {
		t.Errorf("output missing paragraph-break-joined summary %q;\nfull:\n%s", want, out)
	}
}

func TestRenderMarkdown_PositionalOnly_NoMarkers(t *testing.T) {
	b := []Builtin{{
		Name:    "weird",
		Markers: map[string][]string{
			// no param_* markers at all; no walker Params either
			"summary": {"x"},
			"returns": {"y"},
			"since":   {"z"},
			"example": {"e"},
		},
	}}
	out, err := RenderMarkdownString(b)
	if err != nil {
		t.Fatalf("RenderMarkdownString: %v", err)
	}
	if !strings.Contains(out, "_Positional-only signature; see Example below._") {
		t.Errorf("expected positional-only sentinel; got:\n%s", out)
	}
	// Sanity: the table header should NOT appear for positional-only-no-markers
	if strings.Contains(out, "| Param | Type | Required | Description |") {
		t.Errorf("did not expect param table for positional-only-no-markers; got:\n%s", out)
	}
}

func TestRenderMarkdown_PositionalOnly_WithMarkers(t *testing.T) {
	// Mirrors fail() shape: walker Params=[] (UnpackPositionalArgs) but
	// param_message + desc_message markers declared.
	b := []Builtin{{
		Name: "fail",
		Markers: map[string][]string{
			"summary":      {"raises NonRetryableErr"},
			"returns":      {"a *dag.Fail node"},
			"since":        {"phase-04.2"},
			"example":      {"fail(\"bad\")"},
			"param_message": {"string"},
			"desc_message":  {"the error message"},
		},
	}}
	out, err := RenderMarkdownString(b)
	if err != nil {
		t.Fatalf("RenderMarkdownString: %v", err)
	}
	// Sentinel should NOT appear because we have param_* markers.
	if strings.Contains(out, "_Positional-only signature; see Example below._") {
		t.Errorf("did not expect positional-only sentinel; markers should synthesize a row;\n%s", out)
	}
	for _, want := range []string{
		"| Param | Type | Required | Description |",
		"| `message` | string | yes | the error message |",
		"**Signature:** `fail(message)`",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q;\nfull:\n%s", want, out)
		}
	}
}

func TestRenderMarkdown_PositionalOnly_WithMarkers_Idempotent(t *testing.T) {
	// Multiple param_* markers must render in deterministic (alphabetic)
	// order via sort.Strings; two runs must produce byte-equal output.
	b := []Builtin{{
		Name: "multi",
		Markers: map[string][]string{
			"summary":  {"x"},
			"returns":  {"y"},
			"since":    {"z"},
			"example":  {"e"},
			"param_b":  {"any"},
			"param_a":  {"any"},
			"param_c":  {"any"},
			"desc_a":   {"first"},
			"desc_b":   {"second"},
			"desc_c":   {"third"},
		},
	}}
	a1, err := RenderMarkdownString(b)
	if err != nil {
		t.Fatalf("RenderMarkdownString a1: %v", err)
	}
	a2, err := RenderMarkdownString(b)
	if err != nil {
		t.Fatalf("RenderMarkdownString a2: %v", err)
	}
	if a1 != a2 {
		t.Fatalf("non-deterministic render: bytes differ between runs (positional-only-with-markers)")
	}
	// And confirm alphabetic order: a row should appear before b before c.
	idxA := strings.Index(a1, "| `a` |")
	idxB := strings.Index(a1, "| `b` |")
	idxC := strings.Index(a1, "| `c` |")
	if idxA < 0 || idxB < 0 || idxC < 0 {
		t.Fatalf("missing one of the param rows; idxA=%d idxB=%d idxC=%d\n%s", idxA, idxB, idxC, a1)
	}
	if !(idxA < idxB && idxB < idxC) {
		t.Errorf("rows not in alphabetic order: a=%d b=%d c=%d", idxA, idxB, idxC)
	}
}

func TestRender_NoHeuristicFallbackForV1Builtins(t *testing.T) {
	// Live-source assertion: every Param.Name on the 8 v1 builtins should
	// have an explicit param_<Name> marker entry — i.e., none of them fall
	// through to the heuristic Target-name path. Failure names the missing
	// builtin+param pair.
	root := findModuleRoot(t)
	globalsPath := filepath.Join(root, "pkg", "parser", "globals.go")
	builtinsPath := filepath.Join(root, "pkg", "parser", "builtins.go")

	registry, order, err := WalkRegistry(globalsPath)
	if err != nil {
		t.Fatalf("WalkRegistry: %v", err)
	}
	out, err := WalkBuiltins(builtinsPath, registry, order)
	if err != nil {
		t.Fatalf("WalkBuiltins: %v", err)
	}
	for _, b := range out {
		for _, p := range b.Params {
			vs, ok := b.Markers["param_"+p.Name]
			if !ok || len(vs) == 0 {
				t.Errorf("builtin %q param %q has no explicit param_<name> marker; add one in pkg/parser/builtins.go above %s", b.Name, p.Name, b.Function)
			}
		}
	}
}

func TestRenderMarkdown_RegistrationOrderPreserved(t *testing.T) {
	b := []Builtin{
		{Name: "flow", Markers: map[string][]string{"summary": {"a"}, "returns": {"r"}, "since": {"s"}, "example": {"e"}}},
		{Name: "step", Markers: map[string][]string{"summary": {"a"}, "returns": {"r"}, "since": {"s"}, "example": {"e"}}},
		{Name: "if_cond", Markers: map[string][]string{"summary": {"a"}, "returns": {"r"}, "since": {"s"}, "example": {"e"}}},
	}
	out, err := RenderMarkdownString(b)
	if err != nil {
		t.Fatalf("RenderMarkdownString: %v", err)
	}
	idxFlow := strings.Index(out, "## flow")
	idxStep := strings.Index(out, "## step")
	idxIfCond := strings.Index(out, "## if_cond")
	if idxFlow < 0 || idxStep < 0 || idxIfCond < 0 {
		t.Fatalf("missing one of the section headings; flow=%d step=%d if_cond=%d\n%s", idxFlow, idxStep, idxIfCond, out)
	}
	if !(idxFlow < idxStep && idxStep < idxIfCond) {
		t.Errorf("registration order not preserved: flow=%d step=%d if_cond=%d", idxFlow, idxStep, idxIfCond)
	}
}

func TestRenderMarkdown_EmDashForEmptyCells(t *testing.T) {
	// A param with no desc_<name> marker AND no recognized Target should
	// render Description = "—" (U+2014).
	b := []Builtin{{
		Name:    "x",
		Params:  []Param{{Name: "weirdParam", Required: true, Target: "unknownTarget"}},
		Markers: map[string][]string{"summary": {"a"}, "returns": {"r"}, "since": {"s"}, "example": {"e"}},
	}}
	out, err := RenderMarkdownString(b)
	if err != nil {
		t.Fatalf("RenderMarkdownString: %v", err)
	}
	// Em-dash character (U+2014, single character).
	const wantEmDash = "—"
	// We should see the em-dash in the row.
	if !strings.Contains(out, "| `weirdParam` | any | yes | "+wantEmDash+" |") {
		t.Errorf("expected em-dash sentinel for empty Description; got:\n%s", out)
	}
}

func TestRenderMarkdown_TwoRunsByteIdentical(t *testing.T) {
	b := []Builtin{{
		Name:    "step",
		Markers: map[string][]string{"summary": {"x"}, "returns": {"y"}, "since": {"z"}, "example": {"e"}},
	}}
	a, _ := RenderMarkdownString(b)
	c, _ := RenderMarkdownString(b)
	if a != c {
		t.Errorf("non-deterministic render: bytes differ between runs")
	}
}

func TestRenderMarkdown_HeaderHasGenerationWarning(t *testing.T) {
	out, err := RenderMarkdownString(nil)
	if err != nil {
		t.Fatalf("RenderMarkdownString: %v", err)
	}
	// Header preamble warns "Do not edit by hand".
	if !strings.Contains(out, "Do not edit by hand") {
		t.Errorf("expected 'Do not edit by hand' warning in header preamble; got:\n%s", out)
	}
}

func TestRenderMarkdown_LinksToArchitectureDoc(t *testing.T) {
	out, err := RenderMarkdownString(nil)
	if err != nil {
		t.Fatalf("RenderMarkdownString: %v", err)
	}
	// Header preamble cross-links to ../architecture.md.
	if !strings.Contains(out, "../architecture.md") {
		t.Errorf("expected ../architecture.md cross-link in header preamble; got:\n%s", out)
	}
}
