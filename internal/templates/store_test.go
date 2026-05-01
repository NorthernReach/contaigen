package templates

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/NorthernReach/contaigen/internal/config"
	"github.com/NorthernReach/contaigen/internal/model"
)

func TestListIncludesBuiltInProfiles(t *testing.T) {
	store := New(config.Paths{TemplateDir: filepath.Join(t.TempDir(), "templates")})

	profiles, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list profiles: %v", err)
	}

	names := map[string]bool{}
	for _, profile := range profiles {
		names[profile.Name] = true
	}
	for _, want := range []string{"kali-default", "kali-desktop", "parrot-default", "parrot-desktop"} {
		if !names[want] {
			t.Fatalf("missing built-in profile %q in %#v", want, profiles)
		}
	}
}

func TestLoadBuiltInProfile(t *testing.T) {
	store := New(config.Paths{TemplateDir: filepath.Join(t.TempDir(), "templates")})

	profile, err := store.Load(context.Background(), "parrot-default")
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if profile.Spec.Image != model.DefaultEnvironmentImage {
		t.Fatalf("unexpected image: %q", profile.Spec.Image)
	}
	if profile.Spec.Network.Profile != model.NetworkProfileBridge {
		t.Fatalf("unexpected network profile: %q", profile.Spec.Network.Profile)
	}
}

func TestLoadDesktopProfile(t *testing.T) {
	store := New(config.Paths{TemplateDir: filepath.Join(t.TempDir(), "templates")})

	profile, err := store.Load(context.Background(), "parrot-desktop")
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if profile.Spec.Image != model.DefaultDesktopImage {
		t.Fatalf("unexpected image: %q", profile.Spec.Image)
	}
	if profile.Spec.User != "root" {
		t.Fatalf("unexpected user: %q", profile.Spec.User)
	}
	if !profile.Spec.Desktop.Enabled || profile.Spec.Desktop.HostPort != model.DefaultDesktopPort || profile.Spec.Desktop.PasswordEnv != model.DefaultDesktopPasswordEnv {
		t.Fatalf("desktop defaults not loaded: %#v", profile.Spec.Desktop)
	}
}

func TestLoadUserProfileBeforeBuiltIn(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "templates", "profiles")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("create profile dir: %v", err)
	}
	path := filepath.Join(dir, "kali-default.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: contaigen.io/v1alpha1
kind: EnvironmentProfile
metadata:
  name: kali-default
spec:
  image: example/custom
  network:
    profile: isolated
`), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	store := New(config.Paths{TemplateDir: filepath.Dir(dir)})
	profile, err := store.Load(context.Background(), "kali-default")
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if profile.Spec.Image != "example/custom" {
		t.Fatalf("expected user profile to override built-in, got %q", profile.Spec.Image)
	}
}

func TestKaliDesktopProfileRemainsAvailable(t *testing.T) {
	store := New(config.Paths{TemplateDir: filepath.Join(t.TempDir(), "templates")})

	profile, err := store.Load(context.Background(), "kali-desktop")
	if err != nil {
		t.Fatalf("load kali desktop profile: %v", err)
	}
	if profile.Spec.Image != "kasmweb/kali-rolling-desktop:1.18.0" {
		t.Fatalf("unexpected kali desktop image: %q", profile.Spec.Image)
	}
	if profile.Spec.User != "root" {
		t.Fatalf("expected kali desktop to run as root, got %q", profile.Spec.User)
	}
}

func TestValidateFileRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: contaigen.io/v1alpha1
kind: EnvironmentProfile
metadata:
  name: bad
spec:
  image: example/bad
  surprise: nope
`), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	store := New(config.Paths{})
	_, err := store.ValidateFile(context.Background(), path)
	if err == nil {
		t.Fatal("expected unknown field validation error")
	}
}

func TestListIncludesBuiltInServiceTemplates(t *testing.T) {
	store := New(config.Paths{TemplateDir: filepath.Join(t.TempDir(), "templates")})

	services, err := store.ListServices(context.Background())
	if err != nil {
		t.Fatalf("list service templates: %v", err)
	}

	names := map[string]bool{}
	for _, service := range services {
		names[service.Name] = true
	}
	for _, want := range []string{"dvwa", "juice-shop", "webgoat"} {
		if !names[want] {
			t.Fatalf("missing built-in service template %q in %#v", want, services)
		}
	}
}

func TestLoadBuiltInServiceTemplate(t *testing.T) {
	store := New(config.Paths{TemplateDir: filepath.Join(t.TempDir(), "templates")})

	service, err := store.LoadService(context.Background(), "juice-shop")
	if err != nil {
		t.Fatalf("load service template: %v", err)
	}
	if service.Spec.Image != "bkimminich/juice-shop" {
		t.Fatalf("unexpected image: %q", service.Spec.Image)
	}
	if service.Spec.NetworkAlias != "juice-shop" {
		t.Fatalf("unexpected alias: %q", service.Spec.NetworkAlias)
	}
}

func TestProfileListSkipsServiceTemplatesInRootTemplateDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "templates")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("create template dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "target.yaml"), []byte(`apiVersion: contaigen.io/v1alpha1
kind: ServiceTemplate
metadata:
  name: target
spec:
  image: nginx:alpine
`), 0o600); err != nil {
		t.Fatalf("write service template: %v", err)
	}

	store := New(config.Paths{TemplateDir: dir})
	if _, err := store.List(context.Background()); err != nil {
		t.Fatalf("profile list should skip service templates: %v", err)
	}
}

func TestValidateAnyFileSupportsServiceTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.yaml")
	if err := os.WriteFile(path, []byte(`apiVersion: contaigen.io/v1alpha1
kind: ServiceTemplate
metadata:
  name: target
spec:
  image: nginx:alpine
`), 0o600); err != nil {
		t.Fatalf("write service template: %v", err)
	}

	store := New(config.Paths{})
	template, err := store.ValidateAnyFile(context.Background(), path)
	if err != nil {
		t.Fatalf("validate service template: %v", err)
	}
	if template.Kind != model.ServiceKind || template.Name != "target" {
		t.Fatalf("unexpected validated template: %#v", template)
	}
}

func TestApplyProfile(t *testing.T) {
	pull := false
	profile := model.EnvironmentProfile{
		Spec: model.EnvironmentProfileSpec{
			Image: "example/profile",
			Shell: "/bin/zsh",
			User:  "root",
			Network: model.ProfileNetwork{
				Profile: model.NetworkProfileSegment,
				Name:    "client-a",
			},
			Workspace: model.ProfileWorkspace{
				MountPath: "/work",
			},
			Desktop: model.DesktopConfig{
				Enabled:  true,
				HostPort: "7001",
			},
			CapAdd: []string{"NET_ADMIN"},
			Env:    []string{"A=B"},
			Pull:   &pull,
		},
	}

	req := ApplyProfile(profile, model.CreateEnvironmentRequest{Name: "lab", Pull: true, Start: true})
	if req.Image != "example/profile" || req.Shell != "/bin/zsh" {
		t.Fatalf("profile fields not applied: %#v", req)
	}
	if req.User != "root" {
		t.Fatalf("user not applied: %#v", req)
	}
	if req.NetworkProfile != model.NetworkProfileSegment || req.NetworkName != "client-a" {
		t.Fatalf("network not applied: %#v", req)
	}
	if req.WorkspaceMountPath != "/work" {
		t.Fatalf("workspace mount not applied: %#v", req)
	}
	if req.Pull {
		t.Fatal("expected profile pull=false to apply")
	}
	if !req.Desktop.Enabled || req.Desktop.HostPort != "7001" {
		t.Fatalf("desktop not applied: %#v", req.Desktop)
	}
	if len(req.CapAdd) != 1 || req.CapAdd[0] != "NET_ADMIN" {
		t.Fatalf("capabilities not applied: %#v", req.CapAdd)
	}
	if len(req.Env) != 1 || req.Env[0] != "A=B" {
		t.Fatalf("env not applied: %#v", req.Env)
	}
}

func TestApplyServiceTemplate(t *testing.T) {
	pull := false
	service := model.ServiceTemplate{
		Metadata: model.ServiceTemplateMeta{Name: "juice-shop"},
		Spec: model.ServiceTemplateSpec{
			Image:        "bkimminich/juice-shop",
			NetworkAlias: "juice-shop",
			Env:          []string{"A=B"},
			Pull:         &pull,
		},
	}

	req := ApplyServiceTemplate(service, model.CreateServiceRequest{EnvironmentName: "lab", Pull: true, Start: true})
	if req.Image != "bkimminich/juice-shop" || req.Name != "juice-shop" || req.NetworkAlias != "juice-shop" {
		t.Fatalf("service fields not applied: %#v", req)
	}
	if req.Pull {
		t.Fatal("expected service pull=false to apply")
	}
	if len(req.Env) != 1 || req.Env[0] != "A=B" {
		t.Fatalf("env not applied: %#v", req.Env)
	}
}
