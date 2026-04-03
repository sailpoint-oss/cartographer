// Package generics provides structured parsing of generic type expressions
// for Java and TypeScript, replacing fragile string-based extraction.
package generics

import (
	"strings"
)

// TypeNode represents a parsed type expression tree.
type TypeNode struct {
	Name   string      // e.g. "ResponseEntity", "List", "Map", "String"
	Params []*TypeNode // Generic type parameters (recursive)
	Array  bool        // true if trailing "[]"
}

// WrapperTypes that should be unwrapped to get the "real" response type.
var WrapperTypes = map[string]bool{
	"ResponseEntity":   true,
	"HttpEntity":       true,
	"Promise":          true,
	"Observable":       true,
	"CompletableFuture": true,
	"Mono":             true,
	"Flux":             true,
	"DeferredResult":   true,
	"Callable":         true,
}

// ArrayWrapperTypes are wrapper types that represent a stream/collection of items.
// After unwrapping, the inner type should be wrapped in an array schema.
var ArrayWrapperTypes = map[string]bool{
	"Flux": true,
}

// CollectionTypes maps collection type names to their OpenAPI representation.
var CollectionTypes = map[string]bool{
	"List":       true,
	"Set":        true,
	"Collection": true,
	"ArrayList":  true,
	"LinkedList": true,
	"HashSet":    true,
	"TreeSet":    true,
	"Array":      true, // TypeScript Array<T>
}

// MapTypes are types that map to OpenAPI additionalProperties.
var MapTypes = map[string]bool{
	"Map":       true,
	"HashMap":   true,
	"TreeMap":   true,
	"Record":    true, // TypeScript Record<K, V>
	"LinkedHashMap": true,
}

// Parse parses a raw type string into a TypeNode tree.
// Examples:
//
//	"ResponseEntity<List<ServiceAPI>>" -> {Name:"ResponseEntity", Params:[{Name:"List", Params:[{Name:"ServiceAPI"}]}]}
//	"Map<String, List<User>>"         -> {Name:"Map", Params:[{Name:"String"}, {Name:"List", Params:[{Name:"User"}]}]}
//	"String[]"                        -> {Name:"String", Array:true}
//	"? extends Base"                  -> {Name:"?", Params:[{Name:"Base"}]}
func Parse(raw string) *TypeNode {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return &TypeNode{Name: "object"}
	}
	node, _ := parseType(raw, 0)
	return node
}

// parseType parses starting at pos, returns the TypeNode and the position after it.
func parseType(raw string, pos int) (*TypeNode, int) {
	// Skip whitespace
	for pos < len(raw) && raw[pos] == ' ' {
		pos++
	}

	if pos >= len(raw) {
		return &TypeNode{Name: "object"}, pos
	}

	// Handle wildcard: ? or ? extends T or ? super T
	if raw[pos] == '?' {
		pos++
		for pos < len(raw) && raw[pos] == ' ' {
			pos++
		}
		if pos < len(raw) && strings.HasPrefix(raw[pos:], "extends ") {
			pos += len("extends ")
			inner, newPos := parseType(raw, pos)
			return &TypeNode{Name: "? extends", Params: []*TypeNode{inner}}, newPos
		}
		if pos < len(raw) && strings.HasPrefix(raw[pos:], "super ") {
			pos += len("super ")
			inner, newPos := parseType(raw, pos)
			return &TypeNode{Name: "? super", Params: []*TypeNode{inner}}, newPos
		}
		return &TypeNode{Name: "?"}, pos
	}

	// Read the type name (everything up to '<', '>', ',', '[', or end)
	start := pos
	for pos < len(raw) && raw[pos] != '<' && raw[pos] != '>' && raw[pos] != ',' && raw[pos] != '[' {
		pos++
	}
	name := strings.TrimSpace(raw[start:pos])
	if name == "" {
		return &TypeNode{Name: "object"}, pos
	}

	node := &TypeNode{Name: name}

	// Check for generic parameters: <T, U, ...>
	if pos < len(raw) && raw[pos] == '<' {
		pos++ // skip '<'
		for {
			// Skip whitespace
			for pos < len(raw) && raw[pos] == ' ' {
				pos++
			}
			if pos >= len(raw) || raw[pos] == '>' {
				if pos < len(raw) {
					pos++ // skip '>'
				}
				break
			}
			param, newPos := parseType(raw, pos)
			node.Params = append(node.Params, param)
			pos = newPos
			// Skip whitespace and comma
			for pos < len(raw) && (raw[pos] == ' ' || raw[pos] == ',') {
				pos++
			}
			if pos < len(raw) && raw[pos] == '>' {
				pos++ // skip '>'
				break
			}
		}
	}

	// Check for array suffix: []
	if pos+1 < len(raw) && raw[pos] == '[' && raw[pos+1] == ']' {
		node.Array = true
		pos += 2
	}

	return node, pos
}

// UnwrapWrappers strips framework wrapper types, returning the inner type.
// ResponseEntity<List<User>> -> List<User>
// Promise<Observable<Foo>>   -> Foo
// Flux<Item>                 -> List<Item> (array wrapper)
func (n *TypeNode) UnwrapWrappers() *TypeNode {
	if n == nil {
		return n
	}
	current := n
	wasArrayWrapper := false
	for WrapperTypes[current.Name] && len(current.Params) > 0 {
		if ArrayWrapperTypes[current.Name] {
			wasArrayWrapper = true
		}
		current = current.Params[0]
	}
	if wasArrayWrapper {
		return &TypeNode{Name: "List", Params: []*TypeNode{current}}
	}
	return current
}

// ToOpenAPISchema converts a TypeNode to an OpenAPI schema map.
// It resolves collection types (List->array, Set->array+uniqueItems, Map->additionalProperties)
// and produces $ref for named types.
func (n *TypeNode) ToOpenAPISchema(knownSchemas map[string]bool) map[string]interface{} {
	if n == nil {
		return map[string]interface{}{"type": "object"}
	}

	// First unwrap wrappers
	node := n.UnwrapWrappers()
	return node.toSchema(knownSchemas)
}

func (n *TypeNode) toSchema(knownSchemas map[string]bool) map[string]interface{} {
	// Handle arrays (both [] suffix and Array<T>)
	if n.Array {
		inner := &TypeNode{Name: n.Name, Params: n.Params}
		return map[string]interface{}{
			"type":  "array",
			"items": inner.toSchema(knownSchemas),
		}
	}

	// Handle wildcards
	if n.Name == "?" {
		return map[string]interface{}{"type": "object"}
	}
	if n.Name == "? super" {
		if len(n.Params) > 0 {
			return n.Params[0].toSchema(knownSchemas)
		}
		return map[string]interface{}{"type": "object"}
	}
	if n.Name == "? extends" && len(n.Params) > 0 {
		return n.Params[0].toSchema(knownSchemas)
	}

	// Handle void
	if n.Name == "void" || n.Name == "Void" {
		return map[string]interface{}{}
	}

	// Handle collection types -> array
	if CollectionTypes[n.Name] {
		schema := map[string]interface{}{
			"type": "array",
		}
		if len(n.Params) > 0 {
			schema["items"] = n.Params[0].toSchema(knownSchemas)
		} else {
			schema["items"] = map[string]interface{}{}
		}
		if n.Name == "Set" || n.Name == "HashSet" || n.Name == "TreeSet" {
			schema["uniqueItems"] = true
		}
		return schema
	}

	// Handle map types -> object with additionalProperties
	if MapTypes[n.Name] {
		schema := map[string]interface{}{
			"type": "object",
		}
		if len(n.Params) >= 2 {
			schema["additionalProperties"] = n.Params[1].toSchema(knownSchemas)
		} else {
			schema["additionalProperties"] = map[string]interface{}{}
		}
		return schema
	}

	// Handle Optional<T> -> just T
	if n.Name == "Optional" && len(n.Params) > 0 {
		return n.Params[0].toSchema(knownSchemas)
	}

	// Handle Page<T> -> paginated object
	if n.Name == "Page" {
		itemSchema := map[string]interface{}{}
		if len(n.Params) > 0 {
			itemSchema = n.Params[0].toSchema(knownSchemas)
		}
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"content": map[string]interface{}{
					"type":  "array",
					"items": itemSchema,
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

	// Handle simple/primitive types
	if schema := primitiveToSchema(n.Name); schema != nil {
		return schema
	}

	// Complex type with generic params -> normalized name ref
	if len(n.Params) > 0 {
		normalized := normalizeGenericName(n)
		return map[string]interface{}{
			"$ref": "#/components/schemas/" + normalized,
		}
	}

	// Named complex type -> $ref
	return map[string]interface{}{
		"$ref": "#/components/schemas/" + n.Name,
	}
}

// CollectTypeRefs walks the TypeNode tree and collects all referenced type names
// that would need schemas (excluding primitives, collections, maps, wrappers).
func (n *TypeNode) CollectTypeRefs(refs map[string]bool) {
	if n == nil {
		return
	}

	node := n.UnwrapWrappers()
	node.collectRefs(refs)
}

func (n *TypeNode) collectRefs(refs map[string]bool) {
	if n == nil {
		return
	}

	// Wildcards — recurse into bound if present
	if n.Name == "?" {
		return
	}
	if n.Name == "? super" || n.Name == "? extends" {
		for _, p := range n.Params {
			p.collectRefs(refs)
		}
		return
	}

	// Collections/maps/optional -> recurse into params
	if CollectionTypes[n.Name] || MapTypes[n.Name] || n.Name == "Optional" || n.Name == "Page" {
		for _, p := range n.Params {
			p.collectRefs(refs)
		}
		return
	}

	// Wrappers -> recurse
	if WrapperTypes[n.Name] {
		for _, p := range n.Params {
			p.collectRefs(refs)
		}
		return
	}

	// Primitives -> skip
	if primitiveToSchema(n.Name) != nil {
		return
	}
	if n.Name == "void" || n.Name == "Void" || n.Name == "" {
		return
	}

	// Named type with generic params -> add normalized name and recurse
	if len(n.Params) > 0 {
		normalized := normalizeGenericName(n)
		refs[normalized] = true
		for _, p := range n.Params {
			p.collectRefs(refs)
		}
		return
	}

	// Simple named type
	refs[n.Name] = true
}

// Substitute replaces generic type parameters with concrete types.
func (n *TypeNode) Substitute(bindings map[string]*TypeNode) *TypeNode {
	if n == nil {
		return nil
	}

	// If this is a type parameter name in bindings, replace it
	if len(n.Params) == 0 && !n.Array {
		if replacement, ok := bindings[n.Name]; ok {
			return replacement
		}
	}

	// Recurse into params
	newParams := make([]*TypeNode, len(n.Params))
	for i, p := range n.Params {
		newParams[i] = p.Substitute(bindings)
	}

	return &TypeNode{
		Name:   n.Name,
		Params: newParams,
		Array:  n.Array,
	}
}

// String reconstructs the type string (for debugging/display).
func (n *TypeNode) String() string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(n.Name)
	if len(n.Params) > 0 {
		b.WriteByte('<')
		for i, p := range n.Params {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(p.String())
		}
		b.WriteByte('>')
	}
	if n.Array {
		b.WriteString("[]")
	}
	return b.String()
}

// normalizeGenericName converts a TypeNode to a valid OpenAPI schema name.
// e.g. ExportedObject<Foo> -> ExportedObjectFoo
func normalizeGenericName(n *TypeNode) string {
	var b strings.Builder
	b.WriteString(n.Name)
	for _, p := range n.Params {
		b.WriteString(normalizeGenericNameInner(p))
	}
	return b.String()
}

func normalizeGenericNameInner(n *TypeNode) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	// Skip wildcard names in normalization
	if n.Name != "?" && n.Name != "? extends" && n.Name != "? super" {
		b.WriteString(n.Name)
	}
	for _, p := range n.Params {
		b.WriteString(normalizeGenericNameInner(p))
	}
	return b.String()
}

// primitiveToSchema returns the OpenAPI schema for a primitive type name,
// or nil if it's not a known primitive.
func primitiveToSchema(name string) map[string]interface{} {
	switch strings.ToLower(name) {
	case "string", "java.lang.string":
		return map[string]interface{}{"type": "string"}
	case "int", "integer", "java.lang.integer":
		return map[string]interface{}{"type": "integer", "format": "int32"}
	case "long", "java.lang.long":
		return map[string]interface{}{"type": "integer", "format": "int64"}
	case "short", "java.lang.short":
		return map[string]interface{}{"type": "integer", "format": "int32"}
	case "double", "java.lang.double":
		return map[string]interface{}{"type": "number", "format": "double"}
	case "float", "java.lang.float":
		return map[string]interface{}{"type": "number", "format": "float"}
	case "boolean", "java.lang.boolean":
		return map[string]interface{}{"type": "boolean"}
	case "uuid", "java.util.uuid":
		return map[string]interface{}{"type": "string", "format": "uuid"}
	case "date", "localdate":
		return map[string]interface{}{"type": "string", "format": "date"}
	case "localdatetime", "offsetdatetime", "zoneddatetime", "instant":
		return map[string]interface{}{"type": "string", "format": "date-time"}
	case "bigdecimal":
		return map[string]interface{}{"type": "string", "format": "decimal"}
	case "object":
		return map[string]interface{}{"type": "object"}
	case "byte[]":
		return map[string]interface{}{"type": "string", "format": "binary"}
	// TypeScript primitives
	case "number":
		return map[string]interface{}{"type": "number"}
	case "any", "unknown":
		return map[string]interface{}{}
	}
	return nil
}

// IsPrimitive returns true if the type name is a known primitive/simple type.
func IsPrimitive(name string) bool {
	return primitiveToSchema(name) != nil ||
		name == "void" || name == "Void" ||
		name == "undefined" || name == "null" || name == "never"
}
