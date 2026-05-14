---
phase: 07-trigger-primitive-server-shell
plan: 02
type: execute
wave: 1
depends_on: []
priority: high
estimated_tasks: 2
autonomous: true
requirements:
  - TRIG-02
files_modified:
  - pkg/extension/trigger.go
  - pkg/extension/trigger_test.go
  - pkg/extension/trigger_unmarshal.go
  - pkg/extension/testing/triggersource.go
must_haves:
  truths:
    - "extension.TriggerSource sealed interface exists with methods {Kind() string, ReqSchema() []string, MarshalJSON() ([]byte, error), triggerSourceMarker()} where triggerSourceMarker is unexported (sealed)"
    - "Only types defined in pkg/extension or sub-packages it owns can implement TriggerSource (compile-time enforcement via unexported method)"
    - "extension.TriggerSource satisfies the dag.TriggerSource interface (Kind + MarshalJSON) so any extension.TriggerSource value flows through *dag.Trigger.Source"
    - "pkg/extension/trigger_unmarshal.go installs a kind-keyed registry: RegisterTriggerSourceFactory(kind string, fn func([]byte) (TriggerSource, error)) and an init() that calls dag.RegisterTriggerSourceUnmarshaler(extensionTriggerUnmarshaler)"
    - "pkg/extension/testing/triggersource.go ships a reusable test stub fakeTriggerSource{kind, reqFields} satisfying the sealed interface for use across pkg/parser, pkg/dag, pkg/worker, pkg/interpreter test packages"
    - "Source.config carries credential ID strings only; the test stub's MarshalJSON output never includes a Secret value (verified by test)"
  artifacts:
    - path: pkg/extension/trigger.go
      provides: "TriggerSource sealed interface + triggerSourceMarker seal + doc comments documenting the {kind, config} JSON envelope contract"
      contains: "type TriggerSource interface"
    - path: pkg/extension/trigger_test.go
      provides: "Sealed-interface compile assertions (var _ dag.TriggerSource = (*fakeTriggerSource)(nil)), kind-keyed registry round-trip, no-Secret-leak, ReqSchema returns the declared field set"
      contains: "TestTriggerSource_Sealed"
    - path: pkg/extension/trigger_unmarshal.go
      provides: "Kind-keyed factory registry + dag.RegisterTriggerSourceUnmarshaler wire-up at package init"
      contains: "RegisterTriggerSourceFactory"
    - path: pkg/extension/testing/triggersource.go
      provides: "Reusable fakeTriggerSource test stub for cross-package test reuse (pkg/parser, pkg/worker, pkg/interpreter, pkg/dag)"
      contains: "FakeTriggerSource"
  key_links:
    - from: pkg/extension/trigger_unmarshal.go (init)
      to: pkg/dag/marshal.go (RegisterTriggerSourceUnmarshaler)
      via: "dag.RegisterTriggerSourceUnmarshaler(extensionTriggerUnmarshaler) called from init()"
      pattern: "dag\\.RegisterTriggerSourceUnmarshaler"
    - from: pkg/extension/testing/triggersource.go (FakeTriggerSource)
      to: pkg/extension/trigger_unmarshal.go (factory registry)
      via: "test fixtures register FakeTriggerSource via RegisterTriggerSourceFactory(\"fake.webhook\", ...)"
      pattern: "RegisterTriggerSourceFactory\\("
---

<objective>
Land the `extension.TriggerSource` sealed-marker interface (TRIG-02) and the kind-keyed unmarshal registry that wires through to Plan 01's `dag.RegisterTriggerSourceUnmarshaler` seam. Plan 02 runs in parallel with Plan 01 (Wave 1) — both produce pure-data SDK contracts with zero parser/worker dependencies.

NOTE on D-07-08 (REQUIREMENTS.md TRIG-07/08 wording deviation): TRIG-07/08 in `.planning/REQUIREMENTS.md` say `triggers.github_webhook(events=[...], secret_credential=str|None)` and `triggers.generic_http_webhook(...)` ship under `pkg/extension/builtin/triggers/`. CONTEXT.md D-07-08 overrides this: source factories live under their owning extension's namespace (e.g., `github.webhook(...)` under `pkg/extension/builtin/github/`, generic webhook under a `webhook` extension). REQUIREMENTS.md TRIG-07/08 wording will need to be updated in Phase 7.1 when the first real factory lands. Plan 02 ships ZERO production source factories — only the SDK contract (`extension.TriggerSource` sealed marker) and a test-only stub (`pkg/extension/testing/triggersource.go::FakeTriggerSource`). The deviation is honored by construction.

Phase 7 ships ZERO production TriggerSource concrete types. The first real type (`github.WebhookSource`) lands in Phase 7.1 per D-07-08 (sources live under their owning extension's namespace). Plan 02 ships:
1. The sealed interface itself (`pkg/extension/trigger.go`)
2. The kind-keyed unmarshal registry (`pkg/extension/trigger_unmarshal.go`)
3. A reusable test stub `FakeTriggerSource` in `pkg/extension/testing/` so pkg/parser, pkg/dag, pkg/worker, pkg/interpreter test packages can all import it (per RESEARCH.md Open Q 3 recommendation: "export from pkg/extension/testing package — minimal reach, single source of truth. Existing precedent: pkg/testing package for Phase 5's harness").

Purpose: Establish the SDK shape so Plan 03 (parser builtin) can type-assert `sourceVal.(extension.TriggerSource)` and Plan 04 (TriggerRegistry) can iterate triggers grouped by `Source.Kind()`. Phase 7.1's real source factories satisfy this interface verbatim.

Output: ~30 LOC for the sealed interface, ~50 LOC for the unmarshal registry, ~80 LOC for the test stub package, ~150 LOC of tests.

LOAD-BEARING CONSTRAINT: Concrete TriggerSource implementations MUST emit `{kind, config}` JSON envelope where `config` carries credential ID strings only. Plan 02 documents this contract on the interface and tests it via FakeTriggerSource. Plan 06 grep-gates it via firewall test.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/STATE.md
@.planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md
@.planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md
@.planning/phases/07-trigger-primitive-server-shell/07-VALIDATION.md
@CLAUDE.md
@pkg/extension/extension.go
@pkg/extension/operation.go
@pkg/extension/credential.go
@pkg/extension/secret.go
@pkg/dag/trigger.go

<interfaces>
<!-- Concrete code patterns the executor MUST replicate. -->

Existing precedent for sealed interfaces in this codebase (pkg/extension/credential.go:19-55):
```go
type Credential interface {
    ID() string
    String() string
    isCredential() // unexported seal
}

type BearerCredential struct { ID_ string; Token Secret }
func (*BearerCredential) isCredential() {}  // seal satisfied
```

Existing precedent for cross-package test stubs (pkg/extension/testing/fake_handler.go — read this file before writing FakeTriggerSource).

The dag-local interface Plan 01 just shipped (pkg/dag/trigger.go):
```go
// Plan 01 ships:
type TriggerSource interface {
    Kind() string
    MarshalJSON() ([]byte, error)
}
var unmarshalTriggerSource = func(data []byte) (TriggerSource, error) { ... }
func RegisterTriggerSourceUnmarshaler(fn func([]byte) (TriggerSource, error))
```

Plan 02 must produce the SUPERSET sealed interface in pkg/extension AND the registry that calls dag.RegisterTriggerSourceUnmarshaler at init.

extension.TriggerSource interface THIS PLAN must produce (paste verbatim into pkg/extension/trigger.go):
```go
package extension

import (
    "github.com/mikel/skytime/pkg/dag" // adjust import path to actual module path
)

// TriggerSource is the sealed extension-side type representing a trigger
// source factory's value. Phase 7 ships zero concrete TriggerSource types
// — Phase 7.1's first source (github.WebhookSource) is the first real
// implementation. Sources live under their owning extension's namespace
// (D-07-08): GitHub triggers live under pkg/extension/builtin/github,
// generic webhook triggers under examples/http-github-webhook/extensions/webhook,
// cron under TBD-7.2.
//
// Sealed via the unexported triggerSourceMarker() method. Only types
// declared inside pkg/extension or sub-packages it owns can implement
// TriggerSource. Mirrors the seal pattern from extension.Credential
// (isCredential()) and dag.Node (nodeMarker()).
//
// Every TriggerSource implementation must:
//   - Return a stable string Kind() (e.g. "github.webhook"). Used as
//     the JSON discriminator AND the startup banner label
//     ("source-kind → flow-name").
//   - Return a fixed []string ReqSchema() listing valid req.<field>
//     attributes for the parser-time req-walker (Plan 03).
//   - Implement MarshalJSON() producing the {kind, config} envelope.
//     CRITICAL: config carries credential ID strings only — never an
//     extension.Secret, never a resolved credential value. Verified by
//     tests/firewall_credential_redaction_test.go (Plan 06).
//   - Satisfy dag.TriggerSource (Kind + MarshalJSON) so the value flows
//     through *dag.Trigger.Source — guaranteed compile-time by
//     `var _ dag.TriggerSource = TriggerSource(nil)` assertion below.
type TriggerSource interface {
    // Kind returns the discriminator (e.g. "github.webhook"). Used in
    // JSON marshaling and the startup banner.
    Kind() string

    // ReqSchema returns the valid req.<field> attribute names available
    // to the trigger's map and idempotency_key lambdas. Used by the
    // parser-time req-walker (pkg/parser/req_walk.go) to surface
    // "req.payloud" → "did you mean: req.payload?" errors.
    ReqSchema() []string

    // MarshalJSON produces the {kind, config} envelope. config MUST
    // contain credential ID strings only — never resolved Secret values
    // (D-07-09). Implementations are responsible for ensuring no
    // Secret-typed field reaches the marshaled bytes.
    MarshalJSON() ([]byte, error)

    // triggerSourceMarker is the seal — unexported, callable only from
    // pkg/extension or sub-packages.
    triggerSourceMarker()
}

// Compile-time assertion: every extension.TriggerSource MUST satisfy
// dag.TriggerSource. If a concrete TriggerSource type is added that
// somehow doesn't satisfy dag.TriggerSource (e.g., signature drift),
// the build breaks here. Static guarantee that *dag.Trigger.Source can
// always hold an extension.TriggerSource value.
var _ dag.TriggerSource = TriggerSource(nil)
```

Unmarshal registry THIS PLAN must produce (paste verbatim into pkg/extension/trigger_unmarshal.go):
```go
package extension

import (
    "encoding/json"
    "fmt"
    "sync"

    "github.com/mikel/skytime/pkg/dag" // adjust to actual module path
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
// observable). Idempotent: re-registering the same (kind, fn) pair is
// a no-op; re-registering with a different fn returns an error.
func RegisterTriggerSourceFactory(kind string, fn func([]byte) (TriggerSource, error)) error {
    if kind == "" {
        return fmt.Errorf("extension: trigger source kind required")
    }
    if fn == nil {
        return fmt.Errorf("extension: trigger source factory function required for kind %q", kind)
    }
    globalTriggerFactories.mu.Lock()
    defer globalTriggerFactories.mu.Unlock()
    if existing, ok := globalTriggerFactories.factories[kind]; ok {
        // Pointer-equality bypass: registering exact same fn twice is fine
        // (callers may re-init in tests). Different fn = collision = error.
        if &existing == &fn { return nil }
        // Function-pointer comparison in Go is unreliable except for nil.
        // We accept idempotent re-registration only when the caller is the same
        // package init path; treat any second registration as an error to keep
        // registry hygiene strict.
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
```

FakeTriggerSource test stub THIS PLAN must produce (paste verbatim into pkg/extension/testing/triggersource.go):
```go
// Package testing provides reusable test stubs for the extension SDK.
// Importable from any test package without circular dependencies.
//
// FakeTriggerSource is the canonical Trigger source stub for Phase 7
// tests. Use it in pkg/parser, pkg/dag, pkg/worker, pkg/interpreter
// test files. NOT importable from production code (testing package
// convention; not enforced by the compiler but expected by review).
package testing

import (
    "encoding/json"
    "fmt"
    "sort"

    "github.com/mikel/skytime/pkg/extension" // adjust path
)

// FakeTriggerSource is a minimal TriggerSource for tests. Configurable
// kind + req fields. Marshals to {kind, config:{req_fields:[...]}}.
type FakeTriggerSource struct {
    KindName  string
    ReqFields []string
    // CredentialIDInConfig is the test-only knob to confirm credential IDs
    // round-trip through {kind, config} without exposing Secrets. Empty by
    // default. Tests set it to assert the credential-ID-only contract.
    CredentialIDInConfig string
}

// Kind returns the configured kind string.
func (f *FakeTriggerSource) Kind() string { return f.KindName }

// ReqSchema returns the configured req fields, sorted for determinism.
func (f *FakeTriggerSource) ReqSchema() []string {
    out := append([]string(nil), f.ReqFields...)
    sort.Strings(out)
    return out
}

// MarshalJSON emits the {kind, config} envelope. config carries the
// req_fields slice plus a credential_id string (never a Secret value).
// Test asserts: bytes.Contains(result, []byte("\"credential_id\":\"" + f.CredentialIDInConfig + "\""))
//
// CRITICAL: FakeTriggerSource MUST NOT expose any extension.Secret-typed
// field through MarshalJSON. The credential_id string is fine; a Secret
// value would violate D-07-09 / D-07-10.
func (f *FakeTriggerSource) MarshalJSON() ([]byte, error) {
    cfg := map[string]interface{}{
        "req_fields": f.ReqSchema(),
    }
    if f.CredentialIDInConfig != "" {
        cfg["credential_id"] = f.CredentialIDInConfig
    }
    cfgBytes, err := json.Marshal(cfg)
    if err != nil { return nil, err }
    return []byte(fmt.Sprintf(`{"kind":%q,"config":%s}`, f.KindName, string(cfgBytes))), nil
}

// triggerSourceMarker satisfies the seal. Defined here (in the testing
// sub-package of extension) which IS owned by extension — so the seal
// is satisfied legally.
func (*FakeTriggerSource) triggerSourceMarker() {}

// RegisterFakeFactories installs unmarshal factories for two test kinds:
// "skytime.test.webhook" (req fields: payload, headers) and
// "skytime.test.cron" (req fields: scheduled_time, workflow_attempt).
// Idempotent: returns nil if already installed (covers test re-runs in
// the same package). Tests should call this once in TestMain or in a
// test setup helper.
func RegisterFakeFactories() {
    factory := func(data []byte) (extension.TriggerSource, error) {
        var cfg struct {
            ReqFields            []string `json:"req_fields"`
            CredentialIDInConfig string   `json:"credential_id"`
        }
        if err := json.Unmarshal(data, &cfg); err != nil { return nil, err }
        // Note: kind is dispatched on by the registry, so we can't recover
        // it from config bytes. The factory is registered per kind, so we
        // accept the kind from the closure context — but here we have one
        // factory for two kinds. A simpler approach: register two distinct
        // factories.
        return &FakeTriggerSource{
            KindName:             "", // overwritten by per-kind closure
            ReqFields:            cfg.ReqFields,
            CredentialIDInConfig: cfg.CredentialIDInConfig,
        }, nil
    }
    _ = factory // see below for the per-kind closure
    // Per-kind registration — closures pin the KindName:
    _ = extension.RegisterTriggerSourceFactory("skytime.test.webhook", func(data []byte) (extension.TriggerSource, error) {
        var cfg struct {
            ReqFields            []string `json:"req_fields"`
            CredentialIDInConfig string   `json:"credential_id"`
        }
        if err := json.Unmarshal(data, &cfg); err != nil { return nil, err }
        return &FakeTriggerSource{
            KindName:             "skytime.test.webhook",
            ReqFields:            cfg.ReqFields,
            CredentialIDInConfig: cfg.CredentialIDInConfig,
        }, nil
    })
    _ = extension.RegisterTriggerSourceFactory("skytime.test.cron", func(data []byte) (extension.TriggerSource, error) {
        var cfg struct {
            ReqFields            []string `json:"req_fields"`
            CredentialIDInConfig string   `json:"credential_id"`
        }
        if err := json.Unmarshal(data, &cfg); err != nil { return nil, err }
        return &FakeTriggerSource{
            KindName:             "skytime.test.cron",
            ReqFields:            cfg.ReqFields,
            CredentialIDInConfig: cfg.CredentialIDInConfig,
        }, nil
    })
    // Errors are intentionally swallowed — re-registration is expected when
    // multiple test packages call RegisterFakeFactories(); the registry's
    // existing-kind error is the signal for the second-and-onwards caller.
}
```

NOTE: The actual Go module path needs verification — read `go.mod` to confirm. The placeholder `github.com/mikel/skytime` should be replaced with the real module path the repo uses. Run `head -1 go.mod` first to confirm.
</interfaces>
</context>

<tasks>

<task type="auto">
  <id>07-02-01</id>
  <name>Task 1: Create pkg/extension/trigger.go (sealed interface) + pkg/extension/trigger_unmarshal.go (kind-keyed registry + dag wire-up)</name>
  <read_first>
    - pkg/extension/extension.go (the Extension interface for SDK shape parallel)
    - pkg/extension/operation.go (OperationSpec — TriggerSource is parallel SDK shape per D-07-07)
    - pkg/extension/credential.go (the isCredential() seal pattern; mirror for triggerSourceMarker())
    - pkg/extension/secret.go (Secret type — understand WHY no Secret can flow through TriggerSource.MarshalJSON: even an accidental %v leaks "<redacted>" in some error paths; the firewall in Plan 06 grep-gates this)
    - pkg/dag/trigger.go (Plan 01's output — the dag-local TriggerSource interface this plan's extension.TriggerSource must satisfy)
    - pkg/dag/marshal.go (lines 0-25 — the file-header Pos exclusion comment, then the RegisterTriggerSourceUnmarshaler / unmarshalTriggerSource seam shipped by Plan 01 at end of file)
    - go.mod (FIRST LINE — the actual module path, used in import statements)
    - .planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md (`<key_constraints>`: sealed interfaces only; D-07-06 sealed marker; D-07-07 pkg layout; D-07-08 namespace ownership)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Code Examples Example 2 — the verbatim TriggerSource interface; § Open Questions Open Q 3 — registry location decision)
  </read_first>
  <files>pkg/extension/trigger.go, pkg/extension/trigger_unmarshal.go</files>
  <action>
    Step 1 — Confirm the module path:
    ```bash
    head -1 go.mod
    ```
    Use the real module path in all import statements (replace `github.com/mikel/skytime` placeholder in `<interfaces>` with the actual path).

    Step 2 — Create `pkg/extension/trigger.go` (NEW file, ~50 LOC) with the verbatim content from the `<interfaces>` block:
    - `package extension`
    - Import the `dag` package (using the real module path)
    - The `TriggerSource` interface with four methods: `Kind() string`, `ReqSchema() []string`, `MarshalJSON() ([]byte, error)`, and the unexported seal `triggerSourceMarker()`
    - The compile-time assertion `var _ dag.TriggerSource = TriggerSource(nil)` ensuring extension.TriggerSource satisfies dag.TriggerSource

    Step 3 — Create `pkg/extension/trigger_unmarshal.go` (NEW file, ~80 LOC) with the verbatim content from `<interfaces>`:
    - The `triggerFactoryRegistry` struct + `globalTriggerFactories` package-level singleton
    - `RegisterTriggerSourceFactory(kind, fn) error` exported function with mu.Lock + duplicate check + the strict-collision error message exactly: `"extension: trigger source kind %q already registered"`
    - `extensionTriggerUnmarshaler(data) (dag.TriggerSource, error)` reading the {kind, config} envelope and dispatching via the registry; error messages exactly: `"extension: trigger source envelope: %w"` (wrap), `"extension: trigger source envelope: missing kind"`, and `"extension: no factory registered for trigger source kind %q"`
    - `func init() { dag.RegisterTriggerSourceUnmarshaler(extensionTriggerUnmarshaler) }` — wires the cross-package seam at package init

    Step 4 — Verify it compiles:
    ```bash
    go build ./pkg/extension/...
    go vet ./pkg/extension/...
    ```

    Per CLAUDE.md, no new go.mod dependencies are introduced — only stdlib (sync, encoding/json, fmt) plus existing pkg/dag.

    DO NOT add any concrete TriggerSource type in this task — Phase 7 ships ZERO production sources per D-07-08.
    DO NOT touch pkg/extension/extension.go, registry.go, operation.go, credential.go, or secret.go.
    DO NOT add a github.webhook / webhook.post / cron source — those are Phase 7.1 / 7.2.
  </action>
  <acceptance_criteria>
    - File `pkg/extension/trigger.go` exists
    - File `pkg/extension/trigger_unmarshal.go` exists
    - `grep -n 'type TriggerSource interface' pkg/extension/trigger.go` returns exactly one match
    - `grep -n 'triggerSourceMarker()' pkg/extension/trigger.go` returns exactly one match (the unexported seal in the interface declaration)
    - `grep -n 'var _ dag.TriggerSource = TriggerSource(nil)' pkg/extension/trigger.go` returns exactly one match (compile-time assertion)
    - `grep -n 'func RegisterTriggerSourceFactory' pkg/extension/trigger_unmarshal.go` returns exactly one match
    - `grep -n 'extensionTriggerUnmarshaler' pkg/extension/trigger_unmarshal.go` returns at least two matches (definition + use in init)
    - `grep -n 'dag.RegisterTriggerSourceUnmarshaler(extensionTriggerUnmarshaler)' pkg/extension/trigger_unmarshal.go` returns exactly one match (init wire-up)
    - `grep -n 'func init()' pkg/extension/trigger_unmarshal.go` returns exactly one match
    - `grep -nE 'extension: trigger source kind %q already registered' pkg/extension/trigger_unmarshal.go` returns exactly one match (the strict-collision error literal)
    - `grep -n 'extension: no factory registered for trigger source kind' pkg/extension/trigger_unmarshal.go` returns exactly one match (the no-factory error literal)
    - `go build ./pkg/extension/...` exits 0
    - `go vet ./pkg/extension/...` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go build ./pkg/extension/... && go vet ./pkg/extension/... && grep -q 'type TriggerSource interface' pkg/extension/trigger.go && grep -q 'triggerSourceMarker()' pkg/extension/trigger.go && grep -q 'var _ dag.TriggerSource' pkg/extension/trigger.go && grep -q 'func RegisterTriggerSourceFactory' pkg/extension/trigger_unmarshal.go && grep -q 'func init()' pkg/extension/trigger_unmarshal.go
  </verify>
  <done>
    `pkg/extension/trigger.go` defines the sealed `TriggerSource` interface (4 methods including `triggerSourceMarker()`) with a compile-time assertion that it satisfies `dag.TriggerSource`. `pkg/extension/trigger_unmarshal.go` ships the kind-keyed factory registry, the dispatch function `extensionTriggerUnmarshaler`, and a package-init that calls `dag.RegisterTriggerSourceUnmarshaler` to wire the cross-package seam from Plan 01. No concrete TriggerSource type ships in production code (per D-07-08).
  </done>
</task>

<task type="auto" tdd="true">
  <id>07-02-02</id>
  <name>Task 2: Create pkg/extension/testing/triggersource.go (FakeTriggerSource stub) + pkg/extension/trigger_test.go covering TRIG-02</name>
  <read_first>
    - pkg/extension/trigger.go (Task 1 output — the sealed interface)
    - pkg/extension/trigger_unmarshal.go (Task 1 output — the registry)
    - pkg/extension/testing/fake_handler.go (existing precedent for the testing sub-package — read full file for the export pattern)
    - pkg/extension/testing/fake_handler_test.go if exists
    - pkg/dag/trigger.go (Plan 01 output — the dag-local TriggerSource interface that extension.TriggerSource must satisfy)
    - pkg/dag/trigger_test.go (Plan 01 output — the test pattern + the fakeTSrc stub used there; this plan generalizes that to FakeTriggerSource)
    - .planning/phases/07-trigger-primitive-server-shell/07-VALIDATION.md (TRIG-02 maps to TestTriggerSource_Sealed in pkg/extension/trigger_test.go)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Open Questions Open Q 3 — pkg/extension/testing recommended for stub location)
    - go.mod (FIRST LINE — confirm module path for the import statement)
  </read_first>
  <files>pkg/extension/testing/triggersource.go, pkg/extension/trigger_test.go</files>
  <behavior>
    - Test 1 (TestTriggerSource_Sealed): Compile-time + runtime assertion that `*FakeTriggerSource` (from pkg/extension/testing) satisfies `extension.TriggerSource`. Use `var _ extension.TriggerSource = (*testing.FakeTriggerSource)(nil)` style assertion, plus a runtime `_, ok := iface.(extension.TriggerSource); assert.True(t, ok)`. The compile-time `var _` line is the load-bearing seal proof.
    - Test 2 (TestTriggerSource_dagInterfaceSatisfied): Confirm FakeTriggerSource also satisfies `dag.TriggerSource` (Kind + MarshalJSON only). `var _ dag.TriggerSource = (*testing.FakeTriggerSource)(nil)`.
    - Test 3 (TestRegisterTriggerSourceFactory_RoundTrip): Register a factory for kind "skytime.test.webhook" via `extension.RegisterTriggerSourceFactory`. Marshal a FakeTriggerSource{KindName:"skytime.test.webhook", ReqFields:["payload","headers"], CredentialIDInConfig:"my-id"}. Unmarshal via `dag.UnmarshalJSON` (through *dag.Trigger.UnmarshalJSON triggers extensionTriggerUnmarshaler). Assert the recovered source has Kind="skytime.test.webhook", ReqSchema=["headers","payload"] (sorted), CredentialIDInConfig="my-id".
    - Test 4 (TestRegisterTriggerSourceFactory_Duplicate): Register kind "dup.test" once — succeeds. Register the same kind again with a different fn — returns the exact error `"extension: trigger source kind \"dup.test\" already registered"`. Use `t.Cleanup` to clear the registry entry (no exposed clear method — accept residual state across tests; use unique kinds per test).
    - Test 5 (TestRegisterTriggerSourceFactory_EmptyKind): Calling with kind="" returns error containing "trigger source kind required".
    - Test 6 (TestExtensionTriggerUnmarshaler_NoFactory): Marshal a config with kind "unknown.test"; call dag.Trigger.UnmarshalJSON with that envelope; assert error contains `"no factory registered for trigger source kind \"unknown.test\""`.
    - Test 7 (TestFakeTriggerSource_NoSecretInConfig): Construct FakeTriggerSource with CredentialIDInConfig="my-id". Marshal. Confirm output bytes contain `"credential_id":"my-id"`. Confirm output bytes DO NOT contain "<redacted>" (the extension.Secret String() return value) — proxy test that no Secret leaked. Future Phase 7.1 sources will add tighter assertions per their config shape.
  </behavior>
  <action>
    Step 1 — Create `pkg/extension/testing/triggersource.go` (NEW file). Use the verbatim content from the `<interfaces>` block:
    - Package declaration `package testing`
    - Import `"encoding/json"`, `"fmt"`, `"sort"`, and the extension package using the real module path
    - The `FakeTriggerSource` struct with fields `KindName string`, `ReqFields []string`, `CredentialIDInConfig string`
    - Methods `Kind() string`, `ReqSchema() []string` (returns a sorted COPY — use `sort.Strings`), `MarshalJSON() ([]byte, error)` emitting the `{kind, config}` envelope, and the seal `triggerSourceMarker()`
    - The `RegisterFakeFactories()` helper that registers two kinds: `"skytime.test.webhook"` with the closure pinning `KindName: "skytime.test.webhook"` and `"skytime.test.cron"` with `KindName: "skytime.test.cron"`. The closure body decodes the config bytes (`{req_fields, credential_id}`) and returns a fresh FakeTriggerSource. Errors from `extension.RegisterTriggerSourceFactory` are intentionally swallowed (`_ = ...`) so re-registration on second test-package invocation does not panic.

    NOTE: The first sketch in `<interfaces>` had a vestigial `factory := func(...)` declaration that's actually unused. Remove that. The final file should be:
    ```go
    package testing

    import (
        "encoding/json"
        "fmt"
        "sort"

        "<MODULE_PATH>/pkg/extension"
    )

    // FakeTriggerSource ... <doc comment>
    type FakeTriggerSource struct {
        KindName             string
        ReqFields            []string
        CredentialIDInConfig string
    }

    func (f *FakeTriggerSource) Kind() string { return f.KindName }

    func (f *FakeTriggerSource) ReqSchema() []string {
        out := append([]string(nil), f.ReqFields...)
        sort.Strings(out)
        return out
    }

    func (f *FakeTriggerSource) MarshalJSON() ([]byte, error) {
        cfg := map[string]any{
            "req_fields": f.ReqSchema(),
        }
        if f.CredentialIDInConfig != "" {
            cfg["credential_id"] = f.CredentialIDInConfig
        }
        cfgBytes, err := json.Marshal(cfg)
        if err != nil {
            return nil, err
        }
        return []byte(fmt.Sprintf(`{"kind":%q,"config":%s}`, f.KindName, string(cfgBytes))), nil
    }

    func (*FakeTriggerSource) triggerSourceMarker() {}

    // RegisterFakeFactories ... <doc comment>
    func RegisterFakeFactories() {
        _ = extension.RegisterTriggerSourceFactory("skytime.test.webhook", func(data []byte) (extension.TriggerSource, error) {
            var cfg struct {
                ReqFields            []string `json:"req_fields"`
                CredentialIDInConfig string   `json:"credential_id"`
            }
            if err := json.Unmarshal(data, &cfg); err != nil {
                return nil, err
            }
            return &FakeTriggerSource{
                KindName:             "skytime.test.webhook",
                ReqFields:            cfg.ReqFields,
                CredentialIDInConfig: cfg.CredentialIDInConfig,
            }, nil
        })
        _ = extension.RegisterTriggerSourceFactory("skytime.test.cron", func(data []byte) (extension.TriggerSource, error) {
            var cfg struct {
                ReqFields            []string `json:"req_fields"`
                CredentialIDInConfig string   `json:"credential_id"`
            }
            if err := json.Unmarshal(data, &cfg); err != nil {
                return nil, err
            }
            return &FakeTriggerSource{
                KindName:             "skytime.test.cron",
                ReqFields:            cfg.ReqFields,
                CredentialIDInConfig: cfg.CredentialIDInConfig,
            }, nil
        })
    }
    ```

    Replace `<MODULE_PATH>` with the real module path read from `go.mod` line 1.

    Step 2 — Create `pkg/extension/trigger_test.go` (NEW file). Use `package extension_test` (black-box, since FakeTriggerSource lives in the testing sub-package and importing the testing sub-package from extension's own white-box test would create a cycle through the testing-package's own import of extension).

    Use testify's `require` and `assert` per CLAUDE.md.

    Implement Tests 1-7 from the `<behavior>` block. Pattern for the sealed-interface compile assertion (Test 1):
    ```go
    package extension_test

    import (
        "<MODULE_PATH>/pkg/dag"
        "<MODULE_PATH>/pkg/extension"
        exttest "<MODULE_PATH>/pkg/extension/testing"
    )

    // Compile-time seal proof.
    var _ extension.TriggerSource = (*exttest.FakeTriggerSource)(nil)
    var _ dag.TriggerSource       = (*exttest.FakeTriggerSource)(nil)

    func TestTriggerSource_Sealed(t *testing.T) {
        var src extension.TriggerSource = &exttest.FakeTriggerSource{KindName: "x", ReqFields: []string{"a"}}
        require.NotNil(t, src)
        assert.Equal(t, "x", src.Kind())
        assert.Equal(t, []string{"a"}, src.ReqSchema())
    }
    ```

    Use UNIQUE test kind strings to avoid registry collisions across tests:
    - `TestRegisterTriggerSourceFactory_RoundTrip` uses kind `"skytime.test.roundtrip"`
    - `TestRegisterTriggerSourceFactory_Duplicate` uses kind `"skytime.test.duplicate"`
    - `TestRegisterTriggerSourceFactory_EmptyKind` uses kind `""`
    - `TestExtensionTriggerUnmarshaler_NoFactory` uses kind `"skytime.test.unknown"` (intentionally never registered)
    - `TestFakeTriggerSource_NoSecretInConfig` does not touch the registry; only marshals.

    Test 3 (RoundTrip) flow:
    ```go
    func TestRegisterTriggerSourceFactory_RoundTrip(t *testing.T) {
        require.NoError(t, extension.RegisterTriggerSourceFactory("skytime.test.roundtrip", func(data []byte) (extension.TriggerSource, error) {
            var cfg struct {
                ReqFields            []string `json:"req_fields"`
                CredentialIDInConfig string   `json:"credential_id"`
            }
            if err := json.Unmarshal(data, &cfg); err != nil {
                return nil, err
            }
            return &exttest.FakeTriggerSource{KindName: "skytime.test.roundtrip", ReqFields: cfg.ReqFields, CredentialIDInConfig: cfg.CredentialIDInConfig}, nil
        }))
        // Build a Trigger with this Source.
        trig := &dag.Trigger{
            FlowName: "demo",
            Source:   &exttest.FakeTriggerSource{KindName: "skytime.test.roundtrip", ReqFields: []string{"payload", "headers"}, CredentialIDInConfig: "my-id"},
            CredentialID: "my-id",
        }
        out, err := json.Marshal(trig)
        require.NoError(t, err)
        // Unmarshal back through dag.Trigger.UnmarshalJSON
        var got dag.Trigger
        require.NoError(t, json.Unmarshal(out, &got))
        require.NotNil(t, got.Source)
        assert.Equal(t, "skytime.test.roundtrip", got.Source.Kind())
        // Type-assert back to FakeTriggerSource to inspect ReqFields
        ftSrc, ok := got.Source.(*exttest.FakeTriggerSource)
        require.True(t, ok, "Source should be *FakeTriggerSource")
        assert.Equal(t, []string{"headers", "payload"}, ftSrc.ReqSchema())
        assert.Equal(t, "my-id", ftSrc.CredentialIDInConfig)
    }
    ```

    Step 3 — Run:
    ```bash
    go test ./pkg/extension/ -run TestTriggerSource -count=1 -race
    go test ./pkg/extension/ -run TestRegisterTriggerSourceFactory -count=1 -race
    go test ./pkg/extension/ -run TestExtensionTriggerUnmarshaler -count=1 -race
    go test ./pkg/extension/ -run TestFakeTriggerSource -count=1 -race
    go vet ./pkg/extension/...
    go vet ./pkg/extension/testing/...
    ```

    All must exit 0.

    Per D-07-09 / D-07-10 (CONTEXT.md key_constraints): Test 7 (NoSecretInConfig) is the unit-level enforcement of credential-never-serialized. Plan 06 grep-gates the broader contract via firewall test on `%+v` formatting verbs.

    Per D-07-08 (CONTEXT.md): NO production source factory ships in this plan. Phase 7.1 / 7.2 add real ones.
  </action>
  <acceptance_criteria>
    - File `pkg/extension/testing/triggersource.go` exists
    - File `pkg/extension/trigger_test.go` exists
    - `grep -n 'type FakeTriggerSource struct' pkg/extension/testing/triggersource.go` returns exactly one match
    - `grep -nE 'func \(\*FakeTriggerSource\) triggerSourceMarker\(\)' pkg/extension/testing/triggersource.go` returns exactly one match (the seal)
    - `grep -n 'func RegisterFakeFactories' pkg/extension/testing/triggersource.go` returns exactly one match
    - `grep -nE 'skytime\.test\.webhook' pkg/extension/testing/triggersource.go` returns at least two matches (registration + closure pin)
    - `grep -nE 'skytime\.test\.cron' pkg/extension/testing/triggersource.go` returns at least two matches
    - `grep -n 'var _ extension.TriggerSource = (*exttest.FakeTriggerSource)(nil)' pkg/extension/trigger_test.go` returns exactly one match (compile-time seal)
    - `grep -n 'var _ dag.TriggerSource' pkg/extension/trigger_test.go` returns exactly one match
    - `go test ./pkg/extension/ -run TestTriggerSource_Sealed -count=1` exits 0
    - `go test ./pkg/extension/ -run TestRegisterTriggerSourceFactory_RoundTrip -count=1` exits 0
    - `go test ./pkg/extension/ -run TestRegisterTriggerSourceFactory_Duplicate -count=1` exits 0
    - `go test ./pkg/extension/ -run TestRegisterTriggerSourceFactory_EmptyKind -count=1` exits 0
    - `go test ./pkg/extension/ -run TestExtensionTriggerUnmarshaler_NoFactory -count=1` exits 0
    - `go test ./pkg/extension/ -run TestFakeTriggerSource_NoSecretInConfig -count=1` exits 0
    - `go test ./pkg/extension/... -count=1 -race` exits 0 (full extension suite passes)
    - `go vet ./pkg/extension/... ./pkg/extension/testing/...` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go test ./pkg/extension/ -run 'TestTriggerSource_Sealed|TestRegisterTriggerSourceFactory_RoundTrip|TestRegisterTriggerSourceFactory_Duplicate|TestRegisterTriggerSourceFactory_EmptyKind|TestExtensionTriggerUnmarshaler_NoFactory|TestFakeTriggerSource_NoSecretInConfig' -count=1 -race && go vet ./pkg/extension/... ./pkg/extension/testing/... && grep -q 'type FakeTriggerSource struct' pkg/extension/testing/triggersource.go && grep -q 'var _ extension.TriggerSource' pkg/extension/trigger_test.go</automated>
  </verify>
  <done>
    `pkg/extension/testing/FakeTriggerSource` is a reusable, compile-time-sealed test stub satisfying both `extension.TriggerSource` and `dag.TriggerSource`. The kind-keyed registry from Task 1 round-trips Trigger marshaling end-to-end via the test stub. No Secret value reaches any FakeTriggerSource.MarshalJSON output. Registry collision returns the exact strict-error message. Plan 03 (parser) and Plan 04 (registry) consume FakeTriggerSource verbatim from this package.
  </done>
</task>

</tasks>

<verification>
After both tasks complete, run:

```bash
go build ./pkg/extension/... ./pkg/extension/testing/... ./pkg/dag/...
go vet ./pkg/extension/... ./pkg/extension/testing/... ./pkg/dag/...
go test ./pkg/extension/... ./pkg/extension/testing/... ./pkg/dag/... -count=1 -race
```

All must exit 0.

Cross-package check (no production source factory landed):
```bash
git grep -lE 'triggerSourceMarker\(\)' -- 'pkg/' | sort
```

Expected: only `pkg/extension/trigger.go` (interface declaration), `pkg/extension/testing/triggersource.go` (test stub), and `pkg/extension/trigger_test.go` (test). NO production source factory file.
</verification>

<success_criteria>
- TRIG-02 satisfied: `extension.TriggerSource` is a sealed marker interface; only types defined in pkg/extension or sub-packages can implement it (compile-time enforcement via unexported `triggerSourceMarker()`).
- D-07-06 / D-07-07 honored: sealed marker; pkg/extension is the home for the SDK contract (alongside Extension, OperationSpec, Credential).
- D-07-08 honored: NO production source factory ships; Phase 7.1+ adds real ones under owning extension namespaces.
- D-07-09 honored: the {kind, config} envelope shape is documented and round-trips via the kind-keyed factory registry; FakeTriggerSource demonstrates the credential-ID-only contract.
- Wave-1 unblocks Wave-2 (Plan 03 — parser builtinTrigger needs `extension.TriggerSource` for type assertion).
</success_criteria>

<output>
After completion, create `.planning/phases/07-trigger-primitive-server-shell/07-02-SUMMARY.md` documenting:
- The TriggerSource sealed interface (4 methods + the seal)
- The compile-time assertion `var _ dag.TriggerSource = TriggerSource(nil)` and what it guarantees
- The kind-keyed registry — `RegisterTriggerSourceFactory` signature, the strict-collision error literal, the no-factory error literal
- The init() wire-up via `dag.RegisterTriggerSourceUnmarshaler(extensionTriggerUnmarshaler)`
- The FakeTriggerSource stub location (`pkg/extension/testing`) + its two registered kinds (`skytime.test.webhook`, `skytime.test.cron`)
- The list of test functions in `pkg/extension/trigger_test.go` covering TRIG-02 + D-07-09 / D-07-10
- Confirmation that ZERO production source factory ships in Phase 7
</output>
