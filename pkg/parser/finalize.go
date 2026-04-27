package parser

import (
	"fmt"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// finalize runs after starlark.ExecFileOptions returns. It executes the
// post-exec passes that depend on the full parser session being populated:
//
//  1. resolveCallFlows (D-16): walk every Flow.Body recursively for
//     *dag.CallFlow nodes; look up CallFlow.Name in p.flows; set Resolved
//     or return *dag.ParseError "call_flow target not found".
//  2. validateActionRefKwargs (D-11 defense in depth): a no-op in Phase 1
//     because real extension factories validate kwargs at construction
//     time inside their *starlark.Builtin via UnpackOperationKwargs. The
//     finalize pass is the seat for Phase 4's static validator to plug
//     into without restructuring the parser.
//
// finalize returns the FIRST error and stops; tests expect at-most-one
// surfaced error per parse.
func (p *Parser) finalize() error {
	if err := p.resolveCallFlows(); err != nil {
		return err
	}
	return p.validateActionRefKwargs()
}

// resolveCallFlows walks every flow's body recursively and resolves
// *dag.CallFlow nodes against the parser-session flow map (D-16). A call
// to a flow that wasn't declared anywhere in the parsed session yields a
// *dag.ParseError; this is a parse-time check, never runtime.
func (p *Parser) resolveCallFlows() error {
	for _, flow := range p.flows {
		if err := p.walkResolveCallFlows(flow.Body); err != nil {
			return err
		}
	}
	return nil
}

// walkResolveCallFlows is the recursive helper for resolveCallFlows. It
// descends into IfCond.Then/Else and ForEachParallel.Steps so nested
// call_flow() invocations are resolved.
func (p *Parser) walkResolveCallFlows(body []dag.Node) error {
	for _, node := range body {
		switch n := node.(type) {
		case *dag.CallFlow:
			target, ok := p.flows[n.Name]
			if !ok {
				return &dag.ParseError{
					Pos: n.Pos,
					Msg: fmt.Sprintf("call_flow target not found: %q", n.Name),
				}
			}
			n.Resolved = target
		case *dag.IfCond:
			if err := p.walkResolveCallFlows(n.Then); err != nil {
				return err
			}
			if err := p.walkResolveCallFlows(n.Else); err != nil {
				return err
			}
		case *dag.ForEachParallel:
			if err := p.walkResolveCallFlows(n.Steps); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateActionRefKwargs is a no-op in Phase 1. Documentation in
// finalize() above explains why: extension factories validate at
// construction time. Phase 4 will plug a static validator here.
func (p *Parser) validateActionRefKwargs() error {
	return nil
}
