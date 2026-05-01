package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/NorthernReach/contaigen/internal/config"
	"github.com/NorthernReach/contaigen/internal/dockerx"
	"github.com/NorthernReach/contaigen/internal/model"
	"github.com/NorthernReach/contaigen/internal/templates"
	"github.com/NorthernReach/contaigen/internal/workspace"
	"github.com/spf13/cobra"
)

func TestVersionCommand(t *testing.T) {
	output, err := execute(t, NewRootCommand(Options{
		Build: BuildInfo{
			Version: "v0.0.1",
			Commit:  "abc123",
			Date:    "2026-04-30",
		},
	}), "version")
	if err != nil {
		t.Fatalf("version command failed: %v", err)
	}

	for _, want := range []string{"contaigen v0.0.1", "commit: abc123", "built: 2026-04-30"} {
		if !strings.Contains(output, want) {
			t.Fatalf("version output missing %q:\n%s", want, output)
		}
	}
}

func TestDoctorCommandReportsDockerAvailable(t *testing.T) {
	output, err := execute(t, NewRootCommand(Options{
		Paths: fixedPaths,
		NewDockerClient: func() (dockerx.Runtime, error) {
			return &fakeRuntime{}, nil
		},
	}), "doctor")
	if err != nil {
		t.Fatalf("doctor command failed: %v", err)
	}

	for _, want := range []string{
		"Contaigen doctor",
		"config: /tmp/contaigen/config.yaml",
		"Docker: available",
		"server version: 29.0.0",
		"API version: 1.50",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestDoctorCommandReportsDockerUnavailableWithoutFailing(t *testing.T) {
	output, err := execute(t, NewRootCommand(Options{
		Paths: fixedPaths,
		NewDockerClient: func() (dockerx.Runtime, error) {
			return nil, errors.New("docker socket not found")
		},
	}), "doctor")
	if err != nil {
		t.Fatalf("doctor command should not fail when Docker is unavailable: %v", err)
	}
	if !strings.Contains(output, "Docker: unavailable (docker socket not found)") {
		t.Fatalf("doctor output did not report Docker unavailable:\n%s", output)
	}
}

func TestColorAlwaysAddsANSI(t *testing.T) {
	output, err := execute(t, NewRootCommand(Options{
		Paths: fixedPaths,
		NewDockerClient: func() (dockerx.Runtime, error) {
			return &fakeRuntime{}, nil
		},
	}), "--color", "always", "doctor")
	if err != nil {
		t.Fatalf("doctor command failed: %v", err)
	}
	if !strings.Contains(output, "\x1b[") {
		t.Fatalf("expected ANSI color output with --color always:\n%s", output)
	}
}

func TestInvalidColorModeFails(t *testing.T) {
	_, err := execute(t, NewRootCommand(Options{}), "--color", "sometimes", "doctor")
	if err == nil {
		t.Fatal("expected invalid color mode to fail")
	}
	if !strings.Contains(err.Error(), "color must be auto, always, or never") {
		t.Fatalf("unexpected error for invalid color mode: %v", err)
	}
}

func TestRootHelpIncludesExamplesAndGlobalFlags(t *testing.T) {
	output, err := execute(t, NewRootCommand(Options{}), "--help")
	if err != nil {
		t.Fatalf("root help failed: %v", err)
	}

	for _, want := range []string{
		"Examples:",
		"contaigen env create lab --profile parrot-default --network segment",
		"Flags:",
		"--color",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("root help missing %q:\n%s", want, output)
		}
	}
}

func TestRootCommandWiresExpectedCommands(t *testing.T) {
	cmd := NewRootCommand(Options{})

	for _, want := range []string{"compose", "doctor", "env", "net", "nuke", "profile", "service", "template", "version", "vpn", "workspace"} {
		if _, _, err := cmd.Find([]string{want}); err != nil {
			t.Fatalf("expected command %q to be registered: %v", want, err)
		}
	}
}

func execute(t *testing.T, cmd *cobra.Command, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func fixedPaths() (config.Paths, error) {
	return config.Paths{
		ConfigDir:    "/tmp/contaigen",
		ConfigFile:   "/tmp/contaigen/config.yaml",
		DataDir:      "/tmp/contaigen/data",
		WorkspaceDir: "/tmp/contaigen/data/workspaces",
		TemplateDir:  "/tmp/contaigen/data/templates",
		BackupDir:    "/tmp/contaigen/data/backups",
		StateDir:     "/tmp/contaigen/state",
		LogDir:       "/tmp/contaigen/state/logs",
	}, nil
}

type fakeRuntime struct {
	envs            []model.Environment
	created         []model.CreateEnvironmentRequest
	started         []string
	stopped         []string
	removed         []string
	entered         []string
	enterRequests   []model.EnterEnvironmentRequest
	networks        []model.Network
	networkEnsures  []model.EnsureNetworkRequest
	removedNetworks []string
	networkMap      model.NetworkMap
	services        []model.Service
	createdServices []model.CreateServiceRequest
	startedServices []string
	stoppedServices []string
	removedServices []string
	vpns            []model.VPNGateway
	createdVPNs     []model.CreateVPNGatewayRequest
	startedVPNs     []string
	stoppedVPNs     []string
	removedVPNs     []string
	vpnLogs         []string
}

func (*fakeRuntime) Ping(context.Context) (dockerx.PingInfo, error) {
	return dockerx.PingInfo{
		APIVersion: "1.50",
		OSType:     "linux",
	}, nil
}

func (*fakeRuntime) ServerVersion(context.Context) (dockerx.ServerVersion, error) {
	return dockerx.ServerVersion{
		Version:         "29.0.0",
		APIVersion:      "1.50",
		MinAPIVersion:   "1.40",
		OperatingSystem: "linux",
		Architecture:    "arm64",
	}, nil
}

func (*fakeRuntime) EnsureImage(context.Context, string) error {
	return nil
}

func (f *fakeRuntime) CreateEnvironment(_ context.Context, req model.CreateEnvironmentRequest) (model.Environment, []string, error) {
	f.created = append(f.created, req)
	env := model.Environment{
		ID:                 "1234567890abcdef",
		Name:               req.Name,
		ContainerName:      "contaigen-" + req.Name,
		Image:              req.Image,
		State:              "running",
		Status:             "running",
		Shell:              req.Shell,
		User:               req.User,
		Hostname:           req.Hostname,
		NetworkProfile:     req.NetworkProfile,
		NetworkName:        req.NetworkName,
		NetworkMode:        req.NetworkMode,
		VPNName:            req.VPNName,
		WorkspaceName:      req.WorkspaceName,
		WorkspacePath:      req.WorkspacePath,
		WorkspaceMountPath: req.WorkspaceMountPath,
		Desktop:            req.Desktop,
		Ports:              req.Ports,
		Volumes:            req.Volumes,
		CapAdd:             req.CapAdd,
		Labels:             model.EnvironmentLabels(req.Name, req.Shell),
	}
	return env, nil, nil
}

func (f *fakeRuntime) ListEnvironments(context.Context) ([]model.Environment, error) {
	return f.envs, nil
}

func (f *fakeRuntime) InspectEnvironment(_ context.Context, name string) (model.Environment, error) {
	for i := len(f.created) - 1; i >= 0; i-- {
		req := f.created[i]
		if req.Name == name {
			return model.Environment{
				ID:                 "1234567890abcdef",
				Name:               name,
				ContainerName:      "contaigen-" + name,
				Image:              req.Image,
				State:              "running",
				Status:             "running",
				Shell:              req.Shell,
				User:               req.User,
				Hostname:           req.Hostname,
				NetworkProfile:     req.NetworkProfile,
				NetworkName:        req.NetworkName,
				NetworkMode:        req.NetworkMode,
				VPNName:            req.VPNName,
				WorkspaceName:      req.WorkspaceName,
				WorkspacePath:      req.WorkspacePath,
				WorkspaceMountPath: req.WorkspaceMountPath,
				Desktop:            req.Desktop,
				Ports:              req.Ports,
				Volumes:            req.Volumes,
				CapAdd:             req.CapAdd,
				Labels:             model.EnvironmentLabels(req.Name, req.Shell),
			}, nil
		}
	}
	for _, env := range f.envs {
		if env.Name == name {
			return env, nil
		}
	}
	return model.Environment{
		ID:             "1234567890abcdef",
		Name:           name,
		ContainerName:  "contaigen-" + name,
		Image:          model.DefaultEnvironmentImage,
		State:          "running",
		Status:         "running",
		Shell:          model.DefaultEnvironmentShell,
		Hostname:       name,
		NetworkProfile: model.DefaultNetworkProfile,
		NetworkName:    model.DefaultNetworkProfile,
		NetworkMode:    model.DefaultNetworkMode,
	}, nil
}

func (f *fakeRuntime) EnsureNetwork(_ context.Context, req model.EnsureNetworkRequest) (model.Network, []string, error) {
	f.networkEnsures = append(f.networkEnsures, req)
	network := model.Network{
		ID:         "network1234567890",
		Name:       req.Name,
		Driver:     req.Driver,
		Profile:    req.Profile,
		Internal:   req.Internal,
		Attachable: req.Attachable,
	}
	f.networks = append(f.networks, network)
	return network, nil, nil
}

func (f *fakeRuntime) ListNetworks(context.Context) ([]model.Network, error) {
	return f.networks, nil
}

func (f *fakeRuntime) InspectNetwork(_ context.Context, name string) (model.Network, error) {
	for _, network := range f.networks {
		if network.Name == name {
			return network, nil
		}
	}
	return model.Network{
		ID:      "network1234567890",
		Name:    name,
		Driver:  model.DefaultNetworkDriver,
		Profile: model.NetworkProfileSegment,
	}, nil
}

func (f *fakeRuntime) RemoveNetwork(_ context.Context, name string) error {
	f.removedNetworks = append(f.removedNetworks, name)
	return nil
}

func (f *fakeRuntime) NetworkMap(context.Context) (model.NetworkMap, error) {
	if len(f.networkMap.Networks) > 0 {
		return f.networkMap, nil
	}
	return model.NetworkMap{Networks: f.networks}, nil
}

func (f *fakeRuntime) CreateService(_ context.Context, req model.CreateServiceRequest) (model.Service, []string, error) {
	f.createdServices = append(f.createdServices, req)
	service := model.Service{
		ID:              "service1234567890",
		Name:            req.Name,
		ContainerName:   "contaigen-" + req.EnvironmentName + "-svc-" + req.Name,
		EnvironmentName: req.EnvironmentName,
		Image:           req.Image,
		State:           "created",
		Status:          "created",
		NetworkName:     req.NetworkName,
		NetworkAlias:    req.NetworkAlias,
		Ports:           req.Ports,
		Volumes:         req.Volumes,
		Command:         req.Command,
		Labels:          model.ServiceLabels(req.EnvironmentName, req.Name),
	}
	f.services = append(f.services, service)
	return service, nil, nil
}

func (f *fakeRuntime) ListServices(_ context.Context, envName string) ([]model.Service, error) {
	if envName == "" {
		return f.services, nil
	}
	services := []model.Service{}
	for _, service := range f.services {
		if service.EnvironmentName == envName {
			services = append(services, service)
		}
	}
	return services, nil
}

func (f *fakeRuntime) InspectService(_ context.Context, envName string, serviceName string) (model.Service, error) {
	for i := len(f.services) - 1; i >= 0; i-- {
		service := f.services[i]
		if service.EnvironmentName == envName && service.Name == serviceName {
			if service.State == "created" {
				service.State = "running"
				service.Status = "running"
			}
			return service, nil
		}
	}
	return model.Service{
		ID:              "service1234567890",
		Name:            serviceName,
		ContainerName:   "contaigen-" + envName + "-svc-" + serviceName,
		EnvironmentName: envName,
		Image:           "nginx:alpine",
		State:           "running",
		Status:          "running",
		NetworkName:     "contaigen-" + envName,
		NetworkAlias:    serviceName,
		Labels:          model.ServiceLabels(envName, serviceName),
	}, nil
}

func (f *fakeRuntime) StartEnvironment(_ context.Context, name string) error {
	f.started = append(f.started, name)
	return nil
}

func (f *fakeRuntime) StopEnvironment(_ context.Context, name string, _ *int) error {
	f.stopped = append(f.stopped, name)
	return nil
}

func (f *fakeRuntime) RemoveEnvironment(_ context.Context, name string, _ model.RemoveEnvironmentRequest) error {
	f.removed = append(f.removed, name)
	return nil
}

func (f *fakeRuntime) EnterEnvironment(_ context.Context, name string, req model.EnterEnvironmentRequest, streams model.ExecIO) error {
	f.entered = append(f.entered, name)
	f.enterRequests = append(f.enterRequests, req)
	if streams.Stdout != nil {
		_, _ = fmt.Fprintln(streams.Stdout, "entered "+name)
	}
	return nil
}

func (f *fakeRuntime) StartService(_ context.Context, envName string, serviceName string) error {
	f.startedServices = append(f.startedServices, envName+"/"+serviceName)
	return nil
}

func (f *fakeRuntime) StopService(_ context.Context, envName string, serviceName string, _ *int) error {
	f.stoppedServices = append(f.stoppedServices, envName+"/"+serviceName)
	return nil
}

func (f *fakeRuntime) RemoveService(_ context.Context, envName string, serviceName string, _ model.RemoveServiceRequest) error {
	f.removedServices = append(f.removedServices, envName+"/"+serviceName)
	return nil
}

func (f *fakeRuntime) CreateVPNGateway(_ context.Context, req model.CreateVPNGatewayRequest) (model.VPNGateway, []string, error) {
	f.createdVPNs = append(f.createdVPNs, req)
	vpn := model.VPNGateway{
		ID:              "vpn1234567890",
		Name:            req.Name,
		ContainerName:   "contaigen-vpn-" + req.Name,
		Image:           req.Image,
		Provider:        req.Provider,
		RouteMode:       req.RouteMode,
		Routes:          req.Routes,
		State:           "created",
		Status:          "created",
		ConfigPath:      req.ConfigPath,
		ConfigMountPath: req.ConfigMountPath,
		Ports:           req.Ports,
		NoVNCPorts:      req.NoVNCPorts,
		Volumes:         req.Volumes,
		Env:             req.Env,
		Command:         req.Command,
		CapAdd:          req.CapAdd,
		Devices:         req.Devices,
		Labels:          model.VPNLabels(req.Name, req.Provider),
	}
	f.vpns = append(f.vpns, vpn)
	return vpn, nil, nil
}

func (f *fakeRuntime) ListVPNGateways(context.Context) ([]model.VPNGateway, error) {
	return f.vpns, nil
}

func (f *fakeRuntime) InspectVPNGateway(_ context.Context, name string) (model.VPNGateway, error) {
	for i := len(f.vpns) - 1; i >= 0; i-- {
		vpn := f.vpns[i]
		if vpn.Name == name {
			if vpn.State == "created" {
				vpn.State = "running"
				vpn.Status = "running"
			}
			return vpn, nil
		}
	}
	return model.VPNGateway{
		ID:            "vpn1234567890",
		Name:          name,
		ContainerName: "contaigen-vpn-" + name,
		Image:         model.DefaultVPNImage,
		Provider:      model.DefaultVPNProvider,
		RouteMode:     model.VPNRouteModeFull,
		State:         "running",
		Status:        "running",
		Labels:        model.VPNLabels(name, model.DefaultVPNProvider),
	}, nil
}

func (f *fakeRuntime) StartVPNGateway(_ context.Context, name string) error {
	f.startedVPNs = append(f.startedVPNs, name)
	return nil
}

func (f *fakeRuntime) StopVPNGateway(_ context.Context, name string, _ *int) error {
	f.stoppedVPNs = append(f.stoppedVPNs, name)
	return nil
}

func (f *fakeRuntime) RemoveVPNGateway(_ context.Context, name string, _ model.RemoveVPNGatewayRequest) error {
	f.removedVPNs = append(f.removedVPNs, name)
	return nil
}

func (f *fakeRuntime) VPNGatewayLogs(_ context.Context, name string, _ model.VPNLogsRequest, io model.VPNLogIO) error {
	f.vpnLogs = append(f.vpnLogs, name)
	if io.Stdout != nil {
		_, _ = fmt.Fprintln(io.Stdout, "vpn log line")
	}
	return nil
}

func (*fakeRuntime) Close() error {
	return nil
}

type fakeWorkspaceManager struct {
	ensured  []model.EnsureWorkspaceRequest
	created  []model.CreateWorkspaceRequest
	items    []model.Workspace
	backups  []model.BackupWorkspaceRequest
	restores []model.RestoreWorkspaceRequest
	removed  []model.RemoveWorkspaceRequest
}

func (f *fakeWorkspaceManager) Ensure(_ context.Context, req model.EnsureWorkspaceRequest) (model.Workspace, error) {
	f.ensured = append(f.ensured, req)
	path := req.Path
	if path == "" {
		path = "/tmp/contaigen/workspaces/" + req.Name
	}
	return model.Workspace{
		Name: req.Name,
		Path: path,
	}, nil
}

func (f *fakeWorkspaceManager) Create(_ context.Context, req model.CreateWorkspaceRequest) (model.Workspace, error) {
	f.created = append(f.created, req)
	path := req.Path
	if path == "" {
		path = "/tmp/contaigen/workspaces/" + req.Name
	}
	return model.Workspace{
		Name: req.Name,
		Path: path,
	}, nil
}

func (f *fakeWorkspaceManager) List(context.Context) ([]model.Workspace, error) {
	return f.items, nil
}

func (f *fakeWorkspaceManager) Inspect(_ context.Context, name string) (model.Workspace, error) {
	for _, item := range f.items {
		if item.Name == name {
			return item, nil
		}
	}
	return model.Workspace{
		Name: name,
		Path: "/tmp/contaigen/workspaces/" + name,
	}, nil
}

func (f *fakeWorkspaceManager) Backup(_ context.Context, req model.BackupWorkspaceRequest) (model.WorkspaceBackup, error) {
	f.backups = append(f.backups, req)
	path := req.OutputPath
	if path == "" {
		path = "/tmp/contaigen/backups/" + req.Name + ".tar.gz"
	}
	return model.WorkspaceBackup{
		Workspace: model.Workspace{Name: req.Name, Path: "/tmp/contaigen/workspaces/" + req.Name},
		Path:      path,
		SizeBytes: 123,
		Encrypted: req.Password != "",
	}, nil
}

func (f *fakeWorkspaceManager) Restore(_ context.Context, req model.RestoreWorkspaceRequest) (model.WorkspaceRestore, error) {
	f.restores = append(f.restores, req)
	path := req.Path
	if path == "" {
		path = "/tmp/contaigen/workspaces/" + req.Name
	}
	return model.WorkspaceRestore{
		Workspace: model.Workspace{Name: req.Name, Path: path},
		Path:      req.InputPath,
		Files:     2,
		SizeBytes: 123,
	}, nil
}

func (f *fakeWorkspaceManager) Remove(_ context.Context, req model.RemoveWorkspaceRequest) (model.WorkspaceRemove, error) {
	f.removed = append(f.removed, req)
	path := req.Path
	if path == "" {
		path = "/tmp/contaigen/workspaces/" + req.Name
	}
	return model.WorkspaceRemove{
		Workspace: model.Workspace{Name: req.Name, Path: path},
	}, nil
}

func fakeWorkspaceStore(manager workspace.Manager) func() (workspace.Manager, error) {
	return func() (workspace.Manager, error) {
		return manager, nil
	}
}

type fakeProfileManager struct {
	summaries        []model.ProfileSummary
	profiles         map[string]model.EnvironmentProfile
	serviceSummaries []model.ServiceTemplateSummary
	serviceTemplates map[string]model.ServiceTemplate
	validated        []string
}

func (f *fakeProfileManager) List(context.Context) ([]model.ProfileSummary, error) {
	return f.summaries, nil
}

func (f *fakeProfileManager) Load(_ context.Context, name string) (model.EnvironmentProfile, error) {
	if f.profiles != nil {
		if profile, ok := f.profiles[name]; ok {
			return profile, nil
		}
	}
	return model.EnvironmentProfile{
		APIVersion: model.ProfileAPIVersion,
		Kind:       model.ProfileKind,
		Metadata: model.EnvironmentProfileMeta{
			Name: name,
		},
		Spec: model.EnvironmentProfileSpec{
			Image: "example/profile",
			Shell: "/bin/sh",
			Network: model.ProfileNetwork{
				Profile: model.NetworkProfileBridge,
			},
			Workspace: model.ProfileWorkspace{
				MountPath: model.DefaultWorkspaceMountPath,
			},
		},
		Source: "test",
	}, nil
}

func (f *fakeProfileManager) ValidateFile(_ context.Context, path string) (model.EnvironmentProfile, error) {
	f.validated = append(f.validated, path)
	return model.EnvironmentProfile{
		APIVersion: model.ProfileAPIVersion,
		Kind:       model.ProfileKind,
		Metadata: model.EnvironmentProfileMeta{
			Name: "validated",
		},
		Spec: model.EnvironmentProfileSpec{
			Image: "example/validated",
		},
		Source: "file",
		Path:   path,
	}, nil
}

func (f *fakeProfileManager) ListServices(context.Context) ([]model.ServiceTemplateSummary, error) {
	return f.serviceSummaries, nil
}

func (f *fakeProfileManager) LoadService(_ context.Context, name string) (model.ServiceTemplate, error) {
	if f.serviceTemplates != nil {
		if service, ok := f.serviceTemplates[name]; ok {
			return service, nil
		}
	}
	return model.ServiceTemplate{}, fmt.Errorf("%w: %s", templates.ErrServiceTemplateNotFound, name)
}

func (f *fakeProfileManager) ValidateServiceFile(_ context.Context, path string) (model.ServiceTemplate, error) {
	f.validated = append(f.validated, path)
	return model.ServiceTemplate{
		APIVersion: model.ProfileAPIVersion,
		Kind:       model.ServiceKind,
		Metadata: model.ServiceTemplateMeta{
			Name: "validated-service",
		},
		Spec: model.ServiceTemplateSpec{
			Image: "example/service",
		},
		Source: "file",
		Path:   path,
	}, nil
}

func (f *fakeProfileManager) ValidateAnyFile(_ context.Context, path string) (templates.ValidatedTemplate, error) {
	f.validated = append(f.validated, path)
	return templates.ValidatedTemplate{
		Kind:   model.ProfileKind,
		Name:   "validated",
		Source: "file",
		Path:   path,
	}, nil
}

func fakeProfileStore(manager *fakeProfileManager) func() (templates.Manager, error) {
	return func() (templates.Manager, error) {
		return manager, nil
	}
}
