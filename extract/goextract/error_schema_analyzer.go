// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"
)

// ErrorSchemaAnalyzer analyzes web package error constructors to extract schema information.
type ErrorSchemaAnalyzer struct {
	// Map of function name to analyzed schema
	errorSchemas map[string]*EnhancedErrorSchema

	// Function declarations cache
	funcDeclMap map[string]*ast.FuncDecl
}

// EnhancedErrorSchema contains the schema information extracted from error constructors.
type EnhancedErrorSchema struct {
	// Field constant values (e.g., "locale" -> "en-US")
	ConstantFields map[string]interface{}

	// Field types
	FieldTypes map[string]string

	// Nested object schemas (e.g., "messages" -> ErrorMessage schema)
	NestedSchemas map[string]*EnhancedErrorSchema

	// Example values
	Examples map[string]interface{}
}

// NewErrorSchemaAnalyzer creates a new error schema analyzer.
func NewErrorSchemaAnalyzer() *ErrorSchemaAnalyzer {
	return &ErrorSchemaAnalyzer{
		errorSchemas: make(map[string]*EnhancedErrorSchema),
		funcDeclMap:  make(map[string]*ast.FuncDecl),
	}
}

// AnalyzeWebPackage analyzes the web package to extract error schema information.
func (esa *ErrorSchemaAnalyzer) AnalyzeWebPackage(pkgs []*ast.File, info *types.Info) {
	// Build function declaration cache
	for _, file := range pkgs {
		ast.Inspect(file, func(n ast.Node) bool {
			if funcDecl, ok := n.(*ast.FuncDecl); ok {
				esa.funcDeclMap[funcDecl.Name.Name] = funcDecl
			}
			return true
		})
	}

	// Analyze the newError function
	if newErrorFunc, exists := esa.funcDeclMap["newError"]; exists {
		schema := esa.analyzeNewErrorFunction(newErrorFunc, info)
		esa.errorSchemas["web.Error"] = schema
	}
}

// analyzeNewErrorFunction analyzes the newError constructor to extract schema information.
func (esa *ErrorSchemaAnalyzer) analyzeNewErrorFunction(funcDecl *ast.FuncDecl, info *types.Info) *EnhancedErrorSchema {
	schema := &EnhancedErrorSchema{
		ConstantFields: make(map[string]interface{}),
		FieldTypes:     make(map[string]string),
		NestedSchemas:  make(map[string]*EnhancedErrorSchema),
		Examples:       make(map[string]interface{}),
	}

	// Analyze the function body
	if funcDecl.Body == nil {
		return schema
	}

	// Track struct types being constructed
	messageSchema := &EnhancedErrorSchema{
		ConstantFields: make(map[string]interface{}),
		FieldTypes:     make(map[string]string),
		NestedSchemas:  make(map[string]*EnhancedErrorSchema),
		Examples:       make(map[string]interface{}),
	}

	// Walk through the function body
	ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
		// Look for assignments like: message.Locale = "en-US"
		if assignStmt, ok := n.(*ast.AssignStmt); ok {
			for i, lhs := range assignStmt.Lhs {
				if i >= len(assignStmt.Rhs) {
					continue
				}

				rhs := assignStmt.Rhs[i]

				// Check if it's a selector expression (e.g., message.Field)
				if sel, ok := lhs.(*ast.SelectorExpr); ok {
					fieldName := sel.Sel.Name
					varName := getIdentName(sel.X)

					// Extract the value
					value := esa.extractValue(rhs, info)
					if value != nil {
						// Determine which struct this belongs to
						switch varName {
						case "message":
							messageSchema.ConstantFields[jsonFieldName(fieldName)] = value
						case "e":
							// Check if it's a constant value or a dynamic one
							if isConstantValue(rhs, info) {
								schema.ConstantFields[jsonFieldName(fieldName)] = value
							}
						}
					}
				}
			}
		}

		return true
	})

	// Store the message schema
	if len(messageSchema.ConstantFields) > 0 {
		schema.NestedSchemas["messages"] = messageSchema
	}

	return schema
}

// extractValue extracts the constant value from an expression.
func (esa *ErrorSchemaAnalyzer) extractValue(expr ast.Expr, info *types.Info) interface{} {
	// Handle basic literals
	if lit, ok := expr.(*ast.BasicLit); ok {
		switch lit.Kind {
		case token.STRING:
			// Remove quotes
			return strings.Trim(lit.Value, `"`)
		case token.INT:
			return lit.Value
		}
	}

	// Handle constant values
	if tv, ok := info.Types[expr]; ok {
		if tv.Value != nil {
			switch tv.Value.Kind() {
			case constant.String:
				return constant.StringVal(tv.Value)
			case constant.Int:
				if v, ok := constant.Int64Val(tv.Value); ok {
					return v
				}
			case constant.Bool:
				return constant.BoolVal(tv.Value)
			}
		}
	}

	// Handle function calls like http.StatusText(statusCode)
	if call, ok := expr.(*ast.CallExpr); ok {
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			if x, ok := sel.X.(*ast.Ident); ok {
				// Special case: http.StatusText(statusCode)
				if x.Name == "http" && sel.Sel.Name == "StatusText" {
					return "{{http.StatusText}}" // Placeholder for dynamic status text
				}
			}
		}
	}

	return nil
}

// isConstantValue checks if an expression represents a constant value.
func isConstantValue(expr ast.Expr, info *types.Info) bool {
	// Basic literals are constants
	if _, ok := expr.(*ast.BasicLit); ok {
		return true
	}

	// Check type info for constant values
	if tv, ok := info.Types[expr]; ok {
		return tv.Value != nil
	}

	return false
}

// jsonFieldName converts a Go field name to its JSON equivalent.
func jsonFieldName(fieldName string) string {
	// Convert camelCase to lowercase first letter
	if len(fieldName) == 0 {
		return fieldName
	}

	// Simple heuristic: if starts with capital, lowercase it
	runes := []rune(fieldName)
	runes[0] = []rune(strings.ToLower(string(runes[0])))[0]
	return string(runes)
}

// getIdentName extracts the identifier name from an expression.
func getIdentName(expr ast.Expr) string {
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	return ""
}

// GetErrorSchema retrieves the enhanced error schema for web.Error.
func (esa *ErrorSchemaAnalyzer) GetErrorSchema() *EnhancedErrorSchema {
	return esa.errorSchemas["web.Error"]
}

// BuildEnhancedErrorSpec builds the OpenAPI schema for the framework's error
// shape. Cartographer no longer hard-codes a vendor envelope; it emits a
// stub-marked placeholder, and a downstream overlay (e.g. meridian's
// stubcatalog) resolves "LegacyErrorResponse" into the concrete shape.
func (esa *ErrorSchemaAnalyzer) BuildEnhancedErrorSpec() map[string]interface{} {
	return esa.buildBasicErrorSchema()
}

// buildBasicErrorSchema builds the placeholder shape cartographer emits when
// it cannot describe the framework's error envelope generically.
func (esa *ErrorSchemaAnalyzer) buildBasicErrorSchema() map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": "Error response (resolved by consumer overlay if a catalog is configured)",
		"x-source":    map[string]interface{}{"stubName": "LegacyErrorResponse"},
	}
}

// buildMessageSchema is retained for compatibility with existing callers
// that may expect it on the analyzer surface. It now returns the same
// neutral placeholder as buildBasicErrorSchema.
//
//nolint:unused // referenced for back-compat; safe to remove once callers migrate.
func (esa *ErrorSchemaAnalyzer) buildMessageSchema(*EnhancedErrorSchema) map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": "Error message (resolved by consumer overlay if a catalog is configured)",
		"x-source":    map[string]interface{}{"stubName": "LegacyErrorMessage"},
	}
}
