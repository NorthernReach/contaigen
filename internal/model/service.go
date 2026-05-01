package model

import "time"

type Service struct {
	ID              string
	Name            string
	ContainerName   string
	EnvironmentName string
	Image           string
	State           string
	Status          string
	NetworkName     string
	NetworkAlias    string
	CreatedAt       time.Time
	Ports           []PortMapping
	Volumes         []VolumeMount
	Command         []string
	Labels          map[string]string
}

type CreateServiceRequest struct {
	EnvironmentName string
	Name            string
	Image           string
	NetworkName     string
	NetworkAlias    string
	Env             []string
	Ports           []PortMapping
	Volumes         []VolumeMount
	Command         []string
	Pull            bool
	Start           bool
}

type RemoveServiceRequest struct {
	Force         bool
	RemoveVolumes bool
}
