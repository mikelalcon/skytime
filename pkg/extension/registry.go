package extension

import (
	"errors"
	"fmt"
	"sync"
)

// ErrIdempotentRequired is the sentinel returned (wrapped) by Registry.Register
// when an operation lacks an Idempotent declaration. D-12: Idempotent is a
// required, no-default declaration — extensions must explicitly choose
// `Idempotent: extension.Ptr(true)` or `Idempotent: extension.Ptr(false)`.
//
// Callers detect this case with `errors.Is(err, ErrIdempotentRequired)`.
var ErrIdempotentRequired = errors.New("Idempotent declaration required")

// Registry is the per-parser extension registry. D-07 forbids global state:
// every parser owns its own *Registry, constructed by parser.NewParser
// (Phase 1 plan 04 wires this). Plan 03 (this file) owns the data structure
// + Register-time validation.
//
// Thread safety: Register and Get may be called concurrently. The internal
// sync.RWMutex serializes writes and allows concurrent reads.
type Registry struct {
	mu  sync.RWMutex
	ext map[string]Extension // keyed by Extension.Name()
}

// NewRegistry constructs an empty *Registry.
func NewRegistry() *Registry {
	return &Registry{ext: make(map[string]Extension)}
}

// Register validates the extension and adds it to the registry.
//
// Validation (in order — fail fast):
//  1. Extension.Name() must be non-empty.
//  2. For each operation in Extension.Operations():
//     - spec must be non-nil
//     - spec.Idempotent must be non-nil — wraps ErrIdempotentRequired (D-12)
//     - spec.Func must be non-nil
//     - spec.KwargsType must be non-nil
//  3. The name must not already be registered.
//
// If any check fails the registry state is unchanged.
func (r *Registry) Register(ext Extension) error {
	name := ext.Name()
	if name == "" {
		return errors.New("extension name must be non-empty")
	}

	// Validate every operation spec BEFORE acquiring the write lock so a
	// bad spec does not block concurrent reads.
	for opName, spec := range ext.Operations() {
		if spec == nil {
			return fmt.Errorf("extension %q operation %q: nil spec", name, opName)
		}
		if spec.Idempotent == nil {
			return fmt.Errorf("extension %q operation %q: %w", name, opName, ErrIdempotentRequired)
		}
		if spec.Func == nil {
			return fmt.Errorf("extension %q operation %q: nil Func", name, opName)
		}
		if spec.KwargsType == nil {
			return fmt.Errorf("extension %q operation %q: nil KwargsType", name, opName)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, dup := r.ext[name]; dup {
		return fmt.Errorf("extension %q already registered", name)
	}
	r.ext[name] = ext
	return nil
}

// Get returns the registered extension by name. The second return value is
// false if no extension is registered under that name.
func (r *Registry) Get(name string) (Extension, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.ext[name]
	return e, ok
}

// All returns a snapshot copy of the registered extensions keyed by name.
// Mutating the returned map does not affect the registry.
func (r *Registry) All() map[string]Extension {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]Extension, len(r.ext))
	for k, v := range r.ext {
		out[k] = v
	}
	return out
}
