package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NorthernReach/contaigen/internal/dockerx"
	"github.com/NorthernReach/contaigen/internal/model"
)

func TestVPNCreateCommand(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "corp.ovpn")
	if err := os.WriteFile(configPath, []byte("client\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("CONTAIGEN_VPN_AUTH", "user;password")
	runtime := &fakeRuntime{}

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "vpn", "create", "corp", "--config", configPath, "--secret-env", "VPN_AUTH=CONTAIGEN_VPN_AUTH", "--port", "127.0.0.1:8080:8080")
	if err != nil {
		t.Fatalf("vpn create failed: %v", err)
	}
	for _, want := range []string{"Created VPN gateway corp", "Image: dperson/openvpn-client", "Started VPN gateway corp"} {
		if !strings.Contains(output, want) {
			t.Fatalf("vpn create output missing %q:\n%s", want, output)
		}
	}
	if len(runtime.createdVPNs) != 1 {
		t.Fatalf("expected one vpn create request, got %d", len(runtime.createdVPNs))
	}
	req := runtime.createdVPNs[0]
	if req.Name != "corp" || req.Image != model.DefaultVPNImage || req.Provider != model.DefaultVPNProvider {
		t.Fatalf("unexpected vpn request identity: %#v", req)
	}
	if !containsString(req.Env, "VPN_AUTH=user;password") || !hasEnvPrefix(req.Env, "VPN_FILES=") {
		t.Fatalf("expected VPN_AUTH and VPN_FILES env: %#v", req.Env)
	}
	if !containsString(req.CapAdd, "NET_ADMIN") || len(req.Devices) != 1 || req.Devices[0].HostPath != "/dev/net/tun" {
		t.Fatalf("expected default tun permissions: %#v %#v", req.CapAdd, req.Devices)
	}
	if got := req.Ports[0]; got.HostIP != "127.0.0.1" || got.HostPort != "8080" || got.ContainerPort != "8080" {
		t.Fatalf("unexpected port mapping: %#v", got)
	}
}

func TestVPNCreateCommandReadsEnvFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "corp.ovpn")
	if err := os.WriteFile(configPath, []byte("client\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	envPath := filepath.Join(t.TempDir(), "vpn.env")
	if err := os.WriteFile(envPath, []byte("FROM_FILE=true\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	runtime := &fakeRuntime{}

	_, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "vpn", "create", "corp", "--config", configPath, "--env-file", envPath, "--env", "FROM_CLI=true", "--no-start")
	if err != nil {
		t.Fatalf("vpn create failed: %v", err)
	}
	if len(runtime.createdVPNs) != 1 {
		t.Fatalf("expected one vpn create request, got %d", len(runtime.createdVPNs))
	}
	if !containsString(runtime.createdVPNs[0].Env, "FROM_FILE=true") || !containsString(runtime.createdVPNs[0].Env, "FROM_CLI=true") {
		t.Fatalf("expected env file and cli env values: %#v", runtime.createdVPNs[0].Env)
	}
}

func TestVPNCreateCommandDefaultVNCFlag(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "corp.ovpn")
	if err := os.WriteFile(configPath, []byte("client\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	runtime := &fakeRuntime{}

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "vpn", "create", "corp", "--config", configPath, "--vnc", "--no-start")
	if err != nil {
		t.Fatalf("vpn create failed: %v", err)
	}
	if !strings.Contains(output, "NoVNC: 127.0.0.1:6901:6901/tcp") {
		t.Fatalf("vpn create output missing noVNC port:\n%s", output)
	}
	if len(runtime.createdVPNs) != 1 {
		t.Fatalf("expected one vpn create request, got %d", len(runtime.createdVPNs))
	}
	req := runtime.createdVPNs[0]
	if len(req.NoVNCPorts) != 1 {
		t.Fatalf("expected one noVNC port, got %#v", req.NoVNCPorts)
	}
	if got := req.NoVNCPorts[0]; got.HostIP != "127.0.0.1" || got.HostPort != "6901" || got.ContainerPort != "6901" || got.Protocol != "tcp" {
		t.Fatalf("unexpected noVNC mapping: %#v", got)
	}
	if len(req.Ports) != 1 || req.Ports[0] != req.NoVNCPorts[0] {
		t.Fatalf("expected noVNC mapping to be published: ports=%#v novnc=%#v", req.Ports, req.NoVNCPorts)
	}
}

func TestVPNCreateCommandVNCFlagAcceptsCommaPorts(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "corp.ovpn")
	if err := os.WriteFile(configPath, []byte("client\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	runtime := &fakeRuntime{}

	_, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "vpn", "create", "corp", "--config", configPath, "--vnc", "6901,6902,6903", "--no-start")
	if err != nil {
		t.Fatalf("vpn create failed: %v", err)
	}
	req := runtime.createdVPNs[0]
	if len(req.NoVNCPorts) != 3 {
		t.Fatalf("expected three noVNC ports, got %#v", req.NoVNCPorts)
	}
	for index, want := range []string{"6901", "6902", "6903"} {
		got := req.NoVNCPorts[index]
		if got.HostIP != "127.0.0.1" || got.HostPort != want || got.ContainerPort != want {
			t.Fatalf("unexpected noVNC mapping %d: %#v", index, got)
		}
	}
}

func TestVPNCreateCommandSplitModePreparesManagedConfig(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "htb.ovpn")
	if err := os.WriteFile(configPath, []byte(`client
route 10.10.10.0 255.255.255.0
redirect-gateway def1
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	runtime := &fakeRuntime{}

	output, err := execute(t, NewRootCommand(Options{
		Paths: fixedPaths,
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "vpn", "create", "htb", "--config", configPath, "--route-mode", "split", "--no-start")
	if err != nil {
		t.Fatalf("vpn create split failed: %v", err)
	}
	for _, want := range []string{"split route mode discovered VPN routes: 10.10.10.0/24", "Route mode: split", "Routes: 10.10.10.0/24"} {
		if !strings.Contains(output, want) {
			t.Fatalf("vpn split output missing %q:\n%s", want, output)
		}
	}
	if len(runtime.createdVPNs) != 1 {
		t.Fatalf("expected one vpn create request, got %d", len(runtime.createdVPNs))
	}
	req := runtime.createdVPNs[0]
	if req.ConfigPath == configPath || !strings.Contains(req.ConfigPath, "/tmp/contaigen/data/vpn/htb/split/htb.ovpn") {
		t.Fatalf("expected managed split config path, got %q", req.ConfigPath)
	}
	if req.RouteMode != model.VPNRouteModeSplit || len(req.Routes) != 1 || req.Routes[0].CIDR != "10.10.10.0/24" {
		t.Fatalf("unexpected split request: %#v", req)
	}
	data, err := os.ReadFile(req.ConfigPath)
	if err != nil {
		t.Fatalf("read managed split config: %v", err)
	}
	if !strings.Contains(string(data), "route-nopull") || !strings.Contains(string(data), "managed OpenVPN config disabled: redirect-gateway def1") {
		t.Fatalf("managed config did not disable default route:\n%s", string(data))
	}
}

func TestVPNCreateCommandSplitModeAllowsServerPushedRoutes(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "academy.ovpn")
	if err := os.WriteFile(configPath, []byte(`client
dev tun
remote edge-us-academy-6.hackthebox.eu 1337
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	runtime := &fakeRuntime{}

	output, err := execute(t, NewRootCommand(Options{
		Paths: fixedPaths,
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "vpn", "create", "academy", "--config", configPath, "--route-mode", "split", "--no-start")
	if err != nil {
		t.Fatalf("vpn create split failed: %v", err)
	}
	for _, want := range []string{"server-pushed routes", "Route mode: split"} {
		if !strings.Contains(output, want) {
			t.Fatalf("vpn split output missing %q:\n%s", want, output)
		}
	}
	if len(runtime.createdVPNs) != 1 {
		t.Fatalf("expected one vpn create request, got %d", len(runtime.createdVPNs))
	}
	req := runtime.createdVPNs[0]
	if req.ConfigPath == configPath || !strings.Contains(req.ConfigPath, "/tmp/contaigen/data/vpn/academy/split/academy.ovpn") {
		t.Fatalf("expected managed split config path, got %q", req.ConfigPath)
	}
	if req.RouteMode != model.VPNRouteModeSplit || len(req.Routes) != 0 {
		t.Fatalf("unexpected split request: %#v", req)
	}
	data, err := os.ReadFile(req.ConfigPath)
	if err != nil {
		t.Fatalf("read managed split config: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "route-nopull") || !strings.Contains(text, "pull-filter ignore \"redirect-gateway\"") {
		t.Fatalf("managed config should accept pushed routes while blocking default route:\n%s", text)
	}
}

func TestVPNListCommand(t *testing.T) {
	runtime := &fakeRuntime{
		vpns: []model.VPNGateway{
			{
				ID:       "vpn1234567890",
				Name:     "corp",
				Image:    model.DefaultVPNImage,
				Provider: model.DefaultVPNProvider,
				State:    "running",
			},
		},
	}

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "vpn", "list")
	if err != nil {
		t.Fatalf("vpn list failed: %v", err)
	}
	for _, want := range []string{"NAME", "corp", "running", model.DefaultVPNImage} {
		if !strings.Contains(output, want) {
			t.Fatalf("vpn list output missing %q:\n%s", want, output)
		}
	}
}

func TestEnvCreateCommandSupportsVPNNetwork(t *testing.T) {
	runtime := &fakeRuntime{}

	_, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "env", "create", "lab", "--network", "vpn", "--vpn", "corp")
	if err != nil {
		t.Fatalf("env create failed: %v", err)
	}
	if len(runtime.created) != 1 {
		t.Fatalf("expected one create request, got %d", len(runtime.created))
	}
	req := runtime.created[0]
	if req.NetworkProfile != model.NetworkProfileVPN || req.VPNName != "corp" || req.NetworkMode != "container:contaigen-vpn-corp" {
		t.Fatalf("unexpected vpn network request: %#v", req)
	}
}

func TestEnvCreateCommandVPNFlagInfersVPNNetwork(t *testing.T) {
	runtime := &fakeRuntime{}

	_, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "env", "create", "lab", "--profile", "parrot-default", "--vpn", "corp")
	if err != nil {
		t.Fatalf("env create failed: %v", err)
	}
	if len(runtime.created) != 1 {
		t.Fatalf("expected one create request, got %d", len(runtime.created))
	}
	req := runtime.created[0]
	if req.NetworkProfile != model.NetworkProfileVPN || req.VPNName != "corp" || req.NetworkMode != "container:contaigen-vpn-corp" {
		t.Fatalf("unexpected inferred vpn network request: %#v", req)
	}
}

func TestParseDeviceMapping(t *testing.T) {
	device, err := parseDeviceMapping("/dev/net/tun:/dev/net/tun:rwm")
	if err != nil {
		t.Fatalf("parse device: %v", err)
	}
	if device.HostPath != "/dev/net/tun" || device.ContainerPath != "/dev/net/tun" || device.Permissions != "rwm" {
		t.Fatalf("unexpected device: %#v", device)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func hasEnvPrefix(values []string, prefix string) bool {
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
