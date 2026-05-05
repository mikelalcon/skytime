package testing

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarktest"
)

// RunCLI is the non-*testing.T entry-point for pkg/cli. Mirrors
// pkgtesting.Run (Plan 04) but uses a synthetic Reporter (bareReporter)
// so the CLI can drive the harness without inventing a *testing.T.
//
// Returns the per-run pass/fail counts so pkg/cli/test.go can translate
// them to exit codes (D5-E4):
//
//	passed > 0, failed == 0  → exit 0
//	failed > 0               → exit 1
//	err != nil               → exit 1 (CLI prints err.Error() to stderr)
//
// Output (human or JSON per cfg.formatJSON) is written to cfg.formatOut
// (defaults to os.Stdout).
//
// CLI-03 contract: default human format MUST NOT contain Go runtime
// stack frames. RunCLI relies on starlarktest's Starlark-callsite-only
// failure messages and on bareReporter (which does NOT call *testing.T.Error,
// so no Go-test runtime machinery is involved).
//
// Plan 06 deviation D5-runner-cli-adapter: this duplicates ~80 LOC of
// runOneFile because Plan 05's runOneFile is *testing.T-bound (its
// drive callback uses subT.Run / subT.Failed / subT.Skipped). Refactor
// to a shared internal helper is acceptable in a future iteration; for
// v1 the duplication is contained and tested.
func RunCLI(dir string, opts ...Option) (passed, failed int, err error) {
	cfg := &runConfig{}
	for _, opt := range opts {
		if optErr := opt(cfg); optErr != nil {
			return 0, 0, optErr
		}
	}
	out := cfg.formatOut
	if out == nil {
		out = os.Stdout
	}
	var em *jsonEmitter
	if cfg.formatJSON {
		em = newJSONEmitter(out)
	}

	files, walkErr := DiscoverTestFiles(dir)
	if walkErr != nil {
		return 0, 0, fmt.Errorf("discover test files in %s: %w", dir, walkErr)
	}
	if len(files) == 0 {
		// Advisory + zero-counts. CLI translates (0, 0, nil) → exit 0
		// with a stdout "no tests to run" line. Mirrors `go test ./...`
		// semantics when no test files are discoverable.
		if em == nil {
			fmt.Fprintf(out, "no *_test.star files under %s\n", dir)
		}
		return 0, 0, nil
	}

	runStart := time.Now()
	for _, file := range files {
		p, f := runOneFileCLI(file, cfg, out, em)
		passed += p
		failed += f
	}
	if em == nil {
		// Final all-files summary line (D5-E1).
		total := passed + failed
		if failed == 0 {
			fmt.Fprintf(out, "PASS  %d files  %d tests  (%.2fs)\n", len(files), total, time.Since(runStart).Seconds())
		} else {
			fmt.Fprintf(out, "FAIL  %d files  %d tests  %d failed  (%.2fs)\n", len(files), total, failed, time.Since(runStart).Seconds())
		}
	}
	return passed, failed, nil
}

// runOneFileCLI mirrors runner.go::runOneFile but uses bareReporter
// (NOT *testing.T) so no Go-test runtime machinery is involved. Returns
// (passed, failed) for this file. Reuses parseTestFile (Plan 04+05) to
// avoid duplicating parser construction + test discovery.
func runOneFileCLI(file string, cfg *runConfig, out io.Writer, em *jsonEmitter) (passed, failed int) {
	parsed, err := parseTestFile(file, cfg)
	if err != nil {
		fileBase := filepath.Base(file)
		// Surface as a synthetic FAIL so JSON consumers see it.
		if em != nil {
			em.emit("fail", fileBase, "", err.Error(), 0)
		} else {
			fmt.Fprintf(out, "FAIL  %s  %s\n", fileBase, err.Error())
		}
		return 0, 1
	}

	fileBase := filepath.Base(file)
	fileStem := strings.TrimSuffix(fileBase, ".star")
	pkg := fileBase

	var fileTotal, fileFailed int
	fileStart := time.Now()

	for _, tc := range parsed.Tests {
		fullName := fileStem + "." + tc.Name
		if !MatchRunFilter(cfg.runRegex, fullName) {
			continue
		}
		fileTotal++

		if em != nil {
			em.emit("start", pkg, tc.Name, "", 0)
			em.emit("run", pkg, tc.Name, "", 0)
		}

		testStart := time.Now()
		rep := &bareReporter{}
		runOneTestCLI(rep, tc.Fn, parsed.Reg, parsed.WS)
		elapsed := time.Since(testStart).Seconds()

		if rep.failed {
			fileFailed++
			detail := rep.allMessages()
			if em != nil {
				if detail != "" {
					em.emit("output", pkg, tc.Name, detail, 0)
				}
				em.emit("fail", pkg, tc.Name, "", elapsed)
			} else {
				fmt.Fprint(out, formatHumanLine("FAIL", tc.Name, elapsed))
				if detail != "" {
					// D5-E1 indented detail block under FAIL line.
					for _, line := range strings.Split(strings.TrimRight(detail, "\n"), "\n") {
						if line == "" {
							continue
						}
						fmt.Fprintf(out, "    %s\n", line)
					}
				}
			}
		} else {
			if em != nil {
				em.emit("pass", pkg, tc.Name, "", elapsed)
			} else {
				fmt.Fprint(out, formatHumanLine("PASS", tc.Name, elapsed))
			}
		}
	}

	// Per-file summary footer (human only, D5-E1).
	if em == nil && fileTotal > 0 {
		fileElapsed := time.Since(fileStart).Seconds()
		if fileFailed == 0 {
			fmt.Fprintf(out, "PASS  %s  %d tests  (%.2fs)\n", pkg, fileTotal, fileElapsed)
		} else {
			fmt.Fprintf(out, "FAIL  %s  %d tests  %d failed  (%.2fs)\n", pkg, fileTotal, fileFailed, fileElapsed)
		}
	}

	return fileTotal - fileFailed, fileFailed
}

// runOneTestCLI mirrors reporter.go::runOneTest but binds bareReporter
// (NOT *testing.T) to the starlarktest reporter slot. Same behavior:
// fresh thread per test, mock registry frame push/pop, starlark.Call.
func runOneTestCLI(rep *bareReporter, fn *starlark.Function, reg *MockRegistry, ws *WorkflowSpec) {
	_ = ws // ws is held by the runner; runOneTestCLI does not consume it directly.
	thread := &starlark.Thread{
		Name: fmt.Sprintf("test:%s:%s", fn.Position().Filename(), fn.Name()),
	}
	// starlarktest.SetReporter requires the Reporter interface;
	// bareReporter satisfies it via Error(args ...any).
	starlarktest.SetReporter(thread, rep)

	reg.PushTestFrame()
	defer reg.PopTestFrame()

	if _, err := starlark.Call(thread, fn, nil, nil); err != nil {
		// Starlark *EvalError already includes file:line:col in
		// err.Error(). Forward as one more Reporter.Error so callers
		// see the same failure shape regardless of whether the test
		// died from assert.* or a non-assertion eval error.
		rep.Error(err.Error())
	}
}

// bareReporter satisfies starlarktest.Reporter
// (`interface{ Error(args ...any) }`) without depending on *testing.T.
// Records every Error call so the CLI can render indented detail under
// the FAIL line (D5-E1).
type bareReporter struct {
	failed   bool
	messages []string
}

func (b *bareReporter) Error(args ...any) {
	b.failed = true
	b.messages = append(b.messages, fmt.Sprint(args...))
}

func (b *bareReporter) allMessages() string {
	return strings.Join(b.messages, "\n")
}
