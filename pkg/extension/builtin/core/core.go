// Package core is Skytime's namespace for built-in trigger primitives
// that aren't owned by a domain extension. Phase 7.2 ships core.cron;
// future phases may add core.signal, core.queue.
//
// Shipped from the library root; registered by default in
// cmd/skytime/main.go (and example/extbin/main.go) via
// cli.WithExtensions(skycore.New(), ...).
//
// The `_ "time/tzdata"` blank import lives here (NOT cron.go) so that
// every binary registering core.New() pulls in the embedded IANA
// zoneinfo database — required for time.LoadLocation("America/New_York")
// to resolve inside scratch / distroless containers (Pitfall 3, ~450KB
// binary cost).
package core

import (
	_ "time/tzdata" // SECURITY: embeds IANA zoneinfo so time.LoadLocation works in scratch/distroless containers (Pitfall 3, ~450KB binary cost).

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// skytimeCore is the Extension implementation. New() returns an instance.
type skytimeCore struct{}

// New constructs the baked-in core extension.
func New() extension.Extension { return skytimeCore{} }

// Name returns "core" — the global key parser/globals.go binds the
// Initialize return value under.
func (skytimeCore) Name() string { return "core" }

// Initialize returns the Starlark-side namespace value: a
// *starlarkstruct.Module whose single attribute "cron" is a builtin
// factory producing *cronSource values. Mirrors pkg/extension/builtin/http
// Initialize shape.
func (skytimeCore) Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error) {
	return &starlarkstruct.Module{
		Name: "core",
		Members: starlark.StringDict{
			"cron": starlark.NewBuiltin("core.cron", cronFactory),
		},
	}, nil
}

// Operations returns an empty (non-nil) map — core has no activities,
// only trigger primitives. The empty return matches the extension.Registry
// contract (Phase 1).
func (skytimeCore) Operations() map[string]*extension.OperationSpec {
	return map[string]*extension.OperationSpec{}
}
