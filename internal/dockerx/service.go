package dockerx

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/NorthernReach/contaigen/internal/model"
	"github.com/moby/moby/api/types/container"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

var (
	ErrServiceNotFound = errors.New("service not found")
	ErrServiceExists   = errors.New("service already exists")
)

func (c *Client) CreateService(ctx context.Context, req model.CreateServiceRequest) (model.Service, []string, error) {
	if _, err := c.findService(ctx, req.EnvironmentName, req.Name); err == nil {
		return model.Service{}, nil, fmt.Errorf("%w: %s/%s", ErrServiceExists, req.EnvironmentName, req.Name)
	} else if !errors.Is(err, ErrServiceNotFound) {
		return model.Service{}, nil, err
	}

	labels := model.ServiceLabels(req.EnvironmentName, req.Name)
	labels[model.LabelNetworkName] = req.NetworkName
	if req.NetworkAlias != "" {
		labels[model.LabelServiceAlias] = req.NetworkAlias
	}

	portSet, portMap, err := dockerPorts(req.Ports)
	if err != nil {
		return model.Service{}, nil, err
	}

	resp, err := c.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: serviceContainerName(req.EnvironmentName, req.Name),
		Config: &container.Config{
			Image:        req.Image,
			Env:          req.Env,
			Cmd:          req.Command,
			ExposedPorts: portSet,
			Labels:       labels,
		},
		HostConfig: &container.HostConfig{
			NetworkMode:  container.NetworkMode(req.NetworkName),
			PortBindings: portMap,
			Mounts:       dockerMounts(req.Volumes),
		},
		NetworkingConfig: serviceNetworkingConfig(req.NetworkName, req.NetworkAlias),
	})
	if err != nil {
		return model.Service{}, nil, err
	}

	service, err := c.InspectService(ctx, req.EnvironmentName, req.Name)
	if err != nil {
		return model.Service{}, resp.Warnings, err
	}
	return service, resp.Warnings, nil
}

func (c *Client) ListServices(ctx context.Context, envName string) ([]model.Service, error) {
	filters := make(client.Filters).
		Add("label", model.LabelManaged+"=true").
		Add("label", model.LabelKind+"="+model.KindService)
	if envName != "" {
		filters.Add("label", model.LabelEnv+"="+envName)
	}

	resp, err := c.api.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: filters,
	})
	if err != nil {
		return nil, err
	}

	services := make([]model.Service, 0, len(resp.Items))
	for _, item := range resp.Items {
		services = append(services, serviceFromSummary(item))
	}
	sort.Slice(services, func(i, j int) bool {
		if services[i].EnvironmentName == services[j].EnvironmentName {
			return services[i].Name < services[j].Name
		}
		return services[i].EnvironmentName < services[j].EnvironmentName
	})
	return services, nil
}

func (c *Client) InspectService(ctx context.Context, envName string, serviceName string) (model.Service, error) {
	service, err := c.findService(ctx, envName, serviceName)
	if err != nil {
		return model.Service{}, err
	}

	resp, err := c.api.ContainerInspect(ctx, service.ID, client.ContainerInspectOptions{})
	if err != nil {
		return model.Service{}, err
	}
	return serviceFromInspect(resp.Container), nil
}

func (c *Client) StartService(ctx context.Context, envName string, serviceName string) error {
	service, err := c.findService(ctx, envName, serviceName)
	if err != nil {
		return err
	}
	_, err = c.api.ContainerStart(ctx, service.ID, client.ContainerStartOptions{})
	return err
}

func (c *Client) StopService(ctx context.Context, envName string, serviceName string, timeout *int) error {
	service, err := c.findService(ctx, envName, serviceName)
	if err != nil {
		return err
	}
	_, err = c.api.ContainerStop(ctx, service.ID, client.ContainerStopOptions{Timeout: timeout})
	return err
}

func (c *Client) RemoveService(ctx context.Context, envName string, serviceName string, req model.RemoveServiceRequest) error {
	service, err := c.findService(ctx, envName, serviceName)
	if err != nil {
		return err
	}
	_, err = c.api.ContainerRemove(ctx, service.ID, client.ContainerRemoveOptions{
		Force:         req.Force,
		RemoveVolumes: req.RemoveVolumes,
	})
	return err
}

func (c *Client) findService(ctx context.Context, envName string, serviceName string) (model.Service, error) {
	filters := make(client.Filters).
		Add("label", model.LabelManaged+"=true").
		Add("label", model.LabelKind+"="+model.KindService).
		Add("label", model.LabelEnv+"="+envName).
		Add("label", model.LabelService+"="+serviceName)

	resp, err := c.api.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: filters,
	})
	if err != nil {
		return model.Service{}, err
	}
	if len(resp.Items) == 0 {
		return model.Service{}, fmt.Errorf("%w: %s/%s", ErrServiceNotFound, envName, serviceName)
	}
	if len(resp.Items) > 1 {
		return model.Service{}, fmt.Errorf("multiple Contaigen services named %q for environment %q", serviceName, envName)
	}
	return serviceFromSummary(resp.Items[0]), nil
}

func serviceNetworkingConfig(networkName string, alias string) *dockernetwork.NetworkingConfig {
	if networkName == "" {
		return nil
	}
	endpoint := &dockernetwork.EndpointSettings{}
	if alias != "" {
		endpoint.Aliases = []string{alias}
	}
	return &dockernetwork.NetworkingConfig{
		EndpointsConfig: map[string]*dockernetwork.EndpointSettings{
			networkName: endpoint,
		},
	}
}

func serviceFromSummary(summary container.Summary) model.Service {
	labels := cloneLabels(summary.Labels)
	return model.Service{
		ID:              summary.ID,
		Name:            labels[model.LabelService],
		ContainerName:   cleanContainerName(summary.Names),
		EnvironmentName: labels[model.LabelEnv],
		Image:           summary.Image,
		State:           string(summary.State),
		Status:          summary.Status,
		NetworkName:     networkNameFromSummary(summary),
		NetworkAlias:    labels[model.LabelServiceAlias],
		CreatedAt:       time.Unix(summary.Created, 0),
		Ports:           portsFromSummary(summary.Ports),
		Volumes:         volumesFromMountPoints(summary.Mounts),
		Labels:          labels,
	}
}

func serviceFromInspect(inspect container.InspectResponse) model.Service {
	labels := map[string]string{}
	image := inspect.Image
	command := []string(nil)
	if inspect.Config != nil {
		labels = cloneLabels(inspect.Config.Labels)
		image = inspect.Config.Image
		command = append([]string(nil), inspect.Config.Cmd...)
	}

	state := ""
	status := ""
	if inspect.State != nil {
		state = string(inspect.State.Status)
		status = string(inspect.State.Status)
	}

	ports := []model.PortMapping(nil)
	if inspect.HostConfig != nil {
		ports = portsFromPortMap(inspect.HostConfig.PortBindings)
	}

	createdAt, _ := time.Parse(time.RFC3339Nano, inspect.Created)

	return model.Service{
		ID:              inspect.ID,
		Name:            labels[model.LabelService],
		ContainerName:   strings.TrimPrefix(inspect.Name, "/"),
		EnvironmentName: labels[model.LabelEnv],
		Image:           image,
		State:           state,
		Status:          status,
		NetworkName:     networkNameFromInspect(labels, inspect),
		NetworkAlias:    labels[model.LabelServiceAlias],
		CreatedAt:       createdAt,
		Ports:           ports,
		Volumes:         volumesFromMountPoints(inspect.Mounts),
		Command:         command,
		Labels:          labels,
	}
}

func serviceContainerName(envName string, serviceName string) string {
	return "contaigen-" + envName + "-svc-" + serviceName
}
