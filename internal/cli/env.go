package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/NorthernReach/contaigen/internal/engine"
	"github.com/NorthernReach/contaigen/internal/model"
	"github.com/NorthernReach/contaigen/internal/templates"
	"github.com/spf13/cobra"
)

func newEnvCommand(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "env",
		Aliases: []string{"environment", "environments"},
		Short:   "Manage Contaigen workbench environments",
		Long: `Manage named workbench containers.

Environments are reusable Docker containers with Contaigen labels, optional
workspace mounts, and repeatable network profiles. They are the container
equivalent of a named Kali or Parrot VM for a project or engagement.`,
		Example: `  contaigen env create lab --profile parrot-default
  contaigen env create appsec --image alpine:latest --shell /bin/sh --network segment
  contaigen env list
  contaigen env info lab
  contaigen env enter lab
  contaigen env stop lab
  contaigen env rm lab --force`,
	}

	cmd.AddCommand(newEnvCreateCommand(opts))
	cmd.AddCommand(newEnvListCommand(opts))
	cmd.AddCommand(newEnvInfoCommand(opts))
	cmd.AddCommand(newEnvStartCommand(opts))
	cmd.AddCommand(newEnvStopCommand(opts))
	cmd.AddCommand(newEnvRemoveCommand(opts))
	cmd.AddCommand(newEnvEnterCommand(opts))

	return cmd
}

func newEnvCreateCommand(opts Options) *cobra.Command {
	req := model.CreateEnvironmentRequest{
		Image:          model.DefaultEnvironmentImage,
		Shell:          model.DefaultEnvironmentShell,
		NetworkProfile: model.DefaultNetworkProfile,
		Pull:           true,
		Start:          true,
	}
	var envValues []string
	var envFileValues []string
	var portValues []string
	var volumeValues []string
	var capAddValues []string
	var profileRef string
	var noStart bool

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new workbench environment",
		Long: `Create a reusable Docker-backed workbench environment.

By default this creates or reuses a same-name host workspace and mounts it at
/workspace. Use --profile for built-in or local YAML profile defaults, then use
flags to override profile fields for this specific environment.`,
		Example: `  contaigen env create lab --profile parrot-default
  contaigen env create lab --profile parrot-default --env-file ./lab.env
  contaigen env create parrot --profile parrot-default --network segment
  contaigen env create parrot-gui --profile parrot-desktop
  contaigen env create vpn-lab --profile parrot-default --network vpn --vpn corp
  contaigen env create scratch --image alpine:latest --shell /bin/sh --no-start
  contaigen env create app --profile ./profiles/web.yaml --port 127.0.0.1:8080:80`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flagReq := req
			flagReq.Name = args[0]
			var err error
			flagReq.Env, err = parseEnvFiles(envFileValues)
			if err != nil {
				return err
			}
			flagReq.Env = append(flagReq.Env, envValues...)
			flagReq.Ports, err = parsePortMappings(portValues)
			if err != nil {
				return err
			}
			flagReq.Volumes, err = parseVolumeMounts(volumeValues)
			if err != nil {
				return err
			}
			flagReq.CapAdd = capAddValues

			finalReq := defaultCreateEnvironmentRequest(args[0])
			if profileRef != "" {
				profiles, err := opts.NewProfileStore()
				if err != nil {
					return err
				}
				profile, err := profiles.Load(cmd.Context(), profileRef)
				if err != nil {
					return err
				}
				finalReq = templates.ApplyProfile(profile, finalReq)
			}
			finalReq = applyEnvCreateFlagOverrides(cmd, finalReq, flagReq, noStart)

			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			var env model.Environment
			var warnings []string
			if err := runWithProgress(cmd, "Create environment "+finalReq.Name, func(ctx context.Context) error {
				var err error
				env, warnings, err = eng.CreateEnvironment(ctx, finalReq)
				return err
			}); err != nil {
				return err
			}

			for _, warning := range warnings {
				printWarning(cmd, "%s", warning)
			}
			printSuccess(cmd, "Created environment %s (%s)", env.Name, shortID(env.ID))
			if env.WorkspacePath != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s -> %s\n", env.WorkspacePath, env.WorkspaceMountPath)
			}
			writeDesktopConnection(cmd, env, "", false)
			if finalReq.Start {
				printSuccess(cmd, "Started environment %s", env.Name)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&req.Image, "image", req.Image, "Container image to use")
	cmd.Flags().StringVar(&profileRef, "profile", "", "Environment profile name or YAML path")
	cmd.Flags().StringVar(&req.Shell, "shell", req.Shell, "Shell to run when entering the environment")
	cmd.Flags().StringVar(&req.User, "user", "", "Container user for create-time execution, equivalent to docker run --user")
	cmd.Flags().StringVar(&req.Hostname, "hostname", "", "Container hostname; defaults to the environment name")
	cmd.Flags().StringVar(&req.NetworkProfile, "network", req.NetworkProfile, "Network profile: bridge, isolated, host, segment, or vpn")
	cmd.Flags().StringVar(&req.NetworkName, "network-name", "", "Docker network name for segment profile; defaults to contaigen-<env>")
	cmd.Flags().StringVar(&req.VPNName, "vpn", "", "VPN gateway name for vpn network profile")
	cmd.Flags().StringVar(&req.WorkingDir, "workdir", "", "Default working directory inside the container")
	cmd.Flags().StringVar(&req.WorkspaceName, "workspace", "", "Workspace name; defaults to the environment name")
	cmd.Flags().StringVar(&req.WorkspacePath, "workspace-path", "", "Host path for the workspace; defaults to Contaigen's workspace root")
	cmd.Flags().StringVar(&req.WorkspaceMountPath, "workspace-mount", model.DefaultWorkspaceMountPath, "Container path where the workspace is mounted")
	cmd.Flags().BoolVar(&req.DisableWorkspace, "no-workspace", false, "Create the environment without a default workspace mount")
	cmd.Flags().StringArrayVarP(&envValues, "env", "e", nil, "Environment variable in KEY=VALUE form")
	cmd.Flags().StringArrayVar(&envFileValues, "env-file", nil, "Read environment variables from a .env file; may be used more than once")
	cmd.Flags().StringArrayVarP(&portValues, "port", "p", nil, "Publish a port as [host-ip:]host-port:container-port[/protocol]")
	cmd.Flags().StringArrayVarP(&volumeValues, "volume", "v", nil, "Bind mount a volume as source:target[:ro|rw]")
	cmd.Flags().StringArrayVar(&capAddValues, "cap-add", nil, "Linux capability to add to the environment, e.g. NET_ADMIN")
	cmd.Flags().BoolVar(&req.Desktop.Enabled, "desktop", false, "Enable browser-accessible noVNC desktop mode")
	cmd.Flags().StringVar(&req.Desktop.HostIP, "desktop-host", "", "Host IP for the desktop endpoint")
	cmd.Flags().StringVar(&req.Desktop.HostPort, "desktop-port", "", "Host port for the desktop endpoint")
	cmd.Flags().StringVar(&req.Desktop.Password, "desktop-password", "", "noVNC password; generated when omitted")
	cmd.Flags().StringVar(&req.Desktop.User, "desktop-user", "", "noVNC username shown in connection details")
	cmd.Flags().BoolVar(&req.Pull, "pull", req.Pull, "Pull the image if it is not available locally")
	cmd.Flags().BoolVar(&noStart, "no-start", false, "Create the container without starting it")

	return cmd
}

func defaultCreateEnvironmentRequest(name string) model.CreateEnvironmentRequest {
	return model.CreateEnvironmentRequest{
		Name:           name,
		Image:          model.DefaultEnvironmentImage,
		Shell:          model.DefaultEnvironmentShell,
		NetworkProfile: model.DefaultNetworkProfile,
		Pull:           true,
		Start:          true,
	}
}

func applyEnvCreateFlagOverrides(cmd *cobra.Command, base model.CreateEnvironmentRequest, flags model.CreateEnvironmentRequest, noStart bool) model.CreateEnvironmentRequest {
	out := base
	out.Name = flags.Name

	// Profiles provide defaults, while explicitly supplied flags win for the
	// one environment being created.
	if cmd.Flags().Changed("image") {
		out.Image = flags.Image
	}
	if cmd.Flags().Changed("shell") {
		out.Shell = flags.Shell
	}
	if cmd.Flags().Changed("user") {
		out.User = flags.User
	}
	if cmd.Flags().Changed("hostname") {
		out.Hostname = flags.Hostname
	}
	if cmd.Flags().Changed("network") {
		out.NetworkProfile = flags.NetworkProfile
	}
	if cmd.Flags().Changed("network-name") {
		out.NetworkName = flags.NetworkName
	}
	if cmd.Flags().Changed("vpn") {
		out.VPNName = flags.VPNName
		if !cmd.Flags().Changed("network") {
			out.NetworkProfile = model.NetworkProfileVPN
			out.NetworkName = ""
		}
	}
	if cmd.Flags().Changed("desktop") {
		out.Desktop.Enabled = flags.Desktop.Enabled
	}
	if cmd.Flags().Changed("desktop-host") {
		out.Desktop.HostIP = flags.Desktop.HostIP
		if !cmd.Flags().Changed("desktop") {
			out.Desktop.Enabled = true
		}
	}
	if cmd.Flags().Changed("desktop-port") {
		out.Desktop.HostPort = flags.Desktop.HostPort
		if !cmd.Flags().Changed("desktop") {
			out.Desktop.Enabled = true
		}
	}
	if cmd.Flags().Changed("desktop-password") {
		out.Desktop.Password = flags.Desktop.Password
		if !cmd.Flags().Changed("desktop") {
			out.Desktop.Enabled = true
		}
	}
	if cmd.Flags().Changed("desktop-user") {
		out.Desktop.User = flags.Desktop.User
		if !cmd.Flags().Changed("desktop") {
			out.Desktop.Enabled = true
		}
	}
	if cmd.Flags().Changed("workdir") {
		out.WorkingDir = flags.WorkingDir
	}
	if cmd.Flags().Changed("workspace") {
		out.WorkspaceName = flags.WorkspaceName
	}
	if cmd.Flags().Changed("workspace-path") {
		out.WorkspacePath = flags.WorkspacePath
	}
	if cmd.Flags().Changed("workspace-mount") {
		out.WorkspaceMountPath = flags.WorkspaceMountPath
	}
	if cmd.Flags().Changed("no-workspace") {
		out.DisableWorkspace = flags.DisableWorkspace
	}
	if cmd.Flags().Changed("pull") {
		out.Pull = flags.Pull
	}
	if noStart {
		out.Start = false
	}

	// List-like flags extend profile values so users can add one-off mounts,
	// ports, or env vars without duplicating the whole profile.
	out.Env = append(out.Env, flags.Env...)
	out.Ports = append(out.Ports, flags.Ports...)
	out.Volumes = append(out.Volumes, flags.Volumes...)
	out.CapAdd = append(out.CapAdd, flags.CapAdd...)
	return out
}

func newEnvListCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List Contaigen environments",
		Example: `  contaigen env list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			envs, err := eng.ListEnvironments(cmd.Context())
			if err != nil {
				return err
			}
			if len(envs) == 0 {
				printMuted(cmd, "No Contaigen environments found.")
				return nil
			}

			printHeader(cmd, "%-20s %-12s %-24s %-14s %s", "NAME", "STATE", "IMAGE", "NETWORK", "ID")
			p := colorFor(cmd)
			for _, env := range envs {
				fmt.Fprintf(cmd.OutOrStdout(), "%-20s %-12s %-24s %-14s %s\n", env.Name, p.state(env.State), truncate(env.Image, 24), env.NetworkMode, shortID(env.ID))
			}
			return nil
		},
	}
}

func newEnvInfoCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "info <name>",
		Short:   "Show environment details",
		Example: `  contaigen env info lab`,
		Args:    cobra.ExactArgs(1),
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
			desktopUnavailable := ""
			if env.Desktop.Enabled && env.NetworkProfile == model.NetworkProfileVPN && env.VPNName != "" {
				vpn, err := eng.InspectVPNGateway(cmd.Context(), env.VPNName)
				if err != nil {
					desktopUnavailable = fmt.Sprintf("unable to inspect VPN gateway %s", env.VPNName)
				} else if !model.HasPublishedPort(vpn.Ports, env.Desktop.HostIP, env.Desktop.HostPort, env.Desktop.ContainerPort) {
					desktopUnavailable = fmt.Sprintf("publish %s on VPN gateway %s", desktopPortMapping(env.Desktop), env.VPNName)
				}
			}

			out := cmd.OutOrStdout()
			p := colorFor(cmd)
			fmt.Fprintf(out, "%s: %s\n", p.bold("Name"), env.Name)
			fmt.Fprintf(out, "ID: %s\n", env.ID)
			fmt.Fprintf(out, "Container: %s\n", env.ContainerName)
			fmt.Fprintf(out, "Image: %s\n", env.Image)
			fmt.Fprintf(out, "State: %s\n", p.state(env.State))
			fmt.Fprintf(out, "Status: %s\n", env.Status)
			fmt.Fprintf(out, "Shell: %s\n", env.Shell)
			fmt.Fprintf(out, "User: %s\n", valueOrDash(env.User))
			fmt.Fprintf(out, "Hostname: %s\n", env.Hostname)
			fmt.Fprintf(out, "Network: %s\n", formatNetwork(env))
			fmt.Fprintf(out, "Workspace: %s\n", formatWorkspace(env))
			writeDesktopConnection(cmd, env, desktopUnavailable, true)
			if !env.CreatedAt.IsZero() {
				fmt.Fprintf(out, "Created: %s\n", env.CreatedAt.Format(time.RFC3339))
			}
			fmt.Fprintf(out, "Ports: %s\n", formatPorts(env.Ports))
			fmt.Fprintf(out, "Volumes: %s\n", formatVolumes(env.Volumes))
			fmt.Fprintf(out, "Capabilities: %s\n", formatStringList(env.CapAdd))
			return nil
		},
	}
}

func newEnvStartCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "start <name>",
		Short:   "Start an environment",
		Example: `  contaigen env start lab`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			if err := runWithProgress(cmd, "Start environment "+args[0], func(ctx context.Context) error {
				return eng.StartEnvironment(ctx, args[0])
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Started environment %s", args[0])
			return nil
		},
	}
}

func newEnvStopCommand(opts Options) *cobra.Command {
	var timeout int

	cmd := &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop an environment",
		Example: `  contaigen env stop lab
  contaigen env stop lab --timeout 3`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			timeoutPtr := &timeout
			if timeout < 0 {
				timeoutPtr = nil
			}
			if err := runWithProgress(cmd, "Stop environment "+args[0], func(ctx context.Context) error {
				return eng.StopEnvironment(ctx, args[0], timeoutPtr)
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Stopped environment %s", args[0])
			return nil
		},
	}

	cmd.Flags().IntVar(&timeout, "timeout", -1, "Seconds to wait before killing; -1 uses Docker's default")
	return cmd
}

func newEnvRemoveCommand(opts Options) *cobra.Command {
	var req model.RemoveEnvironmentRequest

	cmd := &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove"},
		Short:   "Remove an environment",
		Example: `  contaigen env rm lab
  contaigen env rm lab --force --volumes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			if err := runWithProgress(cmd, "Remove environment "+args[0], func(ctx context.Context) error {
				return eng.RemoveEnvironment(ctx, args[0], req)
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Removed environment %s", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&req.Force, "force", "f", false, "Force removal of a running environment")
	cmd.Flags().BoolVarP(&req.RemoveVolumes, "volumes", "v", false, "Remove anonymous volumes attached to the environment")
	return cmd
}

func newEnvEnterCommand(opts Options) *cobra.Command {
	var req model.EnterEnvironmentRequest
	var logSession bool
	var logOutput string

	cmd := &cobra.Command{
		Use:   "enter <name> [-- command...]",
		Short: "Enter a running environment",
		Example: `  contaigen env enter lab
  contaigen env enter lab --log
  contaigen env enter lab --log-output ./lab-session.log -- /bin/bash -lc 'id && pwd'`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			req.Command = args[1:]

			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			stdout := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()
			if logSession || logOutput != "" {
				logFile, logPath, err := openShellLog(opts, args[0], req, logOutput)
				if err != nil {
					return err
				}
				defer logFile.Close()
				printMuted(cmd, "Logging shell transcript to "+logPath)
				stdout = io.MultiWriter(stdout, logFile)
				stderr = io.MultiWriter(stderr, logFile)
			}

			return eng.EnterEnvironment(cmd.Context(), args[0], req, model.ExecIO{
				Stdin:  cmd.InOrStdin(),
				Stdout: stdout,
				Stderr: stderr,
			})
		},
	}

	cmd.Flags().StringVarP(&req.User, "user", "u", "", "User to run as inside the environment")
	cmd.Flags().StringVarP(&req.WorkDir, "workdir", "w", "", "Working directory inside the environment")
	cmd.Flags().BoolVar(&logSession, "log", false, "Write a shell transcript under Contaigen's log directory")
	cmd.Flags().StringVar(&logOutput, "log-output", "", "Write the shell transcript to a specific file")
	return cmd
}

func openShellLog(opts Options, envName string, req model.EnterEnvironmentRequest, outputPath string) (*os.File, string, error) {
	now := time.Now()
	logPath := strings.TrimSpace(outputPath)
	if logPath == "" {
		paths, err := opts.Paths()
		if err != nil {
			return nil, "", err
		}
		logPath = filepath.Join(paths.LogDir, "shell", envName, now.Format("20060102-150405.000000000")+".log")
	}
	logPath = expandHome(logPath)
	absPath, err := filepath.Abs(logPath)
	if err != nil {
		return nil, "", fmt.Errorf("resolve shell log path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absPath), 0o700); err != nil {
		return nil, "", fmt.Errorf("create shell log directory: %w", err)
	}
	file, err := os.OpenFile(absPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("open shell log: %w", err)
	}
	if _, err := fmt.Fprintf(file, "# Contaigen shell transcript\n# Environment: %s\n# Started: %s\n# Command: %s\n# User: %s\n# Workdir: %s\n\n",
		envName,
		now.Format(time.RFC3339),
		formatShellLogCommand(req.Command),
		valueOrDash(req.User),
		valueOrDash(req.WorkDir),
	); err != nil {
		_ = file.Close()
		return nil, "", fmt.Errorf("write shell log header: %w", err)
	}
	return file, absPath, nil
}

func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return path
		}
		if path == "~" {
			return home
		}
		return filepath.Join(home, strings.TrimPrefix(path, "~/"))
	}
	return path
}

func formatShellLogCommand(command []string) string {
	if len(command) == 0 {
		return "(environment default shell)"
	}
	return strings.Join(command, " ")
}

func openEngine(opts Options) (*engine.Engine, func(), error) {
	runtime, err := opts.NewDockerClient()
	if err != nil {
		return nil, nil, err
	}

	workspaces, err := opts.NewWorkspaceStore()
	if err != nil {
		_ = runtime.Close()
		return nil, nil, err
	}

	return engine.New(runtime, engine.WithWorkspaces(workspaces)), func() { _ = runtime.Close() }, nil
}

func parsePortMappings(values []string) ([]model.PortMapping, error) {
	mappings := make([]model.PortMapping, 0, len(values))
	for _, value := range values {
		mapping, err := parsePortMapping(value)
		if err != nil {
			return nil, err
		}
		mappings = append(mappings, mapping)
	}
	return mappings, nil
}

func parsePortMapping(value string) (model.PortMapping, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 && len(parts) != 3 {
		return model.PortMapping{}, fmt.Errorf("port mapping %q must be [host-ip:]host-port:container-port[/protocol]", value)
	}

	mapping := model.PortMapping{}
	if len(parts) == 3 {
		mapping.HostIP = parts[0]
		mapping.HostPort = parts[1]
		mapping.ContainerPort = parts[2]
	} else {
		mapping.HostPort = parts[0]
		mapping.ContainerPort = parts[1]
	}

	containerPort, protocol, hasProtocol := strings.Cut(mapping.ContainerPort, "/")
	mapping.ContainerPort = containerPort
	if hasProtocol {
		mapping.Protocol = strings.ToLower(protocol)
	} else {
		mapping.Protocol = "tcp"
	}

	if err := validatePort(mapping.HostPort, "host"); err != nil {
		return model.PortMapping{}, err
	}
	if err := validatePort(mapping.ContainerPort, "container"); err != nil {
		return model.PortMapping{}, err
	}
	switch mapping.Protocol {
	case "tcp", "udp", "sctp":
	default:
		return model.PortMapping{}, fmt.Errorf("port protocol %q must be tcp, udp, or sctp", mapping.Protocol)
	}
	return mapping, nil
}

func validatePort(value string, label string) error {
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("%s port %q must be a number from 1 to 65535", label, value)
	}
	return nil
}

func parseVolumeMounts(values []string) ([]model.VolumeMount, error) {
	mounts := make([]model.VolumeMount, 0, len(values))
	for _, value := range values {
		mount, err := parseVolumeMount(value)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, mount)
	}
	return mounts, nil
}

func parseVolumeMount(value string) (model.VolumeMount, error) {
	parts := strings.Split(value, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return model.VolumeMount{}, fmt.Errorf("volume %q must be source:target[:ro|rw]", value)
	}

	mount := model.VolumeMount{
		Source: parts[0],
		Target: parts[1],
	}
	if mount.Source == "" || mount.Target == "" {
		return model.VolumeMount{}, fmt.Errorf("volume %q must include source and target", value)
	}
	if len(parts) == 3 {
		switch strings.ToLower(parts[2]) {
		case "ro", "readonly":
			mount.ReadOnly = true
		case "rw", "":
			mount.ReadOnly = false
		default:
			return model.VolumeMount{}, fmt.Errorf("volume mode %q must be ro or rw", parts[2])
		}
	}
	return mount, nil
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}

func truncate(value string, max int) string {
	if len(value) <= max {
		return value
	}
	if max <= 1 {
		return value[:max]
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func formatPorts(ports []model.PortMapping) string {
	if len(ports) == 0 {
		return "none"
	}
	values := make([]string, 0, len(ports))
	for _, port := range ports {
		host := port.HostPort
		if port.HostIP != "" {
			host = port.HostIP + ":" + host
		}
		if host == "" {
			values = append(values, port.ContainerPort+"/"+port.Protocol)
			continue
		}
		values = append(values, host+":"+port.ContainerPort+"/"+port.Protocol)
	}
	return strings.Join(values, ", ")
}

func formatVolumes(volumes []model.VolumeMount) string {
	if len(volumes) == 0 {
		return "none"
	}
	values := make([]string, 0, len(volumes))
	for _, volume := range volumes {
		mode := "rw"
		if volume.ReadOnly {
			mode = "ro"
		}
		values = append(values, volume.Source+":"+volume.Target+":"+mode)
	}
	return strings.Join(values, ", ")
}

func formatWorkspace(env model.Environment) string {
	if env.WorkspaceName == "" && env.WorkspacePath == "" {
		return "none"
	}
	parts := []string{}
	if env.WorkspaceName != "" {
		parts = append(parts, env.WorkspaceName)
	}
	if env.WorkspacePath != "" {
		parts = append(parts, env.WorkspacePath)
	}
	if env.WorkspaceMountPath != "" {
		parts = append(parts, "mounted at "+env.WorkspaceMountPath)
	}
	return strings.Join(parts, " ")
}

func writeDesktopConnection(cmd *cobra.Command, env model.Environment, unavailable string, includeSudoNote bool) {
	if !env.Desktop.Enabled {
		return
	}
	out := cmd.OutOrStdout()
	if unavailable != "" {
		fmt.Fprintf(out, "Desktop URL: unavailable (%s)\n", unavailable)
		fmt.Fprintf(out, "noVNC user: %s\n", valueOrDash(env.Desktop.User))
		fmt.Fprintf(out, "noVNC password: %s\n", valueOrDash(env.Desktop.Password))
		if includeSudoNote {
			writeDesktopSudoNote(out, env.Name)
		}
		return
	}
	fmt.Fprintf(out, "Desktop URL: %s\n", desktopURL(env.Desktop))
	fmt.Fprintf(out, "noVNC user: %s\n", valueOrDash(env.Desktop.User))
	fmt.Fprintf(out, "noVNC password: %s\n", valueOrDash(env.Desktop.Password))
	if includeSudoNote {
		writeDesktopSudoNote(out, env.Name)
	}
}

func writeDesktopSudoNote(out io.Writer, envName string) {
	fmt.Fprintf(out, "Linux sudo: noVNC password is not the sudo password; use `contaigen env enter %s --user root` for a root shell.\n", envName)
}

func desktopPortMapping(desktop model.DesktopConfig) string {
	hostIP := desktop.HostIP
	if hostIP == "" {
		hostIP = model.DefaultDesktopHostIP
	}
	hostPort := desktop.HostPort
	if hostPort == "" {
		hostPort = model.DefaultDesktopPort
	}
	containerPort := desktop.ContainerPort
	if containerPort == "" {
		containerPort = model.DefaultDesktopPort
	}
	return fmt.Sprintf("%s:%s:%s", hostIP, hostPort, containerPort)
}

func desktopURL(desktop model.DesktopConfig) string {
	scheme := desktop.Scheme
	if scheme == "" {
		scheme = model.DefaultDesktopScheme
	}
	host := desktop.HostIP
	if host == "" {
		host = model.DefaultDesktopHostIP
	}
	port := desktop.HostPort
	if port == "" {
		port = model.DefaultDesktopPort
	}
	path := desktop.Path
	if path == "" {
		path = model.DefaultDesktopPath
	}
	return fmt.Sprintf("%s://%s:%s%s", scheme, host, port, path)
}

func formatNetwork(env model.Environment) string {
	parts := []string{}
	if env.NetworkProfile != "" {
		parts = append(parts, env.NetworkProfile)
	}
	if env.NetworkName != "" {
		parts = append(parts, env.NetworkName)
	}
	if env.NetworkMode != "" {
		parts = append(parts, "mode "+env.NetworkMode)
	}
	if env.VPNName != "" {
		parts = append(parts, "vpn "+env.VPNName)
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " ")
}
