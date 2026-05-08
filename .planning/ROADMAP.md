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

- [ ] Phase 7 — `trigger(...)` parser primitive + `TriggerSource` extension type + `server` shell + rename `dev-server` → `dev-temporal`
- [ ] Phase 7.1 — HTTP webhook receiver + `triggers` extension (github_webhook, generic_http_webhook) + idempotency + signature validation
- [ ] Phase 7.2 — Cron triggers backed by Temporal Schedules
- [ ] Phase 7.3 — Dashboard + manual trigger page (stdlib only)
- [ ] Phase 7.4 — Consolidate `extbin` → thin shim; lift `lazyCredfileHandler` into `pkg/cli`; add `WithBuildID` / `WithCredentialHandler`
- [ ] Phase 7.5 — Auth docs (WIF / IRSA / mTLS-reload patterns)

Full draft plan: [`v1.43-DRAFT-PLAN.md`](v1.43-DRAFT-PLAN.md)

## Progress

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
| 7.1. HTTP webhook receiver | v1.43.0 | 0/TBD | Not started | — |
| 7.2. Cron triggers | v1.43.0 | 0/TBD | Not started | — |
| 7.3. Dashboard | v1.43.0 | 0/TBD | Not started | — |
| 7.4. extbin consolidation | v1.43.0 | 0/TBD | Not started | — |
| 7.5. Auth docs | v1.43.0 | 0/TBD | Not started | — |

---
*Roadmap created: 2026-04-26*
*Last updated: 2026-05-08 after v1.42.0 milestone completion*
