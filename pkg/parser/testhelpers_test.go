package parser

// stringPtr is a helper for syntax.MakePosition's *string filename arg.
// Defined here (instead of inside any single test file) so every white-box
// test in `package parser` can reference it without duplication. Stays in
// `*_test.go` so it never escapes test builds.
func stringPtr(s string) *string { return &s }
