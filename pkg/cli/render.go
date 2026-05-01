package cli

import (
	"log/slog"
)

// setupLogging is the W3 Task 1 stub — Task 2 of plan 04-04 replaces
// it with the charm-log handler + TTY detection. Returning
// slog.Default() keeps PersistentPreRunE wiring testable in isolation.
func setupLogging(debug bool) *slog.Logger {
	_ = debug
	return slog.Default()
}
