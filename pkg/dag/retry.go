package dag

// RetryPolicy and Timeout are placeholders filled in by plan 01-02 task 2.
//
// Real implementation gives them time.Duration fields and starlark.Unpacker
// methods so step()/for_each_parallel() can decode them from Starlark dicts.
// Task 2 replaces this file.
type RetryPolicy struct{}

// Timeout is a placeholder; see RetryPolicy comment.
type Timeout struct{}
