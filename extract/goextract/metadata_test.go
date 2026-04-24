// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"testing"
)

func TestNewExtractedMetadata(t *testing.T) {
	m := NewExtractedMetadata()

	if m == nil {
		t.Fatal("Expected non-nil metadata")
	}

	if m.Operations == nil {
		t.Error("Expected operations map to be initialized")
	}

	if m.Types == nil {
		t.Error("Expected types map to be initialized")
	}

	if m.Files == nil {
		t.Error("Expected files slice to be initialized")
	}
}

func TestAddOperation(t *testing.T) {
	m := NewExtractedMetadata()

	op := &OperationInfo{
		ID:     "testOp",
		Path:   "/test",
		Method: "GET",
	}

	err := m.AddOperation(op)
	if err != nil {
		t.Fatalf("Failed to add operation: %v", err)
	}

	retrieved := m.GetOperation("testOp")
	if retrieved == nil {
		t.Fatal("Failed to retrieve added operation")
	}

	if retrieved.Path != "/test" {
		t.Errorf("Expected path '/test', got '%s'", retrieved.Path)
	}
}

func TestAddOperationDuplicate(t *testing.T) {
	m := NewExtractedMetadata()

	op1 := &OperationInfo{
		ID:     "testOp",
		Path:   "/test",
		Method: "GET",
		File:   "file1.go",
		Line:   10,
	}

	op2 := &OperationInfo{
		ID:     "testOp",
		Path:   "/test2",
		Method: "POST",
		File:   "file2.go",
		Line:   20,
	}

	err := m.AddOperation(op1)
	if err != nil {
		t.Fatalf("Failed to add first operation: %v", err)
	}

	err = m.AddOperation(op2)
	if err == nil {
		t.Error("Expected error when adding duplicate operation ID")
	}
}

func TestAddOperationMissingID(t *testing.T) {
	m := NewExtractedMetadata()

	op := &OperationInfo{
		Path:   "/test",
		Method: "GET",
	}

	err := m.AddOperation(op)
	if err == nil {
		t.Error("Expected error when adding operation without ID")
	}
}

func TestMergeMetadata(t *testing.T) {
	m1 := NewExtractedMetadata()
	m1.AddOperation(&OperationInfo{
		ID:      "op1",
		Path:    "/test1",
		Method:  "GET",
		Summary: "Original summary",
	})

	m2 := NewExtractedMetadata()
	m2.AddOperation(&OperationInfo{
		ID:          "op1",
		Description: "New description",
		Tags:        []string{"tag1"},
	})
	m2.AddOperation(&OperationInfo{
		ID:     "op2",
		Path:   "/test2",
		Method: "POST",
	})

	err := m1.Merge(m2)
	if err != nil {
		t.Fatalf("Merge failed: %v", err)
	}

	// Check that op1 was merged
	op1 := m1.GetOperation("op1")
	if op1 == nil {
		t.Fatal("op1 not found after merge")
	}

	if op1.Summary != "Original summary" {
		t.Errorf("Summary should be preserved: got '%s'", op1.Summary)
	}

	if op1.Description != "New description" {
		t.Errorf("Description should be updated: got '%s'", op1.Description)
	}

	if len(op1.Tags) != 1 || op1.Tags[0] != "tag1" {
		t.Errorf("Tags should be updated: got %v", op1.Tags)
	}

	// Check that op2 was added
	op2 := m1.GetOperation("op2")
	if op2 == nil {
		t.Fatal("op2 not found after merge")
	}

	if op2.Path != "/test2" {
		t.Errorf("Expected path '/test2', got '%s'", op2.Path)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name      string
		operation *OperationInfo
		wantError bool
	}{
		{
			name: "valid operation",
			operation: &OperationInfo{
				ID:     "valid",
				Path:   "/test",
				Method: "GET",
			},
			wantError: false,
		},
		{
			name: "missing path",
			operation: &OperationInfo{
				ID:     "noPat",
				Method: "GET",
			},
			wantError: true,
		},
		{
			name: "missing method",
			operation: &OperationInfo{
				ID:   "noMethod",
				Path: "/test",
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewExtractedMetadata()
			m.AddOperation(tt.operation)

			err := m.Validate()
			if (err != nil) != tt.wantError {
				t.Errorf("Validate() error = %v, wantError %v", err, tt.wantError)
			}
		})
	}
}

func TestJSONSerialization(t *testing.T) {
	m := NewExtractedMetadata()
	m.Package = "test/package"
	m.AddOperation(&OperationInfo{
		ID:     "testOp",
		Path:   "/test",
		Method: "GET",
	})

	// Serialize to JSON
	data, err := m.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// Deserialize from JSON
	m2, err := FromJSON(data)
	if err != nil {
		t.Fatalf("FromJSON failed: %v", err)
	}

	// Verify
	if m2.Package != "test/package" {
		t.Errorf("Package mismatch: expected 'test/package', got '%s'", m2.Package)
	}

	op := m2.GetOperation("testOp")
	if op == nil {
		t.Fatal("Operation not found after deserialization")
	}

	if op.Path != "/test" {
		t.Errorf("Path mismatch: expected '/test', got '%s'", op.Path)
	}
}

func TestSimplifyTypeName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"github.com/sailpoint/pkg.Type", "Type"},
		{"Type", "Type"},
		{"pkg.Type", "Type"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := SimplifyTypeName(tt.input)
			if result != tt.expected {
				t.Errorf("SimplifyTypeName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
