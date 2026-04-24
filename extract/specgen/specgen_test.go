// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package specgen

import (
	"testing"

	"github.com/sailpoint-oss/cartographer/extract/goextract"
)

func newTestNormalizer() *goextract.SchemaNameNormalizer {
	return goextract.NewSchemaNameNormalizer()
}

func newTestErrorAnalyzer() *goextract.ErrorSchemaAnalyzer {
	return goextract.NewErrorSchemaAnalyzer()
}

// --- Phase 1.1: x-experimental / x-internal ---

func TestBuildOperation_ExperimentalPrivate(t *testing.T) {
	tests := []struct {
		name         string
		experimental bool
		private      bool
		wantExpKey   bool
		wantIntKey   bool
	}{
		{"both false", false, false, false, false},
		{"experimental only", true, false, true, false},
		{"private only", false, true, false, true},
		{"both true", true, true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := &goextract.OperationInfo{
				ID:           "testOp",
				Path:         "/test",
				Method:       "GET",
				Experimental: tt.experimental,
				Private:      tt.private,
			}
			result := buildOperation(op, newTestNormalizer(), newTestErrorAnalyzer())

			_, hasExp := result["x-experimental"]
			_, hasInt := result["x-internal"]

			if hasExp != tt.wantExpKey {
				t.Errorf("x-experimental: got present=%v, want present=%v", hasExp, tt.wantExpKey)
			}
			if hasInt != tt.wantIntKey {
				t.Errorf("x-internal: got present=%v, want present=%v", hasInt, tt.wantIntKey)
			}
		})
	}
}

// --- Phase 1.2: Auth types ---

func TestBuildOperation_AuthTypes(t *testing.T) {
	t.Run("user auth", func(t *testing.T) {
		op := &goextract.OperationInfo{
			ID: "userOp", Path: "/test", Method: "GET",
			RequiresAuth: true, UserAuth: true,
			Rights: []string{"read"},
		}
		result := buildOperation(op, newTestNormalizer(), newTestErrorAnalyzer())

		if result["x-auth-type"] != "user" {
			t.Errorf("x-auth-type: got %v, want \"user\"", result["x-auth-type"])
		}
	})

	t.Run("application auth", func(t *testing.T) {
		op := &goextract.OperationInfo{
			ID: "appOp", Path: "/test", Method: "GET",
			RequiresAuth: true, ApplicationAuth: true,
			Rights: []string{"admin"},
		}
		result := buildOperation(op, newTestNormalizer(), newTestErrorAnalyzer())

		if result["x-auth-type"] != "application" {
			t.Errorf("x-auth-type: got %v, want \"application\"", result["x-auth-type"])
		}
	})

	t.Run("unprotected overrides security", func(t *testing.T) {
		op := &goextract.OperationInfo{
			ID: "publicOp", Path: "/test", Method: "GET",
			RequiresAuth: true, Unprotected: true,
			Rights: []string{"read"},
		}
		result := buildOperation(op, newTestNormalizer(), newTestErrorAnalyzer())

		sec, ok := result["security"].([]interface{})
		if !ok {
			t.Fatal("security field missing or wrong type")
		}
		if len(sec) != 0 {
			t.Errorf("security: got %v, want empty array", sec)
		}
	})

	t.Run("no auth type when neither set", func(t *testing.T) {
		op := &goextract.OperationInfo{
			ID: "noAuthOp", Path: "/test", Method: "GET",
		}
		result := buildOperation(op, newTestNormalizer(), newTestErrorAnalyzer())

		if _, ok := result["x-auth-type"]; ok {
			t.Error("x-auth-type should not be present when neither UserAuth nor ApplicationAuth is set")
		}
	})
}

// --- Phase 1.3: Form parameters ---

func TestBuildOperation_FormParams(t *testing.T) {
	op := &goextract.OperationInfo{
		ID: "formOp", Path: "/submit", Method: "POST",
		FormParamDetails: []goextract.OperationParamInfo{
			{Name: "username", Type: "string", Required: true, Description: "The username"},
			{Name: "age", Type: "int", Required: false},
		},
	}
	result := buildOperation(op, newTestNormalizer(), newTestErrorAnalyzer())

	rb, ok := result["requestBody"].(map[string]interface{})
	if !ok {
		t.Fatal("requestBody missing")
	}

	content, ok := rb["content"].(map[string]interface{})
	if !ok {
		t.Fatal("content missing from requestBody")
	}

	formContent, ok := content["application/x-www-form-urlencoded"].(map[string]interface{})
	if !ok {
		t.Fatal("application/x-www-form-urlencoded missing from content")
	}

	schema, ok := formContent["schema"].(map[string]interface{})
	if !ok {
		t.Fatal("schema missing from form content")
	}

	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		t.Fatal("properties missing from form schema")
	}

	if _, ok := props["username"]; !ok {
		t.Error("username property missing")
	}
	if _, ok := props["age"]; !ok {
		t.Error("age property missing")
	}

	req, ok := schema["required"].([]string)
	if !ok {
		t.Fatal("required missing from form schema")
	}
	if len(req) != 1 || req[0] != "username" {
		t.Errorf("required: got %v, want [\"username\"]", req)
	}
}

func TestBuildOperation_FormParamsNotOverrideRequestType(t *testing.T) {
	op := &goextract.OperationInfo{
		ID: "jsonOp", Path: "/submit", Method: "POST",
		RequestType: "CreateRequest",
		FormParamDetails: []goextract.OperationParamInfo{
			{Name: "field", Type: "string"},
		},
	}
	result := buildOperation(op, newTestNormalizer(), newTestErrorAnalyzer())

	rb := result["requestBody"].(map[string]interface{})
	content := rb["content"].(map[string]interface{})

	if _, ok := content["application/x-www-form-urlencoded"]; ok {
		t.Error("form content should not appear when RequestType is set")
	}
	if _, ok := content["application/json"]; !ok {
		t.Error("application/json content should be present when RequestType is set")
	}
}

// --- Phase 1.4: Path parameter descriptions ---

func TestBuildOperation_PathParamDescriptions(t *testing.T) {
	op := &goextract.OperationInfo{
		ID: "paramOp", Path: "/users/{id}", Method: "GET",
		PathParamDetails: []goextract.OperationParamInfo{
			{Name: "id", Type: "string", Description: "The user identifier"},
		},
	}
	result := buildOperation(op, newTestNormalizer(), newTestErrorAnalyzer())

	params, ok := result["parameters"].([]interface{})
	if !ok || len(params) == 0 {
		t.Fatal("parameters missing or empty")
	}

	param := params[0].(map[string]interface{})
	if param["description"] != "The user identifier" {
		t.Errorf("description: got %v, want \"The user identifier\"", param["description"])
	}
}

func TestBuildOperation_PathParamNoDescription(t *testing.T) {
	op := &goextract.OperationInfo{
		ID: "paramOp2", Path: "/items/{itemId}", Method: "GET",
		PathParamDetails: []goextract.OperationParamInfo{
			{Name: "itemId", Type: "string"},
		},
	}
	result := buildOperation(op, newTestNormalizer(), newTestErrorAnalyzer())

	params := result["parameters"].([]interface{})
	param := params[0].(map[string]interface{})
	if _, ok := param["description"]; ok {
		t.Error("description should not be present when empty")
	}
}

// --- Phase 2.2: Schema descriptions ---

func TestBuildSchema_Description(t *testing.T) {
	t.Run("with description", func(t *testing.T) {
		ti := &goextract.TypeInfo{
			Name:        "User",
			Kind:        "struct",
			Description: "User represents a user entity in the system.",
			Fields: []goextract.FieldInfo{
				{Name: "Name", Type: "string", JSONName: "name"},
			},
		}
		schema := buildSchema(ti)

		if schema["description"] != "User represents a user entity in the system." {
			t.Errorf("description: got %v, want the type godoc", schema["description"])
		}
	})

	t.Run("without description", func(t *testing.T) {
		ti := &goextract.TypeInfo{
			Name: "Simple",
			Kind: "struct",
			Fields: []goextract.FieldInfo{
				{Name: "Value", Type: "int", JSONName: "value"},
			},
		}
		schema := buildSchema(ti)

		if _, ok := schema["description"]; ok {
			t.Error("description should not be present when TypeInfo.Description is empty")
		}
	})
}

// --- Phase 3.1: Operation examples ---

func TestBuildOperation_Examples(t *testing.T) {
	op := &goextract.OperationInfo{
		ID: "exOp", Path: "/data", Method: "GET",
		ResponseType: "DataResponse",
		Examples: []goextract.ExampleInfo{
			{Summary: "basic", Value: map[string]interface{}{"id": "123"}},
			{Summary: "full", Value: map[string]interface{}{"id": "456", "name": "test"}},
		},
	}
	result := buildOperation(op, newTestNormalizer(), newTestErrorAnalyzer())

	responses := result["responses"].(map[string]interface{})
	resp200 := responses["200"].(map[string]interface{})
	content := resp200["content"].(map[string]interface{})
	jsonContent := content["application/json"].(map[string]interface{})

	examples, ok := jsonContent["examples"].(map[string]interface{})
	if !ok {
		t.Fatal("examples map missing from response content")
	}
	if len(examples) != 2 {
		t.Errorf("examples count: got %d, want 2", len(examples))
	}
	if _, ok := examples["basic"]; !ok {
		t.Error("example 'basic' missing")
	}
	if _, ok := examples["full"]; !ok {
		t.Error("example 'full' missing")
	}
}

func TestBuildOperation_NoExamplesWhenEmpty(t *testing.T) {
	op := &goextract.OperationInfo{
		ID: "noExOp", Path: "/data", Method: "GET",
		ResponseType: "DataResponse",
	}
	result := buildOperation(op, newTestNormalizer(), newTestErrorAnalyzer())

	responses := result["responses"].(map[string]interface{})
	resp200 := responses["200"].(map[string]interface{})
	content := resp200["content"].(map[string]interface{})
	jsonContent := content["application/json"].(map[string]interface{})

	if _, ok := jsonContent["examples"]; ok {
		t.Error("examples should not be present when op.Examples is empty")
	}
}

// --- Phase 4.1: Request content type ---

func TestBuildOperation_RequestContentType(t *testing.T) {
	t.Run("default json", func(t *testing.T) {
		op := &goextract.OperationInfo{
			ID: "jsonReq", Path: "/upload", Method: "POST",
			RequestType: "Payload",
		}
		result := buildOperation(op, newTestNormalizer(), newTestErrorAnalyzer())

		rb := result["requestBody"].(map[string]interface{})
		content := rb["content"].(map[string]interface{})
		if _, ok := content["application/json"]; !ok {
			t.Error("default content type should be application/json")
		}
	})

	t.Run("custom content type", func(t *testing.T) {
		op := &goextract.OperationInfo{
			ID: "xmlReq", Path: "/upload", Method: "POST",
			RequestType:    "Payload",
			RequestContent: "application/xml",
		}
		result := buildOperation(op, newTestNormalizer(), newTestErrorAnalyzer())

		rb := result["requestBody"].(map[string]interface{})
		content := rb["content"].(map[string]interface{})
		if _, ok := content["application/xml"]; !ok {
			t.Error("content type should be application/xml when RequestContent is set")
		}
		if _, ok := content["application/json"]; ok {
			t.Error("application/json should not be present when custom content type is set")
		}
	})
}

func TestNormalizeOpenAPIPath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "already normalized", in: "/v3/users", want: "/v3/users"},
		{name: "missing slash", in: "v3/users", want: "/v3/users"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeOpenAPIPath(tt.in)
			if got != tt.want {
				t.Fatalf("normalizeOpenAPIPath(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGenerateOpenAPISpec_NormalizesPathKeys(t *testing.T) {
	metadata := &goextract.ExtractedMetadata{
		Operations: map[string]*goextract.OperationInfo{
			"listUsers": {
				ID:     "listUsers",
				Method: "GET",
				Path:   "v3/users",
			},
		},
		Types: map[string]*goextract.TypeInfo{},
	}

	spec := generateOpenAPISpec(metadata, Config{
		Title:          "Test",
		Version:        "1.0.0",
		OpenAPIVersion: "3.2",
	}, newTestNormalizer(), newTestErrorAnalyzer())

	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatal("paths missing")
	}
	if _, ok := paths["/v3/users"]; !ok {
		t.Fatalf("expected normalized path key /v3/users, got keys: %#v", paths)
	}
	if _, ok := paths["v3/users"]; ok {
		t.Fatalf("unexpected non-normalized path key present: %#v", paths)
	}
}
