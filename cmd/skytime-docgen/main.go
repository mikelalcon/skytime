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
		outPath = flag.String("out", "", "Output path for rendered markdown (UNUSED; populated by Phase 04.3 plan 02)")
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

	// Plan 02 replaces this JSON dump with markdown rendering via text/template.
	if *outPath == "" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(builtins); err != nil {
			fmt.Fprintf(os.Stderr, "encode: %v\n", err)
			os.Exit(1)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "skytime-docgen: --out is implemented by Phase 04.3 plan 02")
	os.Exit(2)
}
