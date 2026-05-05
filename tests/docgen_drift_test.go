// Drift test for cmd/skytime-docgen — verifies that
// docs/reference/builtins.md matches a fresh run of skytime-docgen
// against pkg/parser. Drift means a developer changed a builtin
// signature or // skytime:doc marker without re-running go generate.
//
// Failure mode: prints a clear instruction to re-run `go generate
// ./pkg/parser/` and commit the result. The first 200 chars of both
// the want and got bytes are surfaced so the diff is visible without
// reaching for a side-channel diff tool.
//
// Lives in package firewall_test alongside firewall_cli_test.go +
// differential_test.go (the canonical external-test-package convention
// for tests/).
package firewall_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDocgenDrift(t *testing.T) {
	moduleRoot := findModuleRootDocgen(t)
	checkedIn := filepath.Join(moduleRoot, "docs", "reference", "builtins.md")
	wantBytes, err := os.ReadFile(checkedIn)
	if err != nil {
		t.Fatalf("reading checked-in %s: %v", checkedIn, err)
	}
	tmp := filepath.Join(t.TempDir(), "builtins.md")

	cmd := exec.Command("go", "run", "github.com/mikelalcon/skytime/cmd/skytime-docgen",
		"--pkg", filepath.Join(moduleRoot, "pkg", "parser"),
		"--out", tmp,
	)
	cmd.Dir = moduleRoot
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("skytime-docgen exec: %v\nstderr: %s", err, stderr.String())
	}
	got, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("reading regenerated %s: %v", tmp, err)
	}
	if !bytes.Equal(wantBytes, got) {
		t.Errorf("docs/reference/builtins.md is out of date.\nRun `go generate ./pkg/parser/` and commit the result.\n\nFirst 200-char diff:\n--- want\n%s\n--- got\n%s",
			truncateDocgen(string(wantBytes), 200), truncateDocgen(string(got), 200))
	}
}

func truncateDocgen(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func findModuleRootDocgen(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found from cwd")
		}
		dir = parent
	}
}
