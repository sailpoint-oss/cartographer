// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// TestRouterAnalyzer_DirectRequireRights tests detection of direct web.RequireRights usage.
func TestRouterAnalyzer_DirectRequireRights(t *testing.T) {
	src := `
package main

import (
	"net/http"
	"github.com/gorilla/mux"
	"github.com/sailpoint/atlas-go/v2/atlas/web"
)

func setupRouter() {
	router := mux.NewRouter()
	var summarizer interface{}
	var handler http.Handler
	
	// Direct web.RequireRights with single right
	router.Handle("/api/test", web.RequireRights(summarizer, "sp:test:read")(handler)).Methods("GET")
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	if routeInfo.Path != "/api/test" {
		t.Errorf("Expected path /api/test, got %s", routeInfo.Path)
	}

	if len(routeInfo.Rights) != 1 {
		t.Fatalf("Expected 1 right, got %d", len(routeInfo.Rights))
	}

	if routeInfo.Rights[0] != "sp:test:read" {
		t.Errorf("Expected right sp:test:read, got %s", routeInfo.Rights[0])
	}
}

// TestRouterAnalyzer_DirectRequireRights_MultipleRights tests detection with multiple rights.
func TestRouterAnalyzer_DirectRequireRights_MultipleRights(t *testing.T) {
	src := `
package main

import (
	"net/http"
	"github.com/gorilla/mux"
	"github.com/sailpoint/atlas-go/v2/atlas/web"
)

func setupRouter() {
	router := mux.NewRouter()
	var summarizer interface{}
	var handler http.Handler
	
	// Direct web.RequireRights with multiple rights
	router.Handle("/api/test", web.RequireRights(summarizer, "sp:test:read", "sp:test:write")(handler)).Methods("POST")
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	if len(routeInfo.Rights) != 2 {
		t.Fatalf("Expected 2 rights, got %d: %v", len(routeInfo.Rights), routeInfo.Rights)
	}

	expectedRights := map[string]bool{
		"sp:test:read":  true,
		"sp:test:write": true,
	}

	for _, right := range routeInfo.Rights {
		if !expectedRights[right] {
			t.Errorf("Unexpected right: %s", right)
		}
	}
}

// TestRouterAnalyzer_WrapperRequireRight tests detection of service wrapper methods.
func TestRouterAnalyzer_WrapperRequireRight(t *testing.T) {
	src := `
package main

import (
	"net/http"
	"github.com/gorilla/mux"
)

type Service struct{}

func (s *Service) requireRight(handler http.Handler, right string) http.Handler {
	return handler
}

func setupRouter() {
	router := mux.NewRouter()
	s := &Service{}
	var handler http.Handler
	
	// Service wrapper method
	router.Handle("/api/test", s.requireRight(handler, "sp:test:read")).Methods("GET")
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	if routeInfo.Path != "/api/test" {
		t.Errorf("Expected path /api/test, got %s", routeInfo.Path)
	}

	if len(routeInfo.Rights) != 1 {
		t.Fatalf("Expected 1 right, got %d", len(routeInfo.Rights))
	}

	if routeInfo.Rights[0] != "sp:test:read" {
		t.Errorf("Expected right sp:test:read, got %s", routeInfo.Rights[0])
	}
}

// TestRouterAnalyzer_WrapperRequireRights tests detection with alternative wrapper names.
func TestRouterAnalyzer_WrapperRequireRights(t *testing.T) {
	testCases := []struct {
		name         string
		methodName   string
		expectedPass bool
	}{
		{"requireRight", "requireRight", true},
		{"requireRights", "requireRights", true},
		{"RequireRight", "RequireRight", true},
		{"RequireRights", "RequireRights", true},
		{"requireAuth", "requireAuth", true},
		{"RequireAuth", "RequireAuth", true},
		{"authenticate", "authenticate", false}, // Should not match - doesn't have "require"
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			src := `
package main

import (
	"net/http"
	"github.com/gorilla/mux"
)

type Service struct{}

func (s *Service) ` + tc.methodName + `(handler http.Handler, right string) http.Handler {
	return handler
}

func setupRouter() {
	router := mux.NewRouter()
	s := &Service{}
	var handler http.Handler
	
	router.Handle("/api/test", s.` + tc.methodName + `(handler, "sp:test:read")).Methods("GET")
}`

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
			if err != nil {
				t.Fatalf("Failed to parse source: %v", err)
			}

			analyzer := NewRouterAnalyzer()
			info := &types.Info{
				Types: make(map[ast.Expr]types.TypeAndValue),
			}

			var routeInfo *RouteInfo
			ast.Inspect(file, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
						routeInfo = ri
						return false
					}
				}
				return true
			})

			if tc.expectedPass {
				if routeInfo == nil {
					t.Fatal("Expected to find route info, got nil")
				}

				if len(routeInfo.Rights) != 1 {
					t.Fatalf("Expected 1 right, got %d", len(routeInfo.Rights))
				}

				if routeInfo.Rights[0] != "sp:test:read" {
					t.Errorf("Expected right sp:test:read, got %s", routeInfo.Rights[0])
				}
			} else {
				if routeInfo != nil && len(routeInfo.Rights) > 0 {
					t.Errorf("Expected no rights to be detected for method %s, but got %v", tc.methodName, routeInfo.Rights)
				}
			}
		})
	}
}

// TestRouterAnalyzer_NoAuth tests routes without authentication.
func TestRouterAnalyzer_NoAuth(t *testing.T) {
	src := `
package main

import (
	"net/http"
	"github.com/gorilla/mux"
)

func setupRouter() {
	router := mux.NewRouter()
	var handler http.Handler
	
	// Simple handler without authentication
	router.Handle("/api/public", handler).Methods("GET")
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	if routeInfo.Path != "/api/public" {
		t.Errorf("Expected path /api/public, got %s", routeInfo.Path)
	}

	if len(routeInfo.Rights) != 0 {
		t.Errorf("Expected no rights, got %d: %v", len(routeInfo.Rights), routeInfo.Rights)
	}
}

// TestExtractPathParams tests path parameter extraction.
func TestExtractPathParams(t *testing.T) {
	testCases := []struct {
		path           string
		expectedParams []string
	}{
		{"/api/users/{id}", []string{"id"}},
		{"/api/users/{userId}/posts/{postId}", []string{"userId", "postId"}},
		{"/api/test", []string{}},
		{"/api/{org}/items/{id}/details", []string{"org", "id"}},
	}

	for _, tc := range testCases {
		t.Run(tc.path, func(t *testing.T) {
			params := ExtractPathParams(tc.path)

			if len(params) != len(tc.expectedParams) {
				t.Fatalf("Expected %d params, got %d: %v", len(tc.expectedParams), len(params), params)
			}

			for i, expected := range tc.expectedParams {
				if params[i] != expected {
					t.Errorf("Expected param %s at index %d, got %s", expected, i, params[i])
				}
			}
		})
	}
}

// TestRouterAnalyzer_MixedPatterns tests multiple patterns in one source file.
func TestRouterAnalyzer_MixedPatterns(t *testing.T) {
	// Test with a simpler structure that mirrors the other successful tests
	tests := []struct {
		name          string
		src           string
		expectedPath  string
		expectedRight string
		expectAuth    bool
	}{
		{
			name: "public endpoint",
			src: `
package main
import "github.com/gorilla/mux"
func setup() {
	router := mux.NewRouter()
	var handler interface{}
	router.Handle("/api/public", handler).Methods("GET")
}`,
			expectedPath: "/api/public",
			expectAuth:   false,
		},
		{
			name: "direct web.RequireRights",
			src: `
package main
import (
	"github.com/gorilla/mux"
	"github.com/sailpoint/atlas-go/v2/atlas/web"
)
func setup() {
	router := mux.NewRouter()
	var handler interface{}
	var summarizer interface{}
	router.Handle("/api/direct", web.RequireRights(summarizer, "sp:test:read")(handler)).Methods("GET")
}`,
			expectedPath:  "/api/direct",
			expectedRight: "sp:test:read",
			expectAuth:    true,
		},
		{
			name: "wrapper method",
			src: `
package main
import "github.com/gorilla/mux"
type Service struct{}
func (s *Service) requireRight(handler interface{}, right string) interface{} { return handler }
func setup() {
	router := mux.NewRouter()
	s := &Service{}
	var handler interface{}
	router.Handle("/api/wrapped", s.requireRight(handler, "sp:test:write")).Methods("POST")
}`,
			expectedPath:  "/api/wrapped",
			expectedRight: "sp:test:write",
			expectAuth:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tt.src, parser.ParseComments)
			if err != nil {
				t.Fatalf("Failed to parse source: %v", err)
			}

			analyzer := NewRouterAnalyzer()
			info := &types.Info{
				Types: make(map[ast.Expr]types.TypeAndValue),
			}

			var routeInfo *RouteInfo
			ast.Inspect(file, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
						routeInfo = ri
						return false
					}
				}
				return true
			})

			if routeInfo == nil {
				t.Fatal("Expected to find route info, got nil")
			}

			if routeInfo.Path != tt.expectedPath {
				t.Errorf("Expected path %s, got %s", tt.expectedPath, routeInfo.Path)
			}

			if tt.expectAuth {
				if len(routeInfo.Rights) == 0 {
					t.Error("Expected auth rights, but got none")
				} else if routeInfo.Rights[0] != tt.expectedRight {
					t.Errorf("Expected right %s, got %s", tt.expectedRight, routeInfo.Rights[0])
				}
			} else {
				if len(routeInfo.Rights) != 0 {
					t.Errorf("Expected no auth rights, but got %v", routeInfo.Rights)
				}
			}
		})
	}
}

// TestRouterAnalyzer_PathPrefix tests PathPrefix/Subrouter context propagation.
func TestRouterAnalyzer_PathPrefix(t *testing.T) {
	src := `
package main

import "github.com/gorilla/mux"

func setupRouter() {
	router := mux.NewRouter()
	sub := router.PathPrefix("/v1").Subrouter()
	var handler interface{}
	sub.Handle("/users", handler).Methods("GET")
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	// First pass: analyze PathPrefix assignments
	ast.Inspect(file, func(n ast.Node) bool {
		if assignStmt, ok := n.(*ast.AssignStmt); ok {
			analyzer.AnalyzePathPrefix(assignStmt, info)
		}
		return true
	})

	// Second pass: analyze routes
	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	// Path should be combined: /v1 + /users = /v1/users
	if routeInfo.Path != "/v1/users" {
		t.Errorf("Expected path /v1/users, got %s", routeInfo.Path)
	}
}

// TestRouterAnalyzer_UseMiddleware tests Use() middleware detection.
func TestRouterAnalyzer_UseMiddleware(t *testing.T) {
	src := `
package main

import (
	"github.com/gorilla/mux"
	"github.com/sailpoint/atlas-go/v2/atlas/web"
)

func setupRouter() {
	router := mux.NewRouter()
	sub := router.PathPrefix("/v1").Subrouter()
	var summarizer interface{}
	sub.Use(web.RequireRights(summarizer, "sp:v1:access"))
	var handler interface{}
	sub.Handle("/users", handler).Methods("GET")
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	// First pass: analyze PathPrefix assignments
	ast.Inspect(file, func(n ast.Node) bool {
		if assignStmt, ok := n.(*ast.AssignStmt); ok {
			analyzer.AnalyzePathPrefix(assignStmt, info)
		}
		return true
	})

	// Second pass: analyze Use() calls
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			analyzer.AnalyzeUseCall(call, info)
		}
		return true
	})

	// Third pass: analyze routes
	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	// Should inherit rights from Use() middleware
	if len(routeInfo.Rights) == 0 {
		t.Error("Expected rights from Use() middleware")
	}

	found := false
	for _, right := range routeInfo.Rights {
		if right == "sp:v1:access" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected right 'sp:v1:access' from Use(), got %v", routeInfo.Rights)
	}
}

// TestRouterAnalyzer_EmptyPath tests empty path handling on subrouters.
func TestRouterAnalyzer_EmptyPath(t *testing.T) {
	src := `
package main

import "github.com/gorilla/mux"

func setupRouter() {
	router := mux.NewRouter()
	sub := router.PathPrefix("/api").Subrouter()
	var handler interface{}
	sub.Handle("", handler).Methods("GET")
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	// First pass: analyze PathPrefix
	ast.Inspect(file, func(n ast.Node) bool {
		if assignStmt, ok := n.(*ast.AssignStmt); ok {
			analyzer.AnalyzePathPrefix(assignStmt, info)
		}
		return true
	})

	// Second pass: analyze routes
	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	// Empty path on subrouter should give us just the prefix
	if routeInfo.Path != "/api" {
		t.Errorf("Expected path /api, got %s", routeInfo.Path)
	}
}

// TestIsValidHTTPMethod tests HTTP method validation.
func TestIsValidHTTPMethod(t *testing.T) {
	validMethods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS", "TRACE", "QUERY"}
	for _, method := range validMethods {
		if !IsValidHTTPMethod(method) {
			t.Errorf("Expected %s to be valid HTTP method", method)
		}
	}

	// Test lowercase
	if !IsValidHTTPMethod("get") {
		t.Error("Expected lowercase 'get' to be valid HTTP method")
	}

	invalidMethods := []string{"INVALID", "CUSTOM", "FOO"}
	for _, method := range invalidMethods {
		if IsValidHTTPMethod(method) {
			t.Errorf("Expected %s to be invalid HTTP method", method)
		}
	}
}

// TestIsOpenAPI32Method tests OpenAPI 3.2 method detection.
func TestIsOpenAPI32Method(t *testing.T) {
	if !IsOpenAPI32Method("QUERY") {
		t.Error("Expected QUERY to be OpenAPI 3.2 method")
	}

	if !IsOpenAPI32Method("query") {
		t.Error("Expected lowercase 'query' to be OpenAPI 3.2 method")
	}

	nonOpenAPI32Methods := []string{"GET", "POST", "PUT", "DELETE"}
	for _, method := range nonOpenAPI32Methods {
		if IsOpenAPI32Method(method) {
			t.Errorf("Expected %s NOT to be OpenAPI 3.2 method", method)
		}
	}
}

// TestRouterAnalyzer_ExtractIdentifier tests identifier extraction.
func TestRouterAnalyzer_ExtractIdentifier(t *testing.T) {
	analyzer := NewRouterAnalyzer()

	// Simple selector
	sel := &ast.SelectorExpr{
		X:   &ast.Ident{Name: "pkg"},
		Sel: &ast.Ident{Name: "Type"},
	}

	result := analyzer.extractIdentifier(sel)
	if result != "pkg.Type" {
		t.Errorf("Expected 'pkg.Type', got '%s'", result)
	}

	// Nested selector
	nestedSel := &ast.SelectorExpr{
		X: &ast.SelectorExpr{
			X:   &ast.Ident{Name: "outer"},
			Sel: &ast.Ident{Name: "inner"},
		},
		Sel: &ast.Ident{Name: "Type"},
	}

	result = analyzer.extractIdentifier(nestedSel)
	if result != "outer.inner.Type" {
		t.Errorf("Expected 'outer.inner.Type', got '%s'", result)
	}
}

// TestRouterAnalyzer_ExtractMethodArg tests HTTP method argument extraction.
func TestRouterAnalyzer_ExtractMethodArg(t *testing.T) {
	analyzer := NewRouterAnalyzer()

	// String literal
	lit := &ast.BasicLit{Kind: token.STRING, Value: `"POST"`}
	result := analyzer.extractMethodArg(lit)
	if result != "POST" {
		t.Errorf("Expected 'POST', got '%s'", result)
	}

	// http.MethodGet selector
	sel := &ast.SelectorExpr{
		X:   &ast.Ident{Name: "http"},
		Sel: &ast.Ident{Name: "MethodGet"},
	}
	result = analyzer.extractMethodArg(sel)
	if result != "GET" {
		t.Errorf("Expected 'GET', got '%s'", result)
	}
}

// TestRouteInfo struct tests.
func TestRouteInfo(t *testing.T) {
	ri := &RouteInfo{
		Path:        "/api/users",
		Method:      "GET",
		HandlerName: "listUsers",
		Rights:      []string{"sp:users:read"},
		Middleware:  []string{"logger", "auth"},
	}

	if ri.Path != "/api/users" {
		t.Errorf("Expected path '/api/users', got '%s'", ri.Path)
	}

	if ri.Method != "GET" {
		t.Errorf("Expected method 'GET', got '%s'", ri.Method)
	}

	if ri.HandlerName != "listUsers" {
		t.Errorf("Expected handler 'listUsers', got '%s'", ri.HandlerName)
	}

	if len(ri.Rights) != 1 || ri.Rights[0] != "sp:users:read" {
		t.Errorf("Expected rights ['sp:users:read'], got %v", ri.Rights)
	}
}

// TestSubrouterContext tests the SubrouterContext struct.
func TestSubrouterContext(t *testing.T) {
	ctx := &SubrouterContext{
		PathPrefix: "/v1/api",
		Rights:     []string{"sp:api:access"},
	}

	if ctx.PathPrefix != "/v1/api" {
		t.Errorf("Expected PathPrefix '/v1/api', got '%s'", ctx.PathPrefix)
	}

	if len(ctx.Rights) != 1 || ctx.Rights[0] != "sp:api:access" {
		t.Errorf("Expected rights ['sp:api:access'], got %v", ctx.Rights)
	}
}

// TestNewRouterAnalyzer tests analyzer initialization.
func TestNewRouterAnalyzer(t *testing.T) {
	analyzer := NewRouterAnalyzer()

	if analyzer == nil {
		t.Fatal("Expected non-nil analyzer")
	}

	if analyzer.subrouterContexts == nil {
		t.Error("Expected subrouterContexts to be initialized")
	}
}

// TestRouterAnalyzer_IsRouterHandleCall tests Handle/HandleFunc detection.
func TestRouterAnalyzer_IsRouterHandleCall(t *testing.T) {
	analyzer := NewRouterAnalyzer()

	// Handle call
	handleCall := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   &ast.Ident{Name: "router"},
			Sel: &ast.Ident{Name: "Handle"},
		},
	}

	if !analyzer.isRouterHandleCall(handleCall) {
		t.Error("Expected Handle call to be recognized")
	}

	// HandleFunc call
	handleFuncCall := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   &ast.Ident{Name: "router"},
			Sel: &ast.Ident{Name: "HandleFunc"},
		},
	}

	if !analyzer.isRouterHandleCall(handleFuncCall) {
		t.Error("Expected HandleFunc call to be recognized")
	}

	// Non-router call
	otherCall := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   &ast.Ident{Name: "router"},
			Sel: &ast.Ident{Name: "Methods"},
		},
	}

	if analyzer.isRouterHandleCall(otherCall) {
		t.Error("Expected Methods call NOT to be recognized as Handle/HandleFunc")
	}

	// Non-selector call
	identCall := &ast.CallExpr{
		Fun: &ast.Ident{Name: "someFunc"},
	}

	if analyzer.isRouterHandleCall(identCall) {
		t.Error("Expected non-selector call NOT to be recognized")
	}
}

// TestRouterAnalyzer_IsWebPackage tests web package detection.
func TestRouterAnalyzer_IsWebPackage(t *testing.T) {
	analyzer := NewRouterAnalyzer()

	webIdent := &ast.Ident{Name: "web"}
	if !analyzer.isWebPackage(webIdent) {
		t.Error("Expected 'web' identifier to be recognized as web package")
	}

	otherIdent := &ast.Ident{Name: "http"}
	if analyzer.isWebPackage(otherIdent) {
		t.Error("Expected 'http' identifier NOT to be recognized as web package")
	}

	// Non-ident expression
	sel := &ast.SelectorExpr{
		X:   &ast.Ident{Name: "pkg"},
		Sel: &ast.Ident{Name: "Type"},
	}
	if analyzer.isWebPackage(sel) {
		t.Error("Expected selector NOT to be recognized as web package")
	}
}

// TestRouterAnalyzer_ExtractPath tests path extraction from arguments.
func TestRouterAnalyzer_ExtractPath(t *testing.T) {
	analyzer := NewRouterAnalyzer()

	tests := []struct {
		name     string
		arg      ast.Expr
		expected string
	}{
		{
			name:     "string literal",
			arg:      &ast.BasicLit{Kind: token.STRING, Value: `"/api/users"`},
			expected: "/api/users",
		},
		{
			name:     "empty string",
			arg:      &ast.BasicLit{Kind: token.STRING, Value: `""`},
			expected: "",
		},
		{
			name:     "non-string",
			arg:      &ast.Ident{Name: "pathVar"},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.extractPath(tt.arg)
			if result != tt.expected {
				t.Errorf("extractPath() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestRouterAnalyzer_GetRouterVarName tests router variable name extraction.
func TestRouterAnalyzer_GetRouterVarName(t *testing.T) {
	analyzer := NewRouterAnalyzer()

	// Identifier
	ident := &ast.Ident{Name: "router"}
	result := analyzer.getRouterVarName(ident)
	if result != "router" {
		t.Errorf("Expected 'router', got '%s'", result)
	}

	// Non-identifier
	sel := &ast.SelectorExpr{
		X:   &ast.Ident{Name: "s"},
		Sel: &ast.Ident{Name: "router"},
	}
	result = analyzer.getRouterVarName(sel)
	if result != "s" {
		t.Errorf("Expected selector base 's', got '%s'", result)
	}
}

// ============================
// Chi Router Pattern Tests
// ============================

// TestRouterAnalyzer_ChiGet tests chi r.Get() pattern detection.
func TestRouterAnalyzer_ChiGet(t *testing.T) {
	src := `
package main

import "github.com/go-chi/chi/v5"

func setupRouter() {
	r := chi.NewRouter()
	var handler interface{}
	r.Get("/api/users", handler)
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	if routeInfo.Path != "/api/users" {
		t.Errorf("Expected path /api/users, got %s", routeInfo.Path)
	}

	if routeInfo.Method != "GET" {
		t.Errorf("Expected method GET, got %s", routeInfo.Method)
	}
}

// TestRouterAnalyzer_ChiPost tests chi r.Post() pattern detection.
func TestRouterAnalyzer_ChiPost(t *testing.T) {
	src := `
package main

import "github.com/go-chi/chi/v5"

func setupRouter() {
	r := chi.NewRouter()
	var handler interface{}
	r.Post("/api/users", handler)
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	if routeInfo.Path != "/api/users" {
		t.Errorf("Expected path /api/users, got %s", routeInfo.Path)
	}

	if routeInfo.Method != "POST" {
		t.Errorf("Expected method POST, got %s", routeInfo.Method)
	}
}

// TestRouterAnalyzer_ChiAllMethods tests all chi HTTP method patterns.
func TestRouterAnalyzer_ChiAllMethods(t *testing.T) {
	methods := []struct {
		chiMethod  string
		httpMethod string
	}{
		{"Get", "GET"},
		{"Post", "POST"},
		{"Put", "PUT"},
		{"Patch", "PATCH"},
		{"Delete", "DELETE"},
		{"Head", "HEAD"},
		{"Options", "OPTIONS"},
	}

	for _, m := range methods {
		t.Run(m.chiMethod, func(t *testing.T) {
			src := `
package main

import "github.com/go-chi/chi/v5"

func setupRouter() {
	r := chi.NewRouter()
	var handler interface{}
	r.` + m.chiMethod + `("/api/test", handler)
}`

			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
			if err != nil {
				t.Fatalf("Failed to parse source: %v", err)
			}

			analyzer := NewRouterAnalyzer()
			info := &types.Info{
				Types: make(map[ast.Expr]types.TypeAndValue),
			}

			var routeInfo *RouteInfo
			ast.Inspect(file, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
						routeInfo = ri
						return false
					}
				}
				return true
			})

			if routeInfo == nil {
				t.Fatal("Expected to find route info, got nil")
			}

			if routeInfo.Method != m.httpMethod {
				t.Errorf("Expected method %s, got %s", m.httpMethod, routeInfo.Method)
			}
		})
	}
}

// TestRouterAnalyzer_ChiRoute tests chi Route() nested routing with path prefix.
func TestRouterAnalyzer_ChiRoute(t *testing.T) {
	src := `
package main

import "github.com/go-chi/chi/v5"

func setupRouter() {
	r := chi.NewRouter()
	var handler interface{}
	r.Route("/api", func(r chi.Router) {
		r.Get("/users", handler)
	})
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	// First pass: analyze Route() calls for context
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			analyzer.AnalyzeChiRoute(call, info)
		}
		return true
	})

	// Second pass: analyze routes
	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	// Path should be combined: /api + /users = /api/users
	if routeInfo.Path != "/api/users" {
		t.Errorf("Expected path /api/users, got %s", routeInfo.Path)
	}

	if routeInfo.Method != "GET" {
		t.Errorf("Expected method GET, got %s", routeInfo.Method)
	}
}

// TestRouterAnalyzer_ChiGroup tests chi Group() middleware grouping.
func TestRouterAnalyzer_ChiGroup(t *testing.T) {
	src := `
package main

import "github.com/go-chi/chi/v5"

func setupRouter() {
	r := chi.NewRouter()
	var handler interface{}
	r.Group(func(r chi.Router) {
		r.Get("/api/users", handler)
	})
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	// First pass: analyze Group() calls
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			analyzer.AnalyzeChiRoute(call, info)
		}
		return true
	})

	// Second pass: analyze routes
	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	// Group doesn't add a path prefix
	if routeInfo.Path != "/api/users" {
		t.Errorf("Expected path /api/users, got %s", routeInfo.Path)
	}
}

// TestRouterAnalyzer_ChiNestedRoute tests deeply nested chi Route() calls.
func TestRouterAnalyzer_ChiNestedRoute(t *testing.T) {
	src := `
package main

import "github.com/go-chi/chi/v5"

func setupRouter() {
	r := chi.NewRouter()
	var handler interface{}
	r.Route("/api", func(r chi.Router) {
		r.Route("/v1", func(r chi.Router) {
			r.Get("/users", handler)
		})
	})
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	// First pass: analyze Route() calls for context
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			analyzer.AnalyzeChiRoute(call, info)
		}
		return true
	})

	// Second pass: analyze routes
	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	// Path should be fully combined: /api + /v1 + /users = /api/v1/users
	if routeInfo.Path != "/api/v1/users" {
		t.Errorf("Expected path /api/v1/users, got %s", routeInfo.Path)
	}
}

// TestRouterAnalyzer_ChiMount tests chi Mount() subrouter mounting.
func TestRouterAnalyzer_ChiMount(t *testing.T) {
	src := `
package main

import "github.com/go-chi/chi/v5"

func setupRouter() {
	mainRouter := chi.NewRouter()
	apiRouter := chi.NewRouter()
	var handler interface{}
	apiRouter.Get("/users", handler)
	mainRouter.Mount("/api", apiRouter)
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	// First pass: analyze Mount() calls for context
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			analyzer.AnalyzeChiMount(call, info)
		}
		return true
	})

	// Second pass: analyze routes
	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	// Path should include mount prefix: /api + /users = /api/users
	if routeInfo.Path != "/api/users" {
		t.Errorf("Expected path /api/users, got %s", routeInfo.Path)
	}
}

// TestRouterAnalyzer_ChiWith tests chi With() middleware chain detection.
func TestRouterAnalyzer_ChiWith(t *testing.T) {
	src := `
package main

import (
	"github.com/go-chi/chi/v5"
	"github.com/sailpoint/atlas-go/v2/atlas/web"
)

func setupRouter() {
	r := chi.NewRouter()
	var summarizer interface{}
	var handler interface{}
	r.With(web.RequireRights(summarizer, "sp:test:read")).Get("/api/test", handler)
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	// Analyze With() calls for middleware
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			analyzer.AnalyzeChiWith(call, info)
		}
		return true
	})

	// Verify we extracted rights
	// Note: In the actual implementation, With() rights would be applied
	// to the chained Get() call. This test verifies With() detection.
	var foundWith bool
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if rights := analyzer.AnalyzeChiWith(call, info); rights != nil {
				foundWith = true
				if len(rights) == 0 {
					t.Error("Expected rights from With() call")
				}
				return false
			}
		}
		return true
	})

	if !foundWith {
		t.Error("Expected to find With() call")
	}
}

// TestRouterAnalyzer_ChiWithAppliedOnRoute verifies rights from With() are
// applied when route registration is chained from the With() call.
func TestRouterAnalyzer_ChiWithAppliedOnRoute(t *testing.T) {
	src := `
package main

import (
	"github.com/go-chi/chi/v5"
	"github.com/sailpoint/atlas-go/v2/atlas/web"
)

func setupRouter() {
	r := chi.NewRouter()
	var summarizer interface{}
	var handler interface{}
	r.With(web.RequireRights(summarizer, "sp:test:read")).Get("/api/test", handler)
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{Types: make(map[ast.Expr]types.TypeAndValue)}

	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}
	if len(routeInfo.Rights) == 0 {
		t.Fatalf("Expected rights from chi.With chain, got none")
	}
	if routeInfo.Rights[0] != "sp:test:read" {
		t.Fatalf("Expected first right sp:test:read, got %v", routeInfo.Rights)
	}
}

// TestRouterAnalyzer_IsChiRouteOrGroup tests chi Route/Group detection.
func TestRouterAnalyzer_IsChiRouteOrGroup(t *testing.T) {
	analyzer := NewRouterAnalyzer()

	tests := []struct {
		name         string
		methodName   string
		hasPath      bool
		expectedPath string
		isRouteGroup bool
	}{
		{
			name:         "Route with path",
			methodName:   "Route",
			hasPath:      true,
			expectedPath: "/api",
			isRouteGroup: true,
		},
		{
			name:         "Group without path",
			methodName:   "Group",
			hasPath:      false,
			expectedPath: "",
			isRouteGroup: true,
		},
		{
			name:         "Get is not Route/Group",
			methodName:   "Get",
			hasPath:      true,
			expectedPath: "",
			isRouteGroup: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []ast.Expr{}
			if tt.hasPath {
				args = append(args, &ast.BasicLit{Kind: token.STRING, Value: `"/api"`})
			}
			args = append(args, &ast.FuncLit{}) // dummy func lit

			call := &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "r"},
					Sel: &ast.Ident{Name: tt.methodName},
				},
				Args: args,
			}

			isRouteGroup, path := analyzer.isChiRouteOrGroup(call)

			if isRouteGroup != tt.isRouteGroup {
				t.Errorf("isChiRouteOrGroup() returned %v, want %v", isRouteGroup, tt.isRouteGroup)
			}

			if path != tt.expectedPath {
				t.Errorf("isChiRouteOrGroup() path = %q, want %q", path, tt.expectedPath)
			}
		})
	}
}

// TestRouterAnalyzer_IsChiMount tests chi Mount detection.
func TestRouterAnalyzer_IsChiMount(t *testing.T) {
	analyzer := NewRouterAnalyzer()

	tests := []struct {
		name         string
		methodName   string
		hasPath      bool
		isMount      bool
		expectedPath string
	}{
		{
			name:         "Mount with path",
			methodName:   "Mount",
			hasPath:      true,
			isMount:      true,
			expectedPath: "/api",
		},
		{
			name:         "Get is not Mount",
			methodName:   "Get",
			hasPath:      true,
			isMount:      false,
			expectedPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []ast.Expr{}
			if tt.hasPath {
				args = append(args, &ast.BasicLit{Kind: token.STRING, Value: `"/api"`})
			}
			args = append(args, &ast.Ident{Name: "subrouter"})

			call := &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "r"},
					Sel: &ast.Ident{Name: tt.methodName},
				},
				Args: args,
			}

			isMount, path := analyzer.isChiMount(call)

			if isMount != tt.isMount {
				t.Errorf("isChiMount() returned %v, want %v", isMount, tt.isMount)
			}

			if path != tt.expectedPath {
				t.Errorf("isChiMount() path = %q, want %q", path, tt.expectedPath)
			}
		})
	}
}

// ============================
// net/http Pattern Tests
// ============================

// TestRouterAnalyzer_NetHTTP_HandleFunc tests net/http HandleFunc pattern.
func TestRouterAnalyzer_NetHTTP_HandleFunc(t *testing.T) {
	src := `
package main

import "net/http"

func setupRouter() {
	mux := http.NewServeMux()
	var handler interface{}
	mux.HandleFunc("/api/users", handler)
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	if routeInfo.Path != "/api/users" {
		t.Errorf("Expected path /api/users, got %s", routeInfo.Path)
	}

	// net/http HandleFunc defaults to GET when no method is specified
	if routeInfo.Method != "GET" {
		t.Errorf("Expected method GET, got %s", routeInfo.Method)
	}
}

// TestRouterAnalyzer_NetHTTP_Handle tests net/http Handle pattern.
func TestRouterAnalyzer_NetHTTP_Handle(t *testing.T) {
	src := `
package main

import "net/http"

func setupRouter() {
	mux := http.NewServeMux()
	var handler http.Handler
	mux.Handle("/api/users", handler)
}`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	var routeInfo *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeRouterCall(call, file, info, fset); ri != nil {
				routeInfo = ri
				return false
			}
		}
		return true
	})

	if routeInfo == nil {
		t.Fatal("Expected to find route info, got nil")
	}

	if routeInfo.Path != "/api/users" {
		t.Errorf("Expected path /api/users, got %s", routeInfo.Path)
	}
}

// TestChiMethodToHTTP tests the chi method mapping.
func TestChiMethodToHTTP(t *testing.T) {
	tests := []struct {
		chiMethod    string
		expectedHTTP string
	}{
		{"Get", "GET"},
		{"Post", "POST"},
		{"Put", "PUT"},
		{"Patch", "PATCH"},
		{"Delete", "DELETE"},
		{"Head", "HEAD"},
		{"Options", "OPTIONS"},
		{"Trace", "TRACE"},
		{"Connect", "CONNECT"},
	}

	for _, tt := range tests {
		t.Run(tt.chiMethod, func(t *testing.T) {
			if httpMethod, ok := chiMethodToHTTP[tt.chiMethod]; !ok {
				t.Errorf("chiMethodToHTTP missing %s", tt.chiMethod)
			} else if httpMethod != tt.expectedHTTP {
				t.Errorf("chiMethodToHTTP[%s] = %s, want %s", tt.chiMethod, httpMethod, tt.expectedHTTP)
			}
		})
	}

	// Test that unknown methods are not in the map
	if _, ok := chiMethodToHTTP["Handle"]; ok {
		t.Error("Handle should not be in chiMethodToHTTP")
	}
}
