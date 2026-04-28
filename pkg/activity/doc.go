// Package activity is the single Temporal-SDK adapter in Skytime.
//
// FIREWALL: this is the ONLY package allowed to import any go.temporal.io/sdk/*
// path. Every other package (pkg/dag, pkg/extension, pkg/parser, pkg/bridge)
// is forbidden from doing so by an in-package firewall test
// (firewall_test.go) that walks Go AST imports across the module. Crossing
// the firewall would re-introduce the workflow.Context coupling PROJECT.md
// "no context bleed" forbids and would make the four pure-data packages
// unbuildable without Temporal — defeating standalone testing.
//
// Phase 2 lands the package's foundations: an OperationDispatch table
// (D2-17), a per-worker credential cache with TTL and retry-aware bypass
// (D2-10/D2-11), a heartbeat payload (D2-16), an attempt extraction seam
// for D2-11 testability, an error classifier per D2-12, and the Activity
// struct + functional options. The ExecuteBatch entry point itself lands
// in 02-03.
//
// Wire shape: Activity is constructed at worker startup via
//
//	impl, err := activity.New(dispatch, handler, opts...)
//	if err != nil {
//	    // ...
//	}
//	w.RegisterActivityWithOptions(impl.ExecuteBatch, sdkactivity.RegisterOptions{Name: "ExecuteBatch"})
//
// (the second line lives in Phase 3's worker bootstrap; Phase 2 ships only
// the constructor and — in 02-03 — the registrable method.)
//
// D2-18: package name is `activity` despite the import-path collision with
// go.temporal.io/sdk/activity. Consumers disambiguate via aliased imports
// (e.g., `sdkactivity "go.temporal.io/sdk/activity"`) when both are reachable
// from the same file. Phase 3's worker bootstrap is the only place that needs
// to do this.
package activity
