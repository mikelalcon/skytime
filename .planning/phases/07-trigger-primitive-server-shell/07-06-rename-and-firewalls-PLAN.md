---
phase: 07-trigger-primitive-server-shell
plan: 06
type: execute
wave: 5
depends_on: [05]
priority: high
estimated_tasks: 9
autonomous: true
requirements:
  - CLI-13
files_modified:
  - pkg/cli/dev_temporal.go
  - pkg/cli/dev_temporal_test.go
  - pkg/cli/root.go
  - pkg/cli/root_test.go
  - cmd/skytime/main.go
  - examples/http-github-webhook/cmd/extbin/main.go
  - examples/http-github-webhook/cmd/extbin/main_test.go
  - examples/http-github-webhook/README.md
  - examples/README.md
  - README.md
  - CLAUDE.md
  - docs/getting-started.md
  - docs/architecture.md
  - docs/cli-binary.md
  - docs/reference/cli.md
  - docs/for-flow-authors/README.md
  - .github/workflows/scripts/walkthrough_smoke.sh
  - tests/firewall_credential_redaction_test.go
  - tests/dev_server_grep_test.go
must_haves:
  truths:
    - "pkg/cli/dev_server.go renamed to pkg/cli/dev_temporal.go via git mv (rename tracked by similarity)"
    - "pkg/cli/dev_server_test.go renamed to pkg/cli/dev_temporal_test.go"
    - "Symbol newDevServerCommand renamed to newDevTemporalCommand throughout pkg/cli; cobra Use string changed to dev-temporal"
    - "Test names TestDevServerCmd_* renamed to TestDevTemporalCmd_*"
    - "All tracked files (excluding .planning/, CHANGELOG.md, and tests/dev_server_grep_test.go itself) contain ZERO occurrences of the literal dev-server (D-07-22)"
    - "Auto-generated docs (docs/reference/builtins.md and similar) regenerated via go generate ./...; the existing docgen drift test passes"
    - "tests/dev_server_grep_test.go is the CI gate enforcing the absence of the dev-server literal in tracked files (D-07-22)"
    - "tests/firewall_credential_redaction_test.go AST-walks pkg/dag, pkg/extension, and pkg/extension/builtin; rejects fmt directives %+v or %#v in production (non-test) Go source files (D-07-10)"
    - "examples/http-github-webhook/cmd/extbin/main.go top doc-comment subcommand list updates: dev-server reference removed; new server subcommand listed (inherited via cli.NewRootCommand)"
    - ".github/workflows/scripts/walkthrough_smoke.sh contains zero dev-server literals (cleanup-message strings updated)"
  artifacts:
    - path: pkg/cli/dev_temporal.go
      provides: "Renamed file (was dev_server.go); cobra Use dev-temporal; symbol newDevTemporalCommand"
      contains: "newDevTemporalCommand"
    - path: pkg/cli/dev_temporal_test.go
      provides: "Renamed test file with TestDevTemporalCmd_* test names"
      contains: "TestDevTemporalCmd_"
    - path: tests/firewall_credential_redaction_test.go
      provides: "AST walker rejecting %+v / %#v formatting verbs in pkg/dag, pkg/extension, pkg/extension/builtin (D-07-10)"
      contains: "TestCredentialRedactionFirewall"
    - path: tests/dev_server_grep_test.go
      provides: "Tracked-file grep gate ensuring no dev-server literal remains (D-07-22)"
      contains: "TestNoDevServerLiteralRemains"
  key_links:
    - from: pkg/cli/root.go
      to: pkg/cli/dev_temporal.go
      via: "root.AddCommand(newDevTemporalCommand(cfg))"
      pattern: "newDevTemporalCommand"
    - from: tests/dev_server_grep_test.go
      to: every tracked file
      via: "git ls-files iteration with allow-list"
      pattern: "git ls-files"
    - from: tests/firewall_credential_redaction_test.go
      to: pkg/dag/trigger.go
      via: "go/ast walker over production .go files"
      pattern: "ast.Inspect"
---

<objective>
Land the dev-server to dev-temporal rename (CLI-13) plus the credential-redaction AST firewall (D-07-10) and the grep-gate CI test (D-07-22). Wave 5: depends on Plan 05 because the new server subcommand must be registered first.

Purpose: Pre-1.0 hard rename per D-07-21 — no deprecation alias. Every literal dev-server reference in the tree (excluding .planning/ historical archive, CHANGELOG.md, and the grep-gate test itself) becomes dev-temporal. Two new firewall tests enforce: (1) rename completeness via grep gate; (2) no formatting verbs %+v or %#v in pkg/dag, pkg/extension, or pkg/extension/builtin (the load-bearing packages where Trigger and TriggerSource live).

Output: 8 file renames (file moves), ~15 doc/code prose updates, 2 NEW firewall test files, regenerated docs/reference/builtins.md (mechanical), one updated CI smoke script.

LOAD-BEARING CONSTRAINTS:
1. Hard rename per D-07-21 — no dev-server alias remains anywhere.
2. Allow-list for grep gate — .planning/ (historical), CHANGELOG.md if present, tests/dev_server_grep_test.go itself (it contains the literal as the thing being checked). Documented explicitly in the test.
3. Credential-redaction firewall AST-walks Go source. Phase 7 ships zero concrete TriggerSource — only pkg/dag/trigger.go has the Trigger type. Phase 7.1 extends the firewall when github.WebhookSource lands.
4. walkthrough_smoke.sh may invoke temporal server start-dev directly; audit cleanup-message strings before changing.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/STATE.md
@.planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md
@.planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md
@.planning/phases/07-trigger-primitive-server-shell/07-VALIDATION.md
@.planning/phases/07-trigger-primitive-server-shell/07-05-SUMMARY.md
@CLAUDE.md
@pkg/cli/dev_server.go
@pkg/cli/dev_server_test.go
@pkg/cli/root.go
@pkg/cli/root_test.go
@cmd/skytime/main.go
@examples/http-github-webhook/cmd/extbin/main.go
@examples/http-github-webhook/cmd/extbin/main_test.go
@README.md
@docs/getting-started.md
@docs/cli-binary.md
@docs/reference/cli.md
@docs/architecture.md
@docs/for-flow-authors/README.md
@examples/http-github-webhook/README.md
@examples/README.md
@.github/workflows/scripts/walkthrough_smoke.sh
@tests/firewall_cli_test.go
@tests/docgen_drift_test.go
@pkg/dag/trigger.go

<reference_grep_results>
Discovery grep (executed at planning time, RESEARCH.md § Pitfall 8):
- cmd/skytime/main.go: lines 1, 43
- README.md: lines 39, 52, 81, 102, 113, 133, 141, 353
- CLAUDE.md: lines 51, 89, 93
- tests/e2e_skytime_run_test.go: lines 70, 215, 311 (test fixtures — likely OK to keep but verify; if they assert against the literal, treat as legitimate dev-server reference within an existing TEST and exclude via the grep gate's filename allow-list)
- tests/skytime_test_e2e_test.go: line 5 (build tag comment — review)
- docs/architecture.md: line 256
- docs/getting-started.md: lines 10, 31, 42, 62, 70, 283
- docs/cli-binary.md: line 86
- docs/for-flow-authors/README.md: lines 17, 35, 55
- docs/reference/cli.md: many — lines 3, 79, 289, 293, 295, 299, 303, 304, 306, 318, 323, 329 (largest single doc surface)
</reference_grep_results>
</context>

<tasks>

<task type="auto">
  <id>07-06-01</id>
  <name>Task 1: Rename pkg/cli/dev_server.go to dev_temporal.go and rename internal symbols + cobra Use string</name>
  <read_first>
    - pkg/cli/dev_server.go (FULL — symbol newDevServerCommand, Use literal, Long text)
    - pkg/cli/dev_server_test.go (FULL — TestDevServerCmd_* + the literal "dev_server.go" filename in TestDevServerCmd_SignalForward body)
    - pkg/cli/root.go (line ~61 — root.AddCommand(newDevServerCommand(cfg)))
    - pkg/cli/root_test.go (any TestRoot_HasDevServerSubcommand variant)
  </read_first>
  <files>pkg/cli/dev_temporal.go, pkg/cli/dev_temporal_test.go, pkg/cli/root.go, pkg/cli/root_test.go</files>
  <action>
    git-rename:
    ```bash
    git mv pkg/cli/dev_server.go pkg/cli/dev_temporal.go
    git mv pkg/cli/dev_server_test.go pkg/cli/dev_temporal_test.go
    ```

    Edit dev_temporal.go: replace symbol newDevServerCommand to newDevTemporalCommand (declaration + doc comment); replace cobra Use dev-server to dev-temporal; update Short and Long strings.

    Edit dev_temporal_test.go: rename TestDevServerCmd_* functions to TestDevTemporalCmd_*; update internal newDevServerCommand references to newDevTemporalCommand; CRITICAL: replace any literal "dev_server.go" filename reference (in TestDevServerCmd_SignalForward body) with "dev_temporal.go".

    Edit root.go: replace root.AddCommand(newDevServerCommand(cfg)) with newDevTemporalCommand; update package doc comment to list the renamed subcommand.

    Edit root_test.go: rename TestRoot_HasDevServerSubcommand to TestRoot_HasDevTemporalSubcommand; assert against the new Use string dev-temporal.

    Verify:
    ```bash
    go build ./...
    go test ./pkg/cli/... -count=1 -race
    go run ./cmd/skytime --help 2>&1 | grep dev-temporal
    ```
  </action>
  <acceptance_criteria>
    - File pkg/cli/dev_server.go does NOT exist; pkg/cli/dev_temporal.go DOES exist
    - File pkg/cli/dev_server_test.go does NOT exist; pkg/cli/dev_temporal_test.go DOES exist
    - grep -nE 'newDevTemporalCommand' pkg/cli/dev_temporal.go returns at least 2 matches
    - grep -nE 'Use:\s+"dev-temporal"' pkg/cli/dev_temporal.go returns exactly one match
    - ! grep -nE 'newDevServerCommand|Use:\s+"dev-server"' pkg/cli/dev_temporal.go
    - grep -nE 'TestDevTemporalCmd_' pkg/cli/dev_temporal_test.go returns at least 3 matches
    - ! grep -F 'dev_server.go' pkg/cli/dev_temporal_test.go
    - grep -n 'newDevTemporalCommand' pkg/cli/root.go returns exactly one match
    - ! grep -n 'newDevServerCommand' pkg/cli/root.go
    - grep -nE 'TestRoot_HasDevTemporalSubcommand' pkg/cli/root_test.go returns exactly one match
    - go build ./... exits 0
    - go test ./pkg/cli/... -count=1 -race exits 0
    - go run ./cmd/skytime --help shows dev-temporal, NOT dev-server
  </acceptance_criteria>
  <verify>
    <automated>go build ./... && go test ./pkg/cli/... -count=1 -race && [ ! -f pkg/cli/dev_server.go ] && [ -f pkg/cli/dev_temporal.go ] && grep -q 'Use:\s*"dev-temporal"' pkg/cli/dev_temporal.go && ! grep -q 'newDevServerCommand' pkg/cli/root.go</automated>
  </verify>
  <done>
    Files renamed; symbols updated; cobra Use string changed; tests renamed; binary --help shows dev-temporal.
  </done>
</task>

<task type="auto">
  <id>07-06-02</id>
  <name>Task 2: Update top doc-comments in cmd/skytime/main.go and examples/extbin/main.go (and main_test.go subcommand list)</name>
  <read_first>
    - cmd/skytime/main.go (lines 1, 43)
    - examples/http-github-webhook/cmd/extbin/main.go (lines 1-17)
    - examples/http-github-webhook/cmd/extbin/main_test.go (subcommand-list assertion)
  </read_first>
  <files>cmd/skytime/main.go, examples/http-github-webhook/cmd/extbin/main.go, examples/http-github-webhook/cmd/extbin/main_test.go</files>
  <action>
    cmd/skytime/main.go: line 1 doc comment "validate, run, and dev-server subcommands" becomes "validate, run, dev-temporal, server, info, and test subcommands". Line 43 "(validate, run, dev-server)" becomes "(validate, run, dev-temporal, server)".

    extbin/main.go: update lines 7-13 subcommand list. New list:
    ```
    extbin validate <file.star>      static validation (Tier 1)
    extbin run <file.star> ...       trigger a workflow against a Temporal cluster
    extbin dev-temporal              spawn a local Temporal dev server (renamed from dev-server in Phase 7)
    extbin server                    long-lived worker (Phase 7+)
    extbin test <dir>                discover + run *_test.star (Tier 3)
    ```

    extbin/main_test.go: replace any subcommand-list assertion array — old [validate, run, dev-server, test] becomes [validate, run, dev-temporal, server, test]. Verify ordering against the actual cobra Commands() iteration.

    Verify:
    ```bash
    go build ./cmd/skytime/... ./examples/http-github-webhook/cmd/extbin/...
    go test ./examples/http-github-webhook/cmd/extbin/... -count=1 -race
    ! grep -F 'dev-server' cmd/skytime/main.go examples/http-github-webhook/cmd/extbin/main.go examples/http-github-webhook/cmd/extbin/main_test.go
    ```
  </action>
  <acceptance_criteria>
    - ! grep -F 'dev-server' cmd/skytime/main.go
    - grep -F 'dev-temporal' cmd/skytime/main.go returns at least one match
    - ! grep -F 'dev-server' examples/http-github-webhook/cmd/extbin/main.go
    - grep -F 'dev-temporal' examples/http-github-webhook/cmd/extbin/main.go returns at least one match
    - grep -F 'extbin server' examples/http-github-webhook/cmd/extbin/main.go returns at least one match
    - ! grep -F 'dev-server' examples/http-github-webhook/cmd/extbin/main_test.go
    - go build ./cmd/skytime/... ./examples/http-github-webhook/cmd/extbin/... exits 0
    - go test ./examples/http-github-webhook/cmd/extbin/... -count=1 -race exits 0
  </acceptance_criteria>
  <verify>
    <automated>go build ./cmd/skytime/... ./examples/http-github-webhook/cmd/extbin/... && go test ./examples/http-github-webhook/cmd/extbin/... -count=1 -race && ! grep -F 'dev-server' cmd/skytime/main.go && ! grep -F 'dev-server' examples/http-github-webhook/cmd/extbin/main.go && grep -F 'dev-temporal' cmd/skytime/main.go</automated>
  </verify>
  <done>
    Both binary doc comments and the extbin subcommand-list test reflect dev-temporal plus the new server subcommand. No dev-server literal remains.
  </done>
</task>

<task type="auto">
  <id>07-06-03</id>
  <name>Task 3: Update README.md, CLAUDE.md, and root-level docs/ files</name>
  <read_first>
    - README.md (lines 39, 52, 81, 102, 113, 133, 141, 353 per discovery grep)
    - CLAUDE.md (lines 51, 89, 93)
    - docs/getting-started.md (lines 10, 31, 42, 62, 70, 283)
    - docs/architecture.md (line 256)
    - docs/cli-binary.md (line 86)
  </read_first>
  <files>README.md, CLAUDE.md, docs/getting-started.md, docs/architecture.md, docs/cli-binary.md</files>
  <action>
    Replace every dev-server occurrence with dev-temporal in each file. For prose contexts, the simple substitution preserves meaning. For verbatim CLI commands (e.g. ./skytime dev-server), update to ./skytime dev-temporal.

    For CLAUDE.md line 89 (TLS spec note about local dev-server connections): the literal here refers to a connection style (TLSDisabled=true), not a Skytime subcommand. Rewording: "Be explicit: set TLSDisabled: true only for local-dev (dev-temporal) connections" — keeps the parenthetical for clarity, removes the dev-server literal.

    Verify:
    ```bash
    ! grep -F 'dev-server' README.md CLAUDE.md docs/getting-started.md docs/architecture.md docs/cli-binary.md
    grep -F 'dev-temporal' README.md  # at least once
    ```

    All must succeed.
  </action>
  <acceptance_criteria>
    - ! grep -F 'dev-server' README.md
    - ! grep -F 'dev-server' CLAUDE.md
    - ! grep -F 'dev-server' docs/getting-started.md
    - ! grep -F 'dev-server' docs/architecture.md
    - ! grep -F 'dev-server' docs/cli-binary.md
    - grep -F 'dev-temporal' README.md returns at least one match
    - grep -F 'dev-temporal' docs/getting-started.md returns at least one match
    - grep -F 'dev-temporal' docs/cli-binary.md returns at least one match
  </acceptance_criteria>
  <verify>
    <automated>! grep -F 'dev-server' README.md CLAUDE.md docs/getting-started.md docs/architecture.md docs/cli-binary.md && grep -F 'dev-temporal' README.md docs/getting-started.md docs/cli-binary.md</automated>
  </verify>
  <done>
    Five doc files have zero dev-server literal; dev-temporal used consistently.
  </done>
</task>

<task type="auto">
  <id>07-06-04</id>
  <name>Task 4: Update docs/reference/cli.md (largest doc surface, add new server section) and docs/for-flow-authors/README.md</name>
  <read_first>
    - docs/reference/cli.md (FULL — many occurrences per grep results; entire ## skytime dev-server section needs renaming + body updates)
    - docs/for-flow-authors/README.md (lines 17, 35, 55)
    - pkg/cli/server.go (Plan 05 output — source for the new server section's content)
  </read_first>
  <files>docs/reference/cli.md, docs/for-flow-authors/README.md</files>
  <action>
    cli.md changes:
    1. Rename section heading ## skytime dev-server to ## skytime dev-temporal. Update any TOC.
    2. In-section content updates: skytime dev-server [persistent flags] becomes skytime dev-temporal [persistent flags]. Source path pkg/cli/dev_server.go becomes pkg/cli/dev_temporal.go. Prose mentions of dev-server within the section become dev-temporal.
    3. Line 79 "Otherwise to dev-server via worker.NewDevClient" — disambiguate connection-style from subcommand. Replacement: "Otherwise to local-dev (dev-temporal) connection via worker.NewDevClient (TLSDisabled=true)".
    4. ADD a new section ## skytime server documenting SERVER-01..03 surface. Include flag inventory (rootdir, task-queue, addr, credfile, drain-timeout, json-log), drain semantics, signal handling, banner format, exit codes. Reference pkg/cli/server.go as the source. ~80 lines.

    docs/for-flow-authors/README.md: replace dev-server with dev-temporal in the 3 prose occurrences (lines 17, 35, 55).

    Verify:
    ```bash
    ! grep -F 'dev-server' docs/reference/cli.md docs/for-flow-authors/README.md
    grep -F '## skytime dev-temporal' docs/reference/cli.md
    grep -F '## skytime server' docs/reference/cli.md
    ```
  </action>
  <acceptance_criteria>
    - ! grep -F 'dev-server' docs/reference/cli.md
    - ! grep -F 'dev-server' docs/for-flow-authors/README.md
    - grep -F '## skytime dev-temporal' docs/reference/cli.md returns exactly one match
    - grep -F '## skytime server' docs/reference/cli.md returns exactly one match
    - grep -F 'pkg/cli/dev_temporal.go' docs/reference/cli.md returns at least one match
    - ! grep -F 'pkg/cli/dev_server.go' docs/reference/cli.md
  </acceptance_criteria>
  <verify>
    <automated>! grep -F 'dev-server' docs/reference/cli.md docs/for-flow-authors/README.md && grep -F '## skytime dev-temporal' docs/reference/cli.md && grep -F '## skytime server' docs/reference/cli.md && ! grep -F 'pkg/cli/dev_server.go' docs/reference/cli.md</automated>
  </verify>
  <done>
    cli.md rename complete; new ## skytime server section documents SERVER-01..03; pkg/cli source path references updated; for-flow-authors README updated.
  </done>
</task>

<task type="auto">
  <id>07-06-05</id>
  <name>Task 5: Update example READMEs (examples/README.md and examples/http-github-webhook/README.md)</name>
  <read_first>
    - examples/README.md (~5 occurrences per discovery)
    - examples/http-github-webhook/README.md (~3 occurrences)
  </read_first>
  <files>examples/README.md, examples/http-github-webhook/README.md</files>
  <action>
    Replace dev-server with dev-temporal in each file's prose. For walkthrough commands or copy-paste-ready CLI snippets, update to dev-temporal.

    examples/http-github-webhook/README.md may have a more substantial update if the README documents a setup walkthrough using dev-server — rewrite the relevant paragraph to use dev-temporal and note the rename context briefly (one sentence: "renamed from dev-server in Phase 7").

    Verify:
    ```bash
    ! grep -F 'dev-server' examples/README.md examples/http-github-webhook/README.md
    grep -F 'dev-temporal' examples/README.md examples/http-github-webhook/README.md
    ```

    All must succeed.
  </action>
  <acceptance_criteria>
    - ! grep -F 'dev-server' examples/README.md
    - ! grep -F 'dev-server' examples/http-github-webhook/README.md
    - grep -F 'dev-temporal' examples/README.md returns at least one match
    - grep -F 'dev-temporal' examples/http-github-webhook/README.md returns at least one match
  </acceptance_criteria>
  <verify>
    <automated>! grep -F 'dev-server' examples/README.md examples/http-github-webhook/README.md && grep -F 'dev-temporal' examples/README.md examples/http-github-webhook/README.md</automated>
  </verify>
  <done>
    Both example READMEs updated; no dev-server literal remains.
  </done>
</task>

<task type="auto">
  <id>07-06-06</id>
  <name>Task 6: Update .github/workflows/scripts/walkthrough_smoke.sh and any e2e test fixtures</name>
  <read_first>
    - .github/workflows/scripts/walkthrough_smoke.sh (FULL file — lines 44, 48, 52, 58 per discovery grep contain dev-server literal)
    - tests/e2e_skytime_run_test.go (lines 70, 215, 311 contain dev-server in comments)
    - tests/skytime_test_e2e_test.go (line 5 build-tag comment)
  </read_first>
  <files>.github/workflows/scripts/walkthrough_smoke.sh, tests/e2e_skytime_run_test.go, tests/skytime_test_e2e_test.go</files>
  <action>
    walkthrough_smoke.sh: lines 44, 48, 52, 58 reference dev-server in echo / log / cleanup messages. The script itself shells "temporal server start-dev" directly (NOT skytime dev-server). The "dev-server" literals in cleanup messages should become "dev-temporal" for consistency, OR the more accurate "temporal dev server" (referring to the subprocess started via "temporal server start-dev"). Recommendation: use "temporal dev server" for accuracy.

    Replacement strategy:
    - Line 44 echo "==> Starting temporal dev-server (background, headless)" becomes "==> Starting temporal dev server (background, headless)" (drop hyphen — refers to the Temporal dev-server, not Skytime's subcommand).
    - Line 48 trap-cleanup comment "guarantees the dev-server is stopped" becomes "guarantees the temporal dev server is stopped".
    - Line 52 echo "==> Cleaning up temporal dev-server (pid=$TEMPORAL_PID)" becomes "==> Cleaning up temporal dev server (pid=$TEMPORAL_PID)".
    - Line 58 "Wait for the dev-server to be ready" becomes "Wait for the temporal dev server to be ready".

    e2e_skytime_run_test.go: lines 70, 215, 311 contain dev-server in test comments. Replace with "temporal dev server" or "dev-temporal" depending on context (prose comment vs. CLI command reference).

    skytime_test_e2e_test.go line 5 build-tag comment: "this test does not need a Temporal dev-server" — rewrite as "this test does not need a Temporal dev server" (no hyphen — refers to a Temporal-side concept).

    Verify:
    ```bash
    ! grep -F 'dev-server' .github/workflows/scripts/walkthrough_smoke.sh tests/e2e_skytime_run_test.go tests/skytime_test_e2e_test.go
    bash -n .github/workflows/scripts/walkthrough_smoke.sh  # syntax check shell script
    go test ./tests/ -run 'TestE2E' -count=1 -short  # quick smoke if -short skips heavy work
    ```

    All must exit 0.
  </action>
  <acceptance_criteria>
    - ! grep -F 'dev-server' .github/workflows/scripts/walkthrough_smoke.sh
    - ! grep -F 'dev-server' tests/e2e_skytime_run_test.go
    - ! grep -F 'dev-server' tests/skytime_test_e2e_test.go
    - grep -F 'temporal dev server' .github/workflows/scripts/walkthrough_smoke.sh returns at least one match (or alternative phrasing that disambiguates)
    - bash -n .github/workflows/scripts/walkthrough_smoke.sh exits 0
    - go test ./tests/ -count=1 -short exits 0
  </acceptance_criteria>
  <verify>
    <automated>! grep -F 'dev-server' .github/workflows/scripts/walkthrough_smoke.sh tests/e2e_skytime_run_test.go tests/skytime_test_e2e_test.go && bash -n .github/workflows/scripts/walkthrough_smoke.sh</automated>
  </verify>
  <done>
    CI smoke script and e2e test files have zero dev-server literal. Smoke script syntax verified.
  </done>
</task>

<task type="auto">
  <id>07-06-07</id>
  <name>Task 7: Regenerate auto-generated docs (go generate ./...) and confirm docgen drift test passes</name>
  <read_first>
    - tests/docgen_drift_test.go (FULL file — understand the drift test mechanism: runs go generate then diffs)
    - docs/reference/builtins.md (the auto-generated doc — built from skytime:doc markers in pkg/parser/builtins.go)
    - pkg/parser/builtins.go (Plan 03 added skytime:doc markers for the trigger builtin; those propagate to builtins.md on regeneration)
  </read_first>
  <files>docs/reference/builtins.md (regenerated; do NOT hand-edit)</files>
  <action>
    Run regeneration:
    ```bash
    go generate ./...
    ```

    Verify the drift test passes:
    ```bash
    go test ./tests/ -run TestDocgenDrift -count=1
    ```

    Must exit 0. If failure: the regeneration produced a different builtins.md than the committed version — commit the regenerated version (which now includes Plan 03's trigger builtin documentation).

    Step 2 — Verify the regenerated builtins.md does not contain dev-server (it shouldn't — builtins.md only documents Starlark builtins, not CLI subcommands; the rename has zero effect on it):
    ```bash
    ! grep -F 'dev-server' docs/reference/builtins.md
    grep -F 'trigger(' docs/reference/builtins.md  # confirms Plan 03's builtin appears
    ```

    Both must succeed.

    DO NOT hand-edit docs/reference/builtins.md — it's auto-generated; manual edits would drift on next regeneration.
  </action>
  <acceptance_criteria>
    - go generate ./... exits 0
    - go test ./tests/ -run TestDocgenDrift -count=1 exits 0
    - ! grep -F 'dev-server' docs/reference/builtins.md
    - grep -F 'trigger(' docs/reference/builtins.md returns at least one match (Plan 03's trigger builtin documented)
  </acceptance_criteria>
  <verify>
    <automated>go generate ./... && go test ./tests/ -run TestDocgenDrift -count=1 && ! grep -F 'dev-server' docs/reference/builtins.md && grep -F 'trigger(' docs/reference/builtins.md</automated>
  </verify>
  <done>
    Auto-generated docs regenerated and committed; drift test passes; trigger builtin documented in builtins.md.
  </done>
</task>

<task type="auto" tdd="true">
  <id>07-06-08</id>
  <name>Task 8: Author tests/firewall_credential_redaction_test.go (D-07-10 AST walker)</name>
  <read_first>
    - tests/firewall_cli_test.go (FULL file — pattern reference: package firewall_test, ast walker, findModuleRootCLI helper)
    - pkg/dag/trigger.go (the load-bearing type — its file is one target of the AST walker)
    - .planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md (D-07-10)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (Pitfall 13 / Specific Ideas — firewall test scope)
  </read_first>
  <files>tests/firewall_credential_redaction_test.go</files>
  <behavior>
    - Test 1 (TestCredentialRedactionFirewall): walk Go source files in pkg/dag, pkg/extension, and pkg/extension/builtin (skip if not exists); for each non-test .go file, parse via go/ast; reject any fmt.{Sprintf,Printf,Errorf,Fprintf,Print*} call whose first arg is a string literal containing %+v or %#v. Test files (_test.go) are exempt. Failures list each violation with file:line:col + the function name.
    - Test 2 (TestCredentialRedactionFirewall_AcceptsCleanCode): explicit positive case — the test confirms its own scanner accepts a synthetic AST containing fmt.Sprintf("%v %s %d", a, b, c) (no %+v / %#v). Could be implemented inline by parsing a small valid source string and asserting the walker emits zero violations. This protects against a regression where the matcher becomes overly broad.
  </behavior>
  <action>
    Create tests/firewall_credential_redaction_test.go (NEW). Use package firewall_test (matches firewall_cli_test.go).

    Imports:
    ```go
    package firewall_test

    import (
        "go/ast"
        "go/parser"
        "go/token"
        "os"
        "path/filepath"
        "strings"
        "testing"

        "github.com/stretchr/testify/require"
    )
    ```

    Test body — the full TestCredentialRedactionFirewall function from the interfaces section. Key behavior:
    - Iterate over targetDirs := [pkg/dag, pkg/extension, pkg/extension/builtin]; skip directories that don't exist (pkg/extension/builtin may have sub-packages but the immediate dir may not exist — both should be checked).
    - filepath.Walk through .go files; skip _test.go.
    - Parse each file with go/parser; ast.Inspect for fmt.<Print*> calls.
    - Match call args: first arg must be ast.BasicLit with Kind STRING; check if its Value contains %+v or %#v.
    - Collect violations as file:line:col strings.
    - On non-empty violations, t.Errorf with the canonical D-07-10 message.

    Test 2 implementation:
    ```go
    func TestCredentialRedactionFirewall_AcceptsCleanCode(t *testing.T) {
        cleanSrc := `package x

import "fmt"

func ok() string {
    return fmt.Sprintf("%v %s %d", 1, "two", 3)
}
`
        fset := token.NewFileSet()
        file, err := parser.ParseFile(fset, "x.go", cleanSrc, 0)
        require.NoError(t, err)
        // Reuse the same matcher logic as TestCredentialRedactionFirewall.
        // Refactored: extract matchViolations(fset, file) []string helper
        // and call it from both tests.
        violations := matchViolations(fset, file)
        require.Empty(t, violations, "clean code (no %%+v / %%#v) must produce zero violations")
    }
    ```

    Where matchViolations is a package-private helper extracted from Test 1's body for reuse.

    Verify:
    ```bash
    go test ./tests/ -run TestCredentialRedactionFirewall -count=1
    go vet ./tests/...
    ```

    Both must exit 0. If TestCredentialRedactionFirewall fails (production violations exist), fix them BEFORE proceeding — the firewall is gating the codebase.

    DO NOT add %+v / %#v anywhere in pkg/dag, pkg/extension, or pkg/extension/builtin in this plan.
  </action>
  <acceptance_criteria>
    - File tests/firewall_credential_redaction_test.go exists
    - grep -nE 'func TestCredentialRedactionFirewall\b' tests/firewall_credential_redaction_test.go returns exactly one match
    - grep -nE 'func TestCredentialRedactionFirewall_AcceptsCleanCode' tests/firewall_credential_redaction_test.go returns exactly one match
    - grep -n 'ast.Inspect' tests/firewall_credential_redaction_test.go returns at least one match
    - grep -n '%+v' tests/firewall_credential_redaction_test.go returns at least one match (the literal scanner targets it)
    - grep -n '%#v' tests/firewall_credential_redaction_test.go returns at least one match
    - go test ./tests/ -run TestCredentialRedactionFirewall -count=1 exits 0
    - go test ./tests/ -run TestCredentialRedactionFirewall_AcceptsCleanCode -count=1 exits 0
    - go vet ./tests/... exits 0
  </acceptance_criteria>
  <verify>
    <automated>go test ./tests/ -run TestCredentialRedactionFirewall -count=1 && go test ./tests/ -run TestCredentialRedactionFirewall_AcceptsCleanCode -count=1 && go vet ./tests/...</automated>
  </verify>
  <done>
    D-07-10 firewall in place; AST-walks pkg/dag, pkg/extension, pkg/extension/builtin; rejects %+v / %#v in production code (test files exempt); positive test confirms clean code passes.
  </done>
</task>

<task type="auto" tdd="true">
  <id>07-06-09</id>
  <name>Task 9: Author tests/dev_server_grep_test.go (D-07-22 grep gate) and final phase regression</name>
  <read_first>
    - tests/firewall_cli_test.go (full — findModuleRootCLI helper pattern)
    - .planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md (D-07-22 — grep gate semantics, allow-list)
  </read_first>
  <files>tests/dev_server_grep_test.go</files>
  <behavior>
    - Test 1 (TestNoDevServerLiteralRemains): iterate git ls-files (tracked files only); apply allow-list (.planning/ prefix, CHANGELOG.md filename, tests/dev_server_grep_test.go itself); read each file; check for the literal "dev-server"; collect violations; fail with a clear message listing every violation file.
  </behavior>
  <action>
    Create tests/dev_server_grep_test.go (NEW). Use package firewall_test.

    Use the full TestNoDevServerLiteralRemains body from the interfaces block. Key elements:
    - exec.Command("git", "ls-files") to get tracked files (not glob — git knows what's tracked).
    - allow-list: prefix .planning/ + filenames CHANGELOG.md and tests/dev_server_grep_test.go.
    - read each file; strings.Contains check for "dev-server".
    - On violations, t.Errorf with the full file list.

    Verify:
    ```bash
    go test ./tests/ -run TestNoDevServerLiteralRemains -count=1
    go vet ./tests/...
    ```

    Both must exit 0. If the test FAILS, the violations list pinpoints exactly which Tasks 1-6 missed an occurrence — fix and re-run before this Task 9 is done.

    Final phase verification — run the full test suite:
    ```bash
    go build ./...
    go vet ./...
    go test ./... -count=1 -race
    ```

    All must exit 0. This is the Phase-7 acceptance gate — every test green.
  </action>
  <acceptance_criteria>
    - File tests/dev_server_grep_test.go exists
    - grep -nE 'func TestNoDevServerLiteralRemains' tests/dev_server_grep_test.go returns exactly one match
    - grep -n 'git ls-files' tests/dev_server_grep_test.go returns at least one match
    - grep -n '.planning/' tests/dev_server_grep_test.go returns at least one match (allow-list prefix)
    - grep -n 'CHANGELOG.md' tests/dev_server_grep_test.go returns at least one match (allow-list filename)
    - grep -n 'tests/dev_server_grep_test.go' tests/dev_server_grep_test.go returns at least one match (self-exclusion)
    - go test ./tests/ -run TestNoDevServerLiteralRemains -count=1 exits 0
    - go test ./... -count=1 -race exits 0 (FULL PHASE 7 GATE)
    - go vet ./... exits 0
  </acceptance_criteria>
  <verify>
    <automated>go build ./... && go vet ./... && go test ./tests/ -run TestNoDevServerLiteralRemains -count=1 && go test ./... -count=1 -race</automated>
  </verify>
  <done>
    D-07-22 grep gate in place; full Phase 7 test suite passes; rename complete; both new firewalls green.
  </done>
</task>

</tasks>

<verification>
After all 9 tasks:

```bash
go build ./...
go vet ./...
go test ./... -count=1 -race
```

All must exit 0.

Manual verification:
```bash
git grep -F 'dev-server' -- ':(exclude).planning' ':(exclude)CHANGELOG.md' ':(exclude)tests/dev_server_grep_test.go'
# Expected: zero matches.

go run ./cmd/skytime --help 2>&1 | grep -E '(dev-temporal|server)'
# Expected: both surfaced; no dev-server.
```

Phase 7 sign-off check:
```bash
# All Phase 7 success criterion 5 sub-conditions:
go test ./tests/ -run 'TestNoDevServerLiteralRemains|TestCredentialRedactionFirewall|TestDocgenDrift|TestNoCobraImportsOutsideAllowList' -count=1 -race
```

Must exit 0.
</verification>

<success_criteria>
- CLI-13 satisfied: skytime dev-server fully renamed to skytime dev-temporal across code + docs + CI; zero dev-server literal in tracked files (excluding allow-list); the new TestRoot_HasDevTemporalSubcommand asserts the subcommand registration.
- D-07-21 satisfied: hard rename; no deprecation alias.
- D-07-22 satisfied: tests/dev_server_grep_test.go is the grep gate.
- D-07-10 satisfied: tests/firewall_credential_redaction_test.go AST-walks pkg/dag, pkg/extension, pkg/extension/builtin; rejects %+v / %#v in production code.
- All Phase 7 success criterion 5 sub-conditions met (rename + firewalls + drift + cobra-firewall regression).
- Full Phase 7 test suite green under -race.
</success_criteria>

<output>
After completion, create .planning/phases/07-trigger-primitive-server-shell/07-06-SUMMARY.md documenting:
- The complete file rename inventory (pkg/cli/dev_server.go to dev_temporal.go, plus _test.go counterpart)
- Symbol rename: newDevServerCommand to newDevTemporalCommand
- The cobra Use string change: dev-server to dev-temporal
- Test name renames: TestDevServerCmd_* to TestDevTemporalCmd_*; TestRoot_HasDevServerSubcommand to TestRoot_HasDevTemporalSubcommand
- All 15+ doc/code prose updates with the dev-server to dev-temporal substitution
- The new tests/dev_server_grep_test.go and its allow-list (.planning/, CHANGELOG.md, self-exclusion)
- The new tests/firewall_credential_redaction_test.go and its target package list (pkg/dag, pkg/extension, pkg/extension/builtin), with the test-file exemption
- The walkthrough_smoke.sh prose updates (temporal dev server vs Skytime dev-temporal disambiguation)
- The auto-regenerated docs/reference/builtins.md (Plan 03's trigger builtin documented post-regen)
- The full Phase 7 final test pass list (run by the closing -count=1 -race invocation)
</output>
