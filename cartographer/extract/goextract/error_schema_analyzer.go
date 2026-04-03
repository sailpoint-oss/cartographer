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
						if varName == "message" {
							messageSchema.ConstantFields[jsonFieldName(fieldName)] = value
						} else if varName == "e" {
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

// BuildEnhancedErrorSpec builds an enhanced OpenAPI schema spec with const values.
func (esa *ErrorSchemaAnalyzer) BuildEnhancedErrorSpec() map[string]interface{} {
	schema := esa.GetErrorSchema()
	if schema == nil {
		// Return basic schema if analysis failed
		return esa.buildBasicErrorSchema()
	}

	// Build the Error schema
	errorSchema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"detailCode": map[string]interface{}{
				"type":        "string",
				"description": "HTTP status text (e.g., 'Bad Request', 'Internal Server Error')",
				"example":     "Internal Server Error",
			},
			"trackingId": map[string]interface{}{
				"type":        "string",
				"description": "Unique identifier for tracing the request",
				"example":     "12345-67890-abcde-fghij",
			},
			"messages": map[string]interface{}{
				"type":        "array",
				"description": "Array of localized error messages",
				"items":       esa.buildMessageSchema(schema),
			},
		},
		"required": []string{"detailCode", "messages"},
	}

	return errorSchema
}

// buildMessageSchema builds the ErrorMessage schema with const values from analysis.
func (esa *ErrorSchemaAnalyzer) buildMessageSchema(errorSchema *EnhancedErrorSchema) map[string]interface{} {
	messageSchema, hasNested := errorSchema.NestedSchemas["messages"]

	properties := map[string]interface{}{
		"locale": map[string]interface{}{
			"type":        "string",
			"description": "Locale of the message",
		},
		"localeOrigin": map[string]interface{}{
			"type":        "string",
			"description": "Origin of the locale",
		},
		"text": map[string]interface{}{
			"type":        "string",
			"description": "The error message text",
		},
	}

	// Add const values if we extracted them
	if hasNested && messageSchema != nil {
		if locale, ok := messageSchema.ConstantFields["locale"].(string); ok {
			properties["locale"].(map[string]interface{})["const"] = locale
			properties["locale"].(map[string]interface{})["default"] = locale
		}
		if localeOrigin, ok := messageSchema.ConstantFields["localeOrigin"].(string); ok {
			properties["localeOrigin"].(map[string]interface{})["const"] = localeOrigin
			properties["localeOrigin"].(map[string]interface{})["default"] = localeOrigin
		}
	}

	return map[string]interface{}{
		"type":       "object",
		"properties": properties,
		"required":   []string{"locale", "text"},
	}
}

// buildBasicErrorSchema builds a basic error schema without const values.
func (esa *ErrorSchemaAnalyzer) buildBasicErrorSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"detailCode": map[string]interface{}{
				"type":        "string",
				"description": "Error detail code",
			},
			"trackingId": map[string]interface{}{
				"type":        "string",
				"description": "Request tracking ID",
			},
			"messages": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"locale": map[string]interface{}{
							"type": "string",
						},
						"localeOrigin": map[string]interface{}{
							"type": "string",
						},
						"text": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
		},
	}
}
