package parser

import "go.starlark.net/syntax"

// inferType statically infers the TypeInfo of a Starlark expression
// against a typed state schema. Implementation lives in plan 04.2-02
// per RESEARCH.md §Pattern 6 + §Type Inference Decision Table.
//
// schema is the state visible at the call site (flow.Inputs typed via
// typeFromHint, plus any prior script.OutputAlias / item-var entries).
// firstParam is the lambda's first parameter name — typically "ctx" but
// not enforced (a future locked-vocab change could rename it). The
// implementation walks AST nodes (NOT evaluated values); literals,
// `firstParam`.X attribute access (resolved against schema), Starlark
// binary operators (per-operand-type rules), the locked-20 lambda-time
// builtins (known return types), and `a if c else b` recursive arm
// equality each map to a concrete TypeInfo. Anything outside that
// vocabulary collapses to TypeOpaque so the branch-equality validator
// (plan 03) defers strict checks instead of misreporting.
//
// Strict no-LUB (D4.2-11): int + float → opaque, NOT float. Explicit
// casts (`float(x)`/`int(x)`) handle widening at the user level.
func inferType(e syntax.Expr, schema stateSchema, firstParam string) TypeInfo {
	switch n := e.(type) {
	case *syntax.Literal:
		switch n.Token {
		case syntax.INT:
			return TypeScalar{Kind: "int"}
		case syntax.FLOAT:
			return TypeScalar{Kind: "float"}
		case syntax.STRING:
			return TypeScalar{Kind: "string"}
		}
		return TypeOpaque{}

	case *syntax.Ident:
		// True / False / None are *syntax.Ident with reserved names.
		switch n.Name {
		case "True", "False":
			return TypeScalar{Kind: "bool"}
		case "None":
			return TypeScalar{Kind: "none"}
		}
		return TypeOpaque{}

	case *syntax.DotExpr:
		// firstParam.<name>: look up the typed schema. Multi-hop
		// firstParam.a.b is handled by recursing into n.X to get a
		// TypeDict and descending Fields.
		if id, ok := n.X.(*syntax.Ident); ok && id.Name == firstParam {
			t, has := schema.get(n.Name.Name)
			if !has {
				return TypeOpaque{}
			}
			return t
		}
		inner := inferType(n.X, schema, firstParam)
		if td, ok := inner.(TypeDict); ok {
			if t, has := td.Fields[n.Name.Name]; has {
				return t
			}
		}
		return TypeOpaque{}

	case *syntax.UnaryExpr:
		innerType := inferType(n.X, schema, firstParam)
		switch n.Op {
		case syntax.MINUS, syntax.PLUS:
			return innerType // sign preserves arithmetic type
		case syntax.NOT:
			return TypeScalar{Kind: "bool"}
		}
		return TypeOpaque{}

	case *syntax.BinaryExpr:
		return inferBinary(n, schema, firstParam)

	case *syntax.CondExpr:
		// `a if c else b` — recursive arm-equality. Both arms must
		// type-equal for a concrete result; otherwise opaque.
		tt := inferType(n.True, schema, firstParam)
		ft := inferType(n.False, schema, firstParam)
		if Equal(tt, ft) {
			return tt
		}
		return TypeOpaque{}

	case *syntax.ListExpr:
		// homogeneous element type; mismatched → TypeOpaque.
		if len(n.List) == 0 {
			return TypeList{Element: TypeOpaque{}}
		}
		elt := inferType(n.List[0], schema, firstParam)
		for _, ent := range n.List[1:] {
			t := inferType(ent, schema, firstParam)
			if !Equal(elt, t) {
				return TypeOpaque{}
			}
		}
		return TypeList{Element: elt}

	case *syntax.TupleExpr:
		elts := make([]TypeInfo, 0, len(n.List))
		for _, ent := range n.List {
			elts = append(elts, inferType(ent, schema, firstParam))
		}
		return TypeTuple{Elements: elts}

	case *syntax.DictExpr:
		fields := make(map[string]TypeInfo)
		for _, ent := range n.List {
			de, ok := ent.(*syntax.DictEntry)
			if !ok {
				return TypeOpaque{}
			}
			keyLit, ok := de.Key.(*syntax.Literal)
			if !ok || keyLit.Token != syntax.STRING {
				return TypeOpaque{} // computed key — defer
			}
			keyStr, ok := keyLit.Value.(string)
			if !ok {
				return TypeOpaque{}
			}
			fields[keyStr] = inferType(de.Value, schema, firstParam)
		}
		return TypeDict{Fields: fields}

	case *syntax.CallExpr:
		return inferCall(n, schema, firstParam)

	case *syntax.IndexExpr:
		// lst[i] — depends on lhs type; tuple/dict indexing returns
		// opaque (defer to v1.x for tuple-with-literal-index inference).
		lhs := inferType(n.X, schema, firstParam)
		if tl, ok := lhs.(TypeList); ok {
			return tl.Element
		}
		return TypeOpaque{}

	case *syntax.SliceExpr:
		// lst[a:b] returns same type as lhs.
		return inferType(n.X, schema, firstParam)

	case *syntax.ParenExpr:
		return inferType(n.X, schema, firstParam)

	case *syntax.Comprehension:
		// `[x*2 for x in xs]` and friends — out of scope for v1.
		return TypeOpaque{}
	}
	return TypeOpaque{}
}

// inferBinary handles `+`, `-`, `*`, `/`, `//`, `%`, comparisons, and
// `and`/`or` per the Type Inference Decision Table.
//
// Strict no-LUB (D4.2-11): mixed int+float is intentionally TypeOpaque
// rather than promoting to float — the user must wrap one side with
// `float(x)`/`int(x)` to widen explicitly. This mirrors the
// branch-equality strictness so cross-branch and within-expression
// strictness agree.
func inferBinary(n *syntax.BinaryExpr, schema stateSchema, firstParam string) TypeInfo {
	lt := inferType(n.X, schema, firstParam)
	rt := inferType(n.Y, schema, firstParam)
	switch n.Op {
	case syntax.LT, syntax.GT, syntax.LE, syntax.GE, syntax.EQL, syntax.NEQ:
		return TypeScalar{Kind: "bool"}

	case syntax.PLUS:
		// int+int=int, float+float=float, string+string=string,
		// list[T]+list[T]=list[T]; mixed scalars → opaque.
		ls, lok := lt.(TypeScalar)
		rs, rok := rt.(TypeScalar)
		if lok && rok && ls.Kind == rs.Kind {
			return ls
		}
		ll, lLok := lt.(TypeList)
		rl, rLok := rt.(TypeList)
		if lLok && rLok && Equal(ll.Element, rl.Element) {
			return ll
		}
		return TypeOpaque{}

	case syntax.SLASH:
		// Regular div in Starlark/Python returns float (5/2 = 2.5).
		// Defer if either operand isn't a scalar — keeps the rule
		// honest when the user passes a non-numeric expression.
		_, lok := lt.(TypeScalar)
		_, rok := rt.(TypeScalar)
		if !lok || !rok {
			return TypeOpaque{}
		}
		return TypeScalar{Kind: "float"}

	case syntax.MINUS, syntax.STAR, syntax.SLASHSLASH, syntax.PERCENT:
		ls, lok := lt.(TypeScalar)
		rs, rok := rt.(TypeScalar)
		if !lok || !rok {
			return TypeOpaque{}
		}
		// Strict no-LUB: mixed int/float collapses to opaque (D4.2-11).
		if ls.Kind == "int" && rs.Kind == "int" {
			return TypeScalar{Kind: "int"}
		}
		if ls.Kind == "float" && rs.Kind == "float" {
			return TypeScalar{Kind: "float"}
		}
		return TypeOpaque{}

	case syntax.AND, syntax.OR:
		// Short-circuit: result is one of the operands; arm-equality.
		if Equal(lt, rt) {
			return lt
		}
		return TypeOpaque{}
	}
	return TypeOpaque{}
}

// inferCall handles the locked 20-key lambda-time builtins (D-20) and
// type-construction calls (int, float, str, bool, list, dict, tuple).
// Anything else (user helpers, <ext>.<op> calls, dynamic dispatch) is
// TypeOpaque so the branch-equality validator (plan 03) defers.
func inferCall(n *syntax.CallExpr, schema stateSchema, firstParam string) TypeInfo {
	id, ok := n.Fn.(*syntax.Ident)
	if !ok {
		return TypeOpaque{}
	}
	switch id.Name {
	case "int":
		return TypeScalar{Kind: "int"}
	case "float":
		return TypeScalar{Kind: "float"}
	case "str":
		return TypeScalar{Kind: "string"}
	case "bool":
		return TypeScalar{Kind: "bool"}
	case "len":
		return TypeScalar{Kind: "int"}
	case "abs":
		// abs(int) → int; abs(float) → float — preserves arg type.
		if len(n.Args) > 0 {
			return inferType(n.Args[0], schema, firstParam)
		}
		return TypeOpaque{}
	case "sum":
		// sum(iter, start=0) — returns same kind as start (int|float).
		if len(n.Args) >= 2 {
			return inferType(n.Args[1], schema, firstParam)
		}
		return TypeScalar{Kind: "int"}
	case "min", "max":
		// min/max return same type as elements; first-arg-list shape.
		if len(n.Args) > 0 {
			arg := inferType(n.Args[0], schema, firstParam)
			if tl, isList := arg.(TypeList); isList {
				return tl.Element
			}
			return arg
		}
		return TypeOpaque{}
	case "any", "all":
		return TypeScalar{Kind: "bool"}
	case "sorted", "reversed":
		// sorted(list[T]) → list[T]; reversed same. Returns same-type
		// list as the first arg (or opaque if not a list).
		if len(n.Args) > 0 {
			return inferType(n.Args[0], schema, firstParam)
		}
		return TypeOpaque{}
	case "list", "dict", "tuple":
		// Constructors over arbitrary iterables — element types are not
		// statically known in v1.
		return TypeOpaque{}
	case "enumerate", "zip":
		// list of (int,T) / list of tuples — out of v1 vocabulary.
		return TypeOpaque{}
	case "range":
		// `range` object — not in our TypeInfo vocabulary.
		return TypeOpaque{}
	}
	return TypeOpaque{}
}
