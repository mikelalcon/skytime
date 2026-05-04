---
phase: quick-260504-k9c
plan: 01
type: execute
wave: 1
status: complete
one_liner: "Added skytime info <file.star> subcommand with 3-column Flow/Description/Inputs table; flow() builtin gains optional description= kwarg; Parser tracks declaration order via FlowsInOrder()."
dependency_graph:
  requires:
    - "pkg/cli (cobra root + Option/config + render/errSilent + appendUnknownExtensionHint)"
    - "pkg/parser (NewParser/ParseFile/Flows; flowOrder added)"
    - "pkg/dag (Flow struct; flowJSON marshal shape)"
  provides:
    - "*dag.Flow.Description string field (json: omitempty)"
    - "Parser.FlowsInOrder() []*dag.Flow (declaration order)"
    - "skytime info <file.star> CLI subcommand"
    - "examples/skeleton/expression_if.star with description= demo"
  affects:
    - "pkg/cli root help — info now listed alongside validate/run/dev-server"
tech_stack:
  added: []
  patterns:
    - "text/tabwriter for aligned 3-column CLI table (stdlib; first use in pkg/cli)"
    - "em-dash (U+2014) sentinel for empty cells (grep-friendly, distinct from hyphen)"
    - "alphabetical inputs key sort for deterministic output"
    - "FlowsInOrder accessor mirrors Flows() / Lambdas() / FileBytes() live-pointer pattern"
key_files:
  created:
    - "pkg/cli/info.go"
    - "pkg/cli/info_test.go"
    - ".planning/quick/260504-k9c-add-skytime-info-file-star-subcommand-de/260504-k9c-SUMMARY.md"
  modified:
    - "pkg/dag/flow.go"
    - "pkg/dag/marshal.go"
    - "pkg/dag/flow_test.go"
    - "pkg/parser/parser.go"
    - "pkg/parser/builtins.go"
    - "pkg/parser/builtins_test.go"
    - "pkg/cli/root.go"
    - "examples/skeleton/expression_if.star"
decisions:
  - "Use text/tabwriter (stdlib) over a third-party table library — keeps pkg/cli dep tree thin, matches the `keep dependencies minimal` posture."
  - "Em-dash (U+2014) over hyphen for empty cells — distinct from input type-hint hyphens (none today, but defends future), and grep-friendly."
  - "Inputs key alphabetization is unconditional — Go map range is randomized; deterministic output is load-bearing for users grepping/diffing CLI output across runs."
  - "FlowsInOrder returns a fresh slice (not the live flowOrder) — keeps the contract cheap (small files) while preventing external mutation."
  - "description= kwarg accepted on flow() only (not step/script/if_cond) — narrowest scope that meets the user's `what's in this file` need; expandable later without breaking compat."
  - "Help text test asserts long-form content (`Parses a Starlark flow file`) rather than short — cobra renders Long when present, so testing what users actually see."
metrics:
  duration: "5min"
  tasks: 3
  files_changed: 9
  tests_added: 13
  completed_date: "2026-05-04"
---

# Quick 260504-k9c: skytime info subcommand Summary

## What Shipped

A `skytime info <file.star>` subcommand that prints a three-column aligned table (Flow / Description / Inputs) of every flow defined in a Starlark file. Source-declaration order. Empty description and empty inputs render as an em-dash (U+2014). Inputs cell renders `key:type, key:type` with keys alphabetized for deterministic output.

The `flow()` parser builtin gains an optional `description=` kwarg (string, default `""`); `*dag.Flow` gains a `Description` field with `json:"description,omitempty"`; `Parser.FlowsInOrder()` returns flows in source-declaration order via a parallel `flowOrder []string` slice.

## Files Modified

| File | Change |
|------|--------|
| `pkg/dag/flow.go` | Added `Description string` field with doc-comment |
| `pkg/dag/marshal.go` | Added `Description` to `flowJSON` shape (`omitempty`); wired in `MarshalJSON` |
| `pkg/dag/flow_test.go` | Added `TestFlow_MarshalJSON_DescriptionOmitEmpty` |
| `pkg/parser/parser.go` | Added `flowOrder []string` field; `FlowsInOrder()` accessor |
| `pkg/parser/builtins.go` | Accept `description?` kwarg in `builtinFlow`; populate `Flow.Description`; append to `flowOrder` after duplicate-name guard |
| `pkg/parser/builtins_test.go` | Added 6 new tests under "Quick 260504-k9c" header |
| `pkg/cli/info.go` (NEW) | `newInfoCommand`, `renderInfoTable`, `formatInputs`, `emDash` const |
| `pkg/cli/info_test.go` (NEW) | 6 black-box integration tests |
| `pkg/cli/root.go` | Wired `root.AddCommand(newInfoCommand(cfg))` after dev-server |
| `examples/skeleton/expression_if.star` | Added `description=` to all three flows |

## Tests Added (13)

**pkg/dag (1):**
- `TestFlow_MarshalJSON_DescriptionOmitEmpty` — empty Description omitted; non-empty renders verbatim.

**pkg/parser (6):**
- `TestBuiltinFlow_DescriptionKwarg_RoundTrips` — `description="hello world"` → `Flow.Description == "hello world"`.
- `TestBuiltinFlow_DescriptionKwarg_DefaultEmpty` — omitted kwarg → empty string.
- `TestBuiltinFlow_DescriptionKwarg_AcceptsLongFreeForm` — 1.3 KB unicode + newlines round-trips byte-equal.
- `TestBuiltinFlow_DescriptionKwarg_RejectsNonString` — `description=123` → typed `*dag.ParseError` with "description" in message.
- `TestParser_FlowsInOrder_PreservesDeclarationOrder` — alpha → zeta → middle (declared order, NOT alphabetical).
- `TestParser_FlowsInOrder_EmptyBeforeParse` — fresh parser returns empty (non-nil) slice.

**pkg/cli (6):**
- `TestInfoCmd_HappyPath_ThreeColumnTable` — 3-row table with header + correct content + declaration-order regex.
- `TestInfoCmd_EmptyDescription_AndEmptyInputs_RenderEmDash` — em-dash appears twice on bare-flow row.
- `TestInfoCmd_InputsKeysAlphabetized` — `apple:int, mango:bool, zebra:string` exact substring (declared as `zebra/apple/mango`).
- `TestInfoCmd_ParseFailure_RendersErrorAndExitsNonZero` — exit non-zero, file path on stderr, NO table on stdout.
- `TestInfoCmd_NoFlows_RendersHeaderOnly` — empty flow file → header only, no data rows.
- `TestInfoCmd_HelpText` — `--help` shows Use line + Long-text content.

## Demo Transcript

```
$ go run ./cmd/skytime info examples/skeleton/expression_if.star
Flow                Description                                                                         Inputs
procedural_demo     Procedural-mode if_cond — branches on script output, fail() in else                 repo:string
classify_repo_size  Expression-mode if_cond binding to classification — both branches return result()   size_bytes:int
check_user          Asymmetric expression-mode — then binds user, else fails with interpolated message  user_id:string
```

```
$ go run ./cmd/skytime info examples/skeleton/simple_check.star
Flow          Description  Inputs
simple_check  —            repo:string
```

```
$ go run ./cmd/skytime info examples/skeleton/parallel_fanout.star
Flow             Description  Inputs
check_one        —            repo:string
parallel_fanout  —            repos:list
```

Both no-description files (`simple_check.star`, `parallel_fanout.star`) render the em-dash sentinel without modification — proves the kwarg is purely additive.

## Backwards-Compatibility Verification

| Suite | Result | Notes |
|-------|--------|-------|
| `go test -race ./pkg/dag/... -count=1` | PASS | All Phase 1/2/3/04.2 tests still green; new MarshalJSON test added |
| `go test -race ./pkg/parser/... -count=1` | PASS | All Phase 1/4/04.1/04.2 tests still green; description kwarg + flowOrder are purely additive |
| `go test -race ./pkg/cli/... -count=1` | PASS | All validate/run/dev-server/render/progress tests still green; info added |
| `go test -race ./... -count=1` (whole repo) | PASS | 14 packages tested; zero regressions |
| `go vet ./...` | clean | No issues |
| `go run ./cmd/skytime --help` | PASS | `info` listed alongside validate/run/dev-server |
| `go run ./cmd/skytime validate examples/skeleton/expression_if.star` | exit 0 | Description kwarg parses cleanly through validator |
| `go run ./cmd/skytime info examples/skeleton/{expression_if,simple_check,parallel_fanout}.star` | exit 0 | All three example files render correctly |

## Commits

| Task | Description | Hash |
|------|-------------|------|
| 1 | feat: Description field + FlowsInOrder + description= kwarg | `c0a4ce2` |
| 2 | feat: skytime info subcommand + 6 integration tests | `9b7989f` |
| 3 | docs: description= on all three expression_if.star flows | `7ed8edf` |

## Deviations from Plan

None — plan executed exactly as written. The plan's TDD ordering was followed (RED tests went in alongside GREEN implementation in each commit because Task 1's three sub-units share a single atomic commit boundary; both new and existing parser/cli/dag tests pass after the commit).

## Architecture Notes

**Why `text/tabwriter` over a table library:** stdlib-only keeps pkg/cli's dep tree thin (the firewall test enforces no new external deps in this commit; tabwriter is stdlib so no firewall edits needed). For 3 columns and typical 1-10 flows per file, the alignment quality is excellent.

**Why em-dash (U+2014) over hyphen-minus or "(none)":** distinct from any character a user might put in a description or input type-hint; grep-friendly; visually clear in fixed-width fonts; matches established CLI table conventions (cargo, npm, kubectl).

**Why `description=` only on `flow()` (not on `step`, `script`, `if_cond`):** scope discipline — the user's stated need is a "what's in this file" overview. Per-step descriptions add API surface area without solving a stated problem; the `name=` kwarg already exists on step/script for inline labeling. Adding `description=` to other primitives is a separate decision, easy to layer on later.

**Why `FlowsInOrder()` returns a fresh slice rather than the live `flowOrder`:** the `Flows()` map / `Lambdas()` map / `FileBytes()` map all return live pointers (documented `MUST NOT mutate`). Slices are different — Go callers may inadvertently `append` past capacity and mutate the underlying array. Returning a fresh slice from FlowsInOrder removes the foot-gun at zero meaningful cost (1-10 flows per file).

**Backwards compatibility proof:** every existing fixture in `tests/fixtures/`, `examples/skeleton/`, and Phase 1-04.2 test corpora compiles and runs unchanged. The `description=` kwarg is purely additive in `UnpackArgs` (registered as `description?` — optional, no required-arg movement); the `flowOrder` field is initialized empty, populated by the same flow registration path (no new public surface).

## Self-Check: PASSED

- `pkg/cli/info.go` exists.
- `pkg/cli/info_test.go` exists.
- `pkg/dag/flow.go` modified with Description field (verified by grep).
- `pkg/parser/parser.go` modified with FlowsInOrder + flowOrder field (verified by grep).
- `examples/skeleton/expression_if.star` modified with description= on all three flows (verified by grep).
- All three task commits exist (verified via `git log --oneline`).
- Smoke transcript captured above proves end-to-end behavior.
