package extension

import (
	"fmt"
)

// redactedString is the single replacement value every Secret formatter uses.
// Defined once so a future audit can `git grep '"<redacted>"'` and find every
// emission point.
const redactedString = "<redacted>"

// Secret wraps a string secret. All standard fmt and encoding interfaces
// redact to "<redacted>" — the only path to the raw value is .Reveal().
//
// AUDIT: every call site of .Reveal() is a "secret leaves type protection"
// boundary. Code review should treat each one as load-bearing. A future
// linter (post-v1) can flag .Reveal() calls outside an approved sink list.
//
// Decision reference: D2-08 (locked) — Option C, the Secret wrapper that
// closes the %+v / %#v / json / slog-text-handler leak surface.
//
// Decision reference: D2-09 — Skytime relies entirely on type-level
// protection in v1; there is no regex error scrubber. The wrapper's
// formatter coverage IS the defense.
type Secret struct {
	value string
}

// NewSecret wraps a raw string. Constructor is the ONLY path into a Secret;
// the underlying field is unexported so external callers cannot bypass the
// formatter coverage by reaching in.
func NewSecret(raw string) Secret { return Secret{value: raw} }

// String returns "<redacted>" — covers %s, %v, fmt.Stringer, fmt.Sprint(s),
// fmt.Println(s).
func (s Secret) String() string { return redactedString }

// GoString returns "<redacted>" — covers %#v.
func (s Secret) GoString() string { return redactedString }

// MarshalJSON returns the JSON string "<redacted>" — covers encoding/json,
// which is Temporal's default DataConverter. Without this, a Credential
// containing a Secret could leak the raw bytes into Temporal history when
// serialized.
func (s Secret) MarshalJSON() ([]byte, error) {
	return []byte(`"<redacted>"`), nil
}

// MarshalText returns []byte("<redacted>") — covers encoding.TextMarshaler,
// which slog's text handler uses for log values. Without this, a slog.Info
// call passing a Secret-bearing struct could leak the raw bytes through the
// text handler's reflection path.
func (s Secret) MarshalText() ([]byte, error) {
	return []byte(redactedString), nil
}

// Format implements fmt.Formatter so %+v (which bypasses String() to print
// struct field names) ALSO redacts. Without this, code like
//
//	slog.Info("auth", "cred", cred)
//
// could format the surrounding struct via %+v and reveal s.value.
//
// All verbs (%s, %v, %+v, %#v, %q, %d, %x, ...) route through Format and
// emit the redacted string. The verb argument is intentionally ignored —
// no verb produces the raw value.
func (s Secret) Format(state fmt.State, verb rune) {
	fmt.Fprint(state, redactedString)
}

// Reveal returns the raw secret. EVERY call site is a leak boundary —
// audit accordingly. Greppable via `git grep '\.Reveal()'`.
func (s Secret) Reveal() string { return s.value }
