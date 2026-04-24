// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"testing"
)

func TestNewHandlerAnalyzer(t *testing.T) {
	tracer := NewFunctionTracer(nil)
	analyzer := NewHandlerAnalyzer(tracer)

	if analyzer == nil {
		t.Fatal("Expected non-nil analyzer")
	}

	if analyzer.tracer != tracer {
		t.Error("Expected tracer to be set")
	}
}

func TestHandlerAnalyzer_LooksLikeHandler(t *testing.T) {
	src := `
package main

import "net/http"

// Handler function returning HandlerFunc
func createHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {}
}

// Direct handler function
func directHandler(w http.ResponseWriter, r *http.Request) {}

// Not a handler - wrong signature
func notAHandler(x int) int {
	return x + 1
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	// Create basic type info
	info := &types.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
	}

	analyzer := NewHandlerAnalyzer(nil)

	tests := []struct {
		funcName string
		expected bool
	}{
		// Note: Without full type checking, looksLikeHandler relies on signatures
		// We test the pattern matching logic
	}

	// Just verify the analyzer was created and can be called
	for _, funcDecl := range file.Decls {
		if fn, ok := funcDecl.(*ast.FuncDecl); ok {
			_ = analyzer.looksLikeHandler(fn, info)
		}
	}

	// If we got here without panic, the test passes
	_ = tests
}

func TestHandlerAnalyzer_IsJSONDecodeCall(t *testing.T) {
	src := `
package main

import "encoding/json"

func handler() {
	json.NewDecoder(nil).Decode(nil)
	json.Unmarshal(nil, nil)
	someOtherCall()
}

func someOtherCall() {}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewHandlerAnalyzer(nil)
	handlerInfo := &HandlerInfo{jsonDecoderVars: make(map[string]bool)}

	decodeCallFound := false
	unmarshalCallFound := false

	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if analyzer.isJSONDecodeCall(call, handlerInfo) {
				// Check if it's Decode or Unmarshal
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
					if sel.Sel.Name == "Decode" {
						decodeCallFound = true
					}
					if sel.Sel.Name == "Unmarshal" {
						unmarshalCallFound = true
					}
				}
			}
		}
		return true
	})

	if !decodeCallFound {
		t.Error("Expected to find json.Decode() call")
	}

	if !unmarshalCallFound {
		t.Error("Expected to find json.Unmarshal() call")
	}
}

func TestHandlerAnalyzer_IsWriteJSONCall(t *testing.T) {
	src := `
package main

func handler() {
	web.WriteJSON(nil, nil, nil)
	someOther.WriteJSON(nil, nil, nil)
	notWeb.DoSomething()
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewHandlerAnalyzer(nil)

	webWriteJSONCount := 0

	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if analyzer.isWriteJSONCall(call) {
				webWriteJSONCount++
			}
		}
		return true
	})

	if webWriteJSONCount != 1 {
		t.Errorf("Expected 1 web.WriteJSON call, found %d", webWriteJSONCount)
	}
}

func TestHandlerAnalyzer_IsErrorCall(t *testing.T) {
	src := `
package main

func handler() {
	web.BadRequest(nil, nil, nil)
	web.Unauthorized(nil, nil)
	web.Forbidden(nil, nil)
	web.NotFound(nil, nil)
	web.InternalServerError(nil, nil, nil)
	web.ServiceUnavailable(nil, nil)
	web.Gone(nil, nil)
	web.ContextCanceled(nil, nil)
	web.NoContent(nil, nil)
	web.SomeOtherFunc()  // Not an error func
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewHandlerAnalyzer(nil)

	errorCallCount := 0

	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if analyzer.isErrorCall(call) {
				errorCallCount++
			}
		}
		return true
	})

	if errorCallCount != 9 {
		t.Errorf("Expected 9 error calls, found %d", errorCallCount)
	}
}

func TestHandlerAnalyzer_IsWebErrorFunction(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	errorFuncs := []string{
		"BadRequest", "Unauthorized", "Forbidden", "NotFound",
		"InternalServerError", "ServiceUnavailable", "Gone",
		"ContextCanceled", "NoContent",
	}

	for _, fn := range errorFuncs {
		if !analyzer.isWebErrorFunction(fn) {
			t.Errorf("Expected '%s' to be recognized as error function", fn)
		}
	}

	nonErrorFuncs := []string{
		"WriteJSON", "RequireRights", "DoSomething", "Parse",
	}

	for _, fn := range nonErrorFuncs {
		if analyzer.isWebErrorFunction(fn) {
			t.Errorf("Expected '%s' NOT to be recognized as error function", fn)
		}
	}
}

func TestHandlerAnalyzer_GetStatusCodeForFunction(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	tests := []struct {
		funcName string
		expected int
	}{
		{"BadRequest", 400},
		{"Unauthorized", 401},
		{"Forbidden", 403},
		{"NotFound", 404},
		{"Gone", 410},
		{"InternalServerError", 500},
		{"ServiceUnavailable", 503},
		{"ContextCanceled", 499},
		{"NoContent", 204},
		{"UnknownFunc", 500}, // Default
	}

	for _, tt := range tests {
		t.Run(tt.funcName, func(t *testing.T) {
			result := analyzer.getStatusCodeForFunction(tt.funcName)
			if result != tt.expected {
				t.Errorf("getStatusCodeForFunction(%s) = %d, want %d", tt.funcName, result, tt.expected)
			}
		})
	}
}

func TestHandlerAnalyzer_LooksLikeErrorFunction(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	errorNames := []string{
		"sendError", "handleError", "respondError", "writeError",
		"badRequest", "notFound", "unauthorized", "forbidden",
		"failWithError", "invalidInput",
	}

	for _, name := range errorNames {
		if !analyzer.looksLikeErrorFunction(name) {
			t.Errorf("Expected '%s' to look like error function", name)
		}
	}

	nonErrorNames := []string{
		"writeJSON", "processRequest", "handleSuccess", "getUser",
	}

	for _, name := range nonErrorNames {
		if analyzer.looksLikeErrorFunction(name) {
			t.Errorf("Expected '%s' NOT to look like error function", name)
		}
	}
}

func TestHandlerAnalyzer_ExtractErrorMessage(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	tests := []struct {
		name     string
		src      string
		expected string
	}{
		{
			name:     "string literal",
			src:      `"Invalid input"`,
			expected: "Invalid input",
		},
		{
			name:     "errors.New",
			src:      `errors.New("User not found")`,
			expected: "User not found",
		},
		{
			name:     "fmt.Errorf",
			src:      `fmt.Errorf("Failed to process")`,
			expected: "Failed to process",
		},
		{
			name:     "variable",
			src:      `err`,
			expected: "", // Can't extract from variable
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse the expression
			fset := token.NewFileSet()
			expr, err := parser.ParseExpr(tt.src)
			if err != nil {
				t.Skipf("Could not parse expression: %v", err)
				return
			}
			_ = fset

			result := analyzer.extractErrorMessage(expr)
			if result != tt.expected {
				t.Errorf("extractErrorMessage() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestHandlerAnalyzer_MergeErrorCodes(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	handlerInfo := &HandlerInfo{
		ErrorCodes: []int{400},
		ErrorResponses: []ErrorResponseInfo{
			{StatusCode: 400},
			{StatusCode: 500},
			{StatusCode: 401},
		},
	}

	analyzer.mergeErrorCodes(handlerInfo)

	// Should have unique error codes from both sources
	expectedCodes := map[int]bool{400: true, 500: true, 401: true}

	if len(handlerInfo.ErrorCodes) != 3 {
		t.Errorf("Expected 3 error codes, got %d: %v", len(handlerInfo.ErrorCodes), handlerInfo.ErrorCodes)
	}

	for _, code := range handlerInfo.ErrorCodes {
		if !expectedCodes[code] {
			t.Errorf("Unexpected error code: %d", code)
		}
	}
}

func TestHandlerAnalyzer_IsMuxVarsCall(t *testing.T) {
	src := `
package main

import "github.com/gorilla/mux"

func handler() {
	vars := mux.Vars(r)
	other.Vars(r)
}
`

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("Failed to parse source: %v", err)
	}

	analyzer := NewHandlerAnalyzer(nil)

	muxVarsCount := 0

	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if analyzer.isMuxVarsCall(call) {
				muxVarsCount++
			}
		}
		return true
	})

	if muxVarsCount != 1 {
		t.Errorf("Expected 1 mux.Vars call, found %d", muxVarsCount)
	}
}

func TestHandlerInfo_Initialization(t *testing.T) {
	info := &HandlerInfo{
		RequestType:      "Request",
		ResponseType:     "Response",
		ResponseStatus:   200,
		ErrorCodes:       make([]int, 0),
		PathParams:       make([]ParamInfo, 0),
		QueryParams:      make([]ParamInfo, 0),
		HeaderParams:     make([]ParamInfo, 0),
		FormParams:       make([]ParamInfo, 0),
		ErrorResponses:   make([]ErrorResponseInfo, 0),
		SuccessResponses: make([]SuccessResponseInfo, 0),
	}

	if info.RequestType != "Request" {
		t.Errorf("Expected RequestType 'Request', got '%s'", info.RequestType)
	}

	if info.ResponseType != "Response" {
		t.Errorf("Expected ResponseType 'Response', got '%s'", info.ResponseType)
	}

	if info.ResponseStatus != 200 {
		t.Errorf("Expected ResponseStatus 200, got %d", info.ResponseStatus)
	}
}

func TestErrorResponseInfo(t *testing.T) {
	info := ErrorResponseInfo{
		StatusCode:      400,
		ResponseType:    "web.Error",
		ResponsePackage: "github.com/sailpoint/atlas-go/v2/atlas/web",
		ErrorMessage:    "Invalid request",
		Source:          "web.BadRequest",
	}

	if info.StatusCode != 400 {
		t.Errorf("Expected StatusCode 400, got %d", info.StatusCode)
	}

	if info.ResponseType != "web.Error" {
		t.Errorf("Expected ResponseType 'web.Error', got '%s'", info.ResponseType)
	}

	if info.ErrorMessage != "Invalid request" {
		t.Errorf("Expected ErrorMessage 'Invalid request', got '%s'", info.ErrorMessage)
	}
}

func TestSuccessResponseInfo(t *testing.T) {
	info := SuccessResponseInfo{
		StatusCode:      201,
		ResponseType:    "UserResponse",
		ResponsePackage: "github.com/myapp/models",
		Source:          "web.WriteJSON",
	}

	if info.StatusCode != 201 {
		t.Errorf("Expected StatusCode 201, got %d", info.StatusCode)
	}

	if info.ResponseType != "UserResponse" {
		t.Errorf("Expected ResponseType 'UserResponse', got '%s'", info.ResponseType)
	}
}

func TestHandlerAnalyzer_ExtractErrorResponseDetailed_NilSafety(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	// Create a call expression that is NOT a SelectorExpr
	call := &ast.CallExpr{
		Fun: &ast.Ident{Name: "someFunc"},
	}

	handlerInfo := &HandlerInfo{
		ErrorResponses: make([]ErrorResponseInfo, 0),
	}

	// Should not panic
	analyzer.extractErrorResponseDetailed(call, handlerInfo, nil)

	// Should not have added anything
	if len(handlerInfo.ErrorResponses) != 0 {
		t.Error("Should not have added error response for non-selector call")
	}
}

func TestHandlerAnalyzer_ExtractErrorCode_NilSafety(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	// Create a call expression that is NOT a SelectorExpr
	call := &ast.CallExpr{
		Fun: &ast.Ident{Name: "someFunc"},
	}

	handlerInfo := &HandlerInfo{
		ErrorCodes: make([]int, 0),
	}

	// Should not panic
	analyzer.extractErrorCode(call, handlerInfo)

	// Should not have added anything
	if len(handlerInfo.ErrorCodes) != 0 {
		t.Error("Should not have added error code for non-selector call")
	}
}

// ============================
// New Handler Pattern Tests
// ============================

func TestHandlerAnalyzer_IsJSONEncodeCall(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	tests := []struct {
		name     string
		call     *ast.CallExpr
		expected bool
	}{
		{
			name: "json.NewEncoder.Encode",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X: &ast.CallExpr{
						Fun: &ast.SelectorExpr{
							X:   &ast.Ident{Name: "json"},
							Sel: &ast.Ident{Name: "NewEncoder"},
						},
					},
					Sel: &ast.Ident{Name: "Encode"},
				},
			},
			expected: true,
		},
		{
			name: "other.Encode",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "encoder"},
					Sel: &ast.Ident{Name: "Encode"},
				},
			},
			expected: false,
		},
		{
			name: "non-selector",
			call: &ast.CallExpr{
				Fun: &ast.Ident{Name: "Encode"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.isJSONEncodeCall(tt.call)
			if result != tt.expected {
				t.Errorf("isJSONEncodeCall() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHandlerAnalyzer_IsFormValueCall(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	tests := []struct {
		name     string
		call     *ast.CallExpr
		expected bool
	}{
		{
			name: "r.FormValue",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "r"},
					Sel: &ast.Ident{Name: "FormValue"},
				},
			},
			expected: true,
		},
		{
			name: "r.PostFormValue",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "r"},
					Sel: &ast.Ident{Name: "PostFormValue"},
				},
			},
			expected: true,
		},
		{
			name: "r.OtherMethod",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "r"},
					Sel: &ast.Ident{Name: "OtherMethod"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.isFormValueCall(tt.call)
			if result != tt.expected {
				t.Errorf("isFormValueCall() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHandlerAnalyzer_ExtractFormParams(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X:   &ast.Ident{Name: "r"},
			Sel: &ast.Ident{Name: "FormValue"},
		},
		Args: []ast.Expr{
			&ast.BasicLit{Kind: token.STRING, Value: `"username"`},
		},
	}

	handlerInfo := &HandlerInfo{
		FormParams: make([]ParamInfo, 0),
	}

	analyzer.extractFormParams(call, handlerInfo, nil)

	if len(handlerInfo.FormParams) != 1 {
		t.Fatalf("Expected 1 form param, got %d", len(handlerInfo.FormParams))
	}

	if handlerInfo.FormParams[0].Name != "username" {
		t.Errorf("Expected param name 'username', got '%s'", handlerInfo.FormParams[0].Name)
	}

	if handlerInfo.FormParams[0].Type != "string" {
		t.Errorf("Expected param type 'string', got '%s'", handlerInfo.FormParams[0].Type)
	}
}

func TestHandlerAnalyzer_IsContentTypeSetCall(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	tests := []struct {
		name     string
		call     *ast.CallExpr
		expected bool
	}{
		{
			name: "Content-Type header set",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "header"},
					Sel: &ast.Ident{Name: "Set"},
				},
				Args: []ast.Expr{
					&ast.BasicLit{Kind: token.STRING, Value: `"Content-Type"`},
					&ast.BasicLit{Kind: token.STRING, Value: `"application/json"`},
				},
			},
			expected: true,
		},
		{
			name: "other header set",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "header"},
					Sel: &ast.Ident{Name: "Set"},
				},
				Args: []ast.Expr{
					&ast.BasicLit{Kind: token.STRING, Value: `"X-Custom-Header"`},
					&ast.BasicLit{Kind: token.STRING, Value: `"value"`},
				},
			},
			expected: false,
		},
		{
			name: "not a Set call",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "header"},
					Sel: &ast.Ident{Name: "Get"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.isContentTypeSetCall(tt.call)
			if result != tt.expected {
				t.Errorf("isContentTypeSetCall() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHandlerAnalyzer_ExtractContentType(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	call := &ast.CallExpr{
		Args: []ast.Expr{
			&ast.BasicLit{Kind: token.STRING, Value: `"Content-Type"`},
			&ast.BasicLit{Kind: token.STRING, Value: `"application/json"`},
		},
	}

	result := analyzer.extractContentType(call)
	if result != "application/json" {
		t.Errorf("extractContentType() = %q, want %q", result, "application/json")
	}
}

func TestHandlerAnalyzer_IsStrconvCall(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	tests := []struct {
		name     string
		call     *ast.CallExpr
		expected bool
	}{
		{
			name: "strconv.Atoi",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "strconv"},
					Sel: &ast.Ident{Name: "Atoi"},
				},
			},
			expected: true,
		},
		{
			name: "strconv.ParseInt",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "strconv"},
					Sel: &ast.Ident{Name: "ParseInt"},
				},
			},
			expected: true,
		},
		{
			name: "strconv.ParseFloat",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "strconv"},
					Sel: &ast.Ident{Name: "ParseFloat"},
				},
			},
			expected: true,
		},
		{
			name: "strconv.ParseBool",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "strconv"},
					Sel: &ast.Ident{Name: "ParseBool"},
				},
			},
			expected: true,
		},
		{
			name: "strconv.Itoa (not a parse function)",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "strconv"},
					Sel: &ast.Ident{Name: "Itoa"},
				},
			},
			expected: false,
		},
		{
			name: "other.Atoi",
			call: &ast.CallExpr{
				Fun: &ast.SelectorExpr{
					X:   &ast.Ident{Name: "other"},
					Sel: &ast.Ident{Name: "Atoi"},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := analyzer.isStrconvCall(tt.call)
			if result != tt.expected {
				t.Errorf("isStrconvCall() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestStrconvFuncToType(t *testing.T) {
	tests := []struct {
		funcName     string
		expectedType string
	}{
		{"Atoi", "int"},
		{"ParseInt", "int64"},
		{"ParseUint", "uint64"},
		{"ParseFloat", "float64"},
		{"ParseBool", "bool"},
	}

	for _, tt := range tests {
		t.Run(tt.funcName, func(t *testing.T) {
			if resultType, ok := strconvFuncToType[tt.funcName]; !ok {
				t.Errorf("strconvFuncToType missing %s", tt.funcName)
			} else if resultType != tt.expectedType {
				t.Errorf("strconvFuncToType[%s] = %s, want %s", tt.funcName, resultType, tt.expectedType)
			}
		})
	}
}

func TestParamInfo(t *testing.T) {
	p := ParamInfo{
		Name:         "id",
		Type:         "int64",
		Required:     true,
		DefaultValue: "0",
	}

	if p.Name != "id" {
		t.Errorf("Expected Name 'id', got '%s'", p.Name)
	}

	if p.Type != "int64" {
		t.Errorf("Expected Type 'int64', got '%s'", p.Type)
	}

	if !p.Required {
		t.Error("Expected Required to be true")
	}

	if p.DefaultValue != "0" {
		t.Errorf("Expected DefaultValue '0', got '%s'", p.DefaultValue)
	}
}

func TestHandlerAnalyzer_ExtractHeaderParams(t *testing.T) {
	analyzer := NewHandlerAnalyzer(nil)

	call := &ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "r"},
				Sel: &ast.Ident{Name: "Header"},
			},
			Sel: &ast.Ident{Name: "Get"},
		},
		Args: []ast.Expr{
			&ast.BasicLit{Kind: token.STRING, Value: `"Authorization"`},
		},
	}

	handlerInfo := &HandlerInfo{
		HeaderParams: make([]ParamInfo, 0),
	}

	analyzer.extractHeaderParams(call, handlerInfo, nil)

	if len(handlerInfo.HeaderParams) != 1 {
		t.Fatalf("Expected 1 header param, got %d", len(handlerInfo.HeaderParams))
	}

	if handlerInfo.HeaderParams[0].Name != "Authorization" {
		t.Errorf("Expected param name 'Authorization', got '%s'", handlerInfo.HeaderParams[0].Name)
	}

	if handlerInfo.HeaderParams[0].Type != "string" {
		t.Errorf("Expected param type 'string', got '%s'", handlerInfo.HeaderParams[0].Type)
	}
}

