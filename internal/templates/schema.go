// Package templates is Iskele's app catalog: JSON descriptions of common
// applications that render into the same container definition the create
// wizard produces.
//
// A template is a form, not a script. It declares fields, and rendering fills
// a [docker.ContainerSpec] from the answers — which means every template goes
// through the same path whitelist and the same privileged-option gate as
// anything else. A catalog entry cannot do what an operator could not do by
// hand.
package templates

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// FieldType is what a template asks the operator for.
type FieldType string

// Field types.
const (
	// FieldText is a single line of text.
	FieldText FieldType = "text"
	// FieldNumber is an integer, optionally bounded.
	FieldNumber FieldType = "number"
	// FieldPassword is a secret. The UI offers to generate one and never
	// pre-fills it from a default.
	FieldPassword FieldType = "password"
	// FieldSelect is one of a fixed set of options.
	FieldSelect FieldType = "select"
	// FieldBool is a checkbox.
	FieldBool FieldType = "bool"
	// FieldPort is a host port. Separate from number so the UI can warn about
	// publishing on every interface.
	FieldPort FieldType = "port"
	// FieldPath is a host path. It is checked against allowed_paths at deploy
	// time like any other bind source.
	FieldPath FieldType = "path"
	// FieldVolume is a named volume. Empty means "let Iskele name it".
	FieldVolume FieldType = "volume"
)

// Valid reports whether a type is one the engine knows.
func (t FieldType) Valid() bool {
	switch t {
	case FieldText, FieldNumber, FieldPassword, FieldSelect, FieldBool,
		FieldPort, FieldPath, FieldVolume:
		return true
	default:
		return false
	}
}

// Option is one choice in a select field.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Field is one question a template asks.
type Field struct {
	// Name is the placeholder this field fills, written `{{name}}` in the
	// template's strings.
	Name  string    `json:"name"`
	Label string    `json:"label"`
	Type  FieldType `json:"type"`
	// Help is shown under the input. It is where a template explains what a
	// value is for, which is most of what makes a catalog usable.
	Help string `json:"help,omitempty"`
	// Default pre-fills the input. Ignored for passwords: a shipped default
	// password is a password everybody has.
	Default string `json:"default,omitempty"`
	// Required refuses an empty answer.
	Required bool `json:"required,omitempty"`
	// Pattern is a regular expression the answer must match.
	Pattern string `json:"pattern,omitempty"`
	// Min and Max bound a number or a port.
	Min *int `json:"min,omitempty"`
	Max *int `json:"max,omitempty"`
	// Options are the choices for a select.
	Options []Option `json:"options,omitempty"`
	// Generate asks the UI to offer a random value, for passwords and keys.
	Generate bool `json:"generate,omitempty"`
	// GenerateLength is how long a generated value should be.
	GenerateLength int `json:"generate_length,omitempty"`
}

// PortSpec is a published port in a template.
type PortSpec struct {
	// Host may be a `{{field}}` placeholder, which is the point: the port is
	// the first thing an operator changes.
	Host      string `json:"host"`
	Container int    `json:"container"`
	Protocol  string `json:"protocol,omitempty"`
}

// MountSpec is one mount in a template.
type MountSpec struct {
	// Type is "volume" or "bind". tmpfs has no place in a catalog entry: it
	// would silently discard the data the operator thinks they are keeping.
	Type string `json:"type"`
	// Source is a volume name or a host path, and may be a placeholder.
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"read_only,omitempty"`
}

// HealthSpec is a template's health probe.
type HealthSpec struct {
	Test        []string `json:"test"`
	Interval    string   `json:"interval,omitempty"`
	Timeout     string   `json:"timeout,omitempty"`
	StartPeriod string   `json:"start_period,omitempty"`
	Retries     int      `json:"retries,omitempty"`
}

// Template is one catalog entry.
type Template struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Title string `json:"title"`
	// Category groups the catalog: databases, networking, tools.
	Category string `json:"category"`
	// Description is a sentence about what this is, in the operator's terms.
	Description string `json:"description"`
	// Icon is a lucide icon name, so the catalog needs no image assets and
	// works offline.
	Icon string `json:"icon,omitempty"`
	// Website and Documentation point at the project itself.
	Website       string   `json:"website,omitempty"`
	Documentation string   `json:"documentation,omitempty"`
	Keywords      []string `json:"keywords,omitempty"`

	// Image is the container image, and may carry a `{{version}}` placeholder.
	Image string `json:"image"`
	// Command and Entrypoint override the image's.
	Command    []string `json:"command,omitempty"`
	Entrypoint []string `json:"entrypoint,omitempty"`
	// Env is the environment, with placeholders.
	Env map[string]string `json:"env,omitempty"`
	// Ports, Mounts and the rest mirror what the create wizard produces.
	Ports       []PortSpec        `json:"ports,omitempty"`
	Mounts      []MountSpec       `json:"mounts,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Restart     string            `json:"restart,omitempty"`
	HealthCheck *HealthSpec       `json:"health_check,omitempty"`
	// CapAdd, Devices and Privileged exist because a few real applications
	// need them. They are still refused unless the caller holds the privileged
	// permission — a catalog entry is not a way around that gate.
	CapAdd     []string          `json:"cap_add,omitempty"`
	Devices    []string          `json:"devices,omitempty"`
	Privileged bool              `json:"privileged,omitempty"`
	Sysctls    map[string]string `json:"sysctls,omitempty"`
	// Network mode, when the application needs one ("host" for a VPN).
	NetworkMode string `json:"network_mode,omitempty"`

	// Fields are the questions, in the order they are asked.
	Fields []Field `json:"fields,omitempty"`

	// Notes are shown after a successful deploy: the first login, the port to
	// open, the thing that is not obvious.
	Notes string `json:"notes,omitempty"`

	// Source says where this template came from. It is set by the loader, not
	// by the file, so an operator can tell a shipped entry from their own.
	Source string `json:"source,omitempty"`
}

// Template sources.
const (
	SourceBuiltin = "builtin"
	SourceCustom  = "custom"
)

// SchemaError is a template that is not usable.
//
// It names the field so the operator writing a custom template is told what to
// fix rather than that "the template is invalid".
type SchemaError struct {
	Template string
	Field    string
	Message  string
}

func (e *SchemaError) Error() string {
	parts := make([]string, 0, 3)
	if e.Template != "" {
		parts = append(parts, e.Template)
	}
	if e.Field != "" {
		parts = append(parts, e.Field)
	}
	parts = append(parts, e.Message)
	return strings.Join(parts, ": ")
}

// idPattern is what a template id may contain. It ends up in a URL and in a
// container name, so it is deliberately narrow.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// namePattern is what a field name may contain. It is substituted as
// `{{name}}`, so anything that would confuse the substitution is refused.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,63}$`)

// Validate reports whether a template can be rendered.
//
// It is run on every template at load, including the built-in ones: a shipped
// entry that fails its own schema is a bug worth failing the daemon's tests
// over, not something to discover when an operator clicks deploy.
func (t *Template) Validate() error {
	if !idPattern.MatchString(t.ID) {
		return &SchemaError{Template: t.ID, Field: "id",
			Message: "must be lowercase letters, digits, dash or underscore"}
	}
	if strings.TrimSpace(t.Title) == "" {
		return &SchemaError{Template: t.ID, Field: "title", Message: "is required"}
	}
	if strings.TrimSpace(t.Category) == "" {
		return &SchemaError{Template: t.ID, Field: "category", Message: "is required"}
	}
	if strings.TrimSpace(t.Image) == "" {
		return &SchemaError{Template: t.ID, Field: "image", Message: "is required"}
	}

	declared := make(map[string]bool, len(t.Fields))
	for i := range t.Fields {
		if err := t.Fields[i].validate(t.ID); err != nil {
			return err
		}
		if declared[t.Fields[i].Name] {
			return &SchemaError{Template: t.ID, Field: t.Fields[i].Name,
				Message: "is declared twice"}
		}
		declared[t.Fields[i].Name] = true
	}

	if err := t.checkPlaceholders(declared); err != nil {
		return err
	}

	for _, port := range t.Ports {
		if port.Container <= 0 || port.Container > 65535 {
			return &SchemaError{Template: t.ID, Field: "ports",
				Message: fmt.Sprintf("container port %d is out of range", port.Container)}
		}
	}
	for _, mount := range t.Mounts {
		switch mount.Type {
		case "volume", "bind":
		default:
			return &SchemaError{Template: t.ID, Field: "mounts",
				Message: fmt.Sprintf("%q is not a mount type a template may use", mount.Type)}
		}
		if strings.TrimSpace(mount.Destination) == "" {
			return &SchemaError{Template: t.ID, Field: "mounts",
				Message: "every mount needs a destination"}
		}
	}

	return nil
}

// validate checks one field.
func (f *Field) validate(templateID string) error {
	fail := func(format string, args ...any) error {
		return &SchemaError{Template: templateID, Field: f.Name,
			Message: fmt.Sprintf(format, args...)}
	}

	if !namePattern.MatchString(f.Name) {
		return &SchemaError{Template: templateID, Field: f.Name,
			Message: "a field name must be lowercase letters, digits or underscore"}
	}
	if strings.TrimSpace(f.Label) == "" {
		return fail("needs a label")
	}
	if !f.Type.Valid() {
		return fail("%q is not a field type", f.Type)
	}

	if f.Pattern != "" {
		if _, err := regexp.Compile(f.Pattern); err != nil {
			return fail("pattern does not compile: %s", err)
		}
	}

	switch f.Type {
	case FieldSelect:
		if len(f.Options) == 0 {
			return fail("a select needs options")
		}
		if f.Default != "" && !hasOption(f.Options, f.Default) {
			return fail("the default %q is not one of the options", f.Default)
		}
	case FieldNumber, FieldPort:
		if f.Default != "" {
			if _, err := strconv.Atoi(f.Default); err != nil {
				return fail("the default %q is not a number", f.Default)
			}
		}
		if f.Min != nil && f.Max != nil && *f.Min > *f.Max {
			return fail("min is greater than max")
		}
	case FieldPassword:
		// A shipped default password is a password everybody has.
		if f.Default != "" {
			return fail("a password field must not carry a default")
		}
	case FieldBool:
		if f.Default != "" && f.Default != "true" && f.Default != "false" {
			return fail("the default must be true or false")
		}
	}

	return nil
}

// placeholderPattern finds `{{name}}` in a template's strings.
var placeholderPattern = regexp.MustCompile(`\{\{\s*([a-z0-9_]+)\s*\}\}`)

// checkPlaceholders refuses a template that substitutes a field it never asks
// for — which would otherwise render as an empty string and produce a
// container that is subtly wrong.
func (t *Template) checkPlaceholders(declared map[string]bool) error {
	for _, text := range t.strings() {
		for _, match := range placeholderPattern.FindAllStringSubmatch(text, -1) {
			if !declared[match[1]] {
				return &SchemaError{Template: t.ID, Field: match[1],
					Message: fmt.Sprintf("is substituted in %q but never declared", text)}
			}
		}
	}
	return nil
}

// strings lists every value a placeholder may appear in.
func (t *Template) strings() []string {
	out := []string{t.Image, t.NetworkMode}
	out = append(out, t.Command...)
	out = append(out, t.Entrypoint...)

	out = append(out, sortedValues(t.Env)...)
	out = append(out, sortedValues(t.Labels)...)
	out = append(out, sortedValues(t.Sysctls)...)
	out = append(out, t.Devices...)

	for _, port := range t.Ports {
		out = append(out, port.Host)
	}
	for _, mount := range t.Mounts {
		out = append(out, mount.Source, mount.Destination)
	}
	if t.HealthCheck != nil {
		out = append(out, t.HealthCheck.Test...)
	}
	out = append(out, t.Notes)

	return out
}

// sortedValues returns a map's values in a stable order, so an error message
// about a placeholder does not change between runs.
func sortedValues(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, m[key])
	}
	return out
}

// hasOption reports whether value is one of the options.
func hasOption(options []Option, value string) bool {
	for _, option := range options {
		if option.Value == value {
			return true
		}
	}
	return false
}

// NeedsPrivileged reports whether deploying this template requires the
// privileged permission, so the catalog can say so before the operator fills
// in a form they will not be allowed to submit.
func (t *Template) NeedsPrivileged() bool {
	return t.Privileged ||
		len(t.CapAdd) > 0 ||
		len(t.Devices) > 0 ||
		len(t.Sysctls) > 0 ||
		t.NetworkMode == "host"
}
