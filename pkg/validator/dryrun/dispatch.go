package dryrun

import (
	"context"

	"github.com/mikelalcon/skytime/pkg/activity"
	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/extension"
)

// AlwaysOkDispatch wraps each registered op's Func with one that returns
// (nil, nil) — the activity layer encodes that as OkResult{Output: nil}.
//
// The wrap preserves Name, Idempotent, KwargsType, and DefaultTimeout so
// the activity's own kwarg-shape checks (extension.DecodeKwargsFromDict)
// still fire on bad inputs. The only thing replaced is the I/O — the
// dry-run never makes a real network/filesystem call.
//
// Used by tests/differential_test.go's TestDifferentialCorpus to run every
// .star file in examples/skeleton/ through the full SkytimeWorkflow without
// standing up real backends.
//
// Returns an empty (non-nil) OperationDispatch when exts is nil/empty —
// matches the activity-side expectation that dispatch is a non-nil map.
func AlwaysOkDispatch(exts []extension.Extension) activity.OperationDispatch {
	d := activity.OperationDispatch{}
	for _, e := range exts {
		extName := e.Name()
		for opName, spec := range e.Operations() {
			if spec == nil {
				continue
			}
			wrapped := *spec // shallow copy — KwargsType/Idempotent are pointers, OK to share
			wrapped.Func = okFunc
			d[extName+"."+opName] = wrapped
		}
	}
	return d
}

// okFunc is the always-OK OperationFunc. Returns nil typed Output (legal
// per pkg/extension/operation.go doc: "Returning nil for output is
// permitted") and nil error → activity emits OkResult{Output: nil} which
// the interpreter treats as a clean success.
func okFunc(_ context.Context, _ any, _ extension.Credential) (dag.OperationOutput, error) {
	return nil, nil
}
