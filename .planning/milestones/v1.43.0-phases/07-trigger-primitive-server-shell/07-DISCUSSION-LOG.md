# Phase 7: Trigger primitive + server shell - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-08
**Phase:** 07-trigger-primitive-server-shell
**Areas discussed:** Trigger lambda contract, TriggerSource Go-side semantics, Trigger registry shape & boot integration, Server lifecycle on SIGTERM/SIGINT

---

## Trigger lambda contract

### Predeclared globals for map / idempotency_key lambdas

| Option | Description | Selected |
|--------|-------------|----------|
| Locked 20-key lambdaTimeGlobals | Same strict subset workflow lambdas get | |
| Locked 20-key + json/time | Add json.* and time.now() (run at ingress, not in replay) | ✓ |
| Distinct triggerTimeGlobals (subset) | Define a new locked set tailored for trigger ingress | |

**User's choice:** Locked 20-key + json/time
**Notes:** Trigger lambdas run once at HTTP ingress so non-deterministic globals are safe; useful for parsing JSON bodies and stamping arrival timestamps without going through a workflow step. New `triggerTimeGlobals` constant will extend `lambdaTimeGlobals` with these two additions.

### Payload injection model

| Option | Description | Selected |
|--------|-------------|----------|
| *starlarkstruct.Struct | Same dot-notation as ctx (Phase 1) | ✓ |
| Plain *starlark.Dict | Bracket access only | |
| Both: payload (Struct) + raw_body (str) | Default to Struct; expose raw bytes for non-JSON | |

**User's choice:** *starlarkstruct.Struct
**Notes:** Reuses ctx-style ergonomics consultants already learned. Recursive Go-map → Struct conversion ships in pkg/bridge.

### idempotency_key signature

| Option | Description | Selected |
|--------|-------------|----------|
| lambda payload, headers | Two positionals (v1.43-DRAFT-PLAN recommendation) | |
| lambda req: req.payload + req.headers | Single composite | ✓ |
| lambda payload, headers, source_kind | Three positionals | |

**User's choice:** lambda req (single positional with subfields)
**Notes:** User feedback: "I think for gh one lambda req with payload and header as subfield makes sense. This will depend on the type of trigger. Some will have different config and data available to both lambdas." → req shape is source-specific; HTTP-shaped sources expose req.payload + req.headers; cron (later) exposes req.scheduled_time + req.workflow_attempt; each TriggerSource declares its own ReqSchema().

### Determinism requirement for map lambda

| Option | Description | Selected |
|--------|-------------|----------|
| No determinism requirement | Map runs ONCE at ingress, before ExecuteWorkflow | ✓ |
| Deterministic-by-default; document carve-out | Treat trigger lambdas like workflow lambdas | |

**User's choice:** No determinism requirement
**Notes:** Map result becomes the workflow input — frozen at that point — so non-determinism is observably safe. Document explicitly in pkg/parser/doc.go.

### Map lambda signature symmetry with idempotency_key

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — both lambdas take req | Symmetric single-positional req | ✓ |
| Asymmetric — map(payload), idempotency_key(req) | map only sees payload | |
| Both forms accepted | Parser accepts either form | |

**User's choice:** Yes — both lambdas take req
**Notes:** Both lambdas take a single source-specific req. Deviates from ROADMAP success-criterion-1 illustrative example (lambda payload, headers); illustrative interpretation locked here.

### Parse-time lambda validation depth

| Option | Description | Selected |
|--------|-------------|----------|
| Phase 1 lint reused + arity check | Free-var lint + arity check; no body-level type inference | |
| Lint + ctx-style attribute walk | Reuse D4-02 ctx.<name> visitor for req.<field> validation | ✓ |
| Lint only (defer attribute validation to runtime) | Free-var lint + arity is enough | |

**User's choice:** Lint + ctx-style attribute walk
**Notes:** Reuses Phase 4's D4-02 visitor pattern (cached-file-bytes re-parse). Each TriggerSource declares ReqSchema(); typos produce position-aware errors with valid-field list.

---

## TriggerSource Go-side semantics

### TriggerSource Go shape

| Option | Description | Selected |
|--------|-------------|----------|
| Sealed marker interface | type TriggerSource interface { triggerSourceMarker() } | ✓ |
| Interface with capability methods | Polymorphic Kind()/HTTPMounts()/CronSpec() | |
| Opaque any with kind discriminator | type TriggerSource struct { Kind string; Payload any } | |

**User's choice:** Sealed marker interface
**Notes:** Mirrors dag.Node's seal pattern (nodeMarker()). Concrete types live in source extensions; runtime type-switches in 7.1+.

### Package layout

| Option | Description | Selected |
|--------|-------------|----------|
| pkg/extension/trigger.go | Same package as Extension and ActionRef | ✓ |
| pkg/extension/triggersource/ | Sub-package for room to grow | |
| pkg/dag/trigger.go (alongside dag.Trigger) | Co-located with the DAG node consuming it | |

**User's choice:** pkg/extension/trigger.go
**Notes:** Source factories sit alongside operation factories as parallel SDK shapes.

### Source factory call surface in .star

| Option | Description | Selected |
|--------|-------------|----------|
| Top-level extension namespace | triggers.github_webhook(events=...) | |
| Bare globals | github_webhook(...) | |
| Per-source extension | Each source registers its own namespace | |
| Owning-extension namespace (user override) | github.webhook(...) — same namespace as github.create_issue | ✓ |

**User's choice:** Owning-extension namespace
**Notes:** User feedback: "I think the github trigger NEEDS to be in the same namespace as the rest of the GH stuff we have." → DEVIATION from REQUIREMENTS.md TRIG-07/08 wording (which says triggers.github_webhook). Each extension owns its trigger sources end-to-end. REQUIREMENTS.md wording will be updated during planning.

### dag.Trigger.Source JSON shape

| Option | Description | Selected |
|--------|-------------|----------|
| { "kind": "...", "config": {...} } | Two-field discriminated form | ✓ |
| Embedded source value (each field at top level) | Flatter JSON | |
| Opaque base64 blob | Encode with gob/protobuf | |

**User's choice:** { "kind", "config" } two-field
**Notes:** User emphasis (verbatim): "But DO NOT EVER EVER serialize a credential and put it in Temporal. Like that is forbidden. I cannot say loud enough." → CRITICAL constraint added to CONTEXT.md key_constraints section: credentials never reach the Trigger node, the registry, Temporal history, logs, or error rendering. Source.config carries only credential ID strings.

---

## Trigger registry shape & boot integration

### Registry shape

| Option | Description | Selected |
|--------|-------------|----------|
| New sibling TriggerRegistry | bootRegistry returns (*FlowRegistry, *TriggerRegistry, error) | ✓ |
| Extend FlowRegistry with triggers | Add Triggers []*dag.Trigger to ParsedFlow | |
| Single composite ParsedRegistry | type ParsedRegistry struct { Flows; Triggers } | |

**User's choice:** New sibling TriggerRegistry
**Notes:** Keeps responsibilities separate; HTTP routing in 7.1+ reads from TriggerRegistry without entangling flow lookup. Same content_hash discipline + frozen-after-boot semantics.

### Cross-file flow-name validation

| Option | Description | Selected |
|--------|-------------|----------|
| Validate at parse-finalize, allow forward refs | Cross-file lookup; trigger and flow can live in different files | ✓ |
| Per-file validation only | Trigger and flow must live in the same .star file | |
| Lazy validation at server boot | Parse succeeds; boot resolves and errors | |

**User's choice:** Validate at parse-finalize, allow forward refs
**Notes:** Allows trigger.star + flows.star file separation. Position-aware error if unknown.

### Duplicate (flow, source-kind) policy

| Option | Description | Selected |
|--------|-------------|----------|
| Allow | Two triggers can fire same flow from same source kind with different configs | ✓ |
| Reject identical (flow, source-kind) pairs | Force consultants to combine event filters | |
| Reject identical (flow, source-kind, full-config) only | Detect literal duplicates | |

**User's choice:** Allow
**Notes:** HTTP router de-dups handler mounts in 7.1; registry stores both. Friendly warning on byte-identical configs only.

### Phase 7 test harness for TriggerSource

| Option | Description | Selected |
|--------|-------------|----------|
| Test-only stub in pkg/parser/*_test.go | type fakeTriggerSource for parser tests | ✓ |
| Ship a 'noop' source factory in Phase 7 | pkg/extension/builtin/triggers/noop.go | |
| Defer parser tests to Phase 7.1 | Risky — Phase 7 success criterion 2 unverifiable | |

**User's choice:** Test-only stub in pkg/parser/*_test.go
**Notes:** No production-shipped throwaway factory. Phase 7.1 ships the real github.WebhookSource etc.

---

## Server lifecycle on SIGTERM/SIGINT

### SIGINT vs SIGTERM behavior

| Option | Description | Selected |
|--------|-------------|----------|
| Identical — both drain | Both initiate graceful drain up to --drain-timeout | ✓ |
| SIGINT immediate / SIGTERM drain | Ctrl-C fast-stops; orchestrator-driven SIGTERM drains | |
| Both drain, no second-signal escalation | Drain always runs to completion | |

**User's choice:** Identical — both drain
**Notes:** Second signal of either kind during drain forces immediate worker.Stop and exit 1.

### Behavior on --drain-timeout expiry

| Option | Description | Selected |
|--------|-------------|----------|
| worker.Stop force + exit 1 with timeout message | Cancels in-flight; Temporal preserves state for resume | ✓ |
| os.Exit(1) hard — no worker.Stop | Skip cleanup; faster shutdown | |
| Block until completion — ignore timeout | Treat --drain-timeout as advisory | |

**User's choice:** worker.Stop force + exit 1 with timeout message
**Notes:** This is the durability story — workflows resume from event history on next worker start.

### --addr flag semantics in Phase 7 (no HTTP listener)

| Option | Description | Selected |
|--------|-------------|----------|
| Accepted but unused; warned at startup | Stable CLI surface; warning until 7.1 | ✓ |
| Defer the flag to Phase 7.1 | Don't register --addr at all in Phase 7 | |
| Accept silently, no warning | Stable CLI, no log noise | |

**User's choice:** Accepted but unused; warned at startup
**Notes:** Default value :8080. Phase 7.1 just removes the warning.

### Startup log format for registered flows/triggers

| Option | Description | Selected |
|--------|-------------|----------|
| charm-log Bazel-style banner | Same renderer as skytime run | |
| Plain slog at info level | Structured, machine-parseable | |
| Both — charm-log default, JSON via --json-log | Pretty by default, structured opt-in | ✓ |

**User's choice:** Both — charm-log default, JSON via --json-log
**Notes:** Default uses existing charm-log renderer; --json-log swaps slog handler to JSON. Sorted lexicographically; trigger lines show "source-kind → flow-name".

---

## Claude's Discretion (final wrap)

User selected "Wrap up — write CONTEXT.md" over discussing remaining smaller items. The following defaults are captured as Claude's discretion:
- `connectClient` reuse from `skytime run` for `--task-queue`/`--temporal`/`--credfile` routing
- `--credfile` flag accepted-but-deferred (full lift to `pkg/cli` is Phase 7.4)
- Credential redaction in error rendering — implementation detail of the firewall test left to planning

## Deferred Ideas

- Source factory implementations (Phase 7.1+)
- HTTP listener + signature validation (Phase 7.1)
- Idempotency mapping at ingress (Phase 7.1)
- Cron req shape (Phase 7.2)
- Dashboard / manual trigger UI (Phase 7.3)
- `cli.WithCredfile` / `cli.WithBuildID` Option lifts (Phase 7.4)
- Auth integration docs (Phase 7.5)
- Lifting `--json-log` to root command for all subcommands (out of scope — defer to v1.44+)

## Active Deviations from REQUIREMENTS.md / ROADMAP

1. **REQUIREMENTS.md TRIG-07/08 wording** says `triggers.github_webhook(...)` and `triggers.generic_http_webhook(...)` ship in `pkg/extension/builtin/triggers/`. **DECISION D-07-08 overrides:** source factories live in their owning extension's namespace (e.g., `github.webhook(...)`). Update REQUIREMENTS.md TRIG-07/08 wording during planning.
2. **ROADMAP success-criterion-1 illustrative example** for trigger primitive uses `map=lambda payload:` and `idempotency_key=lambda payload, headers:` — **DECISION D-07-03 overrides:** both lambdas take single positional `req`. Read success criterion as illustrative; lock actual signature here.
