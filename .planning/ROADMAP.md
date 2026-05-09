# Roadmap: Skytime

## Milestones

- ✅ **v1.42.0 Foundation** — Phases 1–6 (shipped 2026-05-08) — see [`MILESTONES.md`](MILESTONES.md)
- 📋 **v1.43.0 Durability + Triggers** — Phases 7+ (planned) — see [`v1.43-DRAFT-PLAN.md`](v1.43-DRAFT-PLAN.md)

## Phases

<details>
<summary>✅ v1.42.0 Foundation (Phases 1–6) — SHIPPED 2026-05-08</summary>

- [x] Phase 1 — Type spine, extension contract, parser/bridge foundations (5/5 plans) — 2026-04-27
- [x] Phase 2 — Generic activity, block-batch dispatch, credentials (3/3 plans) — 2026-04-29
- [x] Phase 3 — Lambda-serialization, interpreter, worker (4/4 plans) — 2026-04-30
- [x] Phase 4 — Static validation, CLI skeleton (7/7 plans) — 2026-05-02
- [x] Phase 04.1 — Dynamic step kwargs + `${ctx.expr}` interpolation (8/8 plans) — 2026-05-03
- [x] Phase 04.2 — `if_cond` expression mode + strict-equality binding (7/7 plans) — 2026-05-04
- [x] Phase 04.3 — Documentation + source-driven reference generator (9/9 plans) — 2026-05-04
- [x] Phase 5 — Tier-3 E2E test harness (`temporal_test`) (6/6 plans) — 2026-05-05
- [x] Phase 6 — Example project (HTTP + GitHub + Webhook) (9/9 plans) — 2026-05-07

Full archive: [`milestones/v1.42.0-ROADMAP.md`](milestones/v1.42.0-ROADMAP.md)

</details>

### 📋 v1.43.0 Durability + Triggers (Planned)

Closes the two real gaps surfaced by Phase 6: no long-running worker mode (so Temporal can replay after worker crash), and no triggering primitive (so external events become workflow invocations). Adds `trigger(...)` Starlark builtin, `skytime server` long-running subcommand, HTTP webhook receiver, cron via Temporal Schedules, dashboard, and auth integration docs.

- [ ] **Phase 7: Trigger primitive + server shell** — Top-level `trigger(...)` parser builtin + `dag.Trigger` node + `TriggerSource` extension type + `skytime server` long-running subcommand shell + rename `dev-server` → `dev-temporal`
- [ ] **Phase 7.1: HTTP webhook receiver + GitHub source** — HTTP listener mounted by `server`, `triggers.github_webhook` and `triggers.generic_http_webhook` source factories, HMAC signature validation, idempotency via `WorkflowIDReusePolicy`, `gh webhook forward` walkthrough
- [ ] **Phase 7.2: Cron triggers via Temporal Schedules** — `triggers.cron(...)` source backed by Temporal Schedules (durable, server-side), reconciliation at boot with `--reconcile=strict|preserve|dry-run` safety flag
- [ ] **Phase 7.3: Dashboard + manual trigger page** — Stdlib-only HTML dashboard (`net/http` + `html/template`); live workflow list, recent webhook deliveries ring buffer, manual trigger form sharing the same `executeFlow` code path as ingress
- [ ] **Phase 7.4: extbin consolidation + tech debt cleanup** — `cli.WithCredfile` / `cli.WithBuildID` / `pkg/testing.WithCredentialHandler` options; collapse `extbin/main.go` to ≤30 lines; close v1.42.0 audit tech debt items
- [ ] **Phase 7.5: Auth documentation** — Production cloud-native credential rotation patterns (WIF→GSM, IRSA→AWS Secrets Manager, Azure WI→Key Vault, mTLS reload-on-SIGHUP) in `docs/for-extension-developers/temporal-auth.md`

Full draft plan: [`v1.43-DRAFT-PLAN.md`](v1.43-DRAFT-PLAN.md)

## Phase Details

### Phase 7: Trigger primitive + server shell
**Goal**: Establish the foundation for triggers and durable worker mode in one phase. Add the top-level `trigger(...)` Starlark builtin (parser builtin, separate from `flow()`), the new `dag.Trigger` node type with stable JSON marshaling, the `TriggerSource` value type returned by extension factories (parallel to `ActionRef`), and parse-time validation. Extend the boot registry to walk `--rootdir` and register flows AND triggers from the same `.star` files. Ship the `skytime server --rootdir=... --task-queue=... --temporal=... --addr=... [--credfile=...]` subcommand shell as a long-lived process: registers flows + triggers, starts a Temporal worker, prints the registered set in deterministic order on startup, drains in-flight workflows on SIGTERM/SIGINT up to a configurable `--drain-timeout` (default 30s, matching Kubernetes), and exits cleanly. Rename `skytime dev-server` to `skytime dev-temporal` across code + docs + CI smoke scripts (pre-1.0, no deprecation alias). No HTTP receiver yet — that's 7.1.
**Depends on**: Phase 6 (v1.42.0 shipped — all foundation primitives in place)
**Requirements**: TRIG-01, TRIG-02, TRIG-03, TRIG-04, TRIG-05, SERVER-01, SERVER-02, SERVER-03, CLI-13
**Success Criteria** (what must be TRUE):
  1. A consultant can write `trigger(flow="check_user", source=triggers.github_webhook(events=["push"]), map=lambda payload: {"repo": payload.repository.full_name}, idempotency_key=lambda payload, headers: headers["X-GitHub-Delivery"], credential="gh-secret")` at the top level of a `.star` file alongside `flow(...)` declarations, parse it without I/O, and inspect the resulting `*dag.Trigger` node in a Go unit test (FlowName, Source opaque payload, MapLambda, IdempotencyLambda, CredentialID populated)
  2. A malformed trigger (unknown flow name, missing required source kwarg, source not a `TriggerSource` value, malformed lambda free vars) produces a `<file>:<line>:<col>: <msg>` error at parse time and never panics
  3. `skytime server --rootdir=examples/http-github-webhook/ --task-queue=demo --temporal=localhost:7233` starts, walks the rootdir for `*.star` files (skipping `*_test.star`), prints `registered flows: [...]` and `registered triggers: [...]` in name-sorted order, runs a Temporal worker against the task queue, and stays up until SIGTERM
  4. SIGTERM during in-flight workflow execution waits up to `--drain-timeout` (default 30s) for those workflows to complete via Temporal worker drain semantics, then exits with status 0 if drained cleanly or status 1 with a timeout message if forced
  5. `skytime dev-temporal` exists and works identically to the prior `skytime dev-server`; `skytime dev-server` no longer exists; README, getting-started.md, cli.md, example READMEs, and `.github/workflows/scripts/walkthrough_smoke.sh` all reference the new name with no stale `dev-server` invocations remaining
**Plans**: TBD
**UI hint**: no

### Phase 7.1: HTTP webhook receiver + GitHub source
**Goal**: Make the `skytime server` listener actually receive webhooks. Mount an HTTP handler per registered HTTP-shaped trigger source, ship the first two source factories under `pkg/extension/builtin/triggers/`: `triggers.github_webhook(events=[...], secret_credential=...)` and `triggers.generic_http_webhook(path=..., method=..., secret_credential=...)`. Each handler routes incoming requests through signature validation → payload map lambda → idempotency key lambda → `client.ExecuteWorkflow`. GitHub signing uses HMAC-SHA256 against `X-Hub-Signature-256`, with the secret resolved JIT via the existing `CredentialHandler.Resolve(ctx, id)` so secrets stay wrapped in `extension.Secret`. Idempotency mapping sets the Temporal `WorkflowID` to the lambda result with `WorkflowIDReusePolicy=REJECT_DUPLICATE`, so GitHub redeliveries with the same `X-GitHub-Delivery` ID dedup automatically. The example project's README gains a "GitHub webhook trigger walkthrough" section using `gh webhook forward` (no tunnels, no OAuth app registration), including the crash-recovery demo: open dashboard, trigger flow, kill server mid-execution, restart, watch the workflow complete.
**Depends on**: Phase 7
**Requirements**: TRIG-06, TRIG-07, TRIG-08, TRIG-09, TRIG-10, EX-05
**Success Criteria** (what must be TRUE):
  1. With a `.star` file declaring `trigger(flow=..., source=triggers.github_webhook(events=["issues"], secret_credential="gh-webhook-secret"))`, `skytime server` mounts `POST /webhook/github` and a real GitHub webhook delivery (or `gh webhook forward`) with a valid `X-Hub-Signature-256` header triggers the workflow; an invalid signature returns HTTP 401 and does not invoke `ExecuteWorkflow`
  2. With a `triggers.generic_http_webhook(path="/hooks/custom", method="POST", secret_credential=None)` source, `skytime server` mounts `POST /hooks/custom` and a request body's lambda-mapped payload becomes the workflow input
  3. Two GitHub deliveries with the same `X-GitHub-Delivery` ID, posted within the same retention window, dedupe: only the first triggers a workflow run; the second receives a 200 OK with a "duplicate; skipped" response and no second workflow appears in `client.ListWorkflow`
  4. The example project README's "GitHub webhook trigger walkthrough" section runs end-to-end against `skytime dev-temporal` + `gh webhook forward`, taking a reader from `git clone` to a webhook-triggered workflow in the documented commands
  5. The crash-recovery demo runs as documented: a flow triggered via webhook continues from event history after `kill -9 $SERVER_PID` + restart; verifiable by watching the dashboard or `client.DescribeWorkflowExecution`
**Plans**: 9 plans
- [x] 07.1-01-PLAN.md — pkg/extension/receiver foundation (signature validation, WorkflowID composer, status mapping, log line shape, HTTPMounter sub-interface)
- [x] 07.1-02-PLAN.md — http.webhook source factory (configurable signature scheme; sha256/sha1/sha512 allowlist)
- [x] 07.1-03-PLAN.md — github.webhook source factory (HMAC-SHA256 + X-Hub-Signature-256 hardcoded per TRIG-09; ShouldDispatch event filter)
- [ ] 07.1-04-PLAN.md — receiver Mount + skeleton + Deps validation + http.webhook accessor methods
- [ ] 07.1-04b-PLAN.md — receiver per-request handler pipeline (body limit, JIT credential, signature, lambda eval, ExecuteWorkflow with REJECT_DUPLICATE; .Reveal() leak gate)
- [x] 07.1-05-PLAN.md — worker.WithSDKFactory Option + unskip 3 Phase 7 signal-loop tests
- [ ] 07.1-06-PLAN.md — pkg/cli/server.go listener bind + listener-first shutdown + reasonable HTTP defaults
- [ ] 07.1-07-PLAN.md — examples/http-github-webhook/webhook_demo.star crash-recovery demo flow + trigger
- [ ] 07.1-08-PLAN.md — walkthrough docs (5min README + full docs/walkthroughs/) + firewall extension + headings gate
**UI hint**: no

### Phase 7.2: Cron triggers via Temporal Schedules
**Goal**: Cron triggers backed by Temporal Schedules (durable, server-side) — not in-process polling. Ship `triggers.cron(schedule=str, timezone=str|None, overlap=str|None, catchup_window=duration|None)` alongside the webhook sources; `schedule` accepts standard 5-field POSIX cron syntax (Temporal Schedules' native acceptance). At `skytime server` startup, reconcile Temporal Schedule resources to match the parsed registry: create new ones, update changed ones, and delete orphans (ones present on the cluster but not in the parsed registry). A `--reconcile=strict|preserve|dry-run` flag governs deletion safety: `strict` deletes orphan Schedules, `preserve` leaves them in place, `dry-run` reports what would change without applying. Cron triggers don't carry a per-trigger `credential` kwarg — their flows resolve credentials normally inside steps (no per-invocation HTTP headers exist for cron).
**Depends on**: Phase 7
**Requirements**: SCHED-01, SCHED-02, SCHED-03
**Success Criteria** (what must be TRUE):
  1. A `.star` file declaring `trigger(flow="weekly_digest", source=triggers.cron(schedule="0 9 * * 1", timezone="America/New_York"))` parses successfully and, at `skytime server` boot, creates a corresponding Temporal Schedule resource on the connected cluster — verifiable via `temporal schedule list`
  2. Removing that `trigger(...)` from the `.star` file and restarting with `--reconcile=strict` deletes the Temporal Schedule from the cluster; restarting with `--reconcile=preserve` leaves it in place and logs a warning
  3. `skytime server --rootdir=... --reconcile=dry-run` reports a plan listing schedule creates / updates / deletes that would happen, exits without applying any change to the cluster, and exits with status 0
  4. A 5-field POSIX cron string is accepted at parse time; a 6-field (with-seconds) string or a malformed string produces a position-aware parse error before any Schedule API call
  5. With a server up and a cron trigger fired by Temporal, the corresponding workflow appears in `client.ListWorkflow` at the scheduled time, verified by an end-to-end test or smoke against `skytime dev-temporal`
**Plans**: TBD
**UI hint**: no

### Phase 7.3: Dashboard + manual trigger page
**Goal**: Single-page stdlib-only dashboard so the durability story is visually demoable. `GET /` renders a live workflow list via `client.ListWorkflow` (workflow ID, flow name, status running/completed/failed/replayed, start time) with auto-refresh via polling (no WebSocket complexity). A "Recent webhook deliveries" section shows the last 100 incoming deliveries (in-memory ring buffer, not persistent) with source, headers, payload summary, mapped workflow ID. A manual trigger form: dropdown enumerating registered flows + JSON textarea for input + "Run" button, POSTing to `/api/trigger` which calls `client.ExecuteWorkflow`. Crucially, manual trigger reuses the same `executeFlow` code path as webhook ingress (minus signature validation and idempotency mapping) so HTTP ingress, manual UI, and (later) cron all converge on a single source of truth for "spawn a workflow". Stdlib only — `net/http` + `html/template`. No JS framework, no external CSS, no bundler. Lives entirely under `pkg/cli/server/web/` (or similar). PROJECT.md "Web UI / dashboard" Out-of-Scope entry gets an explicit carve-out: this is a teaching reference, not a Skytime product feature.
**Depends on**: Phase 7.1 (uses HTTP listener + ingress code path)
**Requirements**: UI-01, UI-02, UI-03, UI-04
**Success Criteria** (what must be TRUE):
  1. With `skytime server` up and at least one running workflow, opening `http://localhost:8080/` in a browser shows a workflow list with workflow ID, flow name, status, and start time; the page auto-refreshes (via polling) and a workflow that completes appears as "completed" without manual reload
  2. After a webhook delivery (or several), the dashboard's "Recent webhook deliveries" section shows the last 100 entries with source, header summary, payload summary, and mapped workflow ID; older entries roll out of the ring buffer as new ones arrive
  3. Selecting a flow from the dropdown, pasting valid JSON into the textarea, and clicking "Run" POSTs to `/api/trigger`, calls `client.ExecuteWorkflow`, and the new workflow appears in the workflow list within one polling cycle
  4. The dashboard ships with no JS framework, no external CSS, no bundler — a `find` over `pkg/cli/server/web/` returns only `*.html`, `*.go` files; the firewall test that bans cobra/charm-log outside `pkg/cli` continues to pass and a new firewall test bans non-stdlib HTTP/template imports under `pkg/cli/server/web/`
  5. `executeFlow` is invoked from three call sites (webhook ingress, manual trigger POST, and cron via Temporal Schedule callbacks) — verified by a Go test that asserts call-site count, ensuring no code-path duplication
**Plans**: TBD
**UI hint**: yes

### Phase 7.4: extbin consolidation + tech debt cleanup
**Goal**: Make "build your own binary" visibly tiny and close v1.42.0 audit tech debt items in one focused phase. Lift `lazyCredfileHandler` from `examples/http-github-webhook/cmd/extbin/main.go` into `pkg/cli` as a new `cli.WithCredfile(path string)` option (empty path falls back to `$HOME/.skytime-credentials`; lazy construction defers `credfile.New()` until first `Resolve()`). Add `cli.WithBuildID(string)` so custom binaries set the worker Build ID without `-ldflags` injection (default still `defaultBuildID`). Add `pkg/testing.WithCredentialHandler(h)` and thread `cfg.credHandler` through `pkg/cli/test.go` to `pkg/testing.RunCLI`. With those landed, `extbin/main.go` collapses to ≤30 lines: extension registration + `cli.NewRootCommand(WithExtensions(...), WithCredfile(...), WithBuildID(...)).ExecuteContext(ctx)`.
**Depends on**: Phase 7.3 (final extbin shape only known after dashboard + server modes are wired through `pkg/cli`)
**Requirements**: CLI-08, CLI-09, CLI-10, CLI-11, CLI-12
**Success Criteria** (what must be TRUE):
  1. `cli.WithCredfile("")` and `cli.WithCredfile("/custom/path.toml")` both work end-to-end: `skytime run --extbin -- --credential=...` resolves credentials lazily through the option-installed handler, and a unit test verifies `credfile.New()` is not called until first `Resolve()`
  2. `cli.WithBuildID("v1.43.0-abcdef")` in a custom binary makes `worker.WorkerOptions.BuildID == "v1.43.0-abcdef"`, verified by a Go test reading the configured options; absent the option, the default (`defaultBuildID`, typically `dev` or build-time-injected SHA) is preserved
  3. `pkg/testing.WithCredentialHandler(h)` threads `h` into the Tier-3 test harness; a Go test demonstrates a partial-mock test using a real credential handler and a router callback for action mocks, exercising the same `Resolve()` path production uses
  4. `examples/http-github-webhook/cmd/extbin/main.go` is ≤30 lines (verified by `wc -l`); it consists of extension registration + a single `cli.NewRootCommand(...)` chain + `ExecuteContext(ctx)`, with no `lazyCredfileHandler` definition remaining
  5. The Phase 6 walkthrough (`gh webhook forward` smoke) and webhook + cron + dashboard end-to-end paths still pass after the consolidation; CI gates remain green with the slimmer extbin
**Plans**: TBD
**UI hint**: no

### Phase 7.5: Auth documentation
**Goal**: Document common cloud-native credential rotation patterns for production Temporal connections so customers can connect to Temporal Cloud, self-hosted clusters, and dev-server with credentials sourced from organization-standard chains. Ship `docs/for-extension-developers/temporal-auth.md` with four working snippets: WIF → Google Secret Manager → Temporal Cloud (GCP), IRSA → AWS Secrets Manager → Temporal Cloud (AWS), Azure Workload Identity → Key Vault → Temporal Cloud (Azure), and self-hosted mTLS with reload-on-SIGHUP. Each snippet is a working Go code block using `client.Credentials` rotation patterns; reader copies, fills in their identifiers, runs. Doc-only phase — no library code changes. Independent of phases 7.1–7.4 (can ship anytime after Phase 7).
**Depends on**: Phase 7 (server subcommand exists; auth patterns reference it)
**Requirements**: AUTH-01, AUTH-02, AUTH-03, AUTH-04
**Success Criteria** (what must be TRUE):
  1. `docs/for-extension-developers/temporal-auth.md` exists and is linked from the main README's documentation index; the page has four major sections (GCP/WIF, AWS/IRSA, Azure/WI, self-hosted/mTLS-SIGHUP), each with a working Go snippet and a "what this assumes" + "what to substitute" preamble
  2. Each Go snippet compiles cleanly against `go.temporal.io/sdk@v1.42.0` (verified by a `go vet` or build test running over snippets extracted from the markdown), and uses `client.Credentials` rotation patterns rather than ad-hoc one-shot reads
  3. The mTLS reload-on-SIGHUP snippet shows a complete pattern: `signal.Notify(c, syscall.SIGHUP)` → reload `client.Options.ConnectionOptions.TLS` → reconnect without a process restart; readers can adapt verbatim for production cert rotation
  4. The four snippets collectively cover the three-cloud + self-hosted matrix the milestone targets; a reader operating in any of those environments has a starting point that does not require Skytime-internal knowledge to adopt
**Plans**: TBD
**UI hint**: no

## Progress

**Execution Order:**
Phases execute in numeric order: 7 → 7.1 / 7.2 (parallel) → 7.3 → 7.4; 7.5 independent after 7

| Phase | Milestone | Plans | Status | Completed |
|-------|-----------|-------|--------|-----------|
| 1. Type spine + parser/bridge | v1.42.0 | 5/5 | Complete | 2026-04-27 |
| 2. Generic activity + credentials | v1.42.0 | 3/3 | Complete | 2026-04-29 |
| 3. Interpreter + worker | v1.42.0 | 4/4 | Complete | 2026-04-30 |
| 4. Static validation + CLI | v1.42.0 | 7/7 | Complete | 2026-05-02 |
| 04.1. Dynamic step kwargs | v1.42.0 | 8/8 | Complete | 2026-05-03 |
| 04.2. if_cond expression mode | v1.42.0 | 7/7 | Complete | 2026-05-04 |
| 04.3. Documentation + docgen | v1.42.0 | 9/9 | Complete | 2026-05-04 |
| 5. Tier-3 test harness | v1.42.0 | 6/6 | Complete | 2026-05-05 |
| 6. Example project | v1.42.0 | 9/9 | Complete | 2026-05-07 |
| 7. Trigger primitive + server shell | v1.43.0 | 0/TBD | Not started | — |
| 7.1. HTTP webhook receiver | v1.43.0 | 0/8 | Not started | — |
| 7.2. Cron triggers | v1.43.0 | 0/TBD | Not started | — |
| 7.3. Dashboard | v1.43.0 | 0/TBD | Not started | — |
| 7.4. extbin consolidation | v1.43.0 | 0/TBD | Not started | — |
| 7.5. Auth docs | v1.43.0 | 0/TBD | Not started | — |

---
*Roadmap created: 2026-04-26*
*Last updated: 2026-05-08 — v1.43.0 phase details populated (6 phases, 31 requirements mapped)*
