package dag

// ActionRef is a placeholder filled in by plan 01-02 task 3 (real implementation
// makes ActionRef a custom starlark.Value with recursive Freeze).
//
// This stub exists only so Step.Actions ([]*ActionRef) and the rest of the
// node spine can compile after task 1 lands. Task 3 replaces this file.
type ActionRef struct{}
