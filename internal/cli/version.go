package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Print Contaigen build information",
		Example: `  contaigen version`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "contaigen %s\n", opts.Build.Version)
			fmt.Fprintf(out, "commit: %s\n", opts.Build.Commit)
			fmt.Fprintf(out, "built: %s\n", opts.Build.Date)
			return nil
		},
	}
}
