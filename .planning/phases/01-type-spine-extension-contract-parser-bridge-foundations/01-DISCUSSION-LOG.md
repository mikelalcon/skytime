# Phase 1: Type Spine + Extension Contract + Parser/Bridge Foundations - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-04-26
**Phase:** 01-type-spine-extension-contract-parser-bridge-foundations
**Areas discussed:** Module layout & conventions, Extension SDK shape, load() resolution & multi-flow, Lambda capture format

---

## Module Layout & Conventions

### Module path

| Option | Description | Selected |
|--------|-------------|----------|
| github.com/mikel/skytime | Personal GitHub namespace — typical for early projects | |
| github.com/skytime-dev/skytime | Org namespace from day 1 | |
| go.skytime.dev/skytime | Vanity import path | |
| Local-only for now | Placeholder, rename when public | |

**User's choice:** Free text — `github.com/mikelalcon/skytime`
**Notes:** Personal namespace, GitHub user `mikelalcon`.

### Pkg split

| Option | Description | Selected |
|--------|-------------|----------|
| pkg/dag, pkg/parser, ... | Public API under pkg/, future internals under internal/ (Recommended) | ✓ |
| Top-level: dag/, parser/, ... | Flatter — shorter import paths | |
| internal/ for everything | Tightest control but inconvenient for extension authors | |

**User's choice:** pkg/dag, pkg/parser, ...
**Notes:** Aligns with research's recommended layout.

### Test layout

| Option | Description | Selected |
|--------|-------------|----------|
| Co-located *_test.go | Standard Go: same package | |
| Co-located + tests/ for fixtures | *_test.go plus tests/fixtures/ for .star corpus | ✓ |
| Separate test package per pkg | Less common, harder navigation | |

**User's choice:** Co-located + tests/ for fixtures
**Notes:** Fixture directory will hold .star files used by parser/validator/golden tests; differential corpus test in Phase 4 reuses the same directory.

### Errors

| Option | Description | Selected |
|--------|-------------|----------|
| Typed errors with Position field | Custom error types, errors.As at boundaries (Recommended) | ✓ |
| fmt.Errorf with %w wrapping | Plain wrapping, less ergonomic for line/col rendering | |
| Sentinel errors only | Predefined sentinels, less Position info | |

**User's choice:** Typed errors with Position field
**Notes:** ParseError + ValidationError, both expose Position(); CLI in Phase 4 will rely on errors.As to render Starlark-first errors.

---

## Extension SDK Shape

### Registry

| Option | Description | Selected |
|--------|-------------|----------|
| Per-parser registry | parser.Register(github.New(opts)). Test isolation by default (Recommended) | ✓ |
| Global registry + per-parser override | Convenient but global-state issues for tests | |
| Functional options on NewParser | Most idiomatic, declarative | |

**User's choice:** Per-parser registry
**Notes:** No global state. Functional-options style (`skytime.NewParser(skytime.WithExtensions(...))`) is acceptable as a convenience but the underlying mechanism is per-parser.

### Cred wiring

| Option | Description | Selected |
|--------|-------------|----------|
| Op receives Credential as first arg | Explicit, type-safe (Recommended) | |
| CredentialID on ActionRef + resolver | Op fetches via resolver | |
| Per-extension credential context | Hides plumbing; lifecycle questions | |

**User's choice:** Free text — "We want to inject a credential handler that you pass an identifier string and return an auth object (Optional user and a pass/auth token). That way if someone in starlark does gh = github.endpoint('admin'), internally github looks for the 'admin' auth and injects in the created gh object. That way we can inject different credential handlers (e.g. for the example we could read from $HOME/.timeless.conf or similar"
**Notes:** Pluggable CredentialHandler interface, IDs in Starlark, factory pattern (`github.endpoint("admin")` returns a credential-aware extension instance whose ActionRefs carry only the credential ID).

### Cred lifecycle (follow-up)

| Option | Description | Selected |
|--------|-------------|----------|
| JIT inside activity | Workflow state holds only ID, secret never reaches Temporal history (Recommended) | ✓ |
| At parse time, auth in ActionRef | Resolved early, secret crosses every boundary | |
| Hybrid — once per worker, cached | Compromise; eviction story complicated | |

**User's choice:** Free text — "JIT, but the endpoint might decide to cache on the first API call"
**Notes:** Resolution is JIT inside the activity (preserves security invariant). Extensions may cache the resolved credential internally in their own state (once per extension instance per worker process). State, ActionRef, and Temporal history never contain the secret.

### CredentialHandler interface

| Option | Description | Selected |
|--------|-------------|----------|
| Generic Credential struct | One shape covers Bearer/Basic/OAuth/headers | |
| Typed per-credential-kind | Per-kind types, type assertion at op boundary (Recommended for type safety) | ✓ |
| Opaque any | Maximum flexibility, weakest contract | |

**User's choice:** Typed per-credential-kind
**Notes:** Sealed interface + concrete kinds (BearerCredential, BasicCredential, APIKeyCredential). Each has redacted String(). New credential kinds are non-breaking additions.

### Handler scope

| Option | Description | Selected |
|--------|-------------|----------|
| Once on the worker | Single handler routes by ID across all extensions (Recommended) | ✓ |
| Per-extension at registration | Per-ext routing; supports different credential systems per extension | |
| Both — default + per-ext override | Most flexible, more API surface | |

**User's choice:** Once on the worker
**Notes:** worker.Run(client, flowDir, skytime.WithCredentialHandler(...)). Phase 1 lays the interface; Phase 3 wires the worker entry point.

### Op schema

| Option | Description | Selected |
|--------|-------------|----------|
| Typed Go struct + reflection | star:"..." tags, single source of truth (Recommended) | ✓ |
| Schema struct + manual marshalling | Imperative, less type-safe | |
| Free-form map[string]interface{} | No safety, violates spec | |

**User's choice:** Typed Go struct + reflection
**Notes:** Reflection on `star:"name,required"` tags drives parse-time kwarg validation, parse-time error rendering with line/col, and (later) schema export for static validation in Phase 4.

### Idempotent default

| Option | Description | Selected |
|--------|-------------|----------|
| Default false — must opt in | Safe-by-default | |
| Default true — must opt out | Author burden lower | |
| Required — no default, fail to register | Forces conscious choice | ✓ |

**User's choice:** Required — no default, fail to register
**Notes:** Extension registration fails if any operation is missing the Idempotent declaration. Catches Slack chat.postMessage-style mistakes at registration time.

---

## load() Resolution & Multi-Flow

### load path

| Option | Description | Selected |
|--------|-------------|----------|
| Relative paths | load("./shared/utils.star", "f") (Recommended) | |
| Bazel-style labels | load("//shared:utils.star", "f") | |
| Module-style | load("skytime://shared/utils", "f") | |
| Both relative and absolute | Both syntaxes supported | |

**User's choice:** Free text — "relative and absolute paths. Absolute paths are resolved to the root of either root config path (that could be pass in the cli example as a flag --rootdir) or by default finding the first .git directory in the folder and parent folders. We also will need to be able to load packed starlark libraries (to be defined)"
**Notes:** Both syntaxes; absolute resolves to a root which is set via `--rootdir` flag or auto-discovered as the first `.git` ancestor. Packed Starlark libraries (distributable bundles) are deferred — design must not preclude them.

### Sandbox

| Option | Description | Selected |
|--------|-------------|----------|
| Single root passed to NewParser | Clean model, no traversal escape (Recommended) | ✓ |
| Multiple search paths | Like Python's sys.path — flexible but ambiguous | |
| No sandbox | File system free-for-all | |

**User's choice:** Single root passed to NewParser
**Notes:** Sandbox prevents `../../etc/passwd`-style traversal.

### Multi-flow

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — multiple flow() calls per file | A file is a module (Recommended) | ✓ |
| One flow per file | File name = flow name | |
| Either — author's choice | Two paths to maintain | |

**User's choice:** Yes — multiple flow() calls per file
**Notes:** Parser collects flows across all loaded files into `map[string]*dag.Flow`. Duplicate names across the parser session are a parse error.

### call_flow

| Option | Description | Selected |
|--------|-------------|----------|
| By flow name in same parser session | Simple, names must be unique (Recommended) | ✓ |
| By explicit module path | Verbose, supports same name in different modules | |
| Resolved at runtime, late-bound | Conflicts with static-validation goal | |

**User's choice:** By flow name in same parser session
**Notes:** Cross-flow resolution is a final pass after parsing; unknown flow names error at parse time.

---

## Lambda Capture Format

### Lambda ID

| Option | Description | Selected |
|--------|-------------|----------|
| Hash of file content + position | sha256(fileBytes)[:8] + ":" + line + ":" + col (Recommended) | ✓ |
| Sequential counter per parse | Unstable across re-parse | |
| Hash of source text only | Identical lambdas in different positions collide | |
| User-provided + auto fallback | Most flexible, slight authoring burden | |

**User's choice:** Hash of file content + position
**Notes:** Stable across re-parse of same file content. Phase 3's serialization decision must work with this format.

### Free vars

| Option | Description | Selected |
|--------|-------------|----------|
| Allow frozen module-level constants only | Useful and safe (Recommended) | ✓ |
| Reject any free variable but ctx | Strictest — loses module constant convenience | |
| Allow all, warn on mutable | Most permissive, subtle replay bugs | |

**User's choice:** Allow frozen module-level constants only
**Notes:** Parser inspects free vars at parse time; mutable closures rejected with line/col. Frozen module-level constants and frozen functions OK.

### Globals

| Option | Description | Selected |
|--------|-------------|----------|
| Strict subset | Lock list in Phase 1 (Recommended) | |
| Strict subset + fail() | Plus fail("reason") for clean short-circuit | ✓ |
| Permissive — most safe builtins | Wider net, harder to audit | |

**User's choice:** Strict subset + fail()
**Notes:** Lambda-time globals: len, str, int, float, bool, list, dict, tuple, fail, comparison/arithmetic, struct attr access, frozen-collection helpers (enumerate/zip/range/sorted/reversed/min/max/sum/any/all/abs). NO time/random/I/O/os/getattr-with-dynamic. Locked in Phase 1; expansion requires explicit decision.

### Print

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — routed to workflow logger | thread.Print → workflow.GetLogger.Info (Recommended) | ✓ |
| No — silent | Forces script() output for visibility | |
| Yes — routed but auto-scrubbed | Belt-and-suspenders against logging tokens | |

**User's choice:** Yes — routed to workflow logger
**Notes:** Consultants are responsible for not logging secrets; this is a documented contract. Credential-scrubbing middleware does NOT run on print payloads in v1.

---

## Claude's Discretion

- Exact Starlark builtin names that pass through to lambda-time (D-20 lists the principles; Claude picks the exact set during planning).
- Internal layout of the registration mechanism for `Idempotent` (struct field vs. method).
- Sequence of parser passes (parse → lambda capture → flow registration → cross-flow resolution → lint).
- Test fixture directory structure under `tests/fixtures/`.
- `Node` interface vs. tagged-union (Kind field) — pick whichever produces the cleanest interpreter switch in Phase 3.
- Specific reflection helper for `star:"..."` tags.

## Deferred Ideas

- **Packed Starlark libraries** — distributable bundles of `.star` files loadable by module name. Not in v1.
- **Hot-reload of `.star` files** — already in PROJECT.md "Out of Scope".
- **Schema export to JSON Schema/markdown** — listed as v2.
- **Tier-2 unit tests for `def` blocks** — listed as v2.
