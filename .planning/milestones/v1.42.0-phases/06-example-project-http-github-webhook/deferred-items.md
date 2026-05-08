# Phase 06 — Deferred Items

Out-of-scope discoveries logged during plan execution. Each item names
the discovering plan and the reason it was deferred (per the GSD
SCOPE BOUNDARY rule: only auto-fix issues DIRECTLY caused by the current
plan's changes).

## Logged During 06-08 Execution

### TestIssueTriageTest_PkgTesting / TestIssueTriageTest_SubprocessSmoke fail

- **Discovered during:** 06-08 plan execution (final `go test -race ./...`
  verification step).
- **File:** `examples/http-github-webhook/issue_triage_test.star` and
  `examples/http-github-webhook/issue_triage_test_e2e_test.go`.
- **Symptom:** `child flow not found in worker registry (or registered
  with multiple versions)` (`ChildFlowNotInRegistry`) — the
  `for_each_parallel` body inside the test's `issue_triage` flow calls
  `call_flow("triage_issue", ...)`, but `pkg/interpreter/replay_helper.go::
  RunOnceCapturing` registers only the entry flow with the test workflow
  registry, so the sub-flow is not found at runtime.
- **Why deferred:** 06-08's changes were docs-only (Markdown + TOML
  comments). The failing tests are produced by 06-07 (Tier-3 test
  harness work) running in parallel with 06-08. The latent
  `RunOnceCapturing` gap is a Phase-5 issue documented in 06-07's own
  source comments. Working-directory state shows 06-07 made an
  in-progress edit to `issue_triage_test.star` to inline the per-issue
  steps and avoid `call_flow` — that fix has not yet been committed by
  the parallel agent.
- **Owner:** 06-07 (Tier-3 test harness plan), continuing in parallel
  wave; OR a follow-up plan that registers sibling flows from the
  parsed test file with the test workflow registry.
- **Action for 06-08:** None — out of scope. The credfile resolver
  tests (`./pkg/extension/credfile/...`) all pass, confirming the
  `.skytime-credentials.example` schema is intact.
