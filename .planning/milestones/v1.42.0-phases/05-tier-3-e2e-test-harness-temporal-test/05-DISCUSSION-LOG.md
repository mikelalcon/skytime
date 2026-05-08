# Phase 5: Tier-3 E2E Test Harness (`temporal_test`) - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-05
**Phase:** 05-tier-3-e2e-test-harness-temporal-test
**Areas discussed:** Test file shape & discovery, Mock binding & match precedence, Mock function I/O contract, Replay determinism check, Runner UX, assert.* surfacing, Interpolation in tests

---

## A. Test File Shape & Discovery

### Q1: How does a .star test file declare its tests?

| Option | Description | Selected |
|--------|-------------|----------|
| def test_xxx() functions | Go-style. Discovered like Go's TestXxx. | ✓ |
| Top-level tester.run() calls | Each top-level call is one test. | |
| Single tester.suite([test_a, test_b]) | Explicit suite registration. | |

**User's choice:** def test_xxx() functions
**Notes:** Most aligned with Go ergonomics; per-test isolation; clean multi-test files.

### Q2: File-naming convention for discovery?

| Option | Description | Selected |
|--------|-------------|----------|
| *_test.star | Go-style suffix. | ✓ |
| test_*.star | Pytest-style prefix. | |
| Every .star in <dir> is a test | Most permissive. | |

**User's choice:** *_test.star
**Notes:** Mirrors Go convention already used in the codebase. Lets prod + test files coexist.

### Q3: How does tester.workflow() pass init_state, retry policy, timeouts?

| Option | Description | Selected |
|--------|-------------|----------|
| All kwargs on tester.workflow(...) | Single declaration site. | ✓ |
| All kwargs on tester.run(...) | Per-test config. | |
| Two-step: workflow declares, run configures | Maximum flexibility. | |

**User's choice:** All kwargs on tester.workflow(name=, init_state=, retry_policy=, timeouts=)
**Notes:** tester.run(flow=name) just executes. Per-test variation via re-declaring tester.workflow inside def test_*.

### Q4: Multi-test file — mock isolation or sharing?

| Option | Description | Selected |
|--------|-------------|----------|
| File-level defaults + per-test override | Top-level apply to all; per-test shadows. | ✓ |
| Per-test only | Strict isolation. | |
| File-level only | All tests share, no override. | |

**User's choice:** File-level defaults + per-test override
**Notes:** Mirrors Go-test fixture ergonomics; DRY with override safety net.

---

## B. Mock Binding & Match Precedence

### Q1: What does tester.mock_action match against?

| Option | Description | Selected |
|--------|-------------|----------|
| (extension, op) only | All calls to gh.get use this mock. | |
| (extension, op) + optional kwargs subset | Kwargs subset narrows. | ✓ |
| Per-step name | Bind to step.name. | |

**User's choice:** (extension, op) + optional kwargs subset match
**Notes:** Multiple mocks for same op disambiguated by kwargs match (regex). Precedence rules needed.

### Q2: When a flow calls an action with NO registered mock, what happens?

| Option | Description | Selected |
|--------|-------------|----------|
| Fail fast — NonRetryableErr | Explicit > implicit. | ✓ |
| Auto pass-through to real | Real I/O for un-mocked. | |
| Auto-generate ok(output=None) | Silent default. | |

**User's choice:** Fail fast — NonRetryableErr "no mock for gh.delete at ..."

### Q3: Wildcard / catch-all mocks?

| Option | Description | Selected |
|--------|-------------|----------|
| No wildcards | Must be exact (extension, op). | |
| op="*" catch-all per extension | Wildcard support. | ✓ |
| Explicit tester.default_mock | One named escape hatch. | |

**User's choice:** op="*" catch-all per extension
**Notes:** No cross-extension wildcard for v1. Precedence model needed alongside D5-B1.

### Q4: Ordering when file-level and per-test mocks both match?

| Option | Description | Selected |
|--------|-------------|----------|
| Most-recently-registered wins | Per-test shadows file-level. | ✓ |
| First-registered wins | File-level locks. | |
| Error on duplicate | Force explicit unmock. | |

**User's choice:** Most-recently-registered wins (per-test shadows file-level)

### Q5 (follow-up): Mock precedence — when multiple mocks could match a call, which wins?

| Option | Description | Selected |
|--------|-------------|----------|
| Specificity ladder; recency breaks ties | T1 > T2 > T3; recency within tier. | ✓ |
| Pure recency — last-registered always | Less-specific can shadow more-specific. | |
| Specificity only — same-tier ambiguity errors | Force unmock between. | |

**User's choice:** Specificity ladder; recency breaks ties
**Notes:** Tier 1 = (ext, op) + match={...}; Tier 2 = (ext, op); Tier 3 = (ext, "*"). Most specific tier always wins; recency breaks ties within a tier (so per-test override of file-level (gh, get) works).

### Q6 (follow-up): Kwargs match semantics — match={path: "/users/octocat"} compared to actual call?

| Option | Description | Selected |
|--------|-------------|----------|
| Exact equality on listed kwargs | Subset; exact match per key. | |
| Glob/wildcard support | * pattern in match values. | |
| Regex | Most expressive. | ✓ |

**User's choice:** Regex — match={path: "^/users/[a-z]+$"}
**Notes:** Go `regexp` syntax; partial-match by default unless anchored with ^...$. Document this footgun.

---

## C. Mock Function I/O Contract

### Q1: Mock lambda signature?

| Option | Description | Selected |
|--------|-------------|----------|
| lambda kwargs, attempt | Two positional args. | ✓ |
| lambda kwargs, attempt, credential | Includes credential value. | |
| lambda ctx, kwargs, attempt | Includes full ctx. | |

**User's choice:** lambda kwargs, attempt: ...
**Notes:** Credential ID exposed via kwargs["_credential_id"] if needed; raw Secret never passed. ctx omitted because kwargs already resolved from ctx.

### Q2: Return value shape?

| Option | Description | Selected |
|--------|-------------|----------|
| Typed builders ok/err/nonretryable | Mirrors dag.ActionResult. | ✓ |
| Plain value = ok; raise fail() = nonretryable | No retryable path. | |
| Plain dict {ok: True, ...} | No new builtins. | |

**User's choice:** Typed builders: ok(value=...) / err(msg=...) / nonretryable(msg=...)
**Notes:** New mock-lambda env extends lambdaTimeGlobals with these three builders + receives `attempt`. Production lambda env unchanged.

### Q3: Typed-Output mapping — Starlark dict to OkResult.Output?

| Option | Description | Selected |
|--------|-------------|----------|
| Bridge dict → *starlarkstruct.Struct | Reuses Phase 1 bridge. | ✓ |
| JSON round-trip to extension's typed Output | Validates schema. | |
| Raw any | Zero conversion. | |

**User's choice:** Bridge-style conversion: Starlark dict → *starlarkstruct.Struct
**Notes:** Symmetry with prod path. Confirm bridge.StructFromDict export in pkg/bridge.

### Q4: None / no return semantics?

| Option | Description | Selected |
|--------|-------------|----------|
| Error: must return ok/err/nonretryable | Explicit. | ✓ |
| Implicit ok(value=None) | Convenient. | |
| Implicit ok(value={}) | Empty struct fallback. | |

**User's choice:** Error: "mock must return ok/err/nonretryable"
**Notes:** None is almost always a forgotten return statement.

---

## D. Replay Determinism Check

### Q1: Replay execution model — always-on or opt-in?

| Option | Description | Selected |
|--------|-------------|----------|
| Always-on | Every tester.run replays twice. | ✓ |
| Opt-in via replay=True | Default off. | |
| Per-suite runner flag | Centralized control. | |

**User's choice:** Always-on
**Notes:** TEST-04 explicitly mandates the check; opt-in defeats Tier-3's purpose.

### Q2: Divergence report shape?

| Option | Description | Selected |
|--------|-------------|----------|
| First-divergent event diff with payload | Focused, actionable. | ✓ |
| Full event-list diff | Exhaustive, noisy. | |
| Just "diverged at event N" | Minimal. | |

**User's choice:** First-divergent event diff (event index + before/after struct dump)

### Q3: Starlark callsite attribution?

| Option | Description | Selected |
|--------|-------------|----------|
| Originating step() in flow .star | Points at suspect action. | ✓ |
| The mock_fn lambda | Points at test. | |
| The tester.run() callsite | Most generic. | |

**User's choice:** The originating step() callsite in the flow .star file

### Q4: Diff scope?

| Option | Description | Selected |
|--------|-------------|----------|
| Event types + sequence + payload byte-equality | Mirrors Temporal SDK. | ✓ |
| Event types + sequence only | Skips payload. | |
| Events + payload + final state | Strictest, redundant. | |

**User's choice:** Temporal event types + sequence + payload byte-equality
**Notes:** Catches the Phase 04.2 result_bound payload-divergence bug class.

---

## E. Runner UX (`skytime test`)

### Q1: skytime test output format on TTY?

| Option | Description | Selected |
|--------|-------------|----------|
| Static line-per-test, Go-test style | Familiar; CI-readable. | ✓ |
| Reuse Phase 4 live progress block | Multi-line redrawing. | |
| Compact summary; --verbose for detail | Hides per-test feedback. | |

**User's choice:** Go-test style (with explicit emphasis on CI integration ease)
**Notes:** User wants integration with other testing tools — drove the follow-up Q5 below.

### Q2: Test selection flag?

| Option | Description | Selected |
|--------|-------------|----------|
| --run <regex> (Go-style) | Familiar. | ✓ |
| Pytest-style file::test_name | Explicit per-test address. | |
| Two-flag: --test test_name | Verbose. | |

**User's choice:** skytime test --run <regex>

### Q3 (was Q3 of Area F batch): assert.* failure routing?

| Option | Description | Selected |
|--------|-------------|----------|
| One sub-*testing.T per def test_* via t.Run | Per-test isolation. | ✓ |
| One *testing.T per file | Coarser. | |
| Accumulate without isolation | Loses callsite-per-test. | |

**User's choice:** One sub-*testing.T per def test_* via t.Run
**Notes:** Mirrors Go t.Run subtest pattern; gives JSON output (D5-E2) per-test granularity.

### Q4: ${ctx.expr} interpolation in test .star files?

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — same parser, same desugaring | Zero new code. | ✓ |
| No — restricted to literals | Special-cases test files. | |
| Yes for kwargs, not for mock_fn body | Hybrid. | |

**User's choice:** Yes — same parser, same desugaring; init_state binds via ctx
**Notes:** Tests go through the same parser as prod .star files.

### Q5 (follow-up): CI integration format?

| Option | Description | Selected |
|--------|-------------|----------|
| Go-test style + --format=json (go test -json mirror) | Universal CI compatibility. | ✓ |
| Go-test style + --junit XML | Universal but XML. | |
| Both --format=json + --junit | Maximum flexibility. | |
| Plain Go-test style only | Third-party converter. | |

**User's choice:** Default Go-test style + --format=json flag (Go-test -json mirror)
**Notes:** Mirrors `go test -json` schema exactly; works with gotestsum, tparse, GitHub Actions, etc.

---

## Claude's Discretion

- Internal layout of `pkg/testing` (file structure, helper names) — within `pkg/parser` / `pkg/interpreter` conventions.
- Whether `bridge.StructFromDict` or analog needs to be newly exported through `pkg/bridge`.
- Internal storage shape of mock registry (stack of dicts? layered map?) — must satisfy D5-A4 + D5-B4 invariants.
- `assert.*` multi-failure accumulation vs. fail-fast within a single test (default to library behavior; D5-F2).
- Cross-file test parallelization in `skytime test` (D5-E5; sequential within a file is fixed).

## Deferred Ideas

- Cross-extension wildcards (`extension="*"`) — re-evaluate if Phase 6 demands it
- Per-kwarg type matching beyond strings — match={...} restricted to strings in v1
- JUnit XML output — JSON only in v1; JUnit via gotestsum
- Snapshot testing — out of TEST-01..05
- Fixture frameworks (pytest-style @fixture) — YAGNI for v1
- Mock-call assertion (assert_called) — consultants use assert.eq instead
- Per-test --debug flag — runner-level --debug (D4-19) handles this
- tester.replay() standalone builtin — always-on per D5-D1
- Live progress block for tests — static line-per-test is correct (D5-E1)
