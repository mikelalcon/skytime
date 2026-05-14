---
phase: 07-trigger-primitive-server-shell
plan: 04
subsystem: worker
tags: [trigger, registry, worker-boot, sealed-registry, slog-warn, worker-stop-timeout, frozen-after-boot]

# Dependency graph
requires:
  - phase: 07-01
    provides: "*dag.Trigger struct"
  - phase: 07-02
    provides: "extension.TriggerSource sealed interface, extension.FakeTriggerSource test stub"
  - phase: 07-03
    provides: "Parser.Triggers() []*dag.Trigger and Parser.TriggerWarnings() []string accessors"
provides:
  - "interpreter.TriggerRegistry — sealed registry parallel to FlowRegistry; sync.RWMutex + frozen flag + sorted-slice + byContentHash map; Register/Freeze/All/ByContentHash methods"
  - "interpreter.NewTriggerRegistry constructor"
  - "interpreter.ErrTriggerRegistryFrozen sentinel"
  - "bootRegistry signature: func bootRegistry(rootDir string, exts []extension.Extension) (*interpreter.FlowRegistry, *interpreter.TriggerRegistry, error)"
  - "Worker.triggers field + Worker.Triggers() *interpreter.TriggerRegistry accessor"
  - "WorkerOptions.WorkerStopTimeout time.Duration field with 30s default in applyDefaults"
  - "Boot-time slog.Default().Warn surface for D-07-13 byte-identical duplicate-trigger warnings"
affects:
  - 07-05 (server subcommand consumes Worker.Triggers() and WorkerOptions.WorkerStopTimeout)
  - 07-06 (firewall + rename pass)
  - 07.1 (HTTP webhook receiver iterates Worker.Triggers().All() grouped by Source.Kind)

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Sorted-slice + byContentHash map registry shape (TriggerRegistry) — diverges from FlowRegistry's per-(name,hash) lookup map because triggers are iterated wholesale at HTTP-mount time, never per-request (Pitfall 1)"
    - "Same parser session drains both Parser.Flows() and Parser.Triggers() in bootRegistry — single AST walk, two registries"
    - "Boot-time slog.Default().Warn drain for parser warnings (D-07-13) — accumulates on parser state, surfaces at server startup"
    - "*_test.star skip rule applies uniformly to BOTH flows AND triggers (proven by TestBootRegistry_SkipsTestFiles regression)"
    - "WorkerStopTimeout threading — applyDefaults supplies 30s, NewWorker pipes into sdkworker.Options.WorkerStopTimeout (D-07-16 / D-07-17)"

key-files:
  created:
    - .planning/phases/07-trigger-primitive-server-shell/07-04-SUMMARY.md
  modified:
    - pkg/interpreter/registry.go
    - pkg/interpreter/registry_test.go
    - pkg/worker/boot.go
    - pkg/worker/boot_test.go
    - pkg/worker/worker.go
    - pkg/worker/worker_test.go
    - pkg/worker/options.go

key-decisions:
  - "TriggerRegistry uses sorted-slice + byContentHash map (NOT FlowRegistry's lookup-by-(name,hash) shape) because triggers are iterated wholesale at HTTP-mount time, never looked up per-request. Pitfall 1 in 07-RESEARCH.md."
  - "Parser warning drain via slog.Default().Warn at boot — keeps the parser pure (warnings accumulate on Parser.triggerWarnings) and avoids leaking slog details into pkg/parser. The worker boot owns the slog surface."
  - "Helper fakeWebhookExt + fakeTriggerStarlarkValue duplicated inline in pkg/worker/boot_test.go (Option B from plan) rather than factored to pkg/extension/testing — TODO(phase-7.1) marker records the deferred refactor. Rationale: factoring belongs to a quick that owns the test-infrastructure shape, not this load-bearing plan."
  - "WorkerStopTimeout default = 30s (D-07-17) matches Kubernetes terminationGracePeriodSeconds default; CLI/run path leaves it zero (applyDefaults supplies 30s); skytime server (Plan 05) sets it explicitly from --drain-timeout."
  - "boot_test.go uses pkg/worker (white-box) so the duplicated fakeWebhookExt has package-internal visibility. The parser_test.go version stays in package parser_test (black-box). Both compile; both wrap the same *extension.FakeTriggerSource via embedding."

patterns-established:
  - "Registry shape selection rule: per-request lookup → map keyed by access tuple (FlowRegistry); wholesale iteration → sorted slice + secondary indices (TriggerRegistry). Future registries (e.g., Phase 7.2 ScheduleRegistry) follow the same selection rule."
  - "bootRegistry pattern for adding a sibling registry: extend signature with the new return value, drain the new parser accessor inside the same parse loop (no second AST walk), Freeze both registries at the end, drain deferred warnings via slog.Default()."
  - "*_test.star skip is the single source of truth for test-file exclusion — adding a new declaration type that mirrors flow shape (trigger, schedule, etc.) gets the skip for free because the file never reaches the parser."

requirements-completed: [TRIG-05]

# Metrics
duration: 8min
completed: 2026-05-08
---

# Phase 07 Plan 04: Trigger Registry and Worker Boot Summary

**`interpreter.TriggerRegistry` sealed registry parallel to FlowRegistry, `bootRegistry` extended to drain Parser.Triggers() into a sibling registry from the same parser session, Worker.Triggers() accessor, and WorkerOptions.WorkerStopTimeout threading into sdkworker.Options — all with zero external API impact (Pitfall 12 satisfied).**

## Performance

- **Duration:** ~8 min
- **Started:** 2026-05-08T20:13:07Z
- **Completed:** 2026-05-08T20:20:59Z
- **Tasks:** 6 (Tasks 1+4 TDD; Task 5 audit-only no-commit)
- **Files modified:** 7 (1 created — SUMMARY.md; 7 modified)
- **Commits:** 6 (one RED + five GREEN/feat/test commits)

## Accomplishments

### TriggerRegistry shape (shipped)

```go
type TriggerRegistry struct {
    mu            sync.RWMutex
    frozen        bool
    triggers      []*dag.Trigger             // sorted post-Freeze
    byContentHash map[string][]*dag.Trigger  // secondary index
}

var ErrTriggerRegistryFrozen = errors.New("interpreter: trigger registry is frozen")

func NewTriggerRegistry() *TriggerRegistry
func (r *TriggerRegistry) Register(contentHash string, t *dag.Trigger) error
func (r *TriggerRegistry) Freeze()
func (r *TriggerRegistry) All() []*dag.Trigger        // fresh copy; sorted post-Freeze
func (r *TriggerRegistry) ByContentHash(hash string) []*dag.Trigger
```

**Sort key tuple in Freeze():** `(Source.Kind(), FlowName, Pos.String())`. The Pos.String tiebreaker uses go.starlark.net's stable `file:line:col` format. Pitfall 1 captures the rationale for diverging from FlowRegistry's per-(name,hash) lookup shape: triggers are iterated wholesale at HTTP-mount time (Phase 7.1) and at startup-banner time (Plan 05), never per-request.

**Concurrency contract:** Same RWMutex + frozen-after-boot model as FlowRegistry. `Register` is a Lock; `All` and `ByContentHash` use RLock for the post-Freeze read-only access pattern. Boot is single-goroutine in practice; the lock is for boot-time correctness.

### bootRegistry signature change

```go
// Before:
func bootRegistry(rootDir string, exts []extension.Extension) (*interpreter.FlowRegistry, error)

// After:
func bootRegistry(rootDir string, exts []extension.Extension) (*interpreter.FlowRegistry, *interpreter.TriggerRegistry, error)
```

Same parser session drains both `p.Flows()` and `p.Triggers()`. The trigger-registration loop iterates the deterministically-sorted `[]*dag.Trigger` returned by `Parser.Triggers()` (sorted by Source.Kind, FlowName, Pos.String per Plan 03) and calls `trigReg.Register(hash, trig)` keyed by the trigger's owning file's content_hash. After both registries Freeze, deferred parser warnings (`p.TriggerWarnings()`) drain via `slog.Default().Warn("parser warning", "detail", ...)`.

The `*_test.star` skip rule remains a single condition (`!strings.HasSuffix(d.Name(), "_test.star")`) and applies uniformly to flows AND triggers — `*_test.star` files never reach `parser.ParseFile`, so neither flows nor triggers from those files register. `TestBootRegistry_SkipsTestFiles` proves this end-to-end.

### Worker struct + accessor

```go
type Worker struct {
    sdk      sdkworker.Worker
    registry *interpreter.FlowRegistry
    triggers *interpreter.TriggerRegistry  // NEW (Phase 7)
    opts     WorkerOptions
    stopOnce sync.Once
}

func (w *Worker) Triggers() *interpreter.TriggerRegistry { return w.triggers }
```

`NewWorker` external API is unchanged — the bootRegistry signature change is contained to the package internals. All existing `worker.NewWorker(c, worker.WorkerOptions{...})` call sites in `pkg/cli/run.go`, `pkg/worker/embed_integration_test.go`, and `pkg/worker/doc.go` compile and pass tests with zero edits (Pitfall 12 verified).

### WorkerOptions.WorkerStopTimeout (D-07-16 prep)

```go
type WorkerOptions struct {
    // ... existing fields ...
    WorkerStopTimeout time.Duration  // NEW
}

const defaultWorkerStopTimeout = 30 * time.Second
```

`applyDefaults()` supplies 30s when zero — matches Kubernetes terminationGracePeriodSeconds default per D-07-17. `NewWorker` threads `opts.WorkerStopTimeout` into `sdkworker.Options.WorkerStopTimeout` when non-zero. Plan 05's `skytime server` will set this from `--drain-timeout`; existing `skytime run` callers leave it zero and inherit the 30s default.

### Boot-time parser warning drain

```go
// in bootRegistry, after both Freeze calls:
for _, w := range p.TriggerWarnings() {
    slog.Default().Warn("parser warning", "detail", w)
}
```

D-07-13 byte-identical duplicate triggers are explicitly ACCEPTED (registry stores both copies). The parser flags them via `Parser.TriggerWarnings()` (Plan 03's accumulator on parser state); the worker boot loop owns the slog surface. `TestBootRegistry_DrainsParserWarnings` captures slog output via a bufferred handler and asserts the `"duplicate trigger"` text appears.

## Test Coverage for TRIG-05

`pkg/interpreter/registry_test.go` — 8 new TriggerRegistry tests:

| Test                                      | Behavior pinned                                         |
| ----------------------------------------- | ------------------------------------------------------- |
| TestTriggerRegistry_RegisterAfterFreeze   | Register post-Freeze returns ErrTriggerRegistryFrozen   |
| TestTriggerRegistry_AllSorted             | Freeze sorts by (Source.Kind, FlowName, Pos.String)     |
| TestTriggerRegistry_ConcurrentRegister    | 100 goroutines register without -race findings; all 100 land |
| TestTriggerRegistry_ByContentHash         | Secondary index returns per-file triggers; nil on miss  |
| TestTriggerRegistry_FreezeIdempotent      | Double Freeze does not panic; All() unchanged           |
| TestTriggerRegistry_AllReturnsSnapshot    | Caller mutating returned slice does not affect internal state |
| TestTriggerRegistry_RegisterNilTrigger    | Nil trigger rejected with "trigger required"            |
| TestTriggerRegistry_RegisterEmptyHash     | Empty contentHash rejected with "contentHash required"  |

`pkg/worker/boot_test.go` — 5 new bootRegistry tests:

| Test                                       | Behavior pinned                                         |
| ------------------------------------------ | ------------------------------------------------------- |
| TestBootRegistry_RegistersTriggers         | flows.star with one trigger registers FlowName + Source.Kind + CredentialID |
| TestBootRegistry_SkipsTestFiles            | *_test.star skip applies to BOTH flows AND triggers     |
| TestBootRegistry_DrainsParserWarnings      | D-07-13 duplicate-trigger warning surfaces via slog at boot |
| TestBootRegistry_TriggerInLoadedFile       | Trigger in a separate .star file from its referenced flow registers cleanly |
| TestBootRegistry_NoTriggers                | Directory with only flows yields empty (non-nil) trigger registry |

`pkg/worker/worker_test.go` — 3 new Worker integration tests:

| Test                                          | Behavior pinned                                         |
| --------------------------------------------- | ------------------------------------------------------- |
| TestNewWorker_RegistersTriggers               | Worker.Triggers() exposes the registry built by bootRegistry |
| TestNewWorker_WorkerStopTimeoutDefault        | 30s default propagates to sdkworker.Options.WorkerStopTimeout |
| TestNewWorker_WorkerStopTimeoutCustom         | Explicit 5s value propagates unchanged                  |

**Existing test regression:** All `TestBootRegistry_*` and `TestRegistry_*` tests from prior phases still pass after the signature change (the `_, _ , err := bootRegistry(...)` pattern updates kept them green). FlowRegistry, ContentHashFor, multi-version, concurrency, and validation tests all green.

## Task Commits

Each task was committed atomically:

1. **Task 1 RED (TriggerRegistry tests)** — `614af79` (test)
2. **Task 1 GREEN (TriggerRegistry implementation)** — `5b92eb7` (feat)
3. **Task 2 (bootRegistry signature change)** — `5715557` (feat)
4. **Task 3 (Worker.Triggers + WorkerStopTimeout)** — `e9d10fe` (feat)
5. **Task 4 (boot_test.go new tests)** — `362b8f9` (test)
6. **Task 5 (audit pkg/cli/run.go + extbin)** — no commit (audit-only; zero file changes per plan expectation)
7. **Task 6 (worker_test.go integration tests)** — `3e6d0d9` (test)

**Plan metadata commit:** TBD (after STATE.md / ROADMAP.md updates)

## Files Created/Modified

### Created
- `.planning/phases/07-trigger-primitive-server-shell/07-04-SUMMARY.md` — this file

### Modified
- `pkg/interpreter/registry.go` — appended TriggerRegistry struct + ErrTriggerRegistryFrozen + 4 methods (~131 lines added)
- `pkg/interpreter/registry_test.go` — appended 8 TriggerRegistry tests + helperNewTestTrigger (~131 lines added)
- `pkg/worker/boot.go` — bootRegistry signature change to three returns + trigger-registration loop + parser-warning slog drain (+64 / -27 lines)
- `pkg/worker/boot_test.go` — 5 new tests + fakeWebhookExt/fakeTriggerStarlarkValue helpers + updated existing call sites to discard the new return value (~213 lines added; 9 call sites updated)
- `pkg/worker/worker.go` — Worker.triggers field + Worker.Triggers() accessor + flowReg/trigReg variable rename + WorkerStopTimeout threading (~12 lines net)
- `pkg/worker/worker_test.go` — 3 new tests + extension import + makeFlowsDirWithTrigger helper (~76 lines added)
- `pkg/worker/options.go` — WorkerStopTimeout field + defaultWorkerStopTimeout constant + applyDefaults branch + time import (~17 lines net)

### Audited (zero diff)
- `pkg/cli/run.go` — `worker.NewWorker(c, worker.WorkerOptions{...})` call site compiles unchanged; the 30s default applies via applyDefaults; no edit needed.
- `examples/http-github-webhook/cmd/extbin/main.go` — no direct call to `worker.NewWorker`; routes through `pkg/cli/run` via `cli.NewRootCommand`. Zero diff.
- `pkg/cli/run_internal_test.go` and `pkg/worker/embed_integration_test.go` — referenced in the audit grep; their `worker.NewWorker` call sites compile and pass tests unchanged.

## Decisions Made

- **Sort key includes Pos.String() (not just Pos.Filename, line, col).** `syntax.Position.String()` returns the canonical `<file>:<line>:<col>` format and is the cheapest stable cross-position comparison for the sort tiebreaker. Direct Filename/Line/Col triple comparison would force three branches; `String()` is one branch and the cost is negligible (sort runs once at Freeze).
- **Helper duplication over factoring (Option B).** The plan's <action> block explicitly chose Option B with a TODO marker for Phase 7.1. Decision rationale documented inline: factoring `fakeWebhookExt` and `fakeTriggerStarlarkValue` to `pkg/extension/testing` would touch a third package and pull in a non-load-bearing refactor mid-wave. The TODO marker preserves the audit trail.
- **`slog.Default()` chosen over `opts.Logger`.** The plan's <interfaces> block specified `slog.Default()`. Wiring through `opts.Logger` would create a chicken-and-egg between bootRegistry (which runs BEFORE NewWorker accesses opts.Logger for SDK-side wiring) and the slog handler. `slog.Default()` is the right surface for boot-time warnings; consumers replace it via `slog.SetDefault` if they want to redirect.
- **Existing boot_test.go call sites updated to `_, _, err := bootRegistry(...)`.** The plan said `go build ./pkg/worker/...` would FAIL after Task 2 and that Task 3 fixes it. Task 3's worker.go updates fixed the production caller, but the test file still had 9 stale call sites. Updating the boot_test.go callers to discard the new return value with `_, _, err :=` is the minimal-impact fix that preserves all existing test semantics.
- **TestNewWorker_WorkerStopTimeoutDefault asserts on captured sdkOpts (not on `w.opts.WorkerStopTimeout`).** The plan's behavior block listed both options. Asserting on `capturedOpts.WorkerStopTimeout` (the value the SDK would actually receive) is stronger evidence than `opts.WorkerStopTimeout` (which only proves applyDefaults ran). The existing `withFakeSDKWorker` test seam already captures sdkOpts, so this is zero-extra-infra.

## Deviations from Plan

None directly. Two minor adaptations worth recording:

**1. boot_test.go existing-call-site update.** The plan's <action> block for Task 2 said "The existing tests will FAIL because NewWorker (in worker.go) still calls the old 2-return-value bootRegistry — that's expected; Task 3 fixes it." Task 3's plan section talks about updating worker.go but does not explicitly call out the same update needed in `pkg/worker/boot_test.go`'s 9 call sites (which use bootRegistry directly, not NewWorker). I made those updates as part of Task 3's "restore compile" goal — minimal-impact `_, _, err :=` discard. This is the kind of routine signature-cascade fix that doesn't rise to a deviation; it's executing the plan's intent.

**2. Audit-only Task 5 produced no commit.** The plan's Task 5 acceptance criteria included `git diff pkg/cli/run.go shows ZERO changes` and `git diff examples/http-github-webhook/cmd/extbin/main.go shows ZERO changes`. Both were verified zero-diff. The task description itself describes Task 5 as an audit; not committing is the plan-conformant outcome. The plan's <output> block's metadata commit (single combined SUMMARY+STATE+ROADMAP commit at the end) covers this case.

## Issues Encountered

None. All six tasks ran clean on first GREEN attempt; no auto-fixes needed across Rules 1-3.

## User Setup Required

None — pure registry + boot wiring. No external services.

## Self-Check: PASSED

- File `pkg/interpreter/registry.go` (modified): FOUND
- File `pkg/interpreter/registry_test.go` (modified): FOUND
- File `pkg/worker/boot.go` (modified): FOUND
- File `pkg/worker/boot_test.go` (modified): FOUND
- File `pkg/worker/worker.go` (modified): FOUND
- File `pkg/worker/worker_test.go` (modified): FOUND
- File `pkg/worker/options.go` (modified): FOUND
- Commit `614af79` (Task 1 RED): FOUND
- Commit `5b92eb7` (Task 1 GREEN): FOUND
- Commit `5715557` (Task 2): FOUND
- Commit `e9d10fe` (Task 3): FOUND
- Commit `362b8f9` (Task 4): FOUND
- Commit `3e6d0d9` (Task 6): FOUND
- `go build ./...`: PASS
- `go vet ./...`: PASS
- `go test ./... -count=1 -race`: PASS (full repo green)
- `go test ./pkg/interpreter/ -run TestTriggerRegistry_ -count=1 -race`: PASS (8 tests)
- `go test ./pkg/worker/ -run TestBootRegistry_ -count=1 -race`: PASS
- `go test ./pkg/worker/ -run 'TestNewWorker_(RegistersTriggers|WorkerStopTimeout)' -count=1 -race`: PASS
- Cross-package leakage check: `git grep TriggerRegistry` outside `pkg/interpreter` and `pkg/worker` returns only doc-comment mentions — no production references. Plan 05 will introduce the first real `pkg/cli` consumer.

## Next Phase Readiness

- **Plan 05 (skytime server subcommand)** unblocked. Worker.Triggers() returns the registry; WorkerOptions.WorkerStopTimeout maps to `--drain-timeout`. Plan 05's startup banner can iterate `w.Triggers().All()` deterministically.
- **Plan 06 (firewall + rename)** unblocked transitively.
- **Phase 7.1 (HTTP webhook receiver)** unblocked. The HTTP router will iterate `w.Triggers().All()` grouped by `Source.Kind()` to mount handlers; `ByContentHash` is available for future hot-reload diagnostics. The Phase 7.1 refactor of `fakeWebhookExt` to `pkg/extension/testing` is captured by the TODO marker in `pkg/worker/boot_test.go`.
- **No blockers.** All TRIG-05 acceptance criteria satisfied; the trigger registry is production-ready for the HTTP receiver wiring.

---
*Phase: 07-trigger-primitive-server-shell*
*Completed: 2026-05-08*
