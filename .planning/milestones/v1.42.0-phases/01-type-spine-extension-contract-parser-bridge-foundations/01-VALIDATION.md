---
phase: 1
slug: type-spine-extension-contract-parser-bridge-foundations
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-04-26
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `github.com/stretchr/testify@v1.11.1` |
| **Config file** | `go.mod` (test deps inline); no separate config |
| **Quick run command** | `go test ./pkg/...` (~5s for unit tests in any single package) |
| **Full suite command** | `go test ./... -race -count=1` (race detector required for bridge tests; activity/test code only — workflow code is single-threaded) |
| **Phase gate** | Full suite green + `go vet ./...` clean |
| **Estimated runtime** | ~30s full suite, <5s per-package |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/{package-touched}/...` (<5s)
- **After every plan wave:** Run `go test ./pkg/... -race` (<30s)
- **Before `/gsd:verify-work`:** Full suite must be green: `go test ./... -race -count=1` and `go vet ./...`
- **Max feedback latency:** 30 seconds

---

## Per-Task Verification Map

| Req ID | Behavior | Test Type | Automated Command | File Exists | Status |
|--------|----------|-----------|-------------------|-------------|--------|
| **DSL-01** | `flow(name=..., inputs=..., steps=[...])` produces `*dag.Flow` | unit + golden | `go test ./pkg/parser -run TestParseFlow_DSL01` | ❌ W0 | ⬜ pending |
| **DSL-02** | `step(action=...)` produces `*dag.Step` with one `ActionRef` | unit + fixture | `go test ./pkg/parser -run TestStep_SingleAction` | ❌ W0 | ⬜ pending |
| **DSL-03** | `step(block=[a, b, c])` produces `*dag.Step` with multiple `ActionRef`s | unit + fixture | `go test ./pkg/parser -run TestStep_Block` | ❌ W0 | ⬜ pending |
| **DSL-04** | `if_cond(cond=lambda, then=[...], else_=[...])` produces `*dag.IfCond` with `CapturedLambda` | unit + fixture | `go test ./pkg/parser -run TestIfCond_LambdaCapture` | ❌ W0 | ⬜ pending |
| **DSL-05** | `script(id=..., fn=lambda, output_alias=...)` produces `*dag.Script` with `CapturedLambda` | unit + fixture | `go test ./pkg/parser -run TestScript_LambdaCapture` | ❌ W0 | ⬜ pending |
| **DSL-06** | `for_each_parallel(items=..., item=..., steps=[...])` accepts list and lambda producer | unit + fixture | `go test ./pkg/parser -run TestForEachParallel_BothItemForms` | ❌ W0 | ⬜ pending |
| **DSL-07** | `call_flow(name=..., inputs=..., child_options=...)` resolves by name at parse time | unit + fixture | `go test ./pkg/parser -run TestCallFlow_NameResolution` | ❌ W0 | ⬜ pending |
| **DSL-08** | `step(retry=..., timeout=...)` carries Temporal kwargs as pure data | unit | `go test ./pkg/dag -run TestRetryPolicy_Unpack` | ❌ W0 | ⬜ pending |
| **DSL-09** | `bridge.ToStarlarkStruct` produces deterministic key-order struct | unit (iter-determinism) | `go test ./pkg/bridge -run TestToStarlarkStruct_Deterministic` | ❌ W0 | ⬜ pending |
| **DSL-10** | `resolve.AllowLambda == true` after parser package init | unit | `go test ./pkg/parser -run TestResolveAllowLambdaIsSet` | ❌ W0 | ⬜ pending |
| **EXT-01** | `Extension` interface compiles with `Name`, `Initialize`, `Operations` | compile + unit | `go build ./pkg/extension && go test ./pkg/extension -run TestExtensionInterface` | ❌ W0 | ⬜ pending |
| **EXT-02** | Extension factory in Starlark returns `*ActionRef`, never executes I/O | unit (fake extension) | `go test ./pkg/parser -run TestExtensionFactory_ReturnsActionRef` | ❌ W0 | ⬜ pending |
| **EXT-03** | `OperationFunc` signature uses `context.Context`; lint enforces no `workflow.Context` import | compile + lint | `go vet ./pkg/extension` (lint comes Phase 4) | ❌ W0 | ⬜ pending |
| **EXT-04** | Registration fails if any operation lacks `Idempotent` | unit | `go test ./pkg/extension -run TestRegistration_RequiresIdempotent` | ❌ W0 | ⬜ pending |
| **EXT-05** | `Credential` interface is sealed; `String()` is redacted | unit | `go test ./pkg/extension -run TestCredential_RedactedString` | ❌ W0 | ⬜ pending |
| **EXT-06** | Extensions register statically and dynamically via `parser.Register(...)` | unit | `go test ./pkg/parser -run TestRegistration_StaticAndDynamic` | ❌ W0 | ⬜ pending |
| **PARSE-01** | All six DSL primitives are naked globals (not namespaced) in `parseTimeGlobals` | unit | `go test ./pkg/parser -run TestParseTimeGlobals_NakedPrimitives` | ❌ W0 | ⬜ pending |
| **PARSE-02** | `load()` resolves relative + absolute, sandboxed to root, rejects traversal | fixture-based | `go test ./pkg/parser -run TestLoad_SandboxedResolution` | ❌ W0 | ⬜ pending |
| **PARSE-03** | Two-environment split: parse-time and lambda-time globals are distinct dicts | unit | `go test ./pkg/parser -run TestParseAndLambdaGlobalsAreDistinct` and `go test ./pkg/bridge -run TestLambdaTimeGlobalsLocked` | ❌ W0 | ⬜ pending |
| **PARSE-04** | Lambda capture stores `*starlark.Function` with stable ID + `syntax.Position` | unit + property | `go test ./pkg/parser -run TestLambdaCapture_StableID` | ❌ W0 | ⬜ pending |
| **PARSE-05** | Malformed file produces `*ParseError` with `<file>:<line>:<col>: <msg>`; never panics | fixture-based | `go test ./pkg/parser -run TestInvalidFixtures` | ❌ W0 | ⬜ pending |
| **PARSE-06** | `bridge.CallLambda` uses fresh thread, sets `MaxExecutionSteps`, `Print` hook | unit | `go test ./pkg/bridge -run TestCallLambda_FreshThread` and `TestCallLambda_PrintHookRouted` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

**Toolchain:**
- [ ] Verify Go 1.25+ installed (`go version`); install/upgrade if local toolchain is below 1.25 (current local: 1.21.0 — must upgrade)
- [ ] Initialize `go.mod` declaring `module github.com/mikelalcon/skytime`, `go 1.25`
- [ ] `go mod tidy` after first commits to pin `go.starlark.net@latest` and `github.com/stretchr/testify@v1.11.1`

**Test fixtures (`tests/fixtures/`):**
- [ ] `tests/fixtures/valid/01-minimal-flow.star` + `01-minimal-flow.golden.json`
- [ ] `tests/fixtures/valid/02-all-primitives.star` + `02-all-primitives.golden.json` — exercises every DSL primitive
- [ ] `tests/fixtures/valid/03-multi-flow-per-file.star` (D-15)
- [ ] `tests/fixtures/valid/04-load-relative.star` + `04-load-target.star`
- [ ] `tests/fixtures/valid/05-load-absolute.star`
- [ ] `tests/fixtures/valid/06-call-flow-cross-file.star` + helper file
- [ ] `tests/fixtures/invalid/01-missing-required-kwarg.star` (header: `# expects: missing required 'name'`)
- [ ] `tests/fixtures/invalid/02-mutable-capture.star` (header: `# expects: lambda captures non-module-level variable`)
- [ ] `tests/fixtures/invalid/03-load-traversal.star` (header: `# expects: path escapes parser root`)
- [ ] `tests/fixtures/invalid/04-duplicate-flow-name.star` (header: `# expects: duplicate flow name`)
- [ ] `tests/fixtures/invalid/05-call-flow-not-found.star` (header: `# expects: call_flow target not found`)
- [ ] `tests/fixtures/invalid/06-unknown-extension.star` (header: `# expects: unknown identifier`)
- [ ] `tests/fixtures/invalid/07-forbidden-lambda-builtin.star` (header: `# expects: not allowed in lambda`)
- [ ] `tests/fixtures/invalid/08-bad-syntax.star` (header: `# expects: syntax error`)

**Test files (`pkg/*/{*_test.go}`):**
- [ ] `pkg/dag/node_test.go` — Node interface compile-time assertions, freeze cascade
- [ ] `pkg/dag/action_test.go` — ActionRef freeze cascade
- [ ] `pkg/dag/retry_test.go` — RetryPolicy.Unpack tests (DSL-08)
- [ ] `pkg/dag/lambda_test.go` — CapturedLambda construction
- [ ] `pkg/extension/extension_test.go` — interface assertion + fake extension
- [ ] `pkg/extension/credential_test.go` — sealed interface, redacted `String()`
- [ ] `pkg/extension/registry_test.go` — registration succeeds/fails with/without `Idempotent`
- [ ] `pkg/extension/schema_test.go` — `ParseSchema` reflection + `UnpackOperationKwargs` validator
- [ ] `pkg/parser/parser_test.go` — `TestValidFixtures`, `TestInvalidFixtures`, golden update flag
- [ ] `pkg/parser/builtins_test.go` — six DSL primitives unit tests
- [ ] `pkg/parser/load_test.go` — sandbox enforcement, `.git`-walk root discovery
- [ ] `pkg/parser/lambda_capture_test.go` — stable ID, free-var validation
- [ ] `pkg/parser/resolve_setup_test.go` — `TestResolveAllowLambdaIsSet`
- [ ] `pkg/bridge/struct_test.go` — `TestToStarlarkStruct_Deterministic`
- [ ] `pkg/bridge/lambda_globals_test.go` — `TestLambdaTimeGlobalsLocked`
- [ ] `pkg/bridge/lambda_call_test.go` — fresh thread, `MaxExecutionSteps`, print routed

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|

*None for Phase 1 — every requirement is exercise-able via `go test`. Determinism replay (Pitfall #3) and Temporal dev-server integration are Phase 3 concerns; Phase 1's verifications are entirely in-process Go.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
