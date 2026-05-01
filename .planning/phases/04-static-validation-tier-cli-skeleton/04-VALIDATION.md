---
phase: 4
slug: static-validation-tier-cli-skeleton
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-01
---

# Phase 4 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `github.com/stretchr/testify` v1.11.1 (already in `go.mod`) |
| **Config file** | none — Go discovers `*_test.go` automatically |
| **Quick run command** | `go test ./pkg/validator/... ./pkg/cli/... ./pkg/parser/... -run TestXxx -count=1` |
| **Full suite command** | `go test ./... -race -count=1` |
| **Estimated runtime** | Quick: ~5–10 s · Full: ~30–60 s (testsuite-driven `skytime run` test is the bottleneck) |

---

## Sampling Rate

- **After every task commit:** `go test ./<package-touched>/... -count=1` (matches existing per-task atomic-commit convention)
- **After every plan wave:** `go test ./pkg/... ./cmd/... -race -count=1`
- **Before `/gsd:verify-work`:** `go test ./... -race -count=1` must be green; `TestDifferentialCorpus` is the load-bearing assertion for VAL-02.
- **Max feedback latency:** ~10 s on the per-task command; ~60 s on the full suite.

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| 04-01-01 | 01 (deps + firewall) | 0 | CLI-05 | meta | `go test ./tests -run TestNoCobraImportsOutsideAllowList -count=1` | ❌ W0 | ⬜ pending |
| 04-01-02 | 01 (deps + firewall) | 0 | CLI-05 | meta | `go test ./pkg/cli -run TestPkgCli_ImportsCobra -count=1` | ❌ W0 | ⬜ pending |
| 04-01-03 | 01 (ValidationError) | 0 | VAL-03 | unit | `go test ./pkg/dag -run TestValidationError_FormatWithAction -count=1` | ❌ W0 | ⬜ pending |
| 04-01-04 | 01 (corpus skeleton) | 0 | VAL-02 | integration | `go test ./pkg/validator -run TestDifferentialCorpus -count=1` (skips if no corpus yet — populated W4) | ❌ W0 | ⬜ pending |
| 04-02-01 | 02 (ctx visitor) | 1 | VAL-01 | unit | `go test ./pkg/parser -run TestCtxWalk_FindsAttrAccesses -count=1` | ❌ W0 | ⬜ pending |
| 04-02-02 | 02 (state schema accumulator) | 1 | VAL-01 | unit | `go test ./pkg/parser -run TestStateSchema_AccumulatesScopes -count=1` | ❌ W0 | ⬜ pending |
| 04-02-03 | 02 (finalize wiring) | 1 | VAL-01 | unit | `go test ./pkg/parser -run TestFinalize_CtxAccess_Valid -count=1` | ❌ W0 | ⬜ pending |
| 04-02-04 | 02 (finalize negative) | 1 | VAL-01 | unit | `go test ./pkg/parser -run TestFinalize_CtxAccess_RejectsUnknown -count=1` | ❌ W0 | ⬜ pending |
| 04-02-05 | 02 (kwarg cross-validate) | 1 | VAL-01 | unit | `go test ./pkg/parser -run TestFinalize_KwargCrossValidate -count=1` | ❌ W0 | ⬜ pending |
| 04-03-01 | 03 (validator facade) | 2 | VAL-01 | unit | `go test ./pkg/validator -run TestValidate_ReturnsTypedErrors -count=1` | ❌ W0 | ⬜ pending |
| 04-03-02 | 03 (dry-run dispatch) | 2 | VAL-02 | unit | `go test ./pkg/validator/internal/dryrun -run TestAlwaysOkDispatch -count=1` | ❌ W0 | ⬜ pending |
| 04-03-03 | 03 (differential test) | 2 | VAL-02 | integration | `go test ./pkg/validator -run TestDifferentialCorpus -count=1` | ❌ W0 | ⬜ pending |
| 04-04-01 | 04 (cli root + flags) | 3 | CLI-05 | unit | `go test ./pkg/cli -run TestRootCommand_FlagsRegistered -count=1` | ❌ W0 | ⬜ pending |
| 04-04-02 | 04 (renderer) | 3 | VAL-03 | unit | `go test ./pkg/cli -run TestRenderer_StarlarkFirst -count=1` | ❌ W0 | ⬜ pending |
| 04-04-03 | 04 (renderer --debug) | 3 | VAL-03 | unit | `go test ./pkg/cli -run TestRenderer_DebugUnwrapsChain -count=1` | ❌ W0 | ⬜ pending |
| 04-04-04 | 04 (validate cmd) | 3 | CLI-01 | integration | `go test ./pkg/cli -run TestValidateCmd_HappyPath -count=1` | ❌ W0 | ⬜ pending |
| 04-04-05 | 04 (validate cmd negative) | 3 | CLI-01 | integration | `go test ./pkg/cli -run TestValidateCmd_ExitNonZeroOnError -count=1` | ❌ W0 | ⬜ pending |
| 04-05-01 | 05 (run cmd connection routing) | 4 | CLI-02 | unit | `go test ./pkg/cli -run TestConnectClient_VariantRouting -count=1` | ❌ W0 | ⬜ pending |
| 04-05-02 | 05 (run cmd e2e) | 4 | CLI-02 | integration | `go test ./pkg/cli -run TestRunCmd_EndToEnd -count=1` | ❌ W0 | ⬜ pending |
| 04-05-03 | 05 (run cmd input schema) | 4 | CLI-02 | integration | `go test ./pkg/cli -run TestRunCmd_InputSchemaCheck -count=1` | ❌ W0 | ⬜ pending |
| 04-05-04 | 05 (slog progress streaming) | 4 | CLI-02 | unit | `go test ./pkg/cli -run TestSlogProgress_RendersStepEvents -count=1` | ❌ W0 | ⬜ pending |
| 04-06-01 | 06 (dev-server spawn) | 4 | CLI-04 | integration | `go test ./pkg/cli -run TestDevServerCmd_Spawn -count=1` (skips if `temporal` missing) | ❌ W0 | ⬜ pending |
| 04-06-02 | 06 (dev-server missing binary) | 4 | CLI-04 | unit | `go test ./pkg/cli -run TestDevServerCmd_MissingBinary -count=1` | ❌ W0 | ⬜ pending |
| 04-06-03 | 06 (dev-server signal forward) | 4 | CLI-04 | integration | `go test ./pkg/cli -run TestDevServerCmd_SignalForward -count=1` (uses `sleep` fake on Unix; skips on Windows) | ❌ W0 | ⬜ pending |
| 04-07-01 | 07 (HTTP extension) | 4 | CLI-02 / EX-01 prelim | unit | `go test ./pkg/extension/builtin/http -count=1` | ❌ W0 | ⬜ pending |
| 04-07-02 | 07 (examples/skeleton corpus) | 4 | VAL-02 | integration | `go test ./pkg/validator -run TestDifferentialCorpus -count=1` | ❌ W0 | ⬜ pending |
| 04-07-03 | 07 (docs/cli-binary.md) | 4 | CLI-05 | meta | `test -f docs/cli-binary.md && grep -q 'pkg/cli' docs/cli-binary.md` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

*Task IDs are nominal — the planner will set authoritative IDs in the PLAN.md frontmatter and tasks. The map above mirrors the wave breakdown the researcher proposed: W0 deps + firewall + ValidationError.Action + corpus skeleton; W1 ctx visitor + finalize lints; W2 validator facade + differential corpus test; W3 cli root + renderer + validate; W4 run + dev-server + HTTP extension + corpus + docs.*

---

## Wave 0 Requirements

- [ ] `pkg/cli/` — entire package (root.go, validate.go, run.go, dev_server.go, render.go, options.go, flags.go, *_test.go) — created in W3/W4 but the package directory must exist before the firewall test can pass.
- [ ] `pkg/validator/` — entire package (validator.go, options.go, dryrun_test.go, internal/dryrun/dispatch.go) — created in W2.
- [ ] `pkg/parser/ctx_walk.go` + `pkg/parser/state_schema.go` — D4-02 implementation, created in W1.
- [ ] `pkg/parser/finalize.go` — fill `validateActionRefKwargs` + add `validateLambdaCtxAccesses` to pass list (W1).
- [ ] `pkg/parser/parser.go` — add `Parser.FileBytes()` accessor for re-parse (W0/W1; the AST visitor needs cached source bytes per the research's load-bearing finding).
- [ ] `pkg/dag/errors.go` — add `Action string` field to `ValidationError` + update `Error()` format to render `[flow > step > action]` when Action is non-empty (W0 — task 04-01-03).
- [ ] `pkg/dag/errors_test.go` — extend `TestValidationError_Format` for Action variants (W0 task).
- [ ] `pkg/extension/builtin/http/` — entire baked-in HTTP extension (W4 — exact path is Claude's discretion during planning).
- [ ] `cmd/skytime/main.go` + `cmd/skytime/build_id.go` — binary entry point + `defaultBuildID` ldflag anchor (W3/W4).
- [ ] `examples/skeleton/simple_check.star` + `examples/skeleton/parallel_fanout.star` — D4-17 differential corpus (W4 — `TestDifferentialCorpus` exercises these).
- [ ] `tests/firewall_cli_test.go` (or extend `pkg/activity/firewall_test.go`) — D4-13 cobra/pflag/charmlog firewall (W0 — task 04-01-01).
- [ ] `docs/cli-binary.md` — referenced by the D4-16 hint (W4 — task 04-07-03).
- [ ] Module deps: `go get github.com/spf13/cobra@v1.10.2 github.com/charmbracelet/log/v2@v2.0.0 && go mod tidy` (W0 — task 04-01-01 prereq).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `skytime dev-server` end-to-end against a real `temporal` install | CLI-04 | Cross-platform install + UI port + actual Temporal server lifecycle is heavyweight to script in CI; CI exercises subprocess plumbing only. | 1) Install Temporal CLI per docs. 2) `skytime dev-server` — observe `:7233` and `:8233` come up. 3) In another terminal `skytime validate examples/skeleton/simple_check.star`. 4) `skytime run examples/skeleton/simple_check.star --flow=simple_check --input='{"limit":5}'` — observe per-step progress and final state. 5) Ctrl-C the dev-server; observe clean exit. |
| Charm-log color rendering on a real TTY | VAL-03 / D4-18 | TTY detection is verified by unit test, but the actual color output is for human consumption and only renders on a real terminal. | Run `skytime validate examples/skeleton/bad_flow.star` (the negative fixture) on a TTY and confirm errors appear with the charm-log color theme; on a piped invocation (`skytime ... | cat`) confirm plain output. |
| `--debug` reveals Wrapped chains | VAL-03 / D4-19 | Output diff is asserted by unit tests; manual smoke confirms human-readable rendering. | `skytime validate <bad>.star --debug` — confirm Wrapped chain visible; without `--debug`, only the Starlark-first message appears. |
| Phase 6 README's "git clone to executed flow in <5 commands" walkthrough | (forward to Phase 6) | Whole-system check that depends on Phase 6 deliverables; out of Phase 4's automated scope. | Will be tested in Phase 6's verification pass. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies (W0 task 04-01-04 explicitly skips when corpus is empty)
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify (every task above has a `go test` invocation)
- [ ] Wave 0 covers all MISSING references (cobra/charmlog deps, pkg/cli skeleton, pkg/validator skeleton, examples/skeleton/, docs/cli-binary.md, ValidationError.Action retrofit)
- [ ] No watch-mode flags (`go test` is one-shot by default)
- [ ] Feedback latency < 10 s for per-task; < 60 s for full suite
- [ ] `nyquist_compliant: true` set in frontmatter (flip when planner finalizes the IDs and the W0 file list lands)

**Approval:** pending — orchestrator advances to planning; flip to `approved YYYY-MM-DD` once gsd-planner produces final PLAN.md files and gsd-plan-checker confirms every requirement maps to a green-or-Wave-0 test.
