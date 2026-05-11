package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/client"

	"github.com/mikelalcon/skytime/pkg/extension"
	skycore "github.com/mikelalcon/skytime/pkg/extension/builtin/core"
	skyhttp "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
	"github.com/mikelalcon/skytime/pkg/extension/schedules"
	"github.com/mikelalcon/skytime/pkg/worker"
)

// makeCronPlanTestDir writes a temp rootdir with one valid cron trigger
// for the cron-plan tests. Mirrors makeCronServerTestDir but lives here
// so the test files are independent for go test selection.
func makeCronPlanTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	starContent := `flow(name = "weekly_digest", steps = [])
trigger(
    flow = "weekly_digest",
    source = core.cron(schedule = "0 9 * * 1", timezone = "America/New_York"),
    map = lambda req: {},
    idempotency_key = lambda req: str(req.scheduled_time),
)
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "weekly_digest.star"), []byte(starContent), 0o644))
	return dir
}

// TestCronPlanCmd_DryRun: with a fixture having one cron trigger and an
// empty cluster, the dry-run plan reports one CREATE without invoking
// any mutation methods on the FakeScheduleClient.
func TestCronPlanCmd_DryRun(t *testing.T) {
	installFakeClientFactory(t)

	fakeSC := schedules.NewFakeScheduleClient()
	rootdir := makeCronPlanTestDir(t)

	cfg := &config{
		exts:            []extension.Extension{skyhttp.New(), skycore.New()},
		scheduleFactory: func(_ client.Client) client.ScheduleClient { return fakeSC },
	}
	cmd := newCronPlanCommand(cfg)

	// Redirect stderr capture (charm-log + slog default go to stderr).
	// Also pipe os.Stderr so any handlers writing to stderr via setupServerLogging
	// land in our buffer.
	prevStderr := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w
	t.Cleanup(func() { os.Stderr = prevStderr })

	cmd.SetArgs([]string{"--rootdir", rootdir, "--task-queue", "demo"})
	var cmdStderr, cmdStdout bytes.Buffer
	cmd.SetErr(&cmdStderr)
	cmd.SetOut(&cmdStdout)

	err = cmd.ExecuteContext(context.Background())

	// Close the writer end so the copy below terminates.
	require.NoError(t, w.Close())
	var captured bytes.Buffer
	_, copyErr := io.Copy(&captured, r)
	require.NoError(t, copyErr)

	require.NoError(t, err, "cron-plan dry-run must exit 0")
	require.Empty(t, fakeSC.CreateCalls, "cron-plan must not call Create")
	require.Empty(t, fakeSC.UpdateCalls, "cron-plan must not call Update")
	require.Empty(t, fakeSC.DeleteCalls, "cron-plan must not call Delete")

	combined := captured.String() + cmdStdout.String() + cmdStderr.String()
	require.Contains(t, combined, "1 to add",
		"pretty plan must show terraform-style header counting creates; got: %s", combined)
	require.Contains(t, combined, "  + skytime/weekly_digest/",
		"pretty plan must show '+' marker plus the composed Schedule ID; got: %s", combined)
}

// TestCronPlanCmd_OutputFormat: with --json-log, plan records are
// JSON-parseable; without it, the charm-log Bazel-style output prefixes
// each record with the message text "cron-plan".
func TestCronPlanCmd_OutputFormat(t *testing.T) {
	cases := []struct {
		name     string
		jsonLog  bool
		wantJSON bool
	}{
		{"charm-log default", false, false},
		{"json-log enabled", true, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installFakeClientFactory(t)
			fakeSC := schedules.NewFakeScheduleClient()
			rootdir := makeCronPlanTestDir(t)

			cfg := &config{
				exts:            []extension.Extension{skyhttp.New(), skycore.New()},
				scheduleFactory: func(_ client.Client) client.ScheduleClient { return fakeSC },
			}
			cmd := newCronPlanCommand(cfg)

			prevStderr := os.Stderr
			r, w, err := os.Pipe()
			require.NoError(t, err)
			os.Stderr = w
			t.Cleanup(func() { os.Stderr = prevStderr })

			args := []string{"--rootdir", rootdir, "--task-queue", "demo"}
			if tc.jsonLog {
				args = append(args, "--json-log")
			}
			cmd.SetArgs(args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err = cmd.ExecuteContext(context.Background())
			require.NoError(t, w.Close())
			var captured bytes.Buffer
			_, copyErr := io.Copy(&captured, r)
			require.NoError(t, copyErr)
			require.NoError(t, err)

			out := captured.String()
			require.Contains(t, out, "cron-plan",
				"output should mention cron-plan in any format")

			if tc.wantJSON {
				// At least one line must parse as JSON.
				foundJSON := false
				for _, line := range strings.Split(out, "\n") {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					var rec map[string]any
					if json.Unmarshal([]byte(line), &rec) == nil {
						if _, hasMsg := rec["msg"]; hasMsg {
							foundJSON = true
							break
						}
					}
				}
				assert.True(t, foundJSON,
					"--json-log output must contain at least one JSON record with msg field; got: %s", out)
			} else {
				// charm-log produces non-JSON lines (no {"msg":...} envelope).
				// Pick a line that mentions cron-plan and assert it doesn't
				// look like a JSON object.
				assert.NotContains(t, out, `"msg":"cron-plan reading"`,
					"charm-log default must NOT emit JSON envelope; got: %s", out)
			}
		})
	}
}

// TestCronPlanCmd_RootdirRequired: omitting --rootdir surfaces cobra's
// "required flag(s)" error before any side effects.
func TestCronPlanCmd_RootdirRequired(t *testing.T) {
	cmd := newCronPlanCommand(&config{})
	cmd.SetArgs([]string{"--task-queue", "demo"}) // no --rootdir
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "rootdir",
		"cobra's required-flag error must mention rootdir; got %q", err.Error())
}

// _ keeps the worker import alive across edits if other tests in this
// package drop it later. Currently unused but cheap insurance.
var _ = worker.NewWorkerForTest
