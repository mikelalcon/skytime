---
phase: 06-example-project-http-github-webhook
plan: 01
subsystem: infra
tags: [go-modules, toml, github-api, gitignore, scaffolding]

# Dependency graph
requires:
  - phase: 04-static-validation-tier-cli-skeleton
    provides: pkg/extension/credential.go (sealed Credential interface — credfile maps TOML schema to BearerCredential/BasicCredential/APIKeyCredential)
  - phase: 04-static-validation-tier-cli-skeleton
    provides: pkg/extension/builtin/http/ (template extension shape Wave 1's github + webhook extensions follow)
provides:
  - go.mod direct require entries for pelletier/go-toml/v2 v2.3.1 (TOML parser for credfile)
  - go.mod direct require entries for google/go-github/v78 v78.0.0 (GitHub REST client for github extension)
  - examples/http-github-webhook/.skytime-credentials.example — checked-in TOML template (4 credential entries: bearer/basic/apikey/bearer-as-URL)
  - .gitignore patterns blocking accidental .skytime-credentials commits with !*.skytime-credentials.example negation
  - examples/http-github-webhook/.gitignore example-local equivalent
affects: [06-02-credfile-package, 06-03-github-extension, 06-04-webhook-extension]

# Tech tracking
tech-stack:
  added:
    - "github.com/pelletier/go-toml/v2 v2.3.1 (TOML parser, zero transitive deps)"
    - "github.com/google/go-github/v78 v78.0.0 (GitHub REST client; pulls go-querystring transitive when imported in Wave 1)"
  patterns:
    - "Wave-0 dep scaffolding: add go.mod entries before Wave 1 imports them, allowing parallel Wave-1 package authoring without dep conflicts"
    - ".skytime-credentials gitignore with !*.example negation: defense-in-depth + checked-in template coexist via the standard git negation pattern"

key-files:
  created:
    - "examples/http-github-webhook/.skytime-credentials.example"
    - "examples/http-github-webhook/.gitignore"
  modified:
    - "go.mod"
    - "go.sum"
    - ".gitignore"

key-decisions:
  - "Skipped final `go mod tidy` because no .go file imports the new deps yet (Wave 1 lands implementation); tidy would demote/remove the entries. Acceptance criteria (grep + build/vet) all satisfied without tidy."
  - "Used `go mod edit -require` + `go mod download` to land checksums in go.sum without invoking tidy. Wave 1's first package import + tidy will move them to formal direct require entries."

patterns-established:
  - "Wave-0 scaffolding pattern: deps + config templates land before any consuming code, freeing Wave 1 packages to be developed in parallel without touching shared infrastructure"
  - "Credfile TOML schema is locked verbatim from CONTEXT.md D-CREDS-FORMAT (4 entries × 3 credential types + a bearer-as-URL for webhook.site)"

requirements-completed: [EX-01, EX-03, EX-04]

# Metrics
duration: 3min
completed: 2026-05-07
---

# Phase 6 Plan 1: Wave-0 Scaffolding (deps + credfile template + gitignore) Summary

**pelletier/go-toml/v2 v2.3.1 + google/go-github/v78 v78.0.0 added to go.mod; .skytime-credentials.example template (4 credential entries) checked in; .gitignore defense-in-depth landed at root + example level with !*.example negation.**

## Performance

- **Duration:** ~3 min
- **Started:** 2026-05-07T22:57:04Z
- **Completed:** 2026-05-07T23:00:16Z
- **Tasks:** 3
- **Files modified:** 5 (go.mod, go.sum, .gitignore, examples/http-github-webhook/.skytime-credentials.example, examples/http-github-webhook/.gitignore)

## Accomplishments
- pelletier/go-toml/v2 v2.3.1 added to go.mod + go.sum (zero transitive deps)
- google/go-github/v78 v78.0.0 added to go.mod + go.sum (pulls go-querystring transitively when first imported)
- `go build ./...` + `go vet ./...` both green after dep additions
- `.skytime-credentials.example` template checked in with verbatim TOML schema (bearer/basic/apikey/webhook_url-as-bearer)
- Root `.gitignore` updated with `*.skytime-credentials` defense-in-depth pattern + `!*.skytime-credentials.example` negation (verified via `git check-ignore` that the template stays tracked)
- `examples/http-github-webhook/.gitignore` example-local equivalent for the case where the example is ever extracted

## Task Commits

Each task was committed atomically (no-verify, serial in this wave):

1. **Task 1: Add pelletier/go-toml/v2 + google/go-github/v78 to go.mod** — `dfdd7d5` (chore)
2. **Task 2: Create .skytime-credentials.example with locked TOML schema** — `b4400b2` (docs)
3. **Task 3: Add .skytime-credentials patterns to .gitignore** — `787299e` (chore)

## Files Created/Modified

- `go.mod` — added `github.com/pelletier/go-toml/v2 v2.3.1` and `github.com/google/go-github/v78 v78.0.0` require entries (no `// indirect` marker)
- `go.sum` — added h1 + go.mod checksums for both modules
- `examples/http-github-webhook/.skytime-credentials.example` — TOML credfile template (4 credential entries, chmod 600 guidance, webhook.site reference)
- `.gitignore` — appended Skytime credfile defense-in-depth block (3 ignore patterns + 1 negation)
- `examples/http-github-webhook/.gitignore` — created example-local credfile gitignore (3 lines)

## Decisions Made

- **Skipped `go mod tidy` step** documented in the plan action. Rationale: no .go file imports the new modules yet — Wave 1 (06-02 credfile package, 06-03 github extension) is where imports land. Running tidy now would either demote them to `// indirect` or remove them entirely, defeating the Wave-0 scaffolding intent. Acceptance criteria (grep + build + vet) are all satisfied without tidy. Wave 1's first import + tidy will move the entries to the formal "direct" require block at that point.
- **Used `go mod edit -require` followed by `go mod download`** to land go.sum checksums for the new modules without going through tidy. This is the minimum-impact path that satisfies the plan's verify automation.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 — Blocking] Skipped `go mod tidy` to preserve direct-require entries**
- **Found during:** Task 1 (Add go.mod deps)
- **Issue:** The plan action specifies `go get ... && go mod tidy`. Running `go mod tidy` after `go get` for two modules with zero importing .go files demotes them to `// indirect` (which the acceptance criteria explicitly forbid) or removes them entirely. The first attempt confirmed: tidy reverted go.mod to a state where pelletier was indirect and go-github was missing.
- **Fix:** Used `go mod edit -require=...@v` (twice) then `go mod download <mod>` (for both) to land entries with checksums but without invoking tidy. Acceptance grep, `go build ./...`, and `go vet ./...` all pass.
- **Files modified:** go.mod, go.sum
- **Verification:** `grep -E '^\tgithub.com/(pelletier/go-toml/v2 v2\.3\.1|google/go-github/v78 v78\.0\.0)$' go.mod` finds both entries; build + vet green.
- **Committed in:** dfdd7d5 (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking — Rule 3)
**Impact on plan:** No scope creep; the deviation only affects the *order of operations* within Task 1 (skip tidy). Acceptance criteria all met. Wave 1's first import will reconcile the require blocks via tidy at that point.

## Issues Encountered

None beyond the documented `go mod tidy` deviation.

## User Setup Required

None - no external service configuration required for this scaffolding plan.

## Next Phase Readiness

**Wave 1 (06-02, 06-03, 06-04) can now start in parallel:**
- `06-02-credfile-package` can `import "github.com/pelletier/go-toml/v2"` immediately
- `06-03-github-extension` can `import "github.com/google/go-github/v78/github"` immediately
- `06-04-webhook-extension` has no new deps to wait on (uses stdlib `net/http`)

The first Wave-1 package to import + run `go mod tidy` will move both new entries into the formal direct-require block.

## Self-Check

- [x] FOUND: go.mod (pelletier/go-toml/v2 v2.3.1)
- [x] FOUND: go.mod (google/go-github/v78 v78.0.0)
- [x] FOUND: go.sum (pelletier h1 + go.mod checksums)
- [x] FOUND: go.sum (go-github h1 + go.mod checksums)
- [x] FOUND: examples/http-github-webhook/.skytime-credentials.example
- [x] FOUND: examples/http-github-webhook/.gitignore
- [x] FOUND: .gitignore (4 new lines: 3 ignore patterns + 1 negation)
- [x] FOUND: commit dfdd7d5 (Task 1 — deps)
- [x] FOUND: commit b4400b2 (Task 2 — credfile template)
- [x] FOUND: commit 787299e (Task 3 — gitignore)
- [x] `go build ./...` exits 0
- [x] `go vet ./...` exits 0
- [x] `git check-ignore` confirms `.example` template is NOT ignored

## Self-Check: PASSED

---
*Phase: 06-example-project-http-github-webhook*
*Completed: 2026-05-07*
