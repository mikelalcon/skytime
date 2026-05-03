//go:build !windows

package cli

import (
	"io"
)

// liveRenderer — Phase 04.1-06 Task 2 (filled in next commit) replaces
// this stub with the real render goroutine + spinner + truncation
// implementation per D4.1-17..21. For Task 1, the stub satisfies the
// progressHandler.live field type so the package compiles while the
// static-path refactor lands. The Task 1 plan explicitly notes
// "Handle() simply routes through the static path for both TTY/non-TTY
// for now (no behavior change yet)" — useLiveBlock() is wired but the
// live path is dormant because no test triggers it.
type liveRenderer struct {
	out io.Writer
}

func newLiveRenderer(out io.Writer) *liveRenderer { return &liveRenderer{out: out} }
func (r *liveRenderer) submit(_ progressEvent)    {}
func (r *liveRenderer) Close()                    {}
