package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/interpreter"
	"github.com/mikelalcon/skytime/pkg/parser"
)

// bootRegistry walks rootDir, parses every .star file (recursively), computes
// each file's content_hash (sha256 of file bytes), and registers the flows it
// declares in a fresh FlowRegistry. Returns the frozen registry on success.
//
// Errors propagate from filesystem walk, parse failures, and registry
// duplicate-flow detection.
//
// Extensions are registered with the parser BEFORE any file is parsed so
// extension factory references in .star files (e.g. `gh = github.endpoint("admin")`)
// resolve at parse time.
//
// Files are processed in sorted-path order for determinism (D3-23): the same
// directory contents always produce the same registry.
//
// Empty rootDir (no .star files) returns a non-nil empty frozen registry —
// the worker still boots, just with no registered flows. This is a deliberate
// choice (consistent with consumer "I'll add flows later" workflows); flows
// never resolve at runtime, so workflow start fails cleanly via the
// FlowNotInRegistry error path in interpreter.NewWorkflow.
func bootRegistry(rootDir string, exts []extension.Extension) (*interpreter.FlowRegistry, error) {
	if rootDir == "" {
		return nil, fmt.Errorf("bootRegistry: rootDir required")
	}
	if _, err := os.Stat(rootDir); err != nil {
		return nil, fmt.Errorf("bootRegistry: rootDir %s: %w", rootDir, err)
	}

	// One Parser instance for the whole boot — supports load() across files
	// (D-13) and shares the in-parser flow-name-uniqueness check.
	parserOpts := []parser.Option{parser.WithRoot(rootDir)}
	for _, e := range exts {
		parserOpts = append(parserOpts, parser.WithExtensions(e))
	}
	p, err := parser.NewParser(parserOpts...)
	if err != nil {
		return nil, fmt.Errorf("bootRegistry: parser init: %w", err)
	}

	// Walk rootDir and collect .star paths.
	var starFiles []string
	if err := filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".star" {
			starFiles = append(starFiles, path)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("bootRegistry: walk: %w", err)
	}

	// Sort starFiles for deterministic content_hash assignment + parse order
	// (FS walk order is not guaranteed stable across runs / platforms).
	sort.Strings(starFiles)

	// Compute content_hash per file BEFORE parsing — the parser may load()
	// across files but each file's source bytes are independently hashable.
	fileHashes := make(map[string]string, len(starFiles)) // absolute path → hex hash
	for _, path := range starFiles {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("bootRegistry: abs %s: %w", path, err)
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("bootRegistry: read %s: %w", absPath, err)
		}
		sum := sha256.Sum256(data)
		fileHashes[absPath] = hex.EncodeToString(sum[:])
	}

	// Parse each file. The parser accumulates flows by name across files.
	// Errors from any file abort the boot.
	for _, path := range starFiles {
		if _, err := p.ParseFile(path); err != nil {
			return nil, fmt.Errorf("bootRegistry: parse %s: %w", path, err)
		}
	}

	// Build the registry. Each flow gets registered under
	// (flow_name, content_hash_of_owning_file). The owning file is determined
	// by the flow's Pos.Filename(); the parser stores absolute paths so
	// fileHashes lookup matches directly.
	reg := interpreter.NewRegistry()

	// All captured lambdas across all files. Lambda IDs are globally unique
	// (D-18: sha256(fileBytes)[:8] + ":" + line + ":" + col), so sharing one
	// map across all ParsedFlow entries is correct — a flow only ever invokes
	// the LambdaIDs that appear inside its own DAG.
	lambdas := p.Lambdas()

	// Sort flow names for deterministic registry contents.
	flows := p.Flows()
	flowNames := make([]string, 0, len(flows))
	for name := range flows {
		flowNames = append(flowNames, name)
	}
	sort.Strings(flowNames)

	for _, name := range flowNames {
		f := flows[name]
		owningPath := f.Pos.Filename()
		hash, ok := fileHashes[owningPath]
		if !ok {
			return nil, fmt.Errorf("bootRegistry: flow %s defined in %s but file not in walk", name, owningPath)
		}
		parsed := &interpreter.ParsedFlow{
			Flow:    f,
			Lambdas: lambdas,
		}
		if err := reg.Register(name, hash, parsed); err != nil {
			return nil, fmt.Errorf("bootRegistry: register %s@%s: %w", name, hash, err)
		}
	}

	reg.Freeze()
	return reg, nil
}
