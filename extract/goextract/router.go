// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/sharedspec"
)

// RouteInfo contains information extracted from a router registration call.
type RouteInfo struct {
	Path        string
	Method      string
	HandlerName string
	Rights      []string
	Middleware  []string
}

// SubrouterContext tracks path prefixes and middleware from parent routers.
type SubrouterContext struct {
	PathPrefix string
	Rights     []string
}

// RouterAnalyzer analyzes router registration patterns.
// Supports:
//   - gorilla/mux: Handle, HandleFunc, PathPrefix, Subrouter
//   - chi: Get, Post, Put, Delete, Patch, Route, Group, Mount, With
//   - net/http: HandleFunc, Handle (ServeMux)
type RouterAnalyzer struct {
	subrouterContexts map[string]*SubrouterContext // Maps variable names to their context
	mountPrefixes     map[string]string            // Maps mounted subrouter variables to their mount paths
}

// NewRouterAnalyzer creates a new RouterAnalyzer.
func NewRouterAnalyzer() *RouterAnalyzer {
	return &RouterAnalyzer{
		subrouterContexts: make(map[string]*SubrouterContext),
		mountPrefixes:     make(map[string]string),
	}
}

// AnalyzeRouterCall analyzes a call expression to detect router registrations.
// Patterns detected:
//
//	gorilla/mux:
//	  router.Handle(path, handler).Methods(method)
//	  router.HandleFunc(path, handler).Methods(method)
//	  router.Handle(path, middleware(handler)).Methods(method)
//	  router.Handle(path, s.requireRight(handler, right)).Methods(method)
//	  sub := router.PathPrefix("/prefix").Subrouter()
//	  sub.Use(web.RequireRights(summarizer, "right"))
//	  sub.HandleFunc(path, handler).Methods(method)
//
//	chi:
//	  r.Get("/path", handler)
//	  r.Post("/path", handler)
//	  r.Route("/path", func(r chi.Router) {...})
//	  r.Group(func(r chi.Router) {...})
//	  r.Mount("/prefix", subrouter)
//
//	net/http:
//	  http.HandleFunc("/path", handler)
//	  mux.Handle("/path", handler)
func (ra *RouterAnalyzer) AnalyzeRouterCall(call *ast.CallExpr, file *ast.File, info *types.Info, fset *token.FileSet) *RouteInfo {
	// Check if this is a router.Handle() call
	if !ra.isRouterHandleCall(call) {
		return nil
	}

	// Must have at least 2 arguments (path, handler)
	if len(call.Args) < 2 {
		return nil
	}

	route := &RouteInfo{
		Rights:     make([]string, 0),
		Middleware: make([]string, 0),
	}

	// Extract path (first argument)
	// Note: extractPath returns the actual string value, which could be empty ("")
	// We need to distinguish between "successfully extracted empty string" and "failed to extract"
	pathArg := call.Args[0]
	pathExtracted := false

	if lit, ok := pathArg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		route.Path = ra.extractPath(pathArg)
		pathExtracted = true
	} else {
		resolvedPath := ra.extractPathFromExpr(pathArg, info)
		if resolvedPath != "" {
			route.Path = resolvedPath
			pathExtracted = true
		} else {
			return nil
		}
	}

	// Sanitize malformed paths that look like JSON keys or map literals
	route.Path = sanitizePath(route.Path)

	// Check if this call is on a subrouter and apply context
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		routerName := ra.getRouterVarName(sel.X)
		if withCall, ok := sel.X.(*ast.CallExpr); ok {
			// Support chi middleware chains like r.With(authz()).Get("/x", h).
			if withRights := ra.AnalyzeChiWith(withCall, info); len(withRights) > 0 {
				route.Rights = append(route.Rights, withRights...)
			}
		}
		if routerName != "" {
			// Check for mounted prefix
			if mountPrefix, exists := ra.mountPrefixes[routerName]; exists {
				route.Path = mountPrefix + route.Path
			}
			// Check for subrouter context
			if ctx, exists := ra.subrouterContexts[routerName]; exists {
				// Prepend path prefix
				if ctx.PathPrefix != "" {
					route.Path = ctx.PathPrefix + route.Path
				}
				// Add subrouter-level rights
				route.Rights = append(route.Rights, ctx.Rights...)
			}
		}

		// Check if this is a chi method call (Get, Post, etc.)
		if httpMethod, isChi := chiMethodToHTTP[sel.Sel.Name]; isChi {
			route.Method = httpMethod
		}
	}

	// Allow empty paths if we successfully extracted them (subrouter base paths)
	if !pathExtracted {
		return nil
	}

	// Extract handler and middleware (second argument)
	handlerArg := call.Args[1]
	handlerName, handlerRights := ra.analyzeHandler(handlerArg, info)
	route.HandlerName = handlerName
	route.Rights = append(route.Rights, handlerRights...)

	if route.HandlerName == "" {
		return nil
	}

	// Look for .Methods() call chained after .Handle() (gorilla/mux pattern)
	// Only if we haven't already determined the method from chi patterns
	if route.Method == "" {
		route.Method = ra.extractMethod(call, file, fset)
	}

	// Collapse any duplicate separators introduced by a root-prefix subrouter
	// (PathPrefix("/").Subrouter()) so paths like "//streams" become "/streams".
	route.Path = collapseSlashes(route.Path)

	return route
}

// AnalyzePathPrefix analyzes PathPrefix() calls to track subrouter prefixes.
// Pattern: sub := router.PathPrefix("/v1").Subrouter()
func (ra *RouterAnalyzer) AnalyzePathPrefix(assignStmt *ast.AssignStmt, info *types.Info) {
	if len(assignStmt.Lhs) == 0 || len(assignStmt.Rhs) == 0 {
		return
	}

	// Get the variable name being assigned (e.g., "sub")
	varName := ""
	if ident, ok := assignStmt.Lhs[0].(*ast.Ident); ok {
		varName = ident.Name
	}
	if varName == "" {
		return
	}

	// Check if RHS is a Subrouter() call
	pathPrefix := ra.extractPathPrefixFromExpr(assignStmt.Rhs[0])
	if pathPrefix != "" {
		ra.subrouterContexts[varName] = &SubrouterContext{
			PathPrefix: pathPrefix,
			Rights:     make([]string, 0),
		}
	}
}

// AnalyzeUseCall analyzes sub.Use() calls to detect middleware applied to subrouters.
// Pattern: sub.Use(web.RequireRights(summarizer, "right"))
func (ra *RouterAnalyzer) AnalyzeUseCall(call *ast.CallExpr, info *types.Info) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Use" {
		return
	}

	// Get the router variable name
	routerName := ra.getRouterVarName(sel.X)
	if routerName == "" {
		return
	}

	// Ensure context exists for this router
	if _, exists := ra.subrouterContexts[routerName]; !exists {
		ra.subrouterContexts[routerName] = &SubrouterContext{
			PathPrefix: "",
			Rights:     make([]string, 0),
		}
	}

	// Extract rights from the middleware argument
	if len(call.Args) > 0 {
		_, rights := ra.unwrapHandler(call.Args[0], info)
		ra.subrouterContexts[routerName].Rights = append(
			ra.subrouterContexts[routerName].Rights,
			rights...,
		)
	}
}

// AnalyzeChiRoute analyzes chi Route() and Group() calls to track nested routing.
// Patterns:
//
//	r.Route("/prefix", func(r chi.Router) { ... })
//	r.Group(func(r chi.Router) { ... })
func (ra *RouterAnalyzer) AnalyzeChiRoute(call *ast.CallExpr, info *types.Info) {
	isRouteOrGroup, pathPrefix := ra.isChiRouteOrGroup(call)
	if !isRouteOrGroup {
		return
	}

	// Get parent router context
	parentPrefix := ""
	parentRights := make([]string, 0)
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if parentName := ra.getRouterVarName(sel.X); parentName != "" {
			if parentCtx, exists := ra.subrouterContexts[parentName]; exists {
				parentPrefix = parentCtx.PathPrefix
				parentRights = append(parentRights, parentCtx.Rights...)
			}
			// Also check mount prefixes
			if mountPrefix, exists := ra.mountPrefixes[parentName]; exists {
				parentPrefix = mountPrefix + parentPrefix
			}
		}
	}

	// For Route calls, find the router parameter in the callback
	// Pattern: func(r chi.Router) { r.Get("/path", handler) }
	if len(call.Args) >= 1 {
		lastArg := call.Args[len(call.Args)-1]
		if funcLit, ok := lastArg.(*ast.FuncLit); ok {
			// Find the router parameter name
			if funcLit.Type.Params != nil && len(funcLit.Type.Params.List) > 0 {
				for _, param := range funcLit.Type.Params.List {
					for _, name := range param.Names {
						// Register this parameter as a subrouter with the path prefix
						ra.subrouterContexts[name.Name] = &SubrouterContext{
							PathPrefix: parentPrefix + pathPrefix,
							Rights:     parentRights,
						}
					}
				}
			}
		}
	}
}

// AnalyzeChiMount analyzes chi Mount() calls to track mounted subrouters.
// Pattern: r.Mount("/api", apiRouter)
func (ra *RouterAnalyzer) AnalyzeChiMount(call *ast.CallExpr, info *types.Info) {
	isMount, mountPath := ra.isChiMount(call)
	if !isMount {
		return
	}

	// Get parent router context for prefix propagation
	parentPrefix := ""
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if parentName := ra.getRouterVarName(sel.X); parentName != "" {
			if parentCtx, exists := ra.subrouterContexts[parentName]; exists {
				parentPrefix = parentCtx.PathPrefix
			}
			if parentMount, exists := ra.mountPrefixes[parentName]; exists {
				parentPrefix = parentMount + parentPrefix
			}
		}
	}

	// Track the mounted subrouter's prefix
	if len(call.Args) >= 2 {
		if ident, ok := call.Args[1].(*ast.Ident); ok {
			ra.mountPrefixes[ident.Name] = parentPrefix + mountPath
		}
	}
}

// AnalyzeChiWith analyzes chi With() calls to track middleware chains.
// Pattern: r.With(middleware).Get("/path", handler)
func (ra *RouterAnalyzer) AnalyzeChiWith(call *ast.CallExpr, info *types.Info) []string {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "With" {
		return nil
	}

	rights := make([]string, 0)
	for _, arg := range call.Args {
		_, argRights := ra.unwrapHandler(arg, info)
		rights = append(rights, argRights...)
	}

	return rights
}

// extractPathPrefixFromExpr extracts path prefix from expressions like:
// router.PathPrefix("/v1").Subrouter()
func (ra *RouterAnalyzer) extractPathPrefixFromExpr(expr ast.Expr) string {
	// Look for .Subrouter() call
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return ""
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Subrouter" {
		return ""
	}

	// Check if X is a PathPrefix() call
	prefixCall, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return ""
	}

	prefixSel, ok := prefixCall.Fun.(*ast.SelectorExpr)
	if !ok || prefixSel.Sel.Name != "PathPrefix" {
		return ""
	}

	// Extract the path from PathPrefix argument
	if len(prefixCall.Args) > 0 {
		return ra.extractPath(prefixCall.Args[0])
	}

	return ""
}

// getRouterVarName extracts the router variable name from an expression.
func (ra *RouterAnalyzer) getRouterVarName(expr ast.Expr) string {
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name
	}
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		return ra.getRouterVarName(sel.X)
	}
	if call, ok := expr.(*ast.CallExpr); ok {
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
			return ra.getRouterVarName(sel.X)
		}
	}
	return ""
}

// chiMethodToHTTP maps chi router methods to HTTP methods.
var chiMethodToHTTP = map[string]string{
	"Get":     "GET",
	"Post":    "POST",
	"Put":     "PUT",
	"Patch":   "PATCH",
	"Delete":  "DELETE",
	"Head":    "HEAD",
	"Options": "OPTIONS",
	"Trace":   "TRACE",
	"Connect": "CONNECT",
}

// isRouterHandleCall checks if a call expression is a router registration.
// Supports:
//   - gorilla/mux: Handle, HandleFunc
//   - chi: Get, Post, Put, Delete, Patch, Head, Options, Trace, Connect, HandleFunc
//   - net/http: Handle, HandleFunc (on ServeMux or http package)
func (ra *RouterAnalyzer) isRouterHandleCall(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}

	methodName := sel.Sel.Name

	// gorilla/mux and net/http patterns
	if methodName == "Handle" || methodName == "HandleFunc" {
		return true
	}

	// chi router patterns: r.Get, r.Post, etc.
	if _, isChi := chiMethodToHTTP[methodName]; isChi {
		return true
	}

	return false
}

// isChiRouteOrGroup checks if a call is chi's Route() or Group() for nested routing.
func (ra *RouterAnalyzer) isChiRouteOrGroup(call *ast.CallExpr) (bool, string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false, ""
	}

	methodName := sel.Sel.Name

	// chi.Route("/path", func(r chi.Router) {...})
	if methodName == "Route" {
		if len(call.Args) >= 1 {
			return true, ra.extractPath(call.Args[0])
		}
	}

	// chi.Group(func(r chi.Router) {...}) - no path prefix
	if methodName == "Group" {
		return true, ""
	}

	return false, ""
}

// isChiMount checks if a call is chi's Mount() for mounting subrouters.
// Pattern: r.Mount("/prefix", subrouter)
func (ra *RouterAnalyzer) isChiMount(call *ast.CallExpr) (bool, string) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false, ""
	}

	if sel.Sel.Name != "Mount" {
		return false, ""
	}

	if len(call.Args) >= 1 {
		return true, ra.extractPath(call.Args[0])
	}

	return false, ""
}

// sanitizePath cleans up malformed paths from extraction.
// Removes JSON literal artifacts like leading `{` or trailing `"`, and strips
// extraneous quoting or brace wrapping.
func sanitizePath(path string) string {
	// Strip leading { that comes from map literal keys being misread
	if strings.HasPrefix(path, "{\"") || strings.HasPrefix(path, "{'") {
		path = strings.TrimLeft(path, "{")
		path = strings.Trim(path, "\"'")
	}
	// Strip trailing }
	if strings.HasSuffix(path, "\"}") || strings.HasSuffix(path, "'}") {
		path = strings.TrimRight(path, "}")
		path = strings.Trim(path, "\"'")
	}
	// If the path doesn't start with / after cleanup, it's not a valid path
	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return path
}

// extractPath extracts the route path from the first argument.
func (ra *RouterAnalyzer) extractPath(arg ast.Expr) string {
	// Handle string literals
	if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		// Remove quotes
		path := lit.Value
		if len(path) >= 2 {
			// Extract string content (remove quotes)
			unquoted := path[1 : len(path)-1]
			// Empty string is valid (means base path of subrouter)
			// Return it as-is, we'll handle path prefix separately
			return unquoted
		}
	}

	return ""
}

// extractPathFromExpr attempts to resolve a path from a non-literal expression
// using Go type information (M2: constant path resolution).
func (ra *RouterAnalyzer) extractPathFromExpr(arg ast.Expr, info *types.Info) string {
	if info == nil {
		return ""
	}

	switch e := arg.(type) {
	case *ast.Ident:
		// Simple identifier - try to resolve constant value
		if val := ra.resolveConstant(e, info); val != "" {
			return val
		}
	case *ast.SelectorExpr:
		// Package.Constant - try to resolve
		if val := ra.resolveSelectorConstant(e, info); val != "" {
			return val
		}
	case *ast.BinaryExpr:
		// String concatenation: pathConst + "/suffix"
		if e.Op == token.ADD {
			left := ra.extractPathFromExpr(e.X, info)
			right := ra.extractPathFromExpr(e.Y, info)
			if left != "" || right != "" {
				return left + right
			}
		}
	case *ast.BasicLit:
		if e.Kind == token.STRING && len(e.Value) >= 2 {
			return e.Value[1 : len(e.Value)-1]
		}
	}

	return ""
}

// analyzeHandler analyzes the handler argument to extract the handler name and any security info.
func (ra *RouterAnalyzer) analyzeHandler(handler ast.Expr, info *types.Info) (string, []string) {
	// Unwrap any middleware wrappers
	return ra.unwrapHandler(handler, info)
}

// unwrapHandler recursively unwraps middleware to find the core handler and extract rights.
func (ra *RouterAnalyzer) unwrapHandler(expr ast.Expr, info *types.Info) (string, []string) {
	rights := make([]string, 0)

	switch e := expr.(type) {
	case *ast.CallExpr:
		// Type conversions to a handler: http.HandlerFunc(s.createStream) or
		// http.Handler(h). Unwrap to the converted expression so the terminal
		// handler (not the conversion) is analysed.
		if ra.isHandlerConversion(e) && len(e.Args) == 1 {
			return ra.unwrapHandler(e.Args[0], info)
		}

		// Check if this is a call expression - could be middleware
		if sel, ok := e.Fun.(*ast.SelectorExpr); ok {
			methodName := sel.Sel.Name

			// Pattern 1: Service wrapper - handles multiple argument orders:
			// Pattern A: s.requireRight("right", handler) - right first, handler second.
			// Pattern B: s.requireRight(handler, "right1", "right2") - handler first, rights after.
			// Pattern C: s.requireRight(summarizer, "right", handler) - summarizer, right, handler.
			// Receiver wrappers must be checked before generic auth function names
			// because local helpers often share names such as requireRight.
			if ra.isRequireWrapper(methodName) && len(e.Args) >= 2 {
				return ra.unwrapRequireWrapper(e.Args, info)
			}

			// Pattern 2: a generic auth-requirement call. We extract only
			// arguments that resolve to actual string values (string
			// literals or string-typed named constants). This skips
			// argument positions that hold framework-internal values such
			// as a summarizer / context / handler that happens to share
			// the call surface with the scope strings.
			//
			// Examples that match:
			//   web.RequireRights(summarizer, "right1", "right2")(handler)
			//   auth.RequireScope("read:users")(handler)
			//   middleware.RequireRoles("admin", "ops")(handler)
			if sharedspec.IsAuthFunctionName(methodName) {
				for _, arg := range e.Args {
					if right := ra.extractAuthScopeString(arg, info); right != "" {
						rights = append(rights, right)
					}
				}
				return methodName + "WrappedHandler", rights
			}

			// Pattern 4a: Generic middleware wrapper that takes the terminal
			// handler as one of its arguments, e.g. s.basicAuth(s.getTenant()),
			// middleware.Chain(h, web.RequireRights(...)), or
			// rateLimitMiddleware(rl, burst, getJTI, handler). When an argument
			// is itself a handler we recurse into it so response/status
			// analysis reaches the real handler instead of stopping at the
			// wrapper method name. Non-handler arguments are still mined for
			// auth scope strings.
			if handlerName, wrapperRights, ok := ra.unwrapGenericWrapper(e.Args, info); ok {
				rights = append(rights, wrapperRights...)
				return handlerName, rights
			}

			// Pattern 4b: Handler factory method: s.GetHandler(), s.createHandler(), etc.
			// This is a common pattern where methods return http.HandlerFunc.
			// No handler-typed argument was found, so treat the method itself as
			// the handler identity.
			return methodName, rights
		}

		// Pattern 3: <pkg>.AuthFn(...)(handler) - outer call wraps the
		// auth-requirement middleware. Extract string-literal args from
		// the inner call (skipping non-string identifiers) and recurse
		// into the outer handler argument.
		if innerCall, ok := e.Fun.(*ast.CallExpr); ok {
			if sel, ok := innerCall.Fun.(*ast.SelectorExpr); ok {
				if sharedspec.IsAuthFunctionName(sel.Sel.Name) {
					for _, arg := range innerCall.Args {
						if right := ra.extractAuthScopeString(arg, info); right != "" {
							rights = append(rights, right)
						}
					}

					// The first argument is the handler
					if len(e.Args) > 0 {
						handlerName, innerRights := ra.unwrapHandler(e.Args[0], info)
						rights = append(rights, innerRights...)
						return handlerName, rights
					}
				}
			}
		}

		// Direct function call: someFunc() or requireRight(summarizer, "right", handler)
		if ident, ok := e.Fun.(*ast.Ident); ok {
			funcName := ident.Name

			// Pattern 5: Direct function middleware wrapper: requireRight(summarizer, "right", handler)
			// This is similar to Pattern 2 but for package-level functions instead of methods
			if ra.isRequireWrapper(funcName) && len(e.Args) >= 2 {
				return ra.unwrapRequireWrapper(e.Args, info)
			}

			// Pattern 5a: package-local middleware wrapper that takes the
			// terminal handler as an argument, e.g.
			// Chain(s.listWorkflows(), web.RequireRights(...)). Recurse into the
			// handler argument so response/status analysis reaches the real
			// handler instead of stopping at the wrapper name ("Chain").
			if handlerName, wrapperRights, ok := ra.unwrapGenericWrapper(e.Args, info); ok {
				rights = append(rights, wrapperRights...)
				return handlerName, rights
			}

			return funcName, rights
		}

	case *ast.SelectorExpr:
		// Chained handler invocation: middleware.Chain(h, mw).ServeHTTP — the
		// receiver is the wrapper call carrying the terminal handler. Recurse
		// into the receiver so the real handler is analysed, not "ServeHTTP".
		if (e.Sel.Name == "ServeHTTP" || e.Sel.Name == "ServeHTTPC") {
			if _, ok := e.X.(*ast.CallExpr); ok {
				return ra.unwrapHandler(e.X, info)
			}
		}
		// Method value: s.handlerFunc
		return e.Sel.Name, rights

	case *ast.Ident:
		// Simple identifier
		return e.Name, rights

	case *ast.FuncLit:
		// Anonymous function - try to extract a meaningful name from the body
		handlerName := extractAnonymousFuncName(e)
		return handlerName, rights
	}

	return "", rights
}

func (ra *RouterAnalyzer) isRequireWrapper(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "require") &&
		(strings.Contains(lower, "right") || strings.Contains(lower, "auth"))
}

// isHandlerConversion reports whether the call is an http.HandlerFunc(x) or
// http.Handler(x) type conversion.
func (ra *RouterAnalyzer) isHandlerConversion(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if sel.Sel.Name != "HandlerFunc" && sel.Sel.Name != "Handler" {
		return false
	}
	ident, ok := sel.X.(*ast.Ident)
	return ok && ident.Name == "http"
}

// unwrapGenericWrapper handles an unrecognised middleware wrapper that carries
// the terminal handler as one of its arguments. It returns the inner handler
// name, any auth scope strings mined from the remaining arguments, and whether
// a handler-typed argument was found. When no argument looks like a handler the
// caller should treat the wrapper as a handler factory (ok == false).
func (ra *RouterAnalyzer) unwrapGenericWrapper(args []ast.Expr, info *types.Info) (string, []string, bool) {
	rights := make([]string, 0)
	handlerIdx := -1
	for i, arg := range args {
		if ra.isHandlerLikeArg(arg, info) {
			// Prefer the last handler-typed argument; handlers are conventionally
			// the final wrapper argument (rateLimit(..., handler)) while a leading
			// handler (Chain(h, mw)) is still the only handler-typed one.
			handlerIdx = i
			continue
		}
		// Non-handler argument: mine it for scope strings, recursing into nested
		// auth-requirement calls (e.g. web.RequireRights(summarizer, "right")).
		if right := ra.extractAuthScopeString(arg, info); right != "" {
			rights = append(rights, right)
			continue
		}
		if _, argRights := ra.unwrapHandler(arg, info); len(argRights) > 0 {
			rights = append(rights, argRights...)
		}
	}
	if handlerIdx < 0 {
		return "", rights, false
	}
	name, innerRights := ra.unwrapHandler(args[handlerIdx], info)
	rights = append(rights, innerRights...)
	if name == "" {
		return "", rights, false
	}
	return name, rights, true
}

// isHandlerLikeArg reports whether an argument is an HTTP handler. It prefers
// Go type information (http.Handler / http.HandlerFunc) and falls back to a
// narrow structural check for inline function literals and handler conversions.
func (ra *RouterAnalyzer) isHandlerLikeArg(expr ast.Expr, info *types.Info) bool {
	if info != nil {
		if t := info.TypeOf(expr); t != nil {
			ts := t.String()
			if strings.Contains(ts, "http.Handler") || strings.Contains(ts, "http.HandlerFunc") {
				return true
			}
		}
	}
	switch e := expr.(type) {
	case *ast.FuncLit:
		return true
	case *ast.CallExpr:
		// http.HandlerFunc(...) / http.Handler(...) conversion.
		return ra.isHandlerConversion(e)
	}
	return false
}

// collapseSlashes collapses repeated path separators (e.g. "//x" -> "/x") that
// arise when a root PathPrefix("/") subrouter prefix is concatenated with a
// child route path. URL paths always begin with a single "/", so this is safe.
func collapseSlashes(path string) string {
	if !strings.Contains(path, "//") {
		return path
	}
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return path
}

func (ra *RouterAnalyzer) unwrapRequireWrapper(args []ast.Expr, info *types.Info) (string, []string) {
	rights := make([]string, 0)
	if len(args) == 0 {
		return "", rights
	}

	// Pattern C: requireRight(summarizer, "right", handler)
	if len(args) == 3 && ra.looksStringLike(args[1], info) {
		if right := ra.extractRightString(args[1], info); right != "" {
			rights = append(rights, right)
		}
		handlerName, innerRights := ra.unwrapHandler(args[2], info)
		rights = append(rights, innerRights...)
		return handlerName, rights
	}

	// Pattern A: requireRight("right", handler)
	if len(args) == 2 && ra.looksStringLike(args[0], info) {
		if right := ra.extractRightString(args[0], info); right != "" {
			rights = append(rights, right)
		}
		handlerName, innerRights := ra.unwrapHandler(args[1], info)
		rights = append(rights, innerRights...)
		return handlerName, rights
	}

	// Pattern B: requireRight(handler, "right1", "right2", ...)
	for i := 1; i < len(args); i++ {
		if right := ra.extractRightString(args[i], info); right != "" {
			rights = append(rights, right)
		}
	}
	handlerName, innerRights := ra.unwrapHandler(args[0], info)
	rights = append(rights, innerRights...)
	return handlerName, rights
}

// extractAnonymousFuncName tries to extract a meaningful name from an anonymous function.
// Looks for common patterns like method calls within the function body.
func extractAnonymousFuncName(funcLit *ast.FuncLit) string {
	if funcLit.Body == nil {
		return "anonymousHandler"
	}

	// Walk the function body to find the first significant call
	var handlerName string
	ast.Inspect(funcLit.Body, func(n ast.Node) bool {
		if handlerName != "" {
			return false
		}
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				name := sel.Sel.Name
				// Skip common utility/framework calls
				if name != "Error" && name != "WriteHeader" && name != "Write" &&
					name != "Encode" && name != "Decode" && name != "JSON" &&
					name != "Printf" && name != "Fprintf" && name != "Sprintf" &&
					!strings.HasPrefix(name, "Log") && !strings.HasPrefix(name, "log") {
					handlerName = name
					return false
				}
			}
			if ident, ok := call.Fun.(*ast.Ident); ok {
				name := ident.Name
				if name != "panic" && name != "print" && name != "println" {
					handlerName = name
					return false
				}
			}
		}
		return true
	})

	if handlerName != "" {
		return handlerName
	}
	return "anonymousHandler"
}

// isWebPackage checks if an expression refers to the web package.
func (ra *RouterAnalyzer) isWebPackage(expr ast.Expr) bool {
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name == "web"
	}
	return false
}

// isStringArg checks if an expression is a string literal.
// This is used to detect argument order in middleware wrappers like requireRight.
func (ra *RouterAnalyzer) isStringArg(expr ast.Expr) bool {
	if lit, ok := expr.(*ast.BasicLit); ok {
		return lit.Kind == token.STRING
	}
	return false
}

func (ra *RouterAnalyzer) looksStringLike(expr ast.Expr, info *types.Info) bool {
	if ra.isStringArg(expr) {
		return true
	}
	return ra.extractAuthScopeString(expr, info) != ""
}

// extractRightString extracts a right string from an expression (handles literals and constants).
// Now resolves constant values using type information.
func (ra *RouterAnalyzer) extractRightString(expr ast.Expr, info *types.Info) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		// String literal: "api:scope:read"
		if e.Kind == token.STRING && len(e.Value) >= 2 {
			return e.Value[1 : len(e.Value)-1]
		}
	case *ast.Ident:
		// Simple identifier - try to resolve to constant value
		if value := ra.resolveConstant(e, info); value != "" {
			return value
		}
		// Fall back to identifier name if we can't resolve
		return e.Name
	case *ast.SelectorExpr:
		// Constant or variable: constants.SomeRight
		if value := ra.resolveSelectorConstant(e, info); value != "" {
			return value
		}
		// Fall back to extracted identifier if we can't resolve
		return ra.extractIdentifier(e)
	}
	return ""
}

// extractAuthScopeString is the strict variant of extractRightString used by
// the generic auth-function matcher. It only returns values that are
// definitely strings — string literals or named constants whose typed value
// is a string. Bare identifiers that don't resolve to a string constant
// (e.g. a "summarizer" middleware variable passed as an argument) return
// the empty string so they are not mistaken for scope tokens.
func (ra *RouterAnalyzer) extractAuthScopeString(expr ast.Expr, info *types.Info) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind == token.STRING && len(e.Value) >= 2 {
			return e.Value[1 : len(e.Value)-1]
		}
	case *ast.Ident:
		if value := ra.resolveConstant(e, info); value != "" {
			return value
		}
	case *ast.SelectorExpr:
		if value := ra.resolveSelectorConstant(e, info); value != "" {
			return value
		}
	}
	return ""
}

// resolveConstant attempts to resolve an identifier to its constant string value.
func (ra *RouterAnalyzer) resolveConstant(ident *ast.Ident, info *types.Info) string {
	if info == nil || info.Uses == nil {
		return ""
	}

	// Look up the object this identifier refers to
	obj := info.Uses[ident]
	if obj == nil {
		return ""
	}

	// Check if it's a constant
	if constObj, ok := obj.(*types.Const); ok {
		// Get the constant value
		if constObj.Val() != nil && constObj.Val().Kind() == constant.String {
			// Remove quotes from constant string value
			strVal := constant.StringVal(constObj.Val())
			return strVal
		}
	}

	return ""
}

// resolveSelectorConstant attempts to resolve a selector expression to its constant string value.
func (ra *RouterAnalyzer) resolveSelectorConstant(sel *ast.SelectorExpr, info *types.Info) string {
	if info == nil || info.Uses == nil {
		return ""
	}

	// Look up the object this selector refers to
	obj := info.Uses[sel.Sel]
	if obj == nil {
		return ""
	}

	// Check if it's a constant
	if constObj, ok := obj.(*types.Const); ok {
		// Get the constant value
		if constObj.Val() != nil && constObj.Val().Kind() == constant.String {
			strVal := constant.StringVal(constObj.Val())
			return strVal
		}
	}

	return ""
}

// extractMethod extracts the HTTP method from a chained .Methods() call.
func (ra *RouterAnalyzer) extractMethod(handleCall *ast.CallExpr, file *ast.File, fset *token.FileSet) string {
	// Look for the parent expression which should be .Methods()
	// This requires examining the surrounding context in the AST

	// For now, we'll do a simple approach: look for .Methods() calls near this Handle() call
	var method string

	// Walk the file to find Method calls that reference this Handle call
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if sel.Sel.Name == "Methods" {
					// Check if X is our Handle call
					if sel.X == handleCall || ra.isSameCall(sel.X, handleCall) {
						// Extract the method argument
						if len(call.Args) > 0 {
							method = ra.extractMethodArg(call.Args[0])
						}
					}
				}
			}
		}
		return method == "" // Continue searching if not found
	})

	// Default to GET if not specified
	if method == "" {
		method = "GET"
	}

	return method
}

// isSameCall checks if two call expressions are the same (simple pointer comparison).
func (ra *RouterAnalyzer) isSameCall(a, b ast.Expr) bool {
	// In the AST, if they're the same call, they'll be the same node
	return a == b
}

// extractMethodArg extracts the method string from a Methods() argument.
func (ra *RouterAnalyzer) extractMethodArg(arg ast.Expr) string {
	// Handle string literal: "GET"
	if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		method := lit.Value
		if len(method) >= 2 {
			return method[1 : len(method)-1]
		}
	}

	// Handle selector: http.MethodGet
	if sel, ok := arg.(*ast.SelectorExpr); ok {
		methodName := sel.Sel.Name
		// Convert http.MethodGet -> GET
		if strings.HasPrefix(methodName, "Method") {
			return strings.ToUpper(methodName[6:])
		}
		return strings.ToUpper(methodName)
	}

	return ""
}

// IsValidHTTPMethod checks if a method is a valid HTTP method.
// Includes QUERY method added in OpenAPI 3.2.
func IsValidHTTPMethod(method string) bool {
	validMethods := map[string]bool{
		"GET":     true,
		"POST":    true,
		"PUT":     true,
		"PATCH":   true,
		"DELETE":  true,
		"HEAD":    true,
		"OPTIONS": true,
		"TRACE":   true,
		"QUERY":   true, // OpenAPI 3.2
	}
	return validMethods[strings.ToUpper(method)]
}

// IsOpenAPI32Method checks if a method is only valid in OpenAPI 3.2.
func IsOpenAPI32Method(method string) bool {
	return strings.ToUpper(method) == "QUERY"
}

// extractIdentifier extracts a fully qualified identifier name.
func (ra *RouterAnalyzer) extractIdentifier(sel *ast.SelectorExpr) string {
	parts := []string{sel.Sel.Name}

	// Walk up the selector chain
	current := sel.X
	for {
		switch e := current.(type) {
		case *ast.SelectorExpr:
			parts = append([]string{e.Sel.Name}, parts...)
			current = e.X
		case *ast.Ident:
			parts = append([]string{e.Name}, parts...)
			return strings.Join(parts, ".")
		default:
			return strings.Join(parts, ".")
		}
	}
}

// ExtractPathParams extracts path parameter names from a route path.
// Example: "/approvals/{id}/comments" -> ["id"]
func ExtractPathParams(path string) []string {
	params := make([]string, 0)

	inParam := false
	paramStart := 0

	for i, ch := range path {
		if ch == '{' {
			inParam = true
			paramStart = i + 1
		} else if ch == '}' && inParam {
			paramName := path[paramStart:i]
			params = append(params, paramName)
			inParam = false
		}
	}

	return params
}
