package receiver

// HTTPMounter is implemented by extension.TriggerSource concrete types
// that mount an HTTP handler. The receiver type-asserts at boot-walk
// time: HTTP-shaped sources (github.webhook, http.webhook) implement
// HTTPMount and surface (path, method) for routing. Non-HTTP sources
// (cron in 7.2; queue sources in v1.44+) simply do not implement the
// interface and the receiver's mount loop skips them.
//
// Why a sub-interface and not a method on extension.TriggerSource:
// the core seal in pkg/extension/trigger.go was locked in Phase 7
// Plan 02. Adding methods there would force every future TriggerSource
// to provide an HTTPMount stub even when it makes no sense (cron
// triggers have no path/method). A sub-interface keeps the core
// minimal and lets HTTP capability be additive.
//
// Deviation note: 07.1-RESEARCH.md § Pattern 1 considered an
// HTTPRoute() (path, method string, ok bool) variant. Rejected: the
// type assertion already provides the discriminator (`if _, ok := src.(HTTPMounter); ok`),
// the bool would be redundant.
type HTTPMounter interface {
	// HTTPMount returns the (path, method) tuple this source registers
	// against the receiver's mux. Both must be non-empty; method is an
	// uppercase HTTP verb (e.g. "POST"). Validated at parse time by
	// the source factory (Plan 02 / Plan 03).
	HTTPMount() (path, method string)
}
