---
phase: quick-260504-jtr
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - pkg/cli/root.go
  - pkg/cli/root_test.go
  - cmd/skytime/main.go
autonomous: true
requirements: [QUICK-260504-jtr]

must_haves:
  truths:
    - "Running `skytime` with NO args prints the help block (current behavior preserved)"
    - "Running `skytime <unknown-cmd>` prints `Error: unknown command \"<unknown-cmd>\" for \"skytime\"` to stderr and exits non-zero"
    - "Running `skytime <something>.star ...` adds a suggestion line pointing at `skytime run <file>` and/or `skytime validate <file>` BEFORE the usage block"
    - "Running `skytime validate <file>` (and other valid subcommands) continues to work unchanged — validate/run/dev-server/help/completion exit codes and output identical to before"
    - "The renderer/exit-status split (D4-18) is preserved: pkg/cli owns formatting, cmd/skytime owns os.Exit"
  artifacts:
    - path: "pkg/cli/root.go"
      provides: "RenderRootError exported helper + cobra root construction"
      contains: "func RenderRootError"
    - path: "pkg/cli/root_test.go"
      provides: "Regression tests covering bare-invocation, unknown-command, .star-path-as-cmd cases"
      contains: "TestRootCommand_UnknownCommandRendersError"
    - path: "cmd/skytime/main.go"
      provides: "Wiring: route ExecuteContext error through RenderRootError before exit"
      contains: "cli.RenderRootError"
  key_links:
    - from: "cmd/skytime/main.go"
      to: "pkg/cli.RenderRootError"
      via: "direct call when root.ExecuteContext returns non-nil error"
      pattern: "cli\\.RenderRootError"
    - from: "pkg/cli/root.go"
      to: "errSilent sentinel (existing, in render.go)"
      via: "errors.Is check inside RenderRootError to skip already-rendered errors"
      pattern: "errors\\.Is.*errSilent"
---

<objective>
Fix the silent-exit-1 surprise when users invoke `skytime <something-other-than-a-subcommand>` (most commonly `skytime path/to/flow.star ...` because that's the natural shape consultants assume from `python script.py` / `node script.js`). Cobra's `unknown command` error is currently swallowed by `SilenceErrors=true` (D4-18) and `cmd/skytime/main.go` calls `os.Exit(1)` without rendering.

Purpose: A first-time user typing the obvious-but-wrong `skytime examples/skeleton/simple_check.star --flow simple_check --input ...` gets a clear, actionable error pointing them at `skytime run` / `skytime validate`, instead of a silent exit 1 that looks like the binary is broken.

Output: A single new exported helper `cli.RenderRootError(out io.Writer, err error) bool` (returns true if a meaningful render happened) plus the wiring in `cmd/skytime/main.go` and regression tests proving the unknown-command path stays loud forever.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@CLAUDE.md
@.planning/STATE.md
@cmd/skytime/main.go
@pkg/cli/root.go
@pkg/cli/root_test.go
@pkg/cli/render.go
@pkg/cli/validate.go
@pkg/cli/validate_test.go

<interfaces>
<!-- Key existing types and contracts the executor MUST use verbatim. -->
<!-- Extracted from pkg/cli/root.go, pkg/cli/render.go, pkg/cli/validate.go. -->

From pkg/cli/root.go:
```go
// SilenceErrors=true and SilenceUsage=true on root (D4-18 — DO NOT CHANGE).
// NewRootCommand returns (*cobra.Command, error).
func NewRootCommand(opts ...Option) (*cobra.Command, error)
```

From pkg/cli/render.go (PRIVATE — accessed via in-package use, not exported):
```go
// errSilent is the lowercase package-internal sentinel returned by
// validate's RunE / run's RunE on already-rendered failures. The renderer
// has already printed; cobra just needs to exit non-zero.
var errSilent = errors.New("validation failed")

func renderError(out io.Writer, err error, debug bool)        // single error
func renderErrors(out io.Writer, errs []error, debug bool)    // []error variant
```

From cmd/skytime/main.go (current shape):
```go
if err := root.ExecuteContext(ctx); err != nil {
    // pkg/cli's renderer already printed user-visible diagnostics
    // (D4-18 "renderer owns output, cobra owns exit status"); just
    // exit non-zero.
    os.Exit(1)
}
```

Cobra `unknown command` error format (verified in cobra@v1.10.2/args.go:36):
```
unknown command %q for %q%s    // %q is the offending arg, %q is cmd.CommandPath() ("skytime"), %s is the suggestion suffix (or "")
```
This is a plain `fmt.Errorf` — NOT a typed error. Detection MUST be string-prefix match `strings.HasPrefix(err.Error(), "unknown command ")`.
</interfaces>

<bug_repro>
```bash
$ go run ./cmd/skytime examples/skeleton/simple_check.star --flow simple_check --input '{"repo":"octocat/Hello-World"}'
exit status 1
exit=1
```
Stderr is silent. Expected: clear "unknown command" error + suggestion pointing at `skytime run` because the arg ends in `.star`.

Bare invocation behavior (already correct, preserve it):
```bash
$ go run ./cmd/skytime
Starlark-defined durable workflows on Temporal

Usage:
  skytime [command]

Available Commands:
  ...
exit=0
```
This is cobra's default `Help()` behavior — the constraint hint confirms "the --no-args case might already be handled by cobra's default Help() behavior; if so, the plan focuses on the unknown-command-with-suggestion case only". Verified: it is. Do NOT change bare-invocation behavior.
</bug_repro>
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Render unknown-command errors with .star path suggestion + wire into main</name>
  <files>pkg/cli/root.go, pkg/cli/root_test.go, cmd/skytime/main.go</files>
  <behavior>
    Three regression tests in pkg/cli/root_test.go (one Test func each, table-driven where natural):

    1. **TestRootCommand_BareInvocationPrintsHelp** — `root.SetArgs([]string{})` + `ExecuteContext`:
       - Expect `err == nil` (cobra prints help, exits 0; this is the *preserved* current behavior, so the test pins it as a regression guard).
       - stdout MUST contain `"Available Commands:"` and `"validate"` and `"run"` and `"dev-server"`.
       - stderr MUST be empty (help goes to stdout per cobra default).

    2. **TestRootCommand_UnknownCommandRendersError** — `root.SetArgs([]string{"nonexistent"})` + execute, then route the returned error through `cli.RenderRootError(stderr, err)`:
       - Expect `err != nil` from ExecuteContext (cobra returns the unknown-command error since SilenceErrors=true means cobra didn't print but DID return).
       - `RenderRootError` returns `true` (it recognized the pattern and rendered).
       - stderr MUST contain `Error: unknown command "nonexistent" for "skytime"`.
       - stderr MUST contain `Usage:` and `Available Commands:` (the cobra usage block, surfaced via `root.UsageString()` — note Usage block goes to stderr here, distinct from bare-invocation's stdout help).
       - stderr MUST NOT contain `did you mean` (no `.star` suffix → no path suggestion line).

    3. **TestRootCommand_UnknownCommandStarFileSuggestsRun** — `root.SetArgs([]string{"examples/foo.star", "--flow", "x"})` + execute, route through `RenderRootError`:
       - Expect `err != nil` (cobra error: `--flow` is unknown to root, but the FIRST positional arg "examples/foo.star" is what cobra's `unknown command` reports).
       - stderr MUST contain `Error: unknown command "examples/foo.star" for "skytime"`.
       - stderr MUST contain `did you mean` AND `skytime run examples/foo.star` AND `skytime validate examples/foo.star` (both suggestions, since either is plausible).
       - stderr MUST contain `Usage:` (usage block still appears AFTER the suggestion line).

    4. **TestRootCommand_RenderRootErrorRespectsSilentSentinel** — verify `RenderRootError` does NOT double-print when called on an already-rendered error:
       - Construct an error chain via `fmt.Errorf("validation failed: %w", errSilent_proxy)` where `errSilent_proxy` is the SAME sentinel re-exported. Since `errSilent` is package-private, this test must live INSIDE `pkg/cli` (not `pkg/cli_test`) so it can reference the unexported sentinel — OR alternatively, `RenderRootError` exposes its skip behavior via a separately exported `cli.ErrSilent = errSilent` sentinel and the test asserts `errors.Is(rendered, cli.ErrSilent)` returns true and stderr stays empty. **Use the exported-alias approach** to keep the test in `cli_test` like the other root_test.go tests; export a NEW symbol `cli.ErrAlreadyRendered = errSilent` (alias) so external callers (including main.go) can compose without leaking the lowercase name.
       - With `err = cli.ErrAlreadyRendered`: `RenderRootError(stderr, err)` returns `false` (nothing to render); stderr empty.
       - With `err = fmt.Errorf("wrap: %w", cli.ErrAlreadyRendered)`: same — `errors.Is` finds it through the chain, returns `false`, stderr empty.

    5. **TestRootCommand_ValidSubcommandUnaffected** — pin that adding `RenderRootError` did not break the existing `validate` happy path. Re-run a minimal `validate` happy-path scenario (write a tiny valid .star to `t.TempDir()`, run, expect nil err, empty stderr). This is a smaller dup of TestValidateCmd_HappyPath — keep it because the implementation touches root.go and we want a proximate regression signal in root_test.go.
  </behavior>
  <action>
    Implement the three concerns atomically:

    **Step 1 — RED tests first (TDD).** Add the five tests above to `pkg/cli/root_test.go` (extending the existing file). They will fail to compile until the new symbols exist. Imports needed: `bytes`, `context`, `errors`, `os`, `path/filepath`, `strings`, `testing`, `github.com/stretchr/testify/require`, `github.com/mikelalcon/skytime/pkg/cli`. Helper: a small `func executeRootCapture(t *testing.T, args []string) (stdout, stderr bytes.Buffer, err error)` that builds the root, sets buffers, sets args, calls `ExecuteContext(context.Background())`, returns. Reuse existing test patterns from `validate_test.go` (lines 24-33).

    **Step 2 — Implement `pkg/cli/root.go`.** Append (do NOT modify the existing `NewRootCommand`):

    ```go
    // ErrAlreadyRendered is an exported alias of the package-private
    // errSilent sentinel. RunE handlers in pkg/cli return errSilent when
    // they have already written user-visible output to stderr; external
    // callers (cmd/skytime/main.go) test for this via errors.Is to skip
    // re-rendering. Exporting the alias keeps the lowercase name stable
    // for in-package use while giving cmd/skytime a stable handle.
    var ErrAlreadyRendered = errSilent

    // RenderRootError formats a top-level cobra error to out with a
    // human-friendly message + (when applicable) a Skytime-shaped
    // suggestion + the cobra usage block. Returns true iff something
    // was written.
    //
    // Behavior:
    //   - err == nil → returns false, writes nothing.
    //   - errors.Is(err, ErrAlreadyRendered) → returns false, writes
    //     nothing. The subcommand's RunE already printed its diagnostic
    //     via render.go; main.go just needs the non-zero exit.
    //   - err.Error() starts with "unknown command " → renders
    //         Error: <err.Error()>
    //         <BLANK LINE>
    //         [optional] did you mean:
    //                      skytime run <arg>
    //                      skytime validate <arg>
    //         <BLANK LINE>
    //         <root.UsageString()>
    //     The "did you mean" block fires when the offending arg ends in
    //     ".star" (case-insensitive). Extract the arg by parsing the
    //     fmt.Errorf format: `unknown command "X" for "skytime"...` —
    //     the first quoted token is X.
    //   - any other error → renders just `Error: <err.Error()>` + usage
    //     block. Defensive default for future cobra error shapes
    //     (e.g., "requires at least N args" if root ever gets RunE).
    //
    // Implementation MUST NOT take a *cobra.Command parameter — keeping
    // the signature (io.Writer, error) makes it trivially testable
    // without spinning up a root. The usage block is obtained by
    // building a fresh root via NewRootCommand (no opts) and calling
    // .UsageString(). This is cheap; avoids an API where main.go has
    // to thread the live root in.
    func RenderRootError(out io.Writer, err error) bool {
        if err == nil {
            return false
        }
        if errors.Is(err, ErrAlreadyRendered) {
            return false
        }

        msg := err.Error()
        fmt.Fprintf(out, "Error: %s\n", msg)

        // Detect "unknown command "<arg>" for "skytime"..." → optional
        // .star-path suggestion.
        if strings.HasPrefix(msg, "unknown command ") {
            arg := extractFirstQuoted(msg)
            if arg != "" && strings.HasSuffix(strings.ToLower(arg), ".star") {
                fmt.Fprintln(out, "")
                fmt.Fprintln(out, "did you mean:")
                fmt.Fprintf(out, "  skytime run %s\n", arg)
                fmt.Fprintf(out, "  skytime validate %s\n", arg)
            }
        }

        // Append usage block (cobra usage goes through SetUsageTemplate
        // by default; UsageString respects whatever the root was built
        // with). Build a fresh, optionless root for the template — this
        // is sufficient because the usage block is identical regardless
        // of which Options the original was constructed with (Options
        // affect handler/extension wiring, not the cobra command tree).
        fmt.Fprintln(out, "")
        if root, rootErr := NewRootCommand(); rootErr == nil {
            fmt.Fprint(out, root.UsageString())
        }
        return true
    }

    // extractFirstQuoted returns the first %q-style "..."-quoted
    // substring in s, unquoted. Returns "" if no quoted token is found.
    // Used to recover the offending arg from cobra's
    // `unknown command %q for %q...` error string. Strconv.Unquote
    // handles the standard %q escape rules.
    func extractFirstQuoted(s string) string {
        i := strings.IndexByte(s, '"')
        if i < 0 {
            return ""
        }
        // Find the closing quote, respecting backslash escapes.
        rest := s[i:]
        for j := 1; j < len(rest); j++ {
            if rest[j] == '\\' && j+1 < len(rest) {
                j++ // skip escaped char
                continue
            }
            if rest[j] == '"' {
                if v, err := strconv.Unquote(rest[:j+1]); err == nil {
                    return v
                }
                return ""
            }
        }
        return ""
    }
    ```

    Add imports to root.go: `errors`, `fmt`, `io`, `strconv`, `strings`. Existing imports (`github.com/spf13/cobra`) stay.

    **Step 3 — Wire `cmd/skytime/main.go`.** Replace the bare `os.Exit(1)` block:

    ```go
    if err := root.ExecuteContext(ctx); err != nil {
        cli.RenderRootError(os.Stderr, err)
        os.Exit(1)
    }
    ```

    The existing comment about D4-18 should be UPDATED (not deleted) to reflect the new wiring:

    ```go
    if err := root.ExecuteContext(ctx); err != nil {
        // Per D4-18, pkg/cli owns user-visible output. Subcommands
        // that already rendered (validate, run) return cli.ErrAlreadyRendered
        // and RenderRootError no-ops on it. Top-level cobra errors
        // (unknown command, etc.) reach RenderRootError as plain
        // errors and get a human-friendly stderr render here.
        cli.RenderRootError(os.Stderr, err)
        os.Exit(1)
    }
    ```

    **Step 4 — Run tests, confirm GREEN.**

    Edge cases to verify don't break:
    - `skytime --help` still works (cobra short-circuits, no error returned).
    - `skytime help validate` still works.
    - `skytime completion bash` still works (cobra-built-in subcommand).
    - `skytime validate /nonexistent.star` still returns the validate-side render path (NOT root unknown-command) — `validate` is recognized; the file-not-found goes through the validator's normal error flow.

    **What NOT to do:**
    - Do NOT flip `SilenceErrors` or `SilenceUsage` to false — that would re-introduce cobra's default error printing and conflict with D4-18 (locked decision; would silently change validate/run error formatting).
    - Do NOT add a `RunE` to the root command — bare invocation already prints help via cobra's default-when-no-RunE-and-no-subcommand behavior; adding RunE breaks that.
    - Do NOT special-case `--flow` / `--input` flags in `RenderRootError` — those are run-subcommand flags. The user error mode is "missed the run subcommand entirely", and the .star suggestion implicitly resolves the flag confusion (`skytime run <file> --flow x --input ...` is the corrected form).
    - Do NOT export `errSilent` directly (rename it). The alias `ErrAlreadyRendered` is forward-compatible and avoids touching every internal call site.
  </action>
  <verify>
    <automated>cd /Users/mikel/dev/ai/temporero && go test ./pkg/cli/... -run 'TestRootCommand' -count=1 -race && go test ./pkg/cli/... -count=1 -race && go vet ./... && go build ./cmd/skytime && go run ./cmd/skytime examples/skeleton/simple_check.star 2>&1 | grep -E 'unknown command|did you mean|skytime run' && echo VERIFY_OK</automated>
  </verify>
  <done>
    - `go test ./pkg/cli/... -count=1 -race` passes including the 5 new TestRootCommand_* tests.
    - `go vet ./...` clean.
    - `go build ./cmd/skytime` succeeds.
    - Manual repro `go run ./cmd/skytime examples/skeleton/simple_check.star --flow x` exits 1 AND stderr contains `Error: unknown command`, `did you mean:`, `skytime run examples/skeleton/simple_check.star`, `skytime validate examples/skeleton/simple_check.star`, and the cobra `Usage:` block.
    - Bare `go run ./cmd/skytime` continues to exit 0 with the help block on stdout (regression-pinned by TestRootCommand_BareInvocationPrintsHelp).
    - `go run ./cmd/skytime validate <valid.star>` still exits 0 with empty stderr (no double-render from RenderRootError).
    - `go run ./cmd/skytime validate <bad.star>` still emits the existing typed-dag-error render via render.go AND exits non-zero (RenderRootError no-ops on `ErrAlreadyRendered`).
  </done>
</task>

</tasks>

<verification>
Run the verify command above. Specifically prove:

1. **No subcommand → help (preserved):** `go run ./cmd/skytime` → exit 0, stdout has `Available Commands:`.
2. **Unknown subcommand → loud error:** `go run ./cmd/skytime nonexistent` → exit 1, stderr has `Error: unknown command "nonexistent" for "skytime"` + usage block.
3. **`.star` path → suggestion:** `go run ./cmd/skytime examples/skeleton/simple_check.star` → exit 1, stderr has `did you mean:` + `skytime run examples/skeleton/simple_check.star` + `skytime validate examples/skeleton/simple_check.star`.
4. **Existing surfaces unchanged:** validate/run/dev-server/help/completion paths exit codes + stderr/stdout contents identical to pre-change (proven by full `go test ./pkg/cli/... -count=1` GREEN).
5. **Firewall preserved:** `go test ./tests/... -count=1` (firewall_cli_test.go) GREEN — pkg/cli's new code adds only stdlib imports (errors, fmt, io, strconv, strings) plus existing cobra; cmd/skytime gains no new imports beyond `cli` itself.

</verification>

<success_criteria>
- The five new TestRootCommand_* regression tests are committed and passing.
- The repro command from the bug report (`go run ./cmd/skytime examples/skeleton/simple_check.star --flow simple_check --input ...`) prints a clear, actionable error pointing the user at `skytime run`/`skytime validate`, then exits 1.
- D4-18 architectural invariant (renderer owns output, cobra owns exit) is preserved — no flip of SilenceErrors/SilenceUsage; main.go remains the single os.Exit site; the new helper composes with the existing `errSilent` flow via the exported alias.
- Existing CLI surfaces (validate, run, dev-server, help, completion) are byte-for-byte identical in their error/output streams (verified by the full test suite passing).
</success_criteria>

<output>
After completion, create `.planning/quick/260504-jtr-make-root-skytime-command-print-proper-e/260504-jtr-SUMMARY.md` with the standard summary template.
</output>
