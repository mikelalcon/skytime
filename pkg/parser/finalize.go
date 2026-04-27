package parser

// finalize runs after starlark.ExecFileOptions returns. Task 1 ships the
// hook signature; tasks 2-3 wire the actual passes:
//
//  1. resolveCallFlows (D-16): walk every Flow.Body recursively for
//     *dag.CallFlow nodes; look up CallFlow.Name in p.flows; set Resolved
//     or return *dag.ParseError "call_flow target not found".
//  2. validateActionRefKwargs (D-11 defense in depth): for every ActionRef
//     embedded in Step.Actions or ForEachParallel.Steps, look up the
//     OperationSpec and re-run UnpackOperationKwargs as a safety net.
//
// Phase 1 keeps validateActionRefKwargs mostly a no-op: real extension
// factories validate at construction time inside their *starlark.Builtin.
// The finalize pass is the seat for Phase 4's static validator to plug into.
func (p *Parser) finalize() error {
	if err := p.resolveCallFlows(); err != nil {
		return err
	}
	if err := p.validateActionRefKwargs(); err != nil {
		return err
	}
	return nil
}

// resolveCallFlows is wired in task 2. Stubbed in task 1.
func (p *Parser) resolveCallFlows() error {
	return nil
}

// validateActionRefKwargs is wired in task 2. Stubbed in task 1.
func (p *Parser) validateActionRefKwargs() error {
	return nil
}
