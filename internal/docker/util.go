package docker

import (
	"sort"
	"strconv"
)

func sortStrings(s []string) { sort.Strings(s) }

func sortAttachments(a []NetworkAttachment) {
	sort.Slice(a, func(i, j int) bool { return a[i].Name < a[j].Name })
}

// parsePort converts an engine port string to a number, returning 0 for the
// empty or malformed values the engine occasionally reports.
func parsePort(s string) uint16 {
	if s == "" {
		return 0
	}
	n, err := strconv.ParseUint(s, 10, 16)
	if err != nil {
		return 0
	}
	return uint16(n)
}

// nonNilMap returns m, or an empty map when m is nil, so JSON responses carry
// {} instead of null.
func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
