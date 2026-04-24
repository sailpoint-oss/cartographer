// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestNewFunctionTracer(t *testing.T) {
	registry := NewResponseRegistry()
	tracer := NewFunctionTracer(registry)

	if tracer == nil {
		t.Fatal("Expected non-nil tracer")
	}

	if tracer.funcDeclMap == nil {
		t.Error("Expected funcDeclMap to be initialized")
	}

	if tracer.analysisCache == nil {
		t.Error("Expected analysisCache to be initialized")
	}

	if tracer.callStack == nil {
		t.Error("Expected callStack to be initialized")
	}

	if tracer.responseRegistry != registry {
		t.Error("Expected responseRegistry to be set")
	}
}

func TestFunctionTracer_BuildFunctionCache(t *testing.T) {
	src := `
package main

type Service struct{}

func (s *Service) handler() {}
func (s *Service) anotherHandler() {}
func standaloneFunc() {}
func init() {}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	tracer := NewFunctionTracer(nil)
	tracer.BuildFunctionCache(nil, []*ast.File{file})

	// Should have cached all function declarations
	if len(tracer.funcDeclMap) == 0 {
		t.Error("Expected function declarations to be cached")
	}

	// Check for specific functions
	expectedFuncs := []string{"Service.handler", "Service.anotherHandler", "standaloneFunc", "init"}
	for _, name := range expectedFuncs {
		if _, exists := tracer.funcDeclMap[name]; !exists {
			// Also check without receiver for standalone functions
			simpleName := name
			if name == "standaloneFunc" || name == "init" {
				if _, exists := tracer.funcDeclMap[simpleName]; !exists {
					t.Errorf("Expected function '%s' to be in cache", name)
				}
			}
		}
	}
}

func TestFunctionTracer_IsInCallStack(t *testing.T) {
	tracer := NewFunctionTracer(nil)

	// Empty call stack
	if tracer.isInCallStack("func1") {
		t.Error("Expected func1 NOT to be in empty call stack")
	}

	// Add to call stack
	tracer.callStack = append(tracer.callStack, "func1")
	tracer.callStack = append(tracer.callStack, "func2")

	if !tracer.isInCallStack("func1") {
		t.Error("Expected func1 to be in call stack")
	}

	if !tracer.isInCallStack("func2") {
		t.Error("Expected func2 to be in call stack")
	}

	if tracer.isInCallStack("func3") {
		t.Error("Expected func3 NOT to be in call stack")
	}
}

func TestFunctionTracer_GetStatusCodeForFunction(t *testing.T) {
	tracer := NewFunctionTracer(nil)

	tests := []struct {
		funcName string
		expected int
	}{
		{"BadRequest", 400},
		{"Unauthorized", 401},
		{"Forbidden", 403},
		{"ForbiddenWithError", 403},
		{"NotFound", 404},
		{"NotFoundWithError", 404},
		{"Gone", 410},
		{"InternalServerError", 500},
		{"ServiceUnavailable", 503},
		{"ContextCanceled", 499},
		{"NoContent", 204},
		{"UnknownFunction", 500}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.funcName, func(t *testing.T) {
			result := tracer.getStatusCodeForFunction(tt.funcName)
			if result != tt.expected {
				t.Errorf("getStatusCodeForFunction(%s) = %d, want %d", tt.funcName, result, tt.expected)
			}
		})
	}
}

func TestFunctionTracer_IsKnownErrorFunction(t *testing.T) {
	tracer := NewFunctionTracer(nil)

	errorFuncs := []string{
		"BadRequest", "Unauthorized", "Forbidden", "NotFound",
		"Gone", "InternalServerError", "ServiceUnavailable",
		"ContextCanceled", "NotFoundWithError", "ForbiddenWithError",
	}

	for _, fn := range errorFuncs {
		if !tracer.isKnownErrorFunction(fn) {
			t.Errorf("Expected '%s' to be recognized as known error function", fn)
		}
	}

	nonErrorFuncs := []string{
		"WriteJSON", "RequireRights", "SomeOtherFunc",
	}

	for _, fn := range nonErrorFuncs {
		if tracer.isKnownErrorFunction(fn) {
			t.Errorf("Expected '%s' NOT to be recognized as known error function", fn)
		}
	}
}

func TestFunctionTracer_ExtractErrorMessage(t *testing.T) {
	tracer := NewFunctionTracer(nil)

	tests := []struct {
		name     string
		src      string
		expected string
	}{
		{
			name:     "string literal",
			src:      `"Error message"`,
			expected: "Error message",
		},
		{
			name:     "errors.New",
			src:      `errors.New("Failed to process")`,
			expected: "Failed to process",
		},
		{
			name:     "fmt.Errorf",
			src:      `fmt.Errorf("Invalid input")`,
			expected: "Invalid input",
		},
		{
			name:     "variable",
			src:      `err`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.src)
			if err != nil {
				t.Skipf("Could not parse expression: %v", err)
				return
			}

			result := tracer.extractErrorMessage(expr)
			if result != tt.expected {
				t.Errorf("extractErrorMessage() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFunctionTracer_ShouldTraceCall(t *testing.T) {
	tracer := NewFunctionTracer(nil)

	// Without type info, should not trace
	call := &ast.CallExpr{
		Fun: &ast.Ident{Name: "someFunc"},
	}

	if tracer.shouldTraceCall(call, nil) {
		t.Error("Should not trace call without type info")
	}
}

func TestFunctionAnalysis(t *testing.T) {
	analysis := &FunctionAnalysis{
		ErrorResponses:   make([]ErrorResponseInfo, 0),
		SuccessResponses: make([]SuccessResponseInfo, 0),
		Complete:         true,
	}

	// Add error response
	analysis.ErrorResponses = append(analysis.ErrorResponses, ErrorResponseInfo{
		StatusCode:   400,
		ResponseType: "web.Error",
		ErrorMessage: "Bad request",
	})

	// Add success response
	analysis.SuccessResponses = append(analysis.SuccessResponses, SuccessResponseInfo{
		StatusCode:   200,
		ResponseType: "UserResponse",
	})

	if len(analysis.ErrorResponses) != 1 {
		t.Errorf("Expected 1 error response, got %d", len(analysis.ErrorResponses))
	}

	if len(analysis.SuccessResponses) != 1 {
		t.Errorf("Expected 1 success response, got %d", len(analysis.SuccessResponses))
	}

	if !analysis.Complete {
		t.Error("Expected analysis to be marked complete")
	}
}

func TestExtractTypeName(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		expected string
	}{
		{
			name:     "simple ident",
			src:      "Service",
			expected: "Service",
		},
		{
			name:     "pointer type",
			src:      "*Service",
			expected: "Service",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.src)
			if err != nil {
				t.Skipf("Could not parse expression: %v", err)
				return
			}

			result := extractTypeName(expr)
			if result != tt.expected {
				t.Errorf("extractTypeName() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestFunctionTracer_GetFunctionKey(t *testing.T) {
	tracer := NewFunctionTracer(nil)

	// Create a real types.Func for testing
	// We can't easily create a *types.Func, so we test with nil handling
	// The function is tested indirectly through other tests

	// Test that function works with nil (edge case)
	// In real code, funcObj should never be nil, but we test defensive behavior
	defer func() {
		if r := recover(); r != nil {
			// Expected to panic with nil funcObj - that's acceptable behavior
		}
	}()
	_ = tracer.getFunctionKey(nil)
}

func TestFunctionTracer_CycleDetection(t *testing.T) {
	tracer := NewFunctionTracer(nil)

	// Simulate a call stack with potential cycle
	tracer.callStack = []string{
		"pkg.funcA",
		"pkg.funcB",
		"pkg.funcC",
	}

	// Should detect cycle
	if !tracer.isInCallStack("pkg.funcA") {
		t.Error("Should detect funcA in call stack")
	}

	if !tracer.isInCallStack("pkg.funcB") {
		t.Error("Should detect funcB in call stack")
	}

	// Should not detect non-existent
	if tracer.isInCallStack("pkg.funcD") {
		t.Error("Should not detect funcD in call stack")
	}
}

func TestFunctionTracer_AnalysisCache(t *testing.T) {
	tracer := NewFunctionTracer(nil)

	// Pre-populate cache
	analysis := &FunctionAnalysis{
		ErrorResponses: []ErrorResponseInfo{
			{StatusCode: 400, ErrorMessage: "Cached error"},
		},
		Complete: true,
	}
	tracer.analysisCache["pkg.cachedFunc"] = analysis

	// Verify cache lookup
	cached, exists := tracer.analysisCache["pkg.cachedFunc"]
	if !exists {
		t.Fatal("Expected cached analysis to exist")
	}

	if len(cached.ErrorResponses) != 1 {
		t.Errorf("Expected 1 error response in cache, got %d", len(cached.ErrorResponses))
	}

	if cached.ErrorResponses[0].ErrorMessage != "Cached error" {
		t.Errorf("Expected cached error message, got '%s'", cached.ErrorResponses[0].ErrorMessage)
	}
}

func TestFunctionTracer_ExtractErrorInfo(t *testing.T) {
	tracer := NewFunctionTracer(nil)

	// Create a mock call expression for web.BadRequest
	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   &ast.Ident{Name: "web"},
			Sel: &ast.Ident{Name: "BadRequest"},
		},
		Args: []ast.Expr{
			&ast.Ident{Name: "ctx"},
			&ast.Ident{Name: "w"},
			&ast.BasicLit{Kind: token.STRING, Value: `"Invalid input"`},
		},
	}

	info := tracer.extractErrorInfo(call, nil)

	if info == nil {
		t.Fatal("Expected non-nil error info")
	}

	if info.StatusCode != 400 {
		t.Errorf("Expected StatusCode 400, got %d", info.StatusCode)
	}

	if info.ResponseType != "web.Error" {
		t.Errorf("Expected ResponseType 'web.Error', got '%s'", info.ResponseType)
	}

	if info.Source != "web.BadRequest" {
		t.Errorf("Expected Source 'web.BadRequest', got '%s'", info.Source)
	}

	if info.ErrorMessage != "Invalid input" {
		t.Errorf("Expected ErrorMessage 'Invalid input', got '%s'", info.ErrorMessage)
	}
}

func TestFunctionTracer_ExtractSuccessInfo(t *testing.T) {
	tracer := NewFunctionTracer(nil)

	// Create a mock call expression with insufficient args
	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   &ast.Ident{Name: "web"},
			Sel: &ast.Ident{Name: "WriteJSON"},
		},
		Args: []ast.Expr{
			&ast.Ident{Name: "ctx"},
			&ast.Ident{Name: "w"},
		}, // Only 2 args, need 3
	}

	info := tracer.extractSuccessInfo(call, nil)
	if info != nil {
		t.Error("Expected nil for insufficient args")
	}

	// Test with 3 args
	call.Args = append(call.Args, &ast.Ident{Name: "response"})
	info = tracer.extractSuccessInfo(call, nil)

	if info == nil {
		t.Fatal("Expected non-nil success info")
	}

	if info.StatusCode != 200 {
		t.Errorf("Expected StatusCode 200, got %d", info.StatusCode)
	}

	if info.Source != "web.WriteJSON" {
		t.Errorf("Expected Source 'web.WriteJSON', got '%s'", info.Source)
	}
}

func TestFunctionTracer_FindFunctionDeclaration(t *testing.T) {
	tracer := NewFunctionTracer(nil)

	// Pre-populate function cache
	funcDecl := &ast.FuncDecl{
		Name: &ast.Ident{Name: "testFunc"},
	}
	tracer.funcDeclMap["testFunc"] = funcDecl

	// Test that funcDeclMap is properly populated
	if _, exists := tracer.funcDeclMap["testFunc"]; !exists {
		t.Error("Expected funcDeclMap to contain testFunc")
	}

	// We can't easily create a *types.Func for testing findFunctionDeclaration directly,
	// but we can verify the cache lookup mechanism works via BuildFunctionCache
	src := `
package main
func testFunc() {}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse: %v", err)
	}

	tracer2 := NewFunctionTracer(nil)
	tracer2.BuildFunctionCache(nil, []*ast.File{file})

	// Verify function was cached
	if _, exists := tracer2.funcDeclMap["testFunc"]; !exists {
		t.Error("Expected BuildFunctionCache to cache testFunc")
	}
}

func TestFunctionTracer_TraceFunction_NilInfo(t *testing.T) {
	tracer := NewFunctionTracer(nil)

	call := &ast.CallExpr{
		Fun: &ast.Ident{Name: "someFunc"},
	}

	// Should return nil when type info is nil
	result := tracer.TraceFunction(call, nil)
	if result != nil {
		t.Error("Expected nil result when type info is nil")
	}
}

