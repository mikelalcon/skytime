# Phase 1: Type Spine + Extension Contract + Parser/Bridge Foundations - Research

**Researched:** 2026-04-26
**Domain:** `go.starlark.net` API patterns for parser + lambda capture + state bridge; Go reflection for kwarg validation; error attribution across Starlark/Go boundaries; goldenfile testing patterns
**Confidence:** HIGH on Starlark API surface (verified against pkg.go.dev and official spec); HIGH on package layout (CONTEXT.md locks it); MEDIUM on the reflection-based kwarg validator (no widely-adopted helper exists — hand-rolling is the right call but needs care); HIGH on test corpus organization (`tests/fixtures/` with goldenfiles is a textbook Go pattern).

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Module Layout & Conventions**
- **D-01:** Module path `github.com/mikelalcon/skytime`. Use as canonical Go module path from day one.
- **D-02:** Public packages under `pkg/` (`pkg/dag`, `pkg/extension`, `pkg/parser`, `pkg/bridge`). Future non-public helpers under `internal/`. CLI at `cmd/skytime/` (Phase 4). Examples at `examples/...` (Phase 6).
- **D-03:** Co-located `*_test.go` files in the same package as the code under test. Top-level `tests/fixtures/` directory holds `.star` corpus files for parser tests and (later) the static-vs-runtime differential corpus test in Phase 4.
- **D-04:** Typed errors with `Position()` method. Concretely: `type ParseError struct { Pos syntax.Position; Msg string; Wrapped error }` and `type ValidationError struct { Pos syntax.Position; Flow, Step string; Msg string; Wrapped error }`. Both implement `error`, expose `Position()`, and format as `<file>:<line>:<col>: <msg>`. Use `errors.As` at boundaries.
- **D-05:** `go 1.25` in `go.mod`, toolchain `go1.26.2` available. Forced by `go.starlark.net`'s floor; do not bump to 1.26.
- **D-06:** Library accepts `*slog.Logger`; defaults to `slog.Default()`. No hard backend dependency.

**Extension SDK**
- **D-07:** Per-parser registry. `parser := skytime.NewParser(opts...)` then `parser.Register(github.New(...))`. No global state. Functional-options pattern acceptable as convenience but underlying mechanism is per-parser.
- **D-08:** **JIT credential resolution inside the activity.** Starlark's `gh = github.endpoint("admin")` creates a Starlark value carrying only the credential ID (`"admin"`). Every `ActionRef` derived from `gh` embeds `CredentialID="admin"` only. Credential handler runs inside `ExecuteBatch` (Phase 2 owns call site). Extensions may cache resolved credential internally. Workflow state, `ActionRef`, and Temporal history must never contain a resolved secret.
- **D-09:** Typed per-credential-kind interface. v1 ships `Credential` sealed interface with `BearerCredential`, `BasicCredential`, `APIKeyCredential` concrete kinds. Each has redacted `String()`. Adding new kinds is non-breaking.
- **D-10:** Single `CredentialHandler` registered on the worker (Phase 3 wires `worker.Run(client, flowDir, skytime.WithCredentialHandler(...))`). Phase 1 lays the interface and registration plumbing only.
- **D-11:** Operation kwargs schema is typed Go struct with reflection-based validation. Operations declare a parameter struct with `star:"name,required"` tags. Parser uses reflection to validate Starlark kwargs at parse time, report missing/unknown kwargs with `<file>:<line>:<col>` precision, and export schema for Phase 4 static validation.
- **D-12:** **`Idempotent` declaration is required, no default.** Extension registration fails if any operation is missing the declaration. Forces extension authors to make a conscious choice. Phase 1 picks struct field vs. method and locks it.

**load() Resolution & Multi-Flow**
- **D-13:** Both relative (`load("./shared/utils.star", ...)`) and absolute (`load("/shared/utils.star", ...)`) paths supported.
- **D-14:** Single root configured per parser. Discovery order: (1) explicit `WithRoot("/path/to/flows")` option (CLI exposes as `--rootdir` in Phase 4); (2) walk up from loading file looking for first `.git` directory; (3) if neither found, absolute `load()` is a parse error.
- **D-15:** A single `.star` file can declare multiple flows. Parser collects every `flow()` call into a `map[string]*dag.Flow` keyed by `Name`. Duplicate flow names across the parser session is a parse error.
- **D-16:** `call_flow` resolved by flow name within the parser session. Not found → parse error (not runtime). Sub-flows must be loaded by the time the parent flow's parsing completes.
- **D-17:** Single sandbox root passed to `NewParser`. `load()` cannot escape root (`../../etc/passwd`-style traversal is rejected with clear error).

**Lambda Capture Format**
- **D-18:** Stable lambda ID = `sha256(fileBytes)[:8] + ":" + line + ":" + col`. Stable across re-parse of same file content; resilient to lambda reordering. Phase 3's serialization decision keys off this format.
- **D-19:** Free variables allowed only if they resolve to **frozen module-level constants/functions**. Parser inspects every captured lambda's free vars at parse time. Mutable closures rejected with `<file>:<line>:<col>: lambda captures mutable variable 'X'`.
- **D-20:** Lambda-time predeclared globals — strict subset, locked in Phase 1, never expanded without explicit decision. Exact list: `len`, `str`, `int`, `float`, `bool`, `list`, `dict`, `tuple`, `fail`, comparison/arithmetic operators, struct attribute access, frozen-collection iteration helpers (`enumerate`, `zip`, `range`, `sorted`, `reversed`, `min`, `max`, `sum`, `any`, `all`, `abs`). Forbidden: `time.*`, `random.*`, any I/O, no `getattr` with dynamic lookup, no `set()`, no `load()` at lambda time. Implementation: single `lambdaTimeGlobals starlark.StringDict` constant in `pkg/bridge`. Test asserts the keys haven't changed since v1.
- **D-21:** Starlark's `print(...)` inside a lambda routes via `thread.Print` to `workflow.GetLogger(ctx).Info` (Phase 3 wires the actual logger; Phase 1 establishes the bridge hook). No credential scrubbing on `print` payloads in v1.
- **D-22:** `MaxExecutionSteps` defaults to `10_000_000` per lambda invocation. Override mechanism is a parser option. Cancellation watchdog is Phase 3's job.

### Claude's Discretion

The following implementation choices are open — Claude picks during planning:

- Exact Starlark builtin names that pass through to lambda-time (D-20 lists principles; exact set is what `pkg/bridge` ships).
- Internal layout of registration mechanism for `Idempotent` (D-12 says "required, no default" — Claude picks struct field vs. method).
- Sequence of parser passes (parse → lambda capture → flow registration → cross-flow resolution → lint).
- Test fixture organization under `tests/fixtures/` — directory structure and naming convention.
- Whether to use a single `Node` interface or distinct types with a sum-type-like `Kind` field on a base struct — pick whichever produces cleanest interpreter switch in Phase 3.
- Specific reflection helper for `star:"..."` struct tags — Claude can use existing decoder if it suits, or write a small one.

### Deferred Ideas (OUT OF SCOPE)

- **Packed Starlark libraries** — distributable bundles of `.star` files loadable by name (`load("@skytime-ops//github:reviews.star", "ensure_review")`). Phase 1's `load()` design (rootdir + relative + absolute) leaves room.
- **Hot-reload of `.star` files** — already deferred; door open because `parser.Parse` is a pure function.
- **Schema export** to JSON Schema or markdown — listed as v2 (`OPS-V2`); Phase 1's reflection-based kwarg schema is the foundation.
- **Tier-2 unit tests for `def` blocks** — listed as v2 (`TEST-V2-01`); Phase 1's restricted lambda environment is what Tier-2 will reuse.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| **DSL-01** | Starlark file declares a flow with `flow(name=..., inputs={...}, steps=[...])`, generating a deterministic `dag.Flow` at parse time | `*starlark.Builtin` + `UnpackArgs` for `flow(...)`; collected into `map[name]*dag.Flow` per D-15 |
| **DSL-02** | Sequential step with `step(action=...)` wrapping a single `ActionRef` | `step()` builtin; `ActionRef` is a custom `starlark.Value` returned by extension factories |
| **DSL-03** | Sequential block with `step(block=[a, b, c])` batching multiple `ActionRef`s | `step()` builtin accepts `block=` kwarg as `*starlark.List` of `ActionRef`s |
| **DSL-04** | Branch with `if_cond(cond=lambda ctx: ..., then=[...], else_=[...])` | `if_cond()` builtin extracts `*starlark.Function` from the `cond` kwarg, captures it as `CapturedLambda`, stores `LambdaID` on node |
| **DSL-05** | State transform with `script(id=..., fn=lambda ctx: {...}, output_alias=...)` | Same pattern as DSL-04 — capture the `fn` lambda |
| **DSL-06** | Fan-out with `for_each_parallel(items=..., item=..., steps=[...])` | `items=` accepts either a `*starlark.List` (static) or `*starlark.Function` (lambda producer); type switch determines which |
| **DSL-07** | Subflow invocation with `call_flow(name=..., inputs=..., child_options=...)` | Parse-time name resolution per D-16 — resolved against flow map after all files loaded |
| **DSL-08** | Step accepts Temporal `RetryPolicy` kwargs and timeouts | `RetryPolicy`, `StartToCloseTimeout`, `ScheduleToStartTimeout` as kwargs on `step()` and `for_each_parallel()` — pure data, never executed in Phase 1 |
| **DSL-09** | Lambdas access workflow state via dot-notation; bridge converts Go state maps into nested `*starlarkstruct.Struct` with deterministic key order | `starlarkstruct.FromStringDict(starlarkstruct.Default, dict)` after sorting Go map keys |
| **DSL-10** | `resolve.AllowLambda = true` set explicitly before any Starlark parse | The flag is currently a no-op (default `true`), but PARSE-03 requires explicit assignment as a documentation contract — also set `syntax.FileOptions{}` per file for forward compatibility |
| **EXT-01** | `Extension` Go interface exposes `Name()`, `Initialize(thread, kwargs)`, `Operations() map[string]OperationFunc` | Standard Go interface; `Initialize` returns the Starlark-callable extension instance |
| **EXT-02** | Extension factory methods (Starlark-callable) return `ActionRef` intents — never register Temporal activities | `ActionRef` is a custom `starlark.Value`; factory methods are `*starlark.Builtin`s that construct it |
| **EXT-03** | `OperationFunc` takes `context.Context` (stdlib), never `workflow.Context`; may not import `go.temporal.io/sdk/activity` | Go interface signature only in Phase 1; `forbidigo` lint enforces import boundary later |
| **EXT-04** | Each extension declares per-operation `Idempotent bool` | Per D-12, declared on operation spec; registration fails if missing |
| **EXT-05** | `Credential` typed value (with redacted `String()`) is the only legal way for an extension to receive a secret; workflow state stores only string ID | Sealed `Credential` interface per D-09; `String()` returns redacted form |
| **EXT-06** | Extensions registered statically (Go imports) or dynamically (runtime registry calls) — no plugin/gRPC/out-of-process loading | Per-parser registry per D-07; `parser.Register(ext)` is the single API |
| **PARSE-01** | Parser injects core DSL primitives as naked `*starlark.Builtin` globals (not namespaced) | `flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow` go directly into `predeclared starlark.StringDict` |
| **PARSE-02** | Parser supports `load()` for splitting flows; load resolution sandboxed to configured root | `thread.Load = func(t, module) (StringDict, error)`; resolver enforces root sandbox per D-17 |
| **PARSE-03** | Parser separates parse-time globals (richer) from lambda-time globals (restricted) | Two distinct `starlark.StringDict`s; D-20 fixes the lambda-time set |
| **PARSE-04** | Parser captures `*starlark.Function` lambdas keyed by stable IDs; stores them on `dag` nodes with `syntax.Position` | `Function.Position()` and `LambdaID` per D-18; nodes carry `syntax.Position` |
| **PARSE-05** | Malformed `.star` file produces position-aware error; never panics | `recover()` at parser entry; `EvalError.Backtrace()` captured; `ParseError.Position()` per D-04 |
| **PARSE-06** | Bridge's `CallLambda` always uses fresh `*starlark.Thread`, sets `MaxExecutionSteps`, wires `thread.Cancel` to `workflow.Context.Done()`, routes `Print` to workflow logger | `&starlark.Thread{}` per call; `SetMaxExecutionSteps(10_000_000)` per D-22; `Print` field per D-21; `Cancel` wiring lives in Phase 3 (interface stub in Phase 1) |
</phase_requirements>

## Summary

Phase 1 is the type spine + parser/bridge foundation. Every later phase imports `pkg/dag` and `pkg/extension`; every later parsing or validation step builds on `pkg/parser` and `pkg/bridge`. The technical work is dominated by `go.starlark.net` API patterns — six DSL builtins, two-environment split, lambda capture with free-var inspection, recursive `Freeze()` on custom values, sandboxed `load()`, and a fresh-thread-per-call bridge. None of it touches Temporal.

The single highest-leverage technical decision is **how to implement reflection-based kwarg validation** with `star:"name,required"` tags — D-11 mandates it but no off-the-shelf helper covers our exact needs. **Recommendation: hand-roll a small ~150-line reflector in `pkg/extension/schema.go` rather than pull in `vladimirvivien/startype` or build on `mapstructure`.** The hand-rolled version gives precise error attribution (the `<file>:<line>:<col>` requirement), exports the schema for Phase 4's static validator, and avoids a deps tree we'd inherit forever for ~150 lines of code.

The second-highest-leverage decision is **lambda ID stability vs. content addressing.** D-18 picks `sha256(fileBytes)[:8] + ":" + line + ":" + col`. This format **does compose well with both Phase 3 serialization options** (custom `DataConverter` and re-parse-on-start), but with one caveat that planning must account for: the prefix is `sha256(fileBytes)`, *not* `sha256(canonicalized_AST)` — so cosmetic edits (whitespace, comments) change the ID. That is *intentional* (Phase 3's re-parse-on-start can verify file content matches without re-canonicalizing AST), but it must be documented prominently so consultants understand why a comment edit invalidates running workflows that haven't migrated.

**Primary recommendation:** Build the parser as a single-pass `ExecFileOptions` over the `.star` file with all six builtins pre-registered in the parse-time `starlark.StringDict`. After exec, do a finalization pass that (a) collects all `*dag.Flow`s, (b) resolves `call_flow` references against the flow map, (c) walks every captured lambda's free vars asserting freeze + module-level scope, and (d) validates kwarg schemas against extension specs. Two `starlark.StringDict`s — never one. Fresh `*starlark.Thread` per invocation, never one — even at parse time. Custom `starlark.Value` types implement recursive `Freeze()` from day one.

## Standard Stack

### Core (locked by CONTEXT.md and prior research)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `go.starlark.net` | `v0.0.0-20260326113308-fadfc96def35` (verified 2026-04-26) | Starlark interpreter — DSL parsing, lambda capture, lambda evaluation | Canonical Google reference. Pseudo-version only — no tags, by design. Pin via `go mod tidy` after `go get @latest`. |
| `go.starlark.net/syntax` | (same module) | `syntax.Position`, `syntax.FileOptions`, AST node types | Required for D-04 error format and D-22 file-options-per-parse. |
| `go.starlark.net/resolve` | (same module) | `resolve.AllowLambda` global flag | DSL-10 mandates explicit `resolve.AllowLambda = true` even though it's the default. Setting it documents intent. |
| `go.starlark.net/starlarkstruct` | (same module) | `*starlarkstruct.Struct` for `ctx.req.repo_name` dot-access | Required by DSL-09. `starlarkstruct.FromStringDict(starlarkstruct.Default, dict)` is the construction idiom. |
| `log/slog` (stdlib) | Go 1.25 stdlib | Structured logging interface for the library | D-06: library accepts `*slog.Logger`, defaults to `slog.Default()`. No hard backend dep. |
| `crypto/sha256` (stdlib) | Go 1.25 stdlib | Lambda ID hashing per D-18 | `sha256.Sum256(fileBytes)[:4]` → 8 hex chars. |

### Supporting (test-only)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `github.com/stretchr/testify` | `v1.11.1` (verified) | Table-driven test assertions | `require.NoError`, `assert.Equal`. Co-located test files. |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Hand-rolled reflection-based kwarg validator | `vladimirvivien/startype` (`name`, `position`, `required`, `optional` tags) | Works but adds external dep with a loose maintenance posture (low download count, no recent activity verified). For ~150 LOC of reflection it isn't worth the dep. |
| Hand-rolled validator | `mitchellh/mapstructure` | Wrong abstraction — works on `map[string]interface{}`, not Starlark values. Requires double conversion. Reject. |
| Hand-rolled validator | Forking `starlark.UnpackArgs` | `UnpackArgs` is positional pairs (`"name?", &var`), not struct-tag-driven. Forking would re-implement most of it. The right call is to USE `UnpackArgs` for the six core DSL primitives (where signatures are fixed) and hand-roll a separate `extension.UnpackOperationKwargs(spec, args, kwargs, target)` for extension operation kwargs (where the schema is dynamic per operation). |
| Single `Node` interface | Sum-type `Kind` field on base `Node` struct | Discretion item from CONTEXT.md. **Recommendation: distinct types implementing a small `Node` interface with `Kind() string` and `Position() syntax.Position`.** This produces a clean type switch in Phase 3's interpreter walker (`switch n := node.(type) { case *dag.Step: ...; case *dag.IfCond: ... }`) and eliminates a category of bugs (constructing a `Step` with `IfCond` fields populated). Same approach as `temporalio/samples-go/dsl`. |
| `starlark.ExecFile` | `starlark.ExecFileOptions` | `ExecFile` is deprecated and uses legacy global flags. **Use `ExecFileOptions(opts, thread, filename, src, predeclared)` exclusively.** This is the only forward-compatible API. |

**Installation:**

```bash
# Already in go.mod (locked by Phase 0)
go get go.starlark.net@latest
go get github.com/stretchr/testify@v1.11.1
```

**Version verification (run before writing code):**

```bash
# Confirm pseudo-version is still current
curl -s https://proxy.golang.org/go.starlark.net/@latest
# Expected: v0.0.0-20260326113308-fadfc96def35 or newer (verified 2026-04-26)
```

## Architecture Patterns

### Recommended Project Structure (Phase 1 scope only)

```
github.com/mikelalcon/skytime/
├── go.mod                          # go 1.25, toolchain go1.26.2
├── pkg/
│   ├── dag/                        # Pure data: Node types, ActionRef, Flow, etc.
│   │   ├── node.go                 # Node interface + Flow, Step, IfCond, ForEachParallel, CallFlow, Script
│   │   ├── action.go               # ActionRef (Kind, Kwargs, CredentialID) — also a starlark.Value
│   │   ├── lambda.go               # CapturedLambda (Fn *starlark.Function, FreeVars StringDict, ID, Pos)
│   │   ├── retry.go                # RetryPolicy, Timeouts (kwargs from DSL-08)
│   │   ├── input.go                # WorkflowInput (Phase 3 will serialize this; Phase 1 just defines shape)
│   │   └── *_test.go               # Type construction + freeze cascade tests
│   │
│   ├── extension/                  # Extension SDK contract
│   │   ├── extension.go            # Extension interface (Name, Initialize, Operations)
│   │   ├── operation.go            # OperationFunc, OperationSpec (Name, Idempotent, KwargsType)
│   │   ├── credential.go           # Credential sealed interface, BearerCredential, BasicCredential, APIKeyCredential
│   │   ├── handler.go              # CredentialHandler interface (id → typed Credential)
│   │   ├── schema.go               # Reflection: ParseStarTag(reflect.StructField) -> starSpec; UnpackOperationKwargs
│   │   ├── registry.go             # Registry (per-parser, populated by parser.Register)
│   │   └── *_test.go
│   │
│   ├── parser/                     # Starlark → dag (parse phase)
│   │   ├── parser.go               # Parser type, NewParser, functional options, Parse(file) (*dag.Flow, map, error)
│   │   ├── builtins.go             # flow, step, if_cond, script, for_each_parallel, call_flow as *starlark.Builtin
│   │   ├── globals.go              # parseTimeGlobals (richer) — populated from registry + load
│   │   ├── thread.go               # newParseThread() — sets Load, Print, MaxExecutionSteps
│   │   ├── load.go                 # Sandboxed load resolver: relative + absolute + ../-rejection + .git root walk
│   │   ├── lambda_capture.go       # Captures *starlark.Function, computes LambdaID, walks free vars
│   │   ├── linter.go               # Free-var purity check, mutable-capture rejection
│   │   ├── errors.go               # ParseError, ValidationError types per D-04
│   │   ├── resolve_setup.go        # init() that sets resolve.AllowLambda = true (idempotent — safe to set anywhere)
│   │   └── *_test.go
│   │
│   ├── bridge/                     # state ↔ Starlark conversion + lambda invocation
│   │   ├── struct.go               # ToStarlarkStruct(map[string]any) *starlarkstruct.Struct (recursive, sorted keys)
│   │   ├── value.go                # FromStarlarkValue (lambda return → Go) — partial in Phase 1 (interpreter consumer is Phase 3)
│   │   ├── lambda_globals.go       # lambdaTimeGlobals StringDict — D-20 frozen subset
│   │   ├── lambda_call.go          # CallLambda(fn, ctx, opts) — fresh thread, MaxExecutionSteps, Print hook
│   │   ├── freeze.go               # MustFreeze helper for asserting freeze cascade in tests
│   │   └── *_test.go
│   │
└── tests/
    └── fixtures/                   # .star corpus files for parser tests
        ├── valid/
        │   ├── 01-minimal-flow.star
        │   ├── 02-all-primitives.star
        │   ├── 03-multi-flow-per-file.star
        │   ├── 04-load-relative.star + load-target.star
        │   ├── 05-load-absolute.star
        │   └── 06-call-flow-cross-file.star
        ├── invalid/
        │   ├── 01-missing-required-kwarg.star (expects: <file>:N:M: missing required 'name')
        │   ├── 02-mutable-capture.star (expects: lambda captures mutable variable 'counter')
        │   ├── 03-load-traversal.star (expects: load path escapes root)
        │   ├── 04-duplicate-flow-name.star
        │   ├── 05-call-flow-not-found.star
        │   ├── 06-unknown-extension.star
        │   ├── 07-forbidden-lambda-builtin.star (uses `time.now()`)
        │   └── 08-bad-syntax.star
        └── golden/
            ├── 01-minimal-flow.golden.json (canonical dag.Flow JSON)
            └── 02-all-primitives.golden.json
```

### Pattern 1: DSL Builtin (the six primitives)

**What:** Every DSL primitive is a `*starlark.Builtin` registered in the parse-time globals. The builtin signature is fixed by the Starlark API: `func(thread *Thread, fn *Builtin, args Tuple, kwargs []Tuple) (Value, error)`.

**When to use:** All six primitives (`flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow`).

**Canonical implementation:**

```go
// Source: go.starlark.net/starlark API + skytime CONTEXT.md
package parser

import (
    "go.starlark.net/starlark"
    "go.starlark.net/syntax"

    "github.com/mikelalcon/skytime/pkg/dag"
)

// step(action=..., block=[...], retry=..., timeout=...) -> *dag.Step
func builtinStep(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
    var (
        action  starlark.Value     // either an ActionRef or None
        block   *starlark.List     // either a list of ActionRefs or None
        retry   *dag.RetryPolicy   // optional, decoded via Unpacker interface
        timeout *dag.Timeout       // optional
    )

    if err := starlark.UnpackArgs("step", args, kwargs,
        "action?", &action,
        "block?", &block,
        "retry?", &retry,
        "timeout?", &timeout,
    ); err != nil {
        return nil, err
    }

    // Exactly-one-of validation
    if (action == nil || action == starlark.None) && (block == nil || block.Len() == 0) {
        return nil, fmt.Errorf("step: must provide exactly one of 'action' or 'block'")
    }
    if action != nil && action != starlark.None && block != nil && block.Len() > 0 {
        return nil, fmt.Errorf("step: must provide exactly one of 'action' or 'block' (not both)")
    }

    // Position is grabbed from the call frame, not the args
    pos := callerPosition(thread)

    return &dag.Step{
        Pos:     pos,
        Actions: collectActions(action, block),
        Retry:   retry,
        Timeout: timeout,
    }, nil
}

// callerPosition extracts the syntax.Position of the call site from the thread's call stack.
func callerPosition(thread *starlark.Thread) syntax.Position {
    if thread.CallStackDepth() < 2 {
        return syntax.Position{} // invalid; should be impossible during parse
    }
    return thread.CallFrame(1).Pos
}
```

**Notes:**
- `UnpackArgs` accepts `bool, int*, uint*, string, *List, *Dict, Callable, Iterable, Value, Unpacker` — see [starlark-go unpack.go](https://github.com/google/starlark-go/blob/master/starlark/unpack.go).
- For `RetryPolicy` and `Timeout`, implement `Unpacker` so they can be unpacked from a Starlark `*Dict` directly: `func (r *RetryPolicy) Unpack(v starlark.Value) error { ... }`.
- `?` suffix on a name marks the kwarg optional; `??` means "treat None as absent." Once any name is `?`, all subsequent are implicitly optional.
- The position is captured from the *caller's* frame (depth 1), not from the args themselves — this gives `<file>:<line>:<col>` of the `step(...)` call site in the `.star` file. This is essential for D-04.

### Pattern 2: Custom `starlark.Value` Types — Freeze Cascade

**What:** `*dag.ActionRef` and `*dag.CapturedLambda` are returned from Starlark code, so they must implement `starlark.Value` (`String`, `Type`, `Freeze`, `Truth`, `Hash`).

**When to use:** Any Go type that crosses the Starlark/Go boundary as a returned value.

**Critical rule from [Starlark spec impl.md](https://chromium.googlesource.com/external/github.com/google/starlark-go/+/HEAD/doc/impl.md):**
> "Every value defines a Freeze method that sets its own frozen flag if not already set, and calls Freeze for each value that it contains. Application-defined types must also follow this discipline."

**Canonical implementation:**

```go
// Source: starlark-go spec impl.md + dag types
package dag

import (
    "fmt"
    "sort"

    "go.starlark.net/starlark"
)

type ActionRef struct {
    Kind         string                  // e.g. "github.create_issue"
    Kwargs       *starlark.Dict          // mutable-by-default; freeze required
    CredentialID string
    Pos          syntax.Position
    frozen       bool
}

// Compile-time interface assertion.
var _ starlark.Value = (*ActionRef)(nil)

func (a *ActionRef) String() string { return fmt.Sprintf("ActionRef(%s)", a.Kind) }
func (a *ActionRef) Type() string   { return "ActionRef" }
func (a *ActionRef) Truth() starlark.Bool { return starlark.True }
func (a *ActionRef) Hash() (uint32, error) {
    return 0, fmt.Errorf("ActionRef is not hashable") // unhashable is OK; Starlark spec allows it
}

func (a *ActionRef) Freeze() {
    if a.frozen {
        return
    }
    a.frozen = true
    if a.Kwargs != nil {
        a.Kwargs.Freeze()       // recursive freeze of the dict contents
    }
}
```

### Anti-Patterns to Avoid

- **No-op `Freeze()` on a custom value type.** Pitfall #2 / #6 root cause. Any field that holds a `*starlark.Dict`, `*starlark.List`, or `starlark.Value` must have `Freeze()` called on it. **Test:** every custom value type ships with a "freeze cascade" unit test that constructs the value with mutable contents, calls `Freeze()`, and asserts the contents are frozen.
- **Reusing one `*starlark.Thread` across calls.** Pitfall #1. Even at parse time, every `ExecFileOptions` invocation gets a fresh thread. At lambda eval time (Phase 3), every `starlark.Call` gets a fresh thread.
- **Putting `*starlark.Thread` or `*starlark.Function` into anything that crosses a serialization boundary.** Pitfall #1 + #4. The DAG holds `*starlark.Function` only because it never leaves the worker process boundary (Phase 3's serialization decision handles this). Never embed `*starlark.Thread` in a DAG type.
- **Single predeclared environment for parse + lambda.** Pitfall #3. PARSE-03 / D-20 split is non-negotiable.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Argument unpacking for the six DSL primitives | A custom positional-args walker | `starlark.UnpackArgs(name, args, kwargs, "x", &x, "y?", &y, ...)` | Battle-tested, supports optional/required marking via `?`/`??`, error messages match Starlark conventions. |
| Recursive Go-map → Starlark-struct conversion | Iterate `reflect.Map` and build `*starlark.Dict` | `starlarkstruct.FromStringDict(starlarkstruct.Default, dict)` after manually sorting keys | The official idiom. We MUST sort keys ourselves before constructing the StringDict (Pitfall #3 — Go map iteration is random). |
| Path-traversal-safe `load()` resolver | A `filepath.Clean` + `strings.HasPrefix` check | Go 1.24+'s `os.Root` API for traversal-resistant file APIs | `os.Root.Open(path)` is the canonical traversal-resistant primitive on the modern Go runtime. |
| Lambda free-var inspection | Walking the AST manually | `Function.NumFreeVars()` + `Function.FreeVar(i) (Binding, Value)` | Go-side inspection. `Binding` carries `Name string` and `Pos syntax.Position` — perfect for D-04 error attribution on mutable-capture rejections. |
| Source-position capture | Custom file/line tracking | `syntax.Position` fields (`Filename()`, `Line`, `Col`) — and `Function.Position()` | Every `*starlark.Function` already carries its definition position. DAG nodes get position from the call-frame walk in `callerPosition(thread)`. |
| Freeze cascade verification | A custom "is this frozen" walker | Lean on the standard freeze contract — every `Value.Freeze()` recursively freezes children | Any custom value type just delegates. Don't build a freeze-walker. |

**Key insight:** `go.starlark.net` already provides 95% of what Phase 1 needs. The only thing we hand-roll is the **reflection-based kwarg validator for extension operations** (D-11) — but it's small (~150 LOC) and gives us schema export for free, which Phase 4 needs.

## Common Pitfalls

### Pitfall 1: Go map iteration order leaks into the DAG

**What goes wrong:** `bridge.ToStarlarkStruct(map[string]any{...})` is called twice on the same map. First call iterates keys in order `[a, b, c]` (random Go iteration); second iterates `[c, a, b]`. The resulting `*starlarkstruct.Struct`s have the same fields but the underlying `StringDict` has different insertion order, which feeds into different `Hash()` results and breaks Temporal replay determinism (Pitfall #3 from PITFALLS.md).

**Why it happens:** Go's `for k, v := range m` is intentionally randomized. `starlark.StringDict` is a `map[string]Value` — same problem.

**How to avoid:**

```go
// Source: bridge/struct.go pattern
func ToStarlarkStruct(m map[string]any) *starlarkstruct.Struct {
    keys := make([]string, 0, len(m))
    for k := range m {
        keys = append(keys, k)
    }
    sort.Strings(keys) // ⚠ MANDATORY — never iterate the map directly

    sd := starlark.StringDict{}
    for _, k := range keys {
        sd[k] = toStarlarkValue(m[k]) // recursive
    }
    return starlarkstruct.FromStringDict(starlarkstruct.Default, sd)
}
```

**Warning signs:** The same workflow input produces different `Hash()` on different runs. Replay-twice tests fail intermittently. **Verification:** Phase 1's success criterion #4 mandates an "iteration-determinism unit test that converts the same map twice and asserts byte-equal Starlark dicts."

### Pitfall 2: `resolve.AllowLambda` is now a no-op — but DSL-10 still requires the assignment

**What goes wrong:** A reviewer reads the `go.starlark.net` resolve docs, sees that `AllowLambda` is "obsolete" (it's `true` by default and has no effect), and removes the explicit `resolve.AllowLambda = true` line. A future Starlark major version revives the flag in a different sense and our parser silently breaks.

**Why it happens:** [The current `go.starlark.net/resolve` docs](https://pkg.go.dev/go.starlark.net/resolve) mark `AllowLambda` as obsolete:

> ```go
> // Obsolete flags for features that are now standard. No effect.
> AllowBitwise   = true
> AllowFloat     = true
> AllowLambda    = true
> ```

**How to avoid:** D-10/DSL-10 mandates the explicit assignment — treat it as a documentation contract for downstream maintainers. **Implementation:** put it in `pkg/parser/resolve_setup.go` with a code comment explaining *why* the assignment exists despite the obsolete flag:

```go
// Source: pkg/parser/resolve_setup.go
package parser

import "go.starlark.net/resolve"

// init explicitly enables lambda support. As of go.starlark.net 2026-03-26,
// resolve.AllowLambda is documented as obsolete (true by default), but DSL-10
// requires the explicit assignment as a documentation contract — lambdas are
// the *only* legal expression-evaluation surface in Skytime, and removing this
// line would obscure that intent.
//
// We also pass syntax.FileOptions{} to ExecFileOptions so per-file behavior
// is forward-compatible should the resolve package be refactored.
func init() {
    resolve.AllowLambda = true
}
```

**Warning signs:** Linter or refactorer removes "dead" code. **Verification:** ship a test `TestResolveAllowLambdaIsSet` that asserts `resolve.AllowLambda == true` after package init. The test exists to prevent regression, not to verify functionality.

### Pitfall 3: `Function.Position()` returns the def site, not the call site

**What goes wrong:** Builtin code does `pos := fn.Position()` to attribute a parse error to a lambda's location. The error then points at the line where `lambda ctx: ...` was defined, not the line where `if_cond(cond=lambda ctx: ...)` was called. Two different `if_cond(...)` calls passing the same lambda function get attributed to the same line.

**Why it happens:** [`Function.Position()`](https://pkg.go.dev/go.starlark.net/starlark#Function.Position) returns the position of the function's `def` or `lambda` keyword. The *call site* is in the thread's call frame. For D-04 errors at the DSL-primitive level, we want the call site (`step(...)` line). For lambda-specific errors (mutable capture), we want the def site (the `lambda` keyword).

**How to avoid:** Be explicit about which position you want at every error site:

```go
// For DSL primitive errors (step, flow, etc.) — caller position
pos := thread.CallFrame(1).Pos

// For lambda-specific errors (mutable capture) — def position
pos := fn.Position()

// For free-variable binding errors — binding position
binding, _ := fn.FreeVar(i)
pos := binding.Pos
```

**Warning signs:** Test asserts an error points at line N, but the actual error points at line M. `Function.Position()` and `Binding.Pos` look interchangeable but mean different things.

### Pitfall 4: `ExecFile` (deprecated) silently uses package-level resolve flags

**What goes wrong:** Code uses `starlark.ExecFile(thread, filename, src, predeclared)`. It works — but it relies on legacy global flags (`resolve.AllowGlobalReassign` etc.). A future major version of `go.starlark.net` removes the global-flag path; our parser breaks. Worse: setting flags in `init()` of one test affects all subsequent tests because globals are process-scoped.

**How to avoid:** Always use `starlark.ExecFileOptions(opts, thread, filename, src, predeclared)` with an explicit `*syntax.FileOptions`:

```go
// Source: pkg/parser/parser.go
opts := &syntax.FileOptions{
    Set:               false, // D-20: no set() in lambda env (here we apply at parse time too — discretion)
    While:             false, // forbid `while` (forces non-determinism risk)
    TopLevelControl:   true,  // allow top-level if/for in .star files
    GlobalReassign:    false, // D-20 spirit: no top-level reassign
    LoadBindsGlobally: false, // load bindings stay file-local
    Recursion:         false, // forbid recursion (determinism + bounded execution)
}

globals, err := starlark.ExecFileOptions(opts, thread, filename, src, parseTimeGlobals)
```

**Warning signs:** Tests pass individually but fail in `go test ./...`. Behavior depends on test execution order. Linter flags the `ExecFile` call as deprecated.

### Pitfall 5: Lambda free-var walk misses bindings to module-level functions

**What goes wrong:** D-19 says "free vars allowed if they resolve to frozen module-level constants/functions." The naive walk inspects `Function.FreeVar(i)` and rejects anything that is *itself* a `*starlark.Function`. But `def`-declared helper functions in the module are exactly what should be allowed.

**Why it happens:** A `*starlark.Function` value is "frozen" in the sense that the module's globals were frozen post-init, but the function's `Truth()`/`Hash()` doesn't expose a "is this a frozen module-level value" predicate.

**How to avoid:** Compare the free-var's `Binding.Pos` against the parsed file's top-level scope (any line at indent-zero):

```go
// Source: pkg/parser/linter.go
func isModuleLevelBinding(filename string, binding starlark.Binding) bool {
    // Module-level bindings are at the top of the file, never inside a def or lambda body.
    // The binding's position file must match the function's containing file (for cross-file
    // bindings via load(), this is the load target file).
    return binding.Pos.Filename() == filename && binding.Pos.Col == 1
}

// For each captured lambda, walk free vars
for i := 0; i < fn.NumFreeVars(); i++ {
    binding, value := fn.FreeVar(i)
    if !isModuleLevelBinding(filename, binding) {
        return &ParseError{
            Pos: binding.Pos,
            Msg: fmt.Sprintf("lambda captures non-module-level variable %q", binding.Name),
        }
    }
    // Even if module-level, double-check it's frozen (defensive — Starlark guarantees this post-init)
    if !isFrozen(value) {
        return &ParseError{
            Pos: binding.Pos,
            Msg: fmt.Sprintf("lambda captures unfrozen module-level variable %q (likely a custom value type with broken Freeze())", binding.Name),
        }
    }
}
```

**Warning signs:** Tests pass for trivial lambdas (`lambda ctx: ctx.x`) but reject lambdas that call helper `def`s. **Verification:** the corpus must include `tests/fixtures/valid/02-all-primitives.star` with a `def`-declared helper called from a lambda — test asserts it parses cleanly.

### Pitfall 6: `print()` route hook is `thread.Print = func(*Thread, string)`, not a builtin

**What goes wrong:** A reviewer adds `print` to the lambda-time globals as a `*starlark.Builtin`. It works but routes `print()` calls through user-overridable code. The user (or a buggy extension) overrides it with a no-op or a panic.

**How to avoid:** Use the [`Thread.Print`](https://pkg.go.dev/go.starlark.net/starlark#Thread) field — it's the canonical hook for `print()`:

```go
// Source: pkg/bridge/lambda_call.go
thread := &starlark.Thread{
    Name: "skytime-lambda",
    Print: func(thread *starlark.Thread, msg string) {
        // Phase 1: log to slog at parse time (lambdas don't run at parse time)
        // Phase 3: this hook is replaced with one that calls workflow.GetLogger(ctx).Info
        slog.Default().Info("starlark print", "msg", msg)
    },
}
thread.SetMaxExecutionSteps(10_000_000) // D-22
```

**Warning signs:** `print()` doesn't appear in logs, or appears via a different code path than expected. Test that asserts `print("hello")` calls the hook is missing.

## Code Examples

### Two-environment split (PARSE-03 / D-20)

```go
// Source: pkg/parser/globals.go
// Parse-time globals: richer surface for top-level Starlark code
func newParseTimeGlobals(reg *extension.Registry) starlark.StringDict {
    g := starlark.StringDict{
        // The six core DSL primitives (PARSE-01)
        "flow":              starlark.NewBuiltin("flow", builtinFlow),
        "step":              starlark.NewBuiltin("step", builtinStep),
        "if_cond":           starlark.NewBuiltin("if_cond", builtinIfCond),
        "script":            starlark.NewBuiltin("script", builtinScript),
        "for_each_parallel": starlark.NewBuiltin("for_each_parallel", builtinForEachParallel),
        "call_flow":         starlark.NewBuiltin("call_flow", builtinCallFlow),
    }
    // Extension factories (e.g., "github" -> github.endpoint)
    for name, ext := range reg.All() {
        g[name] = ext.AsStarlarkValue()
    }
    return g
}

// pkg/bridge/lambda_globals.go
// Lambda-time globals: strict subset (D-20). LOCKED — never expand without explicit decision.
var lambdaTimeGlobals = func() starlark.StringDict {
    sd := starlark.StringDict{
        // Type constructors / coercions
        "len":    starlark.Universe["len"],
        "str":    starlark.Universe["str"],
        "int":    starlark.Universe["int"],
        "float":  starlark.Universe["float"],
        "bool":   starlark.Universe["bool"],
        "list":   starlark.Universe["list"],
        "dict":   starlark.Universe["dict"],
        "tuple":  starlark.Universe["tuple"],
        // Failure
        "fail":   starlark.Universe["fail"],
        // Frozen-collection iteration helpers
        "enumerate": starlark.Universe["enumerate"],
        "zip":       starlark.Universe["zip"],
        "range":     starlark.Universe["range"],
        "sorted":    starlark.Universe["sorted"],
        "reversed":  starlark.Universe["reversed"],
        "min":       starlark.Universe["min"],
        "max":       starlark.Universe["max"],
        "sum":       starlark.Universe["sum"],
        "any":       starlark.Universe["any"],
        "all":       starlark.Universe["all"],
        "abs":       starlark.Universe["abs"],
        // Notably absent: print (handled via thread.Print), set (D-20 forbids),
        // load (parse-time only), getattr (no dynamic lookup), time/random/I/O.
    }
    sd.Freeze() // freeze the dict itself; values are already frozen Universe entries
    return sd
}()

// Test that locks the set:
func TestLambdaTimeGlobalsLocked(t *testing.T) {
    expected := []string{
        "len", "str", "int", "float", "bool", "list", "dict", "tuple",
        "fail",
        "enumerate", "zip", "range", "sorted", "reversed",
        "min", "max", "sum", "any", "all", "abs",
    }
    actual := lambdaTimeGlobals.Keys()
    sort.Strings(expected); sort.Strings(actual)
    require.Equal(t, expected, actual,
        "lambda-time globals changed — D-20 requires explicit decision before adding/removing")
}
```

### Sandboxed `load()` resolver (PARSE-02 / D-13–D-17)

```go
// Source: pkg/parser/load.go
package parser

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "go.starlark.net/starlark"
    "go.starlark.net/syntax"
)

func (p *Parser) makeLoad() func(*starlark.Thread, string) (starlark.StringDict, error) {
    return func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
        // Resolve module path against parser root + caller file
        callerFile := thread.CallFrame(1).Pos.Filename()
        resolved, err := p.resolveLoadPath(callerFile, module)
        if err != nil {
            return nil, err // ParseError with clear message per D-17
        }

        // Consult the load cache (idempotent loads)
        if cached, ok := p.loadCache[resolved]; ok {
            return cached.globals, cached.err
        }

        src, err := os.ReadFile(resolved)
        if err != nil {
            return nil, &ParseError{
                Pos: thread.CallFrame(1).Pos,
                Msg: fmt.Sprintf("load %q: %v", module, err),
            }
        }

        // Fresh thread for the loaded module (Pitfall #1 — never reuse threads)
        loadThread := &starlark.Thread{
            Name: "load:" + resolved,
            Load: p.makeLoad(),
            Print: thread.Print, // inherit print hook
        }
        loadThread.SetMaxExecutionSteps(10_000_000)

        opts := &syntax.FileOptions{} // standard options; lambdas allowed (default)
        globals, err := starlark.ExecFileOptions(opts, loadThread, resolved, src, p.parseTimeGlobals)
        if err != nil {
            // Wrap Starlark errors into our typed ParseError per D-04
            err = wrapStarlarkError(err)
        }

        p.loadCache[resolved] = loadCacheEntry{globals: globals, err: err}
        return globals, err
    }
}

func (p *Parser) resolveLoadPath(callerFile, module string) (string, error) {
    var candidate string
    switch {
    case strings.HasPrefix(module, "/"):
        // Absolute (D-13): from configured root
        if p.root == "" {
            return "", &ParseError{Msg: "absolute load path used but no root configured (set WithRoot or place flows under a .git ancestor)"}
        }
        candidate = filepath.Join(p.root, module)
    case strings.HasPrefix(module, "./") || strings.HasPrefix(module, "../"):
        // Relative (D-13): sibling of caller file
        candidate = filepath.Join(filepath.Dir(callerFile), module)
    default:
        // No prefix → bare name → treat as relative
        candidate = filepath.Join(filepath.Dir(callerFile), module)
    }

    abs, err := filepath.Abs(candidate)
    if err != nil {
        return "", &ParseError{Msg: fmt.Sprintf("resolve load %q: %v", module, err)}
    }

    // D-17 sandbox: must stay within root
    rootAbs, err := filepath.Abs(p.root)
    if err != nil {
        return "", &ParseError{Msg: fmt.Sprintf("resolve root %q: %v", p.root, err)}
    }
    rel, err := filepath.Rel(rootAbs, abs)
    if err != nil || strings.HasPrefix(rel, "..") {
        return "", &ParseError{Msg: fmt.Sprintf("load %q: path escapes parser root %q", module, rootAbs)}
    }

    return abs, nil
}
```

**Note:** Go 1.24+'s `os.Root` API offers a more traversal-resistant primitive (`os.Root.Open` rejects symlinks that escape). For Phase 1 the `filepath.Rel` + `..` check is sufficient — but **planning should consider switching to `os.Root` if any test exposes a symlink-traversal CVE**.

### Lambda capture and free-var inspection (PARSE-04 / D-18, D-19)

```go
// Source: pkg/parser/lambda_capture.go
package parser

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"

    "go.starlark.net/starlark"

    "github.com/mikelalcon/skytime/pkg/dag"
)

// captureLambda extracts a *starlark.Function from a kwarg, computes its stable ID,
// and returns a CapturedLambda after validating its free variables.
func (p *Parser) captureLambda(thread *starlark.Thread, fileBytes []byte, kwargName string, val starlark.Value) (*dag.CapturedLambda, error) {
    fn, ok := val.(*starlark.Function)
    if !ok {
        return nil, &ParseError{
            Pos: thread.CallFrame(1).Pos,
            Msg: fmt.Sprintf("kwarg %q must be a lambda or function, got %s", kwargName, val.Type()),
        }
    }

    // D-18: stable ID = sha256(fileBytes)[:8] hex + ":" + line + ":" + col
    pos := fn.Position()
    sum := sha256.Sum256(fileBytes)
    id := fmt.Sprintf("%s:%d:%d", hex.EncodeToString(sum[:4]), pos.Line, pos.Col)

    // D-19: validate free vars
    for i := 0; i < fn.NumFreeVars(); i++ {
        binding, value := fn.FreeVar(i)
        if err := p.validateFreeVar(pos.Filename(), binding, value); err != nil {
            return nil, err // ParseError with binding.Pos
        }
    }

    return &dag.CapturedLambda{
        ID:       id,
        Fn:       fn,
        Pos:      pos,
        FreeVars: collectFreeVarsAsStringDict(fn),
    }, nil
}

func (p *Parser) validateFreeVar(filename string, binding starlark.Binding, value starlark.Value) error {
    // D-19: free vars must be module-level (top-level scope, col == 1) AND frozen
    if binding.Pos.Filename() != filename || binding.Pos.Col != 1 {
        return &ParseError{
            Pos: binding.Pos,
            Msg: fmt.Sprintf("lambda captures mutable variable %q (free vars must reference module-level constants/functions only)", binding.Name),
        }
    }
    // Frozen check: starlark-go freezes module globals after init, so this should always pass
    // for properly-constructed values. The defensive check here catches custom value types
    // with broken Freeze() implementations (Pitfall #6).
    return nil
}
```

### Reflection-based kwarg validator (D-11)

```go
// Source: pkg/extension/schema.go
package extension

import (
    "fmt"
    "reflect"
    "strings"

    "go.starlark.net/starlark"
    "go.starlark.net/syntax"
)

// FieldSpec describes one field in an operation's parameter struct, derived from
// `star:"name,required"` tags via reflection. Exported so Phase 4 can serialize for static validation.
type FieldSpec struct {
    GoName    string         // Field name in the Go struct (for error messages)
    StarName  string         // Name as Starlark callers see it (kwarg key)
    Required  bool           // True if `,required` is present
    GoType    reflect.Type   // Used for type checking and conversion
    FieldIdx  int            // Index in the struct for reflect.Value.Field(i)
}

// ParseSchema reflects on a struct type to extract its FieldSpecs.
// Called once per OperationSpec at registration time, never per parse.
func ParseSchema(t reflect.Type) ([]FieldSpec, error) {
    if t.Kind() == reflect.Ptr {
        t = t.Elem()
    }
    if t.Kind() != reflect.Struct {
        return nil, fmt.Errorf("ParseSchema: %v is not a struct", t)
    }

    var specs []FieldSpec
    for i := 0; i < t.NumField(); i++ {
        f := t.Field(i)
        tag := f.Tag.Get("star")
        if tag == "" {
            continue // unexported or untagged fields are ignored
        }
        parts := strings.Split(tag, ",")
        spec := FieldSpec{
            GoName:   f.Name,
            StarName: parts[0],
            GoType:   f.Type,
            FieldIdx: i,
        }
        for _, opt := range parts[1:] {
            if opt == "required" {
                spec.Required = true
            }
        }
        if spec.StarName == "" {
            return nil, fmt.Errorf("ParseSchema: field %s has empty star: name", f.Name)
        }
        specs = append(specs, spec)
    }
    return specs, nil
}

// UnpackOperationKwargs validates Starlark kwargs against a schema and populates a target struct.
// Errors are position-aware (D-04 format) — caller passes the call-site Position.
func UnpackOperationKwargs(opName string, callPos syntax.Position, specs []FieldSpec, kwargs []starlark.Tuple, target any) error {
    // Build a kwarg lookup
    seen := make(map[string]starlark.Value, len(kwargs))
    for _, kv := range kwargs {
        keyStr, ok := kv[0].(starlark.String)
        if !ok {
            return &ValidationError{Pos: callPos, Msg: fmt.Sprintf("%s: kwarg key must be string", opName)}
        }
        seen[string(keyStr)] = kv[1]
    }

    // Check required
    for _, spec := range specs {
        if spec.Required {
            if _, ok := seen[spec.StarName]; !ok {
                return &ValidationError{
                    Pos: callPos,
                    Msg: fmt.Sprintf("%s: missing required kwarg %q", opName, spec.StarName),
                }
            }
        }
    }

    // Check unknown
    known := make(map[string]bool, len(specs))
    for _, spec := range specs {
        known[spec.StarName] = true
    }
    for k := range seen {
        if !known[k] {
            return &ValidationError{
                Pos: callPos,
                Msg: fmt.Sprintf("%s: unknown kwarg %q", opName, k),
            }
        }
    }

    // Populate target via reflection
    tv := reflect.ValueOf(target).Elem()
    for _, spec := range specs {
        v, present := seen[spec.StarName]
        if !present {
            continue // optional and absent
        }
        if err := assignStarlarkToGo(tv.Field(spec.FieldIdx), v); err != nil {
            return &ValidationError{
                Pos: callPos,
                Msg: fmt.Sprintf("%s: kwarg %q: %v", opName, spec.StarName, err),
            }
        }
    }
    return nil
}

// assignStarlarkToGo handles the basic types: string, int, bool, []string, map[string]string.
// Extend per Phase 1 requirements; ~80 LOC of switch on reflect.Kind.
func assignStarlarkToGo(dst reflect.Value, src starlark.Value) error {
    // ... ~80 LOC of type switch — implementation detail, not load-bearing here
    return nil
}
```

**Why hand-roll this:** It's ~150 LOC total, gives precise error attribution (the call-site `syntax.Position`), and exports the schema (`[]FieldSpec`) for Phase 4's static validator and the future v2 JSON Schema export. `vladimirvivien/startype` covers similar ground but adds a dep with uncertain maintenance and doesn't surface position info naturally.

### Error wrapping at the eval boundary (PARSE-05 / D-04)

```go
// Source: pkg/parser/errors.go
package parser

import (
    "errors"
    "fmt"

    "go.starlark.net/starlark"
    "go.starlark.net/syntax"
)

type ParseError struct {
    Pos     syntax.Position
    Msg     string
    Wrapped error
}

func (e *ParseError) Error() string {
    if e.Pos.IsValid() {
        return fmt.Sprintf("%s:%d:%d: %s", e.Pos.Filename(), e.Pos.Line, e.Pos.Col, e.Msg)
    }
    return e.Msg
}

func (e *ParseError) Position() syntax.Position { return e.Pos }
func (e *ParseError) Unwrap() error             { return e.Wrapped }

// wrapStarlarkError converts go.starlark.net errors into typed ParseErrors.
// Captures EvalError.Backtrace() and SyntaxError.Pos so Phase 4's CLI can render them.
func wrapStarlarkError(err error) error {
    if err == nil {
        return nil
    }
    var evalErr *starlark.EvalError
    if errors.As(err, &evalErr) {
        bt := evalErr.Backtrace()
        // Walk the backtrace to find the .star-side frame closest to the failure
        var pos syntax.Position
        if len(evalErr.CallStack) > 0 {
            pos = evalErr.CallStack[0].Pos
        }
        return &ParseError{Pos: pos, Msg: evalErr.Msg + "\n" + bt, Wrapped: err}
    }
    var syntaxErr syntax.Error
    if errors.As(err, &syntaxErr) {
        return &ParseError{Pos: syntaxErr.Pos, Msg: syntaxErr.Msg, Wrapped: err}
    }
    return &ParseError{Msg: err.Error(), Wrapped: err}
}
```

### Test fixture organization

```go
// Source: pkg/parser/parser_test.go
package parser_test

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"

    "github.com/mikelalcon/skytime/pkg/parser"
)

// TestValidFixtures parses every .star file under tests/fixtures/valid/ and asserts no error.
// For files with a matching .golden.json, asserts the parsed dag matches the golden.
func TestValidFixtures(t *testing.T) {
    fixtures, err := filepath.Glob("../../tests/fixtures/valid/*.star")
    require.NoError(t, err)
    for _, f := range fixtures {
        f := f
        t.Run(filepath.Base(f), func(t *testing.T) {
            p := parser.NewParser(parser.WithRoot("../../tests/fixtures"))
            flows, err := p.ParseFile(f)
            require.NoError(t, err)

            goldenPath := strings.TrimSuffix(f, ".star") + ".golden.json"
            if _, err := os.Stat(goldenPath); err == nil {
                got, _ := json.MarshalIndent(flows, "", "  ")
                want, _ := os.ReadFile(goldenPath)
                if os.Getenv("UPDATE_GOLDEN") != "" {
                    os.WriteFile(goldenPath, got, 0644)
                    return
                }
                assert.JSONEq(t, string(want), string(got))
            }
        })
    }
}

// TestInvalidFixtures parses every .star file under tests/fixtures/invalid/ and asserts:
// (a) parsing fails, (b) the error is a *ParseError, (c) the error format matches <file>:<line>:<col>: <msg>,
// (d) the error message contains the substring in the file's first comment line.
func TestInvalidFixtures(t *testing.T) {
    fixtures, err := filepath.Glob("../../tests/fixtures/invalid/*.star")
    require.NoError(t, err)
    for _, f := range fixtures {
        f := f
        t.Run(filepath.Base(f), func(t *testing.T) {
            // Convention: first comment line of each invalid fixture is "# expects: <substring>"
            data, err := os.ReadFile(f)
            require.NoError(t, err)
            firstLine := strings.SplitN(string(data), "\n", 2)[0]
            require.True(t, strings.HasPrefix(firstLine, "# expects:"))
            expectSub := strings.TrimPrefix(firstLine, "# expects: ")

            p := parser.NewParser(parser.WithRoot("../../tests/fixtures"))
            _, parseErr := p.ParseFile(f)
            require.Error(t, parseErr)

            var pe *parser.ParseError
            require.True(t, errors.As(parseErr, &pe), "error must be *ParseError")
            assert.Contains(t, pe.Error(), expectSub, "error must contain expected substring")

            // Format check: <file>:<line>:<col>: <msg>
            assert.Regexp(t, `^[^:]+:\d+:\d+: `, pe.Error())
        })
    }
}
```

**Convention:** every `tests/fixtures/invalid/*.star` file starts with `# expects: <substring>` declaring the error message it must trigger. This couples the fixture to the assertion. Update both together — never the message without the fixture.

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `starlark.ExecFile(thread, name, src, predeclared)` | `starlark.ExecFileOptions(opts, thread, name, src, predeclared)` | Pre-2024 to current | `ExecFile` is deprecated; rely on `*syntax.FileOptions` instead of package-level resolve flags. Use `ExecFileOptions` everywhere in Phase 1. |
| `resolve.AllowLambda = true` (functional) | `resolve.AllowLambda = true` (no-op + per-file `syntax.FileOptions{}`) | 2024+ | Flag is now obsolete (lambdas always allowed). DSL-10 still mandates the explicit assignment as documentation contract. |
| `filepath.Clean` + manual `..` check for sandbox | Go 1.24+'s `os.Root` API | 2024-08+ (Go 1.24) | Phase 1 can ship with `filepath.Rel` check; switch to `os.Root` if symlink traversal becomes a concern. |
| Single Thread reused for multiple `Call`s | Fresh `*starlark.Thread` per evaluation | Always (community lesson, not API change) | Pitfall #1 root cause. Documented in `cadence-workflow/starlark-worker` and the Skytime PITFALLS.md. |
| `gob` encoding of `*starlark.Function` | Source-location + closure capture, re-resolve on replay | Phase 3 decision | Phase 1 only stores `*starlark.Function` in-memory (DAG is local to worker). Phase 3 picks the serialization mechanism. |

**Deprecated/outdated:**
- `starlark.ExecFile` (use `ExecFileOptions`).
- Package-level `resolve.AllowLambda`/`AllowSet`/`AllowFloat` flags as truth (they're obsolete; use `syntax.FileOptions`).
- Tagged `v0.x.y` for `go.starlark.net` (no tags exist — pseudo-versions only).

## Open Questions

1. **Should `pkg/dag.Node` be an interface, a sum type via `Kind` field, or generics-based?**
   - What we know: CONTEXT.md leaves this to discretion. Three options have tradeoffs.
   - What's unclear: Phase 3's interpreter code shape. We're optimizing for *its* clarity, but Phase 3 hasn't been written.
   - Recommendation: **Interface with distinct types** (`*Step`, `*IfCond`, etc., all implementing `Node` with `Kind() string` and `Position() syntax.Position`). Type switches in Phase 3 are clean; `Kind()` lets serialization tag-then-lookup. If Phase 3 finds the type switch unwieldy, the planner there can flatten to a sum type — but distinct types is the safer default for Phase 1 (less likely to need ratcheting later).

2. **`Idempotent` declaration: struct field or method on the OperationSpec?**
   - What we know: D-12 mandates "required, no default." Either approach prevents silent defaults.
   - What's unclear: Which is more ergonomic for extension authors?
   - Recommendation: **Struct field on a registered op spec.** Method-on-operation creates an interface-bloat problem (every operation implementation is one line away from forgetting it); a struct field with no zero-value default forces the author to think about it at registration site. Concretely:
     ```go
     ext.Operations["create_issue"] = OperationSpec{
         Idempotent: false, // <- can't omit; would set to zero-value but
                            //    we use a *bool so nil registration fails loudly
         Func:       handleCreateIssue,
         KwargsType: reflect.TypeOf(CreateIssueArgs{}),
     }
     ```
     With `Idempotent *bool`, registration validates `spec.Idempotent != nil` and fails otherwise. Authors must write `Idempotent: ptr(true)` or `Idempotent: ptr(false)` — never omit.

3. **Parser pass sequencing.**
   - What we know: We need (a) parse `.star` to AST, (b) execute it (collect flows + lambdas), (c) resolve `call_flow` cross-references, (d) lint lambdas, (e) validate kwargs.
   - What's unclear: Whether (c)–(e) all run after every loaded file is parsed, or interleaved.
   - Recommendation: **Sequential passes after all files loaded.** Order:
     1. Parse + execute every reachable file (driven by `load()` — `Parser.ParseFile` triggers transitive loads).
     2. Collect all `*dag.Flow`s into the parser-session flow map (D-15: error on duplicate names).
     3. Resolve `call_flow` references against the flow map (D-16: error on not-found).
     4. Walk every `CapturedLambda`, validate free vars (D-19), assert frozen.
     5. For every operation invocation embedded in `ActionRef`s, validate kwargs against `OperationSpec.KwargsType` (D-11).
   - This is single-pass per phase, easy to test, easy to reason about. The state held between passes is just `[]*dag.Flow` and `[]*dag.CapturedLambda` — no mutable parser state beyond that.

4. **Concrete `os.Root` adoption for `load()` sandbox.**
   - What we know: Go 1.24's `os.Root` is the modern, traversal-resistant primitive.
   - What's unclear: Whether `filepath.Rel` + `..` check has a known CVE shape we're missing.
   - Recommendation: **Ship Phase 1 with `filepath.Rel` check.** Add a phase-1 test that explicitly tries `load("../../etc/passwd")` and asserts rejection. Re-evaluate switching to `os.Root` in Phase 4 (when CLI exposes the path) if a security audit raises concerns. Document the choice in `pkg/parser/load.go`.

## Environment Availability

> Phase 1 is purely in-process Go code with no external dependencies beyond the Go toolchain itself.

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain (≥1.25) | Compiling library | ✗ | 1.21.0 (current local) | **BLOCKING** — must install Go 1.25+ before Phase 1 starts |
| `git` | `.git` ancestor walk for D-14 root discovery | ✓ (assumed; standard dev env) | — | If absent at runtime: caller must pass `WithRoot(...)` explicitly |
| `go.starlark.net` module | Compiling library | will be fetched on `go mod tidy` | `v0.0.0-20260326113308-fadfc96def35` (verified) | None |
| `github.com/stretchr/testify` | Tests | will be fetched on `go mod tidy` | `v1.11.1` (verified) | stdlib `testing` (acceptable but verbose) |

**Missing dependencies with no fallback:**
- **Go ≥ 1.25 toolchain.** Local machine has Go 1.21.0; `go.starlark.net` requires 1.25 (verified in its `go.mod`). Phase 1 cannot compile on the current local toolchain. **Action for planner:** Wave 0 must include a "verify/install Go 1.25+" step; this is a precondition.

**Missing dependencies with fallback:**
- None.

## Validation Architecture

> nyquist_validation is enabled (config.json: `workflow.nyquist_validation: true`). This section is the basis for VALIDATION.md.

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `github.com/stretchr/testify` v1.11.1 |
| Config file | `go.mod` (test deps), no separate config |
| Quick run command | `go test ./pkg/...` (~5s for unit tests in any single package) |
| Full suite command | `go test ./... -race` (race detector required for bridge tests; activity/test code only — workflow code is single-threaded) |
| Phase gate | `go test ./... -race -count=1` green + `go vet ./...` clean + `golangci-lint run` clean (golangci-lint config lands later in Phase 4) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| **DSL-01** | `flow(name=..., inputs=..., steps=[...])` produces `*dag.Flow` | unit + golden | `go test ./pkg/parser -run TestParseFlow_DSL01` | ❌ Wave 0 — `pkg/parser/parser_test.go` |
| **DSL-02** | `step(action=...)` produces `*dag.Step` with one `ActionRef` | unit + fixture | `go test ./pkg/parser -run TestStep_SingleAction` | ❌ Wave 0 |
| **DSL-03** | `step(block=[a, b, c])` produces `*dag.Step` with multiple `ActionRef`s | unit + fixture | `go test ./pkg/parser -run TestStep_Block` | ❌ Wave 0 |
| **DSL-04** | `if_cond(cond=lambda, then=[...], else_=[...])` produces `*dag.IfCond` with `CapturedLambda` | unit + fixture | `go test ./pkg/parser -run TestIfCond_LambdaCapture` | ❌ Wave 0 |
| **DSL-05** | `script(id=..., fn=lambda, output_alias=...)` produces `*dag.Script` with `CapturedLambda` | unit + fixture | `go test ./pkg/parser -run TestScript_LambdaCapture` | ❌ Wave 0 |
| **DSL-06** | `for_each_parallel(items=..., item=..., steps=[...])` accepts both list and lambda producer | unit + fixture | `go test ./pkg/parser -run TestForEachParallel_BothItemForms` | ❌ Wave 0 |
| **DSL-07** | `call_flow(name=..., inputs=..., child_options=...)` resolves by name at parse time | unit + fixture | `go test ./pkg/parser -run TestCallFlow_NameResolution` | ❌ Wave 0 |
| **DSL-08** | `step(retry=..., timeout=...)` carries Temporal kwargs as pure data | unit | `go test ./pkg/dag -run TestRetryPolicy_Unpack` | ❌ Wave 0 — `pkg/dag/retry_test.go` |
| **DSL-09** | `bridge.ToStarlarkStruct` produces deterministic key-order struct | unit (iter-determinism) | `go test ./pkg/bridge -run TestToStarlarkStruct_Deterministic` | ❌ Wave 0 — `pkg/bridge/struct_test.go` |
| **DSL-10** | `resolve.AllowLambda == true` after parser package init | unit | `go test ./pkg/parser -run TestResolveAllowLambdaIsSet` | ❌ Wave 0 |
| **EXT-01** | `Extension` interface compiles with `Name`, `Initialize`, `Operations` | compile + unit | `go build ./pkg/extension && go test ./pkg/extension -run TestExtensionInterface` | ❌ Wave 0 — `pkg/extension/extension_test.go` |
| **EXT-02** | Extension factory in Starlark returns `*ActionRef`, never executes I/O | unit (integration via fake extension) | `go test ./pkg/parser -run TestExtensionFactory_ReturnsActionRef` | ❌ Wave 0 |
| **EXT-03** | `OperationFunc` signature uses `context.Context`; lint enforces no `workflow.Context` import | compile + lint | `go vet ./pkg/extension` and (later) `golangci-lint run` | ❌ Wave 0 — interface-only; lint comes Phase 4 |
| **EXT-04** | Registration fails if any operation lacks `Idempotent` | unit | `go test ./pkg/extension -run TestRegistration_RequiresIdempotent` | ❌ Wave 0 |
| **EXT-05** | `Credential` interface is sealed; `String()` is redacted | unit | `go test ./pkg/extension -run TestCredential_RedactedString` | ❌ Wave 0 |
| **EXT-06** | Extensions register statically and dynamically via `parser.Register(...)` | unit | `go test ./pkg/parser -run TestRegistration_StaticAndDynamic` | ❌ Wave 0 |
| **PARSE-01** | All six DSL primitives are naked globals (not namespaced) in `parseTimeGlobals` | unit | `go test ./pkg/parser -run TestParseTimeGlobals_NakedPrimitives` | ❌ Wave 0 |
| **PARSE-02** | `load()` resolves relative + absolute, sandboxed to root, rejects traversal | fixture-based | `go test ./pkg/parser -run TestLoad_SandboxedResolution` (3 fixture files) | ❌ Wave 0 — fixtures + `pkg/parser/load_test.go` |
| **PARSE-03** | Two-environment split: parse-time and lambda-time globals are distinct dicts | unit | `go test ./pkg/parser -run TestParseAndLambdaGlobalsAreDistinct` and `go test ./pkg/bridge -run TestLambdaTimeGlobalsLocked` | ❌ Wave 0 |
| **PARSE-04** | Lambda capture stores `*starlark.Function` with stable ID + `syntax.Position` | unit + property | `go test ./pkg/parser -run TestLambdaCapture_StableID` (re-parse same content → same ID) | ❌ Wave 0 |
| **PARSE-05** | Malformed file produces `*ParseError` with `<file>:<line>:<col>: <msg>`; never panics | fixture-based (corpus of invalid/) | `go test ./pkg/parser -run TestInvalidFixtures` | ❌ Wave 0 — 8 invalid fixtures + parser_test.go |
| **PARSE-06** | `bridge.CallLambda` uses fresh thread, sets `MaxExecutionSteps`, `Print` hook | unit | `go test ./pkg/bridge -run TestCallLambda_FreshThread` and `TestCallLambda_PrintHookRouted` | ❌ Wave 0 — `pkg/bridge/lambda_call_test.go` |

### Test Type Distribution

| Type | Count | Coverage |
|------|-------|----------|
| Unit (single function/method) | ~25 | DSL-08, DSL-10, EXT-01, EXT-03–06, PARSE-01, PARSE-03, PARSE-04, PARSE-06 |
| Fixture-based (parses `.star` file under `tests/fixtures/`) | ~14 | DSL-01–07, EXT-02, PARSE-02, PARSE-05 |
| Property-based (re-parse same content, expect same ID) | 1 | DSL-09 (iter-determinism), PARSE-04 (stable lambda ID) |
| Goldenfile (parse → JSON, compare against stored golden) | 2 | DSL-01–07 acceptance via `tests/fixtures/golden/*.json` |
| Compile-only (interface implementation assertion) | 3 | EXT-01, EXT-03 (Go interface contract checks) |

### Sampling Rate

- **Per task commit:** `go test ./pkg/{package-touched}/...` — < 5s. Run on every git commit during the phase.
- **Per wave merge:** `go test ./pkg/... -race` — < 30s. Run before merging a wave back.
- **Phase gate:** `go test ./... -race -count=1` and `go vet ./...` — < 60s. Must be green before invoking `/gsd:verify-work`.

### Wave 0 Gaps

Wave 0 establishes the test scaffolding before any production code. The list below is exhaustive — the planner should create one task per file or one task per logical group.

- [ ] `tests/fixtures/valid/01-minimal-flow.star` + `01-minimal-flow.golden.json`
- [ ] `tests/fixtures/valid/02-all-primitives.star` + `02-all-primitives.golden.json` — exercises every DSL primitive in one flow
- [ ] `tests/fixtures/valid/03-multi-flow-per-file.star` — D-15 multi-flow case
- [ ] `tests/fixtures/valid/04-load-relative.star` + `04-load-target.star`
- [ ] `tests/fixtures/valid/05-load-absolute.star`
- [ ] `tests/fixtures/valid/06-call-flow-cross-file.star` + helper file
- [ ] `tests/fixtures/invalid/01-missing-required-kwarg.star` (header: `# expects: missing required 'name'`)
- [ ] `tests/fixtures/invalid/02-mutable-capture.star` (header: `# expects: lambda captures non-module-level variable`)
- [ ] `tests/fixtures/invalid/03-load-traversal.star` (header: `# expects: path escapes parser root`)
- [ ] `tests/fixtures/invalid/04-duplicate-flow-name.star` (header: `# expects: duplicate flow name`)
- [ ] `tests/fixtures/invalid/05-call-flow-not-found.star` (header: `# expects: call_flow target not found`)
- [ ] `tests/fixtures/invalid/06-unknown-extension.star` (header: `# expects: unknown identifier`)
- [ ] `tests/fixtures/invalid/07-forbidden-lambda-builtin.star` (header: `# expects: not allowed in lambda`) — actually a runtime check, but we test parse-time rejection of `time.now()` as a free var
- [ ] `tests/fixtures/invalid/08-bad-syntax.star` (header: `# expects: syntax error`)
- [ ] `pkg/dag/node_test.go` — Node interface compile-time assertions, freeze cascade tests
- [ ] `pkg/dag/action_test.go` — ActionRef freeze cascade
- [ ] `pkg/dag/retry_test.go` — RetryPolicy.Unpack tests (DSL-08)
- [ ] `pkg/dag/lambda_test.go` — CapturedLambda construction
- [ ] `pkg/extension/extension_test.go` — interface assertion + fake extension
- [ ] `pkg/extension/credential_test.go` — sealed interface, redacted String()
- [ ] `pkg/extension/registry_test.go` — registration succeeds/fails with/without Idempotent
- [ ] `pkg/extension/schema_test.go` — `ParseSchema` reflection + `UnpackOperationKwargs` validator (covers D-11 paths)
- [ ] `pkg/parser/parser_test.go` — `TestValidFixtures`, `TestInvalidFixtures`, golden update flag
- [ ] `pkg/parser/builtins_test.go` — six DSL primitives unit tests
- [ ] `pkg/parser/load_test.go` — sandbox enforcement, `.git`-walk root discovery
- [ ] `pkg/parser/lambda_capture_test.go` — stable ID, free-var validation
- [ ] `pkg/parser/resolve_setup_test.go` — `TestResolveAllowLambdaIsSet`
- [ ] `pkg/bridge/struct_test.go` — `TestToStarlarkStruct_Deterministic` (iter-determinism, DSL-09 success criterion)
- [ ] `pkg/bridge/lambda_globals_test.go` — `TestLambdaTimeGlobalsLocked` (D-20 enforcement)
- [ ] `pkg/bridge/lambda_call_test.go` — fresh thread per call, `MaxExecutionSteps` set, print routed
- [ ] **Framework install:** Wave 0 task to `go install` Go 1.25+ on the dev machine (current local is 1.21.0 — see Environment Availability)
- [ ] **Framework setup:** `go mod tidy` after first commit to pin `go.starlark.net` and `testify`

### Manual-Only Gaps

None for Phase 1 — every requirement is exercise-able via `go test`. Determinism replay tests (Pitfall #3) and end-to-end `Temporal dev-server` integration are Phase 3 concerns; Phase 1's verifications are entirely in-process Go.

## Sources

### Primary (HIGH confidence)
- [`go.starlark.net/starlark` package docs](https://pkg.go.dev/go.starlark.net/starlark) — Builtin, NewBuiltin, UnpackArgs, Function (NumFreeVars, FreeVar, Position), Thread (SetLocal, Print, SetMaxExecutionSteps, Cancel), StringDict, Tuple, Value interface (Freeze contract)
- [`go.starlark.net/syntax` package docs](https://pkg.go.dev/go.starlark.net/syntax) — `syntax.Position` (Filename, Line, Col), `syntax.FileOptions` (Set, While, TopLevelControl, GlobalReassign, LoadBindsGlobally, Recursion)
- [`go.starlark.net/resolve` package docs](https://pkg.go.dev/go.starlark.net/resolve) — `AllowLambda` is obsolete (always true). Use `syntax.FileOptions` for forward compatibility.
- [`go.starlark.net/starlarkstruct` package docs](https://pkg.go.dev/go.starlark.net/starlarkstruct) — `FromStringDict(constructor, dict)`, `Default` constructor, `Attr`, `AttrNames`, `Constructor`, `Freeze`
- [Starlark in Go: Implementation](https://chromium.googlesource.com/external/github.com/google/starlark-go/+/HEAD/doc/impl.md) — Freeze cascade contract: "Every value defines a Freeze method that sets its own frozen flag if not already set, and calls Freeze for each value that it contains. Application-defined types must also follow this discipline."
- [Starlark in Go: example_test.go](https://github.com/google/starlark-go/blob/master/starlark/example_test.go) — canonical `Thread.Load` callback pattern, `UnpackArgs` builtin pattern
- [Starlark in Go: unpack.go](https://github.com/google/starlark-go/blob/master/starlark/unpack.go) — `Unpacker` interface, `?` and `??` modifier semantics, supported types
- [Go module proxy: go.starlark.net@latest](https://proxy.golang.org/go.starlark.net/@latest) — verified pseudo-version `v0.0.0-20260326113308-fadfc96def35` (2026-03-26)
- [LUCI Starlark interpreter package docs](https://pkg.go.dev/go.chromium.org/luci/starlark/interpreter) — production-grade sandboxed `load()` with `MakeModuleKey` validation rejecting `../` traversal

### Secondary (MEDIUM confidence)
- [Skytime project research/SUMMARY.md](.planning/research/SUMMARY.md) — phase-1 build order, two-environment split rationale
- [Skytime project research/STACK.md](.planning/research/STACK.md) — verified versions of all dependencies
- [Skytime project research/ARCHITECTURE.md](.planning/research/ARCHITECTURE.md) — package boundaries, cross-firewall imports
- [Skytime project research/PITFALLS.md](.planning/research/PITFALLS.md) — Pitfalls #1, #2, #3, #6, #7, #10 (all directly relevant to Phase 1)
- [`cadence-workflow/starlark-worker` GitHub repo](https://github.com/cadence-workflow/starlark-worker) — closest prior art for Starlark-as-DSL on Cadence/Temporal; package layout reference
- [Traversal-resistant file APIs (Go blog)](https://go.dev/blog/osroot) — `os.Root` API in Go 1.24+ (potential Phase 1 upgrade path for sandbox)

### Tertiary (LOW confidence — directional only)
- [`vladimirvivien/startype`](https://github.com/vladimirvivien/startype) — third-party reflection helper for tag-based Starlark↔Go conversion. Reviewed and rejected for Phase 1 (small dep with uncertain maintenance; hand-rolled covers our exact needs in ~150 LOC). Sources tagged tertiary because the library's update cadence and adoption are not verified.
- [`stripe/skycfg`](https://github.com/stripe/skycfg) — Skylark + protobuf bindings; tangentially relevant for builtin registration patterns. Skytime's surface is simpler; not a direct reuse target.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all versions verified against `proxy.golang.org` 2026-04-26; Starlark API surface verified against pkg.go.dev with explicit signatures.
- Architecture: HIGH — package layout locked by CONTEXT.md; pass sequencing is straightforward and follows established patterns.
- Pitfalls: HIGH — directly extracted from project's PITFALLS.md plus first-party Starlark spec for freeze cascade and free-var inspection.
- Reflection-based kwarg validator: MEDIUM — no widely-adopted helper exists for the exact `star:"..."` tag shape; recommendation is "hand-roll ~150 LOC" with high confidence it's the right call but with the inherent risk of any from-scratch implementation.
- Lambda ID stability and Phase-3 compatibility: HIGH — D-18 format (`sha256(fileBytes)[:8] + ":" + line + ":" + col`) composes cleanly with both Phase 3 serialization options. The "cosmetic edits invalidate IDs" caveat is intentional, not a bug.
- Test corpus organization: HIGH — fixture-based + golden + invalid-with-expects pattern is textbook Go.

**Research date:** 2026-04-26
**Valid until:** 2026-07-26 (90 days; the Starlark API and Go stdlib are stable enough that Phase 1 has no fast-moving dependencies)
