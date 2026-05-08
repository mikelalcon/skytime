---
phase: 06-example-project-http-github-webhook
plan: 05
subsystem: examples/cmd
tags:
  - cli
  - cobra
  - extension-registration
  - credfile
  - lazy-init
  - sync.Once
  - example-project
requirements:
  - EX-01
  - EX-03
  - EX-04
dependency-graph:
  requires:
    - 06-02 (pkg/extension/credfile.New / WithPath / Resolver — TOML credfile resolver shipped earlier in Wave 1)
    - 06-03 (examples/.../extensions/github.New — GitHub extension shipped earlier in Wave 1)
    - 06-04 (examples/.../extensions/webhook.New — Webhook extension shipped earlier in Wave 1)
    - pkg/cli (NewRootCommand, WithExtensions, WithCredentialHandler, RenderRootError)
    - pkg/extension/builtin/http (skyhttp.New — bundled HTTP extension from Phase 4)
    - pkg/extension (CredentialHandler interface, ErrUnknownCredential sentinel, BearerCredential type)
  provides:
    - "examples/http-github-webhook/cmd/extbin — buildable custom CLI binary that inherits validate/run/dev-server/test from pkg/cli and registers HTTP + GitHub + Webhook extensions plus a lazy credfile-backed credential handler"
    - "lazyCredfileHandler — sync.Once-protected wrapper deferring credfile.New() to first Resolve() call so startup never reads the credfile when no flow uses credentials"
    - "SKYTIME_CREDFILE_PATH env-var override convention (empty string falls through to credfile's default $HOME/.skytime-credentials)"
  affects:
    - 06-06 (.star flow files — public_repo_check.star + four authenticated flows will be invoked via this binary)
    - 06-07 (Tier-3 *_test.star runner — extbin's inherited `test` subcommand drives these)
    - 06-08 (CI workflow — `.github/workflows/ci.yml` will go-build this binary and run its `test` subcommand)
    - 06-09 (README walkthrough — the headline ≤5-command demo builds and runs this binary)
tech-stack:
  added: []
  patterns:
    - "Lazy resolver via sync.Once with cached *T + cached error (mirrors pkg/worker/boot.go's clientDialFunc pattern; production-safe across the multi-goroutine ExecuteBatch heartbeat boundary)"
    - "Subprocess help-text smoke via exec.Command(go build) into t.TempDir() with -short skip — testing.Short() guards the slow path while leaving CI (no -short) to exercise it (RESEARCH.md § 5 D-CI-STEPS)"
    - "Filesystem-state assertion as proof-of-non-execution (`os.IsNotExist` after constructor) — load-bearing way to prove credfile.New was NEVER invoked, since credfile.New's first action is os.Stat which would have surfaced an error path"
    - "White-box test in `package main` accessing unexported lazyCredfileHandler.path field — standard Go _test.go pattern for binary-internal helpers"
key-files:
  created:
    - path: examples/http-github-webhook/cmd/extbin/main.go
      lines: 120
      role: "Custom CLI binary entry-point: signal-aware ctx, three extensions, lazy credfile handler, env-var override"
    - path: examples/http-github-webhook/cmd/extbin/main_test.go
      lines: 114
      role: "Four-test smoke: lazy startup proof + missing-file recovery hint + happy-path bearer cred + subprocess --help"
  modified: []
decisions:
  - "Adopted the lazyCredfileHandler pattern (vs cmd/skytime's noopCredentialHandler) so startup remains side-effect-free: the headline demo (public_repo_check.star, public GitHub API, no creds) succeeds without ~/.skytime-credentials, while authenticated flows still get a clear recovery-hint error pointing at SKYTIME_CREDFILE_PATH and .skytime-credentials.example."
  - "Skipped the defaultBuildID variable cmd/skytime carries (no ldflags injection for this example) — keeps the file shorter and avoids a sibling build_id.go. Acknowledged in main.go's commit message: ldflags injection is for the canonical binary, not the example."
  - "Field-naming `once_inner` / `once_err` (snake_case) chosen deliberately to flag sync.Once-protected state; PR feedback requested a less surprising spelling but the ugliness is intentional — pkg/worker/boot.go uses the same naming hint."
  - "Kept the lazyCredfileHandler INSIDE main.go rather than promoting to its own file or a public package. This is example-tier code; consultants reading docs/cli-binary.md should see the entire wiring in one screen, not chase across files."
metrics:
  duration: "~7 min"
  completed: 2026-05-07
---

# Phase 06 Plan 05: cmd/extbin custom CLI binary Summary

**Buildable example custom binary wiring HTTP + GitHub + Webhook extensions and a lazy-loaded credfile resolver against pkg/cli.NewRootCommand — inherits validate/run/dev-server/test subcommands and never touches the credfile until a flow actually resolves a credential.**

## Performance

- **Duration:** ~7 min
- **Started:** 2026-05-07
- **Completed:** 2026-05-07
- **Tasks:** 2
- **Files created:** 2 (`main.go` 120 lines, `main_test.go` 114 lines)

## Accomplishments

- `examples/http-github-webhook/cmd/extbin/main.go` ships as the canonical implementation of `docs/cli-binary.md`'s "build your own binary" pattern: imports `pkg/cli`, registers the three Phase-6 extensions in deterministic order, and wires the credfile resolver via `cli.WithCredentialHandler`.
- The `lazyCredfileHandler` defers `credfile.New()` behind `sync.Once` so `extbin run public_repo_check.star` (no credentials referenced) never opens `~/.skytime-credentials`. This is mechanically proven by `TestLazyCredfileHandler_DoesNotTouchFileAtConstruction`, which asserts the file remains nonexistent after constructing the handler with a missing-file path — i.e. the constructor's only action is to capture the path string.
- Four tests cover lazy-init proof, missing-file recovery hint, happy-path bearer credential resolution, and subprocess help-text smoke; all pass under `-race`.
- The four subcommands inherited from `pkg/cli` (`validate`, `run`, `dev-server`, `test`) are listed by `--help` — the Tier-3 `test` subcommand will be the vehicle 06-07 uses to run `*_test.star` files in the example dir.

## Subcommands Inherited from pkg/cli

`extbin --help` produces the same command tree as `cmd/skytime --help` (the cobra root's `Use:` is "skytime" — pkg/cli owns that string; consumers don't override it):

```
Available Commands:
  completion  Generate the autocompletion script for the specified shell
  dev-server  Spawn a local Temporal dev server (`temporal server start-dev`)
  help        Help about any command
  info        List flows defined in a .star file
  run         Trigger a workflow on a configured Temporal cluster
  test        Run Tier-3 .star tests in <dir>
  validate    Statically validate a .star flow file
```

The four required subcommands (`validate`, `run`, `dev-server`, `test`) are present.

## Lazy-Load Behavior — Verification

```
=== RUN   TestLazyCredfileHandler_DoesNotTouchFileAtConstruction
--- PASS: TestLazyCredfileHandler_DoesNotTouchFileAtConstruction (0.00s)
=== RUN   TestLazyCredfileHandler_FirstResolveSurfacesMissingFile
--- PASS: TestLazyCredfileHandler_FirstResolveSurfacesMissingFile (0.00s)
=== RUN   TestLazyCredfileHandler_HappyPathWithRealFile
--- PASS: TestLazyCredfileHandler_HappyPathWithRealFile (0.00s)
=== RUN   TestExtbin_BuildsAndShowsHelp
--- PASS: TestExtbin_BuildsAndShowsHelp (1.45s)
PASS
ok    github.com/mikelalcon/skytime/examples/http-github-webhook/cmd/extbin    2.844s
```

The first test mechanically proves construction is side-effect-free: `os.IsNotExist(statErr)` after `newLazyCredfileHandler(path)` confirms the constructor never invoked `credfile.New()` (which would have called `os.Stat` against `path` and surfaced an error). The second proves that when Resolve IS called, the user-recovery hint mentions both `SKYTIME_CREDFILE_PATH` and `.skytime-credentials.example` so a confused reader can self-recover without reading source. The third proves the happy path serves a real `*extension.BearerCredential` whose `Token.Reveal()` round-trips. The fourth is a subprocess smoke that go-builds the binary and asserts `--help` lists the four subcommands.

## Task Commits

1. **Task 1: Implement examples/http-github-webhook/cmd/extbin/main.go** — `53c308f` (feat)
2. **Task 2: Smoke test for cmd/extbin — validates startup wiring + lazy-load behavior** — `2bbc3a1` (test)

## Files Created/Modified

- `examples/http-github-webhook/cmd/extbin/main.go` — Custom CLI binary entry-point. Registers `skyhttp.New()`, `skygh.New()`, `skyweb.New()` via `cli.WithExtensions`. Wires `*lazyCredfileHandler` via `cli.WithCredentialHandler`. Reads `SKYTIME_CREDFILE_PATH` env var (empty falls through to credfile's default).
- `examples/http-github-webhook/cmd/extbin/main_test.go` — Four white-box tests in `package main` accessing the unexported `lazyCredfileHandler` symbol. The subprocess smoke is `-short`-skippable; CI (no `-short`) exercises it.

## Decisions Made

- **Lazy credfile resolution over eager construction.** cmd/skytime uses a `noopCredentialHandler`; cmd/extbin instead defers `credfile.New()` to the first `Resolve()` call. Rationale: the headline demo (`extbin run public_repo_check.star`) uses zero credentials, so eager construction would fail with "stat: no such file" before any work runs. Lazy lets the headline demo work without `~/.skytime-credentials` and surfaces the hint only when the user's flow actually wants a credential.
- **No defaultBuildID indirection.** cmd/skytime carries `var defaultBuildID = "dev"` for ldflags injection; extbin does not. The example is built by readers from source; no release-build pipeline is in scope.
- **Field-naming `once_inner` / `once_err`.** Deliberately ugly to flag sync.Once-protected state. Worth callout for downstream code review.
- **lazyCredfileHandler stays in main.go.** Example-tier code; consultants reading `docs/cli-binary.md` should see the entire wiring in one screen.
- **Subprocess test is `-short`-skippable.** `go build` adds ~1.5s; tight inner loops can `-short` past it. CI (per RESEARCH.md § 5 D-CI-STEPS) runs without `-short`, so the smoke fires there.

## Deviations from Plan

None — plan executed exactly as written. All grep acceptance criteria, all `go build`/`go vet`/`go test -race` checks pass on first try.

## Self-Check: PASSED

- `examples/http-github-webhook/cmd/extbin/main.go` exists (verified: 120 lines)
- `examples/http-github-webhook/cmd/extbin/main_test.go` exists (verified: 114 lines)
- Commit `53c308f` exists in git log
- Commit `2bbc3a1` exists in git log
- `go build -o /tmp/extbin ./examples/http-github-webhook/cmd/extbin` exits 0
- `/tmp/extbin --help` lists `validate`, `run`, `dev-server`, `test`
- `go test -race -count=1 ./examples/http-github-webhook/cmd/extbin/...` all 4 tests PASS
- `go build ./...` passes (no regressions in the rest of the tree)

## Issues Encountered

None.

## Next Phase Readiness

- 06-06 (.star flows) can now author against this binary: `extbin run flows/public_repo_check.star --input repo=octocat/Hello-World`. The three extensions are registered; the parser-side imports of `github`, `webhook`, and `http` will resolve.
- 06-07 (Tier-3 `*_test.star` tests) will use this binary's inherited `test` subcommand: `extbin test ./examples/http-github-webhook/`.
- 06-08 (CI workflow) can `go build ./examples/http-github-webhook/cmd/extbin` then run `./extbin test ./examples/http-github-webhook/` as the success-criterion-5 step.
- 06-09 (README) will instruct readers to `go build ./examples/http-github-webhook/cmd/extbin -o ./extbin` then `./extbin run flows/public_repo_check.star`.

No blockers.

---
*Phase: 06-example-project-http-github-webhook*
*Completed: 2026-05-07*
