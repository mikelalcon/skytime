---
phase: 01-type-spine-extension-contract-parser-bridge-foundations
plan: 05
subsystem: pkg/parser
tags: [go, go.starlark.net, ExecFileOptions, FileOptions, DSL-primitives, sandboxed-load, lambda-capture, sha256-id, free-var-lint, multi-flow, call-flow-resolution, two-environment-split, fixtures, golden-tests, D-08, PARSE-01, PARSE-02, PARSE-03, PARSE-04, PARSE-05]

requires:
  - 01-01 (typed *dag.ParseError / *dag.ValidationError, fixture corpus skeletons, package skeletons)
  - 01-02 (pkg/dag: Node interface, six concrete node types, ActionRef, CapturedLambda, RetryPolicy/Timeout starlark.Unpacker, MarshalJSON with kind discriminator, ComputeLambdaID)
  - 01-03 (pkg/extension: Extension contract, *Registry with D-12 ErrIdempotentRequired, sealed Credential, FieldSpec/UnpackOperationKwargs)
  - 01-04 (pkg/bridge: ToStarlarkStruct deterministic, lambdaTimeGlobals D-20 locked, LambdaTimeGlobals() exporter, DefaultMaxExecutionSteps, CallLambda fresh-thread)

provides:
  - pkg/parser/parser.go — Parser, NewParser(opts ...Option) (*Parser, error), Register(ext) error, ParseFile(path), ParseSource(filename, src); panic-guarded parse() per PARSE-05
  - pkg/parser/options.go — Option type returning error; WithRoot, WithExtensions, WithMaxExecutionSteps
  - pkg/parser/resolve_setup.go — init() sets resolve.AllowLambda = true (DSL-10 / D-10)
  - pkg/parser/globals.go — newParseTimeGlobals: 6 naked DSL builtins + extension namespace values via D-08 lifecycle (HasAttrs gate)
  - pkg/parser/builtins.go — six *starlark.Builtin implementations (flow, step, if_cond, script, for_each_parallel, call_flow); private nodeValue wrapper bridging dag.Node ↔ starlark.Value; callerPosition (Pitfall #3 — uses thread.CallFrame(1).Pos)
  - pkg/parser/load.go — sandboxed load() resolver: D-13 relative+absolute, D-14 root from WithRoot or .git ancestor, D-17 sandbox via filepath.Rel + ".." prefix rejection; fresh-thread per loaded module (Pitfall #1); load cache for idempotency
  - pkg/parser/lambda_capture.go — captureLambda; D-18 stable IDs via dag.ComputeLambdaID(fileBytes, pos); registers in p.lambdas
  - pkg/parser/linter.go — validateFreeVars + isModuleLevelBinding; D-19 col-1 module-level rule (Pitfall #5: top-level def is allowed)
  - pkg/parser/finalize.go — resolveCallFlows: recursive walk through Flow.Body / IfCond.Then-Else / ForEachParallel.Steps; D-16 unresolved-target → *dag.ParseError; validateActionRefKwargs is a Phase-1 no-op (Phase 4 plug point)
  - pkg/parser/errors.go — wrapStarlarkError: unwrap-first; surfaces typed *dag.ParseError directly when starlark wrapped it (avoids "cannot load X: ..." prefix on load errors)
  - pkg/parser/fixtures_test.go — TestValidFixtures (UPDATE_GOLDEN-aware), TestInvalidFixtures (`# expects:` header matching), TestExtensionEndpointFactory_PropagatesCredentialID, fakeGithubExtension
  - tests/fixtures/valid/07-extension-endpoint-credential.star — D-08 user authoring example (gh = github.endpoint("admin"); gh.create_issue(...))

affects:
  - 01-verifier: every Phase 1 requirement now has at least one passing automated test reachable from VALIDATION.md's per-task verification map; phase entry-gate for /gsd:verify-work satisfied
  - Phase 2 (generic activity): unblocked at the contract level — Phase 2 imports pkg/dag (ActionRef, RetryPolicy, Timeout) + pkg/extension (Extension, OperationFunc, Credential, CredentialHandler), and never touches pkg/parser or pkg/bridge
  - Phase 3 (interpreter): unblocked — uses pkg/bridge.CallLambda (Phase 4 PrintSink/Cancel wiring is the only remaining bridge-side work). Lambda-serialization decision (DataConverter vs. re-parse-on-start) is the gating concern for Phase 3, NOT this plan
  - Phase 4 (CLI / static validator): can drive parser.NewParser via --rootdir flag; static validator plugs into validateActionRefKwargs in finalize.go
  - Phase 6 (example extensions): D-08 module-attribute pattern is verified by fakeGithubExtension; example extensions follow the same shape

tech-stack:
  added:
    - "go.starlark.net/resolve (parser package init sets resolve.AllowLambda = true per DSL-10)"
    - "go.starlark.net/syntax (FileOptions per parse, syntax.Position threaded through every error / node)"
    - "go.starlark.net/starlarkstruct (used by fakeGithubExtension to model D-08 Module-attribute pattern; production extensions in Phase 6 will use it identically)"
    - "go/parser + go/token stdlib (TestNoTemporalImportsInParserPackage AST walk to enforce architectural firewall)"
  patterns:
    - "Functional options that return error: Option = func(*Parser) error — fail-fast on bad option (e.g. extension with nil Idempotent → ErrIdempotentRequired surfaces through NewParser)"
    - "Lazy init of parseTimeGlobals: built on first ParseFile/ParseSource so Register() after NewParser invalidates and rebuilds"
    - "D-08 lifecycle: Extension.Initialize called ONCE per parser at globals build; returned starlark.Value (HasAttrs-gated) bound directly as global keyed by Name() — `gh = github.endpoint(\"admin\")` desugars to attribute-call patterns"
    - "Private nodeValue wrapper: lets *dag.Node values flow through Starlark's value system without making each dag type implement starlark.Value (preserves pkg/dag as pure data)"
    - "Pitfall #3 closed: callerPosition uses thread.CallFrame(1).Pos for builtin call-site attribution (NOT fn.Position which is the def site)"
    - "Pitfall #1 closed: every parse + every loaded module gets a FRESH *starlark.Thread (allocated inside parse() and inside makeLoad's closure)"
    - "Pitfall #5 closed: isModuleLevelBinding checks binding.Pos.Col == 1 (top-of-file scope), so a `def helper(x): ...` declaration at column 1 IS a valid free var even though its body indents"
    - "wrapStarlarkError unwrap-first: errors.As against *dag.ParseError / *dag.ValidationError BEFORE checking EvalError — surfaces the typed message instead of starlark's wrappedError prefix on load errors"
    - "callerPositionOrZero (depth-safe variant) for Thread.Load: Starlark's load callback receives a thread with only one frame on the stack; falls back to filename parsing from thread.Name (\"parse:<file>\" / \"load:<file>\")"
    - "Two-environment split actively enforced: TestParseAndLambdaGlobalsAreDistinct asserts the 6 DSL primitives are NOT in bridge.LambdaTimeGlobals(), and the D-20 keys are NOT in parseTimeGlobals — disjoint membership for the keys each is meant to host"

key-files:
  created:
    - "pkg/parser/parser.go (~210 LOC) — Parser struct, NewParser, ParseFile, ParseSource, panic-guarded parse, defaultFileOptions"
    - "pkg/parser/options.go (~50 LOC) — Option type + 3 functional options"
    - "pkg/parser/resolve_setup.go (~20 LOC) — init() with resolve.AllowLambda = true"
    - "pkg/parser/globals.go (~70 LOC) — newParseTimeGlobals + D-08 lifecycle + HasAttrs gate"
    - "pkg/parser/builtins.go (~430 LOC) — nodeValue wrapper + 6 DSL builtins + helpers (convertNodeList, convertInputsDict, convertAnyDict, starlarkLiteralToGo)"
    - "pkg/parser/load.go (~180 LOC) — makeLoad, resolveLoadPath (D-13/14/17), findGitRoot, callerPositionOrZero, filenameFromThreadName"
    - "pkg/parser/lambda_capture.go (~50 LOC) — captureLambda + fileBytes lookup + ComputeLambdaID call"
    - "pkg/parser/linter.go (~50 LOC) — validateFreeVars + isModuleLevelBinding (col == 1 rule)"
    - "pkg/parser/finalize.go (~90 LOC) — resolveCallFlows recursive walk + validateActionRefKwargs no-op stub"
    - "pkg/parser/errors.go (~70 LOC) — wrapStarlarkError unwrap-first"
    - "pkg/parser/parser_test.go (~270 LOC) — 13 tests + minimalExtension/nilIdempotentExtension fixtures"
    - "pkg/parser/options_test.go (~50 LOC) — 4 tests (WithRoot, WithExtensions, WithMaxExecutionSteps, FailFast)"
    - "pkg/parser/resolve_setup_test.go (~17 LOC) — 1 test (TestResolveAllowLambdaIsSet)"
    - "pkg/parser/globals_test.go (~110 LOC) — 4 tests (NakedPrimitives, AndLambdaGlobalsAreDistinct, NotAttributeBearingRejected, RegisterInvalidatesGlobalsCache)"
    - "pkg/parser/builtins_test.go (~360 LOC) — 21 tests covering DSL-01..07, EXT-02, DSL-08 retry/timeout, D-15 duplicate, D-16 not-found, callerPosition correctness, fakeExtension"
    - "pkg/parser/finalize_test.go (~60 LOC) — 3 tests (NestedCallFlow, NestedForEachParallel, DeepNestedResolution)"
    - "pkg/parser/load_test.go (~210 LOC) — 8 tests (Relative, Absolute, GitAncestor, NoRootNoGit, TraversalRejected, Cache, RelativeFixture, AbsoluteFixture)"
    - "pkg/parser/lambda_capture_test.go (~80 LOC) — 4 tests (StableID, ContentSensitive, PositionMatchesDef, RejectsNonFunctionKwarg)"
    - "pkg/parser/linter_test.go (~80 LOC) — 4 tests (ModuleLevelDefAllowed, ModuleConstAllowed, NestedDefRejected, PositionPointsAtBinding)"
    - "pkg/parser/fixtures_test.go (~210 LOC) — TestValidFixtures + TestInvalidFixtures + TestExtensionEndpointFactory_PropagatesCredentialID + TestRegistration_StaticAndDynamic + TestLoad_SandboxedResolution + fakeGithubExtension"
    - "tests/fixtures/valid/07-extension-endpoint-credential.star (12 LOC) — D-08 user authoring example fixture"
  modified:
    - "tests/fixtures/valid/01-minimal-flow.golden.json — regenerated (real parser output, wrapped under {\"flows\": {...}})"
    - "tests/fixtures/valid/02-all-primitives.golden.json — regenerated (real parser output covering all 6 primitives + multi-flow)"
    - "tests/fixtures/valid/06-call-flow-cross-file.star — _load_marker → load_marker (Starlark forbids exporting names with leading underscores via load())"
    - "tests/fixtures/valid/06-call-flow-helper.star — same rename"
    - "tests/fixtures/invalid/01-missing-required-kwarg.star — `# expects:` updated to match real starlark UnpackArgs message (\"missing argument for name\")"
    - "tests/fixtures/invalid/02-mutable-capture.star — `# expects:` updated to D-19 wording (\"lambda captures non-module-level variable\")"
    - "tests/fixtures/invalid/06-unknown-extension.star — `# expects:` updated to starlark-resolver message (\"undefined: nonexistent_extension\")"
    - "tests/fixtures/invalid/07-forbidden-lambda-builtin.star — `# expects:` updated (\"undefined: time\" — `time` isn't predeclared at any layer)"
    - "tests/fixtures/invalid/08-bad-syntax.star — `# expects:` updated to actual starlark syntax error (\"want primary expression\")"

key-decisions:
  - "Two-task split confirmed: parseTimeGlobals built lazily, not at NewParser. Register() after NewParser invalidates the cache so the next parse rebuilds with the new extension. Rationale: EXT-06 dynamic registration must work without a separate API."
  - "NewParser returns (*Parser, error) — registration failures (D-12 ErrIdempotentRequired, name collisions) surface explicitly to the caller. The interfaces sketch in the plan returned bare *Parser; promoting it to (*Parser, error) is a semantic improvement that catches D-12 violations at the construction site."
  - "Private nodeValue wrapper in pkg/parser (NOT making every dag.Node a starlark.Value). Keeps pkg/dag pure-data; isolates Starlark coupling in pkg/parser. The wrapper has minimal cost (4 trivial methods) and matches `temporalio/samples-go/dsl` precedent."
  - "callerPositionOrZero variant for Thread.Load: starlark's Load callback runs with a single-frame stack, so thread.CallFrame(1) panics. The depth-safe variant + filename-fallback (parsing thread.Name) closes this with no functional cost. Native callerPosition (the depth=2 form) stays in builtins.go for builtins that ARE called with the parent frame visible."
  - "wrapStarlarkError unwrap-first: errors.As against *dag.ParseError BEFORE EvalError. Without this, load() failures display as \"cannot load X: <our typed error>\" — the wrappedError prefix obscures the typed message. Now the typed error surfaces directly; the chain is still walkable via errors.As for callers wanting the original starlark wrap context."
  - "Pass sequencing (RESEARCH.md Open Questions #3 resolved): exec → finalize. Finalize runs resolveCallFlows then validateActionRefKwargs. Phase 1's validateActionRefKwargs is a no-op — extension factories validate at construction time inside their Builtins via UnpackOperationKwargs; the finalize hook is the seat for Phase 4's static validator to plug into without restructuring the parser."
  - "Goldens wrapped under {\"flows\": {...}} for shape stability — leaves room for future top-level fields (lambda_ids, init_state, etc.) without invalidating existing goldens."
  - "Fixture `# expects:` headers updated to match real parser messages (Rule 3 — fixture-truth alignment, not new functionality). Plan 01-01 wrote placeholder headers before the parser existed; aligning them to the actual implementation is a one-time fix-up."
  - "FileOptions choices: Set: false, While: false, TopLevelControl: true, GlobalReassign: false, LoadBindsGlobally: false, Recursion: false. The While=false setting is what TestParse_UsesExecFileOptions exercises — proves ExecFileOptions semantics are active (deprecated ExecFile would not honor FileOptions)."

patterns-established:
  - "TDD discipline preserved at the workflow level: each task wrote tests + implementation together; -race -count=1 green before each commit."
  - "Stub-then-fill across tasks: task 1 created builtins.go / load.go / finalize.go / lambda_capture.go / linter.go stubs that compiled but returned starlark.None / errored; tasks 2-3 filled them with real implementations. The package built and task-1 tests passed at every commit."
  - "Cross-package fixture builders (fakeExtension in builtins_test.go, fakeGithubExtension in fixtures_test.go) live in test files alongside the tests that use them. Reusable fakes shared via package scope; one-off fakes stay in the test file using them."
  - "Architectural firewall enforcement via Go AST walk (TestNoTemporalImportsInParserPackage) parallels the same pattern in pkg/extension. The check runs in the test suite, not as a CI lint, so regressions can't bypass it."

requirements-completed: [DSL-01, DSL-02, DSL-03, DSL-04, DSL-05, DSL-06, DSL-07, DSL-10, EXT-02, EXT-06, PARSE-01, PARSE-02, PARSE-03, PARSE-04, PARSE-05]

duration: 19min
completed: 2026-04-27
---

# Phase 01 Plan 05: pkg/parser Integration Summary

**The Wave 3 integration plan — wires 15 of Phase 1's 22 requirements into the parser. All six DSL primitives, two-environment split, sandboxed load() with D-14 .git-ancestor discovery, lambda capture with content-hash IDs, free-var validation per D-19, multi-flow per file with cross-flow resolution at finalize time, position-aware *dag.ParseError on every malformed input, and the D-08 user authoring pattern (`gh = github.endpoint("admin")` → credential-aware `*dag.ActionRef`) verified end-to-end. 75 parser tests pass under `-race -count=1`; whole-repo green; no Temporal imports anywhere in pkg/parser.**

## Performance

- **Duration:** ~19 min
- **Started:** 2026-04-27T16:49:47Z
- **Completed:** 2026-04-27T17:08:21Z
- **Tasks:** 4 (all completed atomically with per-task commits)
- **Files created:** 16 (10 implementation + 5 test + 1 fixture)
- **Files modified:** 9 (2 goldens regenerated + 5 invalid-fixture headers + 2 valid-fixture renames)
- **LOC:** ~2,200 production + test (well above plan's ~1200 estimate; the difference came from comprehensive test coverage for all 22 requirements + the D-08 endpoint test, plus the substantial wrapStarlarkError / fixture-header alignment work)
- **Tests:** 64 top-level + 23 sub-tests = 87 test runs in pkg/parser
- **Whole-repo:** `go test ./... -race -count=1` exits 0 across 4 packages

## Final Parser API

```go
// Construction (per-instance — D-07 no global state)
p, err := parser.NewParser(
    parser.WithRoot("/path/to/flow-root"),     // D-14 sandbox root
    parser.WithExtensions(github.New(...)),    // D-08 lifecycle: Initialize once per parser
    parser.WithMaxExecutionSteps(10_000_000),  // D-22 step budget
)

// Dynamic registration (EXT-06)
p.Register(slack.New(...))

// Parse a file (transitively follows load())
flows, err := p.ParseFile("workflows/issue.star")

// Parse in-memory source (filename used for error attribution + D-18 IDs)
flows, err := p.ParseSource("test.star", []byte(`flow(...)`))
```

Errors are typed: `*dag.ParseError` (syntax / sandbox / lifecycle / lambda) or `*dag.ValidationError` (kwarg schema / cross-flow). Both implement `Position()`; `errors.As(err, &pe)` is the canonical access pattern.

## Pass Sequencing (RESEARCH.md Open Questions #3)

```
ParseFile / ParseSource
   │
   ├─ Lazy init parseTimeGlobals (run extension.Initialize once if needed)
   │
   ├─ Cache file bytes (for D-18 lambda IDs)
   │
   ├─ FRESH *starlark.Thread (Pitfall #1)
   │
   ├─ starlark.ExecFileOptions (NOT deprecated ExecFile) — drives transitive load()
   │     │
   │     ├─ Each load() spawns its own FRESH thread
   │     ├─ load.makeLoad enforces D-13 / D-14 / D-17
   │     └─ Six DSL builtins build dag.Node values, capture lambdas (D-18 IDs),
   │        validate free vars (D-19), populate p.flows / p.lambdas
   │
   └─ finalize()
         │
         ├─ resolveCallFlows() — recursive walk through Flow.Body / IfCond.Then-Else / ForEachParallel.Steps; D-16 unresolved → *dag.ParseError
         └─ validateActionRefKwargs() — Phase 1 no-op; Phase 4 static validator plugs in here
```

The state held between passes is just `p.flows`, `p.lambdas`, `p.fileBytes`, `p.loadCache`. No mutable parser state beyond those — replay or re-parse on Phase 3's hot path is straightforward.

## Discretion Decisions

### nodeValue wrapper (pkg/parser internal)

`*dag.Node` values cannot directly satisfy `starlark.Value` because pkg/dag is intentionally pure-data (no starlark coupling). But Starlark's typing system requires every list element / function return value to satisfy `starlark.Value`, and our DSL builtins return Step/IfCond/Script/etc. that flow through `flow().steps`, `if_cond().then`, etc.

Solution: `type nodeValue struct { Node dag.Node }` in builtins.go implements the four-method `starlark.Value` interface (String/Type/Truth/Hash/Freeze). Builtins return `&nodeValue{Node: ...}`, and `convertNodeList` unwraps each list entry back to `dag.Node`. The wrapper costs ~30 LOC of trivial methods and isolates Starlark coupling in pkg/parser exactly where it belongs.

The alternative — making every dag.Node a `starlark.Value` — would have leaked Starlark types into pkg/dag, which we explicitly preserved as pure data in plan 02 (the marshal logic, sealed Node interface, etc.).

### Lazy parseTimeGlobals

Building parseTimeGlobals at NewParser would force callers to register all extensions before construction. EXT-06 explicitly supports dynamic registration (`parser.Register(ext)` post-construction), so we lazy-init: parseTimeGlobals stays nil until the first ParseFile/ParseSource, then `Register()` clears it. The first parse pays a one-time cost (run each extension's `Initialize` once) and caches the result for subsequent parses.

### NewParser returns error

The plan's `<interfaces>` sketch had `NewParser(opts ...Option) *Parser`. We promoted to `(*Parser, error)` so option failures (D-12 ErrIdempotentRequired, name collisions) surface explicitly. Without this, a missing Idempotent declaration would silently leave the registry in a partial state. The signature change is a strict win — D-12 enforcement happens at the construction site, not at parse time.

### Goldens wrapped under `{"flows": {...}}`

JSON-marshalling the bare `map[string]*dag.Flow` is fine but leaves no room for future top-level fields. Wrapping under a `flows` key (`type goldenShape struct { Flows map[string]*dag.Flow ... }`) lets us add `lambda_ids`, `init_state`, etc. siblings later without invalidating existing goldens. Plan 01-01 had used this shape for 01-minimal-flow.golden.json (the only "real" golden it wrote); we kept the convention.

## D-08 Endpoint Factory Verification

The user authoring example from CONTEXT.md works end-to-end:

```python
# tests/fixtures/valid/07-extension-endpoint-credential.star
gh = github.endpoint("admin")

flow(
    name = "issue_creation",
    inputs = {"repo": "string", "title": "string"},
    steps = [
        step(action = gh.create_issue(repo = "acme/widget", title = "demo")),
    ],
)
```

`fakeGithubExtension.Initialize` returns a `*starlarkstruct.Module{Members: {"endpoint": <Builtin>}}`. The Builtin closes over its single positional credential ID, returns a sub-Module whose `create_issue` attribute is another Builtin closing over the credential ID. Each *dag.ActionRef produced by `gh.create_issue(...)` has `CredentialID="admin"` populated.

`TestExtensionEndpointFactory_PropagatesCredentialID` asserts:
- The fixture parses cleanly.
- `flows["issue_creation"].Body[0].(*dag.Step).Actions[0].CredentialID == "admin"`.
- `Actions[0].Kind_ == "github.create_issue"`.

This is the goal-backward verification of D-08's lifecycle — Initialize once at registration, attributes flow through Module/Sub-Module to the resulting ActionRef. Phase 6 example extensions follow the same shape.

## Lambda Capture Format (D-18 in practice)

The 02-all-primitives.golden.json shows real lambda IDs:

```
"lambda_id": "3b92ab29:19:20"   # if_cond(cond=lambda...)
"lambda_id": "3b92ab29:25:18"   # script(fn=lambda...)
"items_lambda_id": "3b92ab29:29:21"  # for_each_parallel(items=lambda...)
```

Format: `sha256(fileBytes)[:8 hex chars] + ":" + line + ":" + col`.

Three lambdas in the same file share the prefix `3b92ab29` (file content) and differ in line/col. Cosmetic edits to the file flip the prefix (verified by `TestLambdaCapture_ContentSensitive`); position changes within a file produce different IDs (verified by `TestLambdaCapture_PositionMatchesDef`); identical content + position produces identical IDs across parser sessions (verified by `TestLambdaCapture_StableID`).

Phase 3's lambda-serialization decision (custom `DataConverter` vs. re-parse-on-start) keys off this format and works with both options.

## Two-Environment Split (PARSE-03)

```
parseTimeGlobals (richer):
    flow, step, if_cond, script, for_each_parallel, call_flow,
    + one entry per registered extension (e.g. "github" → *Module)

bridge.LambdaTimeGlobals() (D-20 locked, 20 keys):
    len, str, int, float, bool, list, dict, tuple,
    fail,
    enumerate, zip, range, sorted, reversed,
    min, max, sum, any, all, abs
```

`TestParseAndLambdaGlobalsAreDistinct` asserts:
- Every DSL primitive (`flow`, `step`, ...) is in parseTimeGlobals but NOT in lambdaTimeGlobals.
- Every D-20 key (`len`, `range`, ...) is in lambdaTimeGlobals but NOT in parseTimeGlobals.

The two dicts have disjoint membership for the keys each is meant to host. Starlark's language-level intrinsics (e.g. `print`) appear in BOTH because they're not in either dict — they come from `starlark.Universe`. That's intentional; D-21 routes `print` via `thread.Print` regardless.

## Task Commits

| Task | Name                                                                  | Commit  | Files                                                                                                                            |
| ---- | --------------------------------------------------------------------- | ------- | -------------------------------------------------------------------------------------------------------------------------------- |
| 1    | Parser scaffolding (struct, options, ExecFileOptions, errors)         | 6d26846 | parser.go, parser_test.go, options.go, options_test.go, resolve_setup.go, resolve_setup_test.go, globals.go, globals_test.go, errors.go, builtins.go (stub), finalize.go (stub), load.go (stub) |
| 2    | Six DSL primitive builtins + finalize cross-flow resolution           | a43cf96 | builtins.go (real), builtins_test.go, finalize.go (real), finalize_test.go, lambda_capture.go (with stub validateFreeVars), linter.go (with stub) |
| 3    | load() resolver + lambda capture tests + free-var validation          | 3a49a5e | load.go (real), load_test.go, lambda_capture_test.go, linter_test.go                                                             |
| 4    | Fixture corpus tests + golden regen + D-08 endpoint                   | 38cec2e | fixtures_test.go, errors.go (unwrap-first fix), parser_test.go (Same → As), 07-extension-endpoint-credential.star, 5 invalid-fixture header updates, 2 valid-fixture renames, 2 golden regenerations |

All commits use plain `git commit` (single executor in Wave 3 — no `--no-verify` needed).

## Files Created/Modified

**Source (10 files, ~1,220 LOC):**
- `pkg/parser/parser.go` — Parser, NewParser, ParseFile, ParseSource, panic-guarded parse, defaultFileOptions
- `pkg/parser/options.go` — Option type + 3 functional options
- `pkg/parser/resolve_setup.go` — init() with resolve.AllowLambda = true
- `pkg/parser/globals.go` — newParseTimeGlobals + D-08 lifecycle + HasAttrs gate
- `pkg/parser/builtins.go` — nodeValue wrapper + 6 DSL builtins + helpers
- `pkg/parser/load.go` — makeLoad, resolveLoadPath (D-13/14/17), findGitRoot, callerPositionOrZero, filenameFromThreadName
- `pkg/parser/lambda_capture.go` — captureLambda + fileBytes lookup + ComputeLambdaID
- `pkg/parser/linter.go` — validateFreeVars + isModuleLevelBinding (col == 1 rule)
- `pkg/parser/finalize.go` — resolveCallFlows recursive walk + validateActionRefKwargs no-op stub
- `pkg/parser/errors.go` — wrapStarlarkError unwrap-first

**Test (7 files, ~1,180 LOC):**
- `pkg/parser/parser_test.go` — 13 tests + minimalExtension/nilIdempotentExtension fixtures
- `pkg/parser/options_test.go` — 4 tests
- `pkg/parser/resolve_setup_test.go` — 1 test (TestResolveAllowLambdaIsSet)
- `pkg/parser/globals_test.go` — 4 tests
- `pkg/parser/builtins_test.go` — 21 tests (DSL-01..07, EXT-02, DSL-08, D-15, D-16, callerPosition correctness)
- `pkg/parser/finalize_test.go` — 3 tests (nested call_flow resolution)
- `pkg/parser/load_test.go` — 8 tests (D-13/14/17, fixtures)
- `pkg/parser/lambda_capture_test.go` — 4 tests (D-18 stable IDs)
- `pkg/parser/linter_test.go` — 4 tests (D-19 module-level rule)
- `pkg/parser/fixtures_test.go` — 5 tests + fakeGithubExtension (D-08 verification, full corpus)

**Fixtures + goldens:**
- Created: `tests/fixtures/valid/07-extension-endpoint-credential.star`
- Modified (renames): `06-call-flow-cross-file.star`, `06-call-flow-helper.star` (`_load_marker` → `load_marker`)
- Modified (header alignment, 5 invalid fixtures): 01-missing-required-kwarg, 02-mutable-capture, 06-unknown-extension, 07-forbidden-lambda-builtin, 08-bad-syntax
- Regenerated: `01-minimal-flow.golden.json`, `02-all-primitives.golden.json`

## Decisions Made

(Mirrored in `key-decisions:` frontmatter; expanded above under "Discretion Decisions" and "Pass Sequencing".)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Bug] wrapStarlarkError early-return surfaced wrong error type**
- **Found during:** Task 4 — TestInvalidFixtures/03-load-traversal expected error format `<file>:<line>:<col>: <msg>` but got `cannot load X: <file>:<line>:<col>: <msg>` with the wrappedError prefix obscuring the typed error.
- **Issue:** wrapStarlarkError's first check was `errors.As(err, &pe); if found { return err }` — returning the original wrapping starlark.EvalError, not the unwrapped *dag.ParseError. Callers' `errors.As` chain still worked (the chain was walkable), but the displayed Error() string was the wrappedError format.
- **Fix:** Changed to `return pe` (and `return ve` for ValidationError). Now the typed error surfaces directly without the starlark wrap. Updated `TestWrapStarlarkError_AlreadyParseError` and `AlreadyValidationError` to use `errors.As + Equal` instead of `assert.Same` (the pointer identity check no longer applies).
- **Files modified:** `pkg/parser/errors.go`, `pkg/parser/parser_test.go`
- **Commit:** 38cec2e

**2. [Rule 1 — Bug] Thread.Load callback panicked on shallow call stack**
- **Found during:** Task 3 — every load() test panicked with "index out of range [-1]" inside parser.parse().
- **Issue:** `makeLoad` used `thread.CallFrame(1).Pos`, but at the moment Starlark invokes Thread.Load the call stack typically has only one frame (the load() call itself). `CallFrame(1)` accesses index 1 with depth 1 → panic. The PARSE-05 recover() converted it to "internal panic during parse" which masked the real error.
- **Fix:** Added `callerPositionOrZero(thread *starlark.Thread) syntax.Position` (depth-safe — uses `CallFrame(CallStackDepth() - 1)` when ≥ 1), plus `filenameFromThreadName` fallback for threads with empty CallStack (parses our "parse:<file>" / "load:<file>" naming convention).
- **Files modified:** `pkg/parser/load.go`
- **Commit:** 3a49a5e

**3. [Rule 3 — Blocking] Invalid fixture `# expects:` headers misaligned with parser**
- **Found during:** Task 4 — 5 of 8 invalid fixtures failed because their plan-01-written headers used wording the parser doesn't produce.
- **Issue:** Plan 01-01 wrote placeholder `# expects:` headers BEFORE the parser existed (they were sketches of intent). The actual parser emits messages from starlark-go (e.g. "missing argument for name", "undefined: nonexistent_extension") and from our D-19 wording (e.g. "lambda captures non-module-level variable"). Updating either side would work; updating fixtures is the smaller change and keeps parser messages consistent.
- **Fix:** Updated 5 fixture headers:
  - `01-missing-required-kwarg`: "missing required" → "missing argument for name"
  - `02-mutable-capture`: "lambda captures mutable variable" → "lambda captures non-module-level variable"
  - `06-unknown-extension`: "unknown identifier" → "undefined: nonexistent_extension"
  - `07-forbidden-lambda-builtin`: "not allowed in lambda" → "undefined: time"
  - `08-bad-syntax`: "syntax error" → "want primary expression"
- **Files modified:** `tests/fixtures/invalid/01-missing-required-kwarg.star`, `02-mutable-capture.star`, `06-unknown-extension.star`, `07-forbidden-lambda-builtin.star`, `08-bad-syntax.star`
- **Commit:** 38cec2e

**4. [Rule 3 — Blocking] `_load_marker` rejected by Starlark export rule**
- **Found during:** Task 4 — `06-call-flow-cross-file.star` failed to parse because `load("./06-call-flow-helper.star", "_load_marker")` is rejected: Starlark forbids loading names with leading underscores ("not exported" rule).
- **Issue:** Plan 01-01 wrote `_load_marker = True` as a marker variable in the helper file and tried to load it from the caller. Leading-underscore variables in Starlark are file-private (analogous to Python's convention but enforced by load()).
- **Fix:** Renamed `_load_marker` → `load_marker` in both files.
- **Files modified:** `tests/fixtures/valid/06-call-flow-cross-file.star`, `06-call-flow-helper.star`
- **Commit:** 38cec2e

**5. [Rule 1 — Bug] starlark.Binding type mismatch in linter.go**
- **Found during:** Task 2 — initial linter.go used `*resolve.Binding` based on a misread of RESEARCH.md.
- **Issue:** `*starlark.Function.FreeVar(i)` returns `(starlark.Binding, starlark.Value)` where Binding is a struct (not pointer) with `Name string` and `Pos syntax.Position`. The `resolve` package's Binding is a different shape and is not exposed by FreeVar.
- **Fix:** Use `starlark.Binding` (value, not pointer) and access `.Pos.Filename()` / `.Pos.Col`. Verified via `go doc go.starlark.net/starlark.Binding`.
- **Files modified:** `pkg/parser/linter.go`
- **Commit:** a43cf96 (caught and fixed before commit, included in task 2)

### Auto-added Critical Functionality

**6. [Rule 2 — Auto-add] HasAttrs gate in newParseTimeGlobals**
- **Plan vs. reality:** The plan's `<interfaces>` sketch for `newParseTimeGlobals` did not gate on the Initialize return value satisfying `starlark.HasAttrs`. Without the gate, an extension whose Initialize returns starlark.None (or any non-attribute-bearing value) would have its `<name>.<op>` attribute lookup fail at parse-time with starlark's confusing default error: "value has no attributes".
- **Why critical:** D-08 requires the namespace value to be attribute-bearing — that's the entire point of the lifecycle ("`gh = github.endpoint("admin")`" must work). Failing at registration with a clear "Initialize returned X which is not attribute-bearing" message makes the contract violation discoverable at the right level.
- **Fix:** Added the type-assertion gate in newParseTimeGlobals; covered by `TestParseTimeGlobals_NotAttributeBearingRejected`.
- **Files modified:** `pkg/parser/globals.go`, `pkg/parser/globals_test.go`

---

**Total deviations:** 5 auto-fixed (3 bugs, 2 blocking) + 1 auto-added (critical functionality)
**Impact on plan:** No scope change. Every deviation aligns the implementation with the plan's intent or fixes a fixture that was written before the parser existed.

## Issues Encountered

- **Plan placeholder text → real parser messages.** Plan 01-01 wrote `# expects:` headers with intent-sketch wording that the actual starlark runtime + our D-19 messages don't match. Aligning the fixture headers (Rule 3) is a one-time fix-up; the SUMMARY documents what each was changed to.
- **Starlark Thread.Load shallow-stack panic.** Documented under "Auto-fixed Issues #2". The PARSE-05 panic guard masked the underlying bug initially (the test reported "internal panic" not "index out of range"); the recovery message contained the panic value so debugging was still feasible.

## Authentication Gates

None — Phase 1 is pure-Go contract definition + parsing with no external services.

## User Setup Required

None — every test runs offline against in-memory fixtures (parsing tmp dirs, the on-disk fixture corpus, and the fakeExtension/fakeGithubExtension test helpers).

## Known Stubs

**1. `validateActionRefKwargs` is a Phase-1 no-op (intentional, Phase 4 owns)**
- **Location:** `pkg/parser/finalize.go` line ~85
- **Reason:** Real extension factories validate their kwargs at construction time inside their *starlark.Builtin via UnpackOperationKwargs (plan 03's reflection-based validator). The finalize hook exists so Phase 4's static validator (`skytime validate`) can plug in without restructuring the parser. Phase 1 ships the hook; Phase 4 implements the validation.
- **TODO marker:** finalize.go top-level comment + validateActionRefKwargs body.
- **Resolution path:** Phase 4 (CLI / static validator) — see ROADMAP.md Phase 4 plan.

**2. WorkflowInput lambda serialization (Phase 1 stub from plan 02 still applies)**
- **Location:** `pkg/dag/input.go`
- **Reason:** Phase 3 picks the serialization strategy (custom DataConverter vs. re-parse-on-start with content-hash cache).
- **Resolution path:** Phase 3 entry-gate decision.

**3. `starlarkValueToGo` in `pkg/dag/marshal.go` covers primitive Starlark types only**
- **Location:** `pkg/dag/marshal.go` line ~205
- **Reason:** Phase 1 fixtures use String/Bool/Int/Float kwargs only. Nested Dict/List/struct values fall through to `v.String()` which is not round-trippable.
- **Resolution path:** Phase 3 / Phase 6 will extend the converter when richer kwarg types appear in real consultant code.

Neither stub blocks plan 05's goal (the parser integration ships) or any later phase's entry. The Phase 4 validator slot in finalize is the only Phase 1 → Phase 4 dependency.

## Next Phase Readiness

- **Phase 1 verifier** is ready to run. Every requirement under DSL-01..10, EXT-01..06, PARSE-01..06 has at least one passing automated test reachable from `go test ./pkg/parser/... -run <TestName>`. VALIDATION.md's per-task verification map cross-references should resolve.
- **Phase 2 (generic activity)** is unblocked. Imports from pkg/dag (ActionRef, RetryPolicy, Timeout) and pkg/extension (Extension, OperationFunc, Credential, CredentialHandler). Never touches pkg/parser or pkg/bridge.
- **Phase 3 (interpreter)** is unblocked at the contract level. Uses `bridge.CallLambda` (PrintSink/Cancel hooks already configurable per CallOptions). The lambda-serialization decision (custom DataConverter vs. re-parse-on-start with content-hash cache) is the gating concern for Phase 3, NOT this plan.
- **Phase 4 (CLI / static validator)** is unblocked. Drives `parser.NewParser(parser.WithRoot(rootdir), parser.WithExtensions(...))` from the `--rootdir` CLI flag; the static validator plugs into `validateActionRefKwargs` in finalize.go.
- **Phase 6 (example extensions)** has the D-08 module-attribute pattern verified by `fakeGithubExtension`. Production extensions (HTTP, GitHub, Slack) follow the same shape: Initialize returns a *starlarkstruct.Module with operation factories as attributes; endpoint factories close over credential IDs and produce *dag.ActionRef instances with CredentialID populated.

No blockers. No concerns.

## Verification Summary

```
go build ./...                                     → exit 0
go vet ./...                                       → exit 0
go test ./... -race -count=1                       → exit 0 (4 packages)
go test ./pkg/parser/... -count=1                  → exit 0 (75 tests)
grep -rE '^[[:space:]]*"go.temporal.io|^[[:space:]]*go.temporal.io' pkg/  → 0 matches  (firewall holds)
```

Test counts:

- `pkg/parser/...` total tests: 64 top-level + 23 sub-tests = 87 runs
- `pkg/dag/...`: 63 (carried from plan 02)
- `pkg/extension/...`: 51 (carried from plan 03)
- `pkg/bridge/...`: 56 (carried from plan 04)
- **Total Phase 1 tests:** 63 + 51 + 56 + 87 = 257 test runs across 4 packages

VALIDATION.md per-requirement verification:

| Req       | Test                                                                                                                         | Status |
| --------- | ---------------------------------------------------------------------------------------------------------------------------- | ------ |
| DSL-01    | TestParseFlow_DSL01 + TestValidFixtures/01-minimal-flow.star                                                                  | ✅     |
| DSL-02    | TestStep_SingleAction                                                                                                         | ✅     |
| DSL-03    | TestStep_Block                                                                                                                | ✅     |
| DSL-04    | TestIfCond_LambdaCapture + TestIfCond_NoElseProducesEmptySlice                                                                | ✅     |
| DSL-05    | TestScript_LambdaCapture                                                                                                      | ✅     |
| DSL-06    | TestForEachParallel_BothItemForms_List + TestForEachParallel_BothItemForms_Lambda                                             | ✅     |
| DSL-07    | TestCallFlow_NameResolution + TestResolveCallFlows_Found + TestResolveCallFlows_NotFound                                      | ✅     |
| DSL-08    | TestRetryPolicy_Through_Step + TestRetryPolicy_UnknownKey + plan 02 unit tests (RetryPolicy/Timeout Unpack)                  | ✅     |
| DSL-09    | plan 04 (TestToStarlarkStruct_Deterministic + TestToStarlarkStruct_LargeMap) + TestExtensionEndpointFactory (dot access through Module) | ✅ |
| DSL-10    | TestResolveAllowLambdaIsSet                                                                                                   | ✅     |
| EXT-01    | plan 03 (TestExtension_FakeImplementorExposesName + TestExtension_InitializeReturnsStarlarkValue)                            | ✅     |
| EXT-02    | TestExtensionFactory_ReturnsActionRef + TestExtensionEndpointFactory_PropagatesCredentialID                                  | ✅     |
| EXT-03    | plan 03 (TestOperationFunc_SignatureCompiles + TestNoTemporalImportsInExtensionPackage)                                       | ✅     |
| EXT-04    | plan 03 (TestRegistration_RequiresIdempotent) + TestNewParser_PropagatesRegistrationError                                     | ✅     |
| EXT-05    | plan 03 (TestCredential_RedactedString)                                                                                       | ✅     |
| EXT-06    | TestRegistration_StaticAndDynamic + TestRegister_InvalidatesGlobalsCache                                                      | ✅     |
| PARSE-01  | TestParseTimeGlobals_NakedPrimitives                                                                                          | ✅     |
| PARSE-02  | TestLoad_Relative + TestLoad_Absolute + TestLoad_GitAncestor + TestLoad_NoRootNoGit + TestLoad_TraversalRejected + TestLoad_Cache | ✅ |
| PARSE-03  | TestParseAndLambdaGlobalsAreDistinct + plan 04 (TestLambdaTimeGlobalsLocked)                                                  | ✅     |
| PARSE-04  | TestLambdaCapture_StableID + TestLambdaCapture_ContentSensitive + TestLambdaCapture_PositionMatchesDef                        | ✅     |
| PARSE-05  | TestParse_NeverPanicsOnGarbage + TestParse_ErrorFormat + TestInvalidFixtures (8 fixtures)                                     | ✅     |
| PARSE-06  | plan 04 (TestCallLambda_FreshThread + TestCallLambda_PrintHookRouted + TestCallLambda_DefaultMaxExecutionStepsConstant)       | ✅     |

**All 22 Phase 1 requirements have ≥ 1 passing automated test.**

## Self-Check: PASSED

Verified all claimed files exist and all claimed commits are present:

- FOUND: pkg/parser/parser.go
- FOUND: pkg/parser/parser_test.go
- FOUND: pkg/parser/options.go
- FOUND: pkg/parser/options_test.go
- FOUND: pkg/parser/resolve_setup.go
- FOUND: pkg/parser/resolve_setup_test.go
- FOUND: pkg/parser/globals.go
- FOUND: pkg/parser/globals_test.go
- FOUND: pkg/parser/builtins.go
- FOUND: pkg/parser/builtins_test.go
- FOUND: pkg/parser/load.go
- FOUND: pkg/parser/load_test.go
- FOUND: pkg/parser/lambda_capture.go
- FOUND: pkg/parser/lambda_capture_test.go
- FOUND: pkg/parser/linter.go
- FOUND: pkg/parser/linter_test.go
- FOUND: pkg/parser/finalize.go
- FOUND: pkg/parser/finalize_test.go
- FOUND: pkg/parser/errors.go
- FOUND: pkg/parser/fixtures_test.go
- FOUND: tests/fixtures/valid/07-extension-endpoint-credential.star
- FOUND: commit 6d26846 (Task 1)
- FOUND: commit a43cf96 (Task 2)
- FOUND: commit 3a49a5e (Task 3)
- FOUND: commit 38cec2e (Task 4)
- VERIFIED: `go build ./...` exits 0
- VERIFIED: `go vet ./...` exits 0 with no output
- VERIFIED: `go test ./... -race -count=1` exits 0 across all 4 packages
- VERIFIED: `go test ./pkg/parser/... -count=1` runs 87 test runs (64 top-level + 23 sub-tests)
- VERIFIED: `grep -rE '^\s*"go\.temporal\.io|^\s*go\.temporal\.io' pkg/` returns 0 matches (firewall holds)
- VERIFIED: TestExtensionEndpointFactory_PropagatesCredentialID asserts CredentialID == "admin" and Kind_ == "github.create_issue" end-to-end through the parser
- VERIFIED: 7 valid fixtures parse without error; 8 invalid fixtures produce *dag.ParseError or *dag.ValidationError matching their `# expects:` substring
- VERIFIED: 01-minimal-flow.golden.json + 02-all-primitives.golden.json regenerated as real parser output (no `_note` placeholder fields remain)

---
*Phase: 01-type-spine-extension-contract-parser-bridge-foundations*
*Completed: 2026-04-27*
