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
	FormParams             []*Parameter
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
	normalizeInlineTSResponseSchemas(result)

	return result, nil
}

// extractOperations walks TypeScript source files looking for NestJS controller classes.
func extractOperations(pool *parser.Pool, idx *index.Index, rootDir string, verbose bool) ([]*Operation, error) {
	var ops []*Operation
	files, err := loadTSSourceFiles(rootDir)
	if err != nil {
		return nil, err
	}
	methodReturnTypes := collectTSMethodReturnTypes(files)

	for _, file := range files {
		tree, err := pool.Parse("typescript", file.Source)
		if err != nil {
			continue
		}

		func() {
			defer tree.Close()
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "WARN: panic extracting operations from %s: %v\n", file.Path, r)
				}
			}()
			fileOps := extractFileOperations(tree.RootNode(), file.Source, file.Path, idx, methodReturnTypes)
			ops = append(ops, fileOps...)
		}()
	}
	return ops, nil
}

type tsSourceFile struct {
	Path   string
	Source []byte
}

func loadTSSourceFiles(rootDir string) ([]tsSourceFile, error) {
	var files []tsSourceFile
	err := filepath.WalkDir(rootDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			base := entry.Name()
			if base == "node_modules" || base == ".git" || base == "dist" || base == "build" || base == "__tests__" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".spec.ts") || strings.HasSuffix(path, ".test.ts") || strings.HasSuffix(path, ".d.ts") {
			return nil
		}
		source, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		files = append(files, tsSourceFile{Path: path, Source: source})
		return nil
	})
	return files, err
}

func collectTSMethodReturnTypes(files []tsSourceFile) map[string]string {
	out := map[string]string{}
	for _, file := range files {
		for name, typ := range extractTSMethodReturnTypes(string(file.Source)) {
			out[name] = typ
		}
	}
	return out
}

func extractTSMethodReturnTypes(source string) map[string]string {
	out := map[string]string{}
	re := regexp.MustCompile(`(?s)(?:public|private|protected)?\s*(?:async\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\([^)]*\)\s*:\s*([^{};]+?)\s*(?:\{|;)`)
	for _, m := range re.FindAllStringSubmatch(source, -1) {
		name := strings.TrimSpace(m[1])
		typ := strings.TrimSpace(m[2])
		if name != "" && typ != "" {
			out[name] = typ
		}
	}
	return out
}

// extractFileOperations extracts operations from a single TypeScript source file.
func extractFileOperations(root *tree_sitter.Node, source []byte, filePath string, idx *index.Index, methodReturnTypes map[string]string) []*Operation {
	var ops []*Operation

	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Kind() {
		case "class_declaration":
			classOps := extractClassOperations(child, nil, source, filePath, idx, methodReturnTypes)
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
				classOps := extractClassOperations(classDecl, exportDecorators, source, filePath, idx, methodReturnTypes)
				ops = append(ops, classOps...)
			}
		}
	}

	return ops
}

// extractClassOperations extracts operations from a class declaration.
// exportDecorators are decorators found on the wrapping export_statement (e.g. @Controller).
func extractClassOperations(classNode *tree_sitter.Node, exportDecorators []*tree_sitter.Node, source []byte, filePath string, idx *index.Index, methodReturnTypes map[string]string) []*Operation {
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
			methodOps := extractMethodOperationWithDecorators(child, pendingDecorators, source, basePath, classTags, classSec, idx, filePath, methodReturnTypes)
			if len(methodOps) > 0 {
				ops = append(ops, methodOps...)
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
		case "Roles", "Scopes", "RequireRight":
			// NestJS RBAC convention: @Roles('admin', 'ops') /
			// @Scopes('read:users'). Every string-literal argument becomes
			// a security requirement (OAuth2 scope).
			classSecurity.RequiresAuth = true
			classSecurity.Security = append(classSecurity.Security, scopeArgs(args)...)
		case "RequireGroup":
			// Group/role membership is an authorization signal but not an
			// OAuth2 scope. Mark auth required, but do not emit group names
			// (often enum references such as SecurityGroups.ORG_ADMIN) as
			// scope tokens: they are not part of the token grant and fail
			// scope-format checks downstream.
			classSecurity.RequiresAuth = true
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
func extractMethodOperationWithDecorators(methodNode *tree_sitter.Node, decorators []*tree_sitter.Node, source []byte, basePath string, classTags []string, classSec classSecurityInfo, idx *index.Index, filePath string, methodReturnTypes map[string]string) []*Operation {
	httpMethod := ""
	methodPaths := []string{""}
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
	hasFileUpload := false

	processDecorator := func(name, args string) {
		switch name {
		case "Get":
			httpMethod = "GET"
			methodPaths = extractRouteDecoratorPaths(args)
		case "Post":
			httpMethod = "POST"
			methodPaths = extractRouteDecoratorPaths(args)
		case "Put":
			httpMethod = "PUT"
			methodPaths = extractRouteDecoratorPaths(args)
		case "Delete":
			httpMethod = "DELETE"
			methodPaths = extractRouteDecoratorPaths(args)
		case "Patch":
			httpMethod = "PATCH"
			methodPaths = extractRouteDecoratorPaths(args)
		case "Head":
			httpMethod = "HEAD"
			methodPaths = extractRouteDecoratorPaths(args)
		case "Options":
			httpMethod = "OPTIONS"
			methodPaths = extractRouteDecoratorPaths(args)
		case "All":
			httpMethod = "GET"
			methodPaths = extractRouteDecoratorPaths(args)
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
		case "Header":
			// NestJS @Header('X-Name', 'value') is a concrete response header.
			if hName := stripTSQuotes(extractFirstArg(args)); hName != "" {
				if responseHeaders == nil {
					responseHeaders = make(map[string]string)
				}
				responseHeaders[hName] = hName
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
		case "Roles", "Scopes", "RequireRight":
			// NestJS RBAC convention: @Roles('admin', 'ops') /
			// @Scopes('read:users'). Every string-literal argument becomes
			// a security requirement (OAuth2 scope).
			requiresAuth = true
			security = append(security, scopeArgs(args)...)
		case "RequireGroup":
			// Group/role membership marks auth required but is not an OAuth2
			// scope; do not emit group names as scope tokens.
			requiresAuth = true
		case "ANONYMOUS", "Anonymous", "Public":
			requiresAuth = false
			security = nil
		case "UseInterceptors":
			if strings.Contains(args, "FileInterceptor") || strings.Contains(args, "FilesInterceptor") {
				hasFileUpload = true
				consumesContentType = "multipart/form-data"
			}
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
	if returnType == "" {
		returnType = inferTSReturnTypeFromMethodBody(methodNode, source, methodReturnTypes)
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

	params = expandDTOQueryParams(params, idx)

	requestBodyType := ""
	var filteredParams []*Parameter
	var formParams []*Parameter
	for _, p := range params {
		if p.In == "body" {
			requestBodyType = p.Type
		} else if p.In == "form" {
			formParams = append(formParams, p)
		} else if p.In != "" && p.In != "skip" {
			filteredParams = append(filteredParams, p)
		}
	}
	if hasFileUpload && len(formParams) == 0 {
		formParams = append(formParams, &Parameter{Name: "file", In: "form", Type: "file", Required: true})
	}

	if responseStatus == 0 {
		responseStatus = inferResponseStatus(httpMethod, returnType)
	}

	// Capture source location from method node
	startPos := methodNode.StartPosition()

	var ops []*Operation
	for i, methodPath := range methodPaths {
		fullPath := buildPath(basePath, methodPath)
		opParams := cloneTSParams(filteredParams)
		opParams = ensurePathParameters(opParams, fullPath)
		opID := methodName
		if i > 0 {
			opID = methodName + "_" + routeOperationIDSuffix(methodPath)
		}
		ops = append(ops, &Operation{
			Path:                   fullPath,
			Method:                 httpMethod,
			OperationID:            opID,
			Summary:                summary,
			Description:            description,
			Tags:                   classTags,
			Parameters:             opParams,
			FormParams:             cloneTSParams(formParams),
			RequestBodyType:        requestBodyType,
			RequestBodyDescription: requestBodyDescription,
			ResponseType:           normalizeTSReturnType(returnType),
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
		})
	}
	return ops
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
	fileUploadParam := false

	for i := uint(0); i < paramNode.ChildCount(); i++ {
		child := paramNode.Child(i)
		switch child.Kind() {
		case "decorator":
			decName, decArgs := extractTSDecorator(child, source)
			in, apiParamName = classifyNestDecorator(decName, decArgs, in, apiParamName)
			if decName == "UploadedFile" || decName == "UploadedFiles" {
				fileUploadParam = true
			}

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
	if fileUploadParam {
		paramType = "file"
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
		if arg := strings.TrimSpace(extractFirstArg(decArgs)); isTSQuoted(arg) {
			apiName = stripTSQuotes(arg)
		}
	case "Body":
		in = "body"
	case "UploadedFile", "UploadedFiles":
		in = "form"
		apiName = stripTSQuotes(extractFirstArg(decArgs))
		if apiName == "" {
			apiName = "file"
		}
	case "Headers":
		in = "header"
		apiName = stripTSQuotes(extractFirstArg(decArgs))
	case "Req", "Res":
		in = "skip" // framework-injected, skip
	}

	return in, apiName
}

func isTSQuoted(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return false
	}
	return (s[0] == '\'' && s[len(s)-1] == '\'') ||
		(s[0] == '"' && s[len(s)-1] == '"') ||
		(s[0] == '`' && s[len(s)-1] == '`')
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

func expandDTOQueryParams(params []*Parameter, idx *index.Index) []*Parameter {
	if idx == nil {
		return params
	}
	var out []*Parameter
	for _, p := range params {
		if p == nil {
			continue
		}
		decl, ok := idx.ResolveSimple(normalizeTSParamType(p.Type))
		if p.In != "query" || !ok || len(decl.Fields) == 0 {
			out = append(out, p)
			continue
		}
		for _, field := range decl.Fields {
			name := field.JSONName
			if name == "" {
				name = field.Name
			}
			out = append(out, &Parameter{
				Name:        name,
				In:          "query",
				Type:        field.Type,
				Required:    field.Required,
				Description: field.Description,
				Example:     field.Example,
				File:        p.File,
				Line:        p.Line,
				Column:      p.Column,
			})
		}
	}
	return out
}

func normalizeTSParamType(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "Readonly<")
	t = strings.TrimSuffix(t, ">")
	return strings.TrimSuffix(t, "[]")
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

	parts := splitTSArgs(args)
	if len(parts) > 0 {
		return parts[0]
	}

	return args
}

func extractRouteDecoratorPaths(args string) []string {
	first := strings.TrimSpace(extractFirstArg(args))
	if first == "" {
		return []string{""}
	}
	if strings.HasPrefix(first, "[") && strings.HasSuffix(first, "]") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(first, "["), "]"))
		var paths []string
		for _, part := range splitTSArgs(inner) {
			path := stripTSQuotes(part)
			if path != "" {
				paths = append(paths, path)
			}
		}
		if len(paths) > 0 {
			return paths
		}
	}
	return []string{stripTSQuotes(first)}
}

func splitTSArgs(args string) []string {
	var out []string
	depth := 0
	start := 0
	quote := rune(0)
	for i, c := range args {
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(args[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(args[start:]))
	return out
}

func cloneTSParams(params []*Parameter) []*Parameter {
	if len(params) == 0 {
		return nil
	}
	out := make([]*Parameter, 0, len(params))
	for _, p := range params {
		if p == nil {
			continue
		}
		cp := *p
		out = append(out, &cp)
	}
	return out
}

func routeOperationIDSuffix(path string) string {
	path = strings.Trim(path, "/")
	if path == "" {
		return "root"
	}
	var b strings.Builder
	lastUnderscore := false
	for _, r := range path {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

func normalizeTSReturnType(returnType string) string {
	returnType = strings.TrimSpace(returnType)
	for {
		trimmed := strings.TrimSpace(returnType)
		trimmed = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "("), ")"))
		switch {
		case strings.HasPrefix(trimmed, "Promise<") && strings.HasSuffix(trimmed, ">"):
			returnType = strings.TrimSuffix(strings.TrimPrefix(trimmed, "Promise<"), ">")
		case strings.HasPrefix(trimmed, "Observable<") && strings.HasSuffix(trimmed, ">"):
			returnType = strings.TrimSuffix(strings.TrimPrefix(trimmed, "Observable<"), ">")
		case strings.Contains(trimmed, "|"):
			parts := strings.Split(trimmed, "|")
			var kept []string
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part == "" || part == "null" || part == "undefined" {
					continue
				}
				kept = append(kept, part)
			}
			if len(kept) == 1 {
				returnType = kept[0]
				continue
			}
			if len(kept) > 1 {
				return strings.Join(kept, " | ")
			}
			return ""
		default:
			return trimmed
		}
	}
}

func inferTSReturnTypeFromMethodBody(methodNode *tree_sitter.Node, source []byte, methodReturnTypes map[string]string) string {
	body := methodNode.Utf8Text(source)
	callName := ""
	for _, pattern := range []string{
		`return\s+await\s+(?:this\.[A-Za-z_][A-Za-z0-9_]*\.|[A-Za-z_][A-Za-z0-9_]*\.)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
		`return\s+(?:this\.[A-Za-z_][A-Za-z0-9_]*\.|[A-Za-z_][A-Za-z0-9_]*\.)?([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
	} {
		if m := regexp.MustCompile(pattern).FindStringSubmatch(body); len(m) == 2 {
			callName = m[1]
			break
		}
	}
	if callName == "" {
		return ""
	}
	if typ := methodReturnTypes[callName]; typ != "" {
		return typ
	}
	signatureRe := regexp.MustCompile(`(?s)\b` + regexp.QuoteMeta(callName) + `\s*\([^)]*\)\s*:\s*([^{};]+?)\s*(?:\{|;)`)
	if m := signatureRe.FindStringSubmatch(string(source)); len(m) == 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

func normalizeInlineTSResponseSchemas(result *Result) {
	if result == nil || result.Schemas == nil {
		return
	}
	for _, op := range result.Operations {
		if op == nil {
			continue
		}
		schema, ok := parseInlineTSObjectSchema(op.ResponseType)
		if !ok {
			continue
		}
		name := exportedSyntheticSchemaName(op.OperationID, "Response")
		result.Schemas[name] = schema
		op.ResponseType = name
	}
}

func parseInlineTSObjectSchema(typeText string) (map[string]any, bool) {
	typeText = strings.TrimSpace(typeText)
	if !strings.HasPrefix(typeText, "{") || !strings.HasSuffix(typeText, "}") {
		return nil, false
	}
	body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(typeText, "{"), "}"))
	if body == "" {
		return map[string]any{"type": "object"}, true
	}
	properties := map[string]any{}
	for _, part := range splitTSObjectFields(body) {
		if part == "" {
			continue
		}
		name, typ, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(strings.TrimSuffix(name, "?"))
		if name == "" {
			continue
		}
		properties[name] = tsTypeToSchema(strings.TrimSpace(typ))
	}
	return map[string]any{"type": "object", "properties": properties}, true
}

func splitTSObjectFields(body string) []string {
	var out []string
	depth := 0
	start := 0
	for i, c := range body {
		switch c {
		case '<', '[', '{', '(':
			depth++
		case '>', ']', '}', ')':
			if depth > 0 {
				depth--
			}
		case ';', ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(body[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(body[start:]))
	return out
}

func tsTypeToSchema(typeText string) map[string]any {
	typeText = strings.TrimSpace(typeText)
	if strings.HasSuffix(typeText, "[]") {
		return map[string]any{"type": "array", "items": tsTypeToSchema(strings.TrimSuffix(typeText, "[]"))}
	}
	switch strings.ToLower(typeText) {
	case "string":
		return map[string]any{"type": "string"}
	case "number":
		return map[string]any{"type": "number"}
	case "boolean":
		return map[string]any{"type": "boolean"}
	case "void", "undefined", "null":
		return map[string]any{"nullable": true}
	default:
		return map[string]any{"$ref": "#/components/schemas/" + typeText}
	}
}

func exportedSyntheticSchemaName(operationID, suffix string) string {
	base := routeOperationIDSuffix(operationID)
	if base == "" {
		base = "operation"
	}
	parts := strings.Split(base, "_")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "") + suffix
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

// scopeArgs returns only the quoted string-literal arguments from a decorator's
// argument list. Bare identifiers and member expressions (for example an enum
// reference such as SecurityGroups.ORG_ADMIN) are not literal scope tokens and
// are dropped so they never leak into the OAuth2 scope list and trip
// scope-format checks.
func scopeArgs(args string) []string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)
	if args == "" {
		return nil
	}
	var results []string
	depth := 0
	start := 0
	flush := func(seg string) {
		seg = strings.TrimSpace(seg)
		if isTSStringLiteral(seg) {
			if v := stripTSQuotes(seg); v != "" {
				results = append(results, v)
			}
		}
	}
	for i, c := range args {
		switch c {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		case ',':
			if depth == 0 {
				flush(args[start:i])
				start = i + 1
			}
		}
	}
	flush(args[start:])
	return results
}

// isTSStringLiteral reports whether s is a single-, double-, or back-quoted
// string literal.
func isTSStringLiteral(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return false
	}
	switch s[0] {
	case '\'', '"', '`':
		return s[len(s)-1] == s[0]
	}
	return false
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
