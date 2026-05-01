package cli

import (
	"context"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func newDoctorCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check Contaigen host and Docker readiness",
		Long: `Check local readiness for Contaigen.

The doctor command verifies platform information, resolved config/data paths,
and Docker daemon connectivity without requiring any Contaigen resources to
already exist.`,
		Example: `  contaigen doctor
  contaigen doctor --color always`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor(cmd.Context(), cmd, opts)
		},
	}
}

func runDoctor(ctx context.Context, cmd *cobra.Command, opts Options) error {
	out := cmd.OutOrStdout()
	p := colorFor(cmd)

	fmt.Fprintln(out, p.bold("Contaigen doctor"))
	fmt.Fprintf(out, "OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)

	paths, err := opts.Paths()
	if err != nil {
		return fmt.Errorf("resolve config paths: %w", err)
	}

	fmt.Fprintln(out, "Paths:")
	fmt.Fprintf(out, "  config: %s\n", paths.ConfigFile)
	fmt.Fprintf(out, "  data: %s\n", paths.DataDir)
	fmt.Fprintf(out, "  workspaces: %s\n", paths.WorkspaceDir)
	fmt.Fprintf(out, "  templates: %s\n", paths.TemplateDir)
	fmt.Fprintf(out, "  logs: %s\n", paths.LogDir)

	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		printWarning(cmd, "Docker host networking and device mounts can behave differently on this platform.")
	}

	probe, err := opts.NewDockerClient()
	if err != nil {
		fmt.Fprintf(out, "Docker: %s (%v)\n", p.red("unavailable"), err)
		return nil
	}
	defer probe.Close()

	ping, err := probe.Ping(ctx)
	if err != nil {
		fmt.Fprintf(out, "Docker: %s (%v)\n", p.red("unavailable"), err)
		return nil
	}

	version, err := probe.ServerVersion(ctx)
	if err != nil {
		fmt.Fprintf(out, "Docker: %s, but version lookup failed (%v)\n", p.yellow("available"), err)
		return nil
	}

	fmt.Fprintf(out, "Docker: %s\n", p.green("available"))
	fmt.Fprintf(out, "  server version: %s\n", version.Version)
	fmt.Fprintf(out, "  API version: %s\n", version.APIVersion)
	fmt.Fprintf(out, "  minimum API version: %s\n", version.MinAPIVersion)
	fmt.Fprintf(out, "  daemon OS/Arch: %s/%s\n", version.OperatingSystem, version.Architecture)
	fmt.Fprintf(out, "  ping API version: %s\n", ping.APIVersion)
	fmt.Fprintf(out, "  ping OS type: %s\n", ping.OSType)

	return nil
}
