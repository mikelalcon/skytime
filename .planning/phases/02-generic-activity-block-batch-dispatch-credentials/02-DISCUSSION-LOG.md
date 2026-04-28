# Phase 2: Generic Activity + Block-Batch Dispatch + Credentials - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-27
**Phase:** 02-generic-activity-block-batch-dispatch-credentials
**Areas discussed:** ActionResult shape & location, Mixed-batch policy, Credential cache & retry semantics, Error-scrubbing strategy

---

## ActionResult Shape & Location

### Result type

| Option | Description | Selected |
|--------|-------------|----------|
| Sealed sum interface | Mirrors Phase 1 Credential pattern. ActionResult interface with concrete types: OkResult, RetryableErrResult, NonRetryableErrResult, SkippedResult (Recommended) | ✓ |
| Status enum + payload struct | ActionResult struct with Status string + Output any + Err error | |
| Result + error tuple | Two parallel slices: outputs + errors with sentinel ErrSkipped | |

**User's choice:** Sealed sum interface
**Notes:** Type-switch in interpreter; new variants are non-breaking.

### Location

| Option | Description | Selected |
|--------|-------------|----------|
| pkg/dag (Recommended) | Same package as ActionRef. Phase 3 interpreter imports pkg/dag anyway | ✓ |
| pkg/activity | Lives with its consumer | |
| pkg/dispatch | Backend-agnostic name | |

**User's choice:** pkg/dag
**Notes:** No extra package to depend on; pure data, no Temporal.

### Output type

| Option | Description | Selected |
|--------|-------------|----------|
| any (interface{}) (Recommended) | Operations return arbitrary Go types | |
| JSON-encoded []byte | Force ops to JSON-encode | |
| Typed via generics | ActionResult[T any] | |

**User's choice:** Free text — "I think we want any? but that it is serializable in json too?"
**Notes:** Refined into D2-03/D2-04 — sealed `OperationOutput` marker interface.

### Output enforcement

| Option | Description | Selected |
|--------|-------------|----------|
| Sealed OperationOutput interface (refined from "type-tag") | Compile-time check ops return declared output types | ✓ |
| Documented + runtime json.Marshal check | any with runtime safety net | |
| Both — sealed + runtime check | Belt-and-suspenders | |

**User's choice:** Sealed OperationOutput interface
**Notes:** More boilerplate per op, much stronger contract for interpreter. OperationFunc signature narrows from `output any` to `output OperationOutput` (D2-04).

### Skipped variant emission

**User asked for deeper explanation before deciding.** Orchestrator explained three policies (interpreter splits, activity splits with Skipped, activity rejects mixed). User then proposed a fourth: parse-time reject of mixed blocks (Policy D).

---

## Mixed-Batch Policy

### Splitting policy

| Option | Description | Selected |
|--------|-------------|----------|
| Interpreter splits (Policy A) | Phase 3 interpreter groups idempotent ops, spawns non-idempotent solo. Activity always homogeneous | |
| Activity splits with Skipped (Policy B) | Activity emits Skipped for non-idempotent indices; interpreter does follow-ups | |
| Activity rejects mixed batches (Policy C) | Defensive redundancy with NonRetryableErr | |

**User's choice:** Free text proposing Policy D — "not allow mixing idempotent and non-idempotent in the same block. Force user to be explicit."
**Notes:** Orchestrator analyzed pros/cons and recommended Policy D as best fit for Skytime's "make wrong things impossible" design philosophy.

### Policy D confirmation

| Option | Description | Selected |
|--------|-------------|----------|
| Yes, parse-time reject (Recommended) | Parser linter emits ValidationError on mixed block | ✓ |
| Yes, parse-time reject — and drop SkippedResult | Same plus eliminate variant | |
| No, fall back to Policy A | Allow mixed; interpreter splits transparently | |

**User's choice:** Yes, parse-time reject (Recommended)
**Notes:** Phase 1's parser linter gets a new pass; activity defensively rejects too (defense in depth, but should be unreachable). SkippedResult variant defined but unused in v1.

---

## Credential Cache & Retry Semantics

### Activity cache (per-invocation)

| Option | Description | Selected |
|--------|-------------|----------|
| Yes, per-invocation cache (Recommended) | Within one ExecuteBatch call, dedupe Resolve | |
| Yes, per-worker cache with TTL | Cache lives across activities on same worker | ✓ |
| No — resolve every action | Each action calls Resolve independently | |

**User's choice:** Per-worker cache with TTL
**Notes:** Default TTL: 5 minutes (configurable). Cache is process-local. Reconciled with retry semantics below.

### Retry resolve

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — retry = fresh activity = fresh resolution (Recommended) | Each retry starts brand-new invocation; per-invocation cache empty | ✓ |
| No — worker-level cache survives across retries | Faster but stale tokens cause confusing failures | |

**User's choice:** Yes — fresh resolution on retry
**Notes:** Reconciled with per-worker TTL cache via D2-11: when `activity.GetInfo(ctx).Attempt > 1`, cache is invalidated for the batch's credential IDs and re-resolved. Cache for happy path; bypass on retry.

### Batch retry semantics

| Option | Description | Selected |
|--------|-------------|----------|
| Return error — Temporal retries whole batch (Recommended) | Standard Temporal pattern; safe by Policy D (idempotent batches only) | ✓ |
| Return all results, interpreter retries individual actions | Saves duplicate work but complicates interpreter; breaks Temporal idiom | |
| Hybrid retry hint | Confusing | |

**User's choice:** Return error — Temporal retries whole batch
**Notes:** Whole batch re-executes; safe because Policy D guarantees only idempotent batches get retried.

### Resolve failure classification

| Option | Description | Selected |
|--------|-------------|----------|
| NonRetryable on unknown ID, Retryable on transient (Recommended) | Typed errors classify | ✓ |
| Always retryable | Causes infinite loops on misconfig | |
| Always non-retryable | Strict; transient failures kill workflows | |

**User's choice:** NonRetryable on unknown ID, Retryable on transient
**Notes:** Convention: handler authors return `errors.Is(err, extension.ErrUnknownCredential)` for unknown IDs. Default = retryable.

---

## Error-Scrubbing Strategy

### Default patterns

| Option | Description | Selected |
|--------|-------------|----------|
| Bearer + API keys + basic-auth-in-URL (Recommended) | Common token leakage paths | ✓ |
| Strict allowlist of known-safe shapes | Most secure, worst DX | |
| Token-shape detection (entropy + length) | Catches more, false positives | |

**User's choice:** Bearer + API keys + basic-auth-in-URL
**Notes:** Answer captured but later superseded — see "Defense strategy" below where the user opted to drop the regex layer entirely.

### Extensibility, scope, fallback (Q2-Q4)

User paused here and asked: "what is a scrubber?" Orchestrator explained scrubber concept (regex-based last-line-of-defense for secrets that escaped type protection through third-party library logging or error wrapping).

User then noted Java's pattern of overriding toString() at the type level and asked if Go has equivalent.

Orchestrator responded:
1. Yes, Go's `Stringer` interface (`String() string`) — Phase 1 already implemented redacted `String()` on every Credential kind.
2. The protection breaks once the op extracts the raw bytes (which it must, for HTTP headers).
3. Three positions presented:
   - A: Drop scrubber entirely; rely on Phase 1's String() only.
   - B: All three layers — String() + Secret wrapper + regex scrubber (defense in depth).
   - C: String() + Secret wrapper, no regex.

### Defense strategy

| Option | Description | Selected |
|--------|-------------|----------|
| B — String() + Secret wrapper + regex scrubber (Recommended) | All three layers; most paranoid; most code | |
| C — String() + Secret wrapper, no regex | Type-level only with Reveal() pattern | ✓ |
| A — Status quo (Phase 1 only) | Smallest Phase 2; most reliant on op authors | |

**User's choice:** C — String() + Secret wrapper, no regex
**Notes:** Lock the type system at the Credential level. Introduce `Secret` wrapper type with redacted `String()`/`GoString()`/`MarshalJSON()` and explicit `.Reveal()` to extract raw bytes. ACT-05 amended in REQUIREMENTS.md to drop the regex mandate. Q2 (extensibility), Q3 (scope), Q4 (empty fallback) all moot — no scrubber to extend, scope, or fallback. Re-evaluate scrubber after first real customer incident.

---

## Claude's Discretion

- Exact API of `Secret` constructor and accessor (`.Reveal()` vs `.Get()` vs `.Unwrap()`).
- Internal data structure for the credential cache (`sync.Map` vs `sync.RWMutex` + plain map).
- Test fixture for "fake credential handler" — file-based, env-based, or in-memory.
- Specific metrics/observability hooks (none required; debug-level slog OK).
- Whether `SkippedResult` is exposed in the public package (export it).

## Deferred Ideas

- Regex error scrubber — defer; re-evaluate after first incident.
- Per-extension scrubbers via `OperationSpec.Scrubbers` — deferred with the regex layer.
- Time-based heartbeating (long-running single ops) — v1 heartbeats only between actions.
- `SkippedResult` emission — variant defined but unused in v1.
- `OperationOutput` schema export (JSON Schema, markdown) — `OPS-V2-05` in REQUIREMENTS.md.
- Cross-worker credential cache — per-worker only in v1.
- `Secret`-aware linter for `.Reveal()` audit — useful, post-v1.
- Mixed-batch override flag (`allow_mixed=true`) — NOT supported in v1.
