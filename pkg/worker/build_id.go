package worker

// defaultBuildID is the default Temporal Build ID assigned to workers when
// WorkerOptions.BuildID is empty. Overridable via:
//
//	go build -ldflags "-X github.com/mikelalcon/skytime/pkg/worker.defaultBuildID=$(git rev-parse HEAD)"
//
// Set to "dev" by default — appropriate for development. Production deploys
// MUST override via -ldflags or by setting WorkerOptions.BuildID explicitly,
// or workflow tasks will get pinned to "dev" and survive across .star edits,
// causing determinism panics (RESEARCH §Pitfall 7).
//
// The variable is package-level (not const) precisely so -ldflags -X can
// override it at build time. Do not change to const.
var defaultBuildID = "dev"
