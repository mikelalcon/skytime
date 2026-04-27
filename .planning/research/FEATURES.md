# Feature Research

**Domain:** Durable workflow engine — Starlark DSL on top of Temporal (Go library)
**Researched:** 2026-04-26
**Confidence:** HIGH for ecosystem patterns and Temporal primitives (Context7-equivalent: official Temporal docs); MEDIUM for "what consultants/Temporal users wish they had" (synthesis from blog posts, community forum, Uber's Starlark Worker writeup); HIGH for anti-features (project's own constraints in PROJECT.md)

## Frame

Skytime sits in a very specific niche: it is **not** trying to be Airflow/Prefect/Dagster (data-pipeline orchestrators with strong DAG/asset semantics) and it is **not** trying to be a general declarative workflow engine like Argo or Camunda BPMN. It is a **safe author surface** over Temporal — a layer that gives the same readability as a YAML DSL (Zigflow, Serverless Workflow) without giving up the expressiveness lambdas provide, and without adding Temporal's full SDK surface to the consultant's plate.

The feature landscape below is anchored to that wedge. The closest direct prior art is **Uber's Starlark Worker for Cadence** (open-sourced 2024); the closest contemporary is **Zigflow** (YAML DSL for Temporal, early 2026). Skytime is differentiated from both by the two-tier authoring model (Go extension authors + Starlark consultants) and the Command/ActionRef pattern (extensions return intents, single generic activity executes them).

## Feature Landscape

### Table Stakes (Users Expect These)

These are the primitives a workflow engine **must** have for users to take it seriously. Missing any one of these and the response from a Temporal-experienced reviewer will be "what's the point — I'd just use Temporal directly."

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| **Sequential composition** (`flow` of `step`s) | Trivially expected; no workflow tool exists without it | LOW | Already in PROJECT.md scope |
| **Conditional branching** (`if_cond`) | Every comparable tool has it: Airflow `BranchPythonOperator`, Temporal `if/else` in code, Serverless Workflow `switch`, Camunda gateways | LOW | Already in PROJECT.md as `if_cond`; backed by Starlark lambda predicate |
| **Parallel fan-out / for-each parallel** | Airflow fan-out, Temporal `workflow.Go` + `Future`, Argo `withItems`. The single most-cited reason teams reach for an orchestrator | MEDIUM | Already in scope as `for_each_parallel`; needs care with Temporal child-workflow vs in-workflow goroutine choice |
| **Sub-workflow / call_flow** | Temporal `ChildWorkflow`, Airflow SubDAG, Argo workflow templates. Required for both reuse *and* history-size partitioning (Temporal recommends child workflows specifically to stay under event-history limits) | MEDIUM | Already in scope as `call_flow`. Decide: child-workflow-only? Or also "macro-expand into same workflow"? |
| **Per-step retries with exponential backoff** | Airflow `retries`/`retry_exponential_backoff`, Temporal `RetryPolicy`, every comparable tool. Default-on retries are the reason people pick durable engines | LOW | Map directly to Temporal `RetryPolicy` on the generic activity. Expose: `max_attempts`, `initial_interval`, `backoff`, `max_interval`, `non_retryable_errors` |
| **Per-step timeouts** | Temporal `StartToCloseTimeout` and friends. Without it, a hung HTTP call hangs the workflow | LOW | Map to Temporal activity timeouts |
| **Error handling / try-catch equivalent** | Every workflow tool has *some* form (Airflow callbacks, Temporal `defer`/error returns, Serverless Workflow `try/catch`). Without it you can't write "send Slack on failure" | MEDIUM | Skytime needs `on_error`/`on_failure` hooks per step or per flow; design how this composes with retries |
| **Cancellation propagation** | Temporal cancellation, Airflow task cancellation. If a flow is cancelled, in-flight steps must be cancellable and `call_flow` children should follow Temporal's `ParentClosePolicy` | MEDIUM | Inherit from Temporal; expose nothing new; but document behavior |
| **Signals (external events into a running flow)** | Temporal `Signal` is the primary "human-in-the-loop" / "wait for external event" mechanism. Without it, no approval/wait-for-webhook flows work | MEDIUM | Need a `wait_for_signal(name, [schema], [timeout])` Starlark primitive that compiles to a Temporal selector |
| **Queries (read current state)** | Temporal `Query`. Required for any operational tool that wants to check "what's this flow waiting on?" | LOW | Auto-generate a default `getState` query that returns the DAG's current node + state; no per-flow wiring needed in v1 |
| **Just-in-time credential resolution** | Standard expectation that secrets aren't in workflow state. Failure mode: secrets in event history forever (irrevocable leak) | MEDIUM | Already in PROJECT.md; non-negotiable security feature |
| **Static validation of `.star` files** | Bazel/Buildifier sets the bar: `buildifier` lints Starlark, catches ~100 issues. Consultants writing `.star` files need fast feedback before deploying to Temporal | MEDIUM | Already in PROJECT.md as "static validation tier" — verify kwargs and input schemas without executing |
| **Local replay testing with mocked I/O** | Temporal `testsuite` and time-skipping is the gold standard. Every Temporal SDK has it. Without an equivalent, consultants can't iterate | HIGH | Already in PROJECT.md as Tier 3 (`temporal_test` builtin bridging `testsuite` mocks to Starlark lambdas) |
| **Dev CLI for trigger + inspect** | `temporal cli` exists; users expect `skytime run my_flow.star --input ...`, `skytime inspect <run-id>`, etc. | MEDIUM | Already in PROJECT.md as CLI |
| **Temporal Web UI compatibility (no parallel UI)** | Users expect to see runs/history/event log in *some* UI. PROJECT.md correctly defers to Temporal's UI rather than building one | LOW | Just don't break Temporal UI: keep activity names readable, surface flow name as workflow type, stamp memo with Skytime metadata |
| **Determinism guarantees by construction** | The whole reason to use Temporal. Skytime must not regress this. Starlark's hermetic execution is a feature here, but the extension layer must also be deterministic at parse | MEDIUM | Already a constraint in PROJECT.md; needs guardrails in extension API (e.g., parse phase forbidden from doing I/O) |
| **Schema for extension I/O** | Every extension-based system has this: Airflow operator type hints, Dagster `Out`/`In`, Serverless Workflow `dataInputSchema`. Consultants need to know what an extension takes/returns | MEDIUM | Already in PROJECT.md ("verify kwargs and input schemas"). Likely Go struct + reflection-derived schema |
| **Block-batched I/O / history-size discipline** | Temporal users routinely hit history-size limits on long flows. Conductor and Airflow don't have this problem (different model). Skytime explicitly addresses it | MEDIUM | Already in PROJECT.md; differentiator vs naive "one activity per step" DSLs like Zigflow |

### Differentiators (Competitive Advantage)

These are where Skytime competes. Each must align with the project's Core Value: **"safe author surface + declarative readability on top of Temporal."** Picking too many differentiators dilutes the wedge — recommend committing to the bolded ones for v1.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| **Two-tier authoring model (Go extensions + Starlark consultants)** | The core wedge. No other Temporal DSL has this split. Zigflow is single-tier YAML; Starlark Worker is single-tier Starlark; raw Temporal is single-tier Go | HIGH | Already in PROJECT.md. This is the "headline." |
| **Lambdas instead of string expressions** | Every YAML DSL eventually adds expression languages (CEL, JSONPath, SpEL) — a documented anti-pattern ("every simple language eventually becomes Turing-complete"). Lambdas sidestep this entirely | MEDIUM | Already in PROJECT.md as a hard constraint. Marketing point: "no expression sandbox to escape" |
| **ActionRef Command Pattern (single generic activity)** | Lets the engine batch I/O, mock from one place, and route extension calls without per-extension activity registration. Distinct from Starlark Worker (one HTTP function = one activity) and Zigflow (per-task-type activity) | HIGH | Already in PROJECT.md as a key decision |
| **`temporal_test` builtin bridging `testsuite` to Starlark** | Consultants can write E2E tests *in the same `.star` file* without writing Go test harnesses. No comparable tool offers this — every Temporal DSL today expects you to write tests in the host language | HIGH | Already in PROJECT.md (Tier 3 testing). Strong demo material |
| **Fast static validation (sub-second feedback loop)** | `buildifier`-grade lint + kwarg/schema check before any Temporal call. Consultants get IDE-like feedback without an LSP. Beats Temporal raw (errors only at runtime) and YAML DSLs (schema validation only catches structural errors, not semantic ones like wrong action name) | MEDIUM | Already in PROJECT.md. Could be amplified into an LSP later (deferred) |
| **Credential-ID-only state** | Already standard practice but rarely enforced by the framework. Skytime makes it impossible to do wrong: state is just IDs, resolution happens inside the activity | MEDIUM | Already in PROJECT.md. Compliance/audit selling point |
| Explicit anti-versioning posture ("write new flow file, run side-by-side") | Temporal versioning/patching is one of the top three documented pain points. Skytime can sidestep it by making flow files small, immutable, and content-addressable. New customer change → new `.star` file with new workflow type → run new instances under new type, drain old | MEDIUM | Not yet in PROJECT.md but compatible with "no versioning helpers in v1." Worth mentioning as a *practice*, not a feature |
| Self-documenting flows | Because the DAG is parsed deterministically, you can render `.star` → diagram or `.star` → markdown without running anything. Temporal raw can't do this | MEDIUM | Defer to v2. Useful for consultants demoing to customers |
| Schema-derived autocomplete (eventual LSP) | Long-term: starpls-based LSP that knows your registered extensions and their schemas → Starlark autocomplete with extension-specific knowledge | HIGH | v2+. starpls already exists for generic Starlark |

**Recommended differentiator focus for v1:** the first five (two-tier model, lambdas, ActionRef, `temporal_test`, fast static validation). Together they form the pitch. Everything else is amplification.

### Anti-Features (Commonly Requested, Often Problematic)

The project already has a strong "Out of Scope" list. These are the ones to actively *defend* against scope creep, with the alternative articulated for when someone asks.

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| **String expressions / CEL / JSONPath** | "It's just a string, easier than a lambda" | Every simple expression language ends up Turing-complete with a sandbox-escape surface (per "every simple language eventually" body of analysis). Inconsistent semantics with Starlark host. Hard to type-check | Native Starlark lambdas — same expressiveness, no second language |
| **Web UI / dashboard** | Users want a "single pane of glass" | Temporal already has one. Building a parallel UI is a product decision, not a library decision; would dwarf the rest of the library in maintenance | Temporal Web UI + custom search attributes + memo (already in PROJECT.md) |
| **gRPC / out-of-process plugins** | "Plugin in any language" | Adds a network boundary inside the activity, breaks the "single generic activity" batching, complicates determinism arguments | Static or dynamic-local Go extensions (already in PROJECT.md) |
| **Hot-reload of `.star` files in v1** | "Iterate without restart" | Mid-flight workflows would face the same versioning/non-determinism issues that already plague Temporal users. Solving it well requires content-addressed flows + worker versioning, neither of which is v1 scope | Restart the worker; the parser is already a pure function so `.star` reloads cleanly between runs (already in PROJECT.md) |
| **Workflow versioning helpers / Skytime-specific patching API** | "Make versioning easier" | Patching is fundamentally a Temporal-level concern; a thin wrapper hides edge cases. Better to teach the practice "ship new flow file, run side-by-side, drain old" | Expose Temporal patching primitives raw to advanced users (already in PROJECT.md); document the new-flow-file pattern |
| **Multi-tenant SaaS** | "Could host this for customers" | Different product, different team, different security/SOC2/billing posture. Library distribution doesn't preclude it later | Library now; SaaS is a separate company-level decision (already in PROJECT.md) |
| Native scheduling / cron primitives | "Like Airflow, with `schedule_interval`" | Temporal already has Schedules (a separate API). Adding a Skytime cron primitive duplicates that surface and competes with the host | Document "use `temporal schedule create` pointing at a Skytime workflow type" — one CLI command |
| Asset/lineage tracking (Dagster-style) | "Track outputs as assets" | Wrong domain — Skytime is for orchestration, not data assets. Dagster is the right tool for that domain | Be explicit in positioning: "if your unit of value is a dataset/table, use Dagster; if it's a process/business workflow, use Skytime" |
| Built-in human-task UI | "We need approvals" | Approvals are signals + waits. Building a UI for them is again a product decision | Document the pattern: extension publishes a callback URL via Slack/email; reviewer hits it; that calls Temporal SignalWorkflow. Skytime exposes `wait_for_signal` |
| Pure-Starlark unit testing tier (Tier 2) | "Test my `def` blocks without Temporal" | Already deferred to v2 in PROJECT.md. Tier 1 (static) + Tier 3 (E2E with mocks) covers the 80% case, and Tier 2 needs careful design to not duplicate Tier 3 | Defer to v2; revisit once Tier 3 ships and we see what gaps remain |
| `eval`/dynamic flow construction | "Generate flows at runtime from config" | Breaks the parse/execute separation that is Skytime's *whole reason to exist*. Same kind of error as exposing `eval()` in any sandbox | Generate the `.star` file at deploy time using a code generator; parse runs once at workflow-type registration |
| Cross-flow shared state | "Flows need to share state" | Cross-workflow state in Temporal is signals+queries — accept that and use them. A "shared variable" abstraction would create a hidden coordination point and a determinism foothold | Signals between workflows; or extract shared state to an external system |

## Feature Dependencies

```
Static validation (Tier 1)
    └── requires ──> Extension schema (kwargs + input types)
                          └── requires ──> ActionRef Command Pattern
                                                └── requires ──> Single generic activity

E2E test tier (Tier 3, `temporal_test`)
    └── requires ──> Single generic activity (mock at one place)
    └── requires ──> Starlark execution bridge with state injection
    └── requires ──> Extension registry (so mocks know action names)

for_each_parallel
    └── requires ──> call_flow (parallelism via child workflows is the simplest impl)
                          └── requires ──> Block-batched I/O (otherwise children explode history)

Signals (`wait_for_signal`)
    └── requires ──> Starlark execution bridge able to suspend/resume
    └── enhances ──> on_error/on_failure (signals can be a recovery channel)

Just-in-time credential resolver
    └── requires ──> Single generic activity (resolution happens inside it)
    └── conflicts ──> any "log full action input" diagnostic (would log secrets at boundary)

CLI (run + inspect)
    └── requires ──> Static validation (so `skytime validate` exists)
    └── enhances ──> Tier 3 testing (CLI runs tests)
```

### Dependency Notes

- **Static validation requires extension schemas:** without typed I/O on extensions, "static validation" can only check Starlark grammar — that's just `buildifier`. The differentiator is *semantic* validation against the registered extension catalog.
- **`temporal_test` requires single generic activity:** mocking is dramatically simpler when there's one place to intercept. If extensions registered their own activities, the mock surface would be N-large and per-extension; with one generic activity dispatching ActionRefs, mocks live at the dispatch boundary.
- **`for_each_parallel` enhances rather than competes with `call_flow`:** in Temporal, parallel iteration with isolation is naturally child workflows. A pure-in-workflow goroutine fan-out is also possible (via `workflow.Go`), but child-workflow fan-out gives history isolation per item, which is the main reason to want parallel-foreach in the first place.
- **`wait_for_signal` conflicts with naive determinism checks:** the signal payload affects future decisions, so it must be deterministically replayable from history. Temporal handles this; Skytime just needs to not break it (don't peek at signal payloads in the parse phase).
- **Credential resolver conflicts with diagnostic logging:** if Skytime ever wants to log "what was passed to action X," it must do so *before* credential resolution (when state still has IDs). This is a constraint on the diagnostic surface, not a v1 problem to solve, but worth flagging.

## MVP Definition

### Launch With (v1)

The minimum viable Skytime is "a consultant can write a `.star` file, validate it, run it on Temporal, and write a test for it." Everything below is essential for that loop.

- [ ] **Starlark DSL primitives**: `flow`, `step`, `if_cond`, `script`, `for_each_parallel`, `call_flow` — without these, no flow can be expressed
- [ ] **Sequential, conditional, parallel composition** — table stakes; the project doesn't exist without them
- [ ] **Per-step retries + timeouts** — passes through to Temporal `RetryPolicy` and activity timeouts; no value in Skytime without these
- [ ] **Cancellation propagation** — inherited from Temporal; needs to be tested but not invented
- [ ] **`call_flow` (sub-workflow)** — required for both modularity and Temporal history-size discipline
- [ ] **Extension registry with typed kwargs** — without this, no static validation, no autocomplete potential, no `temporal_test` mocks
- [ ] **ActionRef Command Pattern** — extensions return intents, never register Temporal activities
- [ ] **Single generic Temporal activity with block-batched I/O** — the architectural differentiator; required for batching, mocking, and credential resolution to all live in one place
- [ ] **Just-in-time credential resolver** — required for any production use; security non-negotiable
- [ ] **Static validation tier** — fast feedback loop; the consultant's safety net
- [ ] **Tier 3 E2E testing (`temporal_test` builtin)** — without this, consultants can't iterate confidently
- [ ] **Dev CLI** — `skytime run`, `skytime validate`, `skytime test` (or whatever names); the entry point for the whole experience
- [ ] **Example project (HTTP + GitHub + Slack extensions)** — this is the proof-of-life and the documentation; must exercise every primitive (already in PROJECT.md as a requirement)
- [ ] **Temporal Cloud + self-hosted compatibility** — must work on both; constraint already in PROJECT.md

### Add After Validation (v1.x)

Add once core is working and a real customer is using it. Trigger: first paying/serious consultant team, or first feature gap they hit.

- [ ] **Signals primitive (`wait_for_signal`)** — second-most-cited gap after retries; needed the moment any human-in-the-loop or wait-for-webhook flow comes up
- [ ] **Default Query handler** — auto-generated `getCurrentNode`/`getState` query without per-flow boilerplate; trivial once the interpreter walks a DAG
- [ ] **`on_error`/`on_failure` hooks** — error-handling sugar beyond per-step retries; lets flows do "always notify on failure" without wrapping every step
- [ ] **Saga / compensation pattern sugar** — declarative `compensate` per step that runs in reverse on flow failure; it's a common-enough pattern that hand-rolling it in lambdas gets old. Triggered when first customer needs a rollback story
- [ ] **Schema export** — render extension catalog to JSON Schema or markdown; enables external doc generation and feeds into a future LSP
- [ ] **Search-attribute helpers** — let flows tag themselves with custom Temporal search attributes via a Starlark primitive (e.g., `set_search_attribute("customer_id", x)`)
- [ ] **Better diagnostics** — structured error messages from static validation pointing at line/column in `.star` (table stakes for "fast feedback" but easy to ship in a basic form first)

### Future Consideration (v2+)

Defer until product-market fit and validated demand.

- [ ] **Tier 2 unit-testing (pure-Starlark `def`-block testing)** — already deferred in PROJECT.md; revisit once Tier 3 is in real use
- [ ] **Hot-reload of `.star` files** — the parser is already a pure function so the door is open, but actual hot-reload requires worker versioning + content-addressed flows. Big design-space; defer
- [ ] **LSP / IDE plugin** — starpls handles generic Starlark; a Skytime-aware LSP would know your extension catalog. High value, high effort
- [ ] **gRPC / out-of-process plugins** — only if a customer brings a non-Go extension need that can't be solved by writing a thin Go shim
- [ ] **Flow visualization (`.star` → diagram)** — useful for consultant→customer demos, but Temporal Web UI already shows the run-time graph; pre-run rendering is a "nice to have"
- [ ] **Saga/compensation as first-class with rollback DAG generation** — beyond the v1.x sugar, this would generate a full reverse DAG with its own retry/timeout policies. Big design space
- [ ] **Skytime-specific versioning helpers** — only if community pressure becomes overwhelming. Default position: write new flow file, drain old workflows
- [ ] **Multi-tenant hosted SaaS** — explicit out-of-scope for the library; would be a separate product

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Starlark DSL primitives (`flow`, `step`, `if_cond`, etc.) | HIGH | MEDIUM | P1 |
| Per-step retries + timeouts | HIGH | LOW | P1 |
| `call_flow` (sub-workflow) | HIGH | MEDIUM | P1 |
| `for_each_parallel` | HIGH | MEDIUM | P1 |
| ActionRef Command Pattern | HIGH | HIGH | P1 |
| Single generic activity + block-batched I/O | HIGH | HIGH | P1 |
| Extension registry with typed kwargs | HIGH | MEDIUM | P1 |
| Just-in-time credential resolver | HIGH | MEDIUM | P1 |
| Static validation tier | HIGH | MEDIUM | P1 |
| Tier 3 E2E testing (`temporal_test`) | HIGH | HIGH | P1 |
| Dev CLI (run + validate + test) | HIGH | MEDIUM | P1 |
| Cancellation propagation (inherit from Temporal) | HIGH | LOW | P1 |
| Example project (HTTP + GitHub + Slack) | HIGH | MEDIUM | P1 |
| Signals (`wait_for_signal`) | HIGH | MEDIUM | P2 |
| Default Query handler | MEDIUM | LOW | P2 |
| `on_error`/`on_failure` hooks | MEDIUM | MEDIUM | P2 |
| Saga / compensation sugar | MEDIUM | MEDIUM | P2 |
| Search-attribute helpers | MEDIUM | LOW | P2 |
| Schema export (JSON Schema / markdown) | MEDIUM | LOW | P2 |
| Tier 2 unit-testing | MEDIUM | HIGH | P3 |
| Hot-reload | MEDIUM | HIGH | P3 |
| LSP / IDE plugin | HIGH | HIGH | P3 |
| Flow visualization | LOW | MEDIUM | P3 |
| gRPC / out-of-process plugins | LOW | HIGH | P3 |

**Priority key:**
- **P1**: Must have for v1 launch — defining the product
- **P2**: Should have, add when first customer hits the gap
- **P3**: Nice to have, evaluate after PMF

## Competitor Feature Analysis

Comparing Skytime's planned posture vs the closest existing tools, anchored to the question "what does Skytime offer that these don't?"

| Feature | Temporal (raw Go SDK) | Cadence Starlark Worker | Zigflow (YAML DSL for Temporal) | Airflow / Prefect / Dagster | Skytime |
|---------|----------------------|-------------------------|-----|-----|---------|
| **Primary author** | Go/Java/TS engineer | Single-tier Starlark author | YAML author | Python author | **Two-tier**: Go extension dev + Starlark consultant |
| **DSL** | Host language (Go) | Starlark, single tier | YAML | Python decorators | Starlark with naked primitives, parse-time DAG generation |
| **Sequential / conditional / parallel** | Native control flow | Native Starlark control flow | YAML `switch`, `parallel` | Native Python | `flow`/`step`/`if_cond`/`for_each_parallel` |
| **Sub-workflow** | `workflow.ExecuteChildWorkflow` | n/a (single Starlark file) | Workflow refs | SubDAG / asset graph | `call_flow` |
| **Retries + timeouts** | `RetryPolicy` + activity timeouts | Inherited from Cadence | YAML keys, mapped to Temporal | Per-task retries | Inherited from Temporal, exposed as Starlark kwargs |
| **Signals** | `workflow.GetSignalChannel` | n/a | YAML `wait` | Sensor-based (different model) | `wait_for_signal` (P2 in Skytime v1.x) |
| **Cancellation** | Native | Inherited | Inherited | Per-task | Inherited from Temporal |
| **Extension model** | Activities (per-extension) | Adapter interfaces in Go | Bound by YAML schema | Operators (Airflow), Tasks (Prefect), Assets (Dagster) | **ActionRef Command Pattern** — extensions never register activities |
| **Mocking / testing** | `testsuite` mocks per activity | Not documented | Limited (YAML structural tests) | Per-tool test harnesses | **`temporal_test` builtin** — Starlark-native E2E with `testsuite` underneath |
| **Static validation** | Compiler (Go) | Starlark parse | YAML schema | Python typecheck (mypy etc.) | Custom semantic check against extension registry kwargs |
| **History-size management** | Manual (Continue-As-New, child workflows) | Same as Temporal | One-activity-per-step (worse) | Different model (no per-flow history) | **Block-batched I/O** in single generic activity |
| **Credential handling** | Developer's problem | Developer's problem | YAML data passing (risk) | Connection abstraction | **Just-in-time resolver** — IDs only in state |
| **Versioning story** | Patching API | Inherited | Inherited | Per-tool | Document "new flow file" practice; no Skytime-specific helpers |
| **Web UI** | Temporal Web UI | Temporal/Cadence Web UI | Temporal Web UI | Each tool's own UI | Defer to Temporal Web UI |

**Where Skytime wins (vs each):**
- vs **raw Temporal**: dramatically more readable for non-Go authors; safer kwargs / schema check; no Temporal SDK surface in the consultant's lap
- vs **Starlark Worker**: two-tier model lets a *team* of Go authors maintain extensions while *another team* of consultants composes them; ActionRef pattern lets I/O batch and mock from one place
- vs **Zigflow** (or any YAML DSL): lambdas instead of expression strings; expressive control flow without templating layers; testing is a first-class language feature
- vs **Airflow/Prefect/Dagster**: Temporal-grade durability (replay, signals, queries, deterministic event history) — these tools have weaker durability stories or different domain (data assets vs business workflows)

**Where Skytime concedes:**
- vs Airflow: ecosystem of operators (1000+) — Skytime starts with HTTP+GitHub+Slack
- vs Dagster: no asset/lineage tracking — wrong domain for Skytime
- vs Camunda BPMN: no graphical modeler / business-analyst tooling — explicit non-goal
- vs raw Temporal Go SDK: Go developers giving up direct SDK access lose some advanced features (custom data converters, advanced selectors). Mitigation: Go developers write extensions, not flows; the surface they need is fully available

## Sources

- [Code Exchange - Temporal DSL | Temporal](https://temporal.io/code-exchange/temporal-dsl)
- [Zigflow: The Missing Temporal DSL — Simon Emms](https://simonemms.com/blog/2026/02/02/zigflow-the-missing-temporal-dsl)
- [Why I built a YAML DSL for Temporal workflows | Zigflow](https://zigflow.dev/articles/why-i-built-a-yaml-dsl-for-temporal-workflows/)
- [DSL-Based Temporal Workflow Orchestration: Part 2 — DSL Concepts & Syntax (Naresh V, Medium)](https://medium.com/@nareshvenkat14/dsl-based-temporal-workflow-orchestration-part-2-dsl-concepts-syntax-2100cd8e1d50)
- [Temporal Workflow | Temporal Platform Documentation](https://docs.temporal.io/workflows)
- [Handling Signals, Queries, & Updates | Temporal Platform Documentation](https://docs.temporal.io/handling-messages)
- [Child Workflows | Temporal Platform Documentation](https://docs.temporal.io/child-workflows)
- [Testing - Go SDK | Temporal Platform Documentation](https://docs.temporal.io/develop/go/testing-suite)
- [Temporal Visibility | Temporal Platform Documentation](https://docs.temporal.io/visibility)
- [Temporal Web UI | Temporal Platform Documentation](https://docs.temporal.io/web-ui)
- [Spooky Stories: Chilling Temporal anti-patterns (part 1) | Temporal](https://temporal.io/blog/spooky-stories-chilling-temporal-anti-patterns-part-1)
- [Workflow Orchestration Platforms: Kestra vs Temporal vs Prefect (procycons)](https://procycons.com/en/blogs/workflow-orchestration-platforms-comparison-2025/)
- [Temporal vs Restate vs Windmill 2026 (PkgPulse)](https://www.pkgpulse.com/blog/temporal-vs-restate-vs-windmill-durable-workflow-2026)
- [Open-Sourcing Starlark Worker: Define Cadence Workflows with Starlark | Uber Blog](https://www.uber.com/en-IN/blog/starlark/)
- [cadence-workflow/starlark-worker on GitHub](https://github.com/cadence-workflow/starlark-worker)
- [A Letter to Cadence/Temporal Community (Long Quanzheng, on iWF rationale)](https://medium.com/@qlong/a-letter-to-cadence-temporal-and-workflow-tech-community-b32e9fa97a0c)
- [Workflow Should be Code, but Durable Execution is NOT the ONLY way (Long Quanzheng)](https://medium.com/@qlong/workflow-should-be-code-but-durable-execution-is-not-the-only-way-519f7682360c)
- [Every Simple Language Will Eventually End Up Turing Complete – The Solution Space](https://solutionspace.blog/2021/12/04/every-simple-language-will-eventually-end-up-turing-complete/)
- [On YAML Discussions - Earthly Blog](https://earthly.dev/blog/on-yaml-discussions/)
- [Starlark Language | Bazel](https://bazel.build/rules/language)
- [withered-magic/starpls (Starlark LSP)](https://github.com/withered-magic/starpls)
- [Buildifier Recommendations and Resources | Aspect Build](https://blog.aspect.build/buildifier)
- [stripe/skycfg (Starlark + protobuf for configuration)](https://github.com/stripe/skycfg)
- [Tiltfile Concepts | Tilt](https://docs.tilt.dev/tiltfile_concepts.html)
- [Workflow patterns | Camunda 8 Docs](https://docs.camunda.io/docs/components/concepts/workflow-patterns/)
- [Workflow patterns | Dapr Docs](https://docs.dapr.io/developing-applications/building-blocks/workflow/workflow-patterns/)
- [Human-in-the-Loop in Agentic Workflows (Orkes)](https://orkes.io/blog/human-in-the-loop/)
- [Mastering Error Handling in Apache Airflow (Vishal Singh, Medium)](https://medium.com/towards-data-engineering/mastering-error-handling-in-apache-airflow-retries-alerts-and-recovery-strategies-eb075ca78f86)
- [Orchestration Showdown: Dagster vs Prefect vs Airflow (ZenML)](https://www.zenml.io/blog/orchestration-showdown-dagster-vs-prefect-vs-airflow)
- [Temporal Alternatives: 9 Tools (ZenML)](https://www.zenml.io/blog/temporal-alternatives)
- [The 10 best Temporal alternatives for enterprise teams (Akka)](https://akka.io/blog/temporal-alternatives)

---
*Feature research for: durable workflow engine — Skytime (Starlark DSL on Temporal)*
*Researched: 2026-04-26*
