package csharpextract

import (
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/authscope"
	"github.com/sailpoint-oss/cartographer/extract/index"
	"github.com/sailpoint-oss/cartographer/extract/sharedspec"
	"github.com/sailpoint-oss/cartographer/extract/specmodel"
)

type SpecConfig struct {
	Title           string
	Version         string
	Description     string
	OpenAPIVersion  string
	ServiceTemplate string
	TreeShake       bool
	ErrorSchema     string
	AuthScope       authscope.ApplyOptions
}

func GenerateSpec(result *Result, cfg SpecConfig) map[string]any {
	return sharedspec.GenerateSpec(result.ToUnifiedResult(), specmodel.SpecConfig{
		Title:           cfg.Title,
		Version:         cfg.Version,
		Description:     cfg.Description,
		OpenAPIVersion:  cfg.OpenAPIVersion,
		ServiceTemplate: cfg.ServiceTemplate,
		TreeShake:       cfg.TreeShake,
		ErrorSchema:     cfg.ErrorSchema,
		AuthScope:       cfg.AuthScope,
	}, &adapter{})
}

type adapter struct{}

func (a *adapter) ParamTypeToSchema(typeName string) map[string]any {
	switch strings.ToLower(strings.TrimSuffix(typeName, "?")) {
	case "string", "guid":
		return map[string]any{"type": "string"}
	case "int", "long", "short":
		return map[string]any{"type": "integer"}
	case "decimal", "double", "float":
		return map[string]any{"type": "number"}
	case "bool", "boolean":
		return map[string]any{"type": "boolean"}
	default:
		return map[string]any{}
	}
}

func (a *adapter) IsSimpleType(t string) bool { return isSimpleType(t) }

func (a *adapter) IsFileType(typeName string) bool {
	t := strings.ToLower(typeName)
	return strings.Contains(t, "iformfile") || strings.Contains(t, "stream")
}

func (a *adapter) FormParamSchema(typeName string) map[string]any {
	if a.IsFileType(typeName) {
		return map[string]any{"type": "string", "format": "binary"}
	}
	return a.ParamTypeToSchema(typeName)
}

func (a *adapter) BuildSecuritySchemes(result *specmodel.Result) map[string]any {
	scopes := map[string]any{}
	for _, op := range result.Operations {
		for _, sec := range op.Security {
			for _, scope := range sec.Scopes {
				scopes[scope] = scope
			}
		}
	}
	if len(scopes) == 0 {
		return nil
	}
	return map[string]any{
		"oauth2": map[string]any{
			"type": "oauth2",
			"flows": map[string]any{
				"clientCredentials": map[string]any{
					"tokenUrl": "/oauth/token",
					"scopes":   scopes,
				},
			},
		},
	}
}

func (a *adapter) FindTypeBySimpleName(types map[string]*index.TypeDecl, name string) *index.TypeDecl {
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
