//go:build windows

package cli

import (
	"io"
)

// liveRenderer on Windows is a no-op stub — the static path handles
// all output (D4.1-21 + the unix-only build constraint per quick
// 260501-p7c precedent). useLiveBlock() returns false on Windows
// because Windows stdout in cmd.exe rarely reports TTY=true via
// term.IsTerminal; if a test forced ForceTTY=true the live path would
// activate but every method is a no-op so output silently disappears.
//
// progressEvent is defined in pkg/cli/progress.go (no build tag) —
// shared between platforms so buildProgressEvent's field accesses
// compile on Windows even though the live path is never exercised.
type liveRenderer struct {
	out io.Writer
}

func newLiveRenderer(out io.Writer) *liveRenderer { return &liveRenderer{out: out} }
func (r *liveRenderer) submit(_ progressEvent)    {}
func (r *liveRenderer) Close()                    {}
