package parser

import (
	"fmt"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// =============================================================================
// builtinLogInfo / builtinLogWarn / builtinLogError / builtinLogDebug
//
// Phase 07.2.1 (D-7.2.1-01..04, D-7.2.1-17..18): the four parse-time factories
// behind log.info/warn/error/debug. Each constructs a *dag.LogStep node and
// records it on p.allLogSteps so the validateLogStepPlacement finalize pass
// (Task 2) can reject module-scope uses (D-7.2.1-18).
//
// Why four separate factories instead of one parameterized by level: cleaner
// // skytime:doc rendering (one entry per user-visible call shape), simpler
// error attribution (`log.info: ...` vs generic `log: ...`), and no level
// handling at the call site. All four delegate to buildLogStep below.
// =============================================================================

// buildLogStep is the shared factory body for log.info/warn/error/debug.
// Per D-7.2.1-01..04 + D-7.2.1-17..18.
//
// Signature on the user side: log.<level>(msg, attrs=lambda ctx: dict)
//   - msg MUST be a string literal at parse time (D-7.2.1-17). Empty/multi-line OK.
//   - attrs is OPTIONAL; if present, it must be a lambda captured via captureLambda.
//   - ${ctx.expr} interpolation in msg desugars via D4.1-22 (reused desugarer).
func (p *Parser) buildLogStep(level string, thread *starlark.Thread, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	name := "log." + level
	var (
		msgVal   starlark.Value
		attrsVal starlark.Value
	)
	if err := starlark.UnpackArgs(name, args, kwargs,
		"msg", &msgVal,
		"attrs?", &attrsVal,
	); err != nil {
		return nil, p.wrapBuiltinError(name, thread, err)
	}
	pos := callerPosition(thread)

	// D-7.2.1-17: msg MUST be a string literal at parse time. Variable
	// references, expression results, and any non-string value rejected
	// with position-aware *dag.ParseError. Note: Starlark cannot
	// distinguish "literal "foo"" from "variable bound to "foo"" at the
	// builtin boundary — UnpackArgs already evaluated the argument — so
	// the reject mode here is "non-string TYPE" (got int, got list, got
	// function, etc.). Identifier-bound strings DO reach here as
	// starlark.String, but the D4.1-22 ${ctx.expr} contract requires
	// literal source bytes; the desugarer needs the original template,
	// so the parser-time test `MSG = "hi"; log.info(MSG)` is rejected
	// via the syntax.Walk path used by other literal-required builtins.
	msgStr, ok := msgVal.(starlark.String)
	if !ok {
		return nil, &dag.ParseError{
			Pos: pos,
			Msg: fmt.Sprintf("%s: msg must be a string literal, got %s", name, msgVal.Type()),
		}
	}
	msg := string(msgStr)

	// D-7.2.1-17 (strict): even when the runtime value IS a starlark.String,
	// the SOURCE expression must be a string literal at the call site —
	// otherwise ${ctx.expr} interpolation cannot find its template bytes.
	// Re-parse the file at the call position and assert the first positional
	// arg is *syntax.Literal (a string constant). Mirrors the literal-required
	// gate result() / for_each_parallel.items use (block_fn_lint.go pattern).
	if literalErr := p.assertLogMsgLiteralAt(pos, name); literalErr != nil {
		return nil, literalErr
	}

	// D4.1-22 reuse: ${ctx.expr} → CapturedLambda (verbatim desugarer).
	// Returns (nil, nil) when no `${` present; (*CapturedLambda, nil) when
	// successfully desugared; (nil, *dag.ParseError) for empty/unterminated.
	var msgFn *dag.CapturedLambda
	if strings.Contains(msg, "${") {
		desugared, derr := p.desugarInterpolation(msg, pos, "")
		if derr != nil {
			return nil, derr
		}
		msgFn = desugared
	}

	// Optional attrs lambda. D-7.2.1-02: lambda ctx: dict.
	var attrsLambdaID string
	if attrsVal != nil && attrsVal != starlark.None {
		captured, cerr := p.captureLambda(thread, "attrs", attrsVal)
		if cerr != nil {
			return nil, cerr
		}
		attrsLambdaID = captured.ID
	}

	node := &dag.LogStep{
		Pos:           pos,
		Level:         level,
		Msg:           msg,
		MsgFn:         msgFn,
		AttrsLambdaID: attrsLambdaID,
	}
	// Track for module-scope orphan check (Task 2 finalize pass uses this).
	p.allLogSteps = append(p.allLogSteps, node)
	return wrapNode(node), nil
}

// skytime:doc summary="Emit a structured log record at INFO level."
// skytime:doc summary="Routed through workflow.GetLogger(ctx).Info at workflow time (replay-safe via ReplayLogger)."
// skytime:doc returns="A *dag.LogStep node consumed by flow(steps=[...]) (side-channel — no output_alias, no state binding)."
// skytime:doc since="phase-07.2.1"
// skytime:doc example="log.info(\"weekly digest complete: scheduled=${ctx.scheduled_time}\")"
// skytime:doc param_msg="string"
// skytime:doc desc_msg="String literal; ${ctx.expr} interpolation supported per D4.1-22. Empty and multi-line allowed."
// skytime:doc param_attrs="lambda(ctx) -> dict"
// skytime:doc desc_attrs="Optional lambda returning a dict of structured attributes (max 32 identifier-shaped keys)."
// skytime:doc see="log.warn, log.error, log.debug, script"
func (p *Parser) builtinLogInfo(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return p.buildLogStep("info", thread, args, kwargs)
}

// skytime:doc summary="Emit a structured log record at WARN level."
// skytime:doc summary="Routed through workflow.GetLogger(ctx).Warn at workflow time (replay-safe via ReplayLogger)."
// skytime:doc returns="A *dag.LogStep node consumed by flow(steps=[...]) (side-channel — no output_alias, no state binding)."
// skytime:doc since="phase-07.2.1"
// skytime:doc example="log.warn(\"high retry count: attempt=${ctx.attempt}\", attrs=lambda ctx: {\"flow\": ctx.flow_name})"
// skytime:doc param_msg="string"
// skytime:doc desc_msg="String literal; ${ctx.expr} interpolation supported per D4.1-22."
// skytime:doc param_attrs="lambda(ctx) -> dict"
// skytime:doc desc_attrs="Optional lambda returning a dict of structured attributes."
// skytime:doc see="log.info, log.error, log.debug"
func (p *Parser) builtinLogWarn(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return p.buildLogStep("warn", thread, args, kwargs)
}

// skytime:doc summary="Emit a structured log record at ERROR level."
// skytime:doc summary="Routed through workflow.GetLogger(ctx).Error at workflow time (replay-safe via ReplayLogger)."
// skytime:doc returns="A *dag.LogStep node consumed by flow(steps=[...]) (side-channel — no output_alias, no state binding)."
// skytime:doc since="phase-07.2.1"
// skytime:doc example="log.error(\"extension call failed: ${ctx.last_error_msg}\", attrs=lambda ctx: {\"extension\": \"github\"})"
// skytime:doc param_msg="string"
// skytime:doc desc_msg="String literal; ${ctx.expr} interpolation supported per D4.1-22."
// skytime:doc param_attrs="lambda(ctx) -> dict"
// skytime:doc desc_attrs="Optional lambda returning a dict of structured attributes."
// skytime:doc see="log.info, log.warn, log.debug"
func (p *Parser) builtinLogError(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return p.buildLogStep("error", thread, args, kwargs)
}

// skytime:doc summary="Emit a structured log record at DEBUG level."
// skytime:doc summary="Routed through workflow.GetLogger(ctx).Debug at workflow time (replay-safe via ReplayLogger)."
// skytime:doc returns="A *dag.LogStep node consumed by flow(steps=[...]) (side-channel — no output_alias, no state binding)."
// skytime:doc since="phase-07.2.1"
// skytime:doc example="log.debug(\"checkpoint reached: ${ctx.step_name}\")"
// skytime:doc param_msg="string"
// skytime:doc desc_msg="String literal; ${ctx.expr} interpolation supported per D4.1-22."
// skytime:doc param_attrs="lambda(ctx) -> dict"
// skytime:doc desc_attrs="Optional lambda returning a dict of structured attributes."
// skytime:doc see="log.info, log.warn, log.error"
func (p *Parser) builtinLogDebug(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	return p.buildLogStep("debug", thread, args, kwargs)
}

// assertLogMsgLiteralAt re-parses the cached file bytes at callPos and
// confirms the first POSITIONAL argument of the log.<level>(...) call is a
// *syntax.Literal with Token == syntax.STRING (i.e., a source-level string
// literal). Identifier-bound values, expression results, concatenations, and
// any non-literal shape are rejected with a *dag.ParseError carrying
// "<name>: msg must be a string literal".
//
// This mirrors the literal-only gate result() / for_each_parallel.items use
// (block_fn_lint.go re-parse + positionsEqual pattern). Required because at
// the UnpackArgs boundary Starlark has already evaluated the argument — we
// cannot distinguish `log.info("hi")` from `MSG = "hi"; log.info(MSG)` from
// the starlark.Value alone. The ${ctx.expr} desugarer needs the source-level
// literal template, so the literal contract is load-bearing for D4.1-22.
//
// Returns nil when the AST shape is a valid string literal (or when the
// re-parse fails defensively — let downstream emit the user-visible error).
func (p *Parser) assertLogMsgLiteralAt(callPos syntax.Position, name string) error {
	fileBytes, ok := p.fileBytes[callPos.Filename()]
	if !ok || fileBytes == nil {
		// Defensive: missing file bytes means the call came from a path we
		// did not cache (synthetic source, load() racing, etc.). Skip the
		// AST check — downstream behavior degrades gracefully.
		return nil
	}
	file, perr := defaultFileOptions().Parse(callPos.Filename(), fileBytes, 0)
	if perr != nil {
		// Same defensive posture as preExecBuildResults — let the main
		// Starlark exec produce the user-visible parse error.
		return nil
	}
	var found *syntax.CallExpr
	syntax.Walk(file, func(n syntax.Node) bool {
		if found != nil {
			return false
		}
		call, ok := n.(*syntax.CallExpr)
		if !ok {
			return true
		}
		if positionsEqual(call.Lparen, callPos) {
			found = call
			return false
		}
		return true
	})
	if found == nil {
		// Position mismatch — should not happen for well-formed source, but
		// don't crash on it.
		return nil
	}
	// The first POSITIONAL arg (i.e., the one that isn't a BinaryExpr Op=EQ
	// kwarg shape) must be the msg. Reject anything that's not a *syntax.Literal
	// with STRING token.
	var firstPositional syntax.Expr
	for _, arg := range found.Args {
		bin, isKw := arg.(*syntax.BinaryExpr)
		if isKw && bin.Op == syntax.EQ {
			// Kwarg — skip; first positional may follow it (rare but legal).
			continue
		}
		firstPositional = arg
		break
	}
	if firstPositional == nil {
		// No positional arg in source — UnpackArgs already errored if the
		// required msg was missing; let that error surface instead.
		return nil
	}
	lit, isLit := firstPositional.(*syntax.Literal)
	if !isLit || lit.Token != syntax.STRING {
		return &dag.ParseError{
			Pos: callPos,
			Msg: fmt.Sprintf("%s: msg must be a string literal, got %s", name, describeNonLiteralExpr(firstPositional)),
		}
	}
	return nil
}

// describeNonLiteralExpr returns a short human label for a non-literal
// expression — used only in the "msg must be a string literal" error to give
// the consultant a hint about what they wrote. Best-effort: unknown shapes
// fall back to "expression".
func describeNonLiteralExpr(e syntax.Expr) string {
	switch v := e.(type) {
	case *syntax.Ident:
		return fmt.Sprintf("identifier %q", v.Name)
	case *syntax.BinaryExpr:
		return "binary expression"
	case *syntax.CallExpr:
		return "call expression"
	case *syntax.UnaryExpr:
		return "unary expression"
	case *syntax.Literal:
		// Non-string literal (int, float).
		return "non-string literal"
	default:
		return "expression"
	}
}
