package model

import (
	"io"
	"strings"
	"time"
)

const (
	DefaultEnvironmentImage   = "parrotsec/security"
	DefaultDesktopImage       = "kasmweb/parrotos-6-desktop:1.18.0"
	DefaultEnvironmentShell   = "/bin/bash"
	DefaultNetworkMode        = NetworkProfileBridge
	DefaultDesktopProtocol    = "novnc"
	DefaultDesktopHostIP      = "127.0.0.1"
	DefaultDesktopPort        = "6901"
	DefaultDesktopScheme      = "https"
	DefaultDesktopPath        = "/"
	DefaultDesktopUser        = "kasm_user"
	DefaultDesktopPasswordEnv = "VNC_PW"
	DefaultDesktopShmSize     = 512 * 1024 * 1024
)

type Environment struct {
	ID                 string
	Name               string
	ContainerName      string
	Image              string
	State              string
	Status             string
	Shell              string
	User               string
	Hostname           string
	NetworkProfile     string
	NetworkName        string
	NetworkMode        string
	VPNName            string
	WorkspaceName      string
	WorkspacePath      string
	WorkspaceMountPath string
	Desktop            DesktopConfig
	CreatedAt          time.Time
	Ports              []PortMapping
	Volumes            []VolumeMount
	CapAdd             []string
	Labels             map[string]string
}

type CreateEnvironmentRequest struct {
	Name               string
	Image              string
	Shell              string
	User               string
	Hostname           string
	NetworkProfile     string
	NetworkName        string
	NetworkMode        string
	VPNName            string
	WorkingDir         string
	WorkspaceName      string
	WorkspacePath      string
	WorkspaceMountPath string
	DisableWorkspace   bool
	Desktop            DesktopConfig
	Env                []string
	Ports              []PortMapping
	Volumes            []VolumeMount
	CapAdd             []string
	UseImageCommand    bool
	ShmSize            int64
	Pull               bool
	Start              bool
}

type RemoveEnvironmentRequest struct {
	Force         bool
	RemoveVolumes bool
}

type EnterEnvironmentRequest struct {
	Command []string
	User    string
	WorkDir string
}

type ExecIO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

type PortMapping struct {
	HostIP        string `yaml:"hostIP,omitempty"`
	HostPort      string `yaml:"hostPort"`
	ContainerPort string `yaml:"containerPort"`
	Protocol      string `yaml:"protocol,omitempty"`
}

type VolumeMount struct {
	Source   string `yaml:"source"`
	Target   string `yaml:"target"`
	ReadOnly bool   `yaml:"readOnly,omitempty"`
}

type DesktopConfig struct {
	Enabled       bool   `yaml:"enabled,omitempty"`
	Protocol      string `yaml:"protocol,omitempty"`
	HostIP        string `yaml:"hostIP,omitempty"`
	HostPort      string `yaml:"hostPort,omitempty"`
	ContainerPort string `yaml:"containerPort,omitempty"`
	Scheme        string `yaml:"scheme,omitempty"`
	Path          string `yaml:"path,omitempty"`
	User          string `yaml:"user,omitempty"`
	Password      string `yaml:"password,omitempty"`
	PasswordEnv   string `yaml:"passwordEnv,omitempty"`
}

func HasPublishedPort(ports []PortMapping, hostIP string, hostPort string, containerPort string) bool {
	for _, port := range ports {
		protocol := port.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		if !strings.EqualFold(protocol, "tcp") {
			continue
		}
		if port.HostPort != hostPort || port.ContainerPort != containerPort {
			continue
		}
		if hostIP == "" || port.HostIP == "" || port.HostIP == "0.0.0.0" || port.HostIP == "::" || port.HostIP == hostIP {
			return true
		}
	}
	return false
}
