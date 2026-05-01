package cli

import (
	"strings"
	"testing"

	"github.com/NorthernReach/contaigen/internal/dockerx"
	"github.com/NorthernReach/contaigen/internal/model"
)

func TestNukeCommandDryRunShowsPlanWithoutRemoving(t *testing.T) {
	runtime := &fakeRuntime{
		envs: []model.Environment{{Name: "lab", State: "running"}},
		services: []model.Service{{
			EnvironmentName: "lab",
			Name:            "target",
			State:           "running",
		}},
		vpns:     []model.VPNGateway{{Name: "htb", State: "running"}},
		networks: []model.Network{{Name: "contaigen-lab"}},
	}
	workspaces := &fakeWorkspaceManager{
		items: []model.Workspace{{Name: "lab", Path: "/tmp/contaigen/workspaces/lab"}},
	}

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(workspaces),
	}), "nuke", "--dry-run")
	if err != nil {
		t.Fatalf("nuke dry-run failed: %v", err)
	}
	for _, want := range []string{"Contaigen nuke plan", "Services: 1", "Environment targets:", "lab", "Dry run only"} {
		if !strings.Contains(output, want) {
			t.Fatalf("dry-run output missing %q:\n%s", want, output)
		}
	}
	if len(runtime.removed) != 0 || len(runtime.removedServices) != 0 || len(workspaces.removed) != 0 {
		t.Fatalf("dry run removed resources: runtime=%#v services=%#v workspaces=%#v", runtime.removed, runtime.removedServices, workspaces.removed)
	}
}

func TestNukeCommandPromptsBacksUpAndRemoves(t *testing.T) {
	runtime := &fakeRuntime{
		envs: []model.Environment{{
			Name:          "lab",
			State:         "running",
			WorkspaceName: "lab",
			WorkspacePath: "/tmp/contaigen/workspaces/lab",
		}},
		services: []model.Service{{
			EnvironmentName: "lab",
			Name:            "target",
			State:           "running",
		}},
		vpns:     []model.VPNGateway{{Name: "htb", State: "running"}},
		networks: []model.Network{{Name: "contaigen-lab"}},
	}
	workspaces := &fakeWorkspaceManager{
		items: []model.Workspace{{Name: "lab", Path: "/tmp/contaigen/workspaces/lab"}},
	}

	cmd := NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(workspaces),
	})
	cmd.SetIn(strings.NewReader("y\nnuke\n"))
	output, err := execute(t, cmd, "nuke", "--password", "secret")
	if err != nil {
		t.Fatalf("nuke failed: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Workspace backups:") || !strings.Contains(output, "Contaigen nuke complete") {
		t.Fatalf("unexpected nuke output:\n%s", output)
	}
	if len(workspaces.backups) != 1 || workspaces.backups[0].Password != "secret" {
		t.Fatalf("workspace backup missing: %#v", workspaces.backups)
	}
	if len(workspaces.removed) != 1 || workspaces.removed[0].Name != "lab" {
		t.Fatalf("workspace remove missing: %#v", workspaces.removed)
	}
	if strings.Join(runtime.removedServices, ",") != "lab/target" ||
		strings.Join(runtime.removed, ",") != "lab" ||
		strings.Join(runtime.removedVPNs, ",") != "htb" ||
		strings.Join(runtime.removedNetworks, ",") != "contaigen-lab" {
		t.Fatalf("resources not removed: services=%#v envs=%#v vpns=%#v networks=%#v", runtime.removedServices, runtime.removed, runtime.removedVPNs, runtime.removedNetworks)
	}
}

func TestNukeCommandYesRequiresBackupChoice(t *testing.T) {
	_, err := execute(t, NewRootCommand(Options{}), "nuke", "--yes")
	if err == nil {
		t.Fatal("expected --yes without backup choice to fail")
	}
	if !strings.Contains(err.Error(), "--yes requires either --backup-workspaces or --no-backup-workspaces") {
		t.Fatalf("unexpected error: %v", err)
	}
}
