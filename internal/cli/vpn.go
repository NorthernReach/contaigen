package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NorthernReach/contaigen/internal/model"
	"github.com/NorthernReach/contaigen/internal/vpnconfig"
	"github.com/spf13/cobra"
)

func newVPNCommand(opts Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "vpn",
		Aliases: []string{"vpns"},
		Short:   "Manage VPN gateway sidecars",
		Long: `Manage VPN gateway containers used by vpn-routed environments.

VPN gateways are explicit sidecar containers. A workbench created with
--network vpn --vpn <name> shares the gateway's network namespace, so the
workbench route is isolated from host traffic.`,
		Example: `  contaigen vpn create corp --config ~/vpn/client.ovpn --route-mode split --secret-env VPN_AUTH=CONTAIGEN_VPN_AUTH
  contaigen env create lab --profile parrot-default --network vpn --vpn corp
  contaigen vpn logs corp
  contaigen vpn info corp`,
	}

	cmd.AddCommand(newVPNCreateCommand(opts))
	cmd.AddCommand(newVPNListCommand(opts))
	cmd.AddCommand(newVPNInfoCommand(opts))
	cmd.AddCommand(newVPNStartCommand(opts))
	cmd.AddCommand(newVPNStopCommand(opts))
	cmd.AddCommand(newVPNRemoveCommand(opts))
	cmd.AddCommand(newVPNLogsCommand(opts))
	return cmd
}

func newVPNCreateCommand(opts Options) *cobra.Command {
	req := model.CreateVPNGatewayRequest{
		Image:           model.DefaultVPNImage,
		Provider:        model.DefaultVPNProvider,
		RouteMode:       model.VPNRouteModeFull,
		ConfigMountPath: model.DefaultVPNConfigMount,
		CapAdd:          []string{"NET_ADMIN"},
		Devices: []model.DeviceMapping{{
			HostPath:      "/dev/net/tun",
			ContainerPath: "/dev/net/tun",
			Permissions:   "rwm",
		}},
		Pull:  true,
		Start: true,
	}
	var envValues []string
	var envFileValues []string
	var secretEnvValues []string
	var portValues []string
	var vncValues []string
	var volumeValues []string
	var capAddValues []string
	var deviceValues []string
	var noStart bool

	cmd := &cobra.Command{
		Use:   "create <name> [-- command...]",
		Short: "Create an OpenVPN gateway sidecar",
		Long: `Create an OpenVPN gateway sidecar.

The default image is dperson/openvpn-client because it is designed for the
same Docker pattern Contaigen uses: start the VPN container first, then launch
other containers with network mode container:<vpn>. Use --image to bring your
own OpenVPN-compatible sidecar image.

With --route-mode split, Contaigen writes a managed OpenVPN config copy. If the
config contains static route directives, only those networks are routed through
the VPN. If the config has no static routes, Contaigen allows server-pushed
routes and filters default-gateway pushes so the sidecar does not become a full
tunnel by default.

Use --vnc to reserve noVNC desktop ports on the gateway for vpn-routed desktop
workbenches. With no value, --vnc publishes 127.0.0.1:6901:6901/tcp. A comma
list such as --vnc 6901,6902,6903 publishes multiple localhost endpoints.`,
		Example: `  contaigen vpn create corp --config ~/vpn/client.ovpn --route-mode split --secret-env VPN_AUTH=CONTAIGEN_VPN_AUTH
  contaigen vpn create corp --config ~/vpn/client.ovpn --env-file ./vpn.env
  contaigen vpn create corp --config ~/vpn --env VPN_FILES=client.ovpn
  contaigen vpn create htb-desktop --config ~/vpn/htb.ovpn --route-mode split --vnc 6901,6902
  contaigen vpn create corp --config ~/vpn/client.ovpn --port 127.0.0.1:8080:8080
  contaigen vpn create corp --image qmcgaw/gluetun --env VPN_SERVICE_PROVIDER=custom --env VPN_TYPE=openvpn`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			args, vncValues = consumeNoVNCValue(args, vncValues)
			req.Name = args[0]
			req.Command = args[1:]

			var err error
			req.Env, err = parseEnvFiles(envFileValues)
			if err != nil {
				return err
			}
			req.Env = append(req.Env, envValues...)
			secretEnv, err := parseSecretEnvValues(secretEnvValues)
			if err != nil {
				return err
			}
			req.Env = append(req.Env, secretEnv...)
			req.Ports, err = parsePortMappings(portValues)
			if err != nil {
				return err
			}
			req.NoVNCPorts, err = parseNoVNCPorts(vncValues)
			if err != nil {
				return err
			}
			req.Ports = appendNoVNCPorts(req.Ports, req.NoVNCPorts)
			req.Volumes, err = parseVolumeMounts(volumeValues)
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("cap-add") {
				req.CapAdd = capAddValues
			}
			if cmd.Flags().Changed("device") {
				req.Devices, err = parseDeviceMappings(deviceValues)
				if err != nil {
					return err
				}
			}
			if noStart {
				req.Start = false
			}
			var preWarnings []string
			if err := runStatus(cmd, "Prepare VPN route mode for "+req.Name, func(ctx context.Context) error {
				var err error
				preWarnings, err = prepareVPNRouteMode(opts, &req)
				return err
			}); err != nil {
				return err
			}

			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			var vpn model.VPNGateway
			var warnings []string
			if err := runWithProgress(cmd, "Create VPN gateway "+req.Name, func(ctx context.Context) error {
				var err error
				vpn, warnings, err = eng.CreateVPNGateway(ctx, req)
				return err
			}); err != nil {
				return err
			}
			for _, warning := range preWarnings {
				printWarning(cmd, "%s", warning)
			}
			for _, warning := range warnings {
				printWarning(cmd, "%s", warning)
			}
			printSuccess(cmd, "Created VPN gateway %s (%s)", vpn.Name, shortID(vpn.ID))
			fmt.Fprintf(cmd.OutOrStdout(), "Image: %s\n", vpn.Image)
			fmt.Fprintf(cmd.OutOrStdout(), "Config: %s -> %s\n", valueOrDash(vpn.ConfigPath), valueOrDash(vpn.ConfigMountPath))
			fmt.Fprintf(cmd.OutOrStdout(), "Route mode: %s\n", vpn.RouteMode)
			if len(vpn.Routes) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Routes: %s\n", formatVPNRoutes(vpn.Routes))
			}
			if len(vpn.NoVNCPorts) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "NoVNC: %s\n", formatPorts(vpn.NoVNCPorts))
			}
			if req.Start {
				printSuccess(cmd, "Started VPN gateway %s", vpn.Name)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&req.Image, "image", req.Image, "VPN sidecar image")
	cmd.Flags().StringVar(&req.Provider, "provider", req.Provider, "VPN provider type; openvpn is supported")
	cmd.Flags().StringVar(&req.RouteMode, "route-mode", req.RouteMode, "VPN route mode: full or split; split supports static or server-pushed routes")
	cmd.Flags().StringVar(&req.ConfigPath, "config", "", "OpenVPN config file or directory mounted read-only into the gateway")
	cmd.Flags().StringVar(&req.ConfigMountPath, "config-mount", req.ConfigMountPath, "Container path where VPN config is mounted")
	cmd.Flags().StringArrayVarP(&envValues, "env", "e", nil, "Environment variable in KEY=VALUE form")
	cmd.Flags().StringArrayVar(&envFileValues, "env-file", nil, "Read environment variables from a .env file; may be used more than once")
	cmd.Flags().StringArrayVar(&secretEnvValues, "secret-env", nil, "Inject secret from host env as KEY=HOST_ENV_NAME")
	cmd.Flags().StringArrayVarP(&portValues, "port", "p", nil, "Publish a port on the VPN gateway as [host-ip:]host-port:container-port[/protocol]")
	cmd.Flags().StringArrayVar(&vncValues, "vnc", nil, "Publish noVNC port(s) on the VPN gateway; defaults to 6901, or pass comma-separated ports like 6901,6902")
	cmd.Flags().StringArrayVarP(&volumeValues, "volume", "v", nil, "Bind mount a volume as source:target[:ro|rw]")
	cmd.Flags().StringArrayVar(&capAddValues, "cap-add", req.CapAdd, "Linux capability to add to the gateway")
	cmd.Flags().StringArrayVar(&deviceValues, "device", []string{"/dev/net/tun:/dev/net/tun:rwm"}, "Device mapping as host:container[:permissions]")
	cmd.Flags().BoolVar(&req.Privileged, "privileged", false, "Run the VPN gateway privileged")
	cmd.Flags().BoolVar(&req.Pull, "pull", req.Pull, "Pull the image if it is not available locally")
	cmd.Flags().BoolVar(&noStart, "no-start", false, "Create the VPN gateway without starting it")
	cmd.Flags().Lookup("vnc").NoOptDefVal = model.DefaultDesktopPort
	return cmd
}

func prepareVPNRouteMode(opts Options, req *model.CreateVPNGatewayRequest) ([]string, error) {
	if strings.ToLower(strings.TrimSpace(req.RouteMode)) != model.VPNRouteModeSplit {
		return nil, nil
	}
	paths, err := opts.Paths()
	if err != nil {
		return nil, err
	}
	prepared, err := vpnconfig.PrepareSplitConfig(req.ConfigPath, filepath.Join(paths.DataDir, "vpn", req.Name, "split"))
	if err != nil {
		return nil, err
	}
	req.ConfigPath = prepared.Path
	req.Routes = vpnConfigRoutes(prepared.Routes)
	return prepared.Warnings, nil
}

func consumeNoVNCValue(args []string, values []string) ([]string, []string) {
	if len(values) != 1 || values[0] != model.DefaultDesktopPort || len(args) < 2 {
		return args, values
	}
	if !looksLikeNoVNCValue(args[1]) {
		return args, values
	}
	nextArgs := append([]string{args[0]}, args[2:]...)
	return nextArgs, []string{args[1]}
}

func looksLikeNoVNCValue(value string) bool {
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			return false
		}
		if strings.Contains(item, ":") {
			if _, err := parseNoVNCPort(item); err != nil {
				return false
			}
			continue
		}
		if err := validatePort(item, "noVNC"); err != nil {
			return false
		}
	}
	return true
}

func parseNoVNCPorts(values []string) ([]model.PortMapping, error) {
	ports := []model.PortMapping(nil)
	for _, value := range values {
		for _, item := range strings.Split(value, ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			mapping, err := parseNoVNCPort(item)
			if err != nil {
				return nil, err
			}
			ports = append(ports, mapping)
		}
	}
	return ports, nil
}

func parseNoVNCPort(value string) (model.PortMapping, error) {
	if !strings.Contains(value, ":") {
		if err := validatePort(value, "noVNC"); err != nil {
			return model.PortMapping{}, err
		}
		return model.PortMapping{
			HostIP:        model.DefaultDesktopHostIP,
			HostPort:      value,
			ContainerPort: value,
			Protocol:      "tcp",
		}, nil
	}

	mapping, err := parsePortMapping(value)
	if err != nil {
		return model.PortMapping{}, fmt.Errorf("noVNC port %q: %w", value, err)
	}
	if mapping.HostIP == "" {
		mapping.HostIP = model.DefaultDesktopHostIP
	}
	if mapping.Protocol != "" && mapping.Protocol != "tcp" {
		return model.PortMapping{}, fmt.Errorf("noVNC port %q must use tcp", value)
	}
	mapping.Protocol = "tcp"
	return mapping, nil
}

func appendNoVNCPorts(ports []model.PortMapping, vncPorts []model.PortMapping) []model.PortMapping {
	out := append([]model.PortMapping(nil), ports...)
	for _, port := range vncPorts {
		if model.HasPublishedPort(out, port.HostIP, port.HostPort, port.ContainerPort) {
			continue
		}
		out = append(out, port)
	}
	return out
}

func newVPNListCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List VPN gateways",
		Example: `  contaigen vpn list`,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			vpns, err := eng.ListVPNGateways(cmd.Context())
			if err != nil {
				return err
			}
			if len(vpns) == 0 {
				printMuted(cmd, "No Contaigen VPN gateways found.")
				return nil
			}

			printHeader(cmd, "%-18s %-12s %-24s %-10s %s", "NAME", "STATE", "IMAGE", "PROVIDER", "ID")
			p := colorFor(cmd)
			for _, vpn := range vpns {
				fmt.Fprintf(cmd.OutOrStdout(), "%-18s %-12s %-24s %-10s %s\n", vpn.Name, p.state(vpn.State), truncate(vpn.Image, 24), vpn.Provider, shortID(vpn.ID))
			}
			return nil
		},
	}
}

func newVPNInfoCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "info <name>",
		Short:   "Show VPN gateway details",
		Example: `  contaigen vpn info corp`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			vpn, err := eng.InspectVPNGateway(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			writeVPNDetails(cmd, vpn)
			return nil
		},
	}
}

func newVPNStartCommand(opts Options) *cobra.Command {
	return &cobra.Command{
		Use:     "start <name>",
		Short:   "Start a VPN gateway",
		Example: `  contaigen vpn start corp`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			if err := runWithProgress(cmd, "Start VPN gateway "+args[0], func(ctx context.Context) error {
				return eng.StartVPNGateway(ctx, args[0])
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Started VPN gateway %s", args[0])
			return nil
		},
	}
}

func newVPNStopCommand(opts Options) *cobra.Command {
	var timeout int

	cmd := &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a VPN gateway",
		Example: `  contaigen vpn stop corp
  contaigen vpn stop corp --timeout 3`,
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
			if err := runWithProgress(cmd, "Stop VPN gateway "+args[0], func(ctx context.Context) error {
				return eng.StopVPNGateway(ctx, args[0], timeoutPtr)
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Stopped VPN gateway %s", args[0])
			return nil
		},
	}

	cmd.Flags().IntVar(&timeout, "timeout", -1, "Seconds to wait before killing; -1 uses Docker's default")
	return cmd
}

func newVPNRemoveCommand(opts Options) *cobra.Command {
	var req model.RemoveVPNGatewayRequest

	cmd := &cobra.Command{
		Use:     "rm <name>",
		Aliases: []string{"remove"},
		Short:   "Remove a VPN gateway",
		Example: `  contaigen vpn rm corp
  contaigen vpn rm corp --force --volumes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			if err := runWithProgress(cmd, "Remove VPN gateway "+args[0], func(ctx context.Context) error {
				return eng.RemoveVPNGateway(ctx, args[0], req)
			}); err != nil {
				return err
			}
			printSuccess(cmd, "Removed VPN gateway %s", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&req.Force, "force", "f", false, "Force removal of a running VPN gateway")
	cmd.Flags().BoolVarP(&req.RemoveVolumes, "volumes", "v", false, "Remove anonymous volumes attached to the VPN gateway")
	return cmd
}

func newVPNLogsCommand(opts Options) *cobra.Command {
	req := model.VPNLogsRequest{Tail: "100"}

	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Show VPN gateway logs",
		Example: `  contaigen vpn logs corp
  contaigen vpn logs corp --follow --tail 200`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			eng, closeFn, err := openEngine(opts)
			if err != nil {
				return err
			}
			defer closeFn()

			return eng.VPNGatewayLogs(cmd.Context(), args[0], req, model.VPNLogIO{
				Stdout: cmd.OutOrStdout(),
				Stderr: cmd.ErrOrStderr(),
			})
		},
	}

	cmd.Flags().BoolVarP(&req.Follow, "follow", "f", false, "Follow VPN logs")
	cmd.Flags().StringVar(&req.Tail, "tail", req.Tail, "Number of lines to show from the end of logs")
	return cmd
}

func writeVPNDetails(cmd *cobra.Command, vpn model.VPNGateway) {
	out := cmd.OutOrStdout()
	p := colorFor(cmd)
	fmt.Fprintf(out, "%s: %s\n", p.bold("Name"), vpn.Name)
	fmt.Fprintf(out, "ID: %s\n", vpn.ID)
	fmt.Fprintf(out, "Container: %s\n", vpn.ContainerName)
	fmt.Fprintf(out, "Image: %s\n", vpn.Image)
	fmt.Fprintf(out, "Provider: %s\n", vpn.Provider)
	fmt.Fprintf(out, "Route mode: %s\n", valueOrDash(vpn.RouteMode))
	fmt.Fprintf(out, "Routes: %s\n", formatVPNRoutes(vpn.Routes))
	fmt.Fprintf(out, "State: %s\n", p.state(vpn.State))
	fmt.Fprintf(out, "Status: %s\n", vpn.Status)
	fmt.Fprintf(out, "Config: %s -> %s\n", valueOrDash(vpn.ConfigPath), valueOrDash(vpn.ConfigMountPath))
	if !vpn.CreatedAt.IsZero() {
		fmt.Fprintf(out, "Created: %s\n", vpn.CreatedAt.Format(time.RFC3339))
	}
	fmt.Fprintf(out, "Ports: %s\n", formatPorts(vpn.Ports))
	fmt.Fprintf(out, "NoVNC: %s\n", formatPorts(vpn.NoVNCPorts))
	fmt.Fprintf(out, "Volumes: %s\n", formatVolumes(vpn.Volumes))
	fmt.Fprintf(out, "Env keys: %s\n", formatEnvKeys(vpn.Env))
	fmt.Fprintf(out, "Command: %s\n", formatCommand(vpn.Command))
	fmt.Fprintf(out, "Capabilities: %s\n", formatStringList(vpn.CapAdd))
	fmt.Fprintf(out, "Devices: %s\n", formatDevices(vpn.Devices))
}

func parseSecretEnvValues(values []string) ([]string, error) {
	out := make([]string, 0, len(values))
	for _, value := range values {
		key, source, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(key) == "" || strings.TrimSpace(source) == "" {
			return nil, fmt.Errorf("secret env %q must be KEY=HOST_ENV_NAME", value)
		}
		secret, ok := os.LookupEnv(source)
		if !ok {
			return nil, fmt.Errorf("host environment variable %q is not set for secret %q", source, key)
		}
		out = append(out, key+"="+secret)
	}
	return out, nil
}

func parseDeviceMappings(values []string) ([]model.DeviceMapping, error) {
	devices := make([]model.DeviceMapping, 0, len(values))
	for _, value := range values {
		device, err := parseDeviceMapping(value)
		if err != nil {
			return nil, err
		}
		devices = append(devices, device)
	}
	return devices, nil
}

func parseDeviceMapping(value string) (model.DeviceMapping, error) {
	parts := strings.Split(value, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return model.DeviceMapping{}, fmt.Errorf("device %q must be host:container[:permissions]", value)
	}
	device := model.DeviceMapping{
		HostPath:      parts[0],
		ContainerPath: parts[1],
		Permissions:   "rwm",
	}
	if device.HostPath == "" || device.ContainerPath == "" {
		return model.DeviceMapping{}, fmt.Errorf("device %q must include host and container paths", value)
	}
	if len(parts) == 3 && parts[2] != "" {
		device.Permissions = parts[2]
	}
	return device, nil
}

func formatEnvKeys(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(values))
	for _, value := range values {
		key, _, ok := strings.Cut(value, "=")
		if !ok {
			key = value
		}
		keys = append(keys, key)
	}
	return strings.Join(keys, ", ")
}

func formatDevices(devices []model.DeviceMapping) string {
	if len(devices) == 0 {
		return "none"
	}
	values := make([]string, 0, len(devices))
	for _, device := range devices {
		perms := device.Permissions
		if perms == "" {
			perms = "rwm"
		}
		values = append(values, device.HostPath+":"+device.ContainerPath+":"+perms)
	}
	return strings.Join(values, ", ")
}

func vpnConfigRoutes(routes []vpnconfig.Route) []model.VPNRoute {
	out := make([]model.VPNRoute, 0, len(routes))
	for _, route := range routes {
		out = append(out, model.VPNRoute{
			CIDR:      route.CIDR,
			Directive: route.Directive,
		})
	}
	return out
}

func formatVPNRoutes(routes []model.VPNRoute) string {
	if len(routes) == 0 {
		return "none"
	}
	values := make([]string, 0, len(routes))
	for _, route := range routes {
		values = append(values, route.CIDR)
	}
	return strings.Join(values, ", ")
}
