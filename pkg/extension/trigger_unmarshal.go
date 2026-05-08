package extension

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// triggerFactoryRegistry maps source kind ("github.webhook") to an
// unmarshal factory function. Sources register at extension Initialize
// time (or via package init for purely-static sources). Phase 7 ships
// zero entries; the FakeTriggerSource test stub registers
// "skytime.test.webhook" / "skytime.test.cron" only inside test code.
//
// Why this lives here, not in pkg/dag: dag must not import extension
// (cycle), but extensions need a way to register unmarshalers reachable
// from dag.Trigger.UnmarshalJSON. The seam is dag.RegisterTriggerSourceUnmarshaler
// (Plan 01) — pkg/extension's init() calls it once with the dispatch
// function defined here.
type triggerFactoryRegistry struct {
	mu        sync.RWMutex
	factories map[string]func([]byte) (TriggerSource, error)
}

var globalTriggerFactories = &triggerFactoryRegistry{
	factories: map[string]func([]byte) (TriggerSource, error){},
}

// RegisterTriggerSourceFactory installs an unmarshaler for the given
// source kind. Called by sources during their extension's Initialize
// lifecycle (preferred over init() to keep registration explicit and
// observable). Strict-collision: re-registering the same kind with a
// different (or even identical) function pointer returns an error.
// Function-pointer comparison in Go is unreliable except for nil, so
// any second registration of the same kind is treated as a collision
// to keep registry hygiene strict. Tests that need to re-register
// across multiple test packages should swallow the error (the second
// caller's registration is the no-op signal).
func RegisterTriggerSourceFactory(kind string, fn func([]byte) (TriggerSource, error)) error {
	if kind == "" {
		return fmt.Errorf("extension: trigger source kind required")
	}
	if fn == nil {
		return fmt.Errorf("extension: trigger source factory function required for kind %q", kind)
	}
	globalTriggerFactories.mu.Lock()
	defer globalTriggerFactories.mu.Unlock()
	if _, ok := globalTriggerFactories.factories[kind]; ok {
		return fmt.Errorf("extension: trigger source kind %q already registered", kind)
	}
	globalTriggerFactories.factories[kind] = fn
	return nil
}

// extensionTriggerUnmarshaler is the dispatch function dag.Trigger
// installs at init time. Reads the {kind, config} envelope, looks up
// the factory by kind, delegates the config bytes to the factory.
func extensionTriggerUnmarshaler(data []byte) (dag.TriggerSource, error) {
	var env struct {
		Kind   string          `json:"kind"`
		Config json.RawMessage `json:"config"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("extension: trigger source envelope: %w", err)
	}
	if env.Kind == "" {
		return nil, fmt.Errorf("extension: trigger source envelope: missing kind")
	}
	globalTriggerFactories.mu.RLock()
	fn, ok := globalTriggerFactories.factories[env.Kind]
	globalTriggerFactories.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("extension: no factory registered for trigger source kind %q", env.Kind)
	}
	return fn(env.Config)
}

func init() {
	dag.RegisterTriggerSourceUnmarshaler(extensionTriggerUnmarshaler)
}
