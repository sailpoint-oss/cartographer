package pythonextract

import (
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/index"
	"github.com/sailpoint-oss/cartographer/extract/sharedspec"
	"github.com/sailpoint-oss/cartographer/extract/specmodel"
)

// SpecConfig holds configuration for OpenAPI spec generation from Python
// extraction results.
type SpecConfig struct {
	Title           string
	Version         string
	Description     string
	OpenAPIVersion  string // "3.1" or "3.2"
	ServiceTemplate string
	TreeShake       bool
	ErrorSchema     string
}

// GenerateSpec converts Python extraction results into a complete OpenAPI spec.
func GenerateSpec(result *Result, cfg SpecConfig) map[string]any {
	unified := result.ToUnifiedResult()

	// Fallback to pyproject metadata when CLI flags did not supply values.
	if cfg.Title == "" && result.Metadata.Name != "" {
		cfg.Title = titleCaseServiceName(result.Metadata.Name)
	}
	if cfg.Version == "" && result.Metadata.Version != "" {
		cfg.Version = result.Metadata.Version
	}
	if cfg.Description == "" && result.Metadata.Description != "" {
		cfg.Description = result.Metadata.Description
	}

	adapter := &pythonAdapter{}
	spec := sharedspec.GenerateSpec(unified, specmodel.SpecConfig{
		Title:           cfg.Title,
		Version:         cfg.Version,
		Description:     cfg.Description,
		OpenAPIVersion:  cfg.OpenAPIVersion,
		ServiceTemplate: cfg.ServiceTemplate,
		TreeShake:       cfg.TreeShake,
		ErrorSchema:     cfg.ErrorSchema,
	}, adapter)

	// Surface the detected framework so downstream analysis can behave
	// differently for GraphQL services.
	if result.Framework != "" {
		if info, ok := spec["info"].(map[string]any); ok {
			info["x-service-framework"] = result.Framework
		}
	}

	return spec
}

// pythonAdapter implements sharedspec.LanguageAdapter for Python.
type pythonAdapter struct{}

func (a *pythonAdapter) ParamTypeToSchema(typeName string) map[string]any {
	switch strings.ToLower(typeName) {
	case "str", "string":
		return map[string]any{"type": "string"}
	case "int", "integer":
		return map[string]any{"type": "integer", "format": "int32"}
	case "long":
		return map[string]any{"type": "integer", "format": "int64"}
	case "float", "double":
		return map[string]any{"type": "number"}
	case "bool", "boolean":
		return map[string]any{"type": "boolean"}
	case "bytes":
		return map[string]any{"type": "string", "format": "byte"}
	case "uuid", "uuid.uuid":
		return map[string]any{"type": "string", "format": "uuid"}
	case "datetime", "datetime.datetime":
		return map[string]any{"type": "string", "format": "date-time"}
	case "date", "datetime.date":
		return map[string]any{"type": "string", "format": "date"}
	case "time", "datetime.time":
		return map[string]any{"type": "string", "format": "time"}
	case "any", "object", "typing.any", "dict":
		return map[string]any{}
	}
	if strings.HasPrefix(typeName, "list[") || strings.HasPrefix(typeName, "List[") {
		return map[string]any{"type": "array", "items": map[string]any{}}
	}
	if strings.HasPrefix(typeName, "dict[") || strings.HasPrefix(typeName, "Dict[") {
		return map[string]any{"type": "object"}
	}
	return map[string]any{}
}

func (a *pythonAdapter) IsSimpleType(t string) bool {
	simple := map[string]bool{
		"str": true, "string": true,
		"int": true, "integer": true, "long": true,
		"float": true, "double": true, "number": true,
		"bool": true, "boolean": true,
		"bytes": true,
		"None":  true, "none": true, "void": true,
		"UUID": true, "uuid": true,
		"datetime": true, "date": true, "time": true,
		"Any": true, "any": true, "object": true, "dict": true,
	}
	return simple[t]
}

func (a *pythonAdapter) BuildSecuritySchemes(result *specmodel.Result) map[string]any {
	schemes := make(map[string]any)
	for _, op := range result.Operations {
		for _, sec := range op.Security {
			switch sec.Scheme {
			case "bearerAuth":
				schemes["bearerAuth"] = map[string]any{
					"type":   "http",
					"scheme": "bearer",
				}
			case "basicAuth":
				schemes["basicAuth"] = map[string]any{
					"type":   "http",
					"scheme": "basic",
				}
			case "apiKey":
				schemes["apiKey"] = map[string]any{
					"type": "apiKey",
					"name": "X-API-Key",
					"in":   "header",
				}
			case "oauth2":
				if _, exists := schemes["oauth2"]; !exists {
					schemes["oauth2"] = map[string]any{
						"type": "oauth2",
						"flows": map[string]any{
							"clientCredentials": map[string]any{
								"tokenUrl": "/oauth/token",
								"scopes":   map[string]any{},
							},
						},
					}
				}
			}
		}
	}
	return schemes
}

func (a *pythonAdapter) FindTypeBySimpleName(types map[string]*index.TypeDecl, name string) *index.TypeDecl {
	if decl, ok := types[name]; ok {
		return decl
	}
	for _, decl := range types {
		if decl.Name == name {
			return decl
		}
	}
	return nil
}

func (a *pythonAdapter) IsFileType(typeName string) bool {
	switch strings.TrimSpace(typeName) {
	case "UploadFile", "bytes":
		return true
	}
	return false
}

func (a *pythonAdapter) FormParamSchema(typeName string) map[string]any {
	return a.ParamTypeToSchema(typeName)
}

// titleCaseServiceName converts "example-service" or "example_service" into
// "Example Service" for the OpenAPI info.title fallback.
func titleCaseServiceName(s string) string {
	s = strings.ReplaceAll(s, "_", "-")
	parts := strings.Split(s, "-")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
	}
	return strings.Join(parts, " ")
}
