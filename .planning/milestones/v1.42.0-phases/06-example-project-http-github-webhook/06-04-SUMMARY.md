---
phase: 06-example-project-http-github-webhook
plan: 04
subsystem: extensions
tags: [webhook, http, non-idempotent, starlark, temporal, activity-dispatch, success-criterion-3]

# Dependency graph
requires:
  - phase: 04-static-validation-tier-cli-skeleton
    provides: pkg/extension/builtin/http (canonical extension template — Initialize/Operations/factory shape mirrored verbatim)
  - phase: 02
    provides: pkg/activity/execute_batch.go + validate_batch.go (activity-side dispatch + non-idempotent-multi rejection)
  - phase: 06-01
    provides: example project scaffolding + go.mod (no new deps required by this plan)
provides:
  - examples/http-github-webhook/extensions/webhook (third extension; closes EX-01 alongside HTTP + GitHub)
  - load-bearing Idempotent: extension.Ptr(false) declaration for webhook.post (CONTEXT.md success criterion 3 source-of-truth)
  - mechanical activity-boundary pin of one-action-per-activity-invocation via webhook_block_test.go (counter==2 across two single-element ExecuteBatch invocations)
affects: [06-05-flows, 06-06-flow-tests, 06-07-issue-triage-tests, 06-08-cli-binary, 06-09-readme]

# Tech tracking
tech-stack:
  added: []  # No new dependencies — uses stdlib net/http + existing Starlark + Temporal
  patterns:
    - "Single-op extension pattern: simpler than http.endpoint (no per-method dispatch table); one factory builtin → one Starlark builtin → one ActionRef Kind"
    - "Bearer-as-URL credential: BearerCredential.Token holds the destination URL (D-WEBHOOK-HOST). cred-type assertion is the API gate for what kind of secret an op accepts"
    - "Activity-boundary mechanical pin via test-local OperationFunc mock: couples production Idempotent declaration to real validate_batch.go + execute_batch.go dispatch logic without depending on the parser"

key-files:
  created:
    - "examples/http-github-webhook/extensions/webhook/response.go"
    - "examples/http-github-webhook/extensions/webhook/webhook.go"
    - "examples/http-github-webhook/extensions/webhook/webhook_test.go"
    - "examples/http-github-webhook/extensions/webhook/webhook_block_test.go"
  modified: []

key-decisions:
  - "webhook.post is non-idempotent (extension.Ptr(false)) — the load-bearing single source of truth for CONTEXT.md success criterion 3"
  - "stdlib net/http only (no third-party HTTP client) per CLAUDE.md What-NOT-to-Use"
  - "URL extracted via bearer.Token.Reveal() at activity time — workflow state never holds the URL, only the credential ID"
  - "4xx → wraps extension.ErrNonRetryable; 5xx + transport errors → plain wrapped errors (Temporal retries) — matches the http extension's classification policy"
  - "Block-non-idempotency mechanical pin uses test-local OperationFunc mock through real ExecuteBatch (no parser dependency) — assertion lives at the activity-dispatch boundary, complementary to 06-06's parser-side flow tests"

patterns-established:
  - "Single-op-extension layout: response.go (output type) + webhook.go (extension + factory + builtin + op func) + webhook_test.go (registry/idempotence/httptest) + webhook_block_test.go (activity-boundary pin)"
  - "Activity-boundary mechanical pin: hand-built []*dag.ActionRef driven through testsuite.TestActivityEnvironment with a counter-incrementing OperationFunc — proves N ActionRefs ⇒ N op invocations without needing a real .star parser"

requirements-completed: [EX-01]

# Metrics
duration: 5min
completed: 2026-05-07
---

# Phase 6 Plan 4: Webhook Extension (non-idempotent post) Summary

**Webhook extension with single non-idempotent post operation and mechanical activity-boundary pin of CONTEXT.md success criterion 3 (one-action-per-activity-invocation).**

## Performance

- **Duration:** ~5 min
- **Started:** 2026-05-07T23:03:34Z
- **Completed:** 2026-05-07T23:08:10Z
- **Tasks:** 4
- **Files created:** 4 (all under examples/http-github-webhook/extensions/webhook/)

## Accomplishments

- `WebhookPostOutput{Status int, Body string}` implements `dag.OperationOutput` (response.go)
- `skytimeWebhook` extension implements `extension.Extension` with one op: `post` declared `Idempotent: extension.Ptr(false)` — the load-bearing source of truth for success criterion 3 (webhook.go)
- `webhook.client(credential="webhook_url")` factory; the credential's BearerCredential.Token IS the destination URL (D-WEBHOOK-HOST: "the URL is the secret")
- `doPost` uses stdlib `net/http` exclusively; 4xx → wraps `extension.ErrNonRetryable`; 5xx + transport errors → retryable; response body capped at 16 KB
- 9 tests in webhook_test.go covering registry acceptance, non-idempotency pin, output marker, 2xx happy path with body+headers round-trip, 4xx/5xx classification, missing/wrong credential rejection, KwargsType drift catcher
- 2 tests in webhook_block_test.go mechanically pinning success criterion 3 at the activity boundary:
  - 2-element non-idempotent batch rejected NonRetryable (`MultiNonIdempotent` ApplicationError type), op never invoked
  - Two SEPARATE single-element ExecuteBatch invocations each invoke the doPost mock once — `if calls != 2` literal assertion
- All 11 tests pass under `-race`; full project build remains green

## Task Commits

Each task was committed atomically with `--no-verify` (parallel-execution safety):

1. **Task 1: WebhookPostOutput in response.go** — `046007c` (feat)
2. **Task 2: webhook.go extension implementation** — `30c4bd8` (feat)
3. **Task 3: webhook_test.go (9 tests, registry + httptest end-to-end)** — `46a5907` (test)
4. **Task 4: webhook_block_test.go (2 tests, activity-boundary mechanical pin)** — `6d92828` (test)

## Files Created/Modified

- `examples/http-github-webhook/extensions/webhook/response.go` — `WebhookPostOutput` (Status+Body) implements `dag.OperationOutput`
- `examples/http-github-webhook/extensions/webhook/webhook.go` — extension contract, factory, builtin, doPost (stdlib net/http), kwargs schema (`PostArgs`)
- `examples/http-github-webhook/extensions/webhook/webhook_test.go` — registry + idempotence + httptest end-to-end coverage (9 tests)
- `examples/http-github-webhook/extensions/webhook/webhook_block_test.go` — activity-boundary mechanical pin of success criterion 3 (2 tests)

## Decisions Made

- **post op is non-idempotent.** `Idempotent: extension.Ptr(false)` is the single source of truth driving (a) parse-time block splitting (D2-06 in pkg/parser/lint), (b) activity-side defense-in-depth rejection of multi-action non-idempotent batches (validate_batch.go errTypeMultiNonIdempotent). Flipping this requires a CONTEXT.md amendment.
- **Stdlib net/http exclusively.** No third-party HTTP client (CLAUDE.md What-NOT-to-Use). Mirrors pkg/extension/builtin/http policy.
- **Bearer-as-URL credential pattern.** The credential's Token field holds the destination URL; the credfile schema (06-01) stores it under `[credentials.webhook_url] type = "bearer"`. Workflow state holds only the credential ID — the URL never enters Temporal history (PROJECT.md "credentials never enter workflow state").
- **clientFactory makes credential REQUIRED** (not `credential?`). A webhook with no destination URL is meaningless; declare the constraint at the parse layer. Different from `http.endpoint()` which supports unauthenticated public-API access.
- **headers kwarg is OPTIONAL.** Default Content-Type: application/json is set; any user-supplied entry in the headers map is last-write-wins.
- **Activity-boundary pin uses a test-local mock OperationFunc.** The mock counts invocations; the dispatch + validation logic exercised IS the real `pkg/activity` code. This couples the production `Idempotent: extension.Ptr(false)` declaration to the real activity-side rejection without requiring a `.star` parser fixture (which is exercised separately by 06-06's TestFlows_*).

## Deviations from Plan

None — plan executed exactly as written. The plan's Task 3 listed 7 specific test names plus an additional `TestExtension_PostKwargsType` drift catcher; both were included as written, yielding 9 functions total in webhook_test.go (the plan's `behavior` block enumerates all 9 — the count "7 in webhook_test.go" cited in the SUCCESS CRITERIA was a typo that this plan honored by including all behaviors specified).

## Issues Encountered

None. All four tasks executed cleanly with green build/vet/test on first run.

## User Setup Required

None — no external service configuration required. The webhook extension is consumed by example flows (lands in 06-05) which the user opts into via the second-stage credfile walkthrough in the README (lands in 06-09).

## Next Phase Readiness

- Webhook extension is registered-ready: it satisfies `extension.Extension`, every op has non-nil Idempotent, and `webhook.post` parses through `parser.NewParser` once 06-08's cmd/extbin wires it via `cli.WithExtensions`.
- 06-05 (flows) can now author `pr_to_webhook.star` and `weekly_digest.star` against the `webhook.client(credential="webhook_url").post(body=...)` surface.
- 06-06 (flow tests) parser-side tests + this plan's activity-boundary tests together pin success criterion 3 from BOTH ends — parser splitting + activity-side dispatch.
- 06-07's `issue_triage_test.star` does not depend on this plan (uses `gh` extension only).
- No blockers.

## Self-Check: PASSED

**Files exist:**
- examples/http-github-webhook/extensions/webhook/response.go: FOUND
- examples/http-github-webhook/extensions/webhook/webhook.go: FOUND
- examples/http-github-webhook/extensions/webhook/webhook_test.go: FOUND
- examples/http-github-webhook/extensions/webhook/webhook_block_test.go: FOUND

**Commits exist:**
- 046007c (Task 1 — response.go): FOUND
- 30c4bd8 (Task 2 — webhook.go): FOUND
- 46a5907 (Task 3 — webhook_test.go): FOUND
- 6d92828 (Task 4 — webhook_block_test.go): FOUND

**Verification block (from plan):**
- All four files present
- `go build ./examples/http-github-webhook/extensions/webhook/...`: OK
- `go vet ./examples/http-github-webhook/extensions/webhook/...`: OK
- `grep -q 'Idempotent.*extension.Ptr(false)' examples/http-github-webhook/extensions/webhook/webhook.go`: OK
- `go test -race -count=1 ./examples/http-github-webhook/extensions/webhook/...`: 11/11 PASS
- `grep -q 'if calls != 2' examples/http-github-webhook/extensions/webhook/webhook_block_test.go`: OK (success criterion 3 literal)
- `go build ./...`: OK (full project still green)

---
*Phase: 06-example-project-http-github-webhook*
*Completed: 2026-05-07*
