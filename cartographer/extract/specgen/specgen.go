// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

// Package specgen converts extracted metadata into an OpenAPI specification.
// Ported from Atlas-Go/main.go.
package specgen

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/goextract"
	"github.com/sailpoint-oss/cartographer/extract/sharedspec"
	"github.com/sailpoint-oss/cartographer/extract/sourceloc"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Config holds configuration for OpenAPI spec generation.
type Config struct {
	Title              string
	Version            string
	Description        string
	OpenAPIVersion     string // "3.1" or "3.2"
	OAuth2DeviceURL    string
	OAuth2MetadataURL  string
	DeprecatedSecurity bool
	IncludeWebhooks    bool   // Include webhook documentation (OpenAPI 3.1+)
	WebhookFilter      string // Regex pattern to filter webhooks by name
	TreeShake          bool   // Remove unused schemas from output
}

// Generate generates an OpenAPI specification from extracted metadata.
func Generate(metadata *goextract.ExtractedMetadata, extractor *goextract.Extractor, cfg Config) map[string]interface{} {
	enhancedErrorSchema := extractor.GetEnhancedErrorSchema()
	schemaNameNormalizer := extractor.GetSchemaNameNormalizer()
	errorSchemaAnalyzer := extractor.GetErrorSchemaAnalyzer()

	_ = enhancedErrorSchema // used indirectly via errorSchemaAnalyzer

	spec := generateOpenAPISpec(metadata, cfg, schemaNameNormalizer, errorSchemaAnalyzer)
	sharedspec.EnsureSchemaRefsExist(spec)

	// Tree shake unused schemas
	if cfg.TreeShake {
		sharedspec.TreeShake(spec)
	}

	return spec
}

// genericVerbs are single-word operationIds that should be qualified with the resource name.
var genericVerbs = map[string]bool{
	"list": true, "get": true, "create": true, "update": true,
	"delete": true, "patch": true, "put": true, "post": true,
	"search": true, "count": true, "export": true, "import": true,
}

// qualifyOperationID appends a resource name to generic single-word operationIds.
// "list" + "/entitlements" -> "listEntitlements"
// "getById" stays as "getById" (not a single generic verb)
func qualifyOperationID(id, path, method string) string {
	lower := strings.ToLower(id)
	if !genericVerbs[lower] {
		return id
	}

	resource := extractResourceFromPath(path)
	if resource == "" {
		return id
	}

	titleCaser := cases.Title(language.English)
	qualified := lower + titleCaser.String(resource)
	return qualified
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

func normalizeOpenAPIPath(path string) string {
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

// autoSummary generates a human-readable summary from HTTP method and path.
// "GET" + "/entitlements" -> "List Entitlements"
// "GET" + "/entitlements/{id}" -> "Get Entitlement"
// "POST" + "/entitlements" -> "Create Entitlement"
func autoSummary(method, path string) string {
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

// autoResponseDesc generates a response description based on status code and context.
func autoResponseDesc(statusCode, method, path, responseType string) string {
	resource := extractResourceFromPath(path)
	titleCaser := cases.Title(language.English)
	resourceTitle := ""
	if resource != "" {
		resourceTitle = titleCaser.String(strings.ReplaceAll(resource, "-", " "))
	}

	switch statusCode {
	case "200":
		if strings.EqualFold(method, "GET") {
			hasTrailingParam := strings.HasSuffix(path, "}") && strings.Contains(path, "{")
			if hasTrailingParam {
				return singularize(resourceTitle)
			}
			if responseType != "" && (strings.HasPrefix(responseType, "[]") || strings.Contains(strings.ToLower(responseType), "list")) {
				return "List of " + resourceTitle
			}
			return resourceTitle
		}
		return "Success"
	case "201":
		return singularize(resourceTitle) + " created"
	case "202":
		return "Request accepted"
	case "204":
		return "No content"
	case "400":
		return "Bad request"
	case "401":
		return "Unauthorized"
	case "403":
		return "Forbidden"
	case "404":
		return "Not found"
	case "429":
		return "Too many requests"
	case "500":
		return "Internal server error"
	}
	return ""
}

// isJsonPatchType returns true if the type name suggests a JSON Patch request body.
func isJsonPatchType(typeName string) bool {
	lower := strings.ToLower(typeName)
	return strings.Contains(lower, "jsonpatch") ||
		strings.Contains(lower, "json_patch") ||
		strings.Contains(lower, "patchoperation") ||
		strings.Contains(lower, "patchrequest")
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

// getOpenAPIVersionString returns the full OpenAPI version string.
func getOpenAPIVersionString(version string) string {
	switch version {
	case "3.1":
		return "3.1.0"
	case "3.2":
		return "3.2.0"
	default:
		return "3.2.0"
	}
}

// generateOpenAPISpec generates an OpenAPI specification from extracted metadata.
func generateOpenAPISpec(metadata *goextract.ExtractedMetadata, config Config, schemaNameNormalizer *goextract.SchemaNameNormalizer, errorSchemaAnalyzer *goextract.ErrorSchemaAnalyzer) map[string]interface{} {
	openapiVersionStr := getOpenAPIVersionString(config.OpenAPIVersion)

	spec := map[string]interface{}{
		"openapi": openapiVersionStr,
		"info": map[string]interface{}{
			"title":   config.Title,
			"version": config.Version,
		},
		"paths":      make(map[string]interface{}),
		"components": make(map[string]interface{}),
	}

	if config.Description != "" {
		spec["info"].(map[string]interface{})["description"] = config.Description
	}

	// Build paths
	paths := make(map[string]interface{})
	operationIDCounts := make(map[string]int) // L4: track duplicate operation IDs
	for _, op := range metadata.Operations {
		normalizedPath := normalizeOpenAPIPath(op.Path)
		if normalizedPath == "" {
			continue
		}

		// L3: Skip infrastructure/health endpoints
		if isInfrastructurePath(normalizedPath) {
			continue
		}

		// Qualify generic operationIds with resource name from path
		if op.ID != "" {
			op.ID = qualifyOperationID(op.ID, normalizedPath, op.Method)
		}

		// L4: Deduplicate operation IDs
		if op.ID != "" {
			operationIDCounts[op.ID]++
			if operationIDCounts[op.ID] > 1 {
				pathSegment := sharedspec.LastPathSegment(normalizedPath)
				titleCaser := cases.Title(language.English)
				op.ID = fmt.Sprintf("%s%s%s", op.ID, titleCaser.String(strings.ToLower(op.Method)), titleCaser.String(pathSegment))
			}
		}

		pathObj, exists := paths[normalizedPath]
		if !exists {
			pathObj = make(map[string]interface{})
			paths[normalizedPath] = pathObj
		}

		operation := buildOperation(op, schemaNameNormalizer, errorSchemaAnalyzer)
		pathObj.(map[string]interface{})[strings.ToLower(op.Method)] = operation
	}

	spec["paths"] = paths

	// Build tags array
	tags := buildTags(metadata, config.OpenAPIVersion)
	if len(tags) > 0 {
		spec["tags"] = tags
	}

	// Build components/schemas with normalized names
	schemas := make(map[string]interface{})
	originalToNormalized := make(map[string]string)

	for typeName, typeInfo := range metadata.Types {
		schema := buildSchema(typeInfo)
		normalizedName := schemaNameNormalizer.NormalizeSchemaName(typeName)
		schemas[normalizedName] = schema
		originalToNormalized[typeName] = normalizedName
	}

	ensureWrapperSchemas(schemas, originalToNormalized, schemaNameNormalizer, metadata.Operations)
	generateStatusCodeErrorSchemas(schemas, metadata.Operations, errorSchemaAnalyzer)

	components := make(map[string]interface{})
	if len(schemas) > 0 {
		components["schemas"] = schemas
	}

	securitySchemes := buildSecuritySchemes(metadata, config)
	if len(securitySchemes) > 0 {
		components["securitySchemes"] = securitySchemes
	}

	if len(components) > 0 {
		spec["components"] = components
	}

	// Build webhooks section (OpenAPI 3.1+)
	if config.IncludeWebhooks && len(metadata.Webhooks) > 0 {
		webhooks := buildWebhooks(metadata, config, schemaNameNormalizer)
		if len(webhooks) > 0 {
			spec["webhooks"] = webhooks
		}
	}

	return spec
}

// buildSecuritySchemes builds the security schemes for the OpenAPI spec.
// Emits oauth2, userAuth, and applicationAuth schemes to align with reference specs.
func buildSecuritySchemes(metadata *goextract.ExtractedMetadata, config Config) map[string]interface{} {
	hasAuth := false
	hasUserAuth := false
	hasAppAuth := false
	allScopes := make(map[string]bool)

	for _, op := range metadata.Operations {
		if op.RequiresAuth {
			hasAuth = true
			for _, right := range op.Rights {
				allScopes[right] = true
			}
		}
		if op.UserAuth {
			hasUserAuth = true
		}
		if op.ApplicationAuth {
			hasAppAuth = true
		}
	}

	if !hasAuth {
		return nil
	}

	scopes := make(map[string]interface{})
	for scope := range allScopes {
		scopes[scope] = fmt.Sprintf("Permission: %s", scope)
	}

	flows := make(map[string]interface{})
	isOpenAPI32 := config.OpenAPIVersion == "3.2"

	if isOpenAPI32 && config.OAuth2DeviceURL != "" {
		deviceFlow := map[string]interface{}{
			"deviceAuthorizationUrl": config.OAuth2DeviceURL,
			"scopes":                 scopes,
		}
		flows["deviceAuthorization"] = deviceFlow
	}

	flows["clientCredentials"] = map[string]interface{}{
		"tokenUrl": "/oauth/token",
		"scopes":   scopes,
	}

	oauth2Scheme := map[string]interface{}{
		"type":  "oauth2",
		"flows": flows,
	}

	if isOpenAPI32 && config.OAuth2MetadataURL != "" {
		oauth2Scheme["oauth2MetadataUrl"] = config.OAuth2MetadataURL
	}

	if isOpenAPI32 && config.DeprecatedSecurity {
		oauth2Scheme["deprecated"] = true
	}

	schemes := map[string]interface{}{
		"oauth2": oauth2Scheme,
	}

	if hasUserAuth {
		schemes["userAuth"] = map[string]interface{}{
			"type":        "oauth2",
			"flows":       flows,
			"description": "OAuth2 with user-level token (Authorization Code or Device flow)",
		}
	}
	if hasAppAuth {
		schemes["applicationAuth"] = map[string]interface{}{
			"type":        "oauth2",
			"flows":       flows,
			"description": "OAuth2 with application-level token (Client Credentials flow)",
		}
	}

	return schemes
}

// generateStatusCodeErrorSchemas creates status-code-specific error schemas.
func generateStatusCodeErrorSchemas(schemas map[string]interface{}, operations map[string]*goextract.OperationInfo, errorSchemaAnalyzer *goextract.ErrorSchemaAnalyzer) {
	statusCodesUsed := make(map[int]bool)
	statusCodeExamples := make(map[int][]string)

	for _, op := range operations {
		for _, errResp := range op.ErrorResponses {
			statusCodesUsed[errResp.StatusCode] = true
			if errResp.ErrorMessage != "" {
				statusCodeExamples[errResp.StatusCode] = append(statusCodeExamples[errResp.StatusCode], errResp.ErrorMessage)
			}
		}

		if op.RequiresAuth {
			statusCodesUsed[401] = true
			if len(statusCodeExamples[401]) == 0 {
				statusCodeExamples[401] = []string{"Authentication is required to access this resource"}
			}

			if len(op.Rights) > 0 {
				statusCodesUsed[403] = true
				if len(statusCodeExamples[403]) == 0 {
					forbiddenText := fmt.Sprintf("Forbidden - requires permission: %s", strings.Join(op.Rights, ", "))
					statusCodeExamples[403] = []string{forbiddenText}
				}
			}
		}
	}

	errorSchema := errorSchemaAnalyzer.GetErrorSchema()
	locale := "en-US"
	localeOrigin := "DEFAULT"

	if errorSchema != nil {
		if msgSchema, ok := errorSchema.NestedSchemas["messages"]; ok {
			if localeVal, ok := msgSchema.ConstantFields["locale"].(string); ok {
				locale = localeVal
			}
			if localeOriginVal, ok := msgSchema.ConstantFields["localeOrigin"].(string); ok {
				localeOrigin = localeOriginVal
			}
		}
	}

	for statusCode := range statusCodesUsed {
		schemaName := getErrorSchemaName(statusCode)

		exampleText := getStatusDescription(statusCode)
		if examples, hasExamples := statusCodeExamples[statusCode]; hasExamples && len(examples) > 0 {
			exampleText = examples[0]
		}

		messageSchema := map[string]interface{}{
			"type":     "object",
			"required": []string{"locale", "text"},
			"properties": map[string]interface{}{
				"locale": map[string]interface{}{
					"type":        "string",
					"description": "Locale of the message",
					"const":       locale,
					"default":     locale,
					"example":     locale,
				},
				"localeOrigin": map[string]interface{}{
					"type":        "string",
					"description": "Origin of the locale",
					"const":       localeOrigin,
					"default":     localeOrigin,
					"example":     localeOrigin,
				},
				"text": map[string]interface{}{
					"type":        "string",
					"description": "The error message text",
					"example":     exampleText,
				},
			},
		}

		schemas[schemaName] = map[string]interface{}{
			"type":        "object",
			"required":    []string{"detailCode", "messages"},
			"description": fmt.Sprintf("Error response for HTTP %d (%s)", statusCode, getHttpStatusText(statusCode)),
			"properties": map[string]interface{}{
				"detailCode": map[string]interface{}{
					"type":        "string",
					"description": "HTTP status text",
					"const":       getHttpStatusText(statusCode),
					"default":     getHttpStatusText(statusCode),
					"example":     getHttpStatusText(statusCode),
				},
				"trackingId": map[string]interface{}{
					"type":        "string",
					"description": "Unique identifier for tracing the request",
					"example":     "a1b2c3d4-e5f6-4g7h-8i9j-0k1l2m3n4o5p",
				},
				"messages": map[string]interface{}{
					"type":        "array",
					"description": "Array of localized error messages",
					"items":       messageSchema,
				},
			},
		}
	}
}

// ensureWrapperSchemas creates array/pointer/map wrapper schemas for types referenced in operations.
func ensureWrapperSchemas(schemas map[string]interface{}, originalToNormalized map[string]string, normalizer *goextract.SchemaNameNormalizer, operations map[string]*goextract.OperationInfo) {
	typesUsed := make(map[string]bool)

	for _, op := range operations {
		if op.RequestType != "" {
			typesUsed[op.RequestType] = true
		}
		if op.ResponseType != "" {
			typesUsed[op.ResponseType] = true
		}
		for _, resp := range op.SuccessResponses {
			if resp.ResponseType != "" {
				typesUsed[resp.ResponseType] = true
			}
		}
	}

	for typeName := range typesUsed {
		normalizedName := normalizer.NormalizeSchemaName(typeName)

		if _, exists := schemas[normalizedName]; exists {
			continue
		}

		if typeName == "interface{}" || typeName == "interface {}" || typeName == "any" {
			schemas[normalizedName] = map[string]interface{}{
				"description": "Any value",
			}
			continue
		}

		if len(typeName) > 2 && typeName[0] == '[' && typeName[1] == ']' {
			baseType := typeName[2:]

			if baseType == "interface{}" || baseType == "interface {}" || baseType == "any" {
				schemas[normalizedName] = map[string]interface{}{
					"type":        "array",
					"description": "Array of any values",
					"items":       map[string]interface{}{},
				}
				continue
			}

			baseNormalizedName := normalizer.NormalizeSchemaName(baseType)

			if _, baseExists := schemas[baseNormalizedName]; !baseExists {
				baseSchema := buildFieldSchema(baseType)
				if baseSchema["type"] != nil && baseSchema["type"] != "object" {
					schemas[normalizedName] = map[string]interface{}{
						"type":  "array",
						"items": baseSchema,
					}
					continue
				}
				schemas[baseNormalizedName] = map[string]interface{}{
					"type":        "object",
					"description": fmt.Sprintf("Schema for %s", baseType),
				}
			}

			schemas[normalizedName] = map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"$ref": "#/components/schemas/" + baseNormalizedName,
				},
			}
			continue
		}

		if len(typeName) > 1 && typeName[0] == '*' {
			continue
		}

		if strings.HasPrefix(typeName, "map[") {
			bracketCount := 0
			valueStart := -1
			for i, c := range typeName {
				if c == '[' {
					bracketCount++
				} else if c == ']' {
					bracketCount--
					if bracketCount == 0 {
						valueStart = i + 1
						break
					}
				}
			}

			if valueStart > 0 && valueStart < len(typeName) {
				valueType := typeName[valueStart:]

				if valueType == "interface{}" || valueType == "interface {}" || valueType == "any" {
					schemas[normalizedName] = map[string]interface{}{
						"type":                 "object",
						"description":          "Map with any values",
						"additionalProperties": map[string]interface{}{},
					}
					continue
				}

				valueNormalizedName := normalizer.NormalizeSchemaName(valueType)

				additional := goTypeToOpenAPIPrimitiveSchema(valueType)
				if additional == nil {
					additional = map[string]interface{}{
						"$ref": "#/components/schemas/" + valueNormalizedName,
					}
				}
				schemas[normalizedName] = map[string]interface{}{
					"type":                 "object",
					"additionalProperties": additional,
				}
			} else {
				schemas[normalizedName] = map[string]interface{}{
					"type":                 "object",
					"additionalProperties": map[string]interface{}{},
				}
			}
			continue
		}

		schemas[normalizedName] = map[string]interface{}{
			"type":        "object",
			"description": fmt.Sprintf("Schema for %s", typeName),
		}
	}
}

// buildOperation builds an OpenAPI operation object from OperationInfo.
func buildOperation(op *goextract.OperationInfo, schemaNameNormalizer *goextract.SchemaNameNormalizer, errorSchemaAnalyzer *goextract.ErrorSchemaAnalyzer) map[string]interface{} {
	operation := map[string]interface{}{}

	if op.ID != "" {
		operation["operationId"] = op.ID
	}
	if op.Summary != "" {
		operation["summary"] = op.Summary
	} else {
		operation["summary"] = autoSummary(op.Method, op.Path)
	}
	if op.Description != "" {
		operation["description"] = op.Description
	}
	if len(op.DocSources) > 0 {
		sources := make([]interface{}, 0, len(op.DocSources))
		for _, ds := range op.DocSources {
			m := map[string]interface{}{
				"sourceKind": ds.SourceKind,
				"field":      ds.Field,
			}
			if ds.SourceLocation != "" {
				m["sourceLocation"] = ds.SourceLocation
			}
			if ds.Confidence != "" {
				m["confidence"] = ds.Confidence
			}
			if ds.DetectorId != "" {
				m["detectorId"] = ds.DetectorId
			}
			sources = append(sources, m)
		}
		operation["x-doc-sources"] = sources
	}
	if len(op.Tags) > 0 {
		operation["tags"] = op.Tags
	}
	if op.Deprecated {
		operation["deprecated"] = true
	}
	if op.Experimental {
		operation["x-experimental"] = true
	}
	if op.Private {
		operation["x-internal"] = true
	}

	// Source location extensions
	if op.File != "" {
		operation["x-source-file"] = op.File
	}
	if op.Line > 0 {
		operation["x-source-line"] = op.Line
	}
	if op.Column > 0 {
		operation["x-source-column"] = op.Column
	}

	// Parameters
	params := make([]interface{}, 0)

	if len(op.PathParamDetails) == 0 && len(op.PathParams) == 0 && op.Path != "" {
		op.PathParams = goextract.ExtractPathParams(op.Path)
	}

	if len(op.PathParamDetails) > 0 {
		for _, param := range op.PathParamDetails {
			schema := goTypeToOpenAPISchema(param.Type)
			applyValidateTag(schema, param.ValidateTag)
			paramObj := map[string]interface{}{
				"name":     param.Name,
				"in":       "path",
				"required": true,
				"schema":   schema,
			}
			if param.Description != "" {
				paramObj["description"] = param.Description
			}
			if param.Example != "" {
				paramObj["example"] = param.Example
			}
			params = append(params, paramObj)
		}
	} else {
		for _, param := range op.PathParams {
			params = append(params, map[string]interface{}{
				"name":     param,
				"in":       "path",
				"required": true,
				"schema": map[string]interface{}{
					"type": "string",
				},
			})
		}
	}

	for _, param := range op.QueryParamDetails {
		schema := goTypeToOpenAPISchema(param.Type)
		applyValidateTag(schema, param.ValidateTag)
		paramObj := map[string]interface{}{
			"name":     param.Name,
			"in":       "query",
			"required": param.Required,
			"schema":   schema,
		}
		if param.DefaultValue != "" {
			schema["default"] = param.DefaultValue
		}
		if param.Description != "" {
			paramObj["description"] = param.Description
		}
		if param.Example != "" {
			paramObj["example"] = param.Example
		}
		params = append(params, paramObj)
	}

	for _, param := range op.HeaderParamDetails {
		paramObj := map[string]interface{}{
			"name":     param.Name,
			"in":       "header",
			"required": param.Required,
			"schema":   goTypeToOpenAPISchema(param.Type),
		}
		if param.Description != "" {
			paramObj["description"] = param.Description
		}
		params = append(params, paramObj)
	}

	if op.QueryStringSchema != "" {
		normalizedQuerySchema := schemaNameNormalizer.NormalizeSchemaName(op.QueryStringSchema)
		params = append(params, map[string]interface{}{
			"in": "querystring",
			"schema": map[string]interface{}{
				"$ref": "#/components/schemas/" + normalizedQuerySchema,
			},
		})
	}

	if len(params) > 0 {
		operation["parameters"] = params
	}

	// Request body -- form params take precedence when no typed request body exists
	if len(op.FormParamDetails) > 0 && op.RequestType == "" && !methodDisallowsRequestBody(op.Method) {
		props := make(map[string]interface{})
		formRequired := []string{}
		for _, fp := range op.FormParamDetails {
			propSchema := goTypeToOpenAPISchema(fp.Type)
			if fp.Description != "" {
				propSchema["description"] = fp.Description
			}
			props[fp.Name] = propSchema
			if fp.Required {
				formRequired = append(formRequired, fp.Name)
			}
		}
		formSchema := map[string]interface{}{
			"type":       "object",
			"properties": props,
		}
		if len(formRequired) > 0 {
			formSchema["required"] = formRequired
		}
		operation["requestBody"] = map[string]interface{}{
			"required": true,
			"content": map[string]interface{}{
				"application/x-www-form-urlencoded": map[string]interface{}{
					"schema": formSchema,
				},
			},
		}
	} else if op.RequestType != "" && !methodDisallowsRequestBody(op.Method) {
		normalizedRequestType := schemaNameNormalizer.NormalizeSchemaName(op.RequestType)
		contentType := "application/json"
		if ct := strings.TrimSpace(op.RequestContent); ct != "" {
			contentType = ct
		}
		if contentType == "application/json" && strings.EqualFold(op.Method, "PATCH") && isJsonPatchType(op.RequestType) {
			contentType = "application/json-patch+json"
		}
		operation["requestBody"] = map[string]interface{}{
			"required": true,
			"content": map[string]interface{}{
				contentType: map[string]interface{}{
					"schema": map[string]interface{}{
						"$ref": "#/components/schemas/" + normalizedRequestType,
					},
				},
			},
		}
	}

	// Responses
	responses := make(map[string]interface{})

	if op.ResponseType != "" {
		statusCode := "200"
		if op.ResponseStatus > 0 {
			statusCode = fmt.Sprintf("%d", op.ResponseStatus)
		}
		mediaType := "application/json"
		if strings.TrimSpace(op.ResponseContent) != "" {
			mediaType = strings.TrimSpace(op.ResponseContent)
		}

		if op.IsStreaming && op.StreamMediaType != "" {
			streamItemType := op.StreamItemType
			if streamItemType == "" {
				streamItemType = op.ResponseType
			}
			normalizedItemType := schemaNameNormalizer.NormalizeSchemaName(streamItemType)

			responses[statusCode] = map[string]interface{}{
				"description": "Streaming response",
				"content": map[string]interface{}{
					op.StreamMediaType: map[string]interface{}{
						"itemSchema": map[string]interface{}{
							"$ref": "#/components/schemas/" + normalizedItemType,
						},
					},
				},
			}
		} else {
			schemaObj := buildOperationResponseSchema(op.ResponseType, schemaNameNormalizer)
			responses[statusCode] = map[string]interface{}{
				"description": "Success",
				"content": map[string]interface{}{
					mediaType: map[string]interface{}{
						"schema": schemaObj,
					},
				},
			}
		}
	} else if len(op.SuccessResponses) > 0 {
		for _, resp := range op.SuccessResponses {
			statusCode := fmt.Sprintf("%d", resp.StatusCode)
			mediaType := "application/json"
			if strings.TrimSpace(op.ResponseContent) != "" {
				mediaType = strings.TrimSpace(op.ResponseContent)
			}
			schemaObj := buildOperationResponseSchema(resp.ResponseType, schemaNameNormalizer)
			responses[statusCode] = map[string]interface{}{
				"description": "Success",
				"content": map[string]interface{}{
					mediaType: map[string]interface{}{"schema": schemaObj},
				},
			}
		}
	}

	// Error responses
	errorsByStatus := make(map[int][]goextract.ErrorResponseInfo)
	for _, errResp := range op.ErrorResponses {
		errorsByStatus[errResp.StatusCode] = append(errorsByStatus[errResp.StatusCode], errResp)
	}

	for statusCode := range errorsByStatus {
		statusStr := fmt.Sprintf("%d", statusCode)
		schemaRef := fmt.Sprintf("#/components/schemas/%s", getErrorSchemaName(statusCode))

		response := map[string]interface{}{
			"description": getStatusDescription(statusCode),
			"content": map[string]interface{}{
				"application/json": map[string]interface{}{
					"schema": map[string]interface{}{
						"$ref": schemaRef,
					},
				},
			},
		}

		responses[statusStr] = response
	}

	if op.RequiresAuth {
		if _, exists := responses["401"]; !exists {
			responses["401"] = map[string]interface{}{
				"description": "Unauthorized",
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{
							"$ref": fmt.Sprintf("#/components/schemas/%s", getErrorSchemaName(401)),
						},
					},
				},
			}
		}
		if _, exists := responses["403"]; !exists && len(op.Rights) > 0 {
			responses["403"] = map[string]interface{}{
				"description": "Forbidden",
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{
							"$ref": fmt.Sprintf("#/components/schemas/%s", getErrorSchemaName(403)),
						},
					},
				},
			}
		}
	}

	if len(responses) == 0 {
		statusCode := "200"
		if op.ResponseStatus > 0 {
			statusCode = fmt.Sprintf("%d", op.ResponseStatus)
		} else if strings.EqualFold(op.Method, "DELETE") {
			statusCode = "204"
		}

		desc := autoResponseDesc(statusCode, op.Method, op.Path, op.ResponseType)
		if desc == "" {
			desc = "Success"
		}

		resp := map[string]interface{}{
			"description": desc,
		}

		if statusCode != "204" && strings.TrimSpace(op.ResponseContent) != "" {
			mediaType := strings.TrimSpace(op.ResponseContent)
			schemaObj := map[string]interface{}{"type": "object"}
			if mediaType == "text/plain" {
				schemaObj = map[string]interface{}{"type": "string"}
			}
			resp["content"] = map[string]interface{}{
				mediaType: map[string]interface{}{
					"schema": schemaObj,
				},
			}
		}

		responses[statusCode] = resp
	}

	// Inject operation-level examples into the success response content
	if len(op.Examples) > 0 {
		injectExamplesIntoResponse(responses, op)
	}

	if len(responses) > 0 {
		operation["responses"] = responses
	}

	// Security -- emit userAuth/applicationAuth schemes to align with reference spec format
	if op.Unprotected {
		operation["security"] = []interface{}{}
	} else if op.RequiresAuth && len(op.Rights) > 0 {
		secReqs := []interface{}{}
		if op.UserAuth {
			secReqs = append(secReqs, map[string]interface{}{
				"userAuth": op.Rights,
			})
		}
		if op.ApplicationAuth {
			secReqs = append(secReqs, map[string]interface{}{
				"applicationAuth": op.Rights,
			})
		}
		if len(secReqs) == 0 {
			secReqs = append(secReqs, map[string]interface{}{
				"oauth2": op.Rights,
			})
		}
		operation["security"] = secReqs
	}
	if op.UserAuth {
		operation["x-auth-type"] = "user"
	} else if op.ApplicationAuth {
		operation["x-auth-type"] = "application"
	}
	if len(op.UserLevels) > 0 {
		levels := make([]interface{}, len(op.UserLevels))
		for i, l := range op.UserLevels {
			levels[i] = l
		}
		operation["x-sailpoint-userLevels"] = levels
	}

	return operation
}

// injectExamplesIntoResponse adds operation examples to the first success response's
// media type object. Targets the 2xx response content.
func injectExamplesIntoResponse(responses map[string]interface{}, op *goextract.OperationInfo) {
	// Find the first 2xx response
	var targetResp map[string]interface{}
	for code, resp := range responses {
		if len(code) == 3 && code[0] == '2' {
			if r, ok := resp.(map[string]interface{}); ok {
				targetResp = r
				break
			}
		}
	}
	if targetResp == nil {
		return
	}

	content, ok := targetResp["content"].(map[string]interface{})
	if !ok {
		return
	}

	// Find the first media type entry
	for _, mediaObj := range content {
		mt, ok := mediaObj.(map[string]interface{})
		if !ok {
			continue
		}
		examples := make(map[string]interface{})
		for i, ex := range op.Examples {
			key := ex.Summary
			if key == "" {
				key = fmt.Sprintf("example%d", i)
			}
			exObj := map[string]interface{}{}
			if ex.Summary != "" {
				exObj["summary"] = ex.Summary
			}
			if ex.Description != "" {
				exObj["description"] = ex.Description
			}
			if ex.Value != nil {
				exObj["value"] = ex.Value
			}
			if ex.ExternalValue != "" {
				exObj["externalValue"] = ex.ExternalValue
			}
			examples[key] = exObj
		}
		mt["examples"] = examples
		break
	}
}

func buildOperationResponseSchema(typeStr string, schemaNameNormalizer *goextract.SchemaNameNormalizer) map[string]interface{} {
	t := strings.TrimSpace(typeStr)
	if t == "" {
		return map[string]interface{}{"type": "object"}
	}

	if sch := goTypeToOpenAPIPrimitiveSchema(t); sch != nil {
		return sch
	}

	if strings.HasPrefix(t, "[]") {
		elem := strings.TrimSpace(strings.TrimPrefix(t, "[]"))
		items := goTypeToOpenAPIPrimitiveSchema(elem)
		if items == nil {
			normalized := schemaNameNormalizer.NormalizeSchemaName(elem)
			items = map[string]interface{}{"$ref": "#/components/schemas/" + normalized}
		}
		return map[string]interface{}{
			"type":  "array",
			"items": items,
		}
	}

	normalized := schemaNameNormalizer.NormalizeSchemaName(t)
	return map[string]interface{}{"$ref": "#/components/schemas/" + normalized}
}

func goTypeToOpenAPIPrimitiveSchema(goType string) map[string]interface{} {
	switch strings.TrimSpace(goType) {
	case "int", "int32":
		return map[string]interface{}{"type": "integer", "format": "int32"}
	case "int64":
		return map[string]interface{}{"type": "integer", "format": "int64"}
	case "uint", "uint32":
		return map[string]interface{}{"type": "integer", "format": "int32", "minimum": 0}
	case "uint64":
		return map[string]interface{}{"type": "integer", "format": "int64", "minimum": 0}
	case "float32":
		return map[string]interface{}{"type": "number", "format": "float"}
	case "float64":
		return map[string]interface{}{"type": "number", "format": "double"}
	case "bool":
		return map[string]interface{}{"type": "boolean"}
	case "string":
		return map[string]interface{}{"type": "string"}
	default:
		return nil
	}
}

func methodDisallowsRequestBody(method string) bool {
	m := strings.ToUpper(strings.TrimSpace(method))
	return m == "GET" || m == "HEAD"
}

// buildSchema builds an OpenAPI schema from TypeInfo.
func buildSchema(typeInfo *goextract.TypeInfo) map[string]interface{} {
	schema := map[string]interface{}{
		"type": "object",
	}
	sourceloc.Location{File: typeInfo.File, Line: typeInfo.Line}.ApplyTo(schema)

	if typeInfo.Description != "" {
		schema["description"] = typeInfo.Description
	}

	if len(typeInfo.Fields) > 0 {
		properties := make(map[string]interface{})
		required := make([]string, 0)

		for _, field := range typeInfo.Fields {
			fieldSchema := buildFieldSchema(field.Type)

			if field.Description != "" {
				fieldSchema["description"] = field.Description
			}

			if field.Example != "" {
				fieldSchema["example"] = field.Example
			}

			if len(field.Enum) > 0 {
				enumValues := make([]interface{}, len(field.Enum))
				for i, v := range field.Enum {
					enumValues[i] = v
				}
				fieldSchema["enum"] = enumValues
			}

			jsonName := field.JSONName
			if jsonName == "" {
				// M5: Use exact Go field name when no json tag exists.
				// Go's encoding/json uses the exact field name by default, not camelCase.
				jsonName = field.Name
			}

			applyValidationTags(field.Tags, fieldSchema, &required, jsonName)

			properties[jsonName] = fieldSchema

			if field.Required {
				required = append(required, jsonName)
			}
		}

		schema["properties"] = properties
		if len(required) > 0 {
			schema["required"] = required
		}
	}

	return schema
}

func applyValidationTags(tags map[string]string, fieldSchema map[string]interface{}, required *[]string, jsonName string) {
	if tags == nil {
		return
	}

	validate := tags["validate"]
	binding := tags["binding"]

	if strings.Contains(validate, "required") || strings.Contains(binding, "required") {
		if jsonName != "" {
			*required = append(*required, jsonName)
		}
	}

	if strings.Contains(validate, "email") {
		if fieldSchema["type"] == "string" {
			fieldSchema["format"] = "email"
		}
	}
	if strings.Contains(validate, "uuid") || strings.Contains(validate, "uuid4") {
		if fieldSchema["type"] == "string" {
			fieldSchema["format"] = "uuid"
		}
	}
	if strings.Contains(validate, "url") || strings.Contains(validate, "uri") {
		if fieldSchema["type"] == "string" {
			fieldSchema["format"] = "uri"
		}
	}
	if strings.Contains(validate, "datetime") {
		if fieldSchema["type"] == "string" {
			fieldSchema["format"] = "date-time"
		}
	}

	if idx := strings.Index(validate, "oneof="); idx >= 0 {
		rest := validate[idx+len("oneof="):]
		if c := strings.Index(rest, ","); c >= 0 {
			rest = rest[:c]
		}
		parts := strings.Fields(rest)
		if len(parts) > 0 {
			en := make([]interface{}, 0, len(parts))
			for _, p := range parts {
				en = append(en, p)
			}
			fieldSchema["enum"] = en
		}
	}

	applyNumericOrLengthConstraint(validate, "min=", "minimum", "minLength", fieldSchema)
	applyNumericOrLengthConstraint(validate, "max=", "maximum", "maxLength", fieldSchema)
	applyNumericOrLengthConstraint(validate, "gte=", "minimum", "minLength", fieldSchema)
	applyNumericOrLengthConstraint(validate, "lte=", "maximum", "maxLength", fieldSchema)
}

func applyNumericOrLengthConstraint(validate string, key string, numKey string, strKey string, fieldSchema map[string]interface{}) {
	idx := strings.Index(validate, key)
	if idx < 0 {
		return
	}
	rest := validate[idx+len(key):]
	if c := strings.Index(rest, ","); c >= 0 {
		rest = rest[:c]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return
	}

	if fieldSchema["type"] == "string" {
		if n, err := strconv.Atoi(rest); err == nil {
			fieldSchema[strKey] = n
		}
		return
	}
	if fieldSchema["type"] == "integer" {
		if n, err := strconv.ParseInt(rest, 10, 64); err == nil {
			fieldSchema[numKey] = n
		}
		return
	}
	if fieldSchema["type"] == "number" {
		if n, err := strconv.ParseFloat(rest, 64); err == nil {
			fieldSchema[numKey] = n
		}
		return
	}
}

func buildFieldSchema(goType string) map[string]interface{} {
	isNullable := strings.HasPrefix(goType, "*")
	goType = strings.TrimPrefix(goType, "*")

	if goType == "interface{}" || goType == "interface {}" || goType == "any" {
		return map[string]interface{}{
			"description": "Any value",
		}
	}

	// Handle uuid and time types directly
	switch goType {
	case "uuid.UUID", "UUID":
		s := map[string]interface{}{"type": "string", "format": "uuid"}
		if isNullable {
			s["nullable"] = true
		}
		return s
	case "time.Time", "Time":
		s := map[string]interface{}{"type": "string", "format": "date-time"}
		if isNullable {
			s["nullable"] = true
		}
		return s
	}

	// M4: Handle anonymous struct types
	if strings.HasPrefix(goType, "struct{") || strings.HasPrefix(goType, "struct {") {
		return buildAnonymousStructSchema(goType)
	}

	if strings.HasPrefix(goType, "[]") {
		elemType := goType[2:]
		return map[string]interface{}{
			"type":  "array",
			"items": buildFieldSchema(elemType),
		}
	}

	if strings.HasPrefix(goType, "map[") {
		bracketCount := 0
		valueStart := -1
		for i, c := range goType {
			if c == '[' {
				bracketCount++
			} else if c == ']' {
				bracketCount--
				if bracketCount == 0 {
					valueStart = i + 1
					break
				}
			}
		}
		if valueStart > 0 && valueStart < len(goType) {
			valueType := goType[valueStart:]
			return map[string]interface{}{
				"type":                 "object",
				"additionalProperties": buildFieldSchema(valueType),
			}
		}
		return map[string]interface{}{
			"type":                 "object",
			"additionalProperties": map[string]interface{}{},
		}
	}

	simpleType := goType
	if idx := strings.LastIndex(goType, "."); idx >= 0 {
		simpleType = goType[idx+1:]
	}

	var result map[string]interface{}
	switch simpleType {
	case "string":
		result = map[string]interface{}{"type": "string"}
	case "int", "int8", "int16", "int32":
		result = map[string]interface{}{"type": "integer", "format": "int32"}
	case "int64":
		result = map[string]interface{}{"type": "integer", "format": "int64"}
	case "uint", "uint8", "uint16", "uint32":
		result = map[string]interface{}{"type": "integer", "format": "int32", "minimum": 0}
	case "uint64":
		result = map[string]interface{}{"type": "integer", "format": "int64", "minimum": 0}
	case "float32":
		result = map[string]interface{}{"type": "number", "format": "float"}
	case "float64":
		result = map[string]interface{}{"type": "number", "format": "double"}
	case "bool":
		result = map[string]interface{}{"type": "boolean"}
	case "byte":
		result = map[string]interface{}{"type": "string", "format": "byte"}
	case "Time":
		result = map[string]interface{}{"type": "string", "format": "date-time"}
	case "Duration":
		result = map[string]interface{}{"type": "string", "description": "Duration in Go format (e.g., '1h30m')"}
	case "json.RawMessage", "RawMessage":
		result = map[string]interface{}{"description": "Raw JSON value"}
	default:
		result = map[string]interface{}{"type": "object"}
	}

	if isNullable && result != nil {
		result["nullable"] = true
	}
	return result
}

func buildTags(metadata *goextract.ExtractedMetadata, openapiVersion string) []interface{} {
	tagNames := make(map[string]bool)
	for _, op := range metadata.Operations {
		for _, tag := range op.Tags {
			tagNames[tag] = true
		}
	}

	if len(tagNames) == 0 {
		return nil
	}

	tags := make([]interface{}, 0, len(tagNames))
	isOpenAPI32 := openapiVersion == "3.2"

	for tagName := range tagNames {
		tagObj := map[string]interface{}{
			"name": tagName,
		}

		if tagInfo := metadata.Tags[tagName]; tagInfo != nil {
			if tagInfo.Description != "" {
				tagObj["description"] = tagInfo.Description
			}

			if isOpenAPI32 {
				if tagInfo.Summary != "" {
					tagObj["summary"] = tagInfo.Summary
				}
				if tagInfo.Parent != "" {
					tagObj["parent"] = tagInfo.Parent
				}
				if tagInfo.Kind != "" {
					tagObj["kind"] = tagInfo.Kind
				}
			}

			if tagInfo.ExternalDocs != nil && tagInfo.ExternalDocs.URL != "" {
				externalDocs := map[string]interface{}{
					"url": tagInfo.ExternalDocs.URL,
				}
				if tagInfo.ExternalDocs.Description != "" {
					externalDocs["description"] = tagInfo.ExternalDocs.Description
				}
				tagObj["externalDocs"] = externalDocs
			}
		}

		tags = append(tags, tagObj)
	}

	return tags
}

func buildWebhooks(metadata *goextract.ExtractedMetadata, config Config, schemaNameNormalizer *goextract.SchemaNameNormalizer) map[string]interface{} {
	webhooks := make(map[string]interface{})

	var filterRegex *regexp.Regexp
	if config.WebhookFilter != "" {
		filterRegex = regexp.MustCompile(config.WebhookFilter)
	}

	for name, webhook := range metadata.Webhooks {
		if filterRegex != nil && !filterRegex.MatchString(name) {
			continue
		}

		if webhook.Direction == "consume" {
			continue
		}

		operation := map[string]interface{}{
			"operationId": "on" + strings.ToUpper(name[:1]) + name[1:],
		}

		if webhook.Summary != "" {
			operation["summary"] = webhook.Summary
		}
		if webhook.Description != "" {
			operation["description"] = webhook.Description
		}
		if len(webhook.Tags) > 0 {
			operation["tags"] = webhook.Tags
		}

		if webhook.PayloadType != "" {
			normalizedType := schemaNameNormalizer.NormalizeSchemaName(webhook.PayloadType)
			operation["requestBody"] = map[string]interface{}{
				"required": true,
				"content": map[string]interface{}{
					"application/json": map[string]interface{}{
						"schema": map[string]interface{}{
							"$ref": "#/components/schemas/" + normalizedType,
						},
					},
				},
			}
		}

		operation["responses"] = map[string]interface{}{
			"200": map[string]interface{}{
				"description": "Event processed successfully",
			},
		}

		if webhook.Topic != "" {
			operation["x-webhook-topic"] = webhook.Topic
		}
		if webhook.EventType != "" {
			operation["x-webhook-event-type"] = webhook.EventType
		}

		webhooks[name] = map[string]interface{}{
			"post": operation,
		}
	}

	return webhooks
}

func goTypeToOpenAPISchema(goType string) map[string]interface{} {
	switch goType {
	case "int", "int32":
		return map[string]interface{}{"type": "integer", "format": "int32"}
	case "int64":
		return map[string]interface{}{"type": "integer", "format": "int64"}
	case "uint", "uint32":
		return map[string]interface{}{"type": "integer", "format": "int32", "minimum": 0}
	case "uint64":
		return map[string]interface{}{"type": "integer", "format": "int64", "minimum": 0}
	case "float32":
		return map[string]interface{}{"type": "number", "format": "float"}
	case "float64":
		return map[string]interface{}{"type": "number", "format": "double"}
	case "bool":
		return map[string]interface{}{"type": "boolean"}
	case "string":
		return map[string]interface{}{"type": "string"}
	case "uuid.UUID", "UUID":
		return map[string]interface{}{"type": "string", "format": "uuid"}
	case "time.Time", "Time":
		return map[string]interface{}{"type": "string", "format": "date-time"}
	case "net/mail.Address", "mail.Address":
		return map[string]interface{}{"type": "string", "format": "email"}
	case "net/url.URL", "url.URL":
		return map[string]interface{}{"type": "string", "format": "uri"}
	default:
		if strings.HasPrefix(goType, "[]") {
			itemSchema := goTypeToOpenAPISchema(strings.TrimPrefix(goType, "[]"))
			return map[string]interface{}{"type": "array", "items": itemSchema}
		}
		if strings.HasPrefix(goType, "*") {
			base := goTypeToOpenAPISchema(strings.TrimPrefix(goType, "*"))
			base["nullable"] = true
			return base
		}
		return map[string]interface{}{"type": "string"}
	}
}

// applyValidateTag enriches a schema map with constraints from Go validate struct tags.
// Supports: uuid, email, url, min, max, oneof, required, len.
func applyValidateTag(schema map[string]interface{}, validateTag string) {
	if validateTag == "" {
		return
	}
	parts := strings.Split(validateTag, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		switch {
		case part == "uuid" || part == "uuid4":
			schema["format"] = "uuid"
		case part == "email":
			schema["format"] = "email"
		case part == "url" || part == "uri":
			schema["format"] = "uri"
		case part == "datetime":
			schema["format"] = "date-time"
		case part == "required":
			// handled at struct level
		case strings.HasPrefix(part, "min="):
			val := strings.TrimPrefix(part, "min=")
			if n, err := strconv.Atoi(val); err == nil {
				if schema["type"] == "string" {
					schema["minLength"] = n
				} else {
					schema["minimum"] = n
				}
			}
		case strings.HasPrefix(part, "max="):
			val := strings.TrimPrefix(part, "max=")
			if n, err := strconv.Atoi(val); err == nil {
				if schema["type"] == "string" {
					schema["maxLength"] = n
				} else {
					schema["maximum"] = n
				}
			}
		case strings.HasPrefix(part, "len="):
			val := strings.TrimPrefix(part, "len=")
			if n, err := strconv.Atoi(val); err == nil {
				schema["minLength"] = n
				schema["maxLength"] = n
			}
		case strings.HasPrefix(part, "oneof="):
			valStr := strings.TrimPrefix(part, "oneof=")
			values := strings.Fields(valStr)
			enumVals := make([]interface{}, len(values))
			for i, v := range values {
				enumVals[i] = v
			}
			schema["enum"] = enumVals
		}
	}
}

func getHttpStatusText(code int) string {
	statusTexts := map[int]string{
		200: "OK", 201: "Created", 202: "Accepted", 204: "No Content",
		400: "Bad Request", 401: "Unauthorized", 403: "Forbidden",
		404: "Not Found", 405: "Method Not Allowed", 406: "Not Acceptable",
		408: "Request Timeout", 409: "Conflict", 410: "Gone",
		411: "Length Required", 412: "Precondition Failed",
		413: "Request Entity Too Large", 415: "Unsupported Media Type",
		422: "Unprocessable Entity", 429: "Too Many Requests",
		499: "Client Closed Request",
		500: "Internal Server Error", 501: "Not Implemented",
		502: "Bad Gateway", 503: "Service Unavailable", 504: "Gateway Timeout",
	}

	if text, ok := statusTexts[code]; ok {
		return text
	}
	return fmt.Sprintf("Status %d", code)
}

func getErrorSchemaName(code int) string {
	statusText := getHttpStatusText(code)
	return strings.ReplaceAll(statusText, " ", "")
}

func getStatusDescription(code int) string {
	descriptions := map[int]string{
		200: "OK", 201: "Created", 204: "No Content",
		400: "Bad Request", 401: "Unauthorized", 403: "Forbidden",
		404: "Not Found", 410: "Gone",
		500: "Internal Server Error", 503: "Service Unavailable",
	}

	if desc, ok := descriptions[code]; ok {
		return desc
	}
	return fmt.Sprintf("Status %d", code)
}

// buildAnonymousStructSchema parses a Go anonymous struct type string and builds
// an OpenAPI schema with proper properties.
// Input format: struct{Field1 Type1 "json:\"name1\""; Field2 Type2}
func buildAnonymousStructSchema(structType string) map[string]interface{} {
	schema := map[string]interface{}{
		"type": "object",
	}

	// Strip "struct{" prefix and "}" suffix
	inner := structType
	if strings.HasPrefix(inner, "struct{") {
		inner = inner[7:]
	} else if strings.HasPrefix(inner, "struct {") {
		inner = inner[8:]
	}
	inner = strings.TrimSuffix(inner, "}")
	inner = strings.TrimSpace(inner)

	if inner == "" {
		return schema
	}

	// Split on ";" to get individual fields
	fieldStrs := strings.Split(inner, ";")
	properties := make(map[string]interface{})

	for _, fieldStr := range fieldStrs {
		fieldStr = strings.TrimSpace(fieldStr)
		if fieldStr == "" {
			continue
		}

		// Parse field: "Name Type" or "Name Type \"json:\\\"jsonName\\\"\""
		fieldName, fieldType, jsonName := parseAnonymousStructField(fieldStr)
		if fieldName == "" {
			continue
		}

		fieldSchema := buildFieldSchema(fieldType)

		propName := jsonName
		if propName == "" {
			propName = fieldName // Use Go field name directly (M5)
		}

		properties[propName] = fieldSchema
	}

	if len(properties) > 0 {
		schema["properties"] = properties
	}

	return schema
}

// parseAnonymousStructField parses a single field from an anonymous struct type string.
// Returns fieldName, fieldType, jsonName.
func parseAnonymousStructField(field string) (string, string, string) {
	// Find the JSON tag if present
	jsonName := ""
	tagStart := strings.Index(field, "\"")
	mainPart := field
	if tagStart >= 0 {
		mainPart = strings.TrimSpace(field[:tagStart])
		tagPart := field[tagStart:]
		// Extract json tag value from "json:\"name\""
		if idx := strings.Index(tagPart, "json:"); idx >= 0 {
			rest := tagPart[idx+5:]
			// Remove backslash-escaped quotes
			rest = strings.ReplaceAll(rest, "\\\"", "\"")
			rest = strings.Trim(rest, "\"` ")
			// Get the name (before comma if present)
			if commaIdx := strings.Index(rest, ","); commaIdx >= 0 {
				rest = rest[:commaIdx]
			}
			if rest != "" && rest != "-" {
				jsonName = rest
			}
		}
	}

	// Split mainPart into name and type
	parts := strings.Fields(mainPart)
	if len(parts) < 2 {
		return "", "", ""
	}

	return parts[0], strings.Join(parts[1:], " "), jsonName
}

// isInfrastructurePath checks if a path is a common infrastructure/health endpoint
// that should be excluded from the API spec (L3).
func isInfrastructurePath(path string) bool {
	infraPaths := map[string]bool{
		"/":             true,
		"/health":       true,
		"/health/live":  true,
		"/health/ready": true,
		"/healthz":      true,
		"/readyz":       true,
		"/livez":        true,
		"/metrics":      true,
		"/ready":        true,
		"/live":         true,
		"/ping":         true,
		"/version":      true,
		"/debug/pprof":  true,
	}
	return infraPaths[path]
}
