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
		pkgPath = flag.String("pkg", "pkg/parser", "Package directory containing globals.go and builtins.go")
		outPath = flag.String("out", "", "Output path for rendered markdown; when empty, JSON is dumped to stdout for diagnostics")
	)
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "skytime-docgen — extract Skytime Starlark builtin docs from source")
		fmt.Fprintln(os.Stderr, "Usage: skytime-docgen [--pkg <path>] [--out <path>]")
		flag.PrintDefaults()
	}
	flag.Parse()

	globalsPath := filepath.Join(*pkgPath, "globals.go")
	builtinsPath := filepath.Join(*pkgPath, "builtins.go")

	registry, order, err := WalkRegistry(globalsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk registry %s: %v\n", globalsPath, err)
		os.Exit(1)
	}
	builtins, err := WalkBuiltins(builtinsPath, registry, order)
	if err != nil {
		fmt.Fprintf(os.Stderr, "walk builtins %s: %v\n", builtinsPath, err)
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
