// Package http is the baked-in generic HTTP extension shipped with
// cmd/skytime per D4-14 (Phase 4 CONTEXT.md).
//
// The extension exposes endpoint(base_url=..., credential=...) with
// five operations (get, head, post, put, delete) returning
// HTTPResponse — a typed dag.OperationOutput. Idempotence per D4-14:
// get/head are idempotent, post/put/delete are non-idempotent.
//
// (Note: D4-14 lists put and delete as non-idempotent — this diverges
// from RFC-7231 where PUT and DELETE are idempotent. D4-14 is a locked
// user decision; consultants in Phase 6 will declare per-op idempotence
// for their real GitHub/Slack flows.)
//
// Package is empty in Wave 0; full implementation lands in Wave 4.
package http
