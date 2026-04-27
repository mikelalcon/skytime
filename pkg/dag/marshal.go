package dag

import (
	"encoding/json"

	"go.starlark.net/starlark"
)

// JSON marshaling for the six DAG node types and ActionRef.
//
// Every type emits a `"kind"` discriminator that matches Node.Kind() (or, for
// ActionRef, the operation fingerprint). Plan 05's golden-file tests assert
// byte-stable output across two marshals of the same value; the alias-shadow
// pattern below ensures the embedded fields serialize via Go's default
// encoder (which sorts map keys).
//
// IMPORTANT (cross-machine stability):
// We deliberately do NOT include the embedded Pos field in marshaled output.
// syntax.Position carries an absolute Filename which differs between
// machines, breaking golden stability across CI/laptop boundaries. Position
// is a parse-time concern (used by D-04 errors); it is not part of the
// wire/golden contract. If a future plan needs positional info in golden
// output, add it as a relative-path-only field — do NOT serialize Pos
// directly.

// flowJSON is the marshal-time shape of *Flow. Pos and Body excluded from
// the alias path (Body needs separate handling because []Node serializes
// each element through its own MarshalJSON).
type flowJSON struct {
	Kind   string            `json:"kind"`
	Name   string            `json:"name"`
	Inputs map[string]string `json:"inputs,omitempty"`
	Body   []Node            `json:"body"`
}

// MarshalJSON emits a Flow with a "kind":"Flow" discriminator and a body
// whose elements each carry their own discriminator.
func (f *Flow) MarshalJSON() ([]byte, error) {
	body := f.Body
	if body == nil {
		body = []Node{}
	}
	return json.Marshal(flowJSON{
		Kind:   f.Kind(),
		Name:   f.Name,
		Inputs: f.Inputs,
		Body:   body,
	})
}

type stepJSON struct {
	Kind    string       `json:"kind"`
	Actions []*ActionRef `json:"actions"`
	Retry   *RetryPolicy `json:"retry,omitempty"`
	Timeout *Timeout     `json:"timeout,omitempty"`
}

// MarshalJSON emits a Step with a "kind":"Step" discriminator. Actions
// always renders (as `[]` when nil) so plan 05's golden tests assert a
// stable shape.
func (s *Step) MarshalJSON() ([]byte, error) {
	actions := s.Actions
	if actions == nil {
		actions = []*ActionRef{}
	}
	return json.Marshal(stepJSON{
		Kind:    s.Kind(),
		Actions: actions,
		Retry:   s.Retry,
		Timeout: s.Timeout,
	})
}

type ifCondJSON struct {
	Kind     string `json:"kind"`
	LambdaID string `json:"lambda_id"`
	Then     []Node `json:"then"`
	Else     []Node `json:"else"`
}

// MarshalJSON emits an IfCond with a "kind":"IfCond" discriminator. Both
// branches always render (as `[]` when nil/empty) so the golden shape is
// stable regardless of whether `else_=` was provided.
func (n *IfCond) MarshalJSON() ([]byte, error) {
	then := n.Then
	if then == nil {
		then = []Node{}
	}
	els := n.Else
	if els == nil {
		els = []Node{}
	}
	return json.Marshal(ifCondJSON{
		Kind:     n.Kind(),
		LambdaID: n.LambdaID,
		Then:     then,
		Else:     els,
	})
}

type scriptJSON struct {
	Kind        string `json:"kind"`
	ID          string `json:"id"`
	LambdaID    string `json:"lambda_id"`
	OutputAlias string `json:"output_alias"`
}

// MarshalJSON emits a Script with a "kind":"Script" discriminator.
func (n *Script) MarshalJSON() ([]byte, error) {
	return json.Marshal(scriptJSON{
		Kind:        n.Kind(),
		ID:          n.ID,
		LambdaID:    n.LambdaID,
		OutputAlias: n.OutputAlias,
	})
}

type forEachParallelJSON struct {
	Kind          string       `json:"kind"`
	ItemsLambdaID string       `json:"items_lambda_id,omitempty"`
	ItemsLiteral  []any        `json:"items_literal,omitempty"`
	ItemVar       string       `json:"item_var"`
	Steps         []Node       `json:"steps"`
	Retry         *RetryPolicy `json:"retry,omitempty"`
	Timeout       *Timeout     `json:"timeout,omitempty"`
}

// MarshalJSON emits a ForEachParallel with a "kind":"ForEachParallel"
// discriminator. Exactly one of items_lambda_id or items_literal is set in
// well-formed nodes (see Validate()).
func (n *ForEachParallel) MarshalJSON() ([]byte, error) {
	steps := n.Steps
	if steps == nil {
		steps = []Node{}
	}
	return json.Marshal(forEachParallelJSON{
		Kind:          n.Kind(),
		ItemsLambdaID: n.ItemsLambdaID,
		ItemsLiteral:  n.ItemsLiteral,
		ItemVar:       n.ItemVar,
		Steps:         steps,
		Retry:         n.Retry,
		Timeout:       n.Timeout,
	})
}

type callFlowJSON struct {
	Kind         string         `json:"kind"`
	Name         string         `json:"name"`
	Inputs       map[string]any `json:"inputs,omitempty"`
	ChildOptions map[string]any `json:"child_options,omitempty"`
}

// MarshalJSON emits a CallFlow with a "kind":"CallFlow" discriminator. The
// Resolved pointer is intentionally never marshaled — it's tagged json:"-"
// on the struct, but we also build a separate marshal-time view here to keep
// the JSON keys stable + lower-cased.
func (n *CallFlow) MarshalJSON() ([]byte, error) {
	return json.Marshal(callFlowJSON{
		Kind:         n.Kind(),
		Name:         n.Name,
		Inputs:       n.Inputs,
		ChildOptions: n.ChildOptions,
	})
}

// actionRefJSON is the marshal-time shape of *ActionRef. The Pos field is
// deliberately excluded — Pos.Filename is absolute and breaks
// cross-machine golden stability.
type actionRefJSON struct {
	Kind         string         `json:"kind"`
	Kwargs       map[string]any `json:"kwargs"`
	CredentialID string         `json:"credential_id,omitempty"`
}

// MarshalJSON renders an ActionRef as a kind+kwargs+credential_id object.
// Kwargs are converted from *starlark.Dict to a Go map[string]any so the
// standard encoder produces deterministic output (encoding/json sorts map
// keys alphabetically). Phase 1 covers the basic kwarg types our fixtures
// use (String, Int, Bool, Float); richer types fall through to v.String()
// and Phase 3 will extend as needed.
func (a *ActionRef) MarshalJSON() ([]byte, error) {
	kw := map[string]any{}
	if a.Kwargs != nil {
		for _, item := range a.Kwargs.Items() {
			ks, ok := item[0].(starlark.String)
			if !ok {
				// Non-string keys: skip with a placeholder; not expected in
				// any DSL we accept (kwargs are always identifier-keyed).
				continue
			}
			kw[string(ks)] = starlarkValueToGo(item[1])
		}
	}
	return json.Marshal(actionRefJSON{
		Kind:         a.Kind_,
		Kwargs:       kw,
		CredentialID: a.CredentialID,
	})
}

// starlarkValueToGo converts a Starlark value to its Go equivalent for JSON
// marshaling. Phase 1 handles the primitive types our fixtures use; Phase 3
// will extend this to richer cases (nested Dict/List, etc.).
func starlarkValueToGo(v starlark.Value) any {
	switch x := v.(type) {
	case starlark.String:
		return string(x)
	case starlark.Bool:
		return bool(x)
	case starlark.Int:
		if i, ok := x.Int64(); ok {
			return i
		}
		return x.String()
	case starlark.Float:
		return float64(x)
	default:
		return v.String()
	}
}
