package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NorthernReach/contaigen/internal/model"
)

func TestCreateEnvironmentAppliesDefaultsAndStarts(t *testing.T) {
	runtime := &fakeRuntime{}
	eng := New(runtime)

	env, _, err := eng.CreateEnvironment(context.Background(), model.CreateEnvironmentRequest{
		Name:  "lab",
		Pull:  true,
		Start: true,
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}

	if runtime.pulled != model.DefaultEnvironmentImage {
		t.Fatalf("expected default image to be pulled, got %q", runtime.pulled)
	}
	if len(runtime.created) != 1 {
		t.Fatalf("expected one create request, got %d", len(runtime.created))
	}
	req := runtime.created[0]
	if req.Image != model.DefaultEnvironmentImage {
		t.Fatalf("unexpected image: %q", req.Image)
	}
	if req.Shell != model.DefaultEnvironmentShell {
		t.Fatalf("unexpected shell: %q", req.Shell)
	}
	if req.NetworkProfile != model.NetworkProfileBridge {
		t.Fatalf("unexpected network profile: %q", req.NetworkProfile)
	}
	if req.NetworkMode != model.NetworkProfileBridge {
		t.Fatalf("unexpected network mode: %q", req.NetworkMode)
	}
	if req.Hostname != "lab" {
		t.Fatalf("unexpected hostname: %q", req.Hostname)
	}
	if runtime.started != "lab" {
		t.Fatalf("expected environment to start, got %q", runtime.started)
	}
	if env.State != "running" {
		t.Fatalf("expected refreshed running environment, got %q", env.State)
	}
}

func TestCreateEnvironmentRejectsInvalidName(t *testing.T) {
	eng := New(&fakeRuntime{})

	_, _, err := eng.CreateEnvironment(context.Background(), model.CreateEnvironmentRequest{
		Name: "bad name",
	})
	if err == nil {
		t.Fatal("expected invalid name error")
	}
}

func TestCreateEnvironmentRejectsInvalidEnvValue(t *testing.T) {
	eng := New(&fakeRuntime{})

	_, _, err := eng.CreateEnvironment(context.Background(), model.CreateEnvironmentRequest{
		Name: "lab",
		Env:  []string{"TOKEN"},
	})
	if err == nil {
		t.Fatal("expected invalid env value error")
	}
}

func TestCreateEnvironmentEnsuresDefaultWorkspace(t *testing.T) {
	runtime := &fakeRuntime{}
	workspaces := &fakeWorkspaces{}
	eng := New(runtime, WithWorkspaces(workspaces))

	_, _, err := eng.CreateEnvironment(context.Background(), model.CreateEnvironmentRequest{
		Name:          "lab",
		Pull:          false,
		Start:         false,
		WorkspaceName: "client-a",
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	if len(workspaces.ensured) != 1 {
		t.Fatalf("expected one workspace ensure, got %d", len(workspaces.ensured))
	}
	if workspaces.ensured[0].Name != "client-a" {
		t.Fatalf("unexpected workspace ensure request: %#v", workspaces.ensured[0])
	}
	if len(runtime.created) != 1 {
		t.Fatalf("expected one create request, got %d", len(runtime.created))
	}
	req := runtime.created[0]
	if req.WorkspacePath != "/tmp/workspaces/client-a" {
		t.Fatalf("unexpected workspace path: %q", req.WorkspacePath)
	}
	if len(req.Volumes) != 1 {
		t.Fatalf("expected workspace mount, got %#v", req.Volumes)
	}
	if got := req.Volumes[0]; got.Source != "/tmp/workspaces/client-a" || got.Target != model.DefaultWorkspaceMountPath {
		t.Fatalf("unexpected workspace mount: %#v", got)
	}
}

func TestCreateEnvironmentEnsuresSegmentNetwork(t *testing.T) {
	runtime := &fakeRuntime{}
	eng := New(runtime)

	_, _, err := eng.CreateEnvironment(context.Background(), model.CreateEnvironmentRequest{
		Name:           "lab",
		NetworkProfile: model.NetworkProfileSegment,
		Pull:           false,
		Start:          false,
	})
	if err != nil {
		t.Fatalf("create environment: %v", err)
	}
	if len(runtime.networks) != 1 {
		t.Fatalf("expected one network ensure, got %d", len(runtime.networks))
	}
	if runtime.networks[0].Name != "contaigen-lab" {
		t.Fatalf("unexpected network ensure: %#v", runtime.networks[0])
	}
	if runtime.created[0].NetworkMode != "contaigen-lab" {
		t.Fatalf("unexpected Docker network mode: %q", runtime.created[0].NetworkMode)
	}
}

func TestCreateEnvironmentUsesVPNGatewayNetworkNamespace(t *testing.T) {
	runtime := &fakeRuntime{
		inspectVPN: model.VPNGateway{
			Name:          "corp",
			ContainerName: "contaigen-vpn-corp",
			State:         "running",
		},
	}
	eng := New(runtime)

	_, warnings, err := eng.CreateEnvironment(context.Background(), model.CreateEnvironmentRequest{
		Name:           "lab",
		NetworkProfile: model.NetworkProfileVPN,
		VPNName:        "corp",
		Pull:           false,
		Start:          false,
	})
	if err != nil {
		t.Fatalf("create vpn environment: %v", err)
	}
	if len(runtime.created) != 1 {
		t.Fatalf("expected one create request, got %d", len(runtime.created))
	}
	req := runtime.created[0]
	if req.NetworkMode != "container:contaigen-vpn-corp" || req.NetworkName != "corp" || req.VPNName != "corp" {
		t.Fatalf("unexpected vpn network fields: %#v", req)
	}
	if req.Hostname != "" {
		t.Fatalf("vpn-routed environments should not set Docker hostname, got %q", req.Hostname)
	}
	if len(warnings) == 0 || !strings.Contains(strings.Join(warnings, "\n"), "host traffic is unchanged") {
		t.Fatalf("expected vpn routing warning, got %#v", warnings)
	}
}

func TestCreateEnvironmentRejectsVPNNameWithoutVPNNetworkProfile(t *testing.T) {
	runtime := &fakeRuntime{}
	eng := New(runtime)

	_, _, err := eng.CreateEnvironment(context.Background(), model.CreateEnvironmentRequest{
		Name:           "lab",
		NetworkProfile: model.NetworkProfileSegment,
		VPNName:        "corp",
		Pull:           false,
		Start:          false,
	})
	if err == nil {
		t.Fatal("expected vpn/network profile mismatch error")
	}
	if !strings.Contains(err.Error(), "requires vpn network profile") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateEnvironmentRejectsPortsWithVPNNetwork(t *testing.T) {
	runtime := &fakeRuntime{
		inspectVPN: model.VPNGateway{
			Name:          "corp",
			ContainerName: "contaigen-vpn-corp",
			State:         "running",
		},
	}
	eng := New(runtime)

	_, _, err := eng.CreateEnvironment(context.Background(), model.CreateEnvironmentRequest{
		Name:           "lab",
		NetworkProfile: model.NetworkProfileVPN,
		VPNName:        "corp",
		Ports: []model.PortMapping{{
			HostPort:      "8080",
			ContainerPort: "8080",
		}},
	})
	if err == nil {
		t.Fatal("expected vpn port publishing error")
	}
	if !strings.Contains(err.Error(), "publish ports on the VPN gateway") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateEnvironmentAppliesDesktopDefaults(t *testing.T) {
	runtime := &fakeRuntime{}
	eng := New(runtime)

	_, warnings, err := eng.CreateEnvironment(context.Background(), model.CreateEnvironmentRequest{
		Name: "desk",
		Desktop: model.DesktopConfig{
			Enabled: true,
		},
		Pull:  false,
		Start: false,
	})
	if err != nil {
		t.Fatalf("create desktop environment: %v", err)
	}
	if len(runtime.created) != 1 {
		t.Fatalf("expected one create request, got %d", len(runtime.created))
	}
	req := runtime.created[0]
	if req.Image != model.DefaultDesktopImage || !req.UseImageCommand || req.ShmSize != model.DefaultDesktopShmSize {
		t.Fatalf("desktop defaults not applied: %#v", req)
	}
	if !req.Desktop.Enabled || req.Desktop.Protocol != model.DefaultDesktopProtocol || req.Desktop.Password == "" {
		t.Fatalf("desktop config not populated: %#v", req.Desktop)
	}
	if !hasEnvKey(req.Env, model.DefaultDesktopPasswordEnv) {
		t.Fatalf("expected desktop password env, got %#v", req.Env)
	}
	if len(req.Ports) != 1 || req.Ports[0].HostIP != model.DefaultDesktopHostIP || req.Ports[0].HostPort != model.DefaultDesktopPort || req.Ports[0].ContainerPort != model.DefaultDesktopPort {
		t.Fatalf("expected default desktop port, got %#v", req.Ports)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "desktop mode switched image") {
		t.Fatalf("expected desktop image warning, got %#v", warnings)
	}
}

func TestCreateEnvironmentRejectsVPNDesktopWithoutGatewayPort(t *testing.T) {
	runtime := &fakeRuntime{
		inspectVPN: model.VPNGateway{
			Name:          "htb",
			ContainerName: "contaigen-vpn-htb",
			State:         "running",
		},
	}
	eng := New(runtime)

	_, _, err := eng.CreateEnvironment(context.Background(), model.CreateEnvironmentRequest{
		Name:           "desk",
		NetworkProfile: model.NetworkProfileVPN,
		VPNName:        "htb",
		Desktop: model.DesktopConfig{
			Enabled: true,
		},
		Pull:  false,
		Start: false,
	})
	if err == nil {
		t.Fatal("expected missing vpn desktop port error")
	}
	if !strings.Contains(err.Error(), "--port 127.0.0.1:6901:6901") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateEnvironmentAllowsVPNDesktopWithGatewayPort(t *testing.T) {
	runtime := &fakeRuntime{
		inspectVPN: model.VPNGateway{
			Name:          "htb",
			ContainerName: "contaigen-vpn-htb",
			State:         "running",
			Ports: []model.PortMapping{{
				HostIP:        "127.0.0.1",
				HostPort:      "6901",
				ContainerPort: "6901",
				Protocol:      "tcp",
			}},
		},
	}
	eng := New(runtime)

	_, _, err := eng.CreateEnvironment(context.Background(), model.CreateEnvironmentRequest{
		Name:           "desk",
		NetworkProfile: model.NetworkProfileVPN,
		VPNName:        "htb",
		Desktop: model.DesktopConfig{
			Enabled: true,
		},
		Pull:  false,
		Start: false,
	})
	if err != nil {
		t.Fatalf("create vpn desktop environment: %v", err)
	}
	if len(runtime.created) != 1 {
		t.Fatalf("expected one create request, got %d", len(runtime.created))
	}
	req := runtime.created[0]
	if req.NetworkMode != "container:contaigen-vpn-htb" || len(req.Ports) != 0 || !req.Desktop.Enabled {
		t.Fatalf("unexpected vpn desktop request: %#v", req)
	}
}

func TestCreateVPNGatewayAppliesOpenVPNDefaults(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "corp.ovpn")
	if err := os.WriteFile(configPath, []byte("client\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	runtime := &fakeRuntime{}
	eng := New(runtime)

	vpn, _, err := eng.CreateVPNGateway(context.Background(), model.CreateVPNGatewayRequest{
		Name:       "corp",
		ConfigPath: configPath,
		Pull:       true,
		Start:      true,
	})
	if err != nil {
		t.Fatalf("create vpn gateway: %v", err)
	}
	if runtime.pulled != model.DefaultVPNImage {
		t.Fatalf("expected vpn image pull, got %q", runtime.pulled)
	}
	if len(runtime.createdVPNs) != 1 {
		t.Fatalf("expected one vpn create request, got %d", len(runtime.createdVPNs))
	}
	req := runtime.createdVPNs[0]
	if req.Image != model.DefaultVPNImage || req.Provider != model.DefaultVPNProvider {
		t.Fatalf("unexpected vpn defaults: %#v", req)
	}
	if !hasCapability(req.CapAdd, "NET_ADMIN") || !hasDevice(req.Devices, "/dev/net/tun") {
		t.Fatalf("expected net admin/tun defaults: %#v %#v", req.CapAdd, req.Devices)
	}
	if !hasEnvKey(req.Env, "VPN_FILES") {
		t.Fatalf("expected VPN_FILES env from config file: %#v", req.Env)
	}
	if runtime.startedVPN != "corp" {
		t.Fatalf("expected vpn start, got %q", runtime.startedVPN)
	}
	if vpn.State != "running" {
		t.Fatalf("expected refreshed vpn, got %q", vpn.State)
	}
}

func TestCreateVPNGatewaySplitModeUsesRoutesFromConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "htb.ovpn")
	if err := os.WriteFile(configPath, []byte(`client
route 10.10.10.0 255.255.255.0
route 10.129.0.0 255.255.0.0
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	runtime := &fakeRuntime{}
	eng := New(runtime)

	_, warnings, err := eng.CreateVPNGateway(context.Background(), model.CreateVPNGatewayRequest{
		Name:       "htb",
		ConfigPath: configPath,
		RouteMode:  model.VPNRouteModeSplit,
		Pull:       false,
		Start:      false,
	})
	if err != nil {
		t.Fatalf("create split vpn gateway: %v", err)
	}
	if len(runtime.createdVPNs) != 1 {
		t.Fatalf("expected one vpn create request, got %d", len(runtime.createdVPNs))
	}
	req := runtime.createdVPNs[0]
	if req.RouteMode != model.VPNRouteModeSplit {
		t.Fatalf("unexpected route mode: %#v", req)
	}
	if len(req.Routes) != 2 || req.Routes[0].CIDR != "10.10.10.0/24" || req.Routes[1].CIDR != "10.129.0.0/16" {
		t.Fatalf("unexpected split routes: %#v", req.Routes)
	}
	if !hasEnvKey(req.Env, "DEFAULT_GATEWAY") || !hasEnvKey(req.Env, "OTHER_ARGS") {
		t.Fatalf("expected split route env options: %#v", req.Env)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "10.10.10.0/24") {
		t.Fatalf("expected route warning, got %#v", warnings)
	}
}

func TestCreateVPNGatewaySplitModeAllowsServerPushedRoutes(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "academy.ovpn")
	if err := os.WriteFile(configPath, []byte(`client
dev tun
remote edge-us-academy-6.hackthebox.eu 1337
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	runtime := &fakeRuntime{}
	eng := New(runtime)

	_, warnings, err := eng.CreateVPNGateway(context.Background(), model.CreateVPNGatewayRequest{
		Name:       "academy",
		ConfigPath: configPath,
		RouteMode:  model.VPNRouteModeSplit,
		Pull:       false,
		Start:      false,
	})
	if err != nil {
		t.Fatalf("create split vpn gateway: %v", err)
	}
	if len(runtime.createdVPNs) != 1 {
		t.Fatalf("expected one vpn create request, got %d", len(runtime.createdVPNs))
	}
	req := runtime.createdVPNs[0]
	if len(req.Routes) != 0 {
		t.Fatalf("expected no static split routes, got %#v", req.Routes)
	}
	envText := strings.Join(req.Env, "\n")
	if !strings.Contains(envText, "DEFAULT_GATEWAY=false") || !strings.Contains(envText, "--pull-filter ignore redirect-gateway") {
		t.Fatalf("expected pushed-route split env options: %#v", req.Env)
	}
	if strings.Contains(envText, "--route-nopull") {
		t.Fatalf("server-pushed split mode should not add route-nopull: %#v", req.Env)
	}
	if !strings.Contains(strings.Join(warnings, "\n"), "server-pushed VPN routes") {
		t.Fatalf("expected server-pushed route warning, got %#v", warnings)
	}
}

func TestCreateServiceAttachesToEnvironmentNetwork(t *testing.T) {
	runtime := &fakeRuntime{
		inspectEnv: model.Environment{
			Name:           "lab",
			NetworkProfile: model.NetworkProfileSegment,
			NetworkName:    "client-a",
			NetworkMode:    "client-a",
		},
	}
	eng := New(runtime)

	service, _, err := eng.CreateService(context.Background(), model.CreateServiceRequest{
		EnvironmentName: "lab",
		Name:            "target",
		Image:           "nginx:alpine",
		Pull:            true,
		Start:           true,
	})
	if err != nil {
		t.Fatalf("create service: %v", err)
	}
	if runtime.pulled != "nginx:alpine" {
		t.Fatalf("expected service image pull, got %q", runtime.pulled)
	}
	if len(runtime.createdServices) != 1 {
		t.Fatalf("expected one service create request, got %d", len(runtime.createdServices))
	}
	req := runtime.createdServices[0]
	if req.NetworkName != "client-a" || req.NetworkAlias != "target" {
		t.Fatalf("unexpected service network fields: %#v", req)
	}
	if runtime.startedService != "lab/target" {
		t.Fatalf("expected service start, got %q", runtime.startedService)
	}
	if service.State != "running" {
		t.Fatalf("expected refreshed running service, got %q", service.State)
	}
}

func TestCreateServiceRequiresUserDefinedEnvironmentNetwork(t *testing.T) {
	eng := New(&fakeRuntime{})

	_, _, err := eng.CreateService(context.Background(), model.CreateServiceRequest{
		EnvironmentName: "lab",
		Name:            "target",
		Image:           "nginx:alpine",
	})
	if err == nil {
		t.Fatal("expected service create to require a segment network")
	}
	if !strings.Contains(err.Error(), "--network segment") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNukeBacksUpAndRemovesManagedResources(t *testing.T) {
	runtime := &fakeRuntime{
		listServices: []model.Service{{
			EnvironmentName: "lab",
			Name:            "target",
			State:           "running",
		}},
		listEnvs: []model.Environment{{
			Name:          "lab",
			State:         "running",
			WorkspaceName: "client-a",
			WorkspacePath: "/tmp/workspaces/client-a",
		}},
		listVPNs: []model.VPNGateway{{
			Name:  "htb",
			State: "running",
		}},
		listNetworks: []model.Network{{
			Name: "contaigen-lab",
		}},
	}
	workspaces := &fakeWorkspaces{
		items: []model.Workspace{{
			Name: "client-a",
			Path: "/tmp/workspaces/client-a",
		}},
	}
	eng := New(runtime, WithWorkspaces(workspaces))

	result, err := eng.Nuke(context.Background(), model.NukeRequest{
		BackupWorkspaces: true,
		BackupPassword:   "secret",
		StopTimeout:      1,
	})
	if err != nil {
		t.Fatalf("nuke: %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected no nuke errors, got %#v", result.Errors)
	}
	if len(workspaces.backups) != 1 || workspaces.backups[0].Name != "client-a" || workspaces.backups[0].Password != "secret" {
		t.Fatalf("workspace backup not requested: %#v", workspaces.backups)
	}
	if strings.Join(runtime.stoppedServices, ",") != "lab/target" || strings.Join(runtime.removedServices, ",") != "lab/target" {
		t.Fatalf("service not stopped/removed: stopped=%#v removed=%#v", runtime.stoppedServices, runtime.removedServices)
	}
	if strings.Join(runtime.stoppedEnvs, ",") != "lab" || strings.Join(runtime.removedEnvs, ",") != "lab" {
		t.Fatalf("environment not stopped/removed: stopped=%#v removed=%#v", runtime.stoppedEnvs, runtime.removedEnvs)
	}
	if strings.Join(runtime.stoppedVPNs, ",") != "htb" || strings.Join(runtime.removedVPNs, ",") != "htb" {
		t.Fatalf("vpn not stopped/removed: stopped=%#v removed=%#v", runtime.stoppedVPNs, runtime.removedVPNs)
	}
	if strings.Join(runtime.removedNetworks, ",") != "contaigen-lab" {
		t.Fatalf("network not removed: %#v", runtime.removedNetworks)
	}
	if len(workspaces.removed) != 1 || workspaces.removed[0].Name != "client-a" {
		t.Fatalf("workspace not removed: %#v", workspaces.removed)
	}
	if len(result.WorkspaceBackups) != 1 || !result.WorkspaceBackups[0].Encrypted {
		t.Fatalf("expected encrypted workspace backup in result: %#v", result.WorkspaceBackups)
	}
}

type fakeRuntime struct {
	pulled          string
	created         []model.CreateEnvironmentRequest
	createdServices []model.CreateServiceRequest
	createdVPNs     []model.CreateVPNGatewayRequest
	started         string
	startedService  string
	startedVPN      string
	networks        []model.EnsureNetworkRequest
	listNetworks    []model.Network
	removedNetworks []string
	listEnvs        []model.Environment
	listServices    []model.Service
	listVPNs        []model.VPNGateway
	stoppedEnvs     []string
	removedEnvs     []string
	stoppedServices []string
	removedServices []string
	stoppedVPNs     []string
	removedVPNs     []string
	inspectEnv      model.Environment
	inspectVPN      model.VPNGateway
}

func (f *fakeRuntime) EnsureImage(_ context.Context, image string) error {
	f.pulled = image
	return nil
}

func (f *fakeRuntime) CreateEnvironment(_ context.Context, req model.CreateEnvironmentRequest) (model.Environment, []string, error) {
	f.created = append(f.created, req)
	return model.Environment{
		ID:             "1234567890abcdef",
		Name:           req.Name,
		Image:          req.Image,
		State:          "created",
		Shell:          req.Shell,
		User:           req.User,
		Hostname:       req.Hostname,
		NetworkProfile: req.NetworkProfile,
		NetworkName:    req.NetworkName,
		NetworkMode:    req.NetworkMode,
		VPNName:        req.VPNName,
		Desktop:        req.Desktop,
		Ports:          req.Ports,
		CapAdd:         req.CapAdd,
	}, nil, nil
}

func (f *fakeRuntime) EnsureNetwork(_ context.Context, req model.EnsureNetworkRequest) (model.Network, []string, error) {
	f.networks = append(f.networks, req)
	return model.Network{
		ID:         "network123456",
		Name:       req.Name,
		Driver:     req.Driver,
		Profile:    req.Profile,
		Internal:   req.Internal,
		Attachable: req.Attachable,
	}, nil, nil
}

func (f *fakeRuntime) ListNetworks(context.Context) ([]model.Network, error) {
	return f.listNetworks, nil
}

func (f *fakeRuntime) InspectNetwork(_ context.Context, name string) (model.Network, error) {
	return model.Network{Name: name}, nil
}

func (f *fakeRuntime) RemoveNetwork(_ context.Context, name string) error {
	f.removedNetworks = append(f.removedNetworks, name)
	return nil
}

func (f *fakeRuntime) NetworkMap(context.Context) (model.NetworkMap, error) {
	return model.NetworkMap{}, nil
}

func (f *fakeRuntime) CreateVPNGateway(_ context.Context, req model.CreateVPNGatewayRequest) (model.VPNGateway, []string, error) {
	f.createdVPNs = append(f.createdVPNs, req)
	return model.VPNGateway{
		ID:              "vpn123456",
		Name:            req.Name,
		ContainerName:   "contaigen-vpn-" + req.Name,
		Image:           req.Image,
		Provider:        req.Provider,
		RouteMode:       req.RouteMode,
		Routes:          req.Routes,
		State:           "created",
		ConfigPath:      req.ConfigPath,
		ConfigMountPath: req.ConfigMountPath,
		Env:             req.Env,
		NoVNCPorts:      req.NoVNCPorts,
		CapAdd:          req.CapAdd,
		Devices:         req.Devices,
	}, nil, nil
}

func (f *fakeRuntime) ListVPNGateways(context.Context) ([]model.VPNGateway, error) {
	return f.listVPNs, nil
}

func (f *fakeRuntime) InspectVPNGateway(_ context.Context, name string) (model.VPNGateway, error) {
	if f.inspectVPN.Name != "" {
		return f.inspectVPN, nil
	}
	return model.VPNGateway{
		ID:            "vpn123456",
		Name:          name,
		ContainerName: "contaigen-vpn-" + name,
		Image:         model.DefaultVPNImage,
		Provider:      model.DefaultVPNProvider,
		RouteMode:     model.VPNRouteModeFull,
		State:         "running",
	}, nil
}

func (f *fakeRuntime) CreateService(_ context.Context, req model.CreateServiceRequest) (model.Service, []string, error) {
	f.createdServices = append(f.createdServices, req)
	return model.Service{
		ID:              "service123456",
		Name:            req.Name,
		EnvironmentName: req.EnvironmentName,
		Image:           req.Image,
		State:           "created",
		NetworkName:     req.NetworkName,
		NetworkAlias:    req.NetworkAlias,
	}, nil, nil
}

func (f *fakeRuntime) ListServices(context.Context, string) ([]model.Service, error) {
	return f.listServices, nil
}

func (f *fakeRuntime) InspectService(_ context.Context, envName string, serviceName string) (model.Service, error) {
	return model.Service{
		ID:              "service123456",
		Name:            serviceName,
		EnvironmentName: envName,
		Image:           "nginx:alpine",
		State:           "running",
		NetworkName:     "contaigen-" + envName,
		NetworkAlias:    serviceName,
	}, nil
}

func (f *fakeRuntime) ListEnvironments(context.Context) ([]model.Environment, error) {
	return f.listEnvs, nil
}

func (f *fakeRuntime) InspectEnvironment(_ context.Context, name string) (model.Environment, error) {
	if f.inspectEnv.Name != "" {
		return f.inspectEnv, nil
	}
	return model.Environment{
		ID:          "1234567890abcdef",
		Name:        name,
		Image:       model.DefaultEnvironmentImage,
		State:       "running",
		Shell:       model.DefaultEnvironmentShell,
		Hostname:    name,
		NetworkMode: model.DefaultNetworkMode,
	}, nil
}

func (f *fakeRuntime) StartEnvironment(_ context.Context, name string) error {
	f.started = name
	return nil
}

func (f *fakeRuntime) StopEnvironment(_ context.Context, name string, _ *int) error {
	f.stoppedEnvs = append(f.stoppedEnvs, name)
	return nil
}

func (f *fakeRuntime) RemoveEnvironment(_ context.Context, name string, _ model.RemoveEnvironmentRequest) error {
	f.removedEnvs = append(f.removedEnvs, name)
	return nil
}

func (f *fakeRuntime) EnterEnvironment(context.Context, string, model.EnterEnvironmentRequest, model.ExecIO) error {
	return nil
}

func (f *fakeRuntime) StartService(_ context.Context, envName string, serviceName string) error {
	f.startedService = envName + "/" + serviceName
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

func (f *fakeRuntime) StartVPNGateway(_ context.Context, name string) error {
	f.startedVPN = name
	f.inspectVPN = model.VPNGateway{
		ID:            "vpn123456",
		Name:          name,
		ContainerName: "contaigen-vpn-" + name,
		Image:         model.DefaultVPNImage,
		Provider:      model.DefaultVPNProvider,
		RouteMode:     model.VPNRouteModeFull,
		State:         "running",
	}
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

func (f *fakeRuntime) VPNGatewayLogs(context.Context, string, model.VPNLogsRequest, model.VPNLogIO) error {
	return nil
}

func (f *fakeRuntime) Close() error {
	return nil
}

type fakeWorkspaces struct {
	ensured []model.EnsureWorkspaceRequest
	items   []model.Workspace
	backups []model.BackupWorkspaceRequest
	removed []model.RemoveWorkspaceRequest
}

func (f *fakeWorkspaces) Ensure(_ context.Context, req model.EnsureWorkspaceRequest) (model.Workspace, error) {
	f.ensured = append(f.ensured, req)
	path := req.Path
	if path == "" {
		path = "/tmp/workspaces/" + req.Name
	}
	return model.Workspace{
		Name: req.Name,
		Path: path,
	}, nil
}

func (f *fakeWorkspaces) List(context.Context) ([]model.Workspace, error) {
	return f.items, nil
}

func (f *fakeWorkspaces) Backup(_ context.Context, req model.BackupWorkspaceRequest) (model.WorkspaceBackup, error) {
	f.backups = append(f.backups, req)
	return model.WorkspaceBackup{
		Workspace: model.Workspace{Name: req.Name, Path: "/tmp/workspaces/" + req.Name},
		Path:      "/tmp/backups/" + req.Name + ".tar.gz",
		Encrypted: req.Password != "",
	}, nil
}

func (f *fakeWorkspaces) Remove(_ context.Context, req model.RemoveWorkspaceRequest) (model.WorkspaceRemove, error) {
	f.removed = append(f.removed, req)
	return model.WorkspaceRemove{
		Workspace: model.Workspace{Name: req.Name, Path: req.Path},
	}, nil
}
