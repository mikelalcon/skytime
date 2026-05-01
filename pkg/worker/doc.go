// Package worker hosts the Skytime worker: filesystem-backed flow registry
// boot, three named client constructors for Temporal cluster variants, and the
// lifecycle wrapper around go.temporal.io/sdk/worker.
//
// FIREWALL: this is one of three packages allowed to import
// go.temporal.io/sdk/* (the others being pkg/activity (Phase 2) and
// pkg/interpreter (Phase 3)). Every other pkg/* directory is firewall-blocked
// by pkg/activity/firewall_test.go::TestNoTemporalImportsOutsideAllowList.
//
// Library-embed pattern (WORK-03):
//
//	c, err := worker.NewCloudClient(worker.CloudOptions{
//	    HostPort:  "us-west-2.aws.api.temporal.io:7233",
//	    Namespace: "my-namespace",
//	    APIKey:    os.Getenv("TEMPORAL_API_KEY"),
//	})
//	if err != nil { ... }
//	defer c.Close()
//
//	w, err := worker.NewWorker(c, worker.WorkerOptions{
//	    RootDir:           "./flows",
//	    BuildID:           os.Getenv("BUILD_ID"), // optional; -ldflags default
//	    Extensions:        []extension.Extension{...},
//	    CredentialHandler: myhandler,
//	})
//	if err := w.Start(); err != nil { ... }
//
//	// graceful shutdown
//	var stopOnce sync.Once
//	shutdown := func() { stopOnce.Do(w.Stop) }
//	defer shutdown()
//	<-sigChan
//	shutdown()
//
// Lifecycle (D3-18): Start() is non-blocking; Stop() is sync.Once-wrapped
// internally to prevent panic on double-call (RESEARCH §Pitfall 5). A caller
// wrap with sync.Once is still recommended for clarity in main.go.
//
// Build IDs (D3-20): Workflow versioning uses Temporal Build IDs. The default
// is "dev" — overridable via:
//
//	go build -ldflags "-X github.com/mikelalcon/skytime/pkg/worker.defaultBuildID=$(git rev-parse HEAD)"
//
// Without an override, BuildID == "dev" — fine for development; a footgun in
// production (RESEARCH §Pitfall 7).
package worker
