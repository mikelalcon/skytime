# Phase 6: Example Project (HTTP + GitHub + Webhook) - Context

**Gathered:** 2026-05-06
**Status:** Ready for planning

<domain>
## Phase Boundary

Ship `examples/http-github-webhook/` as the dogfooding vehicle and proof-of-life
for the Skytime stack. Concretely:

- **Three real extensions:** HTTP (already shipped in Phase 4 — `pkg/extension/builtin/http/`), GitHub (new, in this phase), and Webhook (new, in this phase). GitHub and Webhook live under `examples/http-github-webhook/extensions/{github,webhook}/` and are registered alongside HTTP in `examples/http-github-webhook/cmd/extbin/main.go`.
- **One library-level credential resolver:** `pkg/extension/credfile/` — TOML-based, default path `$HOME/.skytime-credentials`, configurable via constructor option. Reusable by any consultant building their own CLI binary; not example-only.
- **Five `.star` flows + one `*_test.star`:** `public_repo_check.star` (README demo, no auth), `pr_to_webhook.star`, `issue_triage.star`, `batch_label_issues.star`, `weekly_digest.star`, plus `issue_triage_test.star` (Tier-3 test exercising attempt-aware retries + replay determinism).
- **README walkthrough:** `git clone` → `skytime dev-server` (in another terminal) → build the example custom binary → run `public_repo_check.star` against the public GitHub API. ≤5 commands, no credentials needed for the headline demo.
- **CI pipeline:** `.github/workflows/ci.yml` runs `go vet`, `go test -race ./... -count=1`, builds the example custom binary, and runs `<custom-binary> test ./examples/http-github-webhook/` on every push to any branch.

We will clarify HOW to implement what's scoped — not WHETHER to add capabilities. New extensions beyond HTTP/GitHub/Webhook, hosted-mode features, additional DSL primitives, etc. all belong in other phases.

</domain>

<decisions>
## Implementation Decisions

### Extension Architecture

- **D-EX-ARCH:** Hybrid layout — HTTP stays in `pkg/extension/builtin/http/` (already shipped Phase 4); GitHub + Webhook live in `examples/http-github-webhook/extensions/{github,webhook}/` (example-local). The example's custom CLI binary at `examples/http-github-webhook/cmd/extbin/main.go` registers all three. This signals to readers: HTTP is universal, domain-specific extensions are opt-in per binary — exactly the two-tier authoring model documented in `docs/cli-binary.md`.
- **D-EX-3RD:** The third extension is `webhook` — a generic POST-JSON-to-URL extension demonstrated against `webhook.site` for zero-setup non-idempotency proof. Slack was originally named in EX-01 but swapped during discuss-phase to remove account-creation friction. Webhook is broader-purpose (consultants can point it at Discord/Slack/Teams webhooks too). The example dir is `examples/http-github-webhook/`.
- **D-EX-BINARY:** Example's custom CLI binary path is `examples/http-github-webhook/cmd/extbin/main.go`. Imports `pkg/cli`, registers all three extensions via `cli.WithExtensions(...)`, and wires the new `credfile` resolver. Binary name: `extbin` (Claude's discretion — short, neutral; renaming is a planner task if you want something else).

### Credentials

- **D-CREDS-LIB:** File-based credential resolver lives in the **library** at `pkg/extension/credfile/` — sibling to the existing `pkg/extension/credential.go` (sealed Credential interface) and `pkg/extension/handler.go` (CredentialHandler contract). Reusable by any consultant building a custom binary. NOT example-only.
- **D-CREDS-PATH:** Default file path `$HOME/.skytime-credentials`. Configurable via constructor option (e.g., `credfile.New(credfile.WithPath("/etc/..."))`). Loaded once at construction; not reloaded on each Resolve call (consultants restart the worker to pick up new credentials in v1).
- **D-CREDS-FORMAT:** TOML with explicit type tag per credential. Schema:
  ```toml
  [credentials.github_token]
  type = "bearer"
  token = "ghp_..."

  [credentials.basic_id]
  type = "basic"
  username = "..."
  password = "..."

  [credentials.apikey_id]
  type = "apikey"
  key = "X-API-Key"
  value = "..."

  [credentials.webhook_url]
  type = "bearer"   # webhook.site URLs are bearer-style for signature simplicity
  token = "https://webhook.site/<your-token>"
  ```
  Maps directly to the sealed `Credential` interface (`BearerCredential`, `BasicCredential`, `APIKeyCredential`). Refuses to load if file mode is world-readable (warn-only by default; opt-in strict mode for production). TOML parser choice deferred to research (`BurntSushi/toml` vs `pelletier/go-toml/v2` — researcher to pick based on dep tree, maintenance status, and existing transitive deps).
- **D-CREDS-DEMO:** The README walkthrough demo flow uses GitHub's public unauthenticated API (`/repos/{owner}/{repo}`) — no credfile setup needed for the headline demo. Authenticated flows (`pr_to_webhook`, `issue_triage`, `batch_label_issues`, `weekly_digest`) require setting up `~/.skytime-credentials` and are documented as a separate "second-stage walkthrough" in the README.

### Flow Inventory

- **D-FLOWS-LINEUP:** Five flows + one test fixture, in this order in the README:
  1. `public_repo_check.star` — README walkthrough demo. Inputs: `repo`. Steps: sequential `step` (gh.get) + `script` (extract popularity) + `if_cond` on stars threshold + `block` of follow-up gh.gets in the popular branch. Coverage: sequential + block + if_cond + script. No credentials.
  2. `pr_to_webhook.star` — Authenticated flow. Inputs: `owner`, `repo`. Steps: gh.list_prs (authenticated) → `for_each_parallel` posting each PR's title to the webhook → `script` summarizing. Coverage: sequential + for_each_parallel + script. Credentials, retries.
  3. `issue_triage.star` — Deepest flow. Outer: gh.list_open_issues → `for_each_parallel(call_flow(triage_issue))`. Inner `triage_issue`: gh.get_issue → `script` classifying → `if_cond`(if old → gh.add_comment block; else → no-op). Coverage: call_flow + for_each_parallel + script + if_cond + sequential + block. Credentials, retries, timeouts. **This flow's `for_each_parallel` cancellation semantics also satisfy EX-02's cancellation concern** — when a sibling action raises non-retryable, the standard errgroup-style cancel-siblings fires.
  4. `batch_label_issues.star` — Block-batch demo. Inputs: `owner`, `repo`, `label`. Steps: gh.list_issues → `script` filtering unlabeled → `block_fn` building N gh.add_label calls (idempotent → batched into one activity invocation). Coverage: block batch (idempotent) + script. Credentials, timeouts. Contrasts with webhook.post non-idempotency in pr_to_webhook + weekly_digest.
  5. `weekly_digest.star` — Aggregation demo. Inputs: `owner`, `repo`. Steps: gh.list_recent_merged_prs (last 7 days) → `script` grouping by author → `for_each_parallel` building per-author summary blocks → final `step` posting consolidated digest to webhook. Coverage: sequential + for_each_parallel + script. Credentials, retries.

  Plus: `issue_triage_test.star` — Tier-3 test for `issue_triage`. Mocks gh.list_open_issues, gh.get_issue, gh.add_comment via `tester.mock_action`. Tests: (a) happy path — every issue triaged; (b) retry path — gh.get_issue returns `err()` on attempt=1, succeeds on attempt=2 (exercises TEST-03); (c) cancellation path — one call_flow fails non-retryable, siblings cancelled (incidental cancellation coverage). Replay-determinism asserted by tester.run's always-on replay-twice (TEST-04).

- **D-FLOWS-COVERAGE-MATRIX:** README contains a coverage matrix:

  | Flow | seq | block | if_cond | script | for_each_par | call_flow | retries | timeouts | credentials | cancellation |
  |------|-----|-------|---------|--------|--------------|-----------|---------|----------|-------------|--------------|
  | public_repo_check | ✓ | ✓ | ✓ | ✓ |  |  |  |  |  |  |
  | pr_to_webhook | ✓ |  |  | ✓ | ✓ |  | ✓ |  | ✓ |  |
  | issue_triage | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ (incidental) |
  | batch_label_issues | ✓ | ✓ |  | ✓ |  |  | ✓ | ✓ | ✓ |  |
  | weekly_digest | ✓ |  |  | ✓ | ✓ |  | ✓ |  | ✓ |  |

  All 6 primitives + all 4 concerns covered. Pinned in the README so success criterion 2 is auditable.

- **D-FLOWS-STYLE:** Examples must be highly readable. Self-evident code over heavy comment blocks. Comments only where the WHY isn't obvious from the names. README walkthrough: clear prose, minimal jargon, no vendor-decision-laden language. The skeleton/ flows already establish the comment density target — match or beat them.

### Webhook Extension

- **D-WEBHOOK-OPS:** Two operations: `webhook.post(url, body, headers)` (non-idempotent — creates a new entry on the receiver each call) and `webhook.put(url, body, headers)` (also non-idempotent — webhook receivers don't honor RFC-7231 PUT semantics, and consultants typically map webhooks 1:1 to chat-post semantics regardless of HTTP verb). v1 ships `post` only; `put` is a v2 polish item if real consultants ask. **The non-idempotency declaration is the load-bearing piece** — success criterion 3 explicitly requires verification that `webhook.post` inside a `block` executes one-action-per-activity-invocation.
- **D-WEBHOOK-HOST:** The demo URL is `https://webhook.site/<your-token>` (visit webhook.site in browser → unique URL appears, no account). Reader pastes the URL into the credfile under id `webhook_url` (treated as a bearer credential by the resolver — the URL is the secret). The README walkthrough explains this in the "second-stage" section.

### CI

- **D-CI-FILE:** `.github/workflows/ci.yml` (single file, single job).
- **D-CI-TRIGGER:** `push: { branches: ['**'] }` — every branch, no PR triggers (per user explicit preference).
- **D-CI-RUNNER:** `ubuntu-latest`. Linux only for v1; macOS/Windows matrix is a v2 polish item.
- **D-CI-GO:** Go 1.25 via `actions/setup-go@v5` (stack pin from CLAUDE.md). Module cache enabled.
- **D-CI-STEPS:** Sequential, fail-fast:
  1. `go vet ./...`
  2. `go test -race ./... -count=1`
  3. Build example custom binary: `go build -o /tmp/extbin ./examples/http-github-webhook/cmd/extbin`
  4. Tier-3 .star tests: `/tmp/extbin test ./examples/http-github-webhook/` (hermetic — extensions mocked via `tester.*`)
  5. EX-04 walkthrough smoke: build + run `public_repo_check.star` against public GitHub API. Asserts exit code 0 and expected stdout substring (e.g., `"flow_complete"`).
- **D-CI-AUTH-FLOWS:** Authenticated GitHub flows + webhook flows are NOT exercised in CI. They require secrets (PAT, webhook URL); requiring those for green-on-push contradicts the no-PR-trigger / no-secret-config-required preference. The Tier-3 mocks fully cover them logically.
- **D-CI-WALL:** Target ~3-5 minutes wall-clock. Set `timeout-minutes: 15` as the safety cap.

### Documentation

- **D-DOCS-README:** `examples/http-github-webhook/README.md` — repo-relative front door for the example. Sections:
  1. What this example shows (one paragraph)
  2. Quick start (the ≤5 command walkthrough, public-API-only)
  3. Coverage matrix (table from D-FLOWS-COVERAGE-MATRIX)
  4. Authenticated walkthrough (second-stage; covers credfile setup, webhook.site URL, GitHub PAT)
  5. Flow-by-flow tour (one paragraph per flow + a "what it demonstrates" bullet)
  6. Running the tests (`./extbin test ./examples/http-github-webhook/`)
  7. Building your own custom binary (forward-link to `docs/cli-binary.md`)

- **D-DOCS-MAIN-README:** Main repo `README.md` gets a single-line addition under "Where to Go Next" linking to `examples/http-github-webhook/README.md` as "the rich example project — three extensions, five flows, full coverage".

- **D-DOCS-CRED-RESOLVER:** `pkg/extension/credfile/` is documented in `docs/for-extension-developers/README.md` (it's a tool consultants USE when building binaries; lives in their reading section). A short reference section: schema, file path, security note (file mode), constructor options.

### Coexistence

- **D-COEXIST:** Keep `examples/skeleton/` alongside `examples/http-github-webhook/`. skeleton/ is the minimal demo (3 small flows + 1 test fixture, already integrated into Phase 04.3 docs as the canonical tutorial via `docs/getting-started.md`); http-github-webhook/ is the rich proof-of-life. They serve different audiences. Don't migrate or delete skeleton/.

### Roadmap Evolution

The decisions above include a **roadmap edit** (Slack → Webhook). ROADMAP.md, REQUIREMENTS.md, and PROJECT.md have been updated in this same commit:
- Phase title: "Example Project (HTTP + GitHub + Slack)" → "Example Project (HTTP + GitHub + Webhook)"
- Example dir slug: `http-github-slack` → `http-github-webhook`
- EX-01 wording: Slack → Webhook + clarification that it's a generic POST-JSON-to-URL extension demonstrated against webhook.site
- Success criterion 3: `Slack chat.postMessage` → `webhook.post`
- New success criterion 5: CI pipeline (.github/workflows/ci.yml)
- ROADMAP intro: "HTTP + GitHub + Slack" → "HTTP + GitHub + Webhook"
- PROJECT.md Active item: same swap + credfile + CI mention

### Claude's Discretion

These were not pinned during the discuss-phase — researcher and planner have flexibility:

- TOML parser library choice (`BurntSushi/toml` vs `pelletier/go-toml/v2`) — researcher picks during phase research based on maintenance + dep tree.
- Internal package layout under `examples/http-github-webhook/extensions/{github,webhook}/` — file naming, helper organization, test layout. Match the existing `pkg/extension/builtin/http/` shape.
- Exact GitHub op set in the new extension. Required by flows: `list_open_issues`, `get_issue`, `add_comment`, `add_label`, `list_prs`, `list_recent_merged_prs`, `get_repo`. Researcher to confirm GitHub REST API shapes and idempotence per op (most are GET = idempotent; `add_comment` and `add_label` are non-idempotent).
- README primitive-coverage matrix format (markdown table, ASCII art, both?). Match the existing `docs/for-flow-authors/README.md` table style.
- CI smoke-test exit-condition assertions (whether to use string match, JSON parse, or just exit code). Planner to design.
- Whether the CI yaml uses a separate `Test` job vs a single `CI` job. D-CI-FILE locks single file; planner can split jobs internally if it improves cache hits.
- File-mode strict mode for the credfile resolver (default warn-only; opt-in strict). Researcher decides if the warn message goes to stderr, slog, or both.

### Folded Todos

None — `gsd-tools todo match-phase 6` returned 0 matches.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase Scope + Requirements
- `.planning/ROADMAP.md` §"Phase 6: Example Project (HTTP + GitHub + Webhook)" — phase goal, dependencies, requirements list, success criteria (1-5)
- `.planning/REQUIREMENTS.md` §"Example Project (Dogfood + Demo)" — EX-01 (now Webhook-flavored), EX-02, EX-03, EX-04
- `.planning/PROJECT.md` §"Active" — line referring to "HTTP + GitHub + Webhook example project + credfile resolver + CI workflow"

### Existing Code to Mirror
- `pkg/extension/builtin/http/http.go` — extension implementation template; new `github` and `webhook` extensions follow the same shape (Name, Initialize, Operations, factory under `*starlarkstruct.Module`)
- `pkg/extension/builtin/http/http_test.go` — extension test layout template
- `pkg/extension/credential.go` — sealed Credential interface (BearerCredential / BasicCredential / APIKeyCredential); credfile MUST produce these types verbatim
- `pkg/extension/handler.go` — CredentialHandler contract (`Resolve(ctx, id) (Credential, error)`); credfile implements this
- `pkg/extension/extension.go` — Extension interface (`Name()`, `Initialize()`, `Operations()`)
- `pkg/extension/operation.go` — `OperationSpec` shape including `Idempotent *bool` per-op declaration
- `cmd/skytime/main.go` — thin CLI binary wiring; the example's `cmd/extbin/main.go` is a parallel implementation that registers GitHub + Webhook + uses credfile

### Two-Tier Authoring + Build-Your-Own-Binary
- `docs/cli-binary.md` — the documented "build your own binary" pattern; the example's `cmd/extbin/main.go` IS the canonical implementation of this pattern
- `pkg/cli/options.go` — `WithExtensions`, `WithCredentialHandler` functional options
- `pkg/cli/root.go` — `NewRootCommand(opts ...Option)` entry point

### Test Harness (for EX-03)
- `docs/for-flow-authors/testing.md` — Tier-3 reference manual (every `tester.*` builtin, mock_lambda env, MockRegistry precedence ladder, replay-determinism semantics)
- `docs/for-flow-authors/testing-tutorial.md` — step-by-step tutorial; `issue_triage_test.star` follows this style and structure
- `pkg/testing/router.go` — mock router; mock kwarg keyword is `msg=` (not `message=`); extension is the **registered** name not the local variable
- `pkg/testing/runner.go::parseTestFile` — single-file scope for `*_test.star`; flow MUST be declared inline (no `load()` across files in v1)
- `pkg/worker/boot.go::bootRegistry` — production worker skips `*_test.star` (Phase 5 latent gap fix); ensures example-side test files don't bleed into production `skytime run` paths
- `examples/skeleton/simple_check_test.star` — existing `*_test.star` template (file-scope mock + per-test override + retry semantics)

### CI / Workflow Reference
- `.github/workflows/` (currently empty — this phase creates the directory)
- `Makefile` (none yet — see if Phase 6 adds one or stays Makefile-less)
- `CLAUDE.md` "Technology Stack" section — Go 1.25 floor, Temporal SDK v1.42.0, golangci-lint v2.11.4 (Phase 6 may or may not add lint to CI)

### Public APIs (research targets for the planner)
- GitHub REST API v3 (`api.github.com`) — endpoints used by the flows: GET `/repos/{owner}/{repo}`, GET `/repos/{o}/{r}/issues`, GET `/repos/{o}/{r}/issues/{n}`, POST `/repos/{o}/{r}/issues/{n}/comments`, POST `/repos/{o}/{r}/issues/{n}/labels`, GET `/repos/{o}/{r}/pulls`, GET `/repos/{o}/{r}/pulls?state=closed&sort=updated`. Idempotence: all GETs are idempotent; comment + label POSTs are non-idempotent (each call creates a duplicate).
- webhook.site — anonymous webhook catcher; POST to a unique URL appears in the browser dashboard. No API surface to research; just used for the demo.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`pkg/extension/builtin/http/`** — full extension implementation template. ~110 LOC core + tests. New `github` and `webhook` extensions copy the shape: `Name() string`, `Initialize(*starlark.Thread) starlark.Value` returning `*starlarkstruct.Module`, `Operations() []OperationSpec`. Per-op `Idempotent *bool`.
- **`pkg/extension/credential.go::BearerCredential`/`BasicCredential`/`APIKeyCredential`** — sealed Credential types via unexported `isCredential()` method. credfile produces these directly.
- **`pkg/cli/options.go::WithExtensions(...)`/`WithCredentialHandler(...)`** — functional options used by `cmd/extbin/main.go`.
- **`docs/cli-binary.md`** — documented narrative for custom binaries; the example's cmd/extbin is the canonical implementation of this narrative. Forward-link from the new README back to docs/cli-binary.md keeps the two in lockstep.
- **`tests/skytime_test_e2e_test.go`** — existing subprocess-E2E pattern. CI's EX-04 walkthrough smoke can mirror this style (exec.Command against a freshly-built binary, assert exit code + stdout substring).
- **`examples/skeleton/simple_check_test.star`** — existing `*_test.star` template; `issue_triage_test.star` follows this shape.
- **`pkg/testing/cli_run.go::RunCLI(dir, opts...) (passed, failed int, err error)`** — non-`*testing.T` entry-point used by `pkg/cli/test.go::newTestCommand`. The example's cmd/extbin uses the same `cli.NewRootCommand` and inherits the `test` subcommand for free.

### Established Patterns
- **JSON-everywhere wire format** — Phase 2 + Phase 3 data flows. credfile breaks the pattern by using TOML (deliberately — TOML is for hand-edited config, JSON is for wire data).
- **Sealed interfaces via unexported method** — Credential, MockResult, ActionResult, Node. credfile-loaded credentials use this seal.
- **Functional Options pattern** — `parser.WithTestMode()`, `cli.WithExtensions(...)`, `pkg/testing.WithRunFilter(...)`. `credfile.New(credfile.WithPath(...))` follows the same shape.
- **Firewall AST tests** — `tests/firewall_*_test.go`. The new GitHub + Webhook extensions live OUTSIDE `pkg/`, so the existing library-root firewall isn't affected (extensions in examples/ can import anything they want). No new firewall test needed.
- **Mock router keyed on extension *name*** — Documented Phase 5 pitfall. README's Tier-3 walkthrough must call out: `tester.mock_action(extension="github", op="get_issue", ...)` — `extension` is the registered name, NOT the local variable from `gh = github.client(...)`.

### Integration Points
- **`pkg/cli.NewRootCommand(opts...)`** — example's cmd/extbin entry point.
- **`pkg/extension/handler.go::CredentialHandler` contract** — credfile implements this.
- **`pkg/parser/options.go::WithExtensions(...)`** — parser registers the example's GitHub + Webhook extensions when the example binary parses a `.star` file.
- **`pkg/worker/boot.go::bootRegistry`** — already skips `*_test.star` (Phase 5 fix); the example's `skytime run` paths don't see test fixtures.
- **`.github/workflows/`** — new directory (created this phase). Single `ci.yml` file inside.

</code_context>

<specifics>
## Specific Ideas

- **webhook.site as the demo URL.** Zero-account-creation, browser-visible feedback, unique per-reader. The reader visits webhook.site, copies the URL, pastes it into `~/.skytime-credentials` under id `webhook_url`. Real I/O proves non-idempotency (each `webhook.post` creates a new entry visible in the browser dashboard).
- **`public_repo_check.star` as the headline demo.** Self-contained — no credfile, no webhook URL, just the public GitHub API for `octocat/Hello-World`. Reader sees colored progress + final result without any setup beyond `git clone` and building the binary.
- **TOML credfile, not JSON.** TOML is intentional: credfiles are hand-edited dev artifacts, comments are useful, multiline values feel natural. Adds a TOML parser dep but the alternative (JSON-everywhere consistency) is wrong-tradeoff for this specific file.
- **Examples must be readable; comments must be sparing.** D-FLOWS-STYLE pins this. The skeleton/ flows establish the target density. Don't over-comment.

</specifics>

<deferred>
## Deferred Ideas

- **`webhook.put` operation** — v2 polish. v1 ships `webhook.post` only; consultants asking for PUT can extend the example's webhook extension themselves.
- **macOS/Windows matrix in CI** — v2 polish item per D-CI-RUNNER. Phase 04.1 has Windows build-tag stubs that would benefit from CI exercise; Linux-only is the v1 cap.
- **Strict mode for credfile file mode** — D-CREDS-FORMAT documents file-mode-not-0600 → warn-only as default. Strict mode (refuse to load on world-readable file) is opt-in via `credfile.WithStrictMode()` constructor option; researcher decides exact wording.
- **`golangci-lint` in CI** — Phase 6's CI runs `go vet` only (not `golangci-lint`). Adding lint is a v2 polish item; the existing codebase has not been linted with golangci-lint up to now (Phase 1-5 used go vet + gofumpt locally), and turning it on in CI would surface a wave of style warnings that aren't related to Phase 6.
- **Vault/1Password/cloud credential resolvers** — `pkg/extension/credfile/` is the v1 resolver. Vault + cloud-native (AWS Secrets Manager, GCP Secret Manager) resolvers are v2; D-CREDS-LIB locks the file path in the same package family for those follow-ups.
- **Cross-file `load()` in `*_test.star`** — pre-existing v2 deferral from Phase 5; would let the example's `issue_triage_test.star` `load()` the production `issue_triage.star` instead of redeclaring it inline. Phase 6 inherits this deferral as a known pain point — researcher MAY surface "this awkward duplication is a known v2 fix" in the README.
- **Slack as the third extension** — replaced by Webhook during this discuss-phase per user preference (no Slack workspace). If a real customer/consultant wants a Slack-flavored extension later, it's additive (their custom binary registers `slack` alongside the bundled HTTP and the example-style Webhook).
- **PR triggers for CI** — explicitly out of scope per user; push-only for v1. Adding PR-triggered runs is a one-line yaml addition for whenever the user wants it.

### Reviewed Todos (not folded)
None — todo match-phase returned 0 matches.

</deferred>

---

*Phase: 06-example-project-http-github-webhook*
*Context gathered: 2026-05-06*
