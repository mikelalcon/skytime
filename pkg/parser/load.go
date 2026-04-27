package parser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"

	"github.com/mikelalcon/skytime/pkg/dag"
)

// callerPositionOrZero is the depth-safe variant of callerPosition. Inside
// Thread.Load the call stack is one frame deep, so depth >= 1 is enough to
// read the bottom frame.
func callerPositionOrZero(thread *starlark.Thread) syntax.Position {
	if thread.CallStackDepth() < 1 {
		return syntax.Position{}
	}
	return thread.CallFrame(thread.CallStackDepth() - 1).Pos
}

// filenameFromThreadName extracts the filename suffix from our thread
// naming convention ("parse:<file>" / "load:<file>").
func filenameFromThreadName(name string) string {
	for _, prefix := range []string{"parse:", "load:"} {
		if strings.HasPrefix(name, prefix) {
			return strings.TrimPrefix(name, prefix)
		}
	}
	return ""
}

// makeLoad returns the Thread.Load callback used by every parse and load
// thread. The callback resolves module paths per D-13 (relative + absolute),
// enforces sandbox containment per D-17 (path must stay within the parser
// root), allocates a FRESH *starlark.Thread per loaded module (Pitfall #1),
// caches results so repeated loads of the same file do not re-read disk.
//
// Errors from inside a load() are surfaced as *dag.ParseError so callers
// see typed errors regardless of whether failure happened in the load
// resolver, file I/O, or transitive parse of the loaded module.
func (p *Parser) makeLoad() func(*starlark.Thread, string) (starlark.StringDict, error) {
	return func(thread *starlark.Thread, module string) (starlark.StringDict, error) {
		// Walk down the call stack for the bottom frame's filename. At
		// the time Starlark invokes Thread.Load, the thread has the
		// currently-parsing file as the bottom frame. Use callerPosition
		// which is depth-safe; fall back to the bottom frame if the
		// stack is too shallow.
		callerPos := callerPositionOrZero(thread)
		callerFile := callerPos.Filename()
		if callerFile == "" {
			// Last-ditch: use the thread name (we set it to "parse:<filename>"
			// or "load:<filename>" in our two thread allocators).
			callerFile = filenameFromThreadName(thread.Name)
		}

		resolved, err := p.resolveLoadPath(callerFile, module)
		if err != nil {
			// resolveLoadPath returns *dag.ParseError; attach the call-site
			// position if it wasn't already set.
			if pe, ok := err.(*dag.ParseError); ok && !pe.Pos.IsValid() {
				pe.Pos = callerPos
			}
			return nil, err
		}

		// Cache hit — return the previously-loaded globals (or cached error).
		if cached, ok := p.loadCache[resolved]; ok {
			return cached.globals, cached.err
		}

		src, ioErr := os.ReadFile(resolved)
		if ioErr != nil {
			err := &dag.ParseError{
				Pos: callerPos,
				Msg: fmt.Sprintf("load %q: %v", module, ioErr),
			}
			p.loadCache[resolved] = loadCacheEntry{err: err}
			return nil, err
		}
		p.fileBytes[resolved] = src

		// FRESH thread per loaded module (Pitfall #1).
		loadThread := &starlark.Thread{
			Name:  "load:" + resolved,
			Load:  p.makeLoad(),
			Print: thread.Print,
		}
		loadThread.SetMaxExecutionSteps(p.maxExecSteps)

		opts := defaultFileOptions()
		globals, execErr := starlark.ExecFileOptions(opts, loadThread, resolved, src, p.parseTimeGlobals)
		if execErr != nil {
			execErr = wrapStarlarkError(execErr)
		}

		p.loadCache[resolved] = loadCacheEntry{globals: globals, err: execErr}
		return globals, execErr
	}
}

// resolveLoadPath converts a Starlark `load()` module string into an absolute
// filesystem path inside the parser root.
//
// D-13 path syntax:
//   - Absolute (starts with "/"): resolves against the configured root.
//   - Relative (starts with "./" or "../"): sibling of the loading file.
//   - Bare name: treated as relative.
//
// D-14 root discovery:
//   - WithRoot(...) explicit override (set on Parser at construction).
//   - Otherwise walk up from the loading file looking for the first .git
//     ancestor; that directory becomes the implicit root.
//   - If both are absent, absolute load paths are an error.
//
// D-17 sandbox: the resolved path MUST stay within the root. We compute
// filepath.Rel(root, abs) and reject if it starts with ".." (traversal
// attempt). Phase 1 uses filepath.Rel; Phase 4 may upgrade to os.Root for
// symlink-traversal hardening.
func (p *Parser) resolveLoadPath(callerFile, module string) (string, error) {
	root := p.root
	if root == "" {
		root = findGitRoot(filepath.Dir(callerFile))
	}

	var candidate string
	switch {
	case strings.HasPrefix(module, "/"):
		// D-13 absolute path — must have a configured (or auto-discovered) root.
		if root == "" {
			return "", &dag.ParseError{
				Msg: fmt.Sprintf(
					"absolute load path %q used but no root configured (set WithRoot(...) or place files under a .git ancestor)",
					module),
			}
		}
		// Strip leading "/" because filepath.Join treats absolute paths
		// specially — we want the module to be relative to root.
		candidate = filepath.Join(root, strings.TrimPrefix(module, "/"))
	case strings.HasPrefix(module, "./") || strings.HasPrefix(module, "../"):
		// D-13 relative path — sibling of the caller file.
		candidate = filepath.Join(filepath.Dir(callerFile), module)
	default:
		// Bare name: starlark-go convention is to treat it as a sibling.
		candidate = filepath.Join(filepath.Dir(callerFile), module)
	}

	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", &dag.ParseError{
			Msg: fmt.Sprintf("resolve load %q: %v", module, err),
		}
	}

	// D-17 sandbox: reject any path that climbs out of the root. Only
	// applied when a root is configured — bare-name relative loads inside
	// a tmpdir without a root still work for tests / one-off parses.
	if root != "" {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			return "", &dag.ParseError{
				Msg: fmt.Sprintf("resolve root %q: %v", root, err),
			}
		}
		rel, err := filepath.Rel(rootAbs, abs)
		if err != nil || strings.HasPrefix(rel, "..") {
			return "", &dag.ParseError{
				Msg: fmt.Sprintf("load %q: path escapes parser root %q", module, rootAbs),
			}
		}
	}

	return abs, nil
}

// findGitRoot walks up from `start` looking for the first directory
// containing a `.git` entry. Returns "" if none found before hitting the
// filesystem root.
//
// This implements D-14's auto-discovery rule: when WithRoot is unset, the
// nearest .git ancestor of the file being parsed becomes the implicit root.
// Greenfield repos exercise this immediately — every fixture parse from
// inside the skytime repo hits the project's .git in two or three steps.
func findGitRoot(start string) string {
	cur := start
	for {
		if info, err := os.Stat(filepath.Join(cur, ".git")); err == nil && info != nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}
