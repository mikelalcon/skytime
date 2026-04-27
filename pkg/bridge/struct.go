package bridge

import (
	"fmt"
	"sort"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// ToStarlarkStruct recursively converts a Go state map into a *starlarkstruct.Struct
// with deterministic key order (sort.Strings before iteration — Pitfall #3).
//
// Supported value types in v1: string, int, int64, float64, bool, nil, []any,
// map[string]any. Unsupported types return an error; extending is a one-line
// addition when Phase 6 surfaces a real need.
//
// DSL-09: this is what powers ctx.req.repo_name-style dot access — the nested
// req map is converted to a *starlarkstruct.Struct, and Starlark resolves
// .repo_name as an attribute lookup.
//
// Iteration determinism: the same Go map converted twice produces byte-equal
// output (verified by TestToStarlarkStruct_Deterministic). This is what
// keeps Temporal-history events stable on lambda re-evaluation.
func ToStarlarkStruct(m map[string]any) (*starlarkstruct.Struct, error) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys) // MANDATORY — Pitfall #3 / DSL-09 determinism

	sd := make(starlark.StringDict, len(keys))
	for _, k := range keys {
		v, err := toStarlarkValue(m[k])
		if err != nil {
			return nil, fmt.Errorf("ToStarlarkStruct: key %q: %w", k, err)
		}
		sd[k] = v
	}
	return starlarkstruct.FromStringDict(starlarkstruct.Default, sd), nil
}

// toStarlarkValue converts a single Go value to its Starlark counterpart.
// Recursive — calls ToStarlarkStruct on nested maps so dot access works at
// every level.
func toStarlarkValue(v any) (starlark.Value, error) {
	switch x := v.(type) {
	case nil:
		return starlark.None, nil
	case string:
		return starlark.String(x), nil
	case int:
		return starlark.MakeInt(x), nil
	case int64:
		return starlark.MakeInt64(x), nil
	case float64:
		return starlark.Float(x), nil
	case bool:
		return starlark.Bool(x), nil
	case map[string]any:
		// Recurse — nested struct preserves dot access.
		return ToStarlarkStruct(x)
	case []any:
		// Convert to *starlark.List, freeze it. Workflow state is read-only
		// inside a lambda; a frozen list prevents the lambda from mutating
		// its own iteration source.
		elems := make([]starlark.Value, 0, len(x))
		for i, e := range x {
			sv, err := toStarlarkValue(e)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			elems = append(elems, sv)
		}
		lst := starlark.NewList(elems)
		lst.Freeze()
		return lst, nil
	default:
		return nil, fmt.Errorf("unsupported type %T", v)
	}
}
