package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NorthernReach/contaigen/internal/model"
	"github.com/NorthernReach/contaigen/internal/templates"
	"github.com/spf13/cobra"
)

func newServiceCommand(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "service",
		Aliases: []string{"services", "svc"},
		Short:   "Manage target app containers attached to workbenches",
		Long: `Manage target application containers attached to Contaigen environments.

Services are Docker containers labeled by Contaigen and connected to a
workbench environment network. Use a segment network for stable DNS aliases
from the workbench into target applications.`,
		Example: `  contaigen env create lab --profile parrot-default --network segment
  contaigen service add lab nginx:alpine --name target --alias target.local --port 127.0.0.1:8080:80
  contaigen service list lab
  contaigen service info lab target
  contaigen service stop lab target
  contaigen service rm lab target --force`,
	}

	cmd.AddCommand(newServiceAddCommand(opts))
	cmd.AddCommand(newServiceListCommand(opts))
	cmd.AddCommand(newServiceInfoCommand(opts))
	cmd.AddCommand(newServiceStartCommand(opts))
	cmd.AddCommand(newServiceStopCommand(opts))
	cmd.AddCommand(newServiceRemoveCommand(opts))
	cmd.AddCommand(newServiceTemplateCommand(opts))
	return cmd
}

func newServiceAddCommand(opts Options) *cobra.Command {
	req := model.CreateServiceRequest{
		Pull:  true,
		Start: true,
	}
	var envValues []string
	var envFileValues []string
	var portValues []string
	var volumeValues []string
	var noStart bool

	cmd := &cobra.Command{
		Use:   "add <env> <image-or-template> [-- command...]",
		Short: "Create a target service from an image or template",
		Long: `Create a target service container from a Docker image or service template.

The service is attached to the selected environment's network and receives a
DNS alias. Environments created with --network segment are the intended path
for application testing because Docker's default bridge network does not offer
the same service discovery behavior.`,
		Example: `  contaigen service add lab juice-shop
  contaigen service add lab nginx:alpine --name target --alias target.local
  contaigen service add lab ghcr.io/example/app:latest --port 127.0.0.1:8080:80
  contaigen service add lab postgres:16 --name db --env-file ./postgres.env
  contaigen service add lab python:3.12-alpine --name api -- python -m http.server 8000`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			flagReq := req
			flagReq.EnvironmentName = args[0]
			flagReq.Image = args[1]
			flagReq.Command = args[2:]
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

			finalReq := defaultCreateServiceRequest(args[0])
			profiles, err := opts.NewProfileStore()
			if err != nil {
				return err
			}
			serviceTemplate, err := profiles.LoadService(cmd.Context(), args[1])
			switch {
			case err == nil:
				finalReq = templates.ApplyServiceTemplate(serviceTemplate, finalReq)
			case errors.Is(err, templates.ErrServiceTemplateNotFound):
				finalReq.Image = args[1]
			default:
				return err
			}
			finalReq = applyServiceAddFlagOverrides(cmd, finalReq, flagReq, noStart)

			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			var service model.Service
			var warnings []string
			if err := runWithProgress(cmd, "Create service "+finalReq.Name, func(ctx context.Context) error {
				var err error
				service, warnings, err = eng.CreateService(ctx, finalReq)
				return err
			}); err != nil {
				return err
			}
			for _, warning := range warnings {
				printWarning(cmd, "%s", warning)
			}
			printSuccess(cmd, "Created service %s for environment %s (%s)", service.Name, service.EnvironmentName, shortID(service.ID))
			fmt.Fprintf(cmd.OutOrStdout(), "Image: %s\n", service.Image)
			fmt.Fprintf(cmd.OutOrStdout(), "Network: %s", service.NetworkName)
			if service.NetworkAlias != "" {
				fmt.Fprintf(cmd.OutOrStdout(), " alias %s", service.NetworkAlias)
			}
			fmt.Fprintln(cmd.OutOrStdout())
			if finalReq.Start {
				printSuccess(cmd, "Started service %s", service.Name)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&req.Name, "name", "", "Service name; defaults to the image name")
	cmd.Flags().StringVar(&req.NetworkAlias, "alias", "", "DNS alias on the environment network; defaults to the service name")
	cmd.Flags().StringArrayVarP(&envValues, "env", "e", nil, "Environment variable in KEY=VALUE form")
	cmd.Flags().StringArrayVar(&envFileValues, "env-file", nil, "Read environment variables from a .env file; may be used more than once")
	cmd.Flags().StringArrayVarP(&portValues, "port", "p", nil, "Publish a port as [host-ip:]host-port:container-port[/protocol]")
	cmd.Flags().StringArrayVarP(&volumeValues, "volume", "v", nil, "Bind mount a volume as source:target[:ro|rw]")
	cmd.Flags().BoolVar(&req.Pull, "pull", req.Pull, "Pull the image if it is not available locally")
	cmd.Flags().BoolVar(&noStart, "no-start", false, "Create the service without starting it")
	return cmd
}

func defaultCreateServiceRequest(envName string) model.CreateServiceRequest {
	return model.CreateServiceRequest{
		EnvironmentName: envName,
		Pull:            true,
		Start:           true,
	}
}

func applyServiceAddFlagOverrides(cmd *cobra.Command, base model.CreateServiceRequest, flags model.CreateServiceRequest, noStart bool) model.CreateServiceRequest {
	out := base
	out.EnvironmentName = flags.EnvironmentName

	if cmd.Flags().Changed("name") {
		out.Name = flags.Name
	}
	if cmd.Flags().Changed("alias") {
		out.NetworkAlias = flags.NetworkAlias
	}
	if cmd.Flags().Changed("pull") {
		out.Pull = flags.Pull
	}
	if noStart {
		out.Start = false
	}
	if len(flags.Command) > 0 {
		out.Command = flags.Command
	}
	if out.Name == "" {
		out.Name = deriveServiceName(out.Image)
	}
	if out.NetworkAlias == "" {
		out.NetworkAlias = out.Name
	}

	out.Env = append(out.Env, flags.Env...)
	out.Ports = append(out.Ports, flags.Ports...)
	out.Volumes = append(out.Volumes, flags.Volumes...)
	return out
}

func newServiceListCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "list [env]",
		Short: "List target services",
		Example: `  contaigen service list
  contaigen service list lab`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := ""
			if len(args) == 1 {
				envName = args[0]
			}
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			services, err := eng.ListServices(cmd.Context(), envName)
			if err != nil {
				return err
			}
			if len(services) == 0 {
				printMuted(cmd, "No Contaigen services found.")
				return nil
			}

			printHeader(cmd, "%-16s %-16s %-12s %-24s %-18s %s", "ENV", "SERVICE", "STATE", "IMAGE", "ALIAS", "ID")
			p := colorFor(cmd)
			for _, service := range services {
				fmt.Fprintf(cmd.OutOrStdout(), "%-16s %-16s %-12s %-24s %-18s %s\n",
					service.EnvironmentName,
					service.Name,
					p.state(service.State),
					truncate(service.Image, 24),
					valueOrDash(service.NetworkAlias),
					shortID(service.ID),
				)
			}
			return nil
		},
	}
}

func newServiceInfoCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "info <env> <service>",
		Short:   "Show target service details",
		Example: `  contaigen service info lab target`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			service, err := eng.InspectService(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			writeServiceDetails(cmd, service)
			return nil
		},
	}
}

func newServiceStartCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "start <env> <service>",
		Short:   "Start a target service",
		Example: `  contaigen service start lab target`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			if err := runWithProgress(cmd, "Start service "+args[1], func(ctx context.Context) error {
				return eng.StartService(ctx, args[0], args[1])
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Started service %s for environment %s", args[1], args[0])
			return nil
		},
	}
}

func newServiceStopCommand(opts Options) *cobra.Command {
	var timeout int

	cmd := &cobra.Command{
		Use:   "stop <env> <service>",
		Short: "Stop a target service",
		Example: `  contaigen service stop lab target
  contaigen service stop lab target --timeout 3`,
		Args: cobra.ExactArgs(2),
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
			if err := runWithProgress(cmd, "Stop service "+args[1], func(ctx context.Context) error {
				return eng.StopService(ctx, args[0], args[1], timeoutPtr)
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Stopped service %s for environment %s", args[1], args[0])
			return nil
		},
	}

	cmd.Flags().IntVar(&timeout, "timeout", -1, "Seconds to wait before killing; -1 uses Docker's default")
	return cmd
}

func newServiceRemoveCommand(opts Options) *cobra.Command {
	var req model.RemoveServiceRequest

	cmd := &cobra.Command{
		Use:     "rm <env> <service>",
		Aliases: []string{"remove"},
		Short:   "Remove a target service",
		Example: `  contaigen service rm lab target
  contaigen service rm lab target --force --volumes`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			if err := runWithProgress(cmd, "Remove service "+args[1], func(ctx context.Context) error {
				return eng.RemoveService(ctx, args[0], args[1], req)
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Removed service %s for environment %s", args[1], args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&req.Force, "force", "f", false, "Force removal of a running service")
	cmd.Flags().BoolVarP(&req.RemoveVolumes, "volumes", "v", false, "Remove anonymous volumes attached to the service")
	return cmd
}

func newServiceTemplateCommand(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "template",
		Aliases: []string{"templates"},
		Short:   "List and inspect service templates",
		Long: `List and inspect reusable service templates.

Service templates describe common target application containers. They can be
used directly with service add by passing the template name instead of an
image reference.`,
		Example: `  contaigen service template list
  contaigen service template show juice-shop
  contaigen service add lab juice-shop`,
	}

	cmd.AddCommand(newServiceTemplateListCommand(opts))
	cmd.AddCommand(newServiceTemplateShowCommand(opts))
	return cmd
}

func newServiceTemplateListCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List service templates",
		Example: `  contaigen service template list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := opts.NewProfileStore()
			if err != nil {
				return err
			}
			services, err := profiles.ListServices(cmd.Context())
			if err != nil {
				return err
			}
			if len(services) == 0 {
				printMuted(cmd, "No service templates found.")
				return nil
			}

			printHeader(cmd, "%-18s %-28s %-18s %-10s %s", "NAME", "IMAGE", "ALIAS", "SOURCE", "DESCRIPTION")
			for _, service := range services {
				fmt.Fprintf(cmd.OutOrStdout(), "%-18s %-28s %-18s %-10s %s\n",
					service.Name,
					truncate(service.Image, 28),
					valueOrDash(service.Alias),
					service.Source,
					service.Description,
				)
			}
			return nil
		},
	}
}

func newServiceTemplateShowCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:   "show <name-or-path>",
		Short: "Show a service template",
		Example: `  contaigen service template show juice-shop
  contaigen service template show ./services/target.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			profiles, err := opts.NewProfileStore()
			if err != nil {
				return err
			}
			service, err := profiles.LoadService(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			p := colorFor(cmd)
			fmt.Fprintf(out, "%s: %s\n", p.bold("Name"), service.Metadata.Name)
			fmt.Fprintf(out, "Description: %s\n", valueOrDash(service.Metadata.Description))
			fmt.Fprintf(out, "Image: %s\n", service.Spec.Image)
			fmt.Fprintf(out, "Service name: %s\n", valueOrDash(service.Spec.Name))
			fmt.Fprintf(out, "Alias: %s\n", valueOrDash(service.Spec.NetworkAlias))
			fmt.Fprintf(out, "Env: %s\n", formatStringList(service.Spec.Env))
			fmt.Fprintf(out, "Ports: %s\n", formatPorts(service.Spec.Ports))
			fmt.Fprintf(out, "Volumes: %s\n", formatVolumes(service.Spec.Volumes))
			fmt.Fprintf(out, "Command: %s\n", formatCommand(service.Spec.Command))
			fmt.Fprintf(out, "Source: %s\n", service.Source)
			if service.Path != "" {
				fmt.Fprintf(out, "Path: %s\n", service.Path)
			}
			return nil
		},
	}
}

func writeServiceDetails(cmd *cobra.Command, service model.Service) {
	out := cmd.OutOrStdout()
	p := colorFor(cmd)
	fmt.Fprintf(out, "%s: %s\n", p.bold("Name"), service.Name)
	fmt.Fprintf(out, "ID: %s\n", service.ID)
	fmt.Fprintf(out, "Container: %s\n", service.ContainerName)
	fmt.Fprintf(out, "Environment: %s\n", service.EnvironmentName)
	fmt.Fprintf(out, "Image: %s\n", service.Image)
	fmt.Fprintf(out, "State: %s\n", p.state(service.State))
	fmt.Fprintf(out, "Status: %s\n", service.Status)
	fmt.Fprintf(out, "Network: %s\n", valueOrDash(service.NetworkName))
	fmt.Fprintf(out, "Alias: %s\n", valueOrDash(service.NetworkAlias))
	if !service.CreatedAt.IsZero() {
		fmt.Fprintf(out, "Created: %s\n", service.CreatedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(out, "Ports: %s\n", formatPorts(service.Ports))
	fmt.Fprintf(out, "Volumes: %s\n", formatVolumes(service.Volumes))
	fmt.Fprintf(out, "Command: %s\n", formatCommand(service.Command))
}

func formatCommand(command []string) string {
	if len(command) == 0 {
		return "image default"
	}
	return strings.Join(command, " ")
}

func deriveServiceName(image string) string {
	name := image
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	if at := strings.Index(name, "@"); at >= 0 {
		name = name[:at]
	}
	if colon := strings.Index(name, ":"); colon >= 0 {
		name = name[:colon]
	}

	var out strings.Builder
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r)
		case r >= 'a' && r <= 'z':
			out.WriteRune(r)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == '.' || r == '_' || r == '-':
			out.WriteRune(r)
		default:
			out.WriteRune('-')
		}
	}

	candidate := strings.Trim(out.String(), ".-_")
	if candidate == "" {
		return "service"
	}
	return candidate
}
