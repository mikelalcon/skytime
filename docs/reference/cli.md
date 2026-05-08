# Skytime CLI Reference

The `skytime` binary is the entry point for static validation, workflow dispatch, flow discovery, long-running worker hosting, and local Temporal dev server lifecycle. It lives at `cmd/skytime/main.go` (a thin wrapper) and `pkg/cli/` (the reusable cobra root, callable from custom binaries — see [docs/cli-binary.md](../cli-binary.md)).

**Library-side firewall (D4-13):** cobra, `charm.land/log/v2`, and `charm.land/lipgloss/v2` are reachable only from `pkg/cli` and `cmd/skytime` (enforced by `tests/firewall_cli_test.go`). Adding flags or subcommands means editing `pkg/cli/`; the rest of the library stays cobra-unaware.

**Source-of-truth:** every behavior described below cites a file under `pkg/cli/` so you can jump straight to code. Each subcommand's `Use:` and `Short:` strings are pulled verbatim from the cobra command struct, so what you read here matches `skytime <cmd> --help`.

> **Want auto-completion / man-page generation?** Future phase — for now this is hand-written reference (D-16). The companion [Starlark builtins reference](builtins.md) ships from `cmd/skytime-docgen` and stays in lockstep with `pkg/parser/builtins.go`. To discover what flows a specific `.star` file declares, run `skytime info <file>` (the runtime equivalent of the auto-generated reference).

> **Note on exit codes:** v1 uses exit `1` for *any* error, including cobra usage errors (bad arg count, unknown flag). The conventional Unix `exit 2 → usage` distinction is collapsed into `1` because `cmd/skytime/main.go` returns a blanket `os.Exit(1)` for every error returned from `root.ExecuteContext`. Cobra still renders its usage banner on bad arg counts, so the user-facing diagnostic is unaffected — only the numeric exit code differs from convention. Differentiating `2` is tracked as a v2 polish item.

---

## Persistent Flags

Persistent flags are declared on the root command and inherited by every subcommand. Three resolution sources, in priority order:

1. **Command-line flag** — `skytime --address temporal.acme.com:7233 run flow.star --flow=approve_pr`. Always wins.
2. **Environment variable** — `SKYTIME_TEMPORAL_*`. Applied only when the corresponding flag was NOT supplied on the command line (cobra's `Flag().Changed` is false).
3. **Default** — empty string for connection flags; `false` for booleans.

The env-var fallback is implemented as a table-driven loop in `pkg/cli/flags.go::bindEnvVars` (one row per flag); env vars only fill in *unset* flags, so `--address ""` would NOT pick up `SKYTIME_TEMPORAL_ADDRESS` (Changed=true wins, even with an empty value).

| Flag | Env Var | Default | Purpose |
|------|---------|---------|---------|
| `--debug` | — | `false` | Reveal Go internals in error output (D4-19); the only path past Starlark-first error rendering (D4-18) |
| `--verbose` | — | `false` | Show Temporal SDK INFO/DEBUG logs alongside Skytime progress; also forces static line-per-event renderer instead of the live block (CLI-07) |
| `--address` | `SKYTIME_TEMPORAL_ADDRESS` | `""` | Temporal frontend host:port (e.g., `localhost:7233`, `your-ns.tmprl.cloud:7233`) |
| `--namespace` | `SKYTIME_TEMPORAL_NAMESPACE` | `""` | Temporal namespace |
| `--api-key` | `SKYTIME_TEMPORAL_API_KEY` | `""` | Temporal Cloud API key. Setting this routes connection through `NewCloudClient` (TLS implied per SDK v1.39+) |
| `--client-cert` | `SKYTIME_TEMPORAL_CLIENT_CERT` | `""` | mTLS client certificate path (self-hosted clusters); must be paired with `--client-key` |
| `--client-key` | `SKYTIME_TEMPORAL_CLIENT_KEY` | `""` | mTLS client private-key path; must be paired with `--client-cert` |
| `--server-ca` | `SKYTIME_TEMPORAL_SERVER_CA` | `""` | mTLS server CA bundle (PEM). Optional — used only with the `--client-cert + --client-key` pair to validate the Temporal frontend's certificate against a custom root |

Source: `pkg/cli/flags.go` (registration + env-var binding table).

`--debug` and `--verbose` orthogonally widen *what is shown*: `--debug` adds Go internal causes to error output, `--verbose` reveals SDK-side logs and disables the live block. They can be combined.

---

## skytime run

### Synopsis

    skytime run <file.star> --flow <name> [--input <json>] [persistent flags]

Source: `pkg/cli/run.go`.

### Motivation

`skytime run` is the single command that takes a `.star` file from your laptop to a workflow execution on a Temporal cluster. It statically validates first (D4-07: validation failures NEVER reach a partially-started workflow), connects via the appropriate variant (`--api-key` → cloud, mTLS pair → self-hosted, otherwise → dev), starts an embedded transient worker against the file's directory, dispatches the workflow, and streams progress back to your terminal — Bazel-style multi-line live block by default, static line-per-event under `--verbose`, non-TTY, or Windows (CLI-06, CLI-07; D4.1-17..21).

For production deployments where the worker runs as a long-lived daemon (not embedded in `skytime run`), import `pkg/worker` and write your own service binary. See [docs/cli-binary.md](../cli-binary.md) for the custom-CLI pattern that registers your extensions through `cli.WithExtensions(...)` and `cli.WithCredentialHandler(...)`.

The eight-step recipe (per the `RunE` body in `pkg/cli/run.go`):

1. Static validate via `pkg/validator.Validate` (same source-of-truth as `skytime validate`).
2. Parse `--input` as JSON into `map[string]any`.
3. Connect to Temporal — variant routing per `pkg/cli/connect.go`.
4. Boot an embedded transient `pkg/worker.Worker` against `filepath.Dir(file)` as `RootDir`.
5. Resolve the flow's `content_hash` from the worker's frozen registry.
6. `client.ExecuteWorkflow` with `WorkflowInput{FlowName, ContentHash, InitState}`.
7. Block on `run.Get(ctx, &result)`.
8. Print the result as JSON on stdout.

### Flags

| Flag | Default | Purpose |
|------|---------|---------|
| `--flow` | (required) | Name of the flow to execute (must match a `flow(name=...)` declaration in the file) |
| `--input` | `{}` | JSON-encoded inputs map, parsed against the flow's `inputs={...}` schema |

Plus all persistent flags above. The connection variant is selected automatically (source: `pkg/cli/connect.go::connectClientWithFactory`):

- `--api-key` set → Temporal Cloud via `worker.NewCloudClient` (TLS implied per SDK v1.39+).
- `--client-cert` AND `--client-key` set → self-hosted via `worker.NewSelfHostedClient` (mTLS); `--server-ca` optionally adds a custom root.
- Only one of `--client-cert` / `--client-key` set → friendly error before any client construction (`--client-cert and --client-key must be supplied together for mTLS`).
- Otherwise → local-dev (dev-temporal) connection via `worker.NewDevClient` (`TLSDisabled=true`).

The transient embedded worker uses the same `pkg/cli` `--rootdir`-style convention as `skytime validate`: the file's directory becomes the worker's frozen registry root. For multi-file flows that `load(...)` siblings, place them in (or below) the same directory as `<file.star>`.

### Exit Codes

- `0` — workflow completed successfully; result printed as JSON on stdout.
- `1` — any error including parse error, validation error, missing `--flow` target, malformed `--input` JSON, connect failure, worker init/start failure, workflow failed via top-level `fail(...)` or non-retryable activity error, AND usage errors (bad arg count, missing required flag) per the top-of-doc note. The renderer (`pkg/cli/render.go::renderError`) emits the Starlark-first message; `--debug` adds the Go cause chain underneath (D4-18, D4-19). Cobra surfaces its own usage banner on argument-validation failures before `RunE` runs.
- `130` — NOT used in v1: Ctrl-C during `run.Get` exits with `1` and prints `interrupted; workflow continues on Temporal as runID=<X>`. The workflow itself keeps executing on the cluster.

### Example

```
skytime run examples/skeleton/expression_if.star \
    --flow check_user --input '{"user_id":"42"}'
```

Renders as a multi-line live progress block (cursor-up + line-clear ANSI; preserves scrollback) while the workflow runs; collapses to a final summary line at completion. Pipe through `tee` or redirect stderr to disable the live block (the renderer auto-falls-back to static line-per-event mode when stdout is non-TTY).

See [docs/getting-started.md](../getting-started.md) for the canonical walkthrough using this exact command.

### See Also

- Builtin reference: [`docs/reference/builtins.md`](builtins.md) — lookup syntax for `flow`, `step`, `script`, `if_cond`, `result`, `fail`, `for_each_parallel`, `call_flow`.
- Architecture: [`docs/architecture.md`](../architecture.md) — parse/execute split, lambda capture, deterministic re-parse on workflow start.
- Custom CLI: [`docs/cli-binary.md`](../cli-binary.md) — building binaries with your own extensions and credential handler.
- Discovery: [`skytime info`](#skytime-info) — what flows does this `.star` file declare?

---

## skytime validate

### Synopsis

    skytime validate <file.star> [persistent flags]

Source: `pkg/cli/validate.go` (RunE) + `pkg/validator` (the actual checks).

### Motivation

`skytime validate` is a dry-run static check: it parses the file, runs every validation rule the runtime would run (lambda free-vars-reference-state, schema cross-validate, expression-mode `if_cond` per-key type equality, `${ctx.expr}` typo detection, mixed-idempotency block detection), and exits with a structured error list — without contacting Temporal, without any I/O.

Use it in CI on every `.star` PR. VAL-02 pins a differential test (`tests/differential_test.go`) that runs every `examples/skeleton/*.star` through both static `Validate` and a dry-run interpreter (mock `OperationDispatch`) and asserts they agree on accept/reject; this means *static validation cannot drift from runtime parsing* — both share `pkg/parser`.

The renderer is shared with `skytime run` and `skytime info` (`pkg/cli/render.go::renderError + renderErrors`), so error format is identical across all three subcommands. Unknown-extension errors get an additional hint pointing at [docs/cli-binary.md](../cli-binary.md) (D4-16).

### Flags

No subcommand-specific flags. All persistent flags apply.

### Exit Codes

- `0` — no validation issues found.
- `1` — one or more `*dag.ParseError` or `*dag.ValidationError` rendered to stderr, OR a usage error (bad arg count) per the top-of-doc note. Format (VAL-03): `<file>:<line>:<col> [flow > step > action]: <msg>`. The bracketed segments appear only when at least one of flow/step/action is non-empty, mirroring the legacy `<file>:<line>:<col>: <msg>` format for low-level parse errors that lack flow/step/action context.

`--debug` walks the Go `Unwrap` chain underneath each rendered error (D4-19); without it, only the typed dag-error message prints (D4-18).

### Example

```
skytime validate examples/skeleton/expression_if.star
```

Errors typically look like:

```
examples/skeleton/expression_if.star:14:9 [check_user > assess]: ctx.user_id_typo: state has no key 'user_id_typo' at lambda position
```

Empty-batch warnings, unknown-extension errors, mixed-idempotency rejections, and per-key type mismatches in expression-mode `if_cond` all flow through the same renderer.

### See Also

- Builtin reference: [`docs/reference/builtins.md`](builtins.md) — what each builtin requires (so you know what `validate` is checking against).
- Architecture: [`docs/architecture.md`](../architecture.md) — why static validation and runtime parsing share the same code path (the `pkg/validator` thin facade + `parser/finalize.go` actual checks).
- Custom CLI: [`docs/cli-binary.md`](../cli-binary.md) — register extensions before validating flows that reference them.

---

## skytime info

### Synopsis

    skytime info <file.star> [persistent flags]

Source: `pkg/cli/info.go`.

### Motivation

Discoverability: list every flow defined in the file with its description and inputs schema. Run it *before* `skytime run` so you know what `--flow` value to pass and what shape `--input` should be.

The output is a 3-column bordered Unicode-box-drawing table (rounded corners; `charm.land/lipgloss/v2/table`):

| Column | Source |
|--------|--------|
| `Flow` | `flow(name=...)` declaration name |
| `Description` | `flow(description="...")` kwarg, or em-dash (U+2014) if empty |
| `Inputs` | `flow(inputs={...})` rendered as `key:type, key:type` with keys alphabetized; em-dash if empty |

Rows appear in source-declaration order (NOT alphabetical) so `info` output mirrors the file layout. Inputs *keys* are alphabetized within each row so output is byte-stable across invocations (Go map iteration is randomized).

`info` is parse-time only — no Temporal connection, no `CredentialHandler` required. It uses the same `parser.NewParser` + `ParseFile` path as `validate` and `run`, so an extension referenced by the file must be registered (via `cli.WithExtensions`); otherwise the parse fails with the unknown-extension hint per D4-16.

### Flags

No subcommand-specific flags. All persistent flags apply (though `--api-key`, `--client-cert`, etc., are unused at parse time — `info` opens no network connections).

### Exit Codes

- `0` — table printed to stdout.
- `1` — parse error rendered to stderr via the shared renderer (`pkg/cli/render.go::renderError`), OR a usage error (bad arg count) per the top-of-doc note; no partial table on stdout.

### Example

```
$ skytime info examples/skeleton/expression_if.star
╭────────────────┬──────────────────────────────┬──────────────────╮
│ Flow           │ Description                  │ Inputs           │
├────────────────┼──────────────────────────────┼──────────────────┤
│ check_user     │ Look up the user, decide …   │ user_id:string   │
│ approve_pr     │ —                            │ pr_id:int        │
│ notify_team    │ Post an approval Slack DM    │ —                │
╰────────────────┴──────────────────────────────┴──────────────────╯
```

(Output styled with bold header on TTY; `termenv` auto-suppresses ANSI when stdout is piped.)

### See Also

- Builtin reference: [`docs/reference/builtins.md`](builtins.md) — once you know which flow to run, look up the syntax of the steps inside.
- Tutorial: [`docs/getting-started.md`](../getting-started.md) — uses `info` to introduce the example fixture before running it.

---

## skytime test

### Synopsis

    skytime test <dir> [--run <regex>] [--format human|json] [persistent flags]

Source: `pkg/cli/test.go` (`RunE`) → `pkg/testing.RunCLI` (the Tier-3 driver).

### Motivation

`skytime test <dir>` runs `.star`-defined Tier-3 tests (TEST-01..05) — the end-to-end harness that mocks the single generic `ExecuteBatch` activity inside `testsuite.TestWorkflowEnvironment` and routes per-action calls back to Starlark mock lambdas. Each test runs twice (D5-D1 always-on replay) and any divergence in Temporal event history fails the test with a Starlark-callsite-aware diff (D5-D2).

`skytime test` is the *runner-level* equivalent of `pkgtesting.Run(t, dir)` — the Go-level foundation API that imports cleanly into your example project's `*_test.go`. Use the CLI for ad-hoc + CI runs; use `pkgtesting.Run` when you want Go-side `*testing.T` integration (e.g., `go test ./examples/http-github-slack/...`).

For the per-flow-author surface (`tester.workflow`, `tester.mock_action`, `tester.run`, `assert.*`), see [`docs/for-flow-authors/testing.md`](../for-flow-authors/testing.md).

`<dir>` is walked recursively (`filepath.WalkDir`); only files matching `*_test.star` are picked up (D5-A2). A single-file path (`foo_test.star`) is also accepted.

### Flags

| Flag | Default | Purpose |
|------|---------|---------|
| `--run <regex>` | (empty — run all) | Filter tests by Go-regex against `<file_basename_without_ext>.<test_name>` (D5-E3). E.g. `^users_test\.test_existing` matches `users_test.test_existing_user`. Bad patterns surface at option-parse time wrapping `pkg/testing.ErrBadFilter`. |
| `--format <human\|json>` | `human` | Output format. `human` (default) is static line-per-test Go-test-style (D5-E1: `--- PASS: <test> (<elapsed:.2f>s)`). `json` mirrors stdlib `cmd/test2json` schema (D5-E2) — one JSON record per line, compatible with `gotestsum`, `tparse`, GitHub Actions test annotations. Unknown values fail loudly at option-parse time. |

Plus all persistent flags. Notably:

- `--debug` — when set, RunE writes a brief `skytime test: N failed, M passed` diagnostic to stderr after the human output. Default human format renders Starlark callsites only (CLI-03 explicit: NO Go stack traces in default output).

### Exit Codes

- `0` — every test passed (or no `*_test.star` files found under `<dir>`; an advisory line is printed).
- `1` — one or more tests failed; failures rendered to stdout as `--- FAIL:` lines with indented Starlark assertion detail. Also returned for option-time errors (bad `--run` regex, unknown `--format` value at the runner layer) AND for usage errors (bad arg count) per the top-of-doc note. Cobra surfaces its own usage banner before `RunE` runs.

### Example

```
skytime test examples/http-github-slack/
```

Output (default human format):

```
--- PASS: test_existing_user (0.04s)
--- FAIL: test_default_user (0.03s)
    assertion failed at users_test.star:31:5
      expected: "octocat"
      got:      "default-user"
--- PASS: test_create_issue (0.06s)
PASS  users_test.star  3 tests  1 failed (0.13s)
FAIL  1 files  3 tests  1 failed  (0.13s)
```

JSON format (`--format=json`) emits one record per event:

```json
{"Time":"2026-05-05T10:00:00.123456789Z","Action":"start","Package":"users_test.star","Test":"test_existing_user"}
{"Time":"2026-05-05T10:00:00.124000000Z","Action":"run","Package":"users_test.star","Test":"test_existing_user"}
{"Time":"2026-05-05T10:00:00.164000000Z","Action":"pass","Package":"users_test.star","Test":"test_existing_user","Elapsed":0.04}
```

Filtering with `--run`:

```
skytime test examples/http-github-slack/ --run '^users_test\.test_existing'
```

### See Also

- Flow-author guide: [`docs/for-flow-authors/testing.md`](../for-flow-authors/testing.md) — `tester.workflow`, `tester.mock_action`, `tester.run`, `assert.*`, `*_test.star` convention. (Manual reference; `tester.*` is NOT in the auto-generated [`docs/reference/builtins.md`](builtins.md) per Plan 06 deviation D5-docs-builtins-marker-location.)
- Production builtin reference: [`docs/reference/builtins.md`](builtins.md) — production-only auto-generated reference (`flow`, `step`, `if_cond`, ...).
- Architecture: [`docs/architecture.md`](../architecture.md) — how the harness intercepts `ExecuteBatch` inside `testsuite.TestWorkflowEnvironment` and routes per-action calls back to Starlark mocks.
- Production execution: [`skytime run`](#skytime-run) — the production sibling that targets a real Temporal cluster instead of the in-process test environment.

---

## skytime server

### Synopsis

    skytime server --rootdir <dir> [--task-queue <name>] [--addr <addr>]
                   [--credfile <path>] [--drain-timeout <duration>]
                   [--json-log] [persistent flags]

Source: `pkg/cli/server.go`. Phase 7 ships SERVER-01..03: the long-running worker shell with two-signal drain escalation. Phase 7.1+ extends `--addr` to mount the HTTP webhook receiver on top of the same skeleton.

### Motivation

`skytime server` is the *production sibling* of `skytime run`. Where `run` is a one-shot embedded transient worker (validate → connect → dispatch → wait → exit), `server` boots the same `pkg/worker.Worker` and stays up — picking up workflow tasks from the configured task queue until SIGTERM. The drain semantics match Kubernetes' `terminationGracePeriodSeconds` convention so the binary plugs into a Deployment/StatefulSet without custom shutdown plumbing.

### Flags

| Flag | Type | Default | Purpose |
|------|------|---------|---------|
| `--rootdir` | string | (required) | Directory containing `.star` files. Walked recursively at boot; the resulting `FlowRegistry` and `TriggerRegistry` are frozen — runtime mutation is rejected. |
| `--task-queue` | string | `skytime` | Temporal task queue the worker polls. |
| `--addr` | string | `:8080` | HTTP listener address. Accepted in Phase 7 but unused; emits a warning if set explicitly. Phase 7.1+ mounts the webhook receiver here. |
| `--credfile` | string | `""` | Credential file path. Rejected with a friendly error if the binary has no `cli.WithCredentialHandler` wired (D-07-19). |
| `--drain-timeout` | duration | `30s` | Max time to wait for in-flight workflows to complete on SIGTERM/SIGINT. Range-validated to `[1s, 1h]`; sub-1s rejected before any side effect. Values above 1h emit a warning but are accepted. Default `30s` matches Kubernetes' `terminationGracePeriodSeconds` default. |
| `--json-log` | bool | `false` | Switch the slog handler from charm-log (Bazel-style) to `slog.NewJSONHandler(os.Stderr, ...)`. Production deployments typically set this for log aggregator ingestion. |

The connection variant is selected automatically via the same `pkg/cli/connect.go::connectClient` path used by `skytime run` — `--api-key` → cloud, `--client-cert + --client-key` → self-hosted (mTLS), otherwise → local-dev (dev-temporal) connection.

### Drain Semantics (D-07-17, D-07-20)

Two-signal escalation:

1. **First signal (SIGINT or SIGTERM).** Logs `server draining; second SIGINT/SIGTERM forces immediate exit`, then calls `worker.Stop()` in a goroutine. SDK `Stop` blocks up to `WorkerStopTimeout` (= `--drain-timeout`) waiting for in-flight workflow tasks to complete. On clean drain → exit 0.
2. **Second signal during drain.** Logs `drain interrupted by second signal; forcing exit (workflows resume on next worker start from event history)` and calls `os.Exit(1)`. Workflows resume from Temporal's event history when a fresh worker comes up — durability guarantees survive the forced exit.
3. **Drain timeout expiry.** If `--drain-timeout` elapses before `worker.Stop()` returns, logs `drain-timeout exceeded; restart resumes from event history` and exits 1.

The signal channel is buffered (size 2) so the second signal can land even if the receiver is between selects. `signal.Notify` is used (NOT `signal.NotifyContext`): the latter is single-shot and would silently drop the second signal mid-drain.

### Startup Banner (SERVER-03)

Three slog records emitted before `worker.Start()` so operators see what's about to come online:

```
{"level":"INFO","msg":"starting server","rootdir":"./flows","task-queue":"skytime","addr":":8080"}
{"level":"INFO","msg":"registered flows","count":3,"flows":["batch_label_issues","public_repo_check","weekly_digest"]}
{"level":"INFO","msg":"registered triggers","count":2,"triggers":[{"source":"github_webhook","flow":"on_pr"},{"source":"cron","flow":"weekly_digest"}]}
```

Flow names sorted alphabetically; triggers sorted by `(Source.Kind, FlowName, Pos)` via `Worker.Triggers().All()` (Plan 04's `TriggerRegistry.Freeze`).

### Exit Codes

- `0` — clean drain on first signal; in-flight workflows completed within `--drain-timeout`.
- `1` — connect failure, worker init failure, drain-timeout expiry, second-signal forced exit, range-validation failure on `--drain-timeout`, or `--credfile` set without a credential handler. Renderer emits the diagnostic to stderr; cobra's usage banner accompanies argument-validation failures.

### Example

```sh
# Long-lived worker against the local Temporal dev server (terminal 1: skytime dev-temporal)
skytime server --rootdir=./flows --task-queue=demo --address=localhost:7233

# Production: Temporal Cloud
skytime server --rootdir=./flows --task-queue=prod \
    --address=your-ns.tmprl.cloud:7233 \
    --api-key=$TEMPORAL_API_KEY \
    --namespace=your-ns \
    --json-log \
    --drain-timeout=2m
```

To gracefully shut down: `kill <pid>` or Ctrl-C. To force immediate exit: `kill <pid>` twice (or Ctrl-C twice).

### See Also

- One-shot sibling: [`skytime run`](#skytime-run) — embedded transient worker, exits when the workflow completes.
- Custom CLI: [`docs/cli-binary.md`](../cli-binary.md) — register your own extensions and credential handler in your binary, then run `your-binary server --rootdir=...`.
- Architecture: [`docs/architecture.md`](../architecture.md) — `pkg/worker` boot, frozen registries, deterministic re-parse on workflow start.

---

## skytime dev-temporal

> **Renamed in Phase 7 per D-07-21.** Prior to v1.43, this subcommand carried the legacy name (see CHANGELOG). The hard rename (no deprecation alias) clarifies that the wrapped subprocess is *Temporal*'s dev server, not Skytime's. Update any local scripts or CI pipelines accordingly.

### Synopsis

    skytime dev-temporal [persistent flags] [-- ...args forwarded to `temporal server start-dev`]

Source: `pkg/cli/dev_temporal.go`.

### Motivation

Convenience wrapper around `temporal server start-dev` (D4-09). It does NOT embed Temporalite as a Go dependency — keeps `cmd/skytime` lean and matches the SDK v1.42 dependency footprint (Temporalite would drag sqlite + heavy temporal-server transitives into every `skytime` install). Consultants on macOS run `brew install temporal` once and `skytime dev-temporal` afterward.

Behavior (D4-10, D4-11, D4-12):

- **Subprocess wrapper.** `exec.CommandContext(ctx, "temporal", "server", "start-dev", ...)`. Stdin, stdout, stderr pass through verbatim — the Temporal dev server's URL banner and shutdown logs land directly on your terminal.
- **Flag passthrough.** `DisableFlagParsing: true` on the cobra command so any flags after `dev-temporal` (e.g., `--port 8233`, `--db-filename ./temporal.db`) are forwarded unmodified to `temporal server start-dev`. Persistent Skytime flags before `dev-temporal` (e.g., `skytime --debug dev-temporal`) still bind normally.
- **SIGINT/SIGTERM forwarding.** A `signal.Notify` goroutine catches Ctrl-C and termination signals on the parent and forwards them to the subprocess via `sub.Process.Signal`. The subprocess also shares the parent's process group, so terminal Ctrl-C reaches it directly; the goroutine is defense-in-depth for non-TTY contexts.
- **Missing-binary install hints.** `exec.LookPath("temporal")` failure prints (verbatim from `pkg/cli/dev_temporal.go::printMissingTemporalBinary`):

      error: `temporal` CLI not found on PATH.
      Install:
        macOS:   brew install temporal
        script:  curl -sSf https://temporal.download/cli.sh | sh
        Go:      go install go.temporal.io/server/cmd/temporal@latest

  Then exits non-zero.

### Flags

No subcommand-specific flags from Skytime. Persistent flags apply, but the connection-shape flags (`--address`, `--api-key`, mTLS triplet) are ignored — `dev-temporal` is the *target* of `skytime run`, not a client of it. Anything else placed after `dev-temporal` is forwarded to the subprocess (per `DisableFlagParsing`).

### Exit Codes

- `0` — subprocess exited 0 (rare; you typically Ctrl-C this).
- non-zero — `temporal` binary not on PATH (install hints printed) OR subprocess exited non-zero (Skytime mirrors the exit status). `pkg/cli/dev_server.go` returns the package-private `errSilent` sentinel so cobra produces a non-zero exit without re-printing.

### Example

```
# Terminal 1
skytime dev-temporal

# Terminal 2 (after the Temporal dev server's "Started Server" banner appears)
skytime run examples/skeleton/expression_if.star \
    --flow check_user --input '{"user_id":"42"}'
```

For the Temporal CLI install matrix, see <https://docs.temporal.io/cli>.

### See Also

- Tutorial: [`docs/getting-started.md`](../getting-started.md) — uses `dev-temporal` for the local-loop walkthrough.
- Custom CLI: [`docs/cli-binary.md`](../cli-binary.md) — your own binary inherits this subcommand verbatim through `cli.NewRootCommand`.

---

## Where to Go Next

- **Discoverability runtime equivalent:** [`skytime info`](#skytime-info) is the runtime equivalent of the auto-generated [builtins reference](builtins.md) — `info` shows what a specific `.star` file declares; `builtins.md` shows what the language itself supports.
- **Authoring flows:** [`docs/for-flow-authors/README.md`](../for-flow-authors/README.md)
- **Building extensions (Go developers):** [`docs/for-extension-developers/README.md`](../for-extension-developers/README.md)
- **Tutorial:** [`docs/getting-started.md`](../getting-started.md) — `git clone` → `skytime dev-temporal` → `skytime run` in 5 minutes.
- **Custom CLI:** [`docs/cli-binary.md`](../cli-binary.md) — register your own extensions via `cli.WithExtensions(...)`; supply a `CredentialHandler` for production secrets resolution.
- **Architecture:** [`docs/architecture.md`](../architecture.md) — the parse/execute split (D4-18 Starlark-first error rendering, lambda capture model, registry-based deterministic re-parse on workflow start).
