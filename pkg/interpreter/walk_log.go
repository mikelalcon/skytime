package interpreter

// walkLog routes a *dag.LogStep through workflow.GetLogger(ctx) at the
// matching slog level. Replay-safe by delegation: workflow.GetLogger
// returns a ReplayLogger that suppresses on replay (verified in
// go.temporal.io/sdk@v1.42.0/internal/log/replay_logger.go lines 27-29).
//
// Three-record event sequence per call (D-7.2.1-12):
//
//  1. step_dispatch slog record (kind=log, event=step_dispatch, level=<level>)
//     — suppressed by Plan 04's renderer in human mode (D-7.2.1-13);
//     emitted verbatim in JSON-log mode (D-7.2.1-14).
//  2. The level-appropriate logger.Debug/Info/Warn/Error call carrying the
//     resolved msg with the load-bearing "[skytime/log] " prefix
//     (mirrors the [skytime/print] precedent from D3-22).
//  3. step_complete slog record (kind=log, event=step_complete, status=ok|err)
//     — same suppression rules as the dispatch frame.
//
// Replay safety: NEVER captures the logger at construction. Always calls
// workflow.GetLogger(ctx) inline so the SDK's ReplayLogger does the
// right thing per dispatch (Pitfall 1 from 07.2.1-RESEARCH.md). Attrs
// dict iteration uses *starlark.Dict.Items() for insertion-order replay
// safety (Pitfall 3 / D4.1-14 precedent).

import (
	"fmt"
	"log/slog"
	"regexp"

	"go.starlark.net/starlark"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"github.com/mikelalcon/skytime/pkg/bridge"
	"github.com/mikelalcon/skytime/pkg/dag"
)

// maxLogAttrs is the hard-coded D-7.2.1-07 cap. Attrs dicts with more
// than this many entries are rejected at workflow time with NonRetryableErr.
const maxLogAttrs = 32

// reservedSlogKeys contains the standard slog field names that conflict
// with the structured-logging output if used as attr keys (D-7.2.1-06).
var reservedSlogKeys = map[string]bool{
	"level":  true,
	"msg":    true,
	"time":   true,
	"source": true,
}

// identifierShapeRe enforces D-7.2.1-08: attr keys must match this
// shape so downstream log consumers (slog handlers, json parsers, query
// engines) can rely on identifier-like attribute names.
var identifierShapeRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// walkLog dispatches a *dag.LogStep node at workflow execution time.
func (i *interpreter) walkLog(ctx workflow.Context, n *dag.LogStep) (err error) {
	logger := workflow.GetLogger(ctx)
	start := workflow.Now(ctx)
	path := i.currentPath()
	idx, total := i.stepIdx, i.stepTot
	// D-7.2.1: synthetic label for the dispatch/complete frames.
	// v1 does NOT support an id= kwarg on log.<level>; "log" suffices
	// for renderer attribution and is plenty for slog-consumer indexing.
	label := "log"

	logger.Info("skytime",
		"event", "step_dispatch",
		"kind", "log",
		"label", label,
		"level", n.Level,
		"idx", idx, "total", total, "path", path,
	)
	defer func() {
		status := "ok"
		summary := ""
		if err != nil {
			status = "err"
			summary = err.Error()
		}
		logger.Info("skytime",
			"event", "step_complete",
			"kind", "log",
			"label", label,
			"level", n.Level,
			"status", status,
			"duration_ms", workflow.Now(ctx).Sub(start).Milliseconds(),
			"idx", idx, "total", total, "path", path,
			"summary", summary,
		)
	}()

	// 1. Resolve the message — literal or interpolated.
	msg := n.Msg
	if n.MsgFn != nil {
		val, evalErr := i.evalLambda(ctx, n.MsgFn.ID)
		if evalErr != nil {
			return evalErr
		}
		s, ok := val.(starlark.String)
		if !ok {
			return temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("log.%s at %s: msg lambda returned %s, expected string", n.Level, n.Pos, val.Type()),
				"LogMsgTypeError", nil,
			)
		}
		msg = string(s)
	}

	// 2. Resolve attrs (optional). Iterate via *starlark.Dict.Items()
	//    for insertion-order replay safety (Pitfall 3 / D4.1-14).
	var keyvals []any
	if n.AttrsLambdaID != "" {
		val, evalErr := i.evalLambda(ctx, n.AttrsLambdaID)
		if evalErr != nil {
			return evalErr
		}
		d, ok := val.(*starlark.Dict)
		if !ok {
			return temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("log.%s at %s: attrs lambda returned %s, expected dict", n.Level, n.Pos, val.Type()),
				"LogAttrsTypeError", nil,
			)
		}
		// D-7.2.1-07 cap.
		if d.Len() > maxLogAttrs {
			return temporal.NewNonRetryableApplicationError(
				fmt.Sprintf("log.%s at %s: attrs dict has %d entries (max %d)", n.Level, n.Pos, d.Len(), maxLogAttrs),
				"LogAttrsTooLarge", nil,
			)
		}
		keyvals = make([]any, 0, d.Len()*2)
		for _, item := range d.Items() {
			ks, ok := item[0].(starlark.String)
			if !ok {
				return temporal.NewNonRetryableApplicationError(
					fmt.Sprintf("log.%s at %s: attr key must be string, got %s", n.Level, n.Pos, item[0].Type()),
					"LogAttrKeyType", nil,
				)
			}
			key := string(ks)
			// D-7.2.1-08 identifier-shape check.
			if !identifierShapeRe.MatchString(key) {
				return temporal.NewNonRetryableApplicationError(
					fmt.Sprintf("log.%s at %s: attr key %q not identifier-shaped (must match ^[a-zA-Z_][a-zA-Z0-9_]*$)", n.Level, n.Pos, key),
					"LogAttrKeyShape", nil,
				)
			}
			// D-7.2.1-06 reserved-key check.
			if reservedSlogKeys[key] {
				return temporal.NewNonRetryableApplicationError(
					fmt.Sprintf("log.%s at %s: attr key %q is reserved (slog standard field)", n.Level, n.Pos, key),
					"LogAttrKeyReserved", nil,
				)
			}
			// D-7.2.1-05 type-aware coercion.
			attr, attrErr := starlarkValueToSlogAttr(key, item[1])
			if attrErr != nil {
				return temporal.NewNonRetryableApplicationError(
					fmt.Sprintf("log.%s at %s: attr %q: %s", n.Level, n.Pos, key, attrErr),
					"LogAttrValueConvert", nil,
				)
			}
			keyvals = append(keyvals, attr.Key, attr.Value.Any())
		}
	}

	// 3. Route through workflow.GetLogger(ctx) at the matching level.
	// [skytime/log] prefix at the source mirrors [skytime/print] (D3-22).
	prefixedMsg := "[skytime/log] " + msg
	switch n.Level {
	case "debug":
		logger.Debug(prefixedMsg, keyvals...)
	case "info":
		logger.Info(prefixedMsg, keyvals...)
	case "warn":
		logger.Warn(prefixedMsg, keyvals...)
	case "error":
		logger.Error(prefixedMsg, keyvals...)
	default:
		// Unreachable in practice — parser validates level to one of the
		// four. Defense in depth so any future drift surfaces clearly.
		return temporal.NewNonRetryableApplicationError(
			fmt.Sprintf("log: unknown level %q at %s", n.Level, n.Pos),
			"LogUnknownLevel", nil,
		)
	}
	return nil
}

// starlarkValueToSlogAttr converts a Starlark value to a typed slog.Attr,
// falling back to slog.Any (via bridge.FromStarlarkValue) for non-scalar
// types. Per D-7.2.1-05.
func starlarkValueToSlogAttr(key string, v starlark.Value) (slog.Attr, error) {
	switch sv := v.(type) {
	case starlark.NoneType:
		return slog.Any(key, nil), nil
	case starlark.String:
		return slog.String(key, string(sv)), nil
	case starlark.Bool:
		return slog.Bool(key, bool(sv)), nil
	case starlark.Int:
		i64, ok := sv.Int64()
		if !ok {
			// Big int beyond int64 range — fall back to string repr.
			return slog.String(key, sv.String()), nil
		}
		return slog.Int64(key, i64), nil
	case starlark.Float:
		return slog.Float64(key, float64(sv)), nil
	default:
		// Non-scalar (list, dict, tuple, struct, ...) → bridge conversion + slog.Any.
		goVal, err := bridge.FromStarlarkValue(v)
		if err != nil {
			return slog.Attr{}, err
		}
		return slog.Any(key, goVal), nil
	}
}
