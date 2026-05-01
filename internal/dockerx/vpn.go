package dockerx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/NorthernReach/contaigen/internal/model"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

var (
	ErrVPNGatewayNotFound = errors.New("vpn gateway not found")
	ErrVPNGatewayExists   = errors.New("vpn gateway already exists")
)

func (c *Client) CreateVPNGateway(ctx context.Context, req model.CreateVPNGatewayRequest) (model.VPNGateway, []string, error) {
	if _, err := c.findVPNGateway(ctx, req.Name); err == nil {
		return model.VPNGateway{}, nil, fmt.Errorf("%w: %s", ErrVPNGatewayExists, req.Name)
	} else if !errors.Is(err, ErrVPNGatewayNotFound) {
		return model.VPNGateway{}, nil, err
	}

	labels := model.VPNLabels(req.Name, req.Provider)
	if req.ConfigPath != "" {
		labels[model.LabelVPNConfig] = req.ConfigPath
	}
	if req.ConfigMountPath != "" {
		labels[model.LabelVPNConfigMount] = req.ConfigMountPath
	}
	if req.RouteMode != "" {
		labels[model.LabelVPNRouteMode] = req.RouteMode
	}
	if len(req.Routes) > 0 {
		labels[model.LabelVPNRoutes] = joinVPNRoutes(req.Routes)
	}
	if len(req.NoVNCPorts) > 0 {
		labels[model.LabelVPNNoVNCEnabled] = "true"
		labels[model.LabelVPNNoVNCPorts] = joinVPNPortMappings(req.NoVNCPorts)
	}
	portSet, portMap, err := dockerPorts(req.Ports)
	if err != nil {
		return model.VPNGateway{}, nil, err
	}

	resp, err := c.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: vpnContainerName(req.Name),
		Config: &container.Config{
			Image:        req.Image,
			Env:          req.Env,
			Cmd:          req.Command,
			ExposedPorts: portSet,
			Labels:       labels,
		},
		HostConfig: &container.HostConfig{
			CapAdd:       req.CapAdd,
			Mounts:       dockerMounts(req.Volumes),
			PortBindings: portMap,
			Privileged:   req.Privileged,
			Resources: container.Resources{
				Devices: dockerDevices(req.Devices),
			},
			RestartPolicy: container.RestartPolicy{
				Name: container.RestartPolicyUnlessStopped,
			},
		},
	})
	if err != nil {
		return model.VPNGateway{}, nil, err
	}

	vpn, err := c.InspectVPNGateway(ctx, req.Name)
	if err != nil {
		return model.VPNGateway{}, resp.Warnings, err
	}
	return vpn, resp.Warnings, nil
}

func (c *Client) ListVPNGateways(ctx context.Context) ([]model.VPNGateway, error) {
	filters := make(client.Filters).
		Add("label", model.LabelManaged+"=true").
		Add("label", model.LabelKind+"="+model.KindVPN)
	resp, err := c.api.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: filters,
	})
	if err != nil {
		return nil, err
	}

	vpns := make([]model.VPNGateway, 0, len(resp.Items))
	for _, item := range resp.Items {
		vpns = append(vpns, vpnGatewayFromSummary(item))
	}
	sort.Slice(vpns, func(i, j int) bool {
		return vpns[i].Name < vpns[j].Name
	})
	return vpns, nil
}

func (c *Client) InspectVPNGateway(ctx context.Context, name string) (model.VPNGateway, error) {
	vpn, err := c.findVPNGateway(ctx, name)
	if err != nil {
		return model.VPNGateway{}, err
	}
	resp, err := c.api.ContainerInspect(ctx, vpn.ID, client.ContainerInspectOptions{})
	if err != nil {
		return model.VPNGateway{}, err
	}
	return vpnGatewayFromInspect(resp.Container), nil
}

func (c *Client) StartVPNGateway(ctx context.Context, name string) error {
	vpn, err := c.findVPNGateway(ctx, name)
	if err != nil {
		return err
	}
	_, err = c.api.ContainerStart(ctx, vpn.ID, client.ContainerStartOptions{})
	return err
}

func (c *Client) StopVPNGateway(ctx context.Context, name string, timeout *int) error {
	vpn, err := c.findVPNGateway(ctx, name)
	if err != nil {
		return err
	}
	_, err = c.api.ContainerStop(ctx, vpn.ID, client.ContainerStopOptions{Timeout: timeout})
	return err
}

func (c *Client) RemoveVPNGateway(ctx context.Context, name string, req model.RemoveVPNGatewayRequest) error {
	vpn, err := c.findVPNGateway(ctx, name)
	if err != nil {
		return err
	}
	_, err = c.api.ContainerRemove(ctx, vpn.ID, client.ContainerRemoveOptions{
		Force:         req.Force,
		RemoveVolumes: req.RemoveVolumes,
	})
	return err
}

func (c *Client) VPNGatewayLogs(ctx context.Context, name string, req model.VPNLogsRequest, streams model.VPNLogIO) error {
	vpn, err := c.findVPNGateway(ctx, name)
	if err != nil {
		return err
	}
	tail := req.Tail
	if tail == "" {
		tail = "100"
	}
	resp, err := c.api.ContainerLogs(ctx, vpn.ID, client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     req.Follow,
		Tail:       tail,
	})
	if err != nil {
		return err
	}
	defer resp.Close()

	stdout := streams.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := streams.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	_, err = stdcopy.StdCopy(stdout, stderr, resp)
	return err
}

func (c *Client) findVPNGateway(ctx context.Context, name string) (model.VPNGateway, error) {
	filters := make(client.Filters).
		Add("label", model.LabelManaged+"=true").
		Add("label", model.LabelKind+"="+model.KindVPN).
		Add("label", model.LabelVPN+"="+name)
	resp, err := c.api.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: filters,
	})
	if err != nil {
		return model.VPNGateway{}, err
	}
	if len(resp.Items) == 0 {
		return model.VPNGateway{}, fmt.Errorf("%w: %s", ErrVPNGatewayNotFound, name)
	}
	if len(resp.Items) > 1 {
		return model.VPNGateway{}, fmt.Errorf("multiple Contaigen VPN gateways named %q", name)
	}
	return vpnGatewayFromSummary(resp.Items[0]), nil
}

func vpnGatewayFromSummary(summary container.Summary) model.VPNGateway {
	labels := cloneLabels(summary.Labels)
	return model.VPNGateway{
		ID:              summary.ID,
		Name:            labels[model.LabelVPN],
		ContainerName:   cleanContainerName(summary.Names),
		Image:           summary.Image,
		Provider:        labels[model.LabelProfile],
		RouteMode:       routeModeFromLabels(labels),
		Routes:          routesFromLabels(labels),
		State:           string(summary.State),
		Status:          summary.Status,
		ConfigPath:      labels[model.LabelVPNConfig],
		ConfigMountPath: labels[model.LabelVPNConfigMount],
		CreatedAt:       time.Unix(summary.Created, 0),
		Ports:           portsFromSummary(summary.Ports),
		NoVNCPorts:      noVNCPortsFromLabels(labels),
		Volumes:         volumesFromMountPoints(summary.Mounts),
		Labels:          labels,
	}
}

func vpnGatewayFromInspect(inspect container.InspectResponse) model.VPNGateway {
	labels := map[string]string{}
	image := inspect.Image
	env := []string(nil)
	command := []string(nil)
	if inspect.Config != nil {
		labels = cloneLabels(inspect.Config.Labels)
		image = inspect.Config.Image
		env = append([]string(nil), inspect.Config.Env...)
		command = append([]string(nil), inspect.Config.Cmd...)
	}

	state := ""
	status := ""
	if inspect.State != nil {
		state = string(inspect.State.Status)
		status = string(inspect.State.Status)
	}

	ports := []model.PortMapping(nil)
	capAdd := []string(nil)
	devices := []model.DeviceMapping(nil)
	if inspect.HostConfig != nil {
		ports = portsFromPortMap(inspect.HostConfig.PortBindings)
		capAdd = append([]string(nil), inspect.HostConfig.CapAdd...)
		devices = modelDevices(inspect.HostConfig.Resources.Devices)
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, inspect.Created)

	return model.VPNGateway{
		ID:              inspect.ID,
		Name:            labels[model.LabelVPN],
		ContainerName:   strings.TrimPrefix(inspect.Name, "/"),
		Image:           image,
		Provider:        labels[model.LabelProfile],
		RouteMode:       routeModeFromLabels(labels),
		Routes:          routesFromLabels(labels),
		State:           state,
		Status:          status,
		ConfigPath:      labels[model.LabelVPNConfig],
		ConfigMountPath: labels[model.LabelVPNConfigMount],
		CreatedAt:       createdAt,
		Ports:           ports,
		NoVNCPorts:      noVNCPortsFromLabels(labels),
		Volumes:         volumesFromMountPoints(inspect.Mounts),
		Env:             env,
		Command:         command,
		CapAdd:          capAdd,
		Devices:         devices,
		Labels:          labels,
	}
}

func dockerDevices(devices []model.DeviceMapping) []container.DeviceMapping {
	if len(devices) == 0 {
		return nil
	}
	out := make([]container.DeviceMapping, 0, len(devices))
	for _, device := range devices {
		out = append(out, container.DeviceMapping{
			PathOnHost:        device.HostPath,
			PathInContainer:   device.ContainerPath,
			CgroupPermissions: device.Permissions,
		})
	}
	return out
}

func modelDevices(devices []container.DeviceMapping) []model.DeviceMapping {
	if len(devices) == 0 {
		return nil
	}
	out := make([]model.DeviceMapping, 0, len(devices))
	for _, device := range devices {
		out = append(out, model.DeviceMapping{
			HostPath:      device.PathOnHost,
			ContainerPath: device.PathInContainer,
			Permissions:   device.CgroupPermissions,
		})
	}
	return out
}

func vpnContainerName(name string) string {
	return "contaigen-vpn-" + name
}

func joinVPNRoutes(routes []model.VPNRoute) string {
	values := make([]string, 0, len(routes))
	for _, route := range routes {
		if route.CIDR != "" {
			values = append(values, route.CIDR)
		}
	}
	return strings.Join(values, ",")
}

func routesFromLabels(labels map[string]string) []model.VPNRoute {
	value := labels[model.LabelVPNRoutes]
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	routes := make([]model.VPNRoute, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			routes = append(routes, model.VPNRoute{CIDR: part})
		}
	}
	return routes
}

func joinVPNPortMappings(ports []model.PortMapping) string {
	values := make([]string, 0, len(ports))
	for _, port := range ports {
		protocol := port.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		host := port.HostPort
		if port.HostIP != "" {
			host = port.HostIP + ":" + host
		}
		values = append(values, host+":"+port.ContainerPort+"/"+protocol)
	}
	return strings.Join(values, ",")
}

func noVNCPortsFromLabels(labels map[string]string) []model.PortMapping {
	value := labels[model.LabelVPNNoVNCPorts]
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	ports := make([]model.PortMapping, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		host, containerPort, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		hostIP := ""
		if strings.Contains(containerPort, ":") {
			hostIP = host
			host, containerPort, _ = strings.Cut(containerPort, ":")
		}
		containerPort, protocol, _ := strings.Cut(containerPort, "/")
		if protocol == "" {
			protocol = "tcp"
		}
		ports = append(ports, model.PortMapping{
			HostIP:        hostIP,
			HostPort:      host,
			ContainerPort: containerPort,
			Protocol:      protocol,
		})
	}
	return ports
}

func routeModeFromLabels(labels map[string]string) string {
	if mode := labels[model.LabelVPNRouteMode]; mode != "" {
		return mode
	}
	return model.VPNRouteModeFull
}
