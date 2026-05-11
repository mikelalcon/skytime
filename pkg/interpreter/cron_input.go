package interpreter

import (
	"regexp"
	"time"
)

// scheduleTimestampSuffixRe matches the `-<RFC3339>` suffix Temporal
// Schedules append to a ScheduleAction.ID at fire time (Pitfall 2 in
// 07.2-RESEARCH.md). For action ID "weekly_digest/pVmKyBML" fired at
// 20:17:00Z the resulting WorkflowID is
// "weekly_digest/pVmKyBML-2026-05-11T20:17:00Z".
//
// Accepts:
//
//   - UTC Z form        ("2026-05-11T20:17:00Z")
//   - Fractional seconds ("2026-05-11T20:17:00.123456789Z")
//   - Timezone offsets   ("2026-05-11T20:17:00+02:00", "...-05:00")
//
// Anchored at end-of-string so we only match the schedule-appended
// suffix and never a coincidence elsewhere in the ID.
var scheduleTimestampSuffixRe = regexp.MustCompile(
	`-(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2}))$`,
)

// extractScheduledTime parses the scheduled fire time encoded by Temporal
// in a Schedule-fired workflow's ID. Returns (time, true) when the ID
// ends with a parseable RFC3339 suffix, (zero, false) otherwise.
//
// This is the only deterministic way to recover the scheduled time
// inside the workflow goroutine because:
//
//   - The Go SDK's workflow.Info does NOT expose ScheduleAction.ID's
//     timestamp suffix as a structured field.
//   - The reconciler cannot inline the scheduled time into
//     ScheduleAction.Args (it's a per-fire value, not a per-Schedule
//     value).
//
// Non-Schedule-fired workflows (manually started, child workflows, run
// CLI) have no such suffix and return false — auto-injection skips
// them and InitState passes through unchanged.
func extractScheduledTime(workflowID string) (time.Time, bool) {
	m := scheduleTimestampSuffixRe.FindStringSubmatch(workflowID)
	if m == nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, m[1])
	if err != nil {
		// Regex matched but parse failed — defensive only; the regex
		// shape mirrors RFC3339 strictly enough that time.Parse should
		// always succeed.
		return time.Time{}, false
	}
	return t, true
}
