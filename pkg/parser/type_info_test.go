package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTypeInfo_ScalarStrictNoLUB: D4.2-11 — scalar equality is strict,
// no Least Upper Bound. int ≠ float; explicit casts (`float(x)`) are
// the user-side widening mechanism.
func TestTypeInfo_ScalarStrictNoLUB(t *testing.T) {
	intT := TypeScalar{Kind: "int"}
	floatT := TypeScalar{Kind: "float"}
	boolT := TypeScalar{Kind: "bool"}
	stringT := TypeScalar{Kind: "string"}

	assert.True(t, Equal(intT, TypeScalar{Kind: "int"}))
	assert.False(t, Equal(intT, floatT), "int ≠ float (no LUB)")
	assert.False(t, Equal(boolT, intT), "bool ≠ int")
	assert.True(t, Equal(stringT, TypeScalar{Kind: "string"}))
}

// TestTypeInfo_DictRecursiveEqual: D4.2-14 — dict equality is recursive
// across Fields. Size mismatch → false. Per-key recursive mismatch →
// false.
func TestTypeInfo_DictRecursiveEqual(t *testing.T) {
	d1 := TypeDict{Fields: map[string]TypeInfo{
		"a": TypeScalar{Kind: "int"},
		"b": TypeScalar{Kind: "string"},
	}}
	d1Copy := TypeDict{Fields: map[string]TypeInfo{
		"a": TypeScalar{Kind: "int"},
		"b": TypeScalar{Kind: "string"},
	}}
	d1Smaller := TypeDict{Fields: map[string]TypeInfo{
		"a": TypeScalar{Kind: "int"},
	}}
	d1Mismatch := TypeDict{Fields: map[string]TypeInfo{
		"a": TypeScalar{Kind: "int"},
		"b": TypeScalar{Kind: "float"}, // mismatched per-key
	}}

	assert.True(t, Equal(d1, d1Copy))
	assert.False(t, Equal(d1, d1Smaller), "size mismatch must reject")
	assert.False(t, Equal(d1, d1Mismatch), "per-key recursive mismatch must reject")
}

// TestTypeInfo_ListHomogeneousElement: D4.2-14 — homogeneous lists
// equate when the element type matches; mismatch → false.
func TestTypeInfo_ListHomogeneousElement(t *testing.T) {
	l1 := TypeList{Element: TypeScalar{Kind: "int"}}
	l1Copy := TypeList{Element: TypeScalar{Kind: "int"}}
	l1Float := TypeList{Element: TypeScalar{Kind: "float"}}
	l1Opaque := TypeList{Element: TypeOpaque{}}

	assert.True(t, Equal(l1, l1Copy))
	assert.False(t, Equal(l1, l1Float))
	assert.False(t, Equal(l1, l1Opaque),
		"list[int] ≠ list[opaque] — Equal stays strict; deferral is the validator's job")
}

// TestTypeInfo_TuplePositional: D4.2-14 — tuples compare element-wise
// in positional order. Length mismatch or position mismatch → false.
func TestTypeInfo_TuplePositional(t *testing.T) {
	t1 := TypeTuple{Elements: []TypeInfo{TypeScalar{Kind: "int"}, TypeScalar{Kind: "string"}}}
	t1Copy := TypeTuple{Elements: []TypeInfo{TypeScalar{Kind: "int"}, TypeScalar{Kind: "string"}}}
	t1Reorder := TypeTuple{Elements: []TypeInfo{TypeScalar{Kind: "string"}, TypeScalar{Kind: "int"}}}
	t1Short := TypeTuple{Elements: []TypeInfo{TypeScalar{Kind: "int"}}}

	assert.True(t, Equal(t1, t1Copy))
	assert.False(t, Equal(t1, t1Reorder), "tuple position matters")
	assert.False(t, Equal(t1, t1Short), "tuple length mismatch must reject")
}

// TestTypeInfo_OpaqueDefersStrictEquality: Opaque ≡ Opaque is true,
// but Opaque vs concrete is FALSE — the branch-equality validator
// (plan 03) detects opaque on either side and defers BEFORE calling
// Equal; Equal itself stays strict.
func TestTypeInfo_OpaqueDefersStrictEquality(t *testing.T) {
	op := TypeOpaque{}
	intT := TypeScalar{Kind: "int"}

	assert.True(t, Equal(op, TypeOpaque{}))
	assert.False(t, Equal(op, intT),
		"Equal returns false for Opaque vs concrete; deferral is the validator's job")
	assert.False(t, Equal(intT, op),
		"asymmetric: Equal returns false on Opaque/concrete in either direction")
}

// TestTypeInfo_CompileTimeSeal asserts that the canonical sealed-sum
// pattern compiles — the var blocks at the bottom of type_info.go
// already prove every kind satisfies TypeInfo. This test exists as
// a runtime smoke for the var-block declarations (any future drop of
// isTypeInfo() on a kind would fail to compile this package).
func TestTypeInfo_CompileTimeSeal(t *testing.T) {
	var ti TypeInfo
	for _, candidate := range []TypeInfo{
		TypeScalar{Kind: "int"},
		TypeDict{Fields: map[string]TypeInfo{}},
		TypeList{Element: TypeOpaque{}},
		TypeTuple{Elements: []TypeInfo{}},
		TypeOpaque{},
	} {
		ti = candidate
		require.NotNil(t, ti, "all five kinds must satisfy TypeInfo")
	}
}

// TestTypeInfoString_FormatsForErrors: the typeInfoString helper produces
// human-readable lowercase descriptors for plan 03's branch-equality error
// messages.
func TestTypeInfoString_FormatsForErrors(t *testing.T) {
	cases := []struct {
		t    TypeInfo
		want string
	}{
		{TypeScalar{Kind: "int"}, "int"},
		{TypeScalar{Kind: "string"}, "string"},
		{TypeOpaque{}, "opaque"},
		{TypeList{Element: TypeScalar{Kind: "int"}}, "list[int]"},
		{TypeDict{Fields: nil}, "dict"},
		{TypeDict{Fields: map[string]TypeInfo{
			"a": TypeScalar{Kind: "int"},
			"b": TypeScalar{Kind: "string"},
		}}, "dict[a:int,b:string]"},
		{TypeTuple{Elements: []TypeInfo{TypeScalar{Kind: "int"}, TypeScalar{Kind: "string"}}}, "tuple[int,string]"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, typeInfoString(c.t))
	}
}
