package main

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// stream builds a govulncheck-shaped message stream. The config message is
// what tells the filter the scan actually ran, so it is separate.
func stream(withConfig bool, bodies ...string) string {
	var parts []string
	if withConfig {
		parts = append(parts, `{"config":{"scanner_name":"govulncheck"}}`)
	}
	parts = append(parts, bodies...)
	return strings.Join(parts, "\n")
}

// called is a symbol-level finding: our code reaches the vulnerable function.
func called(id, fixed string) string {
	return `{"finding":{"osv":"` + id + `","fixed_version":"` + fixed + `","trace":[` +
		`{"module":"github.com/docker/docker","package":"client","function":"Vulnerable"},` +
		`{"module":"github.com/ibrahimhates/iskele","package":"internal/docker","function":"Connect"}` +
		`]}}`
}

// required is a module-level finding: the module is in the build, but nothing
// in our code calls the vulnerable symbol.
func required(id string) string {
	return `{"finding":{"osv":"` + id + `","trace":[{"module":"example.com/dep"}]}}`
}

func osv(id, summary string) string {
	return `{"osv":{"id":"` + id + `","summary":"` + summary + `"}}`
}

// The failure that matters most: govulncheck dies before it produces anything
// — no network for the vulnerability database is the usual reason — and the
// pipeline cannot use its exit status because a normal scan with findings
// also exits non-zero. Empty input must not read as "clean".
func TestEmptyInputIsAFailure(t *testing.T) {
	var out strings.Builder

	err := run(strings.NewReader(""), &out)
	if err == nil {
		t.Fatal("empty input passed; a scan that did not run must fail")
	}
	if !strings.Contains(err.Error(), "did not run") {
		t.Errorf("error = %q, want it to say the scan did not run", err)
	}
}

func TestOutputWithoutTheConfigMessageIsAFailure(t *testing.T) {
	var out strings.Builder

	// Findings but no config: a truncated stream.
	err := run(strings.NewReader(stream(false, called(allowlist[0].ID, ""))), &out)
	if err == nil {
		t.Fatal("a stream with no config message passed")
	}
}

func TestAllowlistedVulnerabilitiesPass(t *testing.T) {
	var bodies []string
	for _, a := range allowlist {
		bodies = append(bodies, osv(a.ID, "a daemon-side problem"), called(a.ID, ""))
	}

	var out strings.Builder
	if err := run(strings.NewReader(stream(true, bodies...)), &out); err != nil {
		t.Fatalf("the reviewed allowlist did not pass: %v", err)
	}
	if !strings.Contains(out.String(), "all reviewed") {
		t.Errorf("output = %q, want the ok line", out.String())
	}
	// The assessment has to reach the reader, or the allowlist is just a list
	// of IDs somebody has to go and look up.
	for _, a := range allowlist {
		if !strings.Contains(out.String(), a.ID) {
			t.Errorf("output does not mention %s", a.ID)
		}
	}
}

func TestAnUnreviewedVulnerabilityFails(t *testing.T) {
	var out strings.Builder

	in := stream(true, osv("GO-2099-9999", "something new"), called("GO-2099-9999", ""))
	err := run(strings.NewReader(in), &out)
	if err == nil {
		t.Fatal("an unreviewed vulnerability passed")
	}
	if !strings.Contains(out.String(), "GO-2099-9999") {
		t.Errorf("output does not name the vulnerability: %q", out.String())
	}
	if !strings.Contains(out.String(), "internal/docker.Connect") {
		t.Errorf("output does not say where it is reached from: %q", out.String())
	}
}

// An allowance says "there is nothing to upgrade to". The moment that stops
// being true the allowance is wrong, and sitting on a published fix is worse
// than the original finding.
func TestAnAllowanceWithAFixAvailableFails(t *testing.T) {
	var out strings.Builder

	id := allowlist[0].ID
	in := stream(true, osv(id, "now fixed"), called(id, "v28.6.0"))
	for _, a := range allowlist[1:] {
		in += "\n" + osv(a.ID, "still unfixed") + "\n" + called(a.ID, "")
	}

	err := run(strings.NewReader(in), &out)
	if err == nil {
		t.Fatal("an allowance with a published fix passed")
	}
	if !strings.Contains(out.String(), "v28.6.0") {
		t.Errorf("output does not name the fixed version: %q", out.String())
	}
}

func TestAnAllowanceTheScanNoLongerReportsFails(t *testing.T) {
	if len(allowlist) < 2 {
		t.Skip("needs at least two allowances to drop one")
	}

	var bodies []string
	for _, a := range allowlist[1:] {
		bodies = append(bodies, osv(a.ID, "still there"), called(a.ID, ""))
	}

	var out strings.Builder
	err := run(strings.NewReader(stream(true, bodies...)), &out)
	if err == nil {
		t.Fatal("a stale allowance passed")
	}
	if !strings.Contains(out.String(), allowlist[0].ID) {
		t.Errorf("output does not name the stale allowance: %q", out.String())
	}
}

// govulncheck reports vulnerabilities in modules the build requires but does
// not call. Those have never failed `make vuln` and must not start now — the
// filter would be rejecting things govulncheck itself passes.
func TestAVulnerabilityWeDoNotCallIsIgnored(t *testing.T) {
	var bodies []string
	for _, a := range allowlist {
		bodies = append(bodies, osv(a.ID, "reviewed"), called(a.ID, ""))
	}
	bodies = append(bodies, osv("GO-2099-1111", "in a module we require"), required("GO-2099-1111"))

	var out strings.Builder
	if err := run(strings.NewReader(stream(true, bodies...)), &out); err != nil {
		t.Fatalf("an uncalled vulnerability failed the scan: %v", err)
	}
}

// govulncheck answers 3 when it has findings, which is the ordinary case here;
// anything else means the scan did not finish and must not be read as clean.
func TestExitCodeTellsFindingsFromFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"clean run", nil, 0},
		{"never started", errors.New("exec: not found"), -1},
	}
	for _, c := range cases {
		if got := exitCode(c.err); got != c.want {
			t.Errorf("%s: exitCode = %d, want %d", c.name, got, c.want)
		}
	}

	// A real non-zero status, produced rather than mocked: exec.ExitError is
	// not something a test can construct meaningfully by hand.
	err := exec.Command(shell(), "-c", "exit 3").Run()
	if got := exitCode(err); got != foundVulnerabilities {
		t.Errorf("exitCode of a status-3 command = %d, want %d", got, foundVulnerabilities)
	}
}

func shell() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "/bin/sh"
}

// The reason is the whole reason the file exists.
func TestEveryAllowanceExplainsItself(t *testing.T) {
	for _, a := range allowlist {
		if !strings.HasPrefix(a.ID, "GO-") {
			t.Errorf("%q is not a Go vulnerability database ID", a.ID)
		}
		if len(strings.Fields(a.Reason)) < 15 {
			t.Errorf("%s: the reason is too short to be an assessment: %q", a.ID, a.Reason)
		}
	}
}
