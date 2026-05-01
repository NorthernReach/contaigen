package composex

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/NorthernReach/contaigen/internal/model"
	"gopkg.in/yaml.v3"
)

var composeServiceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

type Manager interface {
	ValidateFile(context.Context, string) (ProjectSummary, error)
	ValidateWithDocker(context.Context, string, model.ExecIO) error
	Up(context.Context, UpRequest, model.ExecIO) error
	Down(context.Context, DownRequest, model.ExecIO) error
}

type CommandRunner interface {
	Run(context.Context, string, []string, []string, io.Writer, io.Writer) error
}

type CLI struct {
	binary string
	runner CommandRunner
}

type Option func(*CLI)

type ProjectSummary struct {
	Name     string
	Path     string
	Services []ServiceSummary
}

type ServiceSummary struct {
	Name     string
	Image    string
	HasBuild bool
}

type UpRequest struct {
	File        string
	ProjectName string
	EnvName     string
	NetworkName string
	Detach      bool
}

type DownRequest struct {
	File          string
	ProjectName   string
	EnvName       string
	NetworkName   string
	RemoveVolumes bool
}

func New(opts ...Option) *CLI {
	cli := &CLI{
		binary: "docker",
		runner: execRunner{},
	}
	for _, opt := range opts {
		opt(cli)
	}
	return cli
}

func WithBinary(binary string) Option {
	return func(cli *CLI) {
		cli.binary = binary
	}
}

func WithRunner(runner CommandRunner) Option {
	return func(cli *CLI) {
		cli.runner = runner
	}
}

func (c *CLI) ValidateFile(ctx context.Context, path string) (ProjectSummary, error) {
	if err := ctx.Err(); err != nil {
		return ProjectSummary{}, err
	}
	return ParseFile(path)
}

func (c *CLI) ValidateWithDocker(ctx context.Context, path string, streams model.ExecIO) error {
	project, err := ParseFile(path)
	if err != nil {
		return err
	}
	return c.run(ctx, []string{"compose", "-f", project.Path, "config", "--quiet"}, nil, streams)
}

func (c *CLI) Up(ctx context.Context, req UpRequest, streams model.ExecIO) error {
	req, project, err := normalizeUpRequest(req)
	if err != nil {
		return err
	}
	override, cleanup, err := writeNetworkOverride(project, req.ProjectName, req.EnvName, req.NetworkName)
	if err != nil {
		return err
	}
	defer cleanup()

	args := []string{"compose", "-f", project.Path, "-f", override, "-p", req.ProjectName, "up"}
	if req.Detach {
		args = append(args, "-d")
	}
	return c.run(ctx, args, composeEnv(req.EnvName, req.NetworkName), streams)
}

func (c *CLI) Down(ctx context.Context, req DownRequest, streams model.ExecIO) error {
	req, project, err := normalizeDownRequest(req)
	if err != nil {
		return err
	}
	override, cleanup, err := writeNetworkOverride(project, req.ProjectName, req.EnvName, req.NetworkName)
	if err != nil {
		return err
	}
	defer cleanup()

	args := []string{"compose", "-f", project.Path, "-f", override, "-p", req.ProjectName, "down"}
	if req.RemoveVolumes {
		args = append(args, "--volumes")
	}
	return c.run(ctx, args, composeEnv(req.EnvName, req.NetworkName), streams)
}

func ParseFile(path string) (ProjectSummary, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ProjectSummary{}, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return ProjectSummary{}, err
	}
	return parse(data, abs)
}

func parse(data []byte, path string) (ProjectSummary, error) {
	var doc struct {
		Name     string                    `yaml:"name"`
		Services map[string]composeService `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return ProjectSummary{}, err
	}
	if len(doc.Services) == 0 {
		return ProjectSummary{}, fmt.Errorf("compose file must define at least one service")
	}

	services := make([]ServiceSummary, 0, len(doc.Services))
	for name, service := range doc.Services {
		name = strings.TrimSpace(name)
		if name == "" {
			return ProjectSummary{}, fmt.Errorf("compose service name is required")
		}
		if !composeServiceNamePattern.MatchString(name) {
			return ProjectSummary{}, fmt.Errorf("compose service name %q must start with a letter or number and contain only letters, numbers, dots, underscores, or dashes", name)
		}
		if strings.TrimSpace(service.Image) == "" && service.Build == nil {
			return ProjectSummary{}, fmt.Errorf("compose service %q must define image or build", name)
		}
		services = append(services, ServiceSummary{
			Name:     name,
			Image:    service.Image,
			HasBuild: service.Build != nil,
		})
	}
	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	return ProjectSummary{
		Name:     strings.TrimSpace(doc.Name),
		Path:     path,
		Services: services,
	}, nil
}

type composeService struct {
	Image string `yaml:"image"`
	Build any    `yaml:"build"`
}

func normalizeUpRequest(req UpRequest) (UpRequest, ProjectSummary, error) {
	project, err := ParseFile(req.File)
	if err != nil {
		return UpRequest{}, ProjectSummary{}, err
	}
	req.File = project.Path
	req.ProjectName = normalizeProjectName(req.ProjectName, req.EnvName)
	req.EnvName = strings.TrimSpace(req.EnvName)
	req.NetworkName = strings.TrimSpace(req.NetworkName)
	if req.EnvName == "" {
		return UpRequest{}, ProjectSummary{}, fmt.Errorf("environment name is required")
	}
	if req.NetworkName == "" {
		return UpRequest{}, ProjectSummary{}, fmt.Errorf("network name is required")
	}
	return req, project, nil
}

func normalizeDownRequest(req DownRequest) (DownRequest, ProjectSummary, error) {
	project, err := ParseFile(req.File)
	if err != nil {
		return DownRequest{}, ProjectSummary{}, err
	}
	req.File = project.Path
	req.ProjectName = normalizeProjectName(req.ProjectName, req.EnvName)
	req.EnvName = strings.TrimSpace(req.EnvName)
	req.NetworkName = strings.TrimSpace(req.NetworkName)
	if req.EnvName == "" {
		return DownRequest{}, ProjectSummary{}, fmt.Errorf("environment name is required")
	}
	if req.NetworkName == "" {
		return DownRequest{}, ProjectSummary{}, fmt.Errorf("network name is required")
	}
	return req, project, nil
}

func normalizeProjectName(projectName string, envName string) string {
	projectName = strings.TrimSpace(projectName)
	if projectName != "" {
		return projectName
	}
	envName = strings.TrimSpace(envName)
	if envName == "" {
		return "contaigen-compose"
	}
	return "contaigen-" + envName
}

func writeNetworkOverride(project ProjectSummary, projectName string, envName string, networkName string) (string, func(), error) {
	var doc struct {
		Services map[string]overrideService `yaml:"services"`
		Networks map[string]overrideNetwork `yaml:"networks"`
	}
	doc.Services = make(map[string]overrideService, len(project.Services))
	for _, service := range project.Services {
		doc.Services[service.Name] = overrideService{
			Labels: map[string]string{
				model.LabelManaged:             "true",
				model.LabelEnv:                 envName,
				model.LabelKind:                model.KindService,
				model.LabelService:             service.Name,
				model.LabelNetworkName:         networkName,
				model.LabelServiceAlias:        service.Name,
				"io.contaigen.compose.project": projectName,
			},
			Networks: map[string]overrideServiceNetwork{
				"default": {Aliases: []string{service.Name}},
			},
		}
	}
	doc.Networks = map[string]overrideNetwork{
		"default": {
			External: true,
			Name:     networkName,
		},
	}

	data, err := yaml.Marshal(doc)
	if err != nil {
		return "", nil, err
	}
	file, err := os.CreateTemp("", "contaigen-compose-*.yaml")
	if err != nil {
		return "", nil, err
	}
	path := file.Name()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", nil, err
	}
	return path, func() { _ = os.Remove(path) }, nil
}

type overrideService struct {
	Labels   map[string]string                 `yaml:"labels"`
	Networks map[string]overrideServiceNetwork `yaml:"networks"`
}

type overrideServiceNetwork struct {
	Aliases []string `yaml:"aliases,omitempty"`
}

type overrideNetwork struct {
	External bool   `yaml:"external"`
	Name     string `yaml:"name"`
}

func composeEnv(envName string, networkName string) []string {
	return []string{
		"CONTAIGEN_ENV=" + envName,
		"CONTAIGEN_NETWORK=" + networkName,
	}
}

func (c *CLI) run(ctx context.Context, args []string, env []string, streams model.ExecIO) error {
	stdout := streams.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	stderr := streams.Stderr
	if stderr == nil {
		stderr = io.Discard
	}
	if c.runner == nil {
		c.runner = execRunner{}
	}
	if c.binary == "" {
		c.binary = "docker"
	}
	return c.runner.Run(ctx, c.binary, args, env, stdout, stderr)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, binary string, args []string, env []string, stdout io.Writer, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	var errBuf bytes.Buffer
	if stderr == io.Discard {
		cmd.Stderr = &errBuf
	}
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(errBuf.String())
		if detail == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, detail)
	}
	return nil
}
