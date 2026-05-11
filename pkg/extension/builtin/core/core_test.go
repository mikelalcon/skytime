package core

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// TestCoreExtension_Name asserts the extension's global key.
func TestCoreExtension_Name(t *testing.T) {
	ext := New()
	require.Equal(t, "core", ext.Name())
}

// TestCoreExtension_InitializeModule asserts Initialize returns a
// *starlarkstruct.Module shaped as the spec demands: Name="core",
// Members has exactly one key "cron", that member is a *starlark.Builtin.
func TestCoreExtension_InitializeModule(t *testing.T) {
	ext := New()
	thread := &starlark.Thread{Name: "test-init"}

	v, err := ext.Initialize(thread, nil)
	require.NoError(t, err)
	require.NotNil(t, v)

	mod, ok := v.(*starlarkstruct.Module)
	require.True(t, ok, "expected *starlarkstruct.Module; got %T", v)
	require.Equal(t, "core", mod.Name)
	require.Len(t, mod.Members, 1, "module must have exactly one member: cron")

	cronAttr, ok := mod.Members["cron"]
	require.True(t, ok, "module must have a 'cron' member")
	_, ok = cronAttr.(*starlark.Builtin)
	require.True(t, ok, "cron member must be a *starlark.Builtin; got %T", cronAttr)
}

// TestCoreExtension_OperationsEmpty asserts Operations() returns an
// empty (non-nil) map. Core has no activities, only trigger primitives.
func TestCoreExtension_OperationsEmpty(t *testing.T) {
	ext := New()
	ops := ext.Operations()
	require.NotNil(t, ops, "Operations() must return a non-nil map (extension.Registry contract)")
	require.Empty(t, ops, "Operations() must be empty for core")
}

// Compile-time interface satisfaction at the test layer.
var _ extension.Extension = skytimeCore{}
