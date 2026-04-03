package sharedspec

import (
	"fmt"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// CamelCaseToWords converts a camelCase string to space-separated words.
func CamelCaseToWords(s string) string {
	var words []string
	start := 0
	for i := 1; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			words = append(words, s[start:i])
			start = i
		}
	}
	words = append(words, s[start:])

	result := strings.Join(words, " ")
	if len(result) > 0 {
		result = strings.ToUpper(result[:1]) + result[1:]
	}
	return result
}

// LastPathSegment extracts the last non-parameter path segment.
func LastPathSegment(path string) string {
	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		seg := parts[i]
		if seg != "" && !strings.HasPrefix(seg, "{") {
			return seg
		}
	}
	return ""
}

// skipSegments are path segments that should not be treated as resource names.
var skipSegments = map[string]bool{
	"v1": true, "v2": true, "v3": true, "v4": true,
	"api": true, "beta": true, "alpha": true,
}

// extractResourceFromPath gets the last meaningful non-parameter path segment,
// skipping version prefixes and common non-resource segments.
// "/v3/entitlements/{id}" -> "entitlements"
// "/api/v1/roles/{id}/access-profiles" -> "accessprofiles"
func extractResourceFromPath(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	var best string
	for _, p := range parts {
		if p == "" || strings.HasPrefix(p, "{") {
			continue
		}
		if skipSegments[strings.ToLower(p)] {
			continue
		}
		best = strings.ReplaceAll(p, "-", "")
	}
	return best
}

// singularize performs naive English singularization.
func singularize(s string) string {
	if strings.HasSuffix(s, "ies") {
		return s[:len(s)-3] + "y"
	}
	if strings.HasSuffix(s, "ses") || strings.HasSuffix(s, "xes") || strings.HasSuffix(s, "zes") {
		return s[:len(s)-2]
	}
	if strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss") {
		return s[:len(s)-1]
	}
	return s
}

// AutoSummary generates a human-readable summary from HTTP method and path.
// "GET" + "/entitlements" -> "List Entitlements"
// "GET" + "/entitlements/{id}" -> "Get Entitlement"
// "POST" + "/entitlements" -> "Create Entitlement"
func AutoSummary(method, path string) string {
	resource := extractResourceFromPath(path)
	if resource == "" {
		return ""
	}

	titleCaser := cases.Title(language.English)
	resourceTitle := titleCaser.String(strings.ReplaceAll(resource, "-", " "))

	// Determine if path ends with a parameter (single resource) or collection
	hasTrailingParam := strings.HasSuffix(path, "}") && strings.Contains(path, "{")

	switch strings.ToUpper(method) {
	case "GET":
		if hasTrailingParam {
			return "Get " + singularize(resourceTitle)
		}
		return "List " + resourceTitle
	case "POST":
		return "Create " + singularize(resourceTitle)
	case "PUT":
		return "Update " + singularize(resourceTitle)
	case "PATCH":
		return "Patch " + singularize(resourceTitle)
	case "DELETE":
		return "Delete " + singularize(resourceTitle)
	default:
		return strings.ToUpper(method) + " " + resourceTitle
	}
}

// AutoDescription generates a description from an operation's method, path, and summary.
func AutoDescription(method, path, summary string) string {
	resource := extractResourceFromPath(path)
	if resource == "" {
		return ""
	}

	titleCaser := cases.Title(language.English)
	resourceTitle := titleCaser.String(strings.ReplaceAll(resource, "-", " "))
	hasTrailingParam := strings.HasSuffix(path, "}") && strings.Contains(path, "{")

	switch strings.ToUpper(method) {
	case "GET":
		if hasTrailingParam {
			return fmt.Sprintf("Retrieve a single %s by its unique identifier.", singularize(resourceTitle))
		}
		return fmt.Sprintf("Retrieve a list of %s.", resourceTitle)
	case "POST":
		return fmt.Sprintf("Create a new %s.", singularize(resourceTitle))
	case "PUT":
		return fmt.Sprintf("Replace an existing %s.", singularize(resourceTitle))
	case "PATCH":
		return fmt.Sprintf("Partially update an existing %s.", singularize(resourceTitle))
	case "DELETE":
		return fmt.Sprintf("Delete an existing %s.", singularize(resourceTitle))
	}
	return ""
}

// AutoResponseDesc generates a response description based on method, path, and response type.
func AutoResponseDesc(method, path, responseType string) string {
	resource := extractResourceFromPath(path)
	titleCaser := cases.Title(language.English)
	resourceTitle := ""
	if resource != "" {
		resourceTitle = titleCaser.String(strings.ReplaceAll(resource, "-", " "))
	}
	if resourceTitle == "" {
		return ""
	}

	hasTrailingParam := strings.HasSuffix(path, "}") && strings.Contains(path, "{")

	switch strings.ToUpper(method) {
	case "GET":
		if hasTrailingParam {
			return singularize(resourceTitle)
		}
		if responseType != "" && (strings.HasPrefix(responseType, "[]") || strings.Contains(strings.ToLower(responseType), "list")) {
			return "List of " + resourceTitle
		}
		return resourceTitle
	case "POST":
		return singularize(resourceTitle) + " created"
	case "PUT", "PATCH":
		return singularize(resourceTitle) + " updated"
	case "DELETE":
		return singularize(resourceTitle) + " deleted"
	}
	return ""
}

// GetStatusDescription returns a human-readable description for an HTTP status code.
func GetStatusDescription(code int) string {
	switch code {
	case 200:
		return "Successful operation"
	case 201:
		return "Created"
	case 202:
		return "Accepted"
	case 204:
		return "No Content"
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 500:
		return "Internal Server Error"
	default:
		return fmt.Sprintf("Status %d", code)
	}
}

// ErrorResponseSchema returns the shared ErrorResponse schema definition.
func ErrorResponseSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"detailCode": map[string]any{"type": "string"},
			"trackingId": map[string]any{"type": "string"},
			"messages": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"locale":       map[string]any{"type": "string"},
						"localeOrigin": map[string]any{"type": "string"},
						"text":         map[string]any{"type": "string"},
					},
				},
			},
		},
	}
}

// ErrorResponseRef returns a $ref to the shared ErrorResponse schema.
func ErrorResponseRef() map[string]any {
	return map[string]any{
		"$ref": "#/components/schemas/ErrorResponse",
	}
}

// StripGenericSuffix removes generic type parameters from a type name.
// e.g. "BaseResource<T>" -> "BaseResource"
func StripGenericSuffix(name string) string {
	if idx := strings.Index(name, "<"); idx >= 0 {
		return name[:idx]
	}
	return name
}

// GenerateStubSchema creates a minimal schema for types that are referenced
// but not defined in the scanned source (e.g. external dependencies).
func GenerateStubSchema(typeName string) map[string]any {
	switch typeName {
	case "JsonPatch":
		return map[string]any{
			"type":        "array",
			"description": "JSON Patch document (RFC 6902)",
			"items": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"op": map[string]any{
						"type": "string",
						"enum": []any{"add", "remove", "replace", "move", "copy", "test"},
					},
					"path": map[string]any{
						"type": "string",
					},
					"value": map[string]any{},
				},
				"required": []any{"op", "path"},
			},
		}
	case "BaseReferenceDto":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type": map[string]any{
					"type": "string",
					"enum": []any{"ACCOUNT", "IDENTITY", "ROLE", "ACCESS_PROFILE", "ENTITLEMENT", "SOURCE", "APP"},
				},
				"id":   map[string]any{"type": "string"},
				"name": map[string]any{"type": "string"},
			},
			"required": []any{"type", "id"},
		}
	case "ObjectImportResult":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"results": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "object"},
				},
			},
		}
	case "StatusEnum":
		return map[string]any{
			"type": "string",
			"enum": []any{"ACTIVE", "INACTIVE", "PENDING", "CANCELLED", "ERROR", "COMPLETED"},
		}
	case "DtoType":
		return map[string]any{
			"type": "string",
			"enum": []any{"ACCOUNT", "IDENTITY", "ROLE", "ACCESS_PROFILE", "ENTITLEMENT", "SOURCE", "APP"},
		}
	case "ErrorMessageDto":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"locale": map[string]any{
					"type":    "string",
					"example": "en-US",
				},
				"localeOrigin": map[string]any{
					"type": "string",
					"enum": []any{"DEFAULT", "REQUEST"},
				},
				"text": map[string]any{"type": "string"},
			},
		}
	case "Locale":
		return map[string]any{
			"type":    "string",
			"example": "en-US",
		}
	case "RequestedItemStatus":
		return map[string]any{
			"type": "string",
			"enum": []any{"PENDING", "APPROVED", "REJECTED", "CANCELLED", "COMPLETED", "FAILED", "PROVISIONING"},
		}
	case "InnerHit":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"paths": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "string"},
				},
			},
		}
	case "TypedReference":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type": map[string]any{"type": "string"},
				"id":   map[string]any{"type": "string"},
			},
			"required": []any{"type", "id"},
		}
	case "Reference", "BaseReference":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type": map[string]any{"type": "string"},
				"id":   map[string]any{"type": "string"},
				"name": map[string]any{"type": "string"},
			},
			"required": []any{"type", "id"},
		}
	case "OwnerReference":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type": map[string]any{"type": "string"},
				"id":   map[string]any{"type": "string"},
				"name": map[string]any{"type": "string"},
			},
			"required": []any{"type", "id"},
		}
	case "Paginator", "PagingResult":
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"count": map[string]any{"type": "integer"},
				"items": map[string]any{
					"type":  "array",
					"items": map[string]any{"type": "object"},
				},
			},
		}
	}

	return map[string]any{
		"type":        "object",
		"description": fmt.Sprintf("External type: %s", typeName),
	}
}

// TreeShake removes unreferenced schemas from a spec.
func TreeShake(spec map[string]any) {
	components, ok := spec["components"].(map[string]any)
	if !ok {
		return
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		return
	}

	refs := make(map[string]bool)
	CollectRefs(spec["paths"], refs)
	CollectRefs(spec["webhooks"], refs)

	changed := true
	for changed {
		changed = false
		for name := range schemas {
			if refs["#/components/schemas/"+name] {
				before := len(refs)
				CollectRefs(schemas[name], refs)
				if len(refs) > before {
					changed = true
				}
			}
		}
	}

	for name := range schemas {
		if !refs["#/components/schemas/"+name] {
			delete(schemas, name)
		}
	}
}

// CollectRefs recursively collects all $ref values from a spec structure.
func CollectRefs(v any, refs map[string]bool) {
	switch val := v.(type) {
	case map[string]any:
		if ref, ok := val["$ref"].(string); ok {
			refs[ref] = true
		}
		for _, child := range val {
			CollectRefs(child, refs)
		}
	case []any:
		for _, child := range val {
			CollectRefs(child, refs)
		}
	}
}

// EnsureSchemaRefsExist adds stub schemas for any local component schema refs
// that point to missing entries. This keeps generated specs internally
// consistent even when extraction misses a type definition.
func EnsureSchemaRefsExist(spec map[string]any) int {
	components, ok := spec["components"].(map[string]any)
	if !ok {
		return 0
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		schemas = make(map[string]any)
		components["schemas"] = schemas
	}
	refs := make(map[string]bool)
	CollectRefs(spec, refs)
	added := 0
	const prefix = "#/components/schemas/"
	for ref := range refs {
		if !strings.HasPrefix(ref, prefix) {
			continue
		}
		name := strings.TrimPrefix(ref, prefix)
		if name == "" {
			continue
		}
		if _, exists := schemas[name]; exists {
			continue
		}
		schemas[name] = GenerateStubSchema(name)
		added++
	}
	return added
}
