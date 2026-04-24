// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"testing"
)

func TestNew(t *testing.T) {
	cfg := Config{
		PackagePatterns: []string{"./..."},
		Verbose:         false,
		IncludeTests:    false,
	}

	extractor := New(cfg)

	if extractor == nil {
		t.Fatal("Expected non-nil extractor")
	}

	if extractor.metadata == nil {
		t.Error("Expected metadata to be initialized")
	}

	if extractor.fset == nil {
		t.Error("Expected fset to be initialized")
	}

	if extractor.handlerInfoCache == nil {
		t.Error("Expected handlerInfoCache to be initialized")
	}

	if extractor.routerAnalyzer == nil {
		t.Error("Expected routerAnalyzer to be initialized")
	}

	if extractor.handlerAnalyzer == nil {
		t.Error("Expected handlerAnalyzer to be initialized")
	}

	if extractor.commentParser == nil {
		t.Error("Expected commentParser to be initialized")
	}

	if extractor.functionTracer == nil {
		t.Error("Expected functionTracer to be initialized")
	}

	if extractor.errorSchemaAnalyzer == nil {
		t.Error("Expected errorSchemaAnalyzer to be initialized")
	}

	if extractor.schemaNameNormalizer == nil {
		t.Error("Expected schemaNameNormalizer to be initialized")
	}
}

func TestExtractor_HandlerInfoCacheIsolation(t *testing.T) {
	// Test that each extractor instance has its own cache
	cfg := Config{
		PackagePatterns: []string{"."},
		Verbose:         false,
	}

	ext1 := New(cfg)
	ext2 := New(cfg)

	// Store in first extractor's cache
	ext1.storeHandlerInfo("handler1", &HandlerInfo{
		RequestType: "TestRequest1",
	}, map[string]string{"id": "op1"}, "")

	// Store in second extractor's cache
	ext2.storeHandlerInfo("handler2", &HandlerInfo{
		RequestType: "TestRequest2",
	}, map[string]string{"id": "op2"}, "")

	// Verify isolation
	if ext1.getHandlerInfo("handler2") != nil {
		t.Error("ext1 should not have handler2 in its cache")
	}

	if ext2.getHandlerInfo("handler1") != nil {
		t.Error("ext2 should not have handler1 in its cache")
	}

	// Verify each extractor has its own handlers
	h1 := ext1.getHandlerInfo("handler1")
	if h1 == nil {
		t.Fatal("ext1 should have handler1")
	}
	if h1.info.RequestType != "TestRequest1" {
		t.Errorf("Expected RequestType 'TestRequest1', got '%s'", h1.info.RequestType)
	}

	h2 := ext2.getHandlerInfo("handler2")
	if h2 == nil {
		t.Fatal("ext2 should have handler2")
	}
	if h2.info.RequestType != "TestRequest2" {
		t.Errorf("Expected RequestType 'TestRequest2', got '%s'", h2.info.RequestType)
	}
}

func TestExtractor_ClearCache(t *testing.T) {
	cfg := Config{
		PackagePatterns: []string{"."},
	}

	ext := New(cfg)

	// Store some data
	ext.storeHandlerInfo("test", &HandlerInfo{
		RequestType: "TestRequest",
	}, nil, "")

	// Verify it's there
	if ext.getHandlerInfo("test") == nil {
		t.Fatal("Expected handler to be in cache")
	}

	// Clear cache
	ext.ClearCache()

	// Verify it's gone
	if ext.getHandlerInfo("test") != nil {
		t.Error("Cache should be empty after ClearCache()")
	}
}

func TestExtractor_StoreAndGetHandlerInfo(t *testing.T) {
	cfg := Config{}
	ext := New(cfg)

	// Store handler info with all fields
	handlerInfo := &HandlerInfo{
		RequestType:   "CreateUserRequest",
		ResponseType:  "UserResponse",
		ResponseStatus: 201,
		ErrorCodes:    []int{400, 500},
	}
	comments := map[string]string{
		"id":          "createUser",
		"summary":     "Create a new user",
		"description": "Creates a new user account",
		"tags":        "Users",
	}

	ext.storeHandlerInfo("createUser", handlerInfo, comments, "")

	// Retrieve and verify
	cached := ext.getHandlerInfo("createUser")
	if cached == nil {
		t.Fatal("Expected to retrieve cached handler info")
	}

	if cached.info.RequestType != "CreateUserRequest" {
		t.Errorf("Expected RequestType 'CreateUserRequest', got '%s'", cached.info.RequestType)
	}

	if cached.info.ResponseType != "UserResponse" {
		t.Errorf("Expected ResponseType 'UserResponse', got '%s'", cached.info.ResponseType)
	}

	if cached.info.ResponseStatus != 201 {
		t.Errorf("Expected ResponseStatus 201, got %d", cached.info.ResponseStatus)
	}

	if cached.comments["id"] != "createUser" {
		t.Errorf("Expected comment id 'createUser', got '%s'", cached.comments["id"])
	}
}

func TestExtractor_StoreHandlerInfoWithNilInfo(t *testing.T) {
	cfg := Config{}
	ext := New(cfg)

	// Store only comments (handler info is nil)
	comments := map[string]string{
		"id":      "publicEndpoint",
		"summary": "Public endpoint",
	}

	ext.storeHandlerInfo("publicEndpoint", nil, comments, "")

	cached := ext.getHandlerInfo("publicEndpoint")
	if cached == nil {
		t.Fatal("Expected to retrieve cached handler info")
	}

	if cached.info != nil {
		t.Error("Expected info to be nil")
	}

	if cached.comments["id"] != "publicEndpoint" {
		t.Errorf("Expected comment id 'publicEndpoint', got '%s'", cached.comments["id"])
	}
}

func TestParseStructTags(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:  "json tag only",
			input: "`json:\"userName\"`",
			expected: map[string]string{
				"json": "userName",
			},
		},
		{
			name:  "json tag with omitempty",
			input: "`json:\"id,omitempty\"`",
			expected: map[string]string{
				"json": "id",
			},
		},
		{
			name:  "multiple tags",
			input: "`json:\"email\" validate:\"required,email\" description:\"User email address\"`",
			expected: map[string]string{
				"json":        "email",
				"validate":    "required,email",
				"description": "User email address",
			},
		},
		{
			name:  "json ignored field",
			input: "`json:\"-\"`",
			expected: map[string]string{
				// json:"-" should be skipped
			},
		},
		{
			name:  "example tag",
			input: "`json:\"age\" example:\"25\"`",
			expected: map[string]string{
				"json":    "age",
				"example": "25",
			},
		},
		{
			name:     "empty string",
			input:    "",
			expected: map[string]string{},
		},
		{
			name:     "empty backticks",
			input:    "``",
			expected: map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseStructTags(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d tags, got %d: %v", len(tt.expected), len(result), result)
				return
			}

			for key, expectedValue := range tt.expected {
				if actualValue, ok := result[key]; !ok {
					t.Errorf("Missing tag '%s'", key)
				} else if actualValue != expectedValue {
					t.Errorf("Tag '%s': expected '%s', got '%s'", key, expectedValue, actualValue)
				}
			}
		})
	}
}

func TestIsRelativePath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{".", true},
		{"..", true},
		{"./internal", true},
		{"../parent", true},
		{"internal", true},
		{"internal/api", true},
		{"pkg/models", true},
		{"/absolute/path", false},
		{"C:/windows/path", false},
		{"github.com/user/repo", false},
		{"example.com/module", false},
		{"golang.org/x/tools", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := isRelativePath(tt.path)
			if result != tt.expected {
				t.Errorf("isRelativePath(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestMergeHandlerInfo(t *testing.T) {
	cfg := Config{}
	ext := New(cfg)

	// Create base operation
	op := &OperationInfo{
		ID:     "testOp",
		Path:   "/test",
		Method: "POST",
	}

	// Create cached handler info
	cached := &cachedHandlerInfo{
		info: &HandlerInfo{
			RequestType:    "CreateRequest",
			ResponseType:   "CreateResponse",
			ResponseStatus: 201,
			ErrorCodes:     []int{400, 500},
			ErrorResponses: []ErrorResponseInfo{
				{StatusCode: 400, ErrorMessage: "Invalid input"},
			},
			SuccessResponses: []SuccessResponseInfo{
				{StatusCode: 201, ResponseType: "CreateResponse"},
			},
		},
		comments: map[string]string{
			"id":          "createResource",
			"summary":     "Create resource",
			"description": "Creates a new resource",
			"tags":        "Resources",
		},
	}

	ext.mergeHandlerInfo(op, cached)

	// Verify merged values
	if op.RequestType != "CreateRequest" {
		t.Errorf("Expected RequestType 'CreateRequest', got '%s'", op.RequestType)
	}

	if op.ResponseType != "CreateResponse" {
		t.Errorf("Expected ResponseType 'CreateResponse', got '%s'", op.ResponseType)
	}

	if op.ResponseStatus != 201 {
		t.Errorf("Expected ResponseStatus 201, got %d", op.ResponseStatus)
	}

	if len(op.PossibleErrors) != 2 {
		t.Errorf("Expected 2 possible errors, got %d", len(op.PossibleErrors))
	}

	if len(op.ErrorResponses) != 1 {
		t.Errorf("Expected 1 error response, got %d", len(op.ErrorResponses))
	}

	if len(op.SuccessResponses) != 1 {
		t.Errorf("Expected 1 success response, got %d", len(op.SuccessResponses))
	}

	// Verify comment annotations were applied
	if op.ID != "createResource" {
		t.Errorf("Expected ID 'createResource', got '%s'", op.ID)
	}

	if op.Summary != "Create resource" {
		t.Errorf("Expected Summary 'Create resource', got '%s'", op.Summary)
	}
}

func TestMergeHandlerInfo_NilCached(t *testing.T) {
	cfg := Config{}
	ext := New(cfg)

	op := &OperationInfo{
		ID:     "testOp",
		Path:   "/test",
		Method: "GET",
	}

	// Should not panic when cached is nil
	ext.mergeHandlerInfo(op, nil)

	// Operation should be unchanged
	if op.ID != "testOp" {
		t.Errorf("Expected ID 'testOp', got '%s'", op.ID)
	}
}

func TestMergeHandlerInfo_PartialInfo(t *testing.T) {
	cfg := Config{}
	ext := New(cfg)

	op := &OperationInfo{
		ID:     "testOp",
		Path:   "/test",
		Method: "GET",
	}

	// Cached with only comments, no handler info
	cached := &cachedHandlerInfo{
		info: nil,
		comments: map[string]string{
			"summary": "Test endpoint",
		},
	}

	ext.mergeHandlerInfo(op, cached)

	// Should still apply comments
	if op.Summary != "Test endpoint" {
		t.Errorf("Expected Summary 'Test endpoint', got '%s'", op.Summary)
	}

	// Request/Response should remain empty
	if op.RequestType != "" {
		t.Errorf("Expected empty RequestType, got '%s'", op.RequestType)
	}
}

func TestExtractor_GetEnhancedErrorSchema(t *testing.T) {
	cfg := Config{}
	ext := New(cfg)

	schema := ext.GetEnhancedErrorSchema()
	if schema == nil {
		t.Fatal("Expected non-nil error schema")
	}

	// Should have basic structure
	if _, ok := schema["type"]; !ok {
		t.Error("Expected 'type' field in error schema")
	}

	if _, ok := schema["properties"]; !ok {
		t.Error("Expected 'properties' field in error schema")
	}
}

func TestExtractor_GetSchemaNameNormalizer(t *testing.T) {
	cfg := Config{}
	ext := New(cfg)

	normalizer := ext.GetSchemaNameNormalizer()
	if normalizer == nil {
		t.Fatal("Expected non-nil schema name normalizer")
	}

	// Verify it works
	result := normalizer.NormalizeSchemaName("github.com/pkg.TestType")
	if result != "TestType" {
		t.Errorf("Expected 'TestType', got '%s'", result)
	}
}

func TestExtractor_GetErrorSchemaAnalyzer(t *testing.T) {
	cfg := Config{}
	ext := New(cfg)

	analyzer := ext.GetErrorSchemaAnalyzer()
	if analyzer == nil {
		t.Fatal("Expected non-nil error schema analyzer")
	}
}

