package credfile

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/pelletier/go-toml/v2"

	"github.com/mikelalcon/skytime/pkg/extension"
)

// Resolver loads credentials from a TOML file once at construction
// and serves them via the extension.CredentialHandler contract.
//
// Loaded ONCE: D-CREDS-PATH locks "not reloaded on each Resolve".
// Consultants restart the worker to pick up new credentials in v1.
type Resolver struct {
	creds map[string]extension.Credential
	path  string
	log   *slog.Logger
}

// New constructs a Resolver. With no options, reads $HOME/.skytime-credentials.
// The file MUST exist and parse as TOML; missing/malformed files return an
// error wrapping the underlying os/toml diagnostic.
//
// File-mode policy (POSIX systems only):
//   - mode & 0o044 == 0:  fine (owner-only).
//   - mode & 0o044 != 0:  default → slog.Warn; WithStrictMode → error.
//   - Windows:            mode check skipped entirely.
func New(opts ...Option) (*Resolver, error) {
	cfg := &config{
		path:   defaultPath(),
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(cfg)
	}
	if cfg.logger == nil {
		cfg.logger = slog.Default()
	}

	info, err := os.Stat(cfg.path)
	if err != nil {
		return nil, fmt.Errorf("credfile: stat %s: %w", cfg.path, err)
	}

	// POSIX file-mode check. Windows skips this entirely — see Pitfall 4.
	if runtime.GOOS != "windows" {
		if info.Mode().Perm()&0o044 != 0 {
			msg := fmt.Sprintf("credfile %s is world/group-readable (mode %o); chmod 600 to silence",
				cfg.path, info.Mode().Perm())
			if cfg.strict {
				return nil, fmt.Errorf("credfile: %s", msg)
			}
			cfg.logger.Warn(msg, "path", cfg.path, "mode", fmt.Sprintf("%o", info.Mode().Perm()))
		}
	}

	bytes, err := os.ReadFile(cfg.path)
	if err != nil {
		return nil, fmt.Errorf("credfile: read %s: %w", cfg.path, err)
	}
	var raw fileShape
	if err := toml.Unmarshal(bytes, &raw); err != nil {
		return nil, fmt.Errorf("credfile: parse %s: %w", cfg.path, err)
	}
	creds, err := buildCredentials(raw)
	if err != nil {
		return nil, fmt.Errorf("credfile %s: %w", cfg.path, err)
	}
	return &Resolver{creds: creds, path: cfg.path, log: cfg.logger}, nil
}

// Resolve implements extension.CredentialHandler. Unknown IDs wrap
// extension.ErrUnknownCredential so the activity classifier
// (pkg/activity/classify.go) treats the failure as NonRetryable.
func (r *Resolver) Resolve(_ context.Context, id string) (extension.Credential, error) {
	cred, ok := r.creds[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s (file=%s)", extension.ErrUnknownCredential, id, r.path)
	}
	return cred, nil
}

// Path returns the resolved credfile path. Useful for diagnostics in
// consumer binaries that want to log "loaded N credentials from <path>".
func (r *Resolver) Path() string { return r.path }

// Compile-time interface check.
var _ extension.CredentialHandler = (*Resolver)(nil)

// defaultPath returns $HOME/.skytime-credentials. Falls back to the
// bare relative path if HOME is unreadable (rare; mostly init scripts).
func defaultPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".skytime-credentials"
	}
	return filepath.Join(home, ".skytime-credentials")
}
