package model

import "time"

const (
	NetworkProfileBridge   = "bridge"
	NetworkProfileIsolated = "isolated"
	NetworkProfileHost     = "host"
	NetworkProfileSegment  = "segment"
	NetworkProfileVPN      = "vpn"

	DefaultNetworkProfile = NetworkProfileBridge
	DefaultNetworkDriver  = "bridge"
)

type Network struct {
	ID          string
	Name        string
	Driver      string
	Scope       string
	Profile     string
	Internal    bool
	Attachable  bool
	CreatedAt   time.Time
	Labels      map[string]string
	Attachments []NetworkAttachment
}

type NetworkAttachment struct {
	ContainerID     string
	ContainerName   string
	EndpointID      string
	IPv4Address     string
	IPv6Address     string
	MacAddress      string
	EnvironmentName string
}

type EnsureNetworkRequest struct {
	Name       string
	Profile    string
	Driver     string
	Internal   bool
	Attachable bool
}

type NetworkMap struct {
	Networks []Network
}
