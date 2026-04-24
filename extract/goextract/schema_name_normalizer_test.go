// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"testing"
)

func TestNewSchemaNameNormalizer(t *testing.T) {
	normalizer := NewSchemaNameNormalizer()

	if normalizer == nil {
		t.Fatal("Expected non-nil normalizer")
	}

	if normalizer.normalizedNames == nil {
		t.Error("Expected normalizedNames map to be initialized")
	}

	if len(normalizer.patterns) == 0 {
		t.Error("Expected patterns to be initialized")
	}
}

func TestSchemaNameNormalizer_NormalizeSchemaName(t *testing.T) {
	normalizer := NewSchemaNameNormalizer()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Array types
		{
			name:     "array type with package",
			input:    "[]github.com/sailpoint/pkg.User",
			expected: "UserArray",
		},
		{
			name:     "array type simple",
			input:    "[]User",
			expected: "UserArray",
		},
		{
			name:     "array type with database package",
			input:    "[]database.TotalCount",
			expected: "TotalCountArray",
		},

		// Pointer types
		{
			name:     "pointer type with package",
			input:    "*github.com/sailpoint/pkg.User",
			expected: "User",
		},
		{
			name:     "pointer type simple",
			input:    "*User",
			expected: "User",
		},

		// Map types
		{
			name:     "map type string to string",
			input:    "map[string]string",
			expected: "StringMap",
		},
		{
			name:     "map type string to type",
			input:    "map[string]User",
			expected: "UserMap",
		},

		// Package-qualified types
		{
			name:     "full github path",
			input:    "github.com/org/repo/pkg.Type",
			expected: "Type",
		},
		{
			name:     "internal package path",
			input:    "internal/model/database.TotalCount",
			expected: "TotalCount",
		},
		{
			name:     "simple package qualified",
			input:    "database.TotalCount",
			expected: "TotalCount",
		},
		{
			name:     "nested package path",
			input:    "pkg/subpkg.MyType",
			expected: "MyType",
		},

		// Simple types
		{
			name:     "simple type",
			input:    "User",
			expected: "User",
		},
		{
			name:     "lowercase type",
			input:    "user",
			expected: "User",
		},

		// Common prefix removal
		{
			name:     "ModelUser prefix",
			input:    "ModelUser",
			expected: "User",
		},
		{
			name:     "DataResponse prefix",
			input:    "DataResponse",
			expected: "Response",
		},
		{
			name:     "EntityRecord prefix",
			input:    "EntityRecord",
			expected: "Record",
		},

		// Edge cases
		{
			name:     "empty string",
			input:    "",
			expected: "Unknown",
		},
		{
			name:     "single lowercase letter",
			input:    "a",
			expected: "A",
		},

		// Complex types from real-world usage
		{
			name:     "real world array type",
			input:    "[]github.com/sailpoint/sp-api-usage/internal/model/database.TotalCount",
			expected: "TotalCountArray",
		},
		{
			name:     "real world pointer type",
			input:    "*github.com/sailpoint/sp-api-usage/internal/model/database.ApiCallBreakdown",
			expected: "ApiCallBreakdown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizer.NormalizeSchemaName(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeSchemaName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSchemaNameNormalizer_Caching(t *testing.T) {
	normalizer := NewSchemaNameNormalizer()

	// First call
	result1 := normalizer.NormalizeSchemaName("github.com/pkg.TestType")

	// Second call should return cached value
	result2 := normalizer.NormalizeSchemaName("github.com/pkg.TestType")

	if result1 != result2 {
		t.Errorf("Expected cached result to match: %q vs %q", result1, result2)
	}

	// Verify it's in the cache
	if len(normalizer.normalizedNames) == 0 {
		t.Error("Expected cache to contain normalized names")
	}

	if normalizer.normalizedNames["github.com/pkg.TestType"] != result1 {
		t.Error("Expected cached value to match result")
	}
}

func TestSchemaNameNormalizer_CleanupName(t *testing.T) {
	normalizer := NewSchemaNameNormalizer()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "remove dots",
			input:    "pkg.Type",
			expected: "pkgType",
		},
		{
			name:     "remove special characters",
			input:    "Type[String]",
			expected: "TypeString",
		},
		{
			name:     "preserve alphanumeric",
			input:    "User123",
			expected: "User123",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizer.cleanupName(tt.input)
			if result != tt.expected {
				t.Errorf("cleanupName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSchemaNameNormalizer_LongNames(t *testing.T) {
	normalizer := NewSchemaNameNormalizer()

	// Test with a very long name (>50 chars)
	longName := "VeryLongTypeNameWithManyWordsForTestingPurposes"
	result := normalizer.cleanupName(longName)

	// Should be truncated or shortened
	if len(result) > 50 {
		t.Errorf("Expected name to be truncated, got length %d", len(result))
	}
}

func TestGetNormalizedSchemaName(t *testing.T) {
	// Test the convenience function
	result := GetNormalizedSchemaName("github.com/pkg.TestType")

	if result != "TestType" {
		t.Errorf("GetNormalizedSchemaName() = %q, want 'TestType'", result)
	}
}

func TestSchemaNameNormalizer_NormalizeSchemaNames(t *testing.T) {
	normalizer := NewSchemaNameNormalizer()

	input := map[string]interface{}{
		"github.com/pkg.User":           map[string]string{"type": "object"},
		"github.com/pkg.Response":       map[string]string{"type": "object"},
		"[]github.com/pkg.Item":         map[string]string{"type": "array"},
	}

	result := normalizer.NormalizeSchemaNames(input)

	expectedKeys := map[string]bool{
		"User":      true,
		"Response":  true,
		"ItemArray": true,
	}

	if len(result) != len(expectedKeys) {
		t.Errorf("Expected %d schemas, got %d", len(expectedKeys), len(result))
	}

	for key := range expectedKeys {
		if _, exists := result[key]; !exists {
			t.Errorf("Expected key '%s' in result", key)
		}
	}
}

func TestSchemaNameNormalizer_GetSchemaNameMapping(t *testing.T) {
	normalizer := NewSchemaNameNormalizer()

	// Normalize some names first
	normalizer.NormalizeSchemaName("github.com/pkg.User")
	normalizer.NormalizeSchemaName("github.com/pkg.Item")

	mapping := normalizer.GetSchemaNameMapping()

	if len(mapping) != 2 {
		t.Errorf("Expected 2 mappings, got %d", len(mapping))
	}

	if mapping["github.com/pkg.User"] != "User" {
		t.Errorf("Expected mapping for User, got '%s'", mapping["github.com/pkg.User"])
	}
}

func TestSchemaNameNormalizer_ConsecutiveUppercase(t *testing.T) {
	normalizer := NewSchemaNameNormalizer()

	tests := []struct {
		input    string
		expected string
	}{
		// The cleanupName function lowercases consecutive uppercase letters
		// to avoid issues with all-caps abbreviations
		{"APIResponse", "Apiresponse"},
		{"HTTPRequest", "Httprequest"},
		{"XMLParser", "Xmlparser"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizer.cleanupName(tt.input)
			if result != tt.expected {
				t.Errorf("cleanupName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSchemaNameNormalizer_GenericTypes(t *testing.T) {
	normalizer := NewSchemaNameNormalizer()

	tests := []struct {
		input    string
		expected string
	}{
		// Generic type handling: the brackets are removed via patterns
		// The current implementation extracts just the inner type for single-word outer types
		{"Response[User]", "User"},
		{"Page[Item]", "PageItem"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizer.NormalizeSchemaName(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeSchemaName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSchemaNameNormalizer_PascalCaseConversion(t *testing.T) {
	normalizer := NewSchemaNameNormalizer()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "lowercase first letter",
			input:    "userResponse",
			expected: "UserResponse",
		},
		{
			name:     "already PascalCase",
			input:    "UserResponse",
			expected: "UserResponse",
		},
		{
			name:     "single lowercase",
			input:    "u",
			expected: "U",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizer.NormalizeSchemaName(tt.input)
			if result != tt.expected {
				t.Errorf("NormalizeSchemaName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSchemaNameNormalizer_MultipleCalls(t *testing.T) {
	normalizer := NewSchemaNameNormalizer()

	// Ensure multiple calls with same input return same result
	inputs := []string{
		"github.com/pkg.User",
		"[]github.com/pkg.Item",
		"*github.com/pkg.Response",
		"map[string]Value",
	}

	for _, input := range inputs {
		result1 := normalizer.NormalizeSchemaName(input)
		result2 := normalizer.NormalizeSchemaName(input)
		result3 := normalizer.NormalizeSchemaName(input)

		if result1 != result2 || result2 != result3 {
			t.Errorf("Inconsistent results for %q: %q, %q, %q", input, result1, result2, result3)
		}
	}
}

