package cli

import (
	"strings"
	"testing"

	"github.com/NorthernReach/contaigen/internal/model"
)

func TestProfileListCommand(t *testing.T) {
	profiles := &fakeProfileManager{
		summaries: []model.ProfileSummary{
			{
				Name:        "parrot-default",
				Source:      "built-in",
				Image:       model.DefaultEnvironmentImage,
				Network:     model.NetworkProfileBridge,
				Description: "Parrot Security",
			},
		},
	}

	output, err := execute(t, NewRootCommand(Options{
		NewProfileStore: fakeProfileStore(profiles),
	}), "profile", "list")
	if err != nil {
		t.Fatalf("profile list failed: %v", err)
	}
	for _, want := range []string{"parrot-default", "built-in", model.DefaultEnvironmentImage, "Parrot Security"} {
		if !strings.Contains(output, want) {
			t.Fatalf("profile list output missing %q:\n%s", want, output)
		}
	}
}

func TestProfileShowCommand(t *testing.T) {
	profiles := &fakeProfileManager{
		profiles: map[string]model.EnvironmentProfile{
			"parrot-default": {
				APIVersion: model.ProfileAPIVersion,
				Kind:       model.ProfileKind,
				Metadata: model.EnvironmentProfileMeta{
					Name:        "parrot-default",
					Description: "Parrot Security",
				},
				Spec: model.EnvironmentProfileSpec{
					Image: model.DefaultEnvironmentImage,
					Shell: "/bin/bash",
					Network: model.ProfileNetwork{
						Profile: model.NetworkProfileBridge,
					},
					Workspace: model.ProfileWorkspace{
						MountPath: model.DefaultWorkspaceMountPath,
					},
				},
				Source: "built-in",
			},
		},
	}

	output, err := execute(t, NewRootCommand(Options{
		NewProfileStore: fakeProfileStore(profiles),
	}), "profile", "show", "parrot-default")
	if err != nil {
		t.Fatalf("profile show failed: %v", err)
	}
	for _, want := range []string{"Name: parrot-default", "Image: " + model.DefaultEnvironmentImage, "Network: bridge", "Workspace: mounted at /workspace"} {
		if !strings.Contains(output, want) {
			t.Fatalf("profile show output missing %q:\n%s", want, output)
		}
	}
}

func TestTemplateValidateCommand(t *testing.T) {
	profiles := &fakeProfileManager{}

	output, err := execute(t, NewRootCommand(Options{
		NewProfileStore: fakeProfileStore(profiles),
	}), "template", "validate", "/tmp/profile.yaml")
	if err != nil {
		t.Fatalf("template validate failed: %v", err)
	}
	if !strings.Contains(output, "Valid EnvironmentProfile validated") {
		t.Fatalf("unexpected output:\n%s", output)
	}
	if len(profiles.validated) != 1 || profiles.validated[0] != "/tmp/profile.yaml" {
		t.Fatalf("unexpected validate calls: %#v", profiles.validated)
	}
}
