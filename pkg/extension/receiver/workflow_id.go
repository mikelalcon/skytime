package receiver

import (
	"crypto/sha256"
	"encoding/base64"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// composeWorkflowID returns the Temporal WorkflowID for a single
// (trigger, user-key) dispatch per D-7.1-08:
//
//	"{flow_name}/{trigger_pos_hash}/{user_key}"
//
// trigger_pos_hash is the first 8 chars of base64url(sha256(Pos.String())).
// This 8-char prefix is purely a disambiguator — long enough that two
// triggers in the same .star corpus collide only on a hash collision
// (negligible), short enough that the WorkflowID stays human-readable
// in the dashboard / `temporal workflow list`.
//
// Two triggers fanning out from the same delivery with the same userKey
// produce DIFFERENT WorkflowIDs (different Pos → different hash). Two
// REDELIVERIES of the same webhook for the SAME trigger collide
// (same Pos, same userKey) — this is the desired REJECT_DUPLICATE
// dedup behavior (D-7.1-08, D-7.1-14).
func composeWorkflowID(t *dag.Trigger, userKey string) string {
	return t.FlowName + "/" + posHash(t) + "/" + userKey
}

// posHash returns the 8-char base64url SHA-256 of Trigger.Pos.String().
// Pos.String() is the canonical "<file>:<line>:<col>" form provided by
// go.starlark.net/syntax; stable across Skytime invocations on the
// same .star file.
func posHash(t *dag.Trigger) string {
	sum := sha256.Sum256([]byte(t.Pos.String()))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:8]
}
