# Stack Research

**Domain:** Go library — Starlark DSL frontend + Temporal workflow execution backend, with companion CLI/example project
**Researched:** 2026-04-26
**Confidence:** HIGH for the three architectural pillars (Go toolchain, Starlark, Temporal SDK); MEDIUM for adjacent CLI/logging/config choices (multiple defensible options exist).

## Summary

The architectural triple is fixed by the project: **Go + `go.starlark.net` + `go.temporal.io/sdk`**. Research focused on (1) verifying the *current* versions of those modules and their compatibility, (2) selecting adjacent libraries (CLI, logging, config, testing, lint) appropriate for a 2026 greenfield Go library that ships an example/CLI, and (3) flagging gotchas introduced by recent versions.

Two non-obvious outcomes from the research:
1. **`go.starlark.net` requires Go 1.25.0** in its `go.mod`, and **`go.temporal.io/sdk` v1.42.0 requires Go 1.24.0**. The intersection is **Go 1.25.0+**, and v1.42.0 raised the SDK floor from earlier minor versions, so anyone porting from older Temporal samples needs to bump their toolchain.
2. **`go.starlark.net` has no tagged release** — only pseudo-versions (e.g. `v0.0.0-20260326113308-fadfc96def35`). This is intentional (the upstream Starlark spec is not v1) and means dependency pinning is by commit, not by SemVer.

## Recommended Stack

### Core Technologies

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| **Go** | **1.25.x** (toolchain `go1.26.2` available) | Implementation language, fixed by project | Required minimum is 1.25 (forced by `go.starlark.net`). Use the latest stable patch (`go1.26.2` released 2026) but declare `go 1.25` in `go.mod` so consumers on 1.25 can compile. Pinning to `go1.26` would cut off the still-supported previous release. |
| **`go.starlark.net`** | `v0.0.0-20260326113308-fadfc96def35` (latest pseudo-version, 2026-03-26) | Starlark interpreter — DSL parsing and lambda evaluation | Canonical Google reference implementation of Starlark. The Bazel-team fork (`bazelbuild/starlark`) is the *spec*, not an implementation. There is no other production-grade Go interpreter. |
| **`go.starlark.net/starlarkstruct`** | (same module) | `*starlarkstruct.Struct` for dot-notation state injection | Required by the spec (`ctx.req.repo_name` access pattern). This is an *optional* Starlark extension that ships in the same module — opt-in via `Predeclared` globals. |
| **`go.starlark.net/starlarktest`** | (same module) | Bridge `assert.*` Starlark builtins to `*testing.T` for E2E tier | Provides `LoadAssertModule()`, `SetReporter(thread, t)`, `GetReporter(thread)`. This is the standard way to run `.star` test files and surface failures in `go test`. |
| **`go.temporal.io/sdk`** | **`v1.42.0`** (released 2026-04-08) | Temporal client + workflow + activity APIs | The only official Go SDK. v1.42.0 is current. Note: requires Go 1.24+, raises floor from earlier minors. |
| **`go.temporal.io/sdk/testsuite`** | (same module) | `TestWorkflowEnvironment` / `TestActivityEnvironment` for Tier-3 testing | Bundled with the SDK, no separate dependency. Required for the E2E tier where Starlark `temporal_test` builtins drive mocks. |
| **`go.temporal.io/sdk/workflow`** | (same module) | `workflow.Go`, `workflow.Await`, `workflow.NewSelector`, child workflow primitives | Used by the interpreter for parallel for-each and signal-style coordination. `workflow.Go` is the *only* legal way to spawn concurrent work inside a workflow — native `go` is non-deterministic and forbidden. |

### Supporting Libraries

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| **`github.com/spf13/cobra`** | `v1.10.2` (2025-12-03) | CLI command tree for the example/dev binary | Use for the CLI in the example project. Mature, widely used (~206k dependents), supports nested subcommands (`skytime run`, `skytime parse`, `skytime test`), shell completion, and integrates with cobra's flag system. |
| **`github.com/spf13/pflag`** | `v1.0.9` (transitive via cobra) | POSIX/GNU-style flag parsing | Comes with cobra. No need to declare directly. |
| **`github.com/knadh/koanf/v2`** | `v2.3.4` (2026-03-20) | Config loading (YAML/TOML/env/flags) for CLI | Recommended over Viper. Smaller binary (~3x smaller), fewer transitive deps, doesn't lowercase keys. v2 has detached parsers/providers — install only what you need. Use only if config gets non-trivial; for v1 a simple struct + `flag` may suffice. |
| **`log/slog`** (stdlib) | Go 1.25 stdlib | Structured logging API everywhere in the library | Use `slog` as the *interface* for all logging in the library. Do NOT take a hard dep on a specific backend — accept `*slog.Logger` (or default to `slog.Default()`) so consumers wire their own handler. |
| **`github.com/charmbracelet/log/v2`** | `v2.0.0` (2026-03-09) | Pretty terminal output for the CLI/example binary | Use as the slog *handler* in the CLI only (not in the library). Provides colorized, human-friendly output for `skytime run` etc. The library itself stays handler-agnostic. |
| **`github.com/stretchr/testify`** | `v1.11.1` (2025-08-27) | Assertions + table-driven test helpers | Use `require` for fail-fast preconditions and `assert` for accumulating checks. Avoid `testify/mock` — Temporal's testsuite has its own mocking; mixing both adds noise. |

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| **`golangci-lint`** | `v2.11.4` (2026-03-22) | Multi-linter runner | Pin minor (`v2.11`), let patch float. Enable: `govet`, `staticcheck`, `errcheck`, `gosimple`, `revive`, `unused`, `gocritic`, `misspell`. Add `forbidigo` to ban `panic` in workflow code paths. |
| **`gofumpt`** | (latest) | Stricter `gofmt` superset | Optional but recommended for greenfield. Catches formatting choices `gofmt` allows. |
| **`go test -race`** | stdlib | Race detector for activity/test code | Always run with `-race` in CI. Workflow code is single-threaded by Temporal's deterministic runner so race issues only surface in activities, the interpreter shell, and test harnesses. |
| **`mockgen` / `gomock`** | not recommended | Mock generation | Skip. The Temporal testsuite provides `OnActivity(...).Return(...)` style mocking; project's `temporal_test` builtin will wrap that. Don't introduce gomock. |
| **`Makefile` or `Taskfile.yml`** | — | Build/test/lint orchestration | A simple `Makefile` with `test`, `lint`, `example-run`, `dev-server` targets is sufficient for v1. |

## Installation

```bash
# Initialize module (Go 1.25+ required)
go mod init github.com/<org>/skytime

# Core dependencies
go get go.starlark.net@latest
go get go.temporal.io/sdk@v1.42.0

# Example/CLI dependencies (in cmd/skytime, NOT in the library root)
go get github.com/spf13/cobra@v1.10.2
go get github.com/charmbracelet/log/v2@v2.0.0
go get github.com/knadh/koanf/v2@v2.3.4   # only if config grows beyond flags

# Test dependencies
go get github.com/stretchr/testify@v1.11.1

# Dev tools (install once, not as module deps)
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.11.4
go install mvdan.cc/gofumpt@latest
```

`go.mod` `go` directive should be:

```go
go 1.25
```

(Do NOT set `1.26` — it cuts off the still-supported `1.25` toolchain. The Starlark library requires `1.25` minimum; that floor is what we inherit.)

## Module / Repository Structure

A library + example + CLI in one repo is well-served by Go's standard layout:

```
.
├── go.mod                          // module github.com/<org>/skytime
├── skytime.go                      // public API root: Parse, Compile, Register
├── ast/                            // DAG node types (flow, step, if_cond, ...)
├── parse/                          // .star → DAG (uses go.starlark.net)
├── interpret/                      // DAG → workflow.Context (uses go.temporal.io/sdk/workflow)
│   └── bridge.go                   // starlarkstruct ⇆ workflow state injection
├── extension/                      // ActionRef registry, intent types
├── examples/
│   └── github-slack/
│       ├── extensions/             // HTTP, GitHub, Slack — pure Go funcs
│       ├── flows/                  // *.star files
│       └── main.go                 // worker registration + sample trigger
└── cmd/
    └── skytime/                    // dev CLI (cobra)
        ├── main.go
        └── cmd/
            ├── parse.go            // skytime parse <file.star>
            ├── run.go              // skytime run <file.star>
            ├── test.go             // skytime test <dir>
            └── server.go           // skytime dev-server (Temporalite wrapper)
```

Rationale:
- **Library root has no `cobra` dependency.** Cobra and charm/log are CLI-only — keeping them out of the library root lets downstream importers depend on Skytime without dragging a 3MB CLI tree.
- **`cmd/skytime` is the dogfooding CLI**, and the example project under `examples/` is what the CLI runs against. This matches the spec ("CLI + example project, not standalone binary").

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| `go.starlark.net` (Google) | `bazelbuild/starlark` | Never for execution — that repo holds the language *spec*, not a runtime. Use it only when reading canonical grammar/semantics. |
| `go.starlark.net` | `qri-io/starlib` | If you need pre-built Starlark modules (HTTP, JSON, hashes) for *consultants* to use directly in `.star`. We won't expose this; extensions wrap I/O at the Go layer. |
| `go.starlark.net` | `tilt-dev/starlark-lsp`, `bazelbuild/buildtools` | LSP/formatting tools — not runtime dependencies. Worth recommending to `.star`-authoring consultants but out of scope for the library. |
| `spf13/cobra` | `urfave/cli/v3` | Lighter dep tree, simpler API. Use if the CLI stays small (3-4 commands) and you don't need shell completion or cobra's middleware. We chose cobra for its `PreRun` chain, which simplifies "load config → init logger → connect Temporal client" pipelines. |
| `spf13/cobra` | `kong` (alecthomas/kong) | Struct-tag-based, very ergonomic. Reasonable choice for solo maintainers; cobra has more community/docs. |
| `spf13/cobra` | `charmbracelet/fang` (cobra wrapper) | Cobra + Charm styling. Worth re-evaluating once `fang` stabilizes; today, plain cobra + charm/log handler is a safer combination. |
| `log/slog` (stdlib) | `uber-go/zap` | High-throughput logging hot paths (>50k logs/sec). Skytime's volume is bounded by Temporal history, so slog is fine. |
| `log/slog` (stdlib) | `rs/zerolog` | Same niche as zap; faster but library-specific API. Pick only if benchmarking shows slog is the bottleneck. |
| `knadh/koanf/v2` | `spf13/viper` | Existing codebases that already use viper, or if you specifically need viper's `WatchConfig` hot-reload (deferred per spec — not needed v1). |
| `knadh/koanf/v2` | hand-rolled `flag` + env vars | If the CLI only needs `--temporal-address`, `--namespace`, `--task-queue`, skip a config library entirely. Re-evaluate if config grows. |
| `stretchr/testify` | stdlib `testing` only | Stdlib alone is fine; testify is a quality-of-life gain (table tests with `require.NoError`, `assert.Equal`, etc.). No religious objections either way. |
| `golangci-lint` | `staticcheck` alone | Smaller scope, faster runs, but you lose `errcheck`, `revive`, `gocritic`. Run staticcheck inside golangci-lint instead. |

## What NOT to Use

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| **Embedding CEL (`google/cel-go`)** | Spec explicitly forbids string-based expressions. CEL would re-introduce the parse-time evaluation surface that lambdas eliminate. | Native Starlark lambdas captured at parse time and evaluated inside the workflow. |
| **Custom DSL parsers (yaml-with-templates, HCL, etc.)** | Same reason — strings need their own parser/evaluator with its own bugs and security surface. | Starlark with `starlarkstruct` injection. |
| **`go.temporal.io/sdk/activity` imports inside extensions** | Spec rule: extensions are plain Go functions, never import the activity package. Importing it tempts authors to call `activity.GetInfo()` and breaks the test/extension boundary. | Extensions return `ActionRef` intents; the single generic activity reads `activity.GetInfo()` once at the top. |
| **`go.temporal.io/sdk/temporal` "old" import path** | Pre-1.0 path (`go.temporal.io/temporal`) — deprecated for years, still appears in stale Stack Overflow answers. | Always `go.temporal.io/sdk/...`. |
| **Running native `go` keyword inside workflows** | Non-deterministic, breaks Temporal replay. The Go SDK's deterministic runner only knows about `workflow.Go`. | `workflow.Go(ctx, fn)` for every concurrent branch. |
| **`gogo/protobuf` direct dependencies** | Temporal SDK migrated off gogo in v1.26 (2024). New code mixing gogo and `google.golang.org/protobuf` will hit type-mismatch errors. | `google.golang.org/protobuf` only. The SDK still has a transitive `gogo/protobuf` for legacy paths — leave it transitive, don't import. |
| **`logrus`** | Maintenance mode; no new features. | `log/slog` (stdlib). |
| **`gomock` / `mockery`** | Adds another code-gen step and competes with Temporal testsuite mocks. | Temporal `testsuite.OnActivity(...)` for activities; manual fakes for the few interfaces (credential resolver, extension registry) that need them. |
| **`viper` for v1** | Heavy dep tree, lowercases YAML keys, breaks spec compatibility for nested config. | `koanf/v2` or hand-rolled flag parsing. |
| **Tagged `v0.x.y` for `go.starlark.net`** | There are no tags. Anyone instructing `go get go.starlark.net@v0.1.0` will fail. | Use `@latest` for initial pull, then `go mod tidy` will pin to the pseudo-version (e.g. `v0.0.0-20260326113308-fadfc96def35`). Update intentionally. |
| **`SetTLSDisabled` defaults from old samples** | Temporal SDK v1.39 (Jan 2025) flipped the default: providing an API key now *implies* TLS. Old "TLS off + API key" samples silently break. | Be explicit: set `TLSDisabled: true` only for local dev-server connections; let production paths default to TLS. |
| **`v1.0.0` tag of `charmbracelet/log`** | `v2.0.0` (2026-03) is the slog-native major; `v1` predates the slog stabilization and has API drift. | `github.com/charmbracelet/log/v2` v2.0.0+. |

## Stack Patterns by Variant

**If running locally for development:**
- Use `temporalio/temporalite` (or `temporal server start-dev`) for an in-memory Temporal — no Postgres, no Cassandra, single binary.
- CLI command `skytime dev-server` should wrap `temporalite` lifecycle.
- `TLSDisabled: true` on the Temporal client.

**If targeting Temporal Cloud:**
- API key + `Namespace` from config.
- Do NOT set `TLSDisabled` (default = enabled in v1.39+).
- Set `Identity` to something traceable (e.g. `skytime/<git-sha>`).

**If running under self-hosted Temporal cluster:**
- mTLS certs via `client.Options.ConnectionOptions.TLS`.
- Same SDK code; only `client.Options` differ. Keep client construction in one place (`internal/temporalclient`) so all three variants flow through one factory.

**If consultants want offline `.star` linting:**
- Out of scope for the library, but recommend `bazelbuild/buildtools` `buildifier` to consultants — it formats Starlark and catches obvious syntax issues without invoking the Go interpreter.

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `go.starlark.net@latest` | Go `1.25.0`+ | The module's `go.mod` pins `go 1.25.0`. Older toolchains will not compile it. |
| `go.temporal.io/sdk@v1.42.0` | Go `1.24.0`+ | v1.42 raised the floor from 1.22. Project lands at Go 1.25 anyway because of Starlark. |
| `go.temporal.io/sdk@v1.42.0` | Temporal Server `1.31.0`+ | v1.41 enabled Worker Heartbeating (server `1.29.1`+) and Nexus error serialization (server `1.31.0`+). For Temporal Cloud, this is automatic. For self-hosted, document the server floor in README. |
| `go.temporal.io/sdk@v1.42.0` | `google.golang.org/grpc v1.79.3`, `google.golang.org/protobuf v1.36.11` | These are SDK transitives. Do not pin lower versions in `go.mod` `replace` — you'll fight the SDK's interface contracts. |
| `spf13/cobra@v1.10.2` | `spf13/pflag@v1.0.9` | Cobra v1.10 requires pflag v1.0.9; older pflag breaks compile. `go mod tidy` resolves automatically. |
| `charmbracelet/log/v2@v2.0.0` | Go `1.21+` (slog requirement) | v2 is slog-native. Use `log.NewWithOptions(...)` then `slog.New(logger)` to wire. |
| `golangci-lint@v2.11.4` | Go `1.25` and `1.26` | Linter supports the latest two minor Go releases. Pinning to the project's `go 1.25` directive keeps both supported. |
| `knadh/koanf/v2@v2.3.4` | Detached providers/parsers | Each provider (`koanf/providers/file`, `koanf/parsers/yaml`, etc.) is its own module — `go get` them individually. |

## Confidence Notes by Item

| Recommendation | Confidence | Verification |
|----------------|------------|--------------|
| Go 1.25 minimum | HIGH | Verified against `go.starlark.net/go.mod` (`go 1.25.0`) via raw GitHub fetch. |
| `go.temporal.io/sdk` v1.42.0 | HIGH | Verified via `proxy.golang.org/go.temporal.io/sdk/@latest` — `v1.42.0`, 2026-04-08. |
| `go.starlark.net` pseudo-version | HIGH | Verified via Go module proxy; no tagged releases ever. |
| `spf13/cobra` v1.10.2 | HIGH | Verified via Go module proxy; latest tagged release 2025-12-03. |
| `stretchr/testify` v1.11.1 | HIGH | Verified via Go module proxy; 2025-08-27. |
| `knadh/koanf/v2` v2.3.4 | HIGH | Verified via Go module proxy; 2026-03-20. |
| `charmbracelet/log/v2` v2.0.0 | HIGH | Verified via Go module proxy; 2026-03-09. |
| `golangci-lint` v2.11.4 | HIGH | Verified via official site + Go module proxy; 2026-03-22. |
| Temporal v1.42 raised Go floor to 1.24 | HIGH | Confirmed in release notes (v1.42.0). |
| Temporal v1.39 TLS-by-default with API key | MEDIUM | From release notes summary; recommend re-reading the v1.39 changelog before flipping production traffic. |
| `koanf` over `viper` | MEDIUM | Multiple sources agree on koanf's lighter footprint; viper is still the more popular choice and either works. Decision is stylistic + dep-weight, not correctness. |
| `slog` over `zap`/`zerolog` | MEDIUM | Ecosystem consensus for *new* Go libraries in 2026, but performance-critical paths can swap handlers. |
| `cobra` over `urfave/cli` | MEDIUM | Both are reasonable; cobra wins on PreRun chains and ecosystem reach for our CLI shape. |

## Sources

- [pkg.go.dev: go.temporal.io/sdk](https://pkg.go.dev/go.temporal.io/sdk) — current SDK version
- [GitHub: temporalio/sdk-go releases](https://github.com/temporalio/sdk-go/releases) — release notes for v1.39 → v1.42
- [pkg.go.dev: go.starlark.net](https://pkg.go.dev/go.starlark.net) — module structure, import paths
- [pkg.go.dev: go.starlark.net/starlarktest](https://pkg.go.dev/go.starlark.net/starlarktest) — test bridge API
- [pkg.go.dev: go.starlark.net/starlarkstruct](https://pkg.go.dev/go.starlark.net/starlarkstruct) — struct/module types
- [GitHub: google/starlark-go go.mod](https://github.com/google/starlark-go/blob/master/go.mod) — Go 1.25 floor
- [GitHub: temporalio/sdk-go go.mod](https://github.com/temporalio/sdk-go/blob/master/go.mod) — Go 1.24 floor, gRPC/protobuf versions
- [Temporal docs: Go SDK multithreading](https://docs.temporal.io/develop/go/go-sdk-multithreading) — `workflow.Go` semantics
- [Temporal docs: Go SDK testing suite](https://docs.temporal.io/develop/go/testing-suite) — `TestWorkflowEnvironment`/`TestActivityEnvironment`
- [Temporal docs: Selectors](https://docs.temporal.io/develop/go/workflows/selectors) — `workflow.Await`, `workflow.NewSelector`
- [Go module proxy: go.temporal.io/sdk@latest](https://proxy.golang.org/go.temporal.io/sdk/@latest) — version verification
- [Go module proxy: go.starlark.net@latest](https://proxy.golang.org/go.starlark.net/@latest) — version verification
- [Go module proxy: github.com/spf13/cobra@latest](https://proxy.golang.org/github.com/spf13/cobra/@latest) — v1.10.2
- [Go module proxy: github.com/stretchr/testify@latest](https://proxy.golang.org/github.com/stretchr/testify/@latest) — v1.11.1
- [Go module proxy: github.com/knadh/koanf/v2@latest](https://proxy.golang.org/github.com/knadh/koanf/v2/@latest) — v2.3.4
- [Go module proxy: github.com/charmbracelet/log/v2@latest](https://proxy.golang.org/github.com/charmbracelet/log/v2/@latest) — v2.0.0
- [Go module proxy: golangci-lint v2@latest](https://proxy.golang.org/github.com/golangci/golangci-lint/v2/@latest) — v2.11.4
- [go.dev/dl JSON](https://go.dev/dl/?mode=json) — Go 1.26.2 latest stable, 1.25 line still supported
- [Dash0: Go logging libraries 2026](https://www.dash0.com/guides/golang-logging-libraries) — slog as default recommendation
- [Koanf vs Viper comparison](https://github.com/knadh/koanf/wiki/Comparison-with-spf13-viper) — dependency / binary-size analysis
- [golangci-lint install docs](https://golangci-lint.run/docs/welcome/install/) — v2.11.4, Go-version policy

---
*Stack research for: Go library — Starlark DSL + Temporal orchestration + dev CLI*
*Researched: 2026-04-26*
