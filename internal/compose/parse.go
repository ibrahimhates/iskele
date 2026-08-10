// Package compose parses Compose files and turns their services into the
// container definitions the rest of Iskele already knows how to create.
//
// It deliberately does not shell out to `docker compose`: the daemon talks to
// the engine over its socket, and a panel that depends on a CLI being installed
// — at a version that matches — is a panel that breaks on somebody else's
// machine. compose-go is the same parser the CLI uses, so a file that loads
// here loads there.
package compose

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/compose-spec/compose-go/v2/dotenv"
	"github.com/compose-spec/compose-go/v2/graph"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
)

// Parse errors.
var (
	ErrNoServices = errors.New("this compose file defines no services")
	ErrCycle      = errors.New("depends_on forms a cycle")
)

// composeFileName is what a parsed document is called in error messages. The
// content usually never touched a disk — it came from the editor — but an error
// still has to say which file it is about.
const composeFileName = "compose.yaml"

// Input is one stack's source material.
type Input struct {
	// Name is the project name. Compose derives it from the directory; a stack
	// has its own name, and that is what labels the containers.
	Name string
	// Compose is the compose file's content.
	Compose string
	// Env is the content of the stack's `.env`, used for interpolation.
	Env string
	// WorkingDir is what relative paths resolve against.
	WorkingDir string
}

// Error is a compose file that could not be loaded.
//
// It is a distinct type so the API can answer a bad file with 422 and the
// operator's own words, rather than a 500 that says nothing.
type Error struct {
	Message string
	// Cause is the parser's error, kept for the logs.
	Cause error
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Cause }

// Parse loads a compose file into a normalized project.
//
// Interpolation sees only the stack's own `.env` — never the daemon's
// environment. iskeled runs with a secret key path, a database path and
// whatever the unit file sets; letting a compose file read `${...}` out of that
// would turn "deploy this stack" into "print me the daemon's environment".
//
// The returned warnings are the parser's own: an unset variable that defaulted
// to an empty string is the difference between a database with a password and
// one without, and compose-go only says so in a log line.
func Parse(ctx context.Context, in Input) (*types.Project, []Warning, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, nil, &Error{Message: "a stack needs a name"}
	}

	env, err := ParseEnv(in.Env)
	if err != nil {
		return nil, nil, err
	}

	details := types.ConfigDetails{
		WorkingDir: in.WorkingDir,
		ConfigFiles: []types.ConfigFile{{
			Filename: composeFileName,
			Content:  []byte(in.Compose),
		}},
		Environment: env,
	}

	var project *types.Project
	warnings, err := captureWarnings(func() error {
		loaded, loadErr := loader.LoadWithContext(ctx, details, func(opts *loader.Options) {
			opts.SetProjectName(name, true)
			// Absolute paths from here on, so the whitelist check and the engine
			// agree on which host directory a bind mount means.
			opts.ResolvePaths = true
			// A stack is deployed as written. Profiles select a subset, and a
			// silently reduced deployment is worse than an explicit one.
			opts.Profiles = []string{"*"}
		})
		project = loaded
		return loadErr
	})
	if err != nil {
		return nil, warnings, &Error{Message: cleanParseError(err), Cause: err}
	}

	if len(project.Services) == 0 {
		return nil, warnings, &Error{Message: ErrNoServices.Error(), Cause: ErrNoServices}
	}
	if err := graph.CheckCycle(project); err != nil {
		return nil, warnings, &Error{Message: fmt.Sprintf("%s: %s", ErrCycle, err), Cause: err}
	}

	return project, warnings, nil
}

// ParseEnv reads a stack's `.env` content.
//
// Lookups resolve within the file itself and nowhere else: `${HOME}` in a
// stack's env file must not reach the daemon's own environment.
func ParseEnv(content string) (types.Mapping, error) {
	if strings.TrimSpace(content) == "" {
		return types.Mapping{}, nil
	}

	parsed, err := dotenv.UnmarshalWithLookup(content, func(string) (string, bool) {
		return "", false
	})
	if err != nil {
		return nil, &Error{Message: "the .env file could not be read: " + err.Error(), Cause: err}
	}

	return types.Mapping(parsed), nil
}

// ServiceOrder returns the services in the order they must be started:
// a service's dependencies come before it.
//
// Down is this reversed. Compose calls it dependency order and it is the whole
// point of `depends_on`: starting a web server before its database produces a
// container that crash-loops for the ten seconds the database takes.
func ServiceOrder(ctx context.Context, project *types.Project) ([]string, error) {
	var (
		order []string
		mu    sync.Mutex
	)

	err := graph.InDependencyOrder(ctx, project, func(_ context.Context, name string, _ types.ServiceConfig) error {
		// The visitor runs concurrently across independent branches, so the
		// slice needs guarding even though the order it produces is valid.
		mu.Lock()
		defer mu.Unlock()
		order = append(order, name)
		return nil
	})
	if err != nil {
		return nil, &Error{Message: "depends_on could not be resolved: " + err.Error(), Cause: err}
	}

	return order, nil
}

// SortedServiceNames returns every service name, alphabetically.
func SortedServiceNames(project *types.Project) []string {
	names := make([]string, 0, len(project.Services))
	for name := range project.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// cleanParseError trims the parser's noise down to what an operator can act on.
//
// compose-go prefixes messages with the file name it was given, which here is
// a name we invented for a document that came from a text box.
func cleanParseError(err error) string {
	message := err.Error()
	message = strings.ReplaceAll(message, composeFileName+": ", "")
	message = strings.TrimPrefix(message, "validating "+composeFileName+": ")
	return message
}
