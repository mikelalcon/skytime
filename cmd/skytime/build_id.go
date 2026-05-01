package main

// defaultBuildID is the git commit SHA injected at link time:
//
//	go build -ldflags "-X main.defaultBuildID=$(git rev-parse HEAD)" ./cmd/skytime
//
// The "dev" fallback supports running unbuilt binaries from `go run`
// during local development. Mirrors D3-20's BuildID pattern: pkg/worker
// has its own defaultBuildID for the embedded-worker path, and
// cmd/skytime carries this one for the CLI binary itself.
//
// Use sites: a future enhancement may surface this on `--version` or
// route it into Temporal's worker Identity. v1 keeps the variable
// declared and unused-but-referenced (see main.go's `_ = defaultBuildID`)
// so ldflags injection works the day someone wires it.
var defaultBuildID = "dev"
