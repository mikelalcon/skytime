---
phase: quick-260503-rhy
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - pkg/cli/progress.go
  - pkg/cli/progress_static.go
  - pkg/cli/progress_live.go
  - pkg/cli/progress_test.go
  - pkg/cli/progress_live_test.go
autonomous: true
requirements:
  - CLI-06
  - CLI-07
must_haves:
  truths:
    - "if_cond renders as a SCOPE: header (▶ branch) on the branch event + indented children + footer (✓/✗ ms) on step_complete."
    - "for_each_parallel renders as a SCOPE: header (▶ open) on step_dispatch + indented children (one row per item) + footer (✓/✗ ms) on step_complete."
    - "step_dispatch for kind=if_cond emits NOTHING (header is delegated to the branch event); the [1/1] middle row that confused the user disappears."
    - "Children inside an if_cond branch (path '3a' or '3b') and inside a for_each_parallel iteration (path '3.0', '3.1', ...) are indented 4 spaces per nesting level; nested cases ('3a.0', '3.0.0') indent 8 spaces."
    - "Leaf kinds (step / script / call_flow) keep the existing dispatch+complete row pair (no scope semantics) but inherit the 4-space-per-depth indent rule from path."
    - "Live block in-progress redraw region only contains LEAF rows — no spinner / progress row for if_cond or for_each_parallel (their progress is visible through the static header above plus the indented child rows in the redraw region)."
    - "qkk branch suffix (' → then'/' → else') moves from the if_cond step_complete LINE onto the if_cond HEADER line; existing 'step → branch' suffix on non-if_cond kinds is unaffected (no other kind emits branch events today)."
    - "q9p ANSI color contract preserved: header arrow ▶ wrapped in ansiYellow (matches existing colorArrow); banner / counter / kind / marker colors unchanged."
    - "qx1 kind+label persistence on step_complete preserved for leaf kinds; for if_cond/for_each_parallel scope FOOTER, kind+label still appear (it IS a step_complete row, just without branch suffix because that's already on the header)."
  artifacts:
    - path: pkg/cli/progress.go
      provides: "pathDepth(path) helper shared by static and live renderers"
      contains: "func pathDepth"
    - path: pkg/cli/progress_static.go
      provides: "Static-path scope rendering — renderStepDispatch suppresses if_cond, renderBranch emits header for if_cond, renderStepComplete emits footer for scope kinds + leaf row for leaf kinds, all rows indented per pathDepth"
      contains: "pathDepth"
    - path: pkg/cli/progress_live.go
      provides: "Live-path scope rendering — case step_dispatch / branch / step_complete branch on KindAttr; activeStep entries created only for leaf kinds; redraw region only renders leaf rows; header/footer flushed as static lines via clearRedrawRegion + Fprintf with indent"
      contains: "pathDepth"
    - path: pkg/cli/progress_test.go
      provides: "Static-path tests pinning D-RHY-01..14 + updated qkk fixtures (branch suffix on header, not on if_cond completion)"
      contains: "TestProgress_IfCond_RendersAsScope"
    - path: pkg/cli/progress_live_test.go
      provides: "Live-path tests pinning the same scope rendering + activeStep filtering on the live path"
      contains: "TestLiveBlock_IfCond_RendersAsScope"
  key_links:
    - from: pkg/cli/progress_static.go
      to: pkg/cli/progress.go
      via: pathDepth(path) call sites in renderStepDispatch / renderStepComplete / renderBranch
      pattern: "pathDepth\\("
    - from: pkg/cli/progress_live.go
      to: pkg/cli/progress.go
      via: pathDepth(ev.Path) call sites in case step_dispatch / branch / step_complete
      pattern: "pathDepth\\("
    - from: pkg/cli/progress_static.go
      to: pkg/interpreter/walk_ifcond.go
      via: branch event consumption — renderBranch emits the if_cond header keyed by buffered idx, renderStepComplete with kind=if_cond emits footer (no branch suffix lookup)
      pattern: "if kind == \"if_cond\""
    - from: pkg/cli/progress_live.go
      to: pkg/cli/progress_live.go
      via: activeStep slice now skips KindAttr ∈ {if_cond, for_each_parallel} on step_dispatch — redraw region only knows about leaf rows
      pattern: "if isLeafKind"
---

<objective>
Render `if_cond` and `for_each_parallel` as block scopes (header + indented children + footer) on BOTH the static and live progress paths. The previous quick (qx1) shipped kind+label persistence on the step_complete line, which surfaced a usability complaint: a top-level if_cond's body produced a confusing `[1/1] step  Get branches  ✓ 445ms` row at the same column as its sibling top-level steps. The fix is structural — these two kinds are scopes, not steps, so they get scope rendering with depth-based indent for their children.

Purpose: scope semantics are the user's mental model of `.star` flow control; the renderer should match. This also paves the way for nested compound expressions (if inside for_each, for_each inside if) to render legibly — same indent rule applies recursively via path depth.

Output: pkg/cli renderer changes (no interpreter, no dag, no schema changes); test coverage for D-RHY-01..14; passing `go test ./pkg/cli/... -count=1` AND `go test ./... -count=1` AND a clean `go build -o skytime ./cmd/skytime`.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@CLAUDE.md
@pkg/cli/progress.go
@pkg/cli/progress_static.go
@pkg/cli/progress_live.go
@pkg/cli/progress_test.go
@pkg/cli/progress_live_test.go

<!-- DO NOT MODIFY any of the interpreter walkers below — they emit the
     events the renderer consumes. Verified during planning that path is
     emitted on every event (step_dispatch, branch, step_complete) for
     every kind (step / script / if_cond / for_each_parallel / call_flow).
     The for_each_parallel label is "items=N" (literal); the if_cond
     label is "cond" (literal). Children of for_each carry path "P.I"
     where P is the parent's path and I is the item index; children of
     if_cond carry path "Pa" or "Pb" where P is the parent's path and
     a/b is the then/else branch suffix. Nested combinations stack
     ("3a.0" = inside the then-branch of step 3, item 0 of the for_each
     in that branch). -->

<interfaces>
<!-- Key types and constants the executor needs. Reading these from
     progress_static.go and progress.go avoids a re-discovery loop. -->

From pkg/cli/progress.go:
```go
// progressEvent carries the slog record into the live renderer.
// Fields used by this plan: Kind, KindAttr, Idx, Total, Label, Status,
// DurationMs, Summary, Branch, Path.
type progressEvent struct {
    Kind       string // "flow_start" | "step_dispatch" | "branch" | "step_complete" | "flow_complete" | "raw"
    KindAttr   string // "step" | "script" | "if_cond" | "for_each_parallel" | "call_flow"
    Idx        int64
    Total      int64
    Label      string
    Status     string
    DurationMs int64
    Summary    string
    Branch     string
    Path       string
    // ... (other fields not relevant to this plan)
}

// attrMap accessors used by static-path renderers
type attrMap map[string]slog.Value
func (m attrMap) str(k string) string
func (m attrMap) int(k string) int64
```

From pkg/cli/progress_static.go (existing helpers — DO NOT rename):
```go
const (
    ansiReset       = "\x1b[0m"
    ansiDimCyan     = "\x1b[2;36m"
    ansiBrightCyan  = "\x1b[1;36m"
    ansiBrightWhite = "\x1b[1;37m"
    ansiGreen       = "\x1b[32m"
    ansiRed         = "\x1b[31m"
    ansiYellow      = "\x1b[33m"
)
func (p *progressHandler) colorBanner(s string) string  // ansiDimCyan
func (p *progressHandler) colorCounter(s string) string // ansiBrightCyan
func (p *progressHandler) colorKind(s string) string    // ansiBrightWhite
func (p *progressHandler) colorOk(s string) string      // ansiGreen
func (p *progressHandler) colorErr(s string) string     // ansiRed
func (p *progressHandler) colorArrow(s string) string   // ansiYellow — REUSE for ▶
func padKind(kind string) string                        // right-pads to width 19
```

From pkg/cli/progress_static.go (helpers being REPLACED):
```go
// computeCounter currently returns "  " + "[path/path]" for nested rows.
// Plan replaces the indent calculation with strings.Repeat(" ", 4*pathDepth(path)).
// Counter format: top-level keeps "[N/M]"; nested keeps "[path/path]" (unchanged from qkk).
func (p *progressHandler) computeCounter(idx, total int64, path string) (indent, counter string)

// isNestedPath returns true when path != fmt.Sprintf("%d", idx). Replaced by pathDepth>0.
func isNestedPath(idx int64, path string) bool
```

Decisions referenced (from bug_context):
- D-RHY-01: SUPPRESS step_dispatch when kind == "if_cond" (header delegated to branch event)
- D-RHY-02: EMIT header on step_dispatch when kind == "for_each_parallel" (no branch event fires)
- D-RHY-03: branch event emits header for kind==if_cond; other kinds noop (defensive)
- D-RHY-04: step_complete with kind ∈ {if_cond, for_each_parallel} emits FOOTER (no branch-suffix lookup)
- D-RHY-05: step_complete with kind ∈ {step, script, call_flow} unchanged from qx1+qkk shape
- D-RHY-06: pathDepth("3")=0; pathDepth("3a")=1; pathDepth("3.0")=1; pathDepth("3a.0")=2; pathDepth("3.0.0")=2
- D-RHY-07: indent = strings.Repeat(" ", 4 * pathDepth(path)); applies to ALL emitted rows
- D-RHY-08: liveRenderer.activeStep slice ONLY holds leaf-kind dispatches; if_cond/for_each are static header+footer with no spinner/in-progress row
- D-RHY-09: header glyph "▶" wrapped in ansiYellow (reuses colorArrow)
- D-RHY-10: if_cond header form `<indent>[N/M] if_cond cond ▶ <branch>`
- D-RHY-11: for_each_parallel header form `<indent>[N/M] for_each_parallel items=K ▶ open`
- D-RHY-12: for_each_parallel footer label = ev.Label (already "items=K"); no recomputation
- D-RHY-13: branchByIdx still buffers; renderBranch consumes it INTO THE HEADER and deletes the entry; step_complete for if_cond does NOT read branchByIdx
- D-RHY-14: nested if_cond inside for_each (or vice-versa) — depth combines via pathDepth's two-component sum
</interfaces>

</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: RED — pin scope rendering, indent, and live-path activeStep filtering</name>
  <files>pkg/cli/progress.go, pkg/cli/progress_test.go, pkg/cli/progress_live_test.go</files>
  <behavior>
    Add a single shared helper `pathDepth(path string) int` to pkg/cli/progress.go (no implementation in this RED task — failing-tests-first; or implement it now since it is a pure function with deterministic outputs and the tests for it are unambiguous; pick the option that lets ALL the new behavioral tests fail on the rendering changes ONLY — implementing pathDepth in this task is acceptable AND makes the failing-test signal precise to the rendering change rather than the helper). Tests for pathDepth must pin every D-RHY-06 case.

    Behavioral test cases (all NEW, all initially FAILING):

    Static path (pkg/cli/progress_test.go):
    - TestPathDepth: table-driven — "" → 0; "3" → 0; "3a" → 1; "3b" → 1; "3.0" → 1; "3.1" → 1; "3a.0" → 2; "3.0.0" → 2; "3.0a" → 2; "3a.0.1b" → 3 (defensive — combine letters at any segment).
    - TestProgress_IfCond_RendersAsScope: drive [step_dispatch idx=3 kind=if_cond label=cond path=3] → branch idx=3 path=3 branch=then → step_dispatch idx=1 kind=step label="Get branches" path=3a → step_complete idx=1 kind=step path=3a status=ok duration_ms=445 summary=status=200 → step_complete idx=3 kind=if_cond path=3 status=ok duration_ms=446 summary="". Assert (stripping ANSI for content):
        a. NO line contains "[3/3] if_cond" WITHOUT either "▶ then" suffix or "✓" / "✗" footer marker — i.e., NO bare dispatch row for if_cond.
        b. EXACTLY one line contains "[3/3]" + "if_cond" + "cond" + "▶ then" (header).
        c. EXACTLY one line contains "[3/3]" + "if_cond" + "cond" + "✓ 446ms" (footer); footer line MUST NOT contain "▶" or "→".
        d. EXACTLY one line contains "[1/1]" + "step" + "Get branches" + "✓ 445ms" + "status=200"; that line MUST start with FOUR spaces of indent (children of if_cond branch path "3a" → depth 1 → 4-space indent).
        e. Header + footer must NOT be indented (parent path "3" → depth 0).
    - TestProgress_IfCond_HeaderHasYellowArrow_OnTTY: same sequence with ForceTTY=true; assert raw output contains ansiYellow + "▶" + ansiReset on the header line; footer line must NOT contain ansiYellow.
    - TestProgress_ForEachParallel_RendersAsScope: drive [step_dispatch idx=2 kind=for_each_parallel label="items=3" path=2] → step_dispatch idx=0 kind=step label="Read x" path=2.0 → step_complete idx=0 kind=step path=2.0 status=ok duration_ms=234 summary=status=200 → step_dispatch idx=1 kind=step label="Read y" path=2.1 → step_complete idx=1 kind=step path=2.1 status=ok duration_ms=250 summary=status=200 → step_dispatch idx=2 kind=step label="Read z" path=2.2 → step_complete idx=2 kind=step path=2.2 status=ok duration_ms=246 summary=status=200 → step_complete idx=2 kind=for_each_parallel label="items=3" path=2 status=ok duration_ms=730 summary="". Assert:
        a. EXACTLY one line contains "[2/" + "for_each_parallel" + "items=3" + "▶ open" (header).
        b. EXACTLY one line contains "[2/" + "for_each_parallel" + "items=3" + "✓ 730ms" (footer); footer must NOT contain "▶" or "→".
        c. THREE lines start with FOUR spaces of indent and contain "step" + ("Read x" or "Read y" or "Read z") + "✓".
        d. Header + footer must NOT be indented.
    - TestProgress_NestedScope_DoubleIndent: drive a nested case — for_each at path "3", inner if_cond at path "3.0", inner step at "3.0a". Assert the inner step row starts with EIGHT spaces of indent (depth 2: "3.0a" → "." segment count 1 + trailing letter 1 = 2 → 8 spaces).
    - TestProgress_LeafKinds_KeepExistingShape: drive a top-level [step_dispatch idx=1 kind=step path=1] + [step_complete idx=1 kind=step status=ok duration_ms=50 path=1 ...]. Assert dispatch + complete BOTH still emit (qx1 shape preserved), neither indented (depth 0).
    - TestProgress_StepDispatch_IfCond_Suppressed: drive a SOLO step_dispatch idx=3 kind=if_cond label=cond path=3 (no branch, no complete). Assert progressOut is EMPTY (D-RHY-01 — header is delegated to branch).
    - TestProgress_StepComplete_IfCond_NoBranchSuffix: drive [branch idx=3 branch=then path=3] → [step_complete idx=3 kind=if_cond path=3 status=ok duration_ms=1 summary=""]. Assert the step_complete (footer) line contains "✓ 1ms" but does NOT contain "→ then" (suffix lives on header now). The HEADER line (emitted by renderBranch) MUST contain "▶ then".
    - UPDATE existing TestProgress_BranchAppendsToStepComplete (then + else sub-tests) and TestProgress_StepCompleteIncludesLabelWithBranchSuffix in progress_test.go: they currently assert the qkk inline-on-step_complete shape for if_cond. Migrate them to assert the new header-on-branch shape — the SINGLE rendered line for an if_cond's branch event must contain "[3/3]" + "if_cond" + "cond" + "▶ then" (or "▶ else"), and the step_complete line is now a SEPARATE footer row containing "[3/3]" + "if_cond" + "cond" + "✓ 1ms" with NO arrow. Keep the no-orphan-line and no-old-standalone-shape assertions verbatim. Keep TestProgress_OrphanBranchEvent_NoStandaloneLine (orphan branch with no matching step_complete must still produce zero output — buffer-only).

    Live path (pkg/cli/progress_live_test.go):
    - TestLiveBlock_IfCond_RendersAsScope: parallel of the static test — submit step_dispatch idx=3 kind=if_cond → branch idx=3 then → step_dispatch idx=1 kind=step path=3a → step_complete idx=1 kind=step path=3a → step_complete idx=3 kind=if_cond path=3 → Close. Assertions on stripAnsiTest output mirror static (header has ▶ then, footer has ✓ ms with no ▶/→, child indented 4 spaces).
    - TestLiveBlock_IfCond_NoActiveStepEntry: submit step_dispatch idx=3 kind=if_cond label=cond path=3 alone, then submit a step_dispatch for a leaf step idx=1 kind=step path=3a (mid-flight). After 200ms wait, the redraw region's "[skytime] in-progress N active" line must report N=1, NOT N=2. (The if_cond is NOT in the active list — D-RHY-08.) Then submit step_complete for the leaf and step_complete for the if_cond, Close, and assert the sequence finalizes cleanly.
    - TestLiveBlock_ForEachParallel_RendersAsScope: parallel of the static for_each test; same header/footer/child assertions on stripAnsiTest output.
    - TestLiveBlock_HeaderArrowYellow: submit if_cond dispatch + branch=then; assert RAW (un-stripped) output contains ansiYellow + "▶" + ansiReset on the same redraw cycle.
    - TestLiveBlock_StepCompleteIfCond_NoBranchSuffix: submit branch idx=3 branch=then → step_complete idx=3 kind=if_cond status=ok duration_ms=1. Assert the FOOTER step_complete line does NOT contain "→ then" (it's on the header from the branch event); the HEADER line emitted by case "branch" DOES contain "▶ then".
    - UPDATE existing TestLiveBlock_BranchArrowColor + TestLiveBlock_BranchAppendsToStepComplete + TestLiveBlock_StepCompleteIncludesLabelWithBranchSuffix to assert the new header shape (▶ on header line) instead of inline-on-step_complete (→ on completion line). Same migration as static tests above.

    pathDepth implementation (place in pkg/cli/progress.go — new function, exported lowercase):

    ```go
    // pathDepth returns the renderer indent depth implied by the given
    // step path. The path conventions emitted by pkg/interpreter/walk_*
    // are:
    //   - "<idx>"            top-level (depth 0)
    //   - "<idx>a"/"<idx>b"  inside an if_cond branch (depth +1)
    //   - "<P>.<I>"          inside a for_each_parallel iteration (depth +1)
    //   - combinations stack ("3a.0" = if_cond branch then for_each item → depth 2)
    //
    // Algorithm: count "." separators in path, then add 1 for each segment
    // whose final byte is a letter (the if_cond branch suffix). An empty
    // path (defensive — pre-init dispatch) returns 0.
    func pathDepth(path string) int {
        if path == "" {
            return 0
        }
        depth := strings.Count(path, ".")
        for _, seg := range strings.Split(path, ".") {
            if seg == "" {
                continue
            }
            last := seg[len(seg)-1]
            if (last >= 'a' && last <= 'z') || (last >= 'A' && last <= 'Z') {
                depth++
            }
        }
        return depth
    }
    ```
    Add `import "strings"` to progress.go if not already present (it isn't — currently only uses io/log/slog/os/sync/golang.org/x/term). Do NOT touch other state in progress.go in this RED task; pathDepth is a pure leaf function.

    Why implement pathDepth now: this lets the new tests fail on the SCOPE-RENDERING / INDENT changes (which are the load-bearing behaviors) rather than on a missing helper that is unambiguous and pure. TestPathDepth still fails-then-passes within this RED-task scope as a sanity guard.

    Run the test suite in RED:
    ```bash
    go test ./pkg/cli/... -count=1 -run 'TestPathDepth|TestProgress_IfCond_|TestProgress_ForEachParallel_|TestProgress_NestedScope_|TestProgress_LeafKinds_|TestProgress_StepDispatch_IfCond_|TestProgress_StepComplete_IfCond_|TestLiveBlock_IfCond_|TestLiveBlock_ForEachParallel_|TestLiveBlock_HeaderArrowYellow|TestLiveBlock_StepCompleteIfCond_'
    ```
    Expected: TestPathDepth passes (helper implemented); ALL behavioral tests fail (scope rendering not yet implemented). The migrated qkk tests (TestProgress_BranchAppendsToStepComplete, TestLiveBlock_BranchArrowColor, etc.) fail on the new assertion shape.
  </behavior>
  <action>
    1. Edit pkg/cli/progress.go: add `pathDepth` per the implementation above. Add `import "strings"`.
    2. Edit pkg/cli/progress_test.go: add ALL new TestProgress_* tests above; migrate the existing qkk tests (TestProgress_BranchAppendsToStepComplete then/else sub-tests, TestProgress_StepCompleteIncludesLabelWithBranchSuffix) to assert the new header-on-branch shape — keep no-orphan-line and no-old-standalone-shape defenses verbatim. Add TestPathDepth.
    3. Edit pkg/cli/progress_live_test.go: add ALL new TestLiveBlock_* tests above; migrate TestLiveBlock_BranchArrowColor + TestLiveBlock_BranchAppendsToStepComplete + TestLiveBlock_StepCompleteIncludesLabelWithBranchSuffix to assert new header-on-branch shape.
    4. DO NOT modify progress_static.go or progress_live.go in this task — that's Task 2 (GREEN).
    5. Run `go test ./pkg/cli/... -count=1` to confirm: TestPathDepth passes, behavioral tests fail (the indent/header/footer assertions are not yet satisfied because static + live renderers haven't been updated). The set of failing tests is the precise specification for Task 2.
    6. Commit:
       ```bash
       node "$HOME/.claude/get-shit-done/bin/gsd-tools.cjs" commit "test(260503-rhy): scope rendering header/footer + indent (RED)" --files pkg/cli/progress.go pkg/cli/progress_test.go pkg/cli/progress_live_test.go
       ```
  </action>
  <verify>
    <automated>cd /Users/mikel/dev/ai/temporero && go test ./pkg/cli/... -count=1 -run 'TestPathDepth' && ! go test ./pkg/cli/... -count=1 -run 'TestProgress_IfCond_RendersAsScope|TestLiveBlock_IfCond_RendersAsScope|TestProgress_ForEachParallel_RendersAsScope|TestLiveBlock_ForEachParallel_RendersAsScope' 2>&1 | tail -5</automated>
  </verify>
  <done>
    TestPathDepth GREEN. All TestProgress_IfCond_*, TestProgress_ForEachParallel_*, TestProgress_NestedScope_*, TestProgress_StepDispatch_IfCond_Suppressed, TestProgress_StepComplete_IfCond_NoBranchSuffix tests RED. All TestLiveBlock_IfCond_*, TestLiveBlock_ForEachParallel_*, TestLiveBlock_HeaderArrowYellow, TestLiveBlock_StepCompleteIfCond_NoBranchSuffix tests RED. Migrated TestProgress_BranchAppendsToStepComplete (then/else) + TestProgress_StepCompleteIncludesLabelWithBranchSuffix + TestLiveBlock_BranchArrowColor + TestLiveBlock_BranchAppendsToStepComplete + TestLiveBlock_StepCompleteIncludesLabelWithBranchSuffix tests RED on the new header-shape assertions. RED commit landed.
  </done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: GREEN — implement scope rendering on static + live renderers</name>
  <files>pkg/cli/progress_static.go, pkg/cli/progress_live.go</files>
  <behavior>
    Implement scope semantics so all RED tests from Task 1 turn GREEN, plus every existing pkg/cli test continues to pass.

    Static path (pkg/cli/progress_static.go) — surgical changes:

    1. Replace `computeCounter` so it uses `pathDepth(path)` for indent and keeps the existing counter format choice:
       ```go
       func (p *progressHandler) computeCounter(idx, total int64, path string) (indent, counter string) {
           indent = strings.Repeat(" ", 4*pathDepth(path))
           if isNestedPath(idx, path) {
               counter = fmt.Sprintf("[%s/%s]", path, path)
           } else {
               counter = fmt.Sprintf("[%d/%d]", idx, total)
           }
           return indent, counter
       }
       ```
       Keep `isNestedPath` (still used by counter format choice). The two indent strategies are now decoupled: `pathDepth` controls indent; `isNestedPath` only controls whether the counter shows "[path/path]" vs "[N/M]". This is the minimal change that satisfies both top-level depth-0 + leaf nested rows.

    2. `renderStepDispatch`: add an early-return for `kind == "if_cond"` (D-RHY-01). Existing dispatch shape preserved for all other kinds:
       ```go
       func (p *progressHandler) renderStepDispatch(a attrMap) error {
           kind := a.str("kind")
           if kind == "if_cond" {
               return nil // header delegated to branch event (D-RHY-01)
           }
           // ... rest unchanged, except after computing the standard line:
           // if kind == "for_each_parallel" → emit it as a HEADER instead.
           // ...
       }
       ```
       Concretely: if kind == "for_each_parallel", emit:
       `<indent><colorCounter(counter)> <colorKind(padKind(kind))> <label> <colorArrow("▶")> open`
       Otherwise emit the existing shape (counter + kind + label, no marker — that comes on step_complete).

    3. `renderBranch`: stop being buffer-only for if_cond. Emit a HEADER line directly when branch event fires (D-RHY-03 + D-RHY-10). The buffer is no longer needed — DELETE the branchByIdx map writes. (The existing branchByIdx field stays declared on progressHandler so the WithAttrs/WithGroup shallow-copy compiles; it just goes unused. Simpler to drop it — fewer dead fields. Choose the one with smaller diff: remove the field entirely from progress.go's progressHandler struct + WithAttrs/WithGroup spreads + the lazy-init + the renderStepComplete consumer block. Remove the `branchByIdx map[int64]string` field, the `if p.branchByIdx == nil { p.branchByIdx = make(...) }` lines, and the `if branch, ok := p.branchByIdx[idx]; ok { ... }` block in renderStepComplete.)
       New renderBranch:
       ```go
       func (p *progressHandler) renderBranch(a attrMap) error {
           branch := a.str("branch")
           if branch == "" {
               return nil
           }
           idx := a.int("idx")
           total := a.int("total")
           path := a.str("path")
           indent, counter := p.computeCounter(idx, total, path)
           // D-RHY-10: if_cond header. The kind is ALWAYS if_cond on a
           // branch event today (only walk_ifcond emits it); we hardcode
           // the column without an attr lookup because the branch event
           // does NOT carry "kind" (verified in walk_ifcond.go's
           // logger.Info call). label "cond" is also implicit — the
           // branch event doesn't carry "label" either.
           line := fmt.Sprintf("%s%s %s %s %s %s",
               indent,
               p.colorCounter(counter),
               p.colorKind(padKind("if_cond")),
               "cond",
               p.colorArrow("▶"),
               branch,
           )
           return p.println(line)
       }
       ```

    4. `renderStepComplete`: for kind ∈ {if_cond, for_each_parallel}, emit a FOOTER (no branch suffix). For other kinds, keep the existing qx1+qkk shape MINUS the branchByIdx consumer (which is now dead code per change #3 above). The shape is:
       ```go
       indent, counter := p.computeCounter(idx, total, path)
       marker := p.colorOk("✓")
       if status == "err" { marker = p.colorErr("✗") }
       line := fmt.Sprintf("%s%s %s  %s  %s %dms  %s",
           indent, p.colorCounter(counter), p.colorKind(padKind(kind)),
           label, marker, dur, summary)
       return p.println(line)
       ```
       Note: this is the EXISTING shape from qx1, just without the branchByIdx suffix lookup. The removal is safe because (a) branch is now consumed by renderBranch directly and (b) only if_cond ever emitted branch events, so there's no other kind whose step_complete used the suffix.

    Live path (pkg/cli/progress_live.go) — `applyEvent` changes:

    5. case "step_dispatch":
       ```go
       case "step_dispatch":
           switch ev.KindAttr {
           case "if_cond":
               // D-RHY-01: header delegated to branch event. No active row.
           case "for_each_parallel":
               // D-RHY-02 + D-RHY-11: emit header as static line above redraw region.
               r.clearRedrawRegion()
               indent := strings.Repeat(" ", 4*pathDepth(ev.Path))
               fmt.Fprintf(r.out, "%s%s[%d/%d]%s %s%s%s %s %s▶%s open\n",
                   indent,
                   ansiBrightCyan, ev.Idx, ev.Total, ansiReset,
                   ansiBrightWhite, padKind("for_each_parallel"), ansiReset,
                   ev.Label,
                   ansiYellow, ansiReset)
           default:
               // Leaf kinds (step / script / call_flow): existing behavior —
               // append to active list, redraw will pick it up.
               r.active = append(r.active, &activeStep{
                   Idx: ev.Idx, Total: ev.Total, Kind: ev.KindAttr, Label: ev.Label,
                   StartedAt: time.Now(),
                   Path:      ev.Path, // NEW field — see below
               })
           }
       ```

    6. case "branch": stop buffering; emit the if_cond HEADER as a static line above the redraw region (D-RHY-03 + D-RHY-10). The header carries the parent's idx/total/path which are NOT in the branch event's payload — wait, they ARE: walk_ifcond emits `idx, path, branch` on the branch event but NOT total. Total has to come from somewhere. RE-READ walk_ifcond.go: it emits `event=branch, idx=parentIdx, path=parentPath, branch=branchName` — no `total`. The static path attrMap.int("total") returns 0 if absent. Solution: cache total via the suppressed step_dispatch.
       Concretely, since step_dispatch for kind=if_cond is suppressed at the SHAPE level (no row emitted) but the dispatch event still arrives at applyEvent for the live renderer, we can still capture (idx → total) from it:
       ```go
       // ADD field to liveRenderer:
       //   ifCondTotalByIdx map[int64]int64
       // Initialized in newLiveRenderer.
       case "step_dispatch":
           switch ev.KindAttr {
           case "if_cond":
               // Capture total so the upcoming branch event can render
               // the header with [N/M] counter. Header is delegated to
               // case "branch" (D-RHY-01).
               r.ifCondTotalByIdx[ev.Idx] = ev.Total
           // ...
       case "branch":
           if ev.Branch == "" { return }
           total := r.ifCondTotalByIdx[ev.Idx]
           delete(r.ifCondTotalByIdx, ev.Idx)
           r.clearRedrawRegion()
           indent := strings.Repeat(" ", 4*pathDepth(ev.Path))
           fmt.Fprintf(r.out, "%s%s[%d/%d]%s %s%s%s %s %s▶%s %s\n",
               indent,
               ansiBrightCyan, ev.Idx, total, ansiReset,
               ansiBrightWhite, padKind("if_cond"), ansiReset,
               "cond",
               ansiYellow, ansiReset, ev.Branch)
       ```
       Mirror in static path: add a `total int64` field to a similar map on progressHandler (or reuse a single helper struct). Actually — simpler for static: the static renderer already has `branchByIdx`; convert it from `map[int64]string` to `map[int64]ifCondCtx` where ifCondCtx holds the total captured at dispatch time. Wait — re-examine: the static path's renderStepDispatch sees the if_cond dispatch with the total attr present, but it returns early under D-RHY-01. The total is THERE in the attrMap — capture it before returning. Concrete static-path version:
       ```go
       // ADD field to progressHandler (replace existing branchByIdx map):
       //   ifCondTotalByIdx map[int64]int64
       func (p *progressHandler) renderStepDispatch(a attrMap) error {
           kind := a.str("kind")
           if kind == "if_cond" {
               if p.ifCondTotalByIdx == nil { p.ifCondTotalByIdx = make(map[int64]int64) }
               p.ifCondTotalByIdx[a.int("idx")] = a.int("total")
               return nil
           }
           // for_each_parallel header path...
           // leaf-kind dispatch path...
       }
       func (p *progressHandler) renderBranch(a attrMap) error {
           branch := a.str("branch")
           if branch == "" { return nil }
           idx := a.int("idx")
           total := int64(0)
           if p.ifCondTotalByIdx != nil {
               total = p.ifCondTotalByIdx[idx]
               delete(p.ifCondTotalByIdx, idx)
           }
           path := a.str("path")
           indent, counter := p.computeCounter(idx, total, path)
           // ... emit header ...
       }
       ```
       This replaces the old `branchByIdx map[int64]string` field with `ifCondTotalByIdx map[int64]int64`. WithAttrs/WithGroup shallow-copy this new field instead of the old one. Drop the old field entirely from progress.go's struct + WithAttrs + WithGroup.

    7. case "step_complete":
       ```go
       case "step_complete":
           r.clearRedrawRegion()
           switch ev.KindAttr {
           case "if_cond", "for_each_parallel":
               // D-RHY-04: scope FOOTER. Static line, no active-list mutation
               // (these never went into active). No branch suffix.
               indent := strings.Repeat(" ", 4*pathDepth(ev.Path))
               marker := ansiGreen + "✓" + ansiReset
               if ev.Status == "err" { marker = ansiRed + "✗" + ansiReset }
               fmt.Fprintf(r.out, "%s%s[%d/%d]%s %s%s%s  %s  %s %dms  %s\n",
                   indent,
                   ansiBrightCyan, ev.Idx, ev.Total, ansiReset,
                   ansiBrightWhite, padKind(ev.KindAttr), ansiReset,
                   ev.Label,
                   marker, ev.DurationMs, ev.Summary)
           default:
               // Leaf kind — existing qx1 behavior, but with depth-based indent.
               for i, s := range r.active {
                   if s.Idx == ev.Idx { r.active = append(r.active[:i], r.active[i+1:]...); break }
               }
               indent := strings.Repeat(" ", 4*pathDepth(ev.Path))
               marker := ansiGreen + "✓" + ansiReset
               if ev.Status == "err" { marker = ansiRed + "✗" + ansiReset }
               // No branch suffix (qkk path consumed by case "branch" above).
               fmt.Fprintf(r.out, "%s%s[%d/%d]%s %s%s%s  %s  %s %dms  %s\n",
                   indent,
                   ansiBrightCyan, ev.Idx, ev.Total, ansiReset,
                   ansiBrightWhite, padKind(ev.KindAttr), ansiReset,
                   ev.Label,
                   marker, ev.DurationMs, ev.Summary)
           }
       ```
       Drop the branchByIdx field on liveRenderer entirely (and its initialization in newLiveRenderer). Replace with `ifCondTotalByIdx map[int64]int64` per change #6.

    8. `redraw`: NO functional changes needed for D-RHY-08 — the active slice now only contains leaf-kind rows (because case "step_dispatch" no longer appends if_cond / for_each_parallel entries), so the in-progress redraw region naturally only renders leaves. BUT the per-row indent should also use pathDepth for in-flight rows: this requires storing Path on activeStep.
       ```go
       type activeStep struct {
           Idx       int64
           Total     int64
           Kind      string
           Label     string
           Path      string // NEW — pathDepth(s.Path) determines redraw indent
           StartedAt time.Time
       }
       ```
       In `redraw`:
       ```go
       indent := strings.Repeat(" ", 4*pathDepth(s.Path))
       fmt.Fprintf(r.out, "%s  [%d/%d] %s  %s  %s %.1fs\n",
           indent, s.Idx, s.Total, padKind(s.Kind), label, spinnerFrames[r.spinIdx], elapsed)
       ```
       Note the existing redraw used a literal `"  "` 2-space indent for ALL rows; replace that with the depth-based indent. The `[skytime] in-progress  N active` header line stays at column 0 (depth 0 for the meta-header). Also, ensure pathDepth is reachable from progress_live.go — it lives in the same package (cli), so direct call.

    9. Verify q9p / qkk / qx1 invariants on leaf paths still hold by running the FULL suite, not just the new tests.

    Build sanity:
    ```bash
    go build -o skytime ./cmd/skytime
    ```
    Should compile clean.

    Run all pkg/cli tests:
    ```bash
    go test ./pkg/cli/... -count=1
    ```
    All tests GREEN.

    Run full repo regression:
    ```bash
    go test ./... -count=1
    ```
    All tests GREEN. (Sanity: pkg/cli is leaf — no other package depends on its renderer internals.)
  </behavior>
  <action>
    1. Edit pkg/cli/progress.go: replace `branchByIdx map[int64]string` field on progressHandler with `ifCondTotalByIdx map[int64]int64`. Update WithAttrs and WithGroup shallow-copy to propagate the new field name. (The plan accepts this rename as part of the field-purpose change.)
    2. Edit pkg/cli/progress_static.go: rewrite renderStepDispatch (early-return for if_cond, header path for for_each_parallel, existing path for leaf kinds), renderBranch (emit header for if_cond from buffered total), renderStepComplete (footer for scope kinds, leaf row without branch suffix for leaf kinds), computeCounter (depth-based indent). Add `import "strings"` if not already there (it is).
    3. Edit pkg/cli/progress_live.go: add `Path` field to activeStep, replace `branchByIdx map[int64]string` with `ifCondTotalByIdx map[int64]int64` on liveRenderer (init in newLiveRenderer), rewrite case "step_dispatch" / "branch" / "step_complete" in applyEvent per the spec above, update redraw to use depth-based indent. Add `import "strings"` if not already there.
    4. Run `go test ./pkg/cli/... -count=1` — must be GREEN end to end.
    5. Run `go build -o skytime ./cmd/skytime` — must compile clean.
    6. Run `go test ./... -count=1` — full repo regression must be GREEN.
    7. Commit:
       ```bash
       node "$HOME/.claude/get-shit-done/bin/gsd-tools.cjs" commit "feat(260503-rhy): render if_cond + for_each as block scopes with indent" --files pkg/cli/progress.go pkg/cli/progress_static.go pkg/cli/progress_live.go
       ```
  </action>
  <verify>
    <automated>cd /Users/mikel/dev/ai/temporero && go test ./pkg/cli/... -count=1 && go build -o /tmp/skytime-rhy ./cmd/skytime && go test ./... -count=1</automated>
  </verify>
  <done>
    pkg/cli tests all GREEN (including the 8+ new TestProgress_IfCond_*/ForEachParallel_*/NestedScope_*/StepDispatch_IfCond_/StepComplete_IfCond_NoBranchSuffix and parallel TestLiveBlock_* tests). Full repo `go test ./... -count=1` GREEN. `go build -o skytime ./cmd/skytime` exits 0 with no diagnostics. The example flows render with the new shape: `simple_check.star` shows the if_cond as `[3/3] if_cond cond ▶ then` header + indented `[1/1] step Get branches ✓ ms` child + `[3/3] if_cond cond ✓ ms` footer; `parallel_fanout.star` shows the for_each as `[1/1] for_each_parallel items=3 ▶ open` header + 3 indented child step rows + `[1/1] for_each_parallel items=3 ✓ ms` footer (validated by reading the demo flows in `examples/skeleton/` and tracing the expected event sequence against the new renderer logic). GREEN commit landed.
  </done>
</task>

</tasks>

<verification>
**End-to-end verification (post-Task 2):**

1. `go test ./pkg/cli/... -count=1` — exit 0. Includes new scope-rendering tests (D-RHY-01..14) + migrated qkk/qx1 tests on the new header-shape contract + every preserved q9p / qkk / qx1 / flow_failed test.
2. `go test ./... -count=1` — exit 0. Full repo regression. Renderer is leaf in the dependency graph (only consumed by cmd/skytime); no surprise ripple.
3. `go vet ./...` — clean.
4. `go build -o skytime ./cmd/skytime` — exit 0, no warnings.
5. Spot-trace the renderer against `examples/skeleton/simple_check.star` event sequence (no execution required — pure dry-trace from the walker code paths to the new render logic): top-level [1/3] step → [2/3] script → [3/3] if_cond header (▶ then) → indented [1/1] step → [3/3] if_cond footer (✓ ms). User's confusion reproduced and fixed.
6. Spot-trace against `examples/skeleton/parallel_fanout.star`: top-level [1/1] for_each_parallel header (▶ open) → indented [1/3] / [2/3] / [3/3] step rows (parallel, may interleave under the live block but order under static is deterministic by completion event order) → [1/1] for_each_parallel footer (✓ ms).

**Key invariants pinned by tests (must not regress):**
- q9p ANSI colors (banner / counter / kind / marker / arrow): RAW-output assertions in TestLiveBlock_BannerHasColor, TestLiveBlock_CompletedRowMarkerColor_Ok/Err, TestLiveBlock_FlowFailedHasRedFailedMarker, TestLiveBlock_FlowCompleteBannerColored, TestProgress_IfCond_HeaderHasYellowArrow_OnTTY (NEW).
- qkk no-orphan-line + no-old-standalone-shape: preserved via TestProgress_OrphanBranchEvent_NoStandaloneLine + the `require.NotEqual(t, "     → then", line, ...)` defenses migrated into the updated qkk tests.
- qx1 kind+label persistence: preserved on leaf kinds via TestProgress_StepCompleteIncludesKindAndLabel, TestProgress_StepCompleteIncludesKindAndLabel_Err, TestLiveBlock_StepCompleteIncludesKindAndLabel, TestLiveBlock_StepCompleteIncludesKindAndLabel_Err.
- TTY/verbose mode selection (D4.1-17/20/21): preserved via TestProgress_StaticPath_*, TestProgress_LivePathChosen_*, TestNewProgressHandler_AcceptsVerboseFlag (no changes — these don't touch the per-event rendering shape).
</verification>

<success_criteria>
- BOTH commits land:
  - `test(260503-rhy): scope rendering header/footer + indent (RED)` (Task 1)
  - `feat(260503-rhy): render if_cond + for_each as block scopes with indent` (Task 2)
- `go test ./pkg/cli/... -count=1` exit 0; new tests assert header (▶ branch / ▶ open), footer (✓/✗ ms with no arrow), child indent (4 spaces per depth level), nested indent (8 spaces), and live-path activeStep filtering.
- `go test ./... -count=1` exit 0 — full repo regression clean.
- `go build -o skytime ./cmd/skytime` exit 0 — binary builds.
- Example traces dry-verify the new shape against `examples/skeleton/simple_check.star` and `examples/skeleton/parallel_fanout.star`.
- No interpreter walker file modified (firewall preserved): `git diff --stat HEAD~2 -- pkg/interpreter/` shows zero changes.
- No new dependencies (`go.mod` `require` block unchanged).
</success_criteria>

<output>
After completion, create `.planning/quick/260503-rhy-render-if-cond-and-for-each-parallel-as-/260503-rhy-SUMMARY.md` recording:
- The pathDepth helper and its placement in pkg/cli/progress.go (shared by both renderers).
- The branchByIdx → ifCondTotalByIdx field rename (semantic change: buffer total at dispatch, not branch name; branch name is now consumed inline by renderBranch).
- The activeStep filter at case "step_dispatch" (live path) — D-RHY-08 enforced by NEVER appending scope-kind rows to the active slice, naturally restricting the redraw region to leaves.
- Migration log for the qkk/qx1 test updates (which assertions changed shape vs. which preserved verbatim).
- Confirmation of `go test ./... -count=1` GREEN + binary build success.
</output>
