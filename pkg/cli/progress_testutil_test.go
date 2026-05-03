package cli

// Shared test helpers for the progress + live-block test files. No build
// tag — usable from both platform-agnostic tests (progress_test.go) and
// the unix-only live-block tests (progress_live_test.go).

import (
	"bytes"
	"strings"
	"sync"
)

// safeBuffer wraps bytes.Buffer with a mutex so the live-render goroutine
// can write while tests read. bytes.Buffer is NOT safe for concurrent
// use; the production liveRenderer guarantees a single writer (its own
// goroutine) but tests read the buffer while writes may still be
// in-flight (between submit() and Close()).
type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// stripAnsiTest removes all ANSI CSI escape sequences from s. Used by
// live-block tests to assert "the static-line residue after redraw
// region is cleared" without coupling to color codes. Mirrors the
// production helper but lives here so tests on both platforms have
// access without the !windows build tag carrying through.
func stripAnsiTest(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			// Skip CSI sequence: ESC '[' ... <final byte 0x40-0x7E>
			j := i + 2
			for j < len(s) {
				c := s[j]
				if (c >= 0x40 && c <= 0x7E) {
					j++
					break
				}
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
