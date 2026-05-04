package dag

import (
	"encoding/json"
	"fmt"
	"sort"

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
	Kind        string            `json:"kind"`
	Name        string            `json:"name"`
	Inputs      map[string]string `json:"inputs,omitempty"`
	Description string            `json:"description,omitempty"` // Quick 260504-k9c
	Body        []Node            `json:"body"`
	TaskQueue   string            `json:"task_queue,omitempty"` // D3-19 (Phase 3)
}

// MarshalJSON emits a Flow with a "kind":"Flow" discriminator and a body
// whose elements each carry their own discriminator.
func (f *Flow) MarshalJSON() ([]byte, error) {
	body := f.Body
	if body == nil {
		body = []Node{}
	}
	return json.Marshal(flowJSON{
		Kind:        f.Kind(),
		Name:        f.Name,
		Inputs:      f.Inputs,
		Description: f.Description,
		Body:        body,
		TaskQueue:   f.TaskQueue,
	})
}

type stepJSON struct {
	Kind      string       `json:"kind"`
	Actions   []*ActionRef `json:"actions"`
	Retry     *RetryPolicy `json:"retry,omitempty"`
	Timeout   *Timeout     `json:"timeout,omitempty"`
	TaskQueue string       `json:"task_queue,omitempty"` // D3-19 (Phase 3)
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
		Kind:      s.Kind(),
		Actions:   actions,
		Retry:     s.Retry,
		Timeout:   s.Timeout,
		TaskQueue: s.TaskQueue,
	})
}

type ifCondJSON struct {
	Kind        string `json:"kind"`
	LambdaID    string `json:"lambda_id"`
	OutputAlias string `json:"output_alias,omitempty"` // D4.2-01: expression-mode marker; zero value omitted
	Then        []Node `json:"then"`
	Else        []Node `json:"else"`
}

// MarshalJSON emits an IfCond with a "kind":"IfCond" discriminator. Both
// branches always render (as `[]` when nil/empty) so the golden shape is
// stable regardless of whether `else_=` was provided. OutputAlias is
// emitted only when non-empty (omitempty) so existing procedural-mode
// fixtures and golden tests keep their current shape.
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
		Kind:        n.Kind(),
		LambdaID:    n.LambdaID,
		OutputAlias: n.OutputAlias,
		Then:        then,
		Else:        els,
	})
}

// resultJSON is the marshal-time shape of *Result. Values + Types are
// deliberately omitted in v1 golden output:
//   - Values holds *CapturedLambda pointers whose IDs are content-hash
//     suffixed (cosmetic edits change them); not stable for goldens.
//   - Types holds parser.TypeInfo values via map[string]any indirection;
//     a JSON shape with kind discriminators is out of scope for v1
//     (Pitfall 8 deferred). Plan 02+ may extend with a stable encoding
//     when the validator wires up.
type resultJSON struct {
	Kind string   `json:"kind"`
	Keys []string `json:"keys"`
}

// MarshalJSON emits a Result with a "kind":"Result" discriminator. Keys
// always renders (as `[]` when nil) for byte-stable golden tests.
func (n *Result) MarshalJSON() ([]byte, error) {
	keys := n.Keys
	if keys == nil {
		keys = []string{}
	}
	return json.Marshal(resultJSON{
		Kind: n.Kind(),
		Keys: keys,
	})
}

// failJSON is the marshal-time shape of *Fail. MessageFn collapses to its
// stable lambda ID (MessageLambdaID) — the *CapturedLambda pointer itself
// is in-memory only.
type failJSON struct {
	Kind            string `json:"kind"`
	Message         string `json:"message,omitempty"`
	MessageLambdaID string `json:"message_lambda_id,omitempty"`
}

// MarshalJSON emits a Fail with a "kind":"Fail" discriminator. Message
// renders verbatim (the literal template, including any ${...} markers).
// When MessageFn != nil, the lambda's ID is emitted as message_lambda_id
// so cross-machine goldens stay stable (the *CapturedLambda struct
// itself is not JSON-friendly).
func (n *Fail) MarshalJSON() ([]byte, error) {
	out := failJSON{
		Kind:    n.Kind(),
		Message: n.Message,
	}
	if n.MessageFn != nil {
		out.MessageLambdaID = n.MessageFn.ID
	}
	return json.Marshal(out)
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
	Kind           string       `json:"kind"`
	ItemsLambdaID  string       `json:"items_lambda_id,omitempty"`
	ItemsLiteral   []any        `json:"items_literal,omitempty"`
	ItemVar        string       `json:"item_var"`
	Steps          []Node       `json:"steps"`
	MaxConcurrency int          `json:"max_concurrency,omitempty"` // D3-13 (Phase 3)
	Retry          *RetryPolicy `json:"retry,omitempty"`
	Timeout        *Timeout     `json:"timeout,omitempty"`
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
		Kind:           n.Kind(),
		ItemsLambdaID:  n.ItemsLambdaID,
		ItemsLiteral:   n.ItemsLiteral,
		ItemVar:        n.ItemVar,
		Steps:          steps,
		MaxConcurrency: n.MaxConcurrency,
		Retry:          n.Retry,
		Timeout:        n.Timeout,
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

// UnmarshalJSON is the inverse of ActionRef.MarshalJSON: it reads the
// {kind, kwargs, credential_id} envelope produced by Marshal and reconstructs
// an ActionRef. The Kwargs map is converted back to a *starlark.Dict so the
// resulting ActionRef satisfies the Phase 2 activity contract
// (ExecuteBatch reads ref.Kwargs as a *starlark.Dict).
//
// Pos is NOT recovered (it was never serialized — see the file header comment
// on Pos exclusion); the resulting ActionRef has a zero syntax.Position. This
// is acceptable for the runtime path: position attribution happens at parse
// time, and the activity layer attributes errors by action index, not source
// position.
//
// Limitations: kwarg values must be JSON primitives (string, bool, number)
// because that is what MarshalJSON's starlarkValueToGo emits. Nested objects /
// arrays will deserialize to map/slice and goValueToStarlark falls through
// to a String coercion — adequate for Phase 2 tests which use empty or
// primitive-keyed kwargs; Phase 3 may extend goValueToStarlark to handle
// richer types as the corpus grows.
func (a *ActionRef) UnmarshalJSON(data []byte) error {
	var raw actionRefJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	a.Kind_ = raw.Kind
	a.CredentialID = raw.CredentialID
	a.Kwargs = starlark.NewDict(len(raw.Kwargs))
	// Determinism (Plan 04.1-05b W8): Go map range is randomized, so iterating
	// raw.Kwargs directly would yield a *starlark.Dict whose insertion-order
	// (and therefore Items() iteration order) varies across activity-boundary
	// round-trips, breaking replay-byte-equal expectations even when the wire
	// JSON is canonical (encoding/json sorts map keys on Marshal). We sort the
	// keys before SetKey so the rebuilt Dict has stable insertion order
	// matching the wire ordering.
	keys := make([]string, 0, len(raw.Kwargs))
	for k := range raw.Kwargs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sv := goValueToStarlark(raw.Kwargs[k])
		if err := a.Kwargs.SetKey(starlark.String(k), sv); err != nil {
			return err
		}
	}
	return nil
}

// goValueToStarlark is the inverse of starlarkValueToGo for the primitive
// types JSON unmarshal produces. encoding/json decodes numbers as float64 by
// default; we coerce integer-valued floats to starlark.Int for fidelity with
// the parse-time shape (parser keyword args produce starlark.Int for integer
// literals).
//
// Unknown / nested types fall through to a starlark.String coercion of the
// fmt-printed form — best-effort for Phase 2; Phase 3 extends.
func goValueToStarlark(v any) starlark.Value {
	switch x := v.(type) {
	case nil:
		return starlark.None
	case string:
		return starlark.String(x)
	case bool:
		return starlark.Bool(x)
	case float64:
		// JSON numbers come in as float64; preserve integer-shaped values.
		if x == float64(int64(x)) {
			return starlark.MakeInt64(int64(x))
		}
		return starlark.Float(x)
	case int:
		return starlark.MakeInt(x)
	case int64:
		return starlark.MakeInt64(x)
	default:
		return starlark.String(fmt.Sprintf("%v", v))
	}
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
