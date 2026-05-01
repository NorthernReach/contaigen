package dockerx

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/NorthernReach/contaigen/internal/model"
	"github.com/containerd/errdefs"
	dockernetwork "github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

var (
	ErrNetworkNotFound = errors.New("network not found")
	ErrNetworkExists   = errors.New("network already exists")
)

func (c *Client) EnsureNetwork(ctx context.Context, req model.EnsureNetworkRequest) (model.Network, []string, error) {
	network, err := c.inspectNetwork(ctx, req.Name)
	if err == nil {
		if network.Labels[model.LabelManaged] != "true" {
			return model.Network{}, nil, fmt.Errorf("%w but is not Contaigen-managed: %s", ErrNetworkExists, req.Name)
		}
		return network, nil, nil
	}
	if !errors.Is(err, ErrNetworkNotFound) {
		return model.Network{}, nil, err
	}

	driver := req.Driver
	if driver == "" {
		driver = model.DefaultNetworkDriver
	}
	profile := req.Profile
	if profile == "" {
		profile = model.NetworkProfileSegment
	}

	resp, err := c.api.NetworkCreate(ctx, req.Name, client.NetworkCreateOptions{
		Driver:     driver,
		Internal:   req.Internal,
		Attachable: req.Attachable,
		Labels:     model.NetworkLabels(req.Name, profile),
	})
	if err != nil {
		return model.Network{}, nil, err
	}

	network, err = c.inspectNetwork(ctx, resp.ID)
	if err != nil {
		return model.Network{}, resp.Warning, err
	}
	return network, resp.Warning, nil
}

func (c *Client) ListNetworks(ctx context.Context) ([]model.Network, error) {
	filters := make(client.Filters).Add("label", model.LabelManaged+"=true")
	resp, err := c.api.NetworkList(ctx, client.NetworkListOptions{Filters: filters})
	if err != nil {
		return nil, err
	}

	networks := make([]model.Network, 0, len(resp.Items))
	for _, item := range resp.Items {
		if item.Labels[model.LabelKind] != model.KindNetwork {
			continue
		}
		networks = append(networks, networkFromSummary(item))
	}
	sort.Slice(networks, func(i, j int) bool {
		return networks[i].Name < networks[j].Name
	})
	return networks, nil
}

func (c *Client) InspectNetwork(ctx context.Context, name string) (model.Network, error) {
	return c.inspectNetwork(ctx, name)
}

func (c *Client) RemoveNetwork(ctx context.Context, name string) error {
	network, err := c.inspectNetwork(ctx, name)
	if err != nil {
		return err
	}
	if network.Labels[model.LabelManaged] != "true" || network.Labels[model.LabelKind] != model.KindNetwork {
		return fmt.Errorf("refusing to remove non-Contaigen network %q", name)
	}
	_, err = c.api.NetworkRemove(ctx, network.ID, client.NetworkRemoveOptions{})
	return err
}

func (c *Client) NetworkMap(ctx context.Context) (model.NetworkMap, error) {
	envs, err := c.ListEnvironments(ctx)
	if err != nil {
		return model.NetworkMap{}, err
	}
	envByID := make(map[string]model.Environment, len(envs)*2)
	for _, env := range envs {
		envByID[env.ID] = env
		if len(env.ID) >= 12 {
			envByID[env.ID[:12]] = env
		}
	}

	resp, err := c.api.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return model.NetworkMap{}, err
	}

	networks := []model.Network{}
	for _, item := range resp.Items {
		network, err := c.inspectNetwork(ctx, item.ID)
		if err != nil {
			return model.NetworkMap{}, err
		}

		hasManagedAttachment := false
		for i := range network.Attachments {
			if env, ok := envByID[network.Attachments[i].ContainerID]; ok {
				network.Attachments[i].EnvironmentName = env.Name
				hasManagedAttachment = true
				continue
			}
			if len(network.Attachments[i].ContainerID) >= 12 {
				if env, ok := envByID[network.Attachments[i].ContainerID[:12]]; ok {
					network.Attachments[i].EnvironmentName = env.Name
					hasManagedAttachment = true
				}
			}
		}

		if network.Labels[model.LabelManaged] == "true" || hasManagedAttachment {
			networks = append(networks, network)
		}
	}
	sort.Slice(networks, func(i, j int) bool {
		return networks[i].Name < networks[j].Name
	})

	return model.NetworkMap{Networks: networks}, nil
}

func (c *Client) inspectNetwork(ctx context.Context, name string) (model.Network, error) {
	resp, err := c.api.NetworkInspect(ctx, name, client.NetworkInspectOptions{})
	if err != nil {
		if errdefs.IsNotFound(err) {
			return model.Network{}, fmt.Errorf("%w: %s", ErrNetworkNotFound, name)
		}
		return model.Network{}, err
	}
	return networkFromInspect(resp.Network), nil
}

func networkFromSummary(summary dockernetwork.Summary) model.Network {
	return model.Network{
		ID:         summary.ID,
		Name:       summary.Name,
		Driver:     summary.Driver,
		Scope:      summary.Scope,
		Profile:    networkProfileFromLabels(summary.Labels),
		Internal:   summary.Internal,
		Attachable: summary.Attachable,
		CreatedAt:  summary.Created,
		Labels:     cloneLabels(summary.Labels),
	}
}

func networkFromInspect(inspect dockernetwork.Inspect) model.Network {
	attachments := make([]model.NetworkAttachment, 0, len(inspect.Containers))
	for id, endpoint := range inspect.Containers {
		attachment := model.NetworkAttachment{
			ContainerID:   id,
			ContainerName: endpoint.Name,
			EndpointID:    endpoint.EndpointID,
		}
		if endpoint.IPv4Address.IsValid() {
			attachment.IPv4Address = endpoint.IPv4Address.String()
		}
		if endpoint.IPv6Address.IsValid() {
			attachment.IPv6Address = endpoint.IPv6Address.String()
		}
		if len(endpoint.MacAddress) > 0 {
			attachment.MacAddress = endpoint.MacAddress.String()
		}
		attachments = append(attachments, attachment)
	}
	sort.Slice(attachments, func(i, j int) bool {
		return attachments[i].ContainerName < attachments[j].ContainerName
	})

	return model.Network{
		ID:          inspect.ID,
		Name:        inspect.Name,
		Driver:      inspect.Driver,
		Scope:       inspect.Scope,
		Profile:     networkProfileFromLabels(inspect.Labels),
		Internal:    inspect.Internal,
		Attachable:  inspect.Attachable,
		CreatedAt:   inspect.Created,
		Labels:      cloneLabels(inspect.Labels),
		Attachments: attachments,
	}
}

func networkProfileFromLabels(labels map[string]string) string {
	if labels == nil {
		return ""
	}
	return labels[model.LabelNetworkProfile]
}
