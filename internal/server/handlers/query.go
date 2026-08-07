package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ibrahimhates/iskele/internal/httpx"
)

// boolParam reads a boolean query parameter. A bare flag ("?all") counts as
// true, matching how the Docker CLI and most HTTP APIs behave.
func boolParam(r *http.Request, name string) (bool, error) {
	q := r.URL.Query()
	if !q.Has(name) {
		return false, nil
	}
	raw := q.Get(name)
	if raw == "" {
		return true, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, httpx.ErrBadRequest("query parameter %s must be a boolean, got %q", name, raw)
	}
	return v, nil
}

// intParam reads an optional integer query parameter, returning nil when it is
// absent so callers can tell "not set" from "set to zero".
func intParam(r *http.Request, name string) (*int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return nil, httpx.ErrBadRequest("query parameter %s must be an integer, got %q", name, raw)
	}
	return &v, nil
}

// listParam reads a repeatable query parameter, also accepting a single
// comma-separated value so ?label=a,b works alongside ?label=a&label=b.
func listParam(r *http.Request, name string) []string {
	var out []string
	for _, raw := range r.URL.Query()[name] {
		for _, part := range strings.Split(raw, ",") {
			if part = strings.TrimSpace(part); part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

// triStateParam reads a boolean that may be absent, for filters where "unset"
// means "do not filter" rather than "false".
func triStateParam(r *http.Request, name string) (*bool, error) {
	if !r.URL.Query().Has(name) {
		return nil, nil
	}
	v, err := boolParam(r, name)
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// list is the envelope every collection endpoint returns. Wrapping the array
// leaves room for pagination metadata without breaking clients.
type list[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

// newList wraps items, guaranteeing a JSON array rather than null when empty.
func newList[T any](items []T) list[T] {
	if items == nil {
		items = []T{}
	}
	return list[T]{Items: items, Total: len(items)}
}
