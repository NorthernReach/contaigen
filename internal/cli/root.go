package cli

import (
	"io"
	"log/slog"

	"github.com/NorthernReach/contaigen/internal/composex"
	"github.com/NorthernReach/contaigen/internal/config"
	"github.com/NorthernReach/contaigen/internal/dockerx"
	"github.com/NorthernReach/contaigen/internal/logging"
	"github.com/NorthernReach/contaigen/internal/templates"
	"github.com/NorthernReach/contaigen/internal/workspace"
	"github.com/spf13/cobra"
)

type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

type Options struct {
	Build             BuildInfo
	Out               io.Writer
	Err               io.Writer
	Paths             func() (config.Paths, error)
	NewDockerClient   func() (dockerx.Runtime, error)
	NewWorkspaceStore func() (workspace.Manager, error)
	NewProfileStore   func() (templates.Manager, error)
	NewComposeManager func() (composex.Manager, error)
}

func NewRootCommand(opts Options) *cobra.Command {
	opts = opts.withDefaults()

	var logLevel string
	var logFormat string
	var colorMode string

	cmd := &cobra.Command{
		Use:   "contaigen",
		Short: "Orchestrate Docker-based security workbenches",
		Long: `Contaigen orchestrates Docker-based security workbenches.

It wraps Docker containers, networks, workspaces, and profile templates into a
CLI built for repeatable security research and application testing workflows.`,
		Example: `  contaigen doctor
  contaigen profile list
  contaigen env create lab --profile parrot-default --network segment
  contaigen service add lab nginx:alpine --name target
  contaigen vpn create corp --config ~/vpn/client.ovpn
  contaigen env enter lab
  contaigen net map
  contaigen workspace backup lab
  contaigen nuke --dry-run`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       opts.Build.Version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if err := validateColorMode(colorMode); err != nil {
				return err
			}
			logger, err := logging.New(logging.Config{
				Level:  logLevel,
				Format: logFormat,
				Output: cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			slog.SetDefault(logger)
			return nil
		},
	}

	cmd.SetOut(opts.Out)
	cmd.SetErr(opts.Err)

	cmd.PersistentFlags().StringVar(&logLevel, "log-level", "warn", "Log level: debug, info, warn, or error")
	cmd.PersistentFlags().StringVar(&logFormat, "log-format", "text", "Log format: text or json")
	cmd.PersistentFlags().StringVar(&colorMode, "color", colorAuto, "Color output: auto, always, or never")

	cmd.AddCommand(newVersionCommand(opts))
	cmd.AddCommand(newComposeCommand(opts))
	cmd.AddCommand(newDoctorCommand(opts))
	cmd.AddCommand(newEnvCommand(opts))
	cmd.AddCommand(newNetCommand(opts))
	cmd.AddCommand(newNukeCommand(opts))
	cmd.AddCommand(newProfileCommand(opts))
	cmd.AddCommand(newServiceCommand(opts))
	cmd.AddCommand(newTemplateCommand(opts))
	cmd.AddCommand(newVPNCommand(opts))
	cmd.AddCommand(newWorkspaceCommand(opts))
	applyHelpTemplates(cmd)

	return cmd
}

func usageTemplate() string {
	return `Usage:
  {{.UseLine}}{{if .HasAvailableSubCommands}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Commands:
{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}  {{rpad .Name .NamePadding }} {{.Short}}
{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}

{{if .HasAvailableSubCommands}}Use "{{.CommandPath}} [command] --help" for more information about a command.
{{end}}`
}

func helpTemplate() string {
	return `{{if .Long}}{{.Long}}{{else}}{{.Short}}{{end}}

{{if .HasAvailableSubCommands}}Usage:
  {{.UseLine}} [command]{{else if .Runnable}}Usage:
  {{.UseLine}}{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}

Commands:
{{range .Commands}}{{if (or .IsAvailableCommand (eq .Name "help"))}}  {{rpad .Name .NamePadding }} {{.Short}}
{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}

{{if .HasAvailableSubCommands}}Use "{{.CommandPath}} [command] --help" for more information about a command.
{{end}}`
}

func applyHelpTemplates(cmd *cobra.Command) {
	cmd.SetUsageTemplate(usageTemplate())
	cmd.SetHelpTemplate(helpTemplate())
	for _, child := range cmd.Commands() {
		applyHelpTemplates(child)
	}
}

func (opts Options) withDefaults() Options {
	if opts.Build.Version == "" {
		opts.Build.Version = "dev"
	}
	if opts.Build.Commit == "" {
		opts.Build.Commit = "none"
	}
	if opts.Build.Date == "" {
		opts.Build.Date = "unknown"
	}
	if opts.Paths == nil {
		opts.Paths = config.DefaultPaths
	}
	if opts.NewDockerClient == nil {
		opts.NewDockerClient = dockerx.NewClient
	}
	if opts.NewWorkspaceStore == nil {
		opts.NewWorkspaceStore = func() (workspace.Manager, error) {
			paths, err := opts.Paths()
			if err != nil {
				return nil, err
			}
			return workspace.New(paths), nil
		}
	}
	if opts.NewProfileStore == nil {
		opts.NewProfileStore = func() (templates.Manager, error) {
			paths, err := opts.Paths()
			if err != nil {
				return nil, err
			}
			return templates.New(paths), nil
		}
	}
	if opts.NewComposeManager == nil {
		opts.NewComposeManager = func() (composex.Manager, error) {
			return composex.New(), nil
		}
	}
	return opts
}
