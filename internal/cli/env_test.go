package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NorthernReach/contaigen/internal/config"
	"github.com/NorthernReach/contaigen/internal/dockerx"
	"github.com/NorthernReach/contaigen/internal/model"
)

func TestEnvCreateCommandRejectsWorkspaceVolumeConflict(t *testing.T) {
	runtime := &fakeRuntime{}
	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "env", "create", "web", "--image", "parrotsec/security", "--shell", "/bin/zsh", "--hostname", "bird", "--network", "bridge", "--env", "TOKEN=value", "--port", "127.0.0.1:8080:80/tcp", "--volume", "/tmp/work:/workspace:ro")
	if err == nil {
		t.Fatal("expected workspace target conflict")
	}
	if !strings.Contains(output, "ERR Create environment web") {
		t.Fatalf("expected progress failure output, got:\n%s", output)
	}
}

func TestEnvCreateCommandAddsDefaultWorkspace(t *testing.T) {
	runtime := &fakeRuntime{}
	workspaces := &fakeWorkspaceManager{}

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(workspaces),
	}), "env", "create", "web", "--image", "parrotsec/security", "--shell", "/bin/zsh", "--user", "root", "--hostname", "bird", "--network", "bridge", "--cap-add", "NET_ADMIN", "--env", "TOKEN=value", "--port", "127.0.0.1:8080:80/tcp", "--volume", "/tmp/tools:/opt/tools:ro")
	if err != nil {
		t.Fatalf("env create failed: %v", err)
	}
	for _, want := range []string{
		"Preparing environment web",
		"Ensuring image parrotsec/security",
		"Creating environment container web",
		"Started environment web",
		"Created environment web",
		"Workspace: /tmp/contaigen/workspaces/web -> /workspace",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
	if len(runtime.created) != 1 {
		t.Fatalf("expected one create request, got %d", len(runtime.created))
	}

	req := runtime.created[0]
	if req.Name != "web" || req.Image != "parrotsec/security" || req.Shell != "/bin/zsh" || req.User != "root" || req.Hostname != "bird" {
		t.Fatalf("unexpected create request: %#v", req)
	}
	if !req.Start {
		t.Fatal("expected create command to start by default")
	}
	if req.WorkspaceName != "web" || req.WorkspacePath != "/tmp/contaigen/workspaces/web" || req.WorkspaceMountPath != "/workspace" {
		t.Fatalf("unexpected workspace fields: %#v", req)
	}
	if req.NetworkProfile != model.NetworkProfileBridge || req.NetworkMode != model.NetworkProfileBridge {
		t.Fatalf("unexpected network fields: %#v", req)
	}
	if !containsString(req.CapAdd, "NET_ADMIN") {
		t.Fatalf("expected capability add, got %#v", req.CapAdd)
	}
	if got := req.Ports[0]; got.HostIP != "127.0.0.1" || got.HostPort != "8080" || got.ContainerPort != "80" || got.Protocol != "tcp" {
		t.Fatalf("unexpected port mapping: %#v", got)
	}
	if got := req.Volumes[0]; got.Source != "/tmp/contaigen/workspaces/web" || got.Target != "/workspace" || got.ReadOnly {
		t.Fatalf("unexpected workspace mount: %#v", got)
	}
	if got := req.Volumes[1]; got.Source != "/tmp/tools" || got.Target != "/opt/tools" || !got.ReadOnly {
		t.Fatalf("unexpected volume mount: %#v", got)
	}
}

func TestEnvCreateCommandReadsEnvFile(t *testing.T) {
	runtime := &fakeRuntime{}
	envPath := filepath.Join(t.TempDir(), "lab.env")
	if err := os.WriteFile(envPath, []byte("FROM_FILE=true\nQUOTED=\"hello world\"\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	_, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "env", "create", "lab", "--env-file", envPath, "--env", "FROM_CLI=true")
	if err != nil {
		t.Fatalf("env create failed: %v", err)
	}
	if len(runtime.created) != 1 {
		t.Fatalf("expected one create request, got %d", len(runtime.created))
	}
	if strings.Join(runtime.created[0].Env, ",") != "FROM_FILE=true,QUOTED=hello world,FROM_CLI=true" {
		t.Fatalf("unexpected env values: %#v", runtime.created[0].Env)
	}
}

func TestEnvCreateCommandSupportsSegmentNetwork(t *testing.T) {
	runtime := &fakeRuntime{}

	_, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "env", "create", "lab", "--network", "segment", "--network-name", "client-a")
	if err != nil {
		t.Fatalf("env create failed: %v", err)
	}
	if len(runtime.networkEnsures) != 1 {
		t.Fatalf("expected one network ensure, got %d", len(runtime.networkEnsures))
	}
	if runtime.networkEnsures[0].Name != "client-a" {
		t.Fatalf("unexpected network ensure: %#v", runtime.networkEnsures[0])
	}
	if runtime.created[0].NetworkMode != "client-a" || runtime.created[0].NetworkProfile != model.NetworkProfileSegment {
		t.Fatalf("unexpected create request: %#v", runtime.created[0])
	}
}

func TestEnvCreateCommandAppliesProfile(t *testing.T) {
	runtime := &fakeRuntime{}
	profiles := &fakeProfileManager{
		profiles: map[string]model.EnvironmentProfile{
			"alpine-appsec": {
				APIVersion: model.ProfileAPIVersion,
				Kind:       model.ProfileKind,
				Metadata: model.EnvironmentProfileMeta{
					Name: "alpine-appsec",
				},
				Spec: model.EnvironmentProfileSpec{
					Image: "alpine:latest",
					Shell: "/bin/sh",
					Network: model.ProfileNetwork{
						Profile: model.NetworkProfileSegment,
						Name:    "appsec",
					},
					Workspace: model.ProfileWorkspace{
						MountPath: "/work",
					},
					CapAdd: []string{"NET_ADMIN"},
					Env:    []string{"FROM_PROFILE=true"},
				},
			},
		},
	}

	_, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
		NewProfileStore:   fakeProfileStore(profiles),
	}), "env", "create", "lab", "--profile", "alpine-appsec", "--env", "FROM_CLI=true")
	if err != nil {
		t.Fatalf("env create with profile failed: %v", err)
	}
	if len(runtime.created) != 1 {
		t.Fatalf("expected one create request, got %d", len(runtime.created))
	}
	req := runtime.created[0]
	if req.Image != "alpine:latest" || req.Shell != "/bin/sh" {
		t.Fatalf("profile image/shell not applied: %#v", req)
	}
	if req.NetworkProfile != model.NetworkProfileSegment || req.NetworkName != "appsec" || req.NetworkMode != "appsec" {
		t.Fatalf("profile network not applied: %#v", req)
	}
	if req.WorkspaceMountPath != "/work" {
		t.Fatalf("profile workspace mount not applied: %#v", req)
	}
	if !containsString(req.CapAdd, "NET_ADMIN") {
		t.Fatalf("profile capabilities not applied: %#v", req)
	}
	if strings.Join(req.Env, ",") != "FROM_PROFILE=true,FROM_CLI=true" {
		t.Fatalf("unexpected env merge: %#v", req.Env)
	}
}

func TestEnvCreateCommandAppliesDesktopProfile(t *testing.T) {
	runtime := &fakeRuntime{}
	profiles := &fakeProfileManager{
		profiles: map[string]model.EnvironmentProfile{
			"parrot-desktop": {
				APIVersion: model.ProfileAPIVersion,
				Kind:       model.ProfileKind,
				Metadata: model.EnvironmentProfileMeta{
					Name: "parrot-desktop",
				},
				Spec: model.EnvironmentProfileSpec{
					Image: model.DefaultDesktopImage,
					Shell: model.DefaultEnvironmentShell,
					User:  "root",
					Desktop: model.DesktopConfig{
						Enabled: true,
					},
				},
			},
		},
	}

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
		NewProfileStore:   fakeProfileStore(profiles),
	}), "env", "create", "desk", "--profile", "parrot-desktop", "--desktop-password", "secret")
	if err != nil {
		t.Fatalf("env create with desktop profile failed: %v", err)
	}
	if !strings.Contains(output, "Desktop URL: https://127.0.0.1:6901/") || !strings.Contains(output, "noVNC password: secret") {
		t.Fatalf("desktop connection details missing:\n%s", output)
	}
	if strings.Contains(output, "Linux sudo:") {
		t.Fatalf("env create should not print sudo note:\n%s", output)
	}
	if len(runtime.created) != 1 {
		t.Fatalf("expected one create request, got %d", len(runtime.created))
	}
	req := runtime.created[0]
	if req.User != "root" {
		t.Fatalf("parrot desktop root user not applied: %#v", req)
	}
	if !req.Desktop.Enabled || req.Desktop.Password != "secret" || !req.UseImageCommand {
		t.Fatalf("desktop request not applied: %#v", req)
	}
	if !containsString(req.Env, "VNC_PW=secret") {
		t.Fatalf("desktop password env missing: %#v", req.Env)
	}
	if len(req.Ports) != 1 || req.Ports[0].HostPort != model.DefaultDesktopPort || req.Ports[0].ContainerPort != model.DefaultDesktopPort {
		t.Fatalf("desktop port missing: %#v", req.Ports)
	}
}

func TestEnvInfoShowsUnavailableVPNDesktopPort(t *testing.T) {
	runtime := &fakeRuntime{
		envs: []model.Environment{{
			ID:             "1234567890abcdef",
			Name:           "desk",
			Image:          model.DefaultDesktopImage,
			State:          "running",
			NetworkProfile: model.NetworkProfileVPN,
			NetworkMode:    "container:contaigen-vpn-htb",
			VPNName:        "htb",
			Desktop: model.DesktopConfig{
				Enabled:       true,
				HostIP:        model.DefaultDesktopHostIP,
				HostPort:      model.DefaultDesktopPort,
				ContainerPort: model.DefaultDesktopPort,
				Scheme:        model.DefaultDesktopScheme,
				Path:          model.DefaultDesktopPath,
				User:          model.DefaultDesktopUser,
				Password:      "secret",
			},
		}},
	}

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "env", "info", "desk")
	if err != nil {
		t.Fatalf("env info failed: %v", err)
	}
	if !strings.Contains(output, "Desktop URL: unavailable (publish 127.0.0.1:6901:6901 on VPN gateway htb)") {
		t.Fatalf("expected unavailable desktop URL, got:\n%s", output)
	}
}

func TestEnvListCommand(t *testing.T) {
	runtime := &fakeRuntime{
		envs: []model.Environment{
			{
				ID:          "1234567890abcdef",
				Name:        "lab",
				Image:       "kalilinux/kali-rolling",
				State:       "running",
				NetworkMode: "bridge",
			},
		},
	}

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "env", "list")
	if err != nil {
		t.Fatalf("env list failed: %v", err)
	}
	for _, want := range []string{"NAME", "lab", "running", "kalilinux/kali-rolling"} {
		if !strings.Contains(output, want) {
			t.Fatalf("list output missing %q:\n%s", want, output)
		}
	}
}

func TestEnvEnterCommandWritesShellLog(t *testing.T) {
	runtime := &fakeRuntime{}
	logRoot := filepath.Join(t.TempDir(), "logs")

	output, err := execute(t, NewRootCommand(Options{
		Paths: func() (config.Paths, error) {
			paths, err := fixedPaths()
			paths.LogDir = logRoot
			return paths, err
		},
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "env", "enter", "lab", "--log", "--", "echo", "hi")
	if err != nil {
		t.Fatalf("env enter failed: %v", err)
	}
	if !strings.Contains(output, "Logging shell transcript to ") || !strings.Contains(output, "entered lab") {
		t.Fatalf("unexpected output:\n%s", output)
	}

	matches, err := filepath.Glob(filepath.Join(logRoot, "shell", "lab", "*.log"))
	if err != nil {
		t.Fatalf("glob shell logs: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one shell log, got %d: %#v", len(matches), matches)
	}

	info, err := os.Stat(matches[0])
	if err != nil {
		t.Fatalf("stat shell log: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected shell log permissions 0600, got %v", info.Mode().Perm())
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read shell log: %v", err)
	}
	for _, want := range []string{
		"# Contaigen shell transcript",
		"# Environment: lab",
		"# Command: echo hi",
		"# User: -",
		"# Workdir: -",
		"entered lab",
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("shell log missing %q:\n%s", want, string(data))
		}
	}
}

func TestEnvEnterCommandLogOutputEnablesShellLog(t *testing.T) {
	runtime := &fakeRuntime{}
	logPath := filepath.Join(t.TempDir(), "custom-session.log")

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "env", "enter", "lab", "--log-output", logPath)
	if err != nil {
		t.Fatalf("env enter failed: %v", err)
	}
	if !strings.Contains(output, "Logging shell transcript to "+logPath) {
		t.Fatalf("unexpected output:\n%s", output)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read custom shell log: %v", err)
	}
	for _, want := range []string{
		"# Command: (environment default shell)",
		"entered lab",
	} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("custom shell log missing %q:\n%s", want, string(data))
		}
	}
}

func TestParsePortMapping(t *testing.T) {
	mapping, err := parsePortMapping("127.0.0.1:8443:443/udp")
	if err != nil {
		t.Fatalf("parse port mapping: %v", err)
	}
	if mapping.HostIP != "127.0.0.1" || mapping.HostPort != "8443" || mapping.ContainerPort != "443" || mapping.Protocol != "udp" {
		t.Fatalf("unexpected mapping: %#v", mapping)
	}
}

func TestParseVolumeMount(t *testing.T) {
	mount, err := parseVolumeMount("/tmp/work:/workspace:ro")
	if err != nil {
		t.Fatalf("parse volume mount: %v", err)
	}
	if mount.Source != "/tmp/work" || mount.Target != "/workspace" || !mount.ReadOnly {
		t.Fatalf("unexpected mount: %#v", mount)
	}
}
