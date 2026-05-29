package authscope

import "strings"

// NormalizeSpringRights strips erroneous ROLE_ prefixes from rights
// produced by Spring @PreAuthorize hasRole('api:domain:action').
func NormalizeSpringRights(tokens []string) []string {
	if len(tokens) == 0 {
		return tokens
	}
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "ROLE_") {
			inner := strings.TrimPrefix(t, "ROLE_")
			if colonDelimitedAuthID.MatchString(inner) {
				t = inner
			}
		}
		out = append(out, t)
	}
	return out
}
