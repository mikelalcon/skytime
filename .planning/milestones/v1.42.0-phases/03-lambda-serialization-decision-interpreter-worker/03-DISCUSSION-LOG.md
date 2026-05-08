# Phase 3: Lambda-Serialization Decision + Interpreter + Worker - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-29
**Phase:** 03-lambda-serialization-decision-interpreter-worker
**Areas discussed:** Lambda serialization mechanism, call_flow + child workflow semantics, for_each_parallel concurrency model, Worker bootstrap & client factory shape

---

## Lambda Serialization Mechanism

User asked clarifying question first: "what would happen if we need to fix the star file because of a bug? Like you can do with temporal in golang, python, java, etc."

Orchestrator explained the versioning landscape:
- Standard Temporal answers: Build IDs / Worker Versioning, `workflow.GetVersion`/`Patch`, or live with broken replays.
- Skytime mappings: Option A (custom DataConverter, source embedded), Option B (re-parse + Build IDs), Option B' (multi-version worker registry without Build IDs).

User followed up: "how is going to deal with bug fixes in starlark and evolutions of configs?"

Orchestrator wrote a full versioning lifecycle walkthrough covering: bug fix in lambda, adding/removing steps, adding flows, adding extensions, extension Go-code fixes (no versioning needed), Skytime DSL evolution. Key mechanism: content_hash recorded per workflow, Build IDs route to compatible workers, old workflows drain on old workers.

### Strategy

| Option | Description | Selected |
|--------|-------------|----------|
| A — Custom DataConverter, source embedded | Each workflow carries source bytes; replay self-contained | |
| B — Re-parse + rely on Build IDs (Recommended) | Workflow input is {flow_name, content_hash, init_state}; Build IDs handle versioning operationally | ✓ |
| B' — Multi-version worker registry (no Build IDs) | Workers hold multiple .star versions; route by content_hash | |
| Defer to follow-up research | Lock answer later | |

**User's choice:** B — Re-parse + rely on Build IDs (Recommended)
**Notes:** Versioning is operational, not authorial. Customers use Temporal Build IDs to drain old workflows.

### WorkflowInput shape

| Option | Description | Selected |
|--------|-------------|----------|
| {flow_name, content_hash, init_state} (Recommended) | Smallest payload; worker registry lookup | ✓ |
| {flow Flow, content_hash, init_state} | Whole DAG embedded | |
| Both — register two workflow types | Two paths | |

**User's choice:** {flow_name, content_hash, init_state}
**Notes:** Worker maintains the parsed-flow registry; smallest wire format.

### Source delivery

| Option | Description | Selected |
|--------|-------------|----------|
| Embedded via go:embed (Recommended) | .star bundled into binary | |
| Filesystem path (--rootdir or env var) | Worker reads from disk at boot | ✓ |
| Both | Both paths supported | |

**User's choice:** Filesystem path
**Notes:** Registry frozen at boot — no hot-reload from filesystem during worker lifetime. Build ID corresponds to "this binary + these files at deploy time."

### Backend abstraction

| Option | Description | Selected |
|--------|-------------|----------|
| Drop it — Temporal directly (Recommended) | YAGNI; consistent with Phase 2's pkg/activity | ✓ |
| Keep wf 5-method abstraction | Defensive hedge for Cadence | |

**User's choice:** Drop it
**Notes:** Interpreter directly imports `go.temporal.io/sdk/workflow`.

---

## call_flow + Child Workflow Semantics

### Always child workflow

| Option | Description | Selected |
|--------|-------------|----------|
| Always child workflow (Recommended) | Each call_flow → workflow.ExecuteChildWorkflow | ✓ |
| Inline by default, child if `child=True` | Macro-expansion | |
| Always inline | Simplest interpreter; violates INTRP-05 | |

**User's choice:** Always child workflow
**Notes:** Sub-flow gets its own history per ARCHITECTURE.md.

### Cross-flow lambda IDs

| Option | Description | Selected |
|--------|-------------|----------|
| No conflict by construction (Recommended) | line+col disambiguates; sha256 differs per file | ✓ |
| Add flow_name to ID prefix | Defensive | |
| Warn on collision at parse time | Statistical near-zero | |

**User's choice:** No conflict by construction
**Notes:** Current ID format already handles this.

### Retry policy inheritance

| Option | Description | Selected |
|--------|-------------|----------|
| Per-call_flow with explicit kwargs (Recommended) | Default to Temporal's child default | |
| Inherit from parent flow's options | Parent's RetryPolicy applies | ✓ |
| Child workflows don't retry by default | "Try once" primitive | |

**User's choice:** Inherit from parent
**Notes:** Override via call_flow kwargs.

### Search attribute / memo propagation

| Option | Description | Selected |
|--------|-------------|----------|
| Inherit by default; allow override (Recommended) | Standard Temporal child behavior | ✓ |
| No propagation | Children get clean slate | |
| Skytime-stamped only | Auto-stamp parent_flow only | |

**User's choice:** Inherit by default; allow override

---

## for_each_parallel Concurrency Model

### Default fan-out cap

| Option | Description | Selected |
|--------|-------------|----------|
| 10 (Recommended) | Conservative; configurable per call | ✓ |
| 100 | Generous default | |
| Unlimited (-1) | Lazy-by-default | |
| Required — must declare | Forces conscious choice | |

**User's choice:** 10
**Notes:** `for_each_parallel(items=..., max_concurrency=N, ...)` overrides.

### Error mode

| Option | Description | Selected |
|--------|-------------|----------|
| Cancel siblings; bubble up the error (Recommended) | errgroup-style | ✓ |
| Let siblings finish; aggregate errors | Per-index (output, error) list | |
| Configurable via on_error kwarg | Adds API surface | |

**User's choice:** Cancel siblings; bubble up
**Notes:** workflow.NewSelector + cancel context.

### Item access in lambdas

| Option | Description | Selected |
|--------|-------------|----------|
| ctx.<item_name> (Recommended) | Bridge injects item under ctx by item kwarg name | ✓ |
| Bound lexically in lambda closure | Lambda signature changes | |
| Both | Two access patterns | |

**User's choice:** ctx.<item_name>

### Iteration contract

| Option | Description | Selected |
|--------|-------------|----------|
| Stable index order; results in input order (Recommended) | Replay-deterministic | ✓ |
| Branches in order; results in completion order | Streaming pattern; non-deterministic | |

**User's choice:** Stable index order

---

## Worker Bootstrap & Client Factory

### Client factory shape

| Option | Description | Selected |
|--------|-------------|----------|
| One constructor with ConnectionOptions struct (Recommended) | One symbol; struct fields disambiguate | |
| Three named constructors | NewCloudClient / NewSelfHostedClient / NewDevClient | ✓ |
| Functional options | WithCloud(...), etc. | |

**User's choice:** Three named constructors
**Notes:** More discoverable in IDE autocomplete.

### Worker entry point

| Option | Description | Selected |
|--------|-------------|----------|
| worker.Run(ctx) blocking (Recommended) | Standard for long-running services | |
| worker.Start non-blocking + Stop | Caller manages lifecycle | ✓ |
| Both | Two paths | |

**User's choice:** worker.Start non-blocking + Stop

### Default task queue

| Option | Description | Selected |
|--------|-------------|----------|
| "skytime" (Recommended) | Sensible default; override via worker.Options | |
| Required — no default | Force consultants to think | |
| Per-flow task queue | DSL surface | |

**User's choice:** Free text — "maybe a default but allow us to overwrite per flow and per task? (task has more priority than flow and flow more than default)"
**Notes:** Default + per-flow + per-step (task) overrides; precedence step > flow > default. Requires DSL retrofit (`task_queue` kwarg on flow() and step() builtins).

### Build IDs

| Option | Description | Selected |
|--------|-------------|----------|
| Required worker.Options field | Forces explicit Build ID per deployment | |
| Optional with sensible default (Recommended) | BuildID defaults to binary hash / build-time-injected | ✓ |
| Defer to Temporal SDK directly | Skytime doesn't surface BuildID | |

**User's choice:** Optional with sensible default
**Notes:** Default = build-time-injected variable (`-ldflags "-X ...defaultBuildID=$(git rev-parse HEAD)"`). Document prominently in Phase 6 README.

---

## Claude's Discretion

- Watchdog goroutine helper naming (`runLambdaWithCancellation` vs `evaluateLambda`).
- Worker registry data structure (`map[flow_name]map[content_hash]*ParsedFlow` vs flat).
- Concurrency primitive for fan-out semaphore (buffered channel vs `golang.org/x/sync/semaphore`).
- Default RetryPolicy when nothing is set (none).
- Whether `dev-server` helper auto-spawns `temporal server start-dev` (no — connects to externally-running).
- File layout in `pkg/interpreter` (one big file vs split by node type).

## Deferred Ideas

- Hot-reload of .star files (v2; design doesn't preclude).
- workflow.Patch / version() DSL primitives (operational versioning only in v1).
- In-process temporal server start-dev spawning.
- Per-flow / per-step Build IDs.
- Multi-version worker registry without Build IDs (Option B').
- Custom DataConverter (Option A) — additive in v1.x if needed.
- signal_workflow / wait_for_signal primitives (DSL-V2-01).
- on_error / on_failure hooks (DSL-V2-02).
- Default Query handler (Phase 7).
- Continue-As-New strategy.
- Backend abstraction (wf interface).
