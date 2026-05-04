package parser

import (
	"fmt"
	"sort"
	"strings"
)

// TypeInfo is the sealed sum of statically-inferable Starlark types
// (D4.2-14). The unexported isTypeInfo() seal forbids external types
// from satisfying the interface — adding a kind requires editing this
// file (deliberate API-evolution gate). Mirrors the pkg/dag.Node and
// pkg/dag.ActionResult precedents.
//
// Concrete kinds:
//   - TypeScalar — int, float, bool, string, none
//   - TypeDict   — homogeneous-key dict; recursive (dict-of-dict allowed)
//   - TypeList   — homogeneous list; mismatched-element lists collapse to TypeOpaque
//   - TypeTuple  — positional tuple
//   - TypeOpaque — "cannot statically infer" sentinel
//
// Strict no-LUB equality (D4.2-11): TypeScalar{int} ≠ TypeScalar{float}.
// Explicit casts (`float(x)`) handle widening at the user level. The
// branch-equality validator separately defers when ONE side is opaque
// and the other is concrete — it does not call Equal in that case;
// Equal returns false for the asymmetric case so the seal remains
// compile-time strict.
type TypeInfo interface {
	isTypeInfo()
}

// TypeScalar covers int, float, bool, string, none.
type TypeScalar struct {
	Kind string // "int" | "float" | "bool" | "string" | "none"
}

func (TypeScalar) isTypeInfo() {}

// TypeDict is a homogeneous-key dict whose Fields map declares per-key types.
// Recursive: a dict-of-dict is fine.
type TypeDict struct {
	Fields map[string]TypeInfo
}

func (TypeDict) isTypeInfo() {}

// TypeList is a homogeneous list. Mismatched-element lists collapse to TypeOpaque.
type TypeList struct {
	Element TypeInfo
}

func (TypeList) isTypeInfo() {}

// TypeTuple is a positional tuple.
type TypeTuple struct {
	Elements []TypeInfo
}

func (TypeTuple) isTypeInfo() {}

// TypeOpaque is the "cannot statically infer" sentinel.
type TypeOpaque struct{}

func (TypeOpaque) isTypeInfo() {}

// Compile-time seal verification — these declarations would fail to
// compile if any concrete kind dropped its isTypeInfo() method.
var (
	_ TypeInfo = TypeScalar{}
	_ TypeInfo = TypeDict{}
	_ TypeInfo = TypeList{}
	_ TypeInfo = TypeTuple{}
	_ TypeInfo = TypeOpaque{}
)

// Equal compares two TypeInfo values by deep structural equality.
// Strict: TypeScalar{int} ≠ TypeScalar{float} (no LUB). Returns true
// only when the concrete kinds match AND any nested TypeInfo values
// are recursively equal. One-side-Opaque returns false — the
// branch-equality validator (plan 03) detects opaque on either side
// BEFORE calling Equal and defers; Equal itself stays strict to keep
// the seal honest.
func Equal(a, b TypeInfo) bool {
	switch ax := a.(type) {
	case TypeScalar:
		bx, ok := b.(TypeScalar)
		return ok && ax.Kind == bx.Kind
	case TypeDict:
		bx, ok := b.(TypeDict)
		if !ok || len(ax.Fields) != len(bx.Fields) {
			return false
		}
		for k, av := range ax.Fields {
			bv, has := bx.Fields[k]
			if !has || !Equal(av, bv) {
				return false
			}
		}
		return true
	case TypeList:
		bx, ok := b.(TypeList)
		return ok && Equal(ax.Element, bx.Element)
	case TypeTuple:
		bx, ok := b.(TypeTuple)
		if !ok || len(ax.Elements) != len(bx.Elements) {
			return false
		}
		for i, av := range ax.Elements {
			if !Equal(av, bx.Elements[i]) {
				return false
			}
		}
		return true
	case TypeOpaque:
		_, ok := b.(TypeOpaque)
		return ok
	}
	return false
}

// typeInfoString formats a TypeInfo for error messages (lowercase,
// human-readable). Used by the branch-equality validator (plan 03)
// when reporting per-key mismatches.
func typeInfoString(t TypeInfo) string {
	switch x := t.(type) {
	case TypeScalar:
		if x.Kind == "" {
			return "scalar"
		}
		return x.Kind
	case TypeList:
		return fmt.Sprintf("list[%s]", typeInfoString(x.Element))
	case TypeDict:
		if len(x.Fields) == 0 {
			return "dict"
		}
		// Sorted for deterministic error output.
		keys := make([]string, 0, len(x.Fields))
		for k := range x.Fields {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s:%s", k, typeInfoString(x.Fields[k])))
		}
		return "dict[" + strings.Join(parts, ",") + "]"
	case TypeTuple:
		parts := make([]string, 0, len(x.Elements))
		for _, e := range x.Elements {
			parts = append(parts, typeInfoString(e))
		}
		return "tuple[" + strings.Join(parts, ",") + "]"
	case TypeOpaque:
		return "opaque"
	}
	return "unknown"
}
