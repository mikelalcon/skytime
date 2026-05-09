package receiver

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	skygh "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/github"
	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	skyhttp "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
)

// =============================================================================
// Test fixtures: golden file loader, mock client, mock credential resolver,
// lambda builder.
// =============================================================================

// goldenFixture mirrors the JSON shape of pkg/extension/receiver/testdata/*.golden.
// The X-Hub-Signature-256 / X-Custom-Sig / similar signature header value is
// the placeholder string "COMPUTE_AT_TEST_TIME"; tests substitute the real
// signature at runtime against the loaded body bytes + the test secret.
type goldenFixture struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

func loadGolden(t *testing.T, name string) goldenFixture {
	t.Helper()
	bytes, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err, "load golden %s", name)
	var g goldenFixture
	require.NoError(t, json.Unmarshal(bytes, &g))
	return g
}

// signBody computes "<algo>=<hex_hmac>" against body using secret. Used to
// fill the placeholder signature value in golden fixtures.
func signBody(body []byte, secret []byte, algo string) string {
	var h hash.Hash
	switch algo {
	case "sha256":
		h = hmac.New(sha256.New, secret)
	case "sha512":
		h = hmac.New(sha512.New, secret)
	default:
		panic("signBody: unsupported algo " + algo)
	}
	h.Write(body)
	return algo + "=" + hex.EncodeToString(h.Sum(nil))
}

// mockTemporalClient is a recording client.Client. Tests configure
// returnErr (set to serviceerror.WorkflowExecutionAlreadyStarted on the
// 2nd call with the same ID for redelivery dedup, or a network-error
// for upstream tests). Calls are stored in calls (workflow IDs) and
// startWorkflowOpts (every StartWorkflowOptions seen).
type mockTemporalClient struct {
	client.Client // embedded interface — unused methods nil-panic, fine for tests
	mu            sync.Mutex
	startedIDs    []string
	startedOpts   []client.StartWorkflowOptions
	startedInputs []any
	// errOn returns the desired error for the i-th call (0-indexed). When
	// nil or i >= len(errOn), behaves as success.
	errOn []error
	// rejectDuplicateAfterFirst: if true, after first call with a given
	// WorkflowID the second + later call with the SAME ID returns
	// serviceerror.WorkflowExecutionAlreadyStarted. Models REJECT_DUPLICATE.
	rejectDuplicateAfterFirst bool
	// alwaysErr: if set, every call returns this error regardless of args.
	alwaysErr error
}

func (m *mockTemporalClient) ExecuteWorkflow(_ context.Context, opts client.StartWorkflowOptions, _ any, args ...any) (client.WorkflowRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.alwaysErr != nil {
		return nil, m.alwaysErr
	}

	if m.rejectDuplicateAfterFirst {
		for _, prevID := range m.startedIDs {
			if prevID == opts.ID {
				m.startedOpts = append(m.startedOpts, opts)
				if len(args) > 0 {
					m.startedInputs = append(m.startedInputs, args[0])
				}
				return nil, &serviceerror.WorkflowExecutionAlreadyStarted{
					Message: "workflow already started",
					RunId:   "previous-run-id",
				}
			}
		}
	}

	idx := len(m.startedIDs)
	m.startedIDs = append(m.startedIDs, opts.ID)
	m.startedOpts = append(m.startedOpts, opts)
	if len(args) > 0 {
		m.startedInputs = append(m.startedInputs, args[0])
	}
	if idx < len(m.errOn) && m.errOn[idx] != nil {
		return nil, m.errOn[idx]
	}
	return &fakeRun{id: opts.ID}, nil
}

// snapshot returns copies of the captured fields for race-safe assertion.
func (m *mockTemporalClient) snapshot() (ids []string, opts []client.StartWorkflowOptions, inputs []any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ids = append(ids, m.startedIDs...)
	opts = append(opts, m.startedOpts...)
	inputs = append(inputs, m.startedInputs...)
	return
}

// fakeRun is the minimal client.WorkflowRun returned by the happy path.
type fakeRun struct {
	id string
}

func (r *fakeRun) GetID() string                                                  { return r.id }
func (r *fakeRun) GetRunID() string                                               { return "fake-run-id" }
func (r *fakeRun) Get(_ context.Context, _ any) error                             { return nil }
func (r *fakeRun) GetWithOptions(_ context.Context, _ any, _ client.WorkflowRunGetOptions) error {
	return nil
}

// mockCredentialHandler resolves a fixed map of id → Credential. Used by
// every signed-mount test to feed the receiver the test secret.
type mockCredentialHandler struct {
	creds map[string]extension.Credential
	err   error
}

func (m *mockCredentialHandler) Resolve(_ context.Context, id string) (extension.Credential, error) {
	if m.err != nil {
		return nil, m.err
	}
	c, ok := m.creds[id]
	if !ok {
		return nil, fmt.Errorf("credential not found: %s", id)
	}
	return c, nil
}

// buildCapturedLambda parses a single Starlark expression of the form
// "lambda req: <body>" and returns a *dag.CapturedLambda usable at
// dispatch time. The expression's ONLY free var is req (a struct).
func buildCapturedLambda(t *testing.T, src string) *dag.CapturedLambda {
	t.Helper()
	wrapped := "f = " + src + "\n"
	srcBytes := []byte(wrapped)
	thread := &starlark.Thread{Name: "test:lambda"}
	globals, err := starlark.ExecFile(thread, "lambda.star", srcBytes, nil)
	require.NoError(t, err, "ExecFile lambda src: %s", src)
	fn, ok := globals["f"].(*starlark.Function)
	require.True(t, ok, "globals[f] is not a function")
	id := dag.ComputeLambdaID(srcBytes, fn.Position())
	return &dag.CapturedLambda{
		ID:       id,
		Fn:       fn,
		Pos:      fn.Position(),
		FreeVars: starlark.StringDict{},
	}
}

// trigWithLambdas builds a *dag.Trigger with map + idempotency lambdas
// captured from in-test starlark expressions and a synthetic Pos.
func trigWithLambdas(t *testing.T, flow string, src dag.TriggerSource, p syntax.Position, mapSrc, idemSrc string) *dag.Trigger {
	return &dag.Trigger{
		Pos:               p,
		FlowName:          flow,
		Source:            src,
		MapLambda:         buildCapturedLambda(t, mapSrc),
		IdempotencyLambda: buildCapturedLambda(t, idemSrc),
	}
}

// captureLogger returns a *slog.Logger writing JSON records to buf so
// tests can grep the emitted log line (e.g., assert no secret bytes).
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// runHandler builds a one-trigger handler, runs httptest, returns the
// httptest.ResponseRecorder for assertions.
func runHandler(t *testing.T, kind string, trigs []*dag.Trigger, deps Deps, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	first := trigs[0]
	mounter, ok := first.Source.(HTTPMounter)
	require.True(t, ok, "source must implement HTTPMounter for tests")
	path, method := mounter.HTTPMount()
	key := mountKey{kind: kind, path: path, method: method}
	h := makeHandler(key, trigs, deps)

	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

// =============================================================================
// Tests — 14 named handler tests covering D-7.1-14 status mapping and the
// .Reveal() leak gates.
// =============================================================================

const testSecret = "topsecret"
const testCredID = "gh-secret"

// makeGithubTrigDeps wires a one-trigger github.webhook handler with a
// known credential + map+idempotency lambdas. Returns the trigger slice +
// the configured deps + the mock client (for assertions).
func makeGithubTrigDeps(t *testing.T, events []string, credID string) ([]*dag.Trigger, Deps, *mockTemporalClient) {
	t.Helper()
	src := skygh.NewGithubWebhookSourceForTest(events, credID)
	tr := trigWithLambdas(
		t,
		"issue_triage",
		src,
		pos("a.star", 1),
		`lambda req: {"repo": req.payload.repository.full_name, "issue_number": req.payload.issue.number}`,
		`lambda req: req.headers["X-Github-Delivery"]`, // Go canonicalizes header keys
	)
	mock := &mockTemporalClient{}
	deps := Deps{
		Client: mock,
		CredentialHandler: &mockCredentialHandler{
			creds: map[string]extension.Credential{
				credID: &extension.BearerCredential{ID_: credID, Token: extension.NewSecret(testSecret)},
			},
		},
		TaskQueue: "test-queue",
		Logger:    discardLogger(),
	}
	return []*dag.Trigger{tr}, deps, mock
}

// TestHandler_GitHubValidSignature: signed delivery with correct HMAC →
// 200 + workflow_id; ExecuteWorkflow called once with the locked options.
func TestHandler_GitHubValidSignature(t *testing.T) {
	gold := loadGolden(t, "github_valid_signature.golden")
	trigs, deps, mock := makeGithubTrigDeps(t, []string{"issues"}, testCredID)
	body := []byte(gold.Body)
	gold.Headers["X-Hub-Signature-256"] = signBody(body, []byte(testSecret), "sha256")

	req := httptest.NewRequest(gold.Method, gold.Path, bytes.NewReader(body))
	for k, v := range gold.Headers {
		req.Header.Set(k, v)
	}

	rec := runHandler(t, skygh.GithubWebhookKind, trigs, deps, req)

	require.Equal(t, http.StatusOK, rec.Code, "valid signature should land 200; body=%s", rec.Body.String())
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Contains(t, resp, "workflow_id", "single-trigger response uses 'workflow_id' key")

	ids, opts, _ := mock.snapshot()
	require.Len(t, ids, 1, "exactly 1 ExecuteWorkflow call")
	require.Equal(t, opts[0].ID, ids[0])
	// Pitfall 1 / 6: WorkflowExecutionErrorWhenAlreadyStarted=true is
	// CRITICAL. Without it, REJECT_DUPLICATE returns err==nil silently.
	require.True(t, opts[0].WorkflowExecutionErrorWhenAlreadyStarted,
		"WorkflowExecutionErrorWhenAlreadyStarted MUST be true (Pitfall 1)")
	require.Equal(t, "test-queue", opts[0].TaskQueue)
	// WorkflowID = {flow_name}/{pos_hash}/{user_key} per D-7.1-08.
	require.True(t, strings.HasPrefix(opts[0].ID, "issue_triage/"),
		"WorkflowID prefix from FlowName: %s", opts[0].ID)
	require.True(t, strings.HasSuffix(opts[0].ID, "/abc-123"),
		"WorkflowID suffix from idempotency_key: %s", opts[0].ID)
}

// TestHandler_GitHubBadSignature: deadbeef sig → 401 unauthorized; no
// dispatch; log line error_class=signature_mismatch.
func TestHandler_GitHubBadSignature(t *testing.T) {
	gold := loadGolden(t, "github_bad_signature.golden")
	trigs, deps, mock := makeGithubTrigDeps(t, []string{"issues"}, testCredID)
	logBuf := &bytes.Buffer{}
	deps.Logger = captureLogger(logBuf)

	req := httptest.NewRequest(gold.Method, gold.Path, bytes.NewReader([]byte(gold.Body)))
	for k, v := range gold.Headers {
		req.Header.Set(k, v)
	}

	rec := runHandler(t, skygh.GithubWebhookKind, trigs, deps, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "unauthorized", resp["error"])
	require.NotContains(t, resp, "detail", "401 must NOT include detail field (D-7.1-14)")

	ids, _, _ := mock.snapshot()
	require.Empty(t, ids, "no ExecuteWorkflow on signature mismatch")

	// Verify error_class on log line.
	require.Contains(t, logBuf.String(), `"error_class":"signature_mismatch"`)
}

// TestHandler_GitHubMissingSignature: omitted X-Hub-Signature-256 → 401.
func TestHandler_GitHubMissingSignature(t *testing.T) {
	gold := loadGolden(t, "github_valid_signature.golden")
	trigs, deps, mock := makeGithubTrigDeps(t, []string{"issues"}, testCredID)

	req := httptest.NewRequest(gold.Method, gold.Path, bytes.NewReader([]byte(gold.Body)))
	for k, v := range gold.Headers {
		if k == "X-Hub-Signature-256" {
			continue
		}
		req.Header.Set(k, v)
	}

	rec := runHandler(t, skygh.GithubWebhookKind, trigs, deps, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	ids, _, _ := mock.snapshot()
	require.Empty(t, ids)
}

// TestHandler_UnsignedHTTPWebhook: http.webhook with no secret → no
// validation; 200 + workflow_id; ExecuteWorkflow fires once.
func TestHandler_UnsignedHTTPWebhook(t *testing.T) {
	gold := loadGolden(t, "http_unsigned_post.golden")
	src := skyhttp.NewHTTPWebhookSourceForTest("/hooks/x", "POST", "", "sha256", "X-Signature")
	tr := trigWithLambdas(
		t, "noop_flow", src, pos("u.star", 1),
		`lambda req: {"echo": req.payload.hello}`,
		`lambda req: "key-fixed"`,
	)
	mock := &mockTemporalClient{}
	deps := Deps{
		Client:            mock,
		CredentialHandler: &mockCredentialHandler{},
		TaskQueue:         "test-queue",
		Logger:            discardLogger(),
	}

	req := httptest.NewRequest(gold.Method, gold.Path, bytes.NewReader([]byte(gold.Body)))
	for k, v := range gold.Headers {
		req.Header.Set(k, v)
	}

	rec := runHandler(t, "http.webhook", []*dag.Trigger{tr}, deps, req)

	require.Equal(t, http.StatusOK, rec.Code, "unsigned http.webhook accepts any body; got body=%s", rec.Body.String())
	ids, _, _ := mock.snapshot()
	require.Len(t, ids, 1)
}

// TestSignature_SHA512: http.webhook with sha512 + custom header validates.
func TestSignature_SHA512(t *testing.T) {
	gold := loadGolden(t, "http_signed_sha512.golden")
	src := skyhttp.NewHTTPWebhookSourceForTest("/hooks/sha512", "POST", "sha512-cred", "sha512", "X-Custom-Sig")
	tr := trigWithLambdas(
		t, "sha512_flow", src, pos("s.star", 1),
		`lambda req: {"v": req.payload.value}`,
		`lambda req: "k"`,
	)
	mock := &mockTemporalClient{}
	deps := Deps{
		Client: mock,
		CredentialHandler: &mockCredentialHandler{
			creds: map[string]extension.Credential{
				"sha512-cred": &extension.BearerCredential{ID_: "sha512-cred", Token: extension.NewSecret(testSecret)},
			},
		},
		TaskQueue: "test-queue",
		Logger:    discardLogger(),
	}

	body := []byte(gold.Body)
	gold.Headers["X-Custom-Sig"] = signBody(body, []byte(testSecret), "sha512")

	req := httptest.NewRequest(gold.Method, gold.Path, bytes.NewReader(body))
	for k, v := range gold.Headers {
		req.Header.Set(k, v)
	}

	rec := runHandler(t, "http.webhook", []*dag.Trigger{tr}, deps, req)
	require.Equal(t, http.StatusOK, rec.Code, "sha512 valid signature → 200; body=%s", rec.Body.String())
	ids, _, _ := mock.snapshot()
	require.Len(t, ids, 1)
}

// TestHandler_GitHubEventFilter: trigger configured for events=["issues"]
// receives a pull_request delivery → 200 + event filtered; no dispatch.
func TestHandler_GitHubEventFilter(t *testing.T) {
	gold := loadGolden(t, "github_unconfigured_event.golden")
	trigs, deps, mock := makeGithubTrigDeps(t, []string{"issues"}, testCredID)
	logBuf := &bytes.Buffer{}
	deps.Logger = captureLogger(logBuf)

	body := []byte(gold.Body)
	gold.Headers["X-Hub-Signature-256"] = signBody(body, []byte(testSecret), "sha256")

	req := httptest.NewRequest(gold.Method, gold.Path, bytes.NewReader(body))
	for k, v := range gold.Headers {
		req.Header.Set(k, v)
	}

	rec := runHandler(t, skygh.GithubWebhookKind, trigs, deps, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "event filtered", resp["status"])

	ids, _, _ := mock.snapshot()
	require.Empty(t, ids)
	require.Contains(t, logBuf.String(), `"error_class":"event_filtered"`)
}

// TestHandler_RedeliveryDedup: same X-GitHub-Delivery sent twice. First
// → 200 + workflow_id. Second → 200 + duplicate; skipped (errors.As
// detects WorkflowExecutionAlreadyStarted per Pitfall 6).
func TestHandler_RedeliveryDedup(t *testing.T) {
	gold := loadGolden(t, "github_redelivery.golden")
	trigs, deps, mock := makeGithubTrigDeps(t, []string{"issues"}, testCredID)
	mock.rejectDuplicateAfterFirst = true
	logBuf := &bytes.Buffer{}
	deps.Logger = captureLogger(logBuf)

	body := []byte(gold.Body)
	gold.Headers["X-Hub-Signature-256"] = signBody(body, []byte(testSecret), "sha256")

	doDeliver := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(gold.Method, gold.Path, bytes.NewReader(body))
		for k, v := range gold.Headers {
			req.Header.Set(k, v)
		}
		return runHandler(t, skygh.GithubWebhookKind, trigs, deps, req)
	}

	// First delivery: success.
	rec1 := doDeliver()
	require.Equal(t, http.StatusOK, rec1.Code)
	var first map[string]any
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &first))
	require.Contains(t, first, "workflow_id")
	firstID := first["workflow_id"].(string)

	// Second delivery (same idempotency_key → same WorkflowID).
	rec2 := doDeliver()
	require.Equal(t, http.StatusOK, rec2.Code)
	var second map[string]any
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &second))
	require.Equal(t, "duplicate; skipped", second["status"])
	require.Equal(t, firstID, second["workflow_id"])

	require.Contains(t, logBuf.String(), `"error_class":"duplicate_skipped"`)
}

// TestHandler_FanOutDifferentWorkflowIDs: two triggers same flow, same
// events filter, DIFFERENT positions → ONE delivery, TWO dispatches with
// different WorkflowIDs (different pos hashes per D-7.1-08).
func TestHandler_FanOutDifferentWorkflowIDs(t *testing.T) {
	gold := loadGolden(t, "fan_out_two_flows.golden")
	src1 := skygh.NewGithubWebhookSourceForTest([]string{"issues"}, testCredID)
	src2 := skygh.NewGithubWebhookSourceForTest([]string{"issues"}, testCredID)
	mapSrc := `lambda req: {"repo": req.payload.repository.full_name}`
	idemSrc := `lambda req: req.headers["X-Github-Delivery"]`
	tr1 := trigWithLambdas(t, "demo", src1, pos("a.star", 5), mapSrc, idemSrc)
	tr2 := trigWithLambdas(t, "demo", src2, pos("a.star", 12), mapSrc, idemSrc) // DIFFERENT line → different pos hash

	mock := &mockTemporalClient{}
	deps := Deps{
		Client: mock,
		CredentialHandler: &mockCredentialHandler{
			creds: map[string]extension.Credential{
				testCredID: &extension.BearerCredential{ID_: testCredID, Token: extension.NewSecret(testSecret)},
			},
		},
		TaskQueue: "test-queue",
		Logger:    discardLogger(),
	}

	body := []byte(gold.Body)
	gold.Headers["X-Hub-Signature-256"] = signBody(body, []byte(testSecret), "sha256")

	req := httptest.NewRequest(gold.Method, gold.Path, bytes.NewReader(body))
	for k, v := range gold.Headers {
		req.Header.Set(k, v)
	}

	rec := runHandler(t, skygh.GithubWebhookKind, []*dag.Trigger{tr1, tr2}, deps, req)
	require.Equal(t, http.StatusOK, rec.Code, "body=%s", rec.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	wfIDsAny, ok := resp["workflow_ids"]
	require.True(t, ok, "fan-out (n>=2) uses 'workflow_ids' key (plural)")
	wfIDs, ok := wfIDsAny.([]any)
	require.True(t, ok)
	require.Len(t, wfIDs, 2, "two triggers → two workflow_ids")
	require.NotEqual(t, wfIDs[0], wfIDs[1], "two triggers MUST produce different WorkflowIDs (D-7.1-08 pos hash)")

	ids, _, _ := mock.snapshot()
	require.Len(t, ids, 2)
}

// TestHandler_BodySizeLimit: 26MB body to a signed mount → MaxBytesReader
// rejects. ExecuteWorkflow MUST NOT fire.
func TestHandler_BodySizeLimit(t *testing.T) {
	trigs, deps, mock := makeGithubTrigDeps(t, []string{"issues"}, testCredID)

	// 26 MB of zeros — exceeds the 25MB cap.
	bigBody := bytes.Repeat([]byte{'a'}, 26*1024*1024)

	req := httptest.NewRequest("POST", "/webhook/github", bytes.NewReader(bigBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "huge")
	req.Header.Set("X-Hub-Signature-256", "sha256=ignored") // never reached

	rec := runHandler(t, skygh.GithubWebhookKind, trigs, deps, req)
	// MaxBytesReader writes 413 (or the handler's status will reflect
	// that the body could not be read); the key invariant is no dispatch.
	require.NotEqual(t, http.StatusOK, rec.Code, "oversized body must NOT 200")
	ids, _, _ := mock.snapshot()
	require.Empty(t, ids, "no dispatch on body-size violation")
}

// TestHandler_MalformedJSON: signed delivery with `{not valid json}`
// body → 400 + bad_request + safe detail (no payload echo).
func TestHandler_MalformedJSON(t *testing.T) {
	trigs, deps, mock := makeGithubTrigDeps(t, []string{"issues"}, testCredID)

	body := []byte(`{not valid json}`)
	req := httptest.NewRequest("POST", "/webhook/github", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "malformed-1")
	req.Header.Set("X-Hub-Signature-256", signBody(body, []byte(testSecret), "sha256"))

	rec := runHandler(t, skygh.GithubWebhookKind, trigs, deps, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "bad_request", resp["error"])
	require.Contains(t, resp, "detail")
	// Detail must not echo payload bytes.
	require.NotContains(t, resp["detail"], "{not valid json")
	require.NotContains(t, rec.Body.String(), "{not valid json")

	ids, _, _ := mock.snapshot()
	require.Empty(t, ids)
}

// TestHandler_NonJSONContentType: Content-Type=application/xml → 415
// unsupported_media_type. THIS TEST PINS THE BUG FIX — the planner
// noticed a path that would have called writeBadRequestResponse (400)
// instead of writeUnsupportedMediaTypeResponse (415).
func TestHandler_NonJSONContentType(t *testing.T) {
	src := skyhttp.NewHTTPWebhookSourceForTest("/hooks/x", "POST", "", "sha256", "X-Signature")
	tr := trigWithLambdas(t, "noop", src, pos("z.star", 1),
		`lambda req: {}`, `lambda req: "k"`)

	mock := &mockTemporalClient{}
	deps := Deps{
		Client:            mock,
		CredentialHandler: &mockCredentialHandler{},
		TaskQueue:         "test-queue",
		Logger:            discardLogger(),
	}

	req := httptest.NewRequest("POST", "/hooks/x", strings.NewReader("<xml/>"))
	req.Header.Set("Content-Type", "application/xml")

	rec := runHandler(t, "http.webhook", []*dag.Trigger{tr}, deps, req)
	require.Equal(t, http.StatusUnsupportedMediaType, rec.Code,
		"non-JSON Content-Type must return 415 (NOT 400) — bug fix from plan checking")

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "unsupported_media_type", resp["error"])
	require.NotContains(t, resp, "detail",
		"415 envelope from writeUnsupportedMediaTypeResponse omits detail (D-7.1-14)")

	ids, _, _ := mock.snapshot()
	require.Empty(t, ids)
}

// TestHandler_TemporalUnreachable: ExecuteWorkflow returns a network
// error → 502 upstream + temporal_unavailable detail; log line records
// dispatch_failed.
func TestHandler_TemporalUnreachable(t *testing.T) {
	trigs, deps, mock := makeGithubTrigDeps(t, []string{"issues"}, testCredID)
	mock.alwaysErr = errors.New("rpc error: code = Unavailable")
	logBuf := &bytes.Buffer{}
	deps.Logger = captureLogger(logBuf)

	gold := loadGolden(t, "github_valid_signature.golden")
	body := []byte(gold.Body)
	gold.Headers["X-Hub-Signature-256"] = signBody(body, []byte(testSecret), "sha256")

	req := httptest.NewRequest(gold.Method, gold.Path, bytes.NewReader(body))
	for k, v := range gold.Headers {
		req.Header.Set(k, v)
	}

	rec := runHandler(t, skygh.GithubWebhookKind, trigs, deps, req)
	require.Equal(t, http.StatusBadGateway, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "upstream", resp["error"])
	require.Equal(t, "temporal_unavailable", resp["detail"])

	require.Contains(t, logBuf.String(), `"error_class":"dispatch_failed"`)
}

// TestHandler_LambdaError: map lambda returns a non-dict (just a string
// "foo") → 500 internal; the underlying starlark error class is set on
// the log line; the user payload does NOT leak into response.
func TestHandler_LambdaError(t *testing.T) {
	src := skygh.NewGithubWebhookSourceForTest([]string{"issues"}, testCredID)
	// map returns a STRING, not a dict — handler must detect and 500.
	tr := trigWithLambdas(
		t, "issue_triage", src, pos("a.star", 1),
		`lambda req: "not a dict"`,
		`lambda req: req.headers["X-Github-Delivery"]`,
	)
	mock := &mockTemporalClient{}
	deps := Deps{
		Client: mock,
		CredentialHandler: &mockCredentialHandler{
			creds: map[string]extension.Credential{
				testCredID: &extension.BearerCredential{ID_: testCredID, Token: extension.NewSecret(testSecret)},
			},
		},
		TaskQueue: "test-queue",
		Logger:    discardLogger(),
	}

	gold := loadGolden(t, "github_valid_signature.golden")
	body := []byte(gold.Body)
	gold.Headers["X-Hub-Signature-256"] = signBody(body, []byte(testSecret), "sha256")

	req := httptest.NewRequest(gold.Method, gold.Path, bytes.NewReader(body))
	for k, v := range gold.Headers {
		req.Header.Set(k, v)
	}

	rec := runHandler(t, skygh.GithubWebhookKind, []*dag.Trigger{tr}, deps, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "internal", resp["error"])
	require.Contains(t, resp, "detail")
	// Payload bytes must NOT leak into response — must NOT contain the
	// repository.full_name / issue.number values.
	require.NotContains(t, rec.Body.String(), "oct/test")
	require.NotContains(t, rec.Body.String(), "topsecret")

	ids, _, _ := mock.snapshot()
	require.Empty(t, ids, "no dispatch when map lambda fails")
}

// TestHandler_NoSecretInLogOrResponse: defense-in-depth — for the valid
// signature happy path, the resolved secret value MUST appear in
// neither the log line NOR the response body.
func TestHandler_NoSecretInLogOrResponse(t *testing.T) {
	gold := loadGolden(t, "github_valid_signature.golden")
	trigs, deps, _ := makeGithubTrigDeps(t, []string{"issues"}, testCredID)
	logBuf := &bytes.Buffer{}
	deps.Logger = captureLogger(logBuf)

	body := []byte(gold.Body)
	gold.Headers["X-Hub-Signature-256"] = signBody(body, []byte(testSecret), "sha256")

	req := httptest.NewRequest(gold.Method, gold.Path, bytes.NewReader(body))
	for k, v := range gold.Headers {
		req.Header.Set(k, v)
	}

	rec := runHandler(t, skygh.GithubWebhookKind, trigs, deps, req)
	require.Equal(t, http.StatusOK, rec.Code, "happy path; body=%s", rec.Body.String())

	// Defense in depth: secret bytes ("topsecret") must not leak into
	// the response body OR the log line.
	assert.NotContains(t, rec.Body.String(), testSecret,
		"resolved secret MUST NOT appear in response body")
	assert.NotContains(t, logBuf.String(), testSecret,
		"resolved secret MUST NOT appear in log line")
}

// =============================================================================
// helpers — body-bytes hash for invariant checking
// =============================================================================

// readAllOrPanic helps inspect body bytes during ad-hoc debugging; not
// used in shipped assertions but kept for parity with how we build
// signatures.
func readAllOrPanic(r io.Reader) []byte {
	b, err := io.ReadAll(r)
	if err != nil {
		panic(err)
	}
	return b
}
