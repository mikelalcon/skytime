package activity_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNoTemporalImportsOutsideAllowList walks every Go file under the
// module's pkg/ tree EXCEPT the allowlisted directories and asserts none
// imports any go.temporal.io/sdk/* path. This enforces PROJECT.md's "no
// context bleed" — only the allowlisted packages are allowed to bridge
// to Temporal.
//
// Phase history:
//   - Phase 2: pkg/activity introduced; firewall covered pkg/activity only.
//   - Phase 3: pkg/interpreter + pkg/worker added; firewall expanded to
//     allowlist all three. Until plans 03-02 / 03-04 land, the latter two
//     packages don't yet exist in the tree, so the "skip" is a no-op for
//     them — but the test is forward-compatible so SDK imports landing
//     in those packages don't break the firewall.
//   - Phase 4 plan 04-05: pkg/cli added — the run subcommand legitimately
//     consumes client.ExecuteWorkflow / WorkflowRun.Get.
//   - Phase 5 (D5-firewall-q8): pkg/testing added — the Tier-3 E2E
//     harness imports go.temporal.io/sdk/testsuite + sdk/activity.
//   - Phase 7.1 plan 04: pkg/extension/receiver added — the HTTP webhook
//     receiver's Deps struct holds a client.Client used at request time
//     by Plan 04b's handler pipeline (ExecuteWorkflow with
//     REJECT_DUPLICATE per D-7.1-08). This is the FIRST allowlisted
//     entry under pkg/extension/* — the receiver is intentionally a
//     "system extension" sibling to pkg/extension/builtin/* (which
//     remains forbidden from importing the SDK), and the directory-
//     prefix match below means only the receiver subtree is permitted,
//     not pkg/extension itself or any future builtin.
//   - Phase 7.2 plan 02: pkg/extension/schedules added — the cron
//     Schedule reconciler imports go.temporal.io/sdk/client for
//     ScheduleClient + ScheduleHandle + ScheduleOptions/Spec/Action
//     types. Called by pkg/cli/server.go and pkg/cli/cron_plan.go
//     (Plan 03) at boot time on the --cron-reconcile replica. Same
//     "system extension" pattern as extension/receiver — sibling to
//     pkg/extension/builtin/* (which remains firewalled).
//
// Mirrors the per-package firewall tests in pkg/parser, pkg/extension (Phase 1)
// but inverts the check — those tests verify their OWN package is clean; this
// test verifies every OTHER pkg/* directory is clean while the allowlisted
// set is permitted to import the SDK.
func TestNoTemporalImportsOutsideAllowList(t *testing.T) {
	moduleRoot := findModuleRoot(t)
	pkgRoot := filepath.Join(moduleRoot, "pkg")

	// Allowlist of pkg/* subdirectories permitted to import go.temporal.io/sdk/*.
	// Order is irrelevant; kept in phase-introduction order for readability.
	// Phase 4 plan 04-05 added "cli" — pkg/cli's run subcommand is the
	// legitimate consumer of client.ExecuteWorkflow / WorkflowRun.Get.
	// Phase 5 (D5-firewall-q8 deviation; RESEARCH.md Open Q8): pkg/testing
	// is the Tier-3 E2E test harness; it imports go.temporal.io/sdk/testsuite
	// + go.temporal.io/sdk/activity (for activity.RegisterOptions). The
	// "no activity import" rule applies to extension packages, not to
	// the harness. See .planning/phases/05-tier-3-e2e-test-harness-temporal-test/
	// 05-RESEARCH.md Investigation 11 + Open Question 8.
	// Phase 7.1 plan 04 added "extension/receiver" — the HTTP webhook
	// receiver's Deps.Client client.Client (Mount-time dependency) and
	// Plan 04b's ExecuteWorkflow(REJECT_DUPLICATE) call site are the
	// legitimate consumers. Allowlist entries with a "/" act as exact
	// directory prefixes — pkg/extension itself and pkg/extension/builtin/*
	// remain firewalled.
	allowedPkgs := []string{"activity", "interpreter", "worker", "cli", "testing", "extension/receiver", "extension/schedules"}
	sep := string(filepath.Separator)

	fset := token.NewFileSet()
	checked := 0
	walkErr := filepath.Walk(pkgRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Skip the allowlisted pkg/* directories — the firewall ALLOWS
		// them to import temporal.
		rel, relErr := filepath.Rel(pkgRoot, path)
		if relErr != nil {
			return relErr
		}
		// rel looks like "activity/foo.go" or "extension/handler.go".
		for _, pkg := range allowedPkgs {
			if rel == pkg || strings.HasPrefix(rel, pkg+sep) {
				return nil
			}
		}

		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(importPath, "go.temporal.io/sdk") {
				t.Errorf("FIREWALL VIOLATION: %s imports %q — only pkg/{activity,interpreter,worker,cli,testing} may import go.temporal.io/sdk/*", path, importPath)
			}
			checked++
		}
		return nil
	})
	require.NoError(t, walkErr)
	t.Logf("checked %d import paths across pkg/ (excluding allowlist %v); none imported go.temporal.io/sdk/*", checked, allowedPkgs)
}

// TestPkgActivity_AllowedToImportTemporal is a meta-test that catches an
// inversion bug: if the firewall test above accidentally allowlists
// pkg/activity but pkg/activity files don't actually import temporal, the
// firewall is a no-op (it claims to allow something that nobody is using).
// Assert at least one pkg/activity *.go file (excluding _test.go) imports a
// go.temporal.io/sdk path.
func TestPkgActivity_AllowedToImportTemporal(t *testing.T) {
	moduleRoot := findModuleRoot(t)
	activityDir := filepath.Join(moduleRoot, "pkg", "activity")

	fset := token.NewFileSet()
	sawTemporalImport := false
	walkErr := filepath.Walk(activityDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		// Production sources only — _test.go files don't count toward the
		// "package actually uses the SDK" assertion.
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			return parseErr
		}
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(importPath, "go.temporal.io/sdk") {
				sawTemporalImport = true
				return filepath.SkipDir
			}
		}
		return nil
	})
	require.NoError(t, walkErr)
	require.True(t, sawTemporalImport,
		"no pkg/activity *.go (non-test) file imports go.temporal.io/sdk/* — the firewall is a no-op")
}

// findModuleRoot walks up from the test working directory until it finds a
// go.mod file. Tests run with cwd == package dir, so for pkg/activity tests
// the walk goes up two levels (pkg/activity → pkg → repo root).
func findModuleRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not find go.mod walking up from %s", cwd)
		}
		dir = parent
	}
}
