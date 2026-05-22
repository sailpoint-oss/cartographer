package extraction

import (
	"github.com/sailpoint-oss/cartographer/extract/authscope"
	"github.com/sailpoint-oss/cartographer/extract/extractionopts"
)

// AuthApplyOptions builds authscope options from extraction settings.
func AuthApplyOptions(opts extractionopts.Options) (authscope.ApplyOptions, error) {
	return authscope.ApplyOptionsFromPath(opts.EnableAuthScopeTranslation, opts.AMSMappingPath)
}

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

// ApplyAuthScopeTranslation enriches a generated spec with rights extensions and PAT scopes.
func ApplyAuthScopeTranslation(specMap map[string]any, opts extractionopts.Options, classifyExisting bool) error {
	ao, err := AuthApplyOptions(opts)
	if err != nil {
		return err
	}
	if !ao.Enabled {
		return nil
	}
	st := authscope.EnrichSpec(specMap, ao, classifyExisting)
	authscope.MergeDiagnostics(specMap, st)
	return nil
}
