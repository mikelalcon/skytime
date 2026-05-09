package github_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"

	"go.starlark.net/starlark"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	skygh "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/github"
	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/extension/receiver"
)

// callWebhookFactory invokes the github extension's `webhook` module
// attribute against an in-memory thread, mimicking the way Starlark
// would call it from a `.star` file. Returns the Starlark value
// produced by the factory (the *githubWebhookSource), or the surfaced
// error.
func callWebhookFactory(t *testing.T, code string) (starlark.Value, error) {
	t.Helper()

	ext := skygh.New()
	thread := &starlark.Thread{Name: "test-webhook"}
	mod, err := ext.Initialize(thread, nil)
	require.NoError(t, err)

	predeclared := starlark.StringDict{"github": mod}
	globals, err := starlark.ExecFile(thread, "test_webhook.star", code, predeclared)
	if err != nil {
		return nil, err
	}
	v, ok := globals["src"]
	require.True(t, ok, "expected `src = github.webhook(...)` global to be set in test code")
	return v, nil
}

// TestGithubWebhookFactory_Success verifies the happy path: kwargs
// parse cleanly, returned value satisfies extension.TriggerSource AND
// receiver.HTTPMounter, and exposed accessors return the configured
// values.
func TestGithubWebhookFactory_Success(t *testing.T) {
	v, err := callWebhookFactory(t, `src = github.webhook(events=["issues"], secret_credential="gh-secret")`)
	require.NoError(t, err)

	src, ok := v.(extension.TriggerSource)
	require.True(t, ok, "value should satisfy extension.TriggerSource (sealed)")
	assert.Equal(t, "github.webhook", src.Kind())
	assert.Equal(t, []string{"headers", "payload"}, src.ReqSchema())

	mounter, ok := v.(receiver.HTTPMounter)
	require.True(t, ok, "value should satisfy receiver.HTTPMounter")
	path, method := mounter.HTTPMount()
	assert.Equal(t, "/webhook/github", path)
	assert.Equal(t, http.MethodPost, method)

	dispatcher, ok := v.(interface {
		ShouldDispatch(eventName string) bool
		SecretCredID() string
	})
	require.True(t, ok, "value should expose ShouldDispatch + SecretCredID")
	assert.True(t, dispatcher.ShouldDispatch("issues"))
	assert.False(t, dispatcher.ShouldDispatch("push"))
	assert.Equal(t, "gh-secret", dispatcher.SecretCredID())
}

// TestGithubWebhookFactory_DefaultEvents verifies that calling
// github.webhook() with no kwargs is legal: events defaults to []
// (match-all) and secret_credential defaults to "" (unsigned mount,
// useful for `gh webhook forward` without --secret).
func TestGithubWebhookFactory_DefaultEvents(t *testing.T) {
	v, err := callWebhookFactory(t, `src = github.webhook()`)
	require.NoError(t, err)

	dispatcher, ok := v.(interface {
		ShouldDispatch(eventName string) bool
		SecretCredID() string
	})
	require.True(t, ok)
	assert.True(t, dispatcher.ShouldDispatch("anything"), "empty events list should match all events")
	assert.True(t, dispatcher.ShouldDispatch("push"))
	assert.Equal(t, "", dispatcher.SecretCredID())
}

// TestGithubWebhookFactory_RejectsNonStringEvent verifies parse-time
// validation: a non-string element in `events=` returns *dag.ParseError
// with a message mentioning "events" and "string".
func TestGithubWebhookFactory_RejectsNonStringEvent(t *testing.T) {
	_, err := callWebhookFactory(t, `src = github.webhook(events=[123])`)
	require.Error(t, err)

	// Starlark execution may wrap our error; unwrap to a *dag.ParseError.
	var pe *dag.ParseError
	require.True(t, errors.As(err, &pe), "expected *dag.ParseError, got %T: %v", err, err)
	assert.Contains(t, pe.Msg, "events")
	assert.Contains(t, pe.Msg, "string")
}

// TestGithubWebhook_ShouldDispatch_Empty: empty events list matches
// every X-GitHub-Event value (D-7.1-04 default).
func TestGithubWebhook_ShouldDispatch_Empty(t *testing.T) {
	v, err := callWebhookFactory(t, `src = github.webhook(events=[])`)
	require.NoError(t, err)
	dispatcher := v.(interface{ ShouldDispatch(string) bool })
	assert.True(t, dispatcher.ShouldDispatch("issues"))
	assert.True(t, dispatcher.ShouldDispatch("pull_request"))
}

// TestGithubWebhook_ShouldDispatch_Match: populated events list is the
// canonical allowlist; only matches succeed.
func TestGithubWebhook_ShouldDispatch_Match(t *testing.T) {
	v, err := callWebhookFactory(t, `src = github.webhook(events=["issues", "pull_request"])`)
	require.NoError(t, err)
	dispatcher := v.(interface{ ShouldDispatch(string) bool })
	assert.True(t, dispatcher.ShouldDispatch("issues"))
	assert.True(t, dispatcher.ShouldDispatch("pull_request"))
	assert.False(t, dispatcher.ShouldDispatch("push"))
}

// TestGithubWebhook_ShouldDispatch_CaseSensitive: GitHub's documented
// X-GitHub-Event values are canonical lowercase strings (e.g.,
// "issues", not "Issues"). Comparison must be case-sensitive per
// D-7.1-07.
func TestGithubWebhook_ShouldDispatch_CaseSensitive(t *testing.T) {
	v, err := callWebhookFactory(t, `src = github.webhook(events=["issues"])`)
	require.NoError(t, err)
	dispatcher := v.(interface{ ShouldDispatch(string) bool })
	assert.True(t, dispatcher.ShouldDispatch("issues"))
	assert.False(t, dispatcher.ShouldDispatch("Issues"), "comparison must be case-sensitive (D-7.1-07)")
	assert.False(t, dispatcher.ShouldDispatch("ISSUES"))
}

// TestGithubWebhook_NoSecretInConfig: MarshalJSON output carries
// credential ID strings ONLY — never resolved Secret values
// (D-07-09 / D-07-10). Defaults are omitted via omitempty semantics.
func TestGithubWebhook_NoSecretInConfig(t *testing.T) {
	// With both events + credential set: both keys appear, value is the ID string.
	v1, err := callWebhookFactory(t, `src = github.webhook(events=["issues"], secret_credential="my-secret-id")`)
	require.NoError(t, err)
	src1 := v1.(extension.TriggerSource)
	bytes1, err := src1.MarshalJSON()
	require.NoError(t, err)

	var env1 struct {
		Kind   string `json:"kind"`
		Config struct {
			Events       []string `json:"events"`
			CredentialID string   `json:"credential_id"`
		} `json:"config"`
	}
	require.NoError(t, json.Unmarshal(bytes1, &env1))
	assert.Equal(t, "github.webhook", env1.Kind)
	assert.Equal(t, []string{"issues"}, env1.Config.Events)
	assert.Equal(t, "my-secret-id", env1.Config.CredentialID)

	// With both defaulted: neither key appears in the marshaled config (omitempty).
	v2, err := callWebhookFactory(t, `src = github.webhook()`)
	require.NoError(t, err)
	src2 := v2.(extension.TriggerSource)
	bytes2, err := src2.MarshalJSON()
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(bytes2, &raw))
	cfg, ok := raw["config"].(map[string]any)
	require.True(t, ok, "config should be an object")
	_, hasEvents := cfg["events"]
	_, hasCred := cfg["credential_id"]
	assert.False(t, hasEvents, "events key should be omitted when defaulted")
	assert.False(t, hasCred, "credential_id key should be omitted when defaulted")
}

// TestGithubWebhook_TriggerSourceSeal verifies the compile-time seal
// assertion exists in webhook.go (a `var _ extension.TriggerSource =
// (*githubWebhookSource)(nil)` declaration). This test asserts the
// runtime side: a constructed value satisfies the sealed interface.
func TestGithubWebhook_TriggerSourceSeal(t *testing.T) {
	v, err := callWebhookFactory(t, `src = github.webhook()`)
	require.NoError(t, err)
	var _ extension.TriggerSource = v.(extension.TriggerSource) // compile + runtime check
}

// TestGithubWebhook_HTTPMounterAssertion: receiver.HTTPMounter type
// assertion succeeds and the (path, method) values are locked.
func TestGithubWebhook_HTTPMounterAssertion(t *testing.T) {
	v, err := callWebhookFactory(t, `src = github.webhook()`)
	require.NoError(t, err)

	mounter, ok := v.(receiver.HTTPMounter)
	require.True(t, ok)
	path, method := mounter.HTTPMount()
	assert.Equal(t, "/webhook/github", path)
	assert.Equal(t, "POST", method)
}

// TestGithubWebhook_RegisterFactoryRoundTrip verifies that the
// init()-registered factory unmarshals JSON back to a *githubWebhookSource
// preserving events list + credential_id.
func TestGithubWebhook_RegisterFactoryRoundTrip(t *testing.T) {
	v, err := callWebhookFactory(t, `src = github.webhook(events=["issues", "pull_request"], secret_credential="round-trip-id")`)
	require.NoError(t, err)
	src := v.(extension.TriggerSource)

	marshaled, err := src.MarshalJSON()
	require.NoError(t, err)

	// Use dag.Trigger.UnmarshalJSON via a synthetic envelope: the
	// extension package's init() installs the dispatcher into dag,
	// which keys on the {kind, config} envelope.
	wrapped := []byte(`{"flow":"f","position":"f.star:1:1","source":` + string(marshaled) + `,"map_lambda":"l1","idem_lambda":"l2","credential_id":"c"}`)
	var trig dag.Trigger
	require.NoError(t, trig.UnmarshalJSON(wrapped))

	require.NotNil(t, trig.Source)
	assert.Equal(t, "github.webhook", trig.Source.Kind())

	dispatcher, ok := trig.Source.(interface {
		ShouldDispatch(string) bool
		SecretCredID() string
	})
	require.True(t, ok, "round-tripped value should re-expose ShouldDispatch + SecretCredID")
	assert.True(t, dispatcher.ShouldDispatch("issues"))
	assert.True(t, dispatcher.ShouldDispatch("pull_request"))
	assert.False(t, dispatcher.ShouldDispatch("push"))
	assert.Equal(t, "round-trip-id", dispatcher.SecretCredID())
}

// TestGithubWebhook_HardcodedAlgoAndHeader: source-level grep gate
// proving TRIG-09 lock — the algo + header are present as string
// literals AND no `signature_algo` / `signature_header` kwargs are
// exposed. Provides a regression dam against any future refactor
// trying to mirror http.webhook's configurable kwargs.
func TestGithubWebhook_HardcodedAlgoAndHeader(t *testing.T) {
	src, err := os.ReadFile("webhook.go")
	require.NoError(t, err)

	assert.True(t, bytes.Contains(src, []byte(`"sha256"`)),
		`webhook.go must contain the literal "sha256" (TRIG-09 algo lock)`)
	assert.True(t, bytes.Contains(src, []byte(`"X-Hub-Signature-256"`)),
		`webhook.go must contain the literal "X-Hub-Signature-256" (TRIG-09 header lock)`)
	assert.False(t, bytes.Contains(src, []byte(`"signature_algo"`)),
		`webhook.go must NOT expose a signature_algo kwarg (locked, not user-configurable)`)
	assert.False(t, bytes.Contains(src, []byte(`"signature_header"`)),
		`webhook.go must NOT expose a signature_header kwarg (locked, not user-configurable)`)
}

// TestGithubWebhook_ExportedConstants pins the surface Plan 04's
// receiver imports. Any rename here breaks Plan 04's compile.
func TestGithubWebhook_ExportedConstants(t *testing.T) {
	assert.Equal(t, "github.webhook", skygh.GithubWebhookKind)
	assert.Equal(t, "/webhook/github", skygh.GithubWebhookPath)
	assert.Equal(t, http.MethodPost, skygh.GithubWebhookMethod)
	assert.Equal(t, "sha256", skygh.GithubWebhookSignatureAlgo)
	assert.Equal(t, "X-Hub-Signature-256", skygh.GithubWebhookSignatureHeader)
	assert.Equal(t, "X-GitHub-Event", skygh.GithubWebhookEventHeader)
}
