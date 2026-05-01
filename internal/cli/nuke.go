package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/NorthernReach/contaigen/internal/model"
	"github.com/spf13/cobra"
)

func newNukeCommand(opts Options) *cobra.Command {
	var dryRun bool
	var yes bool
	var backupWorkspaces bool
	var noBackupWorkspaces bool
	var password string
	var passwordFile string
	var timeout int

	cmd := &cobra.Command{
		Use:   "nuke",
		Short: "Remove all Contaigen-managed resources",
		Long: `Remove all Contaigen-managed resources and return Contaigen to a clean slate.

This removes Contaigen services, environments, VPN gateways, managed networks,
and workspaces. It is intended for full resets, corrupted test setups, or cases
where a workbench may have been compromised and should be destroyed.`,
		Example: `  contaigen nuke
  contaigen nuke --dry-run
  contaigen nuke --backup-workspaces --password-file ./backup.pass
  contaigen nuke --yes --no-backup-workspaces`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if backupWorkspaces && noBackupWorkspaces {
				return fmt.Errorf("use either --backup-workspaces or --no-backup-workspaces, not both")
			}
			if yes && !backupWorkspaces && !noBackupWorkspaces && !dryRun {
				// Non-interactive nuke must still make workspace loss explicit.
				// This keeps automation convenient without making "--yes" a footgun.
				return fmt.Errorf("--yes requires either --backup-workspaces or --no-backup-workspaces")
			}

			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			plan, err := eng.NukePlan(cmd.Context())
			if err != nil {
				return err
			}
			writeNukePlan(cmd, plan)
			if dryRun {
				printMuted(cmd, "Dry run only. No resources were removed.")
				return nil
			}
			if nukePlanEmpty(plan) {
				printSuccess(cmd, "Contaigen is already clean.")
				return nil
			}

			reader := bufio.NewReader(cmd.InOrStdin())
			if len(plan.Workspaces) == 0 {
				backupWorkspaces = false
			} else if !backupWorkspaces && !noBackupWorkspaces {
				backupWorkspaces, err = promptYesNo(cmd.OutOrStdout(), reader, "Back up workspaces before removing them?", true)
				if err != nil {
					return err
				}
			}

			backupPassword, err := resolvePassword(password, passwordFile)
			if err != nil {
				return err
			}
			if backupWorkspaces && backupPassword == "" {
				printWarning(cmd, "Workspace backups will not be encrypted. Use --password-file to encrypt them.")
			}

			if !yes {
				fmt.Fprintln(cmd.OutOrStdout())
				printWarning(cmd, "This will stop and remove every Contaigen-managed container, network, and workspace listed above.")
				confirmed, err := promptExact(cmd.OutOrStdout(), reader, "Type \"nuke\" to continue", "nuke")
				if err != nil {
					return err
				}
				if !confirmed {
					printMuted(cmd, "Nuke cancelled.")
					return nil
				}
			}

			var result model.NukeResult
			if err := runWithProgress(cmd, "Nuke Contaigen resources", func(ctx context.Context) error {
				var err error
				result, err = eng.Nuke(ctx, model.NukeRequest{
					BackupWorkspaces: backupWorkspaces,
					BackupPassword:   backupPassword,
					StopTimeout:      timeout,
				})
				return err
			}); err != nil {
				return err
			}
			writeNukeResult(cmd, result)
			if len(result.Errors) > 0 {
				return fmt.Errorf("nuke completed with %d error(s)", len(result.Errors))
			}
			printSuccess(cmd, "Contaigen nuke complete.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what nuke would remove without removing anything")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the final confirmation prompt; requires an explicit workspace backup choice")
	cmd.Flags().BoolVar(&backupWorkspaces, "backup-workspaces", false, "Back up workspaces before removing them")
	cmd.Flags().BoolVar(&noBackupWorkspaces, "no-backup-workspaces", false, "Remove workspaces without backing them up")
	cmd.Flags().StringVar(&password, "password", "", "Encrypt nuke workspace backups with a password; prefer --password-file")
	cmd.Flags().StringVar(&passwordFile, "password-file", "", "Read the nuke backup encryption password from a file")
	cmd.Flags().IntVar(&timeout, "timeout", 5, "Seconds to wait when stopping running containers before forced removal; -1 uses Docker's default")
	return cmd
}

func writeNukePlan(cmd *cobra.Command, plan model.NukePlan) {
	out := cmd.OutOrStdout()
	p := colorFor(cmd)
	fmt.Fprintln(out, p.bold("Contaigen nuke plan"))
	fmt.Fprintf(out, "Services: %d\n", len(plan.Services))
	fmt.Fprintf(out, "Environments: %d\n", len(plan.Environments))
	fmt.Fprintf(out, "VPN gateways: %d\n", len(plan.VPNGateways))
	fmt.Fprintf(out, "Networks: %d\n", len(plan.Networks))
	fmt.Fprintf(out, "Workspaces: %d\n", len(plan.Workspaces))
	writeNukeNames(out, "Service", serviceNukeNames(plan.Services))
	writeNukeNames(out, "Environment", envNukeNames(plan.Environments))
	writeNukeNames(out, "VPN", vpnNukeNames(plan.VPNGateways))
	writeNukeNames(out, "Network", networkNukeNames(plan.Networks))
	writeNukeNames(out, "Workspace", workspaceNukeNames(plan.Workspaces))
}

func writeNukeNames(out io.Writer, label string, names []string) {
	if len(names) == 0 {
		return
	}
	fmt.Fprintf(out, "%s targets:\n", label)
	for _, name := range names {
		fmt.Fprintf(out, "  %s\n", name)
	}
}

func writeNukeResult(cmd *cobra.Command, result model.NukeResult) {
	out := cmd.OutOrStdout()
	if len(result.WorkspaceBackups) > 0 {
		fmt.Fprintln(out, "Workspace backups:")
		for _, backup := range result.WorkspaceBackups {
			fmt.Fprintf(out, "  %s -> %s", backup.Workspace.Name, backup.Path)
			if backup.Encrypted {
				fmt.Fprint(out, " (encrypted)")
			}
			fmt.Fprintln(out)
		}
	}
	if len(result.RemovedWorkspaces) > 0 {
		fmt.Fprintln(out, "Removed workspaces:")
		for _, removed := range result.RemovedWorkspaces {
			fmt.Fprintf(out, "  %s -> %s\n", removed.Workspace.Name, removed.Workspace.Path)
		}
	}
	if len(result.Errors) > 0 {
		printWarning(cmd, "Nuke completed with errors:")
		for _, item := range result.Errors {
			fmt.Fprintf(out, "  %s %s %s: %s\n", item.Action, item.ResourceType, item.Name, item.Message)
		}
	}
}

func promptYesNo(out io.Writer, reader *bufio.Reader, question string, defaultYes bool) (bool, error) {
	suffix := " [y/N]: "
	if defaultYes {
		suffix = " [Y/n]: "
	}
	fmt.Fprint(out, question+suffix)
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "" {
		return defaultYes, nil
	}
	switch answer {
	case "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("expected yes or no, got %q", answer)
	}
}

func promptExact(out io.Writer, reader *bufio.Reader, question string, expected string) (bool, error) {
	fmt.Fprintf(out, "%s: ", question)
	answer, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return false, err
	}
	return strings.TrimSpace(answer) == expected, nil
}

func nukePlanEmpty(plan model.NukePlan) bool {
	return len(plan.Services) == 0 &&
		len(plan.Environments) == 0 &&
		len(plan.VPNGateways) == 0 &&
		len(plan.Networks) == 0 &&
		len(plan.Workspaces) == 0
}

func serviceNukeNames(services []model.Service) []string {
	names := make([]string, 0, len(services))
	for _, service := range services {
		names = append(names, service.EnvironmentName+"/"+service.Name)
	}
	return names
}

func envNukeNames(envs []model.Environment) []string {
	names := make([]string, 0, len(envs))
	for _, env := range envs {
		names = append(names, env.Name)
	}
	return names
}

func vpnNukeNames(vpns []model.VPNGateway) []string {
	names := make([]string, 0, len(vpns))
	for _, vpn := range vpns {
		names = append(names, vpn.Name)
	}
	return names
}

func networkNukeNames(networks []model.Network) []string {
	names := make([]string, 0, len(networks))
	for _, network := range networks {
		names = append(names, network.Name)
	}
	return names
}

func workspaceNukeNames(workspaces []model.Workspace) []string {
	names := make([]string, 0, len(workspaces))
	for _, ws := range workspaces {
		if ws.Path != "" {
			names = append(names, ws.Name+" -> "+ws.Path)
			continue
		}
		names = append(names, ws.Name)
	}
	return names
}
