package sharedspec

import "testing"

func TestSanitizeSchemaName(t *testing.T) {
	cases := map[string]string{
		"User":                                   "User",
		"Supplier<List<String>>":                 "SupplierListString",
		"Supplier<Map<String, Object>>":          "SupplierMapStringObject",
		"Function<String, String>":               "FunctionStringString",
		"Map<String, Map<String, List<String>>>": "MapStringMapStringListString",
		"":                                       "",
	}
	for in, want := range cases {
		if got := sanitizeSchemaName(in); got != want {
			t.Errorf("sanitizeSchemaName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeSchemaRefs_RewritesRefsAndKeysConsistently(t *testing.T) {
	spec := map[string]any{
		"paths": map[string]any{
			"/search": map[string]any{
				"post": map[string]any{
					"responses": map[string]any{
						"200": map[string]any{
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"$ref": "#/components/schemas/Supplier<List<String>>",
									},
								},
							},
						},
					},
				},
			},
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"Supplier<List<String>>": map[string]any{"type": "object"},
				"User":                   map[string]any{"type": "object"},
			},
		},
	}

	SanitizeSchemaRefs(spec)

	ref := spec["paths"].(map[string]any)["/search"].(map[string]any)["post"].(map[string]any)["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["$ref"]
	if ref != "#/components/schemas/SupplierListString" {
		t.Fatalf("ref = %v, want sanitized SupplierListString", ref)
	}
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	if _, ok := schemas["SupplierListString"]; !ok {
		t.Fatalf("expected renamed component key SupplierListString, got %v keys", len(schemas))
	}
	if _, ok := schemas["Supplier<List<String>>"]; ok {
		t.Fatalf("bracketed component key should be removed")
	}
}

func TestWrapRefSiblings_LiftsRefBesideXSource(t *testing.T) {
	spec := map[string]any{
		"components": map[string]any{
			"schemas": map[string]any{
				"Outer": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"filter": map[string]any{
							"$ref": "#/components/schemas/Filter",
							"x-source": map[string]any{
								"file": "/x/Outer.java",
								"line": 12,
							},
						},
					},
				},
				"Filter": map[string]any{"type": "object"},
			},
		},
	}
	WrapRefSiblings(spec)

	filter := spec["components"].(map[string]any)["schemas"].(map[string]any)["Outer"].(map[string]any)["properties"].(map[string]any)["filter"].(map[string]any)
	if _, hasRef := filter["$ref"]; hasRef {
		t.Fatalf("bare $ref should have been lifted into allOf, got %#v", filter)
	}
	allOf, ok := filter["allOf"].([]any)
	if !ok || len(allOf) != 1 {
		t.Fatalf("expected allOf with one member, got %#v", filter["allOf"])
	}
	if allOf[0].(map[string]any)["$ref"] != "#/components/schemas/Filter" {
		t.Fatalf("allOf member should hold the $ref, got %#v", allOf[0])
	}
	if _, ok := filter["x-source"]; !ok {
		t.Fatalf("x-source provenance must be preserved on the wrapper, got %#v", filter)
	}
}

// TestGenerateSpec_NoBracketedRefs is an end-to-end guard: after GenerateSpec
// no emitted $ref may contain generic-bracket characters, which would break
// JSON-pointer resolution for the whole document.
func TestGenerateSpec_NoBracketedRefs(t *testing.T) {
	spec := map[string]any{
		"components": map[string]any{
			"schemas": map[string]any{
				"Widget": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"compute": map[string]any{"$ref": "#/components/schemas/Function<String, String>"},
					},
				},
			},
		},
	}
	SanitizeSchemaRefs(spec)
	EnsureSchemaRefsExist(spec)

	refs := map[string]bool{}
	CollectRefs(spec, refs)
	for ref := range refs {
		if containsInvalidSchemaNameRune(ref) {
			t.Errorf("invalid bracketed ref survived: %q", ref)
		}
	}
	// The sanitized target must now exist as a component.
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	if _, ok := schemas["FunctionStringString"]; !ok {
		t.Fatalf("expected stub for sanitized ref FunctionStringString")
	}
}
