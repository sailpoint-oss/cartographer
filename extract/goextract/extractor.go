// Copyright (c) 2020-2026. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Extractor performs static analysis on Go packages to extract OpenAPI metadata.
type Extractor struct {
	metadata *ExtractedMetadata
	fset     *token.FileSet
	pkgs     []*packages.Package
	typeInfo map[*ast.File]*types.Info
	config   Config // Store config for use in analysis methods

	// Pattern matchers
	routerAnalyzer       *RouterAnalyzer
	handlerAnalyzer      *HandlerAnalyzer
	commentParser        *CommentParser
	responseRegistry     *ResponseRegistry
	functionTracer       *FunctionTracer
	errorSchemaAnalyzer  *ErrorSchemaAnalyzer
	schemaNameNormalizer *SchemaNameNormalizer

	// Handler info cache - maps function key to cached handler info and comments
	handlerInfoCache map[string]*cachedHandlerInfo
}

// Config holds configuration for the extractor.
type Config struct {
	// PackagePatterns are the Go package patterns to analyze (e.g., "./...")
	PackagePatterns []string

	// Verbose enables detailed logging
	Verbose bool

	// IncludeTests whether to analyze test files
	IncludeTests bool
}

// New creates a new Extractor.
func New(cfg Config) *Extractor {
	// Initialize components in order
	responseRegistry := NewResponseRegistry()
	functionTracer := NewFunctionTracer(responseRegistry)
	handlerAnalyzer := NewHandlerAnalyzer(functionTracer)
	errorSchemaAnalyzer := NewErrorSchemaAnalyzer()
	schemaNameNormalizer := NewSchemaNameNormalizer()

	return &Extractor{
		metadata:             NewExtractedMetadata(),
		fset:                 token.NewFileSet(),
		typeInfo:             make(map[*ast.File]*types.Info),
		handlerInfoCache:     make(map[string]*cachedHandlerInfo),
		routerAnalyzer:       NewRouterAnalyzer(),
		handlerAnalyzer:      handlerAnalyzer,
		commentParser:        NewCommentParser(),
		responseRegistry:     responseRegistry,
		functionTracer:       functionTracer,
		errorSchemaAnalyzer:  errorSchemaAnalyzer,
		schemaNameNormalizer: schemaNameNormalizer,
		config:               cfg,
	}
}

// Extract performs the extraction process and returns the metadata.
func (e *Extractor) Extract(cfg Config) (*ExtractedMetadata, error) {
	// Store config for use in analysis methods
	e.config = cfg

	// Load packages with type information
	if err := e.loadPackages(cfg); err != nil {
		return nil, fmt.Errorf("failed to load packages: %w", err)
	}

	// Build function declaration cache for the tracer
	// This enables unlimited recursive tracing
	e.buildFunctionCache()

	// Analyze all loaded packages
	for _, pkg := range e.pkgs {
		if err := e.analyzePackage(pkg); err != nil {
			return nil, fmt.Errorf("failed to analyze package %s: %w", pkg.PkgPath, err)
		}
	}

	// Validate the extracted metadata
	if err := e.metadata.Validate(); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	return e.metadata, nil
}

// loadPackages loads the specified Go packages with type information.
func (e *Extractor) loadPackages(cfg Config) error {
	mode := packages.NeedName |
		packages.NeedFiles |
		packages.NeedCompiledGoFiles |
		packages.NeedImports |
		packages.NeedDeps |
		packages.NeedTypes |
		packages.NeedSyntax |
		packages.NeedTypesInfo

	if !cfg.IncludeTests {
		mode |= packages.NeedModule
	}

	// Get current working directory as default
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	patterns := cfg.PackagePatterns
	if len(patterns) == 0 {
		patterns = []string{"."}
	}

	// Determine the load directory and resolve patterns
	// For absolute paths pointing to external modules, we need to:
	// 1. Find the module root (directory containing go.mod)
	// 2. Set Dir to that module root
	// 3. Convert patterns to be relative to that root
	loadDir := cwd
	resolvedPatterns := make([]string, len(patterns))

	for i, pattern := range patterns {
		// Check if pattern ends with /... or ... (recursive pattern)
		hasEllipsis := strings.HasSuffix(pattern, "/...") || strings.HasSuffix(pattern, "...")
		basePath := pattern
		if hasEllipsis {
			basePath = strings.TrimSuffix(basePath, "/...")
			basePath = strings.TrimSuffix(basePath, "...")
		}

		// Check if this is an absolute path (pointing to an external module)
		if filepath.IsAbs(basePath) {
			// Find the module root for this path
			moduleRoot, err := findModuleRoot(basePath)
			if err != nil {
				if cfg.Verbose {
					fmt.Printf("Warning: could not find module root for %s: %v\n", basePath, err)
				}
				// Fall back to using the path as-is
				resolvedPatterns[i] = pattern
				continue
			}

			// Use the module root as the load directory
			// (all patterns should be from the same module for this to work correctly)
			loadDir = moduleRoot

			// Convert the pattern to be relative to the module root
			relPath, err := filepath.Rel(moduleRoot, basePath)
			if err != nil {
				return fmt.Errorf("failed to make path relative: %w", err)
			}

			// Ensure it starts with ./
			if !strings.HasPrefix(relPath, ".") {
				relPath = "./" + relPath
			}

			// Add back the /... suffix if it was present
			if hasEllipsis {
				relPath = relPath + "/..."
			}

			resolvedPatterns[i] = relPath
			if cfg.Verbose {
				fmt.Printf("Resolved absolute path: %s -> %s (module root: %s)\n", pattern, relPath, moduleRoot)
			}
		} else if isRelativePath(basePath) {
			// Convert relative path to absolute path based on current working directory
			absPath, err := filepath.Abs(basePath)
			if err != nil {
				return fmt.Errorf("failed to resolve path %s: %w", pattern, err)
			}

			// Find module root and set loadDir, same as the absolute-path branch
			moduleRoot, err := findModuleRoot(absPath)
			if err == nil {
				loadDir = moduleRoot

				relPath, err := filepath.Rel(moduleRoot, absPath)
				if err != nil {
					return fmt.Errorf("failed to make path relative: %w", err)
				}
				if !strings.HasPrefix(relPath, ".") {
					relPath = "./" + relPath
				}
				if hasEllipsis {
					relPath = relPath + "/..."
				}
				resolvedPatterns[i] = relPath
				if cfg.Verbose {
					fmt.Printf("Resolved relative path: %s -> %s (module root: %s)\n", pattern, relPath, moduleRoot)
				}
			} else {
				if cfg.Verbose {
					fmt.Printf("Warning: could not find module root for %s: %v\n", absPath, err)
				}
				// Fall back to absolute path pattern
				if hasEllipsis {
					absPath = absPath + "/..."
				}
				resolvedPatterns[i] = absPath
				if cfg.Verbose {
					fmt.Printf("Resolved relative path (no module root): %s -> %s\n", pattern, absPath)
				}
			}
		} else {
			// Module path or other pattern - use as-is
			resolvedPatterns[i] = pattern
		}
	}

	loadCfg := &packages.Config{
		Mode:  mode,
		Fset:  e.fset,
		Tests: cfg.IncludeTests,
		Dir:   loadDir, // Set to module root for external packages
		// Load the target service strictly in its own module context.
		// Cartographer is frequently invoked by a host tool (the orchestration
		// pipeline, an IDE, the dev inner loop) that itself runs under a Go
		// workspace (go.work). That workspace must not leak into package
		// loading for an unrelated external service module: go/packages would
		// reject the service directory as "not one of the workspace modules"
		// and return zero packages, silently dropping every route. Disabling
		// workspace mode forces resolution via the service's own go.mod.
		Env: append(os.Environ(), "GOWORK=off"),
	}

	if cfg.Verbose {
		fmt.Printf("Loading packages from directory: %s\n", loadDir)
		fmt.Printf("Package patterns: %v\n", resolvedPatterns)
	}

	pkgs, err := packages.Load(loadCfg, resolvedPatterns...)
	if err != nil {
		return err
	}

	// Check for errors in loaded packages
	for _, pkg := range pkgs {
		if len(pkg.Errors) > 0 {
			for _, e := range pkg.Errors {
				if cfg.Verbose {
					fmt.Printf("Package error in %s: %v\n", pkg.PkgPath, e)
				}
			}
		}
	}

	e.pkgs = pkgs
	return nil
}

// isRelativePath checks if a path is a relative path.
// This helps distinguish between relative file paths and module paths.
// A path is considered relative if:
// - It starts with ./ or ../
// - It is . or ..
// - It doesn't start with / (not an absolute Unix path)
// - It doesn't match a module path pattern (contains /)
// This covers cases like "internal", "internal/api", etc. which are relative paths
// that have been normalized by filepath.Join
func isRelativePath(path string) bool {
	// Explicit relative paths
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../") {
		return true
	}
	if path == "." || path == ".." {
		return true
	}

	// Absolute Unix path
	if strings.HasPrefix(path, "/") {
		return false
	}

	// Windows absolute path (C:, D:, etc.)
	if len(path) >= 2 && path[1] == ':' {
		return false
	}

	// Module paths typically have a domain or github in them
	// e.g., "github.com/user/repo", "example.com/module"
	if strings.Contains(path, ".") && strings.Contains(path, "/") {
		// Looks like a module path
		return false
	}

	// Everything else is treated as a relative path
	// This includes: "internal", "internal/api", "pkg/something", etc.
	return true
}

// findModuleRoot finds the Go module root directory (containing go.mod) for the given path.
// It walks up the directory tree starting from absPath until it finds a go.mod file.
func findModuleRoot(absPath string) (string, error) {
	// Clean the path and ensure it's a directory
	dir := absPath
	info, err := os.Stat(dir)
	if err != nil {
		// If the path doesn't exist (might be a pattern), try the parent
		dir = filepath.Dir(dir)
	} else if !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	// Walk up the directory tree looking for go.mod
	for {
		goModPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goModPath); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the root without finding go.mod
			return "", fmt.Errorf("no go.mod found in path hierarchy: %s", absPath)
		}
		dir = parent
	}
}

// analyzePackage analyzes a single package.
func (e *Extractor) analyzePackage(pkg *packages.Package) error {
	e.metadata.Package = pkg.PkgPath

	// Special handling for web package - analyze error constructors
	if isFrameworkWebPackage(pkg.PkgPath) {
		e.errorSchemaAnalyzer.AnalyzeWebPackage(pkg.Syntax, pkg.TypesInfo)
	}

	// PASS 1 (package-wide): collect handler/type declarations from EVERY file
	// before any route is registered. Routes are frequently registered in a
	// router/web_handlers file while their handler implementations live in a
	// separate *_handlers file; per-file passing dropped that cross-file
	// handler response/auth info, leaving routes with only platform error
	// responses and no typed 2xx.
	for i, file := range pkg.Syntax {
		filename := pkg.CompiledGoFiles[i]
		e.metadata.Files = append(e.metadata.Files, filename)
		e.typeInfo[file] = pkg.TypesInfo
		e.collectDeclarations(file, filename, pkg)
	}

	// PASS 2 (package-wide): router setup context (subrouter prefixes, mounts,
	// Use middleware) so registrations can resolve their effective path/auth.
	for _, file := range pkg.Syntax {
		e.collectRouterContext(file, pkg)
	}

	// PASS 3 (package-wide): route registrations, now that all handlers are
	// cached and subrouter context is known.
	for i, file := range pkg.Syntax {
		filename := pkg.CompiledGoFiles[i]
		e.registerRoutesInFile(file, filename, pkg)
	}

	return nil
}

// collectDeclarations is PASS 1: it analyses every function declaration
// (caching handler request/response/auth info by name) and type spec in a file.
func (e *Extractor) collectDeclarations(file *ast.File, filename string, pkg *packages.Package) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			e.analyzeFuncDecl(node, file, filename, pkg)

		case *ast.GenDecl:
			if node.Tok == token.TYPE {
				for _, spec := range node.Specs {
					if ts, ok := spec.(*ast.TypeSpec); ok {
						e.analyzeTypeSpec(ts, node, file, filename, pkg)
					}
				}
				return false
			}
		}
		return true
	})
}

// collectRouterContext is PASS 2: it records subrouter prefixes, mounts, and
// Use-applied middleware so route registrations resolve their effective path
// and inherited auth.
func (e *Extractor) collectRouterContext(file *ast.File, pkg *packages.Package) {
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			e.routerAnalyzer.AnalyzePathPrefix(node, pkg.TypesInfo)

		case *ast.CallExpr:
			e.routerAnalyzer.AnalyzeUseCall(node, pkg.TypesInfo)
			e.routerAnalyzer.AnalyzeChiRoute(node, pkg.TypesInfo)
			e.routerAnalyzer.AnalyzeChiMount(node, pkg.TypesInfo)
		}
		return true
	})
}

// registerRoutesInFile is PASS 3: it registers route handlers, associating each
// with the (now fully populated, package-wide) handler info cache. The mux
// Path/Method/Handler form used by RestEndpoint.BuildRoutes implementations is
// detected by the file-wide call inspection below.
func (e *Extractor) registerRoutesInFile(file *ast.File, filename string, pkg *packages.Package) {
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			e.analyzeRouterCall(call, file, filename, pkg)
			if routeInfo := e.routerAnalyzer.AnalyzeMuxPathMethodHandler(call, file, pkg.TypesInfo, e.fset); routeInfo != nil {
				e.registerRoute(routeInfo, filename, call.Pos())
			}
		}
		return true
	})
}

// analyzeFuncDecl analyzes a function declaration.
func (e *Extractor) analyzeFuncDecl(funcDecl *ast.FuncDecl, file *ast.File, filename string, pkg *packages.Package) {
	if funcDecl.Body == nil {
		return
	}

	// Extract comments above the function
	comments := e.commentParser.ParseFuncComments(funcDecl, file)
	loc := ""
	if pkg != nil && pkg.Fset != nil {
		pos := pkg.Fset.Position(funcDecl.Pos())
		if pos.Filename != "" && pos.Line > 0 {
			loc = fmt.Sprintf("%s:%d", pos.Filename, pos.Line)
		}
	}

	// Analyze the function body for handler patterns
	handlerInfo := e.handlerAnalyzer.AnalyzeHandler(funcDecl, file, pkg.TypesInfo)

	// If this looks like a handler function, store the information
	if handlerInfo != nil {
		// Store for later association with routes
		e.storeHandlerInfo(funcDecl.Name.Name, handlerInfo, comments, loc)
	} else if len(comments) > 0 {
		// Even if not detected as a handler, if it has @openapi annotations,
		// store it anyway for potential matching with routes
		e.storeHandlerInfo(funcDecl.Name.Name, nil, comments, loc)
	}
}

// analyzeRouterCall analyzes calls to router registration functions.
func (e *Extractor) analyzeRouterCall(call *ast.CallExpr, file *ast.File, filename string, pkg *packages.Package) {
	routeInfo := e.routerAnalyzer.AnalyzeRouterCall(call, file, pkg.TypesInfo, e.fset)
	if routeInfo == nil {
		return
	}
	e.registerRoute(routeInfo, filename, call.Pos())
}

func (e *Extractor) registerRoute(routeInfo *RouteInfo, filename string, pos token.Pos) {
	if e.config.Verbose {
		fmt.Printf("  Found route: %s %s -> handler: %s, rights: %v\n",
			routeInfo.Method, routeInfo.Path, routeInfo.HandlerName, routeInfo.Rights)
	}

	op := &OperationInfo{
		ID:           routeInfo.HandlerName,
		Path:         routeInfo.Path,
		Method:       routeInfo.Method,
		Rights:       routeInfo.Rights,
		RequiresAuth: len(routeInfo.Rights) > 0,
		HandlerFunc:  routeInfo.HandlerName,
		File:         filename,
		Line:         e.fset.Position(pos).Line,
	}
	if existing, exists := e.metadata.Operations[op.ID]; exists {
		if existing.Path == op.Path && existing.Method == op.Method {
			if e.config.Verbose {
				fmt.Printf("  Skipping duplicate route: %s %s -> handler: %s\n",
					op.Method, op.Path, op.HandlerFunc)
			}
			return
		}
		op.ID = e.uniqueRouteOperationID(op.ID, op.Method, op.Path)
	}

	if handlerInfo := e.getHandlerInfo(routeInfo.HandlerName); handlerInfo != nil {
		e.mergeHandlerInfo(op, handlerInfo)
		if e.config.Verbose {
			fmt.Printf("  Merged handler info for: %s\n", routeInfo.HandlerName)
		}
	} else if e.config.Verbose {
		fmt.Printf("  No cached handler info for: %s\n", routeInfo.HandlerName)
	}

	if err := e.metadata.AddOperation(op); err != nil {
		fmt.Printf("Warning: %v\n", err)
	}
}

func (e *Extractor) uniqueRouteOperationID(base, method, path string) string {
	if base == "" {
		base = strings.ToLower(method)
	}
	candidate := base
	if method != "" || path != "" {
		candidate = fmt.Sprintf("%s_%s_%s", base, strings.ToLower(method), routeIDPathSuffix(path))
	}
	if candidate == base {
		candidate = base + "_route"
	}
	if _, exists := e.metadata.Operations[candidate]; !exists {
		return candidate
	}
	for i := 2; ; i++ {
		numbered := fmt.Sprintf("%s_%d", candidate, i)
		if _, exists := e.metadata.Operations[numbered]; !exists {
			return numbered
		}
	}
}

func routeIDPathSuffix(path string) string {
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

// analyzeTypeSpec extracts type information from type specifications.
func (e *Extractor) analyzeTypeSpec(typeSpec *ast.TypeSpec, genDecl *ast.GenDecl, file *ast.File, filename string, pkg *packages.Package) {
	obj := pkg.TypesInfo.Defs[typeSpec.Name]
	if obj == nil {
		return
	}

	typeName := obj.Type().String()

	if structType, ok := typeSpec.Type.(*ast.StructType); ok {
		typeInfo := e.extractStructInfo(typeSpec.Name.Name, structType, pkg, filename)
		typeInfo.FullName = typeName

		// Capture type-level godoc: prefer TypeSpec.Doc (grouped declarations),
		// fall back to GenDecl.Doc for standalone type declarations.
		if typeSpec.Doc != nil {
			typeInfo.Description = strings.TrimSpace(typeSpec.Doc.Text())
		} else if genDecl != nil && genDecl.Doc != nil && len(genDecl.Specs) == 1 {
			typeInfo.Description = strings.TrimSpace(genDecl.Doc.Text())
		}

		e.metadata.AddType(typeInfo)
	}
}

// extractStructInfo extracts detailed information about a struct type.
func (e *Extractor) extractStructInfo(name string, structType *ast.StructType, pkg *packages.Package, filename string) *TypeInfo {
	ti := &TypeInfo{
		Package: pkg.PkgPath,
		Name:    name,
		Kind:    "struct",
		Fields:  make([]FieldInfo, 0),
		File:    filename,
		Line:    pkg.Fset.Position(structType.Pos()).Line,
	}

	// Extract fields
	if structType.Fields != nil {
		for _, field := range structType.Fields.List {
			for _, fieldName := range field.Names {
				fieldInfo := e.extractFieldInfo(fieldName, field, pkg.TypesInfo)
				ti.Fields = append(ti.Fields, fieldInfo)
			}
		}
	}

	return ti
}

// extractFieldInfo extracts information about a struct field.
func (e *Extractor) extractFieldInfo(name *ast.Ident, field *ast.Field, info *types.Info) FieldInfo {
	fi := FieldInfo{
		Name: name.Name,
		Tags: make(map[string]string),
	}

	// Get type information
	if field.Type != nil {
		if t := info.TypeOf(field.Type); t != nil {
			fi.Type = TypeString(t)
		}
	}

	// Parse struct tags
	if field.Tag != nil {
		fi.Tags = parseStructTags(field.Tag.Value)

		// Extract common tag values
		if jsonTag, ok := fi.Tags["json"]; ok {
			fi.JSONName = jsonTag
		}
		if desc, ok := fi.Tags["description"]; ok {
			fi.Description = desc
		}
		if example, ok := fi.Tags["example"]; ok {
			fi.Example = example
		}
	}

	return fi
}

// cachedHandlerInfo stores handler information before routes are matched.
type cachedHandlerInfo struct {
	info           *HandlerInfo
	comments       map[string]string
	sourceLocation string
}

// storeHandlerInfo stores handler information in the instance cache.
func (e *Extractor) storeHandlerInfo(name string, info *HandlerInfo, comments map[string]string, sourceLocation string) {
	e.handlerInfoCache[name] = &cachedHandlerInfo{
		info:           info,
		comments:       comments,
		sourceLocation: sourceLocation,
	}
}

// getHandlerInfo retrieves handler information from the instance cache.
func (e *Extractor) getHandlerInfo(name string) *cachedHandlerInfo {
	return e.handlerInfoCache[name]
}

// ClearCache clears the handler info cache. Useful for testing.
func (e *Extractor) ClearCache() {
	e.handlerInfoCache = make(map[string]*cachedHandlerInfo)
}

func (e *Extractor) mergeHandlerInfo(op *OperationInfo, cached *cachedHandlerInfo) {
	if cached == nil {
		return
	}

	info := cached.info
	if info != nil {
		if info.RequestType != "" {
			op.RequestType = info.RequestType
		}
		if info.ResponseType != "" {
			op.ResponseType = info.ResponseType
			op.ResponseStatus = info.ResponseStatus
		}
		if info.ContentType != "" {
			op.ResponseContent = info.ContentType
		}
		if len(info.ErrorCodes) > 0 {
			op.PossibleErrors = info.ErrorCodes
		}

		// Merge detailed response information (NEW - exhaustive extraction)
		if len(info.ErrorResponses) > 0 {
			op.ErrorResponses = append(op.ErrorResponses, info.ErrorResponses...)
		}
		if len(info.SuccessResponses) > 0 {
			op.SuccessResponses = append(op.SuccessResponses, info.SuccessResponses...)
		}
		if len(info.ResponseHeaders) > 0 {
			if op.ResponseHeaders == nil {
				op.ResponseHeaders = make(map[string]string)
			}
			for name, desc := range info.ResponseHeaders {
				op.ResponseHeaders[name] = desc
			}
		}

		// Merge auth tokens discovered via call-graph traversal from the
		// handler body. Router-side middleware rights are already on
		// op.Rights at this point; we union with the body-side tokens.
		if len(info.AuthTokens) > 0 {
			seen := make(map[string]struct{}, len(op.Rights)+len(info.AuthTokens))
			for _, r := range op.Rights {
				seen[r] = struct{}{}
			}
			for _, t := range info.AuthTokens {
				if _, dup := seen[t]; dup {
					continue
				}
				seen[t] = struct{}{}
				op.Rights = append(op.Rights, t)
			}
			if len(op.Rights) > 0 {
				op.RequiresAuth = true
			}
		}

		// Merge detailed parameter information
		for _, p := range info.PathParams {
			op.PathParamDetails = append(op.PathParamDetails, OperationParamInfo{
				Name:         p.Name,
				Type:         p.Type,
				Required:     p.Required,
				DefaultValue: p.DefaultValue,
			})
		}
		for _, p := range info.QueryParams {
			op.QueryParamDetails = append(op.QueryParamDetails, OperationParamInfo{
				Name:         p.Name,
				Type:         p.Type,
				Required:     p.Required,
				DefaultValue: p.DefaultValue,
			})
		}
		for _, p := range info.HeaderParams {
			op.HeaderParamDetails = append(op.HeaderParamDetails, OperationParamInfo{
				Name:         p.Name,
				Type:         p.Type,
				Required:     p.Required,
				DefaultValue: p.DefaultValue,
			})
		}
		for _, p := range info.FormParams {
			op.FormParamDetails = append(op.FormParamDetails, OperationParamInfo{
				Name:         p.Name,
				Type:         p.Type,
				Required:     p.Required,
				DefaultValue: p.DefaultValue,
			})
		}
	}

	// Merge comment annotations
	if cached.comments != nil {
		e.commentParser.ApplyAnnotationsWithSource(op, cached.comments, cached.sourceLocation)
	}
}

// buildFunctionCache builds a cache of all function declarations for the function tracer.
// This enables unlimited recursive tracing of function calls.
func (e *Extractor) buildFunctionCache() {
	if e.functionTracer == nil {
		return
	}

	// Collect all AST files from all packages
	var allFiles []*ast.File
	for _, pkg := range e.pkgs {
		allFiles = append(allFiles, pkg.Syntax...)
	}

	// Build the cache
	e.functionTracer.BuildFunctionCache(nil, allFiles)

	if e.config.Verbose {
		fmt.Printf("Built function declaration cache with %d functions\n", len(e.functionTracer.funcDeclMap))
	}
}

// parseStructTags parses a struct tag string into a map.
// Uses reflect.StructTag for proper parsing of Go struct tags.
func parseStructTags(tagString string) map[string]string {
	tags := make(map[string]string)

	// Remove backticks if present
	if len(tagString) >= 2 && tagString[0] == '`' && tagString[len(tagString)-1] == '`' {
		tagString = tagString[1 : len(tagString)-1]
	}

	// Use reflect.StructTag for proper parsing
	structTag := reflect.StructTag(tagString)

	// Extract common tag keys
	commonTags := []string{"json", "xml", "yaml", "description", "example", "validate", "binding", "form", "query", "uri", "header"}
	for _, key := range commonTags {
		if value, ok := structTag.Lookup(key); ok {
			// For json tag, extract just the field name (before comma options)
			if key == "json" {
				if commaIdx := strings.Index(value, ","); commaIdx != -1 {
					value = value[:commaIdx]
				}
				// Skip if the field is ignored
				if value == "-" {
					continue
				}
			}
			tags[key] = value
		}
	}

	return tags
}

// GetEnhancedErrorSchema returns the enhanced error schema with const values from web.newError analysis.
func (e *Extractor) GetEnhancedErrorSchema() map[string]interface{} {
	return e.errorSchemaAnalyzer.BuildEnhancedErrorSpec()
}

// GetSchemaNameNormalizer returns the schema name normalizer for use in OpenAPI generation.
func (e *Extractor) GetSchemaNameNormalizer() *SchemaNameNormalizer {
	return e.schemaNameNormalizer
}

// GetErrorSchemaAnalyzer returns the error schema analyzer for use in OpenAPI generation.
func (e *Extractor) GetErrorSchemaAnalyzer() *ErrorSchemaAnalyzer {
	return e.errorSchemaAnalyzer
}
