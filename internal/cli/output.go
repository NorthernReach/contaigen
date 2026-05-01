package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func printSuccess(cmd *cobra.Command, format string, args ...any) {
	p := colorFor(cmd)
	fmt.Fprintf(cmd.OutOrStdout(), p.green("OK")+" "+format+"\n", args...)
}

func printWarning(cmd *cobra.Command, format string, args ...any) {
	p := colorFor(cmd)
	fmt.Fprintf(cmd.ErrOrStderr(), p.yellow("warning:")+" "+format+"\n", args...)
}

func printMuted(cmd *cobra.Command, message string) {
	fmt.Fprintln(cmd.OutOrStdout(), message)
}

func printHeader(cmd *cobra.Command, format string, args ...any) {
	p := colorFor(cmd)
	fmt.Fprintf(cmd.OutOrStdout(), p.bold(format)+"\n", args...)
}
