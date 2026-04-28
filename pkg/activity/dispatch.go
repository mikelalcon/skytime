package activity

import "github.com/mikelalcon/skytime/pkg/extension"

// OperationDispatch is the parser-finalize-time-built lookup table from
// ActionRef.Kind_ ("github.create_issue") to the OperationSpec the activity
// should call. Built once per worker by Phase 3's bootstrap; passed to
// activity.New as the first argument.
//
// Key shape: "<extName>.<opName>", matching ActionRef.Kind_ verbatim. The
// activity does NOT parse the key into parts — it does a direct map lookup
// using ActionRef.Kind_ as-is.
//
// D2-17: parser does NOT import pkg/activity; activity does NOT import
// pkg/parser. The dispatch map is the only seam between them — a parser
// produces it via a finalize hook that walks its registry, the activity
// consumes it without ever knowing how it was built.
type OperationDispatch map[string]extension.OperationSpec
