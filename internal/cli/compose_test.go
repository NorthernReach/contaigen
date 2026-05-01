package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthernReach/contaigen/internal/composex"
	"github.com/NorthernReach/contaigen/internal/dockerx"
	"github.com/NorthernReach/contaigen/internal/model"
)

func TestComposeValidateCommand(t *testing.T) {
	compose := &fakeComposeManager{
		project: composex.ProjectSummary{
			Path: "/tmp/compose.yaml",
			Services: []composex.ServiceSummary{
				{Name: "web", Image: "nginx:alpine"},
			},
		},
	}

	output, err := execute(t, NewRootCommand(Options{
		NewComposeManager: fakeComposeStore(compose),
	}), "compose", "validate", "/tmp/compose.yaml")
	if err != nil {
		t.Fatalf("compose validate failed: %v", err)
	}
	for _, want := range []string{"Valid Compose file /tmp/compose.yaml", "SERVICE", "web", "nginx:alpine"} {
		if !strings.Contains(output, want) {
			t.Fatalf("compose validate output missing %q:\n%s", want, output)
		}
	}
	if compose.validatedWithDocker {
		t.Fatal("did not expect docker validation without --docker")
	}
}

func TestComposeUpCommandUsesEnvironmentNetwork(t *testing.T) {
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
	compose := &fakeComposeManager{}

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
		NewComposeManager: fakeComposeStore(compose),
	}), "compose", "up", "lab", "/tmp/compose.yaml", "--project", "client-a-app")
	if err != nil {
		t.Fatalf("compose up failed: %v", err)
	}
	if !strings.Contains(output, "Started Compose app for environment lab on network client-a") {
		t.Fatalf("unexpected output:\n%s", output)
	}
	if compose.up.ProjectName != "client-a-app" || compose.up.EnvName != "lab" || compose.up.NetworkName != "client-a" || !compose.up.Detach {
		t.Fatalf("unexpected compose up request: %#v", compose.up)
	}
}

func TestComposeUpCommandRequiresSegmentNetwork(t *testing.T) {
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
		NewComposeManager: fakeComposeStore(&fakeComposeManager{}),
	}), "compose", "up", "lab", "/tmp/compose.yaml")
	if err == nil {
		t.Fatal("expected segment network requirement")
	}
	if !strings.Contains(err.Error(), "--network segment") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type fakeComposeManager struct {
	project             composex.ProjectSummary
	validatedWithDocker bool
	up                  composex.UpRequest
	down                composex.DownRequest
}

func (f *fakeComposeManager) ValidateFile(context.Context, string) (composex.ProjectSummary, error) {
	if f.project.Path == "" {
		f.project = composex.ProjectSummary{Path: "/tmp/compose.yaml"}
	}
	return f.project, nil
}

func (f *fakeComposeManager) ValidateWithDocker(context.Context, string, model.ExecIO) error {
	f.validatedWithDocker = true
	return nil
}

func (f *fakeComposeManager) Up(_ context.Context, req composex.UpRequest, _ model.ExecIO) error {
	f.up = req
	return nil
}

func (f *fakeComposeManager) Down(_ context.Context, req composex.DownRequest, _ model.ExecIO) error {
	f.down = req
	return nil
}

func fakeComposeStore(manager composex.Manager) func() (composex.Manager, error) {
	return func() (composex.Manager, error) {
		return manager, nil
	}
}
