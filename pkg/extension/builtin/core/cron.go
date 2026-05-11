package core

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	cronv3 "github.com/robfig/cron/v3"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// cronKind is the JSON discriminator + startup banner label for cron
// trigger sources. Locked by D-7.2-01.
const cronKind = "core.cron"

// fiveFieldParser is the locked 5-field POSIX cron parser. The flag set
// INTENTIONALLY OMITS cron.Descriptor — macros like @hourly / @daily are
// rejected per D-7.2-22 + success criterion #4 (Pitfall 6).
var fiveFieldParser = cronv3.NewParser(
	cronv3.Minute | cronv3.Hour | cronv3.Dom | cronv3.Month | cronv3.Dow,
)

// allowedOverlaps is the user-facing overlap allowlist (D-7.2-03). The 4
// keys map 1:1 to enumspb.ScheduleOverlapPolicy in Plan 02; the SDK's
// BUFFER_ALL and TERMINATE_OTHER are intentionally hidden.
var allowedOverlaps = map[string]bool{
	"skip":         true,
	"allow":        true,
	"buffer_one":   true,
	"cancel_other": true,
}

// cronSource is the concrete TriggerSource for core.cron(...). Embeds
// extension.TriggerSourceSeal for method-promoted triggerSourceMarker().
// Does NOT implement receiver.HTTPMounter — the receiver Mount loop
// (Phase 7.1 Plan 04) skips this type cleanly with zero changes.
type cronSource struct {
	extension.TriggerSourceSeal

	schedule      string
	timezone      string
	overlap       string
	catchupWindow *time.Duration // nil ⇒ Temporal default (1 minute)
}

// CronSource is the exported alias of cronSource for cross-package type
// assertions (Plan 02's reconciler reads accessor methods via
// *skycore.CronSource). Keep the underlying type unexported; alias the
// pointer for the receiver shape.
type CronSource = cronSource

// ----- TriggerSource contract -----

// Kind returns "core.cron" — the JSON discriminator + startup banner label.
func (*cronSource) Kind() string { return cronKind }

// ReqSchema returns the locked req.<field> set for cron triggers
// (D-7.2-13 + D-7.2-14). Semantic priority order: scheduled_time first
// (the canonical "what the cron asked for"), actual_time second (the
// "what Temporal actually delivered" — usually within ms but can lag
// during cluster catchup). Deterministic-suggestion ordering is the
// parser walker's responsibility at error-render time (sort the
// suggestions, not the schema).
func (*cronSource) ReqSchema() []string {
	return []string{"scheduled_time", "actual_time"}
}

// MarshalJSON emits the {kind, config} envelope (D-07-09). config
// contains schedule/timezone/overlap and (optionally) catchup_window_ns.
// Cron has no credential; no credential_id field is ever emitted.
func (s *cronSource) MarshalJSON() ([]byte, error) {
	cfg := map[string]any{
		"schedule": s.schedule,
		"timezone": s.timezone,
		"overlap":  s.overlap,
	}
	if s.catchupWindow != nil {
		cfg["catchup_window_ns"] = int64(*s.catchupWindow)
	}
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf(`{"kind":%q,"config":%s}`, cronKind, string(cfgBytes))), nil
}

// ----- starlark.Value contract -----

// String renders a debug-friendly representation; surfaces in slog
// fields when the value is logged. Both schedule and timezone are
// non-secret.
func (s *cronSource) String() string {
	return fmt.Sprintf("core.cron(schedule=%q, timezone=%q)", s.schedule, s.timezone)
}

// Type returns the Starlark type name. Matches Kind() for clarity in
// "type-error: expected X, got core.cron" diagnostics.
func (*cronSource) Type() string { return cronKind }

// Freeze is a no-op — cronSource is immutable after construction.
func (*cronSource) Freeze() {}

// Truth returns starlark.True (non-zero structured value convention).
func (*cronSource) Truth() starlark.Bool { return starlark.True }

// Hash refuses to be a map key. Cron sources are referenced by identity
// (passed once to trigger(...)), never used as dict keys.
func (*cronSource) Hash() (uint32, error) {
	return 0, fmt.Errorf("unhashable type: core.cron")
}

// ----- Accessors (consumed by Plan 02's reconciler via *skycore.CronSource) -----

// Schedule returns the 5-field POSIX cron string.
func (s *cronSource) Schedule() string { return s.schedule }

// Timezone returns the IANA timezone name.
func (s *cronSource) Timezone() string { return s.timezone }

// Overlap returns the overlap policy ({skip, allow, buffer_one, cancel_other}).
func (s *cronSource) Overlap() string { return s.overlap }

// CatchupWindow returns (duration, true) when set, (0, false) otherwise.
// Plan 02's reconciler uses the second return to distinguish unset from
// zero-duration when building ScheduleOptions.
func (s *cronSource) CatchupWindow() (time.Duration, bool) {
	if s.catchupWindow == nil {
		return 0, false
	}
	return *s.catchupWindow, true
}

// Compile-time seal assertions.
var _ extension.TriggerSource = (*cronSource)(nil)
var _ starlark.Value = (*cronSource)(nil)

// ----- parser-side factory -----

// cronFactory implements core.cron(schedule=, timezone=, overlap=, catchup_window=).
//
// Validates ALL kwargs at parse time per D-7.2-03 / D-7.2-22 / D-7.2-23:
//   - schedule (required): 5-field POSIX cron; macros / 6-field rejected
//   - timezone (default "UTC"): IANA name; validated via time.LoadLocation
//   - overlap (default "skip"): ∈ allowedOverlaps
//   - catchup_window (optional): time.ParseDuration; must be non-negative
//
// Errors are *dag.ParseError so the parser's existing error rendering
// produces "<file>:<line>:<col>: <msg>".
func cronFactory(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		schedule      string
		timezone      = "UTC"
		overlap       = "skip"
		catchupWindow string // parsed via time.ParseDuration; empty ⇒ unset
	)
	if err := starlark.UnpackArgs("core.cron", args, kwargs,
		"schedule", &schedule,
		"timezone?", &timezone,
		"overlap?", &overlap,
		"catchup_window?", &catchupWindow,
	); err != nil {
		return nil, &dag.ParseError{Pos: callerPosition(thread), Msg: "core.cron: " + err.Error()}
	}

	if schedule == "" {
		return nil, &dag.ParseError{Pos: callerPosition(thread), Msg: "core.cron: schedule is required"}
	}

	// 1. Cron string validation (D-7.2-22 + success criterion #4 + Pitfall 6).
	if _, err := fiveFieldParser.Parse(schedule); err != nil {
		return nil, &dag.ParseError{
			Pos: callerPosition(thread),
			Msg: fmt.Sprintf("core.cron: invalid 5-field POSIX cron %q: %s", schedule, err.Error()),
		}
	}

	// 2. Timezone validation (D-7.2-23). time.LoadLocation reads the
	//    binary-embedded zoneinfo (via `_ "time/tzdata"` in core.go), so
	//    this resolves identically in any container.
	if _, err := time.LoadLocation(timezone); err != nil {
		return nil, &dag.ParseError{
			Pos: callerPosition(thread),
			Msg: fmt.Sprintf("core.cron: invalid timezone %q: %s (use IANA name like %q)", timezone, err.Error(), "America/New_York"),
		}
	}

	// 3. Overlap allowlist (D-7.2-03).
	if !allowedOverlaps[overlap] {
		return nil, &dag.ParseError{
			Pos: callerPosition(thread),
			Msg: fmt.Sprintf("core.cron: overlap %q not allowed; valid: %v", overlap, sortedKeys(allowedOverlaps)),
		}
	}

	// 4. Catchup window (optional).
	src := &cronSource{schedule: schedule, timezone: timezone, overlap: overlap}
	if catchupWindow != "" {
		d, err := time.ParseDuration(catchupWindow)
		if err != nil {
			return nil, &dag.ParseError{
				Pos: callerPosition(thread),
				Msg: fmt.Sprintf("core.cron: catchup_window %q is not a valid duration: %s", catchupWindow, err.Error()),
			}
		}
		if d < 0 {
			return nil, &dag.ParseError{
				Pos: callerPosition(thread),
				Msg: fmt.Sprintf("core.cron: catchup_window must be non-negative; got %s", d),
			}
		}
		src.catchupWindow = &d
	}
	return src, nil
}

// callerPosition extracts the .star call-site position. Mirrors
// pkg/extension/builtin/http/http.go's helper of the same name —
// thread.CallFrame(1).Pos for normal .star-driven invocations;
// syntax.Position{} when the call stack is too shallow (defensive).
func callerPosition(thread *starlark.Thread) syntax.Position {
	if thread.CallStackDepth() < 2 {
		return syntax.Position{}
	}
	return thread.CallFrame(1).Pos
}

// sortedKeys returns map keys sorted alphabetically — deterministic
// error messages (operators see the same valid-set every time they hit
// a typo).
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// init registers the core.cron unmarshaler. Mirrors
// pkg/extension/builtin/http/webhook.go::init — the dispatcher in
// pkg/extension/trigger_unmarshal.go peels the {kind, config} envelope
// and passes ONLY the inner config bytes here.
//
// Errors are intentionally swallowed (mirrors RegisterFakeFactories);
// duplicate registration across test re-imports is benign.
func init() {
	_ = extension.RegisterTriggerSourceFactory(cronKind, func(configData []byte) (extension.TriggerSource, error) {
		var cfg struct {
			Schedule        string `json:"schedule"`
			Timezone        string `json:"timezone"`
			Overlap         string `json:"overlap"`
			CatchupWindowNs int64  `json:"catchup_window_ns"`
		}
		if err := json.Unmarshal(configData, &cfg); err != nil {
			return nil, fmt.Errorf("core.cron unmarshal: %w", err)
		}
		src := &cronSource{schedule: cfg.Schedule, timezone: cfg.Timezone, overlap: cfg.Overlap}
		if cfg.CatchupWindowNs != 0 {
			d := time.Duration(cfg.CatchupWindowNs)
			src.catchupWindow = &d
		}
		return src, nil
	})
}
