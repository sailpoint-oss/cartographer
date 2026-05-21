package sharedspec

import (
	"github.com/sailpoint-oss/cartographer/extract/index"
)

// BodySchemaEnricher optionally merges indexed validation metadata into request body schemas.
type BodySchemaEnricher interface {
	EnrichRequestBodySchema(typeName string, schema map[string]any, types map[string]*index.TypeDecl) map[string]any
}

func enrichBodySchema(adapter LanguageAdapter, typeName string, schema map[string]any, types map[string]*index.TypeDecl) map[string]any {
	if enricher, ok := adapter.(BodySchemaEnricher); ok {
		return enricher.EnrichRequestBodySchema(typeName, schema, types)
	}
	return schema
}
