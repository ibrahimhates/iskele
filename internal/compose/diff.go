package compose

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/types"
)

// ChangeKind says what happened to one service between two versions of a
// compose file.
type ChangeKind string

// Change kinds.
const (
	ChangeAdded    ChangeKind = "added"
	ChangeRemoved  ChangeKind = "removed"
	ChangeModified ChangeKind = "modified"
)

// ServiceChange is one service's difference between two files.
type ServiceChange struct {
	Service string     `json:"service"`
	Kind    ChangeKind `json:"kind"`
	// Fields names what changed, in compose's own vocabulary, so an operator
	// reading "image, ports" knows what a deploy would do without reading a
	// line-by-line diff.
	Fields []string `json:"fields,omitempty"`
	// Recreates reports whether applying this would replace the container
	// rather than leave it running.
	Recreates bool `json:"recreates"`
}

// Diff is what applying an edit would do.
type Diff struct {
	Services []ServiceChange `json:"services"`
	// Networks and Volumes name resources that would appear or disappear.
	Networks []ResourceChange `json:"networks"`
	Volumes  []ResourceChange `json:"volumes"`
	// Warnings from parsing the new version, so the editor shows them next to
	// the changes rather than only after a deploy.
	Warnings []Warning `json:"warnings"`
}

// ResourceChange is a network or volume that would be created or removed.
type ResourceChange struct {
	Name string     `json:"name"`
	Kind ChangeKind `json:"kind"`
}

// Empty reports a diff that would change nothing.
func (d Diff) Empty() bool {
	return len(d.Services) == 0 && len(d.Networks) == 0 && len(d.Volumes) == 0
}

// Compare reports what deploying `next` would do to a stack currently
// described by `current`.
//
// It exists so an operator can see the consequences of an edit before saving
// it: "this restarts your database" is worth knowing in advance, and reading it
// off a YAML diff is not something a person should have to do.
func Compare(ctx context.Context, current, next Input) (Diff, error) {
	diff := Diff{
		Services: []ServiceChange{},
		Networks: []ResourceChange{},
		Volumes:  []ResourceChange{},
		Warnings: []Warning{},
	}

	nextProject, warnings, err := Parse(ctx, next)
	diff.Warnings = append(diff.Warnings, warnings...)
	if err != nil {
		return diff, err
	}

	// The current version failing to parse is not an error: a stack can be
	// edited out of a broken state, and that edit is exactly when a diff is
	// most useful. Everything then reads as new.
	currentProject, _, currentErr := Parse(ctx, current)
	if currentErr != nil {
		currentProject = &types.Project{Services: types.Services{}}
	}

	diff.Services = compareServices(currentProject, nextProject)
	diff.Networks = compareNames(networkKeys(currentProject), networkKeys(nextProject))
	diff.Volumes = compareNames(volumeKeys(currentProject), volumeKeys(nextProject))

	return diff, nil
}

// compareServices reports the per-service differences.
func compareServices(current, next *types.Project) []ServiceChange {
	changes := []ServiceChange{}

	for _, name := range SortedServiceNames(next) {
		before, existed := current.Services[name]
		after := next.Services[name]

		if !existed {
			changes = append(changes, ServiceChange{
				Service: name, Kind: ChangeAdded, Recreates: true,
			})
			continue
		}

		if fields := changedFields(before, after); len(fields) > 0 {
			changes = append(changes, ServiceChange{
				Service:   name,
				Kind:      ChangeModified,
				Fields:    fields,
				Recreates: true,
			})
		}
	}

	for _, name := range SortedServiceNames(current) {
		if _, stillThere := next.Services[name]; !stillThere {
			changes = append(changes, ServiceChange{
				Service: name, Kind: ChangeRemoved, Recreates: true,
			})
		}
	}

	sort.SliceStable(changes, func(i, j int) bool { return changes[i].Service < changes[j].Service })
	return changes
}

// changedFields names the compose fields that differ between two versions of
// one service.
//
// Only the fields that change what the container is: comparing the whole
// struct would report a difference for an internal pointer and tell the
// operator nothing.
func changedFields(before, after types.ServiceConfig) []string {
	var fields []string

	compare := func(name string, a, b any) {
		if fmt.Sprintf("%v", a) != fmt.Sprintf("%v", b) {
			fields = append(fields, name)
		}
	}

	compare("image", before.Image, after.Image)
	compare("command", before.Command, after.Command)
	compare("entrypoint", before.Entrypoint, after.Entrypoint)
	compare("environment", sortedEnv(before.Environment), sortedEnv(after.Environment))
	compare("ports", portStrings(before.Ports), portStrings(after.Ports))
	compare("volumes", volumeStrings(before.Volumes), volumeStrings(after.Volumes))
	compare("networks", sortedKeys(before.Networks), sortedKeys(after.Networks))
	compare("labels", before.Labels, after.Labels)
	compare("restart", before.Restart, after.Restart)
	compare("depends_on", dependencyNames(before), dependencyNames(after))
	compare("healthcheck", before.HealthCheck, after.HealthCheck)
	compare("deploy", before.Deploy, after.Deploy)
	compare("user", before.User, after.User)
	compare("working_dir", before.WorkingDir, after.WorkingDir)
	compare("privileged", before.Privileged, after.Privileged)
	compare("cap_add", before.CapAdd, after.CapAdd)
	compare("devices", before.Devices, after.Devices)
	compare("build", before.Build, after.Build)

	return fields
}

// sortedEnv renders an environment in a stable order.
func sortedEnv(env types.MappingWithEquals) string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := ""
		if env[key] != nil {
			value = *env[key]
		}
		parts = append(parts, key+"="+value)
	}
	return strings.Join(parts, "\x00")
}

// portStrings renders published ports in a stable order.
func portStrings(ports []types.ServicePortConfig) string {
	parts := make([]string, 0, len(ports))
	for _, port := range ports {
		parts = append(parts, fmt.Sprintf("%s:%s:%d/%s",
			port.HostIP, port.Published, port.Target, port.Protocol))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// volumeStrings renders mounts in a stable order.
func volumeStrings(volumes []types.ServiceVolumeConfig) string {
	parts := make([]string, 0, len(volumes))
	for _, volume := range volumes {
		parts = append(parts, fmt.Sprintf("%s:%s:%s:%t",
			volume.Type, volume.Source, volume.Target, volume.ReadOnly))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// dependencyNames renders depends_on in a stable order.
func dependencyNames(service types.ServiceConfig) string {
	names := make([]string, 0, len(service.DependsOn))
	for name, config := range service.DependsOn {
		names = append(names, name+":"+config.Condition)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// sortedKeys renders a map's keys in a stable order.
func sortedKeys[V any](m map[string]V) string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

// networkKeys lists a project's declared networks.
func networkKeys(project *types.Project) []string {
	keys := make([]string, 0, len(project.Networks))
	for key := range project.Networks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// volumeKeys lists a project's declared volumes.
func volumeKeys(project *types.Project) []string {
	keys := make([]string, 0, len(project.Volumes))
	for key := range project.Volumes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// compareNames reports which names appeared and which went.
func compareNames(before, after []string) []ResourceChange {
	present := make(map[string]bool, len(before))
	for _, name := range before {
		present[name] = true
	}
	wanted := make(map[string]bool, len(after))
	for _, name := range after {
		wanted[name] = true
	}

	changes := []ResourceChange{}
	for _, name := range after {
		if !present[name] {
			changes = append(changes, ResourceChange{Name: name, Kind: ChangeAdded})
		}
	}
	for _, name := range before {
		if !wanted[name] {
			changes = append(changes, ResourceChange{Name: name, Kind: ChangeRemoved})
		}
	}
	return changes
}
