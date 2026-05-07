// issue_triage_test_e2e_test.go is the Go-side runner for the example's
// Tier-3 .star tests. It drives pkg/testing.RunCLI in-process against
// examples/http-github-webhook/ with the example's three extensions
// registered, and additionally exercises the full extbin subprocess
// path (mirroring CI's `extbin test ./examples/http-github-webhook/`
// step from 06-09).
//
// Coverage:
//
//   - TestIssueTriageTest_PkgTesting (load-bearing): in-process Tier-3
//     run; proves issue_triage_test.star executes end-to-end through the
//     pkg/testing harness against the registered HTTP + GitHub + Webhook
//     extensions, all three def test_*() blocks fire, and replay
//     determinism (D5-D1, ALWAYS-ON) holds.
//   - TestIssueTriageTest_SubprocessSmoke (belt-and-suspenders): builds
//     extbin and runs `extbin test ./` from the example dir. Catches
//     binary-only regressions (e.g. a future cmd/extbin/main.go that
//     drops one of the three extension registrations).
//
// Both tests are hermetic: no Temporal server, no network. The mocks in
// issue_triage_test.star satisfy every gh.* action call.
package httpgithubwebhook_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	skyhttp "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
	pkgtesting "github.com/mikelalcon/skytime/pkg/testing"

	skygh "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/github"
	skyweb "github.com/mikelalcon/skytime/examples/http-github-webhook/extensions/webhook"
)

// TestIssueTriageTest_PkgTesting drives the in-process Tier-3 runner
// against the example directory's *_test.star files. Currently only
// issue_triage_test.star matches; the runner walks recursively (Pitfall
// 9) so any future *_test.star under this dir gets picked up too.
//
// The output buffer captures the bareReporter's per-test PASS/FAIL
// lines so a failure can dump the runner output for diagnosis.
func TestIssueTriageTest_PkgTesting(t *testing.T) {
	var outBuf bytes.Buffer

	passed, failed, err := pkgtesting.RunCLI(
		".",
		pkgtesting.WithExtensions(skyhttp.New(), skygh.New(), skyweb.New()),
		pkgtesting.WithOutput(&outBuf),
	)
	if err != nil || failed > 0 {
		t.Log("Tier-3 runner output:\n" + outBuf.String())
	}
	require.NoError(t, err, "pkg/testing.RunCLI must not error")
	require.Equal(t, 0, failed, "no Tier-3 tests should fail; got %d failed (passed=%d)", failed, passed)
	require.Greater(t, passed, 0, "at least one test must have run; output:\n%s", outBuf.String())

	// Sanity: each of the three def test_*() blocks in
	// issue_triage_test.star should appear in the per-test report.
	// bareReporter prints "PASS  <test_name>" / "FAIL  <test_name>" lines
	// (pkg/testing/cli_run.go::runOneFileCLI via formatHumanLine).
	out := outBuf.String()
	for _, name := range []string{
		"test_happy_path",
		"test_get_issue_retries_then_succeeds",
		"test_add_comment_routes_credential",
	} {
		assert.Contains(t, out, name, "expected test %q to appear in runner output", name)
	}
}

// TestIssueTriageTest_SubprocessSmoke builds the example's extbin and
// runs `extbin test ./` from the example directory. This mirrors what
// CI's `extbin test ./examples/http-github-webhook/` step does (06-09)
// and catches binary-only regressions: if a future cmd/extbin/main.go
// accidentally drops one of the three extension registrations, this
// fails while TestIssueTriageTest_PkgTesting still passes (because the
// in-process test imports the extensions directly).
//
// Honors -short because go build adds ~3-5s of overhead.
func TestIssueTriageTest_SubprocessSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess smoke in -short")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "extbin")

	build := exec.Command("go", "build", "-o", bin, "./cmd/extbin")
	build.Stderr = os.Stderr
	require.NoError(t, build.Run(), "go build ./cmd/extbin failed")

	// Run with cwd = examples/http-github-webhook (the test's package
	// directory) so `./` resolves to the example dir.
	run := exec.Command(bin, "test", "./")
	run.Stderr = os.Stderr
	out, err := run.Output()
	outStr := string(out)
	if err != nil {
		t.Logf("extbin test output:\n%s", outStr)
	}
	require.NoError(t, err, "extbin test should exit 0")

	// At minimum, the happy-path test name should appear in the
	// captured stdout — proves the inline test ran via the same code
	// path consumers would use.
	assert.True(t,
		strings.Contains(outStr, "test_happy_path"),
		"expected 'test_happy_path' in extbin output; got:\n%s", outStr,
	)
}
