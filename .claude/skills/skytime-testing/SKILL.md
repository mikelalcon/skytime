---
name: skytime-testing
description: Use when writing or fixing tests in the Skytime repo, deciding which test layer to run (skytime validate, skytime test, go test, smoke scripts), or when a golden/drift/firewall/differential/replay-determinism test fails and you must decide whether to fix code or regenerate an artifact.
---

# Skytime testing map

All commands run from the repo root. Never hand-edit a generated artifact; every generated file has a regeneration command listed below.

## The layers

| Layer | What | Command |
|---|---|---|
| Tier-1 | Static validation of a .star file (parse + all lints, no Temporal, no I/O) | `skytime validate <file.star>` (build via `go build -o /tmp/skytime ./cmd/skytime`) |
| Go unit/integration | Everything under `pkg/` + cross-tree gates under `tests/` | `go test ./... -count=1` |
| Tier-3 | `*_test.star` files run on `testsuite.TestWorkflowEnvironment` via pkg/testing | `skytime test <dir>` or `extbin test <dir>` |
| Smoke | End-to-end shell scripts against a real `temporal server start-dev` | `bash .github/workflows/scripts/walkthrough_smoke.sh` |

Fast commands:
- Single Go test: `go test ./pkg/parser/ -run 'TestValidFixtures' -count=1`
- Single package: `go test ./pkg/interpreter/ -count=1`
- All cross-tree gates: `go test ./tests/ -count=1` (includes docgen drift, differential corpus, all firewalls, and the subprocess e2es — the e2es build binaries, so this takes minutes) (timing unverified)
- Single Tier-3 test: `skytime test <dir> --run '<file_stem>.<test_name>'` — filter key is DOT-joined (`issue_triage_test.test_happy_path`). The Go-side `t.Run` subtest names use SLASH (`stem/test_name`); a slash regex passed to `--run` matches nothing.
- CI-equivalent locally: `go vet ./... && go test -race ./... -count=1` then the extbin/smoke/snippets steps below.

## Tier-3: every test runs TWICE (replay determinism)

`tester.run` in a `*_test.star` executes the production SkytimeWorkflow twice with identical inputs (pkg/testing/builtin_run.go, D5-D1 always-on). The two event captures are diffed by `FirstDivergentEvent` (pkg/testing/replay_diff.go) after filtering SDK DEBUG slog noise and canonicalizing parallel-branch order (multiset fallback for goroutine jitter).

Two distinct second-run failure shapes — diagnose accordingly:

1. **`tester.run (run 2): workflow error` when run 1 passed.** The `AttemptCounter` is deliberately SHARED across both runs, so run 2 sees attempt numbers continuing from run 1. A mock lambda with non-monotone attempt logic (`attempt == 2`) succeeds once and then fails forever. Fix the mock to be monotone: `attempt == 1` for fail-first, or `attempt >= N`.
2. **`replay diverged` report** (event index, kind, run1/run2 lines, flow callsite, test callsite). The two runs produced different event streams: genuine nondeterminism. Look for unsorted Go map iteration, time/random usage, or a new walker that drops `path`/`pos`/`name` attrs on `step_dispatch`. This is a code bug, never something to "regenerate".

Tier-3 mechanics you must not violate:
- `tester.mock_action(extension=...)` takes the REGISTERED extension `Name()` ("github", "http"), never the local Starlark variable. Wrong name = mock silently never matches = `no mock for <ext.op> at <file:line:col> (step "name")` NonRetryable error (pkg/testing/router.go).
- `tester.run` must be called inside a `def test_*()` body; file scope errors.
- `*_test.star` files are single-file scope: flows under test must be redeclared inline (no cross-file `load()` in the harness — pkg/testing/runner.go).
- Mocking `ExecuteBatch` with testify requires `mock.Anything` exactly twice; a mismatch panics inside the SDK.
- Go-side entry points: `pkg/testing.Run(t, dir, opts)` (runner.go) and `RunCLI` (cli_run.go — the `skytime test`/`extbin test` path, deliberately duplicated so no Go stack frames leak). See examples/http-github-webhook/issue_triage_test_e2e_test.go for the wiring pattern.

## Differential corpus (tests/differential_test.go)

`TestDifferentialCorpus` walks EVERY `.star` under examples/skeleton/ (including `*_test.star`) and asserts the static validator and a dry-run interpretation agree accept/reject, and that rejections are typed errors, never panics (VAL-02). A flow whose dry-run hits a top-level `fail()` under stub inputs must be listed in the `expectedErrFlows` map at the top of the file, or CI breaks. Adding a skeleton fixture = three touch points: the `.star` file, examples/README.md index entry, possibly `expectedErrFlows`/`corpusExtensions`.

## Firewall suite — file by file

Under `tests/` (package `firewall_test`; run all with `go test ./tests/`):
- `firewall_cli_test.go` — `TestNoCobraImportsOutsideAllowList`: only pkg/cli imports cobra/pflag/charm-log/lipgloss; `TestPkgCli_ImportsCobra` is the non-vacuity canary.
- `firewall_credential_redaction_test.go` — no `%+v`/`%#v` in production fmt calls under pkg/dag, pkg/extension (+builtin, +receiver, +core, +schedules), examples/http-github-webhook/extensions/github (Secret redaction).
- `firewall_execute_workflow_test.go` — `c.ExecuteWorkflow` in exactly 2 production files (pkg/cli/server/web/flowlaunch/launch.go, pkg/cli/run.go); `BuildWorkflowInput` at exactly 3 sites. Fails on ADDITIONS too. It exempts receivers literally named `env`; renaming a `TestWorkflowEnvironment` var away from `env` trips it.
- `firewall_source_agnostic_test.go` — no provider webhook header literals (X-GitHub-Delivery, Stripe-Signature, ...) in pkg/cli/server/web non-comment code.
- `firewall_testsuite_test.go` — pkg/testing imports `go.temporal.io/sdk/testsuite` but never `go.temporal.io/sdk/worker`.
- `firewall_web_stdlib_test.go` — `TestNoExternalHTTPTemplateInWeb`: pkg/cli/server/web imports only stdlib + go.temporal.io + protobuf/grpc + own module; requires ≥1 Temporal import as canary.
- `receiver_hmac_only_test.go` — pkg/extension/receiver production code never calls `bytes.Equal` (HMAC must be constant-time `hmac.Equal`).
- `dev_server_grep_test.go` — the literal "dev-server" is banned in every tracked file except .planning/, CHANGELOG.md, and the gate file itself. Even a comment trips it; write "temporal dev server".
- `walkthrough_dashboard_headings_test.go` / `walkthrough_github_webhook_headings_test.go` — H2 heading structure of docs/walkthroughs/dashboard.md and github-webhook.md pinned verbatim (em-dashes and backticks included).

Package-local mirrors (run with the owning package):
- `pkg/activity/firewall_test.go::TestNoTemporalImportsOutsideAllowList` — module-wide: only pkg/{activity,interpreter,worker,cli,testing,extension/receiver,extension/schedules} may import the Temporal SDK. `tests/` lives outside `pkg/` specifically to dodge this — moving a tests/ file under pkg/ creates a violation.
- `pkg/parser/parser_test.go::TestNoTemporalImportsInParserPackage`, `pkg/extension/extension_test.go::TestNoTemporalImportsInExtensionPackage`.
- `pkg/interpreter/firewall_test.go::TestWorkflowcheck_NoFindings` — runs Temporal's `workflowcheck` ONLY if it's on PATH; otherwise silently skips, and .github/workflows/ci.yml does NOT install it. Install locally when touching pkg/interpreter: `go install go.temporal.io/sdk/contrib/tools/workflowcheck@latest`.
- `pkg/bridge/lambda_globals_test.go::TestLambdaTimeGlobalsLocked` / `TestTriggerTimeGlobalsLocked` — the lambda env is locked at exactly 20 keys (22 for triggers). Expanding it is a PROJECT.md-level decision, not a test fix.
- `pkg/interpreter/replay_determinism_test.go` — byte-equal event streams across runs; the reason no Go map is ranged directly in workflow code.

When a firewall fails: the fix is almost always to move/remove YOUR change (the import, the call site, the literal), not to edit the firewall. Firewalls encode locked decisions with D-xx IDs.

## Golden files and regeneration

Three golden layers, three different env vars:

1. **Parser DAG goldens** — `tests/fixtures/valid/*.golden.json`, compared by `pkg/parser/fixtures_test.go::TestValidFixtures` (only fixtures that HAVE a golden are compared).
   Regen: `UPDATE_GOLDEN=1 go test ./pkg/parser/... -run TestValidFixtures`
   Verify: re-run WITHOUT the env var (must pass), then `git diff tests/fixtures/valid/` and confirm every hunk is an intended semantic change. Any byte edit to a fixture changes every lambda ID in that file — goldens deliberately exclude lambda-bearing fields; do not add them.
2. **Dashboard HTML golden** — `pkg/cli/server/web/testdata/dashboard.html.golden` vs rendered `dashboard.html` (templates_test.go).
   Regen: `GSD_UPDATE_GOLDEN=1 go test -run TestTemplate_DashboardGolden ./pkg/cli/server/web`
   Verify: re-run without the env var; `git diff pkg/cli/server/web/testdata/`.
3. **Invalid fixtures** are not goldens: `tests/fixtures/invalid/*.star` carry `# expects: <substring>` headers asserted by `TestInvalidFixtures`. Update the header, not an artifact.

NOT goldens despite the name: `pkg/extension/receiver/testdata/*.golden` are INPUT request scenarios loaded by handler_test.go. There is no update flag; changing response envelopes breaks assertions in handler_test.go directly.

Cross-package coupling: `tests/fixtures/{valid,invalid}` are consumed by tests in `pkg/parser` (fixtures_test.go, load_test.go, block_fn_lint_test.go, interpolation_test.go), not by `tests/` itself. Editing a fixture breaks tests in a DIFFERENT package — run `go test ./pkg/parser/ -count=1` after touching them.

Authoring a new invalid fixture:
1. Create `tests/fixtures/invalid/NN-descriptive-name.star`.
2. First line: `# expects: <substring of the expected error>`. `TestInvalidFixtures` also
   type-asserts the error is `*dag.ParseError`/`*dag.ValidationError` and matches the
   D-04 position format `<file>:<line>:<col>[ [flow > step > action]]: msg` — a raw
   starlark error or panic fails the test regardless of the substring.
3. Verify: `go test ./pkg/parser/ -run TestInvalidFixtures -count=1`.

Decision rule: regenerate only when YOU intentionally changed the source of truth (a fixture, a builtin signature, the dashboard template). If a golden diverges and you didn't touch its inputs, you introduced a behavior change — investigate first.

## Drift tests

- **Docgen** (`tests/docgen_drift_test.go::TestDocgenDrift`): docs/reference/builtins.md must byte-match a fresh `cmd/skytime-docgen` run over pkg/parser. After touching any builtin signature or `// skytime:doc` marker: `go generate ./pkg/parser/` then commit builtins.md. Verify: `go test ./tests/ -run TestDocgenDrift -count=1`. Never Edit builtins.md by hand — the "do not edit" banner is not enforced at edit time, only at test time.
- **Auth snippets** (`docs/for-extension-developers/snippets/drift_test.go::TestMarkdownSnippetDrift`): each ```go fence in docs/for-extension-developers/temporal-auth.md (marked `<!-- snippet: X.go -->`) must equal snippets/X.go after TrimSpace only — internal whitespace byte-exact. Edit both sides together. Verify: `cd docs/for-extension-developers/snippets && go test -count=1 ./...` (standalone Go module; must never require the main skytime module).

## Smoke scripts

- `.github/workflows/scripts/walkthrough_smoke.sh` — runs in CI. Needs `temporal` CLI + internet access to api.github.com. Mirrors examples/http-github-webhook/README.md Quick Start verbatim (drift is a CI failure). Greps for 'flow complete' on STDERR.
- `docs/walkthroughs/cron-schedules-smoke.sh` — ~80s; only runs when `SKYTIME_RUN_CRON_SMOKE=1` (gated inside walkthrough_smoke.sh).
- `docs/walkthroughs/dashboard-smoke.sh` — self-gated: `SKYTIME_RUN_DASHBOARD_SMOKE=1 bash docs/walkthroughs/dashboard-smoke.sh`, requires a running `temporal server start-dev` on :7233. Default invocation is a no-op.

## CI sequence (.github/workflows/ci.yml — runs on push to any branch)

1. Setup Go 1.25 + Temporal CLI (`temporalio/setup-temporal`)
2. `go vet ./...`
3. `go test -race ./... -count=1` — the whole tree under -race, including tests/ e2es (Temporal CLI is present in CI, so they actually run)
4. `go build -o /tmp/extbin ./examples/http-github-webhook/cmd/extbin`
5. `/tmp/extbin test ./examples/http-github-webhook/` — Tier-3 .star tests as a SEPARATE step from go test; picks up any `*_test.star` under the example dir recursively
6. `bash .github/workflows/scripts/walkthrough_smoke.sh` (public GitHub API)
7. snippets module: `go mod download && go build ./... && go vet ./... && go test -count=1 ./...` in docs/for-extension-developers/snippets

Not in CI: `workflowcheck` (skips silently), `go test -tags=integration ./pkg/worker/...` (embed_integration_test.go — also skips without a live localhost:7233). Green CI does not prove either ran.

-race expectations: always run at least your touched packages with `-race` before pushing — walkForEach parallel-branch tests (pkg/interpreter/walk_foreach_test.go) are the usual place local non-race runs pass and CI fails.

Subprocess e2e skip semantics (tests/e2e_skytime_run_test.go): TestMain spawns and reaps a
`temporal server start-dev` via process-group kill, REUSES an already-running server on
127.0.0.1:7233 without owning it, and SKIPS (not fails) when the `temporal` CLI or
api.github.com is unavailable. Locally, a green `go test ./tests/` may mean the e2e never
ran — check the skip lines in verbose output. `tests/skytime_test_e2e_test.go` is hermetic
(TestWorkflowEnvironment in-process, no server needed) and always runs.

## Where a new test belongs

- Parser/DAG behavior → colocated `_test.go` in pkg/parser; new syntax accepted/rejected → a
  fixture in tests/fixtures/{valid,invalid} (see authoring steps above).
- Interpreter/walker behavior → pkg/interpreter, using `RunOnceCapturing`/`EventCapture`
  (replay_helper.go) — the public replay harness. Any new early return in a walker must
  assign the named `err` return or the step_complete event lies.
- Activity/extension behavior → pkg/activity or pkg/extension. Remember `tests/` exists
  precisely because it sits OUTSIDE pkg/ and its Temporal-import firewall; cross-tree tests
  that need the SDK plus multiple pkg/ trees go in `tests/` (package `firewall_test`).
- Flow-author-visible behavior → a Tier-3 `*_test.star` next to the flow (see
  examples/http-github-webhook/issue_triage_test.star and
  docs/for-flow-authors/testing-tutorial.md), driven by a thin Go runner like
  examples/http-github-webhook/issue_triage_test_e2e_test.go. CI's `extbin test` step picks
  up ANY new `*_test.star` under the example dir recursively — a broken fixture fails a
  separate CI step from `go test ./...`.
- New repo-wide invariant → a firewall test in `tests/` with a non-vacuity canary (copy the
  pattern in tests/firewall_web_stdlib_test.go: allow-list + a positive assertion that the
  gate still sees the thing it protects).

## Common mistakes

- **TestValidFixtures fails after editing a .star fixture** → expected; regenerate goldens (`UPDATE_GOLDEN=1 ...`). **Fails after a parser code change with untouched fixtures** → behavior change; diff before regenerating.
- **TestDocgenDrift fails** with "Run `go generate ./pkg/parser/`" → someone changed a builtin/marker without regenerating, or hand-edited builtins.md. Regenerate, never hand-fix the diff.
- **`no mock for github.get_issue at ...` in a Tier-3 test** → the `extension=` kwarg names a Starlark variable instead of the registered extension Name(), or your mock's kwarg regex doesn't match (only string kwargs participate in regex matching).
- **`skytime test --run` matches nothing** → you used the `stem/test` slash form; the CLI filter is `stem.test` with a dot.
- **A firewall test fails after adding an innocent import/call** → firewalls gate additions: new `ExecuteWorkflow` call sites, new third-party imports under pkg/cli/server/web, cobra outside pkg/cli, temporal SDK outside the allowlist. Relocate the code; don't extend the allowlist without a design decision.
- **TestNoDevServerLiteralRemains fails on a docs/comment edit** → you typed "dev-server". It bit a shell-script comment once. Rephrase.
- **Walkthrough headings test fails on a docs edit** → you reworded/reordered an H2 in dashboard.md or github-webhook.md. Headings are pinned verbatim; restore them.
- **`fail`-ing skeleton fixture breaks TestDifferentialCorpus** → add the flow name to `expectedErrFlows` in tests/differential_test.go (only if the fail() under stub inputs is intended).
- **Tier-3 test passes solo but `replay diverged` intermittently** → nondeterminism, usually map iteration or attempt-keyed mock logic; see the replay section. Do not retry until green.
- **skytime_test_e2e gate fails on ".go:" substring** → your harness change leaked a Go file path into `skytime test` human output (CLI-03). Route the error through the Starlark-callsite renderer.
- **Test asserting activity retry behavior can't set Attempt** → `TestActivityEnvironment` hardcodes Attempt=1; use the unexported `withAttemptFunc` seam in pkg/activity, not the SDK test env.
