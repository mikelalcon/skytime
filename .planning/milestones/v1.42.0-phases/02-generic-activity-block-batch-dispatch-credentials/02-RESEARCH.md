# Phase 2: Generic Activity + Block-Batch Dispatch + Credentials — Research

**Researched:** 2026-04-28
**Domain:** Temporal Go SDK activity authoring; Go type design for secrets, sealed sums, and per-worker caches; block-batched I/O dispatch under partial-failure semantics
**Confidence:** HIGH on Temporal SDK API surface (verified against v1.42.0 source); HIGH on Go idioms for `Secret` wrapper and sealed interfaces (verified against existing Phase 1 code + community patterns); MEDIUM on the test-bridge interaction with `Attempt` (the public `TestActivityEnvironment` does not expose Attempt simulation — finding documented below).

## Summary

Phase 2 builds `pkg/activity` — a single Temporal activity (`ExecuteBatch`) that takes a `[]*dag.ActionRef` plus a per-worker `OperationDispatch` map, dispatches each action through the registered `OperationFunc`, resolves credentials JIT via a `CredentialHandler` with a per-worker TTL cache (bypassed on retry attempt), heartbeats between every action with a `BatchProgress` payload, and returns a structured `[]dag.ActionResult` whose semantics differ for retryable vs. non-retryable failures (D2-13/D2-14). The implementation is testable standalone with `testsuite.NewTestActivityEnvironment` for happy-path and heartbeat assertions, plus pure-Go unit tests for the cache, the secret wrapper, and the result-construction logic.

The phase also touches three packages outside `pkg/activity`: it adds `ActionResult` and `OperationOutput` sealed sums to `pkg/dag`, refactors `pkg/extension/credential.go` to use a new `Secret` wrapper type, narrows `OperationFunc`'s return type to `OperationOutput`, and backports two new lint passes (mixed-idempotency rejection + block-size cap) into `pkg/parser/linter.go`. Every locked decision in CONTEXT.md is supported by a verified Go API surface; the only nuance worth flagging is that `TestActivityEnvironment.ExecuteActivity` hardcodes `Attempt: 1` and provides no public method to simulate retry attempts, so D2-11 (cache-bypass-on-retry) requires the activity to delegate Attempt extraction to a small abstraction the unit tests can substitute.

**Primary recommendation:** Implement `ExecuteBatch` as a thin orchestrator over four collaborator types — `OperationDispatch` (lookup + idempotency check), `credentialCache` (per-worker, RWMutex + map, lazy TTL eviction, cache-bypass via attempt extraction abstraction), `actionExecutor` (single-action retry/non-retryable classification + `context.WithTimeout` enforcement), and `heartbeatEmitter` (RecordHeartbeat with `BatchProgress`). Each collaborator has a focused interface so unit tests can swap one without spinning up `TestActivityEnvironment`. Use `testsuite.NewTestActivityEnvironment` only for the happy-path integration test that verifies heartbeat-listener behavior and a real `activity.Context()` flowing through the whole pipeline.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**ActionResult — Shape & Location**

- **D2-01: ActionResult is a sealed sum interface in `pkg/dag`.** Mirrors the Phase 1 `Credential` sealed-interface pattern. Concrete types:
  ```go
  type ActionResult interface {
      ActionIndex() int
      isActionResult() // sealed
  }
  type OkResult struct {
      Idx    int
      Output OperationOutput // sealed marker — see D2-04
  }
  type RetryableErrResult struct {
      Idx int
      Err error
  }
  type NonRetryableErrResult struct {
      Idx int
      Err error
  }
  type SkippedResult struct {
      Idx    int
      Reason string
  }
  ```
  Lives in `pkg/dag` so the Phase 3 interpreter can consume by import without an extra package dependency. The `pkg/activity` package (Phase 2) constructs these; `pkg/parser` need not know.

- **D2-02: SkippedResult is defined but unused in v1.** With Policy D (D2-05) preventing mixed batches at parse time, no v1 code path emits a `SkippedResult`. The variant is reserved for future use (precondition-failed, conditional skip). Tests assert `SkippedResult` is constructible but the integration tests don't exercise it.

- **D2-03: Output type is `OperationOutput` — a sealed marker interface.** Operations must return a type that implements `OperationOutput`:
  ```go
  type OperationOutput interface {
      isOperationOutput()
  }
  ```
  Extension authors write typed structs:
  ```go
  type CreateIssueOutput struct {
      Number int
      URL    string
  }
  func (CreateIssueOutput) isOperationOutput() {}
  ```
  Compile-time check: ops cannot return arbitrary `map[string]interface{}` without explicitly implementing the marker.

- **D2-04: OperationFunc signature update.** The Phase 1 signature was:
  ```go
  type OperationFunc func(ctx context.Context, args any, cred Credential) (output any, err error)
  ```
  Phase 2 narrows `output any` → `output OperationOutput`. This is a Phase-1-to-Phase-2 backward-incompatible signature change, but Phase 1 ships no real extensions yet, so the migration is internal.

**Mixed-Idempotency Block Policy (Policy D)**

- **D2-05: A `step(block=[...])` may NOT mix idempotent and non-idempotent operations.** The parser linter emits a position-aware error at parse time. Defense in depth: the activity also rejects mixed batches with `NonRetryableErrResult` for every action.

- **D2-06: Splitting logic lives in Phase 3 interpreter, NOT in Phase 2 activity.** The activity always receives a homogeneous batch (all-idempotent OR a single non-idempotent action).

- **D2-07: Block-size cap = 50 actions.** The parser linter rejects blocks with > 50 actions. Configurable via `parser.WithMaxBlockSize(N)`. The activity also defensively rejects > 50 with `NonRetryableErr`.

**Credential Resolution & Caching**

- **D2-08: Credentials use a `Secret` wrapper type for raw bytes.** Phase 1's `BearerCredential`, `BasicCredential`, `APIKeyCredential` get refactored:
  ```go
  type Secret struct{ value string }
  func (s Secret) String() string                  { return "<redacted>" }
  func (s Secret) GoString() string                { return "<redacted>" }
  func (s Secret) MarshalJSON() ([]byte, error)    { return []byte(`"<redacted>"`), nil }
  func (s Secret) Reveal() string                  { return s.value }
  func NewSecret(raw string) Secret                { return Secret{value: raw} }
  ```
  Operations call `cred.(*BearerCredential).Token.Reveal()` at the HTTP-call site.

- **D2-09: NO regex error scrubber in v1.** Defense relies on (i) Phase 1's redacted `String()` on Credential kinds, (ii) the new `Secret` wrapper's redaction methods, and (iii) the documented op contract.

- **D2-10: Activity-level credential cache is per-worker with TTL.** `pkg/activity` maintains a `map[string]cachedEntry` keyed by credential ID. Default TTL: 5 minutes (configurable via `activity.WithCredentialCacheTTL(...)`).

- **D2-11: Cache bypass on retry attempt.** When `activity.GetInfo(ctx).Attempt > 1`, the activity invalidates cached entries for every credential ID in the current batch and forces a fresh `Resolve` call.

- **D2-12: CredentialHandler error classification.**
  - `errors.Is(err, extension.ErrUnknownCredential)` → `NonRetryableErrResult` (configuration bug).
  - Any other error → `RetryableErrResult` (transient backend failure).

**Batch Failure Semantics**

- **D2-13: On retryable failure within a batch, the activity returns an error to Temporal.** Activity short-circuits and returns the error; Temporal retries the whole `ExecuteBatch` invocation. Safe by Policy D (idempotent batches only).

- **D2-14: On non-retryable failure within a batch, the activity returns ALL results so far.** Complete `[]ActionResult` — successes before failure, the `NonRetryableErrResult` at the failing index, `SkippedResult` placeholders for actions after.

- **D2-15: Activity StartToCloseTimeout = sum(per-action timeouts) + 30s headroom.** Computed by interpreter (Phase 3); activity enforces per-action timeouts via `context.WithTimeout`.

- **D2-16: Heartbeat between every action.** `activity.RecordHeartbeat(ctx, BatchProgress{Action: i, Total: n})` after each action completes.

**Operation Dispatch**

- **D2-17: Operation dispatch table is built at parser finalize-time and passed into the activity environment.** `type OperationDispatch map[string]extension.OperationSpec` — keys are `"extension.op"`, e.g., `"github.create_issue"`. Activity does NOT import `pkg/parser`.

**Package Naming**

- **D2-18: Package name is `pkg/activity`.**

### Claude's Discretion

- Exact API of `Secret` constructor and accessor (e.g., method names like `.Reveal()` vs `.Get()` vs `.Unwrap()`).
- Internal data structure for the credential cache (`sync.Map` vs `sync.RWMutex` + plain map).
- Test fixture for "fake credential handler" — file-based, env-based, or in-memory.
- Specific metrics/observability hooks (none required for v1, but adding `slog` debug-level logs is fine).
- Whether `SkippedResult` is exposed in the public package or kept internal until first use (export it to make the type complete).

### Deferred Ideas (OUT OF SCOPE)

- **Regex error scrubber** — re-evaluate after first real customer incident.
- **Per-extension `Scrubbers []*regexp.Regexp` on `OperationSpec`.**
- **Time-based heartbeating** (heartbeat every N seconds during long single ops).
- **`SkippedResult` emission paths.**
- **OperationOutput schema export** (JSON Schema, markdown docs).
- **Cross-worker credential cache** (Redis, etc.).
- **`Secret`-aware linter** that flags `.Reveal()` call sites.
- **Mixed-batch policy override.**

</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| ACT-01 | A single Temporal activity `ExecuteBatch(ctx, []ActionRef) ([]ActionResult, error)` dispatches all extension I/O — extensions never register their own activities | `OperationDispatch` (D2-17) plus `OperationFunc` lookup; verified `worker.RegisterActivityWithOptions(ExecuteBatch, options)` is the standard registration path |
| ACT-02 | The activity returns a structured per-action result list (`ok-with-output \| retryable_err \| non_retryable_err \| skipped`); the interpreter consumes per-action results, not a single batch outcome | `ActionResult` sealed sum (D2-01); construction patterns documented below; `D2-14` non-retryable path returns full `[]ActionResult` |
| ACT-03 | Non-idempotent operations are never batched — they execute as one action per activity invocation, even when the user wrote them in a `block` | Parser linter rejects mixed batches (D2-05); activity defensively re-validates via `OperationDispatch[ActionRef.Kind_].Idempotent` lookup before executing |
| ACT-04 | A `CredentialResolver` interface is injected into the activity environment; secrets are resolved just-in-time inside the activity, never stored in workflow state | Phase 1's `extension.CredentialHandler` already exists; Phase 2 adds per-worker cache (D2-10) wrapping it; cache bypass on retry (D2-11) verified via `activity.GetInfo(ctx).Attempt` |
| ACT-05 (amended) | Credentials use a `Secret` wrapper type whose `String()`, `GoString()`, and `MarshalJSON()` always return `"<redacted>"`. Operations extract via `.Reveal()`. Integration test asserts secret never appears in any `ActionResult`, error wrapper, or heartbeat payload | `Secret` design (D2-08); verified that `fmt.Formatter` interface is required to fully cover `%+v` and `%#v`; community patterns documented below |
| ACT-06 | The activity heartbeats between actions and uses a per-batch `StartToCloseTimeout` equal to the sum of per-action timeouts plus headroom | `activity.RecordHeartbeat(ctx, BatchProgress{...})` (D2-16); StartToCloseTimeout is computed by Phase 3 interpreter, activity just heartbeats; `SetOnActivityHeartbeatListener` lets tests assert |

</phase_requirements>

## Project Constraints (from CLAUDE.md)

The user's global `CLAUDE.md` only specifies stockflow startup commands (irrelevant here). No project-local CLAUDE.md exists at `/Users/mikel/dev/ai/temporero/CLAUDE.md`. Constraints come exclusively from `PROJECT.md`:

- **No `go.temporal.io/sdk/activity` imports inside extensions.** `pkg/extension` enforces this via `TestNoTemporalImportsInExtensionPackage`. Phase 2 confines the import to `pkg/activity` only — verified.
- **No string compilation, no dynamic activities, no context bleed.** The activity adapts `activity.Context()` (which implements `context.Context`) to a stdlib `context.Context` before calling `OperationFunc`. Verified by Phase 1's `OperationFunc` signature.
- **Credentials never enter workflow state.** Activity input is `[]*dag.ActionRef` containing only `CredentialID` strings; resolved `Credential` values live only in the activity's stack frame.
- **Quality > speed; correct boundaries are hard to fix retroactively.** Sealed-sum interfaces (`ActionResult`, `OperationOutput`) are the v1 wire format between Phase 2 and Phase 3 — pick the shape carefully now.

## Standard Stack

### Core (already pinned in go.mod)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `go.temporal.io/sdk` | **v1.42.0** | Activity API (`activity.RecordHeartbeat`, `activity.GetInfo`, `activity.GetLogger`) and the testsuite | Pinned in `STACK.md`; only legal Temporal SDK path; `activity` package import is restricted to `pkg/activity` by Phase 2 firewall |
| `go.temporal.io/sdk/activity` | (sub-package of above) | Inside `ExecuteBatch` — `RecordHeartbeat`, `GetInfo`, `GetLogger`, sentinel errors | `STACK.md` line 26 explicitly states this is "the only package allowed to import this" |
| `go.temporal.io/sdk/temporal` | (sub-package of above) | `temporal.NewApplicationError`, `temporal.NewNonRetryableApplicationError` for wrapping batch-level errors when D2-13 short-circuits | Standard Temporal error-classification surface |
| `go.temporal.io/sdk/testsuite` | (sub-package of above) | `WorkflowTestSuite` + `NewTestActivityEnvironment` for the happy-path heartbeat-listener integration test | Bundled; required for ACT-06 verification |
| `github.com/stretchr/testify` | v1.11.1 | `require`/`assert` for table-driven activity tests | Already in go.mod |

### Supporting (stdlib only — no new deps for Phase 2)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `sync` | stdlib | `sync.RWMutex` for credentialCache | RWMutex+map preferred over `sync.Map` — see "Per-worker cache" below |
| `time` | stdlib | TTL expiry on cache entries; per-action timeouts | `time.Now()` is permitted INSIDE activities (forbidden inside workflows) |
| `context` | stdlib | `context.WithTimeout` for per-action enforcement; `context.Cause` for cancellation reason | D2-15 |
| `errors` | stdlib | `errors.Is(err, extension.ErrUnknownCredential)` for D2-12 classification | Phase 1 already established this pattern |
| `log/slog` | stdlib | Optional debug-level logs inside the activity | Phase 1 established `slog.Default()` interface |

### Alternatives Considered

| Instead of | Could Use | Why Not |
|------------|-----------|---------|
| `sync.RWMutex` + `map[string]cachedEntry` | `sync.Map` | `sync.Map` is optimized for "many goroutines reading disjoint keys" or "single-writer, many-readers." Our cache is "many keys, occasional writes, frequent reads of the same hot keys" (e.g., a single batch resolves the same credential 50 times). RWMutex+map gives us tighter type safety (no `interface{}` boxing) and is simpler to reason about with `-race`. Source: [Go blog "When to use sync.Map"](https://pkg.go.dev/sync#Map) — explicitly says "Most code should use a plain Go map instead, with separate locking or coordination, for better type safety and to make it easier to maintain other invariants." |
| Per-extension cache (cache key = `extension+credID`) | Bare credential ID as cache key | A single credential ID may resolve to different `Credential` types via different handlers in theory, but Phase 1's CredentialHandler is registered ONCE per worker (D-10) and has a single `Resolve(ctx, id) (Credential, error)` signature — there's no path where the same ID maps to two different credentials in one process. Bare ID is sufficient. |
| Background goroutine for TTL expiry | Lazy expiry on read | Background goroutine introduces shutdown coordination (signal it to stop when worker stops) and a constant-cost ticker even when the cache is idle. Lazy on read costs one `time.Since(entry.created) > ttl` check per get; cost is negligible vs. the saved complexity. Stale entries stay in memory until next read (acceptable — the worker process is the cache lifetime). |
| Custom `Format(state, verb)` only | Both `String()`/`GoString()`/`MarshalJSON()` AND `Format` | Implementing only `Format` would intercept `%v`, `%+v`, `%#v`, `%s`, `%q` uniformly but breaks `json.Marshal` (which doesn't call `Format`) and breaks the case where someone passes a `Secret` to a function expecting `fmt.Stringer`. Belt-and-suspenders: implement all three plus `Format` — see "Secret wrapper" below. |

**Installation:**
No new packages. The activity import path is already covered by `go.temporal.io/sdk@v1.42.0` already in `go.mod`. **Verify before tasks start:**
```bash
go list -m go.temporal.io/sdk
# expect: go.temporal.io/sdk v1.42.0
```

If absent (pre-Phase-2 commit), add via `go get go.temporal.io/sdk@v1.42.0` and run `go mod tidy`. This is currently the case — `go.mod` shows only `go.starlark.net` and `testify`; Phase 2 will be the first phase that needs the Temporal SDK.

**Version verification (run during Wave 0):**
```bash
go list -m -versions go.temporal.io/sdk | tr ' ' '\n' | tail -5
```
Confirm v1.42.0 is the most recent (or pin to a higher v1.x patch if released between research and execution). v1.43.x is acceptable; do NOT jump majors.

## Architecture Patterns

### Recommended Project Structure

```
pkg/
├── activity/                      # NEW (Phase 2)
│   ├── doc.go                     # Package overview + import-firewall doc
│   ├── execute_batch.go           # ExecuteBatch entry point
│   ├── execute_batch_test.go      # Integration tests via TestActivityEnvironment
│   ├── dispatch.go                # OperationDispatch type + Lookup helper
│   ├── dispatch_test.go
│   ├── credential_cache.go        # Per-worker cache with TTL + retry-aware bypass
│   ├── credential_cache_test.go   # Includes -race tests for parallel access
│   ├── action_executor.go         # Single-action execution: timeout + classify err
│   ├── action_executor_test.go
│   ├── heartbeat.go               # BatchProgress + RecordHeartbeat wrapper
│   ├── heartbeat_test.go
│   ├── attempt.go                 # Tiny abstraction for activity.GetInfo(ctx).Attempt
│   ├── attempt_test.go            # Lets unit tests inject Attempt without TestActivityEnvironment
│   ├── options.go                 # Worker registration options (WithCredentialCacheTTL, ...)
│   └── options_test.go
├── dag/
│   ├── result.go                  # NEW — ActionResult sealed sum + 4 concrete kinds
│   ├── result_test.go             # NEW
│   ├── output.go                  # NEW — OperationOutput marker
│   └── output_test.go             # NEW
├── extension/
│   ├── credential.go              # MODIFIED — Secret wrapper + 3 kinds use it
│   ├── credential_test.go         # MODIFIED — verify redaction on %v/%s/%+v/%#v/JSON
│   ├── operation.go               # MODIFIED — OperationFunc returns OperationOutput
│   ├── operation_test.go          # MODIFIED
│   ├── secret.go                  # NEW — Secret type definition
│   ├── secret_test.go             # NEW
│   ├── unknown_credential.go      # NEW — ErrUnknownCredential sentinel
│   └── testing/                   # NEW sub-package
│       ├── fake_handler.go        # FakeCredentialHandler for tests across packages
│       └── fake_handler_test.go
└── parser/
    ├── linter.go                  # MODIFIED — add lintMixedIdempotency + lintBlockSize
    ├── linter_test.go             # MODIFIED — fixtures for mixed-batch + over-cap
    └── options.go                 # MODIFIED — add WithMaxBlockSize
```

**Rationale:**
- `pkg/activity/` is new and contains *all* code that touches `go.temporal.io/sdk/activity`. Add a `TestNoTemporalImportsInOtherPackages` test in `pkg/activity` that mirrors the existing Phase-1 firewall tests, so any future package accidentally importing `activity` is caught at CI time.
- Each collaborator (`dispatch`, `credential_cache`, `action_executor`, `heartbeat`, `attempt`) has its own file + test pair. This is the only way to keep `execute_batch.go` short enough to read end-to-end.
- `pkg/extension/testing/` is a sub-package, NOT in `_test.go`, because Phase 6's example extensions and Phase 5's E2E harness will re-use `FakeCredentialHandler`. Following the `httptest`/`fstest` stdlib pattern.
- The `attempt.go` abstraction is the smallest possible thing — a single function that defaults to `activity.GetInfo(ctx).Attempt` but can be overridden for unit tests. Critical for ACT-04 cache-bypass testability since `TestActivityEnvironment` doesn't expose Attempt.

### Pattern 1: Sealed Marker Interface for `OperationOutput`

**What:** Compile-time enforcement that operation return types are explicitly opted into the type system. No `map[string]interface{}` allowed.

**When to use:** Every `OperationFunc` return type must implement `OperationOutput`.

**Example:**
```go
// pkg/dag/output.go
package dag

// OperationOutput is the sealed marker every extension operation return type
// must implement. The unexported isOperationOutput method prevents downstream
// packages from claiming to be an OperationOutput by accident — they must
// explicitly add the method.
//
// Why an empty marker rather than something richer? Phase 2 only needs to
// know "is this a legal output?" — Phase 3's interpreter does the type
// switch on concrete types it knows about (extension authors export their
// Output structs). Future v2 schema export iterates over registered output
// types via reflection on the OperationSpec.
type OperationOutput interface {
    isOperationOutput()
}
```

**JSON marshaling caveat:** `json.Marshal` on an interface value writes the concrete type's JSON form, NOT the interface type name. So `OkResult{Output: CreateIssueOutput{Number: 42, URL: "..."}}` marshals as:
```json
{"kind":"OkResult","idx":0,"output":{"Number":42,"URL":"https://..."}}
```
There is NO `"type":"CreateIssueOutput"` discriminator unless the concrete type adds one. **Phase 2 chooses NOT to add a discriminator** — the wire shape between Phase 2 and Phase 3 is in-process Go (interpreter consumes the `[]ActionResult` directly), not network JSON. If Phase 3 later needs JSON serialization for replay or debugging, add a `kind` field via `MarshalJSON` on each concrete output type, but that's a Phase-3 problem.

**Source:** Verified pattern from existing `pkg/extension/credential.go` `Credential` interface (Phase 1, sealed-via-`isCredential()`).

### Pattern 2: `ActionResult` Sealed Sum with Indexed Constructors

**What:** Four concrete result types, one sealed parent interface, factory constructors that take the action index explicitly.

**When to use:** Inside `ExecuteBatch`, after each action completes (or is skipped post-failure).

**Example:**
```go
// pkg/dag/result.go
package dag

// ActionResult is the per-action outcome of one ExecuteBatch invocation.
// Sealed sum: only the four types below satisfy this interface. Phase 3's
// interpreter consumes these via type switch.
type ActionResult interface {
    ActionIndex() int
    isActionResult()
}

type OkResult struct {
    Idx    int
    Output OperationOutput
}
func (r OkResult) ActionIndex() int { return r.Idx }
func (r OkResult) isActionResult()  {}

type RetryableErrResult struct {
    Idx int
    Err error
}
func (r RetryableErrResult) ActionIndex() int { return r.Idx }
func (r RetryableErrResult) isActionResult()  {}

type NonRetryableErrResult struct {
    Idx int
    Err error
}
func (r NonRetryableErrResult) ActionIndex() int { return r.Idx }
func (r NonRetryableErrResult) isActionResult()  {}

type SkippedResult struct {
    Idx    int
    Reason string
}
func (r SkippedResult) ActionIndex() int { return r.Idx }
func (r SkippedResult) isActionResult()  {}
```

**Construction-site convention:** the activity code holds `idx` as a loop variable; constructors take it explicitly so misalignment between slice index and `Idx` field is impossible — the loop sets both at once:
```go
for idx, ref := range batch {
    out, err := executor.run(ctx, idx, ref, dispatch)
    if err != nil {
        // … classification logic …
        results = append(results, dag.NonRetryableErrResult{Idx: idx, Err: err})
        // populate Skipped for remaining
        for j := idx + 1; j < len(batch); j++ {
            results = append(results, dag.SkippedResult{Idx: j, Reason: "previous action failed non-retryably"})
        }
        return results, nil // D2-14: non-retryable returns nil error to Temporal
    }
    results = append(results, dag.OkResult{Idx: idx, Output: out})
}
```

### Pattern 3: `Secret` Wrapper with Full Format Coverage

**What:** Struct wrapper around a string with **four redaction methods + one accessor**:
1. `String() string` — for `fmt.Stringer`, called by `%s`, `%v`
2. `GoString() string` — for `fmt.GoStringer`, called by `%#v`
3. `Format(state fmt.State, verb rune)` — catches `%+v` (which bypasses `String`)
4. `MarshalJSON() ([]byte, error)` — for `encoding/json`
5. `Reveal() string` — explicit, greppable, audit-able accessor

**Why all four:**
- `String()` alone catches `%s`, `%v`, `fmt.Println`, `fmt.Printf("%v")`, `fmt.Sprint`.
- `String()` does NOT catch `%+v` (which prints struct field names) or `%#v` (Go-syntax), which are common in debug logs and assertion failures (`testify` uses `%+v`).
- `GoString()` catches `%#v` but not `%+v`.
- `Format(s, verb)` is the only way to intercept `%+v` for a wrapper struct. Implementing it makes all verbs route through one place.
- `MarshalJSON` covers JSON encoding, which is what Temporal's default `JSONPayloadConverter` uses for activity input/output and heartbeat payloads.

**What we DON'T cover:**
- `encoding/gob` — Phase 2 doesn't use gob; Temporal uses its own DataConverter (default JSON). If a future `pkg/codec` adds gob support, we add `GobEncode()` then.
- `encoding/xml` — same reasoning. Skytime never serializes to XML.
- `encoding.TextMarshaler` (`MarshalText`) — used by `text/template` and some logging packages. **Recommend adding** in Phase 2 too: `func (s Secret) MarshalText() ([]byte, error) { return []byte("<redacted>"), nil }` — one-line addition that closes a real gap (slog's text handler calls MarshalText).

**Naming choice — `.Reveal()` vs `.Get()` vs `.Unwrap()`:**

| Name | Used by | Argument |
|------|---------|----------|
| `.Get()` | `andrewbenton/go-secrets`, common-fate `gconfig`, many ad-hoc impls | Most common; short; ambiguous (Get what?) |
| `.Value` (field, public) | common-fate `SecretStringValue` | "Not totally foolproof" — exposed field defeats the wrapper |
| `.Unwrap()` | similar in name to `errors.Unwrap` and `os.Getenv` patterns | Conflicts with `errors.Unwrap` semantics; reader expects to unwrap a chain |
| `.Reveal()` | hashicorp/vault Go SDK style; less common but explicit | **Recommended.** Explicit verb — "I am about to reveal a secret." Greppable: `git grep '\.Reveal()'` lists every secret-leaves-protection site for audit, exactly per CONTEXT.md "specifics" line. |

**Recommend:** `.Reveal()`. The audit grep argument from CONTEXT.md `<specifics>` is decisive. As a bonus, attach a `// noinspection GoUnnecessaryRevealCall` comment hook in the doc comment so a future linter can find these sites.

**Migration concern (from the prompt):** the prompt asks "confirm that switching to `Secret` is purely additive for ops (no breaking change to test code in `pkg/extension`)." **Answer: it is NOT purely additive — but the breakage is contained.**

The Phase 1 tests in `pkg/extension/credential_test.go` assert things like `BearerCredential{Token: "abc"}.Token` returning `"abc"`. After the change, `Token` is `Secret`, not `string`, so:
- Reading `cred.Token` no longer returns a string — it returns a `Secret`.
- Code building these structs in tests must use `Token: extension.NewSecret("abc")` instead of `Token: "abc"`.
- Code reading `cred.Token` to send it must call `.Reveal()`.

**Mitigation plan:** Phase 2 task includes updating `pkg/extension/credential_test.go` in the same commit that introduces `Secret`. Since the only consumers of `BearerCredential` outside `pkg/extension` are (a) future Phase-6 example extensions and (b) Phase-2 activity tests we're writing fresh, the blast radius is bounded to ~a dozen test assertions inside `pkg/extension/credential_test.go` itself — verified by grep:
```
git grep -n 'BearerCredential\|BasicCredential\|APIKeyCredential' pkg/
# All hits are in pkg/extension/credential.go and pkg/extension/credential_test.go
```

**Example implementation:**
```go
// pkg/extension/secret.go
package extension

import (
    "encoding/json"
    "fmt"
)

const redactedString = "<redacted>"

// Secret wraps a string secret. All standard fmt and encoding interfaces
// redact to "<redacted>" — the only path to the raw value is .Reveal().
//
// AUDIT: every call site of .Reveal() is a "secret leaves type protection"
// boundary. Code review should treat each one as load-bearing. A future
// linter (post-v1) can flag .Reveal() calls outside an approved sink list.
type Secret struct {
    value string
}

// NewSecret wraps a raw string. Constructor is the ONLY path into a Secret;
// no exported field, no SetValue method.
func NewSecret(raw string) Secret { return Secret{value: raw} }

// String returns "<redacted>" — covers %s, %v, fmt.Stringer, fmt.Sprint(s),
// fmt.Println(s).
func (s Secret) String() string { return redactedString }

// GoString returns "<redacted>" — covers %#v.
func (s Secret) GoString() string { return redactedString }

// MarshalJSON returns the JSON string "<redacted>" — covers encoding/json,
// which is Temporal's default DataConverter.
func (s Secret) MarshalJSON() ([]byte, error) {
    return []byte(`"<redacted>"`), nil
}

// MarshalText returns []byte("<redacted>") — covers encoding.TextMarshaler,
// which slog's text handler uses for log values.
func (s Secret) MarshalText() ([]byte, error) {
    return []byte(redactedString), nil
}

// Format implements fmt.Formatter so %+v (which bypasses String() to print
// struct field names) ALSO redacts. Without this, code like
//   slog.Info("auth", "cred", cred)
// could format the surrounding struct via %+v and reveal s.value.
func (s Secret) Format(state fmt.State, verb rune) {
    fmt.Fprint(state, redactedString)
}

// Reveal returns the raw secret. EVERY call site is a leak boundary —
// audit accordingly. Greppable via `git grep '\.Reveal()'`.
func (s Secret) Reveal() string { return s.value }
```

**Test asserting full redaction (one of many):**
```go
func TestSecret_FullRedactionMatrix(t *testing.T) {
    s := extension.NewSecret("super-secret-token-abc123")

    table := []struct{ name, format string }{
        {"sprint", fmt.Sprint(s)},
        {"sprintf_v", fmt.Sprintf("%v", s)},
        {"sprintf_plus_v", fmt.Sprintf("%+v", s)},
        {"sprintf_hash_v", fmt.Sprintf("%#v", s)},
        {"sprintf_s", fmt.Sprintf("%s", s)},
        {"sprintf_q", fmt.Sprintf("%q", s)}, // also routes through Format
    }
    for _, tc := range table {
        require.NotContains(t, tc.format, "super-secret",
            "verb %s leaked secret: %s", tc.name, tc.format)
        require.Contains(t, tc.format, "<redacted>",
            "verb %s should contain <redacted>: %s", tc.name, tc.format)
    }

    // JSON
    b, err := json.Marshal(s)
    require.NoError(t, err)
    require.Equal(t, `"<redacted>"`, string(b))

    // TextMarshaler
    txt, err := s.MarshalText()
    require.NoError(t, err)
    require.Equal(t, "<redacted>", string(txt))

    // Reveal still works
    require.Equal(t, "super-secret-token-abc123", s.Reveal())
}

// And the embedded-in-struct test that's the actual ACT-05 assertion:
func TestBearerCredential_RedactedInAllFormats(t *testing.T) {
    cred := &extension.BearerCredential{
        ID_:   "admin",
        Token: extension.NewSecret("ghp_abc123"),
    }
    for _, format := range []string{"%s", "%v", "%+v", "%#v"} {
        out := fmt.Sprintf(format, cred)
        require.NotContains(t, out, "ghp_abc123", "format %s leaked: %s", format, out)
    }
    b, err := json.Marshal(cred)
    require.NoError(t, err)
    require.NotContains(t, string(b), "ghp_abc123")
}
```

### Pattern 4: Per-Worker Credential Cache (RWMutex + map, lazy TTL)

**What:** Process-local cache that wraps `extension.CredentialHandler`. Default 5-minute TTL. Bypassed when activity is on retry (Attempt > 1).

**Example:**
```go
// pkg/activity/credential_cache.go
package activity

import (
    "context"
    "sync"
    "time"

    "github.com/mikelalcon/skytime/pkg/extension"
)

type cachedEntry struct {
    cred    extension.Credential
    cachedAt time.Time
}

type credentialCache struct {
    handler extension.CredentialHandler
    ttl     time.Duration
    now     func() time.Time // injectable for tests

    mu      sync.RWMutex
    entries map[string]cachedEntry
}

func newCredentialCache(handler extension.CredentialHandler, ttl time.Duration) *credentialCache {
    return &credentialCache{
        handler: handler,
        ttl:     ttl,
        now:     time.Now,
        entries: make(map[string]cachedEntry),
    }
}

// resolve returns the credential for id, hitting the cache when possible.
// On bypass=true (retry attempts), the entry is invalidated and re-resolved.
//
// Concurrency: read path takes RLock; write path takes Lock. A cache miss
// or expired entry triggers Resolve(), which we call OUTSIDE the write
// lock to avoid blocking other readers during a slow handler call —
// double-checked-locking pattern.
func (c *credentialCache) resolve(ctx context.Context, id string, bypass bool) (extension.Credential, error) {
    if !bypass {
        c.mu.RLock()
        if entry, ok := c.entries[id]; ok && c.now().Sub(entry.cachedAt) < c.ttl {
            c.mu.RUnlock()
            return entry.cred, nil
        }
        c.mu.RUnlock()
    }

    // Miss / expired / bypass — resolve outside any lock.
    fresh, err := c.handler.Resolve(ctx, id)
    if err != nil {
        return nil, err // caller classifies via D2-12
    }

    c.mu.Lock()
    c.entries[id] = cachedEntry{cred: fresh, cachedAt: c.now()}
    c.mu.Unlock()
    return fresh, nil
}

// invalidate drops the cached entry for id. Used on retry-attempt
// bypass (D2-11): per the locked decision, on Attempt > 1 we drop
// every credential ID in the current batch before resolving.
func (c *credentialCache) invalidate(id string) {
    c.mu.Lock()
    delete(c.entries, id)
    c.mu.Unlock()
}
```

**`sync.Map` vs `RWMutex+map` analysis:**

| Aspect | `sync.Map` | `RWMutex + map` |
|--------|-----------|-----------------|
| Type safety | `interface{}` boxing — runtime type assertion in every read | `map[string]cachedEntry` — compile-time typed |
| Hot-key reads | Lock-free fast path on stable keys | RLock acquire; uncontended atomic CAS pair |
| TTL eviction | Need to track timestamps somewhere; `Range` for sweeps | Lazy eviction in `resolve` is one extra `time.Sub` call |
| `-race` cleanliness | Built-in; no concurrency ergonomics to get wrong | RWMutex is well-understood and `-race` validates correctness |
| Fits our pattern? | Optimized for "many keys read by disjoint goroutines" or "stable single-writer" | Fits "few hot keys, many readers, infrequent writes" — exactly our case |

**Recommendation: RWMutex + map.** `sync.Map` is documented (in stdlib godoc) as "Most code should use a plain Go map instead, with separate locking or coordination, for better type safety and to make it easier to maintain other invariants." Our cache has a clear "maintain TTL invariant" requirement that benefits from explicit locking.

**Race-test scenario:**
```go
func TestCredentialCache_RaceParallelBatches(t *testing.T) {
    handler := &counter{}
    cache := newCredentialCache(handler, 5*time.Minute)

    var wg sync.WaitGroup
    // Simulate 8 parallel batches each resolving the same 3 credential IDs 50 times.
    for i := 0; i < 8; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for j := 0; j < 50; j++ {
                for _, id := range []string{"admin", "ci", "deploy"} {
                    _, err := cache.resolve(context.Background(), id, false)
                    require.NoError(t, err)
                }
            }
        }()
    }
    wg.Wait()
    // After warmup, expect handler.Resolve called ~3 times (one per ID),
    // not 8*50*3 = 1200 times. Allow up to 8x because thundering-herd
    // on cold start can race multiple goroutines through the resolve path.
    require.LessOrEqual(t, handler.calls, 24)
}
```

Run with `go test -race ./pkg/activity/...` in CI.

### Pattern 5: Attempt Abstraction (testability for D2-11)

**What:** A function `attemptOf(ctx context.Context) int32` that defaults to `activity.GetInfo(ctx).Attempt` but is overridable per-test.

**Why:** `TestActivityEnvironment.ExecuteActivity` (verified in v1.42.0 source at `internal/internal_workflow_testsuite.go:735`) hardcodes `Attempt: 1` and exposes no public method to change it. To unit-test D2-11 (cache bypass when `Attempt > 1`) without spinning up a full TestWorkflowEnvironment + RetryPolicy + intentional failure, we factor Attempt extraction into a tiny seam:

```go
// pkg/activity/attempt.go
package activity

import (
    "context"

    "go.temporal.io/sdk/activity"
)

// attemptFunc returns the current activity attempt number. The default reads
// from activity.GetInfo, but tests can swap a stub via the worker option
// withAttemptFunc (unexported — for testing only).
type attemptFunc func(ctx context.Context) int32

func defaultAttemptFunc(ctx context.Context) int32 {
    return activity.GetInfo(ctx).Attempt
}
```

The activity holds an `attemptFunc` field initialized to `defaultAttemptFunc`; tests construct the activity directly with a fixed value via an internal constructor:
```go
func (t *testHelper) newActivityWithAttempt(attempt int32) *executeBatch {
    return &executeBatch{
        // ... other fields ...
        attemptFn: func(_ context.Context) int32 { return attempt },
    }
}
```

This is the pattern Temporal samples use when they need to test things `activity.Get*` would normally provide.

### Pattern 6: Heartbeat Emission

**What:** Wrap `activity.RecordHeartbeat` in a `heartbeatEmitter` so tests assert calls without going through `SetOnActivityHeartbeatListener`.

**Example:**
```go
// pkg/activity/heartbeat.go
package activity

import (
    "context"

    "go.temporal.io/sdk/activity"
)

// BatchProgress is the heartbeat payload after each action completes.
// Phase 2 keeps it minimal; v1.x can add op name, elapsed time, etc.
type BatchProgress struct {
    Action int `json:"action"`
    Total  int `json:"total"`
}

type heartbeatEmitter interface {
    emit(ctx context.Context, progress BatchProgress)
}

// realHeartbeatEmitter calls activity.RecordHeartbeat directly. Used in
// production (registered with the worker).
type realHeartbeatEmitter struct{}

func (realHeartbeatEmitter) emit(ctx context.Context, progress BatchProgress) {
    activity.RecordHeartbeat(ctx, progress)
}
```

**Heartbeat-context interaction (verified from Temporal docs):**
- Calling `RecordHeartbeat` after the workflow has cancelled returns nothing (no error from the function), but the CONTEXT is cancelled (`ctx.Done()` will be closed; `context.Cause(ctx)` returns the cancellation reason).
- The heartbeat is still transmitted server-side even if the context is already cancelled. The "context canceled" error is logged at WARN by the SDK.
- **For Phase 2:** after `RecordHeartbeat`, check `ctx.Err()` (or watch `ctx.Done()` in a select). If cancelled mid-batch, the activity should stop processing and return — Temporal will treat this as an activity-level cancellation. Phase 2's loop SHOULD do this check explicitly:
```go
for idx, ref := range batch {
    if err := ctx.Err(); err != nil {
        // Workflow cancelled mid-batch — stop here.
        return results, ctx.Err() // Temporal classifies as Canceled
    }
    // ... execute action idx ...
    emitter.emit(ctx, BatchProgress{Action: idx + 1, Total: len(batch)})
}
```

**Heartbeat replay interaction:**
- When Temporal retries an activity, the retry attempt's `activity.HasHeartbeatDetails(ctx)` returns true if the previous attempt called RecordHeartbeat with details. `activity.GetHeartbeatDetails(ctx, &progress)` extracts the LAST heartbeat payload from the previous attempt.
- **Phase 2 does NOT use heartbeat-based progress recovery** in v1. Per D2-13, retryable failure means the whole batch retries from the beginning (Policy D safety). Phase 2 may still call `HasHeartbeatDetails` for *observability* (a slog Info on retry: "resuming after previous attempt got to action %d"), but never to skip already-completed actions.

### Pattern 7: Activity Registration

**Verified API** (from Temporal SDK v1.42.0 source):
```go
// In Phase 3's worker bootstrap:
w := worker.New(client, "skytime-task-queue", worker.Options{})
w.RegisterActivityWithOptions(
    activityImpl.ExecuteBatch, // method value bound to the *executeBatch receiver
    activity.RegisterOptions{
        Name: "ExecuteBatch", // explicit name avoids reflection-based naming surprises
    },
)
```

**Recommendation:** Phase 2 exports a constructor that returns a registrable activity:
```go
// pkg/activity/options.go
package activity

import (
    "time"

    "github.com/mikelalcon/skytime/pkg/extension"
)

type Activity struct {
    dispatch       OperationDispatch
    handler        extension.CredentialHandler
    cacheTTL       time.Duration
    attemptFn      attemptFunc
    emitter        heartbeatEmitter
    maxBlockSize   int // defense-in-depth bound; default 50
}

type Option func(*Activity)

func WithCredentialCacheTTL(d time.Duration) Option { /* ... */ }
func WithMaxBlockSize(n int) Option                 { /* ... */ }

func New(dispatch OperationDispatch, handler extension.CredentialHandler, opts ...Option) *Activity {
    a := &Activity{
        dispatch:     dispatch,
        handler:      handler,
        cacheTTL:     5 * time.Minute,
        attemptFn:    defaultAttemptFunc,
        emitter:      realHeartbeatEmitter{},
        maxBlockSize: 50,
    }
    for _, opt := range opts {
        opt(a)
    }
    return a
}

// ExecuteBatch is the registered Temporal activity entry point. Method
// value: `activity.New(...).ExecuteBatch` is a `func(ctx, []*ActionRef) ([]ActionResult, error)`
// suitable for w.RegisterActivityWithOptions.
func (a *Activity) ExecuteBatch(ctx context.Context, batch []*dag.ActionRef) ([]dag.ActionResult, error) {
    // ... orchestration ...
}
```

**Why a method value, not a free function:** the activity needs to close over `dispatch`, `handler`, `cacheTTL`, etc. Free functions can't, so they'd require global variables (forbidden by Phase 1's "no global state" decision). Method values are Temporal-idiomatic — see [Temporal Go SDK samples](https://github.com/temporalio/samples-go) for the same pattern.

### Pattern 8: Per-Action Timeout Enforcement

**What:** Wrap each `OperationFunc` call in `context.WithTimeout(ctx, opTimeout)`.

**Verified Go semantics:** When you call `context.WithTimeout(parentCtx, d)`, the resulting context's deadline is `min(parentCtx.Deadline(), now()+d)`. The activity's parent context (`activity.Context()`) carries the StartToCloseTimeout deadline; layering a per-action timeout under it works as expected — neither overrides the other; whichever fires first cancels the child.

**Where does the per-action timeout come from?** Need to verify in Phase 1's `OperationSpec`:

Looking at `pkg/extension/operation.go` line 32-49, **`OperationSpec` does NOT have a per-action timeout field today.** The current spec is:
```go
type OperationSpec struct {
    Name        string
    Idempotent  *bool
    Func        OperationFunc
    KwargsType  reflect.Type
}
```

**Phase 2 must add a per-action timeout field.** Two options:
1. Add `DefaultTimeout time.Duration` to `OperationSpec` (default per op, settable at registration).
2. Add `MaxTimeout time.Duration` and let the action ref override.

**Recommendation: Option 1.** Phase 2 adds a single `DefaultTimeout time.Duration` field with zero-value semantics "no per-action timeout enforced — only the activity-level StartToCloseTimeout applies." The interpreter (Phase 3) will sum these up to compute StartToCloseTimeout per D2-15.

**Example:**
```go
// In execute_batch.go:
func (a *Activity) runAction(ctx context.Context, idx int, ref *dag.ActionRef) (dag.OperationOutput, error) {
    spec, ok := a.dispatch[ref.Kind_]
    if !ok {
        return nil, &dag.ValidationError{
            Pos: ref.Pos,
            Msg: fmt.Sprintf("unknown operation %q (not in dispatch table)", ref.Kind_),
        }
    }

    // Resolve credential (cache + retry-bypass).
    bypass := a.attemptFn(ctx) > 1
    cred, err := a.cache.resolve(ctx, ref.CredentialID, bypass)
    if err != nil {
        return nil, classifyResolveError(err) // D2-12
    }

    // Per-action timeout (D2-15).
    callCtx := ctx
    var cancel context.CancelFunc
    if spec.DefaultTimeout > 0 {
        callCtx, cancel = context.WithTimeout(ctx, spec.DefaultTimeout)
        defer cancel()
    }

    // Decode kwargs (Starlark dict -> typed Go struct).
    args := reflect.New(spec.KwargsType).Interface()
    if err := decodeActionRefKwargs(ref, spec.KwargsType, args); err != nil {
        return nil, err // non-retryable: contract bug
    }

    // Call the operation.
    out, err := spec.Func(callCtx, args, cred)
    if err != nil {
        return nil, err // classification done by caller
    }
    return out, nil
}
```

**Op-author contract (must document):**
> Operations receive a `context.Context` whose deadline reflects the lesser of the activity's StartToCloseTimeout and the operation's `DefaultTimeout`. When the deadline fires, the context is cancelled. Operations MUST honor cancellation:
> - For HTTP calls: pass `ctx` to `http.NewRequestWithContext` so the HTTP transport cancels in-flight requests cleanly.
> - For long CPU work: poll `ctx.Done()` in tight loops.
> - When cancelled, return whatever error the cancelled work produces (likely `context.DeadlineExceeded` wrapping the underlying I/O error). The activity classifies `context.DeadlineExceeded` as retryable per D2-12 (it's not `ErrUnknownCredential`).

**Source:** Verified via Go stdlib `context` docs — `context.WithTimeout` derives a child context whose Done channel closes at the earlier of parent-done or new-deadline. Context cancellation is cooperative; the operation function must propagate it.

### Anti-Patterns to Avoid

- **Storing credentials in `ActionRef` or activity output payloads.** Credentials live ONLY in the activity's stack frame for one operation invocation. Verified by Phase 1's `ActionRef` struct (only `CredentialID string`).
- **Logging `Credential` values via `slog.Info("...", "cred", cred)`.** With the `Format` method on `Secret` and the existing redacted `String()` on Credential kinds, this is safe in v1, but document it as a code-review checklist item — a future op author might use `slog.Info("...", "token", cred.Token.Reveal())` and bypass everything.
- **Calling `activity.RecordHeartbeat` in a tight loop without a `select` on `ctx.Done()`.** Heartbeat is fire-and-forget; cancellation must be checked separately.
- **Using `sync.Map` for the credential cache.** Type-erased; harder to keep TTL invariant correct.
- **Returning a single batch-level error when a retryable failure occurs partway through a batch.** Per D2-13, that IS the correct semantics, but the error must be a `temporal.NewApplicationError` (not just `fmt.Errorf`) so Temporal sees it as a normal application failure and retries per the configured RetryPolicy. **Don't use `temporal.NewNonRetryableApplicationError` for retryable cases.**
- **Calling `OperationFunc` without checking `OperationDispatch[Kind_].Idempotent`.** The activity must defensively re-validate that the batch is homogeneous (all idempotent OR exactly one non-idempotent). Bug class: parser linter has a hole, batch sneaks through, runs non-idempotent action twice on retry.
- **Forgetting to seal `OperationOutput`.** If a third-party package adds `func (X) isOperationOutput()`, the seal is broken — but the unexported method name makes this impossible from outside `pkg/dag`. Verified: Phase 1 already does this for `Credential`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Activity execution context | Custom timeout/cancellation/heartbeat plumbing | `activity.GetInfo`, `activity.RecordHeartbeat`, `activity.GetLogger` from the SDK | Edge cases include task replay, server-driven cancellation, async completion via task token, heartbeat-based timeout detection — all handled by the SDK. Reinventing breaks Temporal's at-most-once guarantees. |
| Per-action timeout | A goroutine + `time.AfterFunc` + close-channel pattern | `context.WithTimeout(parentCtx, dur)` and pass `ctx` to the op | `context.WithTimeout` already coordinates with parent deadlines, propagates cancellation to HTTP libraries via `http.NewRequestWithContext`, and is `-race` clean. |
| Credential cache | LRU with eviction goroutine | `RWMutex + map[string]cachedEntry` with lazy TTL | Phase 1's `Registry` already established the RWMutex pattern; LRU adds tunable knobs we don't need (worker process is the cache lifetime — no eviction pressure in v1). |
| Sealed sum types | Tagged union via discriminator field | Sealed Go interface with unexported `is*` method + concrete kinds | Phase 1 established the pattern (`Credential`); compile-time enforcement; `errors.As`-friendly; type switch is the consumer pattern. |
| Secret redaction | Regex scrubber on every error path | `Secret` wrapper with String/GoString/Format/MarshalJSON/MarshalText | Type-level protection is greppable, audit-able, and impossible to bypass without an explicit `.Reveal()`. Regex scrubber explicitly deferred (D2-09). |
| Activity testing harness | Custom `*testing.T`-driven activity runner | `testsuite.WorkflowTestSuite{}.NewTestActivityEnvironment()` + `ExecuteActivity` | The testsuite handles serialization, header propagation, heartbeat dispatch, and Activity context construction so behavior matches production. |
| Worker registration | Reflection-based name munging | `worker.RegisterActivityWithOptions(impl, activity.RegisterOptions{Name: "ExecuteBatch"})` | Default registration uses the function name (`ExecuteBatch`), but explicit naming is robust against renames/refactors. |
| OperationDispatch lookup | Linear scan of `[]OperationSpec` | `map[string]OperationSpec` keyed by `"<ext>.<op>"` | Phase 1 already keys ActionRef.Kind_ as `"<ext>.<op>"` (verified via `pkg/dag/action.go` line 34); direct map lookup is O(1) and idiomatic. |

**Key insight:** Every decision in CONTEXT.md has a Go-stdlib or Temporal-SDK primitive that maps cleanly to it. Phase 2 should be ~600 LOC of glue, not custom infrastructure.

## Runtime State Inventory

This phase is greenfield Go code addition (no rename/refactor of existing modules with stored state). Skipping the inventory — the phase touches:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | None — no databases, no migration | None |
| Live service config | None — no n8n / Datadog / Tailscale state to update | None |
| OS-registered state | None — no cron / systemd / launchd registrations | None |
| Secrets/env vars | None — `Secret` is in-process only; the actual SOPS/env wiring is in Phase 3's worker bootstrap | None |
| Build artifacts | `pkg/activity/` is new; `go build ./...` will rebuild it on first compile | None — fresh package |

Verified via grep: no `.egg-info`, no compiled binaries, no Docker tags reference Phase-2 code. Current `go.mod` has no Temporal dep yet — that's added by the first Wave-1 task.

## Common Pitfalls

### Pitfall 1: Activity import firewall regression
**What goes wrong:** A future engineer adds `import "go.temporal.io/sdk/activity"` to `pkg/extension` (or `pkg/dag`, or `pkg/parser`) — re-introducing the workflow.Context coupling PROJECT.md forbids.
**Why it happens:** "Just to peek at the Attempt count" — easy to justify; hard to remove later.
**How to avoid:** Add `TestNoTemporalImportsOutsideActivity` mirroring Phase 1's existing tests in `pkg/extension/extension_test.go`. The test walks every Go file's imports under `pkg/` and fails if any non-`pkg/activity` file imports `go.temporal.io/sdk/...`.
**Warning signs:** A PR adding `go.temporal.io/sdk/activity` to a non-activity package; a test failing only when the activity import is removed.

### Pitfall 2: `Secret` field in struct via `%+v` leaks
**What goes wrong:** `slog.Info("auth ctx", "cred", *cred)` on a `BearerCredential` with `Token Secret` — if `Secret` only implements `String()` and not `Format`, `%+v` prints `{ID_:admin Token:{value:ghp_abc123}}`.
**Why it happens:** `fmt.Stringer` is bypassed by `%+v` — it walks struct fields directly via reflection unless the type implements `fmt.Formatter`.
**How to avoid:** Implement `Format(state fmt.State, verb rune)` on `Secret` (Pattern 3 above). Add table-driven test asserting `%+v` redacts.
**Warning signs:** A test like `fmt.Sprintf("%+v", cred)` produces output containing the raw token; a `slog` debug log in production showing struct dumps with non-redacted values.

### Pitfall 3: Mixed-batch slip-through
**What goes wrong:** Parser linter has a bug; a mixed batch reaches the activity; activity processes it as if homogeneous; non-idempotent action runs twice on retryable failure mid-batch.
**Why it happens:** Defense-in-depth was talked about but not implemented; or the activity trusts the parser too much.
**How to avoid:** First step in `ExecuteBatch` is a defensive scan:
```go
func (a *Activity) validateBatch(batch []*dag.ActionRef) error {
    if len(batch) == 0 { return errors.New("empty batch") }
    if len(batch) > a.maxBlockSize {
        return temporal.NewNonRetryableApplicationError(
            fmt.Sprintf("batch size %d exceeds maximum %d", len(batch), a.maxBlockSize),
            "BatchTooLarge", nil)
    }
    var seenIdempotent, seenNonIdempotent bool
    for _, ref := range batch {
        spec, ok := a.dispatch[ref.Kind_]
        if !ok {
            return temporal.NewNonRetryableApplicationError(
                fmt.Sprintf("unknown operation %q", ref.Kind_),
                "UnknownOperation", nil)
        }
        if spec.Idempotent == nil {
            return temporal.NewNonRetryableApplicationError(
                fmt.Sprintf("operation %q has no Idempotent declaration", ref.Kind_),
                "MissingIdempotent", nil) // shouldn't happen — registry rejects nil
        }
        if *spec.Idempotent {
            seenIdempotent = true
        } else {
            seenNonIdempotent = true
        }
    }
    if seenIdempotent && seenNonIdempotent {
        return temporal.NewNonRetryableApplicationError(
            "batch mixes idempotent and non-idempotent operations (parser bug)",
            "MixedIdempotency", nil)
    }
    if seenNonIdempotent && len(batch) > 1 {
        return temporal.NewNonRetryableApplicationError(
            "batch contains non-idempotent operation but has multiple actions (parser bug)",
            "MultiNonIdempotent", nil)
    }
    return nil
}
```
**Warning signs:** Production retry loops where a non-idempotent action's side effect (Slack post, DB insert) happens twice; tests that pass with single-action batches but fail with multi-action.

### Pitfall 4: `context.WithTimeout` deadline missed on per-action enforcement
**What goes wrong:** The op author calls `http.Get(url)` (no context) instead of `http.NewRequestWithContext(ctx, ...)`. Per-action timeout fires; HTTP request keeps running; activity hangs until activity-level StartToCloseTimeout.
**Why it happens:** The default `http.Client.Get` doesn't honor a context — a context-bearing path is opt-in.
**How to avoid:** Document the op-author contract clearly (Pattern 8 above). Add a doc comment on `OperationFunc` reminding authors. Phase 6 example extensions must demonstrate the pattern.
**Warning signs:** Activities that exceed their StartToCloseTimeout when one op's HTTP call hangs; logs showing per-action timeouts firing but the activity not returning.

### Pitfall 5: TestActivityEnvironment Attempt hardcoding
**What goes wrong:** A naive test for D2-11 cache-bypass-on-retry uses `TestActivityEnvironment` and tries to set `Attempt`. There's no public method. Test passes accidentally (because cache wasn't pre-populated) and the cache-bypass logic ships untested.
**Why it happens:** `TestActivityEnvironment.executeActivity` (verified at internal/internal_workflow_testsuite.go:735) hardcodes `parameters.Attempt = 1`. Public API exposes `SetTestTimeout`, `SetHeartbeatDetails`, `SetWorkerStopChannel`, `SetOnActivityHeartbeatListener` — but NOT `SetAttempt`.
**How to avoid:** Use the `attemptFn` injection abstraction (Pattern 5). Unit test the cache-bypass logic by constructing the `*Activity` directly with a stub `attemptFn`, never going through `TestActivityEnvironment` for that specific test. Use TestActivityEnvironment only for end-to-end integration tests that don't depend on Attempt.
**Warning signs:** A D2-11 test that's marked `t.Skip("can't simulate retry in TestActivityEnvironment")`; a passing CI but a real production retry not invalidating the cache.

### Pitfall 6: Heartbeat payload not serializable
**What goes wrong:** A future contributor adds `OperationFunc` to `BatchProgress` "for richer progress info." `RecordHeartbeat` panics in JSON marshaling because `func` is not serializable.
**Why it happens:** `activity.RecordHeartbeat` accepts `details ...interface{}` and serializes via the configured DataConverter (default: JSON). Functions, channels, and unsafe pointers fail.
**How to avoid:** Keep `BatchProgress` strictly value types (int, string, time.Time, []byte). Add a test that `json.Marshal(BatchProgress{...})` succeeds. Never embed action references or operation specs.
**Warning signs:** Activity panics with "json: unsupported type: func()..." in worker logs.

### Pitfall 7: Activity context not cancellation-checked between actions
**What goes wrong:** Workflow signals cancel; activity's parent context Done(); activity ignores it; loops through 50 actions calling external APIs that don't check `ctx.Done()` (or take a different ctx).
**Why it happens:** The cancellation check is easy to forget — Go's `context` is cooperative.
**How to avoid:** First statement in the per-action loop:
```go
for idx, ref := range batch {
    if err := ctx.Err(); err != nil {
        // Append Skipped for remaining indexes (D2-14 shape).
        for j := idx; j < len(batch); j++ {
            results = append(results, dag.SkippedResult{Idx: j, Reason: "activity cancelled"})
        }
        return results, ctx.Err()
    }
    // ...
}
```
**Warning signs:** Cancellation tests where the activity doesn't return promptly after cancel; observation that workflow.Cancel takes minutes to take effect.

## Code Examples

### Example 1: Minimal `ExecuteBatch` skeleton (full orchestration)

```go
// pkg/activity/execute_batch.go
package activity

import (
    "context"
    "errors"
    "fmt"
    "reflect"

    "go.temporal.io/sdk/activity"
    "go.temporal.io/sdk/temporal"

    "github.com/mikelalcon/skytime/pkg/dag"
    "github.com/mikelalcon/skytime/pkg/extension"
)

func (a *Activity) ExecuteBatch(ctx context.Context, batch []*dag.ActionRef) ([]dag.ActionResult, error) {
    // Defense-in-depth: parser should have prevented all of these (D2-05/07/12)
    // but the activity rejects mixed/oversized batches as non-retryable.
    if err := a.validateBatch(batch); err != nil {
        return nil, err
    }

    // Cache bypass on retry (D2-11): drop entries for every cred ID in this batch.
    if a.attemptFn(ctx) > 1 {
        seenIDs := make(map[string]struct{}, len(batch))
        for _, ref := range batch {
            if ref.CredentialID != "" {
                seenIDs[ref.CredentialID] = struct{}{}
            }
        }
        for id := range seenIDs {
            a.cache.invalidate(id)
        }
        activity.GetLogger(ctx).Info("retry attempt — credential cache invalidated",
            "attempt", a.attemptFn(ctx), "credential_ids", len(seenIDs))
    }

    results := make([]dag.ActionResult, 0, len(batch))
    for idx, ref := range batch {
        // Cooperative cancellation check.
        if err := ctx.Err(); err != nil {
            for j := idx; j < len(batch); j++ {
                results = append(results, dag.SkippedResult{Idx: j, Reason: "activity cancelled"})
            }
            return results, ctx.Err()
        }

        out, err := a.runAction(ctx, ref)
        if err != nil {
            // Classify per D2-12 + D2-13/14.
            if isRetryable(err) {
                // D2-13: short-circuit, return error to Temporal — whole batch retries.
                return nil, err
            }
            // D2-14: non-retryable — return all results, including Skipped for later actions.
            results = append(results, dag.NonRetryableErrResult{Idx: idx, Err: err})
            for j := idx + 1; j < len(batch); j++ {
                results = append(results, dag.SkippedResult{
                    Idx: j, Reason: fmt.Sprintf("action %d failed non-retryably", idx),
                })
            }
            return results, nil // nil error — Temporal does NOT retry.
        }

        results = append(results, dag.OkResult{Idx: idx, Output: out})
        // Heartbeat between actions (D2-16).
        a.emitter.emit(ctx, BatchProgress{Action: idx + 1, Total: len(batch)})
    }
    return results, nil
}

// classifyResolveError applies D2-12: ErrUnknownCredential → non-retryable;
// everything else → retryable.
func classifyResolveError(err error) error {
    if errors.Is(err, extension.ErrUnknownCredential) {
        return temporal.NewNonRetryableApplicationError(
            err.Error(), "UnknownCredential", err)
    }
    return temporal.NewApplicationError(err.Error(), "CredentialResolveFailed")
}

// isRetryable inspects an error returned from runAction. It's retryable
// unless wrapped via temporal.NewNonRetryableApplicationError.
func isRetryable(err error) bool {
    var appErr *temporal.ApplicationError
    if errors.As(err, &appErr) {
        return !appErr.NonRetryable()
    }
    // Default: retryable (transient failure assumption).
    return true
}
```
*Source: synthesized from Temporal Go SDK v1.42.0 documented patterns ([activity package](https://pkg.go.dev/go.temporal.io/sdk/activity), [temporal package](https://pkg.go.dev/go.temporal.io/sdk/temporal)) and CONTEXT.md decisions D2-12/13/14/16/17.*

### Example 2: Integration test using `TestActivityEnvironment`

```go
// pkg/activity/execute_batch_test.go
func TestExecuteBatch_HappyPath_Heartbeats(t *testing.T) {
    ts := &testsuite.WorkflowTestSuite{}
    env := ts.NewTestActivityEnvironment()

    // Capture heartbeat payloads for assertion.
    var heartbeats []BatchProgress
    env.SetOnActivityHeartbeatListener(func(_ *activity.Info, details converter.EncodedValues) {
        var bp BatchProgress
        require.NoError(t, details.Get(&bp))
        heartbeats = append(heartbeats, bp)
    })

    // Build a fake dispatch with two idempotent ops.
    dispatch := OperationDispatch{
        "fake.echo": fakeOp{idempotent: true, fn: func(_ context.Context, args any, _ extension.Credential) (dag.OperationOutput, error) {
            return EchoOutput{Got: "ok"}, nil
        }},
    }
    handler := &fakeHandler{creds: map[string]extension.Credential{
        "admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret("ghp_test")},
    }}

    impl := New(dispatch, handler)
    env.RegisterActivity(impl.ExecuteBatch)

    batch := []*dag.ActionRef{
        {Kind_: "fake.echo", Kwargs: starlark.NewDict(0), CredentialID: "admin"},
        {Kind_: "fake.echo", Kwargs: starlark.NewDict(0), CredentialID: "admin"},
    }
    encoded, err := env.ExecuteActivity(impl.ExecuteBatch, batch)
    require.NoError(t, err)

    var results []dag.ActionResult
    require.NoError(t, encoded.Get(&results))
    require.Len(t, results, 2)
    require.IsType(t, dag.OkResult{}, results[0])
    require.IsType(t, dag.OkResult{}, results[1])

    // Heartbeats: one per action (2 actions => 2 heartbeats).
    require.Len(t, heartbeats, 2)
    require.Equal(t, BatchProgress{Action: 1, Total: 2}, heartbeats[0])
    require.Equal(t, BatchProgress{Action: 2, Total: 2}, heartbeats[1])
}
```
*Source: TestActivityEnvironment API verified at [sdk-go/v1.42.0/internal/workflow_testsuite.go](https://github.com/temporalio/sdk-go/blob/v1.42.0/internal/workflow_testsuite.go#L180-L260).*

### Example 3: ACT-05 secret-leak integration test

```go
// pkg/activity/execute_batch_test.go (continued)
func TestExecuteBatch_ACT05_SecretNeverLeaks(t *testing.T) {
    ts := &testsuite.WorkflowTestSuite{}
    env := ts.NewTestActivityEnvironment()

    const fakeSecret = "phantom-token-XYZ-abc123-do-not-leak"

    // Op that fails mid-batch with an error message that *would* leak the
    // raw token if the op author wasn't careful — but they are, so it shouldn't.
    var capturedHeartbeats [][]byte
    env.SetOnActivityHeartbeatListener(func(_ *activity.Info, details converter.EncodedValues) {
        // Capture raw bytes to scan for the secret.
        b, err := details.Get(&BatchProgress{})
        _ = b
        _ = err
        // (For thorough leak detection, also scan internal payload bytes.)
    })

    dispatch := OperationDispatch{
        "leaky.fail": fakeOp{idempotent: false, fn: func(_ context.Context, _ any, cred extension.Credential) (dag.OperationOutput, error) {
            // Op explicitly does NOT call .Reveal() in the error.
            return nil, fmt.Errorf("operation failed using credential %s", cred)
        }},
    }
    handler := &fakeHandler{creds: map[string]extension.Credential{
        "admin": &extension.BearerCredential{ID_: "admin", Token: extension.NewSecret(fakeSecret)},
    }}

    impl := New(dispatch, handler)
    env.RegisterActivity(impl.ExecuteBatch)

    batch := []*dag.ActionRef{
        {Kind_: "leaky.fail", Kwargs: starlark.NewDict(0), CredentialID: "admin"},
    }

    encoded, err := env.ExecuteActivity(impl.ExecuteBatch, batch)
    // Expected: D2-14 non-retryable returned-with-results path; err == nil.
    require.NoError(t, err)

    var results []dag.ActionResult
    require.NoError(t, encoded.Get(&results))
    require.Len(t, results, 1)
    require.IsType(t, dag.NonRetryableErrResult{}, results[0])

    // The critical assertion: the secret string must NOT appear anywhere.
    nonRetry := results[0].(dag.NonRetryableErrResult)
    require.NotContains(t, nonRetry.Err.Error(), fakeSecret,
        "secret leaked into error message")

    // Also verify it doesn't leak in any heartbeat payload bytes (we never reached one
    // since the only action failed, but assert defensively).
    for _, hb := range capturedHeartbeats {
        require.NotContains(t, string(hb), fakeSecret)
    }
}
```
*Source: synthesized from CONTEXT.md ACT-05 amended language and `Secret` redaction guarantees verified above.*

### Example 4: Parser linter backport (mixed-batch reject)

```go
// pkg/parser/linter.go (additions)

// lintMixedIdempotency walks every flow's body and asserts that each Step's
// Actions are homogeneous — either all idempotent or exactly one non-idempotent
// (D2-05). The check needs the OperationDispatch (or equivalent registry
// lookup) to know which op is which. Since the linter runs at parser
// finalize-time, it has access to p.registry.
func (p *Parser) lintMixedIdempotency() error {
    for _, flow := range p.flows {
        if err := p.walkLintMixed(flow); err != nil {
            return err
        }
    }
    return nil
}

func (p *Parser) walkLintMixed(flow *dag.Flow) error {
    var errOut error
    walk(flow.Body, func(n dag.Node) bool {
        step, ok := n.(*dag.Step)
        if !ok || len(step.Actions) <= 1 {
            return true
        }
        var sawIdempotent, sawNonIdempotent bool
        var nonIdempotentOps, idempotentOps []string
        for _, ref := range step.Actions {
            extName, opName, ok := splitKind(ref.Kind_)
            if !ok {
                continue
            }
            ext, ok := p.registry.Get(extName)
            if !ok {
                continue
            }
            spec, ok := ext.Operations()[opName]
            if !ok || spec.Idempotent == nil {
                continue
            }
            if *spec.Idempotent {
                sawIdempotent = true
                idempotentOps = append(idempotentOps, ref.Kind_+" (idempotent)")
            } else {
                sawNonIdempotent = true
                nonIdempotentOps = append(nonIdempotentOps, ref.Kind_+" (NOT idempotent)")
            }
        }
        if sawIdempotent && sawNonIdempotent {
            errOut = &dag.ValidationError{
                Pos:  step.Pos,
                Flow: flow.Name,
                Msg: fmt.Sprintf(
                    "cannot mix idempotent and non-idempotent operations in a block.\n  - %s\n  - %s\nSuggestion: split into separate steps.",
                    idempotentOps[0], nonIdempotentOps[0]),
            }
            return false
        }
        return true
    })
    return errOut
}

// lintBlockSize rejects step(block=[...]) with > maxBlockSize entries (D2-07).
func (p *Parser) lintBlockSize() error { /* analogous walk */ }

// splitKind splits "github.create_issue" into ("github", "create_issue", true).
func splitKind(kind string) (string, string, bool) {
    i := strings.IndexByte(kind, '.')
    if i < 0 || i == 0 || i == len(kind)-1 {
        return "", "", false
    }
    return kind[:i], kind[i+1:], true
}
```
*Source: pattern follows existing `pkg/parser/finalize.go` `walkResolveCallFlows` style.*

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Per-extension activities registered with the worker | Single generic `ExecuteBatch` activity | Project inception (PROJECT.md core decision) | Enables block-batched I/O; ~50x reduction in Temporal history events for batched workflows |
| `output any` from operations | `output OperationOutput` (sealed marker) | Phase 2 (D2-04) | Compile-time enforcement that op return types are explicit; foundation for v2 schema export |
| `string` fields for secrets | `Secret` wrapper with explicit `.Reveal()` | Phase 2 (D2-08) | Type-level redaction; greppable audit boundary; supersedes the deferred regex scrubber |
| Regex error scrubber (original ACT-05) | Type-level redaction via `Secret` (amended ACT-05) | Phase 2 (D2-09) | Simpler; no per-op tuning; relies on op-author discipline (documented contract) |
| Naive `RegisterActivity(fn)` reflection-named | `RegisterActivityWithOptions(fn, RegisterOptions{Name: "ExecuteBatch"})` | Recommended for Phase 2/3 | Survives function renames in code; explicit Temporal activity type name |

**Deprecated/outdated:**
- The original ACT-05 regex scrubber is superseded by the `Secret` type approach (D2-09). The amendment is recorded in REQUIREMENTS.md line 57.
- `go.temporal.io/temporal` (pre-1.0 import path) — never use; always `go.temporal.io/sdk/...`.

## Open Questions

1. **Should `OperationDispatch` be its own type or just `map[string]extension.OperationSpec`?**
   - What we know: D2-17 says `type OperationDispatch map[string]extension.OperationSpec`. CONTEXT.md `<specifics>` says keys are `"<ext>.<op>"`.
   - What's unclear: should the type have helper methods (`Lookup(kind string)`, `Validate()`) or stay a bare map alias?
   - Recommendation: bare type alias for v1; add methods only when a second consumer (Phase 3 interpreter) discovers the need. YAGNI applies.

2. **Should `NewSecret("")` (empty string) be a valid secret or rejected?**
   - What we know: empty Bearer token is meaningless ("Authorization: Bearer "), but APIKey with empty value at registration time may indicate a future env-var override.
   - What's unclear: error vs allow.
   - Recommendation: allow. The credential handler (Phase 6) is the right layer to reject empty tokens; `Secret` is a value-type wrapper, not a validator.

3. **Does Phase 2 need to plumb `slog.Logger` through the activity, or use `activity.GetLogger(ctx)` directly?**
   - What we know: Phase 1 uses `slog.Default()`. Temporal SDK provides `activity.GetLogger(ctx)` which returns the logger configured on the worker.
   - What's unclear: which one wins?
   - Recommendation: use `activity.GetLogger(ctx)` exclusively inside `ExecuteBatch` (it's the Temporal-correct path; respects the worker's logger config and is replay-aware where applicable). Phase 2 does NOT need a separate slog plumb — Phase 3's worker bootstrap will configure both via `worker.Options.Logger`.

4. **Should `OperationSpec.DefaultTimeout` default to a non-zero value?**
   - What we know: zero means "no per-action timeout enforced." Reasonable HTTP ops complete in <30s; pathological ones take minutes.
   - What's unclear: forcing op authors to declare a timeout vs sensible default.
   - Recommendation: default zero (no enforcement) for Phase 2. Phase 6 example extensions will demonstrate setting reasonable values. This decision can be revisited after first real production incident — easy to add a default later.

5. **Should the cache TTL apply uniformly to all credentials, or per-credential override?**
   - What we know: D2-10 says "default 5 minutes, configurable via worker option."
   - What's unclear: whether *one* TTL applies to all creds or each credential ID gets its own.
   - Recommendation: uniform TTL for v1. Per-credential TTL adds API complexity for a rare case; if a customer needs short-TTL rotated tokens vs long-TTL static keys, they configure their CredentialHandler to handle TTL externally and Skytime's cache is just a tiny working-set optimization.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | Compiling Phase 2 code | ✓ | go1.25+ (forced by Starlark; verified in `go.mod`) | — |
| `go.temporal.io/sdk` | Activity API + testsuite | ✗ (not in go.mod yet) | will be v1.42.0 (per STACK.md) | None — required; Wave 0 task adds via `go get go.temporal.io/sdk@v1.42.0` |
| `github.com/stretchr/testify` | Test assertions | ✓ | v1.11.1 (in go.mod) | — |
| `go.starlark.net` | `starlark.Dict` for ActionRef.Kwargs | ✓ | v0.0.0-20260326113308-fadfc96def35 (in go.mod) | — |
| Local Temporal dev server | Live integration smoke (NOT required for Phase 2 — pure unit/testsuite tests are sufficient) | n/a | n/a | TestActivityEnvironment is in-process; no external server needed |

**Missing dependencies with no fallback:** `go.temporal.io/sdk` — must be added as the first Wave 0 task.

**Missing dependencies with fallback:** None.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `github.com/stretchr/testify@v1.11.1` |
| Config file | None (Go test runner uses package-level discovery) |
| Quick run command | `go test ./pkg/activity/... ./pkg/dag/... ./pkg/extension/... -count=1` |
| Full suite command | `go test ./... -race -count=1` |
| Race detector required | YES — for `pkg/activity/credential_cache_test.go` D2-10 cache concurrency |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| ACT-01 | `ExecuteBatch(ctx, []ActionRef) ([]ActionResult, error)` is a single activity that dispatches all I/O | integration | `go test ./pkg/activity -run TestExecuteBatch_HappyPath_SingleAction -count=1` | ❌ Wave 0 |
| ACT-01 | Activity registers with `worker.RegisterActivityWithOptions(impl.ExecuteBatch, RegisterOptions{Name: "ExecuteBatch"})` and is invokable | integration | `go test ./pkg/activity -run TestExecuteBatch_RegistersWithExplicitName -count=1` | ❌ Wave 0 |
| ACT-02 | Returns `[]ActionResult` of correct shape per outcome (Ok / RetryableErr / NonRetryableErr / Skipped) | unit (table-driven) | `go test ./pkg/dag -run TestActionResult_SealedSum -count=1` | ❌ Wave 0 |
| ACT-02 | Activity emits `OkResult` with correct `Idx` and `Output` for happy path | integration | `go test ./pkg/activity -run TestExecuteBatch_HappyPath_Heartbeats -count=1` | ❌ Wave 0 |
| ACT-02 | Non-retryable mid-batch failure produces full `[]ActionResult` (Ok + NonRetryableErr + Skipped) per D2-14 | integration | `go test ./pkg/activity -run TestExecuteBatch_NonRetryableMidBatch_ReturnsAllResults -count=1` | ❌ Wave 0 |
| ACT-02 | Retryable mid-batch failure short-circuits with error per D2-13 | integration | `go test ./pkg/activity -run TestExecuteBatch_RetryableMidBatch_ShortCircuits -count=1` | ❌ Wave 0 |
| ACT-03 | Mixed-idempotency batch rejected at parse time | unit (parser fixture) | `go test ./pkg/parser -run TestLinter_MixedIdempotency_Rejects -count=1` | ❌ Wave 0 |
| ACT-03 | Mixed-idempotency batch rejected at activity level (defense in depth) | unit | `go test ./pkg/activity -run TestExecuteBatch_DefensivelyRejectsMixedBatch -count=1` | ❌ Wave 0 |
| ACT-03 | Block-size cap (50) enforced at parse time | unit (parser fixture) | `go test ./pkg/parser -run TestLinter_BlockSizeCap_Rejects -count=1` | ❌ Wave 0 |
| ACT-03 | Block-size cap enforced at activity level | unit | `go test ./pkg/activity -run TestExecuteBatch_DefensivelyRejectsOversizedBatch -count=1` | ❌ Wave 0 |
| ACT-03 | Single non-idempotent action allowed | integration | `go test ./pkg/activity -run TestExecuteBatch_SingleNonIdempotentAction_Allowed -count=1` | ❌ Wave 0 |
| ACT-04 | `CredentialHandler.Resolve` called from inside activity, not parse time | integration | `go test ./pkg/activity -run TestExecuteBatch_HandlerInvokedJIT -count=1` | ❌ Wave 0 |
| ACT-04 | Credential cached with TTL on first resolve | unit | `go test ./pkg/activity -run TestCredentialCache_HitsAfterFirstResolve -count=1` | ❌ Wave 0 |
| ACT-04 | Cache expires after TTL | unit (with injectable clock) | `go test ./pkg/activity -run TestCredentialCache_ExpiresAfterTTL -count=1` | ❌ Wave 0 |
| ACT-04 | Cache parallel-access race-clean | unit (race) | `go test ./pkg/activity -run TestCredentialCache_RaceParallelBatches -race -count=1` | ❌ Wave 0 |
| ACT-04 | `errors.Is(err, ErrUnknownCredential)` → NonRetryableErrResult per D2-12 | unit (table) | `go test ./pkg/activity -run TestClassifyResolveError_TableDriven -count=1` | ❌ Wave 0 |
| ACT-04 | Other handler errors → RetryableErrResult per D2-12 | unit (table) | (same test as above) | ❌ Wave 0 |
| ACT-04 | Cache bypassed when `Attempt > 1` per D2-11 | unit (with attemptFn stub) | `go test ./pkg/activity -run TestExecuteBatch_RetryAttempt_BypassesCache -count=1` | ❌ Wave 0 |
| ACT-05 | `Secret.String()`, `GoString()`, `MarshalJSON()`, `MarshalText()`, `Format()` all redact | unit (table over verbs) | `go test ./pkg/extension -run TestSecret_FullRedactionMatrix -count=1` | ❌ Wave 0 |
| ACT-05 | `BearerCredential` / `BasicCredential` / `APIKeyCredential` redact through all formatters | unit | `go test ./pkg/extension -run TestCredentials_RedactedInAllFormats -count=1` | ❌ Wave 0 |
| ACT-05 | Integration: known secret never appears in any returned `ActionResult` | integration | `go test ./pkg/activity -run TestExecuteBatch_ACT05_SecretNeverLeaks -count=1` | ❌ Wave 0 |
| ACT-05 | `Secret.Reveal()` returns the raw value | unit | `go test ./pkg/extension -run TestSecret_Reveal -count=1` | ❌ Wave 0 |
| ACT-05 | Heartbeat payloads do not contain the secret | integration (heartbeat listener) | (rolled into ACT-05 integration test above) | ❌ Wave 0 |
| ACT-06 | Activity heartbeats between every action | integration (heartbeat listener) | `go test ./pkg/activity -run TestExecuteBatch_HappyPath_Heartbeats -count=1` | ❌ Wave 0 |
| ACT-06 | Heartbeat payload is `BatchProgress{Action, Total}` JSON-serializable | unit | `go test ./pkg/activity -run TestBatchProgress_JSONSerializable -count=1` | ❌ Wave 0 |
| ACT-06 | StartToCloseTimeout responsibility documented (set by interpreter, enforced via context.WithTimeout in activity) | unit (per-action timeout) | `go test ./pkg/activity -run TestActionExecutor_PerActionTimeout -count=1` | ❌ Wave 0 |
| ACT-06 | Cancellation between actions terminates the loop with Skipped placeholders | integration | `go test ./pkg/activity -run TestExecuteBatch_CancellationStopsBetweenActions -count=1` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./pkg/activity/... ./pkg/dag/... ./pkg/extension/... -count=1` (the three packages Phase 2 modifies)
- **Per wave merge:** `go test ./... -race -count=1` (full suite with race detector — catches credentialCache concurrency regressions)
- **Phase gate:** `go test ./... -race -count=1 && go vet ./... && go build ./...` — all green before `/gsd:verify-work`

### Wave 0 Gaps

The following test files / fixtures must be created BEFORE production code lands:

- [ ] `pkg/dag/result.go` and `pkg/dag/result_test.go` — `ActionResult` sealed sum + 4 kinds + `_test.go` asserting the seal (compile-time test that an external type cannot satisfy the interface).
- [ ] `pkg/dag/output.go` and `pkg/dag/output_test.go` — `OperationOutput` marker + sealed-test.
- [ ] `pkg/extension/secret.go` and `pkg/extension/secret_test.go` — `Secret` type with full redaction matrix table test (all `fmt` verbs + JSON + Text).
- [ ] `pkg/extension/unknown_credential.go` — define `var ErrUnknownCredential = errors.New("unknown credential")`. Add to existing `handler.go` if smaller package surface preferred.
- [ ] `pkg/extension/testing/fake_handler.go` and `_test.go` — `FakeCredentialHandler` for shared use across `pkg/activity` tests and future Phase 5/6 tests. Sub-package; not internal-only.
- [ ] `pkg/parser/linter_test.go` — fixtures `tests/fixtures/parser/invalid_mixed_idempotency.star` and `invalid_block_oversized.star` for D2-05 and D2-07 validation. Note: Phase 1 fixtures already exist; this adds two more.
- [ ] `pkg/activity/doc.go` — package documentation explaining the firewall, the import allowlist (only this package may import `go.temporal.io/sdk/activity`).
- [ ] `pkg/activity/credential_cache_test.go` — must include a race-condition test runnable with `-race`.
- [ ] `pkg/activity/heartbeat_test.go` — assert `BatchProgress` JSON-serializability; assert real emitter uses `activity.RecordHeartbeat`.
- [ ] `pkg/activity/attempt_test.go` — assert `defaultAttemptFunc` reads from `activity.GetInfo`; assert injected stub overrides it.
- [ ] `pkg/activity/execute_batch_test.go` — the integration tests via `TestActivityEnvironment`. This is the largest test file; covers ACT-01, ACT-02, ACT-03 (single non-idempotent), ACT-04 (JIT resolve), ACT-05 (secret-leak integration), ACT-06 (heartbeat).
- [ ] Framework install: add `go.temporal.io/sdk@v1.42.0` via `go get`; verify `go mod tidy` succeeds; commit `go.mod` + `go.sum`.

## Sources

### Primary (HIGH confidence)
- [Temporal Go SDK v1.42.0 source — testsuite/testsuite.go](https://github.com/temporalio/sdk-go/blob/v1.42.0/testsuite/testsuite.go) — verified `TestActivityEnvironment` is a public type alias to internal.
- [Temporal Go SDK v1.42.0 source — internal/workflow_testsuite.go](https://github.com/temporalio/sdk-go/blob/v1.42.0/internal/workflow_testsuite.go) — verified all public methods of `TestActivityEnvironment` (lines 180-285): `RegisterActivity`, `RegisterActivityWithOptions`, `ExecuteActivity`, `ExecuteLocalActivity`, `SetWorkerOptions`, `SetDataConverter`, `SetFailureConverter`, `SetIdentity`, `SetContextPropagators`, `SetHeader`, `SetTestTimeout`, `SetHeartbeatDetails`, `SetWorkerStopChannel`, `SetOnActivityHeartbeatListener`, `SetExecuteActivitiesInWorkflow`. Confirmed: NO `SetAttempt` method exists.
- [Temporal Go SDK v1.42.0 source — internal/internal_workflow_testsuite.go](https://github.com/temporalio/sdk-go/blob/v1.42.0/internal/internal_workflow_testsuite.go) — verified at line 735+ that `executeActivity` hardcodes `Attempt: 1`.
- [Temporal Go SDK v1.42.0 source — internal/activity.go](https://github.com/temporalio/sdk-go/blob/v1.42.0/internal/activity.go) — verified `ActivityInfo` struct fields: `TaskToken`, `WorkflowExecution`, `ActivityID`, `ActivityRunID`, `ActivityType`, `TaskQueue`, `Namespace`, `HeartbeatTimeout`, `ScheduleToCloseTimeout`, `StartToCloseTimeout`, `ScheduledTime`, `StartedTime`, `Deadline`, `Attempt int32`, `IsLocalActivity`, `Priority`, `RetryPolicy`.
- [pkg.go.dev — go.temporal.io/sdk/activity](https://pkg.go.dev/go.temporal.io/sdk/activity) — verified function signatures for `GetInfo`, `RecordHeartbeat`, `HasHeartbeatDetails`, `GetHeartbeatDetails`, `GetLogger`, sentinel errors `ErrResultPending`, `ErrActivityPaused`, `ErrActivityReset`.
- [pkg.go.dev — go.temporal.io/sdk/temporal](https://pkg.go.dev/go.temporal.io/sdk/temporal) — verified `NewApplicationError(message, errType, details...)`, `NewNonRetryableApplicationError(message, errType, cause, details...)`, `NewApplicationErrorWithCause(message, errType, cause, details...)`. Confirmed `IsRetryableError` is NOT a public function — retryability inspected via `*temporal.ApplicationError.NonRetryable()` after `errors.As`.
- [Temporal Activity Cancellation docs](https://docs.temporal.io/develop/go/cancellation) — verified `ctx.Done()` fires on workflow cancel; `RecordHeartbeat` after cancel returns nothing but logs WARN with "context canceled."
- [Temporal Activity Timeouts blog](https://temporal.io/blog/activity-timeouts) — verified the four timeout types (Schedule-To-Close, Start-To-Close, Heartbeat, Schedule-To-Start) and their semantics.
- Phase 1 source files in `/Users/mikel/dev/ai/temporero/pkg/` — verified existing types (`ActionRef`, `Credential`, `OperationFunc`, `OperationSpec`, `Registry`, `CredentialHandler`, `ParseError`, `ValidationError`) Phase 2 builds on.
- `pkg/parser/finalize.go` and `pkg/parser/linter.go` — verified the lint-pass extension point Phase 2 backports D2-05 and D2-07 into.

### Secondary (MEDIUM confidence)
- [Common Fate — Prevent Logging Secrets in Go](https://www.commonfate.io/blog/prevent-logging-secrets-in-go-by-using-custom-types) — recommended `String()` and `MarshalJSON()` redaction; their implementation does NOT cover `fmt.Formatter` for `%+v` and uses an exposed `.Value` field — Phase 2 improves on this by using a private field, `.Reveal()` accessor, and `Format` implementation.
- [andrewbenton/go-secrets](https://github.com/andrewbenton/go-secrets) — uses `.Get()` accessor; doesn't cover `Format`. Phase 2 prefers `.Reveal()` for greppable audit semantics.
- [Travis Jeffery — Keep Passwords and Secrets Out of Logs with Go](https://medium.com/hackernoon/keep-passwords-and-secrets-out-of-your-logs-with-go-a2294a9546ce) — early-2020 article; covers `fmt.Stringer` only, missing `%+v` coverage. Confirms our gap analysis is correct.
- [Go encoding/json package docs](https://pkg.go.dev/encoding/json) — verified `Marshaler` interface contract: `MarshalJSON` is called when the value implements it; doesn't fall back to `String()`.

### Tertiary (LOW confidence — directional)
- [Go talks 2015 — JSON, interfaces, and go generate](https://go.dev/talks/2015/json.slide) — historical context for the sealed-marker pattern; useful for justifying our choice but the talk predates current Go ecosystem conventions.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — Temporal SDK v1.42.0 verified via direct source inspection of v1.42.0 tag
- Architecture: HIGH — every Pattern is supported by verified API or existing Phase 1 code
- `Secret` design: HIGH — all four formatter interfaces verified against stdlib `fmt`/`encoding/json`/`encoding` semantics
- Testability claim re: TestActivityEnvironment: HIGH — verified by reading the v1.42.0 internal source; the lack of `SetAttempt` is confirmed
- Per-action timeout via `context.WithTimeout`: HIGH — Go stdlib `context` semantics are well-documented
- Pitfalls: HIGH on the credential leak / mixed-batch / cancellation classes (verified against Phase 1 patterns); MEDIUM on the heartbeat replay interaction (D2-13 explicitly avoids using HeartbeatDetails for state recovery, so the interaction is mostly aspirational)

**Research date:** 2026-04-28
**Valid until:** 2026-05-28 (30-day window — Temporal SDK v1.42.0 was released 2026-04-08, no v1.43 yet, stable)

---
*Phase: 02-generic-activity-block-batch-dispatch-credentials*
*Research completed: 2026-04-28*
