package sourcemap

import "strings"

// BuildFromSpec walks an OpenAPI spec map and builds source mappings.
func BuildFromSpec(spec map[string]any) *SourceMap {
	sm := New()
	root := normalizeSpec(spec)

	paths := asMap(root["paths"])
	for pathKey, pathVal := range paths {
		pathItem := asMap(pathVal)
		for method, opVal := range pathItem {
			switch strings.ToLower(method) {
			case "get", "put", "post", "delete", "patch", "head", "options", "trace":
			default:
				continue
			}
			op := asMap(opVal)
			loc := readLocation(op)
			if !loc.IsZero() {
				sm.PutOperation(method, pathKey, loc)
			}
		}
	}

	components := asMap(root["components"])
	schemas := asMap(components["schemas"])
	for schemaName, schemaVal := range schemas {
		schema := asMap(schemaVal)
		if loc := readLocation(schema); !loc.IsZero() {
			sm.SchemaMap[schemaName] = loc
		}
		props := asMap(schema["properties"])
		for fieldName, fieldVal := range props {
			fieldObj := asMap(fieldVal)
			if loc := readLocation(fieldObj); !loc.IsZero() {
				sm.FieldMap[FieldKey(schemaName, fieldName)] = loc
			}
		}
	}

	return sm
}
