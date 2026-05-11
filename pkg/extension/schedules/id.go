package schedules

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// SkytimeScheduleIDPrefix is the literal namespace prefix the reconciler
// uses to identify Schedule resources it owns. ANY Schedule on the cluster
// whose ID starts with this prefix is considered Skytime-managed and is
// subject to reconciliation (create / update / delete). User-created
// Schedules without this prefix are NEVER touched by the reconciler — this
// is the load-bearing isolation mechanism per D-7.2-05.
const SkytimeScheduleIDPrefix = "skytime/"

// ComposeScheduleID returns the canonical Schedule ID for a cron trigger
// per D-7.2-04:
//
//	"skytime/{flow_name}/{8-char-base64url-sha256(Pos.String())}"
//
// Concrete example: a trigger declared at "weekly_digest.star:5:1" for
// flow "weekly_digest" produces ID "skytime/weekly_digest/<8-char-hash>".
//
// The hash is byte-equal to pkg/extension/receiver/workflow_id.go::posHash
// for the same Pos (see TestIDHashParity). Re-implementation rather than
// import dependency avoids cross-package coupling.
func ComposeScheduleID(t *dag.Trigger) string {
	return SkytimeScheduleIDPrefix + t.FlowName + "/" + posHash(t)
}

// IsSkytimeManaged returns true iff id has the SkytimeScheduleIDPrefix.
// The reconciler filters cluster-side Schedule listings through this
// function so user-created Schedules without the prefix are categorically
// out of scope.
func IsSkytimeManaged(id string) bool {
	return strings.HasPrefix(id, SkytimeScheduleIDPrefix)
}

// posHash returns the 8-char base64url SHA-256 of t.Pos.String().
// Pos.String() is the canonical "<file>:<line>:<col>" form provided by
// go.starlark.net/syntax — stable across Skytime invocations on the
// same .star file. Byte-equal to receiver.posHash (parity test in
// id_test.go).
func posHash(t *dag.Trigger) string {
	sum := sha256.Sum256([]byte(t.Pos.String()))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:8]
}
