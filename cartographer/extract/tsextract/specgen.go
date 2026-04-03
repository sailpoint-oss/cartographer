package tsextract

import (
	"strconv"
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/index"
	"github.com/sailpoint-oss/cartographer/extract/sharedspec"
	"github.com/sailpoint-oss/cartographer/extract/specmodel"
)

// SpecConfig holds configuration for OpenAPI spec generation from TypeScript extraction.
type SpecConfig struct {
	Title           string
	Version         string
	Description     string
	OpenAPIVersion  string // "3.1" or "3.2"
	ServiceTemplate string
	TreeShake       bool
}

// GenerateSpec converts TypeScript extraction results into a complete OpenAPI spec.
func GenerateSpec(result *Result, cfg SpecConfig) map[string]any {
	unified := result.ToUnifiedResult()
	adapter := &tsAdapter{}
	return sharedspec.GenerateSpec(unified, specmodel.SpecConfig{
		Title:           cfg.Title,
		Version:         cfg.Version,
		Description:     cfg.Description,
		OpenAPIVersion:  cfg.OpenAPIVersion,
		ServiceTemplate: cfg.ServiceTemplate,
		TreeShake:       cfg.TreeShake,
	}, adapter)
}

// tsAdapter implements sharedspec.LanguageAdapter for TypeScript.
type tsAdapter struct{}

func (a *tsAdapter) ParamTypeToSchema(typeName string) map[string]any {
	switch strings.ToLower(typeName) {
	case "string":
		return map[string]any{"type": "string"}
	case "number":
		return map[string]any{"type": "number"}
	case "boolean":
		return map[string]any{"type": "boolean"}
	case "date":
		return map[string]any{"type": "string", "format": "date-time"}
	case "any", "object", "unknown":
		return map[string]any{}
	}
	return map[string]any{}
}

func (a *tsAdapter) IsSimpleType(t string) bool {
	simple := map[string]bool{
		"string": true, "number": true, "boolean": true,
		"any": true, "void": true, "undefined": true,
		"null": true, "never": true, "unknown": true,
		"Date": true, "object": true, "Object": true,
	}
	return simple[t]
}

func (a *tsAdapter) BuildSecuritySchemes(result *specmodel.Result) map[string]any {
	schemes := make(map[string]any)
	for _, op := range result.Operations {
		for _, sec := range op.Security {
			switch sec.Scheme {
			case "bearerAuth":
				schemes["bearerAuth"] = map[string]any{
					"type":   "http",
					"scheme": "bearer",
				}
			case "oauth2":
				if _, exists := schemes["oauth2"]; !exists {
					schemes["oauth2"] = map[string]any{
						"type": "oauth2",
						"flows": map[string]any{
							"clientCredentials": map[string]any{
								"tokenUrl": "/oauth/token",
								"scopes":   collectTSOAuth2Scopes(result),
							},
						},
					}
				}
			default:
				// If scheme contains read/write patterns, treat as OAuth2 scopes
				if strings.Contains(sec.Scheme, "read") || strings.Contains(sec.Scheme, "write") {
					if _, exists := schemes["oauth2"]; !exists {
						schemes["oauth2"] = map[string]any{
							"type": "oauth2",
							"flows": map[string]any{
								"clientCredentials": map[string]any{
									"tokenUrl": "/oauth/token",
									"scopes":   collectTSOAuth2Scopes(result),
								},
							},
						}
					}
				}
			}
		}
	}
	return schemes
}

func collectTSOAuth2Scopes(result *specmodel.Result) map[string]any {
	scopeMap := make(map[string]any)
	for _, op := range result.Operations {
		for _, sec := range op.Security {
			if sec.Scheme != "bearerAuth" {
				for _, s := range sec.Scopes {
					scopeMap[s] = scopeDescription(s)
				}
			}
		}
	}
	return scopeMap
}

// scopeDescription generates a human-readable description from an OAuth2 scope name.
func scopeDescription(scope string) string {
	parts := strings.Split(scope, ":")
	if len(parts) < 2 {
		return scope
	}
	action := parts[len(parts)-1]
	resource := strings.Join(parts[1:len(parts)-1], " ")
	if resource == "" {
		resource = parts[0]
	}
	if len(action) > 0 {
		action = strings.ToUpper(action[:1]) + action[1:]
	}
	return action + " " + resource + " resources"
}

func (a *tsAdapter) FindTypeBySimpleName(types map[string]*index.TypeDecl, name string) *index.TypeDecl {
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

func (a *tsAdapter) IsFileType(_ string) bool {
	return false // TS doesn't have file upload types like Java
}

func (a *tsAdapter) FormParamSchema(typeName string) map[string]any {
	return a.ParamTypeToSchema(typeName)
}

// applyClassValidatorAnnotations applies class-validator decorator constraints to a schema.
func applyClassValidatorAnnotations(schema map[string]any, annotations map[string]string) {
	if annotations == nil {
		return
	}

	for name, value := range annotations {
		arg := extractAnnotationArg(value)
		switch name {
		case "IsEmail":
			schema["format"] = "email"
		case "IsUUID":
			schema["format"] = "uuid"
		case "IsUrl":
			schema["format"] = "uri"
		case "IsDateString":
			schema["format"] = "date-time"
		case "MinLength":
			if n, err := strconv.Atoi(arg); err == nil {
				schema["minLength"] = n
			}
		case "MaxLength":
			if n, err := strconv.Atoi(arg); err == nil {
				schema["maxLength"] = n
			}
		case "Min":
			if n, err := strconv.Atoi(arg); err == nil {
				schema["minimum"] = n
			}
		case "Max":
			if n, err := strconv.Atoi(arg); err == nil {
				schema["maximum"] = n
			}
		case "Matches":
			pat := stripTSQuotes(arg)
			if pat != "" {
				schema["pattern"] = pat
			}
		}
	}
}

func applyApiPropertyAnnotation(schema map[string]any, annotations map[string]string) {
	if annotations == nil {
		return
	}
	raw, ok := annotations["ApiProperty"]
	if !ok {
		return
	}
	props := extractDecoratorProperties(raw)
	if desc := props["description"]; desc != "" {
		schema["description"] = desc
	}
	if ex := props["example"]; ex != "" {
		schema["example"] = ex
	}
}

// extractAnnotationArg extracts the first argument from annotation value.
func extractAnnotationArg(value string) string {
	value = strings.TrimPrefix(value, "(")
	value = strings.TrimSuffix(value, ")")
	return strings.TrimSpace(value)
}
