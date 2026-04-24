// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"regexp"
	"strings"
)

// SchemaNameNormalizer normalizes Go type names to clean OpenAPI schema names.
type SchemaNameNormalizer struct {
	// Cache for normalized names to avoid re-processing
	normalizedNames map[string]string

	// Patterns for common Go type transformations
	patterns []namePattern
}

// namePattern represents a pattern for transforming type names.
type namePattern struct {
	regex   *regexp.Regexp
	replace string
}

// NewSchemaNameNormalizer creates a new schema name normalizer.
func NewSchemaNameNormalizer() *SchemaNameNormalizer {
	return &SchemaNameNormalizer{
		normalizedNames: make(map[string]string),
		patterns: []namePattern{
			// Array types: []Type -> TypeArray (but preserve package qualification for now)
			{
				regex:   regexp.MustCompile(`^\[\](.+)$`),
				replace: "${1}Array",
			},
			// Pointer types: *Type -> Type (but preserve package qualification for now)
			{
				regex:   regexp.MustCompile(`^\*(.+)$`),
				replace: "${1}",
			},
			// Map types: map[string]Type -> TypeMap (but preserve package qualification for now)
			{
				regex:   regexp.MustCompile(`^map\[string\](.+)$`),
				replace: "${1}Map",
			},
			// Generic types: Type[T] -> TypeT
			{
				regex:   regexp.MustCompile(`^(.+)\[([^]]+)\]$`),
				replace: "${1}${2}",
			},
			// Full package paths: github.com/org/repo/pkg.Type -> Type
			{
				regex:   regexp.MustCompile(`^[^/]+/[^/]+/[^/]+/([^/]+/)*([^.]+)\.([^.]+)$`),
				replace: "${3}",
			},
			// Internal package paths: internal/model/database.Type -> Type
			{
				regex:   regexp.MustCompile(`^internal/[^/]+/[^/]+\.([^.]+)$`),
				replace: "${1}",
			},
			// Package-qualified types with slashes: pkg/subpkg.Type -> Type
			{
				regex:   regexp.MustCompile(`^[^/]+/([^/]+/)*([^.]+)\.([^.]+)$`),
				replace: "${3}",
			},
			// Simple package-qualified types: package.Type -> Type
			{
				regex:   regexp.MustCompile(`^([^/.]+)\.([^.]+)$`),
				replace: "${2}",
			},
			// Remove common prefixes
			{
				regex:   regexp.MustCompile(`^(Model|Data|Entity|Response|Request|Result)([A-Z])`),
				replace: "${2}",
			},
			// Note: PascalCase conversion is handled in NormalizeSchemaName() after pattern application
			// because regex replace strings are evaluated at compile time, not runtime.
		},
	}
}

// NormalizeSchemaName converts a Go type name to a clean OpenAPI schema name.
func (snn *SchemaNameNormalizer) NormalizeSchemaName(typeName string) string {
	// Check cache first
	if normalized, exists := snn.normalizedNames[typeName]; exists {
		return normalized
	}

	originalName := typeName
	normalized := typeName

	// Apply patterns in order
	for _, pattern := range snn.patterns {
		if pattern.regex.MatchString(normalized) {
			normalized = pattern.regex.ReplaceAllString(normalized, pattern.replace)
		}
	}

	// Additional cleanup
	normalized = snn.cleanupName(normalized)

	// Ensure it's not empty and starts with uppercase
	if normalized == "" {
		normalized = "Unknown"
	}
	if len(normalized) > 0 && normalized[0] >= 'a' && normalized[0] <= 'z' {
		normalized = strings.ToUpper(string(normalized[0])) + normalized[1:]
	}

	// Cache the result
	snn.normalizedNames[originalName] = normalized

	return normalized
}

// cleanupName performs additional cleanup on the normalized name.
func (snn *SchemaNameNormalizer) cleanupName(name string) string {
	// Remove any remaining dots or special characters
	name = regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(name, "")

	// Handle multiple consecutive uppercase letters
	name = regexp.MustCompile(`([A-Z])([A-Z]+)`).ReplaceAllStringFunc(name, func(match string) string {
		if len(match) > 2 {
			// Keep first letter, lowercase the rest
			return string(match[0]) + strings.ToLower(match[1:])
		}
		return match
	})

	// Ensure it's not too long (OpenAPI schema names should be reasonable)
	if len(name) > 50 {
		// Truncate but try to keep it meaningful
		words := regexp.MustCompile(`([A-Z][a-z]*)`).FindAllString(name, -1)
		if len(words) > 1 {
			// Keep first few words
			result := ""
			for i, word := range words {
				if i >= 3 || len(result)+len(word) > 40 {
					break
				}
				result += word
			}
			if result != "" {
				name = result
			}
		}
	}

	return name
}

// GetNormalizedSchemaName is a convenience method that creates a normalizer and normalizes a name.
func GetNormalizedSchemaName(typeName string) string {
	normalizer := NewSchemaNameNormalizer()
	return normalizer.NormalizeSchemaName(typeName)
}

// NormalizeSchemaNames normalizes a map of schema names.
func (snn *SchemaNameNormalizer) NormalizeSchemaNames(schemas map[string]interface{}) map[string]interface{} {
	normalized := make(map[string]interface{})

	for originalName, schema := range schemas {
		normalizedName := snn.NormalizeSchemaName(originalName)
		normalized[normalizedName] = schema
	}

	return normalized
}

// GetSchemaNameMapping returns a mapping of original names to normalized names.
func (snn *SchemaNameNormalizer) GetSchemaNameMapping() map[string]string {
	return snn.normalizedNames
}

// Examples of transformations:
// "[]github.com/sailpoint/sp-api-usage/internal/model/database.TotalCount" -> "TotalCountArray"
// "[]database.TotalCount" -> "TotalCountArray"
// "*github.com/sailpoint/sp-api-usage/internal/model/database.ApiCallBreakdown" -> "ApiCallBreakdown"
// "*database.ApiCallBreakdown" -> "ApiCallBreakdown"
// "map[string]string" -> "StringMap"
// "github.com/org/repo/pkg.User" -> "User"
// "internal/model/database.TotalCount" -> "TotalCount"
// "database.TotalCount" -> "TotalCount"
// "ModelUser" -> "User"
// "dataResponse" -> "DataResponse"
