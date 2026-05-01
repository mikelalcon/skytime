package cli

import (
	"github.com/spf13/cobra"
)

// newValidateCommand is the W3 Task 1 stub returning a placeholder.
// Task 3 of plan 04-04 replaces it with the full RunE that calls
// pkg/validator.Validate and pipes errors through renderError.
func newValidateCommand(cfg *config) *cobra.Command {
	_ = cfg
	return &cobra.Command{
		Use:   "validate <file.star>",
		Short: "Statically validate a .star flow file",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil // filled in Task 3
		},
	}
}
