# Milestones

## v1.43.0 Durability + Triggers (Shipped: 2026-05-14)

**Phases completed:** 7 phases, 42 plans, 117 tasks

**Key accomplishments:**

- dag.Trigger pure-data node with stable JSON marshaling, dag-local TriggerSource seam, and Pos-exclusion + credential-never-serialized contracts that mirror dag.ActionRef.
- Sealed extension.TriggerSource SDK contract with kind-keyed factory registry, dag.RegisterTriggerSourceUnmarshaler init-time wiring, and reusable FakeTriggerSource test stub
- Top-level `trigger(...)` Starlark builtin shipped with three-layer parse-time validation (free-var lint + arity-1 enforcement + req-walker), cross-file FlowName resolution at finalize, byte-identical-duplicate-warning, and a generalized free-var AST visitor that lets the existing ctx-walker and the new req-walker share one re-parse.
- `interpreter.TriggerRegistry` sealed registry parallel to FlowRegistry, `bootRegistry` extended to drain Parser.Triggers() into a sibling registry from the same parser session, Worker.Triggers() accessor, and WorkerOptions.WorkerStopTimeout threading into sdkworker.Options — all with zero external API impact (Pitfall 12 satisfied).
- `skytime server` long-running subcommand wired with sorted startup banner, two-signal drain escalation, configurable `--drain-timeout`, charm-log/JSON logger swap, and a six-stage `testDrainHook` seam — covering SERVER-01..03 with the unit-testable subset and deferring three end-to-end signal-loop tests to Phase 7.1 once `worker.WithSDKFactory` ships.
- Completed CLI-13's hard rename of `skytime dev-server` to `skytime dev-temporal` (no deprecation alias per D-07-21); shipped two new firewall tests (D-07-10 credential-redaction AST walker over pkg/dag + pkg/extension + pkg/extension/builtin; D-07-22 grep gate for the legacy `dev-server` literal across every tracked file with a `.planning/` + CHANGELOG.md + self-allow-list); added `## skytime server` section (~80 lines, SERVER-01..03) to docs/reference/cli.md; final Phase 7 test suite green under `-race`.
- HMAC signature primitive (sha256/sha1/sha512 + constant-time hmac.Equal), composeWorkflowID with 8-char position hash for fan-out disambiguation, locked 7-string errorClass taxonomy with all 8 status writers (including the 415 writer Plan 04 needs), and the requestRecord + emit() per-request log line shape — all in a fresh pkg/extension/receiver package with HTTPMounter sub-interface for HTTP-shaped TriggerSource discrimination.
- Generic configurable HTTP webhook source factory at `pkg/extension/builtin/http/http.webhook(path=, method=, secret_credential=, signature_algo=, signature_header=)` — first concrete `extension.TriggerSource` in the codebase; surfaced and resolved a pre-existing seal/sub-package contradiction via the new `extension.TriggerSourceSeal` embeddable carrier.
- 1. [Rule 3 - Blocking] Task 1's TDD tests required Initialize wiring
- Created:
- Per-request webhook dispatch pipeline replaces Plan 04's 501 stub: HMAC signature validation against raw body bytes, JIT credential resolution, lambda-driven workflow input + idempotency key, REJECT_DUPLICATE collision detection via errors.As, and per-trigger fan-out with worst-status response mapping (D-7.1-14) + structured log line (D-7.1-15) — locked .Reveal() leak gates (≤3 sites, all in credentialBytes, each `// SECURITY:` annotated, none feed into fmt.Errorf/slog/http.Error).
- Adds the `worker.WithSDKFactory(f SDKFactory) Option` functional option so `pkg/cli` black-box tests can inject a fake SDK worker constructor; drops the three Phase 7 t.Skip("TODO(phase-7.1)") stubs in `pkg/cli/server_test.go` by wiring them to real assertions against the locked six-stage `testDrainHook` seam.
- Wires Plan 04b's fully-functional receiver.Mount handler pipeline into the skytime server subcommand: pre-binds net.Listen synchronously (Pitfall 9), constructs http.Server with D-7.1-12 timeouts, runs srv.Serve in a goroutine, calls srv.Shutdown(drainCtx) BEFORE worker.Stop on SIGTERM (D-7.1-11), and extends the startup banner to surface mount paths for HTTP-shaped sources — Plan 07's webhook_demo.star can now boot under `skytime server` and accept real webhook deliveries.
- Centerpiece walkthrough flow shipped: webhook_demo.star wires github.webhook(events=["issues"], secret_credential="github_webhook_secret") to a two-activity flow (gh.add_comment → gh.add_label) that proves Temporal's event-history-replay durability via cross-activity kill-restart — NO explicit sleep step (locked at planning time per Plan 07 iteration 1, since v1 has no first-class durable sleep primitive in the .star DSL).
- 1. [Rule 1 - Bug] Anti-claim phrasing in walkthrough's own meta-paragraph triggered the headings test
- 1. [Rule 3 — Blocking] Deferred `go mod tidy` between Task 1 and Task 2
- Boot-time Cron Schedule reconciler — `ReconcileCronSchedules(ctx, sc, triggers, flows, taskQueue, apply, logger)` translates parsed `core.cron(...)` triggers into Temporal Schedule resources via List → diff (Memo-canonical comparison with ContentHash) → Create/Update/Delete buckets sorted by Schedule ID, with `errors.Join` failure aggregation, AlreadyExists-non-fatal handling, and operator-State preservation through Update's DoUpdate callback.
- `skytime server --cron-reconcile` applies cron Schedules between worker.Start() and HTTP listener bind; `skytime cron-plan` ships as the dry-run preview subcommand reading rootdir → diffing cluster Schedules → printing the create/update/delete plan with zero mutations.
- Approver:
- Land the four log.<level>(...) parse-time factories + log module registration + module-scope rejection finalize pass; ${ctx.expr} desugaring and attrs= lambda capture reused verbatim from existing builtins
- Two-path human-mode filter for kind=log step frames: stdlib-only logKindFilterHandler decorator wraps the non-JSON server logger, and progressHandler's static renderers gain kind=log early-returns so `skytime server` + `skytime run` show one [skytime/log] line per fire instead of three.
- Migrate the canonical cron example to `log.info(...)` (LOG-01 proof), update the walkthrough sample stdout to show `[skytime/log]` (LOG-02 proof), regenerate `docs/reference/builtins.md` with four new entries via an extended docgen pipeline, fill in `07.2.1-VALIDATION.md` with the per-task verification map, and backfill `.planning/REQUIREMENTS.md` with LOG-01 + LOG-02.
- 11 SKIP test scaffolds + 4 doc.go package skeletons + dashboard walkthrough skeleton — 38 total SKIPs that Wave 1+ tasks flip to active behavior assertions one-by-one
- Single `flowlaunch.Execute` seam wrapping `c.ExecuteWorkflow("SkytimeWorkflow", ...)`, shared `BuildWorkflowInput` consumed by webhook ingress, cron reconcile, and `skytime run`, plus AST-firewall test pinning exactly 2 ExecuteWorkflow + 3 BuildWorkflowInput call sites.
- 1. [Rule 3 - Blocking] Firewall non-vacuous scan path correction
- Task 1 — Six new files + three test flips
- 4 of 4 tasks complete.
- `cli.WithCredfile(path)` Option lifted from extbin's lazyCredfileHandler into pkg/cli with a 4-step resolution chain (flag > env > arg > default), collapsing per-subcommand --credfile boilerplate from ~22 lines per file to a single 3-line `applyCredfileFlag(cfg, credfilePath)` call.
- `cli.WithBuildID(string) Option` lets custom binaries set the worker Temporal Build ID via Go code (`cli.WithBuildID("v1.43.0-abcdef")`) instead of `-ldflags "-X github.com/mikelalcon/skytime/pkg/worker.defaultBuildID=$SHA"`; value threads through `cfg.buildID` into `worker.WorkerOptions.BuildID` at both pkg/cli construction seams (run.go transient worker + server.go long-running worker).
- pkg/testing.WithCredentialHandler Option + NewFakeCredentialHandler constructor land in pkg/testing; pkg/cli/test.go threads cfg.credHandler into the harness so `skytime test` sees the same CredentialHandler the binary was built with.
- `examples/http-github-webhook/cmd/extbin/main.go` collapsed from 143 lines to exactly 30 (D-7.4-13 verbatim ≤30); the 18-line "build your own binary" package doc-comment migrated to `pkg/cli/doc.go` as a four-section godoc teaching block (Build your own binary → Credentials → Build ID → Implementation notes), making the pattern discoverable via `go doc github.com/mikelalcon/skytime/pkg/cli` instead of via reading an example.
- INTRP-03/04/05 trace-table flipped to Complete (D-7.4-06) and the bare `http.endpoint(...)` surface gained its first parse-only fixture (D-7.4-07), closing two v1.42.0 audit-debt items in a Wave-1 parallel plan with zero live-runtime test surface added.
- Standalone Go snippets module pinning Temporal SDK v1.42 + stdlib drift-test scaffold + temporal-auth.md page skeleton with four cloud-section placeholders ready for Wave 2 to fill.
- Working `newGCPCredentials` snippet shipped — Temporal Cloud client credentials backed by Google Secret Manager via Workload Identity Federation, with rotation-friendly `NewAPIKeyDynamicCredentials` re-reading the GSM secret on every RPC. Closes AUTH-01.
- Working `newAWSCredentials` snippet shipped — Temporal Cloud client credentials backed by AWS Secrets Manager via IRSA, with rotation-friendly `NewAPIKeyDynamicCredentials` re-reading the secret on every RPC. Closes AUTH-02.
- Working `newAzureCredentials` snippet shipped — Temporal Cloud client credentials backed by Azure Key Vault via Workload Identity, with rotation-friendly `NewAPIKeyDynamicCredentials` re-reading the secret on every RPC. Closes AUTH-03.
- Working `MTLSReloader` snippet shipped — self-hosted Temporal client that rebuilds its mTLS cert chain on SIGHUP without process restart, with full re-dial + drain semantics. Closes AUTH-04. Wave 2 of phase 7.5 is now complete: all four AUTH snippets live and drift-gated.
- Three small markdown edits land the auth-docs page in the project's doc-index surfaces and append the locked "Web UI / dashboard" Out-of-Scope carve-out to PROJECT.md per D-7.5-10. SC1 ("linked from the main README's documentation index") closes; D-7.5-10 closes.
- Single-step append to .github/workflows/ci.yml that runs go build + vet + test inside docs/for-extension-developers/snippets, closing SC2 and gating the drift-test on every push.

---

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
