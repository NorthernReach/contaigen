package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/NorthernReach/contaigen/internal/model"
	"github.com/spf13/cobra"
)

func newNetCommand(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "net",
		Aliases: []string{"network", "networks"},
		Short:   "Manage and inspect Contaigen Docker networks",
		Long: `Manage Docker networks used by Contaigen environments.

The first supported managed profile is segment: a named Docker bridge network
for grouping one workbench with target services or project-specific containers.`,
		Example: `  contaigen net create client-a
  contaigen env create lab --network segment --network-name client-a
  contaigen net list
  contaigen net inspect client-a
  contaigen net map`,
	}

	cmd.AddCommand(newNetCreateCommand(opts))
	cmd.AddCommand(newNetListCommand(opts))
	cmd.AddCommand(newNetInspectCommand(opts))
	cmd.AddCommand(newNetMapCommand(opts))

	return cmd
}

func newNetCreateCommand(opts Options) *cobra.Command {
	req := model.EnsureNetworkRequest{
		Profile:    model.NetworkProfileSegment,
		Driver:     model.DefaultNetworkDriver,
		Attachable: true,
	}

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create or reuse a managed network",
		Example: `  contaigen net create client-a
  contaigen net create offline --internal`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Name = args[0]

			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			var network model.Network
			var warnings []string
			if err := runWithProgress(cmd, "Create network "+req.Name, func(ctx context.Context) error {
				var err error
				network, warnings, err = eng.EnsureNetwork(ctx, req)
				return err
			}); err != nil {
				return err
			}
			for _, warning := range warnings {
				printWarning(cmd, "%s", warning)
			}
			printSuccess(cmd, "Ready network %s (%s)", network.Name, shortID(network.ID))
			return nil
		},
	}

	cmd.Flags().StringVar(&req.Profile, "profile", req.Profile, "Network profile; segment is supported for managed creation")
	cmd.Flags().StringVar(&req.Driver, "driver", req.Driver, "Docker network driver")
	cmd.Flags().BoolVar(&req.Internal, "internal", false, "Create an internal network without external connectivity")
	cmd.Flags().BoolVar(&req.Attachable, "attachable", req.Attachable, "Allow manual container attachment where supported")
	return cmd
}

func newNetListCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List Contaigen-managed networks",
		Example: `  contaigen net list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			networks, err := eng.ListNetworks(cmd.Context())
			if err != nil {
				return err
			}
			if len(networks) == 0 {
				printMuted(cmd, "No Contaigen networks found.")
				return nil
			}

			printHeader(cmd, "%-24s %-10s %-10s %-9s %s", "NAME", "PROFILE", "DRIVER", "INTERNAL", "ID")
			for _, network := range networks {
				fmt.Fprintf(cmd.OutOrStdout(), "%-24s %-10s %-10s %-9t %s\n", network.Name, network.Profile, network.Driver, network.Internal, shortID(network.ID))
			}
			return nil
		},
	}
}

func newNetInspectCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "inspect <name>",
		Short:   "Show network details",
		Example: `  contaigen net inspect client-a`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			network, err := eng.InspectNetwork(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			writeNetworkDetails(cmd, network)
			return nil
		},
	}
}

func newNetMapCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "map",
		Short:   "Show Contaigen environments attached to Docker networks",
		Example: `  contaigen net map`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			networkMap, err := eng.NetworkMap(cmd.Context())
			if err != nil {
				return err
			}
			if len(networkMap.Networks) == 0 {
				printMuted(cmd, "No Contaigen network attachments found.")
				return nil
			}
			for i, network := range networkMap.Networks {
				if i > 0 {
					fmt.Fprintln(cmd.OutOrStdout())
				}
				writeNetworkMap(cmd, network)
			}
			return nil
		},
	}
}

func writeNetworkDetails(cmd *cobra.Command, network model.Network) {
	out := cmd.OutOrStdout()
	p := colorFor(cmd)
	fmt.Fprintf(out, "%s: %s\n", p.bold("Name"), network.Name)
	fmt.Fprintf(out, "ID: %s\n", network.ID)
	fmt.Fprintf(out, "Profile: %s\n", valueOrDash(network.Profile))
	fmt.Fprintf(out, "Driver: %s\n", network.Driver)
	fmt.Fprintf(out, "Scope: %s\n", network.Scope)
	fmt.Fprintf(out, "Internal: %t\n", network.Internal)
	fmt.Fprintf(out, "Attachable: %t\n", network.Attachable)
	if !network.CreatedAt.IsZero() {
		fmt.Fprintf(out, "Created: %s\n", network.CreatedAt.Format(time.RFC3339))
	}
	fmt.Fprintln(out, "Attachments:")
	if len(network.Attachments) == 0 {
		fmt.Fprintln(out, "  none")
		return
	}
	for _, attachment := range network.Attachments {
		fmt.Fprintf(out, "  %s %s %s\n", valueOrDash(attachment.EnvironmentName), attachment.ContainerName, valueOrDash(attachment.IPv4Address))
	}
}

func writeNetworkMap(cmd *cobra.Command, network model.Network) {
	out := cmd.OutOrStdout()
	label := network.Name
	if network.Profile != "" {
		label += " [" + network.Profile + "]"
	}
	fmt.Fprintln(out, colorFor(cmd).cyan(label))
	if len(network.Attachments) == 0 {
		fmt.Fprintln(out, "  none")
		return
	}
	for _, attachment := range network.Attachments {
		name := attachment.EnvironmentName
		if name == "" {
			name = attachment.ContainerName
		}
		fields := []string{name}
		if attachment.IPv4Address != "" {
			fields = append(fields, attachment.IPv4Address)
		}
		if attachment.ContainerID != "" {
			fields = append(fields, shortID(attachment.ContainerID))
		}
		fmt.Fprintf(out, "  %s\n", strings.Join(fields, "  "))
	}
}

func valueOrDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}
