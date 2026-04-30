---
phase: 3
slug: lambda-serialization-decision-interpreter-worker
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-04-30
---

# Phase 3 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `github.com/stretchr/testify@v1.11.1` + `go.temporal.io/sdk/testsuite` |
| **Static analysis** | `workflowcheck ./pkg/interpreter/...` — installed via `go install go.temporal.io/sdk/contrib/tools/workflowcheck@latest` (CI step, NOT a `go.mod` dep) |
| **Quick run command** | `go test ./pkg/interpreter/... ./pkg/worker/... -count=1` (~10s for the two new packages) |
| **Full suite command** | `go test ./... -race -count=1` (full repo with race detector) |
| **Replay/integration** | `go test -tags=integration ./pkg/interpreter/... -count=1` (uses real `temporal server start-dev` if available; otherwise skips) |
| **Phase gate** | Full suite green + `workflowcheck ./pkg/interpreter/...` clean + integration tag green + `go vet ./...` clean + `go build ./...` clean |
| **Estimated runtime** | ~30s unit / +60s integration |

---

## Sampling Rate

- **After every task commit:** `go test ./pkg/{package-touched}/... -count=1` (<10s)
- **After every plan wave:** `go test ./pkg/interpreter/... ./pkg/worker/... -race -count=1` (<30s)
- **Before `/gsd:verify-work`:** Full suite + `workflowcheck` + integration tag, all green
- **Max feedback latency:** 30 seconds (60s for integration tag)

---

## Per-Task Verification Map

| Req ID | Behavior | Test Type | Automated Command | File Exists | Status |
|--------|----------|-----------|-------------------|-------------|--------|
| **INTRP-01** | `SkytimeWorkflow` walks any `dag.Flow` produced by the parser | integration (testsuite) | `go test ./pkg/interpreter -run TestSkytimeWorkflow_KitchenSink -count=1` | ❌ W0 | ⬜ pending |
| **INTRP-02** | Lambda-serialization decision (Option B + Build IDs) chosen and committed BEFORE interpreter code; replay-twice equality holds | integration (testsuite) | `go test ./pkg/interpreter -run TestReplay_KitchenSinkFlow -count=1` (decision recorded in CONTEXT.md D3-01) | ❌ W0 | ⬜ pending |
| **INTRP-03** | `if_cond` and `script` produce zero Temporal history events (lambda eval is in-memory) | integration | `go test ./pkg/interpreter -run TestInlineEval_NoHistoryEvents -count=1` (asserts event count baseline + no extras) | ❌ W0 | ⬜ pending |
| **INTRP-04** | `for_each_parallel` honors `max_concurrency`; cancels siblings on non-retryable error; results in input order | integration + unit | `go test ./pkg/interpreter -run TestForEach -race -count=1` | ❌ W0 | ⬜ pending |
| **INTRP-05** | `call_flow` invokes child workflow; retry policy + search attrs inherited from parent | integration | `go test ./pkg/interpreter -run TestCallFlow_ChildInheritsRetryAndSearchAttrs -count=1` | ❌ W0 | ⬜ pending |
| **INTRP-06** | Map iteration sorts keys; replay-twice CI test passes | integration | `go test ./pkg/interpreter -run TestReplay -race -count=1` | ❌ W0 | ⬜ pending |
| **INTRP-07** | Interpreter passes `workflowcheck` analysis (no `go`, `time.*`, `rand.*`, map iteration without sort) | static | `workflowcheck ./pkg/interpreter/...` (CI step; non-zero output = failure) | N/A | ⬜ pending |
| **WORK-01** | Worker registers `SkytimeWorkflow` + `ExecuteBatch` with one Temporal worker | unit | `go test ./pkg/worker -run TestNewWorker_RegistersWorkflowAndActivity -count=1` | ❌ W0 | ⬜ pending |
| **WORK-02** | Three named client constructors: `NewCloudClient` / `NewSelfHostedClient` / `NewDevClient` | unit | `go test ./pkg/worker -run TestClientConstructors -count=1` | ❌ W0 | ⬜ pending |
| **WORK-03** | Library-embed pattern works end-to-end | integration | `go test -tags=integration ./pkg/worker -run TestEmbed_FullStack -count=1` (real dev-server; skip if unavailable) | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

**`pkg/dag` updates (DSL retrofit + WorkflowInput rewrite):**
- [ ] `pkg/dag/flow.go` — add `TaskQueue string` field per D3-19
- [ ] `pkg/dag/step.go` — add `TaskQueue string` field per D3-19
- [ ] `pkg/dag/input.go` — rewrite per D3-04: `{FlowName string, ContentHash string, InitState map[string]any}`; drop the embedded `Flow` field; update `MarshalJSON`
- [ ] `pkg/dag/input_test.go` — new tests for the rewritten shape

**`pkg/parser` retrofit (DSL kwarg):**
- [ ] `pkg/parser/builtins.go` — add optional `task_queue` kwarg to `flow()` and `step()` builtins; thread onto `dag.Flow.TaskQueue` and `dag.Step.TaskQueue`
- [ ] `pkg/parser/linter.go` — validate `task_queue` is non-empty string if provided
- [ ] `pkg/parser/builtins_test.go` — coverage for valid + invalid `task_queue` values
- [ ] `tests/fixtures/valid/08-task-queue-overrides.star` — exercises both per-flow and per-step
- [ ] `tests/fixtures/invalid/11-empty-task-queue.star` — expects "task_queue must be non-empty"

**`pkg/interpreter` (new package):**
- [ ] `pkg/interpreter/doc.go` — package docs explaining the firewall (this + `pkg/worker` + `pkg/activity` are the only packages allowed to import `go.temporal.io/sdk/...`)
- [ ] `pkg/interpreter/registry.go` + `_test.go` — flow registry (`map[string]map[string]*ParsedFlow` keyed by `flow_name → content_hash`); frozen after boot; mismatch returns clean error
- [ ] `pkg/interpreter/workflow.go` + `_test.go` — `SkytimeWorkflow(ctx, WorkflowInput) (map[string]any, error)` entry point
- [ ] `pkg/interpreter/walk.go` — `Node` type-switch dispatcher
- [ ] `pkg/interpreter/walk_step.go` + `_test.go` — Step walker; `workflow.ExecuteActivity(ctx, "ExecuteBatch", batch)` with sum-of-timeouts + per-step `TaskQueue` override
- [ ] `pkg/interpreter/walk_ifcond.go` + `_test.go` — IfCond walker; lambda eval via bridge with watchdog
- [ ] `pkg/interpreter/walk_script.go` + `_test.go` — Script walker; lambda eval; merge output_alias into state
- [ ] `pkg/interpreter/walk_foreach.go` + `_test.go` — ForEachParallel walker; `workflow.WithCancel` + `workflow.NewBufferedChannel(max_concurrency)` + `workflow.Go` per branch + cancel-on-non-retryable
- [ ] `pkg/interpreter/walk_callflow.go` + `_test.go` — CallFlow walker; `workflow.ExecuteChildWorkflow` with retry/search-attr inheritance
- [ ] `pkg/interpreter/cancel_watchdog.go` + `_test.go` — bridges `workflow.Channel` (from `ctx.Done()`) to a native `chan struct{}` via a `workflow.Go` reader; passes the native channel to `bridge.CallLambda`'s cancel hook
- [ ] `pkg/interpreter/replay_test.go` — INTRP-02 / INTRP-06 replay-twice equality test
- [ ] `pkg/interpreter/firewall_test.go` — meta-test that `pkg/interpreter` DOES import `go.temporal.io/sdk/workflow` (catches inversion bug)

**`pkg/worker` (new package):**
- [ ] `pkg/worker/doc.go` — package docs explaining boot, lifecycle, registry freeze
- [ ] `pkg/worker/client.go` + `_test.go` — three named constructors:
  - `NewCloudClient(opts CloudOptions) (client.Client, error)` — uses `client.NewAPIKeyStaticCredentials(opts.APIKey)`; TLS auto-enabled
  - `NewSelfHostedClient(opts SelfHostedOptions) (client.Client, error)` — explicit `tls.Config` in `ConnectionOptions.TLS`
  - `NewDevClient(opts DevClientOptions) (client.Client, error)` — `ConnectionOptions.TLSDisabled = true`
- [ ] `pkg/worker/worker.go` + `_test.go` — `Worker` struct + `New(client, dispatch, options) (*Worker, error)` + `Start()` non-blocking + `Stop()` (with `sync.Once` wrap to prevent panic on double-call)
- [ ] `pkg/worker/options.go` + `_test.go` — `WorkerOptions{RootDir, BuildID, TaskQueue, ...}`; default `TaskQueue = "skytime"`; `BuildID` defaults to a build-time-injected `var defaultBuildID = "dev"` (overridable via `-ldflags "-X github.com/mikelalcon/skytime/pkg/worker.defaultBuildID=$(git rev-parse HEAD)"`)
- [ ] `pkg/worker/boot.go` + `_test.go` — registry boot: walk `RootDir`, parse every `.star`, compute `content_hash`, build registry, freeze
- [ ] `pkg/worker/firewall_test.go` — assert `pkg/worker` is in the small allowed-list of packages importing `go.temporal.io/sdk/...`; update Phase 2's firewall test allow-list to include `pkg/worker` and `pkg/interpreter`

**CI updates:**
- [ ] CI step: `workflowcheck ./pkg/interpreter/...` (INTRP-07)
- [ ] CI step: `go test -tags=integration ./pkg/interpreter/...` and `./pkg/worker/...` (replay-twice + dev-server smoke)
- [ ] CI install: `go install go.temporal.io/sdk/contrib/tools/workflowcheck@latest` before the workflowcheck step

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Build ID drain workflow on real Temporal Cloud / self-hosted | WORK-03 + D3-03 | Requires a real Temporal cluster with Worker Versioning enabled; v1 docs the procedure but customer-side test | Phase 6 README walkthrough: deploy v1 worker, start workflows, deploy v2 worker, observe Temporal UI showing old workflows continuing on v1 worker until completion |

*All other Phase 3 behaviors have automated verification.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s (unit) / < 60s (integration tag)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
