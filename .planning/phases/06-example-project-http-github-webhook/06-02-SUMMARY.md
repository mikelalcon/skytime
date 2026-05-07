---
phase: 06-example-project-http-github-webhook
plan: 02
subsystem: pkg/extension/credfile
tags:
  - credentials
  - extension
  - toml
  - resolver
  - library
requirements:
  - EX-03
  - EX-04
dependency-graph:
  requires:
    - 06-01 (Wave-0 scaffolding — pelletier/go-toml/v2 dep, examples/http-github-webhook directory tree)
    - pkg/extension/credential.go (sealed Credential types: BearerCredential, BasicCredential, APIKeyCredential)
    - pkg/extension/handler.go (CredentialHandler interface + ErrUnknownCredential sentinel)
    - pkg/extension/secret.go (NewSecret constructor; Reveal as the only unwrap path)
    - pkg/activity/classify.go (downstream ErrUnknownCredential → NonRetryable contract)
  provides:
    - "credfile.New(opts ...Option) (*Resolver, error) — TOML-backed extension.CredentialHandler"
    - "credfile.WithPath(p string) Option — overrides default $HOME/.skytime-credentials"
    - "credfile.WithStrictMode() Option — refuse to load world/group-readable credfiles"
    - "credfile.WithLogger(l *slog.Logger) Option — override slog.Default for the file-mode warn"
    - "credfile.Resolver.Path() string — diagnostic accessor for consumer binaries"
  affects:
    - 06-05 (cmd/extbin) — will wire credfile.New() via cli.WithCredentialHandler
    - docs/for-extension-developers/README.md — will document the resolver in 06-08/09 docs wave
tech-stack:
  added:
    - "github.com/pelletier/go-toml/v2 (already on the require list — Wave 0 brought it in)"
  patterns:
    - "Functional options on a private *config struct (mirrors pkg/cli/options.go)"
    - "Compile-time interface check via blank-identifier var (var _ Iface = (*T)(nil))"
    - "Error wrapping: %w around extension.ErrUnknownCredential for retry classification"
    - "Sealed credential construction via struct literals + extension.NewSecret"
    - "POSIX-only file-mode check guarded by runtime.GOOS != \"windows\" (Pitfall 4)"
key-files:
  created:
    - path: pkg/extension/credfile/doc.go
      lines: 35
      role: Package documentation — schema, default path, security note
    - path: pkg/extension/credfile/options.go
      lines: 43
      role: Functional options (WithPath / WithStrictMode / WithLogger) + private config
    - path: pkg/extension/credfile/file.go
      lines: 76
      role: TOML schema (fileShape + credentialEntry) + buildCredentials sealed-type constructor
    - path: pkg/extension/credfile/resolver.go
      lines: 105
      role: Resolver type, New(opts) constructor, Resolve(ctx, id) implementation
    - path: pkg/extension/credfile/resolver_test.go
      lines: 329
      role: 12 top-level tests + 8 table sub-tests (19 results, all pass under -race)
  modified: []
decisions:
  - "Used a single TestResolver_RejectsMalformed_TableDriven for the malformed-input matrix (8 sub-cases) plus a discrete TestResolver_MissingTypeField_Errors top-level case to satisfy the PLAN.md acceptance grep `(MissingTypeField_Errors|RejectsMalformed_TableDriven)` and to give targeted -run access to that path."
  - "Test path-attribution assertion: every malformed-file test now asserts the file path appears in the error message (consistent with the `credfile %s: %w` and `parse %s: %w` wrapping in resolver.go), so multi-entry credfiles remain debuggable."
  - "TestResolver_DefaultPath also sets USERPROFILE under Windows so os.UserHomeDir resolves into the temp dir on either OS family — the test no longer skips on Windows."
metrics:
  duration: "3 min"
  completed: 2026-05-07
---

# Phase 06 Plan 02: credfile package — TOML-backed credential resolver Summary

A library-tier `pkg/extension/credfile/` package that parses a TOML credentials file (default `$HOME/.skytime-credentials`) and serves sealed `extension.Credential` values via the existing `extension.CredentialHandler` contract — reusable by any consultant building a custom Skytime binary, not example-only.

## What Changed

**Package created:** `pkg/extension/credfile/` (sibling of `pkg/extension/credential.go` and `pkg/extension/handler.go`, per **D-CREDS-LIB**). Five Go files, 588 total lines:

- `doc.go` — package docs: TOML schema, default path, security note (file-mode policy + redacted secrets).
- `options.go` — functional options on a private `config` struct: `WithPath`, `WithStrictMode`, `WithLogger`. `WithPath("")` falls back to default for env-var → option translation.
- `file.go` — `fileShape` (top-level `[credentials.<id>]` map) and `credentialEntry` (per-row, type-tagged). `buildCredentials(raw fileShape) (map[string]extension.Credential, error)` constructs sealed `*BearerCredential` / `*BasicCredential` / `*APIKeyCredential` with secrets wrapped via `extension.NewSecret`. Per-entry validation rejects empty `type`, unknown `type`, and missing required fields; the credential ID is quoted in every error message.
- `resolver.go` — `Resolver` struct, `New(opts ...Option)` constructor, `Resolve(ctx, id)` method. Loads the file once at construction (D-CREDS-PATH: not reloaded per-Resolve). POSIX file-mode check (`info.Mode().Perm() & 0o044 != 0`) emits `slog.Warn` by default; `WithStrictMode()` makes it a hard error. The check is skipped entirely on Windows per Research Pitfall 4. Compile-time interface check pins conformance to `extension.CredentialHandler`.
- `resolver_test.go` — 19 test results, all green under `-race`: 3 happy-path cases (one per sealed kind), 1 unknown-ID case proving `errors.Is(err, extension.ErrUnknownCredential)`, 1 missing-file case proving `errors.As(err, &*fs.PathError{})`, 1 table-driven malformed-input matrix (8 sub-cases: TOML parse + 7 per-type validation paths), 1 discrete missing-`type` case, 2 file-mode cases (warn / strict-refuse, both POSIX-skipped on Windows), and 3 path-resolution cases (`$HOME` default, `WithPath` override, empty-string fallback).

## Tasks Completed

| Task | Name                                                                          | Commit    | Files                                          |
| ---- | ----------------------------------------------------------------------------- | --------- | ---------------------------------------------- |
| 1    | Scaffold credfile package — doc.go, options.go, file.go (no Resolver yet)     | `b6a2140` | doc.go, options.go, file.go                    |
| 2    | Implement Resolver — extension.CredentialHandler over the TOML credfile        | `d1d1f37` | resolver.go                                    |
| 3    | Table-driven test coverage for Resolver — 12 top-level + 8 table sub-tests     | `f7a1daf` | resolver_test.go                               |

## Test Status

```
go test -race -count=1 ./pkg/extension/credfile/...
ok  	github.com/mikelalcon/skytime/pkg/extension/credfile	1.277s
```

19 test results pass, no skips on macOS:

- `TestResolver_HappyPath_BearerCredential`
- `TestResolver_HappyPath_BasicCredential`
- `TestResolver_HappyPath_APIKeyCredential`
- `TestResolver_UnknownID_WrapsErrUnknownCredential`
- `TestResolver_MissingFile_ReturnsPathError`
- `TestResolver_RejectsMalformed_TableDriven` (+ 8 sub-tests: MalformedTOML, MissingTypeField, UnknownType, BearerMissingToken, BasicMissingUsername, BasicMissingPassword, APIKeyMissingKey, APIKeyMissingValue)
- `TestResolver_MissingTypeField_Errors`
- `TestResolver_WorldReadable_WarnsByDefault`
- `TestResolver_WorldReadable_WithStrictMode_Refuses`
- `TestResolver_DefaultPath`
- `TestResolver_WithPathOverrides`
- `TestResolver_WithPath_EmptyStringFallsBackToDefault`

Full project regression check (`go build ./...`) green.

## EX-03 / EX-04 Coverage

- **EX-03 (credential resolver):** Mechanically satisfied for the file-based variant. `credfile.Resolver` implements `extension.CredentialHandler` (compile-time check). Three sealed credential kinds round-trip through TOML: `bearer` → `*BearerCredential`, `basic` → `*BasicCredential`, `apikey` → `*APIKeyCredential`. Secrets are wrapped via `extension.NewSecret` so every fmt/json/slog path redacts (verified by the existing `pkg/extension/secret_test.go`).
- **EX-04 (authenticated walkthrough):** Unblocked. The README's second-stage walkthrough can now point readers at `~/.skytime-credentials` with a documented schema; the binary at `examples/http-github-webhook/cmd/extbin/main.go` (06-05) will wire `cli.WithCredentialHandler(credfile.New())` to consume it.

## Hand-off to 06-05 (cmd/extbin)

The custom binary in 06-05 will:

```go
resolver, err := credfile.New() // or credfile.WithPath(os.Getenv("SKYTIME_CREDFILE"))
if err != nil { /* surface to user */ }
cmd := cli.NewRootCommand(
    cli.WithExtensions(skyhttp.New(), github.New(), webhook.New()),
    cli.WithCredentialHandler(resolver),
)
```

The empty-string fallback on `WithPath` is intentionally there so 06-05 can write `credfile.WithPath(os.Getenv("SKYTIME_CREDFILE"))` without an explicit conditional.

## Deviations from Plan

### Auto-fixed Issues

None — no Rule 1/2/3 deviations needed during execution.

### Implementation Choices Within Plan Latitude

1. **Test layout (PLAN.md left layout open between 15 discrete cases or a table-driven equivalent):** Chose hybrid — 12 discrete top-level functions for readable failure output on the cases that have heterogeneous shapes (happy path × 3 kinds, file-mode × 2, path resolution × 3, etc.) plus a single `TestResolver_RejectsMalformed_TableDriven` with 8 sub-tests for the homogeneous "feed bad TOML, assert error substrings" matrix. Kept the discrete `TestResolver_MissingTypeField_Errors` top-level so the PLAN.md grep alternation `(MissingTypeField_Errors|RejectsMalformed_TableDriven)` matches both halves.
2. **Path attribution in malformed-input asserts:** Added `assert.Contains(t, err.Error(), path)` to every table-driven case (not just the unknown-ID test). Justified by the `credfile %s: %w` and `parse %s: %w` wrapping in resolver.go — multi-entry credfiles need this attribution so users can find the offending file.
3. **`TestResolver_DefaultPath` runs on Windows too:** Plan-suggested skeleton noted "also set USERPROFILE for Windows portability if needed"; took that path so the test doesn't skip on Windows. `os.UserHomeDir` reads `USERPROFILE` first on Windows, `HOME` first on POSIX; setting both makes the test cross-platform.

### Auth Gates

None — this is a pure library plan, no human action needed.

## Self-Check: PASSED

**Files exist:**
- FOUND: pkg/extension/credfile/doc.go (35 lines)
- FOUND: pkg/extension/credfile/options.go (43 lines)
- FOUND: pkg/extension/credfile/file.go (76 lines)
- FOUND: pkg/extension/credfile/resolver.go (105 lines)
- FOUND: pkg/extension/credfile/resolver_test.go (329 lines)

**Commits exist:**
- FOUND: b6a2140 — feat(06-02): scaffold credfile package
- FOUND: d1d1f37 — feat(06-02): implement Resolver
- FOUND: f7a1daf — test(06-02): table-driven coverage for credfile.Resolver

**Build + test gates:**
- `go build ./pkg/extension/credfile/...` → OK
- `go vet ./pkg/extension/credfile/...` → OK
- `go test -race -count=1 ./pkg/extension/credfile/...` → ok 1.277s
- `go build ./...` → OK (no project-wide regressions)
- `grep -q 'var _ extension.CredentialHandler = (\*Resolver)(nil)' pkg/extension/credfile/resolver.go` → OK

## Known Stubs

None. Every code path in `resolver.go` and `file.go` is exercised by at least one assertion. No placeholder values, no hardcoded mock data, no "coming soon" markers. The package is consultant-ready and 06-05 can wire it directly.
