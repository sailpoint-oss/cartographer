package sharedspec

import (
	"github.com/sailpoint-oss/cartographer/extract/index"
	"github.com/sailpoint-oss/cartographer/extract/specmodel"
)

// LanguageAdapter provides language-specific behavior for spec generation.
type LanguageAdapter interface {
	// ParamTypeToSchema converts a parameter type name to an OpenAPI schema.
	ParamTypeToSchema(typeName string) map[string]any
	// IsSimpleType returns true if the type does not need a $ref schema.
	IsSimpleType(typeName string) bool
	// BuildSecuritySchemes generates the securitySchemes component.
	BuildSecuritySchemes(result *specmodel.Result) map[string]any
	// FindTypeBySimpleName looks up a TypeDecl by simple name.
	FindTypeBySimpleName(types map[string]*index.TypeDecl, name string) *index.TypeDecl
	// IsFileType checks if a type represents a file upload.
	IsFileType(typeName string) bool
	// FormParamSchema returns an OpenAPI schema for a form parameter type.
	FormParamSchema(typeName string) map[string]any
}
