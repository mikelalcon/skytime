package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/extension"
	skycore "github.com/mikelalcon/skytime/pkg/extension/builtin/core"
	skyhttp "github.com/mikelalcon/skytime/pkg/extension/builtin/http"
)

// TestLoadRegistries_Happy: a rootdir with one valid .star file
// parses into populated flow + trigger registries.
func TestLoadRegistries_Happy(t *testing.T) {
	rootdir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(rootdir, "f.star"), []byte(`
flow(name="weekly_digest", steps=[])
trigger(
    flow="weekly_digest",
    source=core.cron(schedule="0 9 * * 1", timezone="UTC"),
    map=lambda req: {},
    idempotency_key=lambda req: str(req.scheduled_time),
)
`), 0o644))

	flowReg, trigReg, err := loadRegistries(
		context.Background(),
		rootdir,
		[]extension.Extension{skyhttp.New(), skycore.New()},
	)
	require.NoError(t, err)
	require.NotNil(t, flowReg)
	require.NotNil(t, trigReg)
	require.Len(t, trigReg.All(), 1)
	require.Contains(t, flowReg.FlowNames(), "weekly_digest")
}

// TestLoadRegistries_BadStar: a .star file with a 6-field cron string
// (Plan 01 parse-time rejection) surfaces a wrapped error from the
// parser (file:line:col + invalid-5-field-POSIX-cron message).
func TestLoadRegistries_BadStar(t *testing.T) {
	rootdir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(rootdir, "f.star"), []byte(`
flow(name="bad", steps=[])
trigger(
    flow="bad",
    source=core.cron(schedule="0 0 9 * * 1"),
    map=lambda req: {},
    idempotency_key=lambda req: "x",
)
`), 0o644))

	_, _, err := loadRegistries(
		context.Background(),
		rootdir,
		[]extension.Extension{skycore.New()},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid 5-field POSIX cron")
}

// TestLoadRegistries_MissingRootdir: empty rootdir returns a clear
// "rootdir is required" error (vs the lower-level os.Stat error path
// from bootRegistry on an empty string, which is less obvious).
func TestLoadRegistries_MissingRootdir(t *testing.T) {
	_, _, err := loadRegistries(context.Background(), "", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "rootdir is required")
}
