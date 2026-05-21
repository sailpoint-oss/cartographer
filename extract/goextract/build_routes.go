package goextract

import (
	"go/ast"
	"go/token"
	"go/types"
)

// AnalyzeMuxPathMethodHandler detects gorilla/mux registrations such as:
// router.Path("/x").Methods(http.MethodPost).Handler(handler)
func (ra *RouterAnalyzer) AnalyzeMuxPathMethodHandler(call *ast.CallExpr, file *ast.File, info *types.Info, fset *token.FileSet) *RouteInfo {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	if sel.Sel.Name != "Handler" && sel.Sel.Name != "HandlerFunc" {
		return nil
	}
	if len(call.Args) < 1 {
		return nil
	}

	methodsCall, ok := sel.X.(*ast.CallExpr)
	if !ok {
		return nil
	}
	methodsSel, ok := methodsCall.Fun.(*ast.SelectorExpr)
	if !ok || methodsSel.Sel.Name != "Methods" || len(methodsCall.Args) < 1 {
		return nil
	}

	pathCall, ok := methodsSel.X.(*ast.CallExpr)
	if !ok {
		return nil
	}
	pathSel, ok := pathCall.Fun.(*ast.SelectorExpr)
	if !ok || pathSel.Sel.Name != "Path" || len(pathCall.Args) < 1 {
		return nil
	}

	path := ra.extractPath(pathCall.Args[0])
	if path == "" {
		if resolved := ra.extractPathFromExpr(pathCall.Args[0], info); resolved != "" {
			path = resolved
		}
	}
	path = sanitizePath(path)
	if path == "" {
		return nil
	}

	route := &RouteInfo{
		Path:       path,
		Method:     ra.extractMethodArg(methodsCall.Args[0]),
		Rights:     make([]string, 0),
		Middleware: make([]string, 0),
	}
	if route.Method == "" {
		route.Method = "GET"
	}

	handlerName, handlerRights := ra.analyzeHandler(call.Args[0], info)
	route.HandlerName = handlerName
	route.Rights = append(route.Rights, handlerRights...)
	if route.HandlerName == "" {
		return nil
	}

	if routerName := ra.getRouterVarName(pathSel.X); routerName != "" {
		if ctx, exists := ra.subrouterContexts[routerName]; exists && ctx.PathPrefix != "" {
			route.Path = ctx.PathPrefix + route.Path
		}
		if mountPrefix, exists := ra.mountPrefixes[routerName]; exists {
			route.Path = mountPrefix + route.Path
		}
	}

	return route
}

// isBuildRoutesMethod reports whether a function is a RestEndpoint.BuildRoutes implementation.
func isBuildRoutesMethod(fn *ast.FuncDecl) bool {
	return fn != nil && fn.Name != nil && fn.Name.Name == "BuildRoutes"
}
