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

type stubAdapter struct{}

func (stubAdapter) ParamTypeToSchema(string) map[string]any { return map[string]any{"type": "string"} }
func (stubAdapter) IsSimpleType(string) bool                  { return false }
func (stubAdapter) BuildSecuritySchemes(*specmodel.Result) map[string]any {
	return nil
}
func (stubAdapter) FindTypeBySimpleName(map[string]*index.TypeDecl, string) *index.TypeDecl {
	return nil
}
func (stubAdapter) IsFileType(string) bool { return false }
func (stubAdapter) FormParamSchema(string) map[string]any {
	return map[string]any{"type": "string"}
}
