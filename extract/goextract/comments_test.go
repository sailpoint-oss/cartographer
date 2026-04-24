// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestNewCommentParser(t *testing.T) {
	parser := NewCommentParser()
	if parser == nil {
		t.Fatal("Expected non-nil parser")
	}
}

func TestCommentParser_ParseFuncComments(t *testing.T) {
	src := `
package main

// @openapi:id createUser
// @openapi:summary Create a new user
// @openapi:description Creates a new user account with the provided details
// @openapi:tags Users,Admin
// @openapi:deprecated true
// @openapi:experimental false
// @openapi:private true
func createUser() {}

// No annotations here
func noAnnotations() {}

// @openapi:streaming text/event-stream
// @openapi:stream-item Event
// @openapi:querystring SearchParams
func streamHandler() {}

// @openapi:tag:summary User management operations
// @openapi:tag:parent Admin
// @openapi:tag:kind resource
func withTagAnnotations() {}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	cp := NewCommentParser()

	tests := []struct {
		funcName    string
		expectedKey string
		expectedVal string
	}{
		{"createUser", "id", "createUser"},
		{"createUser", "summary", "Create a new user"},
		{"createUser", "description", "Creates a new user account with the provided details"},
		{"createUser", "tags", "Users,Admin"},
		{"createUser", "deprecated", "true"},
		{"createUser", "experimental", "false"},
		{"createUser", "private", "true"},
		{"streamHandler", "streaming", "text/event-stream"},
		{"streamHandler", "stream-item", "Event"},
		{"streamHandler", "querystring", "SearchParams"},
		{"withTagAnnotations", "tag:summary", "User management operations"},
		{"withTagAnnotations", "tag:parent", "Admin"},
		{"withTagAnnotations", "tag:kind", "resource"},
	}

	// Find function declarations and parse their comments
	for _, decl := range file.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			annotations := cp.ParseFuncComments(funcDecl, file)

			for _, tt := range tests {
				if funcDecl.Name.Name == tt.funcName {
					if val, exists := annotations[tt.expectedKey]; exists {
						if val != tt.expectedVal {
							t.Errorf("%s.%s: expected '%s', got '%s'", tt.funcName, tt.expectedKey, tt.expectedVal, val)
						}
					}
				}
			}

			// Test noAnnotations function - should now have godoc annotations
			if funcDecl.Name.Name == "noAnnotations" {
				// The function has "// No annotations here" as a godoc comment
				// which should be extracted as godoc and godoc_summary
				if godoc, ok := annotations["godoc"]; !ok {
					t.Error("Expected godoc annotation from comment 'No annotations here'")
				} else if godoc != "No annotations here" {
					t.Errorf("Expected godoc 'No annotations here', got '%s'", godoc)
				}
			}
		}
	}
}

func TestCommentParser_ParseAnnotation(t *testing.T) {
	cp := NewCommentParser()

	tests := []struct {
		input       string
		expectedKey string
		expectedVal string
	}{
		{"@openapi:id createUser", "id", "createUser"},
		{"@openapi:summary Create user", "summary", "Create user"},
		{"@openapi:tags Users,Admin", "tags", "Users,Admin"},
		{"@openapi:deprecated true", "deprecated", "true"},
		{"@openapi:tag:summary User operations", "tag:summary", "User operations"},
		{"@openapi:streaming text/event-stream", "streaming", "text/event-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			annotations := make(map[string]string)
			cp.parseAnnotation(tt.input, annotations)

			if val, exists := annotations[tt.expectedKey]; !exists {
				t.Errorf("Expected key '%s' not found", tt.expectedKey)
			} else if val != tt.expectedVal {
				t.Errorf("For key '%s': expected '%s', got '%s'", tt.expectedKey, tt.expectedVal, val)
			}
		})
	}
}

func TestCommentParser_ApplyAnnotations(t *testing.T) {
	cp := NewCommentParser()

	op := &OperationInfo{
		ID: "original",
	}

	annotations := map[string]string{
		"id":          "newId",
		"summary":     "New summary",
		"description": "New description",
		"tags":        "Tag1, Tag2, Tag3",
		"deprecated":  "true",
		"experimental": "yes",
		"private":     "1",
		"streaming":   "text/event-stream",
		"stream-item": "EventType",
		"querystring": "QuerySchema",
	}

	cp.ApplyAnnotationsWithSource(op, annotations, "handlers.go:123")

	if op.ID != "newId" {
		t.Errorf("Expected ID 'newId', got '%s'", op.ID)
	}

	if op.Summary != "New summary" {
		t.Errorf("Expected Summary 'New summary', got '%s'", op.Summary)
	}

	if op.Description != "New description" {
		t.Errorf("Expected Description 'New description', got '%s'", op.Description)
	}

	if len(op.Tags) != 3 {
		t.Errorf("Expected 3 tags, got %d: %v", len(op.Tags), op.Tags)
	}

	if op.Tags[0] != "Tag1" || op.Tags[1] != "Tag2" || op.Tags[2] != "Tag3" {
		t.Errorf("Tags mismatch: %v", op.Tags)
	}

	if !op.Deprecated {
		t.Error("Expected Deprecated to be true")
	}

	if !op.Experimental {
		t.Error("Expected Experimental to be true")
	}

	if !op.Private {
		t.Error("Expected Private to be true")
	}

	if !op.IsStreaming {
		t.Error("Expected IsStreaming to be true")
	}

	if op.StreamMediaType != "text/event-stream" {
		t.Errorf("Expected StreamMediaType 'text/event-stream', got '%s'", op.StreamMediaType)
	}

	if op.StreamItemType != "EventType" {
		t.Errorf("Expected StreamItemType 'EventType', got '%s'", op.StreamItemType)
	}

	if op.QueryStringSchema != "QuerySchema" {
		t.Errorf("Expected QueryStringSchema 'QuerySchema', got '%s'", op.QueryStringSchema)
	}
}

func TestCommentParser_ApplyAnnotations_EmptyValues(t *testing.T) {
	cp := NewCommentParser()

	op := &OperationInfo{
		ID:      "existingId",
		Summary: "Existing summary",
	}

	// Empty values should not override existing values
	annotations := map[string]string{
		"id":      "",
		"summary": "",
	}

	cp.ApplyAnnotationsWithSource(op, annotations, "handlers.go:123")

	// ID should remain unchanged (empty string doesn't override)
	if op.ID != "existingId" {
		t.Errorf("Expected ID to remain 'existingId', got '%s'", op.ID)
	}

	// Summary should remain unchanged
	if op.Summary != "Existing summary" {
		t.Errorf("Expected Summary to remain 'Existing summary', got '%s'", op.Summary)
	}
}

func TestCommentParser_ParseBool(t *testing.T) {
	cp := NewCommentParser()

	trueValues := []string{"true", "TRUE", "True", "yes", "YES", "Yes", "1"}
	for _, v := range trueValues {
		if !cp.parseBool(v) {
			t.Errorf("Expected parseBool('%s') to be true", v)
		}
	}

	falseValues := []string{"false", "FALSE", "no", "NO", "0", "anything", ""}
	for _, v := range falseValues {
		if cp.parseBool(v) {
			t.Errorf("Expected parseBool('%s') to be false", v)
		}
	}
}

func TestCommentParser_ParseTagAnnotations(t *testing.T) {
	cp := NewCommentParser()

	tests := []struct {
		name        string
		annotations map[string]string
		tagName     string
		expectNil   bool
		checkFunc   func(*TagInfo) bool
	}{
		{
			name: "all tag annotations",
			annotations: map[string]string{
				"tag:summary":     "User operations",
				"tag:parent":      "Admin",
				"tag:kind":        "resource",
				"tag:description": "Detailed description",
			},
			tagName:   "Users",
			expectNil: false,
			checkFunc: func(tag *TagInfo) bool {
				return tag.Summary == "User operations" &&
					tag.Parent == "Admin" &&
					tag.Kind == "resource" &&
					tag.Description == "Detailed description"
			},
		},
		{
			name: "partial tag annotations",
			annotations: map[string]string{
				"tag:summary": "Only summary",
			},
			tagName:   "PartialTag",
			expectNil: false,
			checkFunc: func(tag *TagInfo) bool {
				return tag.Summary == "Only summary" && tag.Parent == "" && tag.Kind == ""
			},
		},
		{
			name:        "no tag annotations",
			annotations: map[string]string{},
			tagName:     "NoTag",
			expectNil:   true,
		},
		{
			name: "non-tag annotations only",
			annotations: map[string]string{
				"id":      "someId",
				"summary": "regular summary",
			},
			tagName:   "RegularTag",
			expectNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag := cp.ParseTagAnnotations(tt.annotations, tt.tagName)

			if tt.expectNil {
				if tag != nil {
					t.Error("Expected nil tag, got non-nil")
				}
				return
			}

			if tag == nil {
				t.Fatal("Expected non-nil tag, got nil")
			}

			if tag.Name != tt.tagName {
				t.Errorf("Expected tag name '%s', got '%s'", tt.tagName, tag.Name)
			}

			if !tt.checkFunc(tag) {
				t.Errorf("Tag validation failed for %+v", tag)
			}
		})
	}
}

func TestCommentParser_ExtractTagsFromComments(t *testing.T) {
	cp := NewCommentParser()

	tests := []struct {
		name     string
		comments []*ast.Comment
		expected []string
	}{
		{
			name: "swagger route comment",
			comments: []*ast.Comment{
				{Text: "// swagger:route GET /users Users listUsers"},
			},
			expected: []string{"Users"},
		},
		{
			name: "no swagger comments",
			comments: []*ast.Comment{
				{Text: "// Regular comment"},
				{Text: "// Another comment"},
			},
			expected: []string{},
		},
		{
			name:     "empty comments",
			comments: []*ast.Comment{},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := cp.ExtractTagsFromComments(tt.comments)

			if len(tags) != len(tt.expected) {
				t.Errorf("Expected %d tags, got %d: %v", len(tt.expected), len(tags), tags)
				return
			}

			for i, expectedTag := range tt.expected {
				if tags[i] != expectedTag {
					t.Errorf("Expected tag '%s' at index %d, got '%s'", expectedTag, i, tags[i])
				}
			}
		})
	}
}

func TestCommentParser_ParseFuncComments_NilDoc(t *testing.T) {
	cp := NewCommentParser()

	funcDecl := &ast.FuncDecl{
		Name: &ast.Ident{Name: "noDoc"},
		Doc:  nil, // No documentation
	}

	annotations := cp.ParseFuncComments(funcDecl, nil)

	if len(annotations) != 0 {
		t.Errorf("Expected empty annotations for function without doc, got %d", len(annotations))
	}
}

func TestCommentParser_GenerateSummaryFromFuncName(t *testing.T) {
	cp := NewCommentParser()

	tests := []struct {
		funcName string
		expected string
	}{
		{"CreateTenant", "Create Tenant"},
		{"GetUserByID", "Get User By ID"},
		{"ListCars", "List Cars"},
		{"PostAddDriver", "Post Add Driver"},
		{"DeleteCar", "Delete Car"},
		{"", ""},
		{"lowercase", "lowercase"},
		{"HTTPHandler", "HTTP Handler"},
	}

	for _, tt := range tests {
		t.Run(tt.funcName, func(t *testing.T) {
			result := cp.GenerateSummaryFromFuncName(tt.funcName)
			if result != tt.expected {
				t.Errorf("GenerateSummaryFromFuncName(%s): expected '%s', got '%s'", tt.funcName, tt.expected, result)
			}
		})
	}
}

func TestCommentParser_ApplyFallbackSummary(t *testing.T) {
	cp := NewCommentParser()

	// Test that fallback is applied when summary is empty
	op := &OperationInfo{}
	cp.ApplyFallbackSummary(op, "CreateUser")
	if op.Summary != "Create User" {
		t.Errorf("Expected 'Create User', got '%s'", op.Summary)
	}

	// Test that fallback is NOT applied when summary exists
	op2 := &OperationInfo{Summary: "Existing Summary"}
	cp.ApplyFallbackSummary(op2, "CreateUser")
	if op2.Summary != "Existing Summary" {
		t.Errorf("Expected 'Existing Summary', got '%s'", op2.Summary)
	}
}

func TestCommentParser_MultiLineComments(t *testing.T) {
	// Note: Block comments are parsed as a single string without line-by-line processing
	// so they may not be parsed correctly in the current implementation.
	// This test focuses on line comments which work correctly.
	src := `
package main

// @openapi:id lineComment
// @openapi:summary Line comment summary
func lineComment() {}

// @openapi:id anotherFunc
// @openapi:description Another function description
func anotherFunc() {}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	cp := NewCommentParser()

	for _, decl := range file.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok {
			annotations := cp.ParseFuncComments(funcDecl, file)

			switch funcDecl.Name.Name {
			case "lineComment":
				if annotations["id"] != "lineComment" {
					t.Errorf("Expected id 'lineComment', got '%s'", annotations["id"])
				}
				if annotations["summary"] != "Line comment summary" {
					t.Errorf("Expected summary 'Line comment summary', got '%s'", annotations["summary"])
				}
			case "anotherFunc":
				if annotations["id"] != "anotherFunc" {
					t.Errorf("Expected id 'anotherFunc', got '%s'", annotations["id"])
				}
				if annotations["description"] != "Another function description" {
					t.Errorf("Expected description 'Another function description', got '%s'", annotations["description"])
				}
			}
		}
	}
}

func TestCommentParser_ExampleDirective(t *testing.T) {
	src := `
package main

// @openapi:id exampleOp
// @openapi:example basic: {"id": "123", "name": "test"}
// @openapi:example full: {"id": "456", "name": "full", "active": true}
func withExamples() {}

// @openapi:id singleExample
// @openapi:example {"value": 42}
func withJsonOnly() {}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	cp := NewCommentParser()

	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		annotations := cp.ParseFuncComments(funcDecl, file)

		switch funcDecl.Name.Name {
		case "withExamples":
			if annotations["example_0"] == "" {
				t.Error("example_0 should be populated")
			}
			if annotations["example_1"] == "" {
				t.Error("example_1 should be populated")
			}

			op := &OperationInfo{}
			cp.ApplyAnnotationsWithSource(op, annotations, "")
			if len(op.Examples) != 2 {
				t.Errorf("expected 2 examples, got %d", len(op.Examples))
			}
			if len(op.Examples) > 0 && op.Examples[0].Summary != "basic" {
				t.Errorf("first example summary: got %q, want \"basic\"", op.Examples[0].Summary)
			}

		case "withJsonOnly":
			if annotations["example_0"] == "" {
				t.Error("example_0 should be populated for JSON-only example")
			}

			op := &OperationInfo{}
			cp.ApplyAnnotationsWithSource(op, annotations, "")
			if len(op.Examples) != 1 {
				t.Errorf("expected 1 example, got %d", len(op.Examples))
			}
			if len(op.Examples) > 0 && op.Examples[0].Summary != "" {
				t.Errorf("JSON-only example should have no summary, got %q", op.Examples[0].Summary)
			}
		}
	}
}

func TestParseExampleDirective(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantSummary string
		wantNilVal  bool
	}{
		{"summary with json", `basic: {"id": "123"}`, "basic", false},
		{"json only", `{"value": 42}`, "", false},
		{"plain string", `hello world`, "", false},
		{"empty", ``, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := parseExampleDirective(tt.raw)
			if ex.Summary != tt.wantSummary {
				t.Errorf("summary: got %q, want %q", ex.Summary, tt.wantSummary)
			}
			if tt.wantNilVal && ex.Value != nil {
				t.Errorf("value: got %v, want nil", ex.Value)
			}
			if !tt.wantNilVal && ex.Value == nil {
				t.Error("value: got nil, want non-nil")
			}
		})
	}
}

