package github

import (
	"encoding/json"
	"fmt"
	stdhttp "net/http"

	"go.starlark.net/starlark"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// githubWebhookSource is the concrete TriggerSource for github.webhook(...).
//
// Lives under examples/http-github-webhook/extensions/github/ (D-07-08:
// source factories live under their owning extension's namespace, not
// in a separate `triggers.*` package).
//
// Signature scheme is LOCKED (TRIG-09 + D-7.1-04):
//   - algo: HMAC-SHA256 (hardcoded; not user-configurable)
//   - header: X-Hub-Signature-256 (hardcoded; GitHub's documented header)
//
// Users who need a different scheme (Stripe, GitLab, custom provider)
// use http.webhook (Plan 02) which exposes the algo and header as
// configurable kwargs.
//
// Embeds extension.TriggerSourceSeal to satisfy the unexported
// triggerSourceMarker() method of extension.TriggerSource — Go scopes
// unexported method names to their declaring package, so external
// packages must use embedding (method promotion) to satisfy the seal.
type githubWebhookSource struct {
	extension.TriggerSourceSeal // satisfies the unexported seal via promotion

	events       []string // empty = match all events (D-7.1-04 default)
	secretCredID string   // empty = unsigned mount (gh webhook forward without --secret)
}

// EXPORTED package constants. Plan 04's pkg/extension/receiver/handler.go
// imports this package and consumes these names directly — keeping them
// exported from this plan onward avoids a later capitalize-and-rewire
// pass when Plan 04 lands.
const (
	GithubWebhookKind            = "github.webhook"
	GithubWebhookPath            = "/webhook/github"     // D-7.1-04: hardcoded path
	GithubWebhookMethod          = stdhttp.MethodPost    // D-7.1-04: POST only
	GithubWebhookSignatureAlgo   = "sha256"              // TRIG-09 lock
	GithubWebhookSignatureHeader = "X-Hub-Signature-256" // TRIG-09 lock
	GithubWebhookEventHeader     = "X-GitHub-Event"      // D-7.1-07
)

// ----- TriggerSource contract -----

func (*githubWebhookSource) Kind() string        { return GithubWebhookKind }
func (*githubWebhookSource) ReqSchema() []string { return []string{"headers", "payload"} }

// Note: triggerSourceMarker() is satisfied via embedded
// extension.TriggerSourceSeal (method promotion). See seal docs in
// pkg/extension/trigger.go.

// MarshalJSON produces the {kind, config} envelope (D-07-09). config
// contains:
//   - events: []string (omitted if empty)
//   - credential_id: string (omitted if empty)
//
// Signature scheme is locked-by-construction (D-7.1-04) and not part
// of the wire shape — the receiver knows it from the kind discriminator.
//
// CRITICAL: NEVER a resolved extension.Secret. The receiver resolves
// the credential JIT inside the request handler (Plan 04).
func (s *githubWebhookSource) MarshalJSON() ([]byte, error) {
	cfg := map[string]any{}
	if len(s.events) > 0 {
		cfg["events"] = s.events
	}
	if s.secretCredID != "" {
		cfg["credential_id"] = s.secretCredID
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`{"kind":%q,"config":%s}`, GithubWebhookKind, string(cfgBytes))), nil
}

// ----- HTTPMounter contract -----

// HTTPMount returns ("/webhook/github", "POST") — locked per D-7.1-04.
// All GitHub webhook deliveries hit one path; fan-out routing (D-7.1-06)
// dispatches to ALL registered triggers for the (kind, path, method).
func (*githubWebhookSource) HTTPMount() (path, method string) {
	return GithubWebhookPath, GithubWebhookMethod
}

// ----- Event filter (D-7.1-07) -----

// ShouldDispatch returns true when this trigger's events filter matches
// the X-GitHub-Event header value. Empty events list matches ALL events.
// Comparison is CASE-SENSITIVE per D-7.1-07 (GitHub's event names are
// canonical lowercase strings — "issues", "pull_request", "push", etc.).
func (s *githubWebhookSource) ShouldDispatch(eventName string) bool {
	if len(s.events) == 0 {
		return true
	}
	for _, e := range s.events {
		if e == eventName {
			return true
		}
	}
	return false
}

// SecretCredID returns the credential ID, or "" when the mount is
// unsigned. Plan 04's receiver reads this via type assertion to determine
// whether to skip signature validation.
func (s *githubWebhookSource) SecretCredID() string { return s.secretCredID }

// ----- starlark.Value contract -----

func (s *githubWebhookSource) String() string {
	return fmt.Sprintf("github.webhook(events=%v)", s.events)
}
func (*githubWebhookSource) Type() string         { return "github.webhook" }
func (*githubWebhookSource) Freeze()              {}
func (*githubWebhookSource) Truth() starlark.Bool { return starlark.True }
func (*githubWebhookSource) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: github.webhook")
}

// Compile-time seal assertions.
var _ extension.TriggerSource = (*githubWebhookSource)(nil)
var _ starlark.Value = (*githubWebhookSource)(nil)

// ----- parser-side factory -----

// webhookFactory implements `github.webhook(events=, secret_credential=)`.
//
// LOCKED contract per TRIG-09 + D-7.1-04:
//   - signature scheme: HMAC-SHA256 against X-Hub-Signature-256 (NOT
//     a kwarg — users cannot override; use http.webhook for custom
//     schemes)
//   - path / method: /webhook/github + POST (NOT a kwarg — GitHub's
//     repo-settings convention uses one URL per webhook target)
func webhookFactory(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		events     *starlark.List
		secretCred string
	)
	if err := starlark.UnpackArgs("github.webhook", args, kwargs,
		"events?", &events,
		"secret_credential?", &secretCred,
	); err != nil {
		return nil, &dag.ParseError{Pos: callerPosition(thread), Msg: "github.webhook: " + err.Error()}
	}

	src := &githubWebhookSource{secretCredID: secretCred}
	if events != nil {
		iter := events.Iterate()
		defer iter.Done()
		var v starlark.Value
		for iter.Next(&v) {
			s, ok := starlark.AsString(v)
			if !ok {
				return nil, &dag.ParseError{
					Pos: callerPosition(thread),
					Msg: fmt.Sprintf("github.webhook: events must be a list of strings, got %s element of type %s", v.String(), v.Type()),
				}
			}
			src.events = append(src.events, s)
		}
	}
	return src, nil
}

// init registers the github.webhook unmarshaler with the kind-keyed
// factory registry (D-07-09). Round-trip path mirrors http.webhook.
func init() {
	_ = extension.RegisterTriggerSourceFactory(GithubWebhookKind, func(data []byte) (extension.TriggerSource, error) {
		var cfg struct {
			Events       []string `json:"events"`
			CredentialID string   `json:"credential_id"`
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("github.webhook unmarshal: %w", err)
		}
		return &githubWebhookSource{
			events:       cfg.Events,
			secretCredID: cfg.CredentialID,
		}, nil
	})
}

// NewGithubWebhookSourceForTest constructs a *githubWebhookSource directly
// for tests in pkg/extension/receiver/ and other downstream packages.
// Production callers use the parser-side factory webhookFactory; this
// constructor bypasses the parser. Test-only — exported only because
// Go has no out-of-package whitelist mechanism.
func NewGithubWebhookSourceForTest(events []string, secretCredID string) extension.TriggerSource {
	return &githubWebhookSource{events: events, secretCredID: secretCredID}
}
