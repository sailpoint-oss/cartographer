// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"go/types"
)

// ResponseRegistry maintains a registry of known response types and their schemas.
type ResponseRegistry struct {
	// Maps type name (e.g., "web.Error") to response schema
	schemas map[string]*ResponseSchema

	// Cache for analyzed types within this run
	typeCache map[string]*TypeInfo
}

// ResponseSchema describes a response type's structure.
type ResponseSchema struct {
	TypeName    string
	Package     string
	StatusCode  int
	ContentType string
	Fields      []FieldInfo
	Description string
}

// NewResponseRegistry creates a new response registry with predefined web.* types.
func NewResponseRegistry() *ResponseRegistry {
	registry := &ResponseRegistry{
		schemas:   make(map[string]*ResponseSchema),
		typeCache: make(map[string]*TypeInfo),
	}

	// Pre-populate with web package response types
	registry.registerWebErrorType()

	return registry
}

// registerWebErrorType registers the web.Error response type.
func (rr *ResponseRegistry) registerWebErrorType() {
	// web.Error structure
	webError := &ResponseSchema{
		TypeName:    "Error",
		Package:     "github.com/sailpoint/atlas-go/v2/atlas/web",
		ContentType: "application/json",
		Description: "Standard API error response",
		Fields: []FieldInfo{
			{
				Name:        "detailCode",
				Type:        "string",
				JSONName:    "detailCode",
				Description: "Error detail code",
			},
			{
				Name:        "trackingId",
				Type:        "string",
				JSONName:    "trackingId",
				Description: "Request tracking ID for debugging",
			},
			{
				Name:        "messages",
				Type:        "[]ErrorMessage",
				JSONName:    "messages",
				Description: "Localized error messages",
			},
		},
	}

	rr.schemas["github.com/sailpoint/atlas-go/v2/atlas/web.Error"] = webError
	rr.schemas["web.Error"] = webError
}

// GetResponseSchema retrieves the response schema for a given type.
func (rr *ResponseRegistry) GetResponseSchema(typeName string) *ResponseSchema {
	return rr.schemas[typeName]
}

// RegisterResponseType analyzes and registers a custom response type.
func (rr *ResponseRegistry) RegisterResponseType(typeName string, typeInfo *types.Type, pkg *types.Package) *ResponseSchema {
	// Check cache first
	key := pkg.Path() + "." + typeName
	if schema, exists := rr.schemas[key]; exists {
		return schema
	}

	// Create new schema
	schema := &ResponseSchema{
		TypeName:    typeName,
		Package:     pkg.Path(),
		ContentType: "application/json",
		Fields:      make([]FieldInfo, 0),
	}

	// Extract fields from type
	// This will be implemented to handle structs, interfaces, etc.
	schema.Fields = rr.extractFieldsFromType(typeInfo)

	// Register in cache
	rr.schemas[key] = schema
	rr.schemas[typeName] = schema

	return schema
}

// extractFieldsFromType extracts field information from a Go type.
func (rr *ResponseRegistry) extractFieldsFromType(t *types.Type) []FieldInfo {
	fields := make([]FieldInfo, 0)

	// Handle different type kinds
	switch typ := (*t).(type) {
	case *types.Named:
		// Get the underlying type
		underlying := typ.Underlying()
		if structType, ok := underlying.(*types.Struct); ok {
			for i := 0; i < structType.NumFields(); i++ {
				field := structType.Field(i)
				tag := structType.Tag(i)

				fieldInfo := FieldInfo{
					Name:     field.Name(),
					Type:     field.Type().String(),
					JSONName: extractJSONTag(tag),
				}

				// Handle exported fields only
				if field.Exported() {
					fields = append(fields, fieldInfo)
				}
			}
		}
	}

	return fields
}

// extractJSONTag extracts the JSON tag name from a struct tag.
func extractJSONTag(tag string) string {
	// Simple JSON tag extraction
	// TODO: Use reflect.StructTag for proper parsing
	if tag == "" {
		return ""
	}

	// Look for json:"fieldName"
	start := -1
	for i := 0; i < len(tag); i++ {
		if i+5 < len(tag) && tag[i:i+5] == "json:" {
			start = i + 6 // Skip 'json:"'
			break
		}
	}

	if start == -1 {
		return ""
	}

	// Find closing quote
	end := start
	for end < len(tag) && tag[end] != '"' {
		end++
	}

	if end > start {
		// Extract and handle options (e.g., "fieldName,omitempty")
		jsonTag := tag[start:end]
		if commaIdx := 0; commaIdx < len(jsonTag) {
			for i, ch := range jsonTag {
				if ch == ',' {
					return jsonTag[:i]
				}
			}
		}
		return jsonTag
	}

	return ""
}

// GetWebErrorSchema returns the standard web.Error schema for a given status code.
func (rr *ResponseRegistry) GetWebErrorSchema(statusCode int) *ResponseSchema {
	schema := rr.GetResponseSchema("web.Error")
	if schema != nil {
		// Create a copy with the specific status code
		schemaCopy := *schema
		schemaCopy.StatusCode = statusCode
		schemaCopy.Description = GetHTTPStatusDescription(statusCode)
		return &schemaCopy
	}
	return nil
}
