package worker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// noopHandler is a CredentialHandler that always returns ErrUnknownCredential.
// Used to satisfy the WorkerOptions.CredentialHandler required field in tests.
type noopHandler struct{}

func (noopHandler) Resolve(_ context.Context, id string) (extension.Credential, error) {
	return nil, extension.ErrUnknownCredential
}

// ---------------------------------------------------------------------------
// WorkerOptions defaults & validation
// ---------------------------------------------------------------------------

func TestWorkerOptions_Defaults(t *testing.T) {
	o := WorkerOptions{
		RootDir:           "x",
		CredentialHandler: noopHandler{},
	}
	require.NoError(t, o.applyDefaults())
	assert.Equal(t, defaultBuildID, o.BuildID, "BuildID defaults to defaultBuildID")
	assert.Equal(t, "skytime", o.TaskQueue, "TaskQueue defaults to 'skytime'")
	assert.True(t, o.UseBuildIDVersioning, "UseBuildIDVersioning auto-enabled when BuildID set")
}

func TestWorkerOptions_RootDirRequired(t *testing.T) {
	o := WorkerOptions{CredentialHandler: noopHandler{}}
	err := o.applyDefaults()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RootDir")
}

func TestWorkerOptions_CredentialHandlerRequired(t *testing.T) {
	o := WorkerOptions{RootDir: "x"}
	err := o.applyDefaults()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CredentialHandler")
}

func TestWorkerOptions_ExplicitOverrides(t *testing.T) {
	o := WorkerOptions{
		RootDir:           "x",
		BuildID:           "v42",
		TaskQueue:         "critical",
		CredentialHandler: noopHandler{},
	}
	require.NoError(t, o.applyDefaults())
	assert.Equal(t, "v42", o.BuildID)
	assert.Equal(t, "critical", o.TaskQueue)
}

// ---------------------------------------------------------------------------
// build_id.go
// ---------------------------------------------------------------------------

func TestDefaultBuildID_Default(t *testing.T) {
	// In test runs no -ldflags injection occurs, so defaultBuildID stays "dev".
	assert.Equal(t, "dev", defaultBuildID, "defaultBuildID must be 'dev' without -ldflags override")
}
