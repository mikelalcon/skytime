package interpreter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractScheduledTime(t *testing.T) {
	cases := []struct {
		name     string
		id       string
		wantOK   bool
		wantTime string
	}{
		{
			name:     "Z form (UTC), 20-char timestamp",
			id:       "weekly_digest/pVmKyBML-2026-05-11T20:17:00Z",
			wantOK:   true,
			wantTime: "2026-05-11T20:17:00Z",
		},
		{
			name:     "fractional seconds",
			id:       "weekly_digest/pVmKyBML-2026-05-11T20:17:00.123456Z",
			wantOK:   true,
			wantTime: "2026-05-11T20:17:00.123456Z",
		},
		{
			name:     "explicit positive offset",
			id:       "weekly_digest/pVmKyBML-2026-05-11T22:17:00+02:00",
			wantOK:   true,
			wantTime: "2026-05-11T22:17:00+02:00",
		},
		{
			name:     "explicit negative offset",
			id:       "weekly_digest/pVmKyBML-2026-05-11T15:17:00-05:00",
			wantOK:   true,
			wantTime: "2026-05-11T15:17:00-05:00",
		},
		{
			name:   "no timestamp suffix — manually started workflow",
			id:     "638904e6-bf93-4be7-93b6-527acaa30c7d",
			wantOK: false,
		},
		{
			name:   "non-Schedule action ID",
			id:     "weekly_digest/pVmKyBML",
			wantOK: false,
		},
		{
			name:   "garbage after dash",
			id:     "weekly_digest/pVmKyBML-not-a-timestamp",
			wantOK: false,
		},
		{
			name:   "empty",
			id:     "",
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := extractScheduledTime(tc.id)
			assert.Equal(t, tc.wantOK, ok, "ok flag")
			if !tc.wantOK {
				return
			}
			want, err := time.Parse(time.RFC3339, tc.wantTime)
			require.NoError(t, err)
			assert.True(t, got.Equal(want),
				"parsed time mismatch: got %s want %s", got, want)
		})
	}
}
