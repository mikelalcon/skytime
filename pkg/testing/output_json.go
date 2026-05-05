package testing

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// JSONEvent mirrors stdlib cmd/test2json's TestEvent. Field tags use
// capitalized JSON keys EXACTLY as test2json emits them so existing
// CI tooling (gotestsum, tparse, GitHub Actions test annotations,
// JUnit converters) parses Skytime test output without per-tool
// shims.
//
// Open Question 6 accepted: Time is RFC3339Nano in UTC.
//
// Action values Plan 05 emits: start, run, output, pass, fail, skip.
// Plan 05 does NOT emit: bench, pause, cont (no benchmarks; sequential
// within file per D5-E5 + RESEARCH Open Q2).
type JSONEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test,omitempty"`
	Elapsed float64   `json:"Elapsed,omitempty"`
	Output  string    `json:"Output,omitempty"`
}

// jsonEmitter writes one JSON record per event followed by a newline
// (json.Encoder appends \n on every Encode call). Concurrency-safe
// via the runner's sequential-within-file iteration; no internal
// locking added (D5-E5).
type jsonEmitter struct {
	w   io.Writer
	enc *json.Encoder
}

func newJSONEmitter(w io.Writer) *jsonEmitter {
	return &jsonEmitter{w: w, enc: json.NewEncoder(w)}
}

// emit writes one event. Time is captured at emit time as
// time.Now().UTC() so consumers see monotonically-increasing
// timestamps even if the OS clock is in a different zone (Open Q6).
func (j *jsonEmitter) emit(action, pkg, test, output string, elapsed float64) {
	ev := JSONEvent{
		Time:    time.Now().UTC(),
		Action:  action,
		Package: pkg,
		Test:    test,
		Elapsed: elapsed,
		Output:  output,
	}
	_ = j.enc.Encode(ev)
}

// formatHumanLine returns the static D5-E1 line for a finished test.
//
//	PASS → "--- PASS: <test> (<elapsed:.2f>s)\n"
//	FAIL → "--- FAIL: <test> (<elapsed:.2f>s)\n"
//	SKIP → "--- SKIP: <test> (<elapsed:.2f>s)\n"
//
// Mirrors `go test -v` output so consultants familiar with Go test
// runs read Skytime test output without context-switching.
func formatHumanLine(action, test string, elapsed float64) string {
	return fmt.Sprintf("--- %s: %s (%.2fs)\n", action, test, elapsed)
}
