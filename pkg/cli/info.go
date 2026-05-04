package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"github.com/spf13/cobra"

	"github.com/mikelalcon/skytime/pkg/dag"
	"github.com/mikelalcon/skytime/pkg/parser"
)

// emDash is the U+2014 placeholder rendered when a flow has no
// description and/or no inputs. Chosen for visual distinctness from a
// hyphen (which can appear inside descriptions/type-hints) and grep-
// friendliness across the repo.
const emDash = "—"

// newInfoCommand returns the skytime info subcommand. Quick task
// 260504-k9c (initial), 260504-m65 (lipgloss upgrade).
//
// Surface:
//   - Parses a .star file (parse-time only — no Temporal connection).
//   - Prints a 3-column bordered Unicode-box-drawing table to stdout:
//     Flow / Description / Inputs (rounded corners; bold header on TTY,
//     auto-suppressed when stdout is not a TTY via termenv).
//   - Source-declaration order (NOT alphabetical).
//   - Empty description and empty inputs render as em-dash (U+2014).
//   - Inputs cell renders `key:type, key:type` with keys sorted
//     alphabetically (deterministic, grep-friendly).
//   - On parse failure: renders the typed *dag.ParseError to stderr via
//     renderError + appendUnknownExtensionHint (mirrors validate); exits
//     non-zero. NO partial table on stdout.
//
// Architecture: same parse path as validate (no credHandler needed —
// parser walks AST + extension registry only; no I/O at parse time).
func newInfoCommand(cfg *config) *cobra.Command {
	return &cobra.Command{
		Use:   "info <file.star>",
		Short: "List flows defined in a .star file",
		Long: "Parses a Starlark flow file (parse-time only — no Temporal connection) " +
			"and prints a table of every flow with columns: Flow, Description, Inputs. " +
			"Source-declaration order. Empty description and empty inputs render as em-dash.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := args[0]

			parserOpts := []parser.Option{}
			if len(cfg.exts) > 0 {
				parserOpts = append(parserOpts, parser.WithExtensions(cfg.exts...))
			}
			p, err := parser.NewParser(parserOpts...)
			if err != nil {
				renderError(cmd.ErrOrStderr(), err, cfg.debug)
				return errSilent
			}
			if _, err := p.ParseFile(file); err != nil {
				stderr := cmd.ErrOrStderr()
				renderError(stderr, err, cfg.debug)
				appendUnknownExtensionHint(stderr, []error{err})
				return errSilent
			}

			renderInfoTable(cmd.OutOrStdout(), p.FlowsInOrder())
			return nil
		},
	}
}

// renderInfoTable writes a 3-column bordered table (Flow / Description /
// Inputs) to out via charm.land/lipgloss/v2/table. Empty description
// and empty inputs render as em-dash; inputs map keys are alphabetized
// so rendering is deterministic.
//
// Style: rounded box-drawing border with vertical separators. Header
// row is bold (applied via StyleFunc on row == table.HeaderRow == -1);
// termenv auto-suppresses ANSI bold when stdout is not a TTY, so piped
// output stays clean. Column widths auto-size to the longest cell
// (lipgloss default).
func renderInfoTable(out io.Writer, flows []*dag.Flow) {
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		Headers("Flow", "Description", "Inputs").
		StyleFunc(func(row, _ int) lipgloss.Style {
			// Padding(0,1) gives one space of horizontal breathing room
			// inside each cell, matching the visual feel of the previous
			// tabwriter padding=2 (one space each side ≈ two between
			// adjacent columns once borders are drawn). Bold is applied
			// ONLY to the header row (row == table.HeaderRow == -1).
			// termenv auto-suppresses ANSI on non-TTY stdout, so piped
			// output stays clean.
			base := lipgloss.NewStyle().Padding(0, 1)
			if row == table.HeaderRow {
				return base.Bold(true)
			}
			return base
		})
	for _, f := range flows {
		desc := f.Description
		if desc == "" {
			desc = emDash
		}
		t.Row(f.Name, desc, formatInputs(f.Inputs))
	}
	// Render returns the full table as a multi-line string with NO
	// trailing newline; add one so the shell prompt lands on a fresh
	// line, matching the prior tabwriter behavior (Fprintln after the
	// last row provided this implicitly).
	fmt.Fprintln(out, t.Render())
}

// formatInputs renders an Inputs map as "k:type, k:type" with keys
// sorted alphabetically. Empty / nil → em-dash. Determinism is
// load-bearing: Go map range is randomized, so iterating inputs
// directly would produce non-stable output across runs. sort.Strings
// pins the order.
func formatInputs(inputs map[string]string) string {
	if len(inputs) == 0 {
		return emDash
	}
	keys := make([]string, 0, len(inputs))
	for k := range inputs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(inputs[k])
	}
	return b.String()
}
