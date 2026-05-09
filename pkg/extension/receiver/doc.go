// Package receiver implements the HTTP webhook receiver dispatcher
// for Skytime's trigger system (Phase 7.1).
//
// Scope:
//
//   - signature validation (HMAC-SHA256 / SHA1 / SHA512 against raw body)
//   - JSON body decoding to *starlarkstruct.Struct (req.payload)
//   - fan-out routing: each (source_kind, path, method) tuple maps to ONE
//     handler; matching triggers are dispatched in deterministic sorted
//     order (FlowName, Pos.String) per D-7.1-06
//   - per-trigger map + idempotency_key lambda evaluation against the
//     22-key bridge.TriggerTimeGlobals() set
//   - client.ExecuteWorkflow with WorkflowIDReusePolicy=REJECT_DUPLICATE
//     AND WorkflowExecutionErrorWhenAlreadyStarted=true (CRITICAL — see
//     D-7.1-08 + 07.1-RESEARCH.md § Pitfall 1)
//   - locked HTTP status code mapping (D-7.1-14): 200 ok / 200 duplicate /
//     200 event-filtered / 400 bad_request / 401 unauthorized /
//     415 unsupported_media_type / 500 internal / 502 upstream
//   - one structured slog.Info per request (D-7.1-15)
//
// Out of scope:
//
//   - cron / non-HTTP source dispatch (Phase 7.2 owns)
//   - dashboard + manual trigger UI (Phase 7.3 owns)
//
// Credential redaction invariant: a resolved extension.Secret is unwrapped
// via .Reveal() ONLY for HMAC computation (signature.go::validateHMAC).
// The Secret value never enters the request log line, never enters the
// response body, never enters the error returned upstream, never enters
// dag.Trigger.Source.MarshalJSON. Plan 06 firewall extends targetDirs to
// pkg/extension/receiver to mechanically enforce this with the %+v / %#v
// AST gate.
package receiver
