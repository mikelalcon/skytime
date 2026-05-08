---
phase: 06-example-project-http-github-webhook
plan: 03
subsystem: extensions
tags: [github-rest-api, go-github-v78, idempotence, dag-operation-output, starlark-builtin, factory-pattern]

# Dependency graph
requires:
  - phase: 04-static-validation-tier-cli-skeleton
    provides: pkg/extension/builtin/http/ (extension template — Initialize / Operations / per-method builtin pattern mirrored verbatim)
  - phase: 04-static-validation-tier-cli-skeleton
    provides: pkg/extension/credential.go (BearerCredential.Token.Reveal() consumed by newClientForCredential)
  - phase: 04-static-validation-tier-cli-skeleton
    provides: pkg/extension/error.go (ErrNonRetryable sentinel wrapped on 4xx GitHub responses)
  - phase: 06-example-project-http-github-webhook (Wave-0)
    provides: go-github/v78 v78.0.0 in go.mod (added by 06-01)
provides:
  - "examples/http-github-webhook/extensions/github/github.go — github extension implementation: Name, Initialize, Operations + 7 OperationFuncs + clientFactory + per-op builtins"
  - "examples/http-github-webhook/extensions/github/response.go — 6 top-level dag.OperationOutput types (Repo, Issue, IssueList, Comment, Labels, PRList) + nested GitHubPROutput element type"
  - "examples/http-github-webhook/extensions/github/github_test.go — registry-acceptance + idempotence-matrix + output-marker conformance tests (3 funcs, 7 sub-cases for matrix)"
  - "Locked op×idempotence matrix verified by test: 5 idempotent (get_repo, get_issue, list_open_issues, list_prs, list_recent_merged_prs) + 2 non-idempotent (add_comment, add_label)"
  - "Unauthenticated GitHub client path for public_repo_check.star (cred=nil → gogh.NewClient(nil))"
  - "Bearer-credential PAT auth path via gogh.NewClient(nil).WithAuthToken(bearer.Token.Reveal())"
  - "Error classifier: 4xx → wraps ErrNonRetryable; rate-limit + 5xx → pass through (Temporal retries)"
affects: [06-05-cmd-extbin (registers this extension via cli.WithExtensions), 06-06-flows (consume gh.client(...).get_issue/add_comment/etc), 06-07-issue-triage-test (Tier-3 mocks key on extension="github")]

# Tech tracking
tech-stack:
  added:
    - "github.com/google/go-querystring v1.1.0 (indirect — go-github v78 transitive; landed via go get)"
  patterns:
    - "GitHub-extension factory pattern: github.client(credential=...) returns sub-Module with per-op builtins, mirrors http.endpoint(base_url=..., credential=...) shape"
    - "Activity-side error classification: errors.As(*RateLimitError) → retryable; errors.As(*ErrorResponse) with 4xx → wrap ErrNonRetryable; default pass-through"
    - "RFC3339-UTC stringification of time.Time fields for deterministic Temporal JSON encoding (no timezone drift, no replay divergence)"
    - "args coercion helpers (asGetRepoArgs / asGetIssueArgs / etc) — tolerant to value-or-pointer caller convention, mirrors HTTP extension's asGetArgs / asBodyArgs (quick 260502-guu Rule 1 fix pattern)"

key-files:
  created:
    - "examples/http-github-webhook/extensions/github/response.go"
    - "examples/http-github-webhook/extensions/github/github.go"
    - "examples/http-github-webhook/extensions/github/github_test.go"
  modified:
    - "go.mod"
    - "go.sum"

key-decisions:
  - "Used gogh as the import alias for github.com/google/go-github/v78/github to avoid collision with our own local 'package github' (consumers will alias our package as skygh in cmd/extbin per the plan)"
  - "newOpBuiltin uses struct literal &dag.ActionRef{...} not a constructor (verified by grep — no NewActionRef function exists in pkg/dag/), Kind_ format is 'github.<op>' matching the activity-side dispatcher key shape (mirrors pkg/extension/builtin/http/http.go:150-155)"
  - "Test file uses ONLY the skygh alias (not bare github + skygh in same file), avoiding the duplicate-import compile error noted in the plan's 'Notes' caveat"
  - "Added args coercion helpers (asGetRepoArgs etc) even though the plan skeleton used direct type assertions — direct assertions panic when the activity decoder returns value vs pointer; mirrors the existing HTTP extension's asGetArgs/asBodyArgs pattern (Rule 1 prevention)"

patterns-established:
  - "Skytime extension shape for typed third-party SDK wrapping: import as gogh-style alias, project SDK types into local OperationOutput types, classify SDK errors at the OperationFunc boundary, expose factory that closes over credential ID"
  - "Idempotence-matrix test pattern: table-driven map[opName]bool, t.Run subtest per op, plus a final assertion that len(cases) == len(ops) to catch new-op-without-test drift"

requirements-completed: [EX-01]

# Metrics
duration: 4min
completed: 2026-05-07
---

# Phase 6 Plan 3: GitHub Extension (7 ops + idempotence matrix) Summary

**GitHub extension landed at examples/http-github-webhook/extensions/github/ with 7 operations (get_repo, get_issue, list_open_issues, add_comment, add_label, list_prs, list_recent_merged_prs), locked idempotence matrix (5 GET=true / 2 POST=false) verified mechanically by table-driven test, factory-style Starlark surface (github.client(credential=...).get_issue(...)) mirroring http.endpoint pattern, go-github/v78 typed client with 4xx→ErrNonRetryable error classification, and unauthenticated path supporting public_repo_check.star.**

## Performance

- **Duration:** ~4 min
- **Started:** 2026-05-07T23:03:14Z
- **Completed:** 2026-05-07T23:07:18Z
- **Tasks:** 3
- **Files modified:** 5 (3 created, 2 modified)

## Accomplishments

- 6 top-level dag.OperationOutput types declared with marker conformance (Repo, Issue, IssueList, Comment, Labels, PRList; PROutput is embedded element-only)
- 7 OperationSpecs declared with the locked idempotence matrix per RESEARCH.md § 1a
- Factory-style Starlark surface wired: `github.client(credential=...)` returns sub-Module with seven per-op builtins, each closing over the credential ID
- OperationFuncs use go-github v78 typed client; nil credential supports the unauthenticated public-API path required by public_repo_check.star
- Error classifier distinguishes RateLimitError (retryable, pass through) / 4xx (wrap ErrNonRetryable) / 5xx + other (retryable, pass through)
- 3 tests pass under `-race`: registry-acceptance, idempotence-matrix (table-driven, 7 subtests), output-marker (compile-time + runtime)
- Full project `go build ./...` and `go vet ./examples/...` green

## Op × Idempotence Matrix (verified by test)

| Operation               | Method | Endpoint                                     | Idempotent | Source                                  |
| ----------------------- | ------ | -------------------------------------------- | ---------- | --------------------------------------- |
| get_repo                | GET    | /repos/{o}/{r}                               | true       | RFC-7231 + GitHub REST                  |
| get_issue               | GET    | /repos/{o}/{r}/issues/{n}                    | true       | RFC-7231 + GitHub REST                  |
| list_open_issues        | GET    | /repos/{o}/{r}/issues?state=open             | true       | RFC-7231 + GitHub REST                  |
| add_comment             | POST   | /repos/{o}/{r}/issues/{n}/comments           | **false**  | RFC-7231 + application semantics        |
| add_label               | POST   | /repos/{o}/{r}/issues/{n}/labels             | **false**  | RFC-7231 + application semantics        |
| list_prs                | GET    | /repos/{o}/{r}/pulls?state=open              | true       | RFC-7231 + GitHub REST                  |
| list_recent_merged_prs  | GET    | /repos/{o}/{r}/pulls?state=closed (filtered) | true       | RFC-7231 + GitHub REST (client filters) |

`TestExtension_OperationsIdempotenceMatchesEndpoints` pins this matrix verbatim — any change requires CONTEXT.md/RESEARCH.md amendments first.

## Test Results

```
$ go test -race -count=1 ./examples/http-github-webhook/extensions/github/...
ok  	github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/github	1.556s
```

3 test funcs (one with 7 subtests) all pass under `-race`:
- TestExtension_RegistersWithoutError — every op has Idempotent != nil + non-nil Func + non-nil KwargsType
- TestExtension_OperationsIdempotenceMatchesEndpoints — 7 subtests pinning the locked matrix verbatim
- TestExtension_OutputsImplementOperationOutput — 6 compile-time assertions + 6 runtime calls of IsOperationOutput()

## Task Commits

Each task was committed atomically (`--no-verify` per parallel-executor protocol):

1. **Task 1: Output types in response.go (dag.OperationOutput markers)** — `fbf365f` (feat)
2. **Task 2: GitHub extension implementation in github.go** — `6e0bca5` (feat)
3. **Task 3: Tests — registry acceptance + idempotence matrix + output marker conformance** — `74daada` (test)

**Plan metadata:** _to be added by final commit step (SUMMARY + STATE + ROADMAP)_

## Files Created/Modified

- `examples/http-github-webhook/extensions/github/response.go` (created, 77 lines) — 6 top-level OperationOutput types + 1 element type
- `examples/http-github-webhook/extensions/github/github.go` (created, ~440 lines) — Extension impl, 7 ops, factory, classifier, 6 args coercion helpers
- `examples/http-github-webhook/extensions/github/github_test.go` (created, 88 lines) — 3 test funcs covering registration, matrix, marker
- `go.mod` (modified) — added `github.com/google/go-querystring v1.1.0 // indirect` (go-github v78 transitive)
- `go.sum` (modified) — checksums for go-querystring v1.1.0

## Decisions Made

- **Import alias `gogh`** for `github.com/google/go-github/v78/github` to avoid collision with our own local `package github`. Consumers in cmd/extbin will alias our package as `skygh` symmetrically.
- **Struct-literal `&dag.ActionRef{...}` construction** in `newOpBuiltin` instead of a constructor function — verified by `grep -n "func NewActionRef" pkg/dag/` returning empty, matches the canonical pattern at `pkg/extension/builtin/http/http.go:150-155`. The plan's revised `<read_first>` block explicitly noted this; followed verbatim.
- **`Kind_` format `"github.<op>"`** (e.g. `"github.get_repo"`) — matches the activity-side dispatcher key shape verified at `pkg/activity/execute_batch_test.go:52` (`mkRef("fake.echo", ...)` helper).
- **`time.Time` → RFC3339-UTC string** in OperationFunc projection (issueToOutput, prsToOutput) — keeps Temporal's JSON DataConverter deterministic across replay (per RESEARCH.md § Pitfall 6 and CLAUDE.md "Determinism: parsed DAG must be deterministic").
- **Test file uses ONLY the `skygh` alias** (not bare `github` + `skygh` from same module path) to avoid the duplicate-import compile error the plan itself flagged in its Notes block.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Added `google/go-querystring v1.1.0` to go.sum (go-github v78 transitive)**

- **Found during:** Task 2 (github.go implementation — first build attempt)
- **Issue:** `go build ./examples/http-github-webhook/extensions/github/...` failed with:
  > missing go.sum entry for module providing package github.com/google/go-querystring/query (imported by github.com/google/go-github/v78/github); to add: go get github.com/google/go-github/v78/github@v78.0.0
  Wave-0 (06-01) added go-github to go.mod but no consuming `.go` file existed yet, so the transitive dep wasn't materialized in go.sum.
- **Fix:** Ran `go get github.com/google/go-github/v78/github@v78.0.0`, which downloaded `go-querystring` and added the checksum + indirect require to go.mod / go.sum.
- **Files modified:** go.mod (added `require github.com/google/go-querystring v1.1.0 // indirect`), go.sum (checksums)
- **Verification:** `go build ./examples/http-github-webhook/extensions/github/...` and `go build ./...` both green afterward.
- **Committed in:** 6e0bca5 (Task 2 commit, alongside github.go)

**2. [Rule 2 - Missing Critical] Added args coercion helpers (asGetRepoArgs / asGetIssueArgs / etc.)**

- **Found during:** Task 2 implementation, while reading `pkg/extension/builtin/http/http.go` for the canonical pattern
- **Issue:** The plan skeleton used direct type assertions (`a := args.(*GetRepoArgs)`) that would panic at runtime if the production activity decoder returns value-typed args (vs. pointer). The HTTP extension hit this exact bug previously — see comment block at `pkg/extension/builtin/http/http.go:178-185` (quick 260502-guu Rule 1 fix). Re-introducing the same fragility would be a known correctness regression.
- **Fix:** Added 6 helper funcs (`asGetRepoArgs`, `asGetIssueArgs`, `asListIssuesArgs`, `asAddCommentArgs`, `asAddLabelArgs`, `asListPRsArgs`) mirroring HTTP's `asGetArgs`/`asBodyArgs`, each accepting either `*T` or `T`. Each OperationFunc now goes `a := asXxxArgs(args)`.
- **Files modified:** examples/http-github-webhook/extensions/github/github.go
- **Verification:** All 3 tests pass; `go vet` green; pattern matches the existing HTTP extension exactly.
- **Committed in:** 6e0bca5 (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (1 blocking — missing transitive checksum; 1 missing-critical — defensive args coercion to prevent value-vs-pointer panic regression)
**Impact on plan:** No scope creep. Both deviations were pure correctness reinforcement and matched established patterns in the codebase. Plan acceptance criteria all met (file existence, grep counts, build + vet + test green).

## Issues Encountered

- **Plan acceptance criterion `grep -q 'github.NewClient(nil)'` does not match the literal text in github.go.** The plan's own action skeleton uses the alias `gogh "github.com/google/go-github/v78/github"` (because the local package is also named `github`), making the actual symbol `gogh.NewClient(nil)`. The acceptance criterion was specified inconsistently with the action body. Semantic intent — "unauthenticated path for public_repo_check.star" — is fully present (`gogh.NewClient(nil)` appears twice in `newClientForCredential`). Documenting here for transparency; not adjusting the plan retroactively.

## User Setup Required

None — no external service configuration required for this plan. The unauthenticated GitHub path needs zero credentials; authenticated flows will require user-supplied PATs at `~/.skytime-credentials`, which is documented at the README walkthrough phase (06-08) and not consumed by this plan.

## Next Phase Readiness

**Ready for downstream waves:**
- 06-05 (cmd/extbin) can register this extension via `cli.WithExtensions(skygh.New())`
- 06-06 (flow authoring) can write `gh = github.client(credential = "github_token")` then `gh.get_issue(owner=..., repo=..., number=...)`
- 06-07 (issue_triage_test.star) can mock with `tester.mock_action(extension="github", op="get_issue", ...)` — the registered name keys on `New().Name()` (returns `"github"`), NOT the local `gh` Starlark variable

## Known Stubs

None. All seven operations have full implementations dispatching to go-github v78. No placeholder values, no hardcoded empty returns, no TODO/FIXME markers.

## Self-Check

- [x] FOUND: examples/http-github-webhook/extensions/github/response.go
- [x] FOUND: examples/http-github-webhook/extensions/github/github.go
- [x] FOUND: examples/http-github-webhook/extensions/github/github_test.go
- [x] FOUND: commit fbf365f (Task 1 — response.go)
- [x] FOUND: commit 6e0bca5 (Task 2 — github.go + go.mod/go.sum transitive)
- [x] FOUND: commit 74daada (Task 3 — github_test.go)
- [x] grep -c 'IsOperationOutput()' response.go == 6
- [x] grep -c 'extension.Ptr(true)' github.go == 5
- [x] grep -c 'extension.Ptr(false)' github.go == 2
- [x] grep -c 'var _ dag.OperationOutput = ' github_test.go == 6
- [x] `go build ./examples/http-github-webhook/extensions/github/...` exits 0
- [x] `go vet ./examples/http-github-webhook/extensions/github/...` exits 0
- [x] `go test -race -count=1 ./examples/http-github-webhook/extensions/github/...` exits 0
- [x] `go build ./...` exits 0 (no regressions)

## Self-Check: PASSED

---
*Phase: 06-example-project-http-github-webhook*
*Completed: 2026-05-07*
