---
phase: 6
slug: example-project-http-github-webhook
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-05-05
---

# Phase 6 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
>
> **Substantive content lives in `06-RESEARCH.md` § "Validation Architecture (Nyquist VALIDATION.md driver)"** —
> this document is the live tracker; the planner fills the per-task map below from PLAN.md frontmatter.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `testify/{require,assert}` v1.11.1 (existing) + Tier-3 `extbin test` for `*_test.star` |
| **Config file** | none — Go stdlib auto-discovers `*_test.go`; `*_test.star` files run via `extbin test` (CLI-03) |
| **Quick run command** | `go test -race -count=1 ./pkg/extension/credfile/... ./examples/http-github-webhook/...` |
| **Full suite command** | `go test -race -count=1 ./...` |
| **Estimated runtime** | ~10s quick / ~2–3min full suite |

---

## Sampling Rate

- **After every task commit:** Run `go test -race -count=1 ./pkg/extension/credfile/... ./examples/http-github-webhook/...`
- **After every plan wave:** Run `go test -race -count=1 ./...`
- **Before `/gsd:verify-work`:** Full suite green + `extbin test ./examples/http-github-webhook/` green + walkthrough smoke green
- **Max feedback latency:** ~10 seconds (quick run); ~3 minutes (full suite)

---

## Per-Task Verification Map

> Filled by planner from PLAN.md task IDs. RESEARCH.md § Validation Architecture has the per-requirement test map already drafted; this table is the per-task projection.

| Task ID | Plan | Wave | Requirement | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|-----------|-------------------|-------------|--------|
| {N}-01-01 | 01 | 0 | EX-04 | unit | `go test -race ./pkg/extension/credfile -run TestResolver_HappyPath_BearerCredential` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

> Files that MUST exist before Wave 1 tasks can verify against them. Pulled from RESEARCH.md § Validation Architecture > "Wave 0 Gaps".

- [ ] `pkg/extension/credfile/doc.go` — package overview
- [ ] `pkg/extension/credfile/resolver.go` — `Resolver`, `New`, `Resolve` (CredentialHandler impl)
- [ ] `pkg/extension/credfile/options.go` — `Option`, `WithPath`, `WithStrictMode`, `WithLogger`
- [ ] `pkg/extension/credfile/file.go` — `fileShape`, `credentialEntry`, `buildCredentials`
- [ ] `pkg/extension/credfile/resolver_test.go` — table-driven coverage
- [ ] `examples/http-github-webhook/extensions/github/{github.go,response.go,*_test.go}`
- [ ] `examples/http-github-webhook/extensions/webhook/{webhook.go,*_test.go}`
- [ ] `examples/http-github-webhook/cmd/extbin/main.go`
- [ ] Five `.star` flows + one `*_test.star` under `examples/http-github-webhook/`
- [ ] `examples/http-github-webhook/README.md`
- [ ] `examples/http-github-webhook/.skytime-credentials.example`
- [ ] `.github/workflows/ci.yml`
- [ ] `pelletier/go-toml/v2 v2.3.1` and `google/go-github/v78 v78.0.0` added via `go get` + `go mod tidy`

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Reader can `git clone` → run a flow under five commands without prior project knowledge | EX-04 | Five-commands UX is a human-onboarding contract; CI smoke can prove the commands return 0, but cannot judge "comprehensible to a fresh reader" | Have a teammate (or `/gsd:verify-work` UAT) follow the README walkthrough cold from a fresh checkout, time it, and report blockers. |
| `webhook.post` to `webhook.site` produces a visible event in the browser-rendered inbox | EX-01 | webhook.site is browser-only; no programmatic readback in CI | Run `extbin run examples/http-github-webhook/webhook_notify.star`, open the printed `webhook.site` URL, confirm event landed. |
| README's primitive coverage matrix is materially correct (every primitive used in at least one flow as claimed) | EX-02 | Cross-check between human-written matrix and DAG node types is partly automated (`TestFlows_CoverageMatrix`); the table-text correctness vs DAG truth is human-readable | After Wave 2, eyeball matrix vs flow files. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < ~10s (quick) / ~3min (full)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
