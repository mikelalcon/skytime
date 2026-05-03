package extension

import (
	"fmt"
	"reflect"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// FieldSpec describes one field in an operation's parameter struct.
// Exported so Phase 4's static validator (`skytime validate` CLI) can serialize
// the schema and so future v2 features (JSON-Schema export) can iterate it.
type FieldSpec struct {
	// GoName is the field name in the Go struct (used for error messages
	// when the Go and Starlark names differ).
	GoName string

	// StarName is the name as Starlark callers see it — the kwarg key.
	StarName string

	// Required is true iff the `,required` option was present in the
	// `star:"name,required"` tag.
	Required bool

	// GoType is the field's Go type, used for type-checking and conversion.
	GoType reflect.Type

	// FieldIdx is the field index used by reflect.Value.Field(i) when
	// populating the target struct in UnpackOperationKwargs.
	FieldIdx int
}

// ParseSchema reflects on a struct type to extract its FieldSpecs. Called once
// per OperationSpec at registration time, never per parse.
//
// Tag format: `star:"name[,required]"`. Examples:
//
//	Repo  string `star:"repo,required"`   // required kwarg "repo"
//	Body  string `star:"body"`            // optional kwarg "body"
//	Skip  string `star:"-"`               // explicit opt-out (ignored)
//	X     string                          // untagged (ignored)
//
// Returns an error if the type is not a struct (or pointer to struct) or if
// any tagged field has an empty name (e.g. `star:",required"`).
func ParseSchema(t reflect.Type) ([]FieldSpec, error) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("ParseSchema: %v is not a struct", t)
	}

	var specs []FieldSpec
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("star")
		if tag == "" || tag == "-" {
			// Untagged or explicit opt-out — skip silently. This lets
			// Go authors mix internal helper fields with the
			// kwarg-bearing ones.
			continue
		}
		parts := strings.Split(tag, ",")
		spec := FieldSpec{
			GoName:   f.Name,
			StarName: parts[0],
			GoType:   f.Type,
			FieldIdx: i,
		}
		if spec.StarName == "" {
			return nil, fmt.Errorf("ParseSchema: field %s has empty star: name (got tag %q)", f.Name, tag)
		}
		for _, opt := range parts[1:] {
			if opt == "required" {
				spec.Required = true
			}
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// UnpackOperationKwargs validates Starlark kwargs against a schema and
// populates the target struct.
//
//   - opName  is the operation name (used in error messages).
//   - callPos is the .star call-site position; attached to every error.
//   - specs   is the FieldSpec list from ParseSchema.
//   - kwargs  is the raw []starlark.Tuple as Starlark passes them.
//   - target  is a pointer to the operation's parameter struct (the same
//     struct type ParseSchema reflected on).
//
// All errors are *dag.ValidationError so callers can `errors.As` them. The
// returned error format follows D-04: "<file>:<line>:<col>: <opname>: <msg>".
func UnpackOperationKwargs(opName string, callPos syntax.Position, specs []FieldSpec, kwargs []starlark.Tuple, target any) error {
	// Build a kwarg lookup map from the raw tuples. Stops at the first
	// non-string key — Starlark allows arbitrary keys but our kwargs
	// always come in as strings.
	seen := make(map[string]starlark.Value, len(kwargs))
	for _, kv := range kwargs {
		keyStr, ok := kv[0].(starlark.String)
		if !ok {
			return &dag.ValidationError{
				Pos: callPos,
				Msg: fmt.Sprintf("%s: kwarg key must be string, got %s", opName, kv[0].Type()),
			}
		}
		seen[string(keyStr)] = kv[1]
	}

	// Required check.
	for _, spec := range specs {
		if !spec.Required {
			continue
		}
		if _, ok := seen[spec.StarName]; !ok {
			return &dag.ValidationError{
				Pos: callPos,
				Msg: fmt.Sprintf("%s: missing required kwarg %q", opName, spec.StarName),
			}
		}
	}

	// Unknown-kwarg check.
	known := make(map[string]bool, len(specs))
	for _, s := range specs {
		known[s.StarName] = true
	}
	for k := range seen {
		if !known[k] {
			return &dag.ValidationError{
				Pos: callPos,
				Msg: fmt.Sprintf("%s: unknown kwarg %q", opName, k),
			}
		}
	}

	// Populate target via reflection.
	tv := reflect.ValueOf(target).Elem()
	for _, spec := range specs {
		v, present := seen[spec.StarName]
		if !present {
			continue
		}
		if err := assignStarlarkToGo(tv.Field(spec.FieldIdx), v); err != nil {
			return &dag.ValidationError{
				Pos: callPos,
				Msg: fmt.Sprintf("%s: kwarg %q: %v", opName, spec.StarName, err),
			}
		}
	}
	return nil
}

// assignStarlarkToGo converts one Starlark value into the matching Go field.
// Phase 1 supports: string, int (and int8/16/32/64), bool, float64,
// []string, map[string]string. Starlark None on any field is a no-op (the
// Go zero value is left in place).
//
// Extension authors will extend this switch when their Phase-6 ops need
// additional types (e.g., []int, time.Duration, custom enums).
//
// D4.1-05 carve-out: a *dag.StarlarkLambda wrapper means the consultant
// supplied a ${...}-interpolated string (or, in a future plan, an
// explicit lambda) for a string-typed kwarg. The interpreter's runtime
// resolveKwargs (Plan 04.1-05a) replaces the wrapper with a real string
// before activity-side dispatch. For PARSE-time validation we accept the
// lambda for string-typed fields (leaving the Go field zero) and reject
// it for any other type — there is no implicit lambda → int / lambda →
// list coercion.
func assignStarlarkToGo(dst reflect.Value, src starlark.Value) error {
	// Starlark None → leave Go zero value.
	if src == starlark.None {
		return nil
	}

	// D4.1-05: *StarlarkLambda wrapper short-circuit. Accept for string
	// fields (zero-valued; runtime resolveKwargs fills in the resolved
	// string) and reject loudly for non-string fields so callers know
	// lambda→non-string coercion is unsupported.
	if _, isLambda := dag.UnwrapStarlarkLambda(src); isLambda {
		if dst.Kind() == reflect.String {
			return nil
		}
		return fmt.Errorf("lambda not allowed for non-string field (got lambda kwarg, declared type is %s)", dst.Kind())
	}

	switch dst.Kind() {
	case reflect.String:
		s, ok := src.(starlark.String)
		if !ok {
			return fmt.Errorf("expected string, got %s", src.Type())
		}
		dst.SetString(string(s))

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, ok := src.(starlark.Int)
		if !ok {
			return fmt.Errorf("expected int, got %s", src.Type())
		}
		v, ok := i.Int64()
		if !ok {
			return fmt.Errorf("int overflow: %s does not fit in int64", i.String())
		}
		dst.SetInt(v)

	case reflect.Bool:
		b, ok := src.(starlark.Bool)
		if !ok {
			return fmt.Errorf("expected bool, got %s", src.Type())
		}
		dst.SetBool(bool(b))

	case reflect.Float64:
		f, ok := src.(starlark.Float)
		if !ok {
			return fmt.Errorf("expected float, got %s", src.Type())
		}
		dst.SetFloat(float64(f))

	case reflect.Slice:
		// Phase 1: []string only. Extension authors add new element
		// types here as they need them.
		if dst.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported slice element type %s (only []string in phase 1)", dst.Type().Elem())
		}
		lst, ok := src.(*starlark.List)
		if !ok {
			return fmt.Errorf("expected list, got %s", src.Type())
		}
		out := reflect.MakeSlice(dst.Type(), 0, lst.Len())
		iter := lst.Iterate()
		defer iter.Done()
		var x starlark.Value
		for iter.Next(&x) {
			s, ok := x.(starlark.String)
			if !ok {
				return fmt.Errorf("expected string in list, got %s", x.Type())
			}
			out = reflect.Append(out, reflect.ValueOf(string(s)))
		}
		dst.Set(out)

	case reflect.Map:
		// Phase 1: map[string]string only.
		if dst.Type().Key().Kind() != reflect.String || dst.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("unsupported map type %s (only map[string]string in phase 1)", dst.Type())
		}
		d, ok := src.(*starlark.Dict)
		if !ok {
			return fmt.Errorf("expected dict, got %s", src.Type())
		}
		out := reflect.MakeMapWithSize(dst.Type(), d.Len())
		for _, item := range d.Items() {
			k, ok := item[0].(starlark.String)
			if !ok {
				return fmt.Errorf("expected string key, got %s", item[0].Type())
			}
			v, ok := item[1].(starlark.String)
			if !ok {
				return fmt.Errorf("expected string value, got %s", item[1].Type())
			}
			out.SetMapIndex(reflect.ValueOf(string(k)), reflect.ValueOf(string(v)))
		}
		dst.Set(out)

	default:
		return fmt.Errorf("unsupported field type %s (extend assignStarlarkToGo)", dst.Kind())
	}
	return nil
}

// DecodeKwargsFromDict decodes a frozen Starlark *Dict (typically
// ActionRef.Kwargs, frozen at parse time) into the target struct using
// the same `star:"name,required"` reflection that UnpackOperationKwargs
// uses for parse-time []starlark.Tuple.
//
// RUNTIME-PATH COMPANION: UnpackOperationKwargs is the parse-time entry
// point used by pkg/parser to validate kwargs as the .star file is
// loaded — it gets raw []starlark.Tuple from Starlark's call protocol
// and accurate syntax.Position for error attribution. DecodeKwargsFromDict
// is the runtime-path companion: by the time the Phase 2 activity runs,
// the original kwargs have been frozen into a *starlark.Dict on the
// ActionRef, and the activity-side error attribution is the action index
// (wrapped by the activity layer) — not a syntax position.
//
// Errors are *dag.ValidationError with Pos = syntax.Position{} (zero —
// runtime path; the activity layer wraps with action-index attribution
// in its own error message).
//
// Decision reference: the Phase 2 activity uses this; UnpackOperationKwargs
// remains the parser's entry point. Both share the FieldSpec reflection
// logic via ParseSchema, so adding new supported field types only requires
// editing assignStarlarkToGo once.
//
// PERFORMANCE: ActionRef.Kwargs sizes are ≤ a few dozen entries in
// practice; the Items() allocation here is bounded and only paid once per
// action invocation. Frozen-dict iteration is read-only and safe.
func DecodeKwargsFromDict(opName string, kwargs *starlark.Dict, target any) error {
	if target == nil {
		return &dag.ValidationError{
			Msg: fmt.Sprintf("%s: DecodeKwargsFromDict: target is nil", opName),
		}
	}

	tv := reflect.ValueOf(target)
	if tv.Kind() != reflect.Ptr || tv.IsNil() {
		return &dag.ValidationError{
			Msg: fmt.Sprintf("%s: DecodeKwargsFromDict: target must be a non-nil pointer to struct", opName),
		}
	}

	specs, err := ParseSchema(tv.Elem().Type())
	if err != nil {
		return &dag.ValidationError{
			Msg: fmt.Sprintf("%s: DecodeKwargsFromDict: %v", opName, err),
		}
	}

	// Convert the *starlark.Dict to []starlark.Tuple so we can delegate
	// to UnpackOperationKwargs's logic. Dict.Items() returns []Tuple
	// directly (each tuple is [key, value]) — the same shape the parser
	// uses. Iteration is read-only and works on frozen Dicts.
	var tuples []starlark.Tuple
	if kwargs != nil {
		tuples = kwargs.Items()
	}

	// Delegate: zero syntax.Position is acceptable on the runtime path —
	// the activity layer wraps with action-index attribution. Parse-time
	// errors with positions still flow through UnpackOperationKwargs
	// unchanged.
	return UnpackOperationKwargs(opName, syntax.Position{}, specs, tuples, target)
}
