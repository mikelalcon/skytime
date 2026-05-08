# Skytime Retrospective

A living record of milestone retrospectives — what worked, what didn't, what we learned. Append on each milestone closure.

---

## Milestone: v1.42.0 — Foundation

**Shipped:** 2026-05-08
**Phases:** 9 | **Plans:** 58 | **Tasks:** 146
**Timeline:** 2026-04-26 → 2026-05-08 (12 days, 383 commits, ~46k LOC)

### What Was Built

The complete v1 foundation: Starlark DSL with six primitives + expression-mode `if_cond`, generic Temporal activity with sealed credential plumbing, generic interpreter with replay-deterministic lambda re-parse, three named worker client constructors, static validation tier sharing the parser with the runtime, full CLI tree, Tier-3 `.star` test harness with always-on replay determinism, source-driven docs reference generator, complete documentation set, and a dogfooding example project (HTTP + GitHub + Webhook extensions, five `.star` flows, Tier-3 test, README walkthrough, CI gating every push).

### What Worked

**Wave-based execution with parallel subagents.** Phases 6 and 04.3 ran 4 plans in parallel within Wave 1; landed cleanly when plans were file-disjoint, hit go.mod contention only when Wave 1 plans modified deps simultaneously. Resolved by sequencing the dep-introducing plan first, then parallelizing the rest.

**`/gsd:audit-milestone` + 3-source cross-reference.** Caught stale `[ ]` checkboxes on INTRP-03/04/05 (Phase 3 verified `passed` with test evidence months earlier; trace table never updated). 64/64 requirements verified across VERIFICATION.md + SUMMARY.md frontmatter + REQUIREMENTS.md. The cross-phase integration check found 17 wiring points and 0 hard breaks.

**Goal-backward verification.** Every phase ended with a goal-backward `gsd-verifier` pass that checked must-haves against actual codebase, not just task completion. Found and fixed two latent gaps in Phase 5's harness from Phase 6's Tier-3 test: sibling-flow registration + multiset replay-determinism fallback. Both shipped back into `pkg/interpreter` and `pkg/testing` as additive APIs.

**Architectural firewalls enforced by AST-walking tests.** Five firewalls (Temporal SDK reachability, cobra reachability, extension-pkg-no-temporal, docgen-stdlib-only, test-mode parser globals) all hold across the entire v1 codebase. The firewall meta-tests caught violations early enough to fix architecturally rather than papering over.

**Inline UAT fixes for documentation-only gaps.** Phase 04.3 had 5 pending HUMAN-UAT items. Walking them surfaced two real issues (overconfident tone, stale Phases section, inappropriate "consultant" terminology in user-facing docs). Fixed inline as 2 commits rather than spinning up the formal `gsd-planner` → `gsd-plan-checker` → `--gaps-only` cycle. Right-sized response to the work.

### What Was Inefficient

**Stream-idle timeouts on parallel agents.** Two waves (Phase 6 Wave 1, Phase 6 Wave 3) hit Claude Code stream-idle timeouts after 16+ minutes. Required spot-checking via filesystem state and resuming with fresh agents. The completion-signal fallback in `execute-phase.md` worked as designed — verified work via SUMMARY.md + git log, treated unsignaled completions as successful when artifacts existed. Real but manageable.

**Auto-extracted milestone accomplishments dumped raw SUMMARY content** including deviation markers ("1. [Rule 2 — Wire-format gap]"), bare "Status:" lines, and "Created (8):" fragments. Required a manual rewrite of MILESTONES.md after the CLI auto-archived. The `gsd-tools milestone complete` should curate one-liners, not just concatenate the first line of each SUMMARY one-liner field.

**Phase 4 → 04.1 → 04.2 → 04.3 decimal phasing surfaced mid-milestone scope expansion.** Three urgent decimal phases inserted after Phase 4 because the runtime needed dynamic kwargs (04.1), expression-mode if_cond was discovered as a load-bearing feature (04.2), and documentation hadn't been planned (04.3). Each was the right call individually, but the running plan-count creep meant v1 was 64 requirements not 55 by the end. Better discipline at /gsd:new-project would have surfaced the documentation phase up-front; the DSL phases were genuine discoveries.

**Nyquist coverage backlog accumulated silently.** 1/9 phases marked `nyquist_compliant: true` (only 04.1). The other 8 phases have VALIDATION.md but unmet compliance gates. None of this was a blocker — the audit explicitly classified as `tech_debt` — but the backlog grew large enough to be its own multi-session project. Should have surfaced compliance gaps as part of phase verification, not deferred to milestone audit.

### Patterns Established

**Wave-0 type-spine plans.** Every multi-plan phase started with a wave-0 plan that locked the types every other plan would consume (Phase 1 plan 01-01, Phase 2 plan 02-01, Phase 4 plan 04-01, Phase 04.2 plan 04.2-01). This eliminated downstream redesign churn — types were stable before consumers were written.

**Goal-backward task lists.** Each plan's task list was derived from "what must be true for the phase goal to be achieved" not "what could we build here." Plans with this discipline had clean verification passes; plans without it (rare) needed gap-closure cycles.

**Locked decisions in `<phase>-CONTEXT.md`.** Decimal-coded decisions (D2-05, D4.1-22, D-CREDS-FORMAT) referenced verbatim across plans, summaries, and verification reports. Made cross-plan integration unambiguous; made it easy to audit decision drift after the fact.

**Defense-in-depth at the gates.** Mixed-idempotency batches rejected at parse time (best-effort) AND at activity time (definitive). `${ctx.expr}` typos surface as `*dag.ValidationError` at parse time AND fail loudly at workflow time. Multiple layers because errors at each layer have different recovery affordances.

**Architecture-as-test.** Five AST-walking firewall tests (`tests/firewall_*_test.go`) enforce architectural rules at compile time. Violations break CI before they can review. Cheaper than code review for invariants that don't bend.

### Key Lessons

**Document audience terminology choices early.** "Consultant" terminology persisted across 12 user-facing files until the Phase 04.3 UAT caught it. Internal planning docs (PROJECT.md, CLAUDE.md) still use "consultant" because that's the project's framing of the two-tier authoring model — but user-facing prose needs the audience's term. "Flow author" is what the directory `docs/for-flow-authors/` already established. Should have surfaced the inconsistency at the docs-phase research stage.

**Match scope of action to scope of request.** When the user reported documentation issues with concrete actionable fixes, spawning the formal `gsd-planner` → `gsd-plan-checker` cycle would have been ceremony for ceremony's sake. Inline edit + 2 commits was right-sized. The formal cycle is for code bugs where root cause is unknown, not for typos in docs.

**Tone is a real defect class.** AI-generated prose has a recognizable confidence overshoot ("absolute and architectural", "production-grade", em-dash flourishes). Caught only because the user read the README in the GitHub UI. Should be in the verifier's pass for any phase that ships user-facing prose. Future phases shipping docs will want a "tone audit" task before phase close.

**Demo-ability is a requirement, not a nice-to-have.** Phase 6 shipped successfully but couldn't actually *demonstrate* Temporal's replay-after-crash behavior because `skytime run` is a one-shot embedded transient worker. The whole project's load-bearing claim ("durable workflows on Temporal") couldn't be shown end-to-end without a long-running server process. v1.43 corrects this. The lesson: features that exist for *demonstration* (not just *function*) need to be planned with the demo path in mind.

**Stale traceability is silent debt.** INTRP-03/04/05 sat as `[ ]` and `Pending` for the entire v1.42.0 lifetime even though Phase 3 verified them `passed` with test evidence. The audit caught it. The pattern: phase verification updates SUMMARY frontmatter and VERIFICATION.md but doesn't always thread back to REQUIREMENTS.md checkboxes. The fix should be automated in `gsd-tools phase complete`.

### Cost Observations

- Model mix: predominantly Opus 4.7 (1M context) for orchestration; Sonnet 4.6 for executor subagents; some Haiku 4.5 for low-stakes tool calls
- Sessions: ~30-40 (multi-day milestone, /clear between major phase boundaries)
- Notable: Wave-3 of Phase 6 timed out two parallel executors at the 9-hour and 23-minute mark respectively. Both had committed their work but stream went silent. The completion-signal fallback in execute-phase.md saved the work via filesystem spot-checks. Don't trust the stream alone for long-running parallel agents.

---

## Cross-Milestone Trends

(Will populate as more milestones ship.)
