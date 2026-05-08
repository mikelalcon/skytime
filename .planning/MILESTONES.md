# Milestones

## v1.42.0 Foundation (Shipped: 2026-05-08)

**Phases:** 9 | **Plans:** 58 | **Tasks:** 146
**Timeline:** 2026-04-26 → 2026-05-08 (12 days, 383 commits)
**Code:** ~46k LOC Go (135k insertions, 546 files)

### Delivered

A flow-author team can take an extension catalog and a customer brief, write a `.star` file, and have a production-grade durable workflow running on Temporal — without touching Go and without giving up Temporal's retry/timeout/child-workflow guarantees.

### Key Accomplishments

1. **Parse-time DSL** — six Starlark primitives (`flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow`) plus `result()`/`fail()`/`output_alias` for expression-mode branching. Parser-time `${ctx.expr}` interpolation desugars to lambdas. All builtins have `// skytime:doc` markers driving `docs/reference/builtins.md` via `cmd/skytime-docgen`.
2. **Generic Temporal activity** (`pkg/activity.ExecuteBatch`) — single activity dispatches all extension I/O. Idempotent batches share one invocation; non-idempotent ops execute one-per-invocation, mixed batches rejected at parse time. JIT credential resolution with retry-aware cache bypass; secrets sealed in `extension.Secret` (`String()`/`MarshalJSON()` redact).
3. **Generic Temporal interpreter** (`pkg/interpreter.SkytimeWorkflow`) — walks any `dag.Flow`. `if_cond`/`script` evaluate inline (zero history events). `for_each_parallel` via `workflow.Go` + `workflow.Selector` with bounded fan-out. `call_flow` invokes child workflow. Cancellation watchdog bridges `workflow.Context.Done()` → native `chan struct{}` via `sync.Once`-guarded close. Lambdas survive serialization via Option B (re-parse on workflow start with content-hash IDs).
4. **Worker bootstrap** — three named client constructors (`NewCloudClient`, `NewSelfHostedClient`, `NewDevClient`). Filesystem registry boot with frozen-after-boot semantics. Build ID-based versioning (opt-in `WorkerOptions.UseBuildIDVersioning`).
5. **Static validation** (`pkg/validator`) — thin facade over the parser; differential corpus test (`tests/differential_test.go`) runs every `examples/skeleton/*.star` through static `validator.Validate` AND a dry-run interpreter, asserting they agree on accept/reject. Branch-equality validation for expression-mode `if_cond` with permissive type inference.
6. **CLI** (`cmd/skytime` + `pkg/cli`) — `validate` / `run` / `dev-server` / `test` / `info` subcommands. Cobra/charm-log firewall: reachable only from `cmd/skytime` and `pkg/cli`, enforced by AST-walking firewall test. Bazel-style colored output with multi-line live progress (10-frame braille spinner, ANSI cursor-up redraw, `--verbose` toggles static lines).
7. **Tier-3 E2E test harness** (`pkg/testing`) — `tester.workflow`/`mock_action`/`run` parse-time builtins gated by `WithTestMode()`. 3-tier MockRegistry. Replay-determinism always-on (every test runs twice via `RunOnceCapturing`, divergence reporter walks back to nearest `step_dispatch`). `assert.*` from `starlarktest` injected at parse time, surfaced into `*testing.T`. `skytime test <dir>` recursively discovers `*_test.star`.
8. **Example project** (`examples/http-github-webhook/`) — three real extensions (HTTP + GitHub via `go-github/v78` + Webhook). Five `.star` flows mechanically pinning every primitive. One Tier-3 `.star` test exercising attempt-aware retry + replay determinism. Reusable `pkg/extension/credfile/` library credential resolver (TOML, sealed `Credential` + `Secret`). Custom `extbin` binary demonstrating "build your own binary" pattern. README walkthrough: `git clone` → running flow in 4 commands. CI workflow with four locked steps gating every push.
9. **Documentation set** — README front door, `docs/architecture.md` (parse/execute split + ASCII diagram), `docs/getting-started.md` (5-10 min tutorial), audience-split landings (`docs/for-flow-authors/` + `docs/for-extension-developers/`), CLI + builtins reference (auto-generated), HTTP extension reference, examples index.

### Architectural Firewalls (verified by AST-walking tests)

- `go.temporal.io/sdk` reachable only from `pkg/{activity,interpreter,worker,cli,testing}`
- `cobra`/`charmbracelet/log/v2` reachable only from `pkg/cli` and `cmd/skytime`
- Extensions never import `go.temporal.io/sdk/activity`
- `cmd/skytime-docgen` is stdlib-only
- Test-mode parser globals (`tester`, `ok`, `err`, `nonretryable`) gated; production parsers don't set the test-mode flag

### Phases

| Phase | Name | Plans | Completed |
|-------|------|-------|-----------|
| 1 | Type spine + extension contract + parser/bridge | 5 | 2026-04-27 |
| 2 | Generic activity + block-batch + credentials | 3 | 2026-04-29 |
| 3 | Lambda serialization + interpreter + worker | 4 | 2026-04-30 |
| 4 | Static validation + CLI skeleton | 7 | 2026-05-02 |
| 04.1 | Dynamic step kwargs + `${ctx.expr}` interpolation | 8 | 2026-05-03 |
| 04.2 | `if_cond` expression mode + strict-equality binding | 7 | 2026-05-04 |
| 04.3 | Documentation + source-driven reference generator | 9 | 2026-05-04 |
| 5 | Tier-3 E2E test harness (`temporal_test`) | 6 | 2026-05-05 |
| 6 | Example project (HTTP + GitHub + Webhook) | 9 | 2026-05-07 |

### Tech Debt Carried Forward to v1.43

- `pkg/cli` lacks `WithBuildID(string)` Option (custom binaries depend on `-ldflags`)
- `pkg/testing` lacks `WithCredentialHandler` Option (Phase 6 tests mock around it)
- `extbin/main.go` boilerplate (~30 lines of lazy credfile handler reusable into `pkg/cli`)
- 4 of 5 example flows have parse-only coverage, not live execution under CI
- Phase 6 HTTP extension registered but unused in example flows
- Nyquist coverage backlog: 1/9 phases compliant; large enough to be its own thread

### Audit

See [`milestones/v1.42.0-MILESTONE-AUDIT.md`](milestones/v1.42.0-MILESTONE-AUDIT.md). Status: `tech_debt` — all 64 requirements satisfied, all 4 declared E2E flows wired, all 5 architectural firewalls hold; accumulated debt items above carry forward.

### Archive

- [`milestones/v1.42.0-ROADMAP.md`](milestones/v1.42.0-ROADMAP.md)
- [`milestones/v1.42.0-REQUIREMENTS.md`](milestones/v1.42.0-REQUIREMENTS.md)
- [`milestones/v1.42.0-MILESTONE-AUDIT.md`](milestones/v1.42.0-MILESTONE-AUDIT.md)
