// Plan 07.3-04: dashboard template entry point. //go:embed pulls in
// dashboard.html at compile time so the binary stays self-contained —
// no filesystem reads at runtime (Open Question 5).
package web

import (
	"embed"
	"fmt"
	"html/template"
	"io"
	"time"
)

//go:embed dashboard.html
var templateFS embed.FS

// dashboardTpl is parsed once at package init. html/template applies
// contextual auto-escaping; we never template.HTML-wrap user content,
// so the auto-escape covers SSE-snapshot-derived strings rendered into
// the initial page payload (the rest of the wire path is JSON + DOM
// textContent — see the inline <script> B4 defense in dashboard.html).
var dashboardTpl = template.Must(
	template.New("dashboard.html").Funcs(template.FuncMap{
		"middleEllipsis": middleEllipsis,
		"formatTime":     formatTime,
		"formatDuration": formatDuration,
	}).ParseFS(templateFS, "dashboard.html"),
)

// DashboardData is the typed data passed to template execution.
// Snapshot is kept as `any` to avoid a hard import cycle in the
// (currently unrelated) case where pkg/cli/server/web/events would
// ever pull templates in. Callers pass events.Snapshot in practice.
type DashboardData struct {
	// Flows is sourced from FlowRegistry.FlowNames() (M1 — NOT Names()).
	Flows []string
	// Snapshot is the initial page payload — typically events.Snapshot.
	Snapshot any
	// TemporalWebUI is the URL prefix for Temporal Web UI deep-links;
	// empty disables linking (D-7.3-15).
	TemporalWebUI string
}

// RenderDashboard executes the dashboard template against data and
// writes the rendered HTML to w. Returns the underlying template error
// on failure; callers should write a 500 to the http.ResponseWriter
// when this returns non-nil.
func RenderDashboard(w io.Writer, data DashboardData) error {
	return dashboardTpl.ExecuteTemplate(w, "dashboard.html", data)
}

// middleEllipsis caps long IDs at 32 chars with a U+2026 in the middle.
// Workflow IDs are ~50 chars (manual/<flow>/<32hex>); middle ellipsis
// preserves the discriminating prefix + suffix.
func middleEllipsis(s string) string {
	const max = 32
	if len(s) <= max {
		return s
	}
	return s[:16] + "…" + s[len(s)-12:]
}

// formatTime renders a timestamp as RFC3339 in UTC, truncated to
// second precision. Empty string for zero time.
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Truncate(time.Second).Format(time.RFC3339)
}

// formatDuration renders the elapsed wall time between start and close
// (using time.Now when close is nil/zero, i.e., still running).
// Returns a compact human-friendly string ("250ms", "12s", "2m 13s",
// "1h 04m"). Empty string for zero start time.
func formatDuration(start time.Time, close *time.Time) string {
	if start.IsZero() {
		return ""
	}
	end := time.Now()
	if close != nil && !close.IsZero() {
		end = *close
	}
	d := end.Sub(start)
	if d < 0 {
		d = 0
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
}
