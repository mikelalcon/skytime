package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDescribeCron(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		// Wildcard / step shorthands.
		{"* * * * *", "Every minute"},
		{"*/5 * * * *", "Every 5 minutes"},
		{"*/1 * * * *", "Every minute"},
		{"0 * * * *", "Every hour, on the hour"},
		{"0 */2 * * *", "Every 2 hours, on the hour"},

		// Daily at fixed time.
		{"0 9 * * *", "Daily at 09:00"},
		{"30 14 * * *", "Daily at 14:30"},
		{"0 0 * * *", "Daily at 00:00"},

		// Weekly — numeric day-of-week.
		{"0 9 * * 1", "Every Monday at 09:00"},
		{"0 9 * * 0", "Every Sunday at 09:00"},
		{"0 9 * * 7", "Every Sunday at 09:00"},

		// Weekly — three-letter day-of-week.
		{"30 17 * * FRI", "Every Friday at 17:30"},
		{"30 17 * * fri", "Every Friday at 17:30"},

		// Weekdays / weekends.
		{"0 9 * * 1-5", "Every weekday at 09:00"},
		{"0 11 * * 0,6", "Every weekend day at 11:00"},
		{"0 11 * * 6,0", "Every weekend day at 11:00"},

		// Monthly on day-of-month.
		{"0 6 1 * *", "Monthly on day 1 at 06:00"},
		{"15 23 15 * *", "Monthly on day 15 at 23:15"},

		// Unrecognized → empty.
		{"0 9 * 6 1", ""},                  // month filter — too niche to describe
		{"0 9-17 * * 1-5", ""},              // hour range — bail
		{"0 9 1,15 * *", ""},                // dom list — bail
		{"not a cron", ""},                  // garbage
		{"* * * *", ""},                     // 4 fields
		{"* * * * * *", ""},                 // 6 fields
		{"60 9 * * 1", ""},                  // minute out of range
		{"0 24 * * 1", ""},                  // hour out of range
		{"0 9 * * 8", ""},                   // dow out of range
	}

	for _, tc := range cases {
		got := describeCron(tc.expr)
		assert.Equal(t, tc.want, got, "describeCron(%q)", tc.expr)
	}
}
