package authscope

import (
	"sort"
	"strings"
)

const (
	ExtRequiredRights    = "x-sailpoint-required-rights"
	ExtUnmappedRights    = "x-sailpoint-auth-unmapped-rights"
	ExtAuthDiagnostics   = "x-sailpoint-auth-diagnostics"
	schemeOAuth2         = "oauth2"
)

// ApplyOptions configures spec enrichment.
type ApplyOptions struct {
	Enabled bool
	Mapping *Mapping
}

// Stats tracks auth enrichment across a spec.
type Stats struct {
	WithRights       int
	WithScopes       int
	WithUnmapped     int
	UnmappedSample   map[string]bool
}

// ApplyToOperation sets rights extensions and oauth2 security from AMS mapping.
func ApplyToOperation(op map[string]any, rights []string, opts ApplyOptions) {
	if !opts.Enabled || opts.Mapping == nil || len(rights) == 0 {
		return
	}
	rights = dedupeSort(rights)
	scopes, unmapped := opts.Mapping.MinimalScopes(rights)

	if len(rights) > 0 {
		op[ExtRequiredRights] = toAnySlice(rights)
	}
	if len(unmapped) > 0 {
		op[ExtUnmappedRights] = toAnySlice(unmapped)
	}
	if len(scopes) > 0 {
		scopeAny := toAnySlice(scopes)
		op["security"] = []any{
			map[string]any{schemeOAuth2: scopeAny},
		}
	}
}

// EnrichSpec walks all path operations and enriches auth metadata.
// When classifyExisting is true, tokens from existing security are split via mapping.
func EnrichSpec(spec map[string]any, opts ApplyOptions, classifyExisting bool) Stats {
	var st Stats
	if !opts.Enabled || opts.Mapping == nil {
		return st
	}
	st.UnmappedSample = make(map[string]bool)

	paths, _ := spec["paths"].(map[string]any)
	if paths == nil {
		return st
	}
	for _, pathItem := range paths {
		item, ok := pathItem.(map[string]any)
		if !ok {
			continue
		}
		for method, rawOp := range item {
			if !isHTTPMethod(method) {
				continue
			}
			op, ok := rawOp.(map[string]any)
			if !ok {
				continue
			}
			rights := rightsFromExtension(op)
			if len(rights) == 0 && classifyExisting {
				rights = rightsFromSecurity(op, opts.Mapping)
			}
			if len(rights) == 0 {
				continue
			}
			st.WithRights++
			ApplyToOperation(op, rights, opts)
			if sec, ok := op["security"].([]any); ok && len(sec) > 0 {
				st.WithScopes++
			}
			if unmapped, ok := op[ExtUnmappedRights].([]any); ok && len(unmapped) > 0 {
				st.WithUnmapped++
				for _, u := range unmapped {
					if s, ok := u.(string); ok && len(st.UnmappedSample) < 20 {
						st.UnmappedSample[s] = true
					}
				}
			}
		}
	}
	mergeOAuth2ScopesComponent(spec, opts.Mapping, collectUsedScopes(spec))
	return st
}

func rightsFromExtension(op map[string]any) []string {
	raw, ok := op[ExtRequiredRights]
	if !ok {
		return nil
	}
	return anyToStrings(raw)
}

func rightsFromSecurity(op map[string]any, m *Mapping) []string {
	tokens := extractSecurityTokens(op)
	if len(tokens) == 0 {
		return nil
	}
	rights, oauthScopes := m.Classify(tokens)
	if len(oauthScopes) > 0 && len(rights) == 0 {
		// Already PAT scopes; derive rights for extension.
		return m.RightsForScopes(oauthScopes)
	}
	return rights
}

func extractSecurityTokens(op map[string]any) []string {
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
		for scheme, scopesRaw := range m {
			if scheme == schemeOAuth2 || scheme == "userAuth" || scheme == "applicationAuth" {
				out = append(out, anyToStrings(scopesRaw)...)
			}
		}
	}
	return out
}

func collectUsedScopes(spec map[string]any) map[string]bool {
	used := make(map[string]bool)
	paths, _ := spec["paths"].(map[string]any)
	if paths == nil {
		return used
	}
	for _, pathItem := range paths {
		item, ok := pathItem.(map[string]any)
		if !ok {
			continue
		}
		for method, rawOp := range item {
			if !isHTTPMethod(method) {
				continue
			}
			op, ok := rawOp.(map[string]any)
			if !ok {
				continue
			}
			for _, s := range extractSecurityTokens(op) {
				used[s] = true
			}
		}
	}
	return used
}

func mergeOAuth2ScopesComponent(spec map[string]any, m *Mapping, used map[string]bool) {
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
	oauth, _ := schemes[schemeOAuth2].(map[string]any)
	if oauth == nil {
		oauth = map[string]any{
			"type": "oauth2",
			"flows": map[string]any{
				"clientCredentials": map[string]any{
					"tokenUrl": "https://{tenant}.api.example.com/oauth/token",
					"scopes":   map[string]any{},
				},
			},
		}
		schemes[schemeOAuth2] = oauth
	}
	flows, _ := oauth["flows"].(map[string]any)
	if flows == nil {
		return
	}
	cc, _ := flows["clientCredentials"].(map[string]any)
	if cc == nil {
		return
	}
	scopeMap, _ := cc["scopes"].(map[string]any)
	if scopeMap == nil {
		scopeMap = make(map[string]any)
		cc["scopes"] = scopeMap
	}
	for id := range used {
		if _, ok := scopeMap[id]; ok {
			continue
		}
		desc := id
		if m != nil && m.ScopeNames[id] != "" {
			desc = m.ScopeNames[id]
		}
		scopeMap[id] = desc
	}
}

// MergeDiagnostics adds auth stats under info.x-cartographer-diagnostics.auth.
func MergeDiagnostics(spec map[string]any, st Stats) {
	info, _ := spec["info"].(map[string]any)
	if info == nil {
		return
	}
	diag, _ := info["x-cartographer-diagnostics"].(map[string]any)
	if diag == nil {
		diag = make(map[string]any)
		info["x-cartographer-diagnostics"] = diag
	}
	sample := make([]any, 0, len(st.UnmappedSample))
	for r := range st.UnmappedSample {
		sample = append(sample, r)
	}
	sort.Slice(sample, func(i, j int) bool {
		return sample[i].(string) < sample[j].(string)
	})
	diag["auth"] = map[string]any{
		"operationsWithRights":       st.WithRights,
		"operationsWithScopes":       st.WithScopes,
		"operationsWithUnmappedRights": st.WithUnmapped,
		"unmappedRightsSample":       sample,
	}
}

func dedupeSort(in []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func toAnySlice(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
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
	default:
		return nil
	}
}

func isHTTPMethod(k string) bool {
	switch strings.ToUpper(k) {
	case "GET", "PUT", "POST", "DELETE", "OPTIONS", "HEAD", "PATCH", "TRACE":
		return true
	default:
		return false
	}
}
