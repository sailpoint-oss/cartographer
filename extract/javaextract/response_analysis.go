package javaextract

import (
	"regexp"
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/index"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

var (
	reHeaderConstArg = regexp.MustCompile(`\.header\(\s*([A-Za-z_][A-Za-z0-9_.]*)\s*,`)
	reReturnDelegate = regexp.MustCompile(`return\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
)

// indexClassMethods records method bodies for callee tracing within a class.
func indexClassMethods(classNode *tree_sitter.Node, source []byte, className string, ctx *extractContext) {
	if className == "" || ctx == nil {
		return
	}
	if ctx.methodBodies[className] == nil {
		ctx.methodBodies[className] = make(map[string]string)
	}
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
		child := classBody.Child(i)
		if child.Kind() != "method_declaration" {
			continue
		}
		name := extractMethodName(child, source)
		if name == "" {
			continue
		}
		ctx.methodBodies[className][name] = child.Utf8Text(source)
	}
}

func extractMethodName(methodNode *tree_sitter.Node, source []byte) string {
	for i := uint(0); i < methodNode.ChildCount(); i++ {
		child := methodNode.Child(i)
		if child.Kind() == "identifier" {
			return child.Utf8Text(source)
		}
	}
	return ""
}

// extractResponseHeadersFromText detects response headers set in builder chains.
func extractResponseHeadersFromText(bodyText string, constants map[string]string) map[string]string {
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
	for _, m := range reHeaderConstArg.FindAllStringSubmatch(bodyText, -1) {
		name := resolveHeaderConstantName(m[1], constants)
		if name == "" {
			continue
		}
		if headers == nil {
			headers = make(map[string]string)
		}
		headers[name] = describeResponseHeader(name)
	}
	return headers
}

func resolveHeaderConstantName(raw string, constants map[string]string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"") {
		return strings.Trim(raw, "\"")
	}
	if val, ok := constants[raw]; ok {
		return strings.Trim(val, "\"")
	}
	if dot := strings.LastIndex(raw, "."); dot >= 0 {
		short := raw[dot+1:]
		if val, ok := constants[short]; ok {
			return strings.Trim(val, "\"")
		}
	}
	// Well-known header constant suffixes used across services.
	switch {
	case strings.Contains(raw, "TOTAL_COUNT"):
		return "X-Total-Count"
	case strings.Contains(raw, "POLL_INTERVAL"):
		return "X-Poll-Interval"
	}
	return ""
}

// mergeDelegatedResponseAnalysis follows simple `return helper(...)` calls into
// indexed methods on the current class and its superclasses.
func mergeDelegatedResponseAnalysis(bodyText, className, superClass string, ctx *extractContext, constants map[string]string) (headers map[string]string, statusCode int) {
	headers = extractResponseHeadersFromText(bodyText, constants)
	m := reReturnDelegate.FindStringSubmatch(bodyText)
	if m == nil || ctx == nil {
		return headers, 0
	}
	callee := m[1]
	chain := classInheritanceChain(className, superClass, ctx)
	for _, cn := range chain {
		methods := ctx.methodBodies[cn]
		if methods == nil {
			continue
		}
		calleeBody, ok := methods[callee]
		if !ok {
			continue
		}
		if h := extractResponseHeadersFromText(calleeBody, constants); len(h) > 0 {
			if headers == nil {
				headers = make(map[string]string)
			}
			for k, v := range h {
				headers[k] = v
			}
		}
		if strings.Contains(calleeBody, "Response.noContent()") || strings.Contains(calleeBody, "noContent()") {
			statusCode = 204
		}
		break
	}
	return headers, statusCode
}

func classInheritanceChain(className, superClass string, ctx *extractContext) []string {
	var chain []string
	if className != "" {
		chain = append(chain, className)
	}
	for superClass != "" {
		chain = append(chain, superClass)
		if ctx == nil || ctx.idx == nil {
			break
		}
		decl, ok := ctx.idx.ResolveSimple(superClass)
		if !ok || decl.SuperClass == "" {
			break
		}
		superClass = decl.SuperClass
	}
	return chain
}

// expandSignaturePaginationType expands query parameters from indexed DTO fields
// when the type appears in the method signature.
func expandSignaturePaginationType(typeName string, idx *index.Index, configured map[string]bool) []*Parameter {
	if idx == nil {
		return nil
	}
	decl, ok := idx.ResolveSimple(stripGeneric(typeName))
	if !ok {
		return nil
	}
	if configured != nil && !configured[decl.Name] && !configured[typeName] {
		// Only expand types explicitly configured or with pagination-shaped fields.
		if !looksLikePaginationDTO(decl) {
			return nil
		}
	}
	var params []*Parameter
	for _, f := range decl.Fields {
		if f.Type == "" || f.Name == "" {
			continue
		}
		name := f.Name
		if f.JSONName != "" {
			name = f.JSONName
		}
		params = append(params, &Parameter{
			Name:        name,
			In:          "query",
			Type:        f.Type,
			Required:    f.Required,
			Description: f.Description,
		})
	}
	return params
}

func looksLikePaginationDTO(decl *index.TypeDecl) bool {
	if decl == nil {
		return false
	}
	hits := 0
	for _, f := range decl.Fields {
		switch strings.ToLower(f.Name) {
		case "offset", "limit", "page", "size", "count", "sort", "filters", "sorters":
			hits++
		}
	}
	return hits >= 2
}

func stripGeneric(typeName string) string {
	if i := strings.Index(typeName, "<"); i > 0 {
		return typeName[:i]
	}
	return typeName
}

func extractMethodReturnType(methodNode *tree_sitter.Node, source []byte) string {
	for i := uint(0); i < methodNode.ChildCount(); i++ {
		child := methodNode.Child(i)
		switch child.Kind() {
		case "type_identifier", "generic_type", "array_type", "void_type", "scoped_type_identifier":
			return child.Utf8Text(source)
		}
	}
	return ""
}

func extractGenericFromReturn(returnType string) string {
	returnType = strings.TrimSpace(returnType)
	if strings.HasPrefix(returnType, "ResponseEntity<") && strings.HasSuffix(returnType, ">") {
		inner := returnType[len("ResponseEntity<") : len(returnType)-1]
		return strings.TrimSpace(inner)
	}
	if strings.HasPrefix(returnType, "ResponseEntity<") {
		inner := strings.TrimPrefix(returnType, "ResponseEntity<")
		if idx := strings.Index(inner, ">"); idx >= 0 {
			return strings.TrimSpace(inner[:idx])
		}
	}
	return ""
}
