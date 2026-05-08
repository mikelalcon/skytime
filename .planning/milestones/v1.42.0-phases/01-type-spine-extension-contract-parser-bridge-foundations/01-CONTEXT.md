# Phase 1: Type Spine + Extension Contract + Parser/Bridge Foundations - Context

**Gathered:** 2026-04-26
**Status:** Ready for planning

<domain>
## Phase Boundary

Lock the data types every later phase imports (`pkg/dag`, `pkg/extension`), implement the Starlark parser with all six DSL primitives (`flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow`), and stand up the state/lambda bridge so a `.star` file can be parsed into an inspectable `dag.Flow` — with **zero Temporal involvement** in this phase. Phase 2 introduces the activity; Phase 3 introduces the interpreter.

22 v1 requirements are in scope (DSL-01..10, EXT-01..06, PARSE-01..06). All other requirement groups (ACT, INTRP, WORK, VAL, TEST, CLI, EX) are out of scope for this phase.

</domain>

<decisions>
## Implementation Decisions

### Module Layout & Conventions

- **D-01: Module path** — `github.com/mikelalcon/skytime`. Use as the canonical Go module path from day one; rename only on a future move to an org namespace.
- **D-02: Package layout** — Public packages live under `pkg/` (`pkg/dag`, `pkg/extension`, `pkg/parser`, `pkg/bridge`). Future non-public helpers go under `internal/`. CLI lives at `cmd/skytime/` (Phase 4 land); examples at `examples/...` (Phase 6 land).
- **D-03: Test layout** — Co-located `*_test.go` files in the same package as the code under test. A top-level `tests/fixtures/` directory holds `.star` corpus files for parser tests and (later) the static-vs-runtime differential corpus test in Phase 4.
- **D-04: Error design** — Typed errors with a `Position()` method. Concretely: `type ParseError struct { Pos syntax.Position; Msg string; Wrapped error }` and `type ValidationError struct { Pos syntax.Position; Flow, Step string; Msg string; Wrapped error }`. Both implement `error`, expose `Position()`, and format as `<file>:<line>:<col>: <msg>`. Use `errors.As` at boundaries (CLI in Phase 4 will rely on this to render Starlark-first errors).
- **D-05: Go directive** — `go 1.25` in `go.mod`, toolchain `go1.26.2` available. Forced by `go.starlark.net`'s floor; do not bump to 1.26 (cuts off the previous-supported Go release for downstream consumers).
- **D-06: Logging interface** — Library accepts `*slog.Logger`; defaults to `slog.Default()`. No hard backend dependency. CLI (Phase 4) wires charm-log as the slog handler.

### Extension SDK

- **D-07: Registration** — Per-parser registry. `parser := skytime.NewParser(opts...)` then `parser.Register(github.New(...))`. No global state. Functional-options pattern (`skytime.NewParser(skytime.WithRoot(...), skytime.WithExtensions(...))`) is acceptable as a convenience but the underlying mechanism is per-parser.
- **D-08: Credential resolution lifecycle** — **JIT inside the activity.** Starlark's `gh = github.endpoint("admin")` creates a Starlark value carrying only the credential ID (`"admin"`). Every `ActionRef` derived from `gh` embeds `CredentialID="admin"` only. The credential handler runs inside `ExecuteBatch` (Phase 2 owns the call site) and returns a typed `Credential`. **Extensions may cache the resolved credential internally** in their own state (e.g., once per extension instance per worker process). Workflow state, `ActionRef`, and Temporal history must never contain a resolved secret.
- **D-09: CredentialHandler interface** — Typed per-credential-kind. Define a `Credential` interface and concrete kinds. v1 ships:
  ```go
  type Credential interface {
      ID() string
      isCredential() // sealed interface
  }
  type BearerCredential struct{ ID_, Token string }
  type BasicCredential struct{ ID_, User, Password string }
  type APIKeyCredential struct{ ID_, Key, HeaderName string }
  ```
  Each kind has a redacted `String()` (returns `"<credential:bearer:admin>"` or similar — never the secret). Extensions accept the credential kind they need and assert at the operation boundary; mismatches are runtime errors with a clean message. Adding new credential kinds is a non-breaking change.
- **D-10: Handler scope** — Single `CredentialHandler` registered on the worker (Phase 3 wires `worker.Run(client, flowDir, skytime.WithCredentialHandler(...))`). Handler is consulted by ID; it routes internally per-extension if needed. Phase 1 lays the interface and registration plumbing; Phase 3 wires it into the worker entry point.
- **D-11: Operation kwargs schema** — Typed Go struct with reflection-based validation. Operations declare a parameter struct:
  ```go
  type CreateIssueArgs struct {
      Repo  string `star:"repo,required"`
      Title string `star:"title,required"`
      Body  string `star:"body"`
  }
  ```
  Parser uses reflection on these structs to (a) validate Starlark kwargs at parse time, (b) report missing/unknown kwargs with `<file>:<line>:<col>` precision, (c) export the schema for static validation in Phase 4. Single source of truth for types and validation.
- **D-12: Idempotent declaration** — **Required, no default.** Extension registration fails if any operation is missing the declaration. Forces extension authors to make a conscious choice. Declared via the operation's metadata (concretely: a method on the operation, e.g., `Idempotent() bool`, or a struct field on a registered op spec — Phase 1 picks one and locks it).

### load() Resolution & Multi-Flow

- **D-13: load() syntax** — Both relative and absolute paths supported.
  - Relative: `load("./shared/utils.star", "format_pr_link")` resolves to a sibling of the loading file.
  - Absolute: `load("/shared/utils.star", "format_pr_link")` resolves from the **root**.
- **D-14: Root discovery** — Single root configured per parser. Discovery order:
  1. Explicit `WithRoot("/path/to/flows")` option on `NewParser` (CLI exposes this as `--rootdir`).
  2. If unset, walk up from the loading file looking for the first `.git` directory; that directory is the root.
  3. If neither found, `load()` of an absolute path is a parse error with a clear "no root configured" message.
- **D-15: Multi-flow per file** — A single `.star` file can declare multiple flows (`flow(name="approve_pr", ...)`, `flow(name="reject_pr", ...)`). The parser collects every `flow()` call across all loaded files into a `map[string]*dag.Flow` keyed by `Name`. Duplicate flow names across the parser session are a parse error.
- **D-16: call_flow resolution** — By flow name within the parser session. `call_flow(name="approve_pr", inputs={...})` looks up `"approve_pr"` in the flow map. Not found → parse error (not runtime). Sub-flows must be loaded by the time the parent flow's parsing completes (parser does a final cross-flow resolution pass after collecting all flows from all loaded files).
- **D-17: Sandbox** — Single sandbox root passed to `NewParser`. `load()` cannot escape the root (`../../etc/passwd`-style traversal is rejected with a clear error). No multi-root or path-search support in v1.

### Lambda Capture Format

- **D-18: Lambda IDs** — Stable ID = `sha256(fileBytes)[:8] + ":" + line + ":" + col`. Stable across re-parse of the same file content; resilient to lambda reordering within a file (position changes IDs but a re-parse sees the same content). Phase 3's serialization decision (custom `DataConverter` vs. re-parse-on-start) keys off this format — the choice in Phase 3 must work with content-hash-prefixed IDs.
- **D-19: Free-variable strictness** — Allow free variables that resolve to **frozen module-level constants only**. The parser inspects every captured lambda's free vars at parse time:
  - If the free var resolves to a frozen module-level value (constant, function in the load module) → OK.
  - If the free var is a mutable closure variable → reject with `<file>:<line>:<col>: lambda captures mutable variable 'X'`.
- **D-20: Lambda-time predeclared globals** — Strict subset, locked in Phase 1, never expanded without explicit decision. The exact list:
  - `len`, `str`, `int`, `float`, `bool`, `list`, `dict`, `tuple`
  - `fail("reason")` — clean short-circuit; treated as a deterministic "failed lambda" outcome
  - All comparison operators, arithmetic, struct attribute access
  - Frozen-collection iteration helpers: `enumerate`, `zip`, `range`, `sorted`, `reversed`, `min`, `max`, `sum`, `any`, `all`, `abs`
  - **Forbidden:** `time.*`, `random.*`, any I/O, `print` is allowed (see D-21), no `getattr` with dynamic lookup, no `set()` (Starlark's `set` is off-by-default; keep it off), no `load()` (load only at parse time).
  - Implementation: define a single `lambdaTimeGlobals starlark.StringDict` constant in `pkg/bridge`; `bridge.CallLambda` always uses it. A test asserts the keys haven't changed since v1.
- **D-21: print() routing** — Starlark's `print(...)` inside a lambda routes via `thread.Print` to `workflow.GetLogger(ctx).Info` (Phase 3 wires the actual logger; Phase 1 establishes the bridge hook). Consultants are responsible for not logging secrets — this is a documented contract, not a framework guarantee. The credential-scrubbing middleware (Phase 2) does not run on `print` payloads in v1.
- **D-22: MaxExecutionSteps** — Default to `10_000_000` per lambda invocation; document the override mechanism (parser option). Cancellation watchdog (Phase 3) wires `workflow.Context.Done()` to `thread.Cancel` independently.

### Claude's Discretion

The following implementation choices are open — Claude picks during planning:

- Exact Starlark builtin names that pass through to lambda-time (D-20 lists the principles; the exact set is what `pkg/bridge` ships).
- Internal layout of the registration mechanism for `Idempotent` (D-12 says "required, no default" — Claude picks struct field vs. method).
- Sequence of parser passes (parse → lambda capture → flow registration → cross-flow resolution → lint).
- Test fixture organization under `tests/fixtures/` — directory structure and naming convention.
- Whether to use a single `Node` interface or distinct types with a sum-type-like `Kind` field on a base struct — pick whichever produces the cleanest interpreter switch in Phase 3.
- Specific reflection helper for `star:"..."` struct tags — Claude can use the existing `go.starlark.net/starlark` decoder if it suits, or write a small one.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project specs
- `.planning/PROJECT.md` — Vision, constraints, strict directives (no string compilation, no dynamic activities, no context bleed).
- `.planning/REQUIREMENTS.md` — All 55 v1 requirements with REQ-IDs. Phase 1 owns DSL-01..10, EXT-01..06, PARSE-01..06.
- `.planning/ROADMAP.md` — Phase 1 entry: goal, success criteria, requirements list.

### Research (Phase 1 directly informed by these)
- `.planning/research/SUMMARY.md` — §"Phase 1" and §"Architecture Approach" describe the build order and component boundaries this phase implements.
- `.planning/research/STACK.md` — Go 1.25, `go.starlark.net@latest`, slog, golangci-lint with forbidigo. Locks the dependency baseline.
- `.planning/research/ARCHITECTURE.md` — Component boundaries (`pkg/dag`, `pkg/extension`, `pkg/parser`, `pkg/bridge`), data flow Starlark→DAG, and the hard firewall rules between the three subsystems.
- `.planning/research/PITFALLS.md` — §1 (thread reuse), §2 (mutable captures), §6 (freeze audit), §7 (error attribution), §10 (static/runtime parser unity). All must be avoided in this phase's implementation.

### External references (no docs in repo yet — these are the upstream sources)
- `go.starlark.net` package docs — `*starlark.Function`, `*starlark.Thread`, `MaxExecutionSteps`, `Cancel`, `Freeze`, `starlarkstruct.Struct`. Linked in `STACK.md` Sources.
- `go.starlark.net/doc/spec.md` — Starlark language definition, freeze rules, closure semantics. Linked in `ARCHITECTURE.md` Sources.
- `go.starlark.net/doc/impl.md` — Freeze cascade through closure free vars. Linked in `PITFALLS.md` Sources.
- `cadence-workflow/starlark-worker` (GitHub) — Closest prior art for Starlark-as-DSL on a Temporal-like orchestrator; package layout reference. Linked in `ARCHITECTURE.md`.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **None yet** — this is a greenfield repository. Phase 1 establishes the patterns every later phase reuses.

### Established Patterns
- Go module layout, error design, test layout, and logging interface are decided in this phase (D-01..D-06) and locked for downstream phases.

### Integration Points
- `pkg/dag` types are the wire format between this phase's parser and Phase 3's interpreter; freezing them is one of the most consequential acts of Phase 1.
- `pkg/extension` interfaces are the contract Phase 6's example extensions implement.
- `pkg/bridge.CallLambda` is the function Phase 3 calls inside the workflow to evaluate every lambda.

</code_context>

<specifics>
## Specific Ideas

- The user's stated authoring example: `gh = github.endpoint("admin")` — a Starlark expression that returns a credential-aware extension instance, then `gh.create_issue(repo=..., title=...)` produces an `ActionRef` carrying `CredentialID="admin"`. This pattern is the model the extension SDK is built around.
- Example credential handler implementation in the example project (Phase 6) reads `$HOME/.skytime.conf` (or similar). Phase 1 only ensures the interface supports this — Phase 6 implements the file-based reader.
- "Packed Starlark libraries" — the user mentioned a future capability to load distributable bundles of `.star` files (analogous to Go modules or Python packages). Not in v1 scope; deferred. Phase 1 should NOT design `load()` in a way that closes the door — the `(rootdir, relative path)` model is compatible with future bundle loading.

</specifics>

<deferred>
## Deferred Ideas

- **Packed Starlark libraries** — distributable bundles of `.star` files loadable by name (`load("@skytime-ops//github:reviews.star", "ensure_review")`). Out of scope for v1; revisit when a real customer asks. Phase 1's `load()` design (rootdir + relative + absolute) leaves room.
- **Hot-reload of `.star` files** — already in PROJECT.md "Out of Scope" for v1; door open because `parser.Parse` is a pure function of file contents.
- **Schema export** to JSON Schema or markdown for extension docs — listed as v2 in REQUIREMENTS.md (`OPS-V2`); Phase 1's reflection-based kwarg schema (D-11) is the foundation that makes this trivial later.
- **Tier-2 unit tests for `def` blocks** — listed as v2 (`TEST-V2-01`); Phase 1's restricted lambda environment (D-20) is the same restricted environment Tier-2 will reuse.

</deferred>

---

*Phase: 01-type-spine-extension-contract-parser-bridge-foundations*
*Context gathered: 2026-04-26*
