package cli

import (
	"strings"
	"testing"

	"github.com/NorthernReach/contaigen/internal/dockerx"
	"github.com/NorthernReach/contaigen/internal/model"
)

func TestNetCreateCommand(t *testing.T) {
	runtime := &fakeRuntime{}

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "net", "create", "client-a", "--internal")
	if err != nil {
		t.Fatalf("net create failed: %v", err)
	}
	if !strings.Contains(output, "Ready network client-a") {
		t.Fatalf("unexpected output:\n%s", output)
	}
	if len(runtime.networkEnsures) != 1 {
		t.Fatalf("expected one network ensure, got %d", len(runtime.networkEnsures))
	}
	req := runtime.networkEnsures[0]
	if req.Name != "client-a" || req.Profile != model.NetworkProfileSegment || !req.Internal {
		t.Fatalf("unexpected ensure request: %#v", req)
	}
}

func TestNetListCommand(t *testing.T) {
	runtime := &fakeRuntime{
		networks: []model.Network{
			{
				ID:       "network1234567890",
				Name:     "contaigen-lab",
				Profile:  model.NetworkProfileSegment,
				Driver:   model.DefaultNetworkDriver,
				Internal: false,
			},
		},
	}

	output, err := execute(t, NewRootCommand(Options{
		NewDockerClient: func() (dockerx.Runtime, error) {
			return runtime, nil
		},
		NewWorkspaceStore: fakeWorkspaceStore(&fakeWorkspaceManager{}),
	}), "net", "list")
	if err != nil {
		t.Fatalf("net list failed: %v", err)
	}
	for _, want := range []string{"NAME", "contaigen-lab", "segment", "bridge"} {
		if !strings.Contains(output, want) {
			t.Fatalf("list output missing %q:\n%s", want, output)
		}
	}
}

func TestNetMapCommand(t *testing.T) {
	runtime := &fakeRuntime{
		networkMap: model.NetworkMap{
			Networks: []model.Network{
				{
					Name:    "contaigen-lab",
					Profile: model.NetworkProfileSegment,
					Attachments: []model.NetworkAttachment{
						{
							ContainerID:     "1234567890abcdef",
							ContainerName:   "contaigen-lab",
							EnvironmentName: "lab",
							IPv4Address:     "172.18.0.2/16",
						},
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
	}), "net", "map")
	if err != nil {
		t.Fatalf("net map failed: %v", err)
	}
	for _, want := range []string{"contaigen-lab [segment]", "lab", "172.18.0.2/16"} {
		if !strings.Contains(output, want) {
			t.Fatalf("map output missing %q:\n%s", want, output)
		}
	}
}
