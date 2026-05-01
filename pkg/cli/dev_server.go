package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// lookPath is the test seam — production assigns exec.LookPath; tests
// override to simulate missing binary or to redirect to a fake (sleep).
var lookPath = exec.LookPath

// testRunningCmd is the W-8 second test seam — production sets it
// inside RunE just after sub.Start() and clears it on RunE return via
// defer. Tests (TestDevServerCmd_SignalForward) read this to dispatch
// signals directly at the running subprocess. Nil when no subprocess
// is live.
var testRunningCmd *exec.Cmd

// newDevServerCommand returns the skytime dev-server subcommand.
//
// CLI-04: spawns `temporal server start-dev` as a subprocess.
//   - D4-09: subprocess (NOT embedded Temporalite Go dep).
//   - D4-10: foreground + SIGINT/SIGTERM forwarding to the subprocess.
//   - D4-11: DisableFlagParsing — pass user flags verbatim downstream.
//   - D4-12: missing binary → clear install instructions.
func newDevServerCommand(cfg *config) *cobra.Command {
	_ = cfg // dev-server ignores connection flags (D4-08); accept cfg for API symmetry with other subcommands
	return &cobra.Command{
		Use:                "dev-server",
		Short:              "Spawn a local Temporal dev server (`temporal server start-dev`)",
		Long:               "Wraps `temporal server start-dev` — requires the temporal CLI on PATH. Flags after `dev-server` are forwarded verbatim to the subprocess.",
		DisableFlagParsing: true, // D4-11: every arg passes through
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			bin, err := lookPath("temporal")
			if err != nil {
				printMissingTemporalBinary(cmd.ErrOrStderr())
				return errSilent
			}

			subArgs := append([]string{"server", "start-dev"}, args...)
			sub := exec.CommandContext(ctx, bin, subArgs...)
			sub.Stdin = os.Stdin
			sub.Stdout = cmd.OutOrStdout()
			sub.Stderr = cmd.ErrOrStderr()
			// No SysProcAttr.Setpgid — keep the subprocess in the
			// parent's process group so terminal Ctrl-C reaches it
			// naturally. The signal-forwarding goroutine below is
			// defense-in-depth (works in non-tty contexts where the
			// process-group signal might not propagate).

			if err := sub.Start(); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "start temporal: %s\n", err.Error())
				return errSilent
			}
			// W-8 test seam: expose the running *exec.Cmd so behavioral
			// tests (TestDevServerCmd_SignalForward) can dispatch a
			// signal to the SUBPROCESS rather than to the test process.
			// Production sets and reads through this seam too — there's
			// no behavior difference, only test observability.
			testRunningCmd = sub
			defer func() { testRunningCmd = nil }()

			// Signal forwarding goroutine. Stops when sub.Wait returns
			// and we close the sigCh via signal.Stop + close.
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			defer signal.Stop(sigCh)
			forwardingDone := make(chan struct{})
			go func() {
				defer close(forwardingDone)
				for sig := range sigCh {
					// Subprocess may already be gone — Signal returns
					// an error in that case; drop it.
					_ = sub.Process.Signal(sig)
				}
			}()

			if err := sub.Wait(); err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					// Mirror subprocess non-zero exit.
					return errSilent
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "wait: %s\n", err.Error())
				return errSilent
			}
			return nil
		},
	}
}

// printMissingTemporalBinary writes the D4-12 install instruction
// block to out. Format is verbatim per the locked decision.
func printMissingTemporalBinary(out io.Writer) {
	fmt.Fprintln(out, "error: `temporal` CLI not found on PATH.")
	fmt.Fprintln(out, "Install:")
	fmt.Fprintln(out, "  macOS:   brew install temporal")
	fmt.Fprintln(out, "  script:  curl -sSf https://temporal.download/cli.sh | sh")
	fmt.Fprintln(out, "  Go:      go install go.temporal.io/server/cmd/temporal@latest")
}
