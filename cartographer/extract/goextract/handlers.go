// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"
)

// HandlerInfo contains information extracted from analyzing a handler function.
type HandlerInfo struct {
	RequestType      string
	ResponseType     string
	ResponseStatus   int
	ErrorCodes       []int
	PathParams       []ParamInfo // Path parameters with type info
	QueryParams      []ParamInfo // Query parameters with type info
	HeaderParams     []ParamInfo // Header parameters
	FormParams       []ParamInfo // Form parameters from FormValue
	ContentType      string      // Detected content type for response
	ErrorResponses   []ErrorResponseInfo   // Detailed error responses with messages
	SuccessResponses []SuccessResponseInfo // Detailed success responses

	// Internal per-function state (used to improve inference)
	jsonDecoderVars map[string]bool // vars assigned from json.NewDecoder(...)
}

// ParamInfo contains information about a parameter including its detected type.
type ParamInfo struct {
	Name         string // Parameter name
	Type         string // Detected Go type (string, int, int64, float64, bool, etc.)
	Required     bool   // Whether the parameter appears to be required
	DefaultValue string // Default value if detected
}

// HandlerAnalyzer analyzes HTTP handler function bodies.
type HandlerAnalyzer struct {
	tracer *FunctionTracer // Recursive function tracer
}

// NewHandlerAnalyzer creates a new HandlerAnalyzer.
func NewHandlerAnalyzer(tracer *FunctionTracer) *HandlerAnalyzer {
	return &HandlerAnalyzer{
		tracer: tracer,
	}
}

// AnalyzeHandler analyzes a handler function to extract request/response types and error responses.
func (ha *HandlerAnalyzer) AnalyzeHandler(funcDecl *ast.FuncDecl, file *ast.File, info *types.Info) *HandlerInfo {
	if funcDecl.Body == nil {
		return nil
	}

	// Check if this looks like an HTTP handler
	if !ha.looksLikeHandler(funcDecl, info) {
		return nil
	}

	handlerInfo := &HandlerInfo{
		ErrorCodes:       make([]int, 0),
		PathParams:       make([]ParamInfo, 0),
		QueryParams:      make([]ParamInfo, 0),
		HeaderParams:     make([]ParamInfo, 0),
		FormParams:       make([]ParamInfo, 0),
		ErrorResponses:   make([]ErrorResponseInfo, 0),
		SuccessResponses: make([]SuccessResponseInfo, 0),
		ResponseStatus:   200, // Default
		jsonDecoderVars:  make(map[string]bool),
	}

	// Analyze the function body
	ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.CallExpr:
			ha.analyzeCall(node, handlerInfo, info)

		case *ast.AssignStmt:
			ha.analyzeAssignment(node, handlerInfo, info)
		}
		return true
	})

	// Deduplicate error codes from detailed responses
	ha.mergeErrorCodes(handlerInfo)

	// Only return if we found something useful
	if handlerInfo.RequestType != "" || handlerInfo.ResponseType != "" ||
		len(handlerInfo.ErrorCodes) > 0 || len(handlerInfo.ErrorResponses) > 0 {
		return handlerInfo
	}

	return nil
}

// looksLikeHandler checks if a function looks like an HTTP handler.
func (ha *HandlerAnalyzer) looksLikeHandler(funcDecl *ast.FuncDecl, info *types.Info) bool {
	// Check function signature
	if funcDecl.Type == nil || funcDecl.Type.Params == nil {
		return false
	}

	params := funcDecl.Type.Params.List

	// Common handler patterns:
	// 1. func(w http.ResponseWriter, r *http.Request)
	// 2. Returns http.HandlerFunc or http.Handler

	// Check if it returns HandlerFunc or Handler
	if funcDecl.Type.Results != nil && len(funcDecl.Type.Results.List) > 0 {
		for _, result := range funcDecl.Type.Results.List {
			if t := info.TypeOf(result.Type); t != nil {
				typeName := t.String()
				if strings.Contains(typeName, "http.HandlerFunc") ||
					strings.Contains(typeName, "http.Handler") {
					return true
				}
			}
		}
	}

	// Check if it takes (w, r) parameters
	if len(params) >= 2 {
		for _, param := range params {
			if t := info.TypeOf(param.Type); t != nil {
				typeName := t.String()
				if strings.Contains(typeName, "http.ResponseWriter") ||
					strings.Contains(typeName, "http.Request") {
					return true
				}
			}
		}
	}

	return false
}

// analyzeCall analyzes a function call within the handler.
func (ha *HandlerAnalyzer) analyzeCall(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	// Check for different patterns
	switch {
	case ha.isJSONDecodeCall(call, handlerInfo):
		ha.extractRequestType(call, handlerInfo, info)

	case ha.isWriteJSONCall(call) || ha.isWriteJSONCallWithTypeInfo(call, info):
		ha.extractResponseType(call, handlerInfo, info)
		ha.extractSuccessResponseDetailed(call, handlerInfo, info)

	case ha.isJSONEncodeCall(call):
		// json.NewEncoder(w).Encode(data) pattern
		ha.extractJSONEncodeResponseType(call, handlerInfo, info)

	case ha.isErrorCall(call) || ha.isErrorCallWithTypeInfo(call, info):
		ha.extractErrorCode(call, handlerInfo)
		ha.extractErrorResponseDetailed(call, handlerInfo, info)

	case ha.isHttpNotFoundCall(call):
		// Standard library http.NotFound
		handlerInfo.ErrorCodes = append(handlerInfo.ErrorCodes, 404)
		handlerInfo.ErrorResponses = append(handlerInfo.ErrorResponses, ErrorResponseInfo{
			StatusCode: 404,
			Source:     "http.NotFound",
		})

	case ha.isHttpErrorCall(call):
		// Standard library http.Error - extract status code
		statusCode := ha.extractHttpErrorStatusCode(call, info)
		if statusCode > 0 {
			handlerInfo.ErrorCodes = append(handlerInfo.ErrorCodes, statusCode)
			handlerInfo.ErrorResponses = append(handlerInfo.ErrorResponses, ErrorResponseInfo{
				StatusCode: statusCode,
				Source:     "http.Error",
			})
		}

	case ha.isWriteHeaderCall(call):
		// Extract explicit status code from WriteHeader
		statusCode := ha.extractWriteHeaderStatusCode(call, info)
		if statusCode > 0 && statusCode != 200 {
			handlerInfo.ResponseStatus = statusCode
		}

	case ha.isContentTypeSetCall(call):
		// w.Header().Set("Content-Type", "...") - extract content type
		contentType := ha.extractContentType(call)
		if contentType != "" {
			handlerInfo.ContentType = contentType
		}

	case ha.isWriteCall(call, info):
		// Plain writes (often text/plain). Capture content type + response body shape.
		ha.extractWriteResponse(call, handlerInfo, info)

	case ha.isAtlasQueryOptionsCall(call):
		// Atlas middleware-style query parsing: add canonical v3 query params
		ha.addAtlasV3QueryParams(handlerInfo)

	case ha.isQueryContextCall(call):
		// Pattern: model.GetQuery(r.Context()) or similar: indicates query options middleware in effect
		ha.addAtlasV3QueryParams(handlerInfo)

	case ha.isCustomErrorWrapper(call, handlerInfo, info):
		// Use tracer for recursive analysis
		if ha.tracer != nil {
			ha.traceCustomFunction(call, handlerInfo, info)
		}

	case ha.isMuxVarsCall(call):
		// Path parameters being accessed - extract actual param names
		ha.extractMuxVarsParams(call, handlerInfo, info)

	case ha.isURLQueryCall(call):
		// Query parameters being accessed
		ha.extractQueryParams(call, handlerInfo, info)

	case ha.isFormValueCall(call):
		// Form parameters from r.FormValue("name")
		ha.extractFormParams(call, handlerInfo, info)

	case ha.isHeaderGetCall(call):
		// Header parameters being accessed
		ha.extractHeaderParams(call, handlerInfo, info)

	case ha.isStrconvCall(call):
		// Type conversions - detect parameter types
		ha.extractParamTypeFromConversion(call, handlerInfo, info)
	}
}

// =========================================================================
// Plain response writes (non-JSON)
// =========================================================================

// isWriteCall checks if a call is responseWriter.Write(...).
func (ha *HandlerAnalyzer) isWriteCall(call *ast.CallExpr, info *types.Info) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "Write" {
		return false
	}
	if info == nil {
		return false
	}
	t := info.TypeOf(sel.X)
	if t == nil {
		return false
	}
	return strings.Contains(t.String(), "http.ResponseWriter")
}

func (ha *HandlerAnalyzer) extractWriteResponse(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	// If no explicit Content-Type has been set, default to text/plain for plain writes.
	if handlerInfo.ContentType == "" {
		handlerInfo.ContentType = "text/plain"
	}
	// If no explicit status has been set, default remains 200.

	// Best-effort: if writing bytes or strings, represent response as string.
	if handlerInfo.ResponseType == "" {
		handlerInfo.ResponseType = "string"
	}
}

// =========================================================================
// Atlas query options (middleware-style)
// =========================================================================

func (ha *HandlerAnalyzer) isAtlasQueryOptionsCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	switch sel.Sel.Name {
	case "GetQueryOptions", "GetQueryOptionsWithFilterParser", "GetQueryOptionsWithFilterParserV3":
		return true
	default:
		return false
	}
}

func (ha *HandlerAnalyzer) addAtlasV3QueryParams(handlerInfo *HandlerInfo) {
	add := func(name, typ, def string) {
		for _, p := range handlerInfo.QueryParams {
			if p.Name == name {
				return
			}
		}
		handlerInfo.QueryParams = append(handlerInfo.QueryParams, ParamInfo{
			Name:         name,
			Type:         typ,
			Required:     false,
			DefaultValue: def,
		})
	}

	add("filters", "string", "")
	add("sorters", "string", "")
	add("offset", "int", "0")
	add("limit", "int", "250")
	add("count", "bool", "false")
}

func (ha *HandlerAnalyzer) isQueryContextCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "GetQuery" {
		return false
	}
	// Typical pattern is model.GetQuery(...)
	if ident, ok := sel.X.(*ast.Ident); ok {
		return ident.Name == "model"
	}
	return false
}

// isJSONDecodeCall checks if a call is json.NewDecoder().Decode() or similar.
func (ha *HandlerAnalyzer) isJSONDecodeCall(call *ast.CallExpr, handlerInfo *HandlerInfo) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	// We only treat these as request body decoders when they are clearly JSON:
	// - json.NewDecoder(r.Body).Decode(&dst)
	// - json.Unmarshal(data, &dst)
	methodName := sel.Sel.Name

	if methodName == "Decode" {
		// Require json.NewDecoder(...) receiver
		if innerCall, ok := sel.X.(*ast.CallExpr); ok {
			if innerSel, ok := innerCall.Fun.(*ast.SelectorExpr); ok {
				if innerSel.Sel.Name == "NewDecoder" {
					if ident, ok := innerSel.X.(*ast.Ident); ok && ident.Name == "json" {
						return true
					}
				}
			}
		}
		// Also allow:
		//   decoder := json.NewDecoder(r.Body)
		//   decoder.Decode(&dst)
		if handlerInfo != nil && handlerInfo.jsonDecoderVars != nil {
			if ident, ok := sel.X.(*ast.Ident); ok {
				if handlerInfo.jsonDecoderVars[ident.Name] {
					return true
				}
			}
		}
		return false
	}

	if methodName == "Unmarshal" {
		// Require json.Unmarshal using direct package identifier
		if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "json" {
			return true
		}
		return false
	}

	return false
}

// isWriteJSONCall checks if a call is web.WriteJSON().
func (ha *HandlerAnalyzer) isWriteJSONCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	// Check for WriteJSON
	if sel.Sel.Name != "WriteJSON" {
		return false
	}

	// Check if X is "web" identifier
	if ident, ok := sel.X.(*ast.Ident); ok {
		return ident.Name == "web"
	}

	return false
}

// isWriteJSONCallWithTypeInfo checks if a call is web.WriteJSON using type information.
// This works even if the web package is imported with a different alias (e.g., atlasweb).
func (ha *HandlerAnalyzer) isWriteJSONCallWithTypeInfo(call *ast.CallExpr, info *types.Info) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	// Check if the function name is WriteJSON
	if sel.Sel.Name != "WriteJSON" {
		return false
	}

	// Use type information to check if this is from the atlas-go/web package
	if info != nil && info.Uses != nil {
		if obj := info.Uses[sel.Sel]; obj != nil {
			pkg := obj.Pkg()
			if pkg != nil && strings.Contains(pkg.Path(), "atlas-go") && strings.HasSuffix(pkg.Path(), "/web") {
				return true
			}
		}
	}

	return false
}

// isErrorCall checks if a call is a web error function.
// Enhanced to work across packages using type information.
func (ha *HandlerAnalyzer) isErrorCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	// Check if X is "web" identifier (for backward compatibility)
	if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "web" {
		return ha.isWebErrorFunction(sel.Sel.Name)
	}

	return false
}

// isErrorCallWithTypeInfo checks if a call is a web error function using type information.
// This works even if the web package is imported with a different name.
func (ha *HandlerAnalyzer) isErrorCallWithTypeInfo(call *ast.CallExpr, info *types.Info) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	// Use type information to check if this is a web package function
	if info != nil && info.Uses != nil {
		if obj := info.Uses[sel.Sel]; obj != nil {
			// Check if the function belongs to the atlas-go/web package
			pkg := obj.Pkg()
			if pkg != nil && strings.Contains(pkg.Path(), "atlas-go") && strings.HasSuffix(pkg.Path(), "/web") {
				return ha.isWebErrorFunction(sel.Sel.Name)
			}
		}
	}

	return false
}

// isWebErrorFunction checks if a function name is a known web error function.
func (ha *HandlerAnalyzer) isWebErrorFunction(funcName string) bool {
	errorFuncs := []string{
		"BadRequest", "Unauthorized", "Forbidden", "NotFound",
		"InternalServerError", "ServiceUnavailable", "Gone",
		"ContextCanceled", "NoContent",
	}

	for _, fn := range errorFuncs {
		if funcName == fn {
			return true
		}
	}
	return false
}

// isMuxVarsCall checks if a call is mux.Vars().
func (ha *HandlerAnalyzer) isMuxVarsCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	if sel.Sel.Name == "Vars" {
		if ident, ok := sel.X.(*ast.Ident); ok {
			return ident.Name == "mux"
		}
	}

	return false
}

// isCustomErrorWrapper checks if this might be a custom error wrapper function.
// Custom wrappers are functions that might wrap web error functions.
func (ha *HandlerAnalyzer) isCustomErrorWrapper(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) bool {
	// Check if this is a method or function call
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		// Could be a direct function call like sendError()
		if ident, ok := call.Fun.(*ast.Ident); ok {
			// Check if the function has "error", "fail", "bad" etc. in the name
			return ha.looksLikeErrorFunction(ident.Name)
		}
		return false
	}

	// Method call like s.sendError() or helpers.BadRequest()
	return ha.looksLikeErrorFunction(sel.Sel.Name)
}

// looksLikeErrorFunction checks if a function name suggests it's an error response function.
func (ha *HandlerAnalyzer) looksLikeErrorFunction(name string) bool {
	nameLower := strings.ToLower(name)
	errorKeywords := []string{
		"error", "fail", "bad", "invalid", "notfound",
		"unauthorized", "forbidden", "sendError", "handleError",
		"respondError", "writeError",
	}

	for _, keyword := range errorKeywords {
		if strings.Contains(nameLower, keyword) {
			return true
		}
	}
	return false
}

// traceCustomFunction uses the FunctionTracer to recursively analyze a custom function call.
func (ha *HandlerAnalyzer) traceCustomFunction(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	// Use the tracer to analyze this function call
	analysis := ha.tracer.TraceFunction(call, info)
	if analysis == nil {
		return
	}

	// Merge traced results into handler info
	handlerInfo.ErrorResponses = append(handlerInfo.ErrorResponses, analysis.ErrorResponses...)
	handlerInfo.SuccessResponses = append(handlerInfo.SuccessResponses, analysis.SuccessResponses...)
}

// extractErrorResponseDetailed extracts detailed error response information.
func (ha *HandlerAnalyzer) extractErrorResponseDetailed(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	methodName := sel.Sel.Name

	// Get status code
	statusCode := ha.getStatusCodeForFunction(methodName)

	errorInfo := ErrorResponseInfo{
		StatusCode:      statusCode,
		ResponseType:    "web.Error",
		ResponsePackage: "github.com/sailpoint/atlas-go/v2/atlas/web",
		Source:          "web." + methodName,
	}

	// Try to extract error message from arguments
	if len(call.Args) > 2 {
		// Third argument is usually the error
		if errorMsg := ha.extractErrorMessage(call.Args[2]); errorMsg != "" {
			errorInfo.ErrorMessage = errorMsg
		}
	}

	handlerInfo.ErrorResponses = append(handlerInfo.ErrorResponses, errorInfo)
}

// extractSuccessResponseDetailed extracts detailed success response information.
func (ha *HandlerAnalyzer) extractSuccessResponseDetailed(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	// web.WriteJSON(ctx, w, response)
	if len(call.Args) < 3 {
		return
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

	successInfo := SuccessResponseInfo{
		StatusCode:      200,
		ResponseType:    responseType,
		ResponsePackage: responsePackage,
		Source:          "web.WriteJSON",
	}

	handlerInfo.SuccessResponses = append(handlerInfo.SuccessResponses, successInfo)
}

// getStatusCodeForFunction maps function names to status codes.
// Uses the shared WebErrorFunctionStatusCodes map for consistency.
func (ha *HandlerAnalyzer) getStatusCodeForFunction(funcName string) int {
	return GetStatusCodeForErrorFunction(funcName)
}

// extractErrorMessage tries to extract the actual error message from code.
func (ha *HandlerAnalyzer) extractErrorMessage(expr ast.Expr) string {
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

// mergeErrorCodes ensures error codes in ErrorCodes slice are consistent with ErrorResponses.
func (ha *HandlerAnalyzer) mergeErrorCodes(handlerInfo *HandlerInfo) {
	// Build a set of error codes from ErrorResponses
	codeSet := make(map[int]bool)
	for _, errResp := range handlerInfo.ErrorResponses {
		codeSet[errResp.StatusCode] = true
	}

	// Add to ErrorCodes if not already present
	for code := range codeSet {
		found := false
		for _, existing := range handlerInfo.ErrorCodes {
			if existing == code {
				found = true
				break
			}
		}
		if !found {
			handlerInfo.ErrorCodes = append(handlerInfo.ErrorCodes, code)
		}
	}
}

// extractRequestType extracts the request type from a Decode/Unmarshal call.
func (ha *HandlerAnalyzer) extractRequestType(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	// json.NewDecoder(r.Body).Decode(&variable)            -> dst is arg[0]
	// json.Unmarshal(data, &variable)                     -> dst is arg[1]

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	dstIdx := 0
	if sel.Sel.Name == "Unmarshal" {
		dstIdx = 1
	}
	if len(call.Args) <= dstIdx {
		return
	}

	arg := call.Args[dstIdx]

	// Handle &variable (unary expression)
	if unary, ok := arg.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		if ident, ok := unary.X.(*ast.Ident); ok {
			// Get the type of the variable
			if t := info.TypeOf(ident); t != nil {
				handlerInfo.RequestType = TypeString(t)
			}
		} else if composite, ok := unary.X.(*ast.CompositeLit); ok {
			// &Type{} pattern
			if t := info.TypeOf(composite.Type); t != nil {
				handlerInfo.RequestType = TypeString(t)
			}
		}
	}
}

// extractResponseType extracts the response type from web.WriteJSON call.
func (ha *HandlerAnalyzer) extractResponseType(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	// web.WriteJSON(ctx, w, responseVariable)
	if len(call.Args) < 3 {
		return
	}

	// Third argument is the response
	responseArg := call.Args[2]

	// Get the type of the response variable
	if t := info.TypeOf(responseArg); t != nil {
		handlerInfo.ResponseType = TypeString(t)
		handlerInfo.ResponseStatus = 200 // Default success
	}
}

// extractErrorCode extracts the HTTP error code from web error calls.
func (ha *HandlerAnalyzer) extractErrorCode(call *ast.CallExpr, handlerInfo *HandlerInfo) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	methodName := sel.Sel.Name

	// Map function names to HTTP status codes
	errorCodes := map[string]int{
		"BadRequest":          400,
		"Unauthorized":        401,
		"Forbidden":           403,
		"NotFound":            404,
		"Gone":                410,
		"InternalServerError": 500,
		"ServiceUnavailable":  503,
		"ContextCanceled":     499,
		"NoContent":           204,
	}

	if code, ok := errorCodes[methodName]; ok {
		// Add if not already present
		found := false
		for _, existing := range handlerInfo.ErrorCodes {
			if existing == code {
				found = true
				break
			}
		}
		if !found {
			handlerInfo.ErrorCodes = append(handlerInfo.ErrorCodes, code)
		}
	}
}

// analyzeAssignment analyzes variable assignments to find potential request/response types.
func (ha *HandlerAnalyzer) analyzeAssignment(assign *ast.AssignStmt, handlerInfo *HandlerInfo, info *types.Info) {
	// Look for patterns like:
	// var request RequestType
	// request := RequestType{}

	if len(assign.Lhs) == 0 || len(assign.Rhs) == 0 {
		return
	}

	// Check right-hand side for composite literals or type conversions
	for _, rhs := range assign.Rhs {
		// Track decoder := json.NewDecoder(...)
		if call, ok := rhs.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if sel.Sel.Name == "NewDecoder" {
					if ident, ok := sel.X.(*ast.Ident); ok && ident.Name == "json" {
						for _, lhs := range assign.Lhs {
							if id, ok := lhs.(*ast.Ident); ok {
								if handlerInfo != nil && handlerInfo.jsonDecoderVars != nil {
									handlerInfo.jsonDecoderVars[id.Name] = true
								}
							}
						}
					}
				}
			}
		}

		if composite, ok := rhs.(*ast.CompositeLit); ok {
			if t := info.TypeOf(composite.Type); t != nil {
				typeName := TypeString(t)
				// Heuristic: types with "Request" in the name are likely request types
				if strings.Contains(typeName, "Request") || strings.Contains(typeName, "Input") {
					if handlerInfo.RequestType == "" {
						handlerInfo.RequestType = typeName
					}
				}
			}
		}
	}
}

// isHttpNotFoundCall checks if a call is http.NotFound().
func (ha *HandlerAnalyzer) isHttpNotFoundCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	if sel.Sel.Name != "NotFound" {
		return false
	}

	if ident, ok := sel.X.(*ast.Ident); ok {
		return ident.Name == "http"
	}

	return false
}

// isHttpErrorCall checks if a call is http.Error().
func (ha *HandlerAnalyzer) isHttpErrorCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	if sel.Sel.Name != "Error" {
		return false
	}

	if ident, ok := sel.X.(*ast.Ident); ok {
		return ident.Name == "http"
	}

	return false
}

// extractHttpErrorStatusCode extracts the status code from http.Error(w, msg, statusCode).
func (ha *HandlerAnalyzer) extractHttpErrorStatusCode(call *ast.CallExpr, info *types.Info) int {
	// http.Error(w, message, statusCode)
	if len(call.Args) < 3 {
		return 0
	}

	// Third argument is the status code
	statusArg := call.Args[2]

	// Check for http.Status* constants
	if sel, ok := statusArg.(*ast.SelectorExpr); ok {
		return ha.httpStatusConstantToCode(sel.Sel.Name)
	}

	// Check for integer literal
	if lit, ok := statusArg.(*ast.BasicLit); ok && lit.Kind == token.INT {
		var code int
		fmt.Sscanf(lit.Value, "%d", &code)
		return code
	}

	return 0
}

// isWriteHeaderCall checks if a call is w.WriteHeader().
func (ha *HandlerAnalyzer) isWriteHeaderCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	return sel.Sel.Name == "WriteHeader"
}

// extractWriteHeaderStatusCode extracts the status code from w.WriteHeader(statusCode).
func (ha *HandlerAnalyzer) extractWriteHeaderStatusCode(call *ast.CallExpr, info *types.Info) int {
	if len(call.Args) < 1 {
		return 0
	}

	statusArg := call.Args[0]

	// Check for http.Status* constants
	if sel, ok := statusArg.(*ast.SelectorExpr); ok {
		return ha.httpStatusConstantToCode(sel.Sel.Name)
	}

	// Check for integer literal
	if lit, ok := statusArg.(*ast.BasicLit); ok && lit.Kind == token.INT {
		var code int
		fmt.Sscanf(lit.Value, "%d", &code)
		return code
	}

	return 0
}

// httpStatusConstantToCode converts http.Status* constant names to status codes.
func (ha *HandlerAnalyzer) httpStatusConstantToCode(name string) int {
	statusCodes := map[string]int{
		"StatusOK":                  200,
		"StatusCreated":             201,
		"StatusAccepted":            202,
		"StatusNoContent":           204,
		"StatusMovedPermanently":    301,
		"StatusFound":               302,
		"StatusBadRequest":          400,
		"StatusUnauthorized":        401,
		"StatusForbidden":           403,
		"StatusNotFound":            404,
		"StatusMethodNotAllowed":    405,
		"StatusConflict":            409,
		"StatusGone":                410,
		"StatusInternalServerError": 500,
		"StatusNotImplemented":      501,
		"StatusBadGateway":          502,
		"StatusServiceUnavailable":  503,
	}

	if code, ok := statusCodes[name]; ok {
		return code
	}
	return 0
}

// isURLQueryCall checks if a call is accessing URL query parameters.
// Looks for patterns like r.URL.Query().Get("name") or query.Get("name")
func (ha *HandlerAnalyzer) isURLQueryCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	// Check for .Get() method
	if sel.Sel.Name != "Get" {
		return false
	}

	// Check if this looks like a query access
	// Could be r.URL.Query().Get() or queryValues.Get()
	return true // We'll extract the param name if args are present
}

// extractQueryParams extracts query parameter names from query.Get("name") calls.
func (ha *HandlerAnalyzer) extractQueryParams(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	if len(call.Args) < 1 {
		return
	}

	// First argument should be the parameter name
	if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
		paramName := strings.Trim(lit.Value, "\"'`")
		if paramName != "" {
			// Check if already added
			for _, existing := range handlerInfo.QueryParams {
				if existing.Name == paramName {
					return
				}
			}
			handlerInfo.QueryParams = append(handlerInfo.QueryParams, ParamInfo{
				Name: paramName,
				Type: "string", // Default to string, can be updated by conversion detection
			})
		}
	}
}

// extractMuxVarsParams extracts path parameter names from mux.Vars(r)["name"] accesses.
func (ha *HandlerAnalyzer) extractMuxVarsParams(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	// This is called when we detect mux.Vars() - we need to find the index expressions
	// that access specific keys from the returned map
	// For now, mark that path params are used; actual names are extracted from route patterns
	// Path params are cleared to be populated from route pattern
	if len(handlerInfo.PathParams) == 0 {
		handlerInfo.PathParams = make([]ParamInfo, 0)
	}
}

// isHeaderGetCall checks if a call is accessing request headers.
// Looks for r.Header.Get("name")
func (ha *HandlerAnalyzer) isHeaderGetCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	if sel.Sel.Name != "Get" {
		return false
	}

	// Check if X is a Header selector
	if innerSel, ok := sel.X.(*ast.SelectorExpr); ok {
		return innerSel.Sel.Name == "Header"
	}

	return false
}

// extractHeaderParams extracts header parameter names from r.Header.Get("name") calls.
func (ha *HandlerAnalyzer) extractHeaderParams(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	if len(call.Args) < 1 {
		return
	}

	// First argument should be the header name
	if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
		headerName := strings.Trim(lit.Value, "\"'`")
		if headerName != "" {
			// Check if already added
			for _, existing := range handlerInfo.HeaderParams {
				if existing.Name == headerName {
					return
				}
			}
			handlerInfo.HeaderParams = append(handlerInfo.HeaderParams, ParamInfo{
				Name: headerName,
				Type: "string", // Headers are always strings
			})
		}
	}
}

// ============================
// JSON Encoder Pattern Support
// ============================

// isJSONEncodeCall checks if a call is json.NewEncoder(w).Encode(data).
func (ha *HandlerAnalyzer) isJSONEncodeCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	// Check for Encode() method
	if sel.Sel.Name != "Encode" {
		return false
	}

	// Check if X is a call to json.NewEncoder
	if innerCall, ok := sel.X.(*ast.CallExpr); ok {
		if innerSel, ok := innerCall.Fun.(*ast.SelectorExpr); ok {
			if innerSel.Sel.Name == "NewEncoder" {
				if ident, ok := innerSel.X.(*ast.Ident); ok {
					return ident.Name == "json"
				}
			}
		}
	}

	return false
}

// extractJSONEncodeResponseType extracts the response type from json.NewEncoder(w).Encode(data).
func (ha *HandlerAnalyzer) extractJSONEncodeResponseType(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	if len(call.Args) < 1 {
		return
	}

	// First argument is the data being encoded
	dataArg := call.Args[0]

	if t := info.TypeOf(dataArg); t != nil {
		handlerInfo.ResponseType = TypeString(t)
		if handlerInfo.ContentType == "" {
			handlerInfo.ContentType = "application/json"
		}

		// Add success response
		successInfo := SuccessResponseInfo{
			StatusCode:   200,
			ResponseType: TypeString(t),
			Source:       "json.Encode",
		}
		if named, ok := t.(*types.Named); ok {
			if pkg := named.Obj().Pkg(); pkg != nil {
				successInfo.ResponsePackage = pkg.Path()
			}
		}
		handlerInfo.SuccessResponses = append(handlerInfo.SuccessResponses, successInfo)
	}
}

// ============================
// Form Parameter Support
// ============================

// isFormValueCall checks if a call is r.FormValue("name").
func (ha *HandlerAnalyzer) isFormValueCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	return sel.Sel.Name == "FormValue" || sel.Sel.Name == "PostFormValue"
}

// extractFormParams extracts form parameter names from r.FormValue("name") calls.
func (ha *HandlerAnalyzer) extractFormParams(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	if len(call.Args) < 1 {
		return
	}

	if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
		paramName := strings.Trim(lit.Value, "\"'`")
		if paramName != "" {
			// Check if already added
			for _, existing := range handlerInfo.FormParams {
				if existing.Name == paramName {
					return
				}
			}
			handlerInfo.FormParams = append(handlerInfo.FormParams, ParamInfo{
				Name: paramName,
				Type: "string", // Form values are strings by default
			})
		}
	}
}

// ============================
// Content-Type Detection
// ============================

// isContentTypeSetCall checks if a call sets the Content-Type header.
// Pattern: w.Header().Set("Content-Type", "...")
func (ha *HandlerAnalyzer) isContentTypeSetCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	if sel.Sel.Name != "Set" {
		return false
	}

	// Check if we have at least 2 arguments and first is "Content-Type"
	if len(call.Args) < 2 {
		return false
	}

	if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
		headerName := strings.Trim(lit.Value, "\"'`")
		return strings.EqualFold(headerName, "Content-Type")
	}

	return false
}

// extractContentType extracts the content type value from w.Header().Set("Content-Type", "...").
func (ha *HandlerAnalyzer) extractContentType(call *ast.CallExpr) string {
	if len(call.Args) < 2 {
		return ""
	}

	if lit, ok := call.Args[1].(*ast.BasicLit); ok && lit.Kind == token.STRING {
		return strings.Trim(lit.Value, "\"'`")
	}

	return ""
}

// ============================
// Type Conversion Detection
// ============================

// strconvFuncToType maps strconv function names to their result types.
var strconvFuncToType = map[string]string{
	"Atoi":       "int",
	"ParseInt":   "int64",
	"ParseUint":  "uint64",
	"ParseFloat": "float64",
	"ParseBool":  "bool",
}

// isStrconvCall checks if a call is a strconv conversion function.
func (ha *HandlerAnalyzer) isStrconvCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	// Check if it's a strconv package function
	if ident, ok := sel.X.(*ast.Ident); ok {
		if ident.Name == "strconv" {
			_, isConversion := strconvFuncToType[sel.Sel.Name]
			return isConversion
		}
	}

	return false
}

// extractParamTypeFromConversion detects parameter types from strconv conversions.
// Pattern: strconv.Atoi(r.URL.Query().Get("id")) -> id is int
func (ha *HandlerAnalyzer) extractParamTypeFromConversion(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}

	resultType, ok := strconvFuncToType[sel.Sel.Name]
	if !ok {
		return
	}

	// Check arguments for parameter access patterns
	if len(call.Args) < 1 {
		return
	}

	// Look for r.URL.Query().Get("name") or r.FormValue("name") in the argument
	paramName, paramSource := ha.findParamAccessInExpr(call.Args[0])
	if paramName == "" {
		return
	}

	// Update the parameter type based on the conversion
	switch paramSource {
	case "query":
		for i, p := range handlerInfo.QueryParams {
			if p.Name == paramName {
				handlerInfo.QueryParams[i].Type = resultType
				return
			}
		}
		// Add new param with the detected type
		handlerInfo.QueryParams = append(handlerInfo.QueryParams, ParamInfo{
			Name: paramName,
			Type: resultType,
		})

	case "form":
		for i, p := range handlerInfo.FormParams {
			if p.Name == paramName {
				handlerInfo.FormParams[i].Type = resultType
				return
			}
		}
		handlerInfo.FormParams = append(handlerInfo.FormParams, ParamInfo{
			Name: paramName,
			Type: resultType,
		})

	case "path":
		for i, p := range handlerInfo.PathParams {
			if p.Name == paramName {
				handlerInfo.PathParams[i].Type = resultType
				return
			}
		}
		handlerInfo.PathParams = append(handlerInfo.PathParams, ParamInfo{
			Name: paramName,
			Type: resultType,
		})
	}
}

// findParamAccessInExpr searches an expression for parameter access patterns.
// Returns the parameter name and source (query, form, path, or empty).
func (ha *HandlerAnalyzer) findParamAccessInExpr(expr ast.Expr) (string, string) {
	var paramName, paramSource string

	ast.Inspect(expr, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		switch sel.Sel.Name {
		case "Get":
			// Could be query.Get("name") or header.Get("name")
			if len(call.Args) > 0 {
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					paramName = strings.Trim(lit.Value, "\"'`")
					// Determine source from context
					if innerSel, ok := sel.X.(*ast.CallExpr); ok {
						if innerSelExpr, ok := innerSel.Fun.(*ast.SelectorExpr); ok {
							if innerSelExpr.Sel.Name == "Query" {
								paramSource = "query"
							}
						}
					} else if innerSel, ok := sel.X.(*ast.SelectorExpr); ok {
						if innerSel.Sel.Name == "Header" {
							paramSource = "header"
						}
					} else if ident, ok := sel.X.(*ast.Ident); ok {
						// Variable named 'query' or similar
						if strings.Contains(strings.ToLower(ident.Name), "query") {
							paramSource = "query"
						}
					}
				}
			}

		case "FormValue", "PostFormValue":
			if len(call.Args) > 0 {
				if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					paramName = strings.Trim(lit.Value, "\"'`")
					paramSource = "form"
				}
			}
		}

		return paramName == "" // Continue searching if not found
	})

	return paramName, paramSource
}

// ============================
// Updated Query Parameter Extraction
// ============================

// extractQueryParams extracts query parameter names from query.Get("name") calls.
// Updated to use ParamInfo with type information.
func (ha *HandlerAnalyzer) extractQueryParamsUpdated(call *ast.CallExpr, handlerInfo *HandlerInfo, info *types.Info) {
	if len(call.Args) < 1 {
		return
	}

	// First argument should be the parameter name
	if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
		paramName := strings.Trim(lit.Value, "\"'`")
		if paramName != "" {
			// Check if already added
			for _, existing := range handlerInfo.QueryParams {
				if existing.Name == paramName {
					return
				}
			}
			handlerInfo.QueryParams = append(handlerInfo.QueryParams, ParamInfo{
				Name: paramName,
				Type: "string", // Default to string, can be updated by conversion detection
			})
		}
	}
}
