// Package javaextract provides tree-sitter based Java OpenAPI extraction.
// Supports both Spring Boot (@RestController, @GetMapping, etc.) and
// JAX-RS (@Path, @GET, @PathParam, etc.) annotation patterns.
// No JDK or Gradle required at runtime.
package javaextract

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/index"
	"github.com/sailpoint-oss/cartographer/extract/parser"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Config holds Java extraction configuration.
type Config struct {
	RootDir    string
	OutputPath string
	SourceDirs []string
	Classpath  string
	Verbose    bool
}

// Result holds the extraction result.
type Result struct {
	Operations []*Operation
	Schemas    map[string]interface{}
	Types      map[string]*index.TypeDecl
}

// AnnotatedResponse represents a response declared via @ApiResponse annotation.
type AnnotatedResponse struct {
	StatusCode  int
	Description string
	SchemaType  string
}

// Operation represents an extracted API endpoint.
type Operation struct {
	Path                   string
	Method                 string
	OperationID            string
	Summary                string
	Description            string
	Tags                   []string
	Parameters             []*Parameter
	RequestBodyType        string
	RequestBodyDescription string // from @RequestBody description or @param JavaDoc
	ResponseType           string
	ResponseStatus         int
	Deprecated             bool
	DeprecatedSince        string
	Hidden                 bool
	Security               []string // rights/scopes
	ConsumesContentType    string   // from @Consumes or consumes= attribute
	ProducesContentType    string   // from @Produces or produces= attribute
	ReturnDescription      string
	ErrorResponses         map[int]string      // status code -> description from @throws
	AnnotatedResponses     []AnnotatedResponse // from @ApiResponse annotations
	ResponseHeaders        map[string]string   // header name -> description (e.g. X-Total-Count)
	NullableResponse       bool                // from @Nullable or Optional<T> return
	RateLimited            bool                // from @Metered, @Timed, @RateLimited
	FormParams             []*Parameter        // from @FormParam (JAX-RS form parameters)
	File                   string              // source file path
	Line                   int                 // 1-based line number
	Column                 int                 // 1-based column number
}

// Parameter represents an API parameter.
type Parameter struct {
	Name         string
	In           string // path, query, header, cookie, form
	Type         string
	Required     bool
	DefaultValue string
	Description  string
	Format       string
	Pattern      string
	Minimum      *int
	Maximum      *int
	MinLength    *int
	MaxLength    *int
	MinItems     *int
	MaxItems     *int
	Example      string
	Enum         []string
	File         string // source file path
	Line         int    // 1-based line number
	Column       int    // 1-based column number
}

// extractContext holds shared state for the extraction pass.
type extractContext struct {
	constants         map[string]string // Java constant name -> resolved string value
	classPaths        map[string]string // class simple name -> @RequestMapping/@Path base path
	idx               *index.Index
	verbose           bool
	exceptionHandlers map[string]int // exception class name -> HTTP status code (from @ControllerAdvice / ExceptionMapper)
}

// Extract performs tree-sitter based Java extraction.
func Extract(cfg Config) (*Result, error) {
	pool := parser.NewPool()
	if err := pool.RegisterJava(); err != nil {
		return nil, fmt.Errorf("register java grammar: %w", err)
	}

	idx := index.New()
	scanner := index.NewScanner(pool, idx, "java")

	// Scan source directories to build type index
	dirs := cfg.SourceDirs
	if len(dirs) == 0 {
		dirs = []string{cfg.RootDir}
	}
	for _, dir := range dirs {
		if err := scanner.ScanDir(dir); err != nil {
			return nil, fmt.Errorf("scan %s: %w", dir, err)
		}
	}

	// Build extraction context: constant table + class path map
	ctx := &extractContext{
		constants:         make(map[string]string),
		classPaths:        make(map[string]string),
		idx:               idx,
		verbose:           cfg.Verbose,
		exceptionHandlers: make(map[string]int),
	}
	for _, dir := range dirs {
		buildConstantTable(pool, dir, ctx)
	}
	resolveConstants(ctx.constants)

	// Now extract operations from controller/resource classes
	result := &Result{
		Schemas: make(map[string]interface{}),
		Types:   idx.All(),
	}

	for _, dir := range dirs {
		ops, err := extractOperations(pool, ctx, dir)
		if err != nil {
			return nil, err
		}
		result.Operations = append(result.Operations, ops...)
	}

	// Convert indexed types to schemas
	for name, decl := range idx.All() {
		result.Schemas[decl.Name] = idx.ToOpenAPISchema(decl, nil)
		_ = name
	}

	return result, nil
}

// =============================================================================
// Constant table building
// =============================================================================

// buildConstantTable walks Java files to collect static final String constants
// and class-level @RequestMapping/@Path base paths.
func buildConstantTable(pool *parser.Pool, rootDir string, ctx *extractContext) {
	filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if base == "node_modules" || base == ".git" || base == "build" || base == "target" || base == "test" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".java") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		tree, err := pool.Parse("java", source)
		if err != nil {
			return nil
		}
		defer tree.Close()

		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "WARN: panic in constant scan of %s: %v\n", path, r)
				}
			}()
			collectFileConstants(tree.RootNode(), source, ctx)
		}()
		return nil
	})
}

// collectFileConstants extracts constants and class base paths from a Java file AST.
func collectFileConstants(root *tree_sitter.Node, source []byte, ctx *extractContext) {
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child.Kind() == "class_declaration" {
			collectClassConstants(child, source, ctx)
		}
	}
}

// collectClassConstants extracts constants and base path from a class declaration.
func collectClassConstants(classNode *tree_sitter.Node, source []byte, ctx *extractContext) {
	className := ""
	var classBody *tree_sitter.Node

	for i := uint(0); i < classNode.ChildCount(); i++ {
		child := classNode.Child(i)
		switch child.Kind() {
		case "identifier":
			if className == "" {
				className = child.Utf8Text(source)
			}
		case "modifiers":
			// Collect class-level @RequestMapping/@Path for inheritance
			for j := uint(0); j < child.ChildCount(); j++ {
				ann := child.Child(j)
				annName, annArgs := extractAnnotation(ann, source)
				if annName == "RequestMapping" || annName == "Path" {
					path := extractMappingPathFromString(annArgs, nil)
					if path != "" && className != "" {
						ctx.classPaths[className] = path
					}
				}
			}
		case "class_body":
			classBody = child
		}
	}

	if classBody == nil {
		return
	}

	// Collect static final String declarations
	for i := uint(0); i < classBody.ChildCount(); i++ {
		child := classBody.Child(i)
		if child.Kind() != "field_declaration" {
			continue
		}

		isStatic := false
		isFinal := false
		fieldType := ""
		fieldName := ""
		initExpr := ""

		for j := uint(0); j < child.ChildCount(); j++ {
			n := child.Child(j)
			switch n.Kind() {
			case "modifiers":
				modText := n.Utf8Text(source)
				isStatic = strings.Contains(modText, "static")
				isFinal = strings.Contains(modText, "final")
			case "type_identifier":
				fieldType = n.Utf8Text(source)
			case "variable_declarator":
				for k := uint(0); k < n.ChildCount(); k++ {
					vn := n.Child(k)
					if vn.Kind() == "identifier" && fieldName == "" {
						fieldName = vn.Utf8Text(source)
					}
					// Capture the initializer expression text
					if vn.Kind() == "string_literal" || vn.Kind() == "binary_expression" || vn.Kind() == "identifier" {
						if fieldName != "" {
							initExpr = vn.Utf8Text(source)
						}
					}
				}
			}
		}

		if isStatic && isFinal && fieldType == "String" && fieldName != "" && initExpr != "" {
			ctx.constants[fieldName] = initExpr
		}
	}
}

// resolveConstants iterates the constant table to resolve forward references
// and concatenation expressions. Runs multiple passes until stable.
func resolveConstants(constants map[string]string) {
	for pass := 0; pass < 10; pass++ {
		changed := false
		for name, expr := range constants {
			resolved := resolveConstExpr(expr, constants)
			if resolved != expr {
				constants[name] = resolved
				changed = true
			}
		}
		if !changed {
			break
		}
	}
}

// resolveConstExpr resolves a constant expression to its string value.
// Handles: string literals ("foo"), constant references (FOO), and
// concatenation (FOO + "/bar" or FOO + BAR).
func resolveConstExpr(expr string, constants map[string]string) string {
	expr = strings.TrimSpace(expr)

	// Already a resolved string literal
	if len(expr) >= 2 && strings.HasPrefix(expr, "\"") && strings.HasSuffix(expr, "\"") && !strings.Contains(expr, "\" + ") {
		return expr[1 : len(expr)-1]
	}

	// Check if it's a simple constant reference
	if val, ok := constants[expr]; ok {
		// If the value is already resolved (no quotes, no +), return it
		if !strings.Contains(val, "\"") && !strings.Contains(val, "+") {
			return val
		}
		// Otherwise try to resolve the value
		return resolveConstExpr(val, constants)
	}

	// Handle concatenation: split on " + " and resolve each part
	if strings.Contains(expr, "+") {
		parts := strings.Split(expr, "+")
		var resolved []string
		allResolved := true
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			r := resolveConstExpr(part, constants)
			// If it still contains quotes or looks unresolved, strip quotes
			r = strings.Trim(r, "\"")
			resolved = append(resolved, r)
			// Check if part is still an identifier (unresolved)
			if !strings.HasPrefix(part, "\"") && r == part {
				if _, ok := constants[part]; !ok {
					allResolved = false
				}
			}
		}
		if allResolved || len(resolved) > 1 {
			return strings.Join(resolved, "")
		}
	}

	return expr
}

// resolveAnnotationValue resolves an annotation argument value using the constant table.
// For string literals, strips quotes. For constant names, looks up the value.
// For concatenation expressions, resolves all parts.
func resolveAnnotationValue(raw string, constants map[string]string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	// Already a string literal
	if len(raw) >= 2 && strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"") && !strings.Contains(raw, "\" + ") {
		return raw[1 : len(raw)-1]
	}

	// Try constant lookup
	if val, ok := constants[raw]; ok {
		return val
	}
	// Support qualified constant references like ApiPaths.USERS where
	// constants map is keyed by short field name.
	if dotIdx := strings.LastIndex(raw, "."); dotIdx > 0 && dotIdx+1 < len(raw) {
		if val, ok := constants[raw[dotIdx+1:]]; ok {
			return val
		}
	}

	// Handle concatenation
	if strings.Contains(raw, "+") {
		return resolveConstExpr(raw, constants)
	}

	return raw
}

// =============================================================================
// Operation extraction
// =============================================================================

// extractOperations walks Java source files looking for controller/resource classes.
func extractOperations(pool *parser.Pool, ctx *extractContext, rootDir string) ([]*Operation, error) {
	var ops []*Operation

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if base == "node_modules" || base == ".git" || base == "build" || base == "target" || base == "test" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".java") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		tree, err := pool.Parse("java", source)
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
			// Detect ControllerAdvice/ExceptionMapper classes for global exception handling
			extractExceptionHandlerClasses(tree.RootNode(), source, ctx)

			fileOps := extractFileOperations(tree.RootNode(), source, path, ctx)
			ops = append(ops, fileOps...)
		}()

		return nil
	})

	return ops, err
}

// extractFileOperations extracts operations from a single Java source file.
func extractFileOperations(root *tree_sitter.Node, source []byte, filePath string, ctx *extractContext) []*Operation {
	var ops []*Operation

	// Find class declarations
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child.Kind() != "class_declaration" {
			continue
		}

		classOps := extractClassOperations(child, source, filePath, ctx)
		ops = append(ops, classOps...)
	}

	return ops
}

// extractExceptionHandlerClasses scans for @ControllerAdvice and ExceptionMapper classes
// to populate ctx.exceptionHandlers with exception-to-status-code mappings.
func extractExceptionHandlerClasses(root *tree_sitter.Node, source []byte, ctx *extractContext) {
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child.Kind() != "class_declaration" {
			continue
		}

		isControllerAdvice := false
		isExceptionMapper := false
		var mapperExceptionType string

		// Check class annotations and interfaces
		for j := uint(0); j < child.ChildCount(); j++ {
			node := child.Child(j)
			switch node.Kind() {
			case "modifiers":
				for k := uint(0); k < node.ChildCount(); k++ {
					ann := node.Child(k)
					annName, _ := extractAnnotation(ann, source)
					if annName == "ControllerAdvice" || annName == "RestControllerAdvice" {
						isControllerAdvice = true
					}
					if annName == "Provider" {
						isExceptionMapper = true
					}
				}
			case "super_interfaces":
				// Look for implements ExceptionMapper<SomeException>
				text := node.Utf8Text(source)
				if strings.Contains(text, "ExceptionMapper") {
					isExceptionMapper = true
					if start := strings.Index(text, "ExceptionMapper<"); start >= 0 {
						rest := text[start+len("ExceptionMapper<"):]
						if end := strings.Index(rest, ">"); end >= 0 {
							mapperExceptionType = strings.TrimSpace(rest[:end])
						}
					}
				}
			}
		}

		if isControllerAdvice {
			extractControllerAdviceHandlers(child, source, ctx)
		}
		if isExceptionMapper && mapperExceptionType != "" {
			extractExceptionMapperStatus(child, source, mapperExceptionType, ctx)
		}
	}
}

// extractControllerAdviceHandlers extracts @ExceptionHandler methods from a @ControllerAdvice class.
func extractControllerAdviceHandlers(classNode *tree_sitter.Node, source []byte, ctx *extractContext) {
	var classBody *tree_sitter.Node
	for i := uint(0); i < classNode.ChildCount(); i++ {
		if classNode.Child(i).Kind() == "class_body" {
			classBody = classNode.Child(i)
			break
		}
	}
	if classBody == nil {
		return
	}

	for i := uint(0); i < classBody.ChildCount(); i++ {
		method := classBody.Child(i)
		if method.Kind() != "method_declaration" {
			continue
		}

		var exceptionTypes []string
		statusCode := 0

		for j := uint(0); j < method.ChildCount(); j++ {
			node := method.Child(j)
			if node.Kind() != "modifiers" {
				continue
			}
			for k := uint(0); k < node.ChildCount(); k++ {
				ann := node.Child(k)
				annName, annArgs := extractAnnotation(ann, source)
				if annName == "ExceptionHandler" {
					exceptionTypes = parseExceptionHandlerArgs(annArgs)
				}
				if annName == "ResponseStatus" {
					statusCode = parseResponseStatusValue(annArgs)
				}
			}
		}

		if statusCode == 0 {
			// Try to infer from method body
			bodyText := method.Utf8Text(source)
			if strings.Contains(bodyText, "HttpStatus.NOT_FOUND") || strings.Contains(bodyText, "404") {
				statusCode = 404
			} else if strings.Contains(bodyText, "HttpStatus.BAD_REQUEST") || strings.Contains(bodyText, "400") {
				statusCode = 400
			} else if strings.Contains(bodyText, "HttpStatus.CONFLICT") || strings.Contains(bodyText, "409") {
				statusCode = 409
			}
		}

		if statusCode > 0 {
			for _, exType := range exceptionTypes {
				ctx.exceptionHandlers[exType] = statusCode
			}
		}
	}
}

// parseExceptionHandlerArgs parses @ExceptionHandler(FooException.class) or
// @ExceptionHandler({FooException.class, BarException.class}).
func parseExceptionHandlerArgs(args string) []string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)
	args = strings.TrimPrefix(args, "{")
	args = strings.TrimSuffix(args, "}")

	var types []string
	for _, part := range strings.Split(args, ",") {
		part = strings.TrimSpace(part)
		part = strings.TrimSuffix(part, ".class")
		if part != "" {
			types = append(types, part)
		}
	}
	return types
}

// extractExceptionMapperStatus extracts the status code from an ExceptionMapper<T>.toResponse() method.
func extractExceptionMapperStatus(classNode *tree_sitter.Node, source []byte, exceptionType string, ctx *extractContext) {
	var classBody *tree_sitter.Node
	for i := uint(0); i < classNode.ChildCount(); i++ {
		if classNode.Child(i).Kind() == "class_body" {
			classBody = classNode.Child(i)
			break
		}
	}
	if classBody == nil {
		return
	}

	for i := uint(0); i < classBody.ChildCount(); i++ {
		method := classBody.Child(i)
		if method.Kind() != "method_declaration" {
			continue
		}

		// Check if this is the toResponse method
		for j := uint(0); j < method.ChildCount(); j++ {
			node := method.Child(j)
			if node.Kind() == "identifier" && node.Utf8Text(source) == "toResponse" {
				bodyText := method.Utf8Text(source)
				statusCode := inferResponseStatusFromBody(bodyText)
				if statusCode > 0 {
					ctx.exceptionHandlers[exceptionType] = statusCode
				}
				return
			}
		}
	}
}

// inferResponseStatusFromBody infers an HTTP status code from JAX-RS Response builder patterns.
func inferResponseStatusFromBody(bodyText string) int {
	statusPatterns := map[string]int{
		"Response.status(Response.Status.NOT_FOUND)":            404,
		"Response.status(404)":                                  404,
		"Response.status(Response.Status.BAD_REQUEST)":          400,
		"Response.status(400)":                                  400,
		"Response.status(Response.Status.CONFLICT)":             409,
		"Response.status(409)":                                  409,
		"Response.status(Response.Status.FORBIDDEN)":            403,
		"Response.status(403)":                                  403,
		"Response.status(Response.Status.UNAUTHORIZED)":         401,
		"Response.status(401)":                                  401,
		"Response.status(Response.Status.INTERNAL_SERVER_ERROR)": 500,
		"Response.status(500)":                                  500,
		"Response.status(Response.Status.SERVICE_UNAVAILABLE)":  503,
		"Response.status(503)":                                  503,
	}
	for pattern, code := range statusPatterns {
		if strings.Contains(bodyText, pattern) {
			return code
		}
	}
	return 0
}

// extractClassOperations extracts operations from a class declaration.
func extractClassOperations(classNode *tree_sitter.Node, source []byte, filePath string, ctx *extractContext) []*Operation {
	// Determine framework and base path from class-level annotations
	framework, basePath, classTags, classHidden, classSec := analyzeClassAnnotations(classNode, source, ctx.constants)
	if framework == "" || classHidden {
		return nil
	}

	// Check for parent class base path (C3: inheritance)
	superClass := extractSuperClassName(classNode, source)
	if superClass != "" {
		if parentPath, ok := ctx.classPaths[superClass]; ok {
			basePath = joinPaths(parentPath, basePath)
		}
	}

	// Detect base class for pagination parameter inheritance
	baseClassParamSet := baseClassPaginationParams(superClass)

	var ops []*Operation

	// Find the class body
	var classBody *tree_sitter.Node
	for i := uint(0); i < classNode.ChildCount(); i++ {
		child := classNode.Child(i)
		if child.Kind() == "class_body" {
			classBody = child
			break
		}
	}

	if classBody == nil {
		return nil
	}

	// Extract operations from methods
	for i := uint(0); i < classBody.ChildCount(); i++ {
		child := classBody.Child(i)
		if child.Kind() != "method_declaration" {
			continue
		}

		op := extractMethodOperation(child, source, basePath, framework, classTags, classSec, ctx)
		if op != nil {
			op.File = filePath
			// Phase 5: Inject base class pagination params for list endpoints
			if len(baseClassParamSet) > 0 && isListEndpoint(op) {
				op.Parameters = injectBaseClassParams(op.Parameters, baseClassParamSet)
			}
			ops = append(ops, op)
		}
	}

	return ops
}

// extractSuperClassName extracts the parent class name from the extends clause.
func extractSuperClassName(classNode *tree_sitter.Node, source []byte) string {
	for i := uint(0); i < classNode.ChildCount(); i++ {
		child := classNode.Child(i)
		if child.Kind() == "superclass" {
			for j := uint(0); j < child.ChildCount(); j++ {
				n := child.Child(j)
				if n.Kind() == "type_identifier" || n.Kind() == "generic_type" {
					name := n.Utf8Text(source)
					// For generic_type like BaseClass<T>, extract just the class name
					if idx := strings.Index(name, "<"); idx >= 0 {
						name = name[:idx]
					}
					return name
				}
			}
		}
	}
	return ""
}

// analyzeClassAnnotations determines the framework type and base path from class annotations.
// Returns hidden=true if @Hidden is present on the class.
func analyzeClassAnnotations(classNode *tree_sitter.Node, source []byte, constants map[string]string) (framework, basePath string, tags []string, hidden bool, classSecurity []string) {
	for i := uint(0); i < classNode.ChildCount(); i++ {
		child := classNode.Child(i)
		if child.Kind() != "modifiers" {
			continue
		}

		for j := uint(0); j < child.ChildCount(); j++ {
			ann := child.Child(j)
			annName, annArgs := extractAnnotation(ann, source)

			switch annName {
			case "RestController", "Controller":
				framework = "spring"
			case "Path":
				framework = "jaxrs"
				basePath = extractStringFromAnnotationArgs(annArgs, constants)
			case "RequestMapping":
				if framework == "" {
					framework = "spring"
				}
				basePath = extractMappingPathFromString(annArgs, constants)
			case "Hidden":
				hidden = true
			case "Tag":
				tagName := extractStringFromAnnotationArgs(annArgs, constants)
				if tagName != "" {
					tags = append(tags, tagName)
				}
			case "Tags":
				// Support plural @Tags({@Tag(name="A"), @Tag(name="B")})
				parsed := parsePluralTags(annArgs)
				tags = append(tags, parsed...)
			case "PreAuthorize":
				classSecurity = append(classSecurity, parsePreAuthorize(annArgs, constants)...)
			case "Secured":
				classSecurity = append(classSecurity, parseSecuredAnnotation(annArgs)...)
			case "RequireRight":
				classSecurity = append(classSecurity, parseRequireRight(annArgs, constants)...)
			case "RolesAllowed":
				classSecurity = append(classSecurity, parseSecuredAnnotation(annArgs)...)
			}
		}
	}

	// Extract class name for default tag
	if len(tags) == 0 {
		for i := uint(0); i < classNode.ChildCount(); i++ {
			child := classNode.Child(i)
			if child.Kind() == "identifier" {
				className := child.Utf8Text(source)
				className = strings.TrimSuffix(className, "Controller")
				className = strings.TrimSuffix(className, "Resource")
				className = strings.TrimSuffix(className, "RestResource")
				className = strings.TrimSuffix(className, "Api")
				if className != "" {
					tags = append(tags, className)
				}
				break
			}
		}
	}

	return
}

// parsePluralTags parses @Tags({@Tag(name = "A"), @Tag(name = "B")}).
func parsePluralTags(args string) []string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)
	args = strings.TrimPrefix(args, "{")
	args = strings.TrimSuffix(args, "}")

	var tags []string
	chunks := strings.Split(args, "@Tag")
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		chunk = strings.TrimPrefix(chunk, ",")
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		balanced := extractBalancedParens(chunk)
		if balanced == "" {
			continue
		}
		inner := strings.TrimPrefix(balanced, "(")
		inner = strings.TrimSuffix(inner, ")")
		inner = strings.TrimSpace(inner)
		for _, part := range splitAnnotationArgs(inner) {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "name") {
				if idx := strings.Index(part, "="); idx >= 0 {
					name := stripJavaQuotes(strings.TrimSpace(part[idx+1:]))
					if name != "" {
						tags = append(tags, name)
					}
				}
			}
		}
		if !strings.Contains(inner, "=") {
			name := stripJavaQuotes(inner)
			if name != "" && name != inner {
				tags = append(tags, name)
			}
		}
	}
	return tags
}

// extractBalancedParens extracts the first balanced (...) expression from text.
func extractBalancedParens(text string) string {
	start := strings.Index(text, "(")
	if start < 0 {
		return ""
	}
	depth := 0
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}

// extractMethodOperation extracts an operation from a method declaration.
func extractMethodOperation(methodNode *tree_sitter.Node, source []byte, basePath, framework string, classTags []string, classSecurity []string, ctx *extractContext) *Operation {
	httpMethod := ""
	methodPath := ""
	methodName := ""
	var params []*Parameter
	returnType := ""
	deprecated := false
	deprecatedSince := ""
	hidden := false
	summary := ""
	description := ""
	consumesContentType := ""
	producesContentType := ""
	rateLimited := false
	nullableResponse := false
	requestBodyDescription := ""
	var security []string
	security = append(security, classSecurity...)
	var annotatedResponses []AnnotatedResponse

	// Extract method-level annotations and metadata
	for i := uint(0); i < methodNode.ChildCount(); i++ {
		child := methodNode.Child(i)
		switch child.Kind() {
		case "modifiers":
			for j := uint(0); j < child.ChildCount(); j++ {
				ann := child.Child(j)
				annName, annArgs := extractAnnotation(ann, source)

				switch framework {
				case "spring":
					httpMethod, methodPath = analyzeSpringMethodAnnotation(annName, annArgs, source, httpMethod, methodPath, ctx.constants)
					ct, pt := extractSpringContentTypes(annName, annArgs, ctx.constants)
					if ct != "" {
						consumesContentType = ct
					}
					if pt != "" {
						producesContentType = pt
					}
				case "jaxrs":
					httpMethod, methodPath = analyzeJaxRsMethodAnnotation(annName, annArgs, source, httpMethod, methodPath, ctx.constants)
					if annName == "Consumes" {
						consumesContentType = extractMediaType(annArgs)
					}
					if annName == "Produces" {
						producesContentType = extractMediaType(annArgs)
					}
				}

				if annName == "Deprecated" {
					deprecated = true
					if annArgs != "" {
						if since := extractAnnotationNamedParam(annArgs, "since"); since != "" {
							deprecatedSince = stripJavaQuotes(since)
						}
					}
				}
				if annName == "Hidden" {
					hidden = true
				}
				if annName == "Operation" {
					summary, description = extractOpenAPIOperationInfo(annArgs, source)
				}

				// @ApiResponse / @ApiResponses
				if annName == "ApiResponse" {
					if ar := parseApiResponseAnnotation(annArgs); ar != nil {
						annotatedResponses = append(annotatedResponses, *ar)
					}
				}
				if annName == "ApiResponses" {
					annotatedResponses = append(annotatedResponses, parseApiResponsesAnnotation(annArgs)...)
				}

				// @Parameter (OpenAPI 3)
				if annName == "Parameter" {
					// Method-level @Parameter is handled in classifyParameterAnnotation
				}

				// @SecurityRequirement
				if annName == "SecurityRequirement" {
					sec := parseSecurityRequirement(annArgs)
					security = append(security, sec...)
				}

				// @PreAuthorize / @Secured / @RequireRight / @RolesAllowed
				if annName == "PreAuthorize" {
					sec := parsePreAuthorize(annArgs, ctx.constants)
					security = append(security, sec...)
				}
				if annName == "Secured" {
					sec := parseSecuredAnnotation(annArgs)
					security = append(security, sec...)
				}
				if annName == "RequireRight" {
					sec := parseRequireRight(annArgs, ctx.constants)
					security = append(security, sec...)
				}
				if annName == "RolesAllowed" {
					sec := parseSecuredAnnotation(annArgs)
					security = append(security, sec...)
				}

				// Improvement #14: @Metered / @Timed / @RateLimited → x-rate-limited
				if annName == "Metered" || annName == "Timed" || annName == "RateLimited" {
					rateLimited = true
				}

				// Improvement #15: @Nullable on return type
				if annName == "Nullable" {
					nullableResponse = true
				}
			}

		case "identifier":
			methodName = child.Utf8Text(source)

		case "formal_parameters":
			params = extractMethodParameters(child, source, framework, ctx)

		case "type_identifier", "generic_type", "array_type", "void_type":
			returnType = child.Utf8Text(source)
		}
	}

	// Improvement #15: Optional<T> return type → nullable
	if strings.HasPrefix(returnType, "Optional<") {
		nullableResponse = true
	}

	if hidden {
		return nil
	}

	// Parse JavaDoc for structured tags
	jdoc := extractJavaDoc(methodNode, source)
	if summary == "" {
		summary = jdoc.Summary
	}
	if description == "" {
		description = jdoc.Description
	}

	// Apply @param descriptions to parameters
	for _, p := range params {
		if desc, ok := jdoc.Params[p.Name]; ok && p.Description == "" {
			p.Description = desc
		}
	}

	if httpMethod == "" {
		return nil
	}

	// Build full path (C2: proper path joining)
	fullPath := joinPaths(basePath, methodPath)
	if fullPath == "" {
		fullPath = "/"
	}

	// Analyze method body for response type, status code, headers, and thrown exceptions (H1, H4)
	bodyResponseType, bodyStatusCode, bodyHeaders, bodyExceptions := analyzeMethodBody(methodNode, source)

	// For JAX-RS Response return type, use body-inferred type (H1)
	if returnType == "Response" && bodyResponseType != "" {
		returnType = bodyResponseType
	}

	// Determine response status: prefer body analysis, then @ResponseStatus, then heuristic (H4)
	responseStatus := 0
	if bodyStatusCode > 0 {
		responseStatus = bodyStatusCode
	}
	if responseStatus == 0 {
		responseStatus = extractResponseStatusAnnotation(methodNode, source)
	}
	// Improvement #17: 204 for DELETE with void/Response return and no explicit status
	if responseStatus == 0 && strings.ToUpper(httpMethod) == "DELETE" &&
		(returnType == "void" || returnType == "" || returnType == "Response") {
		responseStatus = 204
	}
	if responseStatus == 0 {
		responseStatus = inferResponseStatus(httpMethod, returnType)
	}

	// Determine request body and form params from parameters
	requestBodyType := ""
	var filteredParams []*Parameter
	var formParams []*Parameter
	for _, p := range params {
		if p.In == "body" {
			requestBodyType = p.Type
			// Improvement #19: Request body description from @param JavaDoc
			if requestBodyDescription == "" {
				if desc, ok := jdoc.Params[p.Name]; ok {
					requestBodyDescription = desc
				}
			}
		} else if p.In == "form" {
			formParams = append(formParams, p)
		} else {
			filteredParams = append(filteredParams, p)
		}
	}

	// Build operation ID
	opID := methodName
	if opID == "" {
		opID = strings.ToLower(httpMethod) + strings.ReplaceAll(cases.Title(language.English).String(fullPath), "/", "")
	}

	// Map @throws to error responses
	var errorResponses map[int]string
	if len(jdoc.Throws) > 0 {
		errorResponses = make(map[int]string)
		for exClass, desc := range jdoc.Throws {
			code := exceptionToStatusCode(exClass)
			errorResponses[code] = desc
		}
	}

	// Improvement #3: Exception-to-status mapping from throw statements in method body
	if len(bodyExceptions) > 0 {
		if errorResponses == nil {
			errorResponses = make(map[int]string)
		}
		for _, exName := range bodyExceptions {
			// Check ControllerAdvice/ExceptionMapper handlers first
			code := 0
			if c, ok := ctx.exceptionHandlers[exName]; ok {
				code = c
			}
			if code == 0 {
				code = exceptionToStatusCode(exName)
			}
			if _, exists := errorResponses[code]; !exists {
				errorResponses[code] = getExceptionDescription(exName, code)
			}
		}
	}

	// Phase 6: Merge globally-detected exception handler mappings for @throws declarations
	if len(jdoc.Throws) > 0 {
		for exClass := range jdoc.Throws {
			if code, ok := ctx.exceptionHandlers[exClass]; ok {
				if errorResponses == nil {
					errorResponses = make(map[int]string)
				}
				if _, exists := errorResponses[code]; !exists {
					errorResponses[code] = getExceptionDescription(exClass, code)
				}
			}
		}
	}

	// Capture source location from method node
	startPos := methodNode.StartPosition()

	return &Operation{
		Path:                   fullPath,
		Method:                 httpMethod,
		OperationID:            opID,
		Summary:                summary,
		Description:            description,
		Tags:                   classTags,
		Parameters:             filteredParams,
		RequestBodyType:        requestBodyType,
		RequestBodyDescription: requestBodyDescription,
		ResponseType:           returnType,
		ResponseStatus:         responseStatus,
		Deprecated:             deprecated,
		DeprecatedSince:        deprecatedSince,
		Security:               security,
		ConsumesContentType:    consumesContentType,
		ProducesContentType:    producesContentType,
		ReturnDescription:      jdoc.Return,
		ErrorResponses:         errorResponses,
		AnnotatedResponses:     annotatedResponses,
		ResponseHeaders:        bodyHeaders,
		NullableResponse:       nullableResponse,
		RateLimited:            rateLimited,
		FormParams:             formParams,
		Line:                   int(startPos.Row) + 1,
		Column:                 int(startPos.Column) + 1,
	}
}

// =============================================================================
// Annotation parsing: @ApiResponse, @SecurityRequirement, @PreAuthorize, @Secured
// =============================================================================

// parseApiResponseAnnotation parses @ApiResponse(responseCode = "200", description = "Success",
//
//	content = @Content(schema = @Schema(implementation = MyDto.class)))
func parseApiResponseAnnotation(args string) *AnnotatedResponse {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)
	if args == "" {
		return nil
	}

	ar := &AnnotatedResponse{}
	for _, part := range splitAnnotationArgs(args) {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "responseCode") {
			if idx := strings.Index(part, "="); idx >= 0 {
				code := stripJavaQuotes(strings.TrimSpace(part[idx+1:]))
				ar.StatusCode, _ = strconv.Atoi(code)
			}
		}
		if strings.HasPrefix(part, "description") {
			if idx := strings.Index(part, "="); idx >= 0 {
				ar.Description = stripJavaQuotes(strings.TrimSpace(part[idx+1:]))
			}
		}
		// Improvement #18: content = @Content(schema = @Schema(implementation = MyDto.class))
		if strings.HasPrefix(part, "content") {
			if schemaType := extractApiResponseSchemaType(part); schemaType != "" {
				ar.SchemaType = schemaType
			}
		}
	}
	if ar.StatusCode == 0 && ar.Description == "" {
		return nil
	}
	return ar
}

// reImplementation matches implementation = ClassName.class
var reImplementation = regexp.MustCompile(`implementation\s*=\s*(\w+)\.class`)

// extractApiResponseSchemaType extracts the schema implementation type from @Content(@Schema(implementation = ...))
func extractApiResponseSchemaType(contentArg string) string {
	if m := reImplementation.FindStringSubmatch(contentArg); len(m) > 1 {
		return m[1]
	}
	return ""
}

// parseApiResponsesAnnotation parses @ApiResponses({@ApiResponse(...), @ApiResponse(...)})
func parseApiResponsesAnnotation(args string) []AnnotatedResponse {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)
	args = strings.TrimPrefix(args, "{")
	args = strings.TrimSuffix(args, "}")

	var results []AnnotatedResponse
	chunks := strings.Split(args, "@ApiResponse")
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		chunk = strings.TrimPrefix(chunk, ",")
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		balanced := extractBalancedParens(chunk)
		if balanced != "" {
			if ar := parseApiResponseAnnotation(balanced); ar != nil {
				results = append(results, *ar)
			}
		}
	}
	return results
}

// parseSecurityRequirement parses @SecurityRequirement(name = "oauth2", scopes = {"read", "write"})
func parseSecurityRequirement(args string) []string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")

	var scopes []string
	for _, part := range splitAnnotationArgs(args) {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "scopes") {
			if idx := strings.Index(part, "="); idx >= 0 {
				scopeStr := strings.TrimSpace(part[idx+1:])
				scopeStr = strings.Trim(scopeStr, "{}")
				for _, s := range strings.Split(scopeStr, ",") {
					s = strings.TrimSpace(s)
					s = stripJavaQuotes(s)
					if s != "" {
						scopes = append(scopes, s)
					}
				}
			}
		}
	}
	if len(scopes) == 0 {
		// Fall back to extracting the name itself as a scope
		for _, part := range splitAnnotationArgs(args) {
			part = strings.TrimSpace(part)
			if strings.HasPrefix(part, "name") {
				if idx := strings.Index(part, "="); idx >= 0 {
					name := stripJavaQuotes(strings.TrimSpace(part[idx+1:]))
					if name != "" {
						scopes = append(scopes, name)
					}
				}
			}
		}
	}
	return scopes
}

var reHasAuthority = regexp.MustCompile(`hasAuthority\(\s*'([^']+)'\s*\)`)
var reHasRole = regexp.MustCompile(`hasRole\(\s*'([^']+)'\s*\)`)
var reHasAnyAuthority = regexp.MustCompile(`hasAnyAuthority\(\s*(.+?)\s*\)`)
var reHasAnyRole = regexp.MustCompile(`hasAnyRole\(\s*(.+?)\s*\)`)
var reTClassConstant = regexp.MustCompile(`T\(\w+\)\.(\w+)`)

// parsePreAuthorize parses @PreAuthorize("hasAuthority('sp:api:read')") etc.
func parsePreAuthorize(args string, constants map[string]string) []string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	raw := stripJavaQuotes(args)

	var scopes []string
	for _, m := range reHasAuthority.FindAllStringSubmatch(raw, -1) {
		scopes = append(scopes, m[1])
	}
	for _, m := range reHasRole.FindAllStringSubmatch(raw, -1) {
		scopes = append(scopes, "ROLE_"+m[1])
	}
	if matches := reHasAnyAuthority.FindStringSubmatch(raw); len(matches) > 1 {
		for _, part := range strings.Split(matches[1], ",") {
			part = strings.TrimSpace(part)
			part = strings.Trim(part, "'\"")
			if part != "" {
				scopes = append(scopes, part)
			}
		}
	}
	if matches := reHasAnyRole.FindStringSubmatch(raw); len(matches) > 1 {
		for _, part := range strings.Split(matches[1], ",") {
			part = strings.TrimSpace(part)
			part = strings.Trim(part, "'\"")
			if part != "" {
				scopes = append(scopes, "ROLE_"+part)
			}
		}
	}
	// Resolve T(ClassName).CONSTANT references
	for _, m := range reTClassConstant.FindAllStringSubmatch(raw, -1) {
		if constants != nil {
			if val, ok := constants[m[1]]; ok {
				scopes = append(scopes, val)
			}
		}
	}
	return scopes
}

// parseSecuredAnnotation parses @Secured("ROLE_ADMIN") or @Secured({"ROLE_A", "ROLE_B"})
func parseSecuredAnnotation(args string) []string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)

	// Strip outer { } for array form
	inner := strings.TrimPrefix(args, "{")
	inner = strings.TrimSuffix(inner, "}")

	var roles []string
	for _, part := range strings.Split(inner, ",") {
		part = strings.TrimSpace(part)
		role := stripJavaQuotes(part)
		if role != "" {
			roles = append(roles, role)
		}
	}
	return roles
}

// parseRequireRight parses @RequireRight("sp:scope:read") or @RequireRight({"a", "b"}) or @RequireRight(Right.READ).
func parseRequireRight(args string, constants map[string]string) []string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)

	// Strip outer { } for array form
	inner := strings.TrimPrefix(args, "{")
	inner = strings.TrimSuffix(inner, "}")

	var rights []string
	for _, part := range strings.Split(inner, ",") {
		part = strings.TrimSpace(part)
		stripped := stripJavaQuotes(part)
		if stripped != part && stripped != "" {
			// Was a quoted string literal
			rights = append(rights, stripped)
		} else if part != "" {
			// Try constant resolution (e.g. Right.READ or just READ)
			if constants != nil {
				if val, ok := constants[part]; ok {
					rights = append(rights, val)
					continue
				}
				// Try just the identifier after the dot
				if dotIdx := strings.LastIndex(part, "."); dotIdx >= 0 {
					shortName := part[dotIdx+1:]
					if val, ok := constants[shortName]; ok {
						rights = append(rights, val)
						continue
					}
				}
			}
			// Use as-is if no constant found
			rights = append(rights, part)
		}
	}
	return rights
}

// splitAnnotationArgs splits annotation arguments by top-level commas, respecting nesting.
func splitAnnotationArgs(args string) []string {
	var parts []string
	depth := 0
	start := 0
	inString := false
	for i := 0; i < len(args); i++ {
		c := args[i]
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch c {
		case '(', '{':
			depth++
		case ')', '}':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(args[start:i]))
				start = i + 1
			}
		}
	}
	if start < len(args) {
		parts = append(parts, strings.TrimSpace(args[start:]))
	}
	return parts
}

// =============================================================================
// Method body analysis (H1: Response type, H4: status codes)
// =============================================================================

// analyzeMethodBody scans a method body for Response builder patterns, status codes,
// response headers, and thrown exceptions.
func analyzeMethodBody(methodNode *tree_sitter.Node, source []byte) (responseType string, statusCode int, headers map[string]string, exceptions []string) {
	var body *tree_sitter.Node
	for i := uint(0); i < methodNode.ChildCount(); i++ {
		child := methodNode.Child(i)
		if child.Kind() == "block" {
			body = child
			break
		}
	}
	if body == nil {
		return
	}

	bodyText := body.Utf8Text(source)

	// Infer status code from Response builder methods
	if strings.Contains(bodyText, "Response.ok(") || strings.Contains(bodyText, ".ok(") || strings.Contains(bodyText, "okResponse(") {
		statusCode = 200
	}
	if strings.Contains(bodyText, "Response.noContent()") || strings.Contains(bodyText, "noContent()") {
		statusCode = 204
	}
	if strings.Contains(bodyText, "Response.accepted()") || strings.Contains(bodyText, "accepted()") ||
		strings.Contains(bodyText, "ACCEPTED)") {
		statusCode = 202
	}
	if strings.Contains(bodyText, "Response.created(") || strings.Contains(bodyText, "createdResponse(") {
		statusCode = 201
	}
	if strings.Contains(bodyText, "Response.status(Response.Status.NO_CONTENT)") ||
		strings.Contains(bodyText, "Response.status(NO_CONTENT)") ||
		strings.Contains(bodyText, "status(204)") {
		statusCode = 204
	}
	// Additional JAX-RS Response.status patterns
	if statusCode == 0 {
		if strings.Contains(bodyText, "Response.status(Response.Status.CREATED)") || strings.Contains(bodyText, "status(201)") {
			statusCode = 201
		} else if strings.Contains(bodyText, "Response.status(Response.Status.ACCEPTED)") || strings.Contains(bodyText, "status(202)") {
			statusCode = 202
		}
	}

	// Infer response entity type from Response.ok(entity) or .entity(dto)
	responseType = extractResponseEntityType(bodyText)

	// Improvement #2: Detect response headers from builder chains
	headers = extractResponseHeaders(bodyText)

	// Improvement #3: Detect thrown exceptions for error status mapping
	exceptions = extractThrownExceptions(bodyText)

	return
}

// reHeaderBuilder matches .header("X-Total-Count", ...) or .add("X-Total-Count", ...) patterns.
var reHeaderBuilder = regexp.MustCompile(`\.header\(\s*"([^"]+)"`)
var reHeadersAdd = regexp.MustCompile(`\.add\(\s*"(X-[^"]+)"`)

// extractResponseHeaders detects response header additions in method body text.
func extractResponseHeaders(bodyText string) map[string]string {
	var headers map[string]string
	for _, m := range reHeaderBuilder.FindAllStringSubmatch(bodyText, -1) {
		if headers == nil {
			headers = make(map[string]string)
		}
		headers[m[1]] = describeResponseHeader(m[1])
	}
	for _, m := range reHeadersAdd.FindAllStringSubmatch(bodyText, -1) {
		if headers == nil {
			headers = make(map[string]string)
		}
		headers[m[1]] = describeResponseHeader(m[1])
	}
	return headers
}

// describeResponseHeader provides a description for well-known response headers.
func describeResponseHeader(name string) string {
	switch name {
	case "X-Total-Count":
		return "Total number of matching items"
	case "X-Aggregate-Count":
		return "Aggregate count of matching items"
	default:
		return name
	}
}

// reThrownException matches "throw new ExceptionClass(" patterns.
var reThrownException = regexp.MustCompile(`throw\s+new\s+(\w+Exception)\s*\(`)

// extractThrownExceptions detects exception classes thrown in method body.
func extractThrownExceptions(bodyText string) []string {
	seen := make(map[string]bool)
	var exceptions []string
	for _, m := range reThrownException.FindAllStringSubmatch(bodyText, -1) {
		name := m[1]
		if !seen[name] {
			seen[name] = true
			exceptions = append(exceptions, name)
		}
	}
	return exceptions
}

// getExceptionDescription provides a human-readable description for an exception-to-status mapping.
func getExceptionDescription(exName string, code int) string {
	switch code {
	case 400:
		return "Bad Request"
	case 401:
		return "Unauthorized"
	case 403:
		return "Forbidden"
	case 404:
		return "Not Found"
	case 409:
		return "Conflict"
	case 413:
		return "Payload Too Large"
	case 415:
		return "Unsupported Media Type"
	case 500:
		return "Internal Server Error"
	case 501:
		return "Not Implemented"
	case 503:
		return "Service Unavailable"
	default:
		return fmt.Sprintf("Error %d", code)
	}
}

// extractResponseEntityType tries to infer the response entity type from method body text.
func extractResponseEntityType(bodyText string) string {
	// Look for patterns like:
	//   Response.ok(variable).build()
	//   okResponse(variable)
	//   .entity(variable).build()
	patterns := []string{"Response.ok(", "okResponse(", ".entity("}
	for _, pattern := range patterns {
		idx := strings.Index(bodyText, pattern)
		if idx < 0 {
			continue
		}
		start := idx + len(pattern)
		// Find the closing paren
		depth := 1
		end := start
		for end < len(bodyText) && depth > 0 {
			if bodyText[end] == '(' {
				depth++
			}
			if bodyText[end] == ')' {
				depth--
			}
			end++
		}
		if depth == 0 {
			arg := strings.TrimSpace(bodyText[start : end-1])
			// If arg is a method call like service.getFoo(), we can't resolve it
			// If arg is a variable or new Type(...), try to extract the type
			if strings.HasPrefix(arg, "new ") {
				// new FooDTO(...)
				typeName := strings.TrimPrefix(arg, "new ")
				if parenIdx := strings.Index(typeName, "("); parenIdx >= 0 {
					typeName = typeName[:parenIdx]
				}
				return strings.TrimSpace(typeName)
			}
		}
	}
	return ""
}

// extractResponseStatusAnnotation checks for @ResponseStatus on a method.
func extractResponseStatusAnnotation(methodNode *tree_sitter.Node, source []byte) int {
	for i := uint(0); i < methodNode.ChildCount(); i++ {
		child := methodNode.Child(i)
		if child.Kind() != "modifiers" {
			continue
		}
		for j := uint(0); j < child.ChildCount(); j++ {
			ann := child.Child(j)
			annName, annArgs := extractAnnotation(ann, source)
			if annName == "ResponseStatus" {
				return parseResponseStatusValue(annArgs)
			}
		}
	}
	return 0
}

// parseResponseStatusValue parses @ResponseStatus(HttpStatus.NO_CONTENT) etc.
func parseResponseStatusValue(args string) int {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)

	// Handle value = HttpStatus.XXX or just HttpStatus.XXX
	if strings.Contains(args, "=") {
		parts := strings.SplitN(args, "=", 2)
		args = strings.TrimSpace(parts[1])
	}

	// Map HttpStatus enum values
	statusMap := map[string]int{
		"OK":                    200,
		"CREATED":               201,
		"ACCEPTED":              202,
		"NO_CONTENT":            204,
		"BAD_REQUEST":           400,
		"UNAUTHORIZED":          401,
		"FORBIDDEN":             403,
		"NOT_FOUND":             404,
		"CONFLICT":              409,
		"INTERNAL_SERVER_ERROR": 500,
	}

	for suffix, code := range statusMap {
		if strings.HasSuffix(args, suffix) {
			return code
		}
	}
	return 0
}

// =============================================================================
// Content type extraction (L1, L2)
// =============================================================================

// extractSpringContentTypes extracts consumes/produces from Spring annotations.
func extractSpringContentTypes(annName, annArgs string, constants map[string]string) (consumes, produces string) {
	args := strings.TrimPrefix(annArgs, "(")
	args = strings.TrimSuffix(args, ")")

	for _, part := range strings.Split(args, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "consumes") {
			if idx := strings.Index(part, "="); idx >= 0 {
				val := strings.TrimSpace(part[idx+1:])
				consumes = extractMediaType(val)
			}
		}
		if strings.HasPrefix(part, "produces") {
			if idx := strings.Index(part, "="); idx >= 0 {
				val := strings.TrimSpace(part[idx+1:])
				produces = extractMediaType(val)
			}
		}
	}
	return
}

// extractMediaType extracts a media type string from annotation args.
func extractMediaType(args string) string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)

	// Handle MediaType constants
	mediaTypeMap := map[string]string{
		"MediaType.APPLICATION_JSON":            "application/json",
		"APPLICATION_JSON":                      "application/json",
		"MediaType.APPLICATION_JSON_PATCH_JSON": "application/json-patch+json",
		"APPLICATION_JSON_PATCH_JSON":           "application/json-patch+json",
		"MediaType.APPLICATION_XML":             "application/xml",
		"MediaType.TEXT_PLAIN":                  "text/plain",
		"MediaType.MULTIPART_FORM_DATA":         "multipart/form-data",
	}

	for constant, mediaType := range mediaTypeMap {
		if strings.Contains(args, constant) {
			return mediaType
		}
	}

	// Try as quoted string
	stripped := stripJavaQuotes(args)
	if stripped != args && stripped != "" {
		return stripped
	}

	return ""
}

// =============================================================================
// Annotation analysis
// =============================================================================

// analyzeSpringMethodAnnotation processes Spring Boot method annotations.
func analyzeSpringMethodAnnotation(annName, annArgs string, source []byte, currentMethod, currentPath string, constants map[string]string) (string, string) {
	method := currentMethod
	path := currentPath

	switch annName {
	case "GetMapping":
		method = "GET"
		path = extractMappingPathFromString(annArgs, constants)
	case "PostMapping":
		method = "POST"
		path = extractMappingPathFromString(annArgs, constants)
	case "PutMapping":
		method = "PUT"
		path = extractMappingPathFromString(annArgs, constants)
	case "DeleteMapping":
		method = "DELETE"
		path = extractMappingPathFromString(annArgs, constants)
	case "PatchMapping":
		method = "PATCH"
		path = extractMappingPathFromString(annArgs, constants)
	case "RequestMapping":
		path = extractMappingPathFromString(annArgs, constants)
		if method == "" {
			method = extractRequestMappingMethod(annArgs)
		}
	}

	return method, path
}

// analyzeJaxRsMethodAnnotation processes JAX-RS method annotations.
func analyzeJaxRsMethodAnnotation(annName, annArgs string, source []byte, currentMethod, currentPath string, constants map[string]string) (string, string) {
	method := currentMethod
	path := currentPath

	switch annName {
	case "GET":
		method = "GET"
	case "POST":
		method = "POST"
	case "PUT":
		method = "PUT"
	case "DELETE":
		method = "DELETE"
	case "PATCH":
		method = "PATCH"
	case "HEAD":
		method = "HEAD"
	case "OPTIONS":
		method = "OPTIONS"
	case "Path":
		path = extractStringFromAnnotationArgs(annArgs, constants)
	}

	return method, path
}

// extractMethodParameters extracts parameters from method formal parameters.
func extractMethodParameters(paramsNode *tree_sitter.Node, source []byte, framework string, ctx *extractContext) []*Parameter {
	var params []*Parameter

	for i := uint(0); i < paramsNode.ChildCount(); i++ {
		child := paramsNode.Child(i)
		if child.Kind() != "formal_parameter" {
			continue
		}

		param := extractSingleParameter(child, source, framework, ctx)
		if param != nil {
			// Expand Pageable to standard pagination query parameters
			if param.Type == "Pageable" {
				params = append(params, expandPageableParameters()...)
				continue
			}
			// Improvement #12: Expand custom pagination types
			if isCustomPaginationType(param.Type) {
				params = append(params, expandCustomPaginationParameters(param.Type)...)
				continue
			}
			// Improvement #9: @BeanParam expansion for JAX-RS
			if param.In == "bean" {
				expanded := expandBeanParam(param.Type, ctx.idx)
				if len(expanded) > 0 {
					params = append(params, expanded...)
					continue
				}
			}
			params = append(params, param)
		}
	}

	return params
}

// expandPageableParameters expands a Spring Pageable parameter into standard pagination query params.
func expandPageableParameters() []*Parameter {
	return []*Parameter{
		{Name: "page", In: "query", Type: "int", Required: false, Description: "Page number (zero-based)"},
		{Name: "size", In: "query", Type: "int", Required: false, Description: "Number of items per page"},
		{Name: "sort", In: "query", Type: "String", Required: false, Description: "Sort criteria (e.g. field,asc)"},
	}
}

// Improvement #12: Custom pagination type recognition.
var customPaginationTypes = map[string]bool{
	"ChroniclePagingOptions": true,
	"SimpleQueryOptions":     true,
	"AmsQueryOptions":        true,
	"PagingOptions":          true,
	"QueryOptions":           true,
}

// isCustomPaginationType checks if a type is a known custom pagination type.
func isCustomPaginationType(typeName string) bool {
	return customPaginationTypes[typeName]
}

// expandCustomPaginationParameters expands a custom pagination type to standard offset/limit params.
func expandCustomPaginationParameters(typeName string) []*Parameter {
	return []*Parameter{
		{Name: "offset", In: "query", Type: "int", Required: false, Description: "Start index of results", DefaultValue: "0"},
		{Name: "limit", In: "query", Type: "int", Required: false, Description: "Maximum number of results to return", DefaultValue: "250"},
	}
}

// baseClassPaginationParams returns standard pagination parameters for known base classes.
func baseClassPaginationParams(superClassName string) []*Parameter {
	switch superClassName {
	case "AtlasBaseV3Resource", "AtlasBaseResource", "BaseV3Resource":
		return []*Parameter{
			{Name: "offset", In: "query", Type: "int", Required: false, Description: "Offset into the full result set. Usually specified with `limit` to paginate through the results.", DefaultValue: "0"},
			{Name: "limit", In: "query", Type: "int", Required: false, Description: "Max number of results to return.", DefaultValue: "250"},
			{Name: "count", In: "query", Type: "boolean", Required: false, Description: "If `true`, include the total result count in the response headers."},
			{Name: "filters", In: "query", Type: "String", Required: false, Description: "Filter expression (e.g. `name eq \"value\"`)"},
			{Name: "sorters", In: "query", Type: "String", Required: false, Description: "Comma-separated sort fields (e.g. `name,-created`)"},
		}
	case "BaseListResource":
		return []*Parameter{
			{Name: "offset", In: "query", Type: "int", Required: false, Description: "Offset into the full result set. Usually specified with `limit` to paginate through the results.", DefaultValue: "0"},
			{Name: "limit", In: "query", Type: "int", Required: false, Description: "Max number of results to return.", DefaultValue: "250"},
		}
	}
	return nil
}

// isListEndpoint checks if an operation is a GET on a collection (no trailing path param).
func isListEndpoint(op *Operation) bool {
	return strings.ToUpper(op.Method) == "GET" && !strings.HasSuffix(op.Path, "}")
}

// injectBaseClassParams adds base class params that aren't already present on the operation.
func injectBaseClassParams(existing []*Parameter, baseParams []*Parameter) []*Parameter {
	existingNames := make(map[string]bool)
	for _, p := range existing {
		existingNames[p.Name] = true
	}
	for _, bp := range baseParams {
		if !existingNames[bp.Name] {
			existing = append(existing, bp)
		}
	}
	return existing
}

// Improvement #9: expandBeanParam resolves a @BeanParam type from the index and expands
// its annotated fields into individual parameters.
func expandBeanParam(typeName string, idx *index.Index) []*Parameter {
	if idx == nil {
		return nil
	}
	decl, ok := idx.ResolveSimple(typeName)
	if !ok {
		return nil
	}

	var params []*Parameter
	for _, f := range decl.Fields {
		in := ""
		name := f.Name
		if f.JSONName != "" {
			name = f.JSONName
		}

		// Check field annotations for JAX-RS parameter annotations
		for annName, annVal := range f.Annotations {
			switch annName {
			case "QueryParam":
				in = "query"
				if v := extractBeanAnnotationValue(annVal); v != "" {
					name = v
				}
			case "HeaderParam":
				in = "header"
				if v := extractBeanAnnotationValue(annVal); v != "" {
					name = v
				}
			case "PathParam":
				in = "path"
				if v := extractBeanAnnotationValue(annVal); v != "" {
					name = v
				}
			case "CookieParam":
				in = "cookie"
				if v := extractBeanAnnotationValue(annVal); v != "" {
					name = v
				}
			}
		}

		if in == "" {
			continue // Skip fields without JAX-RS param annotations
		}

		params = append(params, &Parameter{
			Name:     name,
			In:       in,
			Type:     f.Type,
			Required: f.Required || in == "path",
		})
	}
	return params
}

// extractBeanAnnotationValue strips parens and quotes from annotation value.
func extractBeanAnnotationValue(val string) string {
	val = strings.TrimPrefix(val, "(")
	val = strings.TrimSuffix(val, ")")
	val = strings.TrimSpace(val)
	return stripJavaQuotes(val)
}

// extractSingleParameter extracts a single parameter from a formal_parameter node.
func extractSingleParameter(paramNode *tree_sitter.Node, source []byte, framework string, ctx *extractContext) *Parameter {
	paramName := ""
	paramType := ""
	in := ""
	apiParamName := ""
	required := false
	defaultValue := ""
	description := ""
	example := ""
	format := ""
	pattern := ""
	var minimum, maximum, minLength, maxLength, minItems, maxItems *int
	var enumVals []string

	for i := uint(0); i < paramNode.ChildCount(); i++ {
		child := paramNode.Child(i)
		switch child.Kind() {
		case "modifiers":
			for j := uint(0); j < child.ChildCount(); j++ {
				ann := child.Child(j)
				annName, annArgs := extractAnnotation(ann, source)
				in, apiParamName, required, defaultValue = classifyParameterAnnotation(annName, annArgs, framework, in, apiParamName, required, defaultValue, ctx.constants)

				if annName == "Parameter" {
					overrides := parseParameterAnnotation(annArgs)
					if overrides.Description != "" {
						description = overrides.Description
					}
					if overrides.Example != "" {
						example = overrides.Example
					}
					if overrides.Required != nil {
						required = *overrides.Required
					}
				}

				// Validation annotations
				switch annName {
				case "Pattern":
					if v := extractAnnotationField(annArgs, "regexp"); v != "" {
						pattern = stripJavaQuotes(v)
					}
				case "Email":
					format = "email"
				case "Min":
					if n := parseIntArg(annArgs); n != nil {
						minimum = n
					}
				case "Max":
					if n := parseIntArg(annArgs); n != nil {
						maximum = n
					}
				case "Size":
					if isCollectionType(paramType) {
						if v := extractAnnotationField(annArgs, "min"); v != "" {
							if n := parseIntStr(v); n != nil {
								minItems = n
							}
						}
						if v := extractAnnotationField(annArgs, "max"); v != "" {
							if n := parseIntStr(v); n != nil {
								maxItems = n
							}
						}
					} else {
						if v := extractAnnotationField(annArgs, "min"); v != "" {
							if n := parseIntStr(v); n != nil {
								minLength = n
							}
						}
						if v := extractAnnotationField(annArgs, "max"); v != "" {
							if n := parseIntStr(v); n != nil {
								maxLength = n
							}
						}
					}
				case "NotNull", "NotBlank", "NotEmpty":
					required = true
				}
			}

		case "type_identifier", "generic_type", "array_type",
			"integral_type", "floating_point_type", "boolean_type":
			paramType = child.Utf8Text(source)

		case "identifier":
			paramName = child.Utf8Text(source)
		}
	}

	if paramName == "" {
		return nil
	}

	if in == "" {
		in = inferParameterLocation(paramType, framework)
	}
	if apiParamName != "" {
		paramName = apiParamName
	}
	if isJaxRsContextType(paramType) {
		return nil
	}

	// Derive format from Java type when not set by annotation
	if format == "" {
		format = javaTypeFormat(paramType)
	}

	p := &Parameter{
		Name:         paramName,
		In:           in,
		Type:         paramType,
		Required:     required || in == "path",
		DefaultValue: defaultValue,
		Description:  description,
		Format:       format,
		Pattern:      pattern,
		Minimum:      minimum,
		Maximum:      maximum,
		MinLength:    minLength,
		MaxLength:    maxLength,
		MinItems:     minItems,
		MaxItems:     maxItems,
		Example:      example,
		Enum:         enumVals,
	}
	return p
}

// isCollectionType checks if a Java type is a collection type.
func isCollectionType(typeName string) bool {
	lower := strings.ToLower(typeName)
	return strings.HasPrefix(lower, "list<") || strings.HasPrefix(lower, "set<") ||
		strings.HasPrefix(lower, "collection<") || strings.HasSuffix(typeName, "[]") ||
		strings.HasPrefix(lower, "arraylist<") || strings.HasPrefix(lower, "hashset<") ||
		strings.HasPrefix(lower, "treeset<")
}

// paramAnnotationOverrides holds overrides from @Parameter (OpenAPI 3) annotations.
type paramAnnotationOverrides struct {
	Description string
	Example     string
	Required    *bool // pointer to distinguish unset from false
}

// classifyParameterAnnotation determines parameter location from its annotation.
func classifyParameterAnnotation(annName, annArgs, framework, currentIn, currentApiName string, currentRequired bool, currentDefault string, constants map[string]string) (string, string, bool, string) {
	in := currentIn
	apiName := currentApiName
	required := currentRequired
	def := currentDefault

	switch annName {
	// Spring Boot
	case "PathVariable":
		in = "path"
		apiName = extractStringFromAnnotationArgs(annArgs, constants)
	case "RequestParam":
		in = "query"
		apiName, required, def = extractSpringRequestParam(annArgs, constants)
	case "RequestHeader":
		in = "header"
		apiName = extractStringFromAnnotationArgs(annArgs, constants)
	case "RequestBody":
		in = "body"
	case "CookieValue":
		in = "cookie"
		apiName = extractStringFromAnnotationArgs(annArgs, constants)

	// JAX-RS
	case "PathParam":
		in = "path"
		apiName = extractStringFromAnnotationArgs(annArgs, constants)
	case "QueryParam":
		in = "query"
		apiName = extractStringFromAnnotationArgs(annArgs, constants)
	case "HeaderParam":
		in = "header"
		apiName = extractStringFromAnnotationArgs(annArgs, constants)
	case "CookieParam":
		in = "cookie"
		apiName = extractStringFromAnnotationArgs(annArgs, constants)
	case "FormParam":
		in = "form"
		apiName = extractStringFromAnnotationArgs(annArgs, constants)
	case "RequestPart":
		// @RequestPart — mixed form/file upload, treat like @FormParam
		in = "form"
		apiName = extractStringFromAnnotationArgs(annArgs, constants)
	case "DefaultValue":
		def = extractStringFromAnnotationArgs(annArgs, constants)

	// JAX-RS @BeanParam
	case "BeanParam":
		in = "bean"

	// Validation
	case "NotNull", "NonNull", "NotBlank", "NotEmpty":
		required = true
	case "Valid":
		// @Valid on parameter indicates validation but doesn't change location
	}

	return in, apiName, required, def
}

// parseParameterAnnotation parses @Parameter(description = "...", example = "...", required = true).
func parseParameterAnnotation(args string) paramAnnotationOverrides {
	result := paramAnnotationOverrides{}
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")

	for _, part := range splitAnnotationArgs(args) {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "description") {
			if idx := strings.Index(part, "="); idx >= 0 {
				result.Description = stripJavaQuotes(strings.TrimSpace(part[idx+1:]))
			}
		}
		if strings.HasPrefix(part, "example") {
			if idx := strings.Index(part, "="); idx >= 0 {
				result.Example = stripJavaQuotes(strings.TrimSpace(part[idx+1:]))
			}
		}
		if strings.HasPrefix(part, "required") {
			if strings.Contains(part, "true") {
				t := true
				result.Required = &t
			} else if strings.Contains(part, "false") {
				f := false
				result.Required = &f
			}
		}
	}
	return result
}

// inferParameterLocation infers parameter location from its type.
func inferParameterLocation(typeName, framework string) string {
	switch strings.ToLower(typeName) {
	case "string", "int", "integer", "long", "double", "float", "boolean", "uuid":
		return "query"
	}
	// Pageable is handled specially by expandPageableParameters
	if typeName == "Pageable" {
		return "query"
	}
	return "body"
}

// isJaxRsContextType checks if a type is a JAX-RS context/infrastructure type.
func isJaxRsContextType(typeName string) bool {
	contextTypes := map[string]bool{
		"UriInfo":             true,
		"HttpHeaders":         true,
		"SecurityContext":     true,
		"Request":             true,
		"HttpServletRequest":  true,
		"HttpServletResponse": true,
		"ServletContext":      true,
		"AsyncResponse":       true,
	}
	return contextTypes[typeName]
}

// =============================================================================
// Path utilities
// =============================================================================

// joinPaths properly joins two path segments, ensuring exactly one / between them.
func joinPaths(base, sub string) string {
	if base == "" {
		return sub
	}
	if sub == "" {
		return base
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(sub, "/")
}

// =============================================================================
// Annotation parsing helpers
// =============================================================================

func extractAnnotation(ann *tree_sitter.Node, source []byte) (string, string) {
	name := ""
	args := ""
	for i := uint(0); i < ann.ChildCount(); i++ {
		child := ann.Child(i)
		switch child.Kind() {
		case "identifier":
			name = child.Utf8Text(source)
		case "annotation_argument_list":
			args = child.Utf8Text(source)
		}
	}
	return name, args
}

func extractStringFromAnnotationArgs(args string, constants map[string]string) string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)

	if args == "" {
		return ""
	}

	// Simple string literal: ("value")
	if len(args) >= 2 && strings.HasPrefix(args, "\"") && strings.HasSuffix(args, "\"") && !strings.Contains(args, "\" + ") {
		return args[1 : len(args)-1]
	}

	// value = "string" or value = CONSTANT pattern
	if idx := strings.Index(args, "="); idx >= 0 {
		val := strings.TrimSpace(args[idx+1:])
		// Try as literal first
		stripped := stripJavaQuotes(val)
		if stripped != val {
			return stripped
		}
		// Try constant resolution
		if constants != nil {
			return resolveAnnotationValue(val, constants)
		}
		return val
	}

	// Try constant resolution for bare identifier or concatenation
	if constants != nil {
		resolved := resolveAnnotationValue(args, constants)
		if resolved != args {
			return resolved
		}
	}

	return args
}

func extractMappingPath(args string, source []byte) string {
	return extractMappingPathFromString(args, nil)
}

func extractMappingPathFromString(args string, constants map[string]string) string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)

	if args == "" {
		return ""
	}

	// Simple path: ("/path")
	if strings.HasPrefix(args, "\"") && !strings.Contains(args, "=") {
		result := stripJavaQuotes(args)
		if result != "" {
			return result
		}
	}

	// Named: (path = "/path") or (value = "/path") or (value = CONSTANT)
	for _, part := range strings.Split(args, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "path") || strings.HasPrefix(part, "value") {
			if idx := strings.Index(part, "="); idx >= 0 {
				val := strings.TrimSpace(part[idx+1:])
				// Try as literal
				stripped := stripJavaQuotes(val)
				if stripped != val {
					return stripped
				}
				// Try constant resolution
				if constants != nil {
					return resolveAnnotationValue(val, constants)
				}
				return val
			}
		}
	}

	// If no named parameter matched, try as constant expression
	if constants != nil && !strings.Contains(args, "=") {
		resolved := resolveAnnotationValue(args, constants)
		if resolved != args {
			return resolved
		}
	}

	return ""
}

func extractRequestMappingMethod(args string) string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")

	for _, part := range strings.Split(args, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "method") {
			if idx := strings.Index(part, "="); idx >= 0 {
				val := strings.TrimSpace(part[idx+1:])
				// RequestMethod.GET, RequestMethod.POST, etc.
				val = strings.TrimPrefix(val, "RequestMethod.")
				return strings.ToUpper(val)
			}
		}
	}

	return "GET" // default
}

func extractSpringRequestParam(args string, constants map[string]string) (name string, required bool, defaultValue string) {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)

	required = true // Spring defaults required=true

	if args == "" {
		return
	}

	// Simple: ("name")
	if strings.HasPrefix(args, "\"") && !strings.Contains(args, "=") {
		name = stripJavaQuotes(args)
		return
	}

	for _, part := range strings.Split(args, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "value") || strings.HasPrefix(part, "name") {
			if idx := strings.Index(part, "="); idx >= 0 {
				val := strings.TrimSpace(part[idx+1:])
				stripped := stripJavaQuotes(val)
				if stripped != val {
					name = stripped
				} else if constants != nil {
					name = resolveAnnotationValue(val, constants)
				} else {
					name = val
				}
			}
		}
		if strings.HasPrefix(part, "required") {
			if strings.Contains(part, "false") {
				required = false
			}
		}
		if strings.HasPrefix(part, "defaultValue") {
			if idx := strings.Index(part, "="); idx >= 0 {
				defaultValue = stripJavaQuotes(strings.TrimSpace(part[idx+1:]))
			}
		}
		// Handle unnamed first arg as value
		if !strings.Contains(part, "=") && strings.HasPrefix(part, "\"") {
			name = stripJavaQuotes(part)
		}
	}

	return
}

func extractOpenAPIOperationInfo(args string, source []byte) (summary, description string) {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")

	for _, part := range strings.Split(args, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "summary") {
			if idx := strings.Index(part, "="); idx >= 0 {
				summary = stripJavaQuotes(strings.TrimSpace(part[idx+1:]))
			}
		}
		if strings.HasPrefix(part, "description") {
			if idx := strings.Index(part, "="); idx >= 0 {
				description = stripJavaQuotes(strings.TrimSpace(part[idx+1:]))
			}
		}
	}

	return
}

// javaDocResult holds all structured data extracted from a JavaDoc block.
type javaDocResult struct {
	Summary     string
	Description string
	Params      map[string]string // @param name -> description
	Return      string            // @return description
	Throws      map[string]string // exception class -> description
}

func extractJavaDoc(methodNode *tree_sitter.Node, source []byte) *javaDocResult {
	// Walk backwards through siblings, skipping line_comment and annotation nodes,
	// until we find a block_comment (JavaDoc) or hit a non-comment node.
	prev := methodNode.PrevSibling()
	for prev != nil {
		kind := prev.Kind()
		switch kind {
		case "block_comment", "comment":
			text := prev.Utf8Text(source)
			if strings.HasPrefix(text, "/**") || strings.HasPrefix(text, "/*") {
				return parseJavaDoc(text)
			}
			// Single-line block comment, skip and keep looking
			prev = prev.PrevSibling()
		case "line_comment":
			// Skip line comments between JavaDoc and method
			prev = prev.PrevSibling()
		default:
			// Hit a non-comment node (e.g. another method, field) — stop
			return &javaDocResult{}
		}
	}
	return &javaDocResult{}
}

func parseJavaDoc(comment string) *javaDocResult {
	comment = strings.TrimPrefix(comment, "/**")
	comment = strings.TrimPrefix(comment, "/*")
	comment = strings.TrimSuffix(comment, "*/")

	result := &javaDocResult{
		Params: make(map[string]string),
		Throws: make(map[string]string),
	}

	var descLines []string
	var currentTag, currentTagKey string
	var currentTagLines []string

	flushTag := func() {
		if currentTag == "" {
			return
		}
		text := cleanJavaDoc(strings.Join(currentTagLines, " "))
		switch currentTag {
		case "param":
			if currentTagKey != "" {
				result.Params[currentTagKey] = text
			}
		case "return", "returns":
			result.Return = text
		case "throws", "exception":
			if currentTagKey != "" {
				result.Throws[currentTagKey] = text
			}
		}
		currentTag = ""
		currentTagKey = ""
		currentTagLines = nil
	}

	for _, line := range strings.Split(comment, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)

		if line == "" {
			if currentTag != "" {
				currentTagLines = append(currentTagLines, "")
			}
			continue
		}

		if strings.HasPrefix(line, "@") {
			flushTag()

			parts := strings.Fields(line)
			tag := strings.TrimPrefix(parts[0], "@")

			switch tag {
			case "param":
				currentTag = "param"
				if len(parts) >= 2 {
					currentTagKey = parts[1]
					if len(parts) >= 3 {
						currentTagLines = []string{strings.Join(parts[2:], " ")}
					}
				}
			case "return", "returns":
				currentTag = "return"
				if len(parts) >= 2 {
					currentTagLines = []string{strings.Join(parts[1:], " ")}
				}
			case "throws", "exception":
				currentTag = "throws"
				if len(parts) >= 2 {
					currentTagKey = parts[1]
					if len(parts) >= 3 {
						currentTagLines = []string{strings.Join(parts[2:], " ")}
					}
				}
			}
			continue
		}

		if currentTag != "" {
			currentTagLines = append(currentTagLines, line)
		} else {
			descLines = append(descLines, line)
		}
	}
	flushTag()

	if len(descLines) > 0 {
		result.Summary = cleanJavaDoc(descLines[0])
	}
	if len(descLines) > 1 {
		result.Description = cleanJavaDoc(strings.Join(descLines, " "))
	}

	return result
}

var (
	reLinkTag  = regexp.MustCompile(`\{@link\s+([^}]+)\}`)
	reCodeTag  = regexp.MustCompile(`\{@code\s+([^}]+)\}`)
	reValueTag = regexp.MustCompile(`\{@value\s+([^}]+)\}`)
)

// cleanJavaDoc strips {@link}, {@code}, and {@value} markup from JavaDoc text.
func cleanJavaDoc(text string) string {
	text = reLinkTag.ReplaceAllStringFunc(text, func(m string) string {
		inner := reLinkTag.FindStringSubmatch(m)[1]
		inner = strings.TrimSpace(inner)
		if idx := strings.Index(inner, "#"); idx >= 0 {
			cls := inner[:idx]
			method := inner[idx+1:]
			if parenIdx := strings.Index(method, "("); parenIdx >= 0 {
				method = method[:parenIdx]
			}
			if cls != "" {
				return cls + "." + method
			}
			return method
		}
		return inner
	})
	text = reCodeTag.ReplaceAllString(text, "$1")
	text = reValueTag.ReplaceAllString(text, "$1")
	text = strings.TrimSpace(text)
	return text
}

// exceptionToStatusCode maps Java exception class names to HTTP status codes.
func exceptionToStatusCode(exceptionName string) int {
	lower := strings.ToLower(exceptionName)
	switch {
	case strings.Contains(lower, "notfound"):
		return 404
	case strings.Contains(lower, "badrequest") || strings.Contains(lower, "illegalargument") || strings.Contains(lower, "validation"):
		return 400
	case strings.Contains(lower, "unauthorized") || strings.Contains(lower, "authentication"):
		return 401
	case strings.Contains(lower, "forbidden") || strings.Contains(lower, "accessdenied"):
		return 403
	case strings.Contains(lower, "conflict"):
		return 409
	case strings.Contains(lower, "unsupported"):
		return 415
	case strings.Contains(lower, "toolarge"):
		return 413
	case strings.Contains(lower, "notimplemented"):
		return 501
	case strings.Contains(lower, "unavailable"):
		return 503
	}
	return 500
}

func inferResponseStatus(method, returnType string) int {
	if returnType == "void" || returnType == "" {
		return 204
	}
	if strings.ToUpper(method) == "POST" {
		return 201
	}
	return 200
}

func stripJavaQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// javaTypeFormat maps common Java types to OpenAPI format strings.
func javaTypeFormat(javaType string) string {
	switch javaType {
	case "UUID", "java.util.UUID":
		return "uuid"
	case "LocalDate", "java.time.LocalDate":
		return "date"
	case "LocalDateTime", "java.time.LocalDateTime", "OffsetDateTime", "java.time.OffsetDateTime", "ZonedDateTime", "java.time.ZonedDateTime", "Instant", "java.time.Instant", "Date", "java.util.Date":
		return "date-time"
	case "URI", "java.net.URI", "URL", "java.net.URL":
		return "uri"
	}
	return ""
}

// extractAnnotationField extracts a named field value from annotation args.
// e.g. extractAnnotationField(`min = 1, max = 100`, "min") -> "1"
func extractAnnotationField(args, field string) string {
	re := regexp.MustCompile(field + `\s*=\s*([^,)]+)`)
	m := re.FindStringSubmatch(args)
	if len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// parseIntArg extracts a single integer value from annotation args like "(42)" or "(value = 42)".
func parseIntArg(args string) *int {
	args = strings.TrimSpace(args)
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)
	if v := extractAnnotationField("("+args+")", "value"); v != "" {
		args = v
	}
	args = strings.Trim(args, "\"")
	n, err := strconv.Atoi(args)
	if err != nil {
		return nil
	}
	return &n
}

// parseIntStr parses a string to an int pointer.
func parseIntStr(s string) *int {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &n
}
