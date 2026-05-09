package receiver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	skygh "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/github"
	"github.com/mikelalcon/skytime/pkg/bridge"
	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// maxBodyBytes caps each webhook delivery's body. D-7.1-12 + GitHub's
// documented webhook payload max. Pitfall 9 (DoS defense) — without the
// cap, an attacker could exhaust memory by sending a many-GB body.
const maxBodyBytes int64 = 25 * 1024 * 1024 // 25MB

// makeHandler returns the per-mount http.HandlerFunc for one (kind, path,
// method) tuple. The closed-over trigs slice fans out per delivery
// (D-7.1-06): one delivery → 0..N workflows depending on per-trigger
// event filter (github.webhook ShouldDispatch).
//
// LOCKED 9-step pipeline (D-7.1-14 status mapping + D-7.1-15 log line):
//
//  1. defer-emit log + panic recovery gate (Pitfall 4)
//  2. http.MaxBytesReader(w, r.Body, 25MB) (D-7.1-12 + Pitfall 9)
//  3. io.ReadAll → raw body bytes (Pitfall 2 — HMAC against WIRE bytes)
//  4. JIT credential resolve via deps.CredentialHandler.Resolve
//  5. validateHMAC against the raw body bytes (skipped for unsigned)
//  6. Content-Type check → 415 (writeUnsupportedMediaTypeResponse)
//  7. json.Unmarshal body → map[string]any (400 on parse failure)
//  8. Per-trigger fan-out: event filter → eval lambdas → ExecuteWorkflow
//     with WorkflowExecutionErrorWhenAlreadyStarted=true (Pitfall 1)
//  9. Pick worst status; write JSON response
func makeHandler(key mountKey, trigs []*dag.Trigger, deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec := &requestRecord{
			method:     r.Method,
			path:       r.URL.Path,
			start:      time.Now(),
			sourceKind: key.kind,
			errorClass: errorClassOK,
			status:     http.StatusOK,
		}

		// Pitfall 4 mitigation: defer-recover so panics inside lambda /
		// bridge / credential code don't escape to net/http's default
		// handler (which writes 500 but skips OUR deferred logger).
		// Recover here so the structured log line still emits AND the
		// response is mapped to 500 + lambda_panic.
		defer func() {
			if pv := recover(); pv != nil {
				rec.status = http.StatusInternalServerError
				rec.errorClass = errorClassLambdaPanic
				writeInternalResponse(w, "lambda_panic")
			}
			rec.emit(deps.Logger)
		}()

		// Body cap (D-7.1-12 + Pitfall 9 DoS defense). MaxBytesReader
		// returns an error from Read() when the cap is exceeded; the
		// handler owns the response shape since MaxBytesReader does NOT
		// write a response on its own (it only sets a flag the caller
		// honors).
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			rec.status = http.StatusRequestEntityTooLarge
			rec.errorClass = errorClassBadRequest
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			_, _ = w.Write([]byte(`{"error":"bad_request","detail":"body_too_large"}` + "\n"))
			return
		}

		// Event header — only meaningful for github.webhook.
		eventName := ""
		if key.kind == skygh.GithubWebhookKind {
			eventName = r.Header.Get(skygh.GithubWebhookEventHeader)
			rec.event = eventName
		}

		// Determine signing config from the FIRST trigger (all triggers
		// in a fan-out group share the same mount key + signing scheme
		// per D-7.1-06).
		algo, sigHeader, secretCredID := readSigningConfig(key.kind, trigs[0])
		if secretCredID != "" {
			cred, credErr := deps.CredentialHandler.Resolve(r.Context(), secretCredID)
			if credErr != nil {
				rec.status = http.StatusInternalServerError
				rec.errorClass = errorClassDispatchFailed
				writeInternalResponse(w, "credential_resolve_failed")
				return
			}
			secretBytes, ok := credentialBytes(cred)
			if !ok {
				rec.status = http.StatusInternalServerError
				rec.errorClass = errorClassDispatchFailed
				writeInternalResponse(w, "credential_unsupported_kind")
				return
			}
			headerValue := r.Header.Get(sigHeader)
			if hmacErr := validateHMAC(bodyBytes, secretBytes, algo, headerValue); hmacErr != nil {
				rec.status = http.StatusUnauthorized
				rec.errorClass = errorClassSignatureMismatch
				writeUnauthorizedResponse(w)
				return
			}
		}

		// Content-Type check. D-7.1's Body decoder negotiation locked
		// at planning time: 415 cleaner than partial fall-through.
		// Plan 01's writeUnsupportedMediaTypeResponse writes the
		// correct 415 envelope (NOT writeBadRequestResponse — that
		// would emit 400, which the planner explicitly fixed).
		ct := r.Header.Get("Content-Type")
		if !strings.HasPrefix(ct, "application/json") {
			rec.status = http.StatusUnsupportedMediaType
			rec.errorClass = errorClassBadRequest
			writeUnsupportedMediaTypeResponse(w)
			return
		}

		// Parse body. bodyBytes are the RAW bytes the HMAC was computed
		// against; re-encoding via json.Marshal would change the bytes
		// and break replay verification (Pitfall 2).
		var payload map[string]any
		if jsonErr := json.Unmarshal(bodyBytes, &payload); jsonErr != nil {
			rec.status = http.StatusBadRequest
			rec.errorClass = errorClassBadRequest
			writeBadRequestResponse(w, "json_parse_failed")
			return
		}

		// Build req struct for lambdas: req.payload (recursive struct for
		// dot access — D-7.1-05) + req.headers (Starlark dict for
		// `req.headers["X-..."]` index access — D-7.1-05). The two
		// shapes are distinct on purpose: payload is unbounded JSON
		// the user shapes via dot access, while headers are a flat
		// case-preserving map best accessed via index.
		reqStruct, buildErr := buildReqStruct(payload, r.Header)
		if buildErr != nil {
			rec.status = http.StatusInternalServerError
			rec.errorClass = errorClassLambdaPanic
			writeInternalResponse(w, "req_build_error")
			return
		}

		// Filter triggers by github event (when applicable). Non-matching
		// triggers are silently skipped; sibling triggers proceed.
		matchedTriggers := make([]*dag.Trigger, 0, len(trigs))
		for _, t := range trigs {
			if key.kind == skygh.GithubWebhookKind {
				if gh, ok := t.Source.(interface{ ShouldDispatch(string) bool }); ok {
					if !gh.ShouldDispatch(eventName) {
						continue
					}
				}
			}
			matchedTriggers = append(matchedTriggers, t)
		}

		if len(matchedTriggers) == 0 {
			rec.status = http.StatusOK
			rec.errorClass = errorClassEventFiltered
			writeEventFilteredResponse(w)
			return
		}

		// Fan-out per-trigger. Per-trigger errors do NOT short-circuit
		// siblings (Pitfall 7) — accumulate outcomes, pick worst status
		// at the end.
		var (
			anyOK           bool
			anyDuplicate    bool
			anyDispatchFail bool
			anyInternalFail bool
			workflowIDs     []string
			flowsDispatched []string
			duplicateID     string
		)
		for _, t := range matchedTriggers {
			input, lamErr := evalLambdaToMap(r.Context(), t.MapLambda, reqStruct)
			if lamErr != nil {
				anyInternalFail = true
				continue
			}
			userKey, lamErr := evalLambdaToString(r.Context(), t.IdempotencyLambda, reqStruct)
			if lamErr != nil {
				anyInternalFail = true
				continue
			}
			workflowID := composeWorkflowID(t, userKey)
			opts := client.StartWorkflowOptions{
				ID:                                       workflowID,
				TaskQueue:                                deps.TaskQueue,
				WorkflowIDReusePolicy:                    enumspb.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
				WorkflowExecutionErrorWhenAlreadyStarted: true, // CRITICAL — Pitfall 1
			}
			_, execErr := deps.Client.ExecuteWorkflow(r.Context(), opts, "SkytimeWorkflow", input)
			if execErr == nil {
				anyOK = true
				workflowIDs = append(workflowIDs, workflowID)
				flowsDispatched = append(flowsDispatched, t.FlowName)
				continue
			}
			// Pitfall 6: errors.As with pointer-to-pointer to detect
			// REJECT_DUPLICATE. NOT a string match.
			var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
			if errors.As(execErr, &alreadyStarted) {
				anyDuplicate = true
				duplicateID = workflowID
				continue
			}
			anyDispatchFail = true
		}

		// Pick worst status. Order: dispatch_failed > internal > ok > duplicate.
		switch {
		case anyDispatchFail:
			rec.status = http.StatusBadGateway
			rec.errorClass = errorClassDispatchFailed
			writeUpstreamResponse(w)
		case anyInternalFail:
			rec.status = http.StatusInternalServerError
			rec.errorClass = errorClassLambdaPanic
			writeInternalResponse(w, "starlark_eval_error")
		case anyOK:
			rec.status = http.StatusOK
			rec.errorClass = errorClassOK
			rec.flowsDispatched = flowsDispatched
			rec.workflowIDs = workflowIDs
			writeSuccessResponse(w, workflowIDs)
		case anyDuplicate:
			rec.status = http.StatusOK
			rec.errorClass = errorClassDuplicateSkipped
			rec.workflowIDs = []string{duplicateID}
			writeDuplicateResponse(w, duplicateID)
		default:
			// All triggers were filtered or no outcome set — match the
			// event-filter envelope.
			rec.status = http.StatusOK
			rec.errorClass = errorClassEventFiltered
			writeEventFilteredResponse(w)
		}
	}
}

// readSigningConfig returns (algo, header, secretCredID) for a fan-out
// group. github.webhook has hardcoded algo + header (TRIG-09 lock);
// http.webhook reads them from the source's accessor methods (Plan 04
// added: SignatureAlgo / SignatureHeader / SecretCredID).
//
// All triggers in a group share the SAME signing config because they
// share the SAME mount key (same path + method + kind), and signing
// config is part of the source's identity.
func readSigningConfig(kind string, t *dag.Trigger) (algo, header, secretCredID string) {
	switch kind {
	case skygh.GithubWebhookKind:
		algo = skygh.GithubWebhookSignatureAlgo
		header = skygh.GithubWebhookSignatureHeader
		if g, ok := t.Source.(interface{ SecretCredID() string }); ok {
			secretCredID = g.SecretCredID()
		}
	default:
		// Assume http.webhook (the only other HTTP-shaped source in 7.1).
		if h, ok := t.Source.(interface {
			SignatureAlgo() string
			SignatureHeader() string
			SecretCredID() string
		}); ok {
			algo = h.SignatureAlgo()
			header = h.SignatureHeader()
			secretCredID = h.SecretCredID()
		}
	}
	return
}

// credentialBytes returns the raw HMAC secret bytes for the resolved
// credential.
//
// CRITICAL: this is the SOLE Secret-unwrap leak boundary in handler.go.
// The returned bytes feed validateHMAC ONLY; the caller (makeHandler)
// does NOT log, format, or otherwise serialize them. Plan 08 will
// extend the receiver firewall to AST-walk this file and gate any
// further unwrap additions.
func credentialBytes(cred extension.Credential) ([]byte, bool) {
	switch c := cred.(type) {
	case *extension.BearerCredential:
		// SECURITY: Reveal() result feeds validateHMAC ONLY. Caller
		// does not format/log/error these bytes.
		return []byte(c.Token.Reveal()), true
	case *extension.BasicCredential:
		// SECURITY: same contract as BearerCredential above.
		return []byte(c.Password.Reveal()), true
	case *extension.APIKeyCredential:
		// SECURITY: same contract as BearerCredential above.
		return []byte(c.Key.Reveal()), true
	default:
		return nil, false
	}
}

// buildReqStruct builds the `req` struct passed as the single positional
// arg to MapLambda + IdempotencyLambda. Per D-7.1-05 the struct has two
// fields with distinct Starlark shapes:
//
//   - req.payload  — *starlarkstruct.Struct from the JSON body (recursive
//     via bridge.ToStarlarkStruct), enabling `req.payload.repository
//     .full_name` dot access for arbitrary JSON depth.
//   - req.headers  — *starlark.Dict keyed by Go-canonical header name
//     (http.Header.Set canonicalizes to "X-Github-Delivery"), enabling
//     `req.headers["X-Github-Delivery"]` index access. A struct will NOT
//     work here — Starlark struct attribute names cannot contain hyphens.
//
// We bypass bridge.CallLambda's wrap-state-as-struct convenience because
// it would recursively wrap headers as a struct too, breaking index access.
func buildReqStruct(payload map[string]any, headers http.Header) (*starlarkstruct.Struct, error) {
	payloadStruct, err := bridge.ToStarlarkStruct(payload)
	if err != nil {
		return nil, fmt.Errorf("buildReqStruct: payload: %w", err)
	}

	headerDict := starlark.NewDict(len(headers))
	for k, vv := range headers {
		if len(vv) == 0 {
			continue
		}
		// First value only — webhook providers send single-valued headers
		// in practice. http.Header.Set has already canonicalized k.
		if err := headerDict.SetKey(starlark.String(k), starlark.String(vv[0])); err != nil {
			return nil, fmt.Errorf("buildReqStruct: header %q: %w", k, err)
		}
	}
	headerDict.Freeze()

	sd := starlark.StringDict{
		"payload": payloadStruct,
		"headers": headerDict,
	}
	return starlarkstruct.FromStringDict(starlarkstruct.Default, sd), nil
}

// callLambda runs a CapturedLambda with the supplied positional arg on a
// FRESH starlark.Thread (Pitfall #1 — never reuse threads), with the
// step budget bridge.DefaultMaxExecutionSteps applied (D-22). Print
// output is silently discarded; the receiver is not the place to
// surface lambda print() output (a future plan can wire this).
//
// Bypasses bridge.CallLambda because the bridge's state→struct
// conversion would recursively wrap headers as a struct and break
// index access (see buildReqStruct rationale).
func callLambda(_ context.Context, lam *dag.CapturedLambda, arg starlark.Value) (starlark.Value, error) {
	thread := &starlark.Thread{
		Name:  "skytime-receiver:" + lam.ID,
		Print: func(_ *starlark.Thread, _ string) {},
	}
	thread.SetMaxExecutionSteps(bridge.DefaultMaxExecutionSteps)
	return starlark.Call(thread, lam.Fn, starlark.Tuple{arg}, nil)
}

// evalLambdaToMap evaluates a CapturedLambda expecting a *starlark.Dict
// result. Returns the dict converted to map[string]any. The lambda's
// single positional argument is the req struct (D-7.1-05).
func evalLambdaToMap(ctx context.Context, lam *dag.CapturedLambda, req *starlarkstruct.Struct) (map[string]any, error) {
	v, err := callLambda(ctx, lam, req)
	if err != nil {
		return nil, err
	}
	d, ok := v.(*starlark.Dict)
	if !ok {
		return nil, fmt.Errorf("map lambda must return dict, got %s", v.Type())
	}
	return starlarkDictToMap(d)
}

// evalLambdaToString evaluates a CapturedLambda expecting a string
// result. Same lambda-parameter convention as evalLambdaToMap.
func evalLambdaToString(ctx context.Context, lam *dag.CapturedLambda, req *starlarkstruct.Struct) (string, error) {
	v, err := callLambda(ctx, lam, req)
	if err != nil {
		return "", err
	}
	s, ok := starlark.AsString(v)
	if !ok {
		return "", fmt.Errorf("idempotency_key lambda must return string, got %s", v.Type())
	}
	return s, nil
}

// starlarkDictToMap converts a *starlark.Dict to map[string]any with
// recursive value conversion for nested dicts / lists.
func starlarkDictToMap(d *starlark.Dict) (map[string]any, error) {
	out := make(map[string]any, d.Len())
	for _, item := range d.Items() {
		key, ok := starlark.AsString(item.Index(0))
		if !ok {
			return nil, fmt.Errorf("dict key must be string, got %s", item.Index(0).Type())
		}
		val, err := starlarkValueToGo(item.Index(1))
		if err != nil {
			return nil, fmt.Errorf("dict[%q]: %w", key, err)
		}
		out[key] = val
	}
	return out, nil
}

// starlarkValueToGo converts a starlark.Value to its Go counterpart for
// the workflow-input map. Recursion into nested dicts/lists keeps the
// shape consistent with bridge.ToStarlarkStruct's reverse direction.
func starlarkValueToGo(v starlark.Value) (any, error) {
	switch x := v.(type) {
	case starlark.String:
		return string(x), nil
	case starlark.Int:
		i64, _ := x.Int64()
		return i64, nil
	case starlark.Float:
		return float64(x), nil
	case starlark.Bool:
		return bool(x), nil
	case starlark.NoneType:
		return nil, nil
	case *starlark.List:
		out := make([]any, 0, x.Len())
		iter := x.Iterate()
		defer iter.Done()
		var elem starlark.Value
		for iter.Next(&elem) {
			gv, err := starlarkValueToGo(elem)
			if err != nil {
				return nil, err
			}
			out = append(out, gv)
		}
		return out, nil
	case *starlark.Dict:
		return starlarkDictToMap(x)
	default:
		// Unknown / opaque types: stringify as a fallback so the
		// workflow input remains JSON-encodable.
		return v.String(), nil
	}
}
