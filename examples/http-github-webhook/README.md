# HTTP + GitHub + Webhook — the rich example project

> Three extensions (HTTP + GitHub + Webhook), five `.star` flows covering
> every DSL primitive, a Tier-3 test demonstrating attempt-aware retries
> and replay determinism, and the canonical `cmd/extbin` custom binary
> pattern. From `git clone` to a real workflow run in under five commands.

## What this example shows

Skytime's two-tier authoring model in one runnable project. The HTTP
extension is bundled with the library (`pkg/extension/builtin/http/`,
shipped in Phase 4); the GitHub and Webhook extensions are
example-local under `extensions/{github,webhook}/`. All three are
registered together by the example's custom CLI binary at
[`cmd/extbin/main.go`](cmd/extbin/main.go), which mirrors the canonical
"build your own binary" pattern documented in
[`docs/cli-binary.md`](../../docs/cli-binary.md). The five flows in this
directory exercise every DSL primitive (sequential step, block, block_fn,
`if_cond`, `script`, `for_each_parallel`, `call_flow`) and every
operational concern (retries, timeouts, credentials, cancellation). The
headline demo (`public_repo_check.star`) needs zero credentials and runs
against GitHub's public unauthenticated API — `git clone`, build, run.
The four authenticated flows need a TOML credfile at
`~/.skytime-credentials` (a fine-grained GitHub PAT and a webhook URL
from [webhook.site](https://webhook.site)).

## Quick start

After `git clone`, four commands take you from a fresh checkout to a
real workflow run against the public GitHub API.

```bash
# 1. Enter the example directory
cd skytime/examples/http-github-webhook
```

```bash
# 2. Start a local Temporal dev server (in another terminal, or background as shown)
temporal server start-dev --headless &
```

```bash
# 3. Build the example binary
go build -o ./extbin ./cmd/extbin
```

```bash
# 4. Run the headline flow against the public GitHub API (no credentials needed)
./extbin run public_repo_check.star \
  --flow public_repo_check \
  --input '{"repo":"octocat/Hello-World"}'
```

Expected output: a series of per-step progress lines, ending with
`[skytime] flow complete  N/N steps  total Xms`. If you instead see
`[skytime] flow failed  step I/M (reason)  total Xms`, scroll up — the
renderer prints the offending step's error inline. (The literal
terminator substring is `flow complete` — space, not underscore —
emitted from `pkg/cli/progress_static.go:245`.)

### Don't have temporal CLI yet?

The dev server is `temporal server start-dev` from the official
Temporal CLI. Install with one of:

```bash
brew install temporal
# OR
curl -sSf https://temporal.download/cli.sh | sh
```

## Coverage matrix

Every Skytime DSL primitive and every operational concern shows up in
at least one flow. The mapping is mechanically pinned by
`TestFlows_CoverageMatrix` in `flows_test.go` — drift between this
table and the actual DAG fails CI.

| Flow | seq | block | if_cond | script | for_each_par | call_flow | retries | timeouts | credentials | cancellation |
|------|-----|-------|---------|--------|--------------|-----------|---------|----------|-------------|--------------|
| public_repo_check | ✓ | ✓ | ✓ | ✓ |  |  |  |  |  |  |
| pr_to_webhook | ✓ |  |  | ✓ | ✓ |  | ✓ |  | ✓ |  |
| issue_triage | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ (incidental) |
| batch_label_issues | ✓ | ✓ |  | ✓ |  |  | ✓ | ✓ | ✓ |  |
| weekly_digest | ✓ |  |  | ✓ | ✓ |  | ✓ |  | ✓ |  |

Note: `issue_triage`'s `call_flow=✓` corresponds to the parsed top-level
`issue_triage` flow, which calls `triage_issue` via `call_flow`. The
subflow itself doesn't need its own row.

## Authenticated walkthrough

The headline demo uses GitHub's public unauthenticated API. The other
four flows need a GitHub PAT and a webhook URL. Setup is one extra
command.

1. **Get a webhook URL.** Visit [webhook.site](https://webhook.site)
   (no account needed). Copy your unique URL — looks like
   `https://webhook.site/abc-123-def-456`. The browser dashboard at the
   same URL displays each POST your flow sends, in real time.

2. **Get a GitHub PAT.** Visit
   [GitHub Settings → Personal access tokens](https://github.com/settings/personal-access-tokens).
   Generate a fine-grained PAT with the `public_repo` scope (read-only
   is enough for the example flows; the comment + label flows need
   write access — `repo` scope — when running against your own repos).

3. **Copy and edit the credfile:**

   ```bash
   cp .skytime-credentials.example ~/.skytime-credentials
   chmod 600 ~/.skytime-credentials
   $EDITOR ~/.skytime-credentials  # paste your webhook URL + PAT
   ```

> **Note: `chmod 600` is required.** On Linux and macOS, new files
> inherit `umask 022` → mode `0o644` (group + world readable). Skytime's
> `pkg/extension/credfile/` resolver warns when the file is
> world-readable and refuses to load it in strict mode. The first
> `extbin run` after a fresh setup will print this warning until you
> run `chmod 600 ~/.skytime-credentials`.

Now try an authenticated flow:

```bash
./extbin run pr_to_webhook.star \
  --flow pr_to_webhook \
  --input '{"owner":"octocat","repo":"Hello-World"}'
```

Each open PR's title is POSTed to your webhook URL. Refresh
[webhook.site](https://webhook.site) in your browser to see the entries
appear.

## Flow-by-flow tour

### public_repo_check.star

The headline demo. Parses an `"owner/repo"` input, fetches the repo via
GitHub's public unauthenticated API, then conditionally fetches a small
block of follow-up details (issues + PRs) when the repo crosses a
popularity threshold. Self-contained — no credfile, no webhook URL.

**Demonstrates:** sequential step, static block, `if_cond` (procedural
mode), `script`. No credentials.

→ [`public_repo_check.star`](public_repo_check.star)

### pr_to_webhook.star

Authenticated fan-out. Lists the open PRs in a repo, then in parallel
posts each PR's title to your webhook URL. The script after the
`for_each_parallel` summarizes the dispatched count. Demonstrates
`webhook.post` non-idempotency (each call creates a fresh entry on the
receiver — visible in the webhook.site dashboard).

**Demonstrates:** sequential step, `script`, `for_each_parallel`,
retries, credentials (`github_token` + `webhook_url`).

→ [`pr_to_webhook.star`](pr_to_webhook.star)

### issue_triage.star

The deepest flow in the corpus. Two flows in one file: a top-level
`issue_triage` that lists open issues then fans out via
`for_each_parallel(call_flow(triage_issue))`, and a `triage_issue`
subflow that runs per issue — fetches the issue, classifies it via a
`script`, and (for stale issues) appends a comment + label via a
non-idempotent `block_fn` batch. The `for_each_parallel` cancellation
semantics are exercised incidentally: when one sibling raises a
non-retryable error, the standard errgroup-style cancel-siblings fires.

**Demonstrates:** every primitive (sequential, `block_fn`, `if_cond`,
`script`, `for_each_parallel`, `call_flow`) plus retries, timeouts,
credentials, and (incidental) cancellation.

→ [`issue_triage.star`](issue_triage.star)

### batch_label_issues.star

Block-batch demo with idempotent ops. Lists open issues, filters them
to a smaller candidate list via a `script`, then dynamically builds a
single batch of `gh.add_label` calls inside a `block_fn`. The
underlying activity dispatch executes the batch one-call-per-invocation
because `add_label` is declared non-idempotent at the extension layer
— per `pkg/activity/execute_batch.go`'s ACT-03 contract.

**Demonstrates:** sequential step, `script`, dynamic `block_fn`,
retries, timeouts, credentials.

→ [`batch_label_issues.star`](batch_label_issues.star)

### weekly_digest.star

Aggregation demo. Lists recently-merged PRs (last 7 days), groups them
by author via a `script`, builds per-author summary blocks in parallel
via `for_each_parallel`, and posts a single consolidated digest to a
webhook URL.

**Demonstrates:** sequential step, `script`, `for_each_parallel`,
retries, credentials (`github_token` + `webhook_url`).

→ [`weekly_digest.star`](weekly_digest.star)

## Running the tests

The example ships a Tier-3 `*_test.star` test
(`issue_triage_test.star`) that mocks every action and exercises
attempt-aware retries plus replay determinism. Run it with the example's
binary:

```bash
./extbin test ./
```

Tier-3 tests are hermetic: the mock router intercepts every operation
the flow would dispatch, so no Temporal cluster, no GitHub PAT, and no
webhook URL are needed for the test to run. Each test runs twice
internally — once to capture events, once to replay — and a divergence
between the two runs fails the test (replay-determinism is always-on,
no opt-in needed).

### Mock router pitfall

> When writing your own `*_test.star` files,
> `tester.mock_action(extension="github", op="...")` uses the
> REGISTERED extension name `"github"`, NOT the local variable from
> `gh = github.client(...)`. See
> [`docs/for-flow-authors/testing.md`](../../docs/for-flow-authors/testing.md)
> for the full Tier-3 reference.

Full Tier-3 reference:
[`docs/for-flow-authors/testing.md`](../../docs/for-flow-authors/testing.md).
Tutorial:
[`docs/for-flow-authors/testing-tutorial.md`](../../docs/for-flow-authors/testing-tutorial.md).

## Building your own custom binary

The [`cmd/extbin/main.go`](cmd/extbin/main.go) in this directory is the
canonical implementation of Skytime's two-tier authoring model: import
`pkg/cli`, register your extensions via `cli.WithExtensions(...)`, wire
a credential resolver via `cli.WithCredentialHandler(...)`, and you have
your own customer-specific Skytime binary with `validate` / `run` /
`dev-server` / `test` subcommands. The whole wiring is ~120 lines and
inherits every subcommand from `pkg/cli` for free.

A consultant team specializing for one customer typically ships:

- A `cmd/<customer>-skytime/main.go` that mirrors `cmd/extbin/main.go`
  and registers the extensions that customer's flows need.
- One or more `*.star` flow files specialized to the customer's brief.
- A site-specific credential resolver (file, vault, cloud secrets
  manager — whatever the customer's secret store is).

Full guide: [`docs/cli-binary.md`](../../docs/cli-binary.md).
Credential resolver reference:
[`docs/for-extension-developers/README.md`](../../docs/for-extension-developers/README.md#credential-resolution-pkgextensioncredfile).
