package cli

import (
	"fmt"
	"strings"

	"github.com/NorthernReach/contaigen/internal/composex"
	"github.com/NorthernReach/contaigen/internal/model"
	"github.com/spf13/cobra"
)

func newComposeCommand(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "compose",
		Short: "Validate and run Compose apps for workbench testing",
		Long: `Validate and run Docker Compose applications for Contaigen testing.

Compose apps are launched with Docker Compose and an override that attaches
their services to the selected environment's segment network. This makes app
containers reachable from the workbench by Compose service name.`,
		Example: `  contaigen compose validate ./compose.yaml
  contaigen compose validate ./compose.yaml --docker
  contaigen compose up lab ./compose.yaml
  contaigen compose down lab ./compose.yaml`,
	}

	cmd.AddCommand(newComposeValidateCommand(opts))
	cmd.AddCommand(newComposeUpCommand(opts))
	cmd.AddCommand(newComposeDownCommand(opts))
	return cmd
}

func newComposeValidateCommand(opts Options) *cobra.Command {
	var dockerValidate bool

	cmd := &cobra.Command{
		Use:   "validate <path>",
		Short: "Validate a Compose file",
		Long: `Validate a Docker Compose file.

The default validation checks that the file parses and contains usable service
definitions. Use --docker to also invoke docker compose config --quiet for
Compose's full validation.`,
		Example: `  contaigen compose validate ./compose.yaml
  contaigen compose validate ./compose.yaml --docker`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			compose, err := opts.NewComposeManager()
			if err != nil {
				return err
			}
			project, err := compose.ValidateFile(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if dockerValidate {
				if err := compose.ValidateWithDocker(cmd.Context(), project.Path, model.ExecIO{
					Stdout: cmd.OutOrStdout(),
					Stderr: cmd.ErrOrStderr(),
				}); err != nil {
					return err
				}
			}
			printSuccess(cmd, "Valid Compose file %s (%d services)", project.Path, len(project.Services))
			writeComposeServices(cmd, project.Services)
			return nil
		},
	}

	cmd.Flags().BoolVar(&dockerValidate, "docker", false, "Also run docker compose config --quiet")
	return cmd
}

func newComposeUpCommand(opts Options) *cobra.Command {
	var projectName string
	var foreground bool

	cmd := &cobra.Command{
		Use:   "up <env> <path>",
		Short: "Start a Compose app on an environment network",
		Long: `Start a Docker Compose app on an environment network.

Contaigen generates a temporary Compose override that marks services with
Contaigen labels and connects the Compose default network to the selected
environment's segment network.`,
		Example: `  contaigen compose up lab ./compose.yaml
  contaigen compose up lab ./compose.yaml --project client-a-app
  contaigen compose up lab ./compose.yaml --foreground`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			env, err := eng.InspectEnvironment(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			networkName, err := composeNetworkForEnvironment(env)
			if err != nil {
				return err
			}

			compose, err := opts.NewComposeManager()
			if err != nil {
				return err
			}
			req := composex.UpRequest{
				File:        args[1],
				ProjectName: projectName,
				EnvName:     env.Name,
				NetworkName: networkName,
				Detach:      !foreground,
			}
			if err := compose.Up(cmd.Context(), req, model.ExecIO{
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Started Compose app for environment %s on network %s", env.Name, networkName)
			return nil
		},
	}

	cmd.Flags().StringVar(&projectName, "project", "", "Compose project name; defaults to contaigen-<env>")
	cmd.Flags().BoolVar(&foreground, "foreground", false, "Run compose up in the foreground instead of detached mode")
	return cmd
}

func newComposeDownCommand(opts Options) *cobra.Command {
	var projectName string
	var removeVolumes bool

	cmd := &cobra.Command{
		Use:   "down <env> <path>",
		Short: "Stop and remove a Compose app",
		Example: `  contaigen compose down lab ./compose.yaml
  contaigen compose down lab ./compose.yaml --volumes`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			env, err := eng.InspectEnvironment(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			networkName, err := composeNetworkForEnvironment(env)
			if err != nil {
				return err
			}

			compose, err := opts.NewComposeManager()
			if err != nil {
				return err
			}
			req := composex.DownRequest{
				File:          args[1],
				ProjectName:   projectName,
				EnvName:       env.Name,
				NetworkName:   networkName,
				RemoveVolumes: removeVolumes,
			}
			if err := compose.Down(cmd.Context(), req, model.ExecIO{
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Stopped Compose app for environment %s", env.Name)
			return nil
		},
	}

	cmd.Flags().StringVar(&projectName, "project", "", "Compose project name; defaults to contaigen-<env>")
	cmd.Flags().BoolVar(&removeVolumes, "volumes", false, "Remove named volumes declared by the Compose project")
	return cmd
}

func writeComposeServices(cmd *cobra.Command, services []composex.ServiceSummary) {
	if len(services) == 0 {
		return
	}
	printHeader(cmd, "%-20s %-28s %s", "SERVICE", "IMAGE", "BUILD")
	for _, service := range services {
		fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-28s %t\n", service.Name, valueOrDash(service.Image), service.HasBuild)
	}
}

func composeNetworkForEnvironment(env model.Environment) (string, error) {
	networkName := strings.TrimSpace(env.NetworkName)
	if networkName == "" {
		networkName = strings.TrimSpace(env.NetworkMode)
	}
	switch networkName {
	case "", model.NetworkProfileBridge, model.NetworkProfileHost, "none":
		return "", fmt.Errorf("environment %q uses network mode %q; create the environment with --network segment before running Compose apps", env.Name, valueOrDash(env.NetworkMode))
	default:
		return networkName, nil
	}
}
