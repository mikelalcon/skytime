# Phase 4: Static Validation Tier + CLI Skeleton - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-01
**Phase:** 04-static-validation-tier-cli-skeleton
**Areas discussed:** Validator architecture, skytime run execution model, skytime dev-server strategy, CLI extensibility, Corpus

---

## Gray Area Selection (entry gate)

| Option | Description | Selected |
|--------|-------------|----------|
| Validator architecture | New pkg/validator wrapper vs filling parser.finalize stub. Plus what 'declared state' means. | ✓ |
| skytime run execution model | Embedded transient worker vs trigger-only. Plus progress streaming. | ✓ |
| skytime dev-server strategy | Embed temporalite vs shell-out vs document-only. Resolves Phase 3 conflict. | ✓ |
| CLI extensibility | Bake-in vs reusable pkg/cli for Phase 6's custom binary. | ✓ |

**User's choice:** All four areas selected.

---

## Corpus

| Option | Description | Selected |
|--------|-------------|----------|
| Bootstrap minimal examples/ (Recommended) | Phase 4 lands tiny examples/skeleton/ with 2-3 flows; Phase 6 fleshes out. | ✓ |
| Reuse tests/fixtures/valid/ | Run differential against Phase 1 fixtures. ROADMAP says 'examples/'. | |
| Defer corpus test to Phase 6 | Phase 4 ships validator + dry-run seam only. | |

**User's choice:** Bootstrap minimal `examples/skeleton/`.

---

## Area 1: Validator Architecture

### Q1.1 — Where should the new static validation logic live?

| Option | Description | Selected |
|--------|-------------|----------|
| Both: finalize + thin pkg/validator (Recommended) | New checks in parser/finalize.go alongside existing lints. pkg/validator is a thin facade exposing Validate() + the dry-run interpreter seam. | ✓ |
| Fill in parser.finalize only | Extend validateActionRefKwargs no-op stub with all new checks. CLI imports parser directly. | |
| Standalone pkg/validator owning new checks | All new checks in pkg/validator; parser.finalize stays at current scope. | |

**User's choice:** Both: finalize + thin pkg/validator.

### Q1.2 — VAL-01 'every lambda's free variables reference declared state' — what does the check verify concretely?

| Option | Description | Selected |
|--------|-------------|----------|
| ctx.X attribute access vs schema (Recommended) | Scan each lambda's AST for ctx.<name> accesses; verify <name> exists in lexically-visible state schema (flow inputs + script outputs upstream + for_each item binding). | ✓ |
| Starlark free-vars only (current behavior) | Keep the Phase 1 D-19 check as-is. VAL-01 satisfied at Starlark-language level. | |
| Both layers | Keep D-19 plus add ctx.<name> attribute check. | |

**User's choice:** ctx.X attribute access vs schema.
**Notes:** Phase 1's D-19 free-var lint is Starlark-language-level (binding scope). VAL-01's "declared state" check is the user-domain meaning: dot-paths inside lambdas must resolve to real state names. Both checks coexist (D-19 stays; the new check layers on top).

### Q1.3 — VAL-02's 'dry-run interpreter (all actions mocked)' — what shape should that seam take?

| Option | Description | Selected |
|--------|-------------|----------|
| Test-only mock dispatch (Recommended) | Helper that swaps real OperationDispatch for 'always-OkResult' stubs. Differential test = Go test. No CLI exposure. Composes with Phase 5. | ✓ |
| CLI flag --dry-run on skytime run | User-facing flag. Exposes seam as a feature. | |
| Library API pkg/validator.Dryrun(flow, mocks) | Validator exposes Dryrun function; CLI and tests both call it. | |

**User's choice:** Test-only mock dispatch.

### Q1.4 — Adding action attribution to ValidationError for `[flow > step > action]` format?

| Option | Description | Selected |
|--------|-------------|----------|
| Extend ValidationError with Action (Recommended) | Add Action string to dag.ValidationError; update Error() to include `[flow > step > action]` when fields non-empty. | ✓ |
| CLI-side composition only | Leave ValidationError as-is; CLI's renderer composes the bracket. | |
| Generalize to []string Path | Replace Flow/Step strings with Path []string for future scopes. Breaks backward compatibility. | |

**User's choice:** Extend ValidationError with Action field.

---

## Area 2: skytime run Execution Model

### Q2.1 — How does `skytime run` execute the workflow?

| Option | Description | Selected |
|--------|-------------|----------|
| Embedded transient worker (Recommended) | CLI starts worker in-process; calls ExecuteWorkflow; follows; shuts down. Self-contained for demos. | ✓ |
| Trigger-only (separate worker required) | CLI just calls client.ExecuteWorkflow; assumes worker is already running. | |
| Both via flag | Default = embedded; --remote = trigger-only. | |

**User's choice:** Embedded transient worker.

### Q2.2 — What does 'streams progress' mean?

| Option | Description | Selected |
|--------|-------------|----------|
| Per-step CLI output + final result (Recommended) | Print each Step start/finish; activity dispatches; tail print() output; final result. Mid-weight: feels live without being noisy. | ✓ |
| Just final result | Block on ExecuteWorkflow.Get; print final state. Simplest. | |
| Full Temporal event history | Poll history; render every event live. Most thorough; noisy. | |

**User's choice:** Per-step CLI output + final result.

### Q2.3 — How does --input=<json> get validated?

| Option | Description | Selected |
|--------|-------------|----------|
| Same validator as static (Recommended) | Same input-schema check the validator uses. Single source of truth. | ✓ |
| Trust JSON, let workflow fail at runtime | Pass JSON straight into InitState; let workflow blow up. | |
| Strict typed-struct unmarshalling (later) | Defer to v1.x once schemas have richer types. | |

**User's choice:** Same validator as static.

### Q2.4 — How does `skytime run` discover the Temporal cluster?

| Option | Description | Selected |
|--------|-------------|----------|
| Flags + env vars (Recommended) | --address, --namespace, --api-key flags + SKYTIME_TEMPORAL_* env fallbacks. Variant routing by which flags are present. | ✓ |
| Config file + flags (koanf) | ~/.skytime.yaml loaded via koanf; merged with flag overrides. | |
| Hardcoded dev-only | v1 talks only to localhost:7233; cloud + mtls deferred. | |

**User's choice:** Flags + env vars.

---

## Area 3: skytime dev-server Strategy

### Q3.1 — Resolution of the Phase 3 vs Phase 4 conflict on dev-server spawning?

| Option | Description | Selected |
|--------|-------------|----------|
| Shell out to `temporal server start-dev` (Recommended) | Subprocess wrapper; require user to have temporal CLI installed. No heavy Go dep. Updates Phase 3 note: spawning IS in v1, but as subprocess. | ✓ |
| Embed temporalite as a Go dep | Pull temporalite into CLI binary. Single-binary UX, multi-MB transitive deps. | |
| Connect-only / document | Just a connectivity check; user runs `temporal server start-dev` themselves. | |

**User's choice:** Shell out to `temporal server start-dev`.
**Notes:** This decision (D4-09) explicitly supersedes the Phase 3 CONTEXT.md `<specifics>` note that said "we don't spawn the dev server in-process for v1". The original intent (avoid Temporalite Go dep) is preserved; we spawn AS A SUBPROCESS.

### Q3.2 — Lifecycle?

| Option | Description | Selected |
|--------|-------------|----------|
| Foreground, Ctrl-C drains (Recommended) | Subprocess attached to terminal; SIGINT forwarded; exits when subprocess exits. Familiar Unix idiom. | ✓ |
| Background daemon + start/stop subcommands | Detached subprocess; PID file management. | |
| Foreground only, no signal forwarding | Just spawn and wait. | |

**User's choice:** Foreground, Ctrl-C drains.

### Q3.3 — Default configuration?

| Option | Description | Selected |
|--------|-------------|----------|
| Match `temporal server start-dev` defaults (Recommended) | :7233 / :8233 / namespace=default. Pass-through user flags. | ✓ |
| Skytime-opinionated defaults | Force specific namespace; pre-create search attributes. | |
| Bare wrapper (no flag exposure) | No flag pass-through. | |

**User's choice:** Match `temporal server start-dev` defaults.

### Q3.4 — Missing temporal CLI?

| Option | Description | Selected |
|--------|-------------|----------|
| Clear error + install instructions (Recommended) | Detect missing binary; print actionable error with install commands. | ✓ |
| Auto-install on first run | Download official binary into ~/.skytime/bin/. | |
| Just fail with default OS error | Whatever exec.LookPath returns. | |

**User's choice:** Clear error + install instructions.

---

## Area 4: CLI Extensibility

### Q4.1 — How do user extensions reach the CLI's validate/run commands?

| Option | Description | Selected |
|--------|-------------|----------|
| Reusable pkg/cli + cmd/skytime is thin (Recommended) | pkg/cli exposes NewRootCommand(opts). cmd/skytime is thin wrapper. Phase 6's example builds custom binary. AST firewall extends to {cmd/skytime, pkg/cli}. | ✓ |
| All cobra in cmd/skytime, no reusable package | Cobra subcommand files in cmd/skytime/cmd/*.go. Users copy/fork. | |
| internal/cli (private) | Reusable but unexportable. | |

**User's choice:** Reusable pkg/cli + cmd/skytime is thin.

### Q4.2 — Phase 4 cmd/skytime extensions?

| Option | Description | Selected |
|--------|-------------|----------|
| Empty by default; flag-driven test fixtures | cmd/skytime ships with NO real extensions. Hidden --test-extensions flag. | |
| Bake in a generic HTTP extension (chosen by user) | cmd/skytime ships an http extension out of the box. Lets users get to a runnable demo. | ✓ |
| Bake in HTTP + GitHub + Slack (Phase 6 forward-port) | Move Phase 6's three extensions into a shared package and register. | |

**User's choice:** Bake in a generic HTTP extension.
**Notes:** Diverged from the recommended option. Useful divergence: Phase 4 ships a runnable demo without waiting for Phase 6. The HTTP extension in Phase 4 is the lowest-common-denominator (Go stdlib net/http only); Phase 6's HTTP extension may be richer. D4-14 specifies the canonical operation set.

### Q4.3 — How does NewRootCommand receive the extensions list?

| Option | Description | Selected |
|--------|-------------|----------|
| Functional options (Recommended) | NewRootCommand(opts ...Option). Matches Parser/Worker patterns. | ✓ |
| Required struct argument | NewRootCommand(cfg Config) struct of all options. | |
| Global init-style registration | Imports register via global registry. Violates D-07. | |

**User's choice:** Functional options.

### Q4.4 — What does `skytime validate` do with no extensions registered against a flow that references one?

| Option | Description | Selected |
|--------|-------------|----------|
| Fail with clear 'extension not registered' (Recommended) | Same parser behavior; CLI prints actionable hint. No special mode. | ✓ |
| Add `--syntax-only` flag | Skip extension-dependent checks. Useful for early authoring. | |
| Auto-stub unknown extensions | Treat unknown as opaque. Risk: false negatives. | |

**User's choice:** Fail with clear 'extension not registered'.

---

## Claude's Discretion

Areas where the user did not specify a particular choice — Claude has flexibility during planning:

- Exact path of the baked-in HTTP extension package
- Cobra subcommand file layout under pkg/cli/
- Slog handler shim implementation for progress streaming
- Test helper location for the dry-run interpreter mock
- Charm-log rendering options
- Whether to extend the schema declaration shape for `flow(inputs={...})` (currently dict literal; Phase 4 may keep or extend)
- AST visitor implementation for the `ctx.<name>` walker

---

## Deferred Ideas

Captured during discussion, deferred to v1.x or v2:

- Embed Temporalite as Go dep (D4-09 supersedes; deferred re-evaluation)
- koanf config file (D4-08; flags + env sufficient for v1)
- Auto-install of `temporal` CLI (D4-12; out of scope for v1)
- Background daemon `dev-server start/stop/status` (D4-10; foreground only in v1)
- User-facing `--dry-run` flag on `skytime run` (D4-03; test seam only in v1)
- Full Temporal event-history dump (D4-06; per-step only in v1)
- `--syntax-only` validate mode (D4-16; out of scope for v1)
- Auto-stub unknown extensions (D4-16; not recommended)
- JSON / structured error output (`--format=json`) for CI consumption (D4-18 ships text-only)
- Cross-flow dataflow analysis (lambda-output verification beyond ctx.X accesses)
- charmbracelet/fang cobra wrapper (re-evaluate when it stabilizes)
- Tier-2 unit tests for `def` blocks (TEST-V2-01)
