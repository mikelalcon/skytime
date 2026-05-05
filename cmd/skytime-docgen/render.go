package main

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/template"
)

// RenderMarkdown renders a []Builtin to docs/reference/builtins.md
// markdown using stdlib text/template.
//
// The output is a single page with one section per builtin in
// registration order. Two calls on identical input produce byte-equal
// output (idempotency / replay determinism).
func RenderMarkdown(out io.Writer, builtins []Builtin) error {
	rendered := derive(builtins)
	tmpl, err := template.New("builtins").Parse(builtinsTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}
	return tmpl.Execute(out, struct{ Builtins []renderedBuiltin }{rendered})
}

// RenderMarkdownString is a convenience wrapper used by tests.
func RenderMarkdownString(builtins []Builtin) (string, error) {
	var buf bytes.Buffer
	if err := RenderMarkdown(&buf, builtins); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// renderedBuiltin is the per-builtin shape consumed by the template. Fields
// are pre-derived from Builtin so the template body stays simple (no helper
// funcs, no nested ranges with conditional logic).
type renderedBuiltin struct {
	Name          string
	Summary       string // joined "\n\n" of multi-line summaries
	SignatureLine string
	Params        []renderedParam
	Returns       string
	Example       string
	See           string // empty string (renders as omitted) or "step, script"
	Since         string
}

type renderedParam struct {
	Name        string
	Type        string
	Required    bool
	Description string
}

// emDash is the U+2014 character used for empty cells in tables, mirroring
// the convention established by `skytime info` (pkg/cli/info.go).
const emDash = "—"

// derive turns []Builtin into []renderedBuiltin by reading marker keys and
// applying the param-type heuristic + positional-only-with-markers fallback.
func derive(builtins []Builtin) []renderedBuiltin {
	out := make([]renderedBuiltin, 0, len(builtins))
	for _, b := range builtins {
		r := renderedBuiltin{
			Name:    b.Name,
			Summary: joinValues(b.Markers["summary"]),
			Returns: firstValue(b.Markers["returns"], emDash),
			Example: firstValue(b.Markers["example"], ""),
			See:     firstValue(b.Markers["see"], ""),
			Since:   firstValue(b.Markers["since"], emDash),
		}
		r.Params = deriveParams(b)
		r.SignatureLine = signatureLine(b)
		out = append(out, r)
	}
	return out
}

// deriveParams populates the per-row data for the params table. Walker-Params
// (UnpackArgs) take precedence; positional-only-with-markers fallback applies
// when len(b.Params) == 0 — collects param_<name> markers, sorts them
// deterministically (sort.Strings), and emits a row each. Required=true for
// fallback rows because positional args are by definition required.
func deriveParams(b Builtin) []renderedParam {
	out := make([]renderedParam, 0, len(b.Params))
	for _, p := range b.Params {
		rp := renderedParam{
			Name:        p.Name,
			Type:        paramType(b, p),
			Required:    p.Required,
			Description: firstValue(b.Markers["desc_"+p.Name], emDash),
		}
		out = append(out, rp)
	}
	// Positional-only-with-markers fallback: when the walker yielded no
	// Params (UnpackPositionalArgs case, e.g. fail()) but the author
	// declared param_<name>/desc_<name> markers, synthesize rows from
	// those markers. We collect param_* keys, sort them deterministically
	// (sort.Strings; map iteration order is undefined in Go), and emit one
	// renderedParam each. Required=true because these params are
	// positional in the Go signature; markers are the sole metadata source.
	if len(out) == 0 {
		var paramKeys []string
		for k := range b.Markers {
			if strings.HasPrefix(k, "param_") {
				paramKeys = append(paramKeys, k)
			}
		}
		sort.Strings(paramKeys) // deterministic order; idempotency
		for _, k := range paramKeys {
			name := strings.TrimPrefix(k, "param_")
			out = append(out, renderedParam{
				Name:        name,
				Type:        firstValue(b.Markers[k], "any"),
				Required:    true,
				Description: firstValue(b.Markers["desc_"+name], emDash),
			})
		}
	}
	return out
}

// paramType applies the marker-override-then-heuristic ladder. Marker
// overrides always win. Heuristics are best-effort by Target identifier
// name; the param-marker explicit override is the recommended path for
// any param whose default type heuristic is wrong.
func paramType(b Builtin, p Param) string {
	// Marker override always wins.
	if v, ok := b.Markers["param_"+p.Name]; ok && len(v) > 0 {
		return v[0]
	}
	// Heuristic by Target identifier name.
	t := p.Target
	switch {
	case strings.HasSuffix(t, "Lst") || strings.HasSuffix(t, "List"):
		return "list"
	case strings.HasSuffix(t, "D") || strings.HasSuffix(t, "Dict"):
		return "dict"
	case strings.Contains(t, "Retry"):
		return "RetryPolicy"
	case strings.Contains(t, "Timeout"):
		return "Timeout"
	case t == "name" || t == "alias" || strings.Contains(t, "Queue") || strings.Contains(t, "Description") || strings.Contains(t, "Alias"):
		return "string"
	case t == "maxConc":
		return "int"
	}
	return "any"
}

// signatureLine builds the "name(p1, p2?, p3)" signature display. Optional
// params are suffixed with "?". Positional-only builtins (Params=[]) recover
// names from param_* markers via the same deterministic sort as deriveParams;
// fall back to "(...)" when no markers either.
func signatureLine(b Builtin) string {
	if len(b.Params) == 0 {
		// Positional-only: try to recover param names from param_* markers.
		// Same deterministic sort as deriveParams. Falls back to "(...)" if
		// no markers either.
		var paramKeys []string
		for k := range b.Markers {
			if strings.HasPrefix(k, "param_") {
				paramKeys = append(paramKeys, k)
			}
		}
		if len(paramKeys) == 0 {
			return b.Name + "(...)"
		}
		sort.Strings(paramKeys)
		names := make([]string, 0, len(paramKeys))
		for _, k := range paramKeys {
			names = append(names, strings.TrimPrefix(k, "param_"))
		}
		return b.Name + "(" + strings.Join(names, ", ") + ")"
	}
	names := make([]string, 0, len(b.Params))
	for _, p := range b.Params {
		if p.Required {
			names = append(names, p.Name)
		} else {
			names = append(names, p.Name+"?")
		}
	}
	return b.Name + "(" + strings.Join(names, ", ") + ")"
}

// joinValues joins multi-line summary values with "\n\n" (paragraph break)
// for readable output. Empty input returns empty string (caller decides
// what fallback to render).
func joinValues(vs []string) string {
	if len(vs) == 0 {
		return ""
	}
	return strings.Join(vs, "\n\n")
}

// firstValue returns the first element of vs, or fallback when empty.
func firstValue(vs []string, fallback string) string {
	if len(vs) == 0 {
		return fallback
	}
	return vs[0]
}

// builtinsTemplate is the body of docs/reference/builtins.md. Each builtin
// renders one section delimited by an "---" horizontal rule. The header
// preamble warns "Do not edit by hand" and links to ../architecture.md.
//
// Tables ALWAYS render with the full header + alignment row | --- | --- |
// --- | --- | even for single-param builtins (consistency with `skytime
// info`'s lipgloss table aesthetic). Empty cells use em-dash (U+2014).
//
// Multi-line summaries (Markers["summary"] with multiple values) are joined
// with "\n\n" (paragraph break) by joinValues before reaching the template.
const builtinsTemplate = `# Skytime Starlark Builtins Reference

> **Generated by ` + "`cmd/skytime-docgen`" + `.** Do not edit by hand —
> re-run ` + "`go generate ./pkg/parser/`" + `. See
> [docs/architecture.md](../architecture.md) for the parse/execute split
> this DSL enforces.

{{range .Builtins}}
## {{.Name}}

{{.Summary}}

**Signature:** ` + "`{{.SignatureLine}}`" + `

{{if .Params}}| Param | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
{{range .Params}}| ` + "`{{.Name}}`" + ` | {{.Type}} | {{if .Required}}yes{{else}}no{{end}} | {{.Description}} |
{{end}}{{else}}_Positional-only signature; see Example below._
{{end}}
**Returns:** {{.Returns}}

**Example:**

` + "```python" + `
{{.Example}}
` + "```" + `

{{if .See}}**See also:** {{.See}}

{{end}}**Since:** {{.Since}}

---
{{end}}`
