---
phase: 7
slug: trigger-primitive-server-shell
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-08
---

# Phase 7 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `github.com/stretchr/testify` (`require` for fail-fast preconditions, `assert` for accumulating checks) |
| **Config file** | none — `go test` defaults |
| **Quick run command** | `go test ./pkg/parser/... ./pkg/dag/... ./pkg/extension/... -count=1 -race` |
| **Full suite command** | `go test ./... -count=1 -race` |
| **Estimated runtime** | quick ~5s, full ~30s |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/parser/... ./pkg/dag/... ./pkg/extension/... -count=1 -race`
- **After every plan wave:** Run `go test ./... -count=1 -race`
- **Before `/gsd:verify-work`:** Full suite must be green + manual smoke (`go run ./cmd/skytime server --rootdir=examples/http-github-webhook/ --task-queue=demo --temporal=localhost:7233` against a running `temporal server start-dev`; SIGTERM; verify exit code 0 and sorted-banner output)
- **Max feedback latency:** ~5 seconds (per-task quick run); ~30 seconds (per-wave full suite)

---

## Per-Task Verification Map

| Req ID | Behavior | Test Type | Automated Command | Wave 0 / Existing | Status |
|--------|----------|-----------|-------------------|-------------------|--------|
| TRIG-01 | `trigger(...)` parses without I/O | Tier 1 unit | `go test ./pkg/parser/ -run TestBuiltinTrigger -count=1` | ❌ W0 — `pkg/parser/trigger_test.go` | ⬜ pending |
| TRIG-01 | Captured Trigger has correct fields | Tier 2 unit | `go test ./pkg/parser/ -run TestBuiltinTrigger_Fields` | ❌ W0 | ⬜ pending |
| TRIG-02 | Extensions return TriggerSource opaquely | Tier 2 unit | `go test ./pkg/extension/ -run TestTriggerSource_Sealed` | ❌ W0 — `pkg/extension/trigger_test.go` | ⬜ pending |
| TRIG-03 | `dag.Trigger` JSON round-trip (no Pos, no secrets) | Tier 2 unit | `go test ./pkg/dag/ -run TestTrigger_MarshalRoundTrip` | ❌ W0 — `pkg/dag/trigger_test.go` | ⬜ pending |
| TRIG-03 | `Trigger.Freeze()` recursion | Tier 2 unit | `go test ./pkg/dag/ -run TestTrigger_Freeze` | ❌ W0 | ⬜ pending |
| TRIG-04 | Unknown flow → position-aware error | Tier 1 unit | `go test ./pkg/parser/ -run TestTrigger_UnknownFlow` | ❌ W0 | ⬜ pending |
| TRIG-04 | Source not a TriggerSource → error | Tier 1 unit | `go test ./pkg/parser/ -run TestTrigger_BadSource` | ❌ W0 | ⬜ pending |
| TRIG-04 | `req.<field>` typo → error with valid-list | Tier 1 unit | `go test ./pkg/parser/ -run TestTrigger_ReqAttrTypo` | ❌ W0 | ⬜ pending |
| TRIG-04 | Lambda arity wrong → error | Tier 1 unit | `go test ./pkg/parser/ -run TestTrigger_BadArity` | ❌ W0 | ⬜ pending |
| TRIG-04 | Mutable closure → existing free-var lint surfaces | Tier 1 unit | `go test ./pkg/parser/ -run TestTrigger_MutableClosure` | ❌ W0 | ⬜ pending |
| TRIG-05 | `bootRegistry` registers flows + triggers | Tier 2 integration | `go test ./pkg/worker/ -run TestBootRegistry_RegistersTriggers` | ❌ W0 | ⬜ pending |
| TRIG-05 | `*_test.star` skipped from production registration | Tier 2 integration | `go test ./pkg/worker/ -run TestBootRegistry_SkipsTestFiles` | ✅ existing test extends | ⬜ pending |
| SERVER-01 | `server` subcommand registers flags | Tier 2 unit | `go test ./pkg/cli/ -run TestServerCmd_Flags` | ❌ W0 — `pkg/cli/server_test.go` | ⬜ pending |
| SERVER-01 | `server` uses `connectClient` D4-08 routing | Tier 2 unit | `go test ./pkg/cli/ -run TestServerCmd_ConnectClient` | ❌ W0 | ⬜ pending |
| SERVER-02 | Drain on SIGTERM completes within timeout | Tier 2 behavioral via `testDrainHook` | `go test ./pkg/cli/ -run TestServerCmd_DrainOnSIGTERM` | ❌ W0 | ⬜ pending |
| SERVER-02 | `--drain-timeout` expiry → exit 1 + log | Tier 2 behavioral via `testDrainHook` | `go test ./pkg/cli/ -run TestServerCmd_DrainTimeoutExpiry` | ❌ W0 | ⬜ pending |
| SERVER-02 | Second signal forces immediate exit | Tier 2 behavioral via `testDrainHook` | `go test ./pkg/cli/ -run TestServerCmd_SecondSignalForceExit` | ❌ W0 | ⬜ pending |
| SERVER-02 | `--drain-timeout=0` rejected at flag-parse | Tier 2 unit | `go test ./pkg/cli/ -run TestServerCmd_DrainTimeoutRangeCheck` | ❌ W0 | ⬜ pending |
| SERVER-03 | Startup banner sorted lexicographically | Tier 2 unit (slog buffer capture) | `go test ./pkg/cli/ -run TestServerCmd_BannerSorted` | ❌ W0 | ⬜ pending |
| SERVER-03 | `--json-log` emits JSON | Tier 2 unit | `go test ./pkg/cli/ -run TestServerCmd_JSONLog` | ❌ W0 | ⬜ pending |
| CLI-13 | `dev-temporal` subcommand exists | Tier 2 unit | `go test ./pkg/cli/ -run TestRoot_HasDevTemporalSubcommand` | ✅ existing `root_test.go` updates | ⬜ pending |
| CLI-13 | No tracked file contains `dev-server` literal | Tier 2 firewall | `go test ./tests/ -run TestNoDevServerLiteralRemains` | ❌ W0 — D-07-22 grep gate | ⬜ pending |
| CLI-13 | docgen drift test passes after rename | Tier 2 firewall | `go test ./tests/ -run TestDocgenDrift` | ✅ existing — runs `go generate ./...` then compares | ⬜ pending |
| D-07-10 | Credential-redaction firewall (AST-walking %+v / %#v) | Tier 2 firewall | `go test ./tests/ -run TestCredentialRedactionFirewall` | ❌ W0 — `tests/firewall_credential_redaction_test.go` | ⬜ pending |
| D-07-12 | Cross-file `trigger.FlowName` resolution at finalize | Tier 1 unit | `go test ./pkg/parser/ -run TestTrigger_CrossFileFlow` | ❌ W0 | ⬜ pending |
| D-07-13 | Byte-identical duplicate triggers → warning, not error | Tier 1 unit | `go test ./pkg/parser/ -run TestTrigger_DuplicateWarn` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

These files / fixtures must exist (or be created during the first wave) before later waves' tests can run:

- [ ] `pkg/parser/trigger_test.go` — covers TRIG-01, TRIG-04 (unknown flow, bad source, mutable closure, bad arity)
- [ ] `pkg/parser/req_walk.go` + `pkg/parser/req_walk_test.go` — generalized req-attribute walker (refactor of `ctx_walk.go` to a parameterized free-var name + valid-attributes visitor)
- [ ] `pkg/parser/testdata/triggers/` corpus:
  - [ ] `valid.star` — clean trigger; req.payload + req.headers
  - [ ] `typo.star` — req.payloud.foo (errors with valid-field list)
  - [ ] `bad_arity.star` — `lambda req, headers: ...` (errors at parse)
  - [ ] `unknown_flow.star` — `trigger(flow="missing", ...)` (errors at parse-finalize)
  - [ ] `mutable_closure.star` — Phase 1 free-var lint catches; reused
  - [ ] `not_a_source.star` — `trigger(source="just a string", ...)` (errors with "expected TriggerSource")
  - [ ] `cross_file_flow.star` + `cross_file_trigger.star` — flow + trigger in different files
  - [ ] `duplicate_warn.star` — two byte-identical triggers (warning only)
- [ ] `pkg/dag/trigger.go` + `pkg/dag/trigger_test.go` — covers TRIG-03 (round-trip + Freeze recursion + position-stable JSON)
- [ ] `pkg/extension/trigger.go` + `pkg/extension/trigger_test.go` — covers TRIG-02 (sealed marker, JSON unmarshal-registry shape)
- [ ] `pkg/extension/testing/triggersource.go` (or per-package duplicate) — `fakeTriggerSource` test stub reusable across `pkg/parser`, `pkg/dag`, `pkg/worker` tests
- [ ] `pkg/interpreter/registry_test.go` extensions — TriggerRegistry concurrency, frozen-after-boot, sorted iteration
- [ ] `pkg/worker/boot_test.go` extensions — `bootRegistry` returns both registries; trigger files counted; `*_test.star` exclusion preserved
- [ ] `pkg/cli/server.go` + `pkg/cli/server_test.go` — covers SERVER-01..03 (flags, signal-loop via `testDrainHook`, banner format, JSON log)
- [ ] `tests/firewall_credential_redaction_test.go` — covers D-07-10 (AST walker rejecting %+v / %#v on Trigger / TriggerSource)
- [ ] `tests/dev_server_grep_test.go` — covers D-07-22 (zero `dev-server` literal references after rename, with allow-list for `.planning/`, git history, CHANGELOG)
- [ ] Integration smoke (Wave 4 / phase gate, not per-wave): `go run ./cmd/skytime server` against `temporal server start-dev`, send SIGTERM, assert exit code + log output. Skip when `temporal` CLI absent on PATH.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `skytime server` end-to-end smoke (real Temporal) | SERVER-01, SERVER-03 | Requires `temporal` CLI on PATH; CI installs it via `go install go.temporal.io/cli/cmd/temporal@latest` for the existing walkthrough smoke; can be reused | `temporal server start-dev &` then `go run ./cmd/skytime server --rootdir=examples/http-github-webhook/ --task-queue=demo --temporal=localhost:7233` and confirm sorted banner; SIGTERM and confirm clean exit |
| `--drain-timeout` durability proof | SERVER-02 | Requires submitting a workflow with a deliberately slow activity; can be wired in tests but the "watch Temporal preserve state across restart" UX is most easily seen by hand | Submit slow workflow via `skytime run`; SIGTERM `skytime server` mid-execution; restart; check Temporal Web UI shows workflow resuming |
| Charm-log Bazel-style banner formatting visual | SERVER-03 | Pretty output is ANSI-colored; automated tests assert structure but not aesthetic quality | Run server and visually confirm `[skytime] registered N flows: [...]` lines render with `[skytime]` prefix and sorted names |

---

## Validation Sign-Off

- [ ] All tasks have `<acceptance_criteria>` mapped to a Wave 0 file or an existing test
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references in the table above
- [ ] No watch-mode flags in any test command
- [ ] Feedback latency < 30s per wave
- [ ] `nyquist_compliant: true` set in frontmatter once all Wave 0 files exist and the per-task table has all-✅

**Approval:** pending
