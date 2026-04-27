# Architecture Research

**Domain:** Embedded DSL (Starlark) compiled to a DAG and executed inside Temporal workflows
**Researched:** 2026-04-26
**Confidence:** HIGH for component boundaries and data flow (validated against Temporal SDK semantics, `cadence-workflow/starlark-worker`, and Temporal `samples-go/dsl`); MEDIUM for exact Starlark-lambda-inside-workflow.Context behaviour (no public prior art evaluates `*starlark.Function` from inside a Temporal coroutine — this is novel territory and is the project's biggest risk surface).

---

## 1. System Overview

Skytime is a two-phase system. The phase boundary is the project's whole reason to exist and dictates the package layout.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          PARSE PHASE  (no I/O, pure)                         │
│                  Runs once per workflow start, off-Temporal                  │
├─────────────────────────────────────────────────────────────────────────────┤
│   .star file  ──►  parser  ──►  dag (Go AST nodes)  ──►  WorkflowInput      │
│                       │              │                                       │
│                       │              ├── lambdas captured as *starlark.Func  │
│                       │              ├── ActionRef intents (no exec)         │
│                       │              └── flow / step / if / for-each nodes   │
│                       │                                                      │
│                       └── extension registry (Starlark builtins → Go)        │
└─────────────────────────────────────────────────────────────────────────────┘
                                    │
                                    │ starts workflow with WorkflowInput
                                    ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                      EXECUTE PHASE  (durable, deterministic)                 │
│                            Inside Temporal worker                            │
├─────────────────────────────────────────────────────────────────────────────┤
│  ┌────────────────────────────────────────────────────────────────────────┐ │
│  │                      Generic interpreter workflow                       │ │
│  │  walks dag, evaluates lambdas in-process, tracks state, dispatches I/O │ │
│  └─────────┬───────────────────────────┬─────────────────────────┬────────┘ │
│            │                           │                         │          │
│            ▼                           ▼                         ▼          │
│   workflow.ExecuteActivity     workflow.SideEffect      workflow.ChildWF    │
│   (single generic activity)    (lambda + non-det)       (call_flow)         │
│            │                                                                 │
└────────────┼─────────────────────────────────────────────────────────────────┘
             ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                     ACTIVITY PHASE  (one generic activity)                   │
├─────────────────────────────────────────────────────────────────────────────┤
│   For each ActionRef in the batch:                                           │
│     1. Resolve credential JIT (id → secret, never in workflow state)         │
│     2. Look up extension by ActionRef.kind                                   │
│     3. Call extension's plain Go fn (no Temporal imports, no Starlark)       │
│     4. Append result to batch result list                                    │
│   Return [results]                                                           │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Why this differs from "raw Temporal + Go workflow code"

| Aspect | Raw Temporal Go | Skytime |
|---|---|---|
| Workflow definition | Hand-written Go function per workflow | Generic interpreter; `.star` is the spec |
| Activities | Each I/O is its own registered activity | One generic activity batches an ActionRef list |
| Authoring | Requires Temporal+Go literacy | Consultant writes Starlark; Go knowledge optional |
| Conditional logic | Native Go `if`/`for` | Starlark `if_cond` + `lambda` (eval'd in-workflow) |
| Specialization per customer | Per-customer fork or feature flags | New `.star` file, no recompile |
| Replay safety | Author must hand-audit each workflow | Determinism is structural — DAG is immutable; lambdas read-only on injected state |

The key buy: customers get Temporal's durability guarantees with zero Go authorship and a vastly smaller failure surface, because the only "code" they write is restricted to Starlark + lambdas over an injected state struct.

---

## 2. Component Boundaries (Go packages)

Concrete package layout. Each package has one responsibility and a stable downstream contract. Imports flow strictly downward — `interpreter` imports `dag`, never the reverse.

```
skytime/
├── pkg/
│   ├── dag/                  # Pure data: the parsed graph nodes
│   │   ├── node.go           # Node interface + Flow, Step, IfCond, ForEach, CallFlow, Script
│   │   ├── action.go         # ActionRef intent (kind, kwargs, result_var, credential_ref)
│   │   ├── lambda.go         # CapturedLambda{Fn *starlark.Function, Globals StringDict}
│   │   └── input.go          # WorkflowInput (the JSON-serializable payload)
│   │
│   ├── parser/               # Starlark → dag (parse phase)
│   │   ├── parser.go         # ExecFile, builds the root Flow
│   │   ├── builtins.go       # flow(), step(), if_cond(), for_each_parallel(), call_flow(), script()
│   │   ├── thread.go         # *starlark.Thread setup, load() resolver
│   │   └── validation.go     # Static checks: unknown extensions, kwarg schema, lambda arity
│   │
│   ├── extension/            # Extension SDK — what library devs implement
│   │   ├── extension.go      # Extension interface { Name(), Builtins() starlark.StringDict, Execute(ctx, ActionRef) }
│   │   ├── action_ref.go     # ActionRef constructor used by extension Starlark factories
│   │   ├── registry.go       # Global registry; merged into parser's predeclared env
│   │   └── credentials.go    # CredentialResolver interface (id → secret, JIT)
│   │
│   ├── bridge/               # Starlark ↔ Go state conversion (used by interpreter)
│   │   ├── struct.go         # ToStarlarkStruct(any) *starlarkstruct.Struct (recursive)
│   │   ├── value.go          # FromStarlarkValue(starlark.Value) any (for results)
│   │   └── lambda.go         # CallLambda(thread, fn, ctx) — runs a captured lambda
│   │
│   ├── interpreter/          # Generic workflow that walks the DAG (execute phase)
│   │   ├── workflow.go       # SkytimeWorkflow(ctx workflow.Context, in WorkflowInput) — registered with worker
│   │   ├── walk.go           # Visits Flow/Step/IfCond/ForEach/CallFlow/Script nodes
│   │   ├── batch.go          # Collects ActionRefs in a Step, dispatches one activity call
│   │   ├── state.go          # In-workflow state map; converted to starlarkstruct on each lambda call
│   │   └── parallel.go       # for_each_parallel using workflow.Go + Future selectors
│   │
│   ├── activity/             # The single generic activity
│   │   ├── activity.go       # ExecuteBatch(ctx, []ActionRef) → []ActionResult
│   │   ├── dispatch.go       # Routes ActionRef → Extension.Execute, sequential within batch
│   │   └── credentials.go    # Resolves credential IDs JIT; never returns secrets to caller
│   │
│   ├── testing/              # Starlark E2E test harness (Tier 3)
│   │   ├── harness.go        # temporal_test builtin; wires testsuite mocks
│   │   ├── attempt.go        # Tracks attempt number for retry simulation
│   │   └── mock_registry.go  # Maps ActionRef.kind → Starlark mock fn
│   │
│   └── worker/               # Temporal worker bootstrap
│       └── worker.go         # NewWorker(client, taskQueue, registry, resolver) → registers SkytimeWorkflow + ExecuteBatch
│
├── cmd/
│   └── skytime/              # CLI for running .star files locally
│       ├── main.go
│       ├── run.go            # `skytime run flow.star`
│       ├── validate.go       # `skytime validate flow.star` (parse only, Tier 1)
│       └── test.go           # `skytime test flow.star` (Tier 3)
│
└── examples/
    └── http-github-slack/    # Dogfooding example
        ├── extensions/       # http, github, slack extensions
        ├── flows/            # *.star files
        └── main.go           # Wires worker + extensions + CLI
```

### Package responsibilities

| Package | Owns | Imported by | May NOT import |
|---|---|---|---|
| `dag` | Node types, ActionRef, CapturedLambda, WorkflowInput | parser, interpreter, activity, testing | starlark, temporal |
| `parser` | Starlark execution at parse time, builtin registration, static validation | cmd, worker, testing | temporal |
| `extension` | Extension SDK contract | parser (registry), activity (dispatch), examples | temporal, starlark.Thread (only ActionRef construction) |
| `bridge` | Conversions between Go state and starlark values, lambda invocation | interpreter, testing | temporal |
| `interpreter` | The generic workflow function | worker | starlark.Thread *only via bridge*, never direct |
| `activity` | The single batched activity | worker, testing | starlark, parser |
| `testing` | E2E harness | cmd | — |
| `worker` | Temporal client/worker setup, registration | cmd, examples | parser (parse happens before submit) |

The crucial firewall: **`activity` does not import `starlark`** and **`extension` does not import `temporal`**. Lambdas live entirely on the workflow side; extensions live entirely on the activity side. ActionRef is the wire format between them.

---

## 3. Data Flow

### End-to-end: `flow.star` → completed workflow

```
USER SUBMITS:                   flow.star  +  request payload
                                     │
                                     ▼
[cmd/skytime/run]            parser.Parse(file, registry)
                                     │
                                     │  produces: dag.Flow + map[lambdaID]CapturedLambda
                                     ▼
[bridge.ToStarlarkStruct]    request → starlarkstruct (for lambda eval later)
                                     │
                                     ▼
[worker]                     client.ExecuteWorkflow(SkytimeWorkflow, WorkflowInput{
                                  Dag:       dag.Flow,
                                  Lambdas:   map[ID]CapturedLambda,    ⚠ see Risk #1
                                  InitState: {req: {…}},
                                })
                                     │
                                     ▼  (Temporal serializes input to history)
[interpreter.Workflow]       walks dag.Flow:
                               │
                               ├─ Step{actions: [ActionRef…]}
                               │     ├─ batch.Collect(actions)
                               │     ├─ workflow.ExecuteActivity(ExecuteBatch, []ActionRef)
                               │     │     │
                               │     │     ▼
                               │     │  [activity.ExecuteBatch]
                               │     │     for each ActionRef:
                               │     │       resolver.Get(action.CredentialID) → secret
                               │     │       registry.Get(action.Kind).Execute(ctx, kwargs, secret)
                               │     │     return []ActionResult
                               │     │
                               │     └─ state[result_var] = result   (Go side)
                               │
                               ├─ IfCond{lambda_id, then, else}
                               │     ├─ stateStruct = bridge.ToStarlarkStruct(state)
                               │     ├─ result = bridge.CallLambda(captured[lambda_id], stateStruct)
                               │     └─ walk(result ? then : else)
                               │
                               ├─ ForEachParallel{lambda_id, body}
                               │     ├─ items = bridge.CallLambda(captured[lambda_id], stateStruct)
                               │     ├─ for each item: workflow.Go(walk(body, scoped_state))
                               │     └─ wait via selector
                               │
                               ├─ CallFlow{name, kwargs}
                               │     └─ workflow.ExecuteChildWorkflow(SkytimeWorkflow, sub_input)
                               │
                               └─ Script{lambda_id}
                                     └─ bridge.CallLambda(...) — pure data transform, no I/O
                                     │
                                     ▼
                             return final state to caller
```

### Concrete data shapes

**`dag.Node` (the parsed graph):**
```go
type Node interface{ NodeKind() string }

type Flow struct {
    Name     string
    Params   []string         // kwarg names from flow(...) factory
    Body     []Node           // sequence of Step / IfCond / ForEach / CallFlow / Script
}

type Step struct {
    Actions  []ActionRef      // batched, executed in one activity invocation
    ResultVars []string       // where to bind each result in workflow state
}

type ActionRef struct {
    Kind         string                 // e.g. "http.get", "github.create_issue"
    Kwargs       map[string]any         // resolved at parse time (no lambdas — evaluated literals)
    CredentialID string                 // ID only; resolved JIT in activity
}

type IfCond struct {
    LambdaID string                     // key into WorkflowInput.Lambdas
    Then     []Node
    Else     []Node
}

type CapturedLambda struct {
    Fn      *starlark.Function          // ⚠ NOT JSON-serializable; see Risk #1
    Globals starlark.StringDict         // closure free vars (must be frozen + serializable)
}
```

**Workflow input (crosses Temporal serialization):**
```go
type WorkflowInput struct {
    Dag       *Flow                     // serializable: all leaves are JSON-friendly
    Lambdas   map[string]CapturedLambda // ⚠ requires custom DataConverter (see Risk #1)
    InitState map[string]any
}
```

**State during execution (lives in workflow memory only):**
```go
type State struct {
    Vars     map[string]any              // bound by Step.ResultVars and Script outputs
    // Converted to *starlarkstruct.Struct via bridge.ToStarlarkStruct on each lambda call
}
```

---

## 4. Architectural Patterns

### Pattern 1: Command Pattern via ActionRef (intent objects, not callbacks)

**What:** Extensions don't perform I/O when called from Starlark. Their builtin returns an `ActionRef{kind, kwargs, credential_id}`. The interpreter collects these and executes them later.

**When to use:** Always — this is the cornerstone invariant. It means parse phase has zero side effects, the DAG is fully introspectable, and the interpreter is the single point of routing/batching/mocking.

**Trade-offs:** Extension authors must resist the urge to "just do the HTTP call inline." Linting/static analysis should flag any extension whose factory function does I/O. Guard with an integration test that runs the parser with no network and asserts no extension makes outbound calls.

**Example:**
```go
// Extension factory exposed to Starlark — returns intent, never executes
func githubCreateIssue(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
    var repo, title string
    var credID string
    if err := starlark.UnpackArgs("create_issue", args, kwargs, "repo", &repo, "title", &title, "credential", &credID); err != nil {
        return nil, err
    }
    return extension.NewActionRef("github.create_issue", map[string]any{"repo": repo, "title": title}, credID), nil
}
```

### Pattern 2: Captured-Lambda Late Binding (deferred evaluation in workflow context)

**What:** When parser sees `if_cond(lambda ctx: ctx.req.dry_run, ...)`, it stores `*starlark.Function` in the DAG keyed by a stable ID. Interpreter calls it at execute time with current state injected as a `*starlarkstruct.Struct`.

**When to use:** Anywhere user logic needs to read workflow state — conditions, loops, data shaping (`script`).

**Trade-offs:**
- Closures over module-level frozen values: safe (Starlark freezes after module init).
- Closures over mutable captures: **must be prevented at parse time** — any non-frozen free var in a captured lambda is a determinism risk. Bazel/Starlark's freezing semantics give this for free at the *module* level, but local scopes inside a `def` need explicit handling (validation rule: lambdas may close over module globals only).
- Lambda execution must NOT use Temporal SDK calls (no `workflow.ExecuteActivity` from inside a lambda); the bridge constructs a fresh `*starlark.Thread` *each call*, never passing `workflow.Context` into Starlark.

**Example:**
```go
// Inside interpreter.walk — NEVER pass workflow.Context to starlark.Thread
func (i *Interpreter) evalLambda(wfCtx workflow.Context, captured CapturedLambda, state *State) (starlark.Value, error) {
    thread := &starlark.Thread{Name: "lambda"} // fresh, isolated
    stateVal := bridge.ToStarlarkStruct(state.Vars)
    return starlark.Call(thread, captured.Fn, starlark.Tuple{stateVal}, nil)
}
```

### Pattern 3: Block-Batched Activity Dispatch (Temporal history compression)

**What:** A `step()` containing N `ActionRef`s schedules **one** Temporal activity call carrying all N actions; the activity executes them sequentially. History grows by one ActivityTaskScheduled+Completed per step, not per action.

**When to use:** Default for sequential I/O within a step. Parallel I/O uses `for_each_parallel` and gets one activity per branch (scheduled via `workflow.Go`).

**Trade-offs:**
- Pro: 50Ki event limit goes from a 50-action ceiling per workflow to ~25K actions; payload size budget is 4MB per activity which is plenty.
- Con: A failure mid-batch must be communicated as a structured `BatchResult` with per-action status; retry policy must consider whether to re-run the whole batch (idempotency!) or only failed items. Skytime's first version should re-run the whole batch and require extensions to be idempotent — document this loudly.
- Con: Long batches block the workflow on a single activity timeout; cap batch size (e.g., 50 actions) and chunk in interpreter.

**Example:**
```go
// In interpreter — collects sequential I/O, dispatches once
func (i *Interpreter) executeStep(ctx workflow.Context, step *dag.Step, state *State) error {
    var fut workflow.Future = workflow.ExecuteActivity(ctx, "ExecuteBatch", step.Actions)
    var results []ActionResult
    if err := fut.Get(ctx, &results); err != nil { return err }
    for i, r := range results {
        state.Vars[step.ResultVars[i]] = r.Value
    }
    return nil
}
```

### Pattern 4: Just-In-Time Credential Resolution (workflow state holds only IDs)

**What:** Workflow state and history contain `credential_id: "github_main"`, never the secret. The activity calls `resolver.Get(id)` immediately before invoking the extension; the secret never enters workflow context, never appears in history, never gets serialized.

**When to use:** All credentialed I/O. The `CredentialResolver` interface is the integration point for Vault, AWS Secrets Manager, env vars, etc.

**Trade-offs:** Resolver latency adds to every activity. Cache within the activity worker process (in-memory, TTL'd) — but never in workflow state.

### Pattern 5: Backend-Pluggable Workflow API (lesson from `cadence-workflow/starlark-worker`)

**What:** The interpreter could be written against a thin `workflow.Backend` interface that wraps `workflow.ExecuteActivity`, `workflow.Go`, etc. This is what cadence-workflow/starlark-worker does to abstract Cadence vs Temporal.

**When to use:** Skytime spec says Temporal-only, so this is **explicitly out of scope** for v1 — but designing the interpreter to talk to a small interface (3-4 methods) costs nothing and leaves Cadence/durable-task substitution open if the project's market validates and needs it. Recommended: define an internal `wf` interface with `ExecuteActivity`, `Go`, `Now`, `SideEffect`, `ExecuteChildWorkflow`, even if only the Temporal impl exists.

---

## 5. Data Flow Summary

### Key flows

1. **Parse:** `.star` file → parser (Starlark exec, builtins return `ActionRef`s and lambdas) → `dag.Flow` + `map[id]CapturedLambda`. **No I/O. No Temporal.** Pure function of file contents (enables hot-reload later).

2. **Workflow start:** Caller submits `WorkflowInput{Dag, Lambdas, InitState}` via Temporal client. Temporal serializes the input to history. ⚠ This is where the `*starlark.Function` serialization problem lives — see Risk #1.

3. **Workflow execution:** Generic interpreter walks the DAG. For each node type:
   - **Step:** batch ActionRefs, one `ExecuteActivity`.
   - **IfCond / ForEach / Script:** convert state to starlarkstruct, call captured lambda on a fresh `*starlark.Thread`, branch/iterate based on result.
   - **CallFlow:** `ExecuteChildWorkflow(SkytimeWorkflow, sub_input)`.

4. **Activity execution:** Loop over ActionRefs; for each, resolve credential, dispatch to extension, append result. Return result list. Extensions are plain Go — no Temporal imports, no `*starlark.Thread`.

5. **State management:** Workflow-side `map[string]any`. Converted to `*starlarkstruct.Struct` recursively each time a lambda runs (cheap; structures are small). Never converted back — lambdas return values consumed by the interpreter (e.g., a bool for `if_cond`).

---

## 6. Build Order (Phase Implications)

Build the type spine first (everything depends on `dag`), then parser, then the bridge, then activity, then interpreter, then testing/CLI.

### Suggested phase ordering

| Order | Component(s) | Dependencies | Why this slot |
|---|---|---|---|
| 1 | `dag` — Node types, ActionRef, WorkflowInput, CapturedLambda struct | none | Pure data; everything imports it. Define and freeze early — changes here ripple. |
| 2 | `extension` — Extension interface, ActionRef constructor, CredentialResolver | `dag` | Defines the contract for #3 (parser builtins) and #5 (activity dispatch). |
| 3 | `parser` + `bridge` — Starlark exec, builtins (flow/step/if/for/call/script), state↔struct conversion, static validation | `dag`, `extension` | Parser produces dags; bridge enables both parser-time literal evaluation and execute-time lambda invocation. Slot together because lambda capture is parser logic and lambda call is bridge logic. **Validation gate:** end of this phase, you can parse a `.star` file and inspect a complete `dag.Flow` in a unit test. No Temporal yet. |
| 4 | `activity` — single generic activity, batch dispatch, JIT credential resolution | `dag`, `extension` | Independent of interpreter — can be tested standalone with hand-built `[]ActionRef`. Don't drag interpreter risk into activity testing. |
| 5 | `interpreter` — generic Temporal workflow, walk, batch collection, lambda invocation, parallel | `dag`, `bridge`, `activity` (signature only — registered name) | The hard part. Risks #1, #2, #3 below all crystallize here. **Validation gate:** a hand-built `dag.Flow` runs end-to-end on a Temporal dev server with a stub extension. |
| 6 | `worker` — Temporal worker bootstrap, registration glue | all of above | Wires the pieces; trivial once #5 works. |
| 7 | `testing` — `temporal_test` builtin, attempt counter, mock registry | parser, bridge, interpreter, `go.temporal.io/sdk/testsuite` | Needs the production path working before mocking it. Tier 3 of the testing strategy. |
| 8 | `cmd/skytime` — CLI (run/validate/test) | all | UX layer; come last. |
| 9 | `examples/` — HTTP/GitHub/Slack extensions, real `.star` files | all | Dogfood; surfaces issues that unit tests miss. **Validation gate:** the example exercises every primitive; if any feels awkward in Starlark, fix the parser or builtin set before declaring v1 done. |

The boundary between #5 and #6 is the high-risk milestone — that's where Risk #1 gets resolved or replanned.

---

## 7. Architectural Risks (Where the Spec Meets Hard Reality)

### Risk #1 — `*starlark.Function` Serialization Across Workflow History (HIGH severity, HIGH likelihood)

**The problem:** Temporal serializes workflow input to history (4MB blob limit, JSON by default). `*starlark.Function` is **not JSON-serializable** — it embeds a compiled bytecode `funcode`, a defaults slice, free-variable closures, and a reference to the immutable `*starlark.Program` that produced it. Standard `encoding/json` will produce `"{}"` and replays will fail.

**Why it matters:** The spec says lambdas are captured at parse time and evaluated at execute time. Between parse and execute lies Temporal's serialization boundary. If the lambdas can't survive the round trip, the whole architecture is broken.

**Three viable resolutions (pick at start of interpreter phase):**

1. **Custom `DataConverter` that serializes `CapturedLambda` as the original Starlark source text + closure environment + lambda lookup key.** On replay/decode, re-resolve the lambda by re-parsing or by indexing into a parsed program. This keeps lambdas as first-class workflow input but requires the parsed `*starlark.Program` to be available on every replayer. The starlark-worker project effectively does this — workers ship with the script files baked in, and only the script *path* and arguments cross the wire. **Recommended.**

2. **Re-parse on workflow start, keep only an opaque `LambdaID` in history.** The workflow function calls `parser.Parse(scriptPath)` as its first step (under `workflow.SideEffect` to avoid replay re-parse), populates a worker-process-local lambda table, and indexes by ID thereafter. Determinism is preserved because parse is deterministic. **Simpler but couples deployment** — the `.star` file must be available to every worker.

3. **Pre-compile lambdas to a serializable IR.** Walk the lambda's AST at parse time and translate to a small serializable expression DSL (binops, attribute access, literals, conditional). This re-introduces a string compilation surface and explicitly violates a spec invariant. **Reject.**

The recommended path is #1 with a fallback to #2 if `*starlark.Program` reconstruction proves brittle. Either way, **this decision must be made during the interpreter milestone**, with a decision-record committed before activity batching is implemented.

### Risk #2 — Determinism of Lambda Evaluation Inside Replay (HIGH severity, MEDIUM likelihood)

**The problem:** Temporal replays the entire workflow function on every worker restart. Every lambda call must produce the same value on replay as on first execution. Starlark itself is deterministic by design (no `time.now()`, no `random()`, no I/O), **but** the inputs to the lambda — the state struct — must be byte-equivalent on replay.

**Where this can break:**
- Map iteration order in `bridge.ToStarlarkStruct`: Go maps iterate randomly. The conversion must sort keys deterministically.
- Floating-point operations in lambdas: Starlark floats are off by default and should remain off (or fixed-precision).
- Time-dependent lambda outputs: forbid by static check — lambdas must not call any builtin returning current time. Skytime's predeclared environment should not include `time.now`-style functions.

**Mitigation:** Static validator (parser package) walks the lambda AST and rejects calls to non-deterministic builtins. Add a Temporal replay test (`go.temporal.io/sdk/contrib/tools/workflowcheck` is for Go, but its replay test pattern applies) — record a workflow, modify nothing, replay, assert byte-equal commands.

### Risk #3 — Goroutine + Workflow.Context Bleed Inside Bridge (MEDIUM severity, MEDIUM likelihood)

**The problem:** The spec forbids passing `workflow.Context` into a `*starlark.Thread`. The temptation is high — extensions might want a workflow logger, or the bridge might want `workflow.Now()` to power a `time.now`-like Starlark builtin.

**Mitigation:**
- `bridge.CallLambda` constructs a fresh `*starlark.Thread` for every invocation; the thread is scoped to one call, never reused.
- Predeclared lambda environment is the smallest possible: `len`, `dict`, `list`, comparison ops, that's it. No logger, no time, no `workflow.*` access.
- For diagnostic logging from a lambda, capture stdout-style print calls into the workflow's Temporal logger via `thread.Print = func(thread, msg) { logger.Info(msg) }` — but `logger` is captured by the interpreter, never passed to Starlark code.
- `workflow.Go` for parallel execution must scope each goroutine's state map carefully — the parallel `for_each` body must run on a *copy* of state plus its iteration variable, never the parent's mutable state.

### Risk #4 — Activity Batch Failure Semantics (MEDIUM severity, HIGH likelihood)

**The problem:** A batch of N ActionRefs in one activity. Action 3 fails. What happens to actions 4..N? What about action 1's result? Should retry replay all N actions or only the failed one?

**Options:**
- **All-or-nothing with retry of full batch:** simplest; requires extension idempotency. Failure at action 3 means actions 1, 2 were observed by external systems but their results are discarded; on retry, actions 1, 2 happen again.
- **Partial success with checkpoint:** activity returns `[ActionResult]` where each entry has `{ok, value, err}`; the workflow decides how to proceed. More complex but truer to the per-extension fault model.

**Recommendation for v1:** all-or-nothing + idempotency requirement, documented prominently. Defer partial-success to v2 once a real customer flow demands it.

### Risk #5 — Hot-Reload Architecture Lock-In (LOW severity, LOW likelihood, but high opportunity cost if done wrong)

**The problem:** Spec says hot-reload is out of scope but design must not preclude it.

**What preserves hot-reload:**
- Parser is a pure function of file contents — already in design.
- Lambda serialization choice (Risk #1, option #2) requires re-parsing on every workflow start, which natively supports hot-reload (new file content = new parse = new lambdas next workflow start).
- Workflow versioning: changing a `.star` file mid-flight on a running workflow would break determinism. Hot-reload only affects *new* workflow starts; in-flight workflows continue executing the version pinned in their history. This is the right behavior and is automatic if the lambda choice is #1 (lambdas pinned in the input) or correctly handled if #2 (parse cached by file hash).

**What would preclude hot-reload (avoid):** statically registering lambdas at worker startup with no mechanism to refresh. Don't do this; use a content-addressed cache.

### Risk #6 — Starlark Module Freezing & Lambda Closures (LOW severity, MEDIUM likelihood)

**The problem:** Starlark modules freeze all values after module init. Lambdas captured during module init close over frozen module globals — safe. But lambdas defined *inside* a `def` block close over local scope, and the Skytime parser must validate which closures are allowed.

**Mitigation:** Static validator allows lambdas to close over module-level (frozen) values only. Reject closures over local variables of an enclosing `def`, or document that they're captured by value at the moment the lambda is created (Starlark semantics already give value-at-creation for non-mutable types).

### Risk #7 — Child Workflow History Multiplier (LOW severity, LOW likelihood)

**The problem:** `call_flow` becomes `ExecuteChildWorkflow`. Each child has its own 50Ki event budget — generally a feature. But deeply nested call_flow chains can produce surprising parent-history footprints (each child generates `ChildWorkflowExecutionStarted` and `ChildWorkflowExecutionCompleted` in the parent).

**Mitigation:** Document the budget. Support `call_flow(detached=True)` later if needed (runs a sibling workflow, no parent-history coupling). Out of scope for v1.

---

## 8. Anti-Patterns

### Anti-Pattern: Letting Extensions Import `go.temporal.io/sdk/activity`

**What people do:** Convenient access to `activity.GetLogger()`, `activity.RecordHeartbeat()`, etc.
**Why it's wrong:** Couples extensions to Temporal — breaks the mockability of extensions in unit tests, breaks the option to run extensions out-of-process later, and makes the activity's "single generic" boundary leaky. An extension is a plain Go function; it should be testable with `go test` alone.
**Do this instead:** The single generic activity in `pkg/activity` owns all Temporal SDK calls (logging, heartbeats, retries). It passes a context-scoped `extension.ExecCtx` (your own type) to the extension that exposes only what extensions need: a logger, a deadline, and credential getters.

### Anti-Pattern: Passing `*starlark.Thread` Into Activities

**What people do:** "Just thread the Starlark context through so the extension can call back into a starlark mock."
**Why it's wrong:** Starlark threads are not thread-safe, not serializable, and cannot cross goroutines safely. Extensions running in the activity worker (potentially a different process from the workflow worker) would observe a Starlark state divorced from the workflow's view.
**Do this instead:** All Starlark execution happens on the workflow side. The `temporal_test` mock harness (Tier 3) wires Starlark mock functions to extension dispatches via a side-channel that does *not* use the activity boundary — it intercepts `workflow.ExecuteActivity` in `testsuite` and routes to Starlark mocks directly inside the workflow worker.

### Anti-Pattern: Storing `*starlark.Function` in Workflow History as a Black Box

**What people do:** Custom DataConverter that gob-encodes the Function. "Works on my machine."
**Why it's wrong:** Function values transitively reference their compiled program; round-tripping via gob will fail when free vars include builtin functions or non-encodable types. Worse, replay with a different binary (different builtin pointers) silently corrupts.
**Do this instead:** See Risk #1 — serialize the *source location* and re-resolve at replay time, never the Function value itself.

### Anti-Pattern: Using `time.Now()` or `rand.*` Inside Lambdas via Builtin Helpers

**What people do:** Add a `now()` builtin to the lambda environment "for convenience."
**Why it's wrong:** Breaks determinism. Replay produces different value, workflow gets killed for non-determinism.
**Do this instead:** If a workflow needs current time, the *interpreter* gets it via `workflow.Now(ctx)` (replay-safe, recorded in history) and injects it into the state struct passed to the lambda. The lambda reads `ctx.now`, not `now()`.

### Anti-Pattern: Per-Extension Activity Registration

**What people do:** "Extensions are just Temporal activities, register them."
**Why it's wrong:** This is precisely what the spec rejects. Per-activity registration ties extension naming to Temporal task queue topology, exposes activities as separate items in history (defeating batching), and makes the extension catalog a moving deployment target rather than a library concern.
**Do this instead:** One generic activity. Extensions are registered with Skytime (a Go-side registry), not with Temporal.

---

## 9. Scaling Considerations

| Scale | Architecture Adjustments |
|---|---|
| 1 customer, ~10 flows, dev-server | Single worker process, all extensions in-process, dev Temporal server. Default Skytime config. |
| 10 customers, ~100 flows, prod cluster | Worker pool sized to flow concurrency (default Temporal sizing). Credential resolver should cache (process-local, TTL'd). Activity batch size cap at ~50 actions. |
| Many customers, multi-tenant | Skytime is a *library*, not a service — multi-tenant hosting is explicitly out of scope. If reached: separate task queues per customer (or per priority class), separate worker fleets, separate `CredentialResolver` instances. Don't multiplex tenants in one workflow. |

### Scaling priorities

1. **First bottleneck (predictable):** activity history bloat for large flows. Mitigation: batching is the design (already addressed).
2. **Second bottleneck (likely):** parse-phase cost for very large `.star` files on every workflow start. Mitigation: parse cache keyed by file content hash, populated lazily, scoped to worker process.
3. **Third bottleneck (possible):** lambda invocation cost when a flow has thousands of items in `for_each_parallel` each calling a `script` lambda. Mitigation: Starlark eval is fast (~µs per call); if it becomes a problem, batch lambda invocations the way ActionRefs are batched, or move data-shape work into extensions (which run as activities).

---

## 10. Comparison Points (How Skytime Differs From Prior Art)

| Project | What it does | What Skytime borrows | What Skytime does differently |
|---|---|---|---|
| `cadence-workflow/starlark-worker` | Starlark scripts as Cadence/Temporal workflows; backend-pluggable workflow API | Backend abstraction lesson; package layout (star/, activity/, workflow/, ext/, plugin/, cmd/, service/); `temporal_client_main` pattern for invocation | Explicit parse/execute split (starlark-worker mixes them — Starlark *is* the workflow body, not a parsed DAG); Skytime's lambdas are evaluated via a bridge with state struct injection rather than Starlark calling Cadence APIs directly; single-batched activity (starlark-worker has multiple) |
| `temporalio/samples-go/dsl` | YAML-defined DAG executed as a Temporal workflow | DAG node types (Sequence, Parallel, ActivityInvocation), variable binding pattern, walk-and-execute structure | Skytime's DSL is Starlark not YAML; Skytime supports lambdas (samples-go DSL has no expressions, only literal kwargs); Skytime batches sequential activities (samples-go schedules each separately) |
| Tilt's Tiltfile / starkit | Starlark as configuration language; init/reduce extension state pattern | Extension registry with init pattern; load() resolver for imports; Starlark builtins as the extension surface | Tilt is config-only (no execution graph, no durability); Skytime adds the entire Temporal execution layer that Tilt has no analog for |
| Bazel | Starlark for build rules; freezing semantics; deterministic eval | Module-freeze semantics (lambdas may close over frozen module globals); deterministic-by-design Starlark execution | Bazel runs Starlark to *produce* a build graph that other tools execute; Skytime runs Starlark to *produce* a workflow graph that the same project's Temporal interpreter executes — narrower domain, tighter integration |

The novel contribution: **Skytime is the first system (publicly) to combine the Bazel-style "Starlark produces a graph" approach with Temporal-style durable execution, with the lambda-evaluation-at-execute-time pattern that lets workflow logic be expressed in Starlark without Starlark seeing Temporal context.** Starlark-worker is similar but treats the script as the workflow body; Skytime treats the script as a *spec for* the workflow body.

---

## 11. Integration Points

### External services

| Service | Integration Pattern | Notes |
|---|---|---|
| Temporal Cloud / self-hosted | `go.temporal.io/sdk/client.NewClient` | No cloud-only features; namespace + task queue config |
| Credential stores (Vault, AWS SM, env) | `extension.CredentialResolver` interface, JIT-called from activity | Caller supplies; library ships only env-var stub for examples |
| Extensions' downstream APIs (HTTP, GitHub, Slack…) | Extension's `Execute(ctx, ActionRef)` makes the call | Extensions are plain Go; HTTP client of their choosing |

### Internal boundaries

| Boundary | Communication | Notes |
|---|---|---|
| `parser` ↔ `interpreter` | `WorkflowInput` (DAG + lambdas + initial state) over Temporal client | The serialization boundary; Risk #1 lives here |
| `interpreter` ↔ `activity` | `[]ActionRef` request, `[]ActionResult` response, via `workflow.ExecuteActivity` | The Temporal-history boundary; ActionRefs and results must be JSON-serializable |
| `activity` ↔ `extension` | Direct Go function call, in-process | Plain `func(ctx ExecCtx, kwargs map[string]any) (any, error)` |
| `interpreter` ↔ `bridge` | Direct Go calls; bridge is a stateless utility | bridge owns the *only* code that touches `*starlark.Thread` during execute phase |
| `parser` ↔ `extension` | Extensions register Starlark builtins; parser executes them at parse time | The builtin returns `ActionRef`, not a result |

---

## Sources

- [cadence-workflow/starlark-worker · GitHub](https://github.com/cadence-workflow/starlark-worker) — closest prior art; multi-backend Starlark→workflow runner (HIGH confidence reference for package layout)
- [temporal package · cadence-workflow/starlark-worker · pkg.go.dev](https://pkg.go.dev/github.com/cadence-workflow/starlark-worker/temporal) — the Temporal-backend integration pattern
- [samples-go/dsl/workflow.go · temporalio/samples-go](https://github.com/temporalio/samples-go/blob/main/dsl/workflow.go) — official Temporal DSL DAG sample (Statement / Sequence / Parallel / ActivityInvocation tree-walk)
- [go.temporal.io/sdk/workflow · pkg.go.dev](https://pkg.go.dev/go.temporal.io/sdk/workflow) — workflow package API reference
- [Side Effects · Temporal Go SDK Docs](https://docs.temporal.io/develop/go/workflows/side-effects) — `SideEffect` semantics, closure-capture pitfall (Risk #2)
- [Workflow Execution limits · Temporal Docs](https://docs.temporal.io/workflow-execution/limits) — 50Ki events, 50MB history, 4MB blob (Risk #4 sizing)
- [Troubleshoot the blob size limit error · Temporal Docs](https://docs.temporal.io/troubleshooting/blob-size-limit-error) — batching guidance
- [Temporal Go SDK multithreading · Temporal Docs](https://docs.temporal.io/develop/go/go-sdk-multithreading) — `workflow.Go` semantics, no native goroutines (Risk #3)
- [Understanding Non-Determinism in Temporal.io · Sanh Doan, Medium](https://medium.com/@sanhdoan/understanding-non-determinism-in-temporal-io-why-it-matters-how-to-avoid-it-3d397d8a5793) — closure variable pitfall
- [workflowcheck · go.temporal.io/sdk/contrib/tools/workflowcheck](https://pkg.go.dev/go.temporal.io/sdk/contrib/tools/workflowcheck) — static analyzer for non-determinism (testing strategy reference)
- [Starlark in Go: Implementation](https://chromium.googlesource.com/external/github.com/google/starlark-go/+/HEAD/doc/impl.md) — freeze semantics, thread safety (Risk #6)
- [Starlark in Go: Language definition](https://github.com/google/starlark-go/blob/master/doc/spec.md) — determinism guarantees, lambda spec, mutability rules
- [syntax package · go.starlark.net/syntax](https://pkg.go.dev/go.starlark.net/syntax) — `syntax.File`, `FileProgram` API for parser implementation
- [starlark package · go.starlark.net/starlark](https://pkg.go.dev/go.starlark.net/starlark) — `*starlark.Function`, `*starlark.Thread`, `StringDict` (Risk #1 — function values are not JSON-serializable)
- [Workflow for running a DAG in DSL · Temporal Community](https://community.temporal.io/t/workflow-for-running-a-dag-in-dsl/3880) — community DAG patterns
- [Executing a DAG in a workflow · Temporal Community](https://community.temporal.io/t/executing-a-dag-in-a-workflow/8472) — `Task.WhenAll` / `workflow.Go` parallel pattern, dependency tracking
- [Child Workflows · Temporal Docs](https://docs.temporal.io/child-workflows) — when to use child workflows vs activities (Risk #7)
- [How many Activities should I use in my Temporal Workflow? · Temporal Blog](https://temporal.io/blog/how-many-activities-should-i-use-in-my-temporal-workflow) — batching guidance
- [Tilt starkit package](https://pkg.go.dev/github.com/windmilleng/tilt@v0.13.6/internal/tiltfile/starkit) — extension registration model (init/reduce pattern reference)
- [tilt-starlark-codegen · GitHub](https://github.com/tilt-dev/tilt-starlark-codegen) — codegen pattern for typed Starlark builtins (future Skytime opportunity for extension stub generation)

---
*Architecture research for: Skytime — Starlark→DAG→Temporal library*
*Researched: 2026-04-26*
