// Package authscope shapes extracted auth requirements into the standard
// OpenAPI security block. Cartographer surfaces whatever strings it found
// in source AS OAuth2 scopes; downstream consumers decide whether any of
// those strings are actually rights or permissions that need translation.
//
// There is no consumer-supplied mapping JSON, no right→scope translation,
// and no vendor-flavoured x-* extensions emitted by this package.
package authscope

import (
	"sort"
	"strings"
)

// SchemeOAuth2 is the security-scheme name Cartographer uses when emitting
// auth requirements. Downstream tooling can rename or augment it freely.
const SchemeOAuth2 = "oauth2"

// DefaultTokenURL is the fictional token URL Cartographer puts on the
// generated oauth2 security scheme. Downstream tooling typically replaces
// this with a real value during a post-extract overlay step.
const DefaultTokenURL = "https://{tenant}.api.example.com/oauth/token"

// Stats summarises what auth enrichment did for a single spec.
type Stats struct {
	OperationsWithSecurity int
	UniqueScopes           int
}

// ApplyToOperation sets the operation's security block to a single oauth2
// requirement listing tokens as scopes. Tokens are normalised (Spring
// ROLE_ prefix stripped, empty / scheme-name junk filtered, deduplicated,
// sorted) before emission. If no usable tokens remain the operation's
// security block is left untouched.
func ApplyToOperation(op map[string]any, tokens []string) {
	cleaned := Normalize(tokens)
	if len(cleaned) == 0 {
		return
	}
	op["security"] = []any{
		map[string]any{SchemeOAuth2: toAnySlice(cleaned)},
	}
}

// EnrichSpec walks every operation in spec, collects the union of every
// oauth2 scope that appears on any operation's security block, and ensures
// the document declares a matching `components.securitySchemes.oauth2`
// entry whose scope catalogue includes every observed scope.
//
// The function is idempotent: re-running it on the same spec produces the
// same components.securitySchemes content.
func EnrichSpec(spec map[string]any) Stats {
	var st Stats
	if spec == nil {
		return st
	}
	used := collectUsedScopes(spec, &st)
	mergeOAuth2SecurityScheme(spec, used)
	st.UniqueScopes = len(used)
	return st
}

// Normalize cleans the raw auth tokens emitted by language extractors. It
// strips the Spring ROLE_ prefix, filters out scheme placeholder names
// like "oauth2"/"bearer", trims whitespace, deduplicates, and sorts.
func Normalize(tokens []string) []string {
	tokens = NormalizeSpringRights(tokens)
	seen := make(map[string]struct{}, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		switch strings.ToLower(t) {
		case "oauth2", "bearerauth", "bearer", "apikey":
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func collectUsedScopes(spec map[string]any, st *Stats) map[string]struct{} {
	used := make(map[string]struct{})
	paths, _ := spec["paths"].(map[string]any)
	if paths == nil {
		return used
	}
	for _, pathItem := range paths {
		item, ok := pathItem.(map[string]any)
		if !ok {
			continue
		}
		for method, raw := range item {
			if !isHTTPMethod(method) {
				continue
			}
			op, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			before := len(used)
			for _, scope := range extractOAuth2Scopes(op) {
				used[scope] = struct{}{}
			}
			if len(used) > before {
				st.OperationsWithSecurity++
			} else if hasOAuth2Requirement(op) {
				st.OperationsWithSecurity++
			}
		}
	}
	return used
}

func extractOAuth2Scopes(op map[string]any) []string {
	sec, ok := op["security"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, req := range sec {
		m, ok := req.(map[string]any)
		if !ok {
			continue
		}
		for scheme, scopes := range m {
			if scheme != SchemeOAuth2 {
				continue
			}
			out = append(out, anyToStrings(scopes)...)
		}
	}
	return out
}

func hasOAuth2Requirement(op map[string]any) bool {
	sec, ok := op["security"].([]any)
	if !ok {
		return false
	}
	for _, req := range sec {
		m, ok := req.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := m[SchemeOAuth2]; ok {
			return true
		}
	}
	return false
}

func mergeOAuth2SecurityScheme(spec map[string]any, used map[string]struct{}) {
	if len(used) == 0 {
		return
	}
	components, _ := spec["components"].(map[string]any)
	if components == nil {
		components = make(map[string]any)
		spec["components"] = components
	}
	schemes, _ := components["securitySchemes"].(map[string]any)
	if schemes == nil {
		schemes = make(map[string]any)
		components["securitySchemes"] = schemes
	}
	oauth, _ := schemes[SchemeOAuth2].(map[string]any)
	if oauth == nil {
		oauth = map[string]any{
			"type": "oauth2",
			"flows": map[string]any{
				"clientCredentials": map[string]any{
					"tokenUrl": DefaultTokenURL,
					"scopes":   map[string]any{},
				},
			},
		}
		schemes[SchemeOAuth2] = oauth
	}
	flows, _ := oauth["flows"].(map[string]any)
	if flows == nil {
		flows = map[string]any{}
		oauth["flows"] = flows
	}
	cc, _ := flows["clientCredentials"].(map[string]any)
	if cc == nil {
		cc = map[string]any{
			"tokenUrl": DefaultTokenURL,
			"scopes":   map[string]any{},
		}
		flows["clientCredentials"] = cc
	}
	scopeMap, _ := cc["scopes"].(map[string]any)
	if scopeMap == nil {
		scopeMap = map[string]any{}
		cc["scopes"] = scopeMap
	}
	for scope := range used {
		if _, ok := scopeMap[scope]; ok {
			continue
		}
		scopeMap[scope] = scope
	}
}

func toAnySlice(in []string) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func anyToStrings(v any) []string {
	switch t := v.(type) {
	case []string:
		return append([]string(nil), t...)
	case []any:
		var out []string
		for _, x := range t {
			if s, ok := x.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func isHTTPMethod(k string) bool {
	switch strings.ToUpper(k) {
	case "GET", "PUT", "POST", "DELETE", "OPTIONS", "HEAD", "PATCH", "TRACE":
		return true
	}
	return false
}
