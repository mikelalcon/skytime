// Package dag holds the pure data graph types: Flow, Step, IfCond, Script,
// ForEachParallel, CallFlow, ActionRef, CapturedLambda, RetryPolicy. May not
// import starlark or temporal.
//
// This package also hosts the typed error spine (ParseError, ValidationError)
// because all four foundation packages (parser, extension, bridge, dag itself)
// need to construct/return them; placing the errors here avoids circular
// imports.
package dag
