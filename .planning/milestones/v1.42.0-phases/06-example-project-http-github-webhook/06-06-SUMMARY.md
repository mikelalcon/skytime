---
phase: 06-example-project-http-github-webhook
plan: 06
subsystem: example-project
tags: [starlark, dsl, parser, dag, github, webhook, examples, coverage-matrix]

requires:
  - phase: 06-example-project-http-github-webhook (06-03)
    provides: GitHub extension factory + 7 typed ops (get_repo, get_issue, list_open_issues, add_comment, add_label, list_prs, list_recent_merged_prs)
  - phase: 06-example-project-http-github-webhook (06-04)
    provides: Webhook extension factory + non-idempotent post op
  - phase: 04 (04-07)
    provides: Baked-in HTTP extension (pkg/extension/builtin/http) used as the third registered extension
  - phase: 04.1
    provides: action_fn / block_fn lambda forms; ${ctx.x} interpolation in step names + action kwargs

provides:
  - 5 .star flow files exercising every DSL primitive across the example
  - examples/http-github-webhook/flows_test.go with TestFlows_ParseAll + TestFlows_CoverageMatrix
  - Mechanically-pinned DAG-coverage matrix (drift fails CI)

affects:
  - 06-07 (Tier-3 issue_triage_test.star — builds on the issue_triage flow committed here)
  - 06-08 (README — embeds the coverage table whose values come from this test's expected map)

tech-stack:
  added: []
  patterns:
    - "Coverage-matrix mechanical pinning: Go test walks parsed DAG, builds primitive set per flow, asserts against an expected map keyed on flow name. Aggregate coverage assertion guarantees every primitive (step_seq, step_block, step_block_fn, if_cond, script, for_each_parallel, call_flow) is exercised somewhere in the corpus."
    - "Hermetic .star parse-test pattern: pure parser invocation against registered extensions, no Temporal env, no network. Mirrors tests/differential_test.go's static-pass shape but pins DAG shape rather than running interpreter."

key-files:
  created:
    - examples/http-github-webhook/public_repo_check.star
    - examples/http-github-webhook/pr_to_webhook.star
    - examples/http-github-webhook/issue_triage.star
    - examples/http-github-webhook/batch_label_issues.star
    - examples/http-github-webhook/weekly_digest.star
    - examples/http-github-webhook/flows_test.go
  modified: []

key-decisions:
  - "Step outputs are NOT auto-bound to ctx in v1 — confirmed by reading pkg/parser/state_schema.go. Only flow inputs, script.OutputAlias, if_cond.OutputAlias, and for_each.ItemVar populate the state schema. Flows seed iteration lists via prior script outputs (or flow inputs) rather than reading prior step outputs. Documented in each flow's header docstring."
  - "call_flow.inputs accept LITERAL data only per D-19 + parser starlarkLiteralToGo (verified at pkg/parser/builtins.go:1271). issue_triage passes literal placeholder values to triage_issue; v1 has no ${ctx.x} interpolation or ctx-expression support inside call_flow.inputs."
  - "Static block=[gh.add_comment(number=ctx.number, ...), ...] cannot resolve ctx.number at parse time. issue_triage's triage_issue subflow uses block_fn=lambda ctx: [gh.add_comment(...), gh.add_label(...)] inside the if_cond.then to support int-typed kwargs from the runtime context. Coverage map updated to include step_block_fn for triage_issue (was step_block in plan)."
  - "Plan's retry_policy={...} kwarg name is wrong — actual API is retry={...} with keys initial_interval (string e.g. '1s'), max_attempts, backoff_coefficient, non_retryable_errors per pkg/dag/retry.go. timeouts={...} is timeout={...} with start_to_close, schedule_to_start. max_in_flight is max_concurrency (verified at pkg/parser/builtins.go:1185). Flows use the correct kwarg names."

patterns-established:
  - "Per-flow primitive set as machine-checkable contract: future plans modifying any flow must update both the .star file AND the expected map in flows_test.go. Drift surfaces as a coverage assertion failure."
  - "Step→state binding pending future work: every flow's docstring (and a comment near the placeholder script) calls out that v1 does not auto-bind step outputs into ctx. Consultants reading the corpus see the boundary clearly; phase 7+ can address this without breaking the corpus."

requirements-completed:
  - EX-01
  - EX-02

duration: 1m
completed: 2026-05-07
---

# Phase 06 Plan 06: Five .star Flows + Coverage-Pinning Tests Summary

**Five flows covering every DSL primitive (sequential/block/block_fn/script/if_cond/for_each_parallel/call_flow) plus a Go test that mechanically pins the per-flow primitive set against the README's coverage matrix.**

## Performance

- **Duration:** ~1 min (3 atomic commits)
- **Started:** 2026-05-07T23:19:31Z
- **Completed:** 2026-05-07T23:20:23Z
- **Tasks:** 3
- **Files created:** 6

## Accomplishments

- **5 .star flows authored** matching CONTEXT.md D-FLOWS-LINEUP intent (with deviations noted below for items the v1 parser cannot accept verbatim).
- **flows_test.go** ships two hermetic Go tests:
  - `TestFlows_ParseAll`: every non-test .star file under examples/http-github-webhook/ parses cleanly against the example's HTTP + GitHub + Webhook extensions.
  - `TestFlows_CoverageMatrix`: each parsed flow's DAG primitive set matches the expected map; aggregate coverage spans every DSL primitive (step_seq, step_block, step_block_fn, if_cond, script, for_each_parallel, call_flow).
- **Coverage matrix mechanically pinned** — the README's coverage table (06-08) reads from this test's expected map; drift fails CI rather than silently going stale.

## Task Commits

Each task was committed atomically with `--no-verify` (parallel-executor mode):

1. **Task 1: Author 4 .star flows** — `1ffa7bc` (feat)
   - public_repo_check.star, pr_to_webhook.star, batch_label_issues.star, weekly_digest.star
2. **Task 2: Author issue_triage.star** — `6c2c889` (feat)
   - Two-flow file: triage_issue subflow + issue_triage top-level
3. **Task 3: flows_test.go** — `685746d` (test)
   - TestFlows_ParseAll + TestFlows_CoverageMatrix

## Files Created/Modified

- `examples/http-github-webhook/public_repo_check.star` (67 lines) — README headline-demo flow: sequential step + block + if_cond + script, no credentials required (uses unauthenticated GitHub public API).
- `examples/http-github-webhook/pr_to_webhook.star` (57 lines) — Authenticated fan-out: list_prs → script seed → for_each_parallel webhook.post → script summary. Demonstrates retries (max_attempts=3) and credential injection (github_token + webhook_url).
- `examples/http-github-webhook/issue_triage.star` (113 lines) — Two-flow file. triage_issue subflow uses if_cond + block_fn (non-idempotent batch). issue_triage top-level fans out via for_each_parallel + call_flow into the subflow per item. The deepest combinator nesting in the corpus.
- `examples/http-github-webhook/batch_label_issues.star` (46 lines) — block_fn dynamic batch building N gh.add_label ActionRefs at runtime; demonstrates retries + timeout + credentials.
- `examples/http-github-webhook/weekly_digest.star` (57 lines) — Aggregation pattern: list_recent_merged_prs → script grouping → for_each_parallel summarizing → final webhook.post. Demonstrates retries + credentials.
- `examples/http-github-webhook/flows_test.go` (247 lines) — Two hermetic parse + coverage tests. The expected map is the source-of-truth that drives the README coverage table (06-08).

## Per-Flow Detected Primitive Set

The `primitiveSet` walker in flows_test.go produces these sets at test time:

| Flow                | step_seq | step_block | step_block_fn | if_cond | script | for_each_parallel | call_flow |
|---------------------|:--------:|:----------:|:-------------:|:-------:|:------:|:-----------------:|:---------:|
| public_repo_check   |    ✓     |     ✓      |               |    ✓    |   ✓    |                   |           |
| pr_to_webhook       |    ✓     |            |               |         |   ✓    |         ✓         |           |
| issue_triage        |    ✓     |            |               |         |   ✓    |         ✓         |     ✓     |
| triage_issue        |    ✓     |            |       ✓       |    ✓    |   ✓    |                   |           |
| batch_label_issues  |    ✓     |            |       ✓       |         |   ✓    |                   |           |
| weekly_digest       |    ✓     |            |               |         |   ✓    |         ✓         |           |
| **Aggregate**       |    ✓     |     ✓      |       ✓       |    ✓    |   ✓    |         ✓         |     ✓     |

Aggregate row spans every DSL primitive — success criterion of D-FLOWS-COVERAGE-MATRIX met.

```
$ go test -v -count=1 ./examples/http-github-webhook/ -run TestFlows
=== RUN   TestFlows_ParseAll
=== RUN   TestFlows_ParseAll/batch_label_issues.star
=== RUN   TestFlows_ParseAll/issue_triage.star
=== RUN   TestFlows_ParseAll/pr_to_webhook.star
=== RUN   TestFlows_ParseAll/public_repo_check.star
=== RUN   TestFlows_ParseAll/weekly_digest.star
--- PASS: TestFlows_ParseAll (0.01s)
=== RUN   TestFlows_CoverageMatrix
=== RUN   TestFlows_CoverageMatrix/public_repo_check
=== RUN   TestFlows_CoverageMatrix/pr_to_webhook
=== RUN   TestFlows_CoverageMatrix/issue_triage
=== RUN   TestFlows_CoverageMatrix/triage_issue
=== RUN   TestFlows_CoverageMatrix/batch_label_issues
=== RUN   TestFlows_CoverageMatrix/weekly_digest
--- PASS: TestFlows_CoverageMatrix (0.01s)
PASS
ok  github.com/mikelalcon/skytime/examples/http-github-webhook  0.309s
```

## Decisions Made

- **Step output → state binding is NOT in v1.** Verified by reading `pkg/parser/state_schema.go::walkBodyForCtxValidation` — only `script.OutputAlias`, `if_cond.OutputAlias`, `for_each.ItemVar`, and `flow.Inputs` populate the state schema. Steps don't add anything. The plan's spec of `lambda ctx: ctx.get_repo.stars >= 100` (in public_repo_check Step 3) would fail D4-02 ctx validation. Worked around by sourcing predicates from flow inputs / prior script outputs instead. Each flow's header docstring documents this v1 limitation so consultants reading the corpus see the boundary clearly.

- **call_flow.inputs are LITERAL only.** Verified at `pkg/parser/builtins.go:1271 starlarkLiteralToGo`. v1 does not accept `${ctx.x}` interpolation or raw ctx-expression values inside `call_flow.inputs`. issue_triage's call_flow uses literal placeholder values; this matches the existing skeleton example's `inputs={"repo": "x"}` pattern.

- **Static block with int-typed kwargs requires block_fn.** triage_issue's if_cond.then originally specced as `step(block=[gh.add_comment(number=ctx.number, ...), gh.add_label(number=ctx.number, ...)])` — but `ctx.number` cannot be resolved at parse time inside a static block kwarg. Using `block_fn=lambda ctx: [...]` instead defers evaluation to runtime where `ctx.number` is available. Coverage map updated to include `step_block_fn` for `triage_issue` (was `step_block` in plan).

- **Plan kwarg names corrected to actual parser API.** Plan specified `retry_policy={"max_attempts":3, "initial_interval_seconds":1}` and `timeouts={"start_to_close_seconds":30}`. Actual API per `pkg/dag/retry.go` is `retry={"max_attempts":3, "initial_interval":"1s"}` (string Duration) and `timeout={"start_to_close":"30s"}`. Plan said `max_in_flight=4`; actual is `max_concurrency=4` (verified in `pkg/parser/builtins.go:1185`).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Plan kwarg names did not match parser API**
- **Found during:** Task 1
- **Issue:** Plan specified `retry_policy=...`, `timeouts=...`, `max_in_flight=...` — none of these are accepted by the parser. The actual kwargs are `retry`, `timeout`, `max_concurrency`. The dict-key shapes also differ (`initial_interval` is a Duration string like `"1s"`, not `initial_interval_seconds`).
- **Fix:** Used the correct kwarg names + dict shapes throughout all five flows.
- **Files modified:** All 5 .star files
- **Verification:** TestFlows_ParseAll exits 0 — every flow parses against the registered extensions.
- **Committed in:** 1ffa7bc, 6c2c889

**2. [Rule 1 - Bug] Step outputs cannot be referenced via ctx in v1**
- **Found during:** Task 1 (drafting public_repo_check.star)
- **Issue:** Plan's spec used `lambda ctx: ctx.get_repo.stars` and `lambda ctx: ctx.list_prs.pull_requests` — neither would parse, because steps do not auto-bind their outputs to the state schema (verified at pkg/parser/state_schema.go::walkBodyForCtxValidation).
- **Fix:** Each flow now seeds iteration lists / predicates from flow inputs or prior script outputs. Per-flow header docstring documents this v1 limitation.
- **Files modified:** public_repo_check.star, pr_to_webhook.star, batch_label_issues.star, weekly_digest.star
- **Verification:** TestFlows_CoverageMatrix asserts each flow's primitive set; primitives unchanged from D-FLOWS-COVERAGE-MATRIX.
- **Committed in:** 1ffa7bc

**3. [Rule 1 - Bug] Static block with ctx-expression kwargs cannot parse**
- **Found during:** Task 2 (drafting issue_triage.star triage_issue subflow)
- **Issue:** Plan specced `step(block=[gh.add_comment(number=ctx.number, ...), ...])` inside the if_cond.then. The static block evaluates kwargs at parse time, where `ctx` is not in scope. Parser would reject.
- **Fix:** Used `block_fn=lambda ctx: [...]` to defer kwarg evaluation to runtime.
- **Files modified:** issue_triage.star
- **Verification:** TestFlows_CoverageMatrix updated — triage_issue's primitive set now includes `step_block_fn` instead of `step_block`. Aggregate coverage still spans every primitive.
- **Committed in:** 6c2c889

**4. [Rule 1 - Bug] call_flow.inputs reject ${ctx.x} interpolation and ctx-expressions**
- **Found during:** Task 2 (drafting issue_triage.star top-level flow)
- **Issue:** Plan specced `inputs={"owner": "${ctx.owner}", "repo": "${ctx.repo}", "number": "${ctx.num}"}` — call_flow.inputs go through starlarkLiteralToGo (pkg/parser/builtins.go:1271) which only accepts primitive literals; the ${} desugarer is not invoked for call_flow.inputs.
- **Fix:** issue_triage passes literal placeholder values into call_flow (matching the existing skeleton/parallel_fanout.star pattern). Header docstring notes the D-19 constraint.
- **Files modified:** issue_triage.star
- **Verification:** TestFlows_ParseAll passes; call_flow primitive present in coverage set.
- **Committed in:** 6c2c889

---

**Total deviations:** 4 auto-fixed (4 Rule 1 — plan-vs-actual-API mismatches)
**Impact on plan:** All deviations are "spec drift" between the plan author's mental model and the v1 parser's accepted Starlark dialect. The flows still demonstrate every primitive listed in D-FLOWS-COVERAGE-MATRIX (verified by the aggregate coverage assertion in TestFlows_CoverageMatrix). No scope creep; success criteria EX-02 (every primitive + concern exercised) preserved.

## Issues Encountered

- **`if_cond.else_=[]` with empty list is accepted** — verified by parsing issue_triage.star where the triage_issue subflow's if_cond has `else_=[]`. Plan was unclear on this; the parser handles empty else cleanly.

- **Multi-flow per file works without `load()`** — issue_triage.star ships two flows in one file (triage_issue + issue_triage). No special handling needed; multi-flow per file has been supported since Phase 1 (D-15) and the parser's session-flow map collects all flow() calls in one pass.

## User Setup Required

None — flows are pure DSL artifacts. The cmd/extbin binary built in 06-05 + a credfile (06-02) are the runtime prerequisites for actually executing these flows; this plan only authors the flows + tests.

## Next Phase Readiness

- **06-07 (Tier-3 issue_triage_test.star)** — Builds on `issue_triage.star`. The two-flow file shape lets 06-07 author a `*_test.star` fixture that mocks `gh.list_open_issues` / `gh.get_issue` / `gh.add_comment` / `gh.add_label` and asserts the for_each_parallel + call_flow paths.
- **06-08 (README walkthrough)** — Reads the coverage matrix from this test's `expected` map for the published table. The mechanical pinning means the README cannot drift from reality.
- **Known v1 limitations to document in README:**
  - Step outputs do not auto-bind to ctx (consultants seed iteration lists from script outputs).
  - call_flow.inputs accept literal data only (D-19; no ${ctx.x} interpolation).
  - Both limitations are deferred to v2 (none are scoped for Phase 6).

## Self-Check: PASSED

All five .star flows + flows_test.go exist on disk:
- FOUND: examples/http-github-webhook/public_repo_check.star
- FOUND: examples/http-github-webhook/pr_to_webhook.star
- FOUND: examples/http-github-webhook/issue_triage.star
- FOUND: examples/http-github-webhook/batch_label_issues.star
- FOUND: examples/http-github-webhook/weekly_digest.star
- FOUND: examples/http-github-webhook/flows_test.go

Commits exist in git log:
- FOUND: 1ffa7bc (Task 1)
- FOUND: 6c2c889 (Task 2)
- FOUND: 685746d (Task 3)

Tests pass:
- `go test -race -count=1 ./examples/http-github-webhook/...` exits 0.
- `go build ./...` exits 0.
- `go vet ./...` exits 0.

---
*Phase: 06-example-project-http-github-webhook*
*Completed: 2026-05-07*
