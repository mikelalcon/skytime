# Phase 2: Generic Activity + Block-Batch Dispatch + Credentials - Context

**Gathered:** 2026-04-27
**Status:** Ready for planning

<domain>
## Phase Boundary

Build the single Temporal activity (`ExecuteBatch`) that dispatches all extension I/O. Testable standalone with hand-built `[]ActionRef` inputs — no interpreter, no `.star` files, no workflow involvement yet (those are Phases 3 and beyond). Phase 2 owns 6 v1 requirements: ACT-01..06.

The activity is the only code that ever crosses the Skytime↔Temporal-activity boundary. Every operation invocation, every credential resolution, every per-action result, and every error scrub-or-return decision happens inside `ExecuteBatch`.

</domain>

<decisions>
## Implementation Decisions

### ActionResult — Shape & Location

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
  Compile-time check: ops cannot return arbitrary `map[string]interface{}` without explicitly implementing the marker. Trade-off: more boilerplate per op, much stronger contract for the interpreter (Phase 3) and future schema export (v2).

- **D2-04: OperationFunc signature update.** The Phase 1 signature was:
  ```go
  type OperationFunc func(ctx context.Context, args any, cred Credential) (output any, err error)
  ```
  Phase 2 narrows `output any` → `output OperationOutput`. This is a Phase-1-to-Phase-2 backward-incompatible signature change, but Phase 1 ships no real extensions yet (the test-only `fake_ext.echo` is the only consumer), so the migration is internal.

### Mixed-Idempotency Block Policy (Policy D)

- **D2-05: A `step(block=[...])` may NOT mix idempotent and non-idempotent operations.** The parser linter (Phase 1's `pkg/parser/linter.go` already exists; Phase 2 backports a check or — preferred — Phase 2 adds a new lint pass) emits:
  ```
  flows/example.star:5:5 [issue_creation > step #1]: cannot mix idempotent and non-idempotent operations in a block.
    - github.create_issue (idempotent)
    - slack.post_message (NOT idempotent)
  Suggestion: split into separate steps.
  ```
  at parse time. Defense in depth: the activity also rejects mixed batches with `NonRetryableErrResult` for every action, but this should be unreachable given the parser check.

- **D2-06: Splitting logic lives in Phase 3 interpreter, NOT in Phase 2 activity.** The activity always receives a homogeneous batch (all-idempotent OR a single non-idempotent action). This keeps the activity simple and testable in Phase 2 with hand-built batches; no idempotency-aware routing required at the activity layer.

- **D2-07: Block-size cap = 50 actions.** Per project research, balanced against Temporal's ~4MB activity input limit. The parser linter rejects blocks with > 50 actions at parse time:
  ```
  flows/example.star:5:5 [flow > step #1]: block has 73 actions; maximum is 50. Split into multiple steps.
  ```
  Configurable via parser option (`parser.WithMaxBlockSize(N)`); default 50. The activity also defensively rejects > 50 with `NonRetryableErr`.

### Credential Resolution & Caching

- **D2-08: Credentials use a `Secret` wrapper type for raw bytes.** Adopt Option C from the discussion. Phase 1's `BearerCredential`, `BasicCredential`, `APIKeyCredential` get refactored:
  ```go
  // pkg/extension/credential.go (Phase 2 update)
  type Secret struct{ value string }

  func (s Secret) String() string                  { return "<redacted>" }
  func (s Secret) GoString() string                { return "<redacted>" }
  func (s Secret) MarshalJSON() ([]byte, error)    { return []byte(`"<redacted>"`), nil }
  func (s Secret) Reveal() string                  { return s.value }
  func NewSecret(raw string) Secret                { return Secret{value: raw} }

  type BearerCredential struct {
      ID_   string
      Token Secret  // was: Token string
  }
  type BasicCredential struct {
      ID_      string
      User     string
      Password Secret  // was: Password string
  }
  type APIKeyCredential struct {
      ID_        string
      Key        Secret  // was: Key string
      HeaderName string
  }
  ```
  Operations call `cred.(*BearerCredential).Token.Reveal()` at the HTTP-call site. The `.Reveal()` call is greppable; code review can audit every place a secret leaves type protection.

- **D2-09: NO regex error scrubber in v1.** ACT-05 in REQUIREMENTS.md is amended (see "Requirements Amendment" below). Defense relies on (i) Phase 1's redacted `String()` on Credential kinds, (ii) the new `Secret` wrapper's `String()`/`GoString()`/`MarshalJSON()`, and (iii) the documented op contract: "if a third-party library you call from an op leaks a revealed secret into its errors or logs, file a bug upstream or wrap the library; Skytime does not paper over it." Re-evaluate after the first real customer incident; the regex layer can be added in v1.x without breaking changes.

- **D2-10: Activity-level credential cache is per-worker with TTL.** `pkg/activity` maintains a `map[string]cachedEntry` keyed by credential ID. Default TTL: 5 minutes (configurable via `activity.WithCredentialCacheTTL(...)` worker option). Cache is process-local; not persisted, not shared across workers.

- **D2-11: Cache bypass on retry attempt.** When `activity.GetInfo(ctx).Attempt > 1`, the activity invalidates cached entries for every credential ID in the current batch and forces a fresh `Resolve` call. Reconciles D2-10 (worker cache for performance) with the user's preference for "retry = fresh resolution" (correctness for token rotation).

- **D2-12: CredentialHandler error classification.**
  - Handler may return a typed error wrapping `extension.ErrUnknownCredential` → activity wraps as `NonRetryableErrResult` (configuration bug; retrying won't help).
  - Any other error → activity wraps as `RetryableErrResult` (transient backend failure).
  - Convention: handler authors should `errors.Is(err, extension.ErrUnknownCredential)` for unknown IDs to opt into non-retryable classification.

### Batch Failure Semantics

- **D2-13: On retryable failure within a batch, the activity returns an error to Temporal.** The activity does NOT swallow retryable errors and return per-action results. Instead, on the first `RetryableErrResult` produced during batch execution, the activity short-circuits and returns the error to Temporal. Temporal retries the whole `ExecuteBatch` invocation. The whole batch (including actions that already succeeded externally) re-executes — which is safe by Policy D (idempotent batches only).

- **D2-14: On non-retryable failure within a batch, the activity returns ALL results so far.** Non-retryable errors don't trigger Temporal retry (by definition), so the activity produces a complete `[]ActionResult` for every action in the input batch — including successes before the failure, the `NonRetryableErrResult` at the failing index, and `SkippedResult` placeholders for actions after the failure. The interpreter (Phase 3) decides whether the workflow itself fails or proceeds.

- **D2-15: Activity StartToCloseTimeout = sum(per-action timeouts) + 30s headroom.** Computed by the interpreter at activity-schedule time (Phase 3); the activity itself just enforces per-action timeouts via `context.WithTimeout`.

- **D2-16: Heartbeat between every action.** The activity calls `activity.RecordHeartbeat(ctx, BatchProgress{Action: i, Total: n})` after each action completes (success or failure). `BatchProgress` is a simple struct serialized into the heartbeat payload so an external observer can see progress. Heartbeat interval is implicit (per action) — no time-based heartbeating in v1.

### Operation Dispatch

- **D2-17: Operation dispatch table is built at parser finalize-time and passed into the activity environment.** The parser, after collecting all registered extensions, produces:
  ```go
  type OperationDispatch map[string]extension.OperationSpec  // key: "extension.op", e.g. "github.create_issue"
  ```
  This map is handed to the activity at worker registration (Phase 3 wires it). The activity does NOT import `pkg/parser` — it just consumes the map. This decouples activity execution from parser semantics and allows the activity to be tested in isolation with a hand-built dispatch map.

### Package Naming

- **D2-18: Package name is `pkg/activity`.** Despite the Cadence-backend hedge mentioned in project research, `pkg/activity` is clear and matches Temporal's vocabulary. If we ever pluggable-ify the backend, rename to `pkg/dispatch` then; v1 doesn't need the abstraction.

### Claude's Discretion

- Exact API of `Secret` constructor and accessor (e.g., method names like `.Reveal()` vs `.Get()` vs `.Unwrap()`).
- Internal data structure for the credential cache (`sync.Map` vs `sync.RWMutex` + plain map).
- Test fixture for "fake credential handler" — file-based, env-based, or in-memory.
- Specific metrics/observability hooks (none required for v1, but adding `slog` debug-level logs is fine).
- Whether `SkippedResult` is exposed in the public package or kept internal until first use (export it to make the type complete).

</decisions>

<requirements_amendment>
## Requirements Amendment

**ACT-05 (REQUIREMENTS.md) is amended for Phase 2:**

**Original (v1):**
> ACT-05: An error-scrubbing middleware regex-strips token-shaped strings from every error before it returns to Temporal; an integration test injects a known fake-secret and asserts it never appears in any returned error or history payload.

**Amended (v1, post-discussion 2026-04-27):**
> ACT-05: Credentials use a `Secret` wrapper type whose `String()`, `GoString()`, and `MarshalJSON()` always return `"<redacted>"`. Operations extract the raw value via an explicit `.Reveal()` call. An integration test injects a known fake-secret as a `Secret`, fails an op mid-batch, and asserts the secret string never appears in any returned `ActionResult`, error wrapper, or activity heartbeat payload — relying entirely on type-level protection (no regex scrubber in v1). If a third-party library used by an op leaks a revealed secret into its error path, the op author is responsible for wrapping the library or filing an upstream bug.

The orchestrator will edit REQUIREMENTS.md to reflect this amendment alongside the CONTEXT.md commit.

</requirements_amendment>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project specs
- `.planning/PROJECT.md` — Strict directives (no string compilation, no dynamic activities, no context bleed).
- `.planning/REQUIREMENTS.md` — All v1 requirements; Phase 2 owns ACT-01..06 (with ACT-05 amended per this document).
- `.planning/ROADMAP.md` — Phase 2 entry: goal, success criteria, requirement list.

### Phase 1 outputs (Phase 2 builds directly on these)
- `.planning/phases/01-type-spine-extension-contract-parser-bridge-foundations/01-CONTEXT.md` — D-08, D-09, D-10, D-12 are the foundation Phase 2 extends (JIT credential resolution, sealed Credential interface, CredentialHandler at worker level, required Idempotent declaration).
- `.planning/phases/01-type-spine-extension-contract-parser-bridge-foundations/01-VERIFICATION.md` — confirms what's already in place: sealed Credential, redacted String(), CredentialHandler interface, Idempotent enforcement.
- `pkg/dag/action.go` — `ActionRef` struct (already shipped); `ActionResult` and `OperationOutput` types are added in Phase 2.
- `pkg/extension/credential.go` — Bearer/Basic/APIKey types (Phase 2 modifies the secret-bearing fields to use `Secret`).
- `pkg/extension/handler.go` — `CredentialHandler` interface (Phase 2 wires the call from the activity).
- `pkg/extension/operation.go` — `OperationFunc` and `OperationSpec` (Phase 2 narrows return type and consumes `Idempotent`).
- `pkg/extension/registry.go` — Registry (Phase 2 reads it to build the OperationDispatch map).

### Project-level research
- `.planning/research/SUMMARY.md` §"Phase 2" — block-batched I/O, partial-failure semantics, credential leakage at activity boundary.
- `.planning/research/PITFALLS.md` §5 (partial failure), §6 (credential leakage) — Phase 2's specific risk areas.
- `.planning/research/STACK.md` — `go.temporal.io/sdk/activity` is the ONLY package allowed to import this; pinned to v1.42.0.

### External (Temporal-specific)
- Temporal Go SDK `activity` package docs — `activity.GetInfo(ctx).Attempt`, `activity.RecordHeartbeat`, sum-of-timeouts pattern.
- Temporal docs on activity retry policy and `StartToCloseTimeout` vs `HeartbeatTimeout`.
- Temporal docs on payload size limits (4MB activity input default; informs D2-07 batch cap).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/dag.ActionRef` — already a `starlark.Value` with recursive Freeze; Phase 2 reads `Kind_`, `Kwargs`, `CredentialID`, `Pos`.
- `pkg/extension.Credential` (sealed interface) + 3 concrete kinds with redacted `String()` — Phase 1's foundation; Phase 2 modifies the secret-bearing fields to use `Secret`.
- `pkg/extension.CredentialHandler` interface — Phase 2 calls `Resolve(ctx, id)` from the activity.
- `pkg/extension.OperationFunc` / `OperationSpec` (with `Idempotent *bool`) — Phase 2 reads `Idempotent` for batch validation; calls `OperationFunc` for execution.
- `pkg/extension.Registry` — Phase 2 reads it to build the `OperationDispatch` map at worker setup time.
- `pkg/dag.ParseError`, `pkg/dag.ValidationError` — typed error spine; Phase 2 adds `pkg/activity` errors that compose with these.

### Established Patterns
- Sealed interfaces for sum types (Phase 1 `Credential`) — D2-01 reuses for `ActionResult`, D2-03 reuses for `OperationOutput`.
- Typed errors with `Position()` — Phase 2 errors at parse time (mixed-batch lint) reuse `ValidationError`.
- Per-parser registries (no global state) — Phase 2's `OperationDispatch` map is similarly per-worker.
- Co-located `*_test.go` + `tests/fixtures/` for `.star` corpus — Phase 2 adds Go-only unit tests in `pkg/activity/`; no new `.star` fixtures (those are Phase 3+ concerns).
- Atomic commits per task — Phase 2 follows the same convention.

### Integration Points
- `pkg/dag.ActionResult` (new) is the wire format from Phase 2 activity to Phase 3 interpreter.
- `pkg/dag.OperationOutput` (new) is the typed-output marker every extension implements.
- `pkg/extension.Secret` (new) is the wrapper used by all Credential-bearing structs going forward.
- The parser linter (Phase 1) gets a new pass for D2-05 (mixed-batch reject) and D2-07 (block-size cap reject).

</code_context>

<specifics>
## Specific Ideas

- **`Secret.Reveal()` discoverability**: every place that calls `.Reveal()` is a "secret leaves type protection" boundary. Recommendation: put a `// nolint:secretreveal` (or similar) lint comment hook on the function so a future linter can surface call sites for audit. Phase 2 just defines `.Reveal()`; the linter is post-v1.

- **Test fake credential handler**: Phase 2 needs an in-memory `CredentialHandler` for tests. Suggested shape:
  ```go
  type FakeCredentialHandler struct {
      Creds map[string]Credential
  }
  func (h *FakeCredentialHandler) Resolve(ctx context.Context, id string) (Credential, error) {
      c, ok := h.Creds[id]
      if !ok {
          return nil, fmt.Errorf("%w: %s", ErrUnknownCredential, id)
      }
      return c, nil
  }
  ```
  Lives in `pkg/extension/testing/` or similar (test-only sub-package).

- **`OperationDispatch` map shape**: keys are `"extension.op"` strings (e.g., `"github.create_issue"`), values are `OperationSpec`. The activity looks up by composing `ActionRef.Kind_` directly. No string parsing needed — extension authors set `Kind_` to the same `"<ext_name>.<op_name>"` shape when constructing ActionRefs.

- **Heartbeat payload `BatchProgress`**: simple struct with action index and total. Future v1.x can add op name, elapsed time, etc. Keep v1 minimal.

</specifics>

<deferred>
## Deferred Ideas

- **Regex error scrubber** — explicitly deferred. Re-evaluate after first real customer incident. Easy to add in v1.x without breaking changes (purely additive layer at the activity boundary).
- **Per-extension `Scrubbers []*regexp.Regexp` on `OperationSpec`** — deferred with the regex layer.
- **Time-based heartbeating** (heartbeat every N seconds during long single ops) — v1 only heartbeats between actions. Add when first long-running op surfaces the gap.
- **`SkippedResult` emission paths** — variant defined but unused in v1. Add for "extension declared a precondition that wasn't met" or similar in v2.
- **OperationOutput schema export** (JSON Schema, markdown docs) — listed as `OPS-V2-05` in REQUIREMENTS.md. Phase 2's `OperationOutput` interface is the foundation; the export tool is post-v1.
- **Cross-worker credential cache** (Redis, etc.) — out of scope. Per-worker is sufficient for v1.
- **`Secret`-aware linter** that flags `.Reveal()` call sites without an `http.Header`-or-similar approved sink — useful audit tool, post-v1.
- **Mixed-batch policy override** (e.g., a `step(block=[...], allow_mixed=true)` flag) — explicitly NOT supported in v1; revisit if a customer's workflow genuinely needs it.

</deferred>

---

*Phase: 02-generic-activity-block-batch-dispatch-credentials*
*Context gathered: 2026-04-27*
