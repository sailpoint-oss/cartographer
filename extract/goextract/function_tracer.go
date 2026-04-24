// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"go/ast"
	"go/types"
	"strings"
)

// FunctionTracer traces function calls recursively through the codebase.
// NO depth limit - traces as far as needed with cycle detection.
type FunctionTracer struct {
	// Function declaration cache - maps function key to AST node
	funcDeclMap map[string]*ast.FuncDecl

	// Analysis cache - maps function key to analyzed result (per-run cache)
	analysisCache map[string]*FunctionAnalysis

	// Current call stack for cycle detection
	callStack []string

	// Type information from all packages
	typeInfo *types.Info

	// Response registry
	responseRegistry *ResponseRegistry
}

// FunctionAnalysis contains the results of analyzing a function.
type FunctionAnalysis struct {
	// Error responses found in this function
	ErrorResponses []ErrorResponseInfo

	// Success responses found in this function
	SuccessResponses []SuccessResponseInfo

	// Whether this function was fully analyzed
	Complete bool
}

// ErrorResponseInfo describes an error response found in code.
type ErrorResponseInfo struct {
	StatusCode      int
	ResponseType    string // e.g., "web.Error"
	ResponsePackage string
	ErrorMessage    string // Actual error message if extractable
	Source          string // Where it came from (e.g., "web.BadRequest")
}

// SuccessResponseInfo describes a success response found in code.
type SuccessResponseInfo struct {
	StatusCode      int
	ResponseType    string
	ResponsePackage string
	Source          string
}

// NewFunctionTracer creates a new function tracer.
func NewFunctionTracer(responseRegistry *ResponseRegistry) *FunctionTracer {
	return &FunctionTracer{
		funcDeclMap:      make(map[string]*ast.FuncDecl),
		analysisCache:    make(map[string]*FunctionAnalysis),
		callStack:        make([]string, 0),
		responseRegistry: responseRegistry,
	}
}

// BuildFunctionCache builds a cache of all function declarations in loaded packages.
func (ft *FunctionTracer) BuildFunctionCache(packages []*ast.Package, files []*ast.File) {
	// Walk through all files and cache function declarations
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			if funcDecl, ok := n.(*ast.FuncDecl); ok {
				// Create function key
				funcName := funcDecl.Name.Name

				// For methods, include receiver type
				if funcDecl.Recv != nil && len(funcDecl.Recv.List) > 0 {
					if recvType := funcDecl.Recv.List[0].Type; recvType != nil {
						// Handle *Type and Type receivers
						recvTypeName := extractTypeName(recvType)
						funcName = recvTypeName + "." + funcName
					}
				}

				// Store in cache
				// Note: This is a simplified key. In production, include package path
				ft.funcDeclMap[funcName] = funcDecl
			}
			return true
		})
	}
}

// TraceFunction traces a function call and returns all responses it might generate.
func (ft *FunctionTracer) TraceFunction(call *ast.CallExpr, info *types.Info) *FunctionAnalysis {
	// Get function object
	funcObj := ft.getFunctionObject(call, info)
	if funcObj == nil {
		return nil
	}

	// Create function key
	funcKey := ft.getFunctionKey(funcObj)

	// Check if already in call stack (cycle detection)
	if ft.isInCallStack(funcKey) {
		return nil // Cycle detected, stop here
	}

	// Check cache
	if cached, exists := ft.analysisCache[funcKey]; exists {
		return cached
	}

	// Add to call stack
	ft.callStack = append(ft.callStack, funcKey)
	defer func() {
		// Remove from call stack when done
		ft.callStack = ft.callStack[:len(ft.callStack)-1]
	}()

	// Find function declaration
	funcDecl := ft.findFunctionDeclaration(funcKey, funcObj)
	if funcDecl == nil || funcDecl.Body == nil {
		return nil
	}

	// Analyze the function body
	analysis := ft.analyzeFunctionBody(funcDecl, info)

	// Cache the result
	ft.analysisCache[funcKey] = analysis

	return analysis
}

// analyzeFunctionBody analyzes a function body to find all possible responses.
func (ft *FunctionTracer) analyzeFunctionBody(funcDecl *ast.FuncDecl, info *types.Info) *FunctionAnalysis {
	analysis := &FunctionAnalysis{
		ErrorResponses:   make([]ErrorResponseInfo, 0),
		SuccessResponses: make([]SuccessResponseInfo, 0),
		Complete:         true,
	}

	// Walk through function body
	ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			// Check if this is a web error function
			if ft.isWebErrorFunction(call, info) {
				errorInfo := ft.extractErrorInfo(call, info)
				if errorInfo != nil {
					analysis.ErrorResponses = append(analysis.ErrorResponses, *errorInfo)
				}
			}

			// Check if this is web.WriteJSON
			if ft.isWriteJSONCall(call, info) {
				successInfo := ft.extractSuccessInfo(call, info)
				if successInfo != nil {
					analysis.SuccessResponses = append(analysis.SuccessResponses, *successInfo)
				}
			}

			// Check if this is a custom function call - trace it recursively
			if ft.shouldTraceCall(call, info) {
				nestedAnalysis := ft.TraceFunction(call, info)
				if nestedAnalysis != nil {
					// Merge nested results
					analysis.ErrorResponses = append(analysis.ErrorResponses, nestedAnalysis.ErrorResponses...)
					analysis.SuccessResponses = append(analysis.SuccessResponses, nestedAnalysis.SuccessResponses...)
				}
			}
		}
		return true
	})

	return analysis
}

// isWebErrorFunction checks if a call is a web error function.
func (ft *FunctionTracer) isWebErrorFunction(call *ast.CallExpr, info *types.Info) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	// Use type information
	if info != nil && info.Uses != nil {
		if obj := info.Uses[sel.Sel]; obj != nil {
			pkg := obj.Pkg()
			if pkg != nil && strings.Contains(pkg.Path(), "atlas-go") && strings.HasSuffix(pkg.Path(), "/web") {
				return ft.isKnownErrorFunction(sel.Sel.Name)
			}
		}
	}

	return false
}

// isKnownErrorFunction checks if a function name is a known web error function.
// Uses the shared KnownWebErrorFunctions map for consistency.
func (ft *FunctionTracer) isKnownErrorFunction(name string) bool {
	return IsKnownWebErrorFunction(name)
}

// extractErrorInfo extracts error response information from a web error call.
func (ft *FunctionTracer) extractErrorInfo(call *ast.CallExpr, info *types.Info) *ErrorResponseInfo {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}

	funcName := sel.Sel.Name
	statusCode := ft.getStatusCodeForFunction(funcName)

	errorInfo := &ErrorResponseInfo{
		StatusCode:      statusCode,
		ResponseType:    "web.Error",
		ResponsePackage: "github.com/sailpoint/atlas-go/v2/atlas/web",
		Source:          "web." + funcName,
	}

	// Try to extract error message
	if len(call.Args) > 2 {
		// Third argument is usually the error
		if errorMsg := ft.extractErrorMessage(call.Args[2]); errorMsg != "" {
			errorInfo.ErrorMessage = errorMsg
		}
	}

	return errorInfo
}

// getStatusCodeForFunction maps function names to status codes.
// Uses the shared WebErrorFunctionStatusCodes map for consistency.
func (ft *FunctionTracer) getStatusCodeForFunction(funcName string) int {
	return GetStatusCodeForErrorFunction(funcName)
}

// extractErrorMessage tries to extract the actual error message from code.
func (ft *FunctionTracer) extractErrorMessage(expr ast.Expr) string {
	// Handle different expression types
	switch e := expr.(type) {
	case *ast.BasicLit:
		// String literal
		if len(e.Value) >= 2 {
			return e.Value[1 : len(e.Value)-1] // Remove quotes
		}

	case *ast.CallExpr:
		// errors.New("message") or fmt.Errorf("message")
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			if sel.Sel.Name == "New" || sel.Sel.Name == "Errorf" {
				if len(e.Args) > 0 {
					if lit, ok := e.Args[0].(*ast.BasicLit); ok {
						if len(lit.Value) >= 2 {
							return lit.Value[1 : len(lit.Value)-1]
						}
					}
				}
			}
		}
	}

	return ""
}

// isWriteJSONCall checks if a call is web.WriteJSON.
func (ft *FunctionTracer) isWriteJSONCall(call *ast.CallExpr, info *types.Info) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	if info != nil && info.Uses != nil {
		if obj := info.Uses[sel.Sel]; obj != nil {
			pkg := obj.Pkg()
			if pkg != nil && strings.Contains(pkg.Path(), "atlas-go") && strings.HasSuffix(pkg.Path(), "/web") {
				return sel.Sel.Name == "WriteJSON"
			}
		}
	}

	return false
}

// extractSuccessInfo extracts success response information.
func (ft *FunctionTracer) extractSuccessInfo(call *ast.CallExpr, info *types.Info) *SuccessResponseInfo {
	// web.WriteJSON(ctx, w, response)
	// Extract the type of the third argument
	if len(call.Args) < 3 {
		return nil
	}

	responseArg := call.Args[2]
	responseType := ""
	responsePackage := ""

	if info != nil && info.TypeOf(responseArg) != nil {
		t := info.TypeOf(responseArg)
		responseType = t.String()

		// Extract package if it's a named type
		if named, ok := t.(*types.Named); ok {
			if pkg := named.Obj().Pkg(); pkg != nil {
				responsePackage = pkg.Path()
			}
		}
	}

	return &SuccessResponseInfo{
		StatusCode:      200,
		ResponseType:    responseType,
		ResponsePackage: responsePackage,
		Source:          "web.WriteJSON",
	}
}

// shouldTraceCall determines if we should recursively trace this function call.
func (ft *FunctionTracer) shouldTraceCall(call *ast.CallExpr, info *types.Info) bool {
	// Don't trace standard library functions
	funcObj := ft.getFunctionObject(call, info)
	if funcObj == nil {
		return false
	}

	pkg := funcObj.Pkg()
	if pkg == nil {
		return false
	}

	// Don't trace standard library
	if !strings.Contains(pkg.Path(), "/") {
		return false
	}

	// Don't trace third-party libraries (heuristic: has multiple path components)
	// Trace only if it's from the same module or a known atlas-go package
	return strings.Contains(pkg.Path(), "atlas-go") ||
		!strings.Contains(pkg.Path(), "github.com")
}

// Helper functions

func (ft *FunctionTracer) getFunctionObject(call *ast.CallExpr, info *types.Info) *types.Func {
	if info == nil || info.Uses == nil {
		return nil
	}

	switch fn := call.Fun.(type) {
	case *ast.SelectorExpr:
		if obj := info.Uses[fn.Sel]; obj != nil {
			if f, ok := obj.(*types.Func); ok {
				return f
			}
		}
	case *ast.Ident:
		if obj := info.Uses[fn]; obj != nil {
			if f, ok := obj.(*types.Func); ok {
				return f
			}
		}
	}

	return nil
}

func (ft *FunctionTracer) getFunctionKey(funcObj *types.Func) string {
	pkg := funcObj.Pkg()
	if pkg == nil {
		return funcObj.Name()
	}
	return pkg.Path() + "." + funcObj.Name()
}

func (ft *FunctionTracer) isInCallStack(funcKey string) bool {
	for _, key := range ft.callStack {
		if key == funcKey {
			return true
		}
	}
	return false
}

func (ft *FunctionTracer) findFunctionDeclaration(funcKey string, funcObj *types.Func) *ast.FuncDecl {
	// Try to find in cache using simple name
	simpleName := funcObj.Name()
	if decl, ok := ft.funcDeclMap[simpleName]; ok {
		return decl
	}

	// Try with full key
	if decl, ok := ft.funcDeclMap[funcKey]; ok {
		return decl
	}

	return nil
}

func extractTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return extractTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return t.Sel.Name
	}
	return ""
}
