// Package extractionopts holds shared extraction behavior flags used by
// language extractors and sharedspec generation.
package extractionopts

import "strings"

// Options configures code-derived extraction behavior.
type Options struct {
	// ErrorSchema controls optional legacy error component injection.
	// Empty means emit error responses only when source declares them.
	// "legacy-error-response" enables the vendor ErrorResponse component when referenced.
	// "problem-details" enables RFC 7807 ProblemDetails when referenced in source.
	ErrorSchema string

	// SignaturePaginationTypes lists simple type names to expand into query
	// parameters when they appear as method signature parameters. Fields are
	// taken from the indexed type declaration, not hardcoded offset/limit names.
	SignaturePaginationTypes []string

	// MergeCoLocatedOpenAPI merges in-repo OpenAPI path fragments when present.
	MergeCoLocatedOpenAPI bool

	// EnableAuthScopeTranslation emits x-sailpoint-required-rights and translates
	// rights to minimal PAT scopes in operation.security.oauth2.
	EnableAuthScopeTranslation bool

	// AMSMappingPath is a JSON file from ams-mapping-gen (consumer-provided).
	// Empty uses the fictional test fixture when running cartographer tests.
	AMSMappingPath string
}

// SignaturePaginationSet returns configured pagination type names as a set.
func (o Options) SignaturePaginationSet() map[string]bool {
	out := make(map[string]bool, len(o.SignaturePaginationTypes))
	for _, t := range o.SignaturePaginationTypes {
		if t = strings.TrimSpace(t); t != "" {
			out[t] = true
		}
	}
	return out
}
