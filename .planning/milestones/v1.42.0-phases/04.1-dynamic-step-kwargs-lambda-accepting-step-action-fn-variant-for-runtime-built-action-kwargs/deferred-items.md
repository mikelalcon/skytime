# Deferred Items — Phase 04.1

Items found during execution that are out of the current task's scope but should be addressed elsewhere.

## Items

### DI-04.1-01: Add DSL-11 / DSL-12 / DSL-13 to .planning/REQUIREMENTS.md

**Found during:** Plan 04.1-01 execution (Wave 0 type-spine + fixtures).

**Context:** All 04.1-* plan frontmatters reference `requirements: [DSL-11, DSL-12, DSL-13]` (and the CONTEXT.md decision block hints at these IDs as "final IDs assigned during planning"). REQUIREMENTS.md currently enumerates DSL-01..10 only; DSL-11/12/13 are not yet on disk.

**Why deferred:** Adding new requirement IDs to REQUIREMENTS.md is a planning-tier change (impacts the v1 requirement total, the traceability table, and the phase-summary line at the bottom of REQUIREMENTS.md). It also affects how `gsd-tools requirements mark-complete` works — currently fails with `"not_found"` for DSL-11/12/13 because they don't exist. Out of scope for Plan 04.1-01 (whose scope is dag types + fixtures).

**Suggested resolution:** A planning-tier edit (likely during a `/gsd:transition` or as part of Plan 04.1-07 close-out) that:
1. Adds DSL-11/12/13 entries under the "DSL — Starlark Authoring Surface" section of REQUIREMENTS.md with text describing dynamic kwargs / interpolation / live-block.
2. Adds DSL-11/12/13 rows to the Traceability table mapping to "Phase 04.1" with status driven from per-plan SUMMARY frontmatter.
3. Updates the v1 requirement total (55 → 58) and the phase summary line.
4. Re-runs `gsd-tools requirements mark-complete DSL-11 DSL-12 DSL-13` after each phase 04.1 plan whose frontmatter claims to have completed them.

**No-impact for downstream Plan 04.1 waves:** Plans 04.1-02 through 04.1-07 ship code regardless. Only the traceability metadata is affected.
