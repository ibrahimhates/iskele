package docker

import (
	"context"
	"sort"

	"github.com/docker/docker/api/types/network"
)

// ListNetworks returns every network known to the engine, sorted by name.
func (e *engine) ListNetworks(ctx context.Context) ([]Network, error) {
	summaries, err := e.api.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, classify("network.list", "network", "", err)
	}

	out := make([]Network, 0, len(summaries))
	for _, s := range summaries {
		out = append(out, toNetwork(s))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func toNetwork(s network.Summary) Network {
	n := Network{
		ID:             s.ID,
		Name:           s.Name,
		Driver:         s.Driver,
		Scope:          s.Scope,
		Created:        s.Created.UTC(),
		Internal:       s.Internal,
		Attachable:     s.Attachable,
		Ingress:        s.Ingress,
		EnableIPv6:     s.EnableIPv6,
		Labels:         nonNilMap(s.Labels),
		ContainerCount: len(s.Containers),
		IPAM:           make([]IPAMConfig, 0, len(s.IPAM.Config)),
	}
	for _, c := range s.IPAM.Config {
		n.IPAM = append(n.IPAM, IPAMConfig{
			Subnet:  c.Subnet,
			Gateway: c.Gateway,
			IPRange: c.IPRange,
		})
	}
	return n
}
