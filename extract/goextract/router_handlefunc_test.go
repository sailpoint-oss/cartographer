// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

// TestRouterAnalyzer_HandleFunc tests detection of HandleFunc calls.
func TestRouterAnalyzer_HandleFunc(t *testing.T) {
	src := `
package main

import (
	"net/http"
	"github.com/gorilla/mux"
)

type Service struct{}

func (s *Service) handler(w http.ResponseWriter, r *http.Request) {
	// Handler implementation
}

func setupRouter() {
	router := mux.NewRouter()
	s := &Service{}
	
	// Using HandleFunc instead of Handle
	router.HandleFunc("/api/test", s.handler).Methods("GET")
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

	if routeInfo.Method != "GET" {
		t.Errorf("Expected method GET, got %s", routeInfo.Method)
	}

	if routeInfo.HandlerName != "handler" {
		t.Errorf("Expected handler name 'handler', got %s", routeInfo.HandlerName)
	}
}
