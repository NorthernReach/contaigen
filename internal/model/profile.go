package model

const (
	ProfileAPIVersion = "contaigen.io/v1alpha1"
	ProfileKind       = "EnvironmentProfile"
	ServiceKind       = "ServiceTemplate"
)

type EnvironmentProfile struct {
	APIVersion string                 `yaml:"apiVersion"`
	Kind       string                 `yaml:"kind"`
	Metadata   EnvironmentProfileMeta `yaml:"metadata"`
	Spec       EnvironmentProfileSpec `yaml:"spec"`
	Source     string                 `yaml:"-"`
	Path       string                 `yaml:"-"`
}

type EnvironmentProfileMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

type EnvironmentProfileSpec struct {
	Image      string           `yaml:"image"`
	Shell      string           `yaml:"shell,omitempty"`
	User       string           `yaml:"user,omitempty"`
	Hostname   string           `yaml:"hostname,omitempty"`
	WorkingDir string           `yaml:"workingDir,omitempty"`
	Network    ProfileNetwork   `yaml:"network,omitempty"`
	Workspace  ProfileWorkspace `yaml:"workspace,omitempty"`
	Desktop    DesktopConfig    `yaml:"desktop,omitempty"`
	Env        []string         `yaml:"env,omitempty"`
	Ports      []PortMapping    `yaml:"ports,omitempty"`
	Volumes    []VolumeMount    `yaml:"volumes,omitempty"`
	CapAdd     []string         `yaml:"capAdd,omitempty"`
	Pull       *bool            `yaml:"pull,omitempty"`
	Start      *bool            `yaml:"start,omitempty"`
}

type ProfileNetwork struct {
	Profile string `yaml:"profile,omitempty"`
	Name    string `yaml:"name,omitempty"`
}

type ProfileWorkspace struct {
	Name      string `yaml:"name,omitempty"`
	Path      string `yaml:"path,omitempty"`
	MountPath string `yaml:"mountPath,omitempty"`
	Disabled  bool   `yaml:"disabled,omitempty"`
}

type ProfileSummary struct {
	Name        string
	Description string
	Image       string
	Network     string
	Source      string
	Path        string
}

type ServiceTemplate struct {
	APIVersion string              `yaml:"apiVersion"`
	Kind       string              `yaml:"kind"`
	Metadata   ServiceTemplateMeta `yaml:"metadata"`
	Spec       ServiceTemplateSpec `yaml:"spec"`
	Source     string              `yaml:"-"`
	Path       string              `yaml:"-"`
}

type ServiceTemplateMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

type ServiceTemplateSpec struct {
	Image        string        `yaml:"image"`
	Name         string        `yaml:"name,omitempty"`
	NetworkAlias string        `yaml:"alias,omitempty"`
	Env          []string      `yaml:"env,omitempty"`
	Ports        []PortMapping `yaml:"ports,omitempty"`
	Volumes      []VolumeMount `yaml:"volumes,omitempty"`
	Command      []string      `yaml:"command,omitempty"`
	Pull         *bool         `yaml:"pull,omitempty"`
	Start        *bool         `yaml:"start,omitempty"`
}

type ServiceTemplateSummary struct {
	Name        string
	Description string
	Image       string
	Alias       string
	Source      string
	Path        string
}
