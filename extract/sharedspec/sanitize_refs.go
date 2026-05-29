package sharedspec

import "strings"

// invalidSchemaNameRunes are characters that must never appear in an OpenAPI
// component schema name nor in the fragment of a local "#/components/schemas/"
// $ref. Unresolved generic type expressions (for example
// "Supplier<List<String>>" or "Function<String, String>") leak these when a
// type is not resolved to a concrete DTO. An unescaped "<" inside a $ref is an
// invalid JSON pointer and breaks ref resolution for the *entire* document in
// strict parsers/validators, which surfaces as a flood of false
// "unresolved-ref" diagnostics on otherwise valid components.
func containsInvalidSchemaNameRune(name string) bool {
	return strings.ContainsAny(name, "<>,?[](){}&| \t\n")
}

// sanitizeSchemaName collapses a generic type expression into a valid component
// name by dropping structural characters. The transform concatenates the
// generic parameter names, matching generics.normalizeGenericName for the
// common cases (e.g. "Supplier<List<String>>" -> "SupplierListString",
// "Map<String, Object>" -> "MapStringObject"). It is deliberately applied
// identically to both $ref values and component keys so the two stay
// mutually consistent and resolvable.
func sanitizeSchemaName(name string) string {
	if !containsInvalidSchemaNameRune(name) {
		return name
	}
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '<', '>', ',', '?', '[', ']', '(', ')', '{', '}', '&', '|', ' ', '\t', '\n':
			// drop — concatenates generic parameter names
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return "Object"
	}
	return out
}

// SanitizeSchemaRefs rewrites every local schema $ref and every component schema
// key so that no name contains characters that are invalid in an OpenAPI
// component name. Refs and keys are transformed with the same function, so a
// ref that previously pointed at "Supplier<List<String>>" and the (possibly
// generated) component keyed "Supplier<List<String>>" both become
// "SupplierListString" and remain mutually consistent.
//
// This is a defensive, document-wide invariant: regardless of which extractor
// code path produced an invalid generic name, the emitted spec never ships an
// unresolvable bracketed $ref.
func SanitizeSchemaRefs(spec map[string]any) {
	const prefix = "#/components/schemas/"
	sanitizeRefsIn(spec, prefix)

	components, ok := spec["components"].(map[string]any)
	if !ok {
		return
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		return
	}
	for name, schema := range schemas {
		clean := sanitizeSchemaName(name)
		if clean == name {
			continue
		}
		if _, exists := schemas[clean]; !exists {
			schemas[clean] = schema
		}
		delete(schemas, name)
	}
}

func sanitizeRefsIn(v any, prefix string) {
	switch val := v.(type) {
	case map[string]any:
		if ref, ok := val["$ref"].(string); ok && strings.HasPrefix(ref, prefix) {
			name := strings.TrimPrefix(ref, prefix)
			if clean := sanitizeSchemaName(name); clean != name {
				val["$ref"] = prefix + clean
			}
		}
		for _, child := range val {
			sanitizeRefsIn(child, prefix)
		}
	case []any:
		for _, child := range val {
			sanitizeRefsIn(child, prefix)
		}
	}
}

// WrapRefSiblings walks the whole spec and rewrites every schema object that
// holds a "$ref" alongside other keys into a valid allOf form:
//
//	{ $ref: X, x-source: ... }  ->  { allOf: [ { $ref: X } ], x-source: ... }
//
// A bare "$ref" with sibling keys is invalid under OpenAPI 3.0 (the $ref must
// be alone). Strict resolvers (Navigator) abort resolution for the ENTIRE
// document when they encounter one, which surfaces as a flood of false
// "unresolved-ref" diagnostics on otherwise valid components. Field-level
// property schemas frequently carry an "x-source" provenance extension next to
// a type "$ref"; this pass makes every such schema valid without dropping the
// extension. Keywords that are valid annotations on the wrapper (description,
// nullable, x-*) stay on the outer object; only the $ref moves into allOf.
func WrapRefSiblings(spec map[string]any) {
	wrapRefSiblingsIn(spec)
}

func wrapRefSiblingsIn(v any) {
	switch val := v.(type) {
	case map[string]any:
		if ref, ok := val["$ref"]; ok && len(val) > 1 {
			delete(val, "$ref")
			// Recurse into the remaining siblings first (they may themselves
			// contain nested ref-with-siblings), then attach the lifted $ref.
			for _, child := range val {
				wrapRefSiblingsIn(child)
			}
			val["allOf"] = append([]any{map[string]any{"$ref": ref}}, existingAllOf(val)...)
			return
		}
		for _, child := range val {
			wrapRefSiblingsIn(child)
		}
	case []any:
		for _, child := range val {
			wrapRefSiblingsIn(child)
		}
	}
}

// existingAllOf returns and removes any pre-existing allOf list on the object so
// a lifted $ref can be prepended without discarding prior members.
func existingAllOf(val map[string]any) []any {
	raw, ok := val["allOf"]
	if !ok {
		return nil
	}
	delete(val, "allOf")
	switch list := raw.(type) {
	case []any:
		return list
	default:
		return []any{raw}
	}
}
