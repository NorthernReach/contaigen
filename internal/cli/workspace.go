package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/NorthernReach/contaigen/internal/model"
	"github.com/spf13/cobra"
)

func newWorkspaceCommand(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"workspaces", "ws"},
		Short:   "Manage host-backed Contaigen workspaces",
		Long: `Manage persistent host directories mounted into environments.

Workspaces are the durable place for notes, artifacts, tool output, and files
that need to survive container restarts or replacement.`,
		Example: `  contaigen workspace create client-a
  contaigen workspace info client-a
  contaigen workspace backup client-a
  contaigen workspace restore ./client-a.tar.gz --name client-a-restored
  contaigen env create lab --workspace client-a`,
	}

	cmd.AddCommand(newWorkspaceCreateCommand(opts))
	cmd.AddCommand(newWorkspaceListCommand(opts))
	cmd.AddCommand(newWorkspaceInfoCommand(opts))
	cmd.AddCommand(newWorkspaceBackupCommand(opts))
	cmd.AddCommand(newWorkspaceRestoreCommand(opts))

	return cmd
}

func newWorkspaceCreateCommand(opts Options) *cobra.Command {
	var req model.CreateWorkspaceRequest

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a host-backed workspace",
		Example: `  contaigen workspace create client-a
  contaigen workspace create client-a --path ~/engagements/client-a`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Name = args[0]

			workspaces, err := opts.NewWorkspaceStore()
			if err != nil {
				return err
			}
			var ws model.Workspace
			if err := runStatus(cmd, "Create workspace "+req.Name, func(ctx context.Context) error {
				var err error
				ws, err = workspaces.Create(ctx, req)
				return err
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Created workspace %s", ws.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Path: %s\n", ws.Path)
			return nil
		},
	}

	cmd.Flags().StringVar(&req.Path, "path", "", "Host path for the workspace; defaults to Contaigen's workspace root")
	return cmd
}

func newWorkspaceListCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List workspaces",
		Example: `  contaigen workspace list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			workspaces, err := opts.NewWorkspaceStore()
			if err != nil {
				return err
			}
			items, err := workspaces.List(cmd.Context())
			if err != nil {
				return err
			}
			if len(items) == 0 {
				printMuted(cmd, "No Contaigen workspaces found.")
				return nil
			}
			printHeader(cmd, "%-20s %-24s %s", "NAME", "CREATED", "PATH")
			for _, ws := range items {
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-24s %s\n", ws.Name, formatTime(ws.CreatedAt), ws.Path)
			}
			return nil
		},
	}
}

func newWorkspaceInfoCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "info <name>",
		Short:   "Show workspace details",
		Example: `  contaigen workspace info client-a`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workspaces, err := opts.NewWorkspaceStore()
			if err != nil {
				return err
			}
			ws, err := workspaces.Inspect(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			p := colorFor(cmd)
			fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", p.bold("Name"), ws.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Path: %s\n", ws.Path)
			if !ws.CreatedAt.IsZero() {
				fmt.Fprintf(cmd.OutOrStdout(), "Created: %s\n", ws.CreatedAt.Format(time.RFC3339))
			}
			return nil
		},
	}
}

func newWorkspaceBackupCommand(opts Options) *cobra.Command {
	var req model.BackupWorkspaceRequest
	var passwordFile string

	cmd := &cobra.Command{
		Use:   "backup <name>",
		Short: "Create a backup of a workspace",
		Example: `  contaigen workspace backup client-a
  contaigen workspace backup client-a --output ./client-a.tar.gz
  contaigen workspace backup client-a --password-file ./backup.pass`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Name = args[0]
			password, err := resolvePassword(req.Password, passwordFile)
			if err != nil {
				return err
			}
			req.Password = password

			workspaces, err := opts.NewWorkspaceStore()
			if err != nil {
				return err
			}
			var backup model.WorkspaceBackup
			if err := runStatus(cmd, "Back up workspace "+req.Name, func(ctx context.Context) error {
				var err error
				backup, err = workspaces.Backup(ctx, req)
				return err
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Backed up workspace %s", backup.Workspace.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Path: %s\n", backup.Path)
			fmt.Fprintf(cmd.OutOrStdout(), "Size: %d bytes\n", backup.SizeBytes)
			fmt.Fprintf(cmd.OutOrStdout(), "Encrypted: %s\n", yesNo(backup.Encrypted))
			return nil
		},
	}

	cmd.Flags().StringVarP(&req.OutputPath, "output", "o", "", "Output backup path")
	cmd.Flags().StringVar(&req.Password, "password", "", "Encrypt the backup with a password; prefer --password-file for sensitive workflows")
	cmd.Flags().StringVar(&passwordFile, "password-file", "", "Read the backup encryption password from a file")
	return cmd
}

func newWorkspaceRestoreCommand(opts Options) *cobra.Command {
	var req model.RestoreWorkspaceRequest
	var passwordFile string

	cmd := &cobra.Command{
		Use:     "restore <backup>",
		Aliases: []string{"import"},
		Short:   "Restore a workspace from a backup archive",
		Long: `Restore a workspace from a Contaigen workspace backup.

The archive's top-level workspace folder is stripped during restore, so a
backup created from one workspace can be restored into a new workspace name.`,
		Example: `  contaigen workspace restore ./client-a.tar.gz --name client-a-restored
  contaigen workspace restore ./client-a.tar.gz.c3enc --name client-a-restored --password-file ./backup.pass
  contaigen workspace import ./client-a.tar.gz --name client-a --path ~/engagements/client-a`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.InputPath = args[0]
			password, err := resolvePassword(req.Password, passwordFile)
			if err != nil {
				return err
			}
			req.Password = password

			workspaces, err := opts.NewWorkspaceStore()
			if err != nil {
				return err
			}
			var restore model.WorkspaceRestore
			if err := runStatus(cmd, "Restore workspace "+req.Name, func(ctx context.Context) error {
				var err error
				restore, err = workspaces.Restore(ctx, req)
				return err
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Restored workspace %s", restore.Workspace.Name)
			fmt.Fprintf(cmd.OutOrStdout(), "Path: %s\n", restore.Workspace.Path)
			fmt.Fprintf(cmd.OutOrStdout(), "Source: %s\n", restore.Path)
			fmt.Fprintf(cmd.OutOrStdout(), "Files: %d\n", restore.Files)
			fmt.Fprintf(cmd.OutOrStdout(), "Size: %d bytes\n", restore.SizeBytes)
			return nil
		},
	}

	cmd.Flags().StringVar(&req.Name, "name", "", "Workspace name to restore into")
	_ = cmd.MarkFlagRequired("name")
	cmd.Flags().StringVar(&req.Path, "path", "", "Host path for the restored workspace; defaults to Contaigen's workspace root")
	cmd.Flags().StringVar(&req.Password, "password", "", "Password for an encrypted workspace backup; prefer --password-file for sensitive workflows")
	cmd.Flags().StringVar(&passwordFile, "password-file", "", "Read the workspace backup password from a file")
	return cmd
}

func resolvePassword(password string, passwordFile string) (string, error) {
	if password != "" && passwordFile != "" {
		return "", fmt.Errorf("use either --password or --password-file, not both")
	}
	if passwordFile == "" {
		return password, nil
	}
	data, err := os.ReadFile(expandHome(passwordFile))
	if err != nil {
		return "", fmt.Errorf("read password file: %w", err)
	}
	password = strings.TrimRight(string(data), "\r\n")
	if password == "" {
		return "", fmt.Errorf("password file is empty")
	}
	return password, nil
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.Format(time.RFC3339)
}
