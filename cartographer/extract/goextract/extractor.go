// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

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
	if strings.HasSuffix(pkg.PkgPath, "/atlas/web") {
		e.errorSchemaAnalyzer.AnalyzeWebPackage(pkg.Syntax, pkg.TypesInfo)
	}

	// Analyze each file in the package
	for i, file := range pkg.Syntax {
		filename := pkg.CompiledGoFiles[i]
		e.metadata.Files = append(e.metadata.Files, filename)

		// Store type info for this file
		e.typeInfo[file] = pkg.TypesInfo

		if err := e.analyzeFile(file, filename, pkg); err != nil {
			return fmt.Errorf("failed to analyze file %s: %w", filename, err)
		}
	}

	return nil
}

// analyzeFile analyzes a single Go source file.
// Uses two-pass processing to ensure handlers are analyzed before routes.
func (e *Extractor) analyzeFile(file *ast.File, filename string, pkg *packages.Package) error {
	// PASS 1: Collect all function declarations and type specs
	// This ensures handler functions are in the cache before we process routes
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.FuncDecl:
			// Analyze function declarations (including handlers)
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

	// PASS 2: Analyze router setup (subrouters, middleware, chi nested routes)
	// This must happen before route registrations so we have context
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			// Look for PathPrefix assignments: sub := router.PathPrefix("/v1").Subrouter()
			e.routerAnalyzer.AnalyzePathPrefix(node, pkg.TypesInfo)

		case *ast.CallExpr:
			// Look for Use() calls: sub.Use(middleware)
			e.routerAnalyzer.AnalyzeUseCall(node, pkg.TypesInfo)

			// Look for chi Route/Group calls: r.Route("/prefix", func(r chi.Router) {...})
			e.routerAnalyzer.AnalyzeChiRoute(node, pkg.TypesInfo)

			// Look for chi Mount calls: r.Mount("/api", apiRouter)
			e.routerAnalyzer.AnalyzeChiMount(node, pkg.TypesInfo)
		}
		return true
	})

	// PASS 3: Analyze route registrations
	// Now that all handlers are cached and subrouter context is known
	ast.Inspect(file, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			// Look for router.Handle() and router.HandleFunc() calls
			e.analyzeRouterCall(call, file, filename, pkg)
		}
		return true
	})

	return nil
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
	if routeInfo != nil {
		if e.config.Verbose {
			fmt.Printf("  Found route: %s %s -> handler: %s, rights: %v\n",
				routeInfo.Method, routeInfo.Path, routeInfo.HandlerName, routeInfo.Rights)
		}

		// Create operation from route info
		op := &OperationInfo{
			ID:           routeInfo.HandlerName,
			Path:         routeInfo.Path,
			Method:       routeInfo.Method,
			Rights:       routeInfo.Rights,
			RequiresAuth: len(routeInfo.Rights) > 0,
			HandlerFunc:  routeInfo.HandlerName,
			File:         filename,
			Line:         e.fset.Position(call.Pos()).Line,
		}

		// Try to merge with handler analysis if available
		if handlerInfo := e.getHandlerInfo(routeInfo.HandlerName); handlerInfo != nil {
			e.mergeHandlerInfo(op, handlerInfo)
			if e.config.Verbose {
				fmt.Printf("  Merged handler info for: %s\n", routeInfo.HandlerName)
			}
		} else if e.config.Verbose {
			fmt.Printf("  No cached handler info for: %s\n", routeInfo.HandlerName)
		}

		// Add to metadata
		if err := e.metadata.AddOperation(op); err != nil {
			// Log but don't fail - duplicate operations might be expected
			fmt.Printf("Warning: %v\n", err)
		}
	}
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
	info     *HandlerInfo
	comments map[string]string
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
