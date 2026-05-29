package authscope

import (
	"regexp"
	"strings"
)

// colonDelimitedAuthID matches three-segment scope-style ids of the form
// `api:resource:action`.
var colonDelimitedAuthID = regexp.MustCompile(`^[a-z][a-z0-9-]*:[a-z0-9][a-z0-9-]*:[a-z0-9-]+$`)

// IsColonDelimitedAuthID reports whether s looks like a structured
// three-segment colon-delimited security identifier (e.g.
// `api:resource:action`). Cartographer treats every extracted token the
// same way regardless of its shape, but per-language adapters use this to
// recognise structured ids in framework metadata.
func IsColonDelimitedAuthID(s string) bool {
	return colonDelimitedAuthID.MatchString(strings.TrimSpace(s))
}

// SplitTokens splits a single security-annotation string into one or more
// atomic tokens. Many frameworks accept comma- or whitespace-delimited
// scopes inside one annotation argument; this helper normalises those into
// individual entries before they enter the security block.
func SplitTokens(s string) []string {
	if s == "" {
		return nil
	}
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
