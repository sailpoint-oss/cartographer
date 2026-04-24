// Package tsextract provides tree-sitter based NestJS/TypeScript OpenAPI extraction.
// Supports @Controller, @Get/@Post/@Put/@Delete/@Patch decorators, @Param, @Body, @Query,
// and class-validator decorators for DTO validation.
// No Node.js runtime required.
package tsextract

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
)

// Config holds TypeScript extraction configuration.
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
}

// ApiResponseInfo represents a response declared via @ApiResponse decorator.
type ApiResponseInfo struct {
	Status      int
	Description string
	Type        string
}

// Operation represents an extracted API endpoint.
type Operation struct {
	Path                   string
	Method                 string // GET, POST, PUT, DELETE, PATCH, etc.
	OperationID            string
	Summary                string
	Description            string
	Tags                   []string
	Parameters             []*Parameter
	RequestBodyType        string
	RequestBodyDescription string // from @ApiBody({ description })
	ResponseType           string
	ResponseStatus         int
	Deprecated             bool
	DeprecatedSince        string            // from @ApiProperty({ deprecated: 'since' })
	Security               []string          // scopes/roles
	RequiresAuth           bool              // from @UseGuards
	ApiResponses           []ApiResponseInfo // from @ApiResponse decorators
	ResponseHeaders        map[string]string // header name -> description
	NullableResponse       bool              // from nullable return type
	ReturnDescription      string            // from @ApiOkResponse description
	RateLimited            bool              // from @Throttle decorator
	ConsumesContentType    string            // from @ApiConsumes
	ProducesContentType    string            // from @ApiProduces
	ErrorResponses         map[int]string    // status code -> description
	File                   string            // source file path
	Line                   int               // 1-based line number
	Column                 int               // 1-based column number
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

// Extract performs tree-sitter based TypeScript/NestJS extraction.
func Extract(cfg Config) (*Result, error) {
	pool := parser.NewPool()
	if err := pool.RegisterTypeScript(); err != nil {
		return nil, fmt.Errorf("register typescript grammar: %w", err)
	}

	idx := index.New()
	scanner := index.NewScanner(pool, idx, "typescript")

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

	result := &Result{
		Schemas: make(map[string]interface{}),
		Types:   idx.All(),
	}

	for _, dir := range dirs {
		ops, err := extractOperations(pool, idx, dir, cfg.Verbose)
		if err != nil {
			return nil, err
		}
		result.Operations = append(result.Operations, ops...)
	}

	// Convert indexed types to schemas
	for _, decl := range idx.All() {
		result.Schemas[decl.Name] = idx.ToOpenAPISchema(decl, nil)
	}

	return result, nil
}

// extractOperations walks TypeScript source files looking for NestJS controller classes.
func extractOperations(pool *parser.Pool, idx *index.Index, rootDir string, verbose bool) ([]*Operation, error) {
	var ops []*Operation

	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := info.Name()
			if base == "node_modules" || base == ".git" || base == "dist" || base == "build" || base == "__tests__" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".spec.ts") || strings.HasSuffix(path, ".test.ts") || strings.HasSuffix(path, ".d.ts") {
			return nil
		}

		source, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		tree, err := pool.Parse("typescript", source)
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
			fileOps := extractFileOperations(tree.RootNode(), source, path, idx)
			ops = append(ops, fileOps...)
		}()

		return nil
	})

	return ops, err
}

// extractFileOperations extracts operations from a single TypeScript source file.
func extractFileOperations(root *tree_sitter.Node, source []byte, filePath string, idx *index.Index) []*Operation {
	var ops []*Operation

	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Kind() {
		case "class_declaration":
			classOps := extractClassOperations(child, nil, source, filePath, idx)
			ops = append(ops, classOps...)
		case "export_statement":
			// In TypeScript, @Controller decorator is on the export_statement,
			// while class_declaration is a child. We need to collect decorators
			// from the export_statement and pass them to class processing.
			var exportDecorators []*tree_sitter.Node
			var classDecl *tree_sitter.Node
			for j := uint(0); j < child.ChildCount(); j++ {
				inner := child.Child(j)
				if inner.Kind() == "decorator" {
					exportDecorators = append(exportDecorators, inner)
				}
				if inner.Kind() == "class_declaration" {
					classDecl = inner
				}
			}
			if classDecl != nil {
				classOps := extractClassOperations(classDecl, exportDecorators, source, filePath, idx)
				ops = append(ops, classOps...)
			}
		}
	}

	return ops
}

// extractClassOperations extracts operations from a class declaration.
// exportDecorators are decorators found on the wrapping export_statement (e.g. @Controller).
func extractClassOperations(classNode *tree_sitter.Node, exportDecorators []*tree_sitter.Node, source []byte, filePath string, idx *index.Index) []*Operation {
	isController, basePath, classTags, classSec := analyzeClassDecorators(classNode, exportDecorators, source)
	if !isController {
		return nil
	}

	var ops []*Operation

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

	var pendingDecorators []*tree_sitter.Node
	for i := uint(0); i < classBody.ChildCount(); i++ {
		child := classBody.Child(i)
		switch child.Kind() {
		case "decorator":
			pendingDecorators = append(pendingDecorators, child)
		case "method_definition":
			op := extractMethodOperationWithDecorators(child, pendingDecorators, source, basePath, classTags, classSec, idx, filePath)
			if op != nil {
				ops = append(ops, op)
			}
			pendingDecorators = nil
		default:
			if child.Kind() != ";" && child.Kind() != "," {
				pendingDecorators = nil
			}
		}
	}

	return ops
}

// classSecurityInfo holds class-level security metadata inherited by all operations.
type classSecurityInfo struct {
	RequiresAuth bool
	Security     []string
}

// analyzeClassDecorators determines if this is a NestJS controller and extracts metadata.
// exportDecorators are decorators from the wrapping export_statement.
func analyzeClassDecorators(classNode *tree_sitter.Node, exportDecorators []*tree_sitter.Node, source []byte) (isController bool, basePath string, tags []string, classSecurity classSecurityInfo) {
	className := ""

	processDecorator := func(name, args string) {
		switch name {
		case "Controller":
			isController = true
			basePath = stripTSQuotes(extractFirstArg(args))
		case "ApiTags":
			tags = append(tags, extractAllArgs(args)...)
		case "UseGuards":
			classSecurity.RequiresAuth = true
		case "ApiBearerAuth":
			classSecurity.RequiresAuth = true
			scheme := stripTSQuotes(extractFirstArg(args))
			if scheme == "" {
				scheme = "bearerAuth"
			}
			classSecurity.Security = append(classSecurity.Security, scheme)
		case "ApiSecurity":
			classSecurity.RequiresAuth = true
			sec := parseApiSecurityDecorator(args)
			classSecurity.Security = append(classSecurity.Security, sec...)
		case "ApiOAuth2":
			classSecurity.RequiresAuth = true
			scopes := parseApiOAuth2Decorator(args)
			classSecurity.Security = append(classSecurity.Security, scopes...)
		}
	}

	for _, dec := range exportDecorators {
		name, args := extractTSDecorator(dec, source)
		processDecorator(name, args)
	}

	for i := uint(0); i < classNode.ChildCount(); i++ {
		child := classNode.Child(i)
		switch child.Kind() {
		case "decorator":
			name, args := extractTSDecorator(child, source)
			processDecorator(name, args)
		case "type_identifier":
			className = child.Utf8Text(source)
		}
	}

	if isController && len(tags) == 0 && className != "" {
		tag := strings.TrimSuffix(className, "Controller")
		if tag != "" {
			tags = append(tags, tag)
		}
	}

	return
}

// extractMethodOperationWithDecorators extracts an operation from a method_definition node,
// using the pre-collected sibling decorators from class_body.
func extractMethodOperationWithDecorators(methodNode *tree_sitter.Node, decorators []*tree_sitter.Node, source []byte, basePath string, classTags []string, classSec classSecurityInfo, idx *index.Index, filePath string) *Operation {
	httpMethod := ""
	methodPath := ""
	methodName := ""
	returnType := ""
	responseStatus := 0
	deprecated := false
	apiOpSummary := ""
	apiOpDescription := ""
	requiresAuth := classSec.RequiresAuth
	var security []string
	security = append(security, classSec.Security...)
	var apiResponses []ApiResponseInfo
	requestBodyDescription := ""
	nullableResponse := false
	rateLimited := false
	deprecatedSince := ""
	var responseHeaders map[string]string
	var errorResponses map[int]string
	consumesContentType := ""
	producesContentType := ""
	var paramOverrides []paramOverride
	var params []*Parameter

	processDecorator := func(name, args string) {
		switch name {
		case "Get":
			httpMethod = "GET"
			methodPath = stripTSQuotes(extractFirstArg(args))
		case "Post":
			httpMethod = "POST"
			methodPath = stripTSQuotes(extractFirstArg(args))
		case "Put":
			httpMethod = "PUT"
			methodPath = stripTSQuotes(extractFirstArg(args))
		case "Delete":
			httpMethod = "DELETE"
			methodPath = stripTSQuotes(extractFirstArg(args))
		case "Patch":
			httpMethod = "PATCH"
			methodPath = stripTSQuotes(extractFirstArg(args))
		case "Head":
			httpMethod = "HEAD"
			methodPath = stripTSQuotes(extractFirstArg(args))
		case "Options":
			httpMethod = "OPTIONS"
			methodPath = stripTSQuotes(extractFirstArg(args))
		case "All":
			httpMethod = "GET"
			methodPath = stripTSQuotes(extractFirstArg(args))
		case "HttpCode":
			if code := extractFirstArg(args); code != "" {
				if v := parseIntSafe(code); v > 0 {
					responseStatus = v
				}
			}
		case "Deprecated":
			deprecated = true
			if since := extractTSObjectField(args, "since"); since != "" {
				deprecatedSince = since
			}
		case "ApiOperation":
			props := extractDecoratorProperties(args)
			if v := props["summary"]; v != "" {
				apiOpSummary = v
			}
			if v := props["description"]; v != "" {
				apiOpDescription = v
			}
		case "ApiResponse":
			resp := parseApiResponseDecorator(args)
			if resp.Status > 0 || resp.Description != "" {
				apiResponses = append(apiResponses, resp)
			}
		case "ApiParam":
			po := parseApiParamDecorator(args, "path")
			paramOverrides = append(paramOverrides, po)
		case "ApiQuery":
			po := parseApiParamDecorator(args, "query")
			paramOverrides = append(paramOverrides, po)
		case "ApiBody":
			po := parseApiParamDecorator(args, "body")
			paramOverrides = append(paramOverrides, po)
			if desc := extractTSObjectField(args, "description"); desc != "" {
				requestBodyDescription = desc
			}
		case "Throttle":
			rateLimited = true
		case "ApiConsumes":
			consumesContentType = stripTSQuotes(extractFirstArg(args))
		case "ApiProduces":
			producesContentType = stripTSQuotes(extractFirstArg(args))
		case "ApiHeader":
			headerProps := extractDecoratorProperties(args)
			if hName := headerProps["name"]; hName != "" {
				if responseHeaders == nil {
					responseHeaders = make(map[string]string)
				}
				desc := headerProps["description"]
				if desc == "" {
					desc = hName
				}
				responseHeaders[hName] = desc
			}
		case "UseGuards":
			requiresAuth = true
		case "ApiBearerAuth":
			requiresAuth = true
			scheme := stripTSQuotes(extractFirstArg(args))
			if scheme == "" {
				scheme = "bearerAuth"
			}
			security = append(security, scheme)
		case "ApiSecurity":
			requiresAuth = true
			security = append(security, parseApiSecurityDecorator(args)...)
		case "ApiOAuth2":
			requiresAuth = true
			security = append(security, parseApiOAuth2Decorator(args)...)
		}
	}

	for _, dec := range decorators {
		name, args := extractTSDecorator(dec, source)
		processDecorator(name, args)
	}

	for i := uint(0); i < methodNode.ChildCount(); i++ {
		child := methodNode.Child(i)
		switch child.Kind() {
		case "decorator":
			name, args := extractTSDecorator(child, source)
			processDecorator(name, args)

		case "property_identifier":
			methodName = child.Utf8Text(source)

		case "formal_parameters":
			params = extractMethodParameters(child, source, idx)

		case "type_annotation":
			for j := uint(0); j < child.ChildCount(); j++ {
				n := child.Child(j)
				if n.Kind() != ":" {
					returnType = n.Utf8Text(source)
				}
			}
		}
	}

	jsdoc := extractJSDocStructured(methodNode, source)

	if httpMethod == "" {
		return nil
	}

	// Priority: @ApiOperation > JSDoc > camelCase fallback
	summary := apiOpSummary
	description := apiOpDescription
	if summary == "" {
		summary = jsdoc.Summary
	}
	if description == "" {
		description = jsdoc.Description
	}

	// Apply JSDoc param descriptions as fallback
	for _, p := range params {
		if p.Description == "" {
			if d, ok := jsdoc.Params[p.Name]; ok {
				p.Description = d
			}
		}
	}

	// Apply decorator param overrides (@ApiParam, @ApiQuery, @ApiBody)
	for _, po := range paramOverrides {
		applyParamOverride(params, po)
	}

	fullPath := buildPath(basePath, methodPath)
	params = ensurePathParameters(params, fullPath)

	requestBodyType := ""
	var filteredParams []*Parameter
	for _, p := range params {
		if p.In == "body" {
			requestBodyType = p.Type
		} else if p.In != "" && p.In != "skip" {
			filteredParams = append(filteredParams, p)
		}
	}

	if responseStatus == 0 {
		responseStatus = inferResponseStatus(httpMethod, returnType)
	}

	// Capture source location from method node
	startPos := methodNode.StartPosition()

	return &Operation{
		Path:                   fullPath,
		Method:                 httpMethod,
		OperationID:            methodName,
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
		RequiresAuth:           requiresAuth,
		ApiResponses:           apiResponses,
		ResponseHeaders:        responseHeaders,
		NullableResponse:       nullableResponse,
		RateLimited:            rateLimited,
		ConsumesContentType:    consumesContentType,
		ProducesContentType:    producesContentType,
		ErrorResponses:         errorResponses,
		File:                   filePath,
		Line:                   int(startPos.Row) + 1,
		Column:                 int(startPos.Column) + 1,
	}
}

// extractMethodParameters extracts parameters from formal_parameters.
func extractMethodParameters(paramsNode *tree_sitter.Node, source []byte, idx *index.Index) []*Parameter {
	var params []*Parameter

	for i := uint(0); i < paramsNode.ChildCount(); i++ {
		child := paramsNode.Child(i)
		// NestJS parameters have decorators on each formal parameter
		// In tree-sitter, they appear as required_parameter or optional_parameter
		switch child.Kind() {
		case "required_parameter", "optional_parameter":
			param := extractSingleParameter(child, source, idx)
			if param != nil {
				params = append(params, param)
			}
		}
	}

	return params
}

// extractSingleParameter extracts a single parameter from a required_parameter/optional_parameter node.
func extractSingleParameter(paramNode *tree_sitter.Node, source []byte, idx *index.Index) *Parameter {
	paramName := ""
	paramType := ""
	in := ""
	apiParamName := ""
	required := paramNode.Kind() == "required_parameter"
	format := ""
	description := ""
	example := ""
	var minimum, maximum, minLength, maxLength *int

	for i := uint(0); i < paramNode.ChildCount(); i++ {
		child := paramNode.Child(i)
		switch child.Kind() {
		case "decorator":
			decName, decArgs := extractTSDecorator(child, source)
			in, apiParamName = classifyNestDecorator(decName, decArgs, in, apiParamName)

			// class-validator decorators
			switch decName {
			case "IsUUID":
				format = "uuid"
			case "IsEmail":
				format = "email"
			case "IsUrl", "IsURL":
				format = "uri"
			case "IsDateString", "IsISO8601":
				format = "date-time"
			case "MinLength":
				if n := parseTSIntArg(decArgs); n != nil {
					minLength = n
				}
			case "MaxLength":
				if n := parseTSIntArg(decArgs); n != nil {
					maxLength = n
				}
			case "Min":
				if n := parseTSIntArg(decArgs); n != nil {
					minimum = n
				}
			case "Max":
				if n := parseTSIntArg(decArgs); n != nil {
					maximum = n
				}
			case "IsNotEmpty", "IsDefined":
				required = true
			case "ApiParam":
				if desc := extractTSObjectField(decArgs, "description"); desc != "" {
					description = desc
				}
				if ex := extractTSObjectField(decArgs, "example"); ex != "" {
					example = ex
				}
			}

		case "identifier":
			paramName = child.Utf8Text(source)

		case "type_annotation":
			for j := uint(0); j < child.ChildCount(); j++ {
				n := child.Child(j)
				if n.Kind() != ":" {
					paramType = n.Utf8Text(source)
				}
			}
		}
	}

	if paramName == "" {
		return nil
	}

	if isNestInfraType(paramType) {
		return nil
	}
	if apiParamName != "" {
		paramName = apiParamName
	}
	if in == "" {
		in = inferParameterLocation(paramType)
	}

	return &Parameter{
		Name:        paramName,
		In:          in,
		Type:        paramType,
		Required:    required || in == "path",
		Format:      format,
		Description: description,
		Example:     example,
		Minimum:     minimum,
		Maximum:     maximum,
		MinLength:   minLength,
		MaxLength:   maxLength,
	}
}

// classifyNestDecorator determines parameter location from NestJS decorator.
func classifyNestDecorator(decName, decArgs, currentIn, currentApiName string) (string, string) {
	in := currentIn
	apiName := currentApiName

	switch decName {
	case "Param":
		in = "path"
		apiName = stripTSQuotes(extractFirstArg(decArgs))
	case "Query":
		in = "query"
		apiName = stripTSQuotes(extractFirstArg(decArgs))
	case "Body":
		in = "body"
	case "Headers":
		in = "header"
		apiName = stripTSQuotes(extractFirstArg(decArgs))
	case "Req", "Res":
		in = "skip" // framework-injected, skip
	}

	return in, apiName
}

// isNestInfraType checks if a type is a NestJS/Express infrastructure type.
func isNestInfraType(typeName string) bool {
	infraTypes := map[string]bool{
		"Request":  true,
		"Response": true,
		"any":      true,
	}
	return infraTypes[typeName]
}

// inferParameterLocation infers parameter location from its type.
func inferParameterLocation(typeName string) string {
	switch strings.ToLower(typeName) {
	case "string", "number", "boolean", "int", "float":
		return "query"
	}
	return "body"
}

// extractTSDecorator extracts decorator name and arguments from a decorator node.
func extractTSDecorator(node *tree_sitter.Node, source []byte) (string, string) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "call_expression" {
			funcName := ""
			args := ""
			for j := uint(0); j < child.ChildCount(); j++ {
				n := child.Child(j)
				switch n.Kind() {
				case "identifier":
					funcName = n.Utf8Text(source)
				case "arguments":
					args = n.Utf8Text(source)
				}
			}
			return funcName, args
		}
		if child.Kind() == "identifier" {
			return child.Utf8Text(source), ""
		}
	}
	return "", ""
}

// extractFirstArg extracts the first argument from an arguments string "(arg1, arg2)".
func extractFirstArg(args string) string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)

	if args == "" {
		return ""
	}

	// Handle first argument only (before first comma, respecting nested parens)
	depth := 0
	for i, c := range args {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				return strings.TrimSpace(args[:i])
			}
		}
	}

	return args
}

func stripTSQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '`' && s[len(s)-1] == '`') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

func buildPath(basePath, methodPath string) string {
	// Ensure leading slash on base
	if basePath != "" && !strings.HasPrefix(basePath, "/") {
		basePath = "/" + basePath
	}

	// Ensure leading slash on method path if present
	if methodPath != "" && !strings.HasPrefix(methodPath, "/") {
		methodPath = "/" + methodPath
	}

	fullPath := basePath + methodPath
	if fullPath == "" {
		fullPath = "/"
	}

	// Normalize double slashes
	for strings.Contains(fullPath, "//") {
		fullPath = strings.ReplaceAll(fullPath, "//", "/")
	}

	// Convert NestJS :param to OpenAPI {param}
	parts := strings.Split(fullPath, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "{" + strings.TrimPrefix(part, ":") + "}"
		}
	}
	fullPath = strings.Join(parts, "/")

	return fullPath
}

func inferResponseStatus(method, returnType string) int {
	if returnType == "void" || returnType == "" {
		if method == "DELETE" {
			return 204
		}
		return 204
	}
	if method == "POST" {
		return 201
	}
	return 200
}

// jsDocResult holds structured JSDoc parse results.
type jsDocResult struct {
	Summary     string
	Description string
	Params      map[string]string // param name -> description
	Returns     string
}

func extractJSDocStructured(methodNode *tree_sitter.Node, source []byte) jsDocResult {
	// Walk backwards through siblings to find the JSDoc comment,
	// skipping over any decorator nodes that sit between the comment and method.
	node := methodNode
	for node.PrevSibling() != nil {
		prev := node.PrevSibling()
		if prev.Kind() == "comment" {
			return parseJSDocStructured(prev.Utf8Text(source))
		}
		if prev.Kind() == "decorator" {
			node = prev
			continue
		}
		break
	}
	return jsDocResult{}
}

var jsDocParamRe = regexp.MustCompile(`^@param\s+(?:\{[^}]*\}\s+)?(\w+)\s*[-–]?\s*(.*)`)
var jsDocReturnsRe = regexp.MustCompile(`^@returns?\s+(?:\{[^}]*\}\s+)?[-–]?\s*(.*)`)

func parseJSDocStructured(comment string) jsDocResult {
	comment = strings.TrimPrefix(comment, "/**")
	comment = strings.TrimPrefix(comment, "/*")
	comment = strings.TrimSuffix(comment, "*/")

	result := jsDocResult{Params: make(map[string]string)}
	var descLines []string

	for _, line := range strings.Split(comment, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if m := jsDocParamRe.FindStringSubmatch(line); m != nil {
			result.Params[m[1]] = strings.TrimSpace(m[2])
			continue
		}
		if m := jsDocReturnsRe.FindStringSubmatch(line); m != nil {
			result.Returns = strings.TrimSpace(m[1])
			continue
		}
		if strings.HasPrefix(line, "@") {
			continue
		}
		descLines = append(descLines, line)
	}

	if len(descLines) > 0 {
		result.Summary = descLines[0]
	}
	if len(descLines) > 1 {
		result.Description = strings.Join(descLines, " ")
	}

	return result
}

func parseIntSafe(s string) int {
	s = strings.TrimSpace(s)
	val := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			val = val*10 + int(c-'0')
		} else {
			break
		}
	}
	return val
}

// extractAllArgs extracts all comma-separated, quoted arguments from an args string.
// e.g. "('Tag1', 'Tag2')" -> ["Tag1", "Tag2"]
func extractAllArgs(args string) []string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)
	if args == "" {
		return nil
	}

	var results []string
	depth := 0
	start := 0
	for i, c := range args {
		switch c {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		case ',':
			if depth == 0 {
				v := stripTSQuotes(strings.TrimSpace(args[start:i]))
				if v != "" {
					results = append(results, v)
				}
				start = i + 1
			}
		}
	}
	v := stripTSQuotes(strings.TrimSpace(args[start:]))
	if v != "" {
		results = append(results, v)
	}
	return results
}

// extractDecoratorProperties parses object-style decorator arguments like
// "({ summary: 'foo', description: 'bar' })" into a key->value map.
func extractDecoratorProperties(args string) map[string]string {
	result := make(map[string]string)
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)
	args = strings.TrimPrefix(args, "{")
	args = strings.TrimSuffix(args, "}")
	args = strings.TrimSpace(args)

	if args == "" {
		return result
	}

	// Split on commas respecting nesting
	pairs := splitRespectingNesting(args)
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		idx := strings.Index(pair, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(pair[:idx])
		val := strings.TrimSpace(pair[idx+1:])
		result[key] = stripTSQuotes(val)
	}
	return result
}

func splitRespectingNesting(s string) []string {
	var parts []string
	depth := 0
	start := 0
	inString := rune(0)

	for i, c := range s {
		if inString != 0 {
			if c == inString {
				inString = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			inString = c
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

// parseApiResponseDecorator parses @ApiResponse({ status: 200, description: '...' })
func parseApiResponseDecorator(args string) ApiResponseInfo {
	props := extractDecoratorProperties(args)
	resp := ApiResponseInfo{
		Description: props["description"],
		Type:        props["type"],
	}
	if s := props["status"]; s != "" {
		resp.Status = parseIntSafe(s)
	}
	return resp
}

// paramOverride holds overrides from @ApiParam/@ApiQuery/@ApiBody decorators.
type paramOverride struct {
	Name        string
	In          string
	Description string
	Required    string // "true"/"false" or empty
	Example     string
}

func parseApiParamDecorator(args, location string) paramOverride {
	props := extractDecoratorProperties(args)
	return paramOverride{
		Name:        props["name"],
		In:          location,
		Description: props["description"],
		Required:    props["required"],
		Example:     props["example"],
	}
}

func applyParamOverride(params []*Parameter, po paramOverride) {
	if po.Name == "" {
		return
	}
	for _, p := range params {
		if p.Name == po.Name && (po.In == "" || p.In == po.In) {
			if po.Description != "" {
				p.Description = po.Description
			}
			if po.Required == "true" {
				p.Required = true
			} else if po.Required == "false" {
				p.Required = false
			}
			return
		}
	}
}

func ensurePathParameters(params []*Parameter, fullPath string) []*Parameter {
	seen := make(map[string]bool)
	for _, p := range params {
		if p.In == "path" && p.Name != "" {
			seen[p.Name] = true
		}
	}
	for _, name := range extractPathTemplateParams(fullPath) {
		if seen[name] {
			continue
		}
		params = append(params, &Parameter{
			Name:     name,
			In:       "path",
			Type:     "string",
			Required: true,
		})
		seen[name] = true
	}
	return params
}

func extractPathTemplateParams(path string) []string {
	parts := strings.Split(path, "/")
	out := make([]string, 0)
	for _, part := range parts {
		if len(part) >= 3 && strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			out = append(out, strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}"))
		}
	}
	return out
}

func parseApiSecurityDecorator(args string) []string {
	allArgs := extractAllArgs(args)
	if len(allArgs) == 0 {
		return []string{"oauth2"}
	}
	return allArgs
}

func parseApiOAuth2Decorator(args string) []string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)
	if args == "" {
		return []string{"oauth2"}
	}
	// Expect array arg: ['read', 'write']
	args = strings.TrimPrefix(args, "[")
	args = strings.TrimSuffix(args, "]")
	args = strings.TrimSpace(args)
	if args == "" {
		return []string{"oauth2"}
	}
	scopes := extractAllArgs("(" + args + ")")
	if len(scopes) == 0 {
		return []string{"oauth2"}
	}
	return scopes
}

// parseTSIntArg extracts a single integer from decorator args like "(10)" or "10".
func parseTSIntArg(args string) *int {
	args = strings.TrimSpace(args)
	args = strings.Trim(args, "()")
	args = strings.TrimSpace(args)
	// Take first arg if comma-separated
	if idx := strings.Index(args, ","); idx >= 0 {
		args = args[:idx]
	}
	args = strings.Trim(args, "'\"")
	n, err := strconv.Atoi(args)
	if err != nil {
		return nil
	}
	return &n
}

// extractTSObjectField extracts a field from a TypeScript object literal.
// e.g. extractTSObjectField(`{ description: "foo", example: "bar" }`, "description") -> "foo"
func extractTSObjectField(args, field string) string {
	re := regexp.MustCompile(field + `\s*:\s*['"]([^'"]+)['"]`)
	m := re.FindStringSubmatch(args)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}
