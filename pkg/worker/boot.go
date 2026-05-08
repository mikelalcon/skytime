package worker

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mikelalcon/skytime/pkg/extension"
	"github.com/mikelalcon/skytime/pkg/interpreter"
	"github.com/mikelalcon/skytime/pkg/parser"
)

// bootRegistry walks rootDir, parses every .star file (recursively), computes
// each file's content_hash (sha256 of file bytes), and registers the flows
// AND triggers it declares in fresh registries. Returns the frozen registries
// on success.
//
// Phase 7 extension: the parse loop drains Parser.Triggers() in addition to
// Parser.Flows(); the same parser session collects both. Trigger warnings
// (Parser.TriggerWarnings(), e.g., D-07-13 byte-identical duplicate-trigger
// warnings) are surfaced via slog.Warn against slog.Default() at boot time.
//
// Errors propagate from filesystem walk, parse failures, and registry
// registration. Trigger warnings do NOT propagate as errors — they're purely
// informational.
//
// Extensions are registered with the parser BEFORE any file is parsed so
// extension factory references in .star files (e.g. `gh = github.endpoint("admin")`)
// resolve at parse time.
//
// Files are processed in sorted-path order for determinism (D3-23): the same
// directory contents always produce the same registry.
//
// Empty rootDir (no .star files) returns non-nil empty frozen registries —
// the worker still boots, just with no registered flows or triggers. This is
// a deliberate choice (consistent with consumer "I'll add flows later"
// workflows); flows never resolve at runtime, so workflow start fails
// cleanly via the FlowNotInRegistry error path in interpreter.NewWorkflow.
func bootRegistry(rootDir string, exts []extension.Extension) (*interpreter.FlowRegistry, *interpreter.TriggerRegistry, error) {
	if rootDir == "" {
		return nil, nil, fmt.Errorf("bootRegistry: rootDir required")
	}
	if _, err := os.Stat(rootDir); err != nil {
		return nil, nil, fmt.Errorf("bootRegistry: rootDir %s: %w", rootDir, err)
	}

	// One Parser instance for the whole boot — supports load() across files
	// (D-13) and shares the in-parser flow-name-uniqueness check.
	parserOpts := []parser.Option{parser.WithRoot(rootDir)}
	for _, e := range exts {
		parserOpts = append(parserOpts, parser.WithExtensions(e))
	}
	p, err := parser.NewParser(parserOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("bootRegistry: parser init: %w", err)
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
		if filepath.Ext(path) == ".star" && !strings.HasSuffix(d.Name(), "_test.star") {
			// Tier-3 test files are parse-only-in-test-mode (they reference
			// `tester.*`, `ok`, `err`, `nonretryable`, `assert.*`). The
			// production worker MUST skip them — mirrors Go's `*_test.go`
			// convention where the build excludes test files from
			// production binaries. The skip applies to BOTH flows AND
			// triggers (Phase 7): a *_test.star file declaring trigger(...)
			// does NOT register because the file never reaches the parser.
			starFiles = append(starFiles, path)
		}
		return nil
	}); err != nil {
		return nil, nil, fmt.Errorf("bootRegistry: walk: %w", err)
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
			return nil, nil, fmt.Errorf("bootRegistry: abs %s: %w", path, err)
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil, nil, fmt.Errorf("bootRegistry: read %s: %w", absPath, err)
		}
		sum := sha256.Sum256(data)
		fileHashes[absPath] = hex.EncodeToString(sum[:])
	}

	// Parse each file. The parser accumulates flows by name across files.
	// Errors from any file abort the boot.
	for _, path := range starFiles {
		if _, err := p.ParseFile(path); err != nil {
			return nil, nil, fmt.Errorf("bootRegistry: parse %s: %w", path, err)
		}
	}

	// Build the flow registry — unchanged from pre-Phase-7 behavior.
	// Each flow gets registered under (flow_name, content_hash_of_owning_file).
	// The owning file is determined by the flow's Pos.Filename(); the parser
	// stores absolute paths so fileHashes lookup matches directly.
	flowReg := interpreter.NewRegistry()

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
			return nil, nil, fmt.Errorf("bootRegistry: flow %s defined in %s but file not in walk", name, owningPath)
		}
		parsed := &interpreter.ParsedFlow{
			Flow:    f,
			Lambdas: lambdas,
		}
		if err := flowReg.Register(name, hash, parsed); err != nil {
			return nil, nil, fmt.Errorf("bootRegistry: register flow %s@%s: %w", name, hash, err)
		}
	}

	flowReg.Freeze()

	// Build the trigger registry — Phase 7 addition (D-07-11).
	trigReg := interpreter.NewTriggerRegistry()
	for _, trig := range p.Triggers() {
		owningPath := trig.Pos.Filename()
		hash, ok := fileHashes[owningPath]
		if !ok {
			// Defensive: if the trigger's owning file isn't in the walk
			// (load() into a path outside rootDir?), surface a clear
			// error. The parser already cached file bytes by absolute
			// path, but bootRegistry only computed hashes for files
			// INSIDE rootDir.
			return nil, nil, fmt.Errorf("bootRegistry: trigger for flow %s defined in %s but file not in walk (likely loaded from outside rootdir)", trig.FlowName, owningPath)
		}
		if err := trigReg.Register(hash, trig); err != nil {
			return nil, nil, fmt.Errorf("bootRegistry: register trigger (flow=%s, source=%s): %w",
				trig.FlowName, trig.Source.Kind(), err)
		}
	}
	trigReg.Freeze()

	// Surface deferred parser warnings (D-07-13 byte-identical duplicate
	// triggers, etc.) at boot time. These are informational, never errors.
	for _, w := range p.TriggerWarnings() {
		slog.Default().Warn("parser warning", "detail", w)
	}

	return flowReg, trigReg, nil
}
