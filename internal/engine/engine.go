package engine

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/NorthernReach/contaigen/internal/model"
	progressx "github.com/NorthernReach/contaigen/internal/progress"
	"github.com/NorthernReach/contaigen/internal/vpnconfig"
)

var environmentNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

type Runtime interface {
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
	Close() error
}

type Workspaces interface {
	Ensure(context.Context, model.EnsureWorkspaceRequest) (model.Workspace, error)
	List(context.Context) ([]model.Workspace, error)
	Backup(context.Context, model.BackupWorkspaceRequest) (model.WorkspaceBackup, error)
	Remove(context.Context, model.RemoveWorkspaceRequest) (model.WorkspaceRemove, error)
}

type Option func(*Engine)

type Engine struct {
	runtime    Runtime
	workspaces Workspaces
}

func New(runtime Runtime, opts ...Option) *Engine {
	engine := &Engine{runtime: runtime}
	for _, opt := range opts {
		opt(engine)
	}
	return engine
}

func WithWorkspaces(workspaces Workspaces) Option {
	return func(engine *Engine) {
		engine.workspaces = workspaces
	}
}

func (e *Engine) CreateEnvironment(ctx context.Context, req model.CreateEnvironmentRequest) (model.Environment, []string, error) {
	req = normalizeCreateRequest(req)
	progressx.Active(ctx, "Preparing environment "+req.Name, "validating requested options")
	if err := validateCreateRequest(req); err != nil {
		return model.Environment{}, nil, err
	}
	desktopWarnings, err := applyDesktop(&req)
	if err != nil {
		return model.Environment{}, nil, err
	}
	warnings := desktopWarnings
	progressx.Done(ctx, "Prepared environment "+req.Name, "image "+req.Image)
	networkWarnings, err := e.applyNetworkProfile(ctx, &req)
	if err != nil {
		return model.Environment{}, warnings, err
	}
	warnings = append(warnings, networkWarnings...)
	if err := e.attachWorkspace(ctx, &req); err != nil {
		return model.Environment{}, nil, err
	}

	if req.Pull {
		progressx.Info(ctx, "Docker image pulls are non-interactive", "registry or authentication errors will be shown here")
		progressx.Active(ctx, "Ensuring image "+req.Image, "local image check or registry pull")
		if err := e.runtime.EnsureImage(ctx, req.Image); err != nil {
			return model.Environment{}, nil, fmt.Errorf("ensure image %q: %w", req.Image, err)
		}
		progressx.Done(ctx, "Image ready "+req.Image, "")
	} else {
		progressx.Info(ctx, "Skipping image pull", req.Image)
	}

	progressx.Active(ctx, "Creating environment container "+req.Name, "")
	env, dockerWarnings, err := e.runtime.CreateEnvironment(ctx, req)
	if err != nil {
		return model.Environment{}, warnings, err
	}
	progressx.Done(ctx, "Created environment container "+req.Name, shortID(env.ID))
	warnings = append(warnings, dockerWarnings...)

	if req.Start {
		progressx.Active(ctx, "Starting environment "+req.Name, "")
		if err := e.runtime.StartEnvironment(ctx, req.Name); err != nil {
			return env, warnings, err
		}
		progressx.Active(ctx, "Inspecting environment "+req.Name, "waiting for Docker state")
		env, err = e.runtime.InspectEnvironment(ctx, req.Name)
		if err != nil {
			return model.Environment{}, warnings, err
		}
		progressx.Done(ctx, "Started environment "+req.Name, valueOrUnknown(env.State))
	}

	return env, warnings, nil
}

func (e *Engine) ListEnvironments(ctx context.Context) ([]model.Environment, error) {
	return e.runtime.ListEnvironments(ctx)
}

func (e *Engine) InspectEnvironment(ctx context.Context, name string) (model.Environment, error) {
	if err := validateEnvironmentName(name); err != nil {
		return model.Environment{}, err
	}
	return e.runtime.InspectEnvironment(ctx, name)
}

func (e *Engine) StartEnvironment(ctx context.Context, name string) error {
	if err := validateEnvironmentName(name); err != nil {
		return err
	}
	progressx.Active(ctx, "Starting environment "+name, "")
	if err := e.runtime.StartEnvironment(ctx, name); err != nil {
		return err
	}
	progressx.Done(ctx, "Started environment "+name, "")
	return nil
}

func (e *Engine) StopEnvironment(ctx context.Context, name string, timeout *int) error {
	if err := validateEnvironmentName(name); err != nil {
		return err
	}
	progressx.Active(ctx, "Stopping environment "+name, "")
	if err := e.runtime.StopEnvironment(ctx, name, timeout); err != nil {
		return err
	}
	progressx.Done(ctx, "Stopped environment "+name, "")
	return nil
}

func (e *Engine) RemoveEnvironment(ctx context.Context, name string, req model.RemoveEnvironmentRequest) error {
	if err := validateEnvironmentName(name); err != nil {
		return err
	}
	progressx.Active(ctx, "Removing environment "+name, "")
	if err := e.runtime.RemoveEnvironment(ctx, name, req); err != nil {
		return err
	}
	progressx.Done(ctx, "Removed environment "+name, "")
	return nil
}

func (e *Engine) EnterEnvironment(ctx context.Context, name string, req model.EnterEnvironmentRequest, io model.ExecIO) error {
	if err := validateEnvironmentName(name); err != nil {
		return err
	}
	return e.runtime.EnterEnvironment(ctx, name, req, io)
}

func (e *Engine) CreateService(ctx context.Context, req model.CreateServiceRequest) (model.Service, []string, error) {
	req = normalizeCreateServiceRequest(req)
	progressx.Active(ctx, "Preparing service "+req.Name, "validating requested options")
	if err := validateCreateServiceRequest(req); err != nil {
		return model.Service{}, nil, err
	}
	progressx.Done(ctx, "Prepared service "+req.Name, "image "+req.Image)

	progressx.Active(ctx, "Inspecting environment "+req.EnvironmentName, "selecting service network")
	env, err := e.InspectEnvironment(ctx, req.EnvironmentName)
	if err != nil {
		return model.Service{}, nil, err
	}
	networkName, warnings, err := serviceNetworkForEnvironment(env)
	if err != nil {
		return model.Service{}, warnings, err
	}
	req.NetworkName = networkName
	progressx.Done(ctx, "Selected service network "+networkName, "")

	if req.Pull {
		progressx.Info(ctx, "Docker image pulls are non-interactive", "registry or authentication errors will be shown here")
		progressx.Active(ctx, "Ensuring image "+req.Image, "local image check or registry pull")
		if err := e.runtime.EnsureImage(ctx, req.Image); err != nil {
			return model.Service{}, nil, fmt.Errorf("ensure image %q: %w", req.Image, err)
		}
		progressx.Done(ctx, "Image ready "+req.Image, "")
	} else {
		progressx.Info(ctx, "Skipping image pull", req.Image)
	}

	progressx.Active(ctx, "Creating service container "+req.Name, "")
	service, dockerWarnings, err := e.runtime.CreateService(ctx, req)
	if err != nil {
		return model.Service{}, warnings, err
	}
	progressx.Done(ctx, "Created service container "+req.Name, shortID(service.ID))
	warnings = append(warnings, dockerWarnings...)

	if req.Start {
		progressx.Active(ctx, "Starting service "+req.Name, "")
		if err := e.runtime.StartService(ctx, req.EnvironmentName, req.Name); err != nil {
			return service, warnings, err
		}
		progressx.Active(ctx, "Inspecting service "+req.Name, "waiting for Docker state")
		service, err = e.runtime.InspectService(ctx, req.EnvironmentName, req.Name)
		if err != nil {
			return model.Service{}, warnings, err
		}
		progressx.Done(ctx, "Started service "+req.Name, valueOrUnknown(service.State))
	}

	return service, warnings, nil
}

func (e *Engine) ListServices(ctx context.Context, envName string) ([]model.Service, error) {
	envName = strings.TrimSpace(envName)
	if envName != "" {
		if err := validateEnvironmentName(envName); err != nil {
			return nil, err
		}
	}
	return e.runtime.ListServices(ctx, envName)
}

func (e *Engine) InspectService(ctx context.Context, envName string, serviceName string) (model.Service, error) {
	if err := validateEnvironmentName(envName); err != nil {
		return model.Service{}, err
	}
	if err := validateServiceName(serviceName); err != nil {
		return model.Service{}, err
	}
	return e.runtime.InspectService(ctx, envName, serviceName)
}

func (e *Engine) StartService(ctx context.Context, envName string, serviceName string) error {
	if err := validateEnvironmentName(envName); err != nil {
		return err
	}
	if err := validateServiceName(serviceName); err != nil {
		return err
	}
	progressx.Active(ctx, "Starting service "+serviceName, "environment "+envName)
	if err := e.runtime.StartService(ctx, envName, serviceName); err != nil {
		return err
	}
	progressx.Done(ctx, "Started service "+serviceName, "environment "+envName)
	return nil
}

func (e *Engine) StopService(ctx context.Context, envName string, serviceName string, timeout *int) error {
	if err := validateEnvironmentName(envName); err != nil {
		return err
	}
	if err := validateServiceName(serviceName); err != nil {
		return err
	}
	progressx.Active(ctx, "Stopping service "+serviceName, "environment "+envName)
	if err := e.runtime.StopService(ctx, envName, serviceName, timeout); err != nil {
		return err
	}
	progressx.Done(ctx, "Stopped service "+serviceName, "environment "+envName)
	return nil
}

func (e *Engine) RemoveService(ctx context.Context, envName string, serviceName string, req model.RemoveServiceRequest) error {
	if err := validateEnvironmentName(envName); err != nil {
		return err
	}
	if err := validateServiceName(serviceName); err != nil {
		return err
	}
	progressx.Active(ctx, "Removing service "+serviceName, "environment "+envName)
	if err := e.runtime.RemoveService(ctx, envName, serviceName, req); err != nil {
		return err
	}
	progressx.Done(ctx, "Removed service "+serviceName, "environment "+envName)
	return nil
}

func (e *Engine) CreateVPNGateway(ctx context.Context, req model.CreateVPNGatewayRequest) (model.VPNGateway, []string, error) {
	req = normalizeCreateVPNGatewayRequest(req)
	progressx.Active(ctx, "Preparing VPN gateway "+req.Name, "validating requested options")
	routeWarnings, err := applyVPNRouteMode(&req)
	if err != nil {
		return model.VPNGateway{}, routeWarnings, err
	}
	warnings, err := validateCreateVPNGatewayRequest(req)
	if err != nil {
		return model.VPNGateway{}, append(routeWarnings, warnings...), err
	}
	warnings = append(routeWarnings, warnings...)
	progressx.Done(ctx, "Prepared VPN gateway "+req.Name, "image "+req.Image)

	if req.Pull {
		progressx.Info(ctx, "Docker image pulls are non-interactive", "registry or authentication errors will be shown here")
		progressx.Active(ctx, "Ensuring image "+req.Image, "local image check or registry pull")
		if err := e.runtime.EnsureImage(ctx, req.Image); err != nil {
			return model.VPNGateway{}, warnings, fmt.Errorf("ensure image %q: %w", req.Image, err)
		}
		progressx.Done(ctx, "Image ready "+req.Image, "")
	} else {
		progressx.Info(ctx, "Skipping image pull", req.Image)
	}

	progressx.Active(ctx, "Creating VPN gateway container "+req.Name, "")
	vpn, dockerWarnings, err := e.runtime.CreateVPNGateway(ctx, req)
	if err != nil {
		return model.VPNGateway{}, warnings, err
	}
	progressx.Done(ctx, "Created VPN gateway container "+req.Name, shortID(vpn.ID))
	warnings = append(warnings, dockerWarnings...)

	if req.Start {
		progressx.Active(ctx, "Starting VPN gateway "+req.Name, "")
		if err := e.runtime.StartVPNGateway(ctx, req.Name); err != nil {
			return vpn, warnings, err
		}
		progressx.Active(ctx, "Inspecting VPN gateway "+req.Name, "waiting for Docker state")
		vpn, err = e.runtime.InspectVPNGateway(ctx, req.Name)
		if err != nil {
			return model.VPNGateway{}, warnings, err
		}
		progressx.Done(ctx, "Started VPN gateway "+req.Name, valueOrUnknown(vpn.State))
	}
	return vpn, warnings, nil
}

func (e *Engine) ListVPNGateways(ctx context.Context) ([]model.VPNGateway, error) {
	return e.runtime.ListVPNGateways(ctx)
}

func (e *Engine) InspectVPNGateway(ctx context.Context, name string) (model.VPNGateway, error) {
	if err := validateVPNName(name); err != nil {
		return model.VPNGateway{}, err
	}
	return e.runtime.InspectVPNGateway(ctx, name)
}

func (e *Engine) StartVPNGateway(ctx context.Context, name string) error {
	if err := validateVPNName(name); err != nil {
		return err
	}
	progressx.Active(ctx, "Starting VPN gateway "+name, "")
	if err := e.runtime.StartVPNGateway(ctx, name); err != nil {
		return err
	}
	progressx.Done(ctx, "Started VPN gateway "+name, "")
	return nil
}

func (e *Engine) StopVPNGateway(ctx context.Context, name string, timeout *int) error {
	if err := validateVPNName(name); err != nil {
		return err
	}
	progressx.Active(ctx, "Stopping VPN gateway "+name, "")
	if err := e.runtime.StopVPNGateway(ctx, name, timeout); err != nil {
		return err
	}
	progressx.Done(ctx, "Stopped VPN gateway "+name, "")
	return nil
}

func (e *Engine) RemoveVPNGateway(ctx context.Context, name string, req model.RemoveVPNGatewayRequest) error {
	if err := validateVPNName(name); err != nil {
		return err
	}
	progressx.Active(ctx, "Removing VPN gateway "+name, "")
	if err := e.runtime.RemoveVPNGateway(ctx, name, req); err != nil {
		return err
	}
	progressx.Done(ctx, "Removed VPN gateway "+name, "")
	return nil
}

func (e *Engine) VPNGatewayLogs(ctx context.Context, name string, req model.VPNLogsRequest, io model.VPNLogIO) error {
	if err := validateVPNName(name); err != nil {
		return err
	}
	return e.runtime.VPNGatewayLogs(ctx, name, req, io)
}

func (e *Engine) EnsureNetwork(ctx context.Context, req model.EnsureNetworkRequest) (model.Network, []string, error) {
	req.Name = strings.TrimSpace(req.Name)
	req.Profile = strings.TrimSpace(req.Profile)
	req.Driver = strings.TrimSpace(req.Driver)

	if req.Name == "" {
		return model.Network{}, nil, fmt.Errorf("network name is required")
	}
	if req.Profile == "" {
		req.Profile = model.NetworkProfileSegment
	}
	if req.Driver == "" {
		req.Driver = model.DefaultNetworkDriver
	}
	if err := validateNetworkProfile(req.Profile); err != nil {
		return model.Network{}, nil, err
	}
	progressx.Active(ctx, "Preparing network "+req.Name, fmt.Sprintf("driver %s", req.Driver))
	network, warnings, err := e.runtime.EnsureNetwork(ctx, req)
	if err != nil {
		return model.Network{}, nil, err
	}
	progressx.Done(ctx, "Network ready "+network.Name, shortID(network.ID))
	return network, warnings, nil
}

func (e *Engine) ListNetworks(ctx context.Context) ([]model.Network, error) {
	return e.runtime.ListNetworks(ctx)
}

func (e *Engine) InspectNetwork(ctx context.Context, name string) (model.Network, error) {
	if strings.TrimSpace(name) == "" {
		return model.Network{}, fmt.Errorf("network name is required")
	}
	return e.runtime.InspectNetwork(ctx, name)
}

func (e *Engine) NetworkMap(ctx context.Context) (model.NetworkMap, error) {
	return e.runtime.NetworkMap(ctx)
}

func (e *Engine) NukePlan(ctx context.Context) (model.NukePlan, error) {
	services, err := e.runtime.ListServices(ctx, "")
	if err != nil {
		return model.NukePlan{}, fmt.Errorf("list services: %w", err)
	}
	envs, err := e.runtime.ListEnvironments(ctx)
	if err != nil {
		return model.NukePlan{}, fmt.Errorf("list environments: %w", err)
	}
	vpns, err := e.runtime.ListVPNGateways(ctx)
	if err != nil {
		return model.NukePlan{}, fmt.Errorf("list VPN gateways: %w", err)
	}
	networks, err := e.runtime.ListNetworks(ctx)
	if err != nil {
		return model.NukePlan{}, fmt.Errorf("list networks: %w", err)
	}

	workspaces := []model.Workspace(nil)
	if e.workspaces != nil {
		workspaces, err = e.workspaces.List(ctx)
		if err != nil {
			return model.NukePlan{}, fmt.Errorf("list workspaces: %w", err)
		}
		workspaces = appendWorkspaceRefs(workspaces, envs)
	}

	return model.NukePlan{
		Environments: envs,
		Services:     services,
		VPNGateways:  vpns,
		Networks:     networks,
		Workspaces:   workspaces,
	}, nil
}

func (e *Engine) Nuke(ctx context.Context, req model.NukeRequest) (model.NukeResult, error) {
	progressx.Active(ctx, "Planning Contaigen nuke", "listing managed resources")
	plan, err := e.NukePlan(ctx)
	if err != nil {
		return model.NukeResult{}, err
	}
	progressx.Done(ctx, "Planned Contaigen nuke", fmt.Sprintf("%d container(s), %d network(s), %d workspace(s)", len(plan.Services)+len(plan.Environments)+len(plan.VPNGateways), len(plan.Networks), len(plan.Workspaces)))

	result := model.NukeResult{Plan: plan}
	backupFailed := map[string]bool{}
	timeout := &req.StopTimeout
	if req.StopTimeout < 0 {
		timeout = nil
	}

	if req.BackupWorkspaces && e.workspaces != nil {
		for _, ws := range plan.Workspaces {
			progressx.Active(ctx, "Backing up workspace "+ws.Name, ws.Path)
			backup, err := e.workspaces.Backup(ctx, model.BackupWorkspaceRequest{
				Name:     ws.Name,
				Path:     ws.Path,
				Password: req.BackupPassword,
			})
			if err != nil {
				backupFailed[workspaceKey(ws)] = true
				result.Errors = append(result.Errors, nukeError("workspace", ws.Name, "backup", err))
				continue
			}
			progressx.Done(ctx, "Backed up workspace "+ws.Name, backup.Path)
			result.WorkspaceBackups = append(result.WorkspaceBackups, backup)
		}
	}

	for _, service := range plan.Services {
		if isRunningState(service.State) {
			progressx.Active(ctx, "Stopping service "+service.Name, "environment "+service.EnvironmentName)
			if err := e.runtime.StopService(ctx, service.EnvironmentName, service.Name, timeout); err != nil {
				result.Errors = append(result.Errors, nukeError("service", service.EnvironmentName+"/"+service.Name, "stop", err))
			}
		}
		progressx.Active(ctx, "Removing service "+service.Name, "environment "+service.EnvironmentName)
		if err := e.runtime.RemoveService(ctx, service.EnvironmentName, service.Name, model.RemoveServiceRequest{
			Force:         true,
			RemoveVolumes: true,
		}); err != nil {
			result.Errors = append(result.Errors, nukeError("service", service.EnvironmentName+"/"+service.Name, "remove", err))
			continue
		}
		progressx.Done(ctx, "Removed service "+service.Name, "environment "+service.EnvironmentName)
	}

	for _, env := range plan.Environments {
		if isRunningState(env.State) {
			progressx.Active(ctx, "Stopping environment "+env.Name, "")
			if err := e.runtime.StopEnvironment(ctx, env.Name, timeout); err != nil {
				result.Errors = append(result.Errors, nukeError("environment", env.Name, "stop", err))
			}
		}
		progressx.Active(ctx, "Removing environment "+env.Name, "")
		if err := e.runtime.RemoveEnvironment(ctx, env.Name, model.RemoveEnvironmentRequest{
			Force:         true,
			RemoveVolumes: true,
		}); err != nil {
			result.Errors = append(result.Errors, nukeError("environment", env.Name, "remove", err))
			continue
		}
		progressx.Done(ctx, "Removed environment "+env.Name, "")
	}

	for _, vpn := range plan.VPNGateways {
		if isRunningState(vpn.State) {
			progressx.Active(ctx, "Stopping VPN gateway "+vpn.Name, "")
			if err := e.runtime.StopVPNGateway(ctx, vpn.Name, timeout); err != nil {
				result.Errors = append(result.Errors, nukeError("vpn", vpn.Name, "stop", err))
			}
		}
		progressx.Active(ctx, "Removing VPN gateway "+vpn.Name, "")
		if err := e.runtime.RemoveVPNGateway(ctx, vpn.Name, model.RemoveVPNGatewayRequest{
			Force:         true,
			RemoveVolumes: true,
		}); err != nil {
			result.Errors = append(result.Errors, nukeError("vpn", vpn.Name, "remove", err))
			continue
		}
		progressx.Done(ctx, "Removed VPN gateway "+vpn.Name, "")
	}

	for _, network := range plan.Networks {
		progressx.Active(ctx, "Removing network "+network.Name, "")
		if err := e.runtime.RemoveNetwork(ctx, network.Name); err != nil {
			result.Errors = append(result.Errors, nukeError("network", network.Name, "remove", err))
			continue
		}
		progressx.Done(ctx, "Removed network "+network.Name, "")
	}

	if e.workspaces != nil {
		for _, ws := range plan.Workspaces {
			// If the user requested backups, never delete a workspace whose
			// backup failed. Containers and networks can still be removed so the
			// Docker side is reset while data is preserved for inspection.
			if backupFailed[workspaceKey(ws)] {
				continue
			}
			progressx.Active(ctx, "Removing workspace "+ws.Name, ws.Path)
			removed, err := e.workspaces.Remove(ctx, model.RemoveWorkspaceRequest{
				Name: ws.Name,
				Path: ws.Path,
			})
			if err != nil {
				result.Errors = append(result.Errors, nukeError("workspace", ws.Name, "remove", err))
				continue
			}
			progressx.Done(ctx, "Removed workspace "+ws.Name, removed.Workspace.Path)
			result.RemovedWorkspaces = append(result.RemovedWorkspaces, removed)
		}
	}

	progressx.Done(ctx, "Contaigen resources removed", fmt.Sprintf("%d error(s)", len(result.Errors)))
	return result, nil
}

func nukeError(resourceType string, name string, action string, err error) model.NukeError {
	return model.NukeError{
		ResourceType: resourceType,
		Name:         name,
		Action:       action,
		Message:      err.Error(),
	}
}

func isRunningState(state string) bool {
	return strings.EqualFold(strings.TrimSpace(state), "running")
}

func appendWorkspaceRefs(workspaces []model.Workspace, envs []model.Environment) []model.Workspace {
	seen := make(map[string]bool, len(workspaces))
	for _, ws := range workspaces {
		seen[workspaceKey(ws)] = true
	}
	// Custom workspace paths may only be known from container labels. Include
	// them in nuke plans so a clean-slate reset covers env-created workspaces
	// even when they live outside Contaigen's default workspace root.
	for _, env := range envs {
		if env.WorkspaceName == "" || env.WorkspacePath == "" {
			continue
		}
		ws := model.Workspace{Name: env.WorkspaceName, Path: env.WorkspacePath}
		key := workspaceKey(ws)
		if seen[key] {
			continue
		}
		workspaces = append(workspaces, ws)
		seen[key] = true
	}
	return workspaces
}

func workspaceKey(ws model.Workspace) string {
	return ws.Name + "\x00" + ws.Path
}

func (e *Engine) applyNetworkProfile(ctx context.Context, req *model.CreateEnvironmentRequest) ([]string, error) {
	warnings := []string(nil)

	switch req.NetworkProfile {
	case model.NetworkProfileBridge:
		progressx.Active(ctx, "Configuring network for "+req.Name, "Docker bridge")
		req.NetworkMode = model.NetworkProfileBridge
		progressx.Done(ctx, "Network configured for "+req.Name, model.NetworkProfileBridge)
	case model.NetworkProfileIsolated:
		progressx.Active(ctx, "Configuring network for "+req.Name, "isolated")
		req.NetworkMode = "none"
		progressx.Done(ctx, "Network configured for "+req.Name, "isolated")
	case model.NetworkProfileHost:
		progressx.Active(ctx, "Configuring network for "+req.Name, "host")
		req.NetworkMode = model.NetworkProfileHost
		warnings = append(warnings, "host networking can behave differently on Docker Desktop for macOS and Windows")
		progressx.Done(ctx, "Network configured for "+req.Name, model.NetworkProfileHost)
	case model.NetworkProfileSegment:
		if req.NetworkName == "" {
			req.NetworkName = "contaigen-" + req.Name
		}
		network, networkWarnings, err := e.EnsureNetwork(ctx, model.EnsureNetworkRequest{
			Name:       req.NetworkName,
			Profile:    model.NetworkProfileSegment,
			Driver:     model.DefaultNetworkDriver,
			Attachable: true,
		})
		if err != nil {
			return nil, fmt.Errorf("ensure network %q: %w", req.NetworkName, err)
		}
		warnings = append(warnings, networkWarnings...)
		req.NetworkName = network.Name
		req.NetworkMode = network.Name
	case model.NetworkProfileVPN:
		if req.VPNName == "" {
			return nil, fmt.Errorf("vpn network profile requires a VPN gateway name")
		}
		progressx.Active(ctx, "Checking VPN gateway "+req.VPNName, "for environment "+req.Name)
		vpn, err := e.runtime.InspectVPNGateway(ctx, req.VPNName)
		if err != nil {
			return nil, fmt.Errorf("inspect VPN gateway %q: %w", req.VPNName, err)
		}
		if req.Desktop.Enabled && !model.HasPublishedPort(vpn.Ports, req.Desktop.HostIP, req.Desktop.HostPort, req.Desktop.ContainerPort) {
			return nil, fmt.Errorf("desktop mode with VPN gateway %q requires the desktop port to be published on the VPN gateway; recreate the gateway with --port %s", vpn.Name, desktopPortMapping(req.Desktop))
		}
		// In VPN mode Docker puts the workbench in the sidecar's network
		// namespace. Ports, hostnames, and network identity therefore belong to
		// the VPN container, not the environment container.
		req.NetworkName = vpn.Name
		req.NetworkMode = "container:" + vpn.ContainerName
		if req.Hostname != "" && req.Hostname != req.Name {
			warnings = append(warnings, "custom hostnames are ignored for vpn-routed environments because Docker shares the VPN gateway network namespace")
		}
		req.Hostname = ""
		if len(req.Ports) > 0 {
			return nil, fmt.Errorf("ports cannot be published on a container sharing VPN gateway %q; publish ports on the VPN gateway instead", vpn.Name)
		}
		if vpn.State != "running" {
			warnings = append(warnings, fmt.Sprintf("VPN gateway %q is %s; start it before relying on routed traffic", vpn.Name, valueOrUnknown(vpn.State)))
		}
		warnings = append(warnings, fmt.Sprintf("environment traffic will share VPN gateway %q network namespace; host traffic is unchanged", vpn.Name))
		progressx.Done(ctx, "Network configured for "+req.Name, "VPN gateway "+vpn.Name)
	default:
		return nil, fmt.Errorf("unsupported network profile %q", req.NetworkProfile)
	}

	return warnings, nil
}

func applyDesktop(req *model.CreateEnvironmentRequest) ([]string, error) {
	if !req.Desktop.Enabled {
		return nil, nil
	}

	warnings := []string(nil)
	desktop := req.Desktop
	if desktop.Protocol == "" {
		desktop.Protocol = model.DefaultDesktopProtocol
	}
	if desktop.Protocol != model.DefaultDesktopProtocol {
		return nil, fmt.Errorf("desktop protocol %q is not supported yet; novnc is supported", desktop.Protocol)
	}
	if desktop.HostIP == "" {
		desktop.HostIP = model.DefaultDesktopHostIP
	}
	if desktop.HostPort == "" {
		desktop.HostPort = model.DefaultDesktopPort
	}
	if desktop.ContainerPort == "" {
		desktop.ContainerPort = model.DefaultDesktopPort
	}
	if desktop.Scheme == "" {
		desktop.Scheme = model.DefaultDesktopScheme
	}
	if desktop.Path == "" {
		desktop.Path = model.DefaultDesktopPath
	}
	if desktop.User == "" {
		desktop.User = model.DefaultDesktopUser
	}
	if desktop.PasswordEnv == "" {
		desktop.PasswordEnv = model.DefaultDesktopPasswordEnv
	}

	if req.Image == model.DefaultEnvironmentImage {
		req.Image = model.DefaultDesktopImage
		warnings = append(warnings, fmt.Sprintf("desktop mode switched image to %s", req.Image))
	}

	if desktop.Password == "" {
		if password, ok := envValue(req.Env, desktop.PasswordEnv); ok {
			desktop.Password = password
		}
	}
	if desktop.Password == "" {
		password, err := generateDesktopPassword()
		if err != nil {
			return warnings, err
		}
		desktop.Password = password
	}
	req.Env = upsertEnv(req.Env, desktop.PasswordEnv, desktop.Password)
	req.UseImageCommand = true
	if req.ShmSize == 0 {
		req.ShmSize = model.DefaultDesktopShmSize
	}

	if req.NetworkProfile == model.NetworkProfileVPN {
		// The VPN sidecar owns published ports, so desktop mode records the
		// desired endpoint but does not add a port binding to the workbench.
		warnings = append(warnings, fmt.Sprintf("desktop mode is using VPN gateway networking; publish %s:%s:%s on the VPN gateway to access the desktop endpoint", desktop.HostIP, desktop.HostPort, desktop.ContainerPort))
	} else {
		req.Ports = applyDesktopPort(req.Ports, &desktop)
	}

	req.Desktop = desktop
	return warnings, nil
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

func applyDesktopPort(ports []model.PortMapping, desktop *model.DesktopConfig) []model.PortMapping {
	for _, port := range ports {
		protocol := port.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		if port.ContainerPort == desktop.ContainerPort && protocol == "tcp" {
			if port.HostIP != "" {
				desktop.HostIP = port.HostIP
			}
			if port.HostPort != "" {
				desktop.HostPort = port.HostPort
			}
			return ports
		}
	}
	return append(ports, model.PortMapping{
		HostIP:        desktop.HostIP,
		HostPort:      desktop.HostPort,
		ContainerPort: desktop.ContainerPort,
		Protocol:      "tcp",
	})
}

func generateDesktopPassword() (string, error) {
	data := make([]byte, 18)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate desktop password: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

func (e *Engine) attachWorkspace(ctx context.Context, req *model.CreateEnvironmentRequest) error {
	if req.DisableWorkspace {
		progressx.Info(ctx, "Skipping workspace mount", req.Name)
		return nil
	}
	if req.WorkspaceName == "" {
		req.WorkspaceName = req.Name
	}
	if req.WorkspaceMountPath == "" {
		req.WorkspaceMountPath = model.DefaultWorkspaceMountPath
	}

	if req.WorkspacePath == "" && e.workspaces == nil {
		return nil
	}
	if e.workspaces != nil {
		progressx.Active(ctx, "Preparing workspace "+req.WorkspaceName, "host mount")
		ws, err := e.workspaces.Ensure(ctx, model.EnsureWorkspaceRequest{
			Name: req.WorkspaceName,
			Path: req.WorkspacePath,
		})
		if err != nil {
			return fmt.Errorf("ensure workspace %q: %w", req.WorkspaceName, err)
		}
		req.WorkspacePath = ws.Path
		progressx.Done(ctx, "Workspace ready "+req.WorkspaceName, ws.Path)
	}
	if req.WorkspacePath == "" {
		return nil
	}

	for _, volume := range req.Volumes {
		if volume.Target == req.WorkspaceMountPath {
			return fmt.Errorf("workspace mount target %q conflicts with an explicit volume", req.WorkspaceMountPath)
		}
	}
	req.Volumes = append([]model.VolumeMount{{
		Source: req.WorkspacePath,
		Target: req.WorkspaceMountPath,
	}}, req.Volumes...)
	return nil
}

func normalizeCreateRequest(req model.CreateEnvironmentRequest) model.CreateEnvironmentRequest {
	req.Name = strings.TrimSpace(req.Name)
	req.Image = strings.TrimSpace(req.Image)
	req.Shell = strings.TrimSpace(req.Shell)
	req.User = strings.TrimSpace(req.User)
	req.Hostname = strings.TrimSpace(req.Hostname)
	req.NetworkProfile = strings.TrimSpace(req.NetworkProfile)
	req.NetworkName = strings.TrimSpace(req.NetworkName)
	req.NetworkMode = strings.TrimSpace(req.NetworkMode)
	req.VPNName = strings.TrimSpace(req.VPNName)
	req.WorkspaceName = strings.TrimSpace(req.WorkspaceName)
	req.WorkspacePath = strings.TrimSpace(req.WorkspacePath)
	req.WorkspaceMountPath = strings.TrimSpace(req.WorkspaceMountPath)
	req.Desktop.Protocol = strings.TrimSpace(strings.ToLower(req.Desktop.Protocol))
	req.Desktop.HostIP = strings.TrimSpace(req.Desktop.HostIP)
	req.Desktop.HostPort = strings.TrimSpace(req.Desktop.HostPort)
	req.Desktop.ContainerPort = strings.TrimSpace(req.Desktop.ContainerPort)
	req.Desktop.Scheme = strings.TrimSpace(strings.ToLower(req.Desktop.Scheme))
	req.Desktop.Path = strings.TrimSpace(req.Desktop.Path)
	req.Desktop.User = strings.TrimSpace(req.Desktop.User)
	req.Desktop.PasswordEnv = strings.TrimSpace(req.Desktop.PasswordEnv)
	req.CapAdd = normalizeStringList(req.CapAdd)

	if req.Image == "" {
		req.Image = model.DefaultEnvironmentImage
	}
	if req.Shell == "" {
		req.Shell = model.DefaultEnvironmentShell
	}
	if req.Hostname == "" {
		req.Hostname = req.Name
	}
	if req.NetworkProfile == "" {
		if req.NetworkMode != "" {
			req.NetworkProfile = req.NetworkMode
		} else {
			req.NetworkProfile = model.DefaultNetworkProfile
		}
	}
	if req.WorkspaceMountPath == "" {
		req.WorkspaceMountPath = model.DefaultWorkspaceMountPath
	}

	return req
}

func validateCreateRequest(req model.CreateEnvironmentRequest) error {
	if err := validateEnvironmentName(req.Name); err != nil {
		return err
	}
	if req.Image == "" {
		return fmt.Errorf("image is required")
	}
	if req.Shell == "" {
		return fmt.Errorf("shell is required")
	}
	if err := validateNetworkProfile(req.NetworkProfile); err != nil {
		return err
	}
	for _, env := range req.Env {
		if !strings.Contains(env, "=") {
			return fmt.Errorf("environment value %q must be KEY=VALUE", env)
		}
	}
	for _, volume := range req.Volumes {
		if volume.Source == "" || volume.Target == "" {
			return fmt.Errorf("volume source and target are required")
		}
	}
	if !req.DisableWorkspace && req.WorkspaceName != "" {
		if err := validateEnvironmentName(req.WorkspaceName); err != nil {
			return fmt.Errorf("workspace name: %w", err)
		}
	}
	if !req.DisableWorkspace && req.WorkspaceMountPath == "" {
		return fmt.Errorf("workspace mount path is required")
	}
	for _, port := range req.Ports {
		if port.HostPort == "" || port.ContainerPort == "" {
			return fmt.Errorf("port mapping host and container ports are required")
		}
	}
	for _, capability := range req.CapAdd {
		if strings.TrimSpace(capability) == "" {
			return fmt.Errorf("capability entries cannot be empty")
		}
	}
	if req.NetworkProfile == model.NetworkProfileVPN && req.VPNName == "" {
		return fmt.Errorf("vpn gateway name is required for vpn network profile")
	}
	if req.NetworkProfile != model.NetworkProfileVPN && req.VPNName != "" {
		return fmt.Errorf("vpn gateway name requires vpn network profile; use --network vpn --vpn %s", req.VPNName)
	}
	if req.Desktop.Enabled {
		if req.Desktop.Protocol != "" && req.Desktop.Protocol != model.DefaultDesktopProtocol {
			return fmt.Errorf("desktop protocol %q is not supported yet; novnc is supported", req.Desktop.Protocol)
		}
		if req.Desktop.PasswordEnv != "" && strings.Contains(req.Desktop.PasswordEnv, "=") {
			return fmt.Errorf("desktop password environment key must be a variable name, not KEY=VALUE")
		}
	}
	return nil
}

func normalizeCreateVPNGatewayRequest(req model.CreateVPNGatewayRequest) model.CreateVPNGatewayRequest {
	req.Name = strings.TrimSpace(req.Name)
	req.Image = strings.TrimSpace(req.Image)
	req.Provider = strings.TrimSpace(strings.ToLower(req.Provider))
	req.RouteMode = strings.TrimSpace(strings.ToLower(req.RouteMode))
	req.ConfigPath = strings.TrimSpace(req.ConfigPath)
	req.ConfigMountPath = strings.TrimSpace(req.ConfigMountPath)

	if req.Image == "" {
		req.Image = model.DefaultVPNImage
	}
	if req.Provider == "" {
		req.Provider = model.DefaultVPNProvider
	}
	if req.RouteMode == "" {
		req.RouteMode = model.VPNRouteModeFull
	}
	if req.ConfigMountPath == "" {
		req.ConfigMountPath = model.DefaultVPNConfigMount
	}
	if len(req.CapAdd) == 0 && !req.Privileged {
		req.CapAdd = []string{"NET_ADMIN"}
	}
	if len(req.Devices) == 0 && !req.Privileged {
		req.Devices = []model.DeviceMapping{{
			HostPath:      "/dev/net/tun",
			ContainerPath: "/dev/net/tun",
			Permissions:   "rwm",
		}}
	}
	if req.ConfigPath != "" {
		req = attachVPNConfig(req)
	}
	return req
}

func normalizeStringList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func applyVPNRouteMode(req *model.CreateVPNGatewayRequest) ([]string, error) {
	switch req.RouteMode {
	case model.VPNRouteModeFull:
		return nil, nil
	case model.VPNRouteModeSplit:
		if req.ConfigPath == "" {
			return nil, fmt.Errorf("split route mode requires an OpenVPN config file")
		}
		info, err := os.Stat(req.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("vpn config path %q: %w", req.ConfigPath, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("split route mode requires an OpenVPN config file, not a directory")
		}
		if len(req.Routes) == 0 {
			cfg, err := vpnconfig.ParseOpenVPNFile(req.ConfigPath)
			if err != nil {
				return nil, err
			}
			req.Routes = modelVPNRoutes(cfg.Routes)
		}
		if !hasEnvKey(req.Env, "DEFAULT_GATEWAY") {
			req.Env = append([]string{"DEFAULT_GATEWAY=false"}, req.Env...)
		}
		if len(req.Routes) == 0 {
			req.Env = upsertOpenVPNArgs(req.Env, "--pull-filter ignore redirect-gateway")
			return []string{"split route mode will accept server-pushed VPN routes while blocking default-gateway pushes"}, nil
		}
		req.Env = upsertOpenVPNArgs(req.Env, "--route-nopull", "--pull-filter ignore redirect-gateway")
		return []string{fmt.Sprintf("split route mode will route only configured VPN networks: %s", formatVPNRoutes(req.Routes))}, nil
	default:
		return nil, fmt.Errorf("vpn route mode %q must be full or split", req.RouteMode)
	}
}

func validateCreateVPNGatewayRequest(req model.CreateVPNGatewayRequest) ([]string, error) {
	warnings := []string(nil)
	if err := validateVPNName(req.Name); err != nil {
		return warnings, err
	}
	if req.Image == "" {
		return warnings, fmt.Errorf("vpn image is required")
	}
	if req.Provider != model.DefaultVPNProvider {
		return warnings, fmt.Errorf("vpn provider %q is not supported yet; openvpn is supported", req.Provider)
	}
	if req.ConfigPath == "" {
		warnings = append(warnings, "no VPN config path supplied; the VPN image must be fully configured by environment variables or command arguments")
	}
	if req.ConfigPath != "" {
		if _, err := os.Stat(req.ConfigPath); err != nil {
			return warnings, fmt.Errorf("vpn config path %q: %w", req.ConfigPath, err)
		}
	}
	for _, env := range req.Env {
		if !strings.Contains(env, "=") {
			return warnings, fmt.Errorf("environment value %q must be KEY=VALUE", env)
		}
	}
	for _, volume := range req.Volumes {
		if volume.Source == "" || volume.Target == "" {
			return warnings, fmt.Errorf("volume source and target are required")
		}
	}
	for _, port := range req.Ports {
		if port.HostPort == "" || port.ContainerPort == "" {
			return warnings, fmt.Errorf("port mapping host and container ports are required")
		}
	}
	if !req.Privileged && !hasCapability(req.CapAdd, "NET_ADMIN") {
		warnings = append(warnings, "OpenVPN sidecars usually need NET_ADMIN; add --cap-add NET_ADMIN or --privileged if this image requires it")
	}
	if !req.Privileged && !hasDevice(req.Devices, "/dev/net/tun") {
		warnings = append(warnings, "OpenVPN sidecars usually need /dev/net/tun; add --device /dev/net/tun:/dev/net/tun:rwm if this image requires it")
	}
	return warnings, nil
}

func upsertOpenVPNArgs(env []string, args ...string) []string {
	existing := ""
	index := -1
	for i, value := range env {
		if strings.HasPrefix(value, "OTHER_ARGS=") {
			index = i
			existing = strings.TrimPrefix(value, "OTHER_ARGS=")
			break
		}
	}
	for _, arg := range args {
		if !strings.Contains(existing, arg) {
			if existing != "" {
				existing += " "
			}
			existing += arg
		}
	}
	if index >= 0 {
		env[index] = "OTHER_ARGS=" + existing
		return env
	}
	return append(env, "OTHER_ARGS="+existing)
}

func modelVPNRoutes(routes []vpnconfig.Route) []model.VPNRoute {
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

func attachVPNConfig(req model.CreateVPNGatewayRequest) model.CreateVPNGatewayRequest {
	abs, err := filepath.Abs(req.ConfigPath)
	if err == nil {
		req.ConfigPath = abs
	}
	info, err := os.Stat(req.ConfigPath)
	if err != nil {
		return req
	}
	source := req.ConfigPath
	configFile := ""
	if !info.IsDir() {
		source = filepath.Dir(req.ConfigPath)
		configFile = filepath.Base(req.ConfigPath)
	}

	req.Volumes = append([]model.VolumeMount{{
		Source:   source,
		Target:   req.ConfigMountPath,
		ReadOnly: true,
	}}, req.Volumes...)
	if configFile != "" && !hasEnvKey(req.Env, "VPN_FILES") {
		req.Env = append([]string{"VPN_FILES=" + configFile}, req.Env...)
	}
	return req
}

func hasCapability(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}
	return false
}

func hasDevice(values []model.DeviceMapping, path string) bool {
	for _, value := range values {
		if value.HostPath == path || value.ContainerPath == path {
			return true
		}
	}
	return false
}

func hasEnvKey(values []string, key string) bool {
	prefix := key + "="
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
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

func upsertEnv(values []string, key string, value string) []string {
	prefix := key + "="
	env := key + "=" + value
	for index, item := range values {
		if strings.HasPrefix(item, prefix) {
			values[index] = env
			return values
		}
	}
	return append(values, env)
}

func normalizeCreateServiceRequest(req model.CreateServiceRequest) model.CreateServiceRequest {
	req.EnvironmentName = strings.TrimSpace(req.EnvironmentName)
	req.Name = strings.TrimSpace(req.Name)
	req.Image = strings.TrimSpace(req.Image)
	req.NetworkName = strings.TrimSpace(req.NetworkName)
	req.NetworkAlias = strings.TrimSpace(req.NetworkAlias)
	if req.NetworkAlias == "" {
		req.NetworkAlias = req.Name
	}
	return req
}

func validateCreateServiceRequest(req model.CreateServiceRequest) error {
	if err := validateEnvironmentName(req.EnvironmentName); err != nil {
		return err
	}
	if err := validateServiceName(req.Name); err != nil {
		return err
	}
	if req.Image == "" {
		return fmt.Errorf("service image is required")
	}
	for _, env := range req.Env {
		if !strings.Contains(env, "=") {
			return fmt.Errorf("environment value %q must be KEY=VALUE", env)
		}
	}
	for _, volume := range req.Volumes {
		if volume.Source == "" || volume.Target == "" {
			return fmt.Errorf("volume source and target are required")
		}
	}
	for _, port := range req.Ports {
		if port.HostPort == "" || port.ContainerPort == "" {
			return fmt.Errorf("port mapping host and container ports are required")
		}
	}
	return nil
}

func serviceNetworkForEnvironment(env model.Environment) (string, []string, error) {
	networkName := strings.TrimSpace(env.NetworkName)
	if networkName == "" {
		networkName = strings.TrimSpace(env.NetworkMode)
	}

	switch networkName {
	case "", model.NetworkProfileBridge, model.NetworkProfileHost, "none":
		return "", nil, fmt.Errorf("environment %q uses network mode %q; create the environment with --network segment before attaching services", env.Name, valueOrUnknown(env.NetworkMode))
	default:
		return networkName, nil, nil
	}
}

func validateNetworkProfile(profile string) error {
	switch profile {
	case model.NetworkProfileBridge, model.NetworkProfileIsolated, model.NetworkProfileHost, model.NetworkProfileSegment, model.NetworkProfileVPN:
		return nil
	default:
		return fmt.Errorf("network profile %q must be one of bridge, isolated, host, segment, or vpn", profile)
	}
}

func validateEnvironmentName(name string) error {
	if name == "" {
		return fmt.Errorf("environment name is required")
	}
	if !environmentNamePattern.MatchString(name) {
		return fmt.Errorf("environment name %q must start with a letter or number and contain only letters, numbers, dots, underscores, or dashes", name)
	}
	return nil
}

func validateServiceName(name string) error {
	if name == "" {
		return fmt.Errorf("service name is required")
	}
	if !environmentNamePattern.MatchString(name) {
		return fmt.Errorf("service name %q must start with a letter or number and contain only letters, numbers, dots, underscores, or dashes", name)
	}
	return nil
}

func validateVPNName(name string) error {
	if name == "" {
		return fmt.Errorf("vpn gateway name is required")
	}
	if !environmentNamePattern.MatchString(name) {
		return fmt.Errorf("vpn gateway name %q must start with a letter or number and contain only letters, numbers, dots, underscores, or dashes", name)
	}
	return nil
}

func valueOrUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

func shortID(id string) string {
	if len(id) <= 12 {
		return id
	}
	return id[:12]
}
