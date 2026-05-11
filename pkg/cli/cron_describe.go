package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// describeCron returns a human-readable English description of a 5-field
// POSIX cron expression for the common patterns operators write by hand.
//
// Returns "" if the expression doesn't match a recognized pattern — the
// caller should fall back to showing the raw expression alone.
//
// Covered patterns (minute, hour, day-of-month, month, day-of-week):
//
//	* * * * *           Every minute
//	*/N * * * *         Every N minutes
//	0 * * * *           Every hour
//	0 */N * * *         Every N hours
//	MM HH * * *         Daily at HH:MM
//	MM HH * * D         Every {weekday-name} at HH:MM
//	MM HH * * 1-5       Every weekday at HH:MM
//	MM HH * * 0,6 / 6,0 Every weekend day at HH:MM
//	MM HH D * *         Monthly on day D at HH:MM
//
// Anything outside this set returns "". The intent is to help operators
// scanning a plan output, not to be a complete cronstrue port — complex
// expressions surface in the raw form where the reader can grok them with
// a reference.
func describeCron(expr string) string {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return ""
	}
	minute, hour, dom, month, dow := fields[0], fields[1], fields[2], fields[3], fields[4]

	// Every minute
	if minute == "*" && hour == "*" && dom == "*" && month == "*" && dow == "*" {
		return "Every minute"
	}

	// Every N minutes
	if strings.HasPrefix(minute, "*/") && hour == "*" && dom == "*" && month == "*" && dow == "*" {
		if n, err := strconv.Atoi(strings.TrimPrefix(minute, "*/")); err == nil && n > 0 {
			if n == 1 {
				return "Every minute"
			}
			return fmt.Sprintf("Every %d minutes", n)
		}
	}

	// Every hour (0 * * * *)
	if minute == "0" && hour == "*" && dom == "*" && month == "*" && dow == "*" {
		return "Every hour, on the hour"
	}

	// Every N hours (0 */N * * *)
	if minute == "0" && strings.HasPrefix(hour, "*/") && dom == "*" && month == "*" && dow == "*" {
		if n, err := strconv.Atoi(strings.TrimPrefix(hour, "*/")); err == nil && n > 0 {
			if n == 1 {
				return "Every hour, on the hour"
			}
			return fmt.Sprintf("Every %d hours, on the hour", n)
		}
	}

	// Need a literal HH:MM time for the remaining patterns.
	hh, mm, timeOK := literalTime(hour, minute)
	if !timeOK {
		return ""
	}
	timeStr := fmt.Sprintf("%02d:%02d", hh, mm)

	// Daily at HH:MM
	if dom == "*" && month == "*" && dow == "*" {
		return fmt.Sprintf("Daily at %s", timeStr)
	}

	// Weekly patterns (dom == "*" && month == "*")
	if dom == "*" && month == "*" {
		if phrase := describeDow(dow); phrase != "" {
			return fmt.Sprintf("%s at %s", phrase, timeStr)
		}
	}

	// Monthly on day D at HH:MM
	if d, err := strconv.Atoi(dom); err == nil && d >= 1 && d <= 31 &&
		month == "*" && dow == "*" {
		return fmt.Sprintf("Monthly on day %d at %s", d, timeStr)
	}

	return ""
}

// literalTime returns the numeric (hour, minute) iff both fields are
// plain integers within range. Returns ok=false for ranges, lists, or
// step expressions.
func literalTime(hour, minute string) (h, m int, ok bool) {
	h, err := strconv.Atoi(hour)
	if err != nil || h < 0 || h > 23 {
		return 0, 0, false
	}
	m, err = strconv.Atoi(minute)
	if err != nil || m < 0 || m > 59 {
		return 0, 0, false
	}
	return h, m, true
}

// describeDow translates the day-of-week field into an English phrase
// for the patterns most operators use. Returns "" for unrecognized
// shapes — caller should fall back to leaving the raw expression alone.
func describeDow(dow string) string {
	switch dow {
	case "1-5", "MON-FRI", "mon-fri":
		return "Every weekday"
	case "0,6", "6,0", "0,7", "7,0", "SAT,SUN", "sat,sun", "SUN,SAT", "sun,sat":
		return "Every weekend day"
	}

	// Single literal weekday (0..7; 0 and 7 both mean Sunday).
	if n, err := strconv.Atoi(dow); err == nil && n >= 0 && n <= 7 {
		return "Every " + weekdayName(n)
	}

	// Three-letter literal (MON, TUE, ...).
	if name := weekdayFromAbbr(dow); name != "" {
		return "Every " + name
	}

	return ""
}

func weekdayName(n int) string {
	switch n {
	case 0, 7:
		return "Sunday"
	case 1:
		return "Monday"
	case 2:
		return "Tuesday"
	case 3:
		return "Wednesday"
	case 4:
		return "Thursday"
	case 5:
		return "Friday"
	case 6:
		return "Saturday"
	}
	return ""
}

func weekdayFromAbbr(s string) string {
	switch strings.ToUpper(s) {
	case "SUN":
		return "Sunday"
	case "MON":
		return "Monday"
	case "TUE":
		return "Tuesday"
	case "WED":
		return "Wednesday"
	case "THU":
		return "Thursday"
	case "FRI":
		return "Friday"
	case "SAT":
		return "Saturday"
	}
	return ""
}
