package firewall_test

import "testing"

func TestNoExternalHTTPTemplateInWeb(t *testing.T) {
	t.Skip("Wave 0 stub: Plan 04 fills in. AST-walks every .go file under pkg/cli/server/web/ and asserts the import paths intersect ONLY with a stdlib + go.temporal.io/sdk + go.temporal.io/api allow-list. No gorilla, r3labs/sse, tmaxmax/go-sse, htmx, safehtml, etc.")
}
