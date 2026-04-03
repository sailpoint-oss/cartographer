package index

import (
	"strconv"
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/sourceloc"
)

// ToOpenAPISchema converts a TypeDecl to an OpenAPI schema object.
func (idx *Index) ToOpenAPISchema(decl *TypeDecl, visited map[string]bool) map[string]interface{} {
	if decl == nil {
		return map[string]interface{}{"type": "object"}
	}

	// Circular reference guard
	if visited == nil {
		visited = make(map[string]bool)
	}
	if visited[decl.Qualified] {
		return map[string]interface{}{"$ref": "#/components/schemas/" + decl.Name}
	}
	visited[decl.Qualified] = true

	switch decl.Kind {
	case "enum":
		return idx.enumToSchema(decl)
	default:
		return idx.classToSchema(decl, visited)
	}
}

func (idx *Index) enumToSchema(decl *TypeDecl) map[string]interface{} {
	schema := map[string]interface{}{
		"type": "string",
	}
	if len(decl.EnumValues) > 0 {
		values := make([]interface{}, len(decl.EnumValues))
		for i, v := range decl.EnumValues {
			values[i] = v
		}
		schema["enum"] = values
	}
	sourceloc.Location{File: decl.SourceFile, Line: decl.Line, Column: decl.Column}.ApplyTo(schema)
	return schema
}

func (idx *Index) classToSchema(decl *TypeDecl, visited map[string]bool) map[string]interface{} {
	// Improvement #4: Discriminator support for @JsonTypeInfo/@JsonSubTypes
	if decl.DiscriminatorProperty != "" && len(decl.DiscriminatorMapping) > 0 {
		schema := map[string]interface{}{}
		if decl.Description != "" {
			schema["description"] = decl.Description
		}
		discriminator := map[string]interface{}{
			"propertyName": decl.DiscriminatorProperty,
		}
		mapping := make(map[string]interface{})
		var oneOf []interface{}
		for discValue, typeName := range decl.DiscriminatorMapping {
			ref := "#/components/schemas/" + typeName
			mapping[discValue] = ref
			oneOf = append(oneOf, map[string]interface{}{"$ref": ref})
		}
		discriminator["mapping"] = mapping
		schema["discriminator"] = discriminator
		schema["oneOf"] = oneOf
		sourceloc.Location{File: decl.SourceFile, Line: decl.Line, Column: decl.Column}.ApplyTo(schema)
		return schema
	}

	schema := map[string]interface{}{
		"type": "object",
	}

	if decl.Description != "" {
		schema["description"] = decl.Description
	}

	// v5 #10: @Deprecated on class
	if decl.Deprecated {
		schema["deprecated"] = true
	}

	// Inheritance: if there's a superclass, use allOf
	if decl.SuperClass != "" {
		superName := decl.SuperClass
		if openIdx := strings.Index(superName, "<"); openIdx >= 0 {
			superName = superName[:openIdx]
		}
		ownSchema := idx.buildOwnFieldsSchema(decl, visited)
		schema = map[string]interface{}{
			"allOf": []interface{}{
				map[string]interface{}{
					"$ref": "#/components/schemas/" + superName,
				},
				ownSchema,
			},
		}
		if decl.Description != "" {
			schema["description"] = decl.Description
		}
		if decl.Deprecated {
			schema["deprecated"] = true
		}
		if len(decl.Interfaces) > 0 {
			ifaces := make([]interface{}, len(decl.Interfaces))
			for i, iface := range decl.Interfaces {
				ifaces[i] = iface
			}
			schema["x-implements"] = ifaces
		}
		sourceloc.Location{File: decl.SourceFile, Line: decl.Line, Column: decl.Column}.ApplyTo(schema)
		return schema
	}

	if len(decl.Fields) == 0 {
		sourceloc.Location{File: decl.SourceFile, Line: decl.Line, Column: decl.Column}.ApplyTo(schema)
		return schema
	}

	properties := make(map[string]interface{})
	var required []string

	for _, field := range decl.Fields {
		if shouldSkipField(field) {
			continue
		}

		// @JsonUnwrapped: flatten nested object fields into parent schema
		if isJsonUnwrapped(field) {
			idx.inlineUnwrappedFields(field.Type, visited, properties, &required)
			continue
		}

		fieldSchema := idx.fieldTypeToSchema(field.Type, visited)

		if field.Description != "" {
			fieldSchema["description"] = field.Description
		}
		if field.DefaultValue != "" {
			fieldSchema["default"] = field.DefaultValue
		}

		// Apply validation annotations
		applyJavaValidation(field.Annotations, fieldSchema)

		// v5 #1: @Nullable on field → nullable: true
		if field.Nullable {
			fieldSchema["nullable"] = true
		}

		// v5 #10: @Deprecated on field
		if field.Deprecated {
			fieldSchema["deprecated"] = true
		}

		jsonName := field.JSONName
		if jsonName == "" {
			jsonName = field.Name
		}

		properties[jsonName] = fieldSchema

		if field.Required {
			required = append(required, jsonName)
		}
	}

	schema["properties"] = properties
	if len(required) > 0 {
		// Filter required to only contain names present in properties
		var filteredRequired []string
		for _, r := range required {
			if _, exists := properties[r]; exists {
				filteredRequired = append(filteredRequired, r)
			}
		}
		if len(filteredRequired) > 0 {
			schema["required"] = filteredRequired
		}
	}

	if len(decl.Interfaces) > 0 {
		ifaces := make([]interface{}, len(decl.Interfaces))
		for i, iface := range decl.Interfaces {
			ifaces[i] = iface
		}
		schema["x-implements"] = ifaces
	}

	sourceloc.Location{File: decl.SourceFile, Line: decl.Line, Column: decl.Column}.ApplyTo(schema)
	return schema
}

// buildOwnFieldsSchema builds a schema for just the type's own fields (without inheritance).
func (idx *Index) buildOwnFieldsSchema(decl *TypeDecl, visited map[string]bool) map[string]interface{} {
	schema := map[string]interface{}{
		"type": "object",
	}

	if len(decl.Fields) == 0 {
		return schema
	}

	properties := make(map[string]interface{})
	var required []string

	// Collect superclass field names to skip duplicates in allOf
	superFieldNames := make(map[string]bool)
	if decl.SuperClass != "" {
		superName := decl.SuperClass
		if openIdx := strings.Index(superName, "<"); openIdx >= 0 {
			superName = superName[:openIdx]
		}
		if superDecl, ok := idx.types[superName]; ok {
			for _, sf := range superDecl.Fields {
				name := sf.JSONName
				if name == "" {
					name = sf.Name
				}
				superFieldNames[name] = true
			}
		} else if superDecl, ok := idx.ResolveSimple(superName); ok {
			for _, sf := range superDecl.Fields {
				name := sf.JSONName
				if name == "" {
					name = sf.Name
				}
				superFieldNames[name] = true
			}
		}
	}

	for _, field := range decl.Fields {
		if shouldSkipField(field) {
			continue
		}

		jsonName := field.JSONName
		if jsonName == "" {
			jsonName = field.Name
		}

		// Skip fields that exist in the superclass to prevent duplicates in allOf
		if superFieldNames[jsonName] {
			continue
		}

		fieldSchema := idx.fieldTypeToSchema(field.Type, visited)

		if field.Description != "" {
			fieldSchema["description"] = field.Description
		}
		if field.DefaultValue != "" {
			fieldSchema["default"] = field.DefaultValue
		}
		applyJavaValidation(field.Annotations, fieldSchema)

		// v5 #1: @Nullable on field → nullable: true
		if field.Nullable {
			fieldSchema["nullable"] = true
		}

		// v5 #10: @Deprecated on field
		if field.Deprecated {
			fieldSchema["deprecated"] = true
		}

		properties[jsonName] = fieldSchema
		if field.Required {
			required = append(required, jsonName)
		}
	}

	schema["properties"] = properties
	if len(required) > 0 {
		// Filter required to only contain names present in properties
		var filteredRequired []string
		for _, r := range required {
			if _, exists := properties[r]; exists {
				filteredRequired = append(filteredRequired, r)
			}
		}
		if len(filteredRequired) > 0 {
			schema["required"] = filteredRequired
		}
	}

	return schema
}

// fieldTypeToSchema converts a Java/TypeScript type string to an OpenAPI schema.
func (idx *Index) fieldTypeToSchema(typeName string, visited map[string]bool) map[string]interface{} {
	typeName = strings.TrimSpace(typeName)

	// Handle nullable types (TypeScript: string | null)
	typeName = strings.TrimSuffix(typeName, " | null")
	typeName = strings.TrimSuffix(typeName, " | undefined")

	// Handle Java/TS primitive types
	switch strings.ToLower(typeName) {
	case "string", "java.lang.string":
		return map[string]interface{}{"type": "string"}
	case "int", "integer", "java.lang.integer":
		return map[string]interface{}{"type": "integer", "format": "int32"}
	case "long", "java.lang.long":
		return map[string]interface{}{"type": "integer", "format": "int64"}
	case "double", "java.lang.double":
		return map[string]interface{}{"type": "number", "format": "double"}
	case "float", "java.lang.float":
		return map[string]interface{}{"type": "number", "format": "float"}
	case "boolean", "java.lang.boolean":
		return map[string]interface{}{"type": "boolean"}
	case "number": // TypeScript number
		return map[string]interface{}{"type": "number"}
	case "void":
		return map[string]interface{}{}
	case "date", "localdate", "java.time.localdate":
		return map[string]interface{}{"type": "string", "format": "date"}
	case "localdatetime", "offsetdatetime", "zoneddatetime", "instant",
		"java.time.localdatetime", "java.time.offsetdatetime",
		"java.time.zoneddatetime", "java.time.instant":
		return map[string]interface{}{"type": "string", "format": "date-time"}
	case "uuid", "java.util.uuid":
		return map[string]interface{}{"type": "string", "format": "uuid"}
	case "bigdecimal", "java.math.bigdecimal":
		return map[string]interface{}{"type": "string", "format": "decimal"}
	case "short", "java.lang.short":
		return map[string]interface{}{"type": "integer", "format": "int32"}
	case "byte[]":
		return map[string]interface{}{"type": "string", "format": "byte"}
	case "byte", "java.lang.byte":
		return map[string]interface{}{"type": "integer", "format": "int32"}
	case "object", "any", "jsonnode":
		return map[string]interface{}{}
	}

	// Handle Java wildcard types (?, ? extends T, ? super T)
	if typeName == "?" {
		return map[string]interface{}{"type": "object"}
	}
	if strings.HasPrefix(typeName, "? extends ") {
		inner := strings.TrimPrefix(typeName, "? extends ")
		return idx.fieldTypeToSchema(strings.TrimSpace(inner), visited)
	}
	if strings.HasPrefix(typeName, "? super ") {
		inner := strings.TrimPrefix(typeName, "? super ")
		return idx.fieldTypeToSchema(strings.TrimSpace(inner), visited)
	}

	// Handle Page<T> wrapper type
	if strings.HasPrefix(typeName, "Page<") {
		inner := extractGenericInner(typeName)
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"content": map[string]interface{}{
					"type":  "array",
					"items": idx.fieldTypeToSchema(inner, visited),
				},
				"totalElements":    map[string]interface{}{"type": "integer", "format": "int64"},
				"totalPages":       map[string]interface{}{"type": "integer", "format": "int32"},
				"number":           map[string]interface{}{"type": "integer", "format": "int32"},
				"size":             map[string]interface{}{"type": "integer", "format": "int32"},
				"numberOfElements": map[string]interface{}{"type": "integer", "format": "int32"},
				"first":            map[string]interface{}{"type": "boolean"},
				"last":             map[string]interface{}{"type": "boolean"},
				"empty":            map[string]interface{}{"type": "boolean"},
			},
		}
	}

	// Handle Java generic collections
	if strings.HasPrefix(typeName, "List<") || strings.HasPrefix(typeName, "Set<") ||
		strings.HasPrefix(typeName, "Collection<") || strings.HasPrefix(typeName, "HashSet<") ||
		strings.HasPrefix(typeName, "TreeSet<") {
		inner := extractGenericInner(typeName)
		schema := map[string]interface{}{
			"type":  "array",
			"items": idx.fieldTypeToSchema(inner, visited),
		}
		if strings.HasPrefix(typeName, "Set<") || strings.HasPrefix(typeName, "HashSet<") ||
			strings.HasPrefix(typeName, "TreeSet<") {
			schema["uniqueItems"] = true
		}
		return schema
	}

	// Handle TypeScript array syntax: Type[] or Array<Type>
	if strings.HasSuffix(typeName, "[]") {
		inner := strings.TrimSuffix(typeName, "[]")
		return map[string]interface{}{
			"type":  "array",
			"items": idx.fieldTypeToSchema(inner, visited),
		}
	}
	if strings.HasPrefix(typeName, "Array<") {
		inner := extractGenericInner(typeName)
		return map[string]interface{}{
			"type":  "array",
			"items": idx.fieldTypeToSchema(inner, visited),
		}
	}

	// Handle Map/Record types
	if strings.HasPrefix(typeName, "Map<") || strings.HasPrefix(typeName, "HashMap<") {
		// Map<K,V> -> object with additionalProperties
		parts := splitGenericArgs(extractGenericInner(typeName))
		if len(parts) >= 2 {
			return map[string]interface{}{
				"type":                 "object",
				"additionalProperties": idx.fieldTypeToSchema(parts[1], visited),
			}
		}
		return map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{}}
	}
	if strings.HasPrefix(typeName, "Record<") {
		parts := splitGenericArgs(extractGenericInner(typeName))
		if len(parts) >= 2 {
			return map[string]interface{}{
				"type":                 "object",
				"additionalProperties": idx.fieldTypeToSchema(parts[1], visited),
			}
		}
		return map[string]interface{}{"type": "object", "additionalProperties": map[string]interface{}{}}
	}

	// Handle Optional/Nullable
	if strings.HasPrefix(typeName, "Optional<") {
		inner := extractGenericInner(typeName)
		return idx.fieldTypeToSchema(inner, visited)
	}

	// Try to resolve from the index
	if decl, ok := idx.types[typeName]; ok {
		return idx.ToOpenAPISchema(decl, visited)
	}
	// Try simple name resolution
	if decl, ok := idx.ResolveSimple(typeName); ok {
		return idx.ToOpenAPISchema(decl, visited)
	}

	// Unknown type - return a $ref placeholder
	return map[string]interface{}{"$ref": "#/components/schemas/" + typeName}
}

// Resolver resolves a type reference to an OpenAPI schema. Convenience wrapper.
func (idx *Index) Resolver(ref string) (map[string]interface{}, bool) {
	decl, ok := idx.Resolve(ref)
	if !ok {
		return nil, false
	}
	return idx.ToOpenAPISchema(decl, nil), true
}

// shouldSkipField returns true if a field should be excluded from the schema
// based on serialization annotations (@JsonIgnore, @Exclude, @Transient).
func shouldSkipField(field FieldDecl) bool {
	if _, ok := field.Annotations["JsonIgnore"]; ok {
		return true
	}
	if _, ok := field.Annotations["Exclude"]; ok {
		return true
	}
	if _, ok := field.Annotations["Transient"]; ok {
		return true
	}
	// Lombok @Getter(NONE) → writeOnly (no getter, so not in response)
	if val, ok := field.Annotations["Getter"]; ok && strings.Contains(val, "NONE") {
		// Don't skip — just mark as writeOnly (handled in applyJavaValidation)
	}
	// Lombok @Setter(NONE) → readOnly (no setter, so not in request)
	if val, ok := field.Annotations["Setter"]; ok && strings.Contains(val, "NONE") {
		// Don't skip — just mark as readOnly (handled in applyJavaValidation)
	}
	return false
}

// isJsonUnwrapped returns true if the field has @JsonUnwrapped annotation.
func isJsonUnwrapped(field FieldDecl) bool {
	_, ok := field.Annotations["JsonUnwrapped"]
	return ok
}

// inlineUnwrappedFields resolves the given type and copies its properties
// into the parent properties map. This implements @JsonUnwrapped behavior.
func (idx *Index) inlineUnwrappedFields(typeName string, visited map[string]bool, properties map[string]interface{}, required *[]string) {
	typeName = strings.TrimSpace(typeName)
	// Try to resolve to a known type
	var decl *TypeDecl
	if d, ok := idx.types[typeName]; ok {
		decl = d
	} else if d, ok := idx.ResolveSimple(typeName); ok {
		decl = d
	}
	if decl == nil {
		return
	}
	// Build the nested schema and inline its properties
	nestedSchema := idx.ToOpenAPISchema(decl, visited)
	if nestedProps, ok := nestedSchema["properties"].(map[string]interface{}); ok {
		for k, v := range nestedProps {
			properties[k] = v
		}
	}
	if nestedReq, ok := nestedSchema["required"].([]string); ok {
		*required = append(*required, nestedReq...)
	}
}

// applyJavaValidation applies Java/TS validation constraint annotations to a schema.
func applyJavaValidation(annotations map[string]string, schema map[string]interface{}) {
	if annotations == nil {
		return
	}

	schemaType, _ := schema["type"].(string)
	isString := schemaType == "string"
	isNumeric := schemaType == "number" || schemaType == "integer"
	isArray := schemaType == "array"

	for name, value := range annotations {
		switch name {
		case "Email", "IsEmail":
			if isString {
				schema["format"] = "email"
			}
		case "URL", "IsUrl", "IsURL":
			if isString {
				schema["format"] = "uri"
			}
		case "IsUUID":
			if isString {
				schema["format"] = "uuid"
			}
		case "IsDateString", "IsISO8601":
			if isString {
				schema["format"] = "date-time"
			}
		case "Size":
			applySizeConstraint(value, schema, isArray)
		case "MinLength":
			if n, err := strconv.Atoi(extractAnnotationFirstValue(value)); err == nil {
				schema["minLength"] = n
			}
		case "MaxLength":
			if n, err := strconv.Atoi(extractAnnotationFirstValue(value)); err == nil {
				schema["maxLength"] = n
			}
		case "Matches":
			pat := stripQuotes(extractAnnotationFirstValue(value))
			if pat != "" {
				schema["pattern"] = pat
			}
		case "Min":
			if n, err := strconv.Atoi(extractAnnotationFirstValue(value)); err == nil {
				schema["minimum"] = n
			}
		case "Max":
			if n, err := strconv.Atoi(extractAnnotationFirstValue(value)); err == nil {
				schema["maximum"] = n
			}
		case "DecimalMin":
			if n, err := strconv.ParseFloat(stripQuotes(extractAnnotationFirstValue(value)), 64); err == nil {
				schema["minimum"] = n
			}
		case "DecimalMax":
			if n, err := strconv.ParseFloat(stripQuotes(extractAnnotationFirstValue(value)), 64); err == nil {
				schema["maximum"] = n
			}
		case "Pattern":
			pat := extractNamedValue(value, "regexp")
			if pat == "" {
				pat = extractAnnotationFirstValue(value)
			}
			pat = stripQuotes(pat)
			if pat != "" {
				schema["pattern"] = pat
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
				if ex := extractNamedValue(value, "example"); ex != "" {
					schema["example"] = stripQuotes(ex)
				}
				if extractNamedValue(value, "deprecated") == "true" {
					schema["deprecated"] = true
				}
				// v5 #5: Extended @Schema attributes
				if desc := extractNamedValue(value, "description"); desc != "" {
					schema["description"] = stripQuotes(desc)
				}
				if ml := extractNamedValue(value, "minLength"); ml != "" {
					if n, err := strconv.Atoi(ml); err == nil {
						schema["minLength"] = n
					}
				}
				if ml := extractNamedValue(value, "maxLength"); ml != "" {
					if n, err := strconv.Atoi(ml); err == nil {
						schema["maxLength"] = n
					}
				}
				if m := extractNamedValue(value, "minimum"); m != "" {
					if n, err := strconv.Atoi(stripQuotes(m)); err == nil {
						schema["minimum"] = n
					}
				}
				if m := extractNamedValue(value, "maximum"); m != "" {
					if n, err := strconv.Atoi(stripQuotes(m)); err == nil {
						schema["maximum"] = n
					}
				}
				if p := extractNamedValue(value, "pattern"); p != "" {
					schema["pattern"] = stripQuotes(p)
				}
				if am := extractNamedValue(value, "accessMode"); am != "" {
					if strings.Contains(am, "READ_ONLY") {
						schema["readOnly"] = true
					} else if strings.Contains(am, "WRITE_ONLY") {
						schema["writeOnly"] = true
					}
				}
				// @Schema(allowableValues = {"A", "B"}) → enum
				if av := extractNamedValue(value, "allowableValues"); av != "" {
					av = strings.TrimPrefix(av, "{")
					av = strings.TrimSuffix(av, "}")
					var vals []interface{}
					for _, v := range strings.Split(av, ",") {
						v = strings.TrimSpace(v)
						v = stripQuotes(v)
						if v != "" {
							vals = append(vals, v)
						}
					}
					if len(vals) > 0 {
						schema["enum"] = vals
					}
				}
			}
		case "Digits":
			// @Digits(integer=X, fraction=Y) — common on financial/decimal fields
			if value != "" {
				intPart := extractNamedValue(value, "integer")
				fracPart := extractNamedValue(value, "fraction")
				if intPart != "" {
					if n, err := strconv.Atoi(intPart); err == nil {
						schema["x-digits-integer"] = n
					}
				}
				if fracPart != "" {
					if n, err := strconv.Atoi(fracPart); err == nil {
						schema["x-digits-fraction"] = n
					}
				}
			}
		case "JsonFormat":
			// v5 #6: @JsonFormat on date/time fields
			if value != "" {
				if shape := extractNamedValue(value, "shape"); strings.Contains(shape, "STRING") {
					schema["type"] = "string"
				}
				if pat := extractNamedValue(value, "pattern"); pat != "" {
					schema["x-format-pattern"] = stripQuotes(pat)
				}
			}
		case "ApiProperty":
			// NestJS @ApiProperty({ description: '...', example: '...' }) decorator
			if value != "" {
				if desc := extractTSObjectFieldValue(value, "description"); desc != "" {
					schema["description"] = desc
				}
				if ex := extractTSObjectFieldValue(value, "example"); ex != "" {
					schema["example"] = ex
				}
			}
		case "JsonInclude":
			// @JsonInclude(NON_NULL) — field may be null when excluded
			if strings.Contains(value, "NON_NULL") {
				schema["nullable"] = true
			}
		case "Range":
			// @Range(min=X, max=Y) — combined min+max in one annotation
			if value != "" {
				if m := extractNamedValue(value, "min"); m != "" {
					if n, err := strconv.Atoi(m); err == nil {
						schema["minimum"] = n
					}
				}
				if m := extractNamedValue(value, "max"); m != "" {
					if n, err := strconv.Atoi(m); err == nil {
						schema["maximum"] = n
					}
				}
			}
		case "Length":
			// @Length(min=X, max=Y) — combined minLength/maxLength
			if value != "" {
				if m := extractNamedValue(value, "min"); m != "" {
					if n, err := strconv.Atoi(m); err == nil {
						schema["minLength"] = n
					}
				}
				if m := extractNamedValue(value, "max"); m != "" {
					if n, err := strconv.Atoi(m); err == nil {
						schema["maxLength"] = n
					}
				}
			}
		case "UniqueElements":
			// @UniqueElements — collection fields should have uniqueItems
			if isArray {
				schema["uniqueItems"] = true
			}
		case "Getter":
			// Lombok @Getter(NONE) → field is writeOnly (no getter, invisible in responses)
			if strings.Contains(value, "NONE") {
				schema["writeOnly"] = true
			}
		case "Setter":
			// Lombok @Setter(NONE) → field is readOnly (no setter, cannot be set in requests)
			if strings.Contains(value, "NONE") {
				schema["readOnly"] = true
			}
		}
	}
}

func applySizeConstraint(args string, schema map[string]interface{}, isArray bool) {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	for _, part := range strings.Split(args, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "min") {
			if idx := strings.Index(part, "="); idx >= 0 {
				val := strings.TrimSpace(part[idx+1:])
				if n, err := strconv.Atoi(val); err == nil {
					if isArray {
						schema["minItems"] = n
					} else {
						schema["minLength"] = n
					}
				}
			}
		}
		if strings.HasPrefix(part, "max") {
			if idx := strings.Index(part, "="); idx >= 0 {
				val := strings.TrimSpace(part[idx+1:])
				if n, err := strconv.Atoi(val); err == nil {
					if isArray {
						schema["maxItems"] = n
					} else {
						schema["maxLength"] = n
					}
				}
			}
		}
	}
}

// extractAnnotationFirstValue extracts the first positional argument from annotation args.
func extractAnnotationFirstValue(args string) string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	if idx := strings.Index(args, ","); idx >= 0 {
		first := strings.TrimSpace(args[:idx])
		if !strings.Contains(first, "=") {
			return first
		}
	}
	if strings.Contains(args, "=") {
		return args
	}
	return args
}

// extractNamedValue extracts a named parameter from annotation args.
// Handles brace-enclosed values like allowableValues = {"A", "B"}.
func extractNamedValue(args, paramName string) string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	// Split on commas but respect brace nesting
	parts := splitRespectingBraces(args)
	for _, part := range parts {
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

// splitRespectingBraces splits a string on commas, but treats content inside
// curly braces as a single unit (for annotation values like {\"A\", \"B\"}).
func splitRespectingBraces(s string) []string {
	var result []string
	depth := 0
	start := 0
	for i, c := range s {
		switch c {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				result = append(result, s[start:i])
				start = i + 1
			}
		}
	}
	result = append(result, s[start:])
	return result
}

// extractGenericInner extracts the inner type from Generic<Inner>.
func extractGenericInner(typeName string) string {
	start := strings.Index(typeName, "<")
	if start < 0 {
		return typeName
	}
	end := strings.LastIndex(typeName, ">")
	if end <= start {
		return typeName
	}
	return strings.TrimSpace(typeName[start+1 : end])
}

// extractTSObjectFieldValue extracts a field value from a TS object literal.
// e.g. `({ description: 'The unique identifier', example: '123' })` with field "description" -> "The unique identifier"
func extractTSObjectFieldValue(args, field string) string {
	// Strip outer parens and braces
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)
	args = strings.TrimPrefix(args, "{")
	args = strings.TrimSuffix(args, "}")
	args = strings.TrimSpace(args)

	// Look for field: 'value' or field: "value"
	for _, part := range strings.Split(args, ",") {
		part = strings.TrimSpace(part)
		if idx := strings.Index(part, ":"); idx >= 0 {
			key := strings.TrimSpace(part[:idx])
			if key == field {
				val := strings.TrimSpace(part[idx+1:])
				return stripQuotes(val)
			}
		}
	}
	return ""
}

// splitGenericArgs splits "A, B" respecting nested generics.
func splitGenericArgs(args string) []string {
	var result []string
	depth := 0
	start := 0
	for i, c := range args {
		switch c {
		case '<':
			depth++
		case '>':
			depth--
		case ',':
			if depth == 0 {
				result = append(result, strings.TrimSpace(args[start:i]))
				start = i + 1
			}
		}
	}
	result = append(result, strings.TrimSpace(args[start:]))
	return result
}
