# Skytime

## What This Is

Skytime is a Go library that lets teams declare durable workflows in Starlark and execute them on Temporal. The core insight: split workflow authoring into two tiers — library developers write Go *extensions* (typed I/O wrappers, reusable across customers), while consultant/integrator teams compose those extensions in `.star` files specialized per customer. The boundary between Starlark (parse-time, deterministic graph generation) and Temporal (execution-time, durable orchestration) is absolute and architectural — no string compilation, no dynamic activities, no context bleed.

## Core Value

A consultant team can take an extension catalog and a customer brief, write a `.star` file, and have a production-grade durable workflow running on Temporal — without touching Go and without giving up Temporal's retry/timeout/child-workflow guarantees.

## Requirements

### Validated

- ✓ Starlark DSL with naked primitives (`flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow`) backed by Go AST nodes — Phase 1
- ✓ Extension SDK contract — `Extension.Initialize` returns a `*starlarkstruct.Module` once per parser at Register time; ops return `ActionRef` intents; no path to import `go.temporal.io/sdk/activity` — Phase 1
- ✓ Starlark execution bridge with dot-notation state access (`ctx.req.repo_name`) via recursive `*starlarkstruct.Struct` conversion with deterministic key order — Phase 1
- ✓ Two-environment split — locked 20-key `lambdaTimeGlobals` strict subset + `fail()`, distinct from richer parse-time globals — Phase 1
- ✓ Lambda capture with content-hash IDs (`sha256(fileBytes)[:8] + ":" + line + ":" + col`) and free-variable lint that rejects mutable closures — Phase 1
- ✓ Sealed `Credential` interface with `BearerCredential`, `BasicCredential`, `APIKeyCredential` kinds and redacted `String()`; pluggable `CredentialHandler` contract laid (worker wiring deferred to Phase 3) — Phase 1
- ✓ Position-aware error spine (`ParseError`, `ValidationError`) formatted `<file>:<line>:<col>: <msg>`; parser never panics on malformed input — Phase 1
- ✓ `load()` resolution with relative + absolute paths, `.git`-ancestor root walk, traversal rejection — Phase 1
- ✓ Generic Temporal activity (`ExecuteBatch`) dispatches all extension I/O — Phase 2
- ✓ Block-batched I/O: idempotent batches share one activity invocation; mixed batches rejected at parse time (Policy D) — Phase 2
- ✓ Just-in-time credential resolution inside the activity with per-worker TTL cache + retry-aware bypass — Phase 2
- ✓ Type-level secret protection via `Secret` wrapper (`String`/`GoString`/`MarshalJSON`/`MarshalText`/`Format` redact; explicit `.Reveal()` for unwrap) — Phase 2 (replaced original regex-scrubber design)
- ✓ Architectural firewall: only `pkg/activity` may import `go.temporal.io/sdk/...`; enforced by AST-walking firewall test + inversion-bug meta-test — Phase 2

### Active

- [ ] Generic Temporal interpreter (`SkytimeWorkflow`) that walks the DAG, evaluates lambdas natively, and dispatches via the Phase 2 activity
- [ ] Lambda serialization mechanism (custom `DataConverter` vs. re-parse-on-start) — phase-blocking decision in Phase 3
- [ ] Worker bootstrap supporting Temporal Cloud / self-hosted-mTLS / dev-server connection variants
- [ ] Static validation tier — `skytime validate` shares the parser with the runtime via differential corpus testing
- [ ] Starlark E2E testing tier — `temporal_test` builtin that bridges Temporal `testsuite` mocks back to Starlark lambdas, with `attempt` count for retry simulation
- [ ] CLI for triggering and inspecting flows during development
- [ ] Example project with HTTP + GitHub + Slack extensions exercising every primitive (retries, credentials, parallel for-each, child workflow)
- [ ] Compatibility with Temporal Cloud and self-hosted Temporal clusters (BYO cluster, plus dev-server helper for examples)

### Out of Scope

- **Hot-reload of `.star` files** — design must not preclude it, but no implementation in v1
- **Plugin / gRPC / out-of-process extensions** — only static or dynamic-local Go extensions in v1
- **Web UI / dashboard** — Temporal's UI is sufficient for v1 visibility
- **Multi-tenant hosted SaaS** — Skytime is a library; productizing it as a service is a separate decision
- **CEL or string-based expressions** — explicitly rejected; lambdas only
- **Starlark unit-test tier** (Tier 2 in spec) — deferred; Static (Tier 1) and Starlark E2E (Tier 3) ship in v1, pure-Starlark unit testing of `def` blocks moves to v2
- **Workflow versioning helpers** — Temporal patching primitives are available to advanced users, but no Skytime-specific versioning API in v1

## Context

- **Tech stack (decided):** Go, `go.starlark.net` for the DSL, `go.temporal.io/sdk` for orchestration, `go.temporal.io/sdk/testsuite` for E2E testing.
- **Two-tier authoring model:** Library/extension developers (Go) and workflow authors ("consultants" who specialize per customer in Starlark). The DSL must be powerful enough that consultants don't drop down to Go for normal customer specialization, and safe enough that Starlark code can't break the host.
- **Architectural separation is non-negotiable:** Parse phase generates a deterministic DAG with no I/O; execution phase walks the DAG inside Temporal. Lambdas captured at parse time are evaluated inside the workflow with state injected as nested structs. This split is the project's whole reason to exist.
- **Strict directives from the spec:**
  - No string compilation (no CEL, no string parsers for conditionals/data mapping) — only native Starlark lambdas.
  - No dynamic activities — extensions are plain Go functions; they never import `go.temporal.io/sdk/activity`.
  - No context bleed — never pass `workflow.Context` into a Starlark thread, never pass a Starlark `*starlark.Thread` over the network into an activity.
- **Distribution shape:** Go library. The example project (with extensions and CLI) is the dogfooding vehicle and the proof-of-life demo, not a separate product.
- **Greenfield:** No existing code, no migration burden. The repository starts empty.

## Constraints

- **Tech stack**: Go + Starlark + Temporal — fixed. No alternative DSLs, expression languages, or orchestrators in scope.
- **Architecture**: Strict parse/execute separation. — Required for the safety properties (no I/O at parse, no Go escape hatch at execute, no context bleed).
- **Quality**: Quality > speed. — This is foundational infrastructure; correct boundaries are hard to fix retroactively.
- **Determinism**: The parsed DAG must be deterministic. — Temporal replay requires that workflow code (and the lambdas embedded in the DAG) produce the same decisions on replay.
- **Security**: Credentials never enter workflow state. — Resolver is invoked just-in-time inside the activity; state holds only credential IDs.
- **Compatibility**: Temporal Cloud and self-hosted servers must both work. — No reliance on cloud-only or self-hosted-only features.

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Starlark over CEL or custom DSL | Lambdas + struct injection give expressive data access without a string-parsing risk surface | — Pending |
| Single generic Temporal activity | Avoids per-extension activity registration and lets us batch I/O blocks to control history size | — Pending |
| Extensions return `ActionRef` intents (Command Pattern) | Keeps the parse phase pure and lets the interpreter route, batch, and mock I/O from one place | — Pending |
| Tier 2 (Starlark unit tests) deferred to v2 | Static + E2E cover the common cases; Tier 2 mainly helps offline `def`-block testing, lower priority for v1 | — Pending |
| Library + example project + CLI (no standalone binary) | Library is the primary product; CLI/example exist to drive development and demos | — Pending |
| Static or dynamic-local Go extensions only in v1 | Plugins/gRPC add complexity that isn't justified before we have one real customer | — Pending |
| Hot-reload deferred but not precluded | Useful eventually; designing the parser as a pure function of file contents leaves the door open | ✓ Good — `Parser.Parse` is a pure function of file contents (Phase 1) |
| `Extension.Initialize` returns a `*starlarkstruct.Module` once per parser at Register time | The user authoring example `gh = github.endpoint("admin")` requires `github` to be a namespace with attributes, not a callable. Resolved via the module-attribute pattern | ✓ Good — verified end-to-end in Phase 1 (Plan 01-05 fixture 07) |
| Lambda IDs use `sha256(fileBytes)` prefix, not canonicalized AST | Cosmetic edits (whitespace, comments) intentionally invalidate IDs; simpler than canonicalization and acceptable for the v1 use case | — Pending — Phase 3 must work with this ID format when picking a serialization strategy |
| Idempotent declaration is a `*bool` field with nil-check at registration (D-12) | Forces extension authors to make a conscious choice; nil = registration error | ✓ Good — verified by `errors.Is(err, ErrIdempotentRequired)` test in Phase 1 |
| Mixed-idempotency blocks rejected at parse time (Policy D, Phase 2) | "Make wrong things impossible" — keeps activity dumb and homogeneous; consultant gets a friendly fix-suggestion error at parse time instead of a runtime surprise | ✓ Good — verified by `TestLinter_MixedIdempotency_*` and defensive activity-side test |
| Type-level secret protection via `Secret` wrapper (no regex scrubber, ACT-05 amended) | Java-style `toString` redaction via Go `Stringer` + Format/MarshalJSON; `.Reveal()` is greppable for audit. Regex deferred — additive in v1.x if a customer incident proves it needed | — Pending — first real customer with a leaky third-party SDK will tell us if regex is needed |
| Per-worker credential cache with retry-aware bypass | 5-min TTL for happy path; `activity.GetInfo(ctx).Attempt > 1` invalidates batch's IDs to handle token rotation cleanly | ✓ Good — `TestExecuteBatch_RetryAttempt_BypassesCache` verifies |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd:transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd:complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
*Last updated: 2026-04-28 after Phase 2 completion*
