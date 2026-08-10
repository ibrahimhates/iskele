package compose

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Git errors.
var (
	ErrGitMissing = errors.New("git is not installed on this host")
	ErrGitURL     = errors.New("this is not a git URL Iskele will clone")
)

// gitTimeout bounds one git operation. A clone over a slow link is
// legitimately slow; one that has not finished in this long is stuck, most
// likely on a credential prompt that will never be answered.
const gitTimeout = 5 * time.Minute

// GitSource describes a repository to check out.
type GitSource struct {
	URL string
	// Ref is a branch, tag or commit. Empty takes the default branch.
	Ref string
	// Dir is where the working copy lives.
	Dir string
}

// GitResult reports what a checkout produced.
type GitResult struct {
	// Commit is the revision the working copy is at.
	Commit string
	// Ref is the branch or tag that was checked out.
	Ref string
	// Updated reports whether the commit changed, so a caller can say
	// "already up to date" rather than claiming to have deployed something new.
	Updated bool
}

// Checkout clones a repository, or updates an existing working copy.
//
// It runs the `git` binary rather than embedding a Git implementation: cloning
// is not the daemon's core job, the binary is on every machine that deploys
// from a repository, and an embedded implementation is several megabytes of
// dependency to maintain for one feature. When it is missing, the error says so
// plainly instead of failing somewhere further in.
func Checkout(ctx context.Context, source GitSource) (GitResult, error) {
	if err := CheckGitURL(source.URL); err != nil {
		return GitResult{}, err
	}
	if _, err := exec.LookPath("git"); err != nil {
		return GitResult{}, ErrGitMissing
	}
	if strings.TrimSpace(source.Dir) == "" {
		return GitResult{}, errors.New("a git stack needs a working directory")
	}

	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()

	before, _ := gitOutput(ctx, source.Dir, "rev-parse", "HEAD")

	if isRepository(source.Dir) {
		if err := updateRepository(ctx, source); err != nil {
			return GitResult{}, err
		}
	} else {
		if err := cloneRepository(ctx, source); err != nil {
			return GitResult{}, err
		}
	}

	commit, err := gitOutput(ctx, source.Dir, "rev-parse", "HEAD")
	if err != nil {
		return GitResult{}, fmt.Errorf("read the checked-out commit: %w", err)
	}

	ref := source.Ref
	if ref == "" {
		if branch, branchErr := gitOutput(ctx, source.Dir, "rev-parse", "--abbrev-ref", "HEAD"); branchErr == nil {
			ref = branch
		}
	}

	return GitResult{Commit: commit, Ref: ref, Updated: commit != before}, nil
}

// cloneRepository makes a fresh working copy.
func cloneRepository(ctx context.Context, source GitSource) error {
	if err := os.MkdirAll(filepath.Dir(source.Dir), 0o750); err != nil {
		return fmt.Errorf("create the stack directory: %w", err)
	}
	// A half-finished clone from a previous attempt would make git refuse.
	if err := os.RemoveAll(source.Dir); err != nil {
		return fmt.Errorf("clear the stack directory: %w", err)
	}

	// --depth 1 is deliberate: a stack needs the files at one revision, not
	// the project's history, and a shallow clone of a large repository is the
	// difference between seconds and minutes.
	args := []string{"clone", "--depth", "1"}
	if source.Ref != "" {
		args = append(args, "--branch", source.Ref)
	}
	args = append(args, "--", source.URL, source.Dir)

	if _, err := gitOutput(ctx, "", args...); err != nil {
		return fmt.Errorf("clone %s: %w", source.URL, err)
	}
	return nil
}

// updateRepository fetches and resets an existing working copy.
//
// Reset rather than merge: the working copy is a deployment artifact, not
// somewhere anyone edits, and a merge conflict in it would leave a stack that
// cannot deploy and cannot explain why.
func updateRepository(ctx context.Context, source GitSource) error {
	if _, err := gitOutput(ctx, source.Dir, "remote", "set-url", "origin", "--", source.URL); err != nil {
		return fmt.Errorf("point the working copy at %s: %w", source.URL, err)
	}

	ref := source.Ref
	if ref == "" {
		ref = "HEAD"
	}
	if _, err := gitOutput(ctx, source.Dir, "fetch", "--depth", "1", "origin", "--", ref); err != nil {
		return fmt.Errorf("fetch %s: %w", ref, err)
	}
	if _, err := gitOutput(ctx, source.Dir, "reset", "--hard", "FETCH_HEAD"); err != nil {
		return fmt.Errorf("check out %s: %w", ref, err)
	}
	return nil
}

// isRepository reports whether dir already holds a working copy.
func isRepository(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

// gitOutput runs one git command and returns its trimmed stdout.
func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // args are built here, never from user text alone
	cmd.Dir = dir
	// No prompting, ever: a private repository without credentials must fail
	// rather than hang the deploy on a terminal nobody is watching.
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
		"GCM_INTERACTIVE=never",
	)

	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		if text == "" {
			return "", err
		}
		return "", errors.New(text)
	}
	return text, nil
}

// CheckGitURL refuses the URLs that are not a repository to fetch.
//
// Two of these matter beyond tidiness. `ext::` tells git to run an arbitrary
// command as its transport, so a stack URL would be a shell. And a URL
// beginning with a dash is read by git as an option rather than an address,
// which is the classic way to smuggle one in.
func CheckGitURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fmt.Errorf("%w: a git stack needs a repository URL", ErrGitURL)
	}
	if strings.HasPrefix(trimmed, "-") {
		return fmt.Errorf("%w: a URL cannot start with a dash", ErrGitURL)
	}

	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"ext::", "file://", "--upload-pack"} {
		if strings.HasPrefix(lower, prefix) {
			return fmt.Errorf("%w: %s is not an allowed transport", ErrGitURL, prefix)
		}
	}

	// scp-style (git@host:org/repo.git) has no scheme and is the form most
	// people paste for SSH.
	if !strings.Contains(trimmed, "://") {
		if strings.Contains(trimmed, ":") && !strings.HasPrefix(trimmed, "/") {
			return nil
		}
		return fmt.Errorf("%w: %q is a local path, not a repository URL", ErrGitURL, raw)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrGitURL, err)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https", "ssh", "git":
		return nil
	default:
		return fmt.Errorf("%w: %s is not an allowed transport", ErrGitURL, parsed.Scheme)
	}
}

// ComposeFileIn finds the compose file inside a checked-out repository.
//
// The names are compose's own search order, so a repository laid out for the
// CLI works here without being told where its file is.
func ComposeFileIn(dir string) (string, error) {
	candidates := []string{
		"compose.yaml", "compose.yml",
		"docker-compose.yaml", "docker-compose.yml",
	}

	for _, name := range candidates {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("no compose file in the repository (looked for %s)",
		strings.Join(candidates, ", "))
}
