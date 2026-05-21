// Package sharedspec provides unified OpenAPI spec generation for Java and TypeScript
// extractors. Language-specific adapters convert extraction results into the
// specmodel.Result, then this package generates the OpenAPI spec.
package sharedspec

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/generics"
	"github.com/sailpoint-oss/cartographer/extract/sourceloc"
	"github.com/sailpoint-oss/cartographer/extract/specmodel"
)

// paramLocationOrder defines sort priority for parameter locations.
var paramLocationOrder = map[string]int{
	"path":   0,
	"query":  1,
	"header": 2,
	"cookie": 3,
}

func normalizeOpenAPIPath(path string) string {
	if path == "" {
		return ""
	}

	// Strip quote characters that leak from annotation values (e.g. @RequestMapping)
	path = strings.ReplaceAll(path, "\"", "")

	// Fix paths starting with /{" or {"  from corrupted annotations
	if strings.HasPrefix(path, "/{") && !strings.Contains(path[:min(4, len(path))], "}") {
		path = "/" + strings.TrimPrefix(path, "/{")
	}

	if !strings.HasPrefix(path, "/") {
		return "/" + path
	}
	return path
}

// GenerateSpec converts unified extraction results into a complete OpenAPI spec.
func GenerateSpec(result *specmodel.Result, cfg specmodel.SpecConfig, adapter LanguageAdapter) map[string]any {
	openAPIVersion := "3.0.3"
	switch cfg.OpenAPIVersion {
	case "3.1":
		openAPIVersion = "3.1.0"
	case "3.2":
		openAPIVersion = "3.2.0"
	}

	spec := map[string]any{
		"openapi": openAPIVersion,
		"info": map[string]any{
			"title":   cfg.Title,
			"version": cfg.Version,
		},
		"paths":      buildPaths(result, cfg, adapter),
		"components": buildComponents(result, cfg, adapter),
	}

	if cfg.Description != "" {
		spec["info"].(map[string]any)["description"] = cfg.Description
	}

	if cfg.ServiceTemplate != "" {
		spec["info"].(map[string]any)["x-service-template"] = cfg.ServiceTemplate
	}

	if len(result.Diagnostics) > 0 {
		spec["info"].(map[string]any)["x-cartographer-diagnostics"] = result.Diagnostics
	}

	if tags := buildTopLevelTags(result); len(tags) > 0 {
		spec["tags"] = tags
	}

	EnsureSchemaRefsExist(spec)

	if cfg.TreeShake {
		TreeShake(spec)
	}

	return spec
}

// buildTopLevelTags collects unique tag names from operations for the spec root.
func buildTopLevelTags(result *specmodel.Result) []any {
	seen := make(map[string]bool)
	var tags []any
	for _, op := range result.Operations {
		for _, t := range op.Tags {
			if !seen[t] {
				seen[t] = true
				tags = append(tags, map[string]any{
					"name":        t,
					"description": fmt.Sprintf("Operations related to %s", t),
				})
			}
		}
	}
	return tags
}

// buildPaths builds the OpenAPI paths object from extracted operations.
func buildPaths(result *specmodel.Result, cfg specmodel.SpecConfig, adapter LanguageAdapter) map[string]any {
	paths := make(map[string]any)
	operationIDs := make(map[string]int)

	for _, op := range result.Operations {
		normalizedPath := normalizeOpenAPIPath(op.Path)
		if normalizedPath == "" {
			continue
		}

		// Deduplicate operation IDs (include HTTP method to distinguish same-path operations)
		operationIDs[op.OperationID]++
		if operationIDs[op.OperationID] > 1 {
			suffix := LastPathSegment(normalizedPath)
			method := strings.ToLower(op.Method)
			if suffix != "" {
				op.OperationID = fmt.Sprintf("%s_%s_%s", op.OperationID, method, suffix)
			} else {
				op.OperationID = fmt.Sprintf("%s_%s_%d", op.OperationID, method, operationIDs[op.OperationID])
			}
		}

		pathItem, ok := paths[normalizedPath].(map[string]any)
		if !ok {
			pathItem = make(map[string]any)
			paths[normalizedPath] = pathItem
		}

		operation := buildOperation(op, result, cfg, adapter)
		pathItem[strings.ToLower(op.Method)] = operation
	}

	return paths
}

// buildOperation builds an OpenAPI operation object.
func buildOperation(op *specmodel.Operation, result *specmodel.Result, cfg specmodel.SpecConfig, adapter LanguageAdapter) map[string]any {
	operation := map[string]any{
		"operationId": op.OperationID,
	}

	if op.Summary != "" {
		operation["summary"] = op.Summary
	} else if auto := AutoSummary(op.Method, op.Path); auto != "" {
		operation["summary"] = auto
	} else {
		operation["summary"] = CamelCaseToWords(op.OperationID)
	}

	if op.Description != "" {
		operation["description"] = op.Description
	} else if auto := AutoDescription(op.Method, op.Path, ""); auto != "" {
		operation["description"] = auto
	}

	if len(op.Tags) > 0 {
		tags := make([]any, len(op.Tags))
		for i, t := range op.Tags {
			tags[i] = t
		}
		operation["tags"] = tags
	}

	if op.Deprecated {
		operation["deprecated"] = true
		if op.DeprecatedSince != "" {
			operation["x-deprecated-since"] = op.DeprecatedSince
		}
	}

	// Parameters — sort deterministically by (in, name) for consistent output
	if len(op.Parameters) > 0 {
		sortedParams := make([]*specmodel.Parameter, len(op.Parameters))
		copy(sortedParams, op.Parameters)
		sort.Slice(sortedParams, func(i, j int) bool {
			oi, oj := paramLocationOrder[sortedParams[i].In], paramLocationOrder[sortedParams[j].In]
			if oi != oj {
				return oi < oj
			}
			return sortedParams[i].Name < sortedParams[j].Name
		})
		params := make([]any, 0, len(sortedParams))
		for _, p := range sortedParams {
			pSchema := adapter.ParamTypeToSchema(p.Type)

			// Enum parameter values from type index
			if result != nil && result.Types != nil {
				if decl := adapter.FindTypeBySimpleName(result.Types, p.Type); decl != nil && decl.Kind == "enum" && len(decl.EnumValues) > 0 {
					enums := make([]any, len(decl.EnumValues))
					for i, v := range decl.EnumValues {
						enums[i] = v
					}
					pSchema["enum"] = enums
					pSchema["type"] = "string"
				}
			}

			// Apply parameter validation constraints
			applyParamConstraints(pSchema, p)

			// Type-aware default value propagation
			applyParamDefault(pSchema, p)
			param := buildParameter(p, pSchema)

			// Well-known header enrichment
			if p.In == "header" {
				if info, ok := WellKnownHeaders[p.Name]; ok {
					if _, hasDsc := param["description"]; !hasDsc {
						param["description"] = info.Description
					}
					if _, hasEx := param["example"]; !hasEx && info.Example != "" {
						param["example"] = info.Example
					}
				}
			}

			// Well-known query/path param enrichment
			if p.In == "query" || p.In == "path" {
				if info, ok := WellKnownParamDescriptions[p.Name]; ok {
					if _, hasDsc := param["description"]; !hasDsc {
						param["description"] = info.Description
					}
					if _, hasEx := param["example"]; !hasEx && info.Example != "" {
						param["example"] = info.Example
					}
				}
			}

			params = append(params, param)
		}
		operation["parameters"] = params
	}

	// Request body
	if op.RequestBodyType != "" {
		var schema map[string]any
		if result != nil && result.Schemas != nil {
			if indexed, ok := result.Schemas[op.RequestBodyType]; ok {
				if m, ok2 := indexed.(map[string]any); ok2 {
					schema = m
				}
			}
		}
		if schema == nil {
			schema = generics.Parse(op.RequestBodyType).ToOpenAPISchema(nil)
		}
		if result != nil && result.Types != nil {
			schema = enrichBodySchema(adapter, op.RequestBodyType, schema, result.Types)
		}
		contentType := "application/json"
		if op.ConsumesContentType != "" {
			contentType = op.ConsumesContentType
		}
		if contentType == "application/json-patch+json" {
			schema = map[string]any{"$ref": "#/components/schemas/JsonPatch"}
		}
		// Strip readOnly properties from request body schemas
		schema = stripReadOnlyProps(schema)
		reqBody := map[string]any{
			"required": true,
			"content": map[string]any{
				contentType: map[string]any{
					"schema": schema,
				},
			},
		}
		if op.RequestBodyDescription != "" {
			reqBody["description"] = op.RequestBodyDescription
		}
		operation["requestBody"] = reqBody
	}

	// Form params as request body
	if len(op.FormParams) > 0 && op.RequestBodyType == "" {
		props := make(map[string]any)
		var formRequired []any
		hasFile := false
		for _, fp := range op.FormParams {
			propSchema := adapter.FormParamSchema(fp.Type)
			if adapter.IsFileType(fp.Type) {
				hasFile = true
			}
			applyParamConstraints(propSchema, fp)
			props[fp.Name] = propSchema
			if fp.Required {
				formRequired = append(formRequired, fp.Name)
			}
		}
		formSchema := map[string]any{
			"type":       "object",
			"properties": props,
		}
		if len(formRequired) > 0 {
			formSchema["required"] = formRequired
		}
		contentType := "application/x-www-form-urlencoded"
		if hasFile {
			contentType = "multipart/form-data"
		}
		if op.ConsumesContentType != "" {
			contentType = op.ConsumesContentType
		}
		operation["requestBody"] = map[string]any{
			"required": true,
			"content": map[string]any{
				contentType: map[string]any{
					"schema": formSchema,
				},
			},
		}
	}

	// Responses
	operation["responses"] = buildResponses(op, result, cfg, adapter)

	// Source location
	loc := sourceloc.Location{File: op.File, Line: op.Line, Column: op.Column}
	if !loc.IsZero() {
		loc.ApplyTo(operation)
	}

	// x-rate-limited
	if op.RateLimited {
		operation["x-rate-limited"] = true
	}

	// Security
	if len(op.Security) > 0 {
		var secList []any
		for _, sec := range op.Security {
			scopes := make([]any, len(sec.Scopes))
			for i, s := range sec.Scopes {
				scopes[i] = s
			}
			secList = append(secList, map[string]any{
				sec.Scheme: scopes,
			})
		}
		operation["security"] = secList
	}

	return operation
}

func buildParameter(p *specmodel.Parameter, schema map[string]any) map[string]any {
	param := map[string]any{
		"name":     p.Name,
		"in":       p.In,
		"required": p.Required,
		"schema":   schema,
	}
	if p.Example != "" {
		param["example"] = p.Example
	}
	if p.Description != "" {
		param["description"] = p.Description
	}
	if p.Deprecated {
		param["deprecated"] = true
	}
	// Parameter style/explode defaults per OpenAPI spec
	switch p.In {
	case "query":
		param["style"] = "form"
		param["explode"] = true
	case "path":
		param["style"] = "simple"
	case "header":
		param["style"] = "simple"
	}
	loc := sourceloc.Location{File: p.File, Line: p.Line, Column: p.Column}
	if !loc.IsZero() {
		loc.ApplyTo(param)
	}
	return param
}

// buildResponses constructs the responses object for an operation.
func buildResponses(op *specmodel.Operation, result *specmodel.Result, cfg specmodel.SpecConfig, adapter LanguageAdapter) map[string]any {
	responses := make(map[string]any)

	// Success response
	status := op.ResponseStatus
	if status == 0 {
		status = 200
	}
	statusStr := fmt.Sprintf("%d", status)

	successDesc := GetStatusDescription(status)
	if op.ReturnDescription != "" {
		successDesc = op.ReturnDescription
	} else if auto := AutoResponseDesc(op.Method, op.Path, op.ResponseType); auto != "" {
		successDesc = auto
	}

	successResp := map[string]any{
		"description": successDesc,
	}

	if op.ResponseType != "" && op.ResponseType != "void" && op.ResponseType != "Void" {
		parsed := generics.Parse(op.ResponseType).UnwrapWrappers()
		responseStr := parsed.String()

		if responseStr != "" && responseStr != "Void" && responseStr != "Response" && responseStr != "void" {
			var schema map[string]any
			if result != nil && result.Schemas != nil {
				if indexed, ok := result.Schemas[responseStr]; ok {
					if m, ok2 := indexed.(map[string]any); ok2 {
						schema = m
					}
				}
			}
			if schema == nil {
				schema = parsed.ToOpenAPISchema(nil)
			}
			// Strip writeOnly properties from response schemas
			schema = stripWriteOnlyProps(schema)
			if op.NullableResponse {
				if _, hasRef := schema["$ref"]; hasRef {
					schema = map[string]any{
						"allOf":    []any{map[string]any{"$ref": schema["$ref"]}},
						"nullable": true,
					}
				} else {
					schema["nullable"] = true
				}
			}
			contentType := "application/json"
			if op.ProducesContentType != "" {
				contentType = op.ProducesContentType
			}
			successResp["content"] = map[string]any{
				contentType: map[string]any{
					"schema": schema,
				},
			}
		}
	}

	responses[statusStr] = successResp

	// Priority 1: Annotated responses override defaults
	for _, ar := range op.AnnotatedResponses {
		code := fmt.Sprintf("%d", ar.StatusCode)
		resp := map[string]any{
			"description": ar.Description,
		}
		if ar.SchemaType != "" {
			// Prefer pre-computed schemas (WITH validation annotations from index resolver)
			var arSchema map[string]any
			if result != nil && result.Schemas != nil {
				if indexed, ok := result.Schemas[ar.SchemaType]; ok {
					if m, ok2 := indexed.(map[string]any); ok2 {
						arSchema = m
					}
				}
			}
			if arSchema == nil {
				arSchema = generics.Parse(ar.SchemaType).ToOpenAPISchema(nil)
			}
			resp["content"] = map[string]any{
				"application/json": map[string]any{
					"schema": arSchema,
				},
			}
		}
		responses[code] = resp
	}

	// Priority 2: Error responses from exception analysis (description only unless typed below)
	for code, desc := range op.ErrorResponses {
		codeStr := fmt.Sprintf("%d", code)
		if _, exists := responses[codeStr]; !exists {
			responses[codeStr] = map[string]any{
				"description": desc,
			}
		}
	}

	// Priority 2b: Typed error bodies from @ControllerAdvice / handler return types
	for _, ers := range op.ErrorResponseSchemas {
		codeStr := fmt.Sprintf("%d", ers.StatusCode)
		resp := map[string]any{
			"description": ers.Description,
		}
		if ers.SchemaType != "" {
			var errSchema map[string]any
			if result != nil && result.Schemas != nil {
				if indexed, ok := result.Schemas[ers.SchemaType]; ok {
					if m, ok2 := indexed.(map[string]any); ok2 {
						errSchema = m
					}
				}
			}
			if errSchema == nil {
				errSchema = generics.Parse(ers.SchemaType).ToOpenAPISchema(nil)
			}
			contentType := "application/json"
			if cfg.ErrorSchema == "problem-details" || strings.Contains(strings.ToLower(ers.SchemaType), "problemdetail") {
				contentType = "application/problem+json"
			}
			resp["content"] = map[string]any{
				contentType: map[string]any{
					"schema": errSchema,
				},
			}
		}
		responses[codeStr] = resp
	}

	// Legacy opt-in default errors (only when explicitly configured).
	if cfg.ErrorSchema == "legacy-error-response" {
		defaultErrors := map[string]string{
			"400": "Bad Request - Invalid input parameters",
			"401": "Unauthorized - Authentication required",
			"403": "Forbidden - Insufficient permissions",
			"500": "Internal Server Error",
		}
		hasPathParam := false
		for _, p := range op.Parameters {
			if p.In == "path" {
				hasPathParam = true
				break
			}
		}
		method := strings.ToUpper(op.Method)
		if hasPathParam && (method == "GET" || method == "PUT" || method == "PATCH" || method == "DELETE") {
			defaultErrors["404"] = "Not Found - Resource does not exist"
		}
		if method == "POST" || method == "PUT" {
			defaultErrors["409"] = "Conflict - Resource already exists or state conflict"
		}
		for code, desc := range defaultErrors {
			if _, exists := responses[code]; !exists {
				responses[code] = map[string]any{
					"description": desc,
					"content": map[string]any{
						"application/json": map[string]any{
							"schema": ErrorResponseRef(),
						},
					},
				}
			}
		}
	}

	// Content-Disposition header for download endpoints
	if op.ProducesContentType != "" && DownloadContentTypes[op.ProducesContentType] {
		if sr, ok := responses[statusStr].(map[string]any); ok {
			sr["headers"] = map[string]any{
				"Content-Disposition": map[string]any{
					"schema":      HeaderTypeSchema("Content-Disposition"),
					"description": "Attachment filename for downloaded content",
				},
			}
		}
	}

	// Response headers with typed schemas
	if len(op.ResponseHeaders) > 0 {
		if sr, ok := responses[statusStr].(map[string]any); ok {
			headers, _ := sr["headers"].(map[string]any)
			if headers == nil {
				headers = make(map[string]any)
			}
			for headerName, headerDesc := range op.ResponseHeaders {
				headers[headerName] = map[string]any{
					"schema":      HeaderTypeSchema(headerName),
					"description": headerDesc,
				}
			}
			sr["headers"] = headers
		}
	}

	return responses
}

// buildComponents builds the OpenAPI components object.
// BUG FIX: Prefers pre-computed schemas (from index resolver, WITH annotations)
// over rebuilding from TypeDecl (which loses field constraints).
func buildComponents(result *specmodel.Result, cfg specmodel.SpecConfig, adapter LanguageAdapter) map[string]any {
	schemas := make(map[string]any)

	referenced := collectReferencedTypes(result, adapter)

	for name := range referenced {
		// PREFER pre-computed schemas (WITH validation annotations from the index resolver)
		if schema, ok := result.Schemas[name]; ok {
			schemas[name] = schema
		} else if _, ok := result.Types[name]; ok {
			schemas[name] = GenerateStubSchema(name)
		} else if baseSchema := lookupBaseTypeName(name, result.Schemas); baseSchema != nil {
			// Normalized generic name (e.g. "AttributesStringObject") may not
			// match the schema key (e.g. "Attributes"). Try base type lookup.
			schemas[name] = baseSchema
		} else {
			schemas[name] = GenerateStubSchema(name)
		}
	}

	// $ref isolation: per OpenAPI 3.0.x, $ref must be alone in a schema object.
	// Wrap $ref + sibling keywords in allOf to preserve both the reference and
	// the additional constraints (nullable, description, etc.).
	for name, raw := range schemas {
		if m, ok := raw.(map[string]any); ok {
			if ref, hasRef := m["$ref"]; hasRef && len(m) > 1 {
				siblings := make(map[string]any)
				for k, v := range m {
					if k != "$ref" {
						siblings[k] = v
					}
				}
				wrapped := map[string]any{
					"allOf": []any{map[string]any{"$ref": ref}},
				}
				for k, v := range siblings {
					wrapped[k] = v
				}
				schemas[name] = wrapped
			}
		}
	}

	// Auto-inject format-based examples for schemas that lack one
	for _, raw := range schemas {
		if m, ok := raw.(map[string]any); ok {
			injectFormatExample(m)
		}
	}

	// Single-value enums → const (OAS 3.1+)
	for _, raw := range schemas {
		if m, ok := raw.(map[string]any); ok {
			if enumVals, ok := m["enum"].([]interface{}); ok && len(enumVals) == 1 {
				m["const"] = enumVals[0]
				delete(m, "enum")
			} else if enumVals, ok := m["enum"].([]any); ok && len(enumVals) == 1 {
				m["const"] = enumVals[0]
				delete(m, "enum")
			}
		}
	}

	if cfg.ErrorSchema == "legacy-error-response" {
		schemas["ErrorResponse"] = ErrorResponseSchema()
	}
	if cfg.ErrorSchema == "problem-details" {
		schemas["ProblemDetails"] = ProblemDetailsSchema()
	}

	components := map[string]any{
		"schemas": schemas,
	}

	securitySchemes := adapter.BuildSecuritySchemes(result)
	if len(securitySchemes) > 0 {
		components["securitySchemes"] = securitySchemes
	}

	return components
}

// collectReferencedTypes gathers all type names referenced by operations,
// then walks pre-computed schemas transitively to capture nested $ref targets.
func collectReferencedTypes(result *specmodel.Result, adapter LanguageAdapter) map[string]bool {
	refs := make(map[string]bool)

	for _, op := range result.Operations {
		if op.RequestBodyType != "" {
			generics.Parse(op.RequestBodyType).CollectTypeRefs(refs)
		}
		if op.ConsumesContentType == "application/json-patch+json" {
			refs["JsonPatch"] = true
		}
		if op.ResponseType != "" && op.ResponseType != "void" && op.ResponseType != "Void" {
			parsed := generics.Parse(op.ResponseType).UnwrapWrappers()
			s := parsed.String()
			if s != "Response" && s != "" && s != "void" && s != "Void" {
				parsed.CollectTypeRefs(refs)
			}
		}
		for _, p := range op.Parameters {
			if !adapter.IsSimpleType(p.Type) {
				generics.Parse(p.Type).CollectTypeRefs(refs)
			}
		}
		for _, ar := range op.AnnotatedResponses {
			if ar.SchemaType != "" {
				generics.Parse(ar.SchemaType).CollectTypeRefs(refs)
			}
		}
	}

	// Walk pre-computed schemas transitively: schemas may contain $ref to other
	// schemas that aren't directly referenced by operations.
	const prefix = "#/components/schemas/"
	changed := true
	for changed {
		changed = false
		for name := range refs {
			schema, ok := result.Schemas[name]
			if !ok {
				continue
			}
			innerRefs := make(map[string]bool)
			CollectRefs(schema, innerRefs)
			for ref := range innerRefs {
				if !strings.HasPrefix(ref, prefix) {
					continue
				}
				target := strings.TrimPrefix(ref, prefix)
				if target != "" && !refs[target] {
					refs[target] = true
					changed = true
				}
			}
		}
	}

	return refs
}

// applyParamConstraints applies validation constraints from the parameter to the schema.
func applyParamConstraints(schema map[string]any, p *specmodel.Parameter) {
	if p.Format != "" {
		schema["format"] = p.Format
	}
	if p.Pattern != "" {
		schema["pattern"] = p.Pattern
	}
	if p.Minimum != nil {
		schema["minimum"] = *p.Minimum
	}
	if p.Maximum != nil {
		schema["maximum"] = *p.Maximum
	}
	if p.MinLength != nil {
		schema["minLength"] = *p.MinLength
	}
	if p.MaxLength != nil {
		schema["maxLength"] = *p.MaxLength
	}
	if p.MinItems != nil {
		schema["minItems"] = *p.MinItems
	}
	if p.MaxItems != nil {
		schema["maxItems"] = *p.MaxItems
	}
	if len(p.Enum) > 0 {
		enums := make([]any, len(p.Enum))
		for i, e := range p.Enum {
			enums[i] = e
		}
		schema["enum"] = enums
	}
}

// applyParamDefault applies a type-aware default value to the schema.
func applyParamDefault(schema map[string]any, p *specmodel.Parameter) {
	if p.DefaultValue == "" {
		return
	}

	// Filter out Java constructor expressions that leak through annotation parsing
	if isJavaConstructorDefault(p.DefaultValue) {
		return
	}

	switch schema["type"] {
	case "integer", "number":
		if n, err := strconv.Atoi(p.DefaultValue); err == nil {
			schema["default"] = n
		} else if f, err := strconv.ParseFloat(p.DefaultValue, 64); err == nil {
			schema["default"] = f
		} else {
			schema["default"] = p.DefaultValue
		}
	case "boolean":
		schema["default"] = p.DefaultValue == "true"
	default:
		schema["default"] = p.DefaultValue
	}
}

// isJavaConstructorDefault returns true if the value looks like a Java constructor
// expression rather than a real default value (e.g. "new HashSet<>()", "new ArrayList<>()").
func isJavaConstructorDefault(val string) bool {
	return strings.HasPrefix(val, "new ") ||
		strings.Contains(val, "()") ||
		strings.Contains(val, "<>")
}

// stripReadOnlyProps returns a shallow copy of the schema with readOnly
// properties removed from its "properties" map and "required" list.
// This is used for request body schemas where readOnly fields shouldn't appear.
func stripReadOnlyProps(schema map[string]any) map[string]any {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return schema
	}
	var removed []string
	for name, raw := range props {
		if p, ok := raw.(map[string]any); ok {
			if p["readOnly"] == true {
				removed = append(removed, name)
			}
		}
	}
	if len(removed) == 0 {
		return schema
	}
	// Shallow copy to avoid mutating original
	out := make(map[string]any, len(schema))
	for k, v := range schema {
		out[k] = v
	}
	newProps := make(map[string]any, len(props))
	for k, v := range props {
		newProps[k] = v
	}
	removedSet := make(map[string]bool, len(removed))
	for _, r := range removed {
		delete(newProps, r)
		removedSet[r] = true
	}
	out["properties"] = newProps
	// Filter required list
	if req, ok := out["required"].([]any); ok {
		var filtered []any
		for _, r := range req {
			if s, ok := r.(string); ok && !removedSet[s] {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) > 0 {
			out["required"] = filtered
		} else {
			delete(out, "required")
		}
	}
	return out
}

// formatExamples maps OpenAPI format values to example values.
var formatExamples = map[string]string{
	"uuid":      "3fa85f64-5717-4562-b3fc-2c963f66afa6",
	"email":     "user@example.com",
	"uri":       "https://example.com",
	"date":      "2024-01-15",
	"date-time": "2024-01-15T09:30:00Z",
	"ipv4":      "192.168.1.1",
	"ipv6":      "::1",
	"hostname":  "example.com",
}

// injectFormatExample adds an example to a schema based on its format field,
// if the schema doesn't already have an example.
func injectFormatExample(schema map[string]any) {
	if _, has := schema["example"]; has {
		return
	}
	if _, has := schema["$ref"]; has {
		return
	}
	format, _ := schema["format"].(string)
	if format == "" {
		return
	}
	if ex, ok := formatExamples[format]; ok {
		schema["example"] = ex
	}
}

// ValidateSchemaRefs checks that all $ref targets in the spec point to
// existing component schemas. Returns a list of missing ref target names.
func ValidateSchemaRefs(spec map[string]any) []string {
	components, _ := spec["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)

	refs := make(map[string]bool)
	CollectRefs(spec, refs)

	var missing []string
	const prefix = "#/components/schemas/"
	for ref := range refs {
		if !strings.HasPrefix(ref, prefix) {
			continue
		}
		name := strings.TrimPrefix(ref, prefix)
		if _, exists := schemas[name]; !exists {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

// stripWriteOnlyProps returns a shallow copy of the schema with writeOnly
// properties removed. Used for response schemas where writeOnly fields shouldn't appear.
func stripWriteOnlyProps(schema map[string]any) map[string]any {
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return schema
	}
	var removed []string
	for name, raw := range props {
		if p, ok := raw.(map[string]any); ok {
			if p["writeOnly"] == true {
				removed = append(removed, name)
			}
		}
	}
	if len(removed) == 0 {
		return schema
	}
	out := make(map[string]any, len(schema))
	for k, v := range schema {
		out[k] = v
	}
	newProps := make(map[string]any, len(props))
	for k, v := range props {
		newProps[k] = v
	}
	removedSet := make(map[string]bool, len(removed))
	for _, r := range removed {
		delete(newProps, r)
		removedSet[r] = true
	}
	out["properties"] = newProps
	if req, ok := out["required"].([]any); ok {
		var filtered []any
		for _, r := range req {
			if s, ok := r.(string); ok && !removedSet[s] {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) > 0 {
			out["required"] = filtered
		} else {
			delete(out, "required")
		}
	}
	return out
}

// lookupBaseTypeName attempts to find a schema by stripping generic suffixes from
// normalized names. For example, "AttributesStringObject" might match a schema
// stored as "Attributes" if the suffix consists entirely of known primitive names.
func lookupBaseTypeName(normalizedName string, schemas map[string]any) map[string]any {
	knownSuffixes := []string{
		"String", "Object", "Integer", "Long", "Boolean",
		"Double", "Float", "Short", "Byte",
	}
	candidate := normalizedName
	for {
		found := false
		for _, suffix := range knownSuffixes {
			if strings.HasSuffix(candidate, suffix) && len(candidate) > len(suffix) {
				candidate = candidate[:len(candidate)-len(suffix)]
				found = true
				break
			}
		}
		if !found {
			break
		}
		if schema, ok := schemas[candidate]; ok {
			if m, mOK := schema.(map[string]any); mOK {
				return m
			}
		}
	}
	return nil
}
