# Requirements: Skytime v1.43.0 Durability + Triggers

**Defined:** 2026-05-08
**Milestone goal:** Close the durability proof gap (long-running worker mode for replay-after-crash) and add triggering primitives (Starlark `trigger(...)` builtin + `TriggerSource` extension type for HTTP webhooks and cron). Plus consolidate `extbin` boilerplate and document production auth integration patterns.

**Source of truth for design decisions:** [`v1.43-DRAFT-PLAN.md`](v1.43-DRAFT-PLAN.md). Every decision logged there is binding for this milestone.

## v1.43.0 Requirements

Requirements for the v1.43.0 milestone. Each maps to a roadmap phase.

### Trigger Primitive + Sources

- [x] **TRIG-01**: A `.star` file can declare a workflow trigger with `trigger(flow=str, source=TriggerSource, map=lambda payload: {...}, idempotency_key=lambda payload, headers: str, credential=str|None)` as a top-level builtin (separate from `flow()`). The trigger captures references but performs no I/O at parse time.
- [x] **TRIG-02**: Extensions can return a new `TriggerSource` value type (parallel to `ActionRef` for ops). The parser stores `TriggerSource` as an opaque payload on the `dag.Trigger` node; the runtime unpacks it via type-switch.
- [x] **TRIG-03**: A new `dag.Trigger` node type with stable JSON marshaling (`Kind`, `FlowName`, `Source`, `MapLambda`, `IdempotencyLambda`, `CredentialID`). Trigger nodes serialize/deserialize round-trip.
- [x] **TRIG-04**: Parse-time validation rejects malformed triggers (unknown flow name, mismatched source type, missing required source kwargs) with position-aware errors (`<file>:<line>:<col>: <msg>`).
- [x] **TRIG-05**: Boot registry walks `--rootdir` recursively for `*.star` files (skipping `*_test.star` per Phase 5 contract), parses each, and registers flows AND triggers found inside. Flow registry and trigger registry share the same scan — one source of truth.
- [x] **TRIG-06**: A built-in HTTP listener mounts an HTTP handler per registered HTTP-shaped trigger source. Handlers route incoming requests through signature validation → payload map → idempotency key → `client.ExecuteWorkflow`.
- [x] **TRIG-07**: A `triggers.github_webhook(events=[...], secret_credential=str|None)` source factory ships in `pkg/extension/builtin/triggers/`. Returns a `TriggerSource` that registers a `POST /webhook/github` handler.
- [x] **TRIG-08**: A `triggers.generic_http_webhook(path=str, method=str, secret_credential=str|None)` source factory ships alongside `github_webhook`. Returns a `TriggerSource` that registers an arbitrary HTTP path/method handler.
- [x] **TRIG-09**: GitHub webhook signature validation uses HMAC-SHA256 against the `X-Hub-Signature-256` header. The signing secret resolves JIT via the existing `CredentialHandler.Resolve(ctx, id)` — same plumbing as activity-side credentials, secrets stay wrapped in `extension.Secret`.
- [x] **TRIG-10**: Idempotency mapping: the `idempotency_key` lambda result becomes the Temporal `WorkflowID` with `WorkflowIDReusePolicy=REJECT_DUPLICATE`. GitHub redeliveries with the same `X-GitHub-Delivery` ID dedup automatically.

### Server Subcommand

- [x] **SERVER-01**: A new `skytime server --rootdir=... --task-queue=... --temporal=... --addr=... [--credfile=...]` subcommand runs a long-lived process: starts a Temporal worker registered against the task queue, mounts the HTTP listener (port from `--addr`), and stays up until SIGTERM/SIGINT.
- [x] **SERVER-02**: SIGTERM gracefully drains in-flight workflows up to a configurable `--drain-timeout` (default 30s, matching Kubernetes `terminationGracePeriodSeconds`). Refuses new HTTP requests during drain. Forces shutdown after timeout.
- [x] **SERVER-03**: `skytime server` startup logs the registered flows AND triggers in deterministic order (sorted by name). The reader can confirm at-a-glance what's mounted before any traffic flows.

### Cron Triggers (Temporal Schedules)

- [x] **SCHED-01**: A `triggers.cron(schedule=str, timezone=str|None, overlap=str|None, catchup_window=duration|None)` source factory ships alongside webhook sources. `schedule` accepts standard 5-field POSIX cron syntax. Returns a `TriggerSource` not associated with HTTP.
- [x] **SCHED-02**: Cron triggers are backed by Temporal Schedules (durable, server-side) — not in-process polling. The `skytime server` startup creates/updates Temporal Schedule resources matching each `cron(...)`-shaped trigger.
- [x] **SCHED-03**: Schedule reconciliation at boot creates/updates/deletes Temporal Schedules to match the parsed registry. A `--reconcile=strict|preserve|dry-run` flag controls deletion safety: `strict` deletes orphan Schedules, `preserve` leaves them in place, `dry-run` reports what would change without applying.

### Dashboard

- [x] **UI-01**: `GET /` renders a live workflow list via `client.ListWorkflow`. Auto-refreshes via polling (no WebSocket complexity). Shows workflow ID, flow name, status (running/completed/failed/replayed), start time.
- [x] **UI-02**: A "Recent webhook deliveries" section shows the last 100 incoming webhook deliveries (in-memory ring buffer; not persistent). Each entry shows source, headers, payload summary, mapped workflow ID.
- [x] **UI-03**: A manual trigger form: dropdown enumerating registered flows + JSON textarea for input + "Run" button. POSTs to `/api/trigger` which calls `client.ExecuteWorkflow` with the typed input.
- [x] **UI-04**: Manual trigger reuses the same `executeFlow` code path as webhook ingress, minus signature validation and idempotency mapping. Single source of truth for "spawn a workflow"; HTTP ingress, manual UI, and (later) cron all converge there.

### CLI Surface (continuing from CLI-07)

- [x] **CLI-08**: A new `cli.WithCredfile(path string)` option lifts `lazyCredfileHandler` from `extbin/main.go` into `pkg/cli`. `path` empty falls back to default `$HOME/.skytime-credentials`. Lazy construction: defers `credfile.New()` until first `Resolve()` call.
- [x] **CLI-09**: A new `cli.WithBuildID(string)` option lets custom binaries set the worker Build ID without resorting to `-ldflags` injection. Default still `defaultBuildID` (typically `dev` or build-time-injected git SHA) when option is absent.
- [x] **CLI-10**: A new `pkg/testing.WithCredentialHandler(h)` option threads a credential handler into the Tier-3 test harness. Future tests using partial mocks against real credentials can satisfy `Resolve()` calls without spawning the full activity environment.
- [x] **CLI-11**: `pkg/cli/test.go` threads `cfg.credHandler` to `pkg/testing.RunCLI`. The CLI test path uses the same credential handler that `run` and `validate` use.
- [x] **CLI-12**: `examples/http-github-webhook/cmd/extbin/main.go` collapses to ≤30 lines after CLI-08 + CLI-09 land: extension registration + `cli.NewRootCommand(WithExtensions(...), WithCredfile(...), WithBuildID(...)).ExecuteContext(ctx)`. The "build your own binary" pattern is visibly tiny.
- [x] **CLI-13**: `skytime dev-server` renamed to `skytime dev-temporal`. All docs (README, getting-started.md, cli.md, extension docs, example READMEs), CI smoke scripts (`walkthrough_smoke.sh`), and tutorial examples updated. Pre-1.0 — no deprecation alias.

### Example Project (continuing from EX-04)

- [x] **EX-05**: The example project's README gains a "GitHub webhook trigger walkthrough" section using `gh webhook forward` for installation. Reader can trigger flows via real GitHub events without setting up tunnels or registering OAuth apps. Includes the crash-recovery demo: open page, click trigger, kill server mid-flow, restart, watch workflow complete.

### Auth Documentation

- [x] **AUTH-01**: `docs/for-extension-developers/temporal-auth.md` ships with a working WIF → Google Secret Manager → Temporal Cloud snippet (Go code). Customer's GCP workload identity issues short-lived tokens, secret manager hands a Temporal Cloud API key, `client.Credentials` rotates on a refresh callback.
- [x] **AUTH-02**: An IRSA → AWS Secrets Manager → Temporal Cloud snippet in temporal-auth.md. Customer's k8s service account assumes an IAM role, secret manager hands the Temporal credential, rotation handled the same way.
- [x] **AUTH-03**: An Azure Workload Identity → Key Vault → Temporal Cloud snippet in temporal-auth.md.
- [ ] **AUTH-04**: A self-hosted mTLS reload-on-SIGHUP snippet in temporal-auth.md. Production cluster rotates client certs; SIGHUP triggers `client.Options.ConnectionOptions.TLS` reload without restart.

### Structured Logging Step Builtin

- [x] **LOG-01**: A `.star` author can write `log.info(msg, attrs=lambda ctx: dict)` (and `log.warn` / `log.error` / `log.debug`) as a step inside flow(...) bodies. The msg supports `${ctx.expr}` interpolation per D4.1-22; attrs is an optional lambda returning a dict of structured key-value pairs. Empty msg, multi-line msg, and missing attrs all parse cleanly; non-literal msg and module-scope placement reject with position-aware `*dag.ParseError`.
- [x] **LOG-02**: At workflow time the walker emits a structured slog record via `workflow.GetLogger(ctx)` at the matching level (Info / Warn / Error / Debug) with the message decorated as `[skytime/log] <msg>`. The record is replay-safe (Temporal `ReplayLogger` suppresses on replay). In `skytime server` human stdout the user-message record surfaces as one line per `log.<level>` call; the bookend `event=step_dispatch kind=log` and `event=step_complete kind=log` records are suppressed by the renderer (`logKindFilterHandler` + `progressHandler` early-return on `kind=="log"`). In `--json-log` mode all three records pass through verbatim for downstream log-analysis tooling.

## v1.44+ Requirements (Deferred)

Tracked but not in this milestone's scope.

### Triggers
- **TRIG-V2-01**: Webhook signature schemes beyond HMAC-SHA256 (Stripe-style timestamp+signature, AWS SNS, etc.) — extension authors can ship their own; first-party support deferred until customer demand
- **TRIG-V2-02**: 6-field cron syntax with seconds — defer until customer asks (5-field POSIX is the common case and what Temporal Schedules accepts natively)
- **TRIG-V2-03**: Cross-source idempotency framework with shared deduplication store — current design has each source declare its own `idempotency_key` lambda; framework only justified when 3+ customers ask for it

### Worker Credentials Helpers
- **WORK-V2-01**: `pkg/worker/credentials/RefreshingAPIKeyCredentials` wrapping `func() (string, error)` — defer until at least one customer hits the friction
- **WORK-V2-02**: `pkg/worker/credentials/ReloadingTLSCredentials` watching a cert path with auto-reload — same deferral pattern

### Dashboard
- **UI-V2-01**: WebSocket / SSE for real-time workflow updates (vs. polling) — polling is sufficient for v1.43
- **UI-V2-02**: Authentication on the dashboard itself — assume same trust boundary as the worker process for v1.43; add basic-auth/OIDC later if customers expose the dashboard
- **UI-V2-03**: Multi-tenancy in dashboard (filter by namespace/team/queue) — single-tenant is the v1.43 scope

## Out of Scope

Explicit exclusions for v1.43 with reasoning.

- **`pkg/triggers/` framework with HTTP/Cron/Queue interfaces** — we considered a generic library here. Rejected: trigger plumbing is highly source-specific (GitHub vs Stripe vs SQS vs custom-internal each have different signature schemes, payload shapes, idempotency requirements). A generic abstraction would be too thin (everyone bypasses) or too opinionated (no one fits). v1.43 ships sources as concrete extensions; promote to `pkg/triggers/` only if 3+ customers want the same shape.
- **Embedded Temporal Schedules engine** — Temporal Cloud's Schedules API is the right primitive. We don't need to reimplement it.
- **Built-in cron parser library beyond Temporal Schedules' acceptance** — pass cron strings through to Temporal; let it own parsing/validation/timezone semantics.
- **Schema-aware manual trigger form** (per-input typed fields) — JSON textarea is enough; per-type field rendering is creep.
- **JS/CSS framework in dashboard** — stdlib `net/http` + `html/template` only. If we let it grow Vue/React we've changed what Skytime is.
- **Trigger primitives for queue sources (SQS, Pub/Sub, Kafka)** — Temporal's own message-queue integrations cover this; not a Skytime concern.
- **Hot-reload of `.star` files in `skytime server`** — server boot is the registration boundary; reloads require restart for v1.43. Hot-reload was already deferred per PROJECT.md.
- **Authentication on dashboard** — single trust boundary with the worker process; if you expose the dashboard, you're trusted. Auth is a v1.44+ concern when customers expose this beyond local/internal.

## Traceability

| Requirement | Phase | Source Plan | Status |
|-------------|-------|-------------|--------|
| TRIG-01 | Phase 7 | — | Pending |
| TRIG-02 | Phase 7 | — | Pending |
| TRIG-03 | Phase 7 | — | Pending |
| TRIG-04 | Phase 7 | — | Pending |
| TRIG-05 | Phase 7 | — | Pending |
| TRIG-06 | Phase 7.1 | — | Pending |
| TRIG-07 | Phase 7.1 | — | Pending |
| TRIG-08 | Phase 7.1 | — | Pending |
| TRIG-09 | Phase 7.1 | — | Pending |
| TRIG-10 | Phase 7.1 | — | Pending |
| SERVER-01 | Phase 7 | — | Pending |
| SERVER-02 | Phase 7 | — | Pending |
| SERVER-03 | Phase 7 | — | Pending |
| SCHED-01 | Phase 7.2 | — | Pending |
| SCHED-02 | Phase 7.2 | — | Pending |
| SCHED-03 | Phase 7.2 | — | Pending |
| UI-01 | Phase 7.3 | — | Pending |
| UI-02 | Phase 7.3 | — | Pending |
| UI-03 | Phase 7.3 | — | Pending |
| UI-04 | Phase 7.3 | — | Pending |
| CLI-08 | Phase 7.4 | — | Pending |
| CLI-09 | Phase 7.4 | — | Pending |
| CLI-10 | Phase 7.4 | — | Pending |
| CLI-11 | Phase 7.4 | — | Pending |
| CLI-12 | Phase 7.4 | — | Pending |
| CLI-13 | Phase 7 | — | Pending |
| EX-05 | Phase 7.1 | — | Pending |
| AUTH-01 | Phase 7.5 | 07.5-02-snippet-gcp-wif | Complete |
| AUTH-02 | Phase 7.5 | 07.5-03-snippet-aws-irsa | Complete |
| AUTH-03 | Phase 7.5 | 07.5-04-snippet-azure-wi | Complete |
| AUTH-04 | Phase 7.5 | — | Pending |
| LOG-01 | Phase 07.2.1 | 07.2.1-02-parser-builtins, 07.2.1-05-migration-walkthrough | Complete |
| LOG-02 | Phase 07.2.1 | 07.2.1-01-dag-logstep-type, 07.2.1-03-walker-counter-replay, 07.2.1-04-renderer-suppression, 07.2.1-05-migration-walkthrough | Complete |

**Total:** 33 requirements across 8 categories, mapped to 7 phases. `Source Plan` column is `—` placeholder; `/gsd:plan-phase` fills it as plans land.

## Coverage Summary

| Category | Count | Phase(s) |
|----------|-------|----------|
| TRIG (trigger primitive + sources) | 10 | 7, 7.1 |
| SERVER (long-running subcommand) | 3 | 7 |
| SCHED (cron via Temporal Schedules) | 3 | 7.2 |
| UI (dashboard) | 4 | 7.3 |
| CLI (API + rename) | 6 | 7, 7.4 |
| EX (example project addition) | 1 | 7.1 |
| AUTH (production auth docs) | 4 | 7.5 |
| LOG (structured logging step builtin) | 2 | 7.2.1 |

---
*Created: 2026-05-08 — v1.43.0 Durability + Triggers milestone opened*
*Updated: 2026-05-08 — added `Source Plan` column on roadmap creation*
