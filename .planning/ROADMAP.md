# Roadmap: Skytime

## Milestones

- ✅ **v1.42.0 Foundation** — Phases 1–6 (shipped 2026-05-08) — see [`MILESTONES.md`](MILESTONES.md)
- ✅ **v1.43.0 Durability + Triggers** — Phases 7–7.5 (shipped 2026-05-13) — see [`milestones/v1.43.0-ROADMAP.md`](milestones/v1.43.0-ROADMAP.md)

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

<details>
<summary>✅ v1.43.0 Durability + Triggers (Phases 7–7.5) — SHIPPED 2026-05-13</summary>

Closed the two gaps surfaced by Phase 6: added the long-running `skytime server` worker mode (so Temporal can replay after worker crash) and the `trigger(...)` Starlark builtin (so external events become workflow invocations). Plus stdlib-only dashboard, cron via Temporal Schedules, structured logging step builtin, extbin consolidation, and production auth-rotation docs.

- [x] Phase 7 — Trigger primitive + server shell (6/6 plans) — 2026-05-08
- [x] Phase 7.1 — HTTP webhook receiver + GitHub source (9/9 plans) — 2026-05-11
- [x] Phase 7.2 — Cron triggers via Temporal Schedules (4/4 plans) — 2026-05-11
- [x] Phase 07.2.1 — Structured logging step builtin (5/5 plans) — 2026-05-12
- [x] Phase 7.3 — Dashboard + manual trigger page (6/6 plans) — 2026-05-13
- [x] Phase 7.4 — extbin consolidation + tech debt cleanup (5/5 plans) — 2026-05-13
- [x] Phase 7.5 — Auth documentation (7/7 plans) — 2026-05-13

Full archive: [`milestones/v1.43.0-ROADMAP.md`](milestones/v1.43.0-ROADMAP.md) · Audit: [`milestones/v1.43.0-MILESTONE-AUDIT.md`](milestones/v1.43.0-MILESTONE-AUDIT.md)

</details>


## Backlog

(no items — 999.1 promoted to Phase 07.2.1 on 2026-05-12)

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
| 7. Trigger primitive + server shell | v1.43.0 | 6/6 | Complete | 2026-05-08 |
| 7.1. HTTP webhook receiver | v1.43.0 | 9/9 | Complete | 2026-05-11 |
| 7.2. Cron triggers | v1.43.0 | 4/4 | Complete | 2026-05-11 |
| 07.2.1. Structured logging step | v1.43.0 | 5/5 | Complete | 2026-05-12 |
| 7.3. Dashboard | v1.43.0 | 6/6 | Complete | 2026-05-13 |
| 7.4. extbin consolidation | v1.43.0 | 5/5 | Complete | 2026-05-13 |
| 7.5. Auth docs | v1.43.0 | 7/7 | Complete | 2026-05-13 |

---
*Roadmap created: 2026-04-26*
*Last updated: 2026-05-13 — v1.43.0 Durability + Triggers SHIPPED (7 phases, 42 plans). Next: `/gsd:new-milestone` to scope v1.44.0.*
