package docker

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/docker/docker/api/types/filters"
	dockernetwork "github.com/docker/docker/api/types/network"
)

// CreateNetworkOptions is a new network's definition.
type CreateNetworkOptions struct {
	Name string
	// Driver is bridge (default), macvlan, ipvlan, overlay or a plugin's name.
	Driver string
	// Internal cuts the network off from the outside world.
	Internal bool
	// Attachable lets standalone containers join a swarm-scoped network.
	Attachable bool
	EnableIPv6 bool
	// IPAM holds the subnets. Empty leaves the engine to pick one.
	IPAM []IPAMConfig
	// Options are driver-specific, e.g. parent=eth0 for macvlan.
	Options map[string]string
	Labels  map[string]string
}

// CreateNetwork creates a network and returns the engine's view of it.
func (e *engine) CreateNetwork(ctx context.Context, opts CreateNetworkOptions) (Network, error) {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		return Network{}, NewError(KindInvalid, "network.create", "network", "",
			"a network needs a name")
	}

	create := dockernetwork.CreateOptions{
		Driver:     strings.TrimSpace(opts.Driver),
		Internal:   opts.Internal,
		Attachable: opts.Attachable,
		EnableIPv6: &opts.EnableIPv6,
		Options:    opts.Options,
		Labels:     opts.Labels,
	}
	if len(opts.IPAM) > 0 {
		configs := make([]dockernetwork.IPAMConfig, 0, len(opts.IPAM))
		for _, c := range opts.IPAM {
			configs = append(configs, dockernetwork.IPAMConfig{
				Subnet:  c.Subnet,
				Gateway: c.Gateway,
				IPRange: c.IPRange,
			})
		}
		create.IPAM = &dockernetwork.IPAM{Driver: "default", Config: configs}
	}

	resp, err := e.api.NetworkCreate(ctx, name, create)
	if err != nil {
		return Network{}, classify("network.create", "network", name, err)
	}

	// The create response carries only an ID and warnings, so the network is
	// read back: the caller wants the object, not the receipt.
	return e.InspectNetwork(ctx, resp.ID)
}

// InspectNetwork returns one network, including its attached containers.
func (e *engine) InspectNetwork(ctx context.Context, id string) (Network, error) {
	found, err := e.api.NetworkInspect(ctx, id, dockernetwork.InspectOptions{})
	if err != nil {
		return Network{}, classify("network.inspect", "network", id, err)
	}
	return toNetwork(found), nil
}

// InspectNetworkRaw returns the engine's unmodified network payload.
func (e *engine) InspectNetworkRaw(ctx context.Context, id string) (RawInspect, error) {
	found, err := e.api.NetworkInspect(ctx, id, dockernetwork.InspectOptions{Verbose: true})
	if err != nil {
		return nil, classify("network.inspect", "network", id, err)
	}
	payload, err := json.Marshal(found)
	if err != nil {
		return nil, NewError(KindUnknown, "network.inspect", "network", id,
			"could not encode the engine's response: "+err.Error())
	}
	return payload, nil
}

// RemoveNetwork deletes a network. The engine refuses while containers are
// still attached, which is the behavior the operator wants.
func (e *engine) RemoveNetwork(ctx context.Context, id string) error {
	if err := e.api.NetworkRemove(ctx, id); err != nil {
		return classify("network.remove", "network", id, err)
	}
	return nil
}

// PruneNetworks removes user-defined networks with no containers on them.
func (e *engine) PruneNetworks(ctx context.Context) (PruneReport, error) {
	report, err := e.api.NetworksPrune(ctx, filters.NewArgs())
	if err != nil {
		return PruneReport{}, classify("network.prune", "network", "", err)
	}
	// Networks occupy no disk, so the engine reports no reclaimed space.
	return PruneReport{Deleted: sortedStrings(report.NetworksDeleted)}, nil
}

// ConnectOptions are the endpoint settings for attaching a container.
type ConnectOptions struct {
	Aliases     []string
	IPv4Address string
	IPv6Address string
}

// ConnectNetwork attaches a container to a network.
func (e *engine) ConnectNetwork(ctx context.Context, networkID, containerID string, opts ConnectOptions) error {
	settings := &dockernetwork.EndpointSettings{Aliases: opts.Aliases}
	if opts.IPv4Address != "" || opts.IPv6Address != "" {
		settings.IPAMConfig = &dockernetwork.EndpointIPAMConfig{
			IPv4Address: opts.IPv4Address,
			IPv6Address: opts.IPv6Address,
		}
	}

	if err := e.api.NetworkConnect(ctx, networkID, containerID, settings); err != nil {
		return classify("network.connect", "network", networkID, err)
	}
	return nil
}

// DisconnectNetwork detaches a container. Force disconnects one the engine
// still considers busy.
func (e *engine) DisconnectNetwork(ctx context.Context, networkID, containerID string, force bool) error {
	if err := e.api.NetworkDisconnect(ctx, networkID, containerID, force); err != nil {
		return classify("network.disconnect", "network", networkID, err)
	}
	return nil
}
