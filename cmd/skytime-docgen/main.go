package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	var (
		pkgPath = flag.String("pkg", "pkg/parser", "Package directory containing globals.go and builtins*.go")
		outPath = flag.String("out", "", "Output path for rendered markdown; when empty, JSON is dumped to stdout for diagnostics")
	)
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "skytime-docgen — extract Skytime Starlark builtin docs from source")
		fmt.Fprintln(os.Stderr, "Usage: skytime-docgen [--pkg <path>] [--out <path>]")
		flag.PrintDefaults()
	}
	flag.Parse()

	globalsPath := filepath.Join(*pkgPath, "globals.go")

	registry, order, err := WalkRegistry(globalsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk registry %s: %v\n", globalsPath, err)
		os.Exit(1)
	}

	// Walk every `builtins*.go` file in *pkgPath (excluding `*_test.go`).
	// Originally a single `builtins.go` carried all factories; Phase 07.2.1
	// added `builtins_log.go` for the four log.<level> trampolines, and
	// future namespaced surfaces may add more split files. The merged
	// result is deduped by Starlark name; the registration order from
	// globals.go is the source of truth for emission order.
	builtinFiles, err := findBuiltinFiles(*pkgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "find builtins in %s: %v\n", *pkgPath, err)
		os.Exit(1)
	}
	if len(builtinFiles) == 0 {
		fmt.Fprintf(os.Stderr, "no builtins*.go files found in %s\n", *pkgPath)
		os.Exit(1)
	}
	builtins, err := WalkBuiltinsMulti(builtinFiles, registry, order)
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk builtins in %s: %v\n", *pkgPath, err)
		os.Exit(1)
	}

	// --out renders markdown to the specified file; otherwise JSON-to-stdout
	// for diagnostics (preserves the plan-01 transitional shape so contributors
	// can still inspect Markers/Params via `go run ./cmd/skytime-docgen/`).
	if *outPath != "" {
		f, ferr := os.Create(*outPath)
		if ferr != nil {
			fmt.Fprintf(os.Stderr, "create %s: %v\n", *outPath, ferr)
			os.Exit(1)
		}
		defer f.Close()
		if rerr := RenderMarkdown(f, builtins); rerr != nil {
			fmt.Fprintf(os.Stderr, "render: %v\n", rerr)
			os.Exit(1)
		}
		return
	}

	// Default (no --out): JSON dump for diagnostics.
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(builtins); err != nil {
		fmt.Fprintf(os.Stderr, "encode: %v\n", err)
		os.Exit(1)
	}
}
