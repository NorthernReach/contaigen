package dockerx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/NorthernReach/contaigen/internal/model"
	progressx "github.com/NorthernReach/contaigen/internal/progress"
	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/api/types/mount"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

var (
	ErrEnvironmentNotFound = errors.New("environment not found")
	ErrEnvironmentExists   = errors.New("environment already exists")
)

const imagePullProgressInterval = time.Second

type Probe interface {
	Ping(context.Context) (PingInfo, error)
	ServerVersion(context.Context) (ServerVersion, error)
	Close() error
}

type Runtime interface {
	Probe
	EnsureImage(context.Context, string) error
	EnsureNetwork(context.Context, model.EnsureNetworkRequest) (model.Network, []string, error)
	ListNetworks(context.Context) ([]model.Network, error)
	InspectNetwork(context.Context, string) (model.Network, error)
	RemoveNetwork(context.Context, string) error
	NetworkMap(context.Context) (model.NetworkMap, error)
	CreateEnvironment(context.Context, model.CreateEnvironmentRequest) (model.Environment, []string, error)
	ListEnvironments(context.Context) ([]model.Environment, error)
	InspectEnvironment(context.Context, string) (model.Environment, error)
	StartEnvironment(context.Context, string) error
	StopEnvironment(context.Context, string, *int) error
	RemoveEnvironment(context.Context, string, model.RemoveEnvironmentRequest) error
	EnterEnvironment(context.Context, string, model.EnterEnvironmentRequest, model.ExecIO) error
	CreateService(context.Context, model.CreateServiceRequest) (model.Service, []string, error)
	ListServices(context.Context, string) ([]model.Service, error)
	InspectService(context.Context, string, string) (model.Service, error)
	StartService(context.Context, string, string) error
	StopService(context.Context, string, string, *int) error
	RemoveService(context.Context, string, string, model.RemoveServiceRequest) error
	CreateVPNGateway(context.Context, model.CreateVPNGatewayRequest) (model.VPNGateway, []string, error)
	ListVPNGateways(context.Context) ([]model.VPNGateway, error)
	InspectVPNGateway(context.Context, string) (model.VPNGateway, error)
	StartVPNGateway(context.Context, string) error
	StopVPNGateway(context.Context, string, *int) error
	RemoveVPNGateway(context.Context, string, model.RemoveVPNGatewayRequest) error
	VPNGatewayLogs(context.Context, string, model.VPNLogsRequest, model.VPNLogIO) error
}

type PingInfo struct {
	APIVersion string
	OSType     string
}

type ServerVersion struct {
	Version         string
	APIVersion      string
	MinAPIVersion   string
	OperatingSystem string
	Architecture    string
}

type Client struct {
	api *client.Client
}

func NewClient() (Runtime, error) {
	api, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("create Docker client: %w", err)
	}
	return &Client{api: api}, nil
}

func (c *Client) Ping(ctx context.Context) (PingInfo, error) {
	resp, err := c.api.Ping(ctx, client.PingOptions{})
	if err != nil {
		return PingInfo{}, err
	}
	return PingInfo{
		APIVersion: resp.APIVersion,
		OSType:     resp.OSType,
	}, nil
}

func (c *Client) ServerVersion(ctx context.Context) (ServerVersion, error) {
	resp, err := c.api.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return ServerVersion{}, err
	}
	return ServerVersion{
		Version:         resp.Version,
		APIVersion:      resp.APIVersion,
		MinAPIVersion:   resp.MinAPIVersion,
		OperatingSystem: resp.Os,
		Architecture:    resp.Arch,
	}, nil
}

func (c *Client) Close() error {
	return c.api.Close()
}

func (c *Client) EnsureImage(ctx context.Context, image string) error {
	progressx.Active(ctx, "Checking image "+image, "Docker local cache")
	if _, err := c.api.ImageInspect(ctx, image); err == nil {
		progressx.Active(ctx, "Checking image "+image, "already available locally")
		return nil
	} else if !errdefs.IsNotFound(err) {
		return err
	}

	progressx.Active(ctx, "Pulling image "+image, "downloading from registry")
	resp, err := c.api.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		return err
	}

	// Docker emits one JSON message per layer. Aggregate those messages so the
	// CLI can show one stable pull line instead of a stream of layer churn.
	pullProgress := newImagePullProgress(image)
	for message, err := range resp.JSONMessages(ctx) {
		if err != nil {
			return err
		}
		if message.Error != nil {
			return message.Error
		}
		pullProgress.Update(ctx, message)
	}
	pullProgress.Flush(ctx)
	return nil
}

type imagePullProgress struct {
	image      string
	status     string
	layers     map[string]imagePullLayer
	lastReport time.Time
	lastDetail string
	now        func() time.Time
}

type imagePullLayer struct {
	status  string
	current int64
	total   int64
}

func newImagePullProgress(image string) *imagePullProgress {
	return &imagePullProgress{
		image:  image,
		layers: map[string]imagePullLayer{},
		now:    time.Now,
	}
}

func (p *imagePullProgress) Update(ctx context.Context, message jsonstream.Message) {
	status := strings.TrimSpace(message.Status)
	id := strings.TrimSpace(message.ID)
	if status == "" && id == "" {
		return
	}
	if id == "" {
		p.status = status
		p.report(ctx, false)
		return
	}

	layer := p.layers[id]
	if status != "" {
		layer.status = status
	}
	if message.Progress != nil {
		layer.current = message.Progress.Current
		layer.total = message.Progress.Total
	}
	p.layers[id] = layer
	p.report(ctx, false)
}

func (p *imagePullProgress) Flush(ctx context.Context) {
	p.report(ctx, true)
}

func (p *imagePullProgress) report(ctx context.Context, force bool) {
	now := p.now()
	if !force && !p.lastReport.IsZero() && now.Sub(p.lastReport) < imagePullProgressInterval {
		return
	}
	detail, current, total := p.summary()
	if detail == "" {
		return
	}
	if !force && detail == p.lastDetail {
		return
	}
	p.lastReport = now
	p.lastDetail = detail
	progressx.Report(ctx, progressx.Event{
		State:   progressx.StateActive,
		Message: "Pulling image " + p.image,
		Detail:  detail,
		Current: current,
		Total:   total,
	})
}

func (p *imagePullProgress) summary() (string, int64, int64) {
	if len(p.layers) == 0 {
		return p.status, 0, 0
	}

	var downloading int
	var extracting int
	var waiting int
	var complete int
	var other int
	var current int64
	var total int64
	for _, layer := range p.layers {
		switch pullStatusKind(layer.status) {
		case "downloading":
			downloading++
		case "extracting":
			extracting++
		case "waiting":
			waiting++
		case "complete":
			complete++
		default:
			other++
		}
		if layer.total > 0 {
			total += layer.total
			// Docker stops sending byte progress after a layer completes. Count
			// complete layers as fully downloaded so the aggregate percentage does
			// not move backwards as statuses change.
			if pullStatusKind(layer.status) == "complete" {
				current += layer.total
				continue
			}
			current += clampProgress(layer.current, layer.total)
		}
	}

	parts := []string{}
	appendState := func(count int, label string) {
		if count > 1 {
			parts = append(parts, fmt.Sprintf("%d %s", count, label))
			return
		}
		if count == 1 {
			parts = append(parts, "1 "+label)
		}
	}
	appendState(downloading, "downloading")
	appendState(extracting, "extracting")
	appendState(waiting, "waiting")
	appendState(complete, "complete")
	if len(parts) == 0 {
		appendState(other, "active")
	}
	return strings.Join(parts, ", "), current, total
}

func pullStatusKind(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch {
	case strings.Contains(status, "download") && !strings.Contains(status, "complete"):
		return "downloading"
	case strings.Contains(status, "extract"):
		return "extracting"
	case strings.Contains(status, "waiting") || strings.Contains(status, "pulling fs layer"):
		return "waiting"
	case strings.Contains(status, "complete") || strings.Contains(status, "already exists") || strings.Contains(status, "up to date"):
		return "complete"
	default:
		return "other"
	}
}

func clampProgress(current int64, total int64) int64 {
	if current < 0 {
		return 0
	}
	if current > total {
		return total
	}
	return current
}

func (c *Client) CreateEnvironment(ctx context.Context, req model.CreateEnvironmentRequest) (model.Environment, []string, error) {
	if _, err := c.findEnvironment(ctx, req.Name); err == nil {
		return model.Environment{}, nil, fmt.Errorf("%w: %s", ErrEnvironmentExists, req.Name)
	} else if !errors.Is(err, ErrEnvironmentNotFound) {
		return model.Environment{}, nil, err
	}

	labels := model.EnvironmentLabels(req.Name, req.Shell)
	if req.WorkspaceName != "" {
		labels[model.LabelWorkspace] = req.WorkspaceName
	}
	if req.WorkspaceMountPath != "" {
		labels[model.LabelWorkspaceMount] = req.WorkspaceMountPath
	}
	if req.NetworkProfile != "" {
		labels[model.LabelNetworkProfile] = req.NetworkProfile
	}
	if req.NetworkName != "" {
		labels[model.LabelNetworkName] = req.NetworkName
	}
	if req.User != "" {
		labels[model.LabelUser] = req.User
	}
	if req.VPNName != "" {
		labels[model.LabelVPN] = req.VPNName
	}
	if req.Desktop.Enabled {
		labels[model.LabelDesktopEnabled] = strconv.FormatBool(req.Desktop.Enabled)
		labels[model.LabelDesktopProtocol] = req.Desktop.Protocol
		labels[model.LabelDesktopHostIP] = req.Desktop.HostIP
		labels[model.LabelDesktopHostPort] = req.Desktop.HostPort
		labels[model.LabelDesktopContainerPort] = req.Desktop.ContainerPort
		labels[model.LabelDesktopScheme] = req.Desktop.Scheme
		labels[model.LabelDesktopPath] = req.Desktop.Path
		labels[model.LabelDesktopUser] = req.Desktop.User
		labels[model.LabelDesktopPasswordEnv] = req.Desktop.PasswordEnv
	}
	portSet, portMap, err := dockerPorts(req.Ports)
	if err != nil {
		return model.Environment{}, nil, err
	}
	command := []string{"sleep", "infinity"}
	if req.UseImageCommand {
		command = nil
	}

	resp, err := c.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: containerName(req.Name),
		Config: &container.Config{
			Image:        req.Image,
			User:         req.User,
			Hostname:     req.Hostname,
			Env:          req.Env,
			WorkingDir:   req.WorkingDir,
			Cmd:          command,
			Tty:          true,
			OpenStdin:    true,
			StdinOnce:    false,
			ExposedPorts: portSet,
			Labels:       labels,
			Shell:        []string{req.Shell},
		},
		HostConfig: &container.HostConfig{
			NetworkMode:  container.NetworkMode(req.NetworkMode),
			CapAdd:       req.CapAdd,
			PortBindings: portMap,
			Mounts:       dockerMounts(req.Volumes),
			ShmSize:      req.ShmSize,
		},
	})
	if err != nil {
		return model.Environment{}, nil, err
	}

	env, err := c.InspectEnvironment(ctx, req.Name)
	if err != nil {
		return model.Environment{}, resp.Warnings, err
	}
	return env, resp.Warnings, nil
}

func (c *Client) ListEnvironments(ctx context.Context) ([]model.Environment, error) {
	filters := make(client.Filters).Add("label", model.LabelManaged+"=true")
	resp, err := c.api.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: filters,
	})
	if err != nil {
		return nil, err
	}

	envs := make([]model.Environment, 0, len(resp.Items))
	for _, item := range resp.Items {
		if item.Labels[model.LabelKind] != model.KindWorkbench {
			continue
		}
		envs = append(envs, environmentFromSummary(item))
	}
	return envs, nil
}

func (c *Client) InspectEnvironment(ctx context.Context, name string) (model.Environment, error) {
	env, err := c.findEnvironment(ctx, name)
	if err != nil {
		return model.Environment{}, err
	}

	resp, err := c.api.ContainerInspect(ctx, env.ID, client.ContainerInspectOptions{})
	if err != nil {
		return model.Environment{}, err
	}
	return environmentFromInspect(resp.Container), nil
}

func (c *Client) StartEnvironment(ctx context.Context, name string) error {
	env, err := c.findEnvironment(ctx, name)
	if err != nil {
		return err
	}
	_, err = c.api.ContainerStart(ctx, env.ID, client.ContainerStartOptions{})
	return err
}

func (c *Client) StopEnvironment(ctx context.Context, name string, timeout *int) error {
	env, err := c.findEnvironment(ctx, name)
	if err != nil {
		return err
	}
	_, err = c.api.ContainerStop(ctx, env.ID, client.ContainerStopOptions{Timeout: timeout})
	return err
}

func (c *Client) RemoveEnvironment(ctx context.Context, name string, req model.RemoveEnvironmentRequest) error {
	env, err := c.findEnvironment(ctx, name)
	if err != nil {
		return err
	}
	_, err = c.api.ContainerRemove(ctx, env.ID, client.ContainerRemoveOptions{
		Force:         req.Force,
		RemoveVolumes: req.RemoveVolumes,
	})
	return err
}

func (c *Client) EnterEnvironment(ctx context.Context, name string, req model.EnterEnvironmentRequest, ioStreams model.ExecIO) error {
	env, err := c.InspectEnvironment(ctx, name)
	if err != nil {
		return err
	}
	if env.State != "running" {
		return fmt.Errorf("environment %q is %s; start it before entering", name, env.State)
	}

	command := req.Command
	if len(command) == 0 {
		command = []string{env.Shell}
	}
	stdin := ioStreams.Stdin

	create, err := c.api.ExecCreate(ctx, env.ID, client.ExecCreateOptions{
		User:         req.User,
		TTY:          true,
		AttachStdin:  stdin != nil,
		AttachStdout: true,
		AttachStderr: true,
		WorkingDir:   req.WorkDir,
		Cmd:          command,
	})
	if err != nil {
		return err
	}

	attach, err := c.api.ExecAttach(ctx, create.ID, client.ExecAttachOptions{TTY: true})
	if err != nil {
		return err
	}
	defer attach.Close()

	stdout := ioStreams.Stdout
	if stdout == nil {
		stdout = io.Discard
	}

	if stdin != nil {
		go func() {
			_, _ = io.Copy(attach.Conn, stdin)
			_ = attach.CloseWrite()
		}()
	}

	if _, err := io.Copy(stdout, attach.Reader); err != nil {
		return err
	}

	inspect, err := c.api.ExecInspect(ctx, create.ID, client.ExecInspectOptions{})
	if err != nil {
		return err
	}
	if inspect.ExitCode != 0 {
		return fmt.Errorf("exec exited with status %d", inspect.ExitCode)
	}
	return nil
}

func (c *Client) findEnvironment(ctx context.Context, name string) (model.Environment, error) {
	envs, err := c.ListEnvironments(ctx)
	if err != nil {
		return model.Environment{}, err
	}

	var match *model.Environment
	for i := range envs {
		if envs[i].Name != name {
			continue
		}
		if match != nil {
			return model.Environment{}, fmt.Errorf("multiple Contaigen environments named %q", name)
		}
		match = &envs[i]
	}
	if match == nil {
		return model.Environment{}, fmt.Errorf("%w: %s", ErrEnvironmentNotFound, name)
	}
	return *match, nil
}

func dockerPorts(ports []model.PortMapping) (dockernetwork.PortSet, dockernetwork.PortMap, error) {
	if len(ports) == 0 {
		return nil, nil, nil
	}

	portSet := dockernetwork.PortSet{}
	portMap := dockernetwork.PortMap{}

	for _, mapping := range ports {
		protocol := mapping.Protocol
		if protocol == "" {
			protocol = string(dockernetwork.TCP)
		}

		port, err := dockernetwork.ParsePort(mapping.ContainerPort + "/" + protocol)
		if err != nil {
			return nil, nil, fmt.Errorf("parse container port %q: %w", mapping.ContainerPort, err)
		}

		var hostIP netip.Addr
		if mapping.HostIP != "" {
			hostIP, err = netip.ParseAddr(mapping.HostIP)
			if err != nil {
				return nil, nil, fmt.Errorf("parse host IP %q: %w", mapping.HostIP, err)
			}
		}

		portSet[port] = struct{}{}
		portMap[port] = append(portMap[port], dockernetwork.PortBinding{
			HostIP:   hostIP,
			HostPort: mapping.HostPort,
		})
	}

	return portSet, portMap, nil
}

func dockerMounts(volumes []model.VolumeMount) []mount.Mount {
	if len(volumes) == 0 {
		return nil
	}

	mounts := make([]mount.Mount, 0, len(volumes))
	for _, volume := range volumes {
		mounts = append(mounts, mount.Mount{
			Type:     mount.TypeBind,
			Source:   volume.Source,
			Target:   volume.Target,
			ReadOnly: volume.ReadOnly,
		})
	}
	return mounts
}

func environmentFromSummary(summary container.Summary) model.Environment {
	labels := cloneLabels(summary.Labels)
	ports := portsFromSummary(summary.Ports)
	return model.Environment{
		ID:                 summary.ID,
		Name:               labels[model.LabelEnv],
		ContainerName:      cleanContainerName(summary.Names),
		Image:              summary.Image,
		State:              string(summary.State),
		Status:             summary.Status,
		Shell:              shellFromLabels(labels),
		User:               labels[model.LabelUser],
		NetworkProfile:     labels[model.LabelNetworkProfile],
		NetworkName:        networkNameFromSummary(summary),
		NetworkMode:        summary.HostConfig.NetworkMode,
		VPNName:            labels[model.LabelVPN],
		WorkspaceName:      labels[model.LabelWorkspace],
		WorkspacePath:      workspacePathFromVolumes(labels[model.LabelWorkspaceMount], summary.Mounts),
		WorkspaceMountPath: labels[model.LabelWorkspaceMount],
		Desktop:            desktopFromLabels(labels, nil, ports),
		CreatedAt:          time.Unix(summary.Created, 0),
		Ports:              ports,
		Volumes:            volumesFromMountPoints(summary.Mounts),
		Labels:             labels,
	}
}

func environmentFromInspect(inspect container.InspectResponse) model.Environment {
	labels := map[string]string{}
	image := inspect.Image
	hostname := ""
	if inspect.Config != nil {
		labels = cloneLabels(inspect.Config.Labels)
		image = inspect.Config.Image
		hostname = inspect.Config.Hostname
	}
	user := labels[model.LabelUser]
	if inspect.Config != nil && inspect.Config.User != "" {
		user = inspect.Config.User
	}
	env := []string(nil)
	if inspect.Config != nil {
		env = inspect.Config.Env
	}

	state := ""
	status := ""
	if inspect.State != nil {
		state = string(inspect.State.Status)
		status = string(inspect.State.Status)
	}

	networkMode := ""
	ports := []model.PortMapping(nil)
	if inspect.HostConfig != nil {
		networkMode = string(inspect.HostConfig.NetworkMode)
		ports = portsFromPortMap(inspect.HostConfig.PortBindings)
	}
	capAdd := []string(nil)
	if inspect.HostConfig != nil {
		capAdd = append([]string(nil), inspect.HostConfig.CapAdd...)
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, inspect.Created)

	return model.Environment{
		ID:                 inspect.ID,
		Name:               labels[model.LabelEnv],
		ContainerName:      strings.TrimPrefix(inspect.Name, "/"),
		Image:              image,
		State:              state,
		Status:             status,
		Shell:              shellFromLabels(labels),
		User:               user,
		Hostname:           hostname,
		NetworkProfile:     labels[model.LabelNetworkProfile],
		NetworkName:        networkNameFromInspect(labels, inspect),
		NetworkMode:        networkMode,
		VPNName:            labels[model.LabelVPN],
		WorkspaceName:      labels[model.LabelWorkspace],
		WorkspacePath:      workspacePathFromVolumes(labels[model.LabelWorkspaceMount], inspect.Mounts),
		WorkspaceMountPath: labels[model.LabelWorkspaceMount],
		Desktop:            desktopFromLabels(labels, env, ports),
		CreatedAt:          createdAt,
		Ports:              ports,
		Volumes:            volumesFromMountPoints(inspect.Mounts),
		CapAdd:             capAdd,
		Labels:             labels,
	}
}

func portsFromSummary(ports []container.PortSummary) []model.PortMapping {
	if len(ports) == 0 {
		return nil
	}

	out := make([]model.PortMapping, 0, len(ports))
	for _, port := range ports {
		mapping := model.PortMapping{
			ContainerPort: fmt.Sprint(port.PrivatePort),
			Protocol:      port.Type,
		}
		if port.PublicPort != 0 {
			mapping.HostPort = fmt.Sprint(port.PublicPort)
		}
		if port.IP.IsValid() {
			mapping.HostIP = port.IP.String()
		}
		out = append(out, mapping)
	}
	return out
}

func portsFromPortMap(ports dockernetwork.PortMap) []model.PortMapping {
	if len(ports) == 0 {
		return nil
	}

	out := []model.PortMapping{}
	for port, bindings := range ports {
		if len(bindings) == 0 {
			out = append(out, model.PortMapping{
				ContainerPort: port.Port(),
				Protocol:      string(port.Proto()),
			})
			continue
		}
		for _, binding := range bindings {
			mapping := model.PortMapping{
				HostPort:      binding.HostPort,
				ContainerPort: port.Port(),
				Protocol:      string(port.Proto()),
			}
			if binding.HostIP.IsValid() {
				mapping.HostIP = binding.HostIP.String()
			}
			out = append(out, mapping)
		}
	}
	return out
}

func desktopFromLabels(labels map[string]string, env []string, ports []model.PortMapping) model.DesktopConfig {
	if labels[model.LabelDesktopEnabled] != "true" {
		return model.DesktopConfig{}
	}
	desktop := model.DesktopConfig{
		Enabled:       true,
		Protocol:      valueOrDefault(labels[model.LabelDesktopProtocol], model.DefaultDesktopProtocol),
		HostIP:        valueOrDefault(labels[model.LabelDesktopHostIP], model.DefaultDesktopHostIP),
		HostPort:      valueOrDefault(labels[model.LabelDesktopHostPort], model.DefaultDesktopPort),
		ContainerPort: valueOrDefault(labels[model.LabelDesktopContainerPort], model.DefaultDesktopPort),
		Scheme:        valueOrDefault(labels[model.LabelDesktopScheme], model.DefaultDesktopScheme),
		Path:          valueOrDefault(labels[model.LabelDesktopPath], model.DefaultDesktopPath),
		User:          valueOrDefault(labels[model.LabelDesktopUser], model.DefaultDesktopUser),
		PasswordEnv:   valueOrDefault(labels[model.LabelDesktopPasswordEnv], model.DefaultDesktopPasswordEnv),
	}
	for _, port := range ports {
		if port.ContainerPort == desktop.ContainerPort {
			if port.HostIP != "" {
				desktop.HostIP = port.HostIP
			}
			if port.HostPort != "" {
				desktop.HostPort = port.HostPort
			}
			break
		}
	}
	if password, ok := envValue(env, desktop.PasswordEnv); ok {
		desktop.Password = password
	}
	return desktop
}

func valueOrDefault(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func envValue(values []string, key string) (string, bool) {
	prefix := key + "="
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix), true
		}
	}
	return "", false
}

func volumesFromMountPoints(mounts []container.MountPoint) []model.VolumeMount {
	if len(mounts) == 0 {
		return nil
	}

	out := make([]model.VolumeMount, 0, len(mounts))
	for _, point := range mounts {
		out = append(out, model.VolumeMount{
			Source:   point.Source,
			Target:   point.Destination,
			ReadOnly: !point.RW,
		})
	}
	return out
}

func workspacePathFromVolumes(mountPath string, mounts []container.MountPoint) string {
	if mountPath == "" {
		mountPath = model.DefaultWorkspaceMountPath
	}
	for _, point := range mounts {
		if point.Destination == mountPath {
			return point.Source
		}
	}
	return ""
}

func networkNameFromSummary(summary container.Summary) string {
	if summary.Labels != nil && summary.Labels[model.LabelNetworkName] != "" {
		return summary.Labels[model.LabelNetworkName]
	}
	if summary.NetworkSettings == nil {
		return ""
	}
	for name := range summary.NetworkSettings.Networks {
		return name
	}
	return ""
}

func networkNameFromInspect(labels map[string]string, inspect container.InspectResponse) string {
	if labels[model.LabelNetworkName] != "" {
		return labels[model.LabelNetworkName]
	}
	if inspect.NetworkSettings == nil {
		return ""
	}
	for name := range inspect.NetworkSettings.Networks {
		return name
	}
	return ""
}

func cloneLabels(labels map[string]string) map[string]string {
	if labels == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		out[key] = value
	}
	return out
}

func cleanContainerName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.TrimPrefix(names[0], "/")
}

func shellFromLabels(labels map[string]string) string {
	if shell := labels[model.LabelShell]; shell != "" {
		return shell
	}
	return model.DefaultEnvironmentShell
}

func containerName(name string) string {
	return "contaigen-" + name
}
