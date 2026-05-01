package model

import (
	"io"
	"time"
)

const (
	DefaultVPNImage       = "dperson/openvpn-client"
	DefaultVPNProvider    = "openvpn"
	DefaultVPNConfigMount = "/vpn"

	VPNRouteModeFull  = "full"
	VPNRouteModeSplit = "split"
)

type VPNGateway struct {
	ID              string
	Name            string
	ContainerName   string
	Image           string
	Provider        string
	RouteMode       string
	Routes          []VPNRoute
	State           string
	Status          string
	ConfigPath      string
	ConfigMountPath string
	CreatedAt       time.Time
	Ports           []PortMapping
	NoVNCPorts      []PortMapping
	Volumes         []VolumeMount
	Env             []string
	Command         []string
	CapAdd          []string
	Devices         []DeviceMapping
	Labels          map[string]string
}

type CreateVPNGatewayRequest struct {
	Name            string
	Image           string
	Provider        string
	RouteMode       string
	Routes          []VPNRoute
	ConfigPath      string
	ConfigMountPath string
	Env             []string
	Ports           []PortMapping
	NoVNCPorts      []PortMapping
	Volumes         []VolumeMount
	Command         []string
	CapAdd          []string
	Devices         []DeviceMapping
	Pull            bool
	Start           bool
	Privileged      bool
}

type RemoveVPNGatewayRequest struct {
	Force         bool
	RemoveVolumes bool
}

type VPNLogsRequest struct {
	Follow bool
	Tail   string
}

type VPNLogIO struct {
	Stdout io.Writer
	Stderr io.Writer
}

type DeviceMapping struct {
	HostPath      string
	ContainerPath string
	Permissions   string
}

type VPNRoute struct {
	CIDR      string
	Directive string
}
