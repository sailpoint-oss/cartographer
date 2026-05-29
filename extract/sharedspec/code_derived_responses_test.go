package sharedspec

import (
	"testing"

	"github.com/sailpoint-oss/cartographer/extract/index"
	"github.com/sailpoint-oss/cartographer/extract/specmodel"
)

func TestBuildResponsesCodeDerivedOnlyByDefault(t *testing.T) {
	adapter := &stubAdapter{}
	result := &specmodel.Result{
		Operations: []*specmodel.Operation{{
			Path:           "/items",
			Method:         "GET",
			OperationID:    "listItems",
			ResponseType:   "ItemList",
			ResponseStatus: 200,
		}},
	}
	cfg := specmodel.SpecConfig{Title: "Example", Version: "1.0.0"}

	responses := buildResponses(result.Operations[0], result, cfg, adapter)
	for _, code := range []string{"400", "401", "403", "404", "500"} {
		if _, ok := responses[code]; ok {
			t.Errorf("unexpected inferred %s response without code evidence", code)
		}
	}
	if responses["200"] == nil {
		t.Fatal("expected 200 response from return type")
	}
}

func TestBuildResponsesLegacyErrorsOptIn(t *testing.T) {
	adapter := &stubAdapter{}
	result := &specmodel.Result{
		Operations: []*specmodel.Operation{{
			Path:           "/items/{id}",
			Method:         "GET",
			OperationID:    "getItem",
			ResponseType:   "Item",
			ResponseStatus: 200,
			Parameters:     []*specmodel.Parameter{{Name: "id", In: "path", Type: "string"}},
		}},
	}
	cfg := specmodel.SpecConfig{
		Title:       "Example",
		Version:     "1.0.0",
		ErrorSchema: "legacy-error-response",
	}

	responses := buildResponses(result.Operations[0], result, cfg, adapter)
	for _, code := range []string{"400", "401", "403", "404", "500"} {
		if responses[code] == nil {
			t.Errorf("expected legacy %s when opt-in enabled", code)
		}
	}
}

func TestBuildResponsesPreferComponentRefForTypedSuccess(t *testing.T) {
	adapter := &stubAdapter{}
	result := &specmodel.Result{
		Operations: []*specmodel.Operation{{
			Path:           "/items/{id}",
			Method:         "GET",
			OperationID:    "getItem",
			ResponseType:   "Item",
			ResponseStatus: 200,
		}},
		Schemas: map[string]any{
			"Item": map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": map[string]any{"type": "string"}},
			},
		},
	}

	responses := buildResponses(result.Operations[0], result, specmodel.SpecConfig{}, adapter)
	ok200 := responses["200"].(map[string]any)
	content := ok200["content"].(map[string]any)
	jsonContent := content["application/json"].(map[string]any)
	schema := jsonContent["schema"].(map[string]any)
	if schema["$ref"] != "#/components/schemas/Item" {
		t.Fatalf("schema = %#v, want component ref", schema)
	}
}

func TestBuildOperationMergesMultipartFileParamWithDTOBody(t *testing.T) {
	adapter := &stubAdapter{}
	result := &specmodel.Result{
		Schemas: map[string]any{
			"CreateItem": map[string]any{
				"type":       "object",
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			},
		},
	}
	op := &specmodel.Operation{
		Path:                "/items",
		Method:              "POST",
		OperationID:         "createItem",
		RequestBodyType:     "CreateItem",
		ConsumesContentType: "multipart/form-data",
		FormParams: []*specmodel.Parameter{{
			Name:     "upload",
			Type:     "file",
			Required: true,
		}},
	}

	operation := buildOperation(op, result, specmodel.SpecConfig{}, adapter)
	requestBody := operation["requestBody"].(map[string]any)
	content := requestBody["content"].(map[string]any)
	multipart := content["multipart/form-data"].(map[string]any)
	schema := multipart["schema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	upload := props["upload"].(map[string]any)
	if upload["format"] != "binary" {
		t.Fatalf("upload schema = %#v, want binary file field", upload)
	}
}

type stubAdapter struct{}

func (stubAdapter) ParamTypeToSchema(string) map[string]any { return map[string]any{"type": "string"} }
func (stubAdapter) IsSimpleType(string) bool                { return false }
func (stubAdapter) BuildSecuritySchemes(*specmodel.Result) map[string]any {
	return nil
}
func (stubAdapter) FindTypeBySimpleName(map[string]*index.TypeDecl, string) *index.TypeDecl {
	return nil
}
func (stubAdapter) IsFileType(t string) bool { return t == "file" }
func (stubAdapter) FormParamSchema(t string) map[string]any {
	if t == "file" {
		return map[string]any{"type": "string", "format": "binary"}
	}
	return map[string]any{"type": "string"}
}
