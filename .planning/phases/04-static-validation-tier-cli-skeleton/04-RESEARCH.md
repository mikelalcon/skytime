# Phase 4: Static Validation Tier + CLI Skeleton - Research

**Researched:** 2026-04-30
**Domain:** Go CLI on top of Starlark static analysis (cobra + charm-log/v2 + go.starlark.net/syntax) and Temporal subprocess management
**Confidence:** HIGH on cobra/charm-log/syntax APIs (verified against pkg.go.dev + GitHub source); HIGH on existing codebase shape (read directly); MEDIUM on the AST-walk strategy for D4-02 (one critical discovery — see Summary).

## Summary

Phase 4 lays a **CLI tree** (`cmd/skytime/{validate,run,dev-server}`) that depends on a new reusable `pkg/cli` and a new `pkg/validator` facade. Locked decisions D4-01..D4-19 from CONTEXT.md determine almost every architectural choice; this research focuses on the seven gray areas the planner needs concrete shape for.

**The single most important finding:** `*starlark.Function` (the captured-lambda value already stored on `dag.CapturedLambda.Fn`) **does NOT retain a path back to the `*syntax.LambdaExpr` AST**. The runtime `Function.funcode` is compiled bytecode (`*compile.Funcode`); the syntax tree is discarded after compilation. This means D4-02's "AST walk for `ctx.<name>` references" cannot be implemented by reaching through the captured `*starlark.Function`. Instead, the validator must **re-parse the cached file bytes** with `(*syntax.FileOptions).Parse` to recover the AST, then look up each `CapturedLambda` by `Position()` and walk the matching `*syntax.LambdaExpr`/`*syntax.DefStmt` body. Re-parsing is cheap (pure function of bytes) and the parser already caches `fileBytes` per loaded file — the validator just needs read access to those bytes.

**Primary recommendation:** Build `pkg/validator` thin (`Validate(file, opts...)` wraps `parser.ParseFile` + a syntax re-parse for AST lookup), add D4-02's check as a finalize-pass in `pkg/parser/finalize.go` that uses an exported `Parser.FileBytes()` accessor, and put cobra/charm-log strictly under `pkg/cli` + `cmd/skytime` with the AST firewall extending Phase 2's allow-list test to gate them. Build the CLI with `cobra.Command.PersistentPreRunE` chains for shared init, `cobra.Command.SetArgs` for testability, and a slog-handler shim that filters Skytime-namespaced records for `skytime run`'s progress stream.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Validator Architecture**
- **D4-01:** Validation logic split — checks in `parser/finalize.go`, facade in `pkg/validator`. The new finalize-pass checks (kwarg cross-validate beyond the per-call `UnpackOperationKwargs`, free-vars-reference-declared-state, lambda-time globals re-assertion) are added to `parser/finalize.go` alongside `lintMixedIdempotency` / `lintBlockSize` / `lintEmptyTaskQueue`. They fill the documented `validateActionRefKwargs` no-op stub. A new `pkg/validator` package is a thin facade: `Validate(file string, opts ...Option) []error` calls `parser.ParseFile` and returns the typed errors. `pkg/validator` also owns the dry-run interpreter seam (D4-03).

- **D4-02:** "Lambda free vars reference declared state" = AST walk for `ctx.<name>` attribute accesses. State schema accumulates body-walking: flow inputs at entry, += `output_alias` after `script`, += `item` inside `for_each_parallel`, branches in `if_cond` see same pre-branch state. Reject with `*dag.ValidationError` carrying the lambda's position when a name is missing.

- **D4-03:** Dry-run interpreter seam = test-only mock `OperationDispatch`. The differential corpus test runs (a) `parser.ParseFile + validator.Validate` and (b) full `SkytimeWorkflow` against `testsuite.TestWorkflowEnvironment` with mock dispatch returning `OkResult{}` for every call; asserts agreement on accept/reject. Mock dispatch is a test helper, NOT a CLI flag or public library API. Composes with Phase 5: same dispatch-replacement seam, different mocks.

- **D4-04:** `dag.ValidationError` gains an `Action string` field. Format becomes `<file>:<line>:<col> [flow > step > action]: <msg>` when fields are non-empty (each segment dropped when blank).

**`skytime run` Execution Model**
- **D4-05:** Embedded transient worker. `skytime run <file.star> --flow=X --input=...` constructs a worker in-process via `pkg/worker.NewWorker`, calls `client.ExecuteWorkflow` with `WorkflowInput{FlowName, ContentHash, InitState}`, follows execution to completion, prints the result, and shuts the worker down.

- **D4-06:** Per-step progress + final result. Structured event line per Step / IfCond branch / Script / ForEachParallel fan-out / CallFlow child workflow. Activity dispatches print start/end with elapsed time. `print()` output (D3-22) teed to the CLI. NOT full Temporal event-history dump. Likely a `slog.Handler` shim filtering pkg/interpreter and pkg/activity records.

- **D4-07:** `--input=<json>` validated through the same input-schema check as static `validate`. Single source of truth.

- **D4-08:** Connection via flags + env vars; variant chosen by which flags are present.
  - `--api-key` set → `worker.NewCloudClient`
  - mTLS triplet (`--client-cert`, `--client-key`, `--server-ca`) set → `worker.NewSelfHostedClient`
  - Otherwise → `worker.NewDevClient`
  Env-var fallbacks: `SKYTIME_TEMPORAL_*`. No config file (no koanf) in v1.

**`skytime dev-server` Strategy**
- **D4-09:** Shell out to `temporal server start-dev` subprocess. NOT embed Temporalite as a Go dep. Locate via `exec.LookPath`, spawn attached to terminal, stream stdout/stderr.

- **D4-10:** Foreground lifecycle with SIGINT forwarding. Subprocess attached to terminal (no daemonization, no PID files). SIGINT/SIGTERM caught by CLI and forwarded; CLI exits when subprocess exits.

- **D4-11:** Match `temporal server start-dev` defaults; pass-through user flags. Defaults: frontend `:7233`, UI `:8233`, namespace `default`. CLI accepts `--port`, `--ui-port`, `--namespace`, etc., and forwards them.

- **D4-12:** Missing `temporal` binary → clear error with install instructions. `exec.LookPath("temporal")` fails → CLI prints brew/curl/go install instructions, exits non-zero. No auto-install.

**CLI Extensibility**
- **D4-13:** `pkg/cli` is the reusable cobra command tree; `cmd/skytime` is a thin wrapper. AST firewall test extension: only `cmd/skytime/...` and `pkg/cli/...` may import `cobra` or `charm-log/v2`. `pkg/parser`, `pkg/dag`, `pkg/extension`, `pkg/bridge`, `pkg/activity`, `pkg/interpreter`, `pkg/worker`, `pkg/validator` MUST NOT.

- **D4-14:** `cmd/skytime` ships with a generic HTTP extension baked in. Methods get/post/put/delete/head with Idempotent set per HTTP semantics; `HTTPResponse{Status, Body, Headers}` is a typed `OperationOutput`. Accepts BearerCredential, BasicCredential, APIKeyCredential. Uses Go stdlib `net/http`; no third-party HTTP client library.

- **D4-15:** Extension and credential-handler registration via functional options. `pkg/cli.NewRootCommand(cli.WithExtensions(http.New(...)), cli.WithCredentialHandler(myHandler))`. Mirrors Phase 1's `parser.NewParser` and Phase 3's `worker.NewWorker`.

- **D4-16:** No-extensions mode for `validate` = standard `*dag.ParseError` with actionable hint. Parser already produces a `*dag.ParseError` for unknown extensions; CLI's renderer appends a "build your own custom CLI binary" hint pointing to `docs/cli-binary.md`.

**Corpus & Differential Test**
- **D4-17:** Bootstrap minimal `examples/skeleton/`. Phase 4 lands 2-3 `.star` flows exercising every primitive using ONLY the baked-in HTTP extension. Differential corpus test (VAL-02, D4-03) runs against this directory.

**CLI Error Rendering**
- **D4-18:** Default error rendering is Starlark-first. CLI converts every error reaching `cobra.Command.Execute()` via `errors.As` into typed `*dag.ParseError` / `*dag.ValidationError` and renders the `<file>:<line>:<col> [flow > step > action]: <msg>` format. Wrapped Go errors dropped from default output. Color via charm-log when stdout is a TTY.

- **D4-19:** `--debug` flag reveals Go internals. Sets slog handler level to debug and includes `Wrapped` chains (`fmt.Sprintf("%+v", err)`). Stack traces remain hidden — Skytime errors don't carry stack traces, the option just unwraps.

### Claude's Discretion

- Exact path of the baked-in HTTP extension (`pkg/extension/builtin/http`, `extensions/http`, `pkg/http_ext`, etc.).
- Exact AST visitor implementation for the `ctx.<name>` attribute walk (D4-02).
- Cobra subcommand file layout under `pkg/cli/` (one file per subcommand vs combined).
- Slog handler shim implementation for D4-06 progress streaming.
- Test helper location for the dry-run interpreter mock.
- Charm-log rendering options.
- Whether the worker's frozen-after-boot constraint (D3-07) needs any tweak for the embedded `skytime run` worker.
- Schema declaration shape for `flow(inputs={...})`.

### Deferred Ideas (OUT OF SCOPE)

- Embed Temporalite as a Go dep — explicitly NOT in v1 (D4-09).
- Config file (`~/.skytime.yaml`) — koanf-based config loading deferred. Flags + env vars sufficient.
- Auto-install of `temporal` CLI — explicitly NOT in v1 (D4-12).
- `skytime dev-server start/stop/status` daemon mode — explicitly NOT in v1 (D4-10).
- `--dry-run` CLI flag on `skytime run` — dry-run is a test seam, not a user-facing flag.
- Full Temporal event-history dump option (`--event-history`) — v1.x.
- `--syntax-only` validate mode — explicitly NOT in v1 (D4-16).
- Auto-stub unknown extensions in validate — explicitly NOT in v1.
- JSON / structured error output for CI consumption — v1.x.
- Cross-flow dataflow analysis (proving `script(output_alias=X)`'s declared output keys are returned) — v1.x.
- `charmbracelet/fang` (cobra wrapper) — v1.x.
- Tier-2 unit tests for `def` blocks — `TEST-V2-01`.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| **VAL-01** | `skytime validate <file.star>` parses and checks every flow without executing — verifies kwargs match each extension's declared schema, every input maps to a registered schema, every lambda's free variables reference declared state, and the lambda predeclared-global subset is honored | D4-01 split (finalize-pass checks + thin `pkg/validator` facade); D4-02 AST visitor; existing `parser.finalize.validateActionRefKwargs` no-op stub; existing D-11 reflection-based `UnpackOperationKwargs`; existing D-19 free-var lint at Starlark-language level; D-20 lambda-time globals already enforced by `pkg/bridge` |
| **VAL-02** | Static validation and runtime parsing share the same parser code path — a CI corpus test runs every `.star` file in `examples/` through both static `validate` and a dry-run interpreter and asserts agreement on accept/reject | D4-03 dry-run seam = mock `OperationDispatch`; D4-17 corpus is `examples/skeleton/`; existing `interpreter.SkytimeWorkflow` + `testsuite.TestWorkflowEnvironment` (Phase 3 wired); existing `pkg/activity.OperationDispatch` map keyed `<ext>.<op>` |
| **VAL-03** | Validation errors formatted `<file>:<line>:<col> [flow > step > action]: <message>`; `--debug` reveals Go internals only | D4-04 adds `Action` field to `dag.ValidationError`; D4-18 Starlark-first rendering; D4-19 `--debug` semantics; existing `*dag.ParseError`/`*dag.ValidationError` `Position()` methods |
| **CLI-01** | `skytime validate <file.star>` runs static validation (Tier 1) and exits with structured errors | D4-13 `pkg/cli` + `cmd/skytime`; D4-15 functional options; D4-18 error rendering; D4-19 `--debug`; cobra `PersistentPreRunE` for shared init |
| **CLI-02** | `skytime run <file.star> --flow=<name> --input=<json>` parses, validates, and triggers a workflow on a configured Temporal cluster, then streams progress | D4-05 embedded transient worker; D4-06 per-step progress (slog handler shim); D4-07 input-schema validation reuse; D4-08 client variant routing; existing `worker.NewWorker` + `worker.NewCloudClient/NewSelfHostedClient/NewDevClient` |
| **CLI-04** | `skytime dev-server` spawns a local Temporal dev server | D4-09 subprocess; D4-10 foreground + signal forwarding; D4-11 default flag pass-through; D4-12 missing-binary error |
| **CLI-05** | The CLI lives under `cmd/skytime/`; cobra and charmbracelet/log are CLI-only — not reachable from library root | D4-13 firewall extension; existing AST-walk firewall test in `pkg/activity/firewall_test.go` (`TestNoTemporalImportsOutsideAllowList`) is the template |
</phase_requirements>

## Standard Stack

### Core (already in go.mod)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `go.starlark.net/syntax` | v0.0.0-20260326113308 | AST parsing for D4-02 visitor | Same module the parser already uses; `(*syntax.FileOptions).Parse` recovers AST from bytes |
| `go.temporal.io/sdk/client` | v1.42.0 | `client.ExecuteWorkflow` for `skytime run` | Phase 3 already wires three constructors |
| `go.temporal.io/sdk/testsuite` | v1.42.0 | Differential dry-run test runs `SkytimeWorkflow` against `TestWorkflowEnvironment` | Bundled with SDK |

### New (CLI-only — must NOT appear in library packages)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/spf13/cobra` | **v1.10.2** (verify) | CLI command tree (`validate`, `run`, `dev-server`) | Already vetted in `.planning/research/STACK.md`; `PersistentPreRunE` solves "load config → init logger → connect Temporal" pipeline cleanly |
| `github.com/spf13/pflag` | v1.0.9 (transitive via cobra) | POSIX/GNU-style flag parsing | Comes with cobra |
| `github.com/charmbracelet/log/v2` | **v2.0.0** (verify) | slog handler for terminal rendering | v2 is slog-native: `*log.Logger` directly implements `slog.Handler` (verified via pkg.go.dev) |

### Supporting (already-in-tree, no new deps)
| Library | Purpose | Where |
|---------|---------|-------|
| `log/slog` (stdlib) | The interface every Skytime package logs through | Library packages stay handler-agnostic |
| Go stdlib `os/exec` | Subprocess for `temporal server start-dev` | D4-09 — `exec.LookPath` + `cmd.Start`/`cmd.Wait` |
| Go stdlib `os/signal` + `syscall` | SIGINT/SIGTERM forwarding | D4-10 |
| Go stdlib `net/http` | Baked-in HTTP extension (D4-14) | No third-party HTTP client |
| Go stdlib `encoding/json` | `--input=<json>` parsing | D4-07 |
| Go stdlib `go/parser` + `go/token` | AST firewall test for cobra/charmlog | Same idiom as existing `firewall_test.go` |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| cobra | urfave/cli/v3 | Lighter, simpler — but no `PersistentPreRunE` chain; STACK.md already chose cobra |
| charm-log/v2 | logfmt + tint or stdlib `slog.NewTextHandler` | tint is a sleek alternative but lacks the "level + key/value table" rendering charm-log does. STACK.md picked charm-log — keeping consistent. |
| Re-parse via `syntax.Parse` | Fork the parser to retain AST per-lambda | Re-parse is O(file-bytes) and runs once per `validate`/`run` invocation. Forking the parser would couple parse-execute pipelines and re-introduce `dag.CapturedLambda` carrying `*syntax.LambdaExpr`, which violates the existing "pure data DAG, no AST" invariant. Re-parse is the lower-risk path. |
| `cmd.SysProcAttr.Setpgid = true` for subprocess | Default same-process-group | Default keeps the temporal subprocess in the parent's process group → terminal Ctrl-C reaches it naturally. Setpgid would isolate it but require us to forward signals via `syscall.Kill(-pid, sig)`. Default + explicit `signal.Notify` forward is simpler and cross-platform-cleaner. |

**Installation (CLI-only):**
```bash
go get github.com/spf13/cobra@v1.10.2
go get github.com/charmbracelet/log/v2@v2.0.0
```

**Version verification:** Both `cobra` and `charm-log/v2` are NOT yet in `go.mod` (verified via `go list -m -json github.com/spf13/cobra` → "not a known dependency"). The first task in Phase 4 will add them; planner must include `go mod tidy` in the toolchain task. Versions in STACK.md are the floor — `go get @latest` at the time of the plan task is acceptable as long as the resulting versions stay above v1.10.2 / v2.0.0.

## Architecture Patterns

### Recommended Project Structure
```
cmd/
└── skytime/
    └── main.go                     # 30-line wrapper: pkg/cli.NewRootCommand(...).ExecuteContext(ctx)
pkg/
├── cli/                            # NEW — reusable cobra command tree
│   ├── doc.go
│   ├── root.go                     # NewRootCommand(opts ...Option) *cobra.Command + Option type
│   ├── options.go                  # WithExtensions, WithCredentialHandler functional options
│   ├── validate.go                 # `skytime validate` subcommand
│   ├── run.go                      # `skytime run` subcommand (embedded transient worker)
│   ├── dev_server.go               # `skytime dev-server` subcommand (subprocess)
│   ├── flags.go                    # Persistent flags + env var binding helpers
│   ├── render.go                   # Error renderer (D4-18) + slog handler shim (D4-06)
│   └── *_test.go                   # cobra.SetArgs() table tests
├── validator/                      # NEW — thin facade
│   ├── doc.go
│   ├── validator.go                # Validate(file string, opts ...Option) []error
│   ├── options.go                  # WithExtensions, WithCredentialHandler (forwards to parser)
│   ├── dryrun_test.go              # VAL-02 differential corpus test
│   └── testing/                    # OR pkg/activity/testing — TBD by planner
│       └── always_ok_dispatch.go   # mock OperationDispatch for D4-03
├── parser/
│   ├── finalize.go                 # MODIFIED — fills validateActionRefKwargs + 2 new lints
│   ├── ctx_walk.go                 # NEW — *syntax.LambdaExpr / *syntax.DefStmt walker
│   ├── state_schema.go             # NEW — sequential body-walk accumulator
│   └── parser.go                   # MODIFIED — adds Parser.FileBytes() accessor
├── extension/builtin/http/         # NEW — baked-in HTTP extension (D4-14) — exact path is Claude's discretion
│   ├── http.go                     # endpoint factory + Idempotent table
│   ├── response.go                 # HTTPResponse OperationOutput
│   └── http_test.go
└── dag/
    └── errors.go                   # MODIFIED — Action field added to ValidationError (D4-04)
examples/
└── skeleton/                       # NEW — D4-17 differential corpus
    ├── simple_check.star
    └── parallel_fanout.star
docs/
└── cli-binary.md                   # NEW — referenced from D4-16 hint
tests/
└── firewall_test.go                # OR extend pkg/worker/firewall_test.go — gates cobra/charmlog
```

### Pattern 1: cobra `NewRootCommand` with Functional Options + PersistentPreRunE Chain

The PROJECT-wide pattern (per-instance, no globals — D-07) extends to the CLI. `pkg/cli.NewRootCommand` accepts functional options and returns a wired `*cobra.Command`.

```go
// pkg/cli/root.go
package cli

import (
    "context"
    "log/slog"

    "github.com/spf13/cobra"

    "github.com/mikelalcon/skytime/pkg/extension"
)

// Option configures the command tree at construction time.
type Option func(*config) error

type config struct {
    exts             []extension.Extension
    credHandler      extension.CredentialHandler
    // injected by PersistentPreRunE for sub-Run handlers:
    runtimeLogger    *slog.Logger
    debug            bool
}

// WithExtensions registers extensions used by validate / run.
func WithExtensions(e ...extension.Extension) Option { /* ... */ }

// WithCredentialHandler wires the JIT resolver into `skytime run`'s embedded worker.
func WithCredentialHandler(h extension.CredentialHandler) Option { /* ... */ }

func NewRootCommand(opts ...Option) (*cobra.Command, error) {
    cfg := &config{}
    for _, opt := range opts {
        if err := opt(cfg); err != nil {
            return nil, err
        }
    }

    root := &cobra.Command{
        Use:           "skytime",
        Short:         "Starlark-defined durable workflows on Temporal",
        SilenceErrors: true, // D4-18: WE render errors, not cobra
        SilenceUsage:  true, // D4-18: no usage dump on validation errors
    }

    // Persistent flags (visible on every subcommand).
    root.PersistentFlags().BoolVar(&cfg.debug, "debug", false,
        "reveal Go internals in error output (D4-19)")
    root.PersistentFlags().String("address", "", "Temporal address (env: SKYTIME_TEMPORAL_ADDRESS)")
    root.PersistentFlags().String("namespace", "", "Temporal namespace (env: SKYTIME_TEMPORAL_NAMESPACE)")
    root.PersistentFlags().String("api-key", "", "Temporal Cloud API key (env: SKYTIME_TEMPORAL_API_KEY)")
    root.PersistentFlags().String("client-cert", "", "mTLS client cert file (env: SKYTIME_TEMPORAL_CLIENT_CERT)")
    root.PersistentFlags().String("client-key", "", "mTLS client key file (env: SKYTIME_TEMPORAL_CLIENT_KEY)")
    root.PersistentFlags().String("server-ca", "", "mTLS server CA file (env: SKYTIME_TEMPORAL_SERVER_CA)")

    // PersistentPreRunE chains: env-var binding → init slog handler → (subcommand-local)
    // connect Temporal client. PersistentPreRunE is inherited by subcommands, so
    // every subcommand gets the same init order.
    root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
        bindEnvVars(cmd)            // populate flag values from SKYTIME_* env vars
        cfg.runtimeLogger = newSlogLogger(cfg.debug)  // charm-log handler
        slog.SetDefault(cfg.runtimeLogger)
        return nil
    }

    root.AddCommand(newValidateCommand(cfg))
    root.AddCommand(newRunCommand(cfg))
    root.AddCommand(newDevServerCommand(cfg))

    return root, nil
}
```

`cmd/skytime/main.go` becomes:
```go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"
    "syscall"

    "github.com/mikelalcon/skytime/pkg/cli"
    httpext "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
)

func main() {
    ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
    defer cancel()

    root, err := cli.NewRootCommand(
        cli.WithExtensions(httpext.New()),
        cli.WithCredentialHandler(envCredHandler{}),
    )
    if err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
    if err := root.ExecuteContext(ctx); err != nil {
        // Renderer already printed; exit 1.
        os.Exit(1)
    }
}
```

**Why:** Cobra docs explicitly endorse `PersistentPreRunE` for shared init across subcommands; `SilenceErrors` + `SilenceUsage` are required for D4-18's custom rendering; `ExecuteContext` lets `os.Signal` cancellation reach `skytime dev-server` and `skytime run` cleanly.

### Pattern 2: charm-log/v2 as slog Handler

`*charmlog.Logger` directly implements `slog.Handler` in v2. The wiring is one line:

```go
// pkg/cli/render.go
package cli

import (
    "log/slog"
    "os"
    "time"

    charmlog "github.com/charmbracelet/log/v2"
    "golang.org/x/term"
)

// newSlogLogger returns the *slog.Logger the CLI uses. Library packages
// receive this via slog.Default(); the slog handler is the charm-log one.
func newSlogLogger(debug bool) *slog.Logger {
    level := charmlog.InfoLevel
    if debug {
        level = charmlog.DebugLevel
    }

    h := charmlog.NewWithOptions(os.Stderr, charmlog.Options{
        Level:           level,
        ReportTimestamp: false,                 // D4-06 prefers compact lines
        ReportCaller:    debug,                 // D4-19 — only with --debug
        TimeFormat:      time.Kitchen,
    })

    // TTY detection: charm-log auto-disables color when stderr isn't a TTY,
    // but explicit is clearer. (See pkg.go.dev: "Styling is automatically
    // disabled if output is not a TTY".)
    if !term.IsTerminal(int(os.Stderr.Fd())) {
        h.SetColorProfile(charmlog.AsciiProfile) // plain
    }

    return slog.New(h)
}
```

**TTY detection:** `golang.org/x/term.IsTerminal` is the canonical pattern. charm-log auto-detects too — explicit override is defense-in-depth and lets us pin output for piped consumers.

**Avoiding double output:** charm-log's slog handler renders one line per record using its own formatter. We do NOT add a `slog.NewTextHandler` on top — `slog.New(charmHandler)` is the single rendering path.

### Pattern 3: AST Re-Parse for D4-02 `ctx.<name>` Visitor

Critical: `*starlark.Function` does NOT retain a path back to the AST. Recovery strategy:

```go
// pkg/parser/ctx_walk.go
package parser

import (
    "go.starlark.net/syntax"
)

// ctxAccess is one `ctx.<attr>` reference found in a lambda body.
type ctxAccess struct {
    Pos      syntax.Position
    AttrName string
}

// findCtxAccesses re-parses src to recover the AST, locates the lambda whose
// def-position matches lambdaPos, and walks its body collecting every
// `<ctxName>.<attr>` access. ctxName is typically "ctx" (the conventional
// first parameter); the visitor reads it from the lambda's first param.
//
// Re-parse is intentional: *starlark.Function on dag.CapturedLambda discards
// the AST after compilation. Re-parse is a pure function of the file bytes;
// the parser already caches them in p.fileBytes per loaded file.
//
// Match strategy: walk every *syntax.LambdaExpr and *syntax.DefStmt in the
// re-parsed File and pick the one whose Span().start position equals
// lambdaPos. Equality is on (Filename, Line, Col). syntax.Position.Filename()
// in the re-parsed file matches what the parser stored on
// dag.CapturedLambda.Pos because we re-parse with the same filename.
func findCtxAccesses(src []byte, filename string, lambdaPos syntax.Position) ([]ctxAccess, error) {
    opts := &syntax.FileOptions{ /* same as defaultFileOptions() */ }
    file, err := opts.Parse(filename, src, 0)
    if err != nil {
        return nil, err
    }

    var (
        targetParamName string
        targetBody      syntax.Node
    )
    syntax.Walk(file, func(n syntax.Node) bool {
        switch fn := n.(type) {
        case *syntax.LambdaExpr:
            if fn.Lambda == lambdaPos && len(fn.Params) > 0 {
                if id, ok := fn.Params[0].(*syntax.Ident); ok {
                    targetParamName = id.Name
                    targetBody = fn.Body
                }
            }
        case *syntax.DefStmt:
            if fn.Def == lambdaPos && len(fn.Params) > 0 {
                if id, ok := fn.Params[0].(*syntax.Ident); ok {
                    targetParamName = id.Name
                    // Body is []Stmt — wrap into a virtual node or walk separately.
                    walkStmts(fn.Body, &accesses, targetParamName)
                }
            }
        }
        return true
    })

    var accesses []ctxAccess
    if targetBody != nil {
        syntax.Walk(targetBody, func(n syntax.Node) bool {
            if dot, ok := n.(*syntax.DotExpr); ok {
                if id, ok := dot.X.(*syntax.Ident); ok && id.Name == targetParamName {
                    accesses = append(accesses, ctxAccess{
                        Pos:      dot.NamePos,
                        AttrName: dot.Name.Name,
                    })
                }
            }
            return true
        })
    }
    return accesses, nil
}
```

**Edge cases the visitor handles by design:**
- **Nested attribute access** (`ctx.req.repo_name.length`): the outermost `DotExpr` has `X = DotExpr{X: DotExpr{X: Ident{ctx}, Name: req}, Name: repo_name}`. We only collect when `dot.X` is `*syntax.Ident{ctx}` directly — so we collect `req` (the first hop off `ctx`). That's the right semantics for D4-02: state schema names are the top-level keys; deeper attribute access is the lambda's data-shape concern.
- **Function calls inside attribute paths** (`ctx.foo()`): `CallExpr.Fn` would be a `*DotExpr`. Walk descends into both, so we catch `foo` as a state name reference (matches the Starlark semantics: `ctx.foo` is an attribute access; calling it is on top).
- **Conditional access** (`ctx.x if cond else ctx.y`): `*CondExpr` walks into both branches. We collect `x` and `y`.
- **Comprehensions** (`[ctx.row.name for row in ctx.rows]`): the inner `ctx.row` references the comprehension's `row` (which is NOT `ctx`); the outer `ctx.rows` IS `ctx.X`. The first-param-name match (`targetParamName == "ctx"`) keeps the false positive away.

**State schema accumulator (D4-02 stacking):**
```go
// pkg/parser/state_schema.go
type stateSet map[string]struct{}

func (s stateSet) clone() stateSet { /* ... */ }
func (s stateSet) add(name string) { /* ... */ }
func (s stateSet) has(name string) bool { /* ... */ }

// validateLambdaCtxAccesses walks one flow's body sequentially. At each
// node it computes the state set visible to that node's lambdas, finds
// the corresponding lambda's ctx accesses via findCtxAccesses, and emits a
// *dag.ValidationError for every undeclared name.
//
// Branching: if_cond's then/else branches see the same pre-branch state;
// outputs added inside `then` are NOT visible in `else_` (they fork).
// Sequential: outputs added by a script() are visible to every subsequent
// node in the same body.
// for_each_parallel: inside Steps, state += {ItemVar} for the duration.
func (p *Parser) validateLambdaCtxAccesses(flow *dag.Flow) error {
    initial := stateSet{}
    for k := range flow.Inputs {
        initial.add(k)
    }
    return p.walkBodyForCtxValidation(flow, flow.Body, initial)
}

func (p *Parser) walkBodyForCtxValidation(flow *dag.Flow, body []dag.Node, state stateSet) error {
    for _, node := range body {
        switch n := node.(type) {
        case *dag.Script:
            // Validate this script's lambda BEFORE adding its OutputAlias.
            if err := p.checkLambdaCtx(flow, n.Pos, n.LambdaID, state); err != nil {
                return err
            }
            state.add(n.OutputAlias)
        case *dag.IfCond:
            if err := p.checkLambdaCtx(flow, n.Pos, n.LambdaID, state); err != nil {
                return err
            }
            // then/else fork — each branch sees pre-branch state.
            if err := p.walkBodyForCtxValidation(flow, n.Then, state.clone()); err != nil {
                return err
            }
            if err := p.walkBodyForCtxValidation(flow, n.Else, state.clone()); err != nil {
                return err
            }
        case *dag.ForEachParallel:
            inner := state.clone()
            inner.add(n.ItemVar)
            if err := p.walkBodyForCtxValidation(flow, n.Steps, inner); err != nil {
                return err
            }
        case *dag.Step:
            // Step actions are kwargs evaluated at parse time; no lambda eval.
            // Nothing to do here for D4-02. (DSL-08 retry/timeout values are
            // pure data; not lambdas.)
        case *dag.CallFlow:
            // CallFlow inputs are pure data (D-19 enforces). No lambda eval.
        }
    }
    return nil
}

// checkLambdaCtx finds the lambda by ID, gets its ctx accesses, and emits
// a *dag.ValidationError for each undeclared name.
func (p *Parser) checkLambdaCtx(flow *dag.Flow, nodePos syntax.Position, lambdaID string, state stateSet) error {
    captured, ok := p.lambdas[lambdaID]
    if !ok {
        return nil // structurally unreachable post-finalize
    }
    src := p.fileBytes[captured.Pos.Filename()]
    accesses, err := findCtxAccesses(src, captured.Pos.Filename(), captured.Pos)
    if err != nil {
        return err
    }
    for _, acc := range accesses {
        if !state.has(acc.AttrName) {
            return &dag.ValidationError{
                Pos:  acc.Pos,
                Flow: flow.Name,
                Msg:  fmt.Sprintf("ctx.%s not in declared state (visible: %v)", acc.AttrName, sortedKeys(state)),
            }
        }
    }
    return nil
}
```

This sits next to `lintMixedIdempotency` / `lintBlockSize` in `finalize.go`'s pass list.

### Pattern 4: Embedded Transient Worker for `skytime run`

```go
// pkg/cli/run.go (excerpt)
func runFlow(ctx context.Context, cfg *config, file, flowName string, inputJSON string) error {
    // 1. Connect — variant chosen by which flags are present (D4-08).
    c, err := connectClient(cfg)
    if err != nil {
        return err
    }
    defer c.Close()

    // 2. Pre-validate (D4-07): same input-schema check as static `validate`.
    if err := validator.Validate(file,
        validator.WithExtensions(cfg.exts...),
        validator.WithCredentialHandler(cfg.credHandler),
    ); err != nil {
        return err
    }

    // 3. Parse the file ourselves to get ContentHash for the WorkflowInput.
    // Reuses the same parser code path as the worker bootstrap (VAL-02).
    rootDir := filepath.Dir(file)
    initState, err := parseInitState(inputJSON)
    if err != nil {
        return err
    }

    // 4. Boot an embedded worker pointing at rootDir.
    // BuildID = "skytime-cli/<git-sha>"; task queue must match the flow's
    // declared task_queue (D3-19) — read it after parsing.
    w, err := worker.NewWorker(c, worker.WorkerOptions{
        RootDir:           rootDir,
        BuildID:           "skytime-cli",
        TaskQueue:         cfg.taskQueue, // resolves flow → flow.TaskQueue → "skytime"
        Extensions:        cfg.exts,
        CredentialHandler: cfg.credHandler,
    })
    if err != nil {
        return err
    }
    if err := w.Start(); err != nil {
        return err
    }
    defer w.Stop()

    // 5. ContentHash from the worker's frozen registry — same map the worker
    // will look up at workflow tick (D3-04).
    contentHash, ok := w.Registry().ContentHashFor(flowName)
    if !ok {
        return fmt.Errorf("flow %s not found in %s", flowName, rootDir)
    }

    // 6. Trigger the workflow.
    run, err := c.ExecuteWorkflow(ctx, sdkclient.StartWorkflowOptions{
        TaskQueue: cfg.taskQueue,
    }, "SkytimeWorkflow", dag.WorkflowInput{
        FlowName:    flowName,
        ContentHash: contentHash,
        InitState:   initState,
    })
    if err != nil {
        return err
    }

    // 7. Block on completion. SIGINT (caller's signal.NotifyContext) cancels
    // ctx → c.CancelWorkflow is fired by the deferred cleanup (TBD).
    var result map[string]any
    if err := run.Get(ctx, &result); err != nil {
        return err
    }

    // 8. Print result.
    return renderResult(result)
}
```

**SIGINT during run:** With `signal.NotifyContext` from `main.go`, `Ctrl-C` cancels `ctx`. Two strategies for the running workflow:
- **(A) Pass-through:** the `run.Get(ctx, ...)` call returns `context.Canceled`; the workflow keeps running on Temporal until natural completion (orphan from the CLI perspective). Pragmatic for v1.
- **(B) Cancel:** before exit, call `c.CancelWorkflow(ctx, runID, runID)`. Cleaner but costs another RTT.

**Recommendation:** Strategy A for v1, document as "Ctrl-C detaches the CLI; the workflow continues on Temporal." Strategy B is a v1.x add behind `--cancel-on-exit`.

### Pattern 5: Per-Step Progress via slog Handler Shim (D4-06)

The interpreter today only logs via `workflow.GetLogger(ctx).Info("skytime workflow start", ...)` (workflow.go:29). Phase 4 needs to:

1. **Add slog calls in walkers.** Each walker (`walkStep`, `walkScript`, `walkIfCond`, `walkForEach`, `walkCallFlow`) emits an `Info` line at start with structured attrs: `step_kind`, `step_name`, `pos` (file:line), `action_kind` (when applicable). Activity dispatches log start/end with `elapsed_ms`.
2. **CLI installs a custom slog.Handler that filters and renders.** The handler wraps charm-log: it receives every record, but renders Skytime-namespaced records (any record with `lambda_id` or `step_kind` attr — TBD by planner) as compact progress lines, and lets unrelated SDK records pass through at INFO+.

```go
// pkg/cli/render.go (excerpt)
type progressHandler struct {
    underlying slog.Handler // charm-log wrapped
}

func (p *progressHandler) Handle(ctx context.Context, r slog.Record) error {
    if isSkytimeProgress(r) {
        // Render as: [skytime] flow=approve_pr step=2/4 (script:summarize) at example.star:42
        return renderProgressLine(p.underlying, r)
    }
    return p.underlying.Handle(ctx, r)
}
```

**Print routing already works** — D3-22 routes Starlark `print()` to `workflow.GetLogger(ctx).Info("[skytime/print] ..."` — that prefix is the natural filter for "user-print" output in the renderer.

**Concrete attrs to add (planner discretion):** `step_kind` (Step/IfCond/...), `step_name` (computed: "step #N at <pos>" or `Script.ID`), `action_kind` (`<ext>.<op>` from ActionRef), `flow_name` (already on `i.flow.Name`).

### Pattern 6: `temporal server start-dev` Subprocess (D4-09/10/11/12)

```go
// pkg/cli/dev_server.go
func runDevServer(ctx context.Context, cfg *config, args []string) error {
    bin, err := exec.LookPath("temporal")
    if err != nil {
        return missingTemporalErr() // D4-12 — clear message with install hint
    }

    // Pass-through user flags (D4-11). cobra has consumed our root flags;
    // unknown flags + everything after `--` reaches us via `args`.
    subArgs := append([]string{"server", "start-dev"}, args...)

    cmd := exec.CommandContext(ctx, bin, subArgs...)
    cmd.Stdin = os.Stdin
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    // NO SysProcAttr.Setpgid — keep the subprocess in the parent's process
    // group so terminal Ctrl-C reaches it naturally. We additionally forward
    // signals via NotifyContext → cmd.Process.Signal as defense in depth.
    if err := cmd.Start(); err != nil {
        return fmt.Errorf("start temporal: %w", err)
    }

    // Optional defense-in-depth signal forwarder (D4-10):
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    defer signal.Stop(sigCh)
    go func() {
        for sig := range sigCh {
            _ = cmd.Process.Signal(sig)
        }
    }()

    if err := cmd.Wait(); err != nil {
        var exitErr *exec.ExitError
        if errors.As(err, &exitErr) {
            os.Exit(exitErr.ExitCode())
        }
        return err
    }
    return nil
}
```

**Flag pass-through approach (D4-11 trade-off):**
- **Option A (recommended for v1):** Use cobra `DisableFlagParsing: true` on the dev-server subcommand. Everything after `dev-server` reaches `RunE` as raw `args` — we forward verbatim. Pro: future-compatible with new `temporal` CLI flags; con: no autocomplete/help for known flags.
- **Option B:** Define each known flag (`--port`, `--ui-port`, `--namespace`, `--ip`, `--db-filename`, `--headless`) on cobra and forward them. Pro: better UX/help; con: couples to specific `temporal` CLI versions.

**Recommendation:** Option A — Skytime is a wrapper, not a UX layer over Temporal's flags. Document the prerequisite + a one-liner like `skytime dev-server -- --port 7234`. (The `--` separator forces cobra to stop parsing.)

**Cross-platform signals:** On Windows, only `SIGINT` and `SIGKILL` are meaningful via `syscall`. The pattern above is portable: `signal.Notify(SIGINT, SIGTERM)` works (Unix), Windows ignores the SIGTERM registration without error.

**Missing-binary error message:**
```
error: `temporal` CLI not found on PATH.
Install:
  macOS:   brew install temporal
  script:  curl -sSf https://temporal.download/cli.sh | sh
  Go:      go install go.temporal.io/server/cmd/temporal@latest
```

### Pattern 7: AST Firewall Test Extension (D4-13)

Phase 2 already established the firewall pattern in `pkg/activity/firewall_test.go`'s `TestNoTemporalImportsOutsideAllowList`. Phase 4 replicates the same shape for cobra/charmlog:

```go
// tests/firewall_cli_test.go (or wherever the planner places it)
package firewall_test

import (
    "go/parser"
    "go/token"
    "os"
    "path/filepath"
    "strings"
    "testing"

    "github.com/stretchr/testify/require"
)

func TestNoCobraImportsOutsideAllowList(t *testing.T) {
    forbidden := []string{
        "github.com/spf13/cobra",
        "github.com/spf13/pflag",
        "github.com/charmbracelet/log/v2",
    }
    allowedRel := []string{"cli"} // pkg/cli is the only library-side allow

    moduleRoot := findModuleRoot(t)
    pkgRoot := filepath.Join(moduleRoot, "pkg")

    fset := token.NewFileSet()
    walkErr := filepath.Walk(pkgRoot, func(path string, info os.FileInfo, walkErr error) error {
        if walkErr != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
            return walkErr
        }
        rel, _ := filepath.Rel(pkgRoot, path)
        for _, allowed := range allowedRel {
            if rel == allowed || strings.HasPrefix(rel, allowed+string(filepath.Separator)) {
                return nil
            }
        }
        f, _ := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
        for _, imp := range f.Imports {
            ip := strings.Trim(imp.Path.Value, `"`)
            for _, fb := range forbidden {
                if strings.HasPrefix(ip, fb) {
                    t.Errorf("FIREWALL VIOLATION: %s imports %q — only pkg/cli (and cmd/skytime) may import CLI deps", path, ip)
                }
            }
        }
        return nil
    })
    require.NoError(t, walkErr)
}

// Inversion meta-test (mirrors TestPkgActivity_AllowedToImportTemporal):
// at least one pkg/cli/*.go file must actually import cobra, otherwise
// the allow-list is vacuous.
func TestPkgCli_ImportsCobra(t *testing.T) { /* ... */ }
```

**Two locations are valid (Claude's discretion):**
- Extend `pkg/activity/firewall_test.go` (the package-level test that checks every other pkg/) to ALSO check cobra/charmlog imports.
- Add a new `tests/firewall_cli_test.go` co-located with the existing pattern.

**Recommendation:** New `tests/firewall_cli_test.go` (or `pkg/cli/firewall_test.go` checking inversion only). The existing firewall test is logically about Temporal — keeping the cobra firewall in its own file makes the intent obvious and avoids a 3-loop merged test.

### Pattern 8: Differential Corpus Test Harness (VAL-02 + D4-03)

```go
// pkg/validator/dryrun_test.go
package validator_test

import (
    "context"
    "io/fs"
    "path/filepath"
    "testing"

    "go.temporal.io/sdk/testsuite"

    "github.com/mikelalcon/skytime/pkg/dag"
    "github.com/mikelalcon/skytime/pkg/validator"
    "github.com/mikelalcon/skytime/pkg/validator/testing/dryrun"
    httpext "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
)

// TestDifferentialCorpus walks examples/skeleton/ and asserts static
// validation and dry-run interpretation agree on accept/reject for every
// .star file (VAL-02).
func TestDifferentialCorpus(t *testing.T) {
    corpusDir := "../../examples/skeleton"
    err := filepath.WalkDir(corpusDir, func(path string, d fs.DirEntry, _ error) error {
        if d.IsDir() || filepath.Ext(path) != ".star" {
            return nil
        }
        t.Run(filepath.Base(path), func(t *testing.T) {
            staticErrs := validator.Validate(path,
                validator.WithExtensions(httpext.New()),
            )

            // Dry run: full SkytimeWorkflow against TestWorkflowEnvironment
            // with a mock OperationDispatch returning OkResult{} for all.
            ts := &testsuite.WorkflowTestSuite{}
            env := ts.NewTestWorkflowEnvironment()
            mockDispatch := dryrun.AlwaysOkDispatch()
            env.RegisterActivityWithOptions(/* mock activity using mockDispatch */)
            env.ExecuteWorkflow(/* SkytimeWorkflow with WorkflowInput */)
            var dryRunErr error
            if !env.IsWorkflowCompleted() {
                dryRunErr = env.GetWorkflowError()
            }

            // Compare: both pass OR both fail (with same error class).
            staticPassed := len(staticErrs) == 0
            dryRunPassed := dryRunErr == nil
            if staticPassed != dryRunPassed {
                t.Fatalf("DIVERGENCE in %s: static=%v dryrun=%v\n"+
                    "static errs: %v\ndryrun err: %v",
                    path, staticPassed, dryRunPassed, staticErrs, dryRunErr)
            }
        })
        return nil
    })
    require.NoError(t, err)
}
```

**Mock dispatch (D4-03):**
```go
// pkg/validator/testing/dryrun/dispatch.go (or pkg/activity/testing — planner picks)
package dryrun

import (
    "context"

    "github.com/mikelalcon/skytime/pkg/activity"
    "github.com/mikelalcon/skytime/pkg/dag"
    "github.com/mikelalcon/skytime/pkg/extension"
)

// AlwaysOkDispatch returns an OperationDispatch where every operation
// returns OkResult with a zero-value OperationOutput. Used by the
// differential corpus test (D4-03) to dry-run any flow without hitting
// real backends.
func AlwaysOkDispatch() activity.OperationDispatch {
    return activity.OperationDispatch{
        // Empty map — actions resolved by Kind_ at dispatch time;
        // a missing entry returns ErrUnknownOperation, which the test
        // wraps as RetryableErr. Better: provide a wildcard.
    }
    // Alternative: build a wrapper that intercepts every Kind_ and returns
    // OkResult. The exact shape is the planner's call — pkg/activity may
    // need a small extension to support "always-OK" mode without exporting
    // internals.
}
```

**Implementation note:** the existing `activity.OperationDispatch` is `map[string]extension.OperationSpec`. To "mock-dispatch any kind", the test seam either (a) registers a synthetic OperationSpec for every kind appearing in the corpus (looking up via parser registry), or (b) introduces a thin wrapper that intercepts `Activity.ExecuteBatch` at the test boundary. Option (a) is cleaner — corpus files use only `http.*` ops, and the seam pre-registers `OkResult`-returning specs for each.

### Anti-Patterns to Avoid

- **Reaching through `*starlark.Function` for AST.** Doesn't work — `funcode` is bytecode. Re-parse instead.
- **Putting cobra/charm-log imports in `pkg/parser`, `pkg/validator`, `pkg/worker`, etc.** Firewall test catches it. Planner: ensure validate/run wiring routes the slog.Logger through `slog.SetDefault()` so library packages don't need to know about charm-log.
- **Using `cobra.Command.Run` instead of `RunE`.** `RunE` returns errors; CLI renderer expects `errors.As`-able typed errors. `Run` swallows.
- **Putting cobra subcommand wiring at package init().** D-07 (no global state) extends to CLI. Use `NewRootCommand` constructor returning `*cobra.Command`.
- **Letting cobra print errors with `SilenceErrors: false`.** D4-18 mandates the renderer; cobra's default would print a second message. Set `SilenceErrors: true` and `SilenceUsage: true` on the root.
- **Spawning the Temporal subprocess with `cmd.Run()`.** Blocking call — can't forward signals or context cancellation. Use `cmd.Start` + `cmd.Wait` (and `exec.CommandContext`).
- **Embedding `temporalite` as a Go dep.** Explicitly forbidden by D4-09. Use `exec.LookPath` + subprocess.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Walk `*starlark.Function` body for ctx accesses | A custom bytecode walker for `compile.Funcode` | `(*syntax.FileOptions).Parse` re-parse + `syntax.Walk` over LambdaExpr/DefStmt | Bytecode walking is fragile across go.starlark.net versions; AST is the documented stable surface |
| Subcommand registration | Manual `os.Args` parsing + dispatch | cobra `AddCommand` + `RunE` | cobra handles `--help`, `--version`, completion, env-var binding via pflag |
| TTY detection for color toggling | Reading `$TERM` and parsing it | `golang.org/x/term.IsTerminal(int(os.Stderr.Fd()))` | Stdlib pattern, handles edge cases (cygwin, redirects, ssh-tty) |
| Subprocess signal forwarding | Manually translating signals across goroutines | `signal.NotifyContext` (Go 1.16+) + `exec.CommandContext` | Stdlib pattern; one-liner for "cancel on Ctrl-C" |
| JSON input parsing | Hand-rolled string scan | `encoding/json.Unmarshal(b, &map[string]any{})` | D4-07 — same shape `WorkflowInput.InitState` already uses |
| HTTP client for the baked-in extension | Pull in `resty`, `gentleman`, etc. | `net/http.NewRequestWithContext` + `http.DefaultClient` | D4-14 explicitly mandates "Go stdlib net/http only" |
| slog handler implementation | Implement the `Enabled`/`Handle`/`WithAttrs`/`WithGroup` quartet from scratch | `charm-log/v2.NewWithOptions` (already implements all four) | Verified via pkg.go.dev: `*log.Logger` directly implements `slog.Handler` |
| Error format renderer | Hand-roll the `<file>:<line>:<col> [flow > step > action]` builder per call site | One renderer in `pkg/cli/render.go` consumed by `errors.As` over typed dag errors | D4-18 single source of truth; existing typed errors already carry `Position()` |

**Key insight:** Phase 4 ships almost no new infrastructure — it's mostly composition over existing pieces (parser, worker, extension SDK) plus three external libraries (cobra, charm-log, stdlib os/exec). The temptation to "improve" rendering or to write a "smarter" subprocess wrapper should be resisted.

## Common Pitfalls

### Pitfall 1: AST-walk lookup mismatch on lambda position
**What goes wrong:** `findCtxAccesses` re-parses the file, walks for `*syntax.LambdaExpr`/`*syntax.DefStmt`, but the position match on `lambdaPos` fails because the parser caches `*starlark.Function.Position()` (which is the body position) while we compare against `LambdaExpr.Lambda` (the keyword position).
**Why it happens:** Starlark stores both — `LambdaExpr.Lambda` is the `lambda` keyword location; `LambdaExpr.Body` is at the body's start. `*starlark.Function.Position()` per pkg.go.dev returns "the position where the function was defined" — typically the keyword.
**How to avoid:** In `pkg/dag/lambda.go`, `ComputeLambdaID` already keys off `pos.Line` and `pos.Col`. Verify the parser stores `fn.Position()` (the keyword/def position). The visitor's match should use `fn.Lambda == capturedPos` for `LambdaExpr` and `fn.Def == capturedPos` for `DefStmt`. **Test:** a fixture with two lambdas on the same line at different columns must be distinguishable.
**Warning signs:** D4-02 lint passes for valid files but the wrong lambda body is being walked → false negatives or positives.

### Pitfall 2: Cobra `os.Args` vs `cmd.SetArgs` in tests
**What goes wrong:** Subcommand tests call `rootCmd.Execute()` and pick up the `go test` runner's args (`-test.run=...` etc.).
**Why it happens:** cobra reads `os.Args` by default.
**How to avoid:** **Always** call `rootCmd.SetArgs([]string{"validate", "fixture.star"})` before `Execute()` in tests. Verified pattern from cobra docs.
**Warning signs:** Test output shows "unknown command: -test.run".

### Pitfall 3: Worker frozen-after-boot vs `skytime run`'s ad-hoc rootDir
**What goes wrong:** D3-07 says the worker registry is frozen after boot. `skytime run` boots a worker, fires a workflow, gets the result, and stops. If the consultant edits the `.star` file and re-runs, the second `run` invocation re-parses (new worker boot) — fine. But if they edit between `validate` and `run`, validate sees one content_hash and run sees another — the workflow runs against the new content, which is what the user wants. Confirm with a test.
**Why it happens:** Two invocations = two worker boots = two registries.
**How to avoid:** Document in the README. The runtime contract is "edit, run, observe" — exactly what we want. Add an integration test: edit a fixture mid-run, assert second run picks up the new content_hash.
**Warning signs:** Behavior surprise reports.

### Pitfall 4: `signal.NotifyContext` cancellation reaches Temporal client mid-RPC
**What goes wrong:** User Ctrl-C while `run.Get(ctx, &result)` is in flight. ctx is cancelled. The SDK returns `context.Canceled`. The CLI exits with a confusing error.
**Why it happens:** The expected behavior is "detach" — the CLI returns, but the workflow continues on Temporal.
**How to avoid:** In `runFlow`, check for `errors.Is(err, context.Canceled)` after `run.Get` and render a friendly "interrupted; workflow continues on Temporal as runID=X" line. Exit code 130 (Unix convention for SIGINT).
**Warning signs:** Stack traces on Ctrl-C; misleading "workflow failed" output.

### Pitfall 5: `temporal server start-dev` subprocess outlives parent on bad exit path
**What goes wrong:** CLI panics; deferred cleanup doesn't run; subprocess keeps running.
**Why it happens:** Go's deferred functions don't run on panic in `main()` if `os.Exit` is called from a higher frame.
**How to avoid:** Use `exec.CommandContext(ctx, ...)` — when ctx is cancelled the subprocess is killed by the runtime. `signal.NotifyContext` from `main.go` provides the ctx. The kill-on-cancel is documented behavior of `os/exec`.
**Warning signs:** `temporal` processes lingering after CLI crash; orphan processes in CI.

### Pitfall 6: Charm-log color codes piped to non-TTY consumers
**What goes wrong:** `skytime validate file.star | grep error` shows ANSI escapes intermixed with text.
**Why it happens:** charm-log's auto-detection works for stdout/stderr; some pipes look like terminals (e.g., `script` on macOS).
**How to avoid:** Use `golang.org/x/term.IsTerminal` explicitly on both stdout and stderr; force `AsciiProfile` when either is not a TTY. (Pattern shown above.)
**Warning signs:** Garbled text when piped or redirected.

### Pitfall 7: Differential corpus test masks real bugs by accepting both-fail
**What goes wrong:** Static says "missing kwarg X"; dry-run says "panic in interpreter". Both fail, so the test passes — but the dry-run failure is a real bug that should be visible.
**Why it happens:** D4-03 specifies "agree on accept/reject" — coarse equality.
**How to avoid:** Beyond pass/fail, check error categories. Static rejection should be `*dag.ParseError` or `*dag.ValidationError`; dry-run rejection should be a Temporal `*temporal.ApplicationError`. If both fail, assert both errors are user-error class (no internal panics, no Go runtime errors). Planner: include a "no-panic" assertion in the test.
**Warning signs:** A flow that should run cleanly stays accepted in CI but a hand-trigger reports a Go panic.

### Pitfall 8: `cobra.Command.SetContext` vs `ExecuteContext` confusion
**What goes wrong:** The planner uses `root.SetContext(ctx); root.Execute()` thinking ctx is propagated. cobra docs are clear: `Execute()` replaces a nil context with `context.Background()` — but doesn't re-use what `SetContext` set.
**Why it happens:** Two similarly-named entry points.
**How to avoid:** Always call `root.ExecuteContext(ctx)`. (Verified via cobra docs.)
**Warning signs:** Subcommand `cmd.Context()` returns `context.Background()` instead of the cancellable one.

### Pitfall 9: `DotExpr.Name` is `*Ident`, not a string
**What goes wrong:** Visitor writes `if dot.Name == "ctx" { ... }` — type error or always-false comparison.
**Why it happens:** `syntax.DotExpr.Name` is typed `*Ident`; `Ident.Name` is the string.
**How to avoid:** Always go through `dot.Name.Name` (the string field on the Ident). Verified via pkg.go.dev: `DotExpr{X Expr; Dot Position; NamePos Position; Name *Ident}`.
**Warning signs:** Compile errors or zero matches in the visitor.

## Code Examples

Verified patterns from official sources:

### Example 1: cobra `PersistentPreRunE` chain
```go
// Source: pkg.go.dev/github.com/spf13/cobra (verified 2026-04-30)
rootCmd := &cobra.Command{
    Use: "myapp",
    PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
        // Inherited by all subcommands; runs before each subcommand's RunE.
        return initializeConfig()
    },
}
subCmd := &cobra.Command{
    Use: "sub",
    RunE: func(cmd *cobra.Command, args []string) error {
        // PersistentPreRunE has already run.
        return doWork(cmd.Context())
    },
}
rootCmd.AddCommand(subCmd)
```

### Example 2: charm-log/v2 as slog handler
```go
// Source: pkg.go.dev/github.com/charmbracelet/log/v2@v2.0.0 (verified 2026-04-30)
import (
    "log/slog"
    "os"
    "time"
    charmlog "github.com/charmbracelet/log/v2"
)
h := charmlog.NewWithOptions(os.Stderr, charmlog.Options{
    Level:           charmlog.InfoLevel,
    ReportTimestamp: true,
    TimeFormat:      time.Kitchen,
})
slog.SetDefault(slog.New(h))   // *charmlog.Logger implements slog.Handler directly
```

### Example 3: syntax.Walk for `ctx.<attr>`
```go
// Source: derived from go.starlark.net/syntax docs + walk.go (verified via GitHub fetch)
syntax.Walk(lambdaExpr.Body, func(n syntax.Node) bool {
    if dot, ok := n.(*syntax.DotExpr); ok {
        if id, ok := dot.X.(*syntax.Ident); ok && id.Name == "ctx" {
            collect(dot.NamePos, dot.Name.Name)  // dot.Name is *Ident; .Name is its string
        }
    }
    return true  // continue traversal
})
```

### Example 4: subprocess with signal forwarding
```go
// Source: pkg.go.dev/os/exec (verified 2026-04-30)
ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer cancel()

cmd := exec.CommandContext(ctx, "temporal", "server", "start-dev")
cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
if err := cmd.Start(); err != nil { return err }
if err := cmd.Wait(); err != nil {
    var ee *exec.ExitError
    if errors.As(err, &ee) {
        os.Exit(ee.ExitCode())
    }
    return err
}
```

### Example 5: `(*syntax.FileOptions).Parse` AST recovery
```go
// Source: pkg.go.dev/go.starlark.net/syntax (verified 2026-04-30)
opts := &syntax.FileOptions{
    Set: false, While: false, TopLevelControl: true,
    GlobalReassign: false, LoadBindsGlobally: false, Recursion: false,
}
file, err := opts.Parse("flows/example.star", srcBytes, 0)
if err != nil { return err }
// file.Stmts is the AST — walk for LambdaExpr / DefStmt nodes.
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `charmbracelet/log` v1 | `charmbracelet/log/v2` v2.0.0 | 2026-03 | v2 is slog-native (`*log.Logger` implements `slog.Handler` directly); v1 had a separate slog adapter. Use v2. |
| `cobra.Command.Run` (no error) | `cobra.Command.RunE` (returns error) | 2018+ ecosystem-wide | RunE is required for D4-18's `errors.As` rendering pipeline. |
| Cobra `init()` global registration | `NewRootCommand` constructor | Ecosystem trend post-2022 | Aligns with D-07 (no global state). |
| `cmd.Run()` (blocking, no signal handling) | `exec.CommandContext` + `cmd.Start`/`cmd.Wait` | Go 1.16+ | Allows ctx-based cancellation; signal forwarding via `NotifyContext`. |
| `signal.Notify` channels | `signal.NotifyContext` (Go 1.16+) | 2021 | One-liner for "cancel on signal" — used by both `skytime run` and `skytime dev-server`. |
| `syntax.Parse` | `(*syntax.FileOptions).Parse` | go.starlark.net rolling | Plain `syntax.Parse` is deprecated in favor of options-based; matches what the parser already uses. |

**Deprecated/outdated:**
- `*starlark.Function`-via-bytecode AST inspection: NEVER expose this — it's not stable across versions. Use re-parse.
- `cobra.MousetrapHelpText`: legacy Windows-only feature; no v1 use case.
- `charm-log` v1: no slog handler; replaced by v2.
- Pre-context cobra patterns (`func(*cobra.Command)` without context): always use `RunE` + `cmd.Context()`.

## Open Questions

1. **Where does the `OkResult{}` mock dispatch live?** (D4-03)
   - What we know: it's a test helper, not a public API; composes with Phase 5's Starlark-mock dispatch.
   - What's unclear: `pkg/activity/testing/`, `pkg/interpreter/testing/`, or `pkg/validator/internal/dryrun/`.
   - Recommendation: `pkg/validator/internal/dryrun/` (closest to consumer, internal so Phase 5 can either replicate or import via test-only path). Planner picks during Wave-0 setup.

2. **Should `validateActionRefKwargs` actually do anything in Phase 4 beyond what `UnpackOperationKwargs` already enforces?**
   - What we know: VAL-01 says "verifies kwargs match each extension's declared schema." D-11's `UnpackOperationKwargs` is invoked at extension-factory call time and enforces the schema; the parse-time check is therefore complete.
   - What's unclear: is there a second, post-parse audit needed? Possibly: walk every `dag.ActionRef` and re-validate against the registered `OperationSpec.KwargsType` to catch ActionRefs hand-built outside the factory path (test fixtures, future programmatic callers).
   - Recommendation: implement as defense-in-depth — walk all ActionRefs in finalize and re-run `extension.UnpackOperationKwargs(spec, ref.Kwargs)`. ~30 LOC. Low cost, closes the hand-built-ActionRef hole.

3. **Schema declaration shape for `flow(inputs={...})`** (Claude's discretion in CONTEXT.md)
   - What we know: today `flow.Inputs` is `map[string]string` (kwarg-name → declared-type-hint).
   - What's unclear: is the type-hint string ("string", "int", "dict", ...) interpreted by the validator? Currently no — it's stored but unused.
   - Recommendation: keep the existing list-of-names-with-string-hints in v1; D4-02 only checks names exist, not types match. Document deferred items in `## Deferred Ideas`. A v1.x typed-input check is purely additive.

4. **Differential corpus test mock dispatch — wildcard or per-kind?**
   - What we know: `activity.OperationDispatch` is `map[string]extension.OperationSpec` keyed by `<ext>.<op>`.
   - What's unclear: does the mock need to register a synthetic spec per kind, or can it intercept `Activity.ExecuteBatch` at the test boundary?
   - Recommendation: planner ships a `dryrun.NewActivity(parser.Registry()) *activity.Activity` that wraps the registered specs and replaces every `OperationFunc` with one that returns a zero-value `OperationOutput`. Reuses real schemas (so kwarg checks still fire) but bypasses I/O.

5. **`skytime run --task-queue` flag or auto-resolve?**
   - What we know: D3-19 task_queue precedence is `step > flow > worker default`. Embedded worker has a TaskQueue option; the `ExecuteWorkflow` call also takes a TaskQueue.
   - What's unclear: should the CLI accept `--task-queue`, or always read it from the flow's `task_queue` kwarg?
   - Recommendation: accept `--task-queue` but default to the flow's own kwarg when present. Both worker.TaskQueue and StartWorkflowOptions.TaskQueue must match to avoid task routing surprises. Planner: read flow.TaskQueue post-validate, surface as an init log line so the user sees the resolved queue.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | Build/test | ✓ | go1.25.9 | — |
| `go.starlark.net` | Re-parse for D4-02 | ✓ (in go.mod) | pseudo-version | — |
| `go.temporal.io/sdk` | `skytime run` client + testsuite | ✓ (in go.mod) | v1.42.0 | — |
| `github.com/spf13/cobra` | CLI command tree | ✗ | — | None — must be added in Wave 0 (`go get github.com/spf13/cobra@v1.10.2`) |
| `github.com/charmbracelet/log/v2` | Slog handler | ✗ | — | None — must be added in Wave 0 (`go get github.com/charmbracelet/log/v2@v2.0.0`) |
| `golang.org/x/term` | TTY detection | unknown — likely transitive | — | If not transitive, planner adds via `go get golang.org/x/term`. |
| `temporal` CLI binary | `skytime dev-server` runtime | ✗ | — | Not blocking — D4-12 emits a clear install-instruction error. CI test for `skytime dev-server` skips (or runs against a mocked `exec.LookPath`). |
| `workflowcheck` | Determinism CI gate | ✗ | — | Skips at test time per existing `TestWorkflowcheck_NoFindings`; CI installs separately. |
| `golangci-lint` | Lint CI gate | ✗ | — | Phase 4 doesn't introduce a new lint requirement; existing CI handles. |

**Missing dependencies with no fallback:** None. Cobra and charm-log are first-class Phase 4 deps; planner adds in Wave 0.

**Missing dependencies with fallback:**
- `temporal` CLI: `skytime dev-server` test must skip cleanly when binary is absent (mirrors `workflowcheck` pattern). Document this as the explicit `dev-server` integration-test skip path.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `github.com/stretchr/testify` v1.11.1 (already in `go.mod`) |
| Config file | none — Go discovers `*_test.go` automatically |
| Quick run command | `go test ./pkg/validator/... ./pkg/cli/... ./pkg/parser/... -run TestXxx -count=1` |
| Full suite command | `go test ./... -race -count=1` |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| VAL-01 | parser+finalize accepts a valid flow with declared inputs and matching `ctx.<name>` accesses | unit | `go test ./pkg/parser -run TestFinalize_CtxAccess_Valid -count=1` | ❌ Wave 0 |
| VAL-01 | parser rejects an unknown `ctx.<name>` reference with `[flow > step > action]` error | unit | `go test ./pkg/parser -run TestFinalize_CtxAccess_RejectsUnknown -count=1` | ❌ Wave 0 |
| VAL-01 | parser rejects mismatched extension kwargs at parse time (defense-in-depth re-check) | unit | `go test ./pkg/parser -run TestFinalize_KwargCrossValidate -count=1` | ❌ Wave 0 |
| VAL-01 | `validator.Validate()` returns the typed errors as a slice | unit | `go test ./pkg/validator -run TestValidate_ReturnsTypedErrors -count=1` | ❌ Wave 0 |
| VAL-02 | every `.star` file in `examples/skeleton/` agrees on accept/reject between static and dry-run | integration | `go test ./pkg/validator -run TestDifferentialCorpus -count=1` | ❌ Wave 0 |
| VAL-02 | dry-run mock dispatch returns `OkResult{}` for any `<ext>.<op>` in the corpus | unit | `go test ./pkg/validator/internal/dryrun -run TestAlwaysOkDispatch -count=1` | ❌ Wave 0 |
| VAL-03 | `dag.ValidationError.Error()` renders `<file>:<line>:<col> [flow > step > action]: <msg>` when Action is set | unit | `go test ./pkg/dag -run TestValidationError_FormatWithAction -count=1` | ❌ Wave 0 |
| VAL-03 | renderer drops Wrapped chains by default and includes them with `--debug` | unit | `go test ./pkg/cli -run TestRenderer_DebugUnwrapsChain -count=1` | ❌ Wave 0 |
| CLI-01 | `skytime validate` exits non-zero on validation errors | integration | `go test ./pkg/cli -run TestValidateCmd_ExitNonZeroOnError -count=1` | ❌ Wave 0 |
| CLI-01 | `skytime validate` happy path on `examples/skeleton/simple_check.star` exits zero | integration | `go test ./pkg/cli -run TestValidateCmd_HappyPath -count=1` | ❌ Wave 0 |
| CLI-02 | `skytime run` end-to-end against `testsuite.TestWorkflowEnvironment` runs the chosen flow and prints final state | integration | `go test ./pkg/cli -run TestRunCmd_EndToEnd -count=1` | ❌ Wave 0 |
| CLI-02 | `skytime run --input='{"x":1}'` validates the JSON input through the same schema check | integration | `go test ./pkg/cli -run TestRunCmd_InputSchemaCheck -count=1` | ❌ Wave 0 |
| CLI-02 | `--api-key` routes to `worker.NewCloudClient`; mTLS triplet routes to `NewSelfHostedClient`; otherwise `NewDevClient` | unit | `go test ./pkg/cli -run TestConnectClient_VariantRouting -count=1` | ❌ Wave 0 |
| CLI-04 | `skytime dev-server` finds `temporal` on PATH and spawns it with pass-through args | integration | `go test ./pkg/cli -run TestDevServerCmd_Spawn -count=1` (skips if binary missing) | ❌ Wave 0 |
| CLI-04 | missing `temporal` binary surfaces install instructions and exits non-zero | unit | `go test ./pkg/cli -run TestDevServerCmd_MissingBinary -count=1` | ❌ Wave 0 |
| CLI-04 | SIGINT to the CLI is forwarded to the subprocess | integration | `go test ./pkg/cli -run TestDevServerCmd_SignalForward -count=1` (uses `sleep` as fake binary on Unix; skips on Windows) | ❌ Wave 0 |
| CLI-05 | no `pkg/*` directory outside `pkg/cli` imports cobra, pflag, or charm-log/v2 | meta | `go test ./tests -run TestNoCobraImportsOutsideAllowList -count=1` (or extend `pkg/activity/firewall_test.go`) | ❌ Wave 0 |
| CLI-05 | at least one `pkg/cli/*.go` file imports cobra (firewall not vacuous) | meta | `go test ./pkg/cli -run TestPkgCli_ImportsCobra -count=1` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./<package-touched>/... -count=1` (matches existing per-task atomic-commit convention)
- **Per wave merge:** `go test ./pkg/... ./cmd/... -race -count=1`
- **Phase gate:** Full suite green (`go test ./... -race -count=1`) before `/gsd:verify-work`. Differential corpus test (`TestDifferentialCorpus`) is the load-bearing assertion; it MUST be green for VAL-02 to be considered satisfied.

### Wave 0 Gaps
- [ ] `pkg/cli/` — entire package (root.go, validate.go, run.go, dev_server.go, render.go, options.go, flags.go, *_test.go)
- [ ] `pkg/validator/` — entire package (validator.go, options.go, dryrun_test.go, internal/dryrun/dispatch.go)
- [ ] `pkg/parser/ctx_walk.go` + `pkg/parser/state_schema.go` — D4-02 implementation
- [ ] `pkg/parser/finalize.go` — fill `validateActionRefKwargs` + add `validateLambdaCtxAccesses` to pass list
- [ ] `pkg/parser/parser.go` — add `Parser.FileBytes()` accessor for re-parse
- [ ] `pkg/dag/errors.go` — add `Action string` field to ValidationError + update `Error()` format
- [ ] `pkg/dag/errors_test.go` — extend `TestValidationError_Format` for Action variants
- [ ] `pkg/extension/builtin/http/` — entire baked-in HTTP extension (path is Claude's discretion)
- [ ] `cmd/skytime/main.go` + `cmd/skytime/build_id.go` — binary entry point + `defaultBuildID` ldflag anchor
- [ ] `examples/skeleton/simple_check.star` + `parallel_fanout.star` — D4-17 differential corpus
- [ ] `tests/firewall_cli_test.go` (or extend `pkg/activity/firewall_test.go`) — D4-13 cobra/charmlog firewall
- [ ] `docs/cli-binary.md` — referenced by D4-16 hint
- [ ] Module deps: `go get github.com/spf13/cobra@v1.10.2 && go get github.com/charmbracelet/log/v2@v2.0.0 && go mod tidy`

## Sources

### Primary (HIGH confidence)
- [pkg.go.dev: github.com/spf13/cobra](https://pkg.go.dev/github.com/spf13/cobra) — PersistentPreRunE/RunE/SilenceErrors/SilenceUsage/SetArgs/ExecuteContext (verified 2026-04-30)
- [pkg.go.dev: github.com/charmbracelet/log/v2](https://pkg.go.dev/github.com/charmbracelet/log/v2) — `*Logger` implements `slog.Handler` directly; `NewWithOptions`/`Options`/`SetLevel`/`SetColorProfile`
- [pkg.go.dev: go.starlark.net/syntax](https://pkg.go.dev/go.starlark.net/syntax) — `Walk`, `Node`, `LambdaExpr`, `DefStmt`, `DotExpr`, `Ident`, `(*FileOptions).Parse`
- [pkg.go.dev: go.starlark.net/starlark](https://pkg.go.dev/go.starlark.net/starlark) — `*Function.Position()`, `NumParams`, `Param`; **no AST access** (verified)
- [GitHub: google/starlark-go/syntax/walk.go](https://github.com/google/starlark-go/blob/master/syntax/walk.go) — confirmed Walk recurses into LambdaExpr.Body and DefStmt.Body
- [GitHub: google/starlark-go/starlark/value.go](https://github.com/google/starlark-go/blob/master/starlark/value.go) — `Function` struct: `funcode *compile.Funcode` is unexported, no AST exposure
- [pkg.go.dev: os/exec](https://pkg.go.dev/os/exec) — CommandContext, Start/Wait, SysProcAttr, ProcessState.ExitCode
- [docs.temporal.io/cli/server](https://docs.temporal.io/cli/server) — `temporal server start-dev` flag list (--port, --ui-port, --namespace, --ip, --db-filename, --headless, --log-level)
- `pkg/parser/finalize.go` (read directly) — existing pass sequence + `validateActionRefKwargs` no-op stub
- `pkg/parser/linter.go` (read directly) — pattern for new lints
- `pkg/parser/parser.go` (read directly) — Parser shape, `fileBytes` cache
- `pkg/dag/errors.go` (read directly) — `ParseError`/`ValidationError` shape
- `pkg/worker/worker.go`, `pkg/worker/options.go`, `pkg/worker/client.go` (read directly) — embedded-worker recipe
- `pkg/activity/firewall_test.go` (read directly) — AST-walk firewall pattern to extend
- `pkg/interpreter/workflow.go` (read directly) — slog logger usage to extend for D4-06

### Secondary (MEDIUM confidence)
- [pkg.go.dev: golang.org/x/term](https://pkg.go.dev/golang.org/x/term) — `IsTerminal` for TTY detection
- [Cobra GitHub examples](https://github.com/spf13/cobra/tree/main/site/content/examples) — root command construction patterns

### Tertiary (LOW confidence)
- None — every claim above is backed by an authoritative source or direct code read.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — versions verified via STACK.md and pkg.go.dev; cobra/charm-log not yet in go.mod (planner adds in Wave 0).
- Architecture: HIGH — cobra patterns verified via pkg.go.dev; charm-log slog wiring verified via pkg.go.dev; AST re-parse strategy verified via go.starlark.net source code.
- Pitfalls: HIGH — derived from direct code read + verified library docs.
- D4-02 AST visitor: HIGH on the technique (re-parse + walk), MEDIUM on the position-match edge cases (Pitfall 1 must be tested early).
- Dry-run mock dispatch shape: MEDIUM — concrete API depends on planner choice between wildcard wrapper and per-kind synthetic spec.

**Research date:** 2026-04-30
**Valid until:** 2026-05-30 (cobra/charm-log are stable; go.starlark.net moves slowly; Temporal CLI flags drift quarterly — refresh if `dev-server` work is delayed beyond a month)

## RESEARCH COMPLETE

**Phase:** 04 - Static Validation Tier + CLI Skeleton
**Confidence:** HIGH

### Key Findings
- **Critical AST discovery:** `*starlark.Function` does NOT expose its AST (verified via go.starlark.net source code). D4-02's `ctx.<name>` walk MUST re-parse cached file bytes via `(*syntax.FileOptions).Parse`, then match lambdas by position. This is the load-bearing technical decision for VAL-01 implementation.
- **charm-log/v2's `*Logger` directly implements `slog.Handler`** — single-line wiring `slog.New(charmlog.NewWithOptions(...))`. No double-rendering; library packages stay handler-agnostic.
- **Cobra `PersistentPreRunE` + `SilenceErrors`/`SilenceUsage`** is the canonical pattern for shared init + custom error rendering (D4-18). Combined with `RunE` + `errors.As` over typed `*dag.ValidationError`/`*dag.ParseError`, the Starlark-first error contract is achievable in ~50 LOC of renderer code.
- **`temporal server start-dev` subprocess** uses stdlib `exec.CommandContext` + `signal.NotifyContext` — no third-party deps; signal forwarding works cross-platform with the documented Windows caveat (only SIGINT/SIGKILL meaningful).
- **AST firewall test extension** is a clean copy-paste of `pkg/activity/firewall_test.go`'s `TestNoTemporalImportsOutsideAllowList` with a different forbidden-imports list and allow-list. Two firewall tests in tree are easier to read than one combined.

### File Created
`.planning/phases/04-static-validation-tier-cli-skeleton/04-RESEARCH.md`

### Confidence Assessment
| Area | Level | Reason |
|------|-------|--------|
| Standard Stack | HIGH | All versions verified; STACK.md already vetted; new deps are documented mainstream packages |
| Architecture | HIGH | Each pattern (cobra wiring, charm-log, AST walk, subprocess) verified via pkg.go.dev or direct GitHub fetch |
| Pitfalls | HIGH | All 9 pitfalls grounded in code-read + docs; the AST re-parse pitfall (#1) is the highest-leverage one to test first |
| D4-02 implementation | MEDIUM-HIGH | Technique proven; position-match edge cases need a fixture-driven test in Wave 0 |
| Differential corpus mock | MEDIUM | Two viable shapes (wildcard wrapper vs per-kind synthetic spec); planner picks |

### Open Questions
1. Test-helper location for the dry-run mock (`pkg/validator/internal/dryrun/` vs `pkg/activity/testing/`).
2. Whether `validateActionRefKwargs` should defense-in-depth re-walk all ActionRefs or stay a no-op (research recommends re-walk, ~30 LOC).
3. Schema-language extension for `flow(inputs={...})` — keep list-of-name-with-string-hints in v1 (recommended).
4. `--task-queue` CLI flag default — recommend "flow's own kwarg, override with flag".

### Ready for Planning
Research complete. Planner can now create PLAN.md files for waves: (Wave 0 setup + deps + firewall extension), (Wave 1 D4-02 ctx visitor + finalize wiring + ValidationError Action field), (Wave 2 pkg/validator facade + dry-run dispatch + corpus + differential test), (Wave 3 pkg/cli root + flags + render + validate subcommand), (Wave 4 run subcommand + slog progress + dev-server subcommand + HTTP extension + examples/skeleton).
