package sharedspec

import "testing"

func TestWrapSchemaRefSiblings(t *testing.T) {
	in := map[string]any{
		"$ref": "#/components/schemas/BaseDto",
		"properties": map[string]any{
			"foo": map[string]any{"type": "string"},
		},
	}
	out := wrapSchemaRefSiblings(in)
	allOf, ok := out["allOf"].([]any)
	if !ok || len(allOf) != 2 {
		t.Fatalf("allOf = %#v", out["allOf"])
	}
	if allOf[0].(map[string]any)["$ref"] == nil {
		t.Fatal("expected ref in allOf[0]")
	}
	second := allOf[1].(map[string]any)
	if second["type"] != "object" {
		t.Fatalf("type = %v", second["type"])
	}
	if _, ok := second["properties"]; !ok {
		t.Fatal("expected properties on allOf[1]")
	}
}

func TestEnrichSchemaObjectTarget_refOnly(t *testing.T) {
	schema := map[string]any{"$ref": "#/components/schemas/BaseDto"}
	target := EnrichSchemaObjectTarget(schema)
	if target["properties"] == nil {
		t.Fatalf("target = %#v", target)
	}
	if schema["$ref"] != nil {
		t.Fatal("$ref must move under allOf")
	}
	allOf, ok := schema["allOf"].([]any)
	if !ok || len(allOf) != 2 {
		t.Fatalf("allOf = %#v", schema["allOf"])
	}
}

func TestEnrichSchemaObjectTarget_allOf(t *testing.T) {
	schema := map[string]any{
		"allOf": []any{
			map[string]any{"$ref": "#/components/schemas/BaseDto"},
			map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
	target := EnrichSchemaObjectTarget(schema)
	props, ok := target["properties"].(map[string]any)
	if !ok {
		t.Fatalf("target = %#v", target)
	}
	props["bar"] = map[string]any{"type": "string"}
	if _, ok := schema["properties"]; ok {
		t.Fatal("properties must not be added at top level of allOf schema")
	}
}
