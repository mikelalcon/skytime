---
phase: 07-trigger-primitive-server-shell
plan: 06
subsystem: cli-rename-and-firewalls
tags: [cli-rename, dev-temporal, grep-gate, credential-redaction-firewall, ast-walker, hard-rename, d-07-21, d-07-22, d-07-10]

# Dependency graph
requires:
  - phase: 07-05
    provides: "skytime server subcommand registered on root; pkg/cli still allow-listed for cobra; pkg/cli/dev_server.go is the rename source file"
provides:
  - "skytime dev-temporal subcommand (renamed from skytime dev-server per D-07-21; hard rename, no deprecation alias)"
  - "pkg/cli/dev_temporal.go + pkg/cli/dev_temporal_test.go (renamed via git mv with rename similarity tracked)"
  - "newDevTemporalCommand symbol throughout pkg/cli; cobra Use string dev-temporal"
  - "TestRoot_HasDevTemporalSubcommand presence test pinning the new AddCommand line"
  - "tests/firewall_credential_redaction_test.go (D-07-10) — AST walker rejecting %+v and %#v in pkg/dag, pkg/extension, pkg/extension/builtin"
  - "tests/firewall_credential_redaction_test.go::TestCredentialRedactionFirewall_AcceptsCleanCode (positive regression)"
  - "tests/dev_server_grep_test.go (D-07-22) — git ls-files gate for the literal dev-server outside allow-list"
  - "tests/dev_server_grep_test.go::TestNoDevServerLiteralRemains_AllowListIsEffective (sanity guard for the gate's self-allow-list)"
  - "Updated docs/reference/cli.md ## skytime dev-temporal section + new ## skytime server section (~80 lines, SERVER-01..03)"
  - "Allow-list semantics for the grep gate: .planning/ prefix, CHANGELOG.md filename, tests/dev_server_grep_test.go itself"
affects:
  - 07.1 (HTTP webhook receiver: --addr flag from Plan 05's server subcommand becomes the HTTP listener; the dev-temporal rename is purely cosmetic for that work)
  - "Future renames: TestNoDevServerLiteralRemains is the regression-prevention pattern for any subsequent CLI rename — copy-paste the file with a different literal"

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "git mv (preserves rename similarity score; reviewer sees the rename in git log --follow rather than as separate add+delete)"
    - "AST walker pattern in tests/firewall_credential_redaction_test.go: parser.ParseFile + ast.Inspect + selector-expression match on fmt.<verb> calls"
    - "matchViolations(fset, file) []string helper extracted so positive + negative tests reuse the same matcher (regression-resistant against over-broadening)"
    - "git ls-files iteration pattern in tests/dev_server_grep_test.go — uses git's authoritative tracked-file list instead of glob walks; allow-list is prefix + filename map"
    - "Self-allow-list semantics: the grep gate file lists itself in the allow-list because the file MUST contain the literal it's checking for; TestNoDevServerLiteralRemains_AllowListIsEffective is a sanity test that the self-exclusion is wired correctly"

key-files:
  created:
    - tests/firewall_credential_redaction_test.go
    - tests/dev_server_grep_test.go
    - .planning/phases/07-trigger-primitive-server-shell/07-06-SUMMARY.md
  modified:
    - pkg/cli/dev_temporal.go (renamed from dev_server.go via git mv; symbols + cobra Use string + doc strings updated)
    - pkg/cli/dev_temporal_test.go (renamed from dev_server_test.go via git mv; TestDevServerCmd_* → TestDevTemporalCmd_*; in-test "dev_server.go" file reference updated)
    - pkg/cli/root.go (AddCommand line + package doc comment + Phase-7 trailing comment)
    - pkg/cli/root_test.go (bare-help string assertion + TestRootCommand_HasValidateSubcommand docstring + new TestRoot_HasDevTemporalSubcommand)
    - pkg/worker/options.go (UseBuildIDVersioning docstring; flagged by the grep gate after Tasks 1-6)
    - cmd/skytime/main.go (top doc comment + ErrAlreadyRendered chain comment)
    - examples/http-github-webhook/cmd/extbin/main.go (top doc-comment subcommand list)
    - examples/http-github-webhook/cmd/extbin/main_test.go (TestExtbin_BuildsAndShowsHelp subcommand-list array)
    - README.md
    - CLAUDE.md
    - docs/getting-started.md
    - docs/architecture.md
    - docs/cli-binary.md
    - docs/reference/cli.md (largest doc surface — ## skytime dev-server section renamed + bodies updated; new ## skytime server section added)
    - docs/for-flow-authors/README.md
    - examples/README.md
    - examples/http-github-webhook/README.md
    - .github/workflows/scripts/walkthrough_smoke.sh
    - tests/e2e_skytime_run_test.go
    - tests/skytime_test_e2e_test.go

key-decisions:
  - "Hard rename (D-07-21) — no deprecation alias. Pre-1.0, the cost of dragging dev-server forward as a hidden alias is higher than the cost of a single CHANGELOG migration note. Every literal `dev-server` in tracked files becomes `dev-temporal` (Skytime's renamed subcommand) or `temporal dev server` (the underlying Temporal subprocess; e.g., walkthrough_smoke.sh which shells `temporal server start-dev` directly)."
  - "git mv (NOT add+delete): preserves rename similarity score for future blame/log --follow. Edits to the renamed files happen AFTER the mv so git diff reads as 100% rename + N inline edits."
  - "Allow-list for D-07-22 grep gate: .planning/ (historical phase docs), CHANGELOG.md (release notes), tests/dev_server_grep_test.go itself (the gate must contain the literal as the thing being checked). Documented inline in the test source so future maintainers don't second-guess the exclusion."
  - "Allow-list semantics test (TestNoDevServerLiteralRemains_AllowListIsEffective): added as a sanity guard against a future refactor that 'cleans up' the self-reference and silently breaks the gate. The test asserts both that the file contains the literal AND that the allow-list mentions the file path."
  - "AST walker for D-07-10 covers the format-string position correctly across fmt's variants: Sprintf/Errorf/Printf use index 0; Fprintf uses index 1 (first arg is io.Writer); Sprint/Sprintln/Print/Println/Fprint/Fprintln are SKIPPED (no format-string arg). Centralized in `formatPos` map so adding a new fmt entry-point is a one-line change."
  - "Reworded the parenthetical 'renamed from dev-server in Phase 7' → 'renamed in Phase 7 per D-07-21' / 'renamed (see CHANGELOG)'. The first phrasing was the plan's recommendation but contained the legacy literal — the grep gate flagged it. Reworded versions preserve the migration context without the literal."
  - "walkthrough_smoke.sh wording 'temporal dev server' (no hyphen) — accurate phrase for the underlying Temporal subprocess started via `temporal server start-dev`. The hyphenated `dev-temporal` is Skytime's subcommand; the script doesn't invoke that. Disambiguating the two is the point of the rename."
  - "docgen drift was a no-op — Plan 03 already regenerated docs/reference/builtins.md for the trigger() builtin. go generate ./... produced zero diff in this plan; the drift test still passes (TestDocgenDrift green). No commit was made for Task 7 because there was nothing to commit."
  - "Three production files (pkg/cli/root.go, pkg/cli/root_test.go, pkg/worker/options.go) were caught by the grep gate AFTER Tasks 1-6. Treated as Rule 3 (auto-fix blocking issue) — the gate's whole purpose is to catch the cases the human (and AI) miss in the prose pass."
  - "The added TestRoot_HasDevTemporalSubcommand pins the AddCommand line in root.go independently from the implicit coverage TestRootCommand_BareInvocationPrintsHelp gives. Two presence tests is cheap insurance against a regression that drops the AddCommand call without breaking the help-text smoke."

patterns-established:
  - "Hard-rename + grep-gate pattern: when renaming a publicly-visible CLI surface pre-1.0, the lowest-risk rename is git mv → edit symbols → edit prose → add a grep gate against the legacy literal. The grep gate is cheaper than reviewer vigilance and faster than the next phase's bug report."
  - "AST firewall pattern with positive regression test: any AST-walking firewall (D-07-10 here, D4-13 in Phase 4) ships TWO tests — the firewall itself + a positive case that confirms the matcher does not over-fire. Without the second test, a 'fix' that broadens the matcher to all fmt calls would silently pass the firewall and then accidentally fail every future change."
  - "Self-allow-list sanity test pattern: any test file that contains the literal it's checking for SHOULD ship with a sanity test that asserts both (a) the file contains the literal and (b) the allow-list excludes the file. Catches refactors that 'clean up' the self-reference and silently break the gate."

requirements-completed: [CLI-13]

# Metrics
duration: 14min
completed: 2026-05-08
---

# Phase 07 Plan 06: dev-server → dev-temporal Rename + Firewalls Summary

**Completed CLI-13's hard rename of `skytime dev-server` to `skytime dev-temporal` (no deprecation alias per D-07-21); shipped two new firewall tests (D-07-10 credential-redaction AST walker over pkg/dag + pkg/extension + pkg/extension/builtin; D-07-22 grep gate for the legacy `dev-server` literal across every tracked file with a `.planning/` + CHANGELOG.md + self-allow-list); added `## skytime server` section (~80 lines, SERVER-01..03) to docs/reference/cli.md; final Phase 7 test suite green under `-race`.**

## Performance

- **Duration:** ~14 min
- **Started:** 2026-05-08T20:38:39Z
- **Completed:** 2026-05-08T20:52:48Z
- **Tasks:** 9 (Tasks 8 + 9 marked TDD; production code already passed both firewalls on first run, plus the grep gate caught 3 stragglers from earlier tasks treated as Rule 3 auto-fixes)
- **Files:** 3 created, 19 modified
- **Commits:** 8 (Task 7 was a no-op; the auto-generated builtins.md had no drift)

## Accomplishments

### Hard rename: `skytime dev-server` → `skytime dev-temporal`

The renaming surface in numbers:

| Surface | Before | After |
| --- | --- | --- |
| Source file | `pkg/cli/dev_server.go` | `pkg/cli/dev_temporal.go` (git mv) |
| Test file | `pkg/cli/dev_server_test.go` | `pkg/cli/dev_temporal_test.go` (git mv) |
| Cobra command symbol | `newDevServerCommand` | `newDevTemporalCommand` |
| Cobra `Use:` literal | `dev-server` | `dev-temporal` |
| Test function names | `TestDevServerCmd_*` (4 tests) | `TestDevTemporalCmd_*` |
| Presence test (root_test.go) | (none) | `TestRoot_HasDevTemporalSubcommand` |
| Help-text smoke (root_test.go) | asserts `dev-server` | asserts `dev-temporal` |

In-source content updates beyond the symbol/string rename:

- `pkg/cli/dev_temporal_test.go::TestDevTemporalCmd_SignalForwardSourceSmoke` — the source-grep regression test reads `os.ReadFile("dev_temporal.go")` after the rename (was `dev_server.go`); needed a literal-string update or the test would fail.
- `pkg/cli/root.go` — AddCommand line + package-level doc comment listing the inherited subcommands.
- `cmd/skytime/main.go` — top doc comment + ErrAlreadyRendered chain comment listing `(validate, run, dev-temporal, server)`.
- `examples/http-github-webhook/cmd/extbin/main.go` + `main_test.go` — extbin's inherited-subcommand list.
- 13 doc/CI files: README.md, CLAUDE.md, docs/getting-started.md, docs/architecture.md, docs/cli-binary.md, docs/reference/cli.md (largest), docs/for-flow-authors/README.md, examples/README.md, examples/http-github-webhook/README.md, .github/workflows/scripts/walkthrough_smoke.sh, tests/e2e_skytime_run_test.go, tests/skytime_test_e2e_test.go, plus pkg/worker/options.go (caught by the grep gate post-Tasks 1-6).

### `## skytime server` reference section (SERVER-01..03)

Added to `docs/reference/cli.md` between `## skytime test` and `## skytime dev-temporal`. ~80 lines covering:

- **Synopsis** — full flag inventory with types and defaults.
- **Motivation** — `skytime server` as the production sibling of `skytime run`; matches Kubernetes' `terminationGracePeriodSeconds` convention so the binary plugs into a Deployment without custom shutdown plumbing.
- **Flags table** — `--rootdir` (required), `--task-queue` (default skytime), `--addr` (`:8080`, Phase 7.1+ unused), `--credfile` (rejected without credential handler — D-07-19), `--drain-timeout` (range-validated `[1s, 1h]`, default 30s), `--json-log`.
- **Drain semantics (D-07-17, D-07-20)** — first signal logs draining + calls Stop; second signal during drain calls os.Exit(1); drain-timeout expiry exits 1. Buffered signal channel (size 2) so the second signal can land without dropping. `signal.Notify` (NOT `NotifyContext`) per Pitfall 5.
- **Startup banner (SERVER-03)** — three slog records emitted before `Worker.Start()`: `starting server` (rootdir/task-queue/addr), `registered flows` (count + sorted []string), `registered triggers` (count + []map[string]string sorted by Plan 04's `TriggerRegistry.Freeze`).
- **Exit codes** — 0 = clean drain; 1 = connect failure / worker init failure / drain-timeout / forced exit / range validation failure / `--credfile` without handler.
- **Examples** — local dev + Temporal Cloud invocations.

The `## skytime dev-temporal` section that follows had its prior `## skytime dev-server` heading and in-section body updated. Source path references migrated `pkg/cli/dev_server.go` → `pkg/cli/dev_temporal.go`. Added a callout note documenting the rename + linking to D-07-21 + a CHANGELOG hint (without literally writing the legacy name — the grep gate would catch that, and it does).

### `tests/firewall_credential_redaction_test.go` (D-07-10)

Two tests:

- **`TestCredentialRedactionFirewall`** — walks Go source files in `pkg/dag`, `pkg/extension`, and `pkg/extension/builtin`; for each non-test `.go` file, parses via `go/ast` and ast.Inspect-walks every `*ast.CallExpr`. Match rule: `*ast.SelectorExpr` with `ident.Name == "fmt"` + selector name in `formatPos` map → take the format-string position from the map (Sprintf/Errorf/Printf = 0; Fprintf = 1; Print/Println variants skipped because they take no format string) → require the arg to be a `*ast.BasicLit` of kind STRING → reject if `lit.Value` contains `%+v` or `%#v`. Failure message names every violation as `<file>:<line>:<col> fmt.<verb> contains forbidden verb in literal "<value>"`.
- **`TestCredentialRedactionFirewall_AcceptsCleanCode`** — synthetic positive case that runs the same `matchViolations` matcher against an inline source containing `fmt.Sprintf("%v %s %d %q", 1, "two", 3, "four")` and asserts zero violations. Protects against a future "broader matcher" refactor that accidentally rejects legitimate `%v`/`%s`.

The matcher is centralized in `matchViolations(fset, file) []string` so the positive + negative tests share one source of truth. Test files (`_test.go`) are exempt — tests legitimately use `%+v` for diff output where the subject is not a Secret.

Phase 7 ships zero concrete `TriggerSource`, so today the only file with a Trigger type is `pkg/dag/trigger.go`. Phase 7.1 will extend the firewall scope when `github.WebhookSource` lands — the directory list is the only thing that needs editing; the matcher is type-stable.

Result: scanned 3 target dirs, **zero** violations in production code. The firewall is green.

### `tests/dev_server_grep_test.go` (D-07-22)

Two tests:

- **`TestNoDevServerLiteralRemains`** — runs `git ls-files` to enumerate every tracked file (subprocess `exec.Command("git", "ls-files")` with the module root as `Dir`), filters via the allow-list (`.planning/` prefix; `CHANGELOG.md` filename; `tests/dev_server_grep_test.go` self-exclusion), reads each remaining file as bytes, and rejects any file containing the literal `dev-server`. Failure message names every violation file with a clear remediation prompt pointing at the two correct replacements (`dev-temporal` for Skytime's subcommand vs `temporal dev server` for the underlying subprocess).
- **`TestNoDevServerLiteralRemains_AllowListIsEffective`** — sanity test for the gate's self-allow-list. Asserts the file contains the literal `dev-server` (it must — that's the thing being grepped for) AND the allow-list mentions the file path. Catches a future refactor that accidentally drops the self-allow-list and silently breaks the gate.

Result: scanned **369 tracked files**; zero `dev-server` literals outside the allow-list. The gate is green.

### Auto-fixes from running the grep gate

After Tasks 1-6 completed and the grep gate was first run (Task 9 RED), the gate flagged **three stragglers** the prose pass missed:

| File | Line | Original | Fix |
| --- | --- | --- | --- |
| `pkg/cli/root.go` | 62 | `// renamed from dev-server in Phase 7 per D-07-21` | `// renamed in Phase 7 per D-07-21 (hard rename)` |
| `pkg/cli/root_test.go` | 212 (docstring) | `accidentally re-introduces the dev-server name` | `accidentally re-introduces the legacy name` |
| `pkg/worker/options.go` | 89 (docstring) | ``` `skytime dev-server`) work out of the box ``` | ``` `skytime dev-temporal`) work out of the box ``` |

Treated as Rule 3 auto-fix (blocking issue: the gate I just authored fails without these fixes). The gate's whole point is to catch this class of overlooked-prose bug; finding three on first run validates the design.

## Test Coverage for CLI-13 + D-07-10 + D-07-22

| Test | Behavior pinned | Requirement |
| --- | --- | --- |
| `TestDevTemporalCmd_MissingBinary` | install instructions + non-nil error on missing temporal binary | CLI-13 |
| `TestDevTemporalCmd_Spawn` | subprocess launches against real temporal CLI when present (skipped on absence + `-short`) | CLI-13 |
| `TestDevTemporalCmd_SignalForward` | SIGINT propagates to the running subprocess via the W-8 testRunningCmd seam | CLI-13 |
| `TestDevTemporalCmd_SignalForwardSourceSmoke` | source-grep insurance against accidental deletion of the signal.Notify wiring (now reads `dev_temporal.go`) | CLI-13 |
| `TestRoot_HasDevTemporalSubcommand` | AddCommand line in root.go pinned independently from the help-text smoke | CLI-13 |
| `TestRootCommand_BareInvocationPrintsHelp` | bare-help shows `dev-temporal` (was `dev-server`) | CLI-13 |
| `TestExtbin_BuildsAndShowsHelp` | extbin's `--help` lists `[validate, run, dev-temporal, server, test]` | CLI-13 |
| `TestCredentialRedactionFirewall` | AST-walks pkg/dag + pkg/extension + pkg/extension/builtin; rejects %+v / %#v in production code | D-07-10 |
| `TestCredentialRedactionFirewall_AcceptsCleanCode` | positive regression: matcher does NOT reject `%v`/`%s`/`%d`/`%q` | D-07-10 |
| `TestNoDevServerLiteralRemains` | git ls-files gate; allow-list = `.planning/` prefix + CHANGELOG.md + self-exclusion | D-07-22 |
| `TestNoDevServerLiteralRemains_AllowListIsEffective` | sanity guard for the gate's self-allow-list | D-07-22 |
| `TestDocgenDrift` | docs/reference/builtins.md matches a fresh `go generate ./pkg/parser/` (already covered Plan 03's trigger builtin) | Plan 06 ack criterion |
| `TestNoCobraImportsOutsideAllowList` | pkg/cli still the only library-side cobra/charm-log/lipgloss importer (firewall unaffected by the rename) | D4-13 (Phase 4 backstop) |

## Manual Verification

```bash
# Manual git grep — must produce zero matches
git grep -F 'dev-server' -- ':(exclude).planning' ':(exclude)CHANGELOG.md' ':(exclude)tests/dev_server_grep_test.go'
# (empty output — PASS)

# Help text — must show dev-temporal AND server, NOT dev-server
go run ./cmd/skytime --help | grep -E '(dev-temporal|server)'
#   dev-temporal Spawn a local Temporal dev server (`temporal server start-dev`)
#   server       Run a long-lived Skytime worker (drain-on-SIGTERM)

# Phase 7 success criterion 5 sub-conditions (regression-prevention bundle)
go test ./tests/ -run 'TestNoDevServerLiteralRemains|TestCredentialRedactionFirewall|TestDocgenDrift|TestNoCobraImportsOutsideAllowList' -count=1 -race
# ok   github.com/mikelalcon/skytime/tests   1.808s

# Full Phase 7 test suite
go build ./... && go vet ./... && go test ./... -count=1 -race
# all packages green
```

## Task Commits

Each task committed atomically (Task 7 was a no-op):

1. **Task 1 (rename pkg/cli/dev_server → dev_temporal + symbols + cobra Use + tests)** — `3e5d8b2` (refactor)
2. **Task 2 (cmd/skytime/main.go + extbin doc comments + main_test.go subcommand list)** — `ad86e88` (refactor)
3. **Task 3 (README.md + CLAUDE.md + 3 root-level docs/ files)** — `2e8294d` (docs)
4. **Task 4 (docs/reference/cli.md heading + body + new ## skytime server section + docs/for-flow-authors/README.md)** — `e056de7` (docs)
5. **Task 5 (examples/README.md + examples/http-github-webhook/README.md)** — `daa6a68` (docs)
6. **Task 6 (.github/workflows/scripts/walkthrough_smoke.sh + 2 e2e test comments)** — `8878b01` (chore)
7. **Task 7 (go generate ./... — no-op, builtins.md already current from Plan 03)** — no commit (zero diff)
8. **Task 8 (tests/firewall_credential_redaction_test.go — D-07-10 AST walker)** — `8402a8f` (test)
9. **Task 9 (tests/dev_server_grep_test.go — D-07-22 grep gate; auto-fixed 3 stragglers in pkg/cli/root.go + root_test.go + pkg/worker/options.go)** — `f7e2df5` (test)

Plan metadata commit: TBD (after STATE.md / ROADMAP.md updates).

## Decisions Made

- **Hard rename per D-07-21 — no alias.** Pre-1.0 the cost of a single CHANGELOG line is lower than the cost of an alias permanently bloating help text and `--help` output. The grep gate makes the migration verifiable.
- **`git mv` (NOT add+delete).** Preserves rename similarity score for future `git log --follow` / blame archaeology. Edits to the renamed files happen AFTER the mv so the reviewer sees: 100% rename + N inline edits in `git diff`.
- **Allow-list for the grep gate documented inline in the test source.** A future maintainer scanning the source sees exactly why each entry exists and can update it without reverse-engineering intent. The `TestNoDevServerLiteralRemains_AllowListIsEffective` sanity test guards against accidental "cleanup" that breaks the self-allow-list.
- **`temporal dev server` (no hyphen) vs `dev-temporal` (Skytime subcommand).** Two distinct entities; the rename is the entire reason to disambiguate them. Anywhere a literal pointed at the *Temporal subprocess* (e.g., `walkthrough_smoke.sh` which shells `temporal server start-dev`), the prose became `temporal dev server`. Anywhere a literal pointed at *Skytime's subcommand* (e.g., docs walkthroughs, README), the prose became `dev-temporal`.
- **Reworded the parenthetical migration hint.** Plan recommended `(renamed from dev-server in Phase 7)` for example READMEs and the cli.md callout — the grep gate flags that. Reworded to `(renamed in Phase 7 per D-07-21)` and `carried the legacy name (see CHANGELOG)`. The migration context survives without the literal.
- **Task 7 no-op.** `go generate ./...` produced zero diff because Plan 03 already regenerated `docs/reference/builtins.md` for the trigger() builtin. The drift test passes; no commit was made because there was nothing to commit. Documented here so the missing commit is explicable.
- **Three stragglers caught by the gate (pkg/cli/root.go, pkg/cli/root_test.go, pkg/worker/options.go).** Treated as Rule 3 auto-fix — the gate's whole purpose is to catch overlooked occurrences. Bundled into Task 9's commit since the gate's existence is what surfaced them and the fix is one line each.

## Deviations from Plan

**1. Task 7 produced zero diff.** Plan said "regenerate via `go generate ./...`; commit the regenerated version (which now includes Plan 03's trigger builtin documentation)." Plan 03 already regenerated `docs/reference/builtins.md` when it ran (`commit c128b8c test(07-03): trigger_test.go covers TRIG-01/04 + D-07-12/13`). Re-running here was a no-op; no commit was made because there was nothing to commit. The drift test (`TestDocgenDrift`) confirms the on-disk file matches a fresh regeneration — the verification is positive even though the intermediate commit is absent. **Documented in this SUMMARY's Task Commits section + Decisions Made** so the missing commit is explicable to anyone walking the git log.

**2. Plan recommended `(renamed from dev-server in Phase 7)` parenthetical.** Tasks 2 + 4 landed that wording verbatim per the plan. The grep gate (Task 9) then flagged the literal. Reworded both to `(renamed in Phase 7 per D-07-21)` / `carried the legacy name (see CHANGELOG)`. The migration context is preserved; the literal is not. Treated as Rule 1 (auto-fix bug) since the wording violated the plan's own success criterion #1 (zero `dev-server` literals).

**3. Plan referenced `docs/reference/cli.md` line numbers from a discovery-time grep.** Some line numbers shifted after the new `## skytime server` section was inserted (~80 lines added). Final state was verified by `grep -F 'dev-server'` → zero hits, plus presence check on `## skytime dev-temporal` + `## skytime server` headings. Notational, not semantic.

**4. The dev_temporal_test.go in-test file reference.** Plan called out the literal "dev_server.go" filename in `TestDevServerCmd_SignalForward`'s body — actually that literal is in `TestDevServerCmd_SignalForwardSourceSmoke`'s body (`os.ReadFile("dev_server.go")`). Fixed in the right place; the plan's prose pointed at the wrong test function but the file-content is identical, so the fix landed correctly.

## Issues Encountered

None outside the three stragglers documented above. Build, vet, and full-repo `go test -race -count=1` green at every task boundary.

## User Setup Required

None — pure rename + AST firewall + grep gate. No external services touched, no user-side migration steps beyond eventual CHANGELOG note for v1.43 release.

## Self-Check: PASSED

**Files:**

- File `pkg/cli/dev_server.go`: NOT FOUND (correctly removed via git mv)
- File `pkg/cli/dev_temporal.go`: FOUND
- File `pkg/cli/dev_server_test.go`: NOT FOUND (correctly removed via git mv)
- File `pkg/cli/dev_temporal_test.go`: FOUND
- File `tests/firewall_credential_redaction_test.go`: FOUND
- File `tests/dev_server_grep_test.go`: FOUND

**Commits:**

- `3e5d8b2` (Task 1): FOUND
- `ad86e88` (Task 2): FOUND
- `2e8294d` (Task 3): FOUND
- `e056de7` (Task 4): FOUND
- `daa6a68` (Task 5): FOUND
- `8878b01` (Task 6): FOUND
- (Task 7): NO COMMIT — `go generate ./...` was a no-op (documented above)
- `8402a8f` (Task 8): FOUND
- `f7e2df5` (Task 9): FOUND

**Gates:**

- `go build ./...`: PASS
- `go vet ./...`: PASS
- `go test ./... -count=1 -race`: PASS (full repo green)
- `go test ./tests/ -run 'TestNoDevServerLiteralRemains|TestCredentialRedactionFirewall|TestDocgenDrift|TestNoCobraImportsOutsideAllowList' -count=1 -race`: PASS
- `go test ./pkg/cli/ -run TestDevTemporalCmd -count=1 -race`: PASS (4 tests; 1 skipped on `-short`, 1 skipped without temporal binary)
- `go test ./pkg/cli/ -run TestRoot_HasDevTemporalSubcommand -count=1`: PASS
- `git grep -F 'dev-server' -- ':(exclude).planning' ':(exclude)CHANGELOG.md' ':(exclude)tests/dev_server_grep_test.go'`: zero matches (PASS)
- `go run ./cmd/skytime --help | grep -E '(dev-temporal|server)'`: shows both, no `dev-server` (PASS)

## Next Phase Readiness

**Phase 7 closeout — every Phase 7 success criterion is now green:**

- ✓ DAG carries Triggers as a top-level slice (Plan 01)
- ✓ Extension SDK exposes the TriggerSource type with idempotency (Plan 02)
- ✓ Parser builtin trigger(...) lands in the locked-21-key parse-time globals (Plan 03)
- ✓ Worker boot wires a TriggerRegistry alongside FlowRegistry; both freeze (Plan 04)
- ✓ skytime server long-running subcommand with drain-on-SIGTERM (Plan 05)
- ✓ skytime dev-server hard-renamed to dev-temporal; D-07-10 + D-07-22 firewalls in place (Plan 06)

**Phase 7.1 (HTTP webhook receiver) unblocked** at every level:

- The `--addr` flag from Plan 05's `server` subcommand is already wired and warns if set (Phase 7.1 mounts the listener).
- The `D-07-10` firewall's directory list is the single change-point when `github.WebhookSource` lands — add `pkg/extension/builtin/github` (or wherever it lives) to `targetDirs`.
- The `D-07-22` grep gate is forever — any future CLI rename can copy the file with a different literal.

**No blockers** for the Phase 7 transition or for Phase 7.1.

---
*Phase: 07-trigger-primitive-server-shell*
*Completed: 2026-05-08*
