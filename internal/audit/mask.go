// Package audit records who did what, and masks secrets before they are
// written anywhere.
package audit

import (
	"regexp"
	"strings"
)

// Masked replaces a secret value in audit records and logs.
const Masked = "***"

// secretKeyPattern matches names whose values must never be recorded.
//
// It errs towards masking: a masked value that turned out to be harmless
// costs an operator one lookup, while a leaked one cannot be taken back.
var secretKeyPattern = regexp.MustCompile(`(?i)(pass|passwd|password|token|secret|api_?key|_key$|^key$|credential|auth|private|salt|signature|session)`)

// IsSecretKey reports whether a key's value should be masked.
func IsSecretKey(key string) bool {
	return secretKeyPattern.MatchString(key)
}

// MaskValue masks a value unconditionally, keeping empty values empty so a
// record can still show that a field was unset.
func MaskValue(value string) string {
	if value == "" {
		return ""
	}
	return Masked
}

// MaskMap returns a copy of m with secret-looking values replaced.
func MaskMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		if IsSecretKey(k) {
			out[k] = MaskValue(v)
			continue
		}
		out[k] = v
	}
	return out
}

// MaskEnv masks the values of KEY=VALUE entries whose key looks secret.
//
// Container environments are where credentials most often sit, and they are
// the payload of nearly every container-create audit record.
func MaskEnv(env []string) []string {
	if env == nil {
		return nil
	}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			// No "=" means no value to leak.
			out = append(out, entry)
			continue
		}
		if IsSecretKey(key) {
			out = append(out, key+"="+MaskValue(value))
			continue
		}
		out = append(out, entry)
	}
	return out
}

// MaskAny walks a decoded JSON-ish value and masks secret-looking fields at
// any depth, so a nested payload cannot smuggle a credential into the record.
func MaskAny(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			if IsSecretKey(k) {
				out[k] = maskLeaf(item)
				continue
			}
			out[k] = MaskAny(item)
		}
		return out

	case map[string]string:
		return MaskMap(v)

	case []string:
		return MaskEnv(v)

	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = MaskAny(item)
		}
		return out

	default:
		return value
	}
}

// maskLeaf masks a value that sits under a secret-looking key, preserving its
// shape so the record still says whether it was set.
func maskLeaf(value any) any {
	switch v := value.(type) {
	case string:
		return MaskValue(v)
	case nil:
		return nil
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = Masked
		}
		return out
	default:
		return Masked
	}
}
