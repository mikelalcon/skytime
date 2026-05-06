# Phase 6: Example Project (HTTP + GitHub + Webhook) - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-06
**Phase:** 06-example-project-http-github-webhook
**Areas discussed:** Extension architecture, Credential handling, Slack non-idempotency demo (resolved via swap to Webhook), Flow inventory + walkthrough, CI on push (added during wrap-up)

---

## Gray-Area Selection

**Question:** Which gray areas do you want to discuss for Phase 6 (Example Project)?

| Option | Description | Selected |
|--------|-------------|----------|
| Extension architecture | Where do GitHub + Slack extensions live? In pkg/extension/builtin/ (baked into stdlib skytime binary), OR in examples/http-github-slack/cmd/extbin/ (custom-binary pattern from docs/cli-binary.md). | ✓ |
| Credential handling | Authenticated GitHub + Slack APIs need real secrets. Options: env-var resolver, local .credentials file, public APIs only (no auth), or some hybrid. | ✓ |
| Slack non-idempotency demo | What's the actual demo: real bot token + dedicated test channel, incoming-webhook flavor, log-only fake, mock-only-in-tests? | ✓ |
| Flow inventory + walkthrough | Which 4-6 flows ship and which one is THE README demo? | ✓ |

**User's choice:** All four selected.

---

## Extension Architecture

**Question:** Where should the GitHub + Slack extensions live?

| Option | Description | Selected |
|--------|-------------|----------|
| In example custom binary | examples/http-github-slack/cmd/extbin/ contains a main.go that imports pkg/cli + registers GitHub + Slack alongside the bundled HTTP. Standard skytime binary stays minimal. | |
| In pkg/extension/builtin/, baked into skytime | GitHub + Slack live next to HTTP under pkg/extension/builtin/{github,slack}/. cmd/skytime/main.go grows to register all three. | |
| Hybrid: HTTP baked in, GitHub+Slack in example | HTTP stays in pkg/extension/builtin/ (already does), GitHub+Slack live in examples/http-github-slack/extensions/{github,slack}/, and the example's cmd/extbin imports all three. | ✓ |

**User's choice:** Hybrid. HTTP stays universal; domain extensions are example-local. Maps cleanly to real consultant work.

**Notes:** This locks the architecture for the example: `examples/http-github-webhook/extensions/{github,webhook}/` (after the Slack→Webhook swap below).

---

## Credential Handling

**Question:** How should the example handle credentials for authenticated GitHub + Slack APIs?

| Option | Description | Selected |
|--------|-------------|----------|
| Env-var resolver, public-API demo flow | Example ships an EnvVarCredentialHandler. README demo uses GitHub's PUBLIC API; authenticated flows require env vars. | |
| Env-var resolver, all flows require credentials | All flows authenticated; reader must obtain GitHub + Slack tokens before any flow runs. | |
| Local file resolver | Reads from a .skytime-credentials file in JSON or TOML. .gitignored. | ✓ (with amendments) |
| Mock-only resolver | All extensions mocked. Demo runs entirely under `skytime test`. | |

**User's choice:** Local file resolver, but with two amendments:
1. The resolver should be a **library component** so other consultants can use it without reimplementing.
2. File location should be **configurable**, default to `$HOME/.skytime-credentials`.

**Notes:** This expanded scope to include a new library package (`pkg/extension/credfile/`).

---

### Credential Resolver — Package Location (follow-up)

**Question:** Where should the file-based credential resolver live in the package layout?

| Option | Description | Selected |
|--------|-------------|----------|
| pkg/extension/credfile/ | Sits next to pkg/extension/credential.go and handler.go. Mirrors the sibling-pattern of pkg/extension/builtin/http/. | ✓ |
| pkg/credentials/file/ | New top-level package. Cleaner if more credential-resolver implementations are coming. | |
| pkg/cli/credfile/ | Closer to where consultants wire it up. Mismatched layering. | |

**User's choice:** `pkg/extension/credfile/`.

---

### Credential Resolver — File Format (follow-up)

**Question:** What file format should the credential resolver use?

| Option | Description | Selected |
|--------|-------------|----------|
| JSON with explicit type tag | Schema with bearer/basic/apikey type tags, mode-0600 check, matches Skytime's JSON-everywhere stance. | |
| JSON, flat string-only | Bearer-only, simpler. Loses Basic/APIKey support. | |
| TOML with explicit type tag | Same shape but TOML. Better for hand-editing. Requires new dep. | ✓ |

**User's choice:** TOML with explicit type tag. TOML parser library choice deferred to research (BurntSushi/toml vs pelletier/go-toml/v2).

---

## Slack Non-Idempotency Demo

**Question:** How should the Slack chat.postMessage non-idempotency demo actually work end-to-end?

| Option | Description | Selected |
|--------|-------------|----------|
| Real bot token + dedicated test channel | Most realistic. Reader creates Slack app + bot + channel. | |
| Incoming-webhook URL only | Simpler than full bot API. Reader creates webhook in ~5 clicks. | |
| Print-only fake | Logs to stdout instead of posting. Defeats the purpose. | |
| Mock-only-in-tests | Slack extension stub; only Tier-3 tests verify non-idempotency. | |
| **None of the above — swap Slack out** | User asked: can we use something else? Don't have/want a Slack account. | ✓ |

**User's choice:** None of the above — replace Slack entirely. Slack workspace setup is real friction.

---

### Slack Replacement (follow-up)

**Question:** What should replace Slack as the third extension?

| Option | Description | Selected |
|--------|-------------|----------|
| webhook.site (zero setup) | Generic webhook extension that POSTs to any URL. Demonstrated against webhook.site. Zero account creation. | ✓ |
| Discord webhooks | Free Discord account + ~5 min setup. Familiar service for devs. | |
| Local file append | Zero setup but feels artificial. | |
| Drop the 3rd extension entirely | Two extensions; cover non-idempotency via github.add_comment instead. | |

**User's choice:** webhook.site. Generic webhook extension is broader-purpose (Discord/Slack/Teams webhooks all work). Roadmap edit applied: example dir renamed to `http-github-webhook`, EX-01 reworded, success criterion 3 reworded.

---

## Flow Inventory

**Question:** Does the 5-flow + 1-test inventory look right, or do you want a different shape?

Initial proposal: `public_repo_check`, `pr_to_webhook`, `issue_triage`, `batch_label_issues`, `cancellation_demo` + `issue_triage_test`.

| Option | Description | Selected |
|--------|-------------|----------|
| Lock the 5-flow inventory as proposed | 5 flows + 1 test. cancellation_demo dedicated to the cancellation concern. | |
| Trim to 4 flows (drop cancellation_demo) | Cancellation covered incidentally by for_each_parallel + nonretryable elsewhere. | (partial) |
| Expand to 6 flows | Add weekly_digest. | (partial) |
| **Different lineup — I'll describe** | User-specified: drop cancellation, add weekly_digest, examples must be very readable, don't abuse comments. | ✓ |

**User's choice:** Mix of "drop cancellation" + "add weekly_digest" + style guidance ("very readable, don't abuse comments").

**Notes:**
- Final lineup: `public_repo_check`, `pr_to_webhook`, `issue_triage`, `batch_label_issues`, `weekly_digest` + `issue_triage_test`.
- Cancellation is covered incidentally — `issue_triage`'s `for_each_parallel` cancels siblings on a non-retryable failure (standard errgroup-style semantics, exercised when the test's mock returns nonretryable).
- D-FLOWS-STYLE captures the "very readable, don't abuse comments" guidance as a load-bearing convention for the example flow files and the README.

---

## Wrap-Up — Additional Decisions

**Question:** Couple of remaining decisions — anything you want to weigh in on, or claude's discretion?

| Option | Description | Selected |
|--------|-------------|----------|
| Keep examples/skeleton/ alongside http-github-webhook/ | skeleton stays minimal; http-github-webhook is rich. | (Claude's discretion — recorded as D-COEXIST: keep both) |
| Custom-binary name = `extbin` | examples/http-github-webhook/cmd/extbin/. | (Claude's discretion — kept as `extbin`) |
| CI smoke test as Go subprocess test in tests/ | Mirrors existing tests/skytime_test_e2e_test.go pattern. | (Subsumed by user's CI request below) |
| **I have something else — describe** | User added: GitHub Action that runs project tests + .star tests on push (no PRs). | ✓ |

**User's input (free text):** "we want a GH action that run in the project itself that test the code and example star test files on push. So that we know the project is green. Not necessary for PRs for now"

**Notes:** Triggered the CI design follow-up below.

---

### CI Design (follow-up)

**Question:** GitHub Actions CI on push — confirm the design?

| Option | Description | Selected |
|--------|-------------|----------|
| Single job, Linux only, push to any branch | .github/workflows/ci.yml runs vet + race tests + Tier-3 .star tests + EX-04 walkthrough smoke. | ✓ |
| Single job, push to main only | Same but main-only. | |
| Matrix: Linux + macOS, all branches | Ubuntu + macOS runners. Doubles CI minutes. | |
| Different design — describe | Different structure (separate jobs, scheduled cron, etc.). | |

**User's choice:** Single job, Linux only, push to any branch.

**Notes:** Locked as decisions D-CI-FILE through D-CI-WALL in CONTEXT.md. Authenticated flows + webhook flows excluded from CI (no secrets); Tier-3 mocks cover them logically.

---

## Claude's Discretion

Items where the user explicitly deferred (or did not press for a specific choice):

- TOML parser library (BurntSushi/toml vs pelletier/go-toml/v2) — researcher
- Internal package layout under examples/http-github-webhook/extensions/{github,webhook}/ — planner
- Exact GitHub op set in the new extension (subject to flows' actual needs) — researcher
- README primitive-coverage matrix exact format — planner
- CI smoke-test exit-condition assertion form (string match vs JSON parse vs exit code) — planner
- Single CI job vs split jobs internally — planner (D-CI-FILE keeps single yaml file)
- File-mode strict-mode wording for credfile — researcher

## Deferred Ideas

- `webhook.put` operation — v2
- macOS/Windows CI matrix — v2
- Strict mode for credfile file-mode check — v2 polish (warn-only by default; opt-in)
- `golangci-lint` in CI — v2 (avoid baseline noise from prior phases)
- Vault / 1Password / cloud credential resolvers — v2 (filesystem variant only in v1)
- Cross-file `load()` in `*_test.star` — pre-existing v2 deferral from Phase 5
- Slack-flavored extension — replaced by Webhook for v1 (additive in v2 if requested)
- PR triggers for CI — explicitly out of scope; push-only for v1
