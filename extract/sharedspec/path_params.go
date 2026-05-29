package sharedspec

import (
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/specmodel"
)

// EnsurePathParameters adds missing path template parameters declared in the
// URL but absent from handler signatures (nested sub-resources).
func EnsurePathParameters(path string, params []*specmodel.Parameter) []*specmodel.Parameter {
	seen := make(map[string]bool)
	for _, p := range params {
		if p != nil && p.In == "path" && p.Name != "" {
			seen[p.Name] = true
		}
	}
	for _, name := range extractPathTemplateParams(path) {
		if seen[name] {
			continue
		}
		params = append(params, &specmodel.Parameter{
			Name:     name,
			In:       "path",
			Type:     "string",
			Required: true,
		})
		seen[name] = true
	}
	return params
}

func extractPathTemplateParams(path string) []string {
	parts := strings.Split(path, "/")
	out := make([]string, 0)
	for _, part := range parts {
		if len(part) >= 3 && strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
			if name != "" {
				out = append(out, name)
			}
		}
	}
	return out
}
