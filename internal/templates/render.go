package templates

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ibrahimhates/iskele/internal/docker"
)

// ValueError is one answer a template will not accept.
type ValueError struct {
	Field   string
	Message string
}

func (e *ValueError) Error() string { return e.Field + ": " + e.Message }

// ValueErrors is every problem with a set of answers.
//
// All of them at once, not the first: an operator filling in a nine-field form
// should not have to submit it nine times.
type ValueErrors struct {
	Errors []ValueError
}

func (e *ValueErrors) Error() string {
	parts := make([]string, 0, len(e.Errors))
	for _, item := range e.Errors {
		parts = append(parts, item.Error())
	}
	return strings.Join(parts, "; ")
}

// containerNamePattern is what the engine accepts as a container name.
var containerNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// Render turns a template and a set of answers into a container definition.
//
// The result goes through the create service like anything else, so the path
// whitelist and the privileged gate apply. Nothing here bypasses them; this
// only decides what to ask for.
func (t *Template) Render(name string, values map[string]string) (docker.ContainerSpec, error) {
	resolved, err := t.resolve(values)
	if err != nil {
		return docker.ContainerSpec{}, err
	}

	containerName := strings.TrimSpace(name)
	if containerName == "" {
		containerName = t.ID
	}
	if !containerNamePattern.MatchString(containerName) {
		return docker.ContainerSpec{}, &ValueErrors{Errors: []ValueError{{
			Field:   "name",
			Message: "a container name may hold letters, digits, dot, dash and underscore",
		}}}
	}

	spec := docker.ContainerSpec{
		Name:       containerName,
		Image:      substitute(t.Image, resolved),
		Command:    substituteAll(t.Command, resolved),
		Entrypoint: substituteAll(t.Entrypoint, resolved),
		Start:      true,
		Labels:     t.renderLabels(resolved),
		Env:        t.renderEnv(resolved),
		RestartPolicy: docker.RestartPolicy{
			Name: defaultString(t.Restart, "unless-stopped"),
		},
		Security: docker.SecuritySpec{
			Privileged:     t.Privileged,
			CapAdd:         t.CapAdd,
			Devices:        substituteAll(t.Devices, resolved),
			Sysctls:        renderMap(t.Sysctls, resolved),
			ReadOnlyRootFS: false,
		},
		Network: docker.NetworkSpec{Name: substitute(t.NetworkMode, resolved)},
	}

	ports, err := t.renderPorts(resolved)
	if err != nil {
		return docker.ContainerSpec{}, err
	}
	spec.Ports = ports

	mounts, err := t.renderMounts(containerName, resolved)
	if err != nil {
		return docker.ContainerSpec{}, err
	}
	spec.Mounts = mounts

	if t.HealthCheck != nil {
		spec.HealthCheck = &docker.HealthSpec{
			Test:        substituteAll(t.HealthCheck.Test, resolved),
			Interval:    t.HealthCheck.Interval,
			Timeout:     t.HealthCheck.Timeout,
			StartPeriod: t.HealthCheck.StartPeriod,
			Retries:     t.HealthCheck.Retries,
		}
	}

	return spec, nil
}

// RenderNotes fills the template's after-deploy notes with the same answers,
// so "log in with the password you just set" can name the port it is on.
func (t *Template) RenderNotes(values map[string]string) string {
	resolved, err := t.resolve(values)
	if err != nil {
		// Notes are a courtesy; a value that failed validation has already
		// stopped the deploy, and this is never reached with bad input.
		return t.Notes
	}
	return substitute(t.Notes, resolved)
}

// resolve validates the answers and fills in defaults.
func (t *Template) resolve(values map[string]string) (map[string]string, error) {
	resolved := make(map[string]string, len(t.Fields))
	problems := make([]ValueError, 0)

	for i := range t.Fields {
		field := &t.Fields[i]

		raw, provided := values[field.Name]
		raw = strings.TrimSpace(raw)
		if !provided || raw == "" {
			raw = field.Default
		}

		if raw == "" {
			if field.Required {
				problems = append(problems, ValueError{
					Field: field.Name, Message: "is required",
				})
				continue
			}
			resolved[field.Name] = ""
			continue
		}

		if err := field.check(raw); err != nil {
			problems = append(problems, *err)
			continue
		}
		resolved[field.Name] = raw
	}

	// An answer to a field the template never asked for is a client bug or a
	// stale form; either way it would silently do nothing.
	for name := range values {
		if !t.declares(name) {
			problems = append(problems, ValueError{
				Field: name, Message: "is not a field of this template",
			})
		}
	}

	if len(problems) > 0 {
		sort.Slice(problems, func(i, j int) bool { return problems[i].Field < problems[j].Field })
		return nil, &ValueErrors{Errors: problems}
	}
	return resolved, nil
}

// declares reports whether the template has a field by this name.
func (t *Template) declares(name string) bool {
	for i := range t.Fields {
		if t.Fields[i].Name == name {
			return true
		}
	}
	return false
}

// check validates one answer.
func (f *Field) check(value string) *ValueError {
	fail := func(format string, args ...any) *ValueError {
		return &ValueError{Field: f.Name, Message: fmt.Sprintf(format, args...)}
	}

	if f.Pattern != "" {
		// The pattern compiled at load; a template that got this far has a
		// valid one.
		if matched, _ := regexp.MatchString(f.Pattern, value); !matched {
			return fail("does not match the expected format")
		}
	}

	switch f.Type {
	case FieldSelect:
		if !hasOption(f.Options, value) {
			return fail("%q is not one of the choices", value)
		}

	case FieldBool:
		if value != "true" && value != "false" {
			return fail("must be true or false")
		}

	case FieldNumber, FieldPort:
		number, err := strconv.Atoi(value)
		if err != nil {
			return fail("must be a whole number")
		}
		if f.Type == FieldPort && (number < 1 || number > 65535) {
			return fail("must be a port between 1 and 65535")
		}
		if f.Min != nil && number < *f.Min {
			return fail("must be at least %d", *f.Min)
		}
		if f.Max != nil && number > *f.Max {
			return fail("must be at most %d", *f.Max)
		}

	case FieldPath:
		if !strings.HasPrefix(value, "/") {
			return fail("must be an absolute path")
		}

	case FieldVolume:
		if !containerNamePattern.MatchString(value) {
			return fail("a volume name may hold letters, digits, dot, dash and underscore")
		}
	}

	return nil
}

// renderEnv fills the environment.
func (t *Template) renderEnv(values map[string]string) []docker.EnvVar {
	if len(t.Env) == 0 {
		return nil
	}

	keys := make([]string, 0, len(t.Env))
	for key := range t.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]docker.EnvVar, 0, len(keys))
	for _, key := range keys {
		out = append(out, docker.EnvVar{Key: key, Value: substitute(t.Env[key], values)})
	}
	return out
}

// renderLabels marks the container as this template's, so the catalog can say
// what is already deployed.
func (t *Template) renderLabels(values map[string]string) map[string]string {
	labels := make(map[string]string, len(t.Labels)+2)
	for key, value := range t.Labels {
		labels[key] = substitute(value, values)
	}

	labels[LabelTemplate] = t.ID
	labels[LabelManaged] = "true"
	return labels
}

// Catalog labels.
const (
	// LabelTemplate records which catalog entry produced a container.
	LabelTemplate = "com.iskele.template"
	// LabelManaged marks it as Iskele's, matching what stacks write.
	LabelManaged = "com.iskele.managed"
)

// renderPorts fills the published ports.
func (t *Template) renderPorts(values map[string]string) ([]docker.PortMapping, error) {
	if len(t.Ports) == 0 {
		return nil, nil
	}

	out := make([]docker.PortMapping, 0, len(t.Ports))
	for _, port := range t.Ports {
		host := substitute(port.Host, values)
		if host == "" {
			// An unanswered optional port means "do not publish this one",
			// which is how a template offers a port without insisting on it.
			continue
		}
		if _, err := strconv.Atoi(host); err != nil {
			return nil, &ValueErrors{Errors: []ValueError{{
				Field: "ports", Message: fmt.Sprintf("%q is not a port number", host),
			}}}
		}

		out = append(out, docker.PortMapping{
			HostPort:      host,
			ContainerPort: port.Container,
			Protocol:      defaultString(port.Protocol, "tcp"),
		})
	}
	return out, nil
}

// renderMounts fills the mounts.
//
// A volume whose name resolves to nothing is named after the container, so a
// template can offer "where should this live" without making it mandatory.
func (t *Template) renderMounts(name string, values map[string]string) ([]docker.MountSpec, error) {
	if len(t.Mounts) == 0 {
		return nil, nil
	}

	out := make([]docker.MountSpec, 0, len(t.Mounts))
	for i, mount := range t.Mounts {
		source := substitute(mount.Source, values)
		destination := substitute(mount.Destination, values)

		if destination == "" {
			return nil, &ValueErrors{Errors: []ValueError{{
				Field: "mounts", Message: "a mount destination resolved to nothing",
			}}}
		}

		switch mount.Type {
		case "volume":
			if source == "" {
				source = fmt.Sprintf("%s-data-%d", name, i+1)
			}
		case "bind":
			// A bind whose source is a placeholder the operator left blank is
			// an optional mount, and skipping it is the point: that is how a
			// template offers "an alternative config file, if you have one"
			// without insisting on one.
			if source == "" {
				if strings.Contains(mount.Source, "{{") {
					continue
				}
				return nil, &ValueErrors{Errors: []ValueError{{
					Field:   "mounts",
					Message: fmt.Sprintf("the host path for %s is required", destination),
				}}}
			}
			if !strings.HasPrefix(source, "/") {
				return nil, &ValueErrors{Errors: []ValueError{{
					Field:   "mounts",
					Message: fmt.Sprintf("the host path for %s must be absolute", destination),
				}}}
			}
		}

		out = append(out, docker.MountSpec{
			Type:        mount.Type,
			Source:      source,
			Destination: destination,
			ReadOnly:    mount.ReadOnly,
		})
	}
	return out, nil
}

// renderMap substitutes into a map's values.
func renderMap(source map[string]string, values map[string]string) map[string]string {
	if len(source) == 0 {
		return nil
	}

	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = substitute(value, values)
	}
	return out
}

// substitute replaces every `{{field}}` with its answer.
func substitute(text string, values map[string]string) string {
	if text == "" || !strings.Contains(text, "{{") {
		return text
	}

	return placeholderPattern.ReplaceAllStringFunc(text, func(match string) string {
		name := placeholderPattern.FindStringSubmatch(match)[1]
		return values[name]
	})
}

// substituteAll substitutes into every element, dropping the ones that resolve
// to nothing — an optional argument left unanswered should not become an empty
// string in the middle of a command line.
func substituteAll(items []string, values map[string]string) []string {
	if len(items) == 0 {
		return nil
	}

	out := make([]string, 0, len(items))
	for _, item := range items {
		rendered := substitute(item, values)
		if strings.Contains(item, "{{") && rendered == "" {
			continue
		}
		out = append(out, rendered)
	}
	return out
}

// defaultString returns value, or fallback when it is empty.
func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
