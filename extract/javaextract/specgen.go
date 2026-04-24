package javaextract

import (
	"strconv"
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/index"
	"github.com/sailpoint-oss/cartographer/extract/sharedspec"
	"github.com/sailpoint-oss/cartographer/extract/specmodel"
)

// SpecConfig holds configuration for OpenAPI spec generation from Java extraction.
type SpecConfig struct {
	Title           string
	Version         string
	Description     string
	OpenAPIVersion  string // "3.1" or "3.2"
	ServiceTemplate string // "atlas-boot" or "atlas"
	TreeShake       bool
}

// GenerateSpec converts Java extraction results into a complete OpenAPI spec.
func GenerateSpec(result *Result, cfg SpecConfig) map[string]any {
	unified := result.ToUnifiedResult()
	adapter := &javaAdapter{}
	return sharedspec.GenerateSpec(unified, specmodel.SpecConfig{
		Title:           cfg.Title,
		Version:         cfg.Version,
		Description:     cfg.Description,
		OpenAPIVersion:  cfg.OpenAPIVersion,
		ServiceTemplate: cfg.ServiceTemplate,
		TreeShake:       cfg.TreeShake,
	}, adapter)
}

// javaAdapter implements sharedspec.LanguageAdapter for Java.
type javaAdapter struct{}

func (a *javaAdapter) ParamTypeToSchema(typeName string) map[string]any {
	switch strings.ToLower(typeName) {
	case "string", "java.lang.string":
		return map[string]any{"type": "string"}
	case "int", "integer", "java.lang.integer":
		return map[string]any{"type": "integer", "format": "int32"}
	case "long", "java.lang.long":
		return map[string]any{"type": "integer", "format": "int64"}
	case "short", "java.lang.short":
		return map[string]any{"type": "integer", "format": "int32"}
	case "double", "java.lang.double":
		return map[string]any{"type": "number", "format": "double"}
	case "float", "java.lang.float":
		return map[string]any{"type": "number", "format": "float"}
	case "boolean", "java.lang.boolean":
		return map[string]any{"type": "boolean"}
	case "uuid", "java.util.uuid":
		return map[string]any{"type": "string", "format": "uuid"}
	case "date", "localdate":
		return map[string]any{"type": "string", "format": "date"}
	case "localdatetime", "offsetdatetime", "zoneddatetime", "instant":
		return map[string]any{"type": "string", "format": "date-time"}
	case "bigdecimal":
		return map[string]any{"type": "string", "format": "decimal"}
	case "object":
		return map[string]any{"type": "object"}
	}
	return map[string]any{} // unknown
}

func (a *javaAdapter) IsSimpleType(t string) bool {
	simple := map[string]bool{
		"String": true, "string": true, "int": true, "Integer": true,
		"long": true, "Long": true, "double": true, "Double": true,
		"float": true, "Float": true, "boolean": true, "Boolean": true,
		"void": true, "Void": true, "UUID": true, "Date": true,
		"short": true, "Short": true,
		"LocalDate": true, "LocalDateTime": true, "OffsetDateTime": true,
		"ZonedDateTime": true, "Instant": true, "BigDecimal": true,
		"Object": true, "byte[]": true,
	}
	return simple[t]
}

func (a *javaAdapter) BuildSecuritySchemes(result *specmodel.Result) map[string]any {
	allScopes := make(map[string]bool)
	for _, op := range result.Operations {
		for _, sec := range op.Security {
			if sec.Scheme == "oauth2" {
				for _, s := range sec.Scopes {
					allScopes[s] = true
				}
			}
		}
	}
	if len(allScopes) == 0 {
		return nil
	}

	scopeMap := make(map[string]any)
	for s := range allScopes {
		scopeMap[s] = scopeDescription(s)
	}

	return map[string]any{
		"oauth2": map[string]any{
			"type": "oauth2",
			"flows": map[string]any{
				"clientCredentials": map[string]any{
					"tokenUrl": "/oauth/token",
					"scopes":   scopeMap,
				},
			},
		},
	}
}

func (a *javaAdapter) FindTypeBySimpleName(types map[string]*index.TypeDecl, name string) *index.TypeDecl {
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

func (a *javaAdapter) IsFileType(typeName string) bool {
	switch typeName {
	case "MultipartFile", "InputStream", "File", "byte[]", "Part":
		return true
	}
	return false
}

func (a *javaAdapter) FormParamSchema(typeName string) map[string]any {
	if a.IsFileType(typeName) {
		return map[string]any{"type": "string", "format": "binary"}
	}
	return a.ParamTypeToSchema(typeName)
}

// applyJavaFieldAnnotations reads field annotations and applies OpenAPI schema constraints.
// This is used by the typeToSchema fallback path and by the index resolver.
func applyJavaFieldAnnotations(f index.FieldDecl, schema map[string]any) {
	if f.Annotations == nil {
		return
	}

	schemaType, _ := schema["type"].(string)
	isArray := schemaType == "array"
	isString := schemaType == "string"
	isNumeric := schemaType == "number" || schemaType == "integer"

	for name, value := range f.Annotations {
		switch name {
		case "Size":
			minVal := extractAnnotationNamedParam(value, "min")
			maxVal := extractAnnotationNamedParam(value, "max")
			if minVal != "" {
				if n, err := strconv.Atoi(minVal); err == nil {
					if isArray {
						schema["minItems"] = n
					} else {
						schema["minLength"] = n
					}
				}
			}
			if maxVal != "" {
				if n, err := strconv.Atoi(maxVal); err == nil {
					if isArray {
						schema["maxItems"] = n
					} else {
						schema["maxLength"] = n
					}
				}
			}
		case "Min":
			if n, err := strconv.Atoi(extractAnnotationFirstArg(value)); err == nil {
				schema["minimum"] = n
			}
		case "Max":
			if n, err := strconv.Atoi(extractAnnotationFirstArg(value)); err == nil {
				schema["maximum"] = n
			}
		case "DecimalMin":
			if n, err := strconv.ParseFloat(stripJavaQuotes(extractAnnotationFirstArg(value)), 64); err == nil {
				schema["minimum"] = n
			}
		case "DecimalMax":
			if n, err := strconv.ParseFloat(stripJavaQuotes(extractAnnotationFirstArg(value)), 64); err == nil {
				schema["maximum"] = n
			}
		case "Pattern":
			pat := extractAnnotationNamedParam(value, "regexp")
			if pat == "" {
				pat = extractAnnotationFirstArg(value)
			}
			pat = stripJavaQuotes(pat)
			if pat != "" {
				schema["pattern"] = pat
			}
		case "Email":
			if isString {
				schema["format"] = "email"
			}
		case "NotBlank":
			if isString {
				if _, ok := schema["minLength"]; !ok {
					schema["minLength"] = 1
				}
			}
		case "Future", "Past", "FutureOrPresent", "PastOrPresent":
			schema["format"] = "date-time"
		case "Positive":
			if isNumeric {
				schema["exclusiveMinimum"] = 0
			}
		case "PositiveOrZero":
			if isNumeric {
				schema["minimum"] = 0
			}
		case "Negative":
			if isNumeric {
				schema["exclusiveMaximum"] = 0
			}
		case "NegativeOrZero":
			if isNumeric {
				schema["maximum"] = 0
			}
		case "JsonProperty":
			if strings.Contains(value, "READ_ONLY") {
				schema["readOnly"] = true
			} else if strings.Contains(value, "WRITE_ONLY") {
				schema["writeOnly"] = true
			}
		case "Schema":
			if value != "" {
				if ex := extractAnnotationNamedParam(value, "example"); ex != "" {
					schema["example"] = stripJavaQuotes(ex)
				}
				if extractAnnotationNamedParam(value, "deprecated") == "true" {
					schema["deprecated"] = true
				}
			}
		}
	}
}

// scopeDescription generates a human-readable description from an OAuth2 scope name.
// e.g. "sp:auth-org:create" -> "Create auth-org resources"
func scopeDescription(scope string) string {
	parts := strings.Split(scope, ":")
	if len(parts) < 2 {
		return scope
	}
	// Last part is the action, middle parts are the resource
	action := parts[len(parts)-1]
	resource := strings.Join(parts[1:len(parts)-1], " ")
	if resource == "" {
		resource = parts[0]
	}
	// Capitalize the action
	if len(action) > 0 {
		action = strings.ToUpper(action[:1]) + action[1:]
	}
	return action + " " + resource + " resources"
}

// extractAnnotationNamedParam extracts a named parameter from annotation args.
func extractAnnotationNamedParam(args, paramName string) string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	for _, part := range splitAnnotationArgs(args) {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, paramName) {
			rest := strings.TrimPrefix(part, paramName)
			rest = strings.TrimSpace(rest)
			if strings.HasPrefix(rest, "=") {
				return strings.TrimSpace(rest[1:])
			}
		}
	}
	return ""
}

// extractAnnotationFirstArg extracts the first positional argument from annotation args.
func extractAnnotationFirstArg(args string) string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	parts := splitAnnotationArgs(args)
	if len(parts) > 0 {
		first := strings.TrimSpace(parts[0])
		if !strings.Contains(first, "=") {
			return first
		}
	}
	return args
}
