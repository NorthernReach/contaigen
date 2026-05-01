package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NorthernReach/contaigen/internal/dockerx"
	"github.com/NorthernReach/contaigen/internal/model"
)

func TestServiceAddCommand(t *testing.T) {
	runtime := &fakeRuntime{
		envs: []model.Environment{
			{
				Name:           "lab",
				NetworkProfile: model.NetworkProfileSegment,
				NetworkName:    "client-a",
				NetworkMode:    "client-a",
			},
		},
	}

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "service", "add", "lab", "nginx:alpine", "--name", "target", "--alias", "target.local", "--env", "TOKEN=value", "--port", "127.0.0.1:8080:80/tcp", "--volume", "/tmp/app:/app:ro", "--", "nginx", "-g", "daemon off;")
	if err != nil {
		t.Fatalf("service add failed: %v", err)
	}
	for _, want := range []string{"Created service target", "environment lab", "Network: client-a alias target.local", "Started service target"} {
		if !strings.Contains(output, want) {
			t.Fatalf("service add output missing %q:\n%s", want, output)
		}
	}
	if len(runtime.createdServices) != 1 {
		t.Fatalf("expected one service create request, got %d", len(runtime.createdServices))
	}
	req := runtime.createdServices[0]
	if req.EnvironmentName != "lab" || req.Name != "target" || req.Image != "nginx:alpine" {
		t.Fatalf("unexpected service request identity: %#v", req)
	}
	if req.NetworkName != "client-a" || req.NetworkAlias != "target.local" {
		t.Fatalf("unexpected network fields: %#v", req)
	}
	if strings.Join(req.Env, ",") != "TOKEN=value" {
		t.Fatalf("unexpected env values: %#v", req.Env)
	}
	if got := req.Ports[0]; got.HostIP != "127.0.0.1" || got.HostPort != "8080" || got.ContainerPort != "80" || got.Protocol != "tcp" {
		t.Fatalf("unexpected port mapping: %#v", got)
	}
	if got := req.Volumes[0]; got.Source != "/tmp/app" || got.Target != "/app" || !got.ReadOnly {
		t.Fatalf("unexpected volume mount: %#v", got)
	}
	if strings.Join(req.Command, " ") != "nginx -g daemon off;" {
		t.Fatalf("unexpected command: %#v", req.Command)
	}
}

func TestServiceAddCommandReadsEnvFile(t *testing.T) {
	runtime := &fakeRuntime{
		envs: []model.Environment{
			{
				Name:           "lab",
				NetworkProfile: model.NetworkProfileSegment,
				NetworkName:    "client-a",
				NetworkMode:    "client-a",
			},
		},
	}
	envPath := filepath.Join(t.TempDir(), "service.env")
	if err := os.WriteFile(envPath, []byte("FROM_FILE=true\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}

	_, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "service", "add", "lab", "nginx:alpine", "--name", "target", "--env-file", envPath, "--env", "FROM_CLI=true")
	if err != nil {
		t.Fatalf("service add failed: %v", err)
	}
	if len(runtime.createdServices) != 1 {
		t.Fatalf("expected one service create request, got %d", len(runtime.createdServices))
	}
	if strings.Join(runtime.createdServices[0].Env, ",") != "FROM_FILE=true,FROM_CLI=true" {
		t.Fatalf("unexpected env values: %#v", runtime.createdServices[0].Env)
	}
}

func TestServiceAddCommandAppliesTemplate(t *testing.T) {
	runtime := &fakeRuntime{
		envs: []model.Environment{
			{
				Name:           "lab",
				NetworkProfile: model.NetworkProfileSegment,
				NetworkName:    "client-a",
				NetworkMode:    "client-a",
			},
		},
	}
	profiles := &fakeProfileManager{
		serviceTemplates: map[string]model.ServiceTemplate{
			"juice-shop": {
				APIVersion: model.ProfileAPIVersion,
				Kind:       model.ServiceKind,
				Metadata: model.ServiceTemplateMeta{
					Name: "juice-shop",
				},
				Spec: model.ServiceTemplateSpec{
					Image:        "bkimminich/juice-shop",
					NetworkAlias: "juice-shop",
					Env:          []string{"FROM_TEMPLATE=true"},
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
	}), "service", "add", "lab", "juice-shop", "--name", "target", "--env", "FROM_CLI=true")
	if err != nil {
		t.Fatalf("service add with template failed: %v", err)
	}
	if len(runtime.createdServices) != 1 {
		t.Fatalf("expected one service create request, got %d", len(runtime.createdServices))
	}
	req := runtime.createdServices[0]
	if req.Image != "bkimminich/juice-shop" || req.Name != "target" || req.NetworkAlias != "juice-shop" {
		t.Fatalf("template fields not applied/overridden: %#v", req)
	}
	if strings.Join(req.Env, ",") != "FROM_TEMPLATE=true,FROM_CLI=true" {
		t.Fatalf("unexpected env merge: %#v", req.Env)
	}
}

func TestServiceAddCommandRequiresSegmentNetwork(t *testing.T) {
	runtime := &fakeRuntime{
		envs: []model.Environment{
			{
				Name:        "lab",
				NetworkMode: model.NetworkProfileBridge,
			},
		},
	}

	_, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "service", "add", "lab", "nginx:alpine", "--name", "target")
	if err == nil {
		t.Fatal("expected segment network requirement")
	}
	if !strings.Contains(err.Error(), "--network segment") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServiceTemplateListCommand(t *testing.T) {
	profiles := &fakeProfileManager{
		serviceSummaries: []model.ServiceTemplateSummary{
			{
				Name:        "juice-shop",
				Description: "OWASP Juice Shop",
				Image:       "bkimminich/juice-shop",
				Alias:       "juice-shop",
				Source:      "built-in",
			},
		},
	}

	output, err := execute(t, NewRootCommand(Options{
		NewProfileStore: fakeProfileStore(profiles),
	}), "service", "template", "list")
	if err != nil {
		t.Fatalf("service template list failed: %v", err)
	}
	for _, want := range []string{"NAME", "juice-shop", "bkimminich/juice-shop", "built-in"} {
		if !strings.Contains(output, want) {
			t.Fatalf("service template list output missing %q:\n%s", want, output)
		}
	}
}

func TestServiceListCommand(t *testing.T) {
	runtime := &fakeRuntime{
		services: []model.Service{
			{
				ID:              "service1234567890",
				Name:            "target",
				EnvironmentName: "lab",
				Image:           "nginx:alpine",
				State:           "running",
				NetworkAlias:    "target.local",
			},
		},
	}

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "service", "list", "lab")
	if err != nil {
		t.Fatalf("service list failed: %v", err)
	}
	for _, want := range []string{"ENV", "SERVICE", "lab", "target", "nginx:alpine", "target.local"} {
		if !strings.Contains(output, want) {
			t.Fatalf("service list output missing %q:\n%s", want, output)
		}
	}
}

func TestDeriveServiceName(t *testing.T) {
	for image, want := range map[string]string{
		"nginx:alpine":                          "nginx",
		"ghcr.io/example/vulnerable-app:latest": "vulnerable-app",
		"localhost:5000/team/api@sha256:abc123": "api",
	} {
		if got := deriveServiceName(image); got != want {
			t.Fatalf("deriveServiceName(%q) = %q, want %q", image, got, want)
		}
	}
}
