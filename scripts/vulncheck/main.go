// Command vulncheck reads govulncheck's JSON output and decides whether the
// build should fail.
//
// govulncheck has no way to say "we looked at this one". That matters here
// because iskeled links the Docker *client* out of github.com/docker/docker,
// and advisories against that module path cover the *daemon* — the module is
// one repository. An advisory in daemon code we do not compile still fails the
// scan, and it does so with no fixed version to upgrade to, because the fix
// ships under a different module path (github.com/moby/moby/v2).
//
// So: an allowlist, with the assessment written next to each entry, and three
// ways to fail so that the list cannot quietly rot —
//
//  1. a called vulnerability that is not on the list;
//  2. an allowlisted vulnerability that now HAS a fixed version, so the
//     assessment is obsolete and the answer is to upgrade;
//  3. an allowlist entry the scan no longer reports, which is one nobody
//     needs any more.
//
// Only symbol-level findings count. govulncheck also reports vulnerabilities
// in modules the build merely requires; those are not reachable from our code
// and `make vuln` has never failed on them.
//
// It runs govulncheck itself rather than reading a pipe, because the exit
// status is the only reliable evidence that the scan finished: govulncheck
// prints its config message before it fetches the vulnerability database, so a
// scan that dies there still produces plausible-looking output with no
// findings in it. Running it also means not going through `go run`, which
// reports every failure as exit status 1 and would make "found vulnerabilities"
// indistinguishable from "could not reach the database".
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
)

// govulncheckVersion is resolved at run time rather than pinned: the tool is a
// scanner, not a dependency of the daemon, and the vulnerability database it
// reads is fetched fresh on every run anyway.
const govulncheckVersion = "golang.org/x/vuln/cmd/govulncheck@latest"

// foundVulnerabilities is govulncheck's exit status when it has something to
// report. For this tool that is the ordinary case, not a failure.
const foundVulnerabilities = 3

// An allowance is a vulnerability we have read and decided not to act on. The
// reason is the point of the file: a bare ID is indistinguishable from an ID
// somebody pasted in to make the build green.
type allowance struct {
	ID     string
	Reason string
}

// Reviewed by hand. Adding to this list is a security decision — the reason
// belongs in the review, not only here.
var allowlist = []allowance{
	{
		ID: "GO-2026-4887",
		Reason: "CVE-2026-34040, AuthZ plugin bypass on oversized request bodies. " +
			"Daemon-side: it is the engine's authorization-plugin middleware. " +
			"iskeled links the client and runs no authz plugin. Reported against " +
			"every version of github.com/docker/docker with no fix on that module " +
			"path; the fix is in github.com/moby/moby/v2 v2.0.0-beta.8. See D-086.",
	},
	{
		ID: "GO-2026-4883",
		Reason: "CVE-2026-33997, off-by-one in legacy plugin privilege validation. " +
			"Daemon-side, in the legacy plugin installation path. iskeled installs " +
			"no plugins and exposes no plugin API. Same module-path situation as " +
			"GO-2026-4887. See D-086.",
	},
}

func main() {
	// A full scan takes minutes, which makes it exactly the sort of command
	// somebody interrupts. Without this, Ctrl-C would leave govulncheck
	// running and the temporary directory behind.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pkgs := os.Args[1:]

	var input io.Reader = os.Stdin
	if len(pkgs) > 0 {
		out, err := scan(ctx, pkgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "vulncheck: %v\n", err)
			os.Exit(1)
		}
		input = bytes.NewReader(out)
	}

	if err := run(input, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "vulncheck: %v\n", err)
		os.Exit(1)
	}
}

// scan runs govulncheck over the given packages and returns its JSON output.
func scan(ctx context.Context, pkgs []string) ([]byte, error) {
	bin, cleanup, err := install(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	cmd := exec.CommandContext(ctx, bin, append([]string{"-format", "json"}, pkgs...)...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	switch code := exitCode(err); code {
	case 0, foundVulnerabilities:
		return out.Bytes(), nil
	default:
		return nil, fmt.Errorf("govulncheck exited with status %d; the scan did not complete", code)
	}
}

// install builds govulncheck into a temporary directory. `go run` would be
// shorter, but it collapses every exit status onto 1.
func install(ctx context.Context) (bin string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "iskele-vulncheck")
	if err != nil {
		return "", func() {}, fmt.Errorf("temporary directory: %w", err)
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	cmd := exec.CommandContext(ctx, "go", "install", govulncheckVersion)
	cmd.Env = append(os.Environ(), "GOBIN="+dir)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("install govulncheck: %w", err)
	}

	name := "govulncheck"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(dir, name), cleanup, nil
}

// exitCode reports the status a finished command exited with. A command that
// never started has no status of its own, so it gets one that is not a status
// govulncheck uses.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// finding is the subset of govulncheck's finding message we read.
type finding struct {
	OSV          string `json:"osv"`
	FixedVersion string `json:"fixed_version"`
	Trace        []struct {
		Module   string `json:"module"`
		Package  string `json:"package"`
		Function string `json:"function"`
		Receiver string `json:"receiver"`
	} `json:"trace"`
}

// message is one line of the stream. govulncheck emits a sequence of objects,
// each carrying exactly one of these.
type message struct {
	Config  *json.RawMessage `json:"config"`
	OSV     *osvEntry        `json:"osv"`
	Finding *finding         `json:"finding"`
}

type osvEntry struct {
	ID      string   `json:"id"`
	Summary string   `json:"summary"`
	Aliases []string `json:"aliases"`
}

// vuln is what we learned about one advisory across all of its findings.
type vuln struct {
	id           string
	summary      string
	aliases      []string
	fixedVersion string
	module       string
	// callers are the entry points in our own code that reach it, kept for the
	// report; a name nobody can locate is a finding nobody acts on.
	callers []string
}

func run(in io.Reader, out io.Writer) error {
	vulns, sawConfig, err := parse(in)
	if err != nil {
		return err
	}
	// A second guard, for the stdin path: an empty or truncated stream must not
	// read as "nothing found".
	if !sawConfig {
		return errors.New("no govulncheck output; the scan did not run")
	}

	p := &printer{w: out}

	allowed := map[string]string{}
	for _, a := range allowlist {
		allowed[a.ID] = a.Reason
	}

	var unexpected, obsolete, stale []string
	seen := map[string]bool{}

	ids := make([]string, 0, len(vulns))
	for id := range vulns {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		v := vulns[id]
		seen[id] = true

		reason, ok := allowed[id]
		if !ok {
			unexpected = append(unexpected, describe(v))
			continue
		}
		if v.fixedVersion != "" {
			obsolete = append(obsolete, fmt.Sprintf("%s is fixed in %s %s — upgrade and drop the allowance",
				id, v.module, v.fixedVersion))
			continue
		}
		p.printf("allowed  %s  %s\n", id, v.summary)
		p.printf("         %s\n", wrap(reason, 9))
	}

	for _, a := range allowlist {
		if !seen[a.ID] {
			stale = append(stale, a.ID)
		}
	}

	return report(p, unexpected, obsolete, stale, len(vulns))
}

// printer writes the report and keeps the first write error, so the body of
// this tool reads as prose rather than as a chain of error checks. The error
// still matters: a report nobody received is not a report.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) printf(format string, args ...any) {
	if p.err != nil {
		return
	}
	_, p.err = fmt.Fprintf(p.w, format, args...)
}

// parse folds the message stream into one entry per advisory.
func parse(in io.Reader) (map[string]*vuln, bool, error) {
	vulns := map[string]*vuln{}
	details := map[string]*osvEntry{}
	sawConfig := false

	dec := json.NewDecoder(in)
	for {
		var msg message
		if err := dec.Decode(&msg); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, sawConfig, fmt.Errorf("decode govulncheck output: %w", err)
		}

		switch {
		case msg.Config != nil:
			sawConfig = true
		case msg.OSV != nil:
			details[msg.OSV.ID] = msg.OSV
		case msg.Finding != nil:
			collect(vulns, msg.Finding)
		}
	}

	// The advisory text arrives in its own message, ahead of the findings that
	// cite it.
	for id, v := range vulns {
		if d, ok := details[id]; ok {
			v.summary = strings.TrimSpace(d.Summary)
			v.aliases = d.Aliases
		}
	}
	return vulns, sawConfig, nil
}

// collect records a finding if it is symbol-level: govulncheck reports the
// same advisory at module, package and symbol granularity, and only the last
// means our code actually reaches the vulnerable function.
func collect(vulns map[string]*vuln, f *finding) {
	if len(f.Trace) == 0 || f.Trace[0].Function == "" {
		return
	}

	v, ok := vulns[f.OSV]
	if !ok {
		v = &vuln{id: f.OSV, module: f.Trace[0].Module}
		vulns[f.OSV] = v
	}
	if f.FixedVersion != "" {
		v.fixedVersion = f.FixedVersion
	}

	// The trace runs from the vulnerable symbol out to our entry point, which
	// is the frame a maintainer can actually go and look at.
	entry := f.Trace[len(f.Trace)-1]
	name := entry.Package
	if entry.Function != "" {
		name += "." + entry.Function
	}
	if len(v.callers) < 5 && !contains(v.callers, name) {
		v.callers = append(v.callers, name)
	}
}

func describe(v *vuln) string {
	line := v.id
	if len(v.aliases) > 0 {
		line += " (" + strings.Join(v.aliases, ", ") + ")"
	}
	if v.summary != "" {
		line += ": " + v.summary
	}
	if v.fixedVersion != "" {
		line += fmt.Sprintf("\n    fixed in %s %s", v.module, v.fixedVersion)
	}
	for _, c := range v.callers {
		line += "\n    reached from " + c
	}
	return line
}

func report(p *printer, unexpected, obsolete, stale []string, total int) error {
	var problems []string

	if len(unexpected) > 0 {
		p.printf("\n%d vulnerability(ies) reached by our code and not reviewed:\n\n", len(unexpected))
		for _, u := range unexpected {
			p.printf("  %s\n\n", u)
		}
		p.printf("%s", "Upgrade the dependency. If there is no fix and the vulnerable code is\n"+
			"unreachable from iskeled, add it to scripts/vulncheck/main.go with the\n"+
			"assessment written out, and record the decision in DECISIONS.md.\n")
		problems = append(problems, fmt.Sprintf("%d unreviewed", len(unexpected)))
	}

	if len(obsolete) > 0 {
		p.printf("%s", "\nAllowances that are now obsolete:\n\n")
		for _, o := range obsolete {
			p.printf("  %s\n", o)
		}
		problems = append(problems, fmt.Sprintf("%d fixable", len(obsolete)))
	}

	if len(stale) > 0 {
		p.printf("\nAllowances the scan no longer reports: %s\n", strings.Join(stale, ", "))
		p.printf("%s", "Remove them from scripts/vulncheck/main.go — an allowlist nobody prunes\n"+
			"is an allowlist nobody reads.\n")
		problems = append(problems, fmt.Sprintf("%d stale", len(stale)))
	}

	if len(problems) > 0 {
		return errors.New(strings.Join(problems, ", "))
	}

	if p.err != nil {
		return fmt.Errorf("write the report: %w", p.err)
	}

	p.printf("\nok: %d called vulnerability(ies), all reviewed, none fixable.\n", total)
	return p.err
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// wrap reflows a reason to fit an indented block, so a three-line assessment
// stays readable in CI output.
func wrap(s string, indent int) string {
	const width = 70

	var (
		b    strings.Builder
		line int
	)
	pad := strings.Repeat(" ", indent)
	for i, word := range strings.Fields(s) {
		switch {
		case i == 0:
			b.WriteString(word)
			line = len(word)
		case line+1+len(word) > width:
			b.WriteString("\n" + pad + word)
			line = len(word)
		default:
			b.WriteString(" " + word)
			line += 1 + len(word)
		}
	}
	return b.String()
}
