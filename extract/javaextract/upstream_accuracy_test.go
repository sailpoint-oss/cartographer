package javaextract

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sailpoint-oss/cartographer/extract/extractionopts"
	"github.com/sailpoint-oss/cartographer/extract/specmodel"
	"github.com/sailpoint-oss/cartographer/extract/sharedspec"
)

func upstreamFixtureDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "testdata", "java-upstream", "com", "example")
}

func extractUpstreamFixture(t *testing.T, opts extractionopts.Options) *Result {
	t.Helper()
	dir, err := filepath.Abs(upstreamFixtureDir(t))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{dir},
		Extraction: opts,
	})
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return result
}

func opByMethodPath(t *testing.T, result *Result, method, path string) *Operation {
	t.Helper()
	for _, op := range result.Operations {
		if op.Method == method && op.Path == path {
			return op
		}
	}
	t.Fatalf("operation %s %s not found; have %d ops", method, path, len(result.Operations))
	return nil
}

func TestInterfaceControllerExtraction(t *testing.T) {
	result := extractUpstreamFixture(t, extractionopts.Options{})

	list := opByMethodPath(t, result, "GET", "/api/v1/orders")
	if list.OperationID == "" {
		t.Error("expected operationId on interface GET")
	}
	if list.ResponseType == "" {
		t.Error("expected response type on interface GET")
	}

	create := opByMethodPath(t, result, "POST", "/api/v1/orders")
	if create.RequestBodyType == "" {
		t.Errorf("expected request body type on interface POST, got %q", create.RequestBodyType)
	}
}

func TestNestedInnerControllerExtraction(t *testing.T) {
	result := extractUpstreamFixture(t, extractionopts.Options{})

	op := opByMethodPath(t, result, "GET", "/inner/items")
	if op.Summary == "" && op.OperationID == "" {
		t.Error("expected metadata on nested inner controller operation")
	}
}

func TestDelegatedResponseHeaderTracing(t *testing.T) {
	result := extractUpstreamFixture(t, extractionopts.Options{})

	op := opByMethodPath(t, result, "GET", "/api/v1/catalog/items")
	if op.ResponseHeaders == nil {
		t.Fatal("expected response headers from superclass helper")
	}
	if _, ok := op.ResponseHeaders["X-Total-Count"]; !ok {
		t.Errorf("expected X-Total-Count from delegated helper, got %v", op.ResponseHeaders)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Catalog API", Version: "1.0.0"})
	paths := spec["paths"].(map[string]any)
	item := paths["/api/v1/catalog/items"].(map[string]any)
	getOp := item["get"].(map[string]any)
	responses := getOp["responses"].(map[string]any)
	ok200 := responses["200"].(map[string]any)
	headers := ok200["headers"].(map[string]any)
	if headers["X-Total-Count"] == nil {
		t.Error("expected X-Total-Count on OpenAPI 200 response headers")
	}
}

func TestControllerAdviceTypedErrorSchema(t *testing.T) {
	result := extractUpstreamFixture(t, extractionopts.Options{})

	op := opByMethodPath(t, result, "GET", "/api/v1/widgets/{id}")
	if len(op.ErrorResponseSchemas) == 0 {
		t.Fatal("expected typed error schema from @ControllerAdvice")
	}
	found := false
	for _, e := range op.ErrorResponseSchemas {
		if e.StatusCode == 404 && e.SchemaType == "ApiErrorDto" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 404 ApiErrorDto error mapping, got %#v", op.ErrorResponseSchemas)
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Widget API", Version: "1.0.0"})
	paths := spec["paths"].(map[string]any)
	widget := paths["/api/v1/widgets/{id}"].(map[string]any)
	getOp := widget["get"].(map[string]any)
	responses := getOp["responses"].(map[string]any)
	resp404 := responses["404"].(map[string]any)
	content := resp404["content"].(map[string]any)
	jsonContent := content["application/json"].(map[string]any)
	schema := jsonContent["schema"].(map[string]any)
	if ref, ok := schema["$ref"].(string); ok && ref != "" {
		if ref != "#/components/schemas/ApiErrorDto" {
			t.Errorf("404 schema $ref = %q, want ApiErrorDto", ref)
		}
	} else if schema["properties"] == nil {
		t.Fatalf("404 schema = %#v, want $ref or inline ApiErrorDto properties", schema)
	}
}

func TestValidRequestBodyConstraintCascade(t *testing.T) {
	result := extractUpstreamFixture(t, extractionopts.Options{})

	op := opByMethodPath(t, result, "POST", "/api/v1/widgets")
	if op.RequestBodyType == "" {
		t.Fatal("expected request body type")
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Widget API", Version: "1.0.0"})
	paths := spec["paths"].(map[string]any)
	widget := paths["/api/v1/widgets"].(map[string]any)
	postOp := widget["post"].(map[string]any)
	reqBody := postOp["requestBody"].(map[string]any)
	content := reqBody["content"].(map[string]any)
	jsonContent := content["application/json"].(map[string]any)
	schema := jsonContent["schema"].(map[string]any)
	required, _ := schema["required"].([]any)
	if len(required) == 0 {
		t.Fatal("expected required fields on @Valid request body schema")
	}
	if required[0] != "name" {
		t.Errorf("required = %v, want [name]", required)
	}
}

func TestScopedAnnotationMapping(t *testing.T) {
	result := extractUpstreamFixture(t, extractionopts.Options{})

	op := opByMethodPath(t, result, "GET", "/api/v1/scoped/entries")
	if op.Path != "/api/v1/scoped/entries" {
		t.Errorf("path = %q", op.Path)
	}
}

func TestDiagnosticsMetadata(t *testing.T) {
	result := extractUpstreamFixture(t, extractionopts.Options{})

	if result.Diagnostics == nil {
		t.Fatal("expected diagnostics map")
	}
	if result.Diagnostics["controllers"] == nil {
		t.Error("expected controllers count in diagnostics")
	}
	if result.Diagnostics["operations"] == nil {
		t.Error("expected operations count in diagnostics")
	}
	if got, ok := result.Diagnostics["operations"].(int); !ok || got != len(result.Operations) {
		t.Errorf("operations diagnostic = %v, want %d", result.Diagnostics["operations"], len(result.Operations))
	}

	spec := GenerateSpec(result, SpecConfig{Title: "Upstream Fixture", Version: "1.0.0"})
	info := spec["info"].(map[string]any)
	diag := info["x-cartographer-diagnostics"].(map[string]any)
	if diag["controllers"] == nil || diag["operations"] == nil {
		t.Errorf("expected diagnostics on spec info, got %v", diag)
	}
}

func TestNoDefaultErrorsWithoutLegacySchema(t *testing.T) {
	result := extractUpstreamFixture(t, extractionopts.Options{})

	unified := result.ToUnifiedResult()
	spec := sharedspec.GenerateSpec(unified, specmodel.SpecConfig{
		Title:   "Upstream Fixture",
		Version: "1.0.0",
	}, &javaAdapter{})

	paths := spec["paths"].(map[string]any)
	for path, rawItem := range paths {
		item := rawItem.(map[string]any)
		for method, rawOp := range item {
			if method == "parameters" {
				continue
			}
			op := rawOp.(map[string]any)
			responses := op["responses"].(map[string]any)
			for _, code := range []string{"400", "401", "403", "500"} {
				if _, ok := responses[code]; ok {
					t.Errorf("%s %s: unexpected default %s without legacy-error-response", method, path, code)
				}
			}
		}
	}

	comps := spec["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)
	if _, ok := schemas["ErrorResponse"]; ok {
		t.Error("ErrorResponse component should not exist without legacy-error-response opt-in")
	}
}

func TestLegacyErrorSchemaOptIn(t *testing.T) {
	result := extractUpstreamFixture(t, extractionopts.Options{})

	spec := GenerateSpec(result, SpecConfig{
		Title:       "Upstream Fixture",
		Version:     "1.0.0",
		ErrorSchema: "legacy-error-response",
	})
	comps := spec["components"].(map[string]any)
	schemas := comps["schemas"].(map[string]any)
	if schemas["ErrorResponse"] == nil {
		t.Fatal("expected ErrorResponse component with legacy-error-response")
	}

	paths := spec["paths"].(map[string]any)
	item := paths["/api/v1/orders"].(map[string]any)
	getOp := item["get"].(map[string]any)
	responses := getOp["responses"].(map[string]any)
	if responses["400"] == nil {
		t.Error("expected legacy 400 response when opt-in enabled")
	}
}

func TestSignaturePaginationTypeExpansion(t *testing.T) {
	src := `package com.example;
public class QueryPagingOptions {
    private int offset;
    private int limit;
}
@Path("/api/v1/records")
public class RecordResource {
    @GET
    public java.util.List<String> list(QueryPagingOptions paging) {
        return null;
    }
}`
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "RecordResource.java"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := Extract(Config{
		RootDir:    dir,
		SourceDirs: []string{filepath.Join(dir, "src")},
		Extraction: extractionopts.Options{
			SignaturePaginationTypes: []string{"QueryPagingOptions"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	op := opByMethodPath(t, result, "GET", "/api/v1/records")
	names := map[string]bool{}
	for _, p := range op.Parameters {
		names[p.Name] = true
	}
	if !names["offset"] || !names["limit"] {
		t.Errorf("expected offset/limit from signature pagination type, got %v", names)
	}
}
