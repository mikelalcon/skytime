---
name: skytime-docs
description: Load before editing ANY .md file, docgen marker, walkthrough, snippet, or .planning/ document in the Skytime repo — covers the audience-split docs tree, the generated builtins.md (never hand-edit), the README↔getting-started duplication rule, heading/grep/drift gates that fail CI on innocent doc edits, and GSD planning-doc conventions.
---

# Skytime Documentation Conventions

Docs in this repo are load-bearing: multiple CI tests grep, diff, and byte-compare them.
An "innocent" wording tweak can fail four different gates. Read the relevant section below
BEFORE editing, and run the listed verification command AFTER.

## The docs tree (by audience)

| Path | Audience / role |
|---|---|
| `docs/getting-started.md` | Canonical tutorial (clone → run one flow). README embeds it — see duplication rule. |
| `docs/architecture.md` | Required reading for BOTH audiences; parse/execute split narrative. |
| `docs/for-flow-authors/` | `.star` authors: `README.md` landing, `testing.md`, `testing-tutorial.md`, `extensions/http.md`. |
| `docs/for-extension-developers/` | Go devs: `README.md` landing, `temporal-auth.md` (+ `snippets/` standalone Go module). |
| `docs/reference/builtins.md` | **GENERATED** by `cmd/skytime-docgen`. Never hand-edit. |
| `docs/reference/cli.md` | Hand-written CLI reference. |
| `docs/cli-binary.md` | Hand-written custom-binary doc. |
| `docs/walkthroughs/` | `github-webhook.md`, `cron-schedules.md`, `dashboard.md` + env-gated smoke scripts (`*-smoke.sh`). |
| `examples/README.md` | Index for `examples/skeleton/` fixtures — fixed section template per fixture. |

Cross-links are sibling-relative: `for-flow-authors/` and `for-extension-developers/` pages
link `../architecture.md`, `../reference/*`, `../../examples/`. Moving any doc file breaks
multiple landings — grep for the old path before moving anything.

Terminology: user-facing docs say **"flow author"**, never "consultant" (v1.42.0 retro
lesson). Planning docs (`.planning/`, `CLAUDE.md`) deliberately retain "consultant" —
do not "fix" them.

## builtins.md is generated — the docgen pipeline

`docs/reference/builtins.md` carries a "Do not edit by hand" banner, but nothing physically
blocks an edit — `tests/docgen_drift_test.go` (`TestDocgenDrift`) catches it by re-running
docgen and byte-comparing. To change builtin documentation, edit the **Go source markers**,
then regenerate:

```sh
go generate ./pkg/parser/        # runs cmd/skytime-docgen --pkg . --out ../../docs/reference/builtins.md
```

(Directive lives in `pkg/parser/generate.go`.) Then verify:

```sh
git diff docs/reference/builtins.md            # your change appears, nothing else
go test ./tests/ -run TestDocgenDrift -count=1 # passes
```

### Marker syntax (parsed by `cmd/skytime-docgen/marker.go`)

Markers are Go comment lines directly above a builtin's func decl. The prefix is exactly
`// skytime:doc ` (trailing space required), followed by `key="Go-quoted string"`
(unquoted via `strconv.Unquote`, so escape with `\"`):

```go
// skytime:doc summary="Emit a structured log record at INFO level."
// skytime:doc param_msg="string"
// skytime:doc desc_msg="String literal; ${ctx.expr} interpolation supported."
// skytime:doc returns="A *dag.LogStep node..."
// skytime:doc example="log.info(\"done: ${ctx.x}\")"
// skytime:doc see="log.warn, log.error"
// skytime:doc since="phase-07.2.1"
```

Recognized keys (see `cmd/skytime-docgen/render.go`): `summary` (repeatable — values join
as paragraphs), `returns`, `example`, `see`, `since`, `param_<name>` (type string),
`desc_<name>` (param description). Malformed marker lines print a stderr warning and are
SILENTLY SKIPPED — check docgen stderr after editing markers, a typo won't fail the build,
it just drops the entry.

The walker (`cmd/skytime-docgen/walk.go`) discovers builtins from
`starlark.NewBuiltin("name", p.builtinXxx)` registrations in `pkg/parser/globals.go`, then
reads markers from every non-test `builtins*.go` file in `pkg/parser/` (currently
`builtins.go` + `builtins_log.go`). Output order = registration source order. Reference
`pkg/parser/builtins_log.go:112-121` for a complete marker block to copy.

`cmd/skytime-docgen` must stay **stdlib-only** — no cobra/charm-log/lipgloss
(`tests/firewall_cli_test.go` allow-lists only `pkg/cli` for those imports).

## README.md ↔ docs/getting-started.md duplication rule (D-06)

`docs/getting-started.md` is canonical. `README.md` embeds the same tutorial verbatim in its
"Getting Started" section — with one deliberate difference: **README uses root-relative links**
(`examples/skeleton/...`), getting-started uses `../`-relative ones. A naive copy in either
direction produces dead links. There is NO drift test; README itself says to check with
`git diff docs/getting-started.md README.md`, and slight divergence is tolerated. When you
change the tutorial, update both files, rewriting link prefixes.

Known stale: README's Status section links `.planning/REQUIREMENTS.md`, which was archived to
`.planning/milestones/v1.43.0-REQUIREMENTS.md`. Fix only if asked — it is not gated.

## Walkthrough docs are structure-pinned

Two walkthroughs have their H2 heading sets pinned **verbatim** (em-dashes, backticks,
start-of-line position included) by tests in `tests/`:

- `docs/walkthroughs/dashboard.md` → `tests/walkthrough_dashboard_headings_test.go`
  (12 exact H2s + required reference strings like `event: shutdown`, `SKYTIME_TEMPORAL_WEB_UI`,
  `--replay-history-threshold=50`).
- `docs/walkthroughs/github-webhook.md` → `tests/walkthrough_github_webhook_headings_test.go`
  (12 headings + references like `gh webhook forward`, `X-Hub-Signature-256`,
  `signature_mismatch` — AND a forbidden phrase: `30 seconds later` must never appear).

Never rename, reorder, or reword a pinned H2. You may add NEW sections and edit body prose,
but keep every required reference string somewhere in the body. These tests grep, they don't
parse markdown — even meta-prose *about* a forbidden phrase trips them (this happened once).
`cron-schedules.md` has no headings test. After any walkthrough edit run:

```sh
go test ./tests/ -run TestWalkthrough -count=1
```

Smoke scripts: `docs/walkthroughs/cron-schedules-smoke.sh` (gated `SKYTIME_RUN_CRON_SMOKE=1`)
and `dashboard-smoke.sh` (gated `SKYTIME_RUN_DASHBOARD_SMOKE=1`) are no-ops in default CI.
The always-run CI smoke is `.github/workflows/scripts/walkthrough_smoke.sh`, which mirrors
`examples/http-github-webhook/README.md` "Quick start" commands 2–4 **verbatim** — editing
those README commands requires the identical edit in the script (declared CI failure otherwise).

## Repo-wide grep gates that bite doc edits

- **`dev-server` is banned** (`tests/dev_server_grep_test.go`, D-07-22): the literal may not
  appear in ANY git-tracked file outside `.planning/`, `CHANGELOG.md`, and the gate file itself.
  Comments and shell scripts count. Write `dev-temporal` (the subcommand) or
  `temporal dev server` (the subprocess). Failure text starts "D-07-22 GREP GATE VIOLATION".
- **`TLSDisabled` stays out of the auth snippets**: `snippets/*.go` and the matching fences in
  `temporal-auth.md` deliberately paraphrase ("do NOT disable TLS via ConnectionOptions") —
  a project firewall decision recorded in `.planning/STATE.md` (Phase 07.5 plans 02/03). The
  literal is fine elsewhere (`docs/reference/cli.md`, `pkg/worker/`). (No automated test found
  enforcing this — treat as a locked decision, unverified as a gate.)
- Provider webhook header names (e.g. `X-GitHub-Event`) may not appear in non-comment lines
  under `pkg/cli/server/web/` (`tests/firewall_source_agnostic_test.go`) — relevant if a doc
  edit tempts you to "improve" dashboard code alongside.

## temporal-auth.md ↔ snippets drift gate

`docs/for-extension-developers/temporal-auth.md` ships four Go auth snippets. Each ```` ```go ````
fence is preceded by `<!-- snippet: <name>.go -->` and must equal
`docs/for-extension-developers/snippets/<name>.go` byte-for-byte after TrimSpace only —
internal whitespace matters (`snippets/drift_test.go`). Edit fence and file together, then:

```sh
cd docs/for-extension-developers/snippets && go test -run TestMarkdownSnippetDrift -count=1 .
```

`snippets/` is a **standalone Go module** (own `go.mod`) that must never require the main
skytime module. Its `go.mod` says `go 1.25.0` — the toolchain rewrites `go 1.25` on tidy, so
never grep for `^go 1.25$`. CI runs download/build/vet/test in that directory as a dedicated
step (see `.github/workflows/ci.yml`).

## examples/README.md and skeleton fixtures

A new `examples/skeleton/*.star` fixture has **three touch points**:
1. The `.star` file itself (≤30 LOC, one concept).
2. An `examples/README.md` section using the fixed template (What It Demonstrates / How to
   Run / Expected Output / See Also) — see the "Adding a New Example" section there.
3. `tests/differential_test.go` — the corpus auto-picks up the file; add extensions to
   `corpusExtensions(t)` and, if the flow deliberately `fail()`s under stub inputs, add its
   name to `expectedErrFlows`, or CI breaks.

## GSD planning docs (`.planning/`)

- `CLAUDE.md` mandates routing file edits through `/gsd:quick`, `/gsd:debug`, or
  `/gsd:execute-phase` unless the user explicitly bypasses. Honor it for doc edits too.
- `PROJECT.md` holds **locked carve-out texts** quoted verbatim across docs (D4.1-22
  no-string-compilation carve-out, the Web UI carve-out). Never reword them; extending the
  interpolation carve-out requires a new ADR.
- `STATE.md` is a ~510-line append-only decision log. Its "Blockers/Concerns" contains stale
  v1.42-era items and its frontmatter still says `status: verifying` — do not act on either.
- `MILESTONES.md` contains raw auto-dumped SUMMARY fragments in the v1.43.0 section
  ("Approver:", "[Rule 3 - Blocking]...") — curation is manual, not a bug to auto-fix.
- Milestone plans follow the house style of `.planning/v1.43-DRAFT-PLAN.md`: Status header
  awaiting `/gsd:new-milestone`, "Why This Milestone Exists" gaps, numbered Definition of
  Done, "Locked Design Decisions" table (do not re-litigate), per-phase Goal/Deliverables,
  migration path, open items. Completed milestones archive to `.planning/milestones/`
  (e.g. `v1.43.0-REQUIREMENTS.md`, `v1.43.0-MILESTONE-AUDIT.md`).
- Decision IDs (`D4-14`, `D-07-22`, ...) and REQ IDs (`TRIG-01`, `AUTH-01`, ...) are
  cross-referenced **verbatim** between planning docs, user docs, and test names. Never
  rename one — traceability breaks silently.

## Common mistakes

- **Hand-edited `builtins.md`** → `TestDocgenDrift` fails: "docs/reference/builtins.md is out
  of date. Run `go generate ./pkg/parser/` and commit the result." Same failure if you change
  a builtin signature or marker and forget to regenerate.
- **Typed `dev-server` in any tracked file** (even a script comment — bit
  `cron-schedules-smoke.sh` once) → `TestNoDevServerLiteralRemains` lists the file.
- **Edited a `temporal-auth.md` fence but not `snippets/*.go`** (or vice versa) →
  `TestMarkdownSnippetDrift` fails with "drift between markdown fence and <name>.go" and a
  side-by-side dump. Remember: TrimSpace-only normalization.
- **Reworded a pinned walkthrough H2** ("Step 5 — ..." em-dash → hyphen counts) →
  `TestWalkthroughDashboardHeadings` / `TestWalkthrough_GitHubWebhookHeadings` reports the
  missing heading string.
- **Wrote "30 seconds later" in github-webhook.md** (even to explain why it's forbidden) →
  anti-claim assertion fails; describe the kill-restart demo instead.
- **Copied tutorial text README→getting-started (or back) without rewriting link prefixes**
  → dead links; no test catches it, reviewers do.
- **Wrote "consultant" in a user-facing doc** → no test, but violates a locked retro decision;
  use "flow author".
- **Edited `pkg/cli/server/web/dashboard.html` while touching the dashboard walkthrough** →
  byte-exact golden fails; regenerate with
  `GSD_UPDATE_GOLDEN=1 go test -run TestTemplate_DashboardGolden ./pkg/cli/server/web`
  (golden at `pkg/cli/server/web/testdata/dashboard.html.golden`).
- **Edited a `tests/fixtures/valid/*.star` doc-adjacent fixture** → goldens in another package
  fail; regenerate via `UPDATE_GOLDEN=1 go test ./pkg/parser/... -run TestValidFixtures`.

## Verification one-liner after any docs change

```sh
go test ./tests/ -run 'TestDocgenDrift|TestWalkthrough|TestNoDevServerLiteral' -count=1
```

CI (`.github/workflows/ci.yml`) runs on every push to any branch: vet, `go test -race ./...`,
extbin build + `extbin test`, walkthrough smoke, snippets module step. All doc gates above run
inside `go test ./...`.
