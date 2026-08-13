package store

import "testing"

// Docker Hub answers to several names; one credential has to cover all of
// them, or a pull of "nginx" fails while a pull of "docker.io/nginx" works.
func TestNormalizeRegistryServer(t *testing.T) {
	cases := map[string]string{
		"docker.io":                  "docker.io",
		"index.docker.io":            "docker.io",
		"registry-1.docker.io":       "docker.io",
		"registry.hub.docker.com":    "docker.io",
		"https://index.docker.io/":   "docker.io",
		"":                           "docker.io",
		"ghcr.io":                    "ghcr.io",
		"GHCR.IO":                    "ghcr.io",
		"https://ghcr.io":            "ghcr.io",
		"http://registry.local:5000": "registry.local:5000",
		"registry.example.com/":      "registry.example.com",
	}

	for input, want := range cases {
		if got := NormalizeRegistryServer(input); got != want {
			t.Errorf("NormalizeRegistryServer(%q) = %q, want %q", input, got, want)
		}
	}
}

// The rule the Docker CLI uses: the part before the first slash is a registry
// only when it looks like a host. Getting this wrong sends Hub credentials to
// a third party, or fails to send private ones at all.
func TestRegistryServerForImage(t *testing.T) {
	cases := map[string]string{
		"nginx":                         "docker.io",
		"nginx:1.27":                    "docker.io",
		"library/nginx":                 "docker.io",
		"myorg/myapp:v1":                "docker.io",
		"ghcr.io/org/app":               "ghcr.io",
		"ghcr.io/org/app:v2":            "ghcr.io",
		"registry.example.com/team/app": "registry.example.com",
		"registry.local:5000/app":       "registry.local:5000",
		"localhost/app":                 "localhost",
		"localhost:5000/app":            "localhost:5000",
		"  ghcr.io/org/app  ":           "ghcr.io",
	}

	for input, want := range cases {
		if got := RegistryServerForImage(input); got != want {
			t.Errorf("RegistryServerForImage(%q) = %q, want %q", input, got, want)
		}
	}
}
