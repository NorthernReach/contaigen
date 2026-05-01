package templates

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NorthernReach/contaigen/internal/config"
	"github.com/NorthernReach/contaigen/internal/model"
	"gopkg.in/yaml.v3"
)

//go:embed profiles/*.yaml services/*.yaml
var builtinTemplates embed.FS

const (
	SourceBuiltin = "built-in"
	SourceUser    = "user"
	SourceFile    = "file"
)

var (
	ErrProfileNotFound         = errors.New("profile not found")
	ErrServiceTemplateNotFound = errors.New("service template not found")
	ErrTemplateKindMismatch    = errors.New("template kind mismatch")
)

type Store struct {
	paths config.Paths
}

type Manager interface {
	List(context.Context) ([]model.ProfileSummary, error)
	Load(context.Context, string) (model.EnvironmentProfile, error)
	ValidateFile(context.Context, string) (model.EnvironmentProfile, error)
	ListServices(context.Context) ([]model.ServiceTemplateSummary, error)
	LoadService(context.Context, string) (model.ServiceTemplate, error)
	ValidateServiceFile(context.Context, string) (model.ServiceTemplate, error)
	ValidateAnyFile(context.Context, string) (ValidatedTemplate, error)
}

type ValidatedTemplate struct {
	Kind    string
	Name    string
	Source  string
	Path    string
	Profile model.EnvironmentProfile
	Service model.ServiceTemplate
}

func New(paths config.Paths) *Store {
	return &Store{paths: paths}
}

func (s *Store) List(ctx context.Context) ([]model.ProfileSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	profiles := []model.EnvironmentProfile{}
	builtins, err := s.loadBuiltins()
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, builtins...)

	userProfiles, err := s.loadUserProfiles(ctx)
	if err != nil {
		return nil, err
	}
	profiles = append(profiles, userProfiles...)

	summaries := make([]model.ProfileSummary, 0, len(profiles))
	for _, profile := range profiles {
		summaries = append(summaries, Summary(profile))
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Name == summaries[j].Name {
			return summaries[i].Source < summaries[j].Source
		}
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}

func (s *Store) Load(ctx context.Context, nameOrPath string) (model.EnvironmentProfile, error) {
	if err := ctx.Err(); err != nil {
		return model.EnvironmentProfile{}, err
	}
	nameOrPath = strings.TrimSpace(nameOrPath)
	if nameOrPath == "" {
		return model.EnvironmentProfile{}, fmt.Errorf("profile name or path is required")
	}

	// Direct paths are loaded first so users can test a profile without copying
	// it into the configured template directory.
	if profile, err := s.loadProfilePath(nameOrPath, SourceFile); err == nil {
		return profile, nil
	} else if looksLikePath(nameOrPath) {
		return model.EnvironmentProfile{}, err
	}

	// User profiles intentionally shadow built-ins by name; that lets teams
	// keep local defaults while still falling back to Contaigen's profiles.
	userProfiles, err := s.loadUserProfiles(ctx)
	if err != nil {
		return model.EnvironmentProfile{}, err
	}
	for _, profile := range userProfiles {
		if profile.Metadata.Name == nameOrPath {
			return profile, nil
		}
	}

	builtins, err := s.loadBuiltins()
	if err != nil {
		return model.EnvironmentProfile{}, err
	}
	for _, profile := range builtins {
		if profile.Metadata.Name == nameOrPath {
			return profile, nil
		}
	}

	return model.EnvironmentProfile{}, fmt.Errorf("%w: %s", ErrProfileNotFound, nameOrPath)
}

func (s *Store) ValidateFile(ctx context.Context, path string) (model.EnvironmentProfile, error) {
	if err := ctx.Err(); err != nil {
		return model.EnvironmentProfile{}, err
	}
	return s.loadProfilePath(path, SourceFile)
}

func (s *Store) ListServices(ctx context.Context) ([]model.ServiceTemplateSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	services := []model.ServiceTemplate{}
	builtins, err := s.loadBuiltinServices()
	if err != nil {
		return nil, err
	}
	services = append(services, builtins...)

	userServices, err := s.loadUserServices(ctx)
	if err != nil {
		return nil, err
	}
	services = append(services, userServices...)

	summaries := make([]model.ServiceTemplateSummary, 0, len(services))
	for _, service := range services {
		summaries = append(summaries, ServiceSummary(service))
	}
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].Name == summaries[j].Name {
			return summaries[i].Source < summaries[j].Source
		}
		return summaries[i].Name < summaries[j].Name
	})
	return summaries, nil
}

func (s *Store) LoadService(ctx context.Context, nameOrPath string) (model.ServiceTemplate, error) {
	if err := ctx.Err(); err != nil {
		return model.ServiceTemplate{}, err
	}
	nameOrPath = strings.TrimSpace(nameOrPath)
	if nameOrPath == "" {
		return model.ServiceTemplate{}, fmt.Errorf("service template name or path is required")
	}

	if service, err := s.loadServicePath(nameOrPath, SourceFile); err == nil {
		return service, nil
	} else if looksLikeTemplatePath(nameOrPath) {
		return model.ServiceTemplate{}, err
	}

	userServices, err := s.loadUserServices(ctx)
	if err != nil {
		return model.ServiceTemplate{}, err
	}
	for _, service := range userServices {
		if service.Metadata.Name == nameOrPath {
			return service, nil
		}
	}

	builtins, err := s.loadBuiltinServices()
	if err != nil {
		return model.ServiceTemplate{}, err
	}
	for _, service := range builtins {
		if service.Metadata.Name == nameOrPath {
			return service, nil
		}
	}

	return model.ServiceTemplate{}, fmt.Errorf("%w: %s", ErrServiceTemplateNotFound, nameOrPath)
}

func (s *Store) ValidateServiceFile(ctx context.Context, path string) (model.ServiceTemplate, error) {
	if err := ctx.Err(); err != nil {
		return model.ServiceTemplate{}, err
	}
	return s.loadServicePath(path, SourceFile)
}

func (s *Store) ValidateAnyFile(ctx context.Context, path string) (ValidatedTemplate, error) {
	if err := ctx.Err(); err != nil {
		return ValidatedTemplate{}, err
	}
	data, abs, err := readPath(path)
	if err != nil {
		return ValidatedTemplate{}, err
	}
	kind, err := templateKind(data)
	if err != nil {
		return ValidatedTemplate{}, err
	}
	switch kind {
	case model.ProfileKind:
		profile, err := parseProfile(data, SourceFile, abs)
		if err != nil {
			return ValidatedTemplate{}, err
		}
		return ValidatedTemplate{
			Kind:    profile.Kind,
			Name:    profile.Metadata.Name,
			Source:  profile.Source,
			Path:    profile.Path,
			Profile: profile,
		}, nil
	case model.ServiceKind:
		service, err := parseServiceTemplate(data, SourceFile, abs)
		if err != nil {
			return ValidatedTemplate{}, err
		}
		return ValidatedTemplate{
			Kind:    service.Kind,
			Name:    service.Metadata.Name,
			Source:  service.Source,
			Path:    service.Path,
			Service: service,
		}, nil
	default:
		return ValidatedTemplate{}, fmt.Errorf("unsupported template kind %q", kind)
	}
}

func Summary(profile model.EnvironmentProfile) model.ProfileSummary {
	return model.ProfileSummary{
		Name:        profile.Metadata.Name,
		Description: profile.Metadata.Description,
		Image:       profile.Spec.Image,
		Network:     profile.Spec.Network.Profile,
		Source:      profile.Source,
		Path:        profile.Path,
	}
}

func ServiceSummary(service model.ServiceTemplate) model.ServiceTemplateSummary {
	return model.ServiceTemplateSummary{
		Name:        service.Metadata.Name,
		Description: service.Metadata.Description,
		Image:       service.Spec.Image,
		Alias:       service.Spec.NetworkAlias,
		Source:      service.Source,
		Path:        service.Path,
	}
}

func ApplyProfile(profile model.EnvironmentProfile, req model.CreateEnvironmentRequest) model.CreateEnvironmentRequest {
	if profile.Spec.Image != "" {
		req.Image = profile.Spec.Image
	}
	if profile.Spec.Shell != "" {
		req.Shell = profile.Spec.Shell
	}
	if profile.Spec.User != "" {
		req.User = profile.Spec.User
	}
	if profile.Spec.Hostname != "" {
		req.Hostname = profile.Spec.Hostname
	}
	if profile.Spec.WorkingDir != "" {
		req.WorkingDir = profile.Spec.WorkingDir
	}
	if profile.Spec.Network.Profile != "" {
		req.NetworkProfile = profile.Spec.Network.Profile
	}
	if profile.Spec.Network.Name != "" {
		if profile.Spec.Network.Profile == model.NetworkProfileVPN {
			req.VPNName = profile.Spec.Network.Name
		} else {
			req.NetworkName = profile.Spec.Network.Name
		}
	}
	if profile.Spec.Workspace.Name != "" {
		req.WorkspaceName = profile.Spec.Workspace.Name
	}
	if profile.Spec.Workspace.Path != "" {
		req.WorkspacePath = profile.Spec.Workspace.Path
	}
	if profile.Spec.Workspace.MountPath != "" {
		req.WorkspaceMountPath = profile.Spec.Workspace.MountPath
	}
	if profile.Spec.Workspace.Disabled {
		req.DisableWorkspace = true
	}
	if profile.Spec.Desktop.Enabled {
		req.Desktop = profile.Spec.Desktop
	}
	if profile.Spec.Pull != nil {
		req.Pull = *profile.Spec.Pull
	}
	if profile.Spec.Start != nil {
		req.Start = *profile.Spec.Start
	}
	req.Env = append(req.Env, profile.Spec.Env...)
	req.Ports = append(req.Ports, profile.Spec.Ports...)
	req.Volumes = append(req.Volumes, profile.Spec.Volumes...)
	req.CapAdd = append(req.CapAdd, profile.Spec.CapAdd...)
	return req
}

func ApplyServiceTemplate(service model.ServiceTemplate, req model.CreateServiceRequest) model.CreateServiceRequest {
	if service.Spec.Image != "" {
		req.Image = service.Spec.Image
	}
	if service.Spec.Name != "" {
		req.Name = service.Spec.Name
	} else if req.Name == "" {
		req.Name = service.Metadata.Name
	}
	if service.Spec.NetworkAlias != "" {
		req.NetworkAlias = service.Spec.NetworkAlias
	}
	if service.Spec.Pull != nil {
		req.Pull = *service.Spec.Pull
	}
	if service.Spec.Start != nil {
		req.Start = *service.Spec.Start
	}
	req.Env = append(req.Env, service.Spec.Env...)
	req.Ports = append(req.Ports, service.Spec.Ports...)
	req.Volumes = append(req.Volumes, service.Spec.Volumes...)
	req.Command = append(req.Command, service.Spec.Command...)
	return req
}

func ValidateProfile(profile model.EnvironmentProfile) error {
	if profile.APIVersion != model.ProfileAPIVersion {
		return fmt.Errorf("apiVersion must be %q", model.ProfileAPIVersion)
	}
	if profile.Kind != model.ProfileKind {
		return fmt.Errorf("kind must be %q", model.ProfileKind)
	}
	if strings.TrimSpace(profile.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if strings.TrimSpace(profile.Spec.Image) == "" {
		return fmt.Errorf("spec.image is required")
	}
	network := profile.Spec.Network.Profile
	if network != "" {
		switch network {
		case model.NetworkProfileBridge, model.NetworkProfileHost, model.NetworkProfileIsolated, model.NetworkProfileSegment, model.NetworkProfileVPN:
		default:
			return fmt.Errorf("spec.network.profile must be one of bridge, isolated, host, segment, or vpn")
		}
	}
	for _, env := range profile.Spec.Env {
		if !strings.Contains(env, "=") {
			return fmt.Errorf("spec.env value %q must be KEY=VALUE", env)
		}
	}
	for _, volume := range profile.Spec.Volumes {
		if volume.Source == "" || volume.Target == "" {
			return fmt.Errorf("spec.volumes entries require source and target")
		}
	}
	for _, port := range profile.Spec.Ports {
		if port.HostPort == "" || port.ContainerPort == "" {
			return fmt.Errorf("spec.ports entries require hostPort and containerPort")
		}
	}
	for _, capability := range profile.Spec.CapAdd {
		if strings.TrimSpace(capability) == "" {
			return fmt.Errorf("spec.capAdd entries cannot be empty")
		}
	}
	if profile.Spec.Desktop.Enabled {
		if profile.Spec.Desktop.Protocol != "" && profile.Spec.Desktop.Protocol != model.DefaultDesktopProtocol {
			return fmt.Errorf("spec.desktop.protocol must be novnc")
		}
		if strings.Contains(profile.Spec.Desktop.PasswordEnv, "=") {
			return fmt.Errorf("spec.desktop.passwordEnv must be a variable name, not KEY=VALUE")
		}
	}
	return nil
}

func ValidateServiceTemplate(service model.ServiceTemplate) error {
	if service.APIVersion != model.ProfileAPIVersion {
		return fmt.Errorf("apiVersion must be %q", model.ProfileAPIVersion)
	}
	if service.Kind != model.ServiceKind {
		return fmt.Errorf("kind must be %q", model.ServiceKind)
	}
	if strings.TrimSpace(service.Metadata.Name) == "" {
		return fmt.Errorf("metadata.name is required")
	}
	if strings.TrimSpace(service.Spec.Image) == "" {
		return fmt.Errorf("spec.image is required")
	}
	for _, env := range service.Spec.Env {
		if !strings.Contains(env, "=") {
			return fmt.Errorf("spec.env value %q must be KEY=VALUE", env)
		}
	}
	for _, volume := range service.Spec.Volumes {
		if volume.Source == "" || volume.Target == "" {
			return fmt.Errorf("spec.volumes entries require source and target")
		}
	}
	for _, port := range service.Spec.Ports {
		if port.HostPort == "" || port.ContainerPort == "" {
			return fmt.Errorf("spec.ports entries require hostPort and containerPort")
		}
	}
	return nil
}

func (s *Store) loadBuiltins() ([]model.EnvironmentProfile, error) {
	entries, err := fs.ReadDir(builtinTemplates, "profiles")
	if err != nil {
		return nil, err
	}

	profiles := make([]model.EnvironmentProfile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isYAML(entry.Name()) {
			continue
		}
		path := filepath.ToSlash(filepath.Join("profiles", entry.Name()))
		data, err := builtinTemplates.ReadFile(path)
		if err != nil {
			return nil, err
		}
		profile, err := parseProfile(data, SourceBuiltin, path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

func (s *Store) loadBuiltinServices() ([]model.ServiceTemplate, error) {
	entries, err := fs.ReadDir(builtinTemplates, "services")
	if err != nil {
		return nil, err
	}

	services := make([]model.ServiceTemplate, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !isYAML(entry.Name()) {
			continue
		}
		path := filepath.ToSlash(filepath.Join("services", entry.Name()))
		data, err := builtinTemplates.ReadFile(path)
		if err != nil {
			return nil, err
		}
		service, err := parseServiceTemplate(data, SourceBuiltin, path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		services = append(services, service)
	}
	return services, nil
}

func (s *Store) loadUserProfiles(ctx context.Context) ([]model.EnvironmentProfile, error) {
	dirs := []string{
		s.paths.TemplateDir,
		filepath.Join(s.paths.TemplateDir, "profiles"),
	}

	profiles := []model.EnvironmentProfile{}
	seen := map[string]bool{}
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		files, err := profileFiles(dir)
		if err != nil {
			return nil, err
		}
		for _, path := range files {
			if seen[path] {
				continue
			}
			seen[path] = true
			profile, err := s.loadProfilePath(path, SourceUser)
			if errors.Is(err, ErrTemplateKindMismatch) {
				continue
			}
			if err != nil {
				return nil, err
			}
			profiles = append(profiles, profile)
		}
	}
	return profiles, nil
}

func (s *Store) loadUserServices(ctx context.Context) ([]model.ServiceTemplate, error) {
	dirs := []string{
		s.paths.TemplateDir,
		filepath.Join(s.paths.TemplateDir, "services"),
	}

	services := []model.ServiceTemplate{}
	seen := map[string]bool{}
	for _, dir := range dirs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		files, err := profileFiles(dir)
		if err != nil {
			return nil, err
		}
		for _, path := range files {
			if seen[path] {
				continue
			}
			seen[path] = true
			service, err := s.loadServicePath(path, SourceUser)
			if errors.Is(err, ErrTemplateKindMismatch) {
				continue
			}
			if err != nil {
				return nil, err
			}
			services = append(services, service)
		}
	}
	return services, nil
}

func (s *Store) loadProfilePath(path string, source string) (model.EnvironmentProfile, error) {
	data, abs, err := readPath(path)
	if err != nil {
		return model.EnvironmentProfile{}, err
	}
	return parseProfile(data, source, abs)
}

func (s *Store) loadServicePath(path string, source string) (model.ServiceTemplate, error) {
	data, abs, err := readPath(path)
	if err != nil {
		return model.ServiceTemplate{}, err
	}
	return parseServiceTemplate(data, source, abs)
}

func readPath(path string) ([]byte, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, "", err
	}
	return data, abs, nil
}

func parseProfile(data []byte, source string, path string) (model.EnvironmentProfile, error) {
	kind, err := templateKind(data)
	if err != nil {
		return model.EnvironmentProfile{}, err
	}
	if kind != model.ProfileKind {
		return model.EnvironmentProfile{}, fmt.Errorf("%w: expected %s, got %s", ErrTemplateKindMismatch, model.ProfileKind, kind)
	}

	var profile model.EnvironmentProfile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&profile); err != nil {
		return model.EnvironmentProfile{}, err
	}
	profile.Source = source
	profile.Path = path
	if err := ValidateProfile(profile); err != nil {
		return model.EnvironmentProfile{}, err
	}
	if profile.Spec.Network.Profile == "" {
		profile.Spec.Network.Profile = model.DefaultNetworkProfile
	}
	if profile.Spec.Shell == "" {
		profile.Spec.Shell = model.DefaultEnvironmentShell
	}
	if profile.Spec.Workspace.MountPath == "" {
		profile.Spec.Workspace.MountPath = model.DefaultWorkspaceMountPath
	}
	if profile.Spec.Desktop.Enabled {
		if profile.Spec.Desktop.Protocol == "" {
			profile.Spec.Desktop.Protocol = model.DefaultDesktopProtocol
		}
		if profile.Spec.Desktop.HostIP == "" {
			profile.Spec.Desktop.HostIP = model.DefaultDesktopHostIP
		}
		if profile.Spec.Desktop.HostPort == "" {
			profile.Spec.Desktop.HostPort = model.DefaultDesktopPort
		}
		if profile.Spec.Desktop.ContainerPort == "" {
			profile.Spec.Desktop.ContainerPort = model.DefaultDesktopPort
		}
		if profile.Spec.Desktop.Scheme == "" {
			profile.Spec.Desktop.Scheme = model.DefaultDesktopScheme
		}
		if profile.Spec.Desktop.Path == "" {
			profile.Spec.Desktop.Path = model.DefaultDesktopPath
		}
		if profile.Spec.Desktop.User == "" {
			profile.Spec.Desktop.User = model.DefaultDesktopUser
		}
		if profile.Spec.Desktop.PasswordEnv == "" {
			profile.Spec.Desktop.PasswordEnv = model.DefaultDesktopPasswordEnv
		}
	}
	return profile, nil
}

func parseServiceTemplate(data []byte, source string, path string) (model.ServiceTemplate, error) {
	kind, err := templateKind(data)
	if err != nil {
		return model.ServiceTemplate{}, err
	}
	if kind != model.ServiceKind {
		return model.ServiceTemplate{}, fmt.Errorf("%w: expected %s, got %s", ErrTemplateKindMismatch, model.ServiceKind, kind)
	}

	var service model.ServiceTemplate
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&service); err != nil {
		return model.ServiceTemplate{}, err
	}
	service.Source = source
	service.Path = path
	if err := ValidateServiceTemplate(service); err != nil {
		return model.ServiceTemplate{}, err
	}
	if service.Spec.Name == "" {
		service.Spec.Name = service.Metadata.Name
	}
	if service.Spec.NetworkAlias == "" {
		service.Spec.NetworkAlias = service.Metadata.Name
	}
	return service, nil
}

func templateKind(data []byte) (string, error) {
	var header struct {
		Kind string `yaml:"kind"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&header); err != nil {
		return "", err
	}
	if strings.TrimSpace(header.Kind) == "" {
		return "", fmt.Errorf("kind is required")
	}
	return header.Kind, nil
}

func profileFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	files := []string{}
	for _, entry := range entries {
		if entry.IsDir() || !isYAML(entry.Name()) {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}
	sort.Strings(files)
	return files, nil
}

func looksLikePath(value string) bool {
	return strings.ContainsRune(value, os.PathSeparator) || strings.HasSuffix(value, ".yaml") || strings.HasSuffix(value, ".yml")
}

func looksLikeTemplatePath(value string) bool {
	return strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".yaml") || strings.HasSuffix(value, ".yml")
}

func isYAML(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}
