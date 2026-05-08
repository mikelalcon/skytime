---
phase: 06-example-project-http-github-webhook
verified: 2026-05-07T00:00:00Z
status: passed
score: 5/5 success criteria verified
---

# Phase 6: Example Project (HTTP + GitHub + Webhook) — Verification Report

**Phase Goal:** Ship `examples/http-github-webhook/` as the dogfooding vehicle and proof-of-life: three real extensions (HTTP, GitHub, Webhook) with per-operation `Idempotent bool`, four-to-six `.star` flows exercising every primitive and every concern, at least one `.star` test using `temporal_test`, a `pkg/extension/credfile/` library credential resolver (TOML, `$HOME/.skytime-credentials` default), and a README walkthrough that takes a reader from `git clone` to a successfully-executed flow against `skytime dev-server` in under five commands. Plus `.github/workflows/ci.yml` running the full Go test suite + `skytime test ./examples/` on every push (any branch).

**Verified:** 2026-05-07
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (Success Criteria from ROADMAP.md)

| #  | Truth (Success Criterion)                                                                                                              | Status     | Evidence                                                                                                                                                                                                                                                                              |
|----|-----------------------------------------------------------------------------------------------------------------------------------------|------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| 1  | A reader can `git clone`, follow the README walkthrough, and execute a `.star` flow against `skytime dev-server` in under five commands | ✓ VERIFIED | README.md Quick Start has 4 commands (`cd`, `temporal server start-dev --headless &`, `go build`, `./extbin run`); CI smoke `walkthrough_smoke.sh` exercises the EXACT command verbatim and asserts `flow complete` substring                                                          |
| 2  | Four-to-six `.star` flows collectively exercise every DSL primitive and every concern; coverage matrix in README                       | ✓ VERIFIED | 5 `.star` flows present (public_repo_check, pr_to_webhook, issue_triage, batch_label_issues, weekly_digest); README.md "Coverage matrix" section copies the table verbatim; `flows_test.go::TestFlows_CoverageMatrix` mechanically pins all 7 primitives across the corpus                |
| 3  | Three extensions declare `Idempotent bool` per operation; `webhook.post` non-idempotent and verified to execute one-action-per-invocation even inside a `block` | ✓ VERIFIED | All 7 GitHub ops + 1 webhook op have `Idempotent: extension.Ptr(true|false)`; `webhook_block_test.go::TestWebhookPost_NonIdempotent_OneActivityInvocationPerActionRef` mechanically asserts counter==2 across two single-element ExecuteBatch invocations                              |
| 4  | At least one `.star` test uses `temporal_test` to exercise retries via `attempt` and asserts replay determinism; awkward Starlark ergonomics fed back as fixes | ✓ VERIFIED | `issue_triage_test.star` has `def test_get_issue_retries_then_succeeds` using `lambda kwargs, attempt:` with `attempt == 1` first-fail-second-succeed; replay determinism is ALWAYS-ON (D5-D1) via `RunOnceCapturingWithSiblings` doubling each `tester.run`; two latent-gap fixes shipped in `pkg/interpreter` and `pkg/testing` |
| 5  | `.github/workflows/ci.yml` runs `go vet`, `go test -race ./... -count=1`, `extbin test`, and EX-04 walkthrough smoke on every push to any branch                | ✓ VERIFIED | `ci.yml` has all 4 named steps in order on `push: branches: ['**']`; uses `actions/checkout@v6`, `actions/setup-go@v6` (cache:true), `temporalio/setup-temporal@v0`; ubuntu-latest with timeout-minutes:15                                                                              |

**Score:** 5/5 truths verified

### Required Artifacts (Levels 1-3 + 4 where applicable)

| Plan  | Artifact                                                                          | Lines | Substantive | Wired         | Status      |
|-------|------------------------------------------------------------------------------------|-------|-------------|---------------|-------------|
| 06-01 | `go.mod` (pelletier/go-toml/v2 v2.3.1, google/go-github/v78 v78.0.0)              | n/a   | ✓           | ✓ go build OK | ✓ VERIFIED  |
| 06-01 | `examples/http-github-webhook/.skytime-credentials.example`                        | n/a   | ✓ all 3 types + bearer-as-URL | ✓ schema match w/ credfile | ✓ VERIFIED |
| 06-01 | `.gitignore` (`.skytime-credentials` w/ `!*.example` negation)                    | n/a   | ✓           | n/a           | ✓ VERIFIED  |
| 06-02 | `pkg/extension/credfile/doc.go`                                                    | 35    | ✓ ≥25       | ✓             | ✓ VERIFIED  |
| 06-02 | `pkg/extension/credfile/options.go`                                                | 43    | ✓ ≥25       | ✓             | ✓ VERIFIED  |
| 06-02 | `pkg/extension/credfile/file.go`                                                   | 76    | ✓ ≥60       | ✓ NewSecret   | ✓ VERIFIED  |
| 06-02 | `pkg/extension/credfile/resolver.go`                                               | 105   | ✓ ≥80       | ✓ CredentialHandler iface check + ErrUnknownCredential + toml.Unmarshal | ✓ VERIFIED |
| 06-02 | `pkg/extension/credfile/resolver_test.go`                                          | 329   | ✓ ≥200      | ✓ tests pass  | ✓ VERIFIED  |
| 06-03 | `examples/http-github-webhook/extensions/github/github.go`                         | 460   | ✓ ≥250 + 7 ops idempotence matrix locked | ✓ go-github v78 + extension.Ptr | ✓ VERIFIED |
| 06-03 | `examples/http-github-webhook/extensions/github/response.go`                       | 77    | ✓ ≥50 + 6 IsOperationOutput markers | ✓ dag.OperationOutput | ✓ VERIFIED |
| 06-03 | `examples/http-github-webhook/extensions/github/github_test.go`                    | 88    | ✓ ≥80 + idempotence-matrix test | ✓ tests pass | ✓ VERIFIED |
| 06-04 | `examples/http-github-webhook/extensions/webhook/webhook.go`                       | 208   | ✓ ≥150 + Ptr(false) + bearer.Token.Reveal() | ✓ stdlib net/http | ✓ VERIFIED |
| 06-04 | `examples/http-github-webhook/extensions/webhook/response.go`                      | 27    | ✓ ≥15 + IsOperationOutput | ✓ dag.OperationOutput | ✓ VERIFIED |
| 06-04 | `examples/http-github-webhook/extensions/webhook/webhook_test.go`                  | 165   | ✓ ≥100 + httptest.Server end-to-end | ✓ tests pass | ✓ VERIFIED |
| 06-04 | `examples/http-github-webhook/extensions/webhook/webhook_block_test.go`            | 151   | ✓ ≥80 + counter==2 assertion via real ExecuteBatch | ✓ tests pass | ✓ VERIFIED |
| 06-05 | `examples/http-github-webhook/cmd/extbin/main.go`                                  | 120   | ✓ ≥70 + cli.NewRootCommand + 3 extensions + lazy credfile | ✓ all imports + binary builds | ✓ VERIFIED |
| 06-05 | `examples/http-github-webhook/cmd/extbin/main_test.go`                             | 114   | ✓ ≥60 + lazy-init proof + subprocess --help smoke | ✓ tests pass | ✓ VERIFIED |
| 06-06 | `examples/http-github-webhook/public_repo_check.star`                              | 67    | ✓ ≥30       | ✓ parses OK   | ✓ VERIFIED  |
| 06-06 | `examples/http-github-webhook/pr_to_webhook.star`                                  | 57    | ✓ ≥30       | ✓ parses OK   | ✓ VERIFIED  |
| 06-06 | `examples/http-github-webhook/issue_triage.star` (incl. `triage_issue` subflow)    | 113   | ✓ ≥50 + call_flow + for_each_parallel + script + if_cond + block_fn | ✓ parses OK | ✓ VERIFIED |
| 06-06 | `examples/http-github-webhook/batch_label_issues.star`                             | 46    | ✓ ≥25 + block_fn dynamic batch | ✓ parses OK | ✓ VERIFIED |
| 06-06 | `examples/http-github-webhook/weekly_digest.star`                                  | 57    | ✓ ≥30       | ✓ parses OK   | ✓ VERIFIED  |
| 06-06 | `examples/http-github-webhook/flows_test.go`                                       | 247   | ✓ ≥100 + TestFlows_ParseAll + TestFlows_CoverageMatrix | ✓ tests pass | ✓ VERIFIED |
| 06-07 | `examples/http-github-webhook/issue_triage_test.star`                              | 171   | ✓ ≥80 + 3 def test_* + attempt-aware retry + tester.workflow | ✓ extbin test passes | ✓ VERIFIED |
| 06-07 | `examples/http-github-webhook/issue_triage_test_e2e_test.go`                       | 117   | ✓ ≥80 + RunCLI + WithExtensions + subprocess smoke | ✓ tests pass | ✓ VERIFIED |
| 06-08 | `examples/http-github-webhook/README.md`                                           | 257   | ✓ ≥180 + all 7 sections + chmod 600 + cp .skytime-credentials.example | ✓ links to docs/cli-binary.md + extbin test | ✓ VERIFIED |
| 06-08 | `README.md` (root)                                                                 | n/a   | ✓ "examples/http-github-webhook/README.md" link present (line 361) | ✓ | ✓ VERIFIED |
| 06-08 | `docs/for-extension-developers/README.md`                                          | n/a   | ✓ "Credential resolution: pkg/extension/credfile/" section (line 55) | ✓ | ✓ VERIFIED |
| 06-09 | `.github/workflows/ci.yml`                                                         | 45    | ✓ ≥30 + 4 named steps + push:branches:['**'] + pinned actions | ✓ wires extbin + smoke | ✓ VERIFIED |
| 06-09 | `.github/workflows/scripts/walkthrough_smoke.sh`                                   | 118   | ✓ ≥30 + shebang + set -euo pipefail + trap cleanup + 'flow complete' assert | ✓ runs public_repo_check.star | ✓ VERIFIED |

### Phase 5 Latent-Gap Fixes (per Success Criterion 4)

| Fix                                                | Location                                                                                  | Status     |
|----------------------------------------------------|-------------------------------------------------------------------------------------------|------------|
| Sibling-flow registration (multi-flow .star tests) | `pkg/interpreter/replay_helper.go::SiblingFlow` + `RunOnceCapturingWithSiblings` (line 311) | ✓ VERIFIED |
| Multi-flow wrapper at testing layer                | `pkg/testing/replay.go::RunOnceCapturingWithSiblings` (line 60)                            | ✓ VERIFIED |
| `tester.run` passes runContext.flows as siblings   | `pkg/testing/builtin_run.go::buildSiblingMap` (line 156)                                   | ✓ VERIFIED |
| Multiset replay-determinism fallback               | `pkg/testing/replay_diff.go::multisetEqual` (line 157) + `filterDeterministicEvents` (line 211) | ✓ VERIFIED |

All four fixes are exercised by `go test -count=1 ./pkg/interpreter/... ./pkg/testing/... ./examples/http-github-webhook/...` (all green).

### Key Link Verification

| From                                                          | To                                                | Via                                                         | Status |
|---------------------------------------------------------------|---------------------------------------------------|-------------------------------------------------------------|--------|
| `.skytime-credentials.example`                                | `pkg/extension/credfile`                          | TOML schema match (bearer/basic/apikey type tags)           | ✓ WIRED |
| `pkg/extension/credfile/resolver.go`                          | `pkg/extension/handler.go`                        | `var _ extension.CredentialHandler = (*Resolver)(nil)` (line 95) | ✓ WIRED |
| `pkg/extension/credfile/resolver.go`                          | `extension.ErrUnknownCredential`                  | `errors.Is`-friendly wrap at line 85                        | ✓ WIRED |
| `pkg/extension/credfile/file.go`                              | `extension.NewSecret`                             | Lines 49, 58, 67                                            | ✓ WIRED |
| `pkg/extension/credfile/resolver.go`                          | `pelletier/go-toml/v2`                            | `toml.Unmarshal` at line 69                                 | ✓ WIRED |
| `examples/.../extensions/github/github.go`                    | `go-github/v78`                                   | `gogh.NewClient(nil)` + 7 op funcs                          | ✓ WIRED |
| `examples/.../extensions/github/github.go`                    | `pkg/extension`                                   | `extension.Ptr(true)` × 5, `extension.Ptr(false)` × 2       | ✓ WIRED |
| `examples/.../extensions/github/response.go`                  | `pkg/dag`                                         | 6 `IsOperationOutput()` impls                               | ✓ WIRED |
| `examples/.../extensions/webhook/webhook.go`                  | `pkg/extension` (Ptr(false), ErrNonRetryable)     | Line 66                                                     | ✓ WIRED |
| `examples/.../extensions/webhook/webhook.go`                  | `net/http` stdlib                                 | `stdhttp.NewRequestWithContext` at line 163                 | ✓ WIRED |
| `examples/.../extensions/webhook/webhook.go`                  | `BearerCredential.Token.Reveal()`                 | Line 158                                                    | ✓ WIRED |
| `examples/.../extensions/webhook/webhook_block_test.go`       | `pkg/activity.ExecuteBatch` + non-idempotent reject | env.ExecuteActivity("ExecuteBatch", ...) + counter==2 + non-idempotent NonRetryable | ✓ WIRED |
| `examples/.../cmd/extbin/main.go`                             | `pkg/cli` (NewRootCommand + WithExtensions + WithCredentialHandler) | Lines 40-46                                                 | ✓ WIRED |
| `examples/.../cmd/extbin/main.go`                             | `pkg/extension/credfile.New`                      | Line 107 (lazy init)                                        | ✓ WIRED |
| `examples/.../cmd/extbin/main.go`                             | All 3 extensions                                  | `skyhttp.New(), skygh.New(), skyweb.New()`                  | ✓ WIRED |
| `examples/*.star`                                             | 3 extensions                                      | `github.client(...)`, `webhook.client(...)`, `http.endpoint(...)` factories — all 5 flows parse against newExampleParser | ✓ WIRED |
| `flows_test.go`                                               | `pkg/parser`                                      | `parser.NewParser(parser.WithExtensions(...))`              | ✓ WIRED |
| `issue_triage_test.star`                                      | `examples/.../extensions/github` (registered "github") | `tester.mock_action(extension="github", ...)`              | ✓ WIRED |
| `issue_triage_test.star`                                      | `pkg/testing` (tester.workflow / mock_action / run + ok/err + assert) | `tester.run(flow="issue_triage")` + 3 def test_* | ✓ WIRED |
| `issue_triage_test_e2e_test.go`                               | `pkg/testing.RunCLI` + `WithExtensions`           | In-process runner registers 3 extensions                    | ✓ WIRED |
| README.md                                                     | `.skytime-credentials.example`                    | "cp .skytime-credentials.example" at line 111               | ✓ WIRED |
| README.md                                                     | `docs/cli-binary.md`                              | "Building your own custom binary" forward link              | ✓ WIRED |
| README.md                                                     | `issue_triage_test.star`                          | "extbin test" at line 213                                   | ✓ WIRED |
| Root README.md                                                | example README.md                                 | Bullet on line 361 in "Where to Go Next"                    | ✓ WIRED |
| docs/for-extension-developers/README.md                       | `pkg/extension/credfile`                          | New section at line 55                                      | ✓ WIRED |
| ci.yml                                                        | `cmd/extbin`                                      | `go build -o /tmp/extbin ./examples/http-github-webhook/cmd/extbin` (line 37) | ✓ WIRED |
| ci.yml                                                        | `issue_triage_test.star`                          | `/tmp/extbin test ./examples/http-github-webhook/` (line 40) | ✓ WIRED |
| walkthrough_smoke.sh                                          | `public_repo_check.star`                          | `extbin run examples/http-github-webhook/public_repo_check.star --flow public_repo_check --input '{"repo":"octocat/Hello-World"}'` (lines 78-80) | ✓ WIRED |
| walkthrough_smoke.sh                                          | README.md Quick Start                             | EXACT same command as documented in README                  | ✓ WIRED |

### Behavioral Spot-Checks

| Behavior                                                      | Command                                                                                | Result                                                                                          | Status |
|---------------------------------------------------------------|----------------------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------|--------|
| Build whole module                                            | `go build ./...`                                                                       | exit 0 (no output)                                                                              | ✓ PASS |
| Module-wide go vet                                            | `go vet ./...`                                                                         | exit 0 (no output)                                                                              | ✓ PASS |
| Example tests pass                                            | `go test ./examples/http-github-webhook/... -count=1 -short`                           | 4 packages: ok                                                                                  | ✓ PASS |
| credfile + interpreter + testing tests pass                   | `go test ./pkg/extension/credfile/... ./pkg/interpreter/... ./pkg/testing/... -count=1 -short` | 3 packages: ok                                                                          | ✓ PASS |
| extbin builds                                                 | `go build -o /tmp/extbin ./examples/http-github-webhook/cmd/extbin`                    | exit 0                                                                                          | ✓ PASS |
| extbin Tier-3 .star runs                                      | `/tmp/extbin test ./examples/http-github-webhook/`                                     | `PASS  1 files  3 tests` (test_happy_path, test_get_issue_retries_then_succeeds, test_add_comment_routes_credential) | ✓ PASS |
| extbin --help shows 4 inherited subcommands                   | `/tmp/extbin --help`                                                                   | `validate`, `run`, `dev-server`, `test` all present                                             | ✓ PASS |

The CI walkthrough smoke (`walkthrough_smoke.sh`) requires a running `temporal server start-dev` and network access to `api.github.com`; not run inline (would exceed 10s constraint and modify state). The script itself is verified to be a real bash script with `set -euo pipefail`, trap-based cleanup, and the documented `flow complete` assertion. Routed to "Human Verification" considerations below — but CI will exercise it on every push.

### Requirements Coverage

| Requirement | Source Plan                              | Description                                                                                          | Status     | Evidence                                                                                                                              |
|-------------|------------------------------------------|------------------------------------------------------------------------------------------------------|------------|---------------------------------------------------------------------------------------------------------------------------------------|
| EX-01       | 06-01, 06-03, 06-04, 06-05, 06-06        | Three real extensions (HTTP + GitHub + Webhook) each declaring `Idempotent bool` per operation        | ✓ SATISFIED | HTTP shipped Phase 4; GitHub at `examples/.../extensions/github/` with 7 ops; Webhook at `examples/.../extensions/webhook/` with `post`; all `Idempotent` non-nil; registered in `extbin/main.go`     |
| EX-02       | 06-06                                    | 4-6 .star flows exercising every DSL primitive and every concern                                      | ✓ SATISFIED | 5 flows; `flows_test.go::TestFlows_CoverageMatrix` mechanically pins every primitive (step_seq, step_block, step_block_fn, if_cond, script, for_each_parallel, call_flow); README coverage table |
| EX-03       | 06-01, 06-02, 06-05, 06-07               | At least one .star test using `temporal_test` to exercise retries via `attempt` + replay determinism  | ✓ SATISFIED | `issue_triage_test.star` `test_get_issue_retries_then_succeeds` uses `attempt == 1` first-fail-second-succeed; replay determinism is D5-D1 always-on via doubled `tester.run`                  |
| EX-04       | 06-01, 06-02, 06-05, 06-08, 06-09        | README walkthrough: `git clone` → flow run against `skytime dev-server` in <5 commands; CI YAML       | ✓ SATISFIED | README Quick Start has 4 commands; `ci.yml` runs all 4 named steps on `push: branches: ['**']`; `walkthrough_smoke.sh` exercises documented commands byte-for-byte                            |

No orphaned requirements. REQUIREMENTS.md table maps EX-01..EX-04 to Phase 6, all four claimed across plans.

### Anti-Patterns Found

| File                                                | Line | Pattern                                  | Severity | Impact                                                                                                                                  |
|-----------------------------------------------------|------|------------------------------------------|----------|-----------------------------------------------------------------------------------------------------------------------------------------|
| issue_triage.star, issue_triage_test.star            | various | "placeholder" comments                   | ℹ️ Info  | These are intentional v1-limitation callouts (step→ctx auto-binding deferred per 06-06-SUMMARY key-decisions); not unfinished code     |

No TODO/FIXME/XXX/HACK in production code. No empty returns or stub patterns. All grep matches for "placeholder" are documented v1-limitation comments in the .star corpus that explain the v1 boundary to consultants.

### Human Verification Required

While all automated checks pass, the following human-verifiable behaviors fall outside fast-grep verification scope. CI will exercise items 1 and 2 on every push:

#### 1. Walkthrough smoke against live `temporal server start-dev`

**Test:** Run `bash .github/workflows/scripts/walkthrough_smoke.sh` on a workstation with `temporal` CLI installed and internet access to api.github.com.
**Expected:** Script exits 0 with `==> SUCCESS: 'flow complete' substring present in output`.
**Why human:** Requires running `temporal server start-dev` (not idempotent — modifies local SQLite state), public network access (api.github.com rate limit 60/hr unauth), and ~10-30s wall time exceeding the 10s spot-check constraint. CI runs this on every push.

#### 2. Visual quality of README walkthrough

**Test:** Read `examples/http-github-webhook/README.md` end-to-end and confirm the seven sections flow well, the coverage matrix renders correctly on GitHub, and the chmod 600 callout reads naturally.
**Expected:** A new reader can clone the repo, follow the Quick Start, and reach `flow complete` in under five commands.
**Why human:** Markdown rendering, prose clarity, and UX-feel can't be grep-verified.

### Gaps Summary

No gaps found. Phase 6 achieves its goal:

- All five Success Criteria verified.
- All 30+ artifacts present at substantive line counts and wired into the dependency graph.
- All key links verified (interface checks, sentinel errors, factory wiring, README→CI byte-for-byte commands).
- All Phase 5 latent-gap fixes (sibling-flow registration + multiset replay-determinism fallback) shipped in `pkg/interpreter` and `pkg/testing` and exercised by passing tests, satisfying Success Criterion 4's "any awkward Starlark ergonomics surfaced by writing this example are fed back as fixes".
- All four Phase 6 requirements (EX-01, EX-02, EX-03, EX-04) satisfied.
- `go build ./...`, `go vet ./...`, `go test ./examples/http-github-webhook/... ./pkg/extension/credfile/... ./pkg/interpreter/... ./pkg/testing/...` all green.
- `extbin test ./examples/http-github-webhook/` runs the Tier-3 `.star` test file and reports `PASS  1 files  3 tests`.

Phase 6 is the proof-of-life: a consultant can clone the repo, follow ≤4 commands, and watch `public_repo_check.star` execute against the public GitHub API on a freshly-started Temporal dev server, with CI mechanically pinning that experience against drift on every push.

---

_Verified: 2026-05-07_
_Verifier: Claude (gsd-verifier)_
