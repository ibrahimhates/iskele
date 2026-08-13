package templates

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// mustTemplate parses a template body and fails the test if it will not load.
func mustTemplate(t *testing.T, body string) *Template {
	t.Helper()

	template, err := parse([]byte(body), SourceCustom)
	if err != nil {
		t.Fatalf("parse() error = %v", err)
	}
	return template
}

// refuses parses a template body and expects the schema to reject it.
func refuses(t *testing.T, body, wants string) {
	t.Helper()

	_, err := parse([]byte(body), SourceCustom)
	if err == nil {
		t.Fatalf("parse() error = nil, want a refusal mentioning %q", wants)
	}
	if !strings.Contains(err.Error(), wants) {
		t.Errorf("error = %q, want it to mention %q", err, wants)
	}
}

func TestSchemaRefusesTheThingsThatWouldRenderWrong(t *testing.T) {
	t.Run("a placeholder with no field", func(t *testing.T) {
		// This would render as an empty string and produce a container that is
		// subtly wrong rather than obviously broken.
		refuses(t, `{"id":"x","title":"X","category":"tools","image":"app:{{version}}"}`,
			"never declared")
	})

	t.Run("a shipped default password", func(t *testing.T) {
		refuses(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
		  "fields":[{"name":"pw","label":"Password","type":"password","default":"admin"}]}`,
			"must not carry a default")
	})

	t.Run("a select with no options", func(t *testing.T) {
		refuses(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
		  "fields":[{"name":"mode","label":"Mode","type":"select"}]}`,
			"needs options")
	})

	t.Run("a default that is not one of the options", func(t *testing.T) {
		refuses(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
		  "fields":[{"name":"mode","label":"Mode","type":"select","default":"c",
		    "options":[{"value":"a","label":"A"},{"value":"b","label":"B"}]}]}`,
			"not one of the options")
	})

	t.Run("a pattern that does not compile", func(t *testing.T) {
		refuses(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
		  "fields":[{"name":"n","label":"N","type":"text","pattern":"["}]}`,
			"does not compile")
	})

	t.Run("an unknown field type", func(t *testing.T) {
		refuses(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
		  "fields":[{"name":"n","label":"N","type":"color"}]}`,
			"is not a field type")
	})

	t.Run("a duplicate field", func(t *testing.T) {
		refuses(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
		  "fields":[{"name":"n","label":"N","type":"text"},{"name":"n","label":"N2","type":"text"}]}`,
			"declared twice")
	})

	t.Run("a tmpfs mount", func(t *testing.T) {
		// It would silently discard the data the operator thinks they keep.
		refuses(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
		  "mounts":[{"type":"tmpfs","source":"","destination":"/data"}]}`,
			"not a mount type")
	})

	t.Run("no image", func(t *testing.T) {
		refuses(t, `{"id":"x","title":"X","category":"tools"}`, "image")
	})

	t.Run("an id that would not survive a URL", func(t *testing.T) {
		refuses(t, `{"id":"My App!","title":"X","category":"tools","image":"app:1"}`, "id")
	})

	t.Run("a numeric default that is not a number", func(t *testing.T) {
		refuses(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
		  "fields":[{"name":"n","label":"N","type":"number","default":"lots"}]}`,
			"not a number")
	})
}

func TestRenderReportsEveryBadAnswerAtOnce(t *testing.T) {
	template := mustTemplate(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
	  "fields":[
	    {"name":"port","label":"Port","type":"port"},
	    {"name":"count","label":"Count","type":"number","min":1,"max":10},
	    {"name":"needed","label":"Needed","type":"text","required":true}
	  ]}`)

	_, err := template.Render("app", map[string]string{"port": "99999", "count": "50"})

	var problems *ValueErrors
	if !errors.As(err, &problems) {
		t.Fatalf("error = %v (%T), want *ValueErrors", err, err)
	}
	// An operator filling in a form should not have to submit it three times.
	if len(problems.Errors) != 3 {
		t.Errorf("errors = %+v, want one per bad answer", problems.Errors)
	}
}

func TestRenderRefusesAnAnswerToAFieldThatDoesNotExist(t *testing.T) {
	template := mustTemplate(t, `{"id":"x","title":"X","category":"tools","image":"app:1"}`)

	_, err := template.Render("app", map[string]string{"ghost": "value"})
	if err == nil || !strings.Contains(err.Error(), "not a field") {
		t.Errorf("error = %v, want a stale answer to be refused", err)
	}
}

func TestRenderFillsDefaultsAndSubstitutes(t *testing.T) {
	template := mustTemplate(t, `{"id":"x","title":"X","category":"tools",
	  "image":"app:{{version}}",
	  "env":{"MODE":"{{mode}}","FIXED":"yes"},
	  "ports":[{"host":"{{port}}","container":80}],
	  "fields":[
	    {"name":"version","label":"Version","type":"text","default":"1.2"},
	    {"name":"mode","label":"Mode","type":"select","default":"fast",
	      "options":[{"value":"fast","label":"Fast"},{"value":"slow","label":"Slow"}]},
	    {"name":"port","label":"Port","type":"port","default":"8080"}
	  ]}`)

	spec, err := template.Render("app", nil)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	if spec.Image != "app:1.2" {
		t.Errorf("image = %q, want app:1.2", spec.Image)
	}
	if len(spec.Ports) != 1 || spec.Ports[0].HostPort != "8080" {
		t.Errorf("ports = %+v, want the default published", spec.Ports)
	}

	byKey := map[string]string{}
	for _, env := range spec.Env {
		byKey[env.Key] = env.Value
	}
	if byKey["MODE"] != "fast" || byKey["FIXED"] != "yes" {
		t.Errorf("env = %v, want the default substituted", byKey)
	}
}

// A template offering a port without insisting on it should publish nothing
// when the answer is left blank, not publish an empty one.
func TestRenderSkipsAnUnansweredOptionalPort(t *testing.T) {
	template := mustTemplate(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
	  "ports":[{"host":"{{main}}","container":80},{"host":"{{extra}}","container":8080}],
	  "fields":[
	    {"name":"main","label":"Main","type":"port","default":"8080"},
	    {"name":"extra","label":"Extra","type":"port"}
	  ]}`)

	spec, err := template.Render("app", nil)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if len(spec.Ports) != 1 || spec.Ports[0].ContainerPort != 80 {
		t.Errorf("ports = %+v, want only the answered one", spec.Ports)
	}
}

func TestRenderNamesAnUnnamedVolumeAfterTheContainer(t *testing.T) {
	template := mustTemplate(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
	  "mounts":[{"type":"volume","source":"{{vol}}","destination":"/data"}],
	  "fields":[{"name":"vol","label":"Volume","type":"volume"}]}`)

	spec, err := template.Render("my-app", nil)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if len(spec.Mounts) != 1 || spec.Mounts[0].Source != "my-app-data-1" {
		t.Errorf("mounts = %+v, want a volume named after the container", spec.Mounts)
	}
}

// An optional bind the operator left blank is not a mount at all. That is how
// a template offers "an alternative config file, if you have one".
func TestRenderSkipsAnUnansweredOptionalBind(t *testing.T) {
	template := mustTemplate(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
	  "mounts":[
	    {"type":"volume","source":"{{vol}}","destination":"/data"},
	    {"type":"bind","source":"{{config}}","destination":"/etc/app.conf"}
	  ],
	  "fields":[
	    {"name":"vol","label":"Volume","type":"volume"},
	    {"name":"config","label":"Config file","type":"path"}
	  ]}`)

	spec, err := template.Render("app", nil)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if len(spec.Mounts) != 1 || spec.Mounts[0].Destination != "/data" {
		t.Errorf("mounts = %+v, want only the volume", spec.Mounts)
	}
}

// A template that hard-codes an empty bind source is a broken template, not an
// optional mount.
func TestRenderRefusesABindWithNoSourceAtAll(t *testing.T) {
	template := mustTemplate(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
	  "mounts":[{"type":"bind","source":"","destination":"/data"}]}`)

	if _, err := template.Render("app", nil); err == nil {
		t.Error("Render() error = nil, want a bind with no source to be refused")
	}
}

func TestRenderRefusesARelativeHostPath(t *testing.T) {
	template := mustTemplate(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
	  "mounts":[{"type":"bind","source":"{{dir}}","destination":"/data"}],
	  "fields":[{"name":"dir","label":"Directory","type":"path","required":true}]}`)

	_, err := template.Render("app", map[string]string{"dir": "relative/path"})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Errorf("error = %v, want a relative path refused", err)
	}
}

func TestRenderRefusesAnUnusableContainerName(t *testing.T) {
	template := mustTemplate(t, `{"id":"x","title":"X","category":"tools","image":"app:1"}`)

	if _, err := template.Render("has spaces", nil); err == nil {
		t.Error("Render() error = nil, want the engine's name rules applied")
	}
}

// The privileged options a template declares survive rendering, so the gate in
// front of the create service can refuse them. A catalog entry is not a way
// around that gate.
func TestRenderCarriesPrivilegedOptionsThrough(t *testing.T) {
	template := mustTemplate(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
	  "cap_add":["NET_ADMIN"],"privileged":true,
	  "sysctls":{"net.ipv4.ip_forward":"1"},
	  "network_mode":"host"}`)

	spec, err := template.Render("app", nil)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if !spec.Security.Privileged || len(spec.Security.CapAdd) != 1 {
		t.Errorf("security = %+v, want the declared options carried through", spec.Security)
	}
	if spec.Network.Name != "host" {
		t.Errorf("network = %q, want host", spec.Network.Name)
	}
	if !template.NeedsPrivileged() {
		t.Error("NeedsPrivileged() = false, want the catalog to warn in advance")
	}
}

func TestRenderNotesSubstitutesTheSameAnswers(t *testing.T) {
	template := mustTemplate(t, `{"id":"x","title":"X","category":"tools","image":"app:1",
	  "notes":"Open http://localhost:{{port}}",
	  "fields":[{"name":"port","label":"Port","type":"port","default":"8080"}]}`)

	if got := template.RenderNotes(nil); got != "Open http://localhost:8080" {
		t.Errorf("notes = %q, want the port substituted", got)
	}
}

// Every shipped template has to round-trip through JSON, since that is how
// the API hands it to the browser.
func TestTemplatesSurviveJSON(t *testing.T) {
	for _, template := range load(t).List() {
		encoded, err := json.Marshal(template)
		if err != nil {
			t.Fatalf("%s: marshal: %v", template.ID, err)
		}

		var decoded Template
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			t.Fatalf("%s: unmarshal: %v", template.ID, err)
		}
		if decoded.ID != template.ID || len(decoded.Fields) != len(template.Fields) {
			t.Errorf("%s did not survive the round trip", template.ID)
		}
	}
}
