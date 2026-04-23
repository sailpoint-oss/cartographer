// Package pythonextract provides tree-sitter-based OpenAPI extraction for
// Python services.
//
// Supported web frameworks:
//
//   - FastAPI    — @app.get / @router.post / @router.api_route decorators on
//                  functions, Pydantic BaseModel DTOs, typed path/query/body
//                  parameters including `Annotated[X, Query(...)]` and
//                  Depends(...) for auth dependencies.
//   - Starlette  — app.add_route / Route(path, handler, methods=[...]) and
//                  @app.route / @router.route(... methods=...). Mount() is
//                  followed for composed routers.
//   - Flask      — @app.route / @blueprint.route decorators with an optional
//                  `methods=[...]` kwarg.
//   - Ariadne /  — GraphQL-only services are recognised and get a documented
//                  GraphQL   `/graphql` POST endpoint plus any REST routes
//                  (health, metrics, explorer) that the app exposes.
//
// The extractor also reads pyproject.toml for service metadata (name,
// version, description) so even GraphQL / worker services produce a valid,
// meaningful OpenAPI document.
package pythonextract

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/index"
	"github.com/sailpoint-oss/cartographer/extract/parser"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Config holds Python extraction configuration.
type Config struct {
	RootDir    string
	OutputPath string
	SourceDirs []string
	Verbose    bool
}

// Result holds the extraction result.
type Result struct {
	Operations []*Operation
	Schemas    map[string]interface{}
	Types      map[string]*index.TypeDecl
	Metadata   ProjectMetadata
	// Framework is one of "fastapi", "starlette", "flask", "ariadne", "".
	Framework string
}

// ProjectMetadata captures Python package metadata from pyproject.toml or
// setup.py. These are used to populate the generated OpenAPI info block when
// CLI flags do not override them.
type ProjectMetadata struct {
	Name        string
	Version     string
	Description string
}

// Operation represents an extracted Python API endpoint.
type Operation struct {
	Path                string
	Method              string
	OperationID         string
	Summary             string
	Description         string
	Tags                []string
	Parameters          []*Parameter
	RequestBodyType     string
	ResponseType        string
	ResponseStatus      int
	Deprecated          bool
	RequiresAuth        bool
	Security            []string
	ConsumesContentType string
	ProducesContentType string
	File                string
	Line                int
	Column              int
}

// Parameter represents an API parameter.
type Parameter struct {
	Name         string
	In           string // path, query, header, cookie
	Type         string
	Required     bool
	DefaultValue string
	Description  string
	Format       string
	File         string
	Line         int
	Column       int
}

// Extract performs tree-sitter based Python extraction.
func Extract(cfg Config) (*Result, error) {
	pool := parser.NewPool()
	if err := pool.RegisterPython(); err != nil {
		return nil, fmt.Errorf("register python grammar: %w", err)
	}

	idx := index.New()
	scanner := index.NewScanner(pool, idx, "python")

	dirs := cfg.SourceDirs
	if len(dirs) == 0 {
		dirs = []string{cfg.RootDir}
	}
	for _, dir := range dirs {
		if err := scanner.ScanDir(dir); err != nil {
			return nil, fmt.Errorf("scan %s: %w", dir, err)
		}
	}

	result := &Result{
		Schemas: make(map[string]interface{}),
		Types:   idx.All(),
	}

	for _, dir := range dirs {
		ops, framework, err := extractOperations(pool, idx, dir, cfg.Verbose)
		if err != nil {
			return nil, err
		}
		result.Operations = append(result.Operations, ops...)
		if framework != "" && result.Framework == "" {
			result.Framework = framework
		}
	}

	for _, decl := range idx.All() {
		result.Schemas[decl.Name] = idx.ToOpenAPISchema(decl, nil)
	}

	result.Metadata = readProjectMetadata(cfg.RootDir)

	// Fabricate a /graphql endpoint for GraphQL-only services so the generated
	// spec still communicates the service's public surface area.
	if result.Framework == "ariadne" && len(result.Operations) == 0 {
		result.Operations = append(result.Operations, graphqlStubOperation())
	}

	return result, nil
}

func extractOperations(pool *parser.Pool, idx *index.Index, rootDir string, verbose bool) ([]*Operation, string, error) {
	var ops []*Operation
	var framework string

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			switch base {
			case ".git", "__pycache__", ".venv", "venv", ".mypy_cache",
				".pytest_cache", ".ruff_cache", ".tox", ".eggs",
				"tests", "test", "dist", "build", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".py") {
			return nil
		}
		base := filepath.Base(path)
		if strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		tree, err := pool.Parse("python", source)
		if err != nil {
			return nil
		}
		defer tree.Close()

		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "WARN: panic extracting operations from %s: %v\n", path, r)
				}
			}()
			fileOps, frameworkDetected := extractFileOperations(tree.RootNode(), source, path, idx)
			ops = append(ops, fileOps...)
			if frameworkDetected != "" {
				// Prefer FastAPI > Starlette > Flask > Ariadne when multiple
				// frameworks co-exist (common during Flask→Starlette migrations).
				framework = mergeFramework(framework, frameworkDetected)
			}
		}()

		return nil
	})

	return ops, framework, err
}

func mergeFramework(current, detected string) string {
	rank := map[string]int{"fastapi": 4, "starlette": 3, "flask": 2, "ariadne": 1}
	if rank[detected] > rank[current] {
		return detected
	}
	return current
}

// extractFileOperations walks a single module looking for route decorators and
// Starlette Route(...) / add_route() calls.
func extractFileOperations(root *tree_sitter.Node, source []byte, filePath string, idx *index.Index) ([]*Operation, string) {
	var ops []*Operation
	framework := ""

	// Detect framework by top-level imports
	framework = detectFramework(root, source)

	// First pass: collect any FastAPI APIRouter(prefix=...) or Flask
	// Blueprint(..., url_prefix=...) declarations so routes defined against
	// those routers inherit the prefix. Also capture any additional prefix
	// applied via `app.include_router(router, prefix="/v1")`.
	prefixes := collectRouterPrefixes(root, source)

	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Kind() {
		case "decorated_definition":
			if op, f := extractDecoratedRoute(child, source, filePath, idx, prefixes); op != nil {
				ops = append(ops, op)
				if f != "" {
					framework = mergeFramework(framework, f)
				}
			}
		case "expression_statement":
			// Top-level statements in Python are wrapped in expression_statement.
			// Two patterns we care about:
			//   `app.add_route(...)`   -> call node directly under expr_stmt
			//   `routes = [Route(...), Route(...)]` -> assignment with a list RHS
			for j := uint(0); j < child.ChildCount(); j++ {
				inner := child.Child(j)
				switch inner.Kind() {
				case "call":
					if extracted := extractAddRouteCall(inner, source, filePath); extracted != nil {
						ops = append(ops, extracted)
						framework = mergeFramework(framework, "starlette")
					}
				case "assignment":
					if right := inner.ChildByFieldName("right"); right != nil {
						routeOps := extractRouteListCalls(right, source, filePath)
						if len(routeOps) > 0 {
							ops = append(ops, routeOps...)
							framework = mergeFramework(framework, "starlette")
						}
					}
				}
			}
		case "function_definition":
			// async def routes declared via app.add_route -- we only catch the
			// handler here; the routing call is handled above.
		}
	}

	return ops, framework
}

// collectRouterPrefixes walks a module for FastAPI APIRouter and Flask Blueprint
// declarations, returning a map of local variable name → path prefix. It also
// detects `app.include_router(router, prefix="/v1")` calls and combines the
// include prefix with the router's own prefix so the final effective prefix is
// stored under the router variable name.
//
// Example resolutions:
//
//	router = APIRouter(prefix="/v3/users")       -> "router" -> "/v3/users"
//	app.include_router(router, prefix="/api")    -> "router" -> "/api/v3/users"
//	bp = Blueprint("users", __name__, url_prefix="/v3/users")
//	                                              -> "bp"     -> "/v3/users"
func collectRouterPrefixes(root *tree_sitter.Node, source []byte) map[string]string {
	prefixes := make(map[string]string)

	// Pass 1: pick up direct router/blueprint construction prefixes.
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child.Kind() != "expression_statement" {
			continue
		}
		for j := uint(0); j < child.ChildCount(); j++ {
			inner := child.Child(j)
			if inner.Kind() != "assignment" {
				continue
			}
			name, prefix, ok := extractRouterAssignment(inner, source)
			if !ok {
				continue
			}
			prefixes[name] = normalisePath(prefix)
		}
	}

	// Pass 2: apply include_router prefixes. We combine them with any existing
	// router prefix: include prefix comes first, then the router's own prefix.
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child.Kind() != "expression_statement" {
			continue
		}
		for j := uint(0); j < child.ChildCount(); j++ {
			inner := child.Child(j)
			if inner.Kind() != "call" {
				continue
			}
			name, prefix, ok := extractIncludeRouterCall(inner, source)
			if !ok {
				continue
			}
			existing := prefixes[name]
			combined := joinURLPath(normalisePath(prefix), existing)
			if combined != "" {
				prefixes[name] = combined
			}
		}
	}

	return prefixes
}

// extractRouterAssignment matches `name = APIRouter(prefix="...")` and
// `name = Blueprint("x", __name__, url_prefix="...")`.
func extractRouterAssignment(assign *tree_sitter.Node, source []byte) (string, string, bool) {
	left := assign.ChildByFieldName("left")
	right := assign.ChildByFieldName("right")
	if left == nil || right == nil || right.Kind() != "call" {
		return "", "", false
	}
	lhs := strings.TrimSpace(left.Utf8Text(source))
	if !isSimpleIdentifier(lhs) {
		return "", "", false
	}

	fn := right.ChildByFieldName("function")
	args := right.ChildByFieldName("arguments")
	if fn == nil || args == nil {
		return "", "", false
	}
	fnName := strings.TrimSpace(fn.Utf8Text(source))
	// Accept bare identifier or dotted import: APIRouter / fastapi.APIRouter /
	// APIRouter as r -> we only see the final name via tree-sitter.
	if idx := strings.LastIndex(fnName, "."); idx >= 0 {
		fnName = fnName[idx+1:]
	}

	kwargKey := ""
	switch fnName {
	case "APIRouter":
		kwargKey = "prefix"
	case "Blueprint":
		kwargKey = "url_prefix"
	default:
		return "", "", false
	}

	prefix, ok := extractKeywordString(args, source, kwargKey)
	if !ok {
		return "", "", false
	}
	return lhs, prefix, true
}

// extractIncludeRouterCall matches `app.include_router(router, prefix="...")`
// and returns the router variable name and the include prefix.
func extractIncludeRouterCall(call *tree_sitter.Node, source []byte) (string, string, bool) {
	fn := call.ChildByFieldName("function")
	args := call.ChildByFieldName("arguments")
	if fn == nil || args == nil {
		return "", "", false
	}
	fnText := strings.TrimSpace(fn.Utf8Text(source))
	methodName := fnText
	if idx := strings.LastIndex(fnText, "."); idx >= 0 {
		methodName = fnText[idx+1:]
	}
	if methodName != "include_router" {
		return "", "", false
	}

	routerName := ""
	for i := uint(0); i < args.ChildCount(); i++ {
		arg := args.Child(i)
		if arg.Kind() == "identifier" {
			routerName = arg.Utf8Text(source)
			break
		}
	}
	if routerName == "" {
		return "", "", false
	}
	prefix, ok := extractKeywordString(args, source, "prefix")
	if !ok {
		// include_router without a prefix is valid, but it's only interesting
		// to us when a prefix is supplied — without one the router's own
		// prefix is already covered by collectRouterPrefixes pass 1.
		return "", "", false
	}
	return routerName, prefix, true
}

// extractKeywordString returns the string-literal value of the named keyword
// argument, stripped of quotes, if present.
func extractKeywordString(argList *tree_sitter.Node, source []byte, name string) (string, bool) {
	for i := uint(0); i < argList.ChildCount(); i++ {
		arg := argList.Child(i)
		if arg.Kind() != "keyword_argument" {
			continue
		}
		k := arg.ChildByFieldName("name")
		v := arg.ChildByFieldName("value")
		if k == nil || v == nil {
			continue
		}
		if k.Utf8Text(source) != name {
			continue
		}
		val := strings.TrimSpace(v.Utf8Text(source))
		if val == "" {
			continue
		}
		return trimQuotes(val), true
	}
	return "", false
}

func isSimpleIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// joinURLPath joins two URL path segments with a single slash. A trailing "/"
// on the second segment is treated as an empty suffix so the common FastAPI
// pattern `@router.get("/")` resolves to the router's prefix instead of
// "/prefix/".
func joinURLPath(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" || b == "/" {
		return a
	}
	return strings.TrimRight(a, "/") + "/" + strings.TrimLeft(b, "/")
}

// detectFramework looks at the module imports to identify the web framework.
func detectFramework(root *tree_sitter.Node, source []byte) string {
	framework := ""
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child.Kind() != "import_from_statement" && child.Kind() != "import_statement" {
			continue
		}
		text := child.Utf8Text(source)
		switch {
		case strings.Contains(text, "fastapi"):
			framework = mergeFramework(framework, "fastapi")
		case strings.Contains(text, "starlette") || strings.Contains(text, "uvicorn"):
			framework = mergeFramework(framework, "starlette")
		case strings.Contains(text, "from flask") || strings.Contains(text, "import flask"):
			framework = mergeFramework(framework, "flask")
		case strings.Contains(text, "from ariadne") || strings.Contains(text, "import ariadne"):
			framework = mergeFramework(framework, "ariadne")
		}
	}
	return framework
}

// extractDecoratedRoute handles FastAPI/Flask/Starlette decorator patterns.
//
// Supported decorator shapes (examples):
//
//	@app.get("/users/{id}", response_model=User, tags=["users"])
//	@router.post("/items", status_code=201)
//	@app.route("/legacy", methods=["GET", "POST"])       # Flask / Starlette
//	@router.api_route("/proxy", methods=["GET", "POST"]) # FastAPI
//	@blueprint.delete("/users/{id}")                     # FastAPI/Starlette
//
// prefixes maps router/blueprint variable names to a path prefix (e.g.
// "router" -> "/v3/users"). When the decorator's receiver matches a key, the
// prefix is prepended to the path parsed from the decorator.
func extractDecoratedRoute(node *tree_sitter.Node, source []byte, filePath string, idx *index.Index, prefixes map[string]string) (*Operation, string) {
	var routeDecorators []routeDecorator
	var funcDef *tree_sitter.Node

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "decorator":
			if rd, ok := parseRouteDecorator(child, source); ok {
				if prefix, known := prefixes[rd.Receiver]; known && prefix != "" {
					rd.Path = joinURLPath(prefix, rd.Path)
				}
				routeDecorators = append(routeDecorators, rd)
			}
		case "function_definition":
			funcDef = child
		}
	}

	if len(routeDecorators) == 0 || funcDef == nil {
		return nil, ""
	}

	// For simplicity, use the first route decorator; a future enhancement can
	// emit one operation per decorator.
	rd := routeDecorators[0]

	op := buildOperationFromFunc(funcDef, source, filePath, idx, rd)
	return op, rd.Framework
}

type routeDecorator struct {
	Path        string
	Methods     []string
	Summary     string
	Description string
	Tags        []string
	ResponseModel string
	StatusCode  int
	Deprecated  bool
	Framework   string
	// Receiver is the variable name before the dot in the decorator target
	// (e.g. "router" in @router.get("/items")). Empty for bare decorators.
	Receiver string
}

// parseRouteDecorator attempts to interpret the decorator as a route decl.
//
// Returns ok=false if the decorator does not look like a route.
func parseRouteDecorator(decorator *tree_sitter.Node, source []byte) (routeDecorator, bool) {
	rd := routeDecorator{}

	// Find the decorator body: either a `call` node (parameterised) or an
	// `attribute`/`identifier` (bare @app).
	var body *tree_sitter.Node
	for i := uint(0); i < decorator.ChildCount(); i++ {
		child := decorator.Child(i)
		switch child.Kind() {
		case "call", "attribute", "identifier":
			body = child
		}
	}
	if body == nil {
		return rd, false
	}
	if body.Kind() != "call" {
		return rd, false
	}

	// `app.get("/path", ...)` or `router.post("/path", ...)`
	fn := body.ChildByFieldName("function")
	args := body.ChildByFieldName("arguments")
	if fn == nil || args == nil {
		return rd, false
	}

	fnText := fn.Utf8Text(source)
	// strip receiver prefix, keep the method name; also remember the receiver
	// so the caller can look up any APIRouter/Blueprint prefix.
	methodName := fnText
	if idx := strings.LastIndex(fnText, "."); idx >= 0 {
		rd.Receiver = fnText[:idx]
		methodName = fnText[idx+1:]
	}

	// Recognised HTTP verbs
	switch strings.ToLower(methodName) {
	case "get":
		rd.Methods = []string{"GET"}
	case "post":
		rd.Methods = []string{"POST"}
	case "put":
		rd.Methods = []string{"PUT"}
	case "patch":
		rd.Methods = []string{"PATCH"}
	case "delete":
		rd.Methods = []string{"DELETE"}
	case "head":
		rd.Methods = []string{"HEAD"}
	case "options":
		rd.Methods = []string{"OPTIONS"}
	case "trace":
		rd.Methods = []string{"TRACE"}
	case "route":
		// Flask / Starlette @app.route(path, methods=[...])
		rd.Framework = "flask"
	case "api_route":
		// FastAPI app.api_route(path, methods=[...])
		rd.Framework = "fastapi"
	case "websocket":
		// websockets aren't part of the HTTP spec — skip
		return rd, false
	default:
		return rd, false
	}

	if rd.Framework == "" {
		// default to fastapi for verb decorators — FastAPI is the most common
		// modern Python web framework at SailPoint
		rd.Framework = "fastapi"
	}

	// First positional argument is the path
	var positional []string
	var kwargs = map[string]string{}
	for i := uint(0); i < args.ChildCount(); i++ {
		arg := args.Child(i)
		switch arg.Kind() {
		case "string":
			positional = append(positional, trimQuotes(arg.Utf8Text(source)))
		case "integer":
			positional = append(positional, arg.Utf8Text(source))
		case "keyword_argument":
			k := arg.ChildByFieldName("name")
			v := arg.ChildByFieldName("value")
			if k != nil && v != nil {
				kwargs[k.Utf8Text(source)] = v.Utf8Text(source)
			}
		}
	}
	if len(positional) > 0 {
		rd.Path = positional[0]
	}

	if methods, ok := kwargs["methods"]; ok {
		rd.Methods = parseStringList(methods)
	}
	if v, ok := kwargs["summary"]; ok {
		rd.Summary = trimQuotes(v)
	}
	if v, ok := kwargs["description"]; ok {
		rd.Description = trimQuotes(v)
	}
	if v, ok := kwargs["tags"]; ok {
		rd.Tags = parseStringList(v)
	}
	if v, ok := kwargs["response_model"]; ok {
		rd.ResponseModel = strings.TrimSpace(v)
	}
	if v, ok := kwargs["status_code"]; ok {
		// simple int parsing, ignoring expressions
		var n int
		_, err := fmt.Sscanf(v, "%d", &n)
		if err == nil {
			rd.StatusCode = n
		}
	}
	if v, ok := kwargs["deprecated"]; ok {
		rd.Deprecated = strings.EqualFold(strings.TrimSpace(v), "True")
	}

	return rd, rd.Path != ""
}

// extractRouteListCalls walks a Starlette-style routes list and returns one
// Operation per Route(...) / Mount(...) / WebSocketRoute(...) call found
// inside (Mount and WebSocketRoute are recognised but skipped).
func extractRouteListCalls(expr *tree_sitter.Node, source []byte, filePath string) []*Operation {
	var ops []*Operation
	switch expr.Kind() {
	case "list", "tuple", "set":
		for i := uint(0); i < expr.ChildCount(); i++ {
			child := expr.Child(i)
			if child.Kind() != "call" {
				continue
			}
			if op := extractAddRouteCall(child, source, filePath); op != nil {
				ops = append(ops, op)
			}
		}
	case "call":
		if op := extractAddRouteCall(expr, source, filePath); op != nil {
			ops = append(ops, op)
		}
	}
	return ops
}

// extractAddRouteCall parses Starlette `app.add_route(path, handler, methods=[])`
// and `Route(path, handler, methods=[])`.
func extractAddRouteCall(call *tree_sitter.Node, source []byte, filePath string) *Operation {
	fn := call.ChildByFieldName("function")
	args := call.ChildByFieldName("arguments")
	if fn == nil || args == nil {
		return nil
	}

	fnText := fn.Utf8Text(source)
	methodName := fnText
	if idx := strings.LastIndex(fnText, "."); idx >= 0 {
		methodName = fnText[idx+1:]
	}
	if methodName != "add_route" && methodName != "Route" {
		return nil
	}

	var positional []string
	kwargs := map[string]string{}
	for i := uint(0); i < args.ChildCount(); i++ {
		arg := args.Child(i)
		switch arg.Kind() {
		case "string":
			positional = append(positional, trimQuotes(arg.Utf8Text(source)))
		case "identifier":
			positional = append(positional, arg.Utf8Text(source))
		case "keyword_argument":
			k := arg.ChildByFieldName("name")
			v := arg.ChildByFieldName("value")
			if k != nil && v != nil {
				kwargs[k.Utf8Text(source)] = v.Utf8Text(source)
			}
		}
	}

	if len(positional) < 1 {
		return nil
	}
	path := positional[0]
	methods := []string{"GET"}
	if v, ok := kwargs["methods"]; ok {
		if parsed := parseStringList(v); len(parsed) > 0 {
			methods = parsed
		}
	}
	handler := ""
	if len(positional) >= 2 {
		handler = positional[1]
	}

	pos := call.StartPosition()
	return &Operation{
		Path:        path,
		Method:      methods[0],
		OperationID: handler,
		Summary:     humaniseOperationID(handler),
		File:        filePath,
		Line:        int(pos.Row) + 1,
		Column:      int(pos.Column) + 1,
	}
}

// buildOperationFromFunc converts a decorated function definition into an
// Operation using the route decorator metadata.
func buildOperationFromFunc(funcDef *tree_sitter.Node, source []byte, filePath string, idx *index.Index, rd routeDecorator) *Operation {
	name := ""
	var params *tree_sitter.Node
	var returnType *tree_sitter.Node
	var body *tree_sitter.Node

	for i := uint(0); i < funcDef.ChildCount(); i++ {
		child := funcDef.Child(i)
		switch child.Kind() {
		case "identifier":
			if name == "" {
				name = child.Utf8Text(source)
			}
		case "parameters":
			params = child
		case "type":
			returnType = child
		case "block":
			body = child
		}
	}

	op := &Operation{
		Path:            normalisePath(rd.Path),
		Method:          firstMethod(rd.Methods),
		OperationID:     name,
		Summary:         rd.Summary,
		Description:     rd.Description,
		Tags:            rd.Tags,
		ResponseType:    strings.TrimSpace(rd.ResponseModel),
		ResponseStatus:  rd.StatusCode,
		Deprecated:      rd.Deprecated,
		File:            filePath,
	}

	if op.Summary == "" {
		op.Summary = humaniseOperationID(name)
	}
	if op.Description == "" {
		op.Description = extractPythonDocstring(body, source)
	}
	if returnType != nil && op.ResponseType == "" {
		op.ResponseType = strings.TrimSpace(strings.TrimPrefix(returnType.Utf8Text(source), "->"))
	}

	pos := funcDef.StartPosition()
	op.Line = int(pos.Row) + 1
	op.Column = int(pos.Column) + 1

	if params != nil {
		op.Parameters, op.RequestBodyType, op.RequiresAuth = parseHandlerParameters(params, source, rd.Path, idx)
	}

	return op
}

// parseHandlerParameters converts the function signature into OpenAPI
// parameters. Path parameters are inferred from rd.Path ({name} syntax);
// body params are BaseModel types; Depends(...) parameters signal auth.
func parseHandlerParameters(params *tree_sitter.Node, source []byte, routePath string, idx *index.Index) ([]*Parameter, string, bool) {
	var out []*Parameter
	requestBody := ""
	requiresAuth := false

	pathParams := extractPathParamNames(routePath)

	for i := uint(0); i < params.ChildCount(); i++ {
		p := params.Child(i)
		switch p.Kind() {
		case "identifier":
			// self / cls — skip
			continue
		case "typed_parameter", "typed_default_parameter", "default_parameter":
			pm := parseTypedParameter(p, source, pathParams, idx)
			if pm == nil {
				continue
			}
			switch {
			case pm.In == "body":
				if requestBody == "" {
					requestBody = pm.Type
				}
			case pm.In == "auth":
				requiresAuth = true
			default:
				out = append(out, pm)
			}
		}
	}

	return out, requestBody, requiresAuth
}

func parseTypedParameter(p *tree_sitter.Node, source []byte, pathParams map[string]bool, idx *index.Index) *Parameter {
	name := ""
	typeStr := ""
	defaultValue := ""

	for i := uint(0); i < p.ChildCount(); i++ {
		child := p.Child(i)
		switch child.Kind() {
		case "identifier":
			if name == "" {
				name = child.Utf8Text(source)
			}
		case "type":
			typeStr = strings.TrimSpace(child.Utf8Text(source))
		}
	}
	if right := p.ChildByFieldName("value"); right != nil {
		defaultValue = strings.TrimSpace(right.Utf8Text(source))
	}

	if name == "" {
		return nil
	}

	// FastAPI's Depends(get_current_user) signals auth requirement -- this
	// check runs before the underscore-filter because handlers commonly name
	// their auth dependency `_user=Depends(get_current_user)` (the leading
	// underscore conventionally marks it unused in the body).
	if strings.HasPrefix(defaultValue, "Depends(") {
		return &Parameter{In: "auth", Name: name}
	}

	if strings.HasPrefix(name, "_") {
		return nil
	}

	// Path parameters are those matching a {placeholder} in the route
	if pathParams[name] {
		return &Parameter{
			Name:     name,
			In:       "path",
			Type:     typeStr,
			Required: true,
		}
	}

	// Body parameters: Pydantic BaseModel-derived types
	if typeStr != "" && isBodyType(typeStr, idx) {
		return &Parameter{
			Name: name,
			In:   "body",
			Type: typeStr,
		}
	}

	// Annotated[str, Query(...)] / Annotated[str, Header(...)] / Annotated[str, Cookie(...)]
	if strings.HasPrefix(typeStr, "Annotated[") {
		inner := strings.TrimSuffix(strings.TrimPrefix(typeStr, "Annotated["), "]")
		parts := strings.SplitN(inner, ",", 2)
		core := strings.TrimSpace(parts[0])
		meta := ""
		if len(parts) > 1 {
			meta = strings.TrimSpace(parts[1])
		}
		switch {
		case strings.Contains(meta, "Query("):
			return &Parameter{Name: name, In: "query", Type: core, Required: defaultValue == "" || strings.Contains(meta, "...") || strings.Contains(defaultValue, "...")}
		case strings.Contains(meta, "Header("):
			return &Parameter{Name: name, In: "header", Type: core}
		case strings.Contains(meta, "Cookie("):
			return &Parameter{Name: name, In: "cookie", Type: core}
		case strings.Contains(meta, "Path("):
			return &Parameter{Name: name, In: "path", Type: core, Required: true}
		case strings.Contains(meta, "Body("):
			return &Parameter{Name: name, In: "body", Type: core}
		}
	}

	// Query(...) / Path(...) / Header(...) FastAPI shortcut (default position)
	switch {
	case strings.HasPrefix(defaultValue, "Query("):
		return &Parameter{Name: name, In: "query", Type: typeStr, Required: strings.Contains(defaultValue, "...")}
	case strings.HasPrefix(defaultValue, "Path("):
		return &Parameter{Name: name, In: "path", Type: typeStr, Required: true}
	case strings.HasPrefix(defaultValue, "Header("):
		return &Parameter{Name: name, In: "header", Type: typeStr}
	case strings.HasPrefix(defaultValue, "Cookie("):
		return &Parameter{Name: name, In: "cookie", Type: typeStr}
	case strings.HasPrefix(defaultValue, "Body("):
		return &Parameter{Name: name, In: "body", Type: typeStr}
	case strings.HasPrefix(defaultValue, "File("):
		return &Parameter{Name: name, In: "formData", Type: "bytes"}
	}

	// FastAPI convention: bare scalar types without a Query(...) default are
	// interpreted as query parameters (required when no default, optional when
	// there is one).
	if typeStr != "" && !isBodyType(typeStr, idx) {
		p := &Parameter{
			Name: name,
			In:   "query",
			Type: typeStr,
		}
		p.Required = defaultValue == ""
		return p
	}

	return nil
}

// isBodyType returns true if typeStr represents a Pydantic BaseModel (or other
// user-defined DTO tracked in the index).
func isBodyType(typeStr string, idx *index.Index) bool {
	typeStr = strings.TrimSpace(typeStr)
	typeStr = strings.TrimSuffix(typeStr, " | None")
	typeStr = strings.TrimPrefix(typeStr, "Optional[")
	typeStr = strings.TrimSuffix(typeStr, "]")
	typeStr = strings.TrimSpace(typeStr)
	// Strip list[...] wrapping for list-of-models bodies
	if strings.HasPrefix(typeStr, "list[") && strings.HasSuffix(typeStr, "]") {
		typeStr = strings.TrimSuffix(strings.TrimPrefix(typeStr, "list["), "]")
	}
	if strings.HasPrefix(typeStr, "List[") && strings.HasSuffix(typeStr, "]") {
		typeStr = strings.TrimSuffix(strings.TrimPrefix(typeStr, "List["), "]")
	}
	if decl, ok := idx.Resolve(typeStr); ok {
		return decl.Kind == "class" || decl.Kind == "interface"
	}
	if decl, ok := idx.ResolveSimple(typeStr); ok {
		return decl.Kind == "class" || decl.Kind == "interface"
	}
	return false
}

// extractPathParamNames returns a set of names appearing as {placeholders}.
func extractPathParamNames(path string) map[string]bool {
	names := make(map[string]bool)
	depth := 0
	start := -1
	for i, c := range path {
		switch c {
		case '{':
			depth++
			if depth == 1 {
				start = i + 1
			}
		case '}':
			if depth == 1 && start >= 0 {
				name := path[start:i]
				// Starlette uses `{name:int}` converters — strip
				if colon := strings.Index(name, ":"); colon >= 0 {
					name = name[:colon]
				}
				names[name] = true
				start = -1
			}
			depth--
		}
	}
	return names
}

// graphqlStubOperation returns a canonical POST /graphql operation for
// GraphQL-only services so their generated spec still documents the public
// endpoint.
func graphqlStubOperation() *Operation {
	return &Operation{
		Path:                "/graphql",
		Method:              "POST",
		OperationID:         "graphql",
		Summary:             "GraphQL endpoint",
		Description:         "POST a GraphQL query or mutation to this endpoint. This service's schema is defined in GraphQL SDL and is federated via Apollo Federation; see the service README for schema details.",
		Tags:                []string{"graphql"},
		ConsumesContentType: "application/json",
		ProducesContentType: "application/json",
	}
}

// extractPythonDocstring returns the leading string literal inside a
// function/class block.
func extractPythonDocstring(body *tree_sitter.Node, source []byte) string {
	if body == nil {
		return ""
	}
	for i := uint(0); i < body.ChildCount(); i++ {
		child := body.Child(i)
		if child.Kind() != "expression_statement" {
			continue
		}
		for j := uint(0); j < child.ChildCount(); j++ {
			inner := child.Child(j)
			if inner.Kind() == "string" {
				s := strings.TrimSpace(inner.Utf8Text(source))
				for _, q := range []string{`"""`, `'''`, `"`, `'`} {
					if strings.HasPrefix(s, q) && strings.HasSuffix(s, q) {
						s = strings.TrimPrefix(s, q)
						s = strings.TrimSuffix(s, q)
						break
					}
				}
				return strings.TrimSpace(s)
			}
		}
		return ""
	}
	return ""
}

func parseStringList(literal string) []string {
	literal = strings.TrimSpace(literal)
	literal = strings.TrimPrefix(literal, "[")
	literal = strings.TrimSuffix(literal, "]")
	if literal == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(literal, ",") {
		p = strings.TrimSpace(p)
		p = trimQuotes(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func trimQuotes(s string) string {
	s = strings.TrimSpace(s)
	for _, q := range []string{`"""`, `'''`, `"`, `'`} {
		if strings.HasPrefix(s, q) && strings.HasSuffix(s, q) && len(s) >= 2*len(q) {
			s = strings.TrimPrefix(s, q)
			s = strings.TrimSuffix(s, q)
			break
		}
	}
	return s
}

func normalisePath(p string) string {
	if p == "" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func firstMethod(methods []string) string {
	if len(methods) == 0 {
		return "GET"
	}
	return strings.ToUpper(strings.TrimSpace(methods[0]))
}

// humaniseOperationID converts "list_users" or "listUsers" into "List users"
// for a nicer default summary.
func humaniseOperationID(id string) string {
	if id == "" {
		return ""
	}
	// snake_case → words
	words := strings.Split(id, "_")
	if len(words) == 1 {
		// camelCase → words
		var split []string
		buf := []rune{}
		for i, r := range id {
			if i > 0 && r >= 'A' && r <= 'Z' {
				split = append(split, string(buf))
				buf = []rune{r}
				continue
			}
			buf = append(buf, r)
		}
		if len(buf) > 0 {
			split = append(split, string(buf))
		}
		words = split
	}
	for i, w := range words {
		if w == "" {
			continue
		}
		if i == 0 {
			words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
		} else {
			words[i] = strings.ToLower(w)
		}
	}
	return strings.Join(words, " ")
}

// readProjectMetadata reads pyproject.toml / setup.py to pick up name, version,
// description.
//
// This is intentionally a very small hand-rolled parser because pulling in a
// full TOML library would be overkill for three fields, and we already depend
// on BurntSushi/toml transitively — but we don't want to couple the extractor
// to a specific TOML parser version.
func readProjectMetadata(root string) ProjectMetadata {
	if root == "" {
		return ProjectMetadata{}
	}
	pyproject := filepath.Join(root, "pyproject.toml")
	data, err := os.ReadFile(pyproject)
	if err != nil {
		return ProjectMetadata{}
	}
	return parsePyprojectMetadata(string(data))
}

// stripTrailingTomlComment returns v with any trailing `# comment` removed,
// respecting single- and double-quoted string values. It is intentionally
// narrow-purpose -- it only handles the subset of TOML we need for the
// [project] table (scalar string values with optional trailing comments) --
// but it correctly handles cases like `"quoted # hash"  # real comment`.
func stripTrailingTomlComment(v string) string {
	inSingle := false
	inDouble := false
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case c == '#' && !inSingle && !inDouble:
			return strings.TrimSpace(v[:i])
		}
	}
	return v
}

// parsePyprojectMetadata extracts `[project]` metadata from pyproject.toml.
func parsePyprojectMetadata(s string) ProjectMetadata {
	meta := ProjectMetadata{}
	inProject := false
	for _, raw := range strings.Split(s, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inProject = strings.HasPrefix(line, "[project]")
			continue
		}
		if !inProject {
			continue
		}
		if eq := strings.Index(line, "="); eq > 0 {
			k := strings.TrimSpace(line[:eq])
			v := strings.TrimSpace(line[eq+1:])
			v = stripTrailingTomlComment(v)
			v = trimQuotes(v)
			switch k {
			case "name":
				meta.Name = v
			case "version":
				meta.Version = v
			case "description":
				meta.Description = v
			}
		}
	}
	return meta
}
