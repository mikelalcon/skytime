package bridge

import (
	"fmt"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// FromStarlarkValue is the inverse of toStarlarkValue. Phase 1 supports the
// types Phase 3's interpreter immediately needs: NoneType, String, Int (as
// int64), Float (as float64), Bool, *List (as []any), *Dict (as map[string]any
// — string keys only), and *starlarkstruct.Struct (as map[string]any).
//
// Unsupported types — notably *starlark.Function (lambdas surface to Phase 3
// via *dag.CapturedLambda, not as raw values) — return an error.
func FromStarlarkValue(v starlark.Value) (any, error) {
	switch x := v.(type) {
	case starlark.NoneType:
		return nil, nil
	case starlark.String:
		return string(x), nil
	case starlark.Int:
		i, ok := x.Int64()
		if !ok {
			return nil, fmt.Errorf("FromStarlarkValue: int overflow")
		}
		return i, nil
	case starlark.Float:
		return float64(x), nil
	case starlark.Bool:
		return bool(x), nil
	case *starlark.List:
		out := make([]any, 0, x.Len())
		iter := x.Iterate()
		defer iter.Done()
		var elem starlark.Value
		for iter.Next(&elem) {
			g, err := FromStarlarkValue(elem)
			if err != nil {
				return nil, err
			}
			out = append(out, g)
		}
		return out, nil
	case *starlark.Dict:
		out := make(map[string]any, x.Len())
		for _, item := range x.Items() {
			ks, ok := item[0].(starlark.String)
			if !ok {
				return nil, fmt.Errorf("FromStarlarkValue: dict key must be string, got %s", item[0].Type())
			}
			gv, err := FromStarlarkValue(item[1])
			if err != nil {
				return nil, err
			}
			out[string(ks)] = gv
		}
		return out, nil
	case *starlarkstruct.Struct:
		names := x.AttrNames()
		out := make(map[string]any, len(names))
		for _, name := range names {
			attr, err := x.Attr(name)
			if err != nil {
				return nil, fmt.Errorf("FromStarlarkValue: struct.Attr(%q): %w", name, err)
			}
			gv, err := FromStarlarkValue(attr)
			if err != nil {
				return nil, err
			}
			out[name] = gv
		}
		return out, nil
	default:
		return nil, fmt.Errorf("FromStarlarkValue: unsupported type %s", v.Type())
	}
}
