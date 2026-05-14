---
phase: 07-trigger-primitive-server-shell
plan: 04
type: execute
wave: 3
depends_on: [01, 03]
priority: high
estimated_tasks: 6
autonomous: true
requirements:
  - TRIG-05
files_modified:
  - pkg/interpreter/registry.go
  - pkg/interpreter/registry_test.go
  - pkg/worker/boot.go
  - pkg/worker/boot_test.go
  - pkg/worker/worker.go
  - pkg/worker/worker_test.go
  - pkg/worker/options.go
  - pkg/worker/options_test.go
  - pkg/cli/run.go
  - examples/http-github-webhook/cmd/extbin/main.go
must_haves:
  truths:
    - "interpreter.TriggerRegistry exists parallel to FlowRegistry — sync.RWMutex + frozen flag + sorted-slice triggers field + byContentHash map; Register / Freeze / All / ByContentHash methods"
    - "TriggerRegistry.All() returns triggers sorted by (Source.Kind, FlowName, Pos) deterministically"
    - "TriggerRegistry.Register returns ErrRegistryFrozen post-Freeze (mirrors FlowRegistry contract)"
    - "bootRegistry signature is (rootDir string, exts []extension.Extension) (*FlowRegistry, *TriggerRegistry, error) — consumes both Parser.Flows() and Parser.Triggers() from the same parser session"
    - "bootRegistry skips *_test.star for BOTH flows AND triggers (regression check: a *_test.star file declaring trigger(...) does NOT register)"
    - "bootRegistry surfaces parser warnings (Parser.TriggerWarnings()) via slog.Warn at the worker.Logger if non-nil, else slog.Default()"
    - "Worker struct gains triggers *interpreter.TriggerRegistry field plus Worker.Triggers() *interpreter.TriggerRegistry accessor (parallel to Worker.Registry())"
    - "WorkerOptions gains WorkerStopTimeout time.Duration field; threaded into sdkworker.Options.WorkerStopTimeout in NewWorker; default 30s applied in applyDefaults"
    - "Existing pkg/cli/run.go and examples/http-github-webhook/cmd/extbin/main.go compile cleanly after the bootRegistry signature change (one internal caller per § Pitfall 12 — no external impact expected)"
  artifacts:
    - path: pkg/interpreter/registry.go
      provides: "TriggerRegistry struct + NewTriggerRegistry + Register + Freeze + All + ByContentHash + ErrTriggerRegistryFrozen"
      contains: "type TriggerRegistry struct"
    - path: pkg/interpreter/registry_test.go
      provides: "TestTriggerRegistry_RegisterAfterFreeze, TestTriggerRegistry_AllSorted, TestTriggerRegistry_ConcurrentRegister, TestTriggerRegistry_ByContentHash, TestTriggerRegistry_FreezeIdempotent"
      contains: "TestTriggerRegistry_"
    - path: pkg/worker/boot.go
      provides: "bootRegistry returning (*FlowRegistry, *TriggerRegistry, error); same parse loop drains both registries"
      contains: "*interpreter.TriggerRegistry, error"
    - path: pkg/worker/worker.go
      provides: "Worker.triggers field + Worker.Triggers() accessor; NewWorker stores both registries"
      contains: "triggers *interpreter.TriggerRegistry"
    - path: pkg/worker/options.go
      provides: "WorkerStopTimeout field + 30s default in applyDefaults"
      contains: "WorkerStopTimeout time.Duration"
    - path: pkg/worker/boot_test.go
      provides: "TestBootRegistry_RegistersTriggers, TestBootRegistry_SkipsTestFiles (extended with trigger fixture), TestBootRegistry_DrainsParserWarnings"
      contains: "TestBootRegistry_RegistersTriggers"
  key_links:
    - from: pkg/worker/boot.go (bootRegistry)
      to: pkg/parser/parser.go (Parser.Triggers)
      via: "for _, trig := range p.Triggers() { trigReg.Register(...) }"
      pattern: "p\\.Triggers\\(\\)"
    - from: pkg/worker/boot.go (bootRegistry)
      to: pkg/parser/parser.go (Parser.TriggerWarnings)
      via: "for _, w := range p.TriggerWarnings() { logger.Warn(\"parser warning\", \"detail\", w) }"
      pattern: "TriggerWarnings\\(\\)"
    - from: pkg/worker/worker.go (NewWorker)
      to: pkg/worker/boot.go (bootRegistry)
      via: "flowReg, trigReg, err := bootRegistry(opts.RootDir, opts.Extensions)"
      pattern: "trigReg.*bootRegistry"
    - from: pkg/worker/worker.go (NewWorker)
      to: go.temporal.io/sdk/worker (sdkworker.Options.WorkerStopTimeout)
      via: "sdkOpts.WorkerStopTimeout = opts.WorkerStopTimeout"
      pattern: "sdkOpts\\.WorkerStopTimeout"
---

<objective>
Land the worker-side TriggerRegistry plumbing (TRIG-05). Wave 3: depends on Plan 01 (`*dag.Trigger`) and Plan 03 (`Parser.Triggers()` + `Parser.TriggerWarnings()` accessors). Plan 02's `extension.TriggerSource` is a transitive dependency through Plan 01.

Purpose: Mirror `interpreter.FlowRegistry` for triggers — same concurrency discipline, same frozen-after-boot lifecycle, but a different access shape (sorted-slice + byContentHash map per § Pitfall 1, since Triggers are iterated wholesale at Phase 7.1's HTTP-router-mount time, not looked up per-request like flows). Extend `bootRegistry` to drain BOTH `Parser.Flows()` and `Parser.Triggers()` from the SAME parser session. Add `Worker.Triggers()` accessor and `WorkerOptions.WorkerStopTimeout` field (the latter powers Plan 05's drain semantics via SDK's built-in graceful-stop machinery).

Output: `pkg/interpreter/registry.go` extension (~80 LOC for TriggerRegistry), `pkg/worker/boot.go` signature change (~30 LOC of changes), `pkg/worker/worker.go` field + accessor (~10 LOC), `pkg/worker/options.go` new field (~10 LOC), test extensions across all four files (~250 LOC), audit + compile-fix `pkg/cli/run.go` and `examples/http-github-webhook/cmd/extbin/main.go` (no API change expected — `NewWorker` signature unchanged from outside; only its internal call to bootRegistry changes).

LOAD-BEARING CONSTRAINT: The bootRegistry signature change is INTERNAL — `NewWorker` external API is unchanged. § Pitfall 12 verified ONE caller of bootRegistry (NewWorker itself). All `worker.NewWorker(c, worker.WorkerOptions{...})` call sites compile unchanged.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/PROJECT.md
@.planning/ROADMAP.md
@.planning/STATE.md
@.planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md
@.planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md
@.planning/phases/07-trigger-primitive-server-shell/07-VALIDATION.md
@.planning/phases/07-trigger-primitive-server-shell/07-01-SUMMARY.md
@.planning/phases/07-trigger-primitive-server-shell/07-03-SUMMARY.md
@CLAUDE.md
@pkg/interpreter/registry.go
@pkg/worker/boot.go
@pkg/worker/worker.go
@pkg/worker/options.go
@pkg/cli/run.go
@examples/http-github-webhook/cmd/extbin/main.go
@pkg/dag/trigger.go
@pkg/extension/trigger.go
@pkg/extension/testing/triggersource.go

<interfaces>
<!-- Concrete code patterns the executor MUST replicate. -->

Existing FlowRegistry shape (pkg/interpreter/registry.go lines 46-108):
```go
type FlowRegistry struct {
    mu     sync.RWMutex
    frozen bool
    byFlow map[string]map[string]*ParsedFlow
}

var ErrRegistryFrozen = errors.New("interpreter: flow registry is frozen")

func NewRegistry() *FlowRegistry { return &FlowRegistry{byFlow: map[string]map[string]*ParsedFlow{}} }

func (r *FlowRegistry) Register(flowName, contentHash string, parsed *ParsedFlow) error { ... }
func (r *FlowRegistry) Freeze() { ... }
func (r *FlowRegistry) Lookup(flowName, contentHash string) (*ParsedFlow, bool) { ... }
```

Existing bootRegistry (pkg/worker/boot.go full file, line 37-144):
```go
func bootRegistry(rootDir string, exts []extension.Extension) (*interpreter.FlowRegistry, error) {
    // ... walk + parse + register flows ...
}
```

Existing NewWorker (pkg/worker/worker.go lines 39-94):
```go
func NewWorker(c client.Client, opts WorkerOptions) (*Worker, error) {
    if err := opts.applyDefaults(); err != nil { return nil, err }
    registry, err := bootRegistry(opts.RootDir, opts.Extensions)
    if err != nil { return nil, fmt.Errorf("NewWorker: %w", err) }
    // ... build dispatch, build sdkOpts, register workflow + activity ...
    return &Worker{sdk: sdkW, registry: registry, opts: opts}, nil
}
```

Existing applyDefaults (pkg/worker/options.go lines 117-131):
```go
func (o *WorkerOptions) applyDefaults() error {
    if o.RootDir == "" { return errors.New("WorkerOptions: RootDir is required") }
    if o.CredentialHandler == nil { return errors.New("WorkerOptions: CredentialHandler is required ...") }
    if o.BuildID == "" { o.BuildID = defaultBuildID }
    if o.TaskQueue == "" { o.TaskQueue = defaultTaskQueue }
    return nil
}
```

The TriggerRegistry shape THIS PLAN must produce (paste at end of pkg/interpreter/registry.go):
```go
// ErrTriggerRegistryFrozen is returned by TriggerRegistry.Register after
// Freeze has been called. Indicates a worker-boot bug: registration must
// complete before Freeze.
var ErrTriggerRegistryFrozen = errors.New("interpreter: trigger registry is frozen")

// TriggerRegistry stores all *dag.Trigger values registered at boot.
//
// Why this shape diverges from FlowRegistry (§ Pitfall 1): Flows are
// looked up per-workflow-start by (flow_name, content_hash) — that's
// FlowRegistry's primary access pattern. Triggers have a different
// lifecycle: registered once at boot, iterated wholesale once when the
// HTTP router mounts handlers (Phase 7.1) or when the cron scheduler
// reconciles schedules (Phase 7.2). NEVER looked up per-request.
//
// Therefore the primary access shape is a sorted slice plus a per-file
// content_hash secondary index for future hot-reload diagnostics.
//
// Determinism: Freeze() sorts the internal slice by (Source.Kind,
// FlowName, Pos) so All() returns the same order across runs. Plan 05's
// startup banner depends on this sorted order.
//
// Concurrency: same RWMutex + frozen-after-boot model as FlowRegistry —
// Register from worker boot (single goroutine), All from any number of
// readers (HTTP router, cron, dashboard) post-Freeze.
type TriggerRegistry struct {
    mu       sync.RWMutex
    frozen   bool
    triggers []*dag.Trigger
    // byContentHash groups triggers by the content_hash of their owning
    // file. Phase 7 sets but doesn't read this index; future phases (hot
    // reload, per-file diagnostics) consume it.
    byContentHash map[string][]*dag.Trigger
}

// NewTriggerRegistry returns an empty, unfrozen TriggerRegistry. The
// worker's boot step (pkg/worker/boot.go) fills it via Register() then
// calls Freeze() before NewWorker returns.
func NewTriggerRegistry() *TriggerRegistry {
    return &TriggerRegistry{byContentHash: map[string][]*dag.Trigger{}}
}

// Register adds a trigger to the registry, indexed by the content_hash
// of its owning file. Returns ErrTriggerRegistryFrozen post-Freeze. Safe
// for concurrent calls during boot — but boot is single-goroutine in
// practice, so contention is theoretical.
//
// Unlike FlowRegistry.Register, there is NO duplicate detection at the
// (flow, hash) layer — D-07-13 explicitly allows multiple triggers per
// (flow, source-kind) pair. The parser's warnDuplicateTriggers pass
// (Plan 03) emits a warning for byte-identical duplicates; the registry
// stores both.
func (r *TriggerRegistry) Register(contentHash string, t *dag.Trigger) error {
    if t == nil {
        return errors.New("TriggerRegistry.Register: trigger required")
    }
    if contentHash == "" {
        return errors.New("TriggerRegistry.Register: contentHash required")
    }
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.frozen {
        return ErrTriggerRegistryFrozen
    }
    r.triggers = append(r.triggers, t)
    r.byContentHash[contentHash] = append(r.byContentHash[contentHash], t)
    return nil
}

// Freeze marks the registry as immutable AND sorts the internal slice by
// (Source.Kind, FlowName, Pos) for deterministic All() output.
// Idempotent: calling Freeze on an already-frozen registry is a no-op.
func (r *TriggerRegistry) Freeze() {
    r.mu.Lock()
    defer r.mu.Unlock()
    if r.frozen {
        return
    }
    sort.SliceStable(r.triggers, func(i, j int) bool {
        a, b := r.triggers[i], r.triggers[j]
        if a.Source == nil || b.Source == nil {
            // Defensive: nil sources sort last (shouldn't happen post-parse).
            return a.Source != nil
        }
        if a.Source.Kind() != b.Source.Kind() {
            return a.Source.Kind() < b.Source.Kind()
        }
        if a.FlowName != b.FlowName {
            return a.FlowName < b.FlowName
        }
        // Tiebreaker: file:line:col — Pos.String() formats stably.
        return a.Pos.String() < b.Pos.String()
    })
    r.frozen = true
}

// All returns a fresh slice of triggers in sorted order (by Source.Kind,
// then FlowName, then Pos). Plan 05's startup banner reads this. Phase
// 7.1's HTTP router groups by Source.Kind() for handler mounting. Phase
// 7.2's cron reconciler filters by Source type-switch.
//
// Returns an empty (non-nil) slice when no triggers are registered.
// Safe to call before Freeze (returns the slice in registration order
// in that case — only post-Freeze guarantees sorted order).
func (r *TriggerRegistry) All() []*dag.Trigger {
    r.mu.RLock()
    defer r.mu.RUnlock()
    out := make([]*dag.Trigger, len(r.triggers))
    copy(out, r.triggers)
    return out
}

// ByContentHash returns the triggers declared in the file with the given
// content_hash. Returns nil if no triggers were registered for that hash.
// Used by future hot-reload diagnostics (Phase 7+); Phase 7 sets but
// doesn't consume.
func (r *TriggerRegistry) ByContentHash(hash string) []*dag.Trigger {
    r.mu.RLock()
    defer r.mu.RUnlock()
    triggers, ok := r.byContentHash[hash]
    if !ok {
        return nil
    }
    out := make([]*dag.Trigger, len(triggers))
    copy(out, triggers)
    return out
}
```

The bootRegistry signature change THIS PLAN must produce (replace pkg/worker/boot.go body — the signature changes, so EVERY caller must be audited):
```go
// bootRegistry walks rootDir, parses every .star file (recursively),
// computes each file's content_hash (sha256 of file bytes), and registers
// the flows AND triggers it declares in fresh registries. Returns the
// frozen registries on success.
//
// Phase 7 extension: the parse loop drains Parser.Triggers() in addition
// to Parser.Flows(); the same parser session collects both. Trigger
// warnings (Parser.TriggerWarnings(), e.g., D-07-13 byte-identical
// duplicate-trigger warnings) are surfaced via slog.Warn against
// slog.Default() at boot time.
//
// Errors propagate from filesystem walk, parse failures, and registry
// registration. Trigger warnings do NOT propagate as errors — they're
// purely informational.
func bootRegistry(rootDir string, exts []extension.Extension) (*interpreter.FlowRegistry, *interpreter.TriggerRegistry, error) {
    if rootDir == "" {
        return nil, nil, fmt.Errorf("bootRegistry: rootDir required")
    }
    if _, err := os.Stat(rootDir); err != nil {
        return nil, nil, fmt.Errorf("bootRegistry: rootDir %s: %w", rootDir, err)
    }

    // Same parser session collects both flows and triggers.
    parserOpts := []parser.Option{parser.WithRoot(rootDir)}
    for _, e := range exts {
        parserOpts = append(parserOpts, parser.WithExtensions(e))
    }
    p, err := parser.NewParser(parserOpts...)
    if err != nil {
        return nil, nil, fmt.Errorf("bootRegistry: parser init: %w", err)
    }

    // Walk + collect .star paths (skip *_test.star — production worker
    // does NOT register test files; this rule applies to BOTH flows AND
    // triggers per § Wave 0 Requirements).
    var starFiles []string
    if err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
        if walkErr != nil {
            return walkErr
        }
        if d.IsDir() {
            return nil
        }
        if filepath.Ext(path) == ".star" && !strings.HasSuffix(d.Name(), "_test.star") {
            starFiles = append(starFiles, path)
        }
        return nil
    }); err != nil {
        return nil, nil, fmt.Errorf("bootRegistry: walk: %w", err)
    }
    sort.Strings(starFiles)

    // Compute content_hash per file BEFORE parsing.
    fileHashes := make(map[string]string, len(starFiles))
    for _, path := range starFiles {
        absPath, err := filepath.Abs(path)
        if err != nil {
            return nil, nil, fmt.Errorf("bootRegistry: abs %s: %w", path, err)
        }
        data, err := os.ReadFile(absPath)
        if err != nil {
            return nil, nil, fmt.Errorf("bootRegistry: read %s: %w", absPath, err)
        }
        sum := sha256.Sum256(data)
        fileHashes[absPath] = hex.EncodeToString(sum[:])
    }

    // Parse each file. Errors abort boot.
    for _, path := range starFiles {
        if _, err := p.ParseFile(path); err != nil {
            return nil, nil, fmt.Errorf("bootRegistry: parse %s: %w", path, err)
        }
    }

    // Build the flow registry — unchanged from pre-Phase-7 behavior.
    flowReg := interpreter.NewRegistry()
    lambdas := p.Lambdas()
    flows := p.Flows()
    flowNames := make([]string, 0, len(flows))
    for name := range flows {
        flowNames = append(flowNames, name)
    }
    sort.Strings(flowNames)
    for _, name := range flowNames {
        f := flows[name]
        owningPath := f.Pos.Filename()
        hash, ok := fileHashes[owningPath]
        if !ok {
            return nil, nil, fmt.Errorf("bootRegistry: flow %s defined in %s but file not in walk", name, owningPath)
        }
        parsed := &interpreter.ParsedFlow{Flow: f, Lambdas: lambdas}
        if err := flowReg.Register(name, hash, parsed); err != nil {
            return nil, nil, fmt.Errorf("bootRegistry: register flow %s@%s: %w", name, hash, err)
        }
    }
    flowReg.Freeze()

    // Build the trigger registry — Phase 7 addition.
    trigReg := interpreter.NewTriggerRegistry()
    for _, trig := range p.Triggers() {
        owningPath := trig.Pos.Filename()
        hash, ok := fileHashes[owningPath]
        if !ok {
            // Defensive: if the trigger's owning file isn't in the walk
            // (load() into a path outside rootDir?), surface a clear
            // error. The parser already cached file bytes by absolute
            // path, but bootRegistry only computed hashes for files
            // INSIDE rootDir.
            return nil, nil, fmt.Errorf("bootRegistry: trigger for flow %s defined in %s but file not in walk (likely loaded from outside rootdir)", trig.FlowName, owningPath)
        }
        if err := trigReg.Register(hash, trig); err != nil {
            return nil, nil, fmt.Errorf("bootRegistry: register trigger (flow=%s, source=%s): %w",
                trig.FlowName, trig.Source.Kind(), err)
        }
    }
    trigReg.Freeze()

    // Surface deferred parser warnings (D-07-13 byte-identical duplicate
    // triggers, etc.) at boot time. These are informational, never errors.
    for _, w := range p.TriggerWarnings() {
        slog.Default().Warn("parser warning", "detail", w)
    }

    return flowReg, trigReg, nil
}
```

The Worker struct extension THIS PLAN must produce (modify pkg/worker/worker.go):
```go
type Worker struct {
    sdk      sdkworker.Worker
    registry *interpreter.FlowRegistry
    triggers *interpreter.TriggerRegistry  // NEW (Phase 7)
    opts     WorkerOptions
    stopOnce sync.Once
}

// (existing Registry accessor unchanged)

// Triggers returns the frozen trigger registry. Used by Phase 7's startup
// banner (Plan 05's pkg/cli/server.go) and by Phase 7.1's HTTP router for
// handler mounting.
func (w *Worker) Triggers() *interpreter.TriggerRegistry { return w.triggers }
```

NewWorker body changes (modify pkg/worker/worker.go::NewWorker):
```go
func NewWorker(c client.Client, opts WorkerOptions) (*Worker, error) {
    if err := opts.applyDefaults(); err != nil {
        return nil, err
    }
    flowReg, trigReg, err := bootRegistry(opts.RootDir, opts.Extensions)
    if err != nil {
        return nil, fmt.Errorf("NewWorker: %w", err)
    }
    dispatch := buildDispatch(opts.Extensions)
    actOpts := []skyactivity.Option{}
    act, err := skyactivity.New(dispatch, opts.CredentialHandler, actOpts...)
    if err != nil {
        return nil, fmt.Errorf("NewWorker: activity init: %w", err)
    }

    sdkOpts := sdkworker.Options{
        BuildID:                 opts.BuildID,
        UseBuildIDForVersioning: opts.UseBuildIDVersioning,
        Identity:                "skytime/" + opts.BuildID,
    }
    if opts.MaxConcurrentActivities > 0 {
        sdkOpts.MaxConcurrentActivityExecutionSize = opts.MaxConcurrentActivities
    }
    if opts.WorkerStopTimeout > 0 {
        sdkOpts.WorkerStopTimeout = opts.WorkerStopTimeout
    }
    _ = sdklog.NewStructuredLogger
    _ = opts.Logger
    sdkW := sdkWorkerNew(c, opts.TaskQueue, sdkOpts)

    wf := interpreter.NewWorkflow(flowReg)
    sdkW.RegisterWorkflowWithOptions(wf, workflow.RegisterOptions{Name: "SkytimeWorkflow"})
    sdkW.RegisterActivityWithOptions(act.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})

    return &Worker{
        sdk:      sdkW,
        registry: flowReg,
        triggers: trigReg, // NEW
        opts:     opts,
    }, nil
}
```

The WorkerOptions extension (modify pkg/worker/options.go):
```go
// (add to existing WorkerOptions struct, after Logger field)

// WorkerStopTimeout is the SDK's graceful-stop duration (D-07-16).
// Worker.Stop() closes the poll channel and blocks up to this duration
// waiting for in-flight tasks to complete; uncompleted workflow tasks
// are NOT ACK'd back to the server, so Temporal redispatches them on
// the next worker start (this IS the durability story). Default 30s
// applied in applyDefaults; matches Kubernetes terminationGracePeriodSeconds
// default per D-07-17.
//
// Phase 5 / Phase 4 paths (skytime run, embedded transient workers) can
// leave this zero — the SDK uses its own zero-default behavior, which is
// fine for one-shot workflows. Phase 7's skytime server explicitly sets
// this from --drain-timeout.
WorkerStopTimeout time.Duration
```

applyDefaults extension (modify pkg/worker/options.go::applyDefaults):
```go
const defaultWorkerStopTimeout = 30 * time.Second

func (o *WorkerOptions) applyDefaults() error {
    if o.RootDir == "" {
        return errors.New("WorkerOptions: RootDir is required")
    }
    if o.CredentialHandler == nil {
        return errors.New("WorkerOptions: CredentialHandler is required (use a no-op handler if your flows don't use credentials)")
    }
    if o.BuildID == "" {
        o.BuildID = defaultBuildID
    }
    if o.TaskQueue == "" {
        o.TaskQueue = defaultTaskQueue
    }
    if o.WorkerStopTimeout == 0 {
        o.WorkerStopTimeout = defaultWorkerStopTimeout
    }
    return nil
}
```

Required new import in pkg/worker/options.go: `"time"` (already may be imported transitively via WorkerOptions; add explicitly if absent).
</interfaces>
</context>

<tasks>

<task type="auto" tdd="true">
  <id>07-04-01</id>
  <name>Task 1: Add TriggerRegistry to pkg/interpreter/registry.go + tests</name>
  <read_first>
    - pkg/interpreter/registry.go (FULL file — FlowRegistry's mu/frozen/byFlow shape; understand the lock discipline)
    - pkg/interpreter/registry_test.go (existing tests for FlowRegistry — TestRegistry_*, learn the harness pattern)
    - pkg/dag/trigger.go (Plan 01 output — *Trigger struct)
    - pkg/extension/testing/triggersource.go (Plan 02 output — FakeTriggerSource for test fixtures)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Pitfall 1, lines 425-453 — the canonical TriggerRegistry shape rationale)
  </read_first>
  <files>pkg/interpreter/registry.go, pkg/interpreter/registry_test.go</files>
  <behavior>
    - Test 1 (TestTriggerRegistry_RegisterAfterFreeze): create registry, register one trigger, Freeze, attempt second Register — assert returns ErrTriggerRegistryFrozen.
    - Test 2 (TestTriggerRegistry_AllSorted): register triggers in random order — kind="z", flow="a", pos=line=10; kind="a", flow="z", pos=line=20; kind="a", flow="a", pos=line=30. Freeze. All() returns sorted: [{kind=a, flow=a, line=30}, {kind=a, flow=z, line=20}, {kind=z, flow=a, line=10}].
    - Test 3 (TestTriggerRegistry_ConcurrentRegister): spawn 100 goroutines each calling Register with a distinct trigger; wait; Freeze; assert len(All()) == 100. No data race (verify with -race flag).
    - Test 4 (TestTriggerRegistry_ByContentHash): register two triggers under hash "h1" and one under "h2"; assert ByContentHash("h1") returns 2 triggers, ByContentHash("h2") returns 1, ByContentHash("h3") returns nil.
    - Test 5 (TestTriggerRegistry_FreezeIdempotent): Freeze; assert no panic. Freeze again; assert no panic. All() returns same content.
    - Test 6 (TestTriggerRegistry_AllReturnsSnapshot): Freeze; modify the returned slice (e.g., out[0] = nil); assert internal state unchanged on subsequent All() call (copy semantics).
    - Test 7 (TestTriggerRegistry_RegisterNilTrigger): Register("h", nil) returns non-nil error containing "trigger required".
    - Test 8 (TestTriggerRegistry_RegisterEmptyHash): Register("", validTrigger) returns non-nil error containing "contentHash required".
  </behavior>
  <action>
    Step 1 — APPEND to `pkg/interpreter/registry.go` the following (paste verbatim from `<interfaces>`):
    - `var ErrTriggerRegistryFrozen` declaration
    - `type TriggerRegistry struct` with all fields
    - `NewTriggerRegistry()` constructor
    - `Register(contentHash, *dag.Trigger) error` method
    - `Freeze()` method (sorts by Source.Kind, FlowName, Pos)
    - `All() []*dag.Trigger` method (returns fresh slice copy)
    - `ByContentHash(hash) []*dag.Trigger` method

    Required new import: `"sort"` (already present in registry.go for FlowRegistry's ContentHashFor; verify and use).

    Step 2 — APPEND to `pkg/interpreter/registry_test.go` the eight new tests above. Use `package interpreter` (white-box, since the existing tests are white-box). Reuse the `*dag.Trigger` constructor from pkg/dag and the `*FakeTriggerSource` test stub from `pkg/extension/testing`.

    For test fixtures, create a helper:
    ```go
    func newTestTrigger(kind, flowName string, line int32, hash string) *dag.Trigger {
        return &dag.Trigger{
            Pos:      syntax.Position{Filename: "fix.star", Line: line, Col: 1},
            FlowName: flowName,
            Source:   &exttest.FakeTriggerSource{KindName: kind, ReqFields: []string{"payload"}},
        }
    }
    // Note: Pos.Filename is a method on syntax.Position, but Filename
    // field is constructed via the Pos.Filename pattern — verify against
    // the actual Position struct in go.starlark.net/syntax. Most likely
    // syntax.MakePosition("fix.star", line, 1) is the correct constructor.
    ```

    NOTE: `syntax.Position` has private fields; construct via `syntax.MakePosition("fix.star", line, 1)`. If the function signature is different (verify via `go doc go.starlark.net/syntax MakePosition`), adjust accordingly.

    Imports for `pkg/interpreter/registry_test.go`:
    ```go
    import (
        "errors"
        "sync"
        "testing"

        "github.com/stretchr/testify/assert"
        "github.com/stretchr/testify/require"

        "go.starlark.net/syntax"

        "github.com/mikelalcon/skytime/pkg/dag"
        exttest "github.com/mikelalcon/skytime/pkg/extension/testing"
    )
    ```

    Step 3 — Run:
    ```bash
    go build ./pkg/interpreter/...
    go vet ./pkg/interpreter/...
    go test ./pkg/interpreter/ -run TestTriggerRegistry_ -count=1 -race
    go test ./pkg/interpreter/ -count=1 -race  # FULL — confirm no FlowRegistry regression
    ```

    All must exit 0.

    DO NOT modify FlowRegistry.
    DO NOT add Lookup-by-flow-name semantics to TriggerRegistry — that's not how the registry is consumed (per § Pitfall 1).
    DO NOT introduce a duplicate-detection error path in Register — D-07-13 explicitly allows duplicates.
  </action>
  <acceptance_criteria>
    - `grep -nE 'var ErrTriggerRegistryFrozen' pkg/interpreter/registry.go` returns exactly one match
    - `grep -nE 'type TriggerRegistry struct' pkg/interpreter/registry.go` returns exactly one match
    - `grep -nE 'func NewTriggerRegistry\(\)' pkg/interpreter/registry.go` returns exactly one match
    - `grep -nE 'func \(r \*TriggerRegistry\) Register\(' pkg/interpreter/registry.go` returns exactly one match
    - `grep -nE 'func \(r \*TriggerRegistry\) Freeze\(\)' pkg/interpreter/registry.go` returns exactly one match
    - `grep -nE 'func \(r \*TriggerRegistry\) All\(\) \[\]\*dag\.Trigger' pkg/interpreter/registry.go` returns exactly one match
    - `grep -nE 'func \(r \*TriggerRegistry\) ByContentHash\(' pkg/interpreter/registry.go` returns exactly one match
    - `grep -n 'sort.SliceStable' pkg/interpreter/registry.go` returns at least one match (deterministic sort in Freeze)
    - `grep -c '^func TestTriggerRegistry_' pkg/interpreter/registry_test.go` returns at least 8
    - `go test ./pkg/interpreter/ -run TestTriggerRegistry_ -count=1 -race` exits 0
    - `go test ./pkg/interpreter/ -count=1 -race` exits 0 (REGRESSION — full suite)
    - `go build ./pkg/interpreter/...` exits 0
    - `go vet ./pkg/interpreter/...` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go test ./pkg/interpreter/ -run TestTriggerRegistry_ -count=1 -race && go test ./pkg/interpreter/ -count=1 -race && go vet ./pkg/interpreter/... && grep -q 'type TriggerRegistry struct' pkg/interpreter/registry.go && grep -q 'func NewTriggerRegistry' pkg/interpreter/registry.go</automated>
  </verify>
  <done>
    `interpreter.TriggerRegistry` is a sealed registry parallel to FlowRegistry: sync.RWMutex + frozen flag + sorted-slice + byContentHash map. All eight tests for register-after-freeze, sorting, concurrency, by-hash, idempotent freeze, snapshot semantics, nil-arg validation pass under -race. FlowRegistry tests remain green.
  </done>
</task>

<task type="auto">
  <id>07-04-02</id>
  <name>Task 2: Extend pkg/worker/boot.go to return (*FlowRegistry, *TriggerRegistry, error) and drain Parser.Triggers()</name>
  <read_first>
    - pkg/worker/boot.go (FULL file — current bootRegistry; understand the parse loop)
    - pkg/parser/parser.go (Plan 03 output — Parser.Triggers() and Parser.TriggerWarnings() signatures)
    - pkg/interpreter/registry.go (Task 1 output — TriggerRegistry)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Pitfall 12 — verified ONE caller of bootRegistry: NewWorker)
    - .planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md (D-07-11 sibling registry; "bootRegistry returns (*FlowRegistry, *TriggerRegistry, error)")
  </read_first>
  <files>pkg/worker/boot.go</files>
  <action>
    Edit `pkg/worker/boot.go::bootRegistry` (REPLACE the entire function body and signature) with the version from `<interfaces>`. Key changes:

    1. Signature: `func bootRegistry(rootDir string, exts []extension.Extension) (*interpreter.FlowRegistry, *interpreter.TriggerRegistry, error)`.
    2. Same parse loop produces both registries from the same parser session.
    3. The trigger-registration loop iterates `p.Triggers()` (returns sorted slice per Plan 03) and calls `trigReg.Register(hash, trig)`.
    4. After both registries are frozen, drain `p.TriggerWarnings()` via `slog.Default().Warn("parser warning", "detail", w)`.
    5. The `*_test.star` skip rule applies to BOTH flows AND triggers (proven by the existing condition: a `*_test.star` file is excluded from `starFiles`, so neither its flows nor its triggers are parsed at all).

    Add the `"log/slog"` import if not already present.

    Critical edge cases preserved from existing code:
    - Empty rootDir (`""`) → error (line 38 in original).
    - rootDir doesn't exist → error wrapping the os.Stat failure.
    - Walk error → error.
    - File not in walk but trigger references it → defensive error per the new code in `<interfaces>` (the trigger's owning file's path was loaded via `load()` from outside rootDir; surface a clear error).

    Verify:
    ```bash
    go build ./pkg/worker/...
    go vet ./pkg/worker/...
    ```

    Both must exit 0. **The existing tests will FAIL** because NewWorker (in worker.go) still calls the old 2-return-value bootRegistry — that's expected; Task 3 fixes it.

    DO NOT touch pkg/worker/worker.go in this task — Task 3 owns that.
    DO NOT remove the *_test.star skip — it remains correct for both flows and triggers.
    DO NOT add a `Triggers map[string]*dag.Trigger` field to ParsedFlow — triggers are NOT per-flow; they live in the standalone TriggerRegistry.
  </action>
  <acceptance_criteria>
    - `grep -nE 'func bootRegistry\(rootDir string, exts \[\]extension\.Extension\) \(\*interpreter\.FlowRegistry, \*interpreter\.TriggerRegistry, error\)' pkg/worker/boot.go` returns exactly one match
    - `grep -n 'p.Triggers()' pkg/worker/boot.go` returns exactly one match (the new drain loop)
    - `grep -n 'p.TriggerWarnings()' pkg/worker/boot.go` returns exactly one match (the new warning surface)
    - `grep -n 'interpreter.NewTriggerRegistry()' pkg/worker/boot.go` returns exactly one match
    - `grep -n 'trigReg.Register(' pkg/worker/boot.go` returns exactly one match
    - `grep -n 'trigReg.Freeze()' pkg/worker/boot.go` returns exactly one match
    - `grep -n 'log/slog' pkg/worker/boot.go` returns exactly one match (the new import)
    - The string `"\\*_test.star"` skip logic is preserved (regression: `grep -n '_test.star' pkg/worker/boot.go` returns at least one match)
    - `go build ./pkg/worker/...` FAILS (expected — worker.go still references the old signature; Task 3 fixes it). Document this expectation in the task verify step.
  </acceptance_criteria>
  <verify>
    <automated>grep -qE 'func bootRegistry\(rootDir string, exts \[\]extension\.Extension\) \(\*interpreter\.FlowRegistry, \*interpreter\.TriggerRegistry, error\)' pkg/worker/boot.go && grep -q 'p.Triggers()' pkg/worker/boot.go && grep -q 'p.TriggerWarnings()' pkg/worker/boot.go && grep -q 'interpreter.NewTriggerRegistry' pkg/worker/boot.go && grep -q '_test.star' pkg/worker/boot.go</automated>
  </verify>
  <done>
    `bootRegistry` returns three values; same parser session produces both registries; trigger warnings surface via slog.Default().Warn at boot. The *_test.star skip applies uniformly to both flows and triggers. The package WILL NOT compile until Task 3 updates NewWorker — the verify only greps source patterns, doesn't run go build.
  </done>
</task>

<task type="auto">
  <id>07-04-03</id>
  <name>Task 3: Add Worker.triggers field + Worker.Triggers() accessor + WorkerOptions.WorkerStopTimeout — restore compile</name>
  <read_first>
    - pkg/worker/worker.go (FULL file — current Worker struct + NewWorker)
    - pkg/worker/options.go (FULL file — WorkerOptions struct + applyDefaults)
    - pkg/worker/boot.go (Task 2 output — new bootRegistry signature)
    - pkg/interpreter/registry.go (Task 1 output — *interpreter.TriggerRegistry)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Pitfall 12 — Worker shape change; § Library/SDK Knowledge — WorkerStopTimeout SDK semantics, lines 124-179)
    - .planning/phases/07-trigger-primitive-server-shell/07-CONTEXT.md (D-07-16 drain timeout, D-07-17 default 30s)
  </read_first>
  <files>pkg/worker/worker.go, pkg/worker/options.go</files>
  <action>
    Step 1 — Edit `pkg/worker/options.go`:

    1. Add `"time"` to the import block if not present (verify; the file imports stdlib but verify time is there).
    2. APPEND to the `WorkerOptions` struct (after the `Logger *slog.Logger` field) the `WorkerStopTimeout time.Duration` field VERBATIM from `<interfaces>` (with full doc comment).
    3. Add a `defaultWorkerStopTimeout = 30 * time.Second` constant near the existing `defaultTaskQueue` block (line 14).
    4. Update `applyDefaults` to set the default when zero VERBATIM from `<interfaces>`.

    Step 2 — Edit `pkg/worker/worker.go`:

    1. Add `triggers *interpreter.TriggerRegistry` to the `Worker` struct (after `registry *interpreter.FlowRegistry`).
    2. Update the variable name in NewWorker — rename `registry` to `flowReg` and add `trigReg` capture from the THREE-return bootRegistry:
       ```go
       flowReg, trigReg, err := bootRegistry(opts.RootDir, opts.Extensions)
       if err != nil {
           return nil, fmt.Errorf("NewWorker: %w", err)
       }
       ```
    3. Update the Worker construction at the end of NewWorker:
       ```go
       return &Worker{
           sdk:      sdkW,
           registry: flowReg,
           triggers: trigReg, // NEW
           opts:     opts,
       }, nil
       ```
    4. Update `interpreter.NewWorkflow(registry)` call to use the new variable name: `interpreter.NewWorkflow(flowReg)`.
    5. Thread `WorkerStopTimeout` into `sdkOpts`:
       ```go
       if opts.WorkerStopTimeout > 0 {
           sdkOpts.WorkerStopTimeout = opts.WorkerStopTimeout
       }
       ```
       Insert after the `MaxConcurrentActivities` block in NewWorker.
    6. Add `Worker.Triggers()` accessor below the existing `Registry()` accessor:
       ```go
       // Triggers returns the frozen trigger registry. Used by Phase 7's
       // startup banner (pkg/cli/server.go) and by Phase 7.1's HTTP router
       // for handler mounting. Empty registry (no .star file declared a
       // trigger) is the normal case for Phase 4-6 worker uses.
       func (w *Worker) Triggers() *interpreter.TriggerRegistry { return w.triggers }
       ```

    Step 3 — Verify compile:
    ```bash
    go build ./pkg/worker/...
    go vet ./pkg/worker/...
    ```

    Both must now exit 0 (Task 2's change is now reconciled).

    Step 4 — Quick regression spot-check:
    ```bash
    go test ./pkg/worker/ -run TestWorker -count=1 -race  # existing tests still green
    ```

    Note: TestBootRegistry_RegistersTriggers and TestBootRegistry_SkipsTestFiles are wired in Task 4; this task only restores compile.

    DO NOT remove the existing `Worker.Registry()` accessor.
    DO NOT change the `MaxConcurrentActivities` threading.
    DO NOT remove the `_ = sdklog.NewStructuredLogger` no-op (the existing comment explains why it's retained).
  </action>
  <acceptance_criteria>
    - `grep -nE 'WorkerStopTimeout time\.Duration' pkg/worker/options.go` returns exactly one match (the new field)
    - `grep -nE 'defaultWorkerStopTimeout = 30 \* time\.Second' pkg/worker/options.go` returns exactly one match
    - `grep -n 'o.WorkerStopTimeout = defaultWorkerStopTimeout' pkg/worker/options.go` returns exactly one match (in applyDefaults)
    - `grep -nE 'triggers \*interpreter\.TriggerRegistry' pkg/worker/worker.go` returns exactly one match (new struct field)
    - `grep -nE 'func \(w \*Worker\) Triggers\(\) \*interpreter\.TriggerRegistry' pkg/worker/worker.go` returns exactly one match
    - `grep -n 'flowReg, trigReg, err := bootRegistry' pkg/worker/worker.go` returns exactly one match (the new three-return call)
    - `grep -n 'sdkOpts.WorkerStopTimeout = opts.WorkerStopTimeout' pkg/worker/worker.go` returns exactly one match (threading into SDK options)
    - `grep -n 'interpreter.NewWorkflow(flowReg)' pkg/worker/worker.go` returns exactly one match (variable rename)
    - `go build ./pkg/worker/...` exits 0 (compile restored)
    - `go vet ./pkg/worker/...` exits 0
    - `go test ./pkg/worker/ -count=1 -race` exits 0 OR fails ONLY in TestBootRegistry_RegistersTriggers / TestBootRegistry_SkipsTestFiles which Task 4 will add (existing tests must remain green)
  </acceptance_criteria>
  <verify>
    <automated>go build ./pkg/worker/... && go vet ./pkg/worker/... && grep -q 'WorkerStopTimeout time.Duration' pkg/worker/options.go && grep -q 'triggers \*interpreter.TriggerRegistry' pkg/worker/worker.go && grep -q 'func (w \*Worker) Triggers() \*interpreter.TriggerRegistry' pkg/worker/worker.go && grep -q 'flowReg, trigReg, err := bootRegistry' pkg/worker/worker.go</automated>
  </verify>
  <done>
    `WorkerOptions.WorkerStopTimeout` field exists with 30s default. Worker struct gains `triggers` field + `Triggers()` accessor. NewWorker drains the new bootRegistry signature and threads WorkerStopTimeout into sdkOpts. The package compiles and existing tests pass (new boot tests come in Task 4).
  </done>
</task>

<task type="auto" tdd="true">
  <id>07-04-04</id>
  <name>Task 4: Author pkg/worker/boot_test.go extensions for TRIG-05 — bootRegistry registers triggers + skips _test.star</name>
  <read_first>
    - pkg/worker/boot_test.go (FULL file — existing tests like TestBootRegistry_*; understand the harness)
    - pkg/worker/boot.go (Task 2 output)
    - pkg/worker/worker.go (Task 3 output)
    - pkg/extension/testing/triggersource.go (Plan 02 output — FakeTriggerSource)
    - pkg/parser/trigger_test.go (Plan 03 output — fakeWebhookExt + fakeTriggerStarlarkValue test harness; reusable shape)
    - .planning/phases/07-trigger-primitive-server-shell/07-VALIDATION.md (TRIG-05 lines)
  </read_first>
  <files>pkg/worker/boot_test.go</files>
  <behavior>
    - Test 1 (TestBootRegistry_RegistersTriggers): create temp directory containing a single `flows.star` declaring `flow(name="check_user", steps=[])` AND `trigger(flow="check_user", source=fake.webhook(req_fields=["payload"]), map=lambda req: req.payload, idempotency_key=lambda req: "k")`. Build a fakeWebhookExt extension. Call bootRegistry. Assert flowReg.ContentHashFor("check_user") returns a non-empty hash; assert trigReg.All() returns exactly 1 trigger; assert trigger.FlowName == "check_user"; assert trigger.Source.Kind() == "skytime.test.webhook".
    - Test 2 (TestBootRegistry_SkipsTestFiles): create temp directory with a `flows.star` declaring `flow(name="prod", steps=[])` (registered) AND a `flows_test.star` declaring `flow(name="testonly", steps=[])` AND `trigger(flow="testonly", source=fake.webhook(...), map=..., idempotency_key=...)`. Call bootRegistry. Assert flowReg.ContentHashFor("prod") returns non-empty; assert flowReg.ContentHashFor("testonly") returns ("", false); assert trigReg.All() returns ZERO triggers (the test file's trigger was skipped along with its flow).
    - Test 3 (TestBootRegistry_DrainsParserWarnings): create temp directory with a `dup.star` declaring TWO byte-identical triggers (same flow + source + lambdas + credential). Call bootRegistry. Assert no error returned; assert trigReg.All() returns 2 triggers (registry doesn't dedup); also capture slog output via a buffered handler and assert the warning text contains "duplicate trigger" (proves D-07-13 warning surfaces at boot, not just on parser).
    - Test 4 (TestBootRegistry_TriggerInLoadedFile): create temp directory with `prod.star` (flow only) + `triggers.star` (trigger referencing the flow via name). Call bootRegistry. Assert both flow and trigger register from their respective files.
    - Test 5 (TestBootRegistry_NoTriggers): existing test pattern — directory with just flows. Assert trigReg.All() returns empty (non-nil) slice.
  </behavior>
  <action>
    Step 1 — Author the new tests at the end of `pkg/worker/boot_test.go`. Use `package worker` (white-box, matching existing tests). Reuse the `fakeWebhookExt` and `fakeTriggerStarlarkValue` patterns from `pkg/parser/trigger_test.go` — but since pkg/worker can't import a test type from pkg/parser (test packages aren't transitively visible), DUPLICATE the helper inline OR factor it to `pkg/extension/testing` if Plan 02's testing package can house Starlark-side helpers.

    DECISION: factor the `fakeWebhookExt` / `fakeTriggerStarlarkValue` to `pkg/extension/testing/fake_extension.go` so both Plan 03 (`pkg/parser/trigger_test.go`) and Plan 04 (`pkg/worker/boot_test.go`) consume one source. **NOTE:** This refactor was NOT scheduled in Plan 03 — handle it here defensively:

    - **OPTION A (preferred if scope allows):** Extract `fakeWebhookExt` + `fakeTriggerStarlarkValue` from `pkg/parser/trigger_test.go` into `pkg/extension/testing/fake_extension.go` (NEW). Update `pkg/parser/trigger_test.go` to import from there. This factors test infra cleanly.

    - **OPTION B (scope-safe):** Duplicate the helper types inline in `pkg/worker/boot_test.go`. Mark the duplicate as "TODO: factor to pkg/extension/testing in Phase 7.1" with a comment.

    Use OPTION B for this task — defer the refactor. Add a `// TODO(phase-7.1): factor fakeWebhookExt to pkg/extension/testing` comment at the helper definition.

    Step 2 — Test 1 implementation:
    ```go
    func TestBootRegistry_RegistersTriggers(t *testing.T) {
        dir := t.TempDir()
        starPath := filepath.Join(dir, "flows.star")
        require.NoError(t, os.WriteFile(starPath, []byte(`
flow(name = "check_user", steps = [])
trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
    credential = "github-app",
)
`), 0o644))
        flowReg, trigReg, err := bootRegistry(dir, []extension.Extension{fakeWebhookExt{}})
        require.NoError(t, err)
        require.NotNil(t, flowReg)
        require.NotNil(t, trigReg)

        // Flow side: existing semantics.
        hash, ok := flowReg.ContentHashFor("check_user")
        require.True(t, ok)
        require.NotEmpty(t, hash)

        // Trigger side: TRIG-05 proof.
        trigs := trigReg.All()
        require.Len(t, trigs, 1, "expected exactly 1 trigger from flows.star")
        assert.Equal(t, "check_user", trigs[0].FlowName)
        assert.Equal(t, "skytime.test.webhook", trigs[0].Source.Kind())
        assert.Equal(t, "github-app", trigs[0].CredentialID)
    }
    ```

    Step 3 — Test 2 implementation (the *_test.star skip case):
    ```go
    func TestBootRegistry_SkipsTestFiles(t *testing.T) {
        dir := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(dir, "flows.star"), []byte(`
flow(name = "prod", steps = [])
`), 0o644))
        require.NoError(t, os.WriteFile(filepath.Join(dir, "flows_test.star"), []byte(`
flow(name = "testonly", steps = [])
trigger(
    flow = "testonly",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
)
`), 0o644))
        flowReg, trigReg, err := bootRegistry(dir, []extension.Extension{fakeWebhookExt{}})
        require.NoError(t, err, "the *_test.star file is skipped, so its trigger never reaches finalize and the prod flow can't conflict")
        // Production flow is registered.
        _, ok := flowReg.ContentHashFor("prod")
        assert.True(t, ok)
        // Test-only flow is NOT registered.
        _, ok = flowReg.ContentHashFor("testonly")
        assert.False(t, ok)
        // Test-only trigger is NOT registered.
        assert.Empty(t, trigReg.All(), "trigger from *_test.star must be skipped")
    }
    ```

    Step 4 — Test 3 implementation (duplicate-warn drains via slog):
    ```go
    func TestBootRegistry_DrainsParserWarnings(t *testing.T) {
        // Capture slog default's output via a custom handler.
        var buf bytes.Buffer
        prev := slog.Default()
        slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
        t.Cleanup(func() { slog.SetDefault(prev) })

        dir := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(dir, "dup.star"), []byte(`
flow(name = "check_user", steps = [])
trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
    credential = "github-app",
)
trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
    credential = "github-app",
)
`), 0o644))
        _, trigReg, err := bootRegistry(dir, []extension.Extension{fakeWebhookExt{}})
        require.NoError(t, err)
        require.Len(t, trigReg.All(), 2, "duplicate triggers accepted; both register")
        assert.Contains(t, buf.String(), "duplicate trigger", "duplicate-warn must surface at boot via slog")
    }
    ```

    Step 5 — Tests 4 and 5 — straightforward applications of the harness above.

    Step 6 — Run:
    ```bash
    go test ./pkg/worker/ -run TestBootRegistry_ -count=1 -race
    go test ./pkg/worker/... -count=1 -race  # FULL — confirms compile + no regression
    ```

    All must exit 0.

    DO NOT add a Tier-3 .star fixture in this task — keep tests inline-source for hermeticity.
    DO NOT remove the existing TestBootRegistry_SkipsTestFiles test if it predates this plan — extend it (add trigger lines to the test file) so the new behavior is asserted in the same test. If the test doesn't exist yet, create it new.
  </action>
  <acceptance_criteria>
    - File `pkg/worker/boot_test.go` exists (extended; the file already exists for FlowRegistry tests)
    - `grep -nE 'func TestBootRegistry_RegistersTriggers' pkg/worker/boot_test.go` returns exactly one match
    - `grep -nE 'func TestBootRegistry_SkipsTestFiles' pkg/worker/boot_test.go` returns exactly one match (extended with trigger fixture content; trigger from *_test.star must NOT register)
    - `grep -nE 'func TestBootRegistry_DrainsParserWarnings' pkg/worker/boot_test.go` returns exactly one match
    - `grep -n 'fakeWebhookExt' pkg/worker/boot_test.go` returns at least 2 matches (definition + use OR import + use)
    - `grep -n 'TODO(phase-7.1): factor fakeWebhookExt' pkg/worker/boot_test.go` returns exactly one match (records the deferred refactor)
    - `go test ./pkg/worker/ -run TestBootRegistry_RegistersTriggers -count=1 -race` exits 0
    - `go test ./pkg/worker/ -run TestBootRegistry_SkipsTestFiles -count=1 -race` exits 0
    - `go test ./pkg/worker/ -run TestBootRegistry_DrainsParserWarnings -count=1 -race` exits 0
    - `go test ./pkg/worker/... -count=1 -race` exits 0 (full regression)
    - `go vet ./pkg/worker/...` exits 0
  </acceptance_criteria>
  <verify>
    <automated>go test ./pkg/worker/ -run 'TestBootRegistry_(RegistersTriggers|SkipsTestFiles|DrainsParserWarnings)' -count=1 -race && go test ./pkg/worker/... -count=1 -race && go vet ./pkg/worker/...</automated>
  </verify>
  <done>
    Three new tests prove TRIG-05: bootRegistry registers triggers from production .star files, skips triggers in *_test.star, and surfaces duplicate-trigger warnings via slog.Default(). The test harness duplicates the parser test's fakeWebhookExt with a TODO marker for Phase 7.1 refactor.
  </done>
</task>

<task type="auto">
  <id>07-04-05</id>
  <name>Task 5: Audit pkg/cli/run.go and examples/http-github-webhook/cmd/extbin/main.go for compile breakage</name>
  <read_first>
    - pkg/cli/run.go (FULL file — looks for worker.NewWorker call site)
    - examples/http-github-webhook/cmd/extbin/main.go (FULL file — top-level wiring; verify it doesn't call bootRegistry directly)
    - pkg/worker/worker.go (Task 3 output — NewWorker signature should be unchanged externally)
    - .planning/phases/07-trigger-primitive-server-shell/07-RESEARCH.md (§ Pitfall 12 — verified zero external callers of bootRegistry)
  </read_first>
  <files>pkg/cli/run.go, examples/http-github-webhook/cmd/extbin/main.go</files>
  <action>
    Audit task — confirm no external API change broke any caller of NewWorker.

    Step 1 — Grep for direct callers of bootRegistry (excluding pkg/worker itself):
    ```bash
    git grep -F 'bootRegistry' -- 'pkg/' 'cmd/' 'examples/' | grep -v 'pkg/worker/'
    ```
    Expected: ZERO matches. If any exist, treat as a critical Plan 04 dependency that needs handling here. Per § Pitfall 12, none should exist.

    Step 2 — Grep for callers of `worker.NewWorker`:
    ```bash
    git grep -F 'worker.NewWorker' -- 'pkg/' 'cmd/' 'examples/'
    ```
    Expected matches:
    - pkg/cli/run.go (existing run subcommand)
    - examples/http-github-webhook/cmd/extbin/main.go (in main()) OR via `pkg/cli` (no direct call from extbin's own code; extbin wires via `cli.NewRootCommand` which internally uses pkg/cli's run subcommand which calls worker.NewWorker)
    - pkg/worker/worker_test.go (existing tests)

    Step 3 — Verify `pkg/cli/run.go::newRunCommand` compiles unchanged:
    ```bash
    go build ./pkg/cli/...
    go vet ./pkg/cli/...
    ```
    Both must exit 0. The `worker.NewWorker(c, worker.WorkerOptions{...})` call at run.go line 109 doesn't set WorkerStopTimeout — that's fine; applyDefaults gives it 30s. No change to run.go expected.

    Step 4 — Verify `examples/http-github-webhook/cmd/extbin/main.go` compiles unchanged:
    ```bash
    go build ./examples/http-github-webhook/cmd/extbin/...
    ```
    Must exit 0.

    Step 5 — Verify the full module compiles:
    ```bash
    go build ./...
    go vet ./...
    ```

    All must exit 0.

    Step 6 — Run all worker + cli + interpreter tests as a regression sweep:
    ```bash
    go test ./pkg/worker/... ./pkg/cli/... ./pkg/interpreter/... -count=1 -race
    ```

    All must exit 0.

    If any test fails or any package fails to compile, that's the audit's primary deliverable: identify the root cause, fix it (e.g., a missing field reference) in this task. Most likely scenarios:
    - A test in pkg/worker that constructed a Worker directly with `&Worker{registry: ...}` literal — needs `triggers: nil` added.
    - An extbin test asserting subcommand list — unaffected by this plan but may surface in adjacent unrelated rename concerns (those are Plan 06).

    DO NOT modify pkg/cli/run.go to thread WorkerStopTimeout — `skytime run` is a one-shot worker; the 30s default is fine. Plan 05's skytime server explicitly threads it.
    DO NOT modify extbin's main.go shape — Phase 7.4 owns the credfile lift. Phase 7's extbin gains the `server` subcommand for free via cli.NewRootCommand (Plan 05 wires that), but extbin's own main.go isn't edited here.
  </action>
  <acceptance_criteria>
    - `git grep -F 'bootRegistry' -- 'pkg/' 'cmd/' 'examples/' | grep -v 'pkg/worker/'` returns ZERO matches (verified — no external callers)
    - `go build ./pkg/cli/...` exits 0 (no change to run.go)
    - `go build ./examples/http-github-webhook/cmd/extbin/...` exits 0
    - `go build ./...` exits 0 (full module compile)
    - `go vet ./...` exits 0
    - `go test ./pkg/worker/... ./pkg/cli/... ./pkg/interpreter/... -count=1 -race` exits 0 (regression — three packages all green)
    - `git diff pkg/cli/run.go` shows ZERO changes (no edit needed)
    - `git diff examples/http-github-webhook/cmd/extbin/main.go` shows ZERO changes (no edit needed)
  </acceptance_criteria>
  <verify>
    <automated>go build ./... && go vet ./... && go test ./pkg/worker/... ./pkg/cli/... ./pkg/interpreter/... -count=1 -race && [ "$(git grep -F 'bootRegistry' -- 'pkg/' 'cmd/' 'examples/' | grep -v 'pkg/worker/' | wc -l | tr -d ' ')" = "0" ]</automated>
  </verify>
  <done>
    No external caller of bootRegistry exists; pkg/cli/run.go and examples/extbin/main.go compile and pass tests unchanged. Plan 04's API impact is contained to pkg/worker — NewWorker external signature is preserved.
  </done>
</task>

<task type="auto">
  <id>07-04-06</id>
  <name>Task 6: Add Worker.Triggers() integration sanity test in pkg/worker/worker_test.go</name>
  <read_first>
    - pkg/worker/worker_test.go (FULL file — existing TestNewWorker_*, TestWorker_*, etc.)
    - pkg/worker/worker.go (Task 3 output — Worker.Triggers accessor)
    - pkg/worker/boot_test.go (Task 4 output — fakeWebhookExt helper available within pkg/worker)
  </read_first>
  <files>pkg/worker/worker_test.go</files>
  <behavior>
    - Test 1 (TestNewWorker_RegistersTriggers): create temp dir with one .star file declaring flow + trigger; build a fake Temporal client (use existing test seam pattern from worker_test.go — likely `sdkWorkerNew` test override); call `worker.NewWorker(...)`; assert `w.Triggers().All()` returns 1 trigger; also assert `w.Registry().ContentHashFor("flow_name")` works (regression).
    - Test 2 (TestNewWorker_WorkerStopTimeoutDefault): construct WorkerOptions with WorkerStopTimeout zero; call NewWorker; verify the captured sdkOpts (via the existing test seam) has WorkerStopTimeout = 30s — proves applyDefaults applied. If the test seam captures sdkOpts, assert directly. If not, assert indirectly via `w.opts.WorkerStopTimeout` (note: opts is package-private, so this works in white-box test).
    - Test 3 (TestNewWorker_WorkerStopTimeoutCustom): construct WorkerOptions with WorkerStopTimeout = 5*time.Second; call NewWorker; assert sdkOpts captured with WorkerStopTimeout = 5*time.Second.
  </behavior>
  <action>
    Step 1 — Read existing worker_test.go to find the sdkWorkerNew test seam pattern. Look for a captureNewWorker helper or a `var capturedOpts sdkworker.Options` global the test sets up.

    If no test seam captures sdkOpts, the simplest path is to set `sdkWorkerNew` to a closure that captures the third argument:
    ```go
    var capturedSdkOpts sdkworker.Options
    sdkWorkerNew = func(c client.Client, taskQueue string, opts sdkworker.Options) sdkworker.Worker {
        capturedSdkOpts = opts
        // Return a no-op fake worker. The existing test infra likely
        // already provides one — reuse it. If not, define inline:
        return fakeSDKWorker{}
    }
    ```

    Step 2 — Author the three tests:

    Test 1:
    ```go
    func TestNewWorker_RegistersTriggers(t *testing.T) {
        dir := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(dir, "flows.star"), []byte(`
flow(name = "check_user", steps = [])
trigger(
    flow = "check_user",
    source = fake.webhook(req_fields = ["payload"]),
    map = lambda req: req.payload,
    idempotency_key = lambda req: "k",
)
`), 0o644))

        // Set up the SDK test seam.
        prev := sdkWorkerNew
        sdkWorkerNew = func(c client.Client, taskQueue string, opts sdkworker.Options) sdkworker.Worker {
            return fakeSDKWorker{} // assumed provided by existing test infra; if not, add a minimal no-op impl
        }
        t.Cleanup(func() { sdkWorkerNew = prev })

        w, err := NewWorker(fakeClient{}, WorkerOptions{
            RootDir:           dir,
            Extensions:        []extension.Extension{fakeWebhookExt{}},
            CredentialHandler: noopCredHandler{},
        })
        require.NoError(t, err)
        require.NotNil(t, w.Triggers())
        assert.Len(t, w.Triggers().All(), 1, "trigger from flows.star registered")
        assert.NotNil(t, w.Registry(), "flow registry preserved")
    }
    ```

    Test 2:
    ```go
    func TestNewWorker_WorkerStopTimeoutDefault(t *testing.T) {
        dir := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(dir, "flows.star"), []byte("flow(name=\"x\", steps=[])"), 0o644))

        var captured sdkworker.Options
        prev := sdkWorkerNew
        sdkWorkerNew = func(c client.Client, taskQueue string, opts sdkworker.Options) sdkworker.Worker {
            captured = opts
            return fakeSDKWorker{}
        }
        t.Cleanup(func() { sdkWorkerNew = prev })

        _, err := NewWorker(fakeClient{}, WorkerOptions{
            RootDir:           dir,
            CredentialHandler: noopCredHandler{},
            // WorkerStopTimeout intentionally zero — applyDefaults supplies 30s.
        })
        require.NoError(t, err)
        assert.Equal(t, 30*time.Second, captured.WorkerStopTimeout)
    }
    ```

    Test 3 (mirror of Test 2 with explicit value):
    ```go
    func TestNewWorker_WorkerStopTimeoutCustom(t *testing.T) {
        dir := t.TempDir()
        require.NoError(t, os.WriteFile(filepath.Join(dir, "flows.star"), []byte("flow(name=\"x\", steps=[])"), 0o644))

        var captured sdkworker.Options
        prev := sdkWorkerNew
        sdkWorkerNew = func(c client.Client, taskQueue string, opts sdkworker.Options) sdkworker.Worker {
            captured = opts
            return fakeSDKWorker{}
        }
        t.Cleanup(func() { sdkWorkerNew = prev })

        _, err := NewWorker(fakeClient{}, WorkerOptions{
            RootDir:           dir,
            CredentialHandler: noopCredHandler{},
            WorkerStopTimeout: 5 * time.Second,
        })
        require.NoError(t, err)
        assert.Equal(t, 5*time.Second, captured.WorkerStopTimeout)
    }
    ```

    NOTE: `fakeSDKWorker`, `fakeClient`, `noopCredHandler` are likely already defined in worker_test.go (or boot_test.go). Reuse them. If not present, the existing test infra is incomplete — add minimal no-op implementations:
    ```go
    type fakeSDKWorker struct{}
    func (fakeSDKWorker) Start() error { return nil }
    func (fakeSDKWorker) Run(_ <-chan interface{}) error { return nil }
    func (fakeSDKWorker) Stop() {}
    func (fakeSDKWorker) RegisterWorkflow(_ interface{}) {}
    func (fakeSDKWorker) RegisterWorkflowWithOptions(_ interface{}, _ workflow.RegisterOptions) {}
    func (fakeSDKWorker) RegisterActivity(_ interface{}) {}
    func (fakeSDKWorker) RegisterActivityWithOptions(_ interface{}, _ activity.RegisterOptions) {}
    // implement the full sdkworker.Worker interface — verify methods via `go doc go.temporal.io/sdk/worker Worker`
    ```

    Verify and adjust against the actual sdkworker.Worker interface signatures (the SDK's Worker is a small interface, ~7-10 methods).

    Step 3 — Run:
    ```bash
    go test ./pkg/worker/ -run 'TestNewWorker_(RegistersTriggers|WorkerStopTimeout)' -count=1 -race
    go test ./pkg/worker/... -count=1 -race  # FULL regression
    ```

    All must exit 0.

    DO NOT add a real Temporal integration test — those live in tests/e2e_skytime_run_test.go (Phase 4). Plan 04 is unit-level only.
  </action>
  <acceptance_criteria>
    - `grep -nE 'func TestNewWorker_RegistersTriggers' pkg/worker/worker_test.go` returns exactly one match
    - `grep -nE 'func TestNewWorker_WorkerStopTimeoutDefault' pkg/worker/worker_test.go` returns exactly one match
    - `grep -nE 'func TestNewWorker_WorkerStopTimeoutCustom' pkg/worker/worker_test.go` returns exactly one match
    - `grep -n 'w.Triggers()' pkg/worker/worker_test.go` returns at least one match (accessor exercised)
    - `grep -n '30\*time.Second' pkg/worker/worker_test.go` returns at least one match (default assertion)
    - `go test ./pkg/worker/ -run TestNewWorker_RegistersTriggers -count=1 -race` exits 0
    - `go test ./pkg/worker/ -run TestNewWorker_WorkerStopTimeoutDefault -count=1 -race` exits 0
    - `go test ./pkg/worker/ -run TestNewWorker_WorkerStopTimeoutCustom -count=1 -race` exits 0
    - `go test ./pkg/worker/... -count=1 -race` exits 0 (FULL regression — every prior test still green)
  </acceptance_criteria>
  <verify>
    <automated>go test ./pkg/worker/ -run 'TestNewWorker_(RegistersTriggers|WorkerStopTimeoutDefault|WorkerStopTimeoutCustom)' -count=1 -race && go test ./pkg/worker/... -count=1 -race</automated>
  </verify>
  <done>
    Three integration sanity tests prove the Worker exposes the trigger registry via its accessor AND threads WorkerStopTimeout into the SDK options (default 30s + custom value). Plan 05's skytime server can now consume both via worker.WorkerOptions{WorkerStopTimeout: drainTimeout} and `w.Triggers()`.
  </done>
</task>

</tasks>

<verification>
After all 6 tasks complete, run:

```bash
go build ./...
go vet ./...
go test ./pkg/interpreter/... ./pkg/worker/... ./pkg/cli/... ./pkg/parser/... ./pkg/dag/... ./pkg/extension/... -count=1 -race
```

All must exit 0. Quick-loop suite per VALIDATION.md sampling rate is green.

Cross-package check (no leakage):
```bash
git grep -F 'TriggerRegistry' -- 'pkg/' | grep -v 'pkg/interpreter/' | grep -v 'pkg/worker/' | head -5
# Expected: only pkg/worker references it. Plan 05 will add pkg/cli references.
```

Wave 3 done — Plan 05 (skytime server subcommand) consumes Worker.Triggers() and WorkerOptions.WorkerStopTimeout.
</verification>

<success_criteria>
- TRIG-05 satisfied: bootRegistry walks --rootdir, registers flows AND triggers from the same parser session; *_test.star files are skipped uniformly.
- D-07-11 satisfied: TriggerRegistry mirrors FlowRegistry concurrency model; bootRegistry returns both registries; NewWorker accepts both internally and exposes Worker.Triggers().
- D-07-13 surfacing satisfied: byte-identical duplicate-trigger warnings flow from parser.TriggerWarnings() to slog.Default().Warn at boot.
- D-07-16 prep satisfied: WorkerOptions.WorkerStopTimeout exists with 30s default; threaded into sdkworker.Options.WorkerStopTimeout. Plan 05 will set this from --drain-timeout.
- The bootRegistry signature change has zero external impact — pkg/cli/run.go and examples/extbin compile unchanged.
- Wave-3 unblocks Wave-4 (Plan 05's skytime server reads w.Triggers() for the startup banner and threads --drain-timeout into WorkerStopTimeout).
</success_criteria>

<output>
After completion, create `.planning/phases/07-trigger-primitive-server-shell/07-04-SUMMARY.md` documenting:
- The TriggerRegistry shape exactly as shipped (struct fields, method signatures, sort key tuple)
- The ErrTriggerRegistryFrozen sentinel
- The bootRegistry signature: `func bootRegistry(rootDir string, exts []extension.Extension) (*interpreter.FlowRegistry, *interpreter.TriggerRegistry, error)`
- The Worker struct's new triggers field + Triggers() accessor
- The WorkerOptions.WorkerStopTimeout field + 30s default behavior
- The slog.Default().Warn flow for parser warnings (D-07-13 byte-identical duplicates)
- The *_test.star skip rule applies to BOTH flows AND triggers
- Confirmation that pkg/cli/run.go and examples/extbin compile unchanged
- Test coverage for TRIG-05 — explicit list of test functions in pkg/worker/boot_test.go and pkg/worker/worker_test.go
- The deferred fakeWebhookExt refactor TODO (Phase 7.1 will factor to pkg/extension/testing)
</output>
</content>
</invoke>