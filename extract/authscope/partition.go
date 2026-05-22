package authscope

import (
	"regexp"
	"sort"
	"strings"
)

// amsToken matches Barrelman sailpoint-oauth-scope-format (three colon-separated segments).
var amsToken = regexp.MustCompile(`^[a-z][a-z0-9-]*:[a-z0-9][a-z0-9-]*:[a-z0-9-]+$`)

// PartitionTokens splits security-related strings into AMS rights vs PAT scope ids.
// When mapping is nil, three-segment tokens are treated as rights and shorter tokens as scopes.
func PartitionTokens(tokens []string, m *Mapping) (rights, oauthScopes []string) {
	if m != nil {
		return m.Classify(tokens)
	}
	seenR := make(map[string]bool)
	seenS := make(map[string]bool)
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" || t == "oauth2" || t == "bearerAuth" || t == "bearer" {
			continue
		}
		if amsToken.MatchString(t) {
			if !seenR[t] {
				seenR[t] = true
				rights = append(rights, t)
			}
		} else {
			if !seenS[t] {
				seenS[t] = true
				oauthScopes = append(oauthScopes, t)
			}
		}
	}
	sort.Strings(rights)
	sort.Strings(oauthScopes)
	return rights, oauthScopes
}
