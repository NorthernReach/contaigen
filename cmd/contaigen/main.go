package main

import (
	"fmt"
	"os"

	"github.com/NorthernReach/contaigen/internal/cli"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cmd := cli.NewRootCommand(cli.Options{
		Build: cli.BuildInfo{
			Version: version,
			Commit:  commit,
			Date:    date,
		},
	})

	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
