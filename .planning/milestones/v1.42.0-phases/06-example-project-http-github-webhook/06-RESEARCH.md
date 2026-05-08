# Phase 6: Example Project (HTTP + GitHub + Webhook) — Research

**Researched:** 2026-05-06
**Researcher:** gsd-researcher
**Domain:** Go example project (extension authoring + credfile resolver + Tier-3 .star tests + GitHub Actions CI)
**Confidence overall:** HIGH (all locked decisions are implementable against verified library/code surfaces)

---

## Research Summary

- **Every locked decision in CONTEXT.md is implementable as written against the existing v1 surfaces** — `pkg/extension/builtin/http/http.go` is a complete copy-paste template for the new `github` and `webhook` extensions; `pkg/cli.NewRootCommand(WithExtensions, WithCredentialHandler)` already accepts both options; `extension.OperationSpec.Idempotent *bool` already enforces `webhook.post` non-idempotency at the registry boundary; and Phase 2's `pkg/activity/execute_batch.go` already routes non-idempotent ops one-per-activity-invocation (success criterion 3 needs an *assertion test*, not new enforcement code).
- **TOML parser pick: `github.com/pelletier/go-toml/v2 v2.3.1` (2026-04-16).** Active maintenance, faster than `BurntSushi/toml` for typical config sizes, no extra transitive deps. Both are MIT and broadly adopted; pelletier wins on recency. (See `## Implementation Recommendations § Credfile`.)
- **GitHub library pick: `github.com/google/go-github/v78 v78.0.0` (2025-11-08).** Compatible with Go 1.25 + Temporal SDK v1.42 (no protobuf collision — go-github depends only on `google/go-querystring` + stdlib `net/http`). All seven REST endpoints needed by the locked flows (list issues, get issue, add comment, add label, list PRs, list recently-merged PRs, get repo) are exposed as typed methods.
- **Webhook receiver: NOT needed in v1.** The locked flow lineup ONLY *posts* to webhook URLs (`webhook.post(url, body, headers)`); it does not receive webhooks. The "webhook extension" is a generic outbound-POST extension demonstrated against `webhook.site` (a hosted catcher). No `net/http.Server` lives anywhere in this phase. This eliminates the entire webhook-server-lifecycle pitfall before it can land.
- **CI wires Temporal CLI via official `temporalio/setup-temporal@v0`.** EX-04 walkthrough smoke uses `temporal server start-dev --headless &` (background dev server) → `extbin run public_repo_check.star` against the public GitHub API (zero auth needed). All authenticated flows + webhook flows are covered by Tier-3 mocks via `extbin test ./examples/http-github-webhook/` — no secrets in CI.
- **`.star` test patterns are well-documented and proven.** `examples/skeleton/simple_check_test.star` is the working template; `issue_triage_test.star` follows it with deeper mock-fn patterns (attempt-aware retries, file-scope vs per-test mocks, `assert.*` from starlarktest). The `tester.workflow / mock_action / run` API is locked in `pkg/testing/module.go` and documented end-to-end in `docs/for-flow-authors/testing.md`.

**Primary recommendation:** This phase is execution-heavy, not research-heavy. The architecture, API surfaces, and patterns are all already shipped. Plans should (1) copy `pkg/extension/builtin/http/` shape into `examples/http-github-webhook/extensions/{github,webhook}/`; (2) implement `pkg/extension/credfile/` against the locked TOML schema; (3) write the five flows + one test; (4) build `cmd/extbin` as a 30-line wiring of `cli.NewRootCommand(WithExtensions(...), WithCredentialHandler(credfile.New(...)))`; (5) ship `.github/workflows/ci.yml` per the locked D-CI-* sequence.

---

## Locked Decisions Acknowledged

> Source: `.planning/phases/06-example-project-http-github-webhook/06-CONTEXT.md`. Researcher does NOT re-litigate these; the recommendations below operationalize them.

### Architecture (D-EX-*)
- **D-EX-ARCH:** Hybrid layout — HTTP stays in `pkg/extension/builtin/http/`; GitHub + Webhook live in `examples/http-github-webhook/extensions/{github,webhook}/`; example custom binary at `examples/http-github-webhook/cmd/extbin/main.go`.
- **D-EX-3RD:** Third extension is `webhook` (generic outbound POST), demonstrated against webhook.site.
- **D-EX-BINARY:** Binary name `extbin`. Wires HTTP + GitHub + Webhook + credfile resolver via `pkg/cli`.

### Credentials (D-CREDS-*)
- **D-CREDS-LIB:** Library credential resolver at `pkg/extension/credfile/` (sibling to existing `pkg/extension/credential.go` + `handler.go`).
- **D-CREDS-PATH:** Default `$HOME/.skytime-credentials`; override via `credfile.WithPath(...)` constructor option.
- **D-CREDS-FORMAT:** TOML, explicit `type` tag per credential. Schema verbatim:
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
  type = "bearer"
  token = "https://webhook.site/<your-token>"
  ```
  Maps to existing sealed `extension.{Bearer,Basic,APIKey}Credential`. World-readable file → warn (default) or refuse (opt-in strict).
- **D-CREDS-DEMO:** README headline demo (`public_repo_check.star`) needs ZERO credentials — uses GitHub's public unauthenticated API. Authenticated walkthrough is "second-stage".

### Flows (D-FLOWS-*)
- **D-FLOWS-LINEUP:** Five flows + one test fixture, ordered: `public_repo_check`, `pr_to_webhook`, `issue_triage`, `batch_label_issues`, `weekly_digest` + `issue_triage_test`.
- **D-FLOWS-COVERAGE-MATRIX:** Matrix in README is auditable per-row (which primitive + concern each flow demonstrates).
- **D-FLOWS-STYLE:** Examples must be highly readable; comments only where WHY isn't obvious from names.

### Webhook (D-WEBHOOK-*)
- **D-WEBHOOK-OPS:** v1 ships `webhook.post(url, body, headers)` ONLY. (`put` deferred.) Non-idempotent.
- **D-WEBHOOK-HOST:** Demo URL is `https://webhook.site/<your-token>` — paste into credfile under id `webhook_url`.

### CI (D-CI-*)
- **D-CI-FILE:** Single `.github/workflows/ci.yml`.
- **D-CI-TRIGGER:** `push: branches: ['**']` — every branch, no PR triggers.
- **D-CI-RUNNER:** `ubuntu-latest`, Linux-only.
- **D-CI-GO:** Go 1.25 via `actions/setup-go@v5`. Module cache enabled.
- **D-CI-STEPS (sequential, fail-fast):** `go vet ./...` → `go test -race ./... -count=1` → `go build -o /tmp/extbin ./examples/http-github-webhook/cmd/extbin` → `/tmp/extbin test ./examples/http-github-webhook/` → EX-04 walkthrough smoke (public-API GitHub flow, exit code + stdout substring).
- **D-CI-AUTH-FLOWS:** Authenticated flows + webhook flows NOT exercised in CI (no secrets). Tier-3 mocks cover them logically.
- **D-CI-WALL:** Target ~3-5 min; `timeout-minutes: 15` safety cap.

### Documentation (D-DOCS-*)
- **D-DOCS-README:** `examples/http-github-webhook/README.md` with 7 sections (intro, quick-start, coverage matrix, authenticated walkthrough, flow tour, running tests, build-your-own-binary link).
- **D-DOCS-MAIN-README:** Single-line addition to main README under "Where to Go Next".
- **D-DOCS-CRED-RESOLVER:** Short reference in `docs/for-extension-developers/README.md`.

### Coexistence + Roadmap
- **D-COEXIST:** Keep `examples/skeleton/` alongside the new `examples/http-github-webhook/`.
- ROADMAP / REQUIREMENTS / PROJECT.md already updated for Slack→Webhook swap (per CONTEXT.md "Roadmap Evolution").

### Claude's Discretion (locked CHOICES that researcher resolves below)
- TOML parser library — **PICKED: `github.com/pelletier/go-toml/v2 v2.3.1`**.
- GitHub op set — **PICKED: 7 endpoints via `google/go-github/v78`** (see Implementation § Extensions).
- File-mode strict mode wording — **PICKED: warn via `slog` at WARN level by default; refuse on `WithStrictMode()`** (see Implementation § Credfile).
- Internal layout under `examples/http-github-webhook/extensions/{github,webhook}/` — **PICKED: mirror `pkg/extension/builtin/http/`** (one `<ext>.go` + one `<ext>_test.go` + a `response.go` per extension).
- Coverage matrix format — markdown table per CONTEXT.md (already shown in D-FLOWS-COVERAGE-MATRIX).
- CI smoke-test exit-condition — **PICKED: exit code 0 + grep for `flow_complete` substring on stdout** (renderer's existing terminal output line; matches `pkg/cli/progress.go` output shape).

### Phase Requirements (mandatory)

| ID | Description | Research Support |
|----|-------------|------------------|
| EX-01 | Three extensions (HTTP/GitHub/Webhook) with per-op `Idempotent bool` | `OperationSpec.Idempotent *bool` already required at registration; HTTP shipped; GitHub + Webhook follow HTTP template (Implementation § Extensions) |
| EX-02 | 4–6 `.star` flows exercising every primitive + concern | Locked flow lineup covers all 6 primitives + all 4 concerns (verified against `D-FLOWS-COVERAGE-MATRIX`); flow patterns proven by `examples/skeleton/` |
| EX-03 | `.star` test using `temporal_test`, exercising retries via `attempt` and asserting replay determinism | `examples/skeleton/simple_check_test.star` is the working template; `issue_triage_test.star` extends with attempt-aware retries; replay-twice is ALWAYS-ON in `tester.run` (D5-D1, no opt-in needed) |
| EX-04 | README walkthrough: git-clone → executed flow in <5 commands | `public_repo_check.star` (no credentials) + `temporal server start-dev --headless` (background) + `go build` + `extbin run` = 4 commands (Implementation § README) |

### Deferred Ideas (OUT OF SCOPE)
- `webhook.put` operation
- macOS/Windows CI matrix
- Strict mode for credfile (opt-in only; warn-only is the default)
- `golangci-lint` in CI
- Vault / 1Password / cloud credential resolvers
- Cross-file `load()` in `*_test.star`
- Slack-flavored extension
- PR triggers for CI

---

## Project Constraints (from CLAUDE.md)

These directives have the same authority as locked decisions. Research recommendations MUST NOT contradict them.

- **Tech stack: Go + Starlark + Temporal — fixed.** No alternative DSLs, expression languages, or orchestrators.
- **Architecture: Strict parse/execute separation.** Webhook extension MUST NOT import `go.temporal.io/sdk/activity`; mock harness lives in `pkg/testing` (already does).
- **Determinism: parsed DAG must be deterministic.** GitHub op responses pass through `dag.OperationOutput` and Temporal's data converter — no map iteration order, no time.Now(), no rand inside any walker callback. (Verified: existing `pkg/extension/builtin/http/http.go` does not reach for time/rand inside `doHTTP`.)
- **Security: credentials never enter workflow state.** `pkg/extension/credfile/` MUST resolve at activity time only — its `Resolve(ctx, id)` is called from `pkg/activity/execute_batch.go::actionExecutor` (existing path). The CredentialID is what flows in workflow state; the *resolved* `Credential` only exists in the activity's stack frame for the duration of one operation.
- **Compatibility: Cloud + self-hosted both work.** This phase doesn't touch the client-construction path; the example's `extbin` inherits the existing `pkg/cli/connect.go` variant routing for free.
- **Stack lockdown:** Go 1.25, go.starlark.net latest pseudo-version, `go.temporal.io/sdk` v1.42.0, cobra v1.10.2, charm/log v2.0.0, koanf v2.3.4, slog interface, testify v1.11.1, golangci-lint v2.11.4. Phase 6 adds: `pelletier/go-toml/v2` and `google/go-github/v78` (both researched against Go module proxy below).

---

## Implementation Recommendations

### 1. Extensions

#### 1a. GitHub extension (`examples/http-github-webhook/extensions/github/`)

**Library pick: `github.com/google/go-github/v78 v78.0.0` (2025-11-08).**

Verified via `https://proxy.golang.org/github.com/google/go-github/v78/@latest`. v78 is the latest tagged major as of 2026-05-06 (v76: 2025-10-14, v77: 2025-11-04, v78: 2025-11-08 — three releases in ~3 weeks reflect normal monthly cadence). The library has only one direct external dep (`google/go-querystring`) plus stdlib; **no protobuf** anywhere → zero risk of collision with Temporal SDK v1.42's `google.golang.org/protobuf v1.36.11` transitive.

**Why go-github over raw `net/http`:**
1. Typed responses — `*github.Issue`, `*github.PullRequest` — eliminate JSON unmarshaling ceremony in op funcs.
2. Built-in pagination handlers (`ListOptions{PerPage, Page}`) for `list_open_issues` / `list_recent_merged_prs`.
3. Rate-limit awareness — surfaces `*github.RateLimitError` distinctly so the activity can classify it as retryable (`extension.ErrNonRetryable`-NOT-applied) vs configuration error.
4. Same `context.Context`-aware request shape as our existing HTTP extension — passes the activity-deadline ctx through naturally.

**Op set (7 ops, idempotence per GitHub REST semantics):**

| Op | Method | Endpoint | Idempotent | go-github call |
|----|--------|----------|------------|----------------|
| `get_repo` | GET | `/repos/{o}/{r}` | true | `client.Repositories.Get(ctx, owner, repo)` |
| `list_open_issues` | GET | `/repos/{o}/{r}/issues?state=open` | true | `client.Issues.ListByRepo(ctx, owner, repo, &github.IssueListByRepoOptions{State: "open"})` |
| `get_issue` | GET | `/repos/{o}/{r}/issues/{n}` | true | `client.Issues.Get(ctx, owner, repo, number)` |
| `add_comment` | POST | `/repos/{o}/{r}/issues/{n}/comments` | **false** | `client.Issues.CreateComment(ctx, owner, repo, n, &github.IssueComment{Body: ...})` |
| `add_label` | POST | `/repos/{o}/{r}/issues/{n}/labels` | **false** | `client.Issues.AddLabelsToIssue(ctx, owner, repo, n, []string{label})` |
| `list_prs` | GET | `/repos/{o}/{r}/pulls` | true | `client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{State: "open"})` |
| `list_recent_merged_prs` | GET | `/repos/{o}/{r}/pulls?state=closed&sort=updated` | true | `client.PullRequests.List(ctx, owner, repo, &github.PullRequestListOptions{State: "closed", Sort: "updated", Direction: "desc"})` + filter `*pr.MergedAt != nil && pr.MergedAt.After(time.Now().AddDate(0,0,-7))` |

> **Idempotence pitfall:** `add_comment` and `add_label` are *application-level* non-idempotent — calling either twice produces two distinct comments / two label rows. RFC-7231 says POST is non-idempotent at the protocol layer too, so the project's per-op declaration aligns with both. Pin verbatim in `TestExtension_OperationsIdempotenceMatchesEndpoints` (mirror `TestExtension_OperationsIdempotenceMatchesD4_14` from the HTTP extension at `pkg/extension/builtin/http/http_test.go`).

**Surface in `.star` (mirroring HTTP):**
```python
gh = github.client(credential = "github_token")    # factory returns sub-Module
step(action = gh.get_issue(owner = "octocat", repo = "Hello-World", number = 1))
step(action = gh.add_comment(owner = "octocat", repo = "Hello-World", number = 1, body = "thanks"))
```

**Implementation skeleton (matches `pkg/extension/builtin/http/http.go::endpointFactory` pattern):**
```go
// examples/http-github-webhook/extensions/github/github.go
package github

import (
    "context"
    "reflect"
    "time"

    "github.com/google/go-github/v78/github"
    "go.starlark.net/starlark"
    "go.starlark.net/starlarkstruct"

    "github.com/mikelalcon/skytime/pkg/dag"
    "github.com/mikelalcon/skytime/pkg/extension"
)

type skytimeGitHub struct{}

func New() extension.Extension { return skytimeGitHub{} }
func (skytimeGitHub) Name() string { return "github" }

// Initialize returns the `github` namespace value with one factory: client(credential=).
func (skytimeGitHub) Initialize(thread *starlark.Thread, kwargs []starlark.Tuple) (starlark.Value, error) {
    return &starlarkstruct.Module{
        Name: "github",
        Members: starlark.StringDict{
            "client": starlark.NewBuiltin("github.client", clientFactory),
        },
    }, nil
}

// Operations declares per-op specs with idempotence flags.
func (skytimeGitHub) Operations() map[string]*extension.OperationSpec {
    return map[string]*extension.OperationSpec{
        "get_repo":               {Name: "get_repo",               Idempotent: extension.Ptr(true),  Func: doGetRepo,             KwargsType: reflect.TypeOf(GetRepoArgs{}),         DefaultTimeout: 30 * time.Second},
        "get_issue":              {Name: "get_issue",              Idempotent: extension.Ptr(true),  Func: doGetIssue,            KwargsType: reflect.TypeOf(GetIssueArgs{}),        DefaultTimeout: 30 * time.Second},
        "list_open_issues":       {Name: "list_open_issues",       Idempotent: extension.Ptr(true),  Func: doListOpenIssues,      KwargsType: reflect.TypeOf(ListIssuesArgs{}),      DefaultTimeout: 30 * time.Second},
        "add_comment":            {Name: "add_comment",            Idempotent: extension.Ptr(false), Func: doAddComment,          KwargsType: reflect.TypeOf(AddCommentArgs{}),      DefaultTimeout: 30 * time.Second},
        "add_label":              {Name: "add_label",              Idempotent: extension.Ptr(false), Func: doAddLabel,            KwargsType: reflect.TypeOf(AddLabelArgs{}),        DefaultTimeout: 30 * time.Second},
        "list_prs":               {Name: "list_prs",               Idempotent: extension.Ptr(true),  Func: doListPRs,             KwargsType: reflect.TypeOf(ListPRsArgs{}),         DefaultTimeout: 30 * time.Second},
        "list_recent_merged_prs": {Name: "list_recent_merged_prs", Idempotent: extension.Ptr(true),  Func: doListRecentMergedPRs, KwargsType: reflect.TypeOf(ListPRsArgs{}),         DefaultTimeout: 30 * time.Second},
    }
}

// clientFactory(credential="github_token") → sub-Module exposing per-op builtins.
// Each builtin returns a *dag.ActionRef carrying CredentialID = credential.
func clientFactory(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
    var credential string
    if err := starlark.UnpackArgs("github.client", args, kwargs, "credential?", &credential); err != nil {
        return nil, err
    }
    return &starlarkstruct.Module{
        Name: "github.client",
        Members: starlark.StringDict{
            "get_repo":               newMethodBuiltin("github.get_repo", credential),
            "get_issue":              newMethodBuiltin("github.get_issue", credential),
            "list_open_issues":       newMethodBuiltin("github.list_open_issues", credential),
            "add_comment":            newMethodBuiltin("github.add_comment", credential),
            "add_label":              newMethodBuiltin("github.add_label", credential),
            "list_prs":               newMethodBuiltin("github.list_prs", credential),
            "list_recent_merged_prs": newMethodBuiltin("github.list_recent_merged_prs", credential),
        },
    }, nil
}
```

**Activity-side OperationFunc body (typical):**
```go
func doGetIssue(ctx context.Context, args any, cred extension.Credential) (dag.OperationOutput, error) {
    a := asGetIssueArgs(args)
    client := newClientForCredential(cred)
    issue, _, err := client.Issues.Get(ctx, a.Owner, a.Repo, a.Number)
    if err != nil {
        // go-github returns *github.RateLimitError → retryable (don't wrap with ErrNonRetryable);
        // 4xx not-found → wrap with extension.ErrNonRetryable so retries don't fire.
        var rate *github.RateLimitError
        if errors.As(err, &rate) {
            return nil, fmt.Errorf("github get_issue rate-limited: %w", err)
        }
        var resp *github.ErrorResponse
        if errors.As(err, &resp) && resp.Response.StatusCode >= 400 && resp.Response.StatusCode < 500 {
            return nil, fmt.Errorf("github get_issue %d: %s: %w", resp.Response.StatusCode, resp.Message, extension.ErrNonRetryable)
        }
        return nil, fmt.Errorf("github get_issue: %w", err)
    }
    return GitHubIssueOutput{
        Number: issue.GetNumber(),
        Title:  issue.GetTitle(),
        State:  issue.GetState(),
        Body:   issue.GetBody(),
    }, nil
}

// newClientForCredential returns a *github.Client wrapping a stdlib *http.Client
// with the credential's token applied via the standard "Authorization: Bearer ..."
// pattern. Nil credential → unauthenticated client (works for public APIs).
func newClientForCredential(cred extension.Credential) *github.Client {
    if cred == nil {
        return github.NewClient(nil)  // unauthenticated, public API only
    }
    bearer, ok := cred.(*extension.BearerCredential)
    if !ok {
        // GitHub API only takes bearer/PAT; reject other credential kinds gracefully.
        return github.NewClient(nil)  // falls through to public API; caller will see 401 → ErrNonRetryable
    }
    return github.NewClient(nil).WithAuthToken(bearer.Token.Reveal())
}
```

> **`WithAuthToken` API:** Verified in go-github v78 — `(*Client).WithAuthToken(token string) *Client` is the canonical PAT method (replaced the old `oauth2.NewClient` pattern). See `https://pkg.go.dev/github.com/google/go-github/v78/github#Client.WithAuthToken`.

**Output types** must implement `dag.OperationOutput` via the marker:
```go
type GitHubIssueOutput struct {
    Number int    `json:"number"`
    Title  string `json:"title"`
    State  string `json:"state"`
    Body   string `json:"body"`
}
func (GitHubIssueOutput) IsOperationOutput() {}  // marker — see pkg/dag/output.go
```

#### 1b. Webhook extension (`examples/http-github-webhook/extensions/webhook/`)

**No third-party library needed** — the webhook extension is "POST JSON to a URL". Uses stdlib `net/http` exactly as the existing HTTP extension does. Surface:

```python
wh = webhook.client(credential = "webhook_url")  # the URL itself is the secret
step(action = wh.post(body = '{"text": "hello"}'))
```

Note: `webhook.url` is bearer-style — the entire URL is the secret. The extension extracts the URL from `cred.(*extension.BearerCredential).Token.Reveal()` at activity time (mirrors how HTTP extension extracts the bearer token). The `post` op kwargs are `body` (string, optionally JSON) + `headers` (dict). No `url=` kwarg — URL comes from the credential.

**Op set (1 op in v1; `put` deferred per D-WEBHOOK-OPS):**

| Op | Idempotent | Notes |
|----|------------|-------|
| `post` | **false** | Each call appends a new entry to the receiver's queue/dashboard — this is the load-bearing non-idempotency demo for success criterion 3 |

**`Idempotent: extension.Ptr(false)` — VERIFY behavior:** Add a unit test mirroring `TestExtension_OperationsIdempotenceMatchesD4_14` and an integration test that confirms `step(block_fn=lambda ctx: [wh.post(body="a"), wh.post(body="b")])` runs **two activity invocations** (not one batched). The enforcement already exists in `pkg/activity/execute_batch.go` (D2-05 `mixedIdempotency` check + ACT-03 one-per-activity-invocation). The example should add an integration test that drives this code path with the new webhook extension to satisfy success criterion 3 explicitly.

#### 1c. HTTP extension — no changes

`pkg/extension/builtin/http/http.go` is shipped (Phase 4 plan 04-07). Example registers it via `cli.WithExtensions(skyhttp.New())` per the existing `cmd/skytime/main.go` pattern.

---

### 2. Credfile Package (`pkg/extension/credfile/`)

**TOML parser pick: `github.com/pelletier/go-toml/v2 v2.3.1` (2026-04-16, MIT).**

| Property | pelletier/go-toml/v2 | BurntSushi/toml |
|----------|----------------------|-----------------|
| Latest version | v2.3.1 (2026-04-16) | v1.6.0 (2025-12-18) |
| Stewardship | Active monthly releases | Active but slower |
| Performance | ~3× faster Decode for typical configs | Acceptable, slower on large docs |
| Transitive deps | None | None |
| API style | `toml.Unmarshal([]byte, &v)` (stdlib-mirroring) | `toml.Decode(string, &v)` |
| Position info on errors | Yes — `*toml.DecodeError` with line/col | Yes — `toml.ParseError` with line |
| MarshalTOML / UnmarshalTOML hooks | Yes | Yes |

Both are MIT, both work. **Pelletier wins on recency + API ergonomics + performance.** Recommendation: pick pelletier and document it in CONTEXT.md amendments under the discretion-resolved table.

**Verification (Go module proxy):**
- `https://proxy.golang.org/github.com/pelletier/go-toml/v2/@latest` → `v2.3.1`, `2026-04-16T23:29:02Z`.
- `https://proxy.golang.org/github.com/!burnt!sushi/toml/@latest` → `v1.6.0`, `2025-12-18T12:15:22Z`.

**Schema → Go struct (verbatim from D-CREDS-FORMAT):**
```go
// pkg/extension/credfile/file.go
package credfile

type fileShape struct {
    Credentials map[string]credentialEntry `toml:"credentials"`
}

type credentialEntry struct {
    Type     string `toml:"type"`               // "bearer" | "basic" | "apikey"
    Token    string `toml:"token,omitempty"`    // bearer
    Username string `toml:"username,omitempty"` // basic
    Password string `toml:"password,omitempty"` // basic
    Key      string `toml:"key,omitempty"`      // apikey: header NAME (e.g. "X-API-Key")
    Value    string `toml:"value,omitempty"`    // apikey: header VALUE (the secret)
}
```

> **Schema clarification:** D-CREDS-FORMAT shows the apikey shape as `key = "X-API-Key"` + `value = "..."`. This means `key` is the *header name* and `value` is the *header value (secret)*. Matches the existing `extension.APIKeyCredential{HeaderName, Key}` field meaning (where `Key` is `Secret`-typed and `HeaderName` is the plaintext header). Mapping: TOML `key`→Go `HeaderName`, TOML `value`→Go `Key` (the Secret).

**Resolver implementation (matches existing `extension.CredentialHandler` interface verbatim):**
```go
// pkg/extension/credfile/resolver.go
package credfile

import (
    "context"
    "fmt"
    "io/fs"
    "log/slog"
    "os"
    "path/filepath"
    "sync"

    "github.com/pelletier/go-toml/v2"

    "github.com/mikelalcon/skytime/pkg/extension"
)

// Resolver implements extension.CredentialHandler against a TOML file.
// Loaded ONCE at construction (D-CREDS-PATH locks "not reloaded on each Resolve");
// consultants restart the worker to pick up new credentials.
type Resolver struct {
    creds map[string]extension.Credential
    path  string
    log   *slog.Logger
}

// New constructs a Resolver. Defaults to $HOME/.skytime-credentials.
// File must exist; world-readable file warns (unless WithStrictMode, which refuses).
func New(opts ...Option) (*Resolver, error) {
    cfg := &config{
        path:   defaultPath(),
        logger: slog.Default(),
    }
    for _, opt := range opts {
        opt(cfg)
    }

    info, err := os.Stat(cfg.path)
    if err != nil {
        return nil, fmt.Errorf("credfile: stat %s: %w", cfg.path, err)
    }
    // File-mode check (D-CREDS-FORMAT). Mode().Perm() & 044 != 0 means group or other has read.
    if info.Mode().Perm()&0o044 != 0 {
        msg := fmt.Sprintf("credfile %s is world/group-readable (mode %o); chmod 600 to silence",
            cfg.path, info.Mode().Perm())
        if cfg.strict {
            return nil, fmt.Errorf("credfile: %s", msg)
        }
        cfg.logger.Warn(msg, "path", cfg.path, "mode", fmt.Sprintf("%o", info.Mode().Perm()))
    }

    bytes, err := os.ReadFile(cfg.path)
    if err != nil {
        return nil, fmt.Errorf("credfile: read %s: %w", cfg.path, err)
    }
    var raw fileShape
    if err := toml.Unmarshal(bytes, &raw); err != nil {
        return nil, fmt.Errorf("credfile: parse %s: %w", cfg.path, err)
    }
    creds, err := buildCredentials(raw)
    if err != nil {
        return nil, fmt.Errorf("credfile: %s: %w", cfg.path, err)
    }
    return &Resolver{creds: creds, path: cfg.path, log: cfg.logger}, nil
}

// Resolve implements extension.CredentialHandler.
// Unknown ID wraps extension.ErrUnknownCredential (D2-12) so the
// activity classifies as NonRetryable.
func (r *Resolver) Resolve(_ context.Context, id string) (extension.Credential, error) {
    cred, ok := r.creds[id]
    if !ok {
        return nil, fmt.Errorf("%w: %s (file=%s)", extension.ErrUnknownCredential, id, r.path)
    }
    return cred, nil
}

// Compile-time check.
var _ extension.CredentialHandler = (*Resolver)(nil)

// buildCredentials maps the raw TOML entries → sealed extension.Credential types.
func buildCredentials(raw fileShape) (map[string]extension.Credential, error) {
    out := make(map[string]extension.Credential, len(raw.Credentials))
    for id, e := range raw.Credentials {
        switch e.Type {
        case "bearer":
            if e.Token == "" {
                return nil, fmt.Errorf("credential %q (bearer): token is required", id)
            }
            out[id] = &extension.BearerCredential{ID_: id, Token: extension.NewSecret(e.Token)}
        case "basic":
            if e.Username == "" || e.Password == "" {
                return nil, fmt.Errorf("credential %q (basic): username and password are required", id)
            }
            out[id] = &extension.BasicCredential{ID_: id, User: e.Username, Password: extension.NewSecret(e.Password)}
        case "apikey":
            if e.Key == "" || e.Value == "" {
                return nil, fmt.Errorf("credential %q (apikey): key (header name) and value (secret) are required", id)
            }
            out[id] = &extension.APIKeyCredential{ID_: id, HeaderName: e.Key, Key: extension.NewSecret(e.Value)}
        case "":
            return nil, fmt.Errorf("credential %q: type is required (one of: bearer, basic, apikey)", id)
        default:
            return nil, fmt.Errorf("credential %q: unknown type %q (one of: bearer, basic, apikey)", id, e.Type)
        }
    }
    return out, nil
}

func defaultPath() string {
    home, err := os.UserHomeDir()
    if err != nil {
        return ".skytime-credentials"
    }
    return filepath.Join(home, ".skytime-credentials")
}
```

**Functional options (matches existing `pkg/cli/options.go` and `pkg/parser` patterns):**
```go
// pkg/extension/credfile/options.go
package credfile

import "log/slog"

type config struct {
    path   string
    strict bool
    logger *slog.Logger
}

type Option func(*config)

// WithPath overrides the default $HOME/.skytime-credentials.
func WithPath(p string) Option { return func(c *config) { c.path = p } }

// WithStrictMode refuses to load if the file is group/world-readable.
// Default is warn-via-slog only (D-CREDS-FORMAT warn-only default).
func WithStrictMode() Option { return func(c *config) { c.strict = true } }

// WithLogger overrides slog.Default() for the file-mode warning.
func WithLogger(l *slog.Logger) Option { return func(c *config) { c.logger = l } }
```

**API verification:** `extension.NewSecret(string) Secret` — VERIFY this constructor exists by reading `pkg/extension/secret.go`. If the constructor is named differently (e.g. `extension.SecretOf` or struct-literal `extension.Secret{Value: s}`), adjust the credfile Build calls accordingly. The existing test `pkg/extension/secret_test.go` will surface the actual constructor name.

**File-permission check:**
- `info.Mode().Perm() & 0o044 != 0` → group OR other has read bit set → emit warning.
- Default mode for the recommended sample file: `chmod 600 ~/.skytime-credentials`.
- Behavior on missing file: `os.Stat` returns `*fs.PathError`; surface as `credfile: stat ...: no such file or directory`. Caller (the example's `cmd/extbin/main.go`) handles by printing a friendly "first time? copy `.skytime-credentials.example` from the example dir" message — see § README.

**Test pattern:** Use `t.TempDir()` + `os.WriteFile(filepath.Join(dir, ".skytime-credentials"), tomlBytes, 0o600)` per test case. No in-memory FS needed; the temp file approach mirrors `pkg/extension/builtin/http/http_test.go`'s style (concrete fixtures > mocks). Test cases:
1. Happy path (all three credential kinds) → `Resolve(ctx, "github_token")` returns `*BearerCredential`.
2. Unknown ID → wraps `extension.ErrUnknownCredential` (verify with `errors.Is`).
3. Missing file → returns `*fs.PathError` from `os.Stat`.
4. Malformed TOML → returns parse error with file path.
5. Missing `type` field → `credential %q: type is required` error.
6. Unknown `type` → `unknown type %q` error.
7. Bearer missing `token` → `(bearer): token is required` error.
8. World-readable file (mode 0o644) + default → succeeds + emits one slog Warn.
9. World-readable file (mode 0o644) + `WithStrictMode()` → returns error.
10. Custom path via `WithPath(...)` → reads from that path, ignores `$HOME`.

---

### 3. `.star` Test Patterns Using `temporal_test`

**Surface (verified against `pkg/testing/module.go` + `docs/for-flow-authors/testing.md`):**
- `tester.workflow(name=, init_state=, retry_policy=, timeouts=)` — declares the workflow under test (last-write-wins; per-test override allowed).
- `tester.mock_action(extension=, op=, mock_fn=, match=)` — registers a mock; file-scope or per-test-scope (per-test shadows file-scope).
- `tester.run(flow=)` — must be called inside `def test_*()`; runs the flow TWICE (D5-D1 always-on replay) and reports divergence.
- Mock function signature: `lambda kwargs, attempt: <ok | err | nonretryable>`.
  - `kwargs` is a frozen Starlark dict (post-interpolation, post-credential-resolve).
  - `attempt` is 1-indexed; increments only on **retryable** failure.
  - `kwargs["_credential_id"]` is exposed (the credential ID string, NOT the secret).
- Return-shape builders (registered automatically in test mode): `ok(value=...)`, `err(msg=...)`, `nonretryable(msg=...)`.
- `assert.eq / assert.true / assert.contains / assert.fails / ...` — from `go.starlark.net/starlarktest`, available file-wide in test mode.

**Replay-determinism is ALWAYS-ON.** No opt-in flag. Every `tester.run(flow=...)` runs the workflow twice with shared `MockRegistry` + shared `AttemptCounter` and diffs Temporal event histories via `pkg/testing/replay_diff.go::FirstDivergentEvent`. Divergence reports `flow callsite + test callsite + payload before/after`. **What MUST hold for replay to actually work:**
1. Map iteration in the interpreter must be sorted (already enforced by INTRP-06 + workflowcheck).
2. No `time.Now()` / no `random` in mock lambdas (already locked by `lambdaTimeGlobals` 20-key subset).
3. Mock `kwargs` dict must produce identical iteration order across runs (verified by `kwargsAsStringMap`'s `Items()` insertion-order preservation, locked by Phase 04.1's documented language contract).
4. `attempt` must be deterministic across the two runs — guaranteed because the SAME `*AttemptCounter` is reused across both `RunOnceCapturing` calls (verified at `pkg/testing/builtin_run.go:86-88`).

**`issue_triage_test.star` skeleton (locked by D-FLOWS-LINEUP):**
```python
"""issue_triage_test.star — Tier-3 tests for issue_triage.

Demonstrates:
  - File-scope mocks shared across every test
  - Per-test override that simulates a transient failure with attempt=
  - assert.* for outcome verification
  - Replay determinism (always-on; no opt-in)

NOTE: v1 does not support load() across files (pkg/testing/runner.go:
"Single-file scope only"). The flow under test is REDECLARED inline below.
"""

gh = github.client(credential = "github_token")

# ----- Inline flow declaration (would normally load() from issue_triage.star) -----
flow(
    name = "triage_issue",
    inputs = {"owner": "string", "repo": "string", "number": "int"},
    steps = [
        step(name = "Get issue #${ctx.number}", action = gh.get_issue(
            owner = "${ctx.owner}", repo = "${ctx.repo}", number = "${ctx.number}",
        )),
        script(
            id = "classify",
            fn = lambda ctx: {"is_old": True},  # placeholder — real fn reads ctx.<step_output>
            output_alias = "classification",
        ),
        if_cond(
            cond = lambda ctx: ctx.classification.is_old,
            then = [
                step(action = gh.add_comment(
                    owner = "${ctx.owner}", repo = "${ctx.repo}", number = "${ctx.number}",
                    body = "This issue has been open >30 days — auto-triaged.",
                )),
            ],
            else_ = [],
        ),
    ],
)

flow(
    name = "issue_triage",
    inputs = {"owner": "string", "repo": "string"},
    steps = [
        step(action = gh.list_open_issues(owner = "${ctx.owner}", repo = "${ctx.repo}")),
        for_each_parallel(
            items = lambda ctx: ctx.list_open_issues_output.issues,  # actual ctx path TBD per output shape
            item = "issue",
            steps = [
                call_flow(
                    name = "triage_issue",
                    inputs = {"owner": "x", "repo": "x", "number": "x"},
                ),
            ],
        ),
    ],
)

# ----- File-scope mocks (every test inherits these) -----
tester.mock_action(
    extension = "github",   # NOTE: the REGISTERED name "github", not the local "gh" variable
    op = "list_open_issues",
    mock_fn = lambda kwargs, attempt: ok(value = {"issues": [{"number": 1}, {"number": 2}]}),
)
tester.mock_action(
    extension = "github",
    op = "get_issue",
    mock_fn = lambda kwargs, attempt: ok(value = {"number": 1, "title": "Test", "state": "open"}),
)
tester.mock_action(
    extension = "github",
    op = "add_comment",
    mock_fn = lambda kwargs, attempt: ok(value = {}),
)

tester.workflow(name = "issue_triage", init_state = {"owner": "octocat", "repo": "Hello-World"})


def test_happy_path():
    """Every issue triaged via inherited mocks; replay-determinism asserted automatically."""
    tester.run(flow = "issue_triage")


def test_get_issue_retries_then_succeeds():
    """First attempt of get_issue fails (retryable), second succeeds.
    Exercises EX-03 attempt-aware retry path."""
    tester.mock_action(
        extension = "github",
        op = "get_issue",
        mock_fn = lambda kwargs, attempt: (
            err(msg = "transient HTTP 503") if attempt == 1
            else ok(value = {"number": kwargs["number"], "title": "Triaged", "state": "open"})
        ),
    )
    tester.run(flow = "issue_triage")


def test_add_comment_routes_through_credfile():
    """Asserts the credential ID flows to the mock — proves credfile resolver
    is wired into the example's binary."""
    tester.mock_action(
        extension = "github",
        op = "add_comment",
        mock_fn = lambda kwargs, attempt: (
            ok(value = {}) if kwargs["_credential_id"] == "github_token"
            else nonretryable(msg = "wrong credential routed: " + kwargs["_credential_id"])
        ),
    )
    tester.run(flow = "issue_triage")
```

**How tests get wired in:** `extbin test ./examples/http-github-webhook/` walks the directory recursively for `*_test.star` files via `pkg/testing.RunCLI(dir, opts...)` — this is already wired in `pkg/cli/test.go` and the example's `cmd/extbin/main.go` inherits the `test` subcommand for free from `cli.NewRootCommand`. Production `skytime run` paths skip `*_test.star` via `pkg/worker/boot.go:65` (the `!strings.HasSuffix(d.Name(), "_test.star")` check) — so test files don't bleed into production execution.

---

### 4. `Idempotent bool` Per-Operation Mechanics

**Already implemented at the right layer.** `pkg/extension/operation.go:48-76` defines `OperationSpec.Idempotent *bool` (REQUIRED, nil rejected by Registry per D-12 / `extension.ErrIdempotentRequired`). `extension.Ptr(true)` / `extension.Ptr(false)` is the registration idiom.

**Where one-action-per-activity-invocation enforcement lives:** `pkg/activity/execute_batch.go` (Phase 2 plan 02-03) + `pkg/activity/validate_batch.go` (Phase 2 plan 02-03 D2-05 mixed-idempotency reject). The interpreter at `pkg/interpreter/walk_step.go` consults the `OperationSpec.Idempotent` flag when packaging a `block` into an `ExecuteBatch` activity invocation — non-idempotent ops are dispatched as separate activity invocations even if syntactically grouped in a `block`. This is **shipped behavior**, not new work.

**What Phase 6 must do:** Add a verification test that exercises the path via the new webhook extension. Test shape:
```go
// examples/http-github-webhook/extensions/webhook/webhook_test.go
func TestWebhookPost_NonIdempotent_OnePerActivityInvocation(t *testing.T) {
    // Register webhook ext, build a mini-flow that wraps two webhook.post calls
    // in a step(block_fn=lambda ctx: [wh.post(body="a"), wh.post(body="b")]),
    // run via testsuite.TestActivityEnvironment + spy on the activity callsite count.
    // Assert: 2 activity invocations recorded, NOT 1 batched.
    // This satisfies success criterion 3 explicitly.
}
```

The spy pattern matches `pkg/interpreter/walk_step_test.go`'s `helperRegisterFakeExecuteBatch` approach — register a fake `ExecuteBatch` activity that records its argument count per invocation, then `assert.Equal(t, 2, len(invocations))`.

---

### 5. CI Workflow (`.github/workflows/ci.yml`)

**Action versions (verified May 2026):**

| Action | Version | Verification |
|--------|---------|--------------|
| `actions/checkout` | `@v6` | Latest `v6.0.2` (2026-01-09) per `https://github.com/actions/checkout/releases/latest`. Pin major for security-patch float. |
| `actions/setup-go` | `@v6` | Latest `v6.4.0` (2026-03-30) per `https://github.com/actions/setup-go/releases/latest`. v6 supports Go 1.22+ and has built-in module + build cache (no separate `actions/cache@v4` needed). |
| `temporalio/setup-temporal` | `@v0` | Official action that installs `temporal` CLI on PATH. Default version = latest (v1.7.0 as of 2026-05-06). |

> **Note:** D-CI-GO says `actions/setup-go@v5`. Researcher recommends `@v6` instead — v6 is current, v5 is one major behind, and `setup-go`'s v6 release is fully backward-compatible with v5 inputs (`go-version`, `cache`). If the planner prefers to stick with the literal CONTEXT.md value, `@v5` works too. Flagging this as a discretionary upgrade.

**Caching:** `actions/setup-go@v6` provides built-in module + build cache when `cache: true` (the default since v4). Hash-of-`go.sum` keys the cache. No need for a separate `actions/cache@v4` step.

**Temporal dev server in CI:** Use `temporal server start-dev --headless &` (background, no UI). `--headless` is faster (~2s startup vs ~4s with web UI) and more deterministic. Wait for readiness with a short poll loop OR `temporal operator namespace describe default --address localhost:7233 --retry-attempts 30` (built-in retry backoff). The temporalite Go-embedded option is **off the table** — the upstream repo was archived 2024-04-03 in favor of `temporal server start-dev` (verified at `https://github.com/temporalio/temporalite`).

**Matrix:** Single Go version (`1.25`) per CONTEXT.md D-CI-GO. Adding `1.26` would double minutes for ~zero coverage gain; leave for v2 if a real consumer requests it.

**Race detector:** ON for all `go test` invocations per CONTEXT.md (`go test -race ./... -count=1`). The CI smoke step's `extbin run public_repo_check.star` does NOT need `-race` (it's a binary run, not `go test`); race issues only surface in activity/test code per CLAUDE.md ("Workflow code is single-threaded by Temporal's deterministic runner").

**GitHub-flow smoke strategy:** The locked decision (D-CI-STEPS) is to hit `api.github.com` directly with the `public_repo_check.star` flow against `octocat/Hello-World`. Rate-limit risk in CI: GitHub's unauthenticated API allows 60 requests/hour per IP. CI runners have shared IPs but the limit is *per-IP* and shared with many users. **Mitigation:** the flow makes ~3 requests per CI run (one `get_repo`, plus ~2 follow-ups under the popular branch). At 60/hr per IP, this leaves headroom for ~20 builds/hour/runner-IP-pool; should never hit the cap unless multiple repos on the same runner pool are aggressively retesting. **Researcher recommends sticking with the live-API path for v1** — it's the most honest demonstration of the "from git-clone to real workflow" promise, the rate-limit risk is low in practice, and a recorded VCR fixture would add complexity (a VCR library, fixture maintenance, replay stability) for marginal benefit. If the rate limit ever DOES bite, the fix is a one-line addition: cache the response via a CI step that hits the API once and stores `~/.cache/skytime-test-fixtures/octocat-hello-world.json`, with the flow optionally pointing at a local mock URL via `--input '{"base_url": "..."}'`. Document this fallback in the README's "If CI flakes" section.

**Smoke-step exit-condition assertion:** Exit code 0 + `grep flow_complete` on stdout. `flow_complete` is the standard renderer terminator from `pkg/cli/progress.go`'s static and live progress modes — it appears as `flow_complete` in the slog handler's text output when the workflow finishes successfully. (Verified by reading `pkg/cli/progress.go` and `pkg/cli/progress_static.go`.) The test command:
```bash
output=$(/tmp/extbin run examples/http-github-webhook/public_repo_check.star \
    --flow public_repo_check --input '{"repo":"octocat/Hello-World"}')
echo "$output" | grep -q "flow_complete" || (echo "$output"; exit 1)
```

**Complete `ci.yml` skeleton:**
```yaml
name: CI
on:
  push:
    branches: ['**']
permissions:
  contents: read
jobs:
  ci:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with:
          go-version: '1.25'
          cache: true
      - uses: temporalio/setup-temporal@v0
      - name: go vet
        run: go vet ./...
      - name: go test (race)
        run: go test -race ./... -count=1
      - name: build extbin
        run: go build -o /tmp/extbin ./examples/http-github-webhook/cmd/extbin
      - name: extbin test (Tier-3 .star tests)
        run: /tmp/extbin test ./examples/http-github-webhook/
      - name: EX-04 walkthrough smoke (public GitHub API)
        run: |
          temporal server start-dev --headless &
          for i in {1..30}; do
            temporal operator namespace describe default --address localhost:7233 2>/dev/null && break
            sleep 1
          done
          output=$(/tmp/extbin run examples/http-github-webhook/public_repo_check.star \
            --flow public_repo_check --input '{"repo":"octocat/Hello-World"}')
          echo "$output"
          echo "$output" | grep -q "flow_complete"
```

---

### 6. README Walkthrough Structure (EX-04 ≤5 commands)

**The five commands** (after `git clone`):

1. `cd skytime/examples/http-github-webhook`
2. `temporal server start-dev --headless &` *(in another terminal, OR in the background as shown)*
3. `go build -o ./extbin ./cmd/extbin`
4. `./extbin run public_repo_check.star --flow public_repo_check --input '{"repo":"octocat/Hello-World"}'`

That's 4 commands (under the ≤5 cap). The `git clone https://github.com/.../skytime` is the implicit "step 0" the reader has already done. NO Makefile needed for the headline demo — every command is a one-liner.

**Optional step 5 for the second-stage authenticated walkthrough:** `cp .skytime-credentials.example ~/.skytime-credentials && chmod 600 ~/.skytime-credentials && $EDITOR ~/.skytime-credentials` — paste GitHub PAT + webhook.site URL.

**`~/.skytime-credentials` bootstrapping:** Ship `examples/http-github-webhook/.skytime-credentials.example` in the repo with placeholder values:
```toml
# .skytime-credentials.example — copy to ~/.skytime-credentials and edit.
# IMPORTANT: chmod 600 ~/.skytime-credentials after copying.

[credentials.github_token]
type = "bearer"
token = "ghp_REPLACE_WITH_YOUR_PAT"

[credentials.webhook_url]
type = "bearer"
token = "https://webhook.site/REPLACE_WITH_YOUR_TOKEN"
```

The README's "Authenticated walkthrough" section says: "Visit https://webhook.site (no account needed), copy your unique URL, paste into `webhook_url` above. Visit https://github.com/settings/personal-access-tokens (any account), generate a fine-grained PAT with `public_repo` scope, paste into `github_token` above."

**Coverage matrix table format:** Markdown table per D-FLOWS-COVERAGE-MATRIX (already drafted in CONTEXT.md verbatim — the planner copies that into the README). Format:

| Flow | seq | block | if_cond | script | for_each_par | call_flow | retries | timeouts | credentials | cancellation |
|------|-----|-------|---------|--------|--------------|-----------|---------|----------|-------------|--------------|
| public_repo_check | ✓ | ✓ | ✓ | ✓ |  |  |  |  |  |  |
| pr_to_webhook | ✓ |  |  | ✓ | ✓ |  | ✓ |  | ✓ |  |
| issue_triage | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ (incidental) |
| batch_label_issues | ✓ | ✓ |  | ✓ |  |  | ✓ | ✓ | ✓ |  |
| weekly_digest | ✓ |  |  | ✓ | ✓ |  | ✓ |  | ✓ |  |

**No Makefile needed.** Every command in the walkthrough is short enough to type. A Makefile would add indirection (`make example` vs `./extbin run examples/...`) for marginal benefit. If a planner decides to add one for the developer-loop UX, the targets should be: `build`, `test`, `dev-server`, `example`, `clean` — all one-liners wrapping the equivalent `go`/`extbin`/`temporal` commands.

---

### 7. Validation Architecture (Nyquist VALIDATION.md driver)

> Required because `workflow.nyquist_validation: true` (default; absent from config means enabled).

#### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `testify/{require,assert}` v1.11.1 (existing) |
| Config file | none — Go stdlib auto-discovers `*_test.go`; `*_test.star` files run via `extbin test` (CLI-03) |
| Quick run command | `go test -race -count=1 ./pkg/extension/credfile/... ./examples/http-github-webhook/...` (narrowed per-task) |
| Full suite command | `go test -race -count=1 ./...` |
| Estimated runtime | ~10s for credfile/extension package quick run; ~2-3min full suite (matches Phase 5 cadence) |

#### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| EX-01 | HTTP/GitHub/Webhook each implement `extension.Extension` and register without `ErrIdempotentRequired` | Unit | `go test -race ./examples/http-github-webhook/extensions/github -run TestExtension_RegistersWithoutError` | ❌ Wave 0 |
| EX-01 | Each op declares `Idempotent *bool` correctly (GitHub: GET=true, POST=false; Webhook: post=false) | Unit | `go test -race ./examples/http-github-webhook/extensions/github -run TestExtension_OperationsIdempotenceMatchesEndpoints` + `...webhook -run TestExtension_PostIsNonIdempotent` | ❌ Wave 0 |
| EX-01 | `webhook.post` inside a `block` runs one-action-per-activity-invocation (success criterion 3) | Integration | `go test -race ./examples/http-github-webhook/extensions/webhook -run TestWebhookPost_NonIdempotent_OnePerActivityInvocation` | ❌ Wave 1 |
| EX-02 | Coverage matrix is materially correct: parsing each flow produces a DAG that exercises the listed primitives | Integration | `go test -race ./examples/http-github-webhook -run TestFlows_CoverageMatrix` (re-parses each `.star` and asserts node-type set per flow against an expected map) | ❌ Wave 2 |
| EX-02 | All 5 flows parse without error under the example's extension set | Integration | `go test -race ./examples/http-github-webhook -run TestFlows_ParseAll` | ❌ Wave 1 |
| EX-02 | All 5 flows pass differential corpus (parse + dryrun agree) | Integration | `go test -race ./tests -run TestDifferentialCorpus` (existing; auto-picks up new files in `examples/`) — verify the differential test's directory walk includes `examples/http-github-webhook/` | ❌ verify pre-existing |
| EX-03 | `issue_triage_test.star` runs end-to-end via `extbin test` and exercises `attempt`-aware retry | E2E | `go test -race ./examples/http-github-webhook -run TestIssueTriageTest_E2E` (subprocess against `extbin`) OR `go test -race ./examples/http-github-webhook -run TestIssueTriageTest_PkgTesting` (in-process via `pkgtesting.Run(t, "...", WithExtensions(...))`) | ❌ Wave 2 |
| EX-03 | Replay determinism is exercised (always-on; assertion: `tester.run` doesn't surface a divergence) | E2E | Subsumed by the `_E2E` test above — replay-twice is automatic; failure surfaces as test failure | ❌ Wave 2 |
| EX-04 | README walkthrough commands work end-to-end (≤5 commands, exit code 0, `flow_complete` substring present) | E2E (CI smoke) | `bash .github/workflows/scripts/walkthrough_smoke.sh` invoked by `ci.yml` step | ❌ Wave 3 |
| EX-04 (sub) | `pkg/extension/credfile/` Resolver returns expected `*BearerCredential` for the github_token id | Unit | `go test -race ./pkg/extension/credfile -run TestResolver_HappyPath_BearerCredential` | ❌ Wave 0 |
| EX-04 (sub) | Credfile rejects malformed TOML, missing type, unknown type, missing required fields | Unit | `go test -race ./pkg/extension/credfile -run TestResolver_RejectsMalformed_TableDriven` | ❌ Wave 0 |
| EX-04 (sub) | Unknown credential ID wraps `extension.ErrUnknownCredential` (D2-12) | Unit | `go test -race ./pkg/extension/credfile -run TestResolver_UnknownID_WrapsErrUnknownCredential` | ❌ Wave 0 |
| EX-04 (sub) | World-readable file: warns (default) / refuses (WithStrictMode) | Unit | `go test -race ./pkg/extension/credfile -run TestResolver_WorldReadable_WarnsByDefault` + `..._WithStrictMode_Refuses` | ❌ Wave 0 |
| EX-04 (sub) | Default path is `$HOME/.skytime-credentials`; `WithPath` overrides | Unit | `go test -race ./pkg/extension/credfile -run TestResolver_DefaultPath` + `..._WithPathOverrides` | ❌ Wave 0 |
| (CI infra) | `.github/workflows/ci.yml` is syntactically valid YAML and parseable as a GitHub Actions workflow | Manual+lint | `actionlint .github/workflows/ci.yml` (recommended; not a hard gate) | ❌ Wave 3 |

#### Sampling Rate

- **Per task commit:** `go test -race -count=1 ./pkg/extension/credfile/... ./examples/http-github-webhook/...` (~10s).
- **Per wave merge:** `go test -race -count=1 ./...` (~2-3min).
- **Phase gate:** Full suite green + `extbin test ./examples/http-github-webhook/` green + walkthrough smoke green before `/gsd:verify-work`.

#### Wave 0 Gaps

- [ ] `pkg/extension/credfile/doc.go` — package overview
- [ ] `pkg/extension/credfile/resolver.go` — `Resolver`, `New`, `Resolve` (CredentialHandler impl)
- [ ] `pkg/extension/credfile/options.go` — `Option`, `WithPath`, `WithStrictMode`, `WithLogger`
- [ ] `pkg/extension/credfile/file.go` — `fileShape`, `credentialEntry`, `buildCredentials`
- [ ] `pkg/extension/credfile/resolver_test.go` — full table-driven coverage (the 10 cases listed in § Credfile)
- [ ] `examples/http-github-webhook/extensions/github/github.go` + `*_test.go` + `response.go` (output types)
- [ ] `examples/http-github-webhook/extensions/webhook/webhook.go` + `*_test.go`
- [ ] `examples/http-github-webhook/cmd/extbin/main.go` (~30 LOC wiring)
- [ ] Five `.star` flows + one `*_test.star` under `examples/http-github-webhook/`
- [ ] `examples/http-github-webhook/README.md` (7 sections per D-DOCS-README)
- [ ] `examples/http-github-webhook/.skytime-credentials.example`
- [ ] `.github/workflows/ci.yml`
- [ ] Optional: `.github/workflows/scripts/walkthrough_smoke.sh` (extracted CI smoke step for local repro)
- [ ] One-line addition to root `README.md` under "Where to Go Next"
- [ ] Short reference section in `docs/for-extension-developers/README.md` for `pkg/extension/credfile/`

**Framework install:** NONE — every dep already in `go.mod` except `pelletier/go-toml/v2` and `google/go-github/v78`, both added by `go get` in Wave 0:
```bash
go get github.com/pelletier/go-toml/v2@v2.3.1
go get github.com/google/go-github/v78@v78.0.0
go mod tidy
```

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | All tasks | ✓ | go1.26.2 (toolchain) / go1.25 (declared) | — |
| `temporal` CLI | EX-04 walkthrough smoke + manual demos | check at plan time | latest (v1.7.0+) | `temporal server start-dev --headless` is the canonical path; brew install temporal OR `curl -sSf https://temporal.download/cli.sh \| sh` (existing CLI message at `pkg/cli/dev_server.go:106-110`) |
| Internet to `api.github.com` | EX-04 smoke + headline demo | ✓ in CI; assumed for human readers | — | If down: README's headline demo fails — flag for human-UAT note |
| Internet to `webhook.site` | Authenticated walkthrough only (NOT CI) | ✓ for human readers | — | Reader can substitute any URL accepting POST (Discord/Slack/Teams webhook) |
| `pelletier/go-toml/v2` | credfile package | not yet | v2.3.1 (verified) | — (added in Wave 0 via `go get`) |
| `google/go-github/v78` | github extension | not yet | v78.0.0 (verified) | — (added in Wave 0 via `go get`) |

**Missing dependencies with no fallback:** None blocking — every dependency can be installed via `go get` (libraries) or has a documented install path (temporal CLI; `pkg/cli/dev_server.go:106-110` already prints install instructions on the missing-binary path).

**Missing dependencies with fallback:** None.

---

## Pitfalls and Non-Obvious Traps

### Pitfall 1: Webhook server lifecycle (NON-ISSUE in v1)

**Initial concern:** "Where does the webhook receiver run? Inside Temporal? Sidecar? Separate binary?"
**Resolution:** Phase 6 ships a webhook *outbound* extension (POST to a URL), NOT a webhook *receiver*. The "receiver" is `webhook.site` (hosted, browser-visible). No `net/http.Server` lives anywhere in this phase. **Never put `http.ListenAndServe` inside a workflow** — it would (1) violate determinism, (2) hold the workflow goroutine forever. If a v2 extension ever needs an inbound webhook, it lives in a separate process (sidecar) that receives the webhook and uses `client.SignalWorkflow(...)` to deliver the payload to a waiting workflow via the (deferred) `wait_for_signal` primitive (DSL-V2-01).

### Pitfall 2: Mock router keys on EXTENSION NAME, not the local variable

Documented in `docs/for-flow-authors/testing.md`. If a flow does `gh = github.client(credential="github_token")`, the mock MUST use `extension="github"`, not `extension="gh"`. The `_test.star` template above gets this right; the README's "writing tests" section MUST call this out explicitly. Failure mode: `no mock for github.get_issue at <pos>` error — the hint is the verbatim `github` (registered name) in the error, even though the user wrote `gh`.

### Pitfall 3: GitHub rate limits in CI for unauthenticated calls

60 req/hr per IP for unauthenticated `api.github.com`. CI runners share IP pools but the limit is per-IP, not per-runner. Risk is LOW for `public_repo_check.star` (~3 requests per CI run) but non-zero. **Mitigation in v1:** rely on the live API; document the fallback (cache fixture in `~/.cache/skytime-test-fixtures/` and rerun) in the README's "If CI is flaking" section. **Mitigation deferred:** if a real consumer hits this, switch the smoke flow to use a recorded fixture via a local mock URL.

### Pitfall 4: Credential file mode permissions on Linux vs macOS

`os.UserHomeDir()` returns `/home/<user>` on Linux and `/Users/<user>` on macOS. Both default to `umask 022` for new files, which produces mode `0o644` (group + world readable). The credfile resolver's warning fires on `mode & 0o044 != 0` — this means the FIRST `extbin run` after a fresh install will emit a warning unless the reader explicitly `chmod 600`s the file. The README's "second-stage walkthrough" includes the `chmod 600` step explicitly, and the warning message points at the fix. **Windows:** out of scope per D-CI-RUNNER (Linux-only CI; macOS/Windows matrix deferred). The credfile resolver should still WORK on Windows (`os.UserHomeDir` returns `C:\Users\<user>`), but the file-mode check semantics are different on NTFS — Windows ignores `0o6xx` permissions in the POSIX sense. Recommendation: skip the file-mode warning entirely on Windows (`if runtime.GOOS == "windows" { return nil }` short-circuit before the mode check) — document as "file-mode warning is POSIX-only".

### Pitfall 5: HMAC verification timing-attack — NON-ISSUE in v1

The original concern about HMAC verification (`crypto/subtle.ConstantTimeCompare`) applies to **inbound webhooks** with signature verification (e.g., GitHub's `X-Hub-Signature-256` header). Phase 6 ships only outbound POST — no signatures to verify. **For v2 if an inbound-webhook extension ever lands:** use `crypto/hmac.Equal` (which calls `subtle.ConstantTimeCompare` internally) — never use `==` or `bytes.Equal` for HMAC comparison. Out of scope for this phase.

### Pitfall 6: `block` + non-idempotent op + replay

Phase 2 already enforces (D2-05 mixed-idempotency reject + ACT-03 one-per-activity-invocation). **Subtle Temporal-specific gotcha to watch for:** Temporal's history records each activity invocation distinctly. A `block` of two `webhook.post` actions produces TWO `ActivityTaskScheduled` events on the wire (not one), which is the expected and correct behavior. The replay-twice diff in `tester.run` will catch any non-determinism, but the test author must ensure the mock returns IDENTICAL outputs on both replay runs (don't read a clock or random in the mock body — already enforced by `lambdaTimeGlobals`, but worth pointing out).

### Pitfall 7: go-github v78 transitive dep collision risk

go-github v78 depends only on `google/go-querystring` (a tiny pure-Go library, no protobuf). Verified against `https://github.com/google/go-github/blob/master/go.mod`. **No risk of collision** with Temporal SDK v1.42's `google.golang.org/protobuf v1.36.11` transitive — they're in different module trees. CLAUDE.md's "Avoid `gogo/protobuf` direct dependencies" warning does NOT apply because go-github doesn't use protobuf at all.

### Pitfall 8: Temporal CLI version pinning in CI

`temporalio/setup-temporal@v0` defaults to the latest CLI version. Safer for reproducibility: pin the version explicitly:
```yaml
- uses: temporalio/setup-temporal@v0
  with:
    version: v1.7.0   # pin against silent CLI behavior changes
```
Trade-off: pinning means quarterly bumps to track upstream; unpinned means CI breaks unannounced if the upstream CLI introduces a breaking change. Recommend **unpinned for v1** (CONTEXT.md doesn't lock a version, the upstream API is stable) and **flag for human-UAT review at first CI run** to confirm the version that lands.

### Pitfall 9: `pkg/testing.RunCLI` walks RECURSIVELY

`extbin test ./examples/http-github-webhook/` walks the whole directory tree. Any stray `*_test.star` file in subdirectories will be picked up. The `.skytime-credentials.example` won't trigger this (different extension). **Defensive step:** verify Phase 6 doesn't accidentally stash test fixtures matching `*_test.star` outside the intended location.

### Pitfall 10: `pkg/testing.WithExtensions` vs `pkg/cli.WithExtensions`

These are TWO DIFFERENT options on TWO DIFFERENT packages. The `extbin test` subcommand wires `pkg/testing.WithExtensions(cfg.exts...)` from `pkg/cli/test.go:53` — it pulls from the `cli.config.exts` slice that was populated via `cli.WithExtensions(...)` at `cli.NewRootCommand` time. So **the example's `cmd/extbin/main.go` only calls `cli.WithExtensions(...)` ONCE** and the test subcommand inherits the extensions automatically. Do not call `pkg/testing.WithExtensions` directly in the example's `main.go`.

---

## Open Questions

After deep research, the following remain genuinely unresolved (NOT items already locked in CONTEXT.md):

1. **Should `cmd/extbin/main.go` add `--credfile` as a flag, or use `$SKYTIME_CREDFILE_PATH` env var, or both?**
   - What we know: D-CREDS-PATH locks the default at `$HOME/.skytime-credentials` and locks `WithPath(...)` as the override. The env-var / CLI-flag wiring is the EXAMPLE's choice.
   - What's unclear: whether the example wires a flag like `--credfile path` (cobra-friendly) or relies on env-var (`SKYTIME_CREDFILE_PATH`) reading via the existing `pkg/cli/flags.go` env-binding path.
   - Recommendation: planner picks. Env-var is least-friction (no new flag to document); CLI flag is more discoverable. Default to env-var with the env name documented in the README; add the flag later if needed.

2. **Should the example commit `*.skytime-credentials` to `.gitignore` at the repo root, or in a sub-`.gitignore` under `examples/http-github-webhook/`?**
   - What we know: the file lives at `~/.skytime-credentials`, NOT under the repo. The risk is the user accidentally `cp`-ing it INTO the repo and committing.
   - Recommendation: add `*.skytime-credentials` (without the leading dot) to the root `.gitignore` as a defense-in-depth + add a ".skytime-credentials*" pattern explicitly. Not load-bearing; planner decides.

3. **Does `pkg/testing.RunCLI` (invoked by `extbin test`) pick up the `pkg/cli` config's `WithCredentialHandler` automatically?**
   - What we know: `pkg/cli/test.go:52-58` only forwards `cfg.exts` and output options to `pkg/testing.RunCLI`; it does NOT forward `cfg.credHandler`.
   - What's unclear: whether the Tier-3 mocks bypass credential resolution entirely (the router exposes `kwargs["_credential_id"]` but does not invoke the real resolver — verified at `pkg/testing/router.go:206-235`). This means the credfile resolver is NEVER exercised by `extbin test`; it's only exercised by `extbin run`.
   - Resolution: this is INTENDED behavior — Tier-3 tests are hermetic, no network calls, no real credentials. The walkthrough smoke step in CI exercises the credfile resolver via `extbin run public_repo_check.star` (which uses an EMPTY credential set since the public flow needs no auth — `Resolve` is never called for an empty CredentialID). To exercise credfile resolution end-to-end **in CI**, would need a CI-only mock-server step OR a dedicated unit test for `credfile.Resolver` (already in the validation matrix). **Recommendation: rely on the unit tests for credfile + the manual second-stage walkthrough for human-UAT verification of end-to-end resolver behavior. Do not block on a CI integration test for the resolver.** Document this in HUMAN-UAT.md.

4. **Should the GitHub extension expose pagination kwargs (`per_page`, `page`)?**
   - What we know: `list_open_issues`, `list_prs`, `list_recent_merged_prs` all return paginated results. go-github exposes `&github.IssueListByRepoOptions{ListOptions: github.ListOptions{PerPage: 100}}` natively.
   - Recommendation: v1 uses defaults (30/page) and accepts a single page. The example flows don't need >30 issues for the demo. If pagination is needed, bump to `PerPage: 100` (max). Document as "v1 returns first page only (up to 100 items); pagination is v2." Planner decides whether to expose the kwarg or hard-code.

These are minor scoping decisions, NOT blockers. Research is otherwise complete.

---

## Sources

### Primary (HIGH confidence)
- **CONTEXT.md** — `.planning/phases/06-example-project-http-github-webhook/06-CONTEXT.md` (locked decisions; all references prefixed `D-`)
- **REQUIREMENTS.md** — `.planning/REQUIREMENTS.md` (EX-01..EX-04, lines 99-102)
- **ROADMAP.md** — `.planning/ROADMAP.md` (Phase 6 section, lines 158-169)
- **CLAUDE.md** — project root (tech stack lockdown, security/determinism constraints)
- **HTTP extension source** — `pkg/extension/builtin/http/http.go` (canonical extension implementation template)
- **OperationSpec source** — `pkg/extension/operation.go:48-76` (Idempotent *bool semantics)
- **CredentialHandler interface** — `pkg/extension/handler.go` (Resolve(ctx, id) → Credential, error)
- **Credential types** — `pkg/extension/credential.go` (sealed BearerCredential / BasicCredential / APIKeyCredential)
- **CLI options** — `pkg/cli/options.go` (WithExtensions / WithCredentialHandler functional options)
- **CLI root** — `pkg/cli/root.go` (NewRootCommand wires test subcommand at line 63)
- **CLI test subcommand** — `pkg/cli/test.go` (RunCLI invocation; pulls cfg.exts, NOT cfg.credHandler)
- **Testing module** — `pkg/testing/module.go`, `builtin_run.go`, `builtin_workflow.go`, `builtin_mock_action.go` (tester.* surface)
- **Testing router** — `pkg/testing/router.go` (mock dispatch; `_credential_id` exposure at line 217)
- **Testing CLI runner** — `pkg/testing/cli_run.go` (RunCLI signature; bareReporter pattern)
- **Worker boot** — `pkg/worker/boot.go:65` (`*_test.star` skip pattern)
- **Existing examples** — `examples/skeleton/{simple_check,parallel_fanout,simple_check_test,expression_if}.star` (style + comment density target)
- **Documented Tier-3 reference** — `docs/for-flow-authors/testing.md` (full `tester.*` API + caveats)
- **Build-your-own-binary doc** — `docs/cli-binary.md` (canonical pattern the example operationalizes)
- **Existing main** — `cmd/skytime/main.go` (~70 LOC template; `extbin/main.go` is a near-copy with credfile + 3 extensions)

### Library Version Verification (HIGH confidence — Go module proxy)
- `https://proxy.golang.org/github.com/pelletier/go-toml/v2/@latest` → v2.3.1, 2026-04-16
- `https://proxy.golang.org/github.com/!burnt!sushi/toml/@latest` → v1.6.0, 2025-12-18
- `https://proxy.golang.org/github.com/google/go-github/v78/@latest` → v78.0.0, 2025-11-08
- `https://proxy.golang.org/github.com/google/go-github/v77/@latest` → v77.0.0, 2025-11-04
- `https://proxy.golang.org/github.com/google/go-github/v76/@latest` → v76.0.0, 2025-10-14
- `https://api.github.com/repos/actions/setup-go/releases/latest` → v6.4.0, 2026-03-30
- `https://api.github.com/repos/actions/checkout/releases/latest` → v6.0.2, 2026-01-09
- `https://github.com/temporalio/temporalite` → archived 2024-04-03, redirects to `temporalio/cli`
- `https://api.github.com/repos/temporalio/cli/releases/latest` → v1.7.0
- Existing `go.mod` confirms Go 1.25.8, Temporal SDK v1.42.0, go.starlark.net pseudo-version 2026-03-26 — matches CLAUDE.md exactly

### Secondary (MEDIUM confidence)
- [temporalio/setup-temporal README](https://github.com/temporalio/setup-temporal) — `@v0` install flow + `--headless` start
- [go-github v78 pkg.go.dev](https://pkg.go.dev/github.com/google/go-github/v78/github) — `(*Client).WithAuthToken` API, `*github.RateLimitError`, `*github.ErrorResponse`
- [pelletier/go-toml/v2 README](https://github.com/pelletier/go-toml) — `toml.Unmarshal` API, performance comparisons, position-aware errors
- [Temporal CLI docs: dev server](https://docs.temporal.io/cli/server) — `temporal server start-dev --headless` semantics

### Tertiary (LOW confidence — flagged for validation)
- None. Every recommendation above is grounded in EITHER (a) existing in-repo code, OR (b) Go module proxy version verification, OR (c) official upstream documentation.

---

## Metadata

**Confidence breakdown:**

| Area | Level | Reason |
|------|-------|--------|
| Standard stack | HIGH | All locked by CLAUDE.md + verified via Go module proxy; only NEW deps are pelletier/go-toml/v2 + go-github/v78, both verified |
| Architecture / extension shape | HIGH | Direct copy-paste from `pkg/extension/builtin/http/` template; surface is shipped Phase 4 code |
| Credfile design | HIGH | Maps 1:1 to existing sealed `extension.Credential` types + `CredentialHandler` interface; TOML schema locked verbatim in CONTEXT.md |
| .star test patterns | HIGH | `examples/skeleton/simple_check_test.star` is the working template; full surface documented in `docs/for-flow-authors/testing.md` |
| Idempotent enforcement | HIGH | Already shipped at `pkg/activity/execute_batch.go` + validated by `pkg/extension/operation.go::OperationSpec.Idempotent`; Phase 6 only adds an exercising test |
| CI workflow | HIGH | All actions verified at latest tagged versions (May 2026); `temporalio/setup-temporal@v0` is the canonical pattern |
| README walkthrough | HIGH | 4-command path proven against existing `cmd/skytime` UX; the only NEW piece is `cmd/extbin` (which is a copy of `cmd/skytime/main.go` + credfile + 3 extensions) |
| Pitfalls | HIGH | All pitfalls grounded in existing in-repo code references; no speculation |

**Research date:** 2026-05-06
**Valid until:** 2026-06-05 (30 days for stable; the only fast-moving piece is the temporal CLI version, which is OK to drift)

---

## RESEARCH COMPLETE
