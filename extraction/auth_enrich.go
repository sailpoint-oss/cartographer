package extraction

import (
	"github.com/sailpoint-oss/cartographer/extract/authscope"
)

// ToAnySpecMap converts a string-interface{} spec map for authscope helpers.
func ToAnySpecMap(spec map[string]interface{}) map[string]any {
	if spec == nil {
		return nil
	}
	out := make(map[string]any, len(spec))
	for k, v := range spec {
		out[k] = v
	}
	return out
}

// ApplyAuthScopeTranslation walks every operation in the generated spec
// and ensures `components.securitySchemes.oauth2` declares every scope
// observed on any operation's security block.
//
// Cartographer does NOT translate extracted tokens into any other shape
// (no right→scope mapping, no consumer JSON load). Downstream tooling
// owns that translation — see meridian/overlay for the reference flow.
func ApplyAuthScopeTranslation(specMap map[string]any) error {
	if specMap == nil {
		return nil
	}
	_ = authscope.EnrichSpec(specMap)
	return nil
}
