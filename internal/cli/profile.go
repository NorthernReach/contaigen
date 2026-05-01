package cli

import (
	"fmt"
	"strings"

	"github.com/NorthernReach/contaigen/internal/model"
	"github.com/spf13/cobra"
)

func newProfileCommand(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "profile",
		Aliases: []string{"profiles"},
		Short:   "List and inspect environment profiles",
		Long: `List and inspect environment profiles.

Profiles are YAML defaults for environment creation. Built-in profiles ship with
Contaigen, and user profiles can live in the configured template directory.`,
		Example: `  contaigen profile list
  contaigen profile show parrot-default
  contaigen profile show parrot-desktop
  contaigen profile show ./profiles/webapp.yaml
  contaigen env create lab --profile parrot-default`,
	}

	cmd.AddCommand(newProfileListCommand(opts))
	cmd.AddCommand(newProfileShowCommand(opts))

	return cmd
}

func newProfileListCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List available environment profiles",
		Example: `  contaigen profile list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := opts.NewProfileStore()
			if err != nil {
				return err
			}
			items, err := profiles.List(cmd.Context())
			if err != nil {
				return err
			}
			if len(items) == 0 {
				printMuted(cmd, "No Contaigen profiles found.")
				return nil
			}
			printHeader(cmd, "%-20s %-10s %-28s %-10s %s", "NAME", "SOURCE", "IMAGE", "NETWORK", "DESCRIPTION")
			for _, item := range items {
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-10s %-28s %-10s %s\n", item.Name, item.Source, truncate(item.Image, 28), valueOrDash(item.Network), item.Description)
			}
			return nil
		},
	}
}

func newProfileShowCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "show <name-or-path>",
		Short: "Show environment profile details",
		Example: `  contaigen profile show parrot-default
  contaigen profile show ./profiles/parrot-web.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := opts.NewProfileStore()
			if err != nil {
				return err
			}
			profile, err := profiles.Load(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			writeProfile(cmd, profile)
			return nil
		},
	}
}

func writeProfile(cmd *cobra.Command, profile model.EnvironmentProfile) {
	out := cmd.OutOrStdout()
	p := colorFor(cmd)
	fmt.Fprintf(out, "%s: %s\n", p.bold("Name"), profile.Metadata.Name)
	fmt.Fprintf(out, "Source: %s\n", profile.Source)
	if profile.Path != "" {
		fmt.Fprintf(out, "Path: %s\n", profile.Path)
	}
	fmt.Fprintf(out, "Description: %s\n", valueOrDash(profile.Metadata.Description))
	fmt.Fprintf(out, "Image: %s\n", profile.Spec.Image)
	fmt.Fprintf(out, "Shell: %s\n", profile.Spec.Shell)
	fmt.Fprintf(out, "User: %s\n", valueOrDash(profile.Spec.User))
	fmt.Fprintf(out, "Network: %s\n", profileNetwork(profile))
	fmt.Fprintf(out, "Workspace: %s\n", profileWorkspace(profile))
	fmt.Fprintf(out, "Desktop: %s\n", profileDesktop(profile))
	fmt.Fprintf(out, "Env: %s\n", formatStringList(profile.Spec.Env))
	fmt.Fprintf(out, "Ports: %s\n", formatPorts(profile.Spec.Ports))
	fmt.Fprintf(out, "Volumes: %s\n", formatVolumes(profile.Spec.Volumes))
	fmt.Fprintf(out, "Capabilities: %s\n", formatStringList(profile.Spec.CapAdd))
}

func profileNetwork(profile model.EnvironmentProfile) string {
	parts := []string{}
	if profile.Spec.Network.Profile != "" {
		parts = append(parts, profile.Spec.Network.Profile)
	}
	if profile.Spec.Network.Name != "" {
		parts = append(parts, profile.Spec.Network.Name)
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " ")
}

func profileWorkspace(profile model.EnvironmentProfile) string {
	if profile.Spec.Workspace.Disabled {
		return "disabled"
	}
	parts := []string{}
	if profile.Spec.Workspace.Name != "" {
		parts = append(parts, profile.Spec.Workspace.Name)
	}
	if profile.Spec.Workspace.Path != "" {
		parts = append(parts, profile.Spec.Workspace.Path)
	}
	if profile.Spec.Workspace.MountPath != "" {
		parts = append(parts, "mounted at "+profile.Spec.Workspace.MountPath)
	}
	if len(parts) == 0 {
		return "default"
	}
	return strings.Join(parts, " ")
}

func profileDesktop(profile model.EnvironmentProfile) string {
	if !profile.Spec.Desktop.Enabled {
		return "disabled"
	}
	desktop := profile.Spec.Desktop
	protocol := desktop.Protocol
	if protocol == "" {
		protocol = model.DefaultDesktopProtocol
	}
	host := desktop.HostIP
	if host == "" {
		host = model.DefaultDesktopHostIP
	}
	port := desktop.HostPort
	if port == "" {
		port = model.DefaultDesktopPort
	}
	user := desktop.User
	if user == "" {
		user = model.DefaultDesktopUser
	}
	return fmt.Sprintf("%s at %s://%s:%s%s user %s", protocol, defaultString(desktop.Scheme, model.DefaultDesktopScheme), host, port, defaultString(desktop.Path, model.DefaultDesktopPath), user)
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func formatStringList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}
