package validator_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/validator"
)

// TestValidate_ReturnsTypedErrors writes a tempfile referencing an unknown
// extension global and asserts Validate returns a *dag.ParseError or
// *dag.ValidationError. The exact shape comes from the parser's finalize
// pass (Phase 1/2/3/4 lints).
func TestValidate_ReturnsTypedErrors(t *testing.T) {
	dir := t.TempDir()

	// Bad: references undeclared name `unknown_extension.foo()` →
	// parse-time NameError surfaces as *dag.ParseError.
	bad := filepath.Join(dir, "bad.star")
	require.NoError(t, os.WriteFile(bad, []byte(`flow(name="x", inputs={}, steps=[step(action=unknown_extension.foo())])`), 0o644))

	errs := validator.Validate(bad)
	require.NotEmpty(t, errs)
	var pe *dag.ParseError
	var ve *dag.ValidationError
	require.True(t, errors.As(errs[0], &pe) || errors.As(errs[0], &ve),
		"expected *dag.ParseError or *dag.ValidationError, got %T: %v", errs[0], errs[0])
}

// TestValidate_HappyPathReturnsEmpty parses a minimal valid flow and
// asserts the returned slice is empty (NOT nil).
func TestValidate_HappyPathReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "ok.star")
	require.NoError(t, os.WriteFile(good, []byte(`flow(name="ok", inputs={}, steps=[script(id="x", fn=lambda ctx: {"a": 1}, output_alias="a")])`), 0o644))

	errs := validator.Validate(good)
	require.Empty(t, errs)
	require.NotNil(t, errs, "Validate must return empty (not nil) on success")
}

// TestValidate_NonexistentFile surfaces parser read failures as
// *dag.ParseError. Smoke check: passing a missing path doesn't panic.
func TestValidate_NonexistentFile(t *testing.T) {
	errs := validator.Validate("/no/such/file.star")
	require.NotEmpty(t, errs)
	// Parser surfaces *dag.ParseError on read failure.
	var pe *dag.ParseError
	require.True(t, errors.As(errs[0], &pe))
}
