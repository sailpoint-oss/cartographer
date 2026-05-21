package goextract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

func TestAnalyzeMuxPathMethodHandler(t *testing.T) {
	src := `
package main

import (
	"net/http"
	"github.com/gorilla/mux"
)

func register(router *mux.Router, handler http.Handler) {
	router.Path("/api/v1/prompts").Methods(http.MethodPost).Handler(handler)
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "routes.go", src, 0)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	analyzer := NewRouterAnalyzer()
	info := &types.Info{Types: make(map[ast.Expr]types.TypeAndValue)}

	var route *RouteInfo
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if ri := analyzer.AnalyzeMuxPathMethodHandler(call, file, info, fset); ri != nil {
				route = ri
				return false
			}
		}
		return true
	})

	if route == nil {
		t.Fatal("expected mux Path().Methods().Handler route")
	}
	if route.Path != "/api/v1/prompts" {
		t.Errorf("path = %q", route.Path)
	}
	if route.Method != "POST" {
		t.Errorf("method = %q, want POST", route.Method)
	}
}

func TestIsBuildRoutesMethod(t *testing.T) {
	if !isBuildRoutesMethod(&ast.FuncDecl{Name: ast.NewIdent("BuildRoutes")}) {
		t.Error("expected BuildRoutes to match")
	}
	if isBuildRoutesMethod(&ast.FuncDecl{Name: ast.NewIdent("Setup")}) {
		t.Error("Setup should not match BuildRoutes")
	}
}
