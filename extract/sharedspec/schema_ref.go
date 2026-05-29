package sharedspec

// wrapSchemaRefSiblings moves keywords that cannot sit beside $ref into a valid
// allOf: [{ $ref }, { type: object, ...siblings }].
func wrapSchemaRefSiblings(schema map[string]any) map[string]any {
	if schema == nil {
		return schema
	}
	ref, hasRef := schema["$ref"]
	if !hasRef || len(schema) <= 1 {
		return schema
	}
	siblings := make(map[string]any)
	for k, v := range schema {
		if k != "$ref" {
			siblings[k] = v
		}
	}
	if len(siblings) == 0 {
		return map[string]any{"$ref": ref}
	}
	if _, hasType := siblings["type"]; !hasType {
		siblings["type"] = "object"
	}
	return map[string]any{
		"allOf": []any{
			map[string]any{"$ref": ref},
			siblings,
		},
	}
}

// EnrichSchemaObjectTarget returns the map that should receive inline
// properties/required when enriching a request-body schema (handles allOf).
func EnrichSchemaObjectTarget(schema map[string]any) map[string]any {
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if _, ok := schema["properties"].(map[string]any); ok {
		return schema
	}
	if allOf, ok := schema["allOf"].([]any); ok && len(allOf) > 0 {
		for i := len(allOf) - 1; i >= 0; i-- {
			m, ok := allOf[i].(map[string]any)
			if !ok || m["$ref"] != nil {
				continue
			}
			if m["type"] == nil {
				m["type"] = "object"
			}
			if m["properties"] == nil {
				m["properties"] = map[string]any{}
			}
			return m
		}
		own := map[string]any{"type": "object", "properties": map[string]any{}}
		schema["allOf"] = append(allOf, own)
		return own
	}
	if ref, hasRef := schema["$ref"]; hasRef {
		wrapped := wrapSchemaRefSiblings(schema)
		if _, ok := wrapped["allOf"].([]any); ok {
			for k := range schema {
				delete(schema, k)
			}
			for k, v := range wrapped {
				schema[k] = v
			}
			return EnrichSchemaObjectTarget(schema)
		}
		own := map[string]any{"type": "object", "properties": map[string]any{}}
		delete(schema, "$ref")
		schema["allOf"] = []any{
			map[string]any{"$ref": ref},
			own,
		}
		return own
	}
	if schema["type"] == nil {
		schema["type"] = "object"
	}
	if schema["properties"] == nil {
		schema["properties"] = map[string]any{}
	}
	return schema
}
