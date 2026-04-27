# Pitfalls Research

**Domain:** DSL-over-workflow-engine — Go host embedding Starlark and executing on Temporal
**Researched:** 2026-04-26
**Confidence:** HIGH on Temporal determinism / replay (Context7 + official docs); HIGH on Starlark freeze/closure semantics (official spec); MEDIUM on the *seams* between the three systems (extrapolated from primary docs — Skytime's specific architecture has no published precedent).

This file is opinionated about Skytime's three-system seam (Go + Starlark + Temporal). Generic Temporal advice ("use idempotent activities") and generic Starlark advice ("set MaxExecutionSteps") is collapsed into one-liners; the depth is reserved for pitfalls that **only show up because all three systems share state at parse-time and at execute-time**.

The PROJECT.md's invariants ("no string compilation", "no dynamic activities", "no context bleed") are the front line of defense against most of these pitfalls. Where a pitfall threatens an invariant, that's called out explicitly.

---

## Critical Pitfalls

### Pitfall 1: Re-using a Starlark Thread Across the Parse/Execute Boundary

**What goes wrong:**
A `*starlark.Thread` is created at parse time to evaluate the `.star` file. Lambdas captured during that parse retain references to that thread's environment. Later, inside the Temporal workflow, the interpreter wants to evaluate one of those lambdas and reaches for the thread it has — possibly the parse-time thread, possibly an activity-side thread, possibly one carried in a struct. Bugs that follow:

- The parse-time thread holds locals (`SetLocal`) that referenced parse-time configuration (a logger, a registry, a `context.Context` that has since been cancelled). Lambda execution at workflow time picks up stale or nil values.
- The thread reference accidentally escapes into a payload that gets serialized for Temporal — the `*starlark.Thread` is not gob/JSON-serializable in any meaningful way and *certainly* not stable across worker restarts.
- Two concurrent `for_each_parallel` branches share one thread → undefined behavior; Starlark threads are not safe for concurrent use.

**Why it happens:**
Starlark's `Thread` type is mis-named for someone coming from Go. It is **not** a goroutine; it is "evaluator state for one independent execution." Every published Starlark sample reuses one thread because every published sample is a one-shot script. Skytime's two-phase model (parse → execute, possibly months later, possibly on a different machine, definitely in a `workflow.Context`) is not what the Starlark API was designed for, so the right pattern isn't documented anywhere.

**How to avoid:**
- **Architectural rule:** A `*starlark.Thread` exists for the duration of one Starlark evaluation and never crosses a function boundary that returns Go values. The parse phase returns `*Flow` (the DAG), with lambdas captured as `*starlark.Function` values inside DAG nodes — *no thread reference*.
- **At lambda invocation time** (inside the workflow interpreter), construct a **fresh** `*starlark.Thread` per call. Set its `Print` to route to `workflow.GetLogger(ctx)` (which is replay-safe). Set thread-locals only with values derived from the workflow state being passed in.
- **Never** put a `*starlark.Thread` into any field of any struct that the workflow stores or that is passed to an activity. The only Starlark value-types that should live in workflow state are frozen plain-data values (after conversion to/from Go via the `starlarkstruct` bridge).
- **Concurrency:** for `for_each_parallel`, each branch gets its own fresh thread. Never share.

**Warning signs:**
- Any test where lambda execution works on the first call but fails after a workflow replay.
- A struct field of type `*starlark.Thread` anywhere outside the parser package.
- "It works on dev, breaks on prod" — usually means a parse-time thread held a reference (e.g., to stdout, a config) that doesn't exist on the worker.
- `workflowcheck` complaining about `os.Stdout` reachable from workflow code (parse-time thread leaked).
- Test assertion like `thread.Local("x")` returning unexpected values inside a lambda — the thread isn't the one you think it is.

**Phase to address:**
**Phase 2 (Interpreter)** must establish the thread-per-invocation pattern and an explicit "parse thread is dead at end of parse" lifecycle. Phase 1 (DSL primitives) must ensure DAG nodes capture `*starlark.Function` and frozen value defaults — never `*starlark.Thread`.

---

### Pitfall 2: Lambdas Capture Mutable State at Parse Time

**What goes wrong:**
A `.star` file does:

```python
counter = [0]
my_step = step("incr", lambda state: counter.append(len(counter)) or counter[-1])
```

At parse time, `counter` is a Starlark list captured by the lambda's closure. After parsing, Skytime freezes the module — Starlark spec mandates this — so `counter` becomes immutable. **But:** the lambda was captured *before* the freeze took effect, and the freeze of the module also freezes the lambda's free variables. If something doesn't freeze (a Go-bridged value with a custom `Freeze()` no-op), or freezing happens too late, the lambda has effectively non-deterministic captured state — different runs, different replays, see different values.

A subtler version: if the parse phase is run only once at process start and many workflows share the resulting `*Flow`, then any *un*frozen mutable Go value reachable from a lambda's closure becomes a hidden global, racing across workflow invocations.

**Why it happens:**
Starlark spec says module init produces frozen globals — but only for *Starlark* values. If you pre-declare a Go-bridged `starlark.Value` whose `Freeze()` is a no-op (a common bug in custom value types), the closure stays mutable. Also, lambdas capture by reference to the variable cell, not by value — even after freeze, the *cell* contents at evaluation time are what matter, and a non-Starlark capture (a Go pointer plumbed through a builtin's return) bypasses the freeze entirely.

**How to avoid:**
- **Audit every custom `starlark.Value` type for a correct `Freeze()` implementation.** Anything bridged from Go must either be inherently immutable or freeze recursively. The `*starlarkstruct.Struct` bridge mentioned in PROJECT.md has correct freeze semantics; any custom type added later must too.
- **Reject mutable captures statically.** During parse, inspect `*starlark.Function`'s free variables (via `NumFreeVars()`/`Freevar(i)`). If any free variable points to a mutable type (list, dict not frozen post-init, custom Go-bridged value), reject the file with a clear error.
- **The DAG must be the only state.** Lambdas should read `state` (the parameter) and return values — they should not read free variables for anything other than predeclared constants. Encourage / lint for "pure lambda" style.
- **Recommend top-level `def` over lambda for non-trivial logic** — they have the same closure semantics but are easier to audit.

**Warning signs:**
- A lambda's behavior depends on call order ("the first run gives 0, the second 1").
- Replay tests pass once and fail on re-run within the same process.
- Two workflow executions started from the same parsed `*Flow` interfere.
- Heap profile shows a large object retained by a `*starlark.Function`.

**Phase to address:**
**Phase 1 (DSL primitives)** — lambda capture analysis and the freeze contract for bridged values. **Phase 5 (Static validation)** — a lint that rejects lambdas with mutable free variables.

---

### Pitfall 3: Determinism Violations Hidden Inside Lambdas

**What goes wrong:**
Skytime's selling point is that consultants write Starlark, not Go, and the host enforces safety. But Starlark lambdas can still produce non-deterministic results in ways that Temporal will later refuse to replay:

- A lambda iterates a `dict` (Starlark `dict` iteration is insertion-ordered, but if the dict was built from a Go-bridged map, the *Go* iteration order leaked in at construction time is non-deterministic).
- A lambda calls a predeclared global like `now()` or `random()` that the extension developer naïvely added. From Starlark's perspective this is just a function call; from Temporal's perspective it's a determinism violation.
- A lambda calls `print()` and the `Print` hook side-effects (writes to a file, increments a metric not via `workflow.GetMetricsHandler`).
- A lambda reads `os.Environ`-equivalent state that the host accidentally exposed.

Because Starlark hides the call inside an opaque lambda that the workflow interpreter dutifully invokes, `workflowcheck` can't see it — the static analyzer only sees the interpreter calling `starlark.Call`, which looks deterministic. **The non-determinism is invisible to the standard tooling.**

**Why it happens:**
The Temporal community has a strong toolchain (`workflowcheck`, replay tests) for Go-only workflows. Skytime is creating a new layer where the workflow logic is opaque to those tools. Extension developers will be tempted to expose useful-looking globals (`now`, `uuid`, `http_get` for parse-time-only fetching) that violate determinism at evaluation time.

**How to avoid:**
- **Explicit allowlist for predeclared globals.** PROJECT.md already enforces "extensions return ActionRef intents, never register Temporal activities." Extend the rule: predeclared globals available *inside the workflow* (i.e., available to lambdas at evaluation time) are limited to pure functions. Time, randomness, network, and any I/O must be expressible only as `ActionRef` intents that flow through the generic activity.
- **Two predeclared environments.** One for parse-time top-level Starlark code (can be richer — file inclusion via `load`, registry lookups). One for lambda-execution time (severely restricted: arithmetic, string ops, struct access, frozen-collection iteration). Lambdas evaluated in the workflow get the restricted environment.
- **Replay testing extends to Starlark.** Phase 6's E2E testing tier must include a "run twice with same inputs, compare DAG-traversal trace" assertion. Mismatch → fail with a determinism error pointing at the offending lambda's source line.
- **Map iteration warning:** the Go SDK already says don't iterate Go maps in workflows. The same applies to lambdas — iterating a `dict` whose origin is a Go map is forbidden. The bridge layer should convert Go maps to *sorted* Starlark dicts at the boundary.

**Warning signs:**
- `panic: non-deterministic workflow definition` in tests after refactoring a lambda.
- An extension developer asking "can I expose `now()` to lambdas?" — the answer is no, route via `ActionRef`.
- A predeclared global with side-effects in its Go implementation (anything that touches `time`, `rand`, `net`, `os`, `sync`).
- Replay test divergence at a specific step ID with a lambda involvement.

**Phase to address:**
**Phase 2 (Interpreter)** establishes the two-environment split. **Phase 5 (Static validation)** rejects predeclared globals with side-effecting Go implementations from the lambda environment. **Phase 6 (E2E testing)** adds determinism replay tests to the test harness.

---

### Pitfall 4: Context Bleed — `workflow.Context` Reachable from a Starlark Thread (or Vice Versa)

**What goes wrong:**
The interpreter receives `workflow.Context` from Temporal. To evaluate a lambda, it constructs a Starlark thread. The temptation: stash `ctx` on the thread via `SetLocal("workflow_ctx", ctx)` so a builtin can reach it later.

This breaks PROJECT.md's "no context bleed" invariant in two directions:
- **Workflow → Starlark:** if a Starlark builtin can reach `workflow.Context`, it can call `workflow.ExecuteActivity` directly — bypassing the `ActionRef` indirection, defeating block-batched I/O, defeating the ability to mock at the Starlark layer.
- **Starlark → Activity:** if a `*starlark.Thread` ends up reachable from an activity (e.g., embedded in a payload because someone made a custom value type that holds a thread), the activity is now coupled to the workflow process state, and serialization/deserialization will produce silent garbage.

Even short of those direct violations, more subtle leaks matter: if a lambda's closure transitively holds `workflow.Context`, garbage collection retention bloats workflow memory; if that closure outlives the workflow task, you're holding a stale context.

**Why it happens:**
`SetLocal` is the obvious "Starlark equivalent of context.Value" and it's natural to pour everything into it. The Starlark API doesn't push back. Activity handlers in extensions need *some* context — they need cancellation, deadlines, credentials — and the path of least resistance is to pass `workflow.Context` through. PROJECT.md's invariant prevents this only if the architecture makes the right thing easier than the wrong thing.

**How to avoid:**
- **Thread-locals for the lambda thread are limited to a fixed allowlist:** the workflow run ID (string), the attempt number (int), a cancellable shim (a `<-chan struct{}` derived from `ctx.Done()` but not `ctx` itself). Never `workflow.Context`.
- **Extension activity functions take `context.Context` (Go's stdlib), not `workflow.Context`.** The generic activity is the only code that sees `workflow.Context`; it adapts to `context.Context` before calling extensions. This is the single point of credential resolution and cancellation translation.
- **`*starlark.Thread` and `*starlark.Function` are forbidden in any payload type.** Enforce via:
  - Never include them in workflow input/output structs.
  - Lint check: any type used as activity input/output is checked recursively for these types.
  - The `ActionRef` payload must be Go-native (strings, numbers, structs). Starlark values get converted to Go at the activity boundary.
- **No `unsafe.Pointer`-style shortcuts.** A clever developer might serialize a thread ID and look it up at the activity side. Prohibit by code review.

**Warning signs:**
- Compiler error mentioning `workflow.Context` in a file under a Starlark builtin package.
- `SetLocal` calls in interpreter code with anything other than the allowlisted keys.
- A test using `testsuite` that suddenly fails because activity inputs are unexpectedly large (probably because a thread or function got captured).
- Extension developers asking "how do I get the workflow ID inside my activity" — this is fine via context but they should never reach for `workflow.Context`.

**Phase to address:**
**Phase 2 (Interpreter)** establishes the bridge boundary and the Go-context adapter. **Phase 4 (Extension API)** enforces that extension function signatures take `context.Context`, not `workflow.Context`. **Phase 5 (Static validation)** can lint payload types for forbidden types.

---

### Pitfall 5: Block-Batched Activity Has Ambiguous Partial-Failure Semantics

**What goes wrong:**
PROJECT.md mandates block-batched I/O: multiple `ActionRef`s in one step run sequentially in one activity invocation, to avoid Temporal history bloat. Now consider:

- Step has actions A, B, C. A succeeds, B fails with a retryable error, C never runs.
- Temporal sees the activity as failed; it retries the *entire* step. A runs *again*. If A is idempotent (creating the same row), great. If A is "post a Slack message", **the message gets posted twice**.
- The user never sees A's individual outcome — only the step-level error. They have no way to know A "succeeded twice."

Or:

- Action B fails with a non-retryable error. C is skipped. The lambda for the *next* step depends on B's output, which doesn't exist. Either the next lambda crashes (poor UX), or it sees a `None` and silently continues (data corruption).

Or:

- Total activity execution exceeds `StartToCloseTimeout` because the batch is too large. The user set a 30-second timeout thinking it was per action; actually it's for the whole batch.

**Why it happens:**
Block-batching is the right answer for history-size reasons, but it conflicts with Temporal's per-activity retry/timeout/failure model. Users will have a Temporal mental model ("each action is an activity") that doesn't match the implementation. Documentation alone won't save them — the failure modes need to be designed away.

**How to avoid:**
- **Idempotency is a contract on the extension function**, not on the user's lambda. Extension authors must declare in their typed wrapper whether the action is idempotent. Skytime refuses to batch non-idempotent actions; they get one-action-per-activity-invocation.
- **The generic activity returns a per-action result list, not just a final value.** Each entry: success-with-output, retryable-failure, non-retryable-failure, skipped. The interpreter then decides workflow-level semantics: a non-retryable failure short-circuits the rest of the block; a retryable failure causes the *whole batch* to retry (via Temporal); successful actions are recorded so the next lambda sees them.
- **Compose timeouts intentionally.** The block's `StartToCloseTimeout` is the *sum* of the individual action timeouts plus headroom, not an arbitrary user-set value. Document this; default it for users.
- **Heartbeat from inside the batched activity** between actions. Long batches without heartbeats will be killed and retried, which is the worst case.
- **Prefer one-action-per-activity for non-idempotent or long-running actions.** Block-batching is an optimization, not a default — measure history size and engage only when needed.

**Warning signs:**
- An extension function with side effects but no idempotency key — refuse to batch.
- Step retried twice; logs show "action A: 2 successes" — definitionally a bug.
- E2E tests that pass with a single action per step but fail when actions are batched.
- A `step()` with a long action list and no per-action mocking in `temporal_test`.

**Phase to address:**
**Phase 3 (Generic activity & block batching)** establishes the per-action result protocol and idempotency contract. **Phase 4 (Extension API)** requires extension authors to declare idempotency. **Phase 6 (E2E testing)** must exercise partial-failure scenarios with `attempt`-aware mocks.

---

### Pitfall 6: Credentials Leaking via Error Messages, Logs, or Replay History

**What goes wrong:**
PROJECT.md says workflow state holds only credential IDs; the resolver runs just-in-time inside the activity. Good — but the surface area is wider:

- An extension's HTTP wrapper builds a URL like `https://api.example.com?token=<resolved>`. The HTTP client logs the URL on retry. Now the secret is in worker stdout, in Datadog, in the Temporal Cloud audit log if a custom `Print` hook routes there.
- An action fails with `error: 401 Unauthorized: Bearer abc123 invalid`. Temporal serializes the error. **The error message goes into the workflow event history.** The history is encrypted only if a payload codec is configured; the Web UI may render it; reset/replay surfaces it.
- A lambda receives the resolved credential because someone added a `with_credential` builtin "for convenience" in tests. The credential is now in workflow state.
- The credential resolver itself caches across workflow invocations and returns stale-rotated credentials, which then fail with messages including the stale credential.

**Why it happens:**
"Credentials never enter workflow state" is easy to enforce at the input boundary. It is hard to enforce at the *output* boundary (errors, logs, exception traces) because failures can include any stack content. Temporal's encryption story (payload codec) is opt-in; if not configured, error strings are stored in plaintext in event history.

**How to avoid:**
- **The resolver returns a typed `Credential` value with a `Redacted` `String()` method.** The extension's HTTP wrapper takes a `Credential`, never a raw string. The wrapper applies it to the request without ever logging it.
- **Wrap every extension activity invocation in a credential-scrubbing error handler.** Before the error returns to Temporal, run it through a scrubber that strips any string that looks like a token, JWT, AWS key prefix, etc. Blunt but effective; it's the last line of defense.
- **Recommend a Temporal payload codec for production.** Even with scrubbing, mistakes happen. Document that example projects ship with a codec configuration template.
- **Never put resolved credentials on the Starlark thread or in lambda inputs.** Lambdas see credential *IDs*, never values. Credential resolution is strictly a Go-side activity-time concern.
- **Audit the `Print` hook.** It should route to `workflow.GetLogger` (replay-safe and goes through Temporal's logger which can be configured) and not to stdout in production.
- **Test for it.** A unit test that injects a known fake-secret as a credential, runs a flow that fails, and asserts the secret string does not appear in the workflow event history.

**Warning signs:**
- A `String()` method on a credential type that returns the raw value.
- An extension wrapper that takes `string` for a credential parameter (it should take `Credential`).
- Any log line in worker output that contains an `Authorization:` header or a token-like string.
- A test that passes a real credential — they should always be fakes scrubbed at the assertion.
- Default `Print` hook routing to stdout in production builds.

**Phase to address:**
**Phase 4 (Extension API)** — `Credential` type + scrubbing extension contract. **Phase 7 (Production hardening / observability)** — payload codec recommendations, scrubber as middleware around generic activity. The scrubber test is part of the Phase 6 test harness.

---

### Pitfall 7: Error Reporting Loses Source Location Across Three Languages

**What goes wrong:**
A workflow fails. The user gets a stack trace. The trace says:

```
panic: nil pointer dereference
  at github.com/x/skytime/interpreter.(*Interpreter).callLambda (line 142)
  at github.com/x/skytime/activity.(*Generic).Execute (line 87)
  at go.temporal.io/sdk/internal.(*activityExecutor).Execute (line 412)
```

Where in the `.star` file is the bug? Nowhere visible. Was it a parse-time issue? An evaluation issue? A bug in extension X? The user goes back to logs, finds a 50MB stack trace, eventually grep s for clues.

This breaks the "consultants write Starlark, not Go" promise: if any failure forces them into Go stack traces, Skytime has failed at its abstraction.

**Why it happens:**
Three error provenances mix: Starlark eval (has line/col in `.star`), Go panic in interpreter (no Starlark context), Temporal activity failure (wraps the Go error and may strip the originating frame). The Starlark API exposes `EvalError` with backtraces, but only if you catch errors at the eval boundary; once they pass through Go's normal error returns, the Starlark frames are gone.

**How to avoid:**
- **Capture Starlark error context at the eval boundary.** Every place the interpreter calls `starlark.Call`, wrap the error: if it's an `*starlark.EvalError`, attach `err.Backtrace()` to a structured error type. If it's a Go error, annotate with the Starlark callsite that triggered it (the DAG node has a `Position` from parse).
- **DAG nodes carry a `syntax.Position`** captured at parse time (file, line, col). Every step, every lambda, every action ref includes its parse location. Errors include it.
- **Error rendering for users is Starlark-first, Go-last.** Format: `error in <file.star>:<line>:<col> ('flow_name' > step 'X' > action B): <message>`. A `--debug` flag reveals the Go stack for library developers.
- **Activities propagate structured failure info.** Use `temporal.NewApplicationError` with details that include the Starlark callsite. The interpreter unwraps and re-presents.
- **Don't `panic` for user errors.** Distinguish library bugs (panic, library author's problem) from user bugs (return an error with location, user's problem).

**Warning signs:**
- An error message in tests that has no `.star` filename in it.
- The user has to ask "what does line 142 of `interpreter.go` mean?"
- A `panic` in interpreter code on bad user input.
- `EvalError.Backtrace()` not in the codebase anywhere.

**Phase to address:**
**Phase 1 (DSL primitives)** — DAG nodes carry `syntax.Position`. **Phase 2 (Interpreter)** — structured error types and the eval-boundary wrap. **Phase 5 (Static validation)** — same error format used for static errors so users learn it once.

---

### Pitfall 8: `workflow.Context` Lifecycle Mismatch with the Interpreter Loop

**What goes wrong:**
The interpreter walks a DAG inside the workflow. Each step waits for an activity (block-batched). Between steps, the interpreter evaluates lambdas to compute branching, parameters, etc.

- Cancellation: the user signals the workflow to cancel. Temporal cancels `workflow.Context`. The interpreter is mid-lambda; the lambda is a CPU loop. Without a cancellation check, the lambda runs to completion (or forever).
- Selector / channel handling: the interpreter uses `workflow.Future` from activities. If it doesn't compose properly with selector loops, signals arriving mid-step get dropped or processed in non-deterministic order.
- Workflow termination: the workflow returns. But a goroutine spawned by `workflow.Go` (perhaps for a background `for_each_parallel` branch) is still running. Temporal will block on it; the workflow won't complete.

**Why it happens:**
Skytime is layering its own loop on top of Temporal's coroutine scheduler. Subtle interactions with `workflow.Go`, `workflow.Channel`, selectors, and `workflow.Context` cancellation are easy to get wrong, and the symptoms are usually "workflow runs forever" — debugging requires reading event histories, not stack traces.

**How to avoid:**
- **Thread `workflow.Context` cancellation into Starlark via `Thread.Cancel` and `MaxExecutionSteps`.** Set both per lambda invocation. If `ctx.Done()` fires while a lambda is running, call `thread.Cancel("workflow cancelled")` from a watchdog goroutine spawned via `workflow.Go`. (The watchdog goroutine itself is fine to use here because it's coordinating cancellation, not driving workflow logic.)
- **`MaxExecutionSteps` defaults to a sane value** (e.g., 1M steps). Misbehaving lambdas terminate with an actionable error rather than running forever.
- **Use `workflow.Selector` correctly for parallel branches.** `for_each_parallel` should fan out via `workflow.Go` + `workflow.Channel`, and the interpreter should join on a `workflow.Selector` that handles cancellation as a first-class case.
- **Test cancellation explicitly.** A test that starts a flow with a long-running step, signals cancel, asserts the workflow completes within N seconds.
- **No native goroutines** in workflow code. `workflowcheck` will flag `go func()` calls — keep this enforced in CI.

**Warning signs:**
- Workflows that don't respond to cancellation in tests.
- `workflowcheck` complaining about `time.Sleep` or `go ` in interpreter code.
- A test timeout in `TestWorkflowEnvironment` after `CancelWorkflow`.
- Background goroutines visible in `pprof` after a workflow returns.

**Phase to address:**
**Phase 2 (Interpreter)** — cancellation propagation and `MaxExecutionSteps` per call. **Phase 3 (Generic activity)** — heartbeat on long batches respects cancellation. **Phase 6 (E2E testing)** — cancellation test cases.

---

### Pitfall 9: The Test Bridge (`temporal_test`) Allows Non-Determinism Through the Mock

**What goes wrong:**
The `temporal_test` builtin lets `.star` authors write E2E tests by mocking `ActionRef` results. The bridge connects Temporal's `testsuite` mocks back to Starlark lambdas with an `attempt` count.

If the mock lambdas themselves are non-deterministic — e.g., a mock that returns `random()`, or one that appends to a list captured from outer scope — the test produces different results every run. Worse: a mock that inadvertently causes the Starlark thread to retain state across activity calls means the *test harness* leaks state that the *production interpreter* would never have.

The trap is that test code feels like a place where "anything goes" — but Skytime's tests are testing a deterministic system, and the mocks must respect the same constraints as production lambdas.

**Why it happens:**
Test ergonomics push toward expressiveness. Production constraints push toward purity. The `temporal_test` builtin lives in a third world where it has to mirror production semantics or it lies.

**How to avoid:**
- **Mock lambdas execute in the same restricted environment as production lambdas** — same predeclared globals, same freeze rules, same `MaxExecutionSteps`. The "attempt" counter is passed in as a parameter, *not* implemented by closure-mutated state.
- **Mock state is explicit.** If a test needs cross-attempt state ("succeed on third try"), it's expressed via the `attempt` parameter and a `dict` keyed by attempt number, not by closure mutation.
- **Determinism replay test in the harness:** run every E2E test twice and assert identical event histories. Catches both production and mock non-determinism.
- **Document explicitly:** mocks are not Python `unittest.mock`. They are deterministic stubs.

**Warning signs:**
- Mock lambda using `random()` or `now()` (the lambda environment shouldn't expose these, but if a test-only global slips in, this catches it).
- Flaky tests in CI that pass locally.
- A mock with a closure variable that mutates between calls.
- Test passes once, fails on second run within the same `go test` invocation.

**Phase to address:**
**Phase 6 (E2E testing — Tier 3 in PROJECT.md)** — design `temporal_test` from day one with the determinism contract. The mock environment is built on the same restricted predeclared environment as the workflow lambda environment.

---

### Pitfall 10: Static Validation Diverges from Runtime Semantics

**What goes wrong:**
PROJECT.md mandates a static validation tier — parse `.star` files without executing, verify kwargs and input schemas. The trap: static validation is implemented by re-implementing parts of the interpreter (e.g., simulating the DAG, type-checking lambda signatures). Over time, the static validator and the runtime drift:

- Static says "OK" for code the runtime rejects (false negative — production breaks).
- Static says "fail" for code the runtime accepts (false positive — users blocked from valid configs).

Either is bad. Both happen.

**Why it happens:**
Two implementations of "what is a valid Skytime program" tend to drift. Static is added later, copies behavior, and gets out of sync as the runtime evolves.

**How to avoid:**
- **Single source of truth.** The runtime parser produces the DAG. Static validation reuses the same parser, then runs schema/kwarg checks on the DAG — *not* on a separate AST representation. Validation = parser + schema check, no second parser.
- **Schema definitions live with extensions.** Extensions declare their input schemas as Go structs / annotations. Both parse-time validation and runtime activity dispatch read from the same schema source.
- **Differential testing.** A test corpus of `.star` files runs through both static validation and a "dry run" of the full interpreter (with all actions mocked to return zero values). They must agree on accept/reject.
- **Static is a *subset* of runtime checks**, not a different check. Anything the runtime would reject at parse time, static rejects too. Things requiring evaluation (lambda body correctness for any input) static cannot catch — be honest about that boundary.

**Warning signs:**
- Two AST traversal functions doing similar things.
- A test that passes static validation but fails at workflow start.
- A schema definition file referenced only by the validator, not by the runtime.

**Phase to address:**
**Phase 5 (Static validation)** — share the parser with the runtime; validation is a post-parse pass over the DAG.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Reuse one `*starlark.Thread` for parse + execute | Less code, no thread bookkeeping | Pitfall 1 — inevitable replay/concurrency bugs | **Never** |
| Skip `Freeze()` audit on custom value types | Faster extension authoring | Pitfall 2 — non-deterministic captures | Only for value types provably immutable by construction |
| Pass `workflow.Context` into builtins "just for now" | Easier to plumb cancellation | Pitfall 4 — context bleed; invalidates the architecture | **Never** |
| Default to block-batching all actions | Smaller event histories | Pitfall 5 — partial-failure landmines | Only when extension declares idempotency |
| Use `panic` for user errors during dev | Quick failure feedback | Pitfall 7 — bad UX, leaks Go internals | Only behind a `--debug` flag |
| Skip the credential scrubber on errors | One less middleware | Pitfall 6 — credentials in event history | **Never** in production builds |
| Single predeclared environment for parse + lambdas | Simpler API | Pitfall 3 — non-deterministic globals leak in | Only if the env is provably side-effect-free |
| Mock lambdas with closure state in tests | Easier test authoring | Pitfall 9 — flaky tests, false confidence | **Never** — use `attempt` parameter |
| Skip replay tests in CI for E2E flows | Faster CI | Determinism bugs reach production | Only for flows explicitly tagged "non-deterministic-allowed" (none should exist) |
| Skip `MaxExecutionSteps` on lambda calls | Marginal perf | Workflows hang on bad lambdas | **Never** in production |

---

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| `go.starlark.net` | Reusing one Thread across calls | Fresh `*starlark.Thread` per lambda invocation; thread dies at end of call |
| `go.starlark.net` | Leaving `resolve.AllowGlobalReassign` defaults | Explicitly configure resolve options to match Skytime's purity goals |
| `go.starlark.net` | Custom value types with no-op `Freeze()` | Implement `Freeze()` recursively for every reachable value |
| `go.starlark.net` | Using `Thread.Print` for production logging | Route to `workflow.GetLogger(ctx)` inside workflow; replay-safe |
| `go.temporal.io/sdk` | Native `go` statement in interpreter | `workflow.Go` only; `workflowcheck` enforces |
| `go.temporal.io/sdk` | `time.Now`, `rand.Int`, `os.Getenv` in workflow code | `workflow.Now`, `workflow.SideEffect`, pass via inputs |
| `go.temporal.io/sdk` | `workflow.SideEffect` to mutate state | Side effects only return values; never mutate |
| `go.temporal.io/sdk` | Map iteration with `range` | Sort keys first, then iterate |
| `go.temporal.io/sdk` | Branching on `workflow.IsReplaying` for logic | Only for log/metric suppression, never for commands |
| `go.temporal.io/sdk` | Large workflow input passed by value | Use claim-check pattern; store in S3/blob, pass URL |
| `go.temporal.io/sdk` | One activity per single I/O call (history bloat) | Block-batch when idempotent; split otherwise |
| `go.temporal.io/sdk` | Local activity used for long batch | Regular activity with heartbeats |
| `go.temporal.io/sdk` | Single `StartToCloseTimeout` for batched activity | Compute as sum of per-action timeouts plus headroom |
| `go.temporal.io/sdk/testsuite` | Activity retry policy not respected in mock | Test against real Temporal in addition to testsuite for retry-sensitive flows |
| Temporal Cloud | Plaintext error messages in event history | Configure payload codec; scrub credentials before error returns |

---

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Single-action steps producing thousands of events | "history size warning" then workflow termination | Block-batch idempotent actions; use `continue-as-new` for long flows | At ~10K events (warn) / 51,200 events (terminate) |
| Large lambda closures retained across workflow lifetime | Worker memory growth proportional to flow count | Lambda free vars limited to scalars/frozen structs; lint mutable captures | At 1000s of concurrent workflows on one worker |
| Block batch too long, exceeds `StartToCloseTimeout` | Activity timeouts and retries; effective throughput collapses | Per-batch budget + heartbeats; split when budget exceeded | Batch crosses ~30s without heartbeat |
| `MaxExecutionSteps` not set, lambda CPU-loops | Workflow hung; worker thread stuck | Default `MaxExecutionSteps` per lambda call | Any user-supplied lambda with a bug |
| Workflow input >2MB (large state) | Workflow start fails with payload limit error | Claim check pattern; pass IDs not blobs | At 2MB input or 50MB total history |
| Parsing `.star` files on every workflow start | Latency on every run; CPU overhead | Cache parsed `*Flow` keyed by file content hash; freeze and reuse | At >10 workflows/sec startup rate |
| `for_each_parallel` with unbounded fanout | Thousands of concurrent activities; rate-limit issues | Bound parallelism (semaphore); document max parallel | At >100 concurrent branches per parent workflow |

---

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Resolved credentials present in workflow state | Plaintext storage in event history | Resolver runs *inside* generic activity; state holds only IDs (PROJECT.md invariant) |
| Credentials in error messages | Plaintext in event history; visible in Web UI | Scrubbing middleware on every error returned from generic activity |
| Credentials passed to lambdas | Lambdas can capture them; freezes locks them in DAG | Lambdas see `credential_id` (string), never the resolved value |
| Predeclared `http_get` builtin available at parse | Network call during static validation; SSRF risk | No I/O builtins in parse environment; intent goes via `ActionRef` only |
| Predeclared `os.Environ` or file-read builtin | Information disclosure from worker host | Don't expose; use Temporal-managed config |
| Custom `Print` hook leaks to stdout in prod | Credentials in worker logs | Route to `workflow.GetLogger`; verify scrubber runs before logger |
| `.star` file from untrusted source executed | Resource exhaustion (CPU, memory) | `MaxExecutionSteps`; memory caps; only trusted authors in v1 (consultants) |
| Extension panics with sensitive data in stack | Stack in event history with secrets | Recover panics in generic activity; rewrite as scrubbed error |
| Codec server misconfigured | Decoded payloads visible to wrong audience | Document codec server auth; example project uses an authenticated codec |
| Credential resolver caches across workflows | Stale credentials after rotation | Resolver invoked per activity attempt; no cross-attempt cache |

---

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Errors that show Go stack instead of `.star` location | Consultants forced into Go debugging | Pitfall 7 — Starlark-first error rendering |
| Static validator says "OK" but workflow fails at start | "I tested it, why is prod broken?" | Pitfall 10 — static = subset of runtime checks |
| Determinism error message says "command mismatch at index 47" | No actionable context | Wrap with Starlark step name + lambda location |
| Mock setup language differs from production | Mocks pass, prod fails | Pitfall 9 — mock environment = lambda environment |
| `.star` syntax error reported with column off by N | Hard to find in editor | Use `syntax.Error.Pos`; preserve through pipeline |
| Long activity batch with no progress indicator | "Did it hang?" | Heartbeat with progress; CLI surfaces heartbeat data |
| Cancellation that doesn't cancel mid-batch | Workflow keeps running after user clicks cancel | Pitfall 8 — cancellation propagates to Starlark thread |
| `for_each_parallel` failure surfaces as one of many errors | "Which iteration failed?" | Per-iteration error reporting with iteration index |
| Schema mismatch error doesn't say which kwarg | Trial and error to find the typo | Schema validator points at exact kwarg with expected/got |

---

## "Looks Done But Isn't" Checklist

- [ ] **Lambda execution:** Often missing thread-per-call lifecycle — verify no test reuses a `*starlark.Thread`.
- [ ] **Determinism:** Often missing replay testing for Starlark-driven flows — verify CI runs every E2E test twice and diffs event histories.
- [ ] **Cancellation:** Often missing watchdog that cancels Starlark thread on `ctx.Done()` — verify cancel test completes within N seconds.
- [ ] **Block batching:** Often missing per-action result list — verify activity returns structured per-action outcomes, not a single value.
- [ ] **Idempotency contract:** Often missing on extension API — verify every extension declares `Idempotent bool`.
- [ ] **Credential scrubbing:** Often missing on error path — verify a test injects a fake credential, fails the action, asserts secret not in event history.
- [ ] **Static validation:** Often missing schema-from-extension wiring — verify static validator and runtime read schemas from the same source.
- [ ] **Error reporting:** Often missing `syntax.Position` in DAG nodes — verify errors include `.star:line:col`.
- [ ] **`workflowcheck` integration:** Often missing in CI — verify it runs and fails on `go ` / `time.Sleep` / native maps.
- [ ] **Predeclared environment split:** Often single env — verify lambda environment is a strict subset of parse environment.
- [ ] **Freeze audit on custom values:** Often missing — verify every Go-bridged `starlark.Value` type has a recursive `Freeze()`.
- [ ] **Mock environment parity:** Often divergent — verify `temporal_test` uses the same predeclared env as workflow lambdas.
- [ ] **Heartbeat in batched activity:** Often missing between actions — verify heartbeat call between every action in a batch.
- [ ] **`MaxExecutionSteps`:** Often missing on lambda calls — verify default is set on every interpreter-side `starlark.Call`.
- [ ] **Codec recommendation:** Often skipped in example project — verify example ships with codec config and a "for production, configure this" note.

---

## Recovery Strategies

When pitfalls occur despite prevention, how to recover.

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Pitfall 1 (thread reuse) | MEDIUM | Refactor to per-call thread; add lint; replay tests catch regressions |
| Pitfall 2 (mutable capture) | MEDIUM | Audit `Freeze()` impls; add free-var lint; reject offending `.star` files; consultants update their code |
| Pitfall 3 (lambda non-determinism) | HIGH | Determinism error in prod requires Temporal patching/versioning; users can't replay; new flows use restricted env going forward |
| Pitfall 4 (context bleed) | MEDIUM | If discovered post-shipping, requires API break for affected extensions; avoid via lint from day one |
| Pitfall 5 (partial failure) | HIGH | If non-idempotent action ran twice, manual reconciliation per affected workflow; future flows use idempotency declaration |
| Pitfall 6 (credential leak) | HIGH | Rotate exposed credentials; audit event histories; configure codec; deploy scrubber retroactively |
| Pitfall 7 (error reporting) | LOW | Add error wrappers post-facto; existing errors still readable, just verbose |
| Pitfall 8 (context lifecycle) | MEDIUM | Add cancellation propagation; existing hung workflows manually terminated |
| Pitfall 9 (test non-determinism) | LOW | Re-write affected tests with `attempt` param; add determinism replay assert |
| Pitfall 10 (static/runtime drift) | MEDIUM | Refactor static to share parser; add differential corpus; users may see new validation errors on previously-accepted files |

---

## Pitfall-to-Phase Mapping

(Phases are illustrative — final phase numbers come from the roadmap. The mapping is by *concern*, not number.)

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| 1. Thread reuse across parse/execute | Phase 1 (DSL primitives) + Phase 2 (Interpreter) | Lint: no `*starlark.Thread` field in DAG types; tests run in fresh-thread mode |
| 2. Mutable capture in lambdas | Phase 1 (DSL primitives) + Phase 5 (Static validation) | Free-variable lint; corpus of bad `.star` files all rejected |
| 3. Lambda non-determinism | Phase 2 (Interpreter — restricted env) + Phase 6 (E2E testing — replay) | Replay test diffs event histories on every E2E run |
| 4. Context bleed | Phase 2 (Interpreter — bridge boundary) + Phase 4 (Extension API) | Lint: no `workflow.Context` in extension/builtin packages |
| 5. Block batch partial failure | Phase 3 (Generic activity) + Phase 4 (Extension API — idempotency contract) | E2E test with intentional mid-batch failure; assert no double-execution of non-idempotent actions |
| 6. Credential leakage | Phase 4 (Extension API — Credential type) + Phase 7 (Production hardening — scrubber) | Test with fake credential, assert scrubbed from event history |
| 7. Cross-language error reporting | Phase 1 (DSL primitives — Position capture) + Phase 2 (Interpreter — error wrap) | Every error in tests has `.star:line:col` |
| 8. `workflow.Context` lifecycle | Phase 2 (Interpreter — cancellation watchdog) + Phase 6 (E2E testing — cancel cases) | Cancel test completes within timeout |
| 9. Test bridge non-determinism | Phase 6 (E2E testing — `temporal_test` design) | Every E2E test runs twice, asserts identical history |
| 10. Static/runtime drift | Phase 5 (Static validation — share parser) | Differential corpus test in CI |

---

## Sources

- [Temporal Go SDK — Versioning](https://docs.temporal.io/develop/go/versioning) — patching primitives, replay testing, non-determinism root causes (HIGH).
- [Temporal — Workflow Definition](https://docs.temporal.io/workflow-definition) — determinism contract, replay semantics (HIGH).
- [Temporal Go SDK — Side Effects](https://docs.temporal.io/develop/go/side-effects) — `workflow.SideEffect` semantics, "do not mutate state" rule (HIGH).
- [Temporal Go SDK — workflowcheck](https://pkg.go.dev/go.temporal.io/sdk/contrib/tools/workflowcheck) — static analysis for invalid constructs in workflow code (HIGH).
- [Temporal — Multithreading](https://docs.temporal.io/develop/go/go-sdk-multithreading) — `workflow.Go` vs native `go`, deterministic runner (HIGH).
- [Temporal — Spooky Stories anti-patterns](https://temporal.io/blog/spooky-stories-chilling-temporal-anti-patterns-part-1) — over-wrapping, large history, non-idempotent local activities (HIGH).
- [Temporal — Activity timeouts](https://temporal.io/blog/activity-timeouts) — four timeout types, heartbeat semantics for long-running batches (HIGH).
- [Temporal — Idempotency](https://temporal.io/blog/idempotency-and-durable-execution) — idempotency contract for activities (HIGH).
- [Temporal — Blob size limit](https://docs.temporal.io/troubleshooting/blob-size-limit-error) — 2MB payload limit, 4MB transaction limit, 50MB history (HIGH).
- [Temporal — Continue-as-new](https://docs.temporal.io/workflow-execution/continue-as-new) — long-running workflow strategy (HIGH).
- [Temporal — Child workflow context bleed](https://community.temporal.io/t/child-workflow-fails-when-not-specifying-activityoptions/8977) — concrete context-bleed bug from community (MEDIUM).
- [Temporal — Data encryption / payload codec](https://docs.temporal.io/payload-codec) — at-rest encryption, codec server (HIGH).
- [Temporal — Handling Signals, Queries, Updates](https://docs.temporal.io/handling-messages) — handler determinism and replay safety (HIGH).
- [Temporal — Testing Suite](https://docs.temporal.io/develop/go/testing-suite) — `OnActivity`, retry semantics in tests (HIGH).
- [Temporal SDK Go — issue #406, #429 — retry policy in tests](https://github.com/temporalio/sdk-go/issues/429) — testsuite divergence from production retry behavior (MEDIUM).
- [Starlark in Go — package docs](https://pkg.go.dev/go.starlark.net/starlark) — Thread, SetLocal, MaxExecutionSteps, Cancel, freeze (HIGH).
- [Starlark in Go — Implementation](https://chromium.googlesource.com/external/github.com/google/starlark-go/+/HEAD/doc/impl.md) — freeze mechanism, thread safety guarantees (HIGH).
- [Starlark — Language definition](https://chromium.googlesource.com/external/github.com/google/starlark-go/+/HEAD/doc/spec.md) — closure capture, freeze of free variables in functions (HIGH).
- [Starlark in Go — issue #160 — preventing long-running scripts](https://github.com/google/starlark-go/issues/160) — execution step quotas (HIGH).
- [Starlark — Embedding tutorial (Vivien)](https://medium.com/@vladimirvivien/embedding-starlark-part-1-configure-go-programs-with-starlark-scripts-5abde31b8265) — embedding patterns, predeclared globals (MEDIUM).
- [Buck2 — Starlark environments](https://buck2.build/docs/developers/starlark/environment/) — environment scoping rules, mutability across contexts (MEDIUM).
- [Starlark — Specification](https://starlark-lang.org/spec.html) — formal language semantics for hermetic execution (HIGH).
- [Temporal — Best practices article (Beamonte)](https://raphaelbeamonte.com/posts/good-practices-for-writing-temporal-workflows-and-activities/) — community-collated practices and gotchas (MEDIUM).

---
*Pitfalls research for: Skytime — Starlark DSL compiled into Temporal workflows, Go host*
*Researched: 2026-04-26*
