// Package csharpextract provides Go-native C# OpenAPI extraction.
//
// It intentionally avoids a Roslyn/.NET runtime dependency. Tree-sitter gives
// us a real syntax layer, while this package builds the project/type index and
// endpoint model Cartographer needs for first-class service extraction.
package csharpextract

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/sailpoint-oss/cartographer/extract/index"
	"github.com/sailpoint-oss/cartographer/extract/parser"
)

type Config struct {
	RootDir    string
	SourceDirs []string
	Verbose    bool
}

type Result struct {
	Operations []*Operation
	Schemas    map[string]interface{}
	Types      map[string]*index.TypeDecl
}

type Operation struct {
	Path            string
	Method          string
	OperationID     string
	Summary         string
	Description     string
	Tags            []string
	Parameters      []*Parameter
	RequestBodyType string
	ResponseType    string
	ResponseStatus  int
	ResponseHeaders map[string]string
	Security        []string
	File            string
	Line            int
	Column          int
}

type Parameter struct {
	Name     string
	In       string
	Type     string
	Required bool
	File     string
	Line     int
	Column   int
}

type sourceFile struct {
	path string
	data []byte
	text string
}

func Extract(cfg Config) (*Result, error) {
	pool := parser.NewPool()
	if err := pool.RegisterCSharp(); err != nil {
		return nil, fmt.Errorf("register csharp grammar: %w", err)
	}
	dirs := cfg.SourceDirs
	if len(dirs) == 0 {
		dirs = []string{cfg.RootDir}
	}
	files, err := readCSharpFiles(pool, dirs)
	if err != nil {
		return nil, err
	}
	result := &Result{
		Schemas: map[string]interface{}{},
		Types:   map[string]*index.TypeDecl{},
	}
	for _, f := range files {
		for name, schema := range extractClassSchemas(f) {
			result.Schemas[name] = schema
		}
	}
	methodReturnTypes := extractCSharpMethodReturnTypes(files)
	for _, f := range files {
		result.Operations = append(result.Operations, extractMinimalAPIOperations(f, methodReturnTypes)...)
		result.Operations = append(result.Operations, extractControllerOperations(f)...)
	}
	sort.SliceStable(result.Operations, func(i, j int) bool {
		if result.Operations[i].Path != result.Operations[j].Path {
			return result.Operations[i].Path < result.Operations[j].Path
		}
		return result.Operations[i].Method < result.Operations[j].Method
	})
	return result, nil
}

func readCSharpFiles(pool *parser.Pool, dirs []string) ([]sourceFile, error) {
	var out []sourceFile
	for _, dir := range dirs {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				switch info.Name() {
				case ".git", "bin", "obj", "node_modules":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".cs") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			tree, err := pool.Parse("csharp", data)
			if err != nil {
				return fmt.Errorf("parse %s: %w", path, err)
			}
			tree.Close()
			out = append(out, sourceFile{path: path, data: data, text: string(data)})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func extractMinimalAPIOperations(f sourceFile, methodReturnTypes map[string]string) []*Operation {
	groupPrefixes := map[string]string{"app": ""}
	groupSecurity := map[string][]string{"app": nil}
	constants := extractStringConstants(f.text)
	groupRe := regexp.MustCompile(`(?m)(?:var\s+)?(\w+)\s*=\s*(\w+)\.MapGroup\(\s*"([^"]*)"\s*\)`)
	for _, loc := range groupRe.FindAllStringSubmatchIndex(f.text, -1) {
		m := groupRe.FindStringSubmatch(f.text[loc[0]:loc[1]])
		parent := groupPrefixes[m[2]]
		groupPrefixes[m[1]] = joinPaths(parent, m[3])
		chain := endpointChain(f.text, loc[0])
		groupSecurity[m[1]] = append(append([]string{}, groupSecurity[m[2]]...), authorizeMetadata(chain, constants)...)
	}
	endpointRe := regexp.MustCompile(`(?s)(\w+)\.Map(Get|Post|Put|Delete|Patch)\(\s*"([^"]*)"\s*,\s*(?:async\s*)?\(([^)]*)\)\s*=>`)
	var ops []*Operation
	for _, loc := range endpointRe.FindAllStringSubmatchIndex(f.text, -1) {
		m := endpointRe.FindStringSubmatch(f.text[loc[0]:loc[1]])
		receiver, method, route, params := m[1], strings.ToUpper(m[2]), m[3], m[4]
		prefix := groupPrefixes[receiver]
		path := normalizeRoute(joinPaths(prefix, route))
		line, col := lineCol(f.text, loc[0])
		chain := endpointChain(f.text, loc[0])
		if strings.Contains(chain, ".ExcludeFromDescription(") {
			continue
		}
		bodySegment := f.text[loc[1]:min(len(f.text), loc[1]+600)]
		op := &Operation{
			Path:        path,
			Method:      method,
			OperationID: operationID(method, path),
			Summary:     summaryBefore(f.text, loc[0]),
			Tags:        []string{tagFromPath(path)},
			File:        f.path,
			Line:        line,
			Column:      col,
		}
		op.Parameters, op.RequestBodyType = parseMinimalParams(params, path, f.path, line)
		op.ResponseType = responseTypeFromProduces(chain)
		if op.ResponseType == "" {
			op.ResponseType = inferMinimalResponseType(bodySegment, methodReturnTypes)
		}
		op.ResponseStatus = minimalResponseStatus(chain+"\n"+bodySegment, method, op.ResponseType)
		op.ResponseHeaders = extractCSharpResponseHeaders(chain + "\n" + bodySegment)
		if name := endpointName(chain); name != "" {
			op.OperationID = name
		}
		op.Security = append(append([]string{}, groupSecurity[receiver]...), chainedMetadata(chain, constants)...)
		ops = append(ops, op)
	}

	methodGroupRe := regexp.MustCompile(`(?s)(\w+)\.Map(Get|Post|Put|Delete|Patch)\(\s*"([^"]*)"\s*,\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)`)
	for _, loc := range methodGroupRe.FindAllStringSubmatchIndex(f.text, -1) {
		m := methodGroupRe.FindStringSubmatch(f.text[loc[0]:loc[1]])
		receiver, method, route, handlerName := m[1], strings.ToUpper(m[2]), m[3], m[4]
		prefix := groupPrefixes[receiver]
		path := normalizeRoute(joinPaths(prefix, route))
		line, col := lineCol(f.text, loc[0])
		chain := endpointChain(f.text, loc[0])
		if strings.Contains(chain, ".ExcludeFromDescription(") {
			continue
		}
		returnType, params, _ := minimalHandlerSignature(f.text, handlerName)
		handlerBody := minimalHandlerBody(f.text, handlerName)
		op := &Operation{
			Path:        path,
			Method:      method,
			OperationID: handlerName,
			Summary:     summaryBefore(f.text, loc[0]),
			Tags:        []string{tagFromPath(path)},
			File:        f.path,
			Line:        line,
			Column:      col,
		}
		op.Parameters, op.RequestBodyType = parseMinimalParams(params, path, f.path, line)
		op.ResponseType = responseTypeFromProduces(chain)
		if op.ResponseType == "" {
			op.ResponseType = unwrapResponseType(returnType)
		}
		op.ResponseStatus = minimalResponseStatus(chain, method, op.ResponseType)
		op.ResponseHeaders = extractCSharpResponseHeaders(chain + "\n" + handlerBody)
		if name := endpointName(chain); name != "" {
			op.OperationID = name
		}
		op.Security = append(append([]string{}, groupSecurity[receiver]...), chainedMetadata(chain, constants)...)
		ops = append(ops, op)
	}
	return ops
}

var (
	reCSharpHeaderIndex  = regexp.MustCompile(`(?m)(?:Response|response|httpContext\.Response|context\.Response)\.Headers\[\s*"([^"]+)"\s*\]`)
	reCSharpHeaderAppend = regexp.MustCompile(`(?m)(?:Response|response|httpContext\.Response|context\.Response)\.Headers\.(?:Append|Add)\(\s*"([^"]+)"`)
)

func extractCSharpResponseHeaders(text string) map[string]string {
	headers := map[string]string{}
	for _, m := range reCSharpHeaderIndex.FindAllStringSubmatch(text, -1) {
		addCSharpResponseHeader(headers, m[1])
	}
	for _, m := range reCSharpHeaderAppend.FindAllStringSubmatch(text, -1) {
		addCSharpResponseHeader(headers, m[1])
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}

func addCSharpResponseHeader(headers map[string]string, name string) {
	name = strings.TrimSpace(name)
	if name == "" || strings.EqualFold(name, "Content-Type") {
		return
	}
	headers[name] = name
}

func extractCSharpMethodReturnTypes(files []sourceFile) map[string]string {
	out := map[string]string{}
	re := regexp.MustCompile(`(?s)(?:public|private|internal|protected)?\s*(?:static\s+)?(?:async\s+)?([A-Za-z0-9_<>,\.\?\[\]\s]+?)\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	for _, f := range files {
		for _, m := range re.FindAllStringSubmatch(f.text, -1) {
			returnType := cleanType(m[1])
			name := strings.TrimSpace(m[2])
			if returnType == "" || name == "" || isCSharpKeyword(returnType) {
				continue
			}
			out[name] = unwrapResponseType(returnType)
		}
	}
	return out
}

func isCSharpKeyword(s string) bool {
	switch strings.TrimSpace(s) {
	case "if", "for", "foreach", "while", "switch", "catch", "using", "return", "new":
		return true
	default:
		return false
	}
}

func extractControllerOperations(f sourceFile) []*Operation {
	constants := extractStringConstants(f.text)
	classRe := regexp.MustCompile(`(?s)((?:\s*\[[^\]]+\]\s*)*)public\s+(?:partial\s+)?class\s+(\w+)[^{]*\{`)
	matches := classRe.FindAllStringSubmatchIndex(f.text, -1)
	var ops []*Operation
	for i, loc := range matches {
		attrs := f.text[loc[2]:loc[3]]
		className := f.text[loc[4]:loc[5]]
		bodyStart := loc[1]
		bodyEnd := len(f.text)
		if i+1 < len(matches) {
			bodyEnd = matches[i+1][0]
		}
		basePath := controllerBasePath(attrs, className)
		if basePath == "" && !strings.Contains(attrs, "ApiController") {
			continue
		}
		body := f.text[bodyStart:bodyEnd]
		methodRe := regexp.MustCompile(`(?s)((?:\s*\[[^\]]+\]\s*)*)public\s+(?:async\s+)?(?:Task<)?([A-Za-z0-9_<>,\s]+)>?\s+(\w+)\s*\(([^)]*)\)`)
		for _, ml := range methodRe.FindAllStringSubmatchIndex(body, -1) {
			methodAttrs := body[ml[2]:ml[3]]
			httpMethod, route := httpAttribute(methodAttrs)
			if httpMethod == "" {
				continue
			}
			returnType := cleanType(body[ml[4]:ml[5]])
			methodName := body[ml[6]:ml[7]]
			params := body[ml[8]:ml[9]]
			offset := bodyStart + ml[0]
			line, col := lineCol(f.text, offset)
			path := normalizeRoute(joinPaths(basePath, route))
			op := &Operation{
				Path:           path,
				Method:         httpMethod,
				OperationID:    methodName,
				Summary:        summaryBefore(f.text, offset),
				Tags:           []string{tagFromController(className)},
				ResponseType:   unwrapResponseType(returnType),
				ResponseStatus: responseStatus(methodAttrs, httpMethod, returnType),
				Security:       authorizeMetadata(methodAttrs+"\n"+attrs, constants),
				File:           f.path,
				Line:           line,
				Column:         col,
			}
			op.Parameters, op.RequestBodyType = parseControllerParams(params, path, f.path, line)
			ops = append(ops, op)
		}
	}
	return ops
}

func extractClassSchemas(f sourceFile) map[string]interface{} {
	out := map[string]interface{}{}
	classRe := regexp.MustCompile(`(?s)public\s+(?:record|class)\s+(\w+)[^{;(]*(?:\(([^)]*)\))?\s*\{([^}]*)\}`)
	for _, m := range classRe.FindAllStringSubmatch(f.text, -1) {
		name := m[1]
		props := map[string]interface{}{}
		for _, p := range parseProperties(m[2] + "\n" + m[3]) {
			propSchema := typeSchema(p.Type)
			if p.MaxLen != nil {
				propSchema["maxLength"] = *p.MaxLen
			}
			props[p.Name] = propSchema
		}
		if len(props) > 0 {
			schema := map[string]interface{}{"type": "object", "properties": props}
			var required []interface{}
			for _, p := range parseProperties(m[2] + "\n" + m[3]) {
				if p.Required {
					required = append(required, p.Name)
				}
			}
			if len(required) > 0 {
				schema["required"] = required
			}
			out[name] = schema
		}
	}
	return out
}

type property struct {
	Name     string
	Type     string
	Required bool
	MinLen   *int
	MaxLen   *int
}

func parseProperties(text string) []property {
	var out []property
	propRe := regexp.MustCompile(`(?s)(?:\[[^\]]+\]\s*)*(?:public\s+)?([A-Za-z0-9_<>,\[\]?]+)\s+(\w+)\s*(?:\{|[,)]|$)`)
	for _, loc := range propRe.FindAllStringSubmatchIndex(text, -1) {
		typ := text[loc[2]:loc[3]]
		name := text[loc[4]:loc[5]]
		if typ == "class" || typ == "record" || name == "get" || name == "set" {
			continue
		}
		attrs := text[loc[0]:loc[2]]
		wireName := extractCSharpJsonWireName(attrs)
		if wireName == "" {
			wireName = lowerFirst(name)
		}
		p := property{Name: wireName, Type: cleanType(typ)}
		if strings.Contains(attrs, "[Required") {
			p.Required = true
		}
		if mm := regexp.MustCompile(`\[StringLength\(\s*(\d+)`).FindStringSubmatch(attrs); len(mm) > 1 {
			if n, err := strconv.Atoi(mm[1]); err == nil {
				p.MaxLen = &n
			}
		}
		if mm := regexp.MustCompile(`\[Range\(\s*(\d+)\s*,\s*(\d+)`).FindStringSubmatch(attrs); len(mm) > 2 {
			if min, err := strconv.Atoi(mm[1]); err == nil {
				_ = min
			}
			if max, err := strconv.Atoi(mm[2]); err == nil {
				p.MaxLen = &max
			}
		}
		out = append(out, p)
	}
	return out
}

func parseMinimalParams(params, path, file string, line int) ([]*Parameter, string) {
	return parseParams(params, path, file, line, false)
}

func parseControllerParams(params, path, file string, line int) ([]*Parameter, string) {
	return parseParams(params, path, file, line, true)
}

func parseParams(params, path, file string, line int, attributes bool) ([]*Parameter, string) {
	pathParams := pathParamNames(path)
	var out []*Parameter
	body := ""
	for _, raw := range splitParams(params) {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		source := p
		p = regexp.MustCompile(`\[[^\]]+\]\s*`).ReplaceAllString(p, "")
		fields := strings.Fields(p)
		if len(fields) < 2 {
			continue
		}
		typ, name := cleanType(fields[len(fields)-2]), strings.Trim(fields[len(fields)-1], ",")
		name = strings.TrimPrefix(name, "@")
		if isFrameworkInjectedParam(typ, name) {
			continue
		}
		in := "query"
		required := false
		if pathParams[name] {
			in = "path"
			required = true
		}
		switch {
		case strings.Contains(source, "FromRoute"):
			in, required = "path", true
		case strings.Contains(source, "FromQuery"):
			in = "query"
		case strings.Contains(source, "FromHeader"):
			in = "header"
		case strings.Contains(source, "FromBody"):
			body = typ
			continue
		case !isSimpleType(typ) && !strings.HasSuffix(typ, "Service") && !strings.HasSuffix(typ, "Handler") && !strings.HasPrefix(typ, "I"):
			body = typ
			continue
		}
		if attributes || pathParams[name] || isSimpleType(typ) {
			out = append(out, &Parameter{Name: name, In: in, Type: typ, Required: required, File: file, Line: line})
		}
	}
	return out, body
}

func isFrameworkInjectedParam(typ, name string) bool {
	clean := strings.TrimSuffix(strings.ToLower(cleanType(typ)), "?")
	switch clean {
	case "cancellationtoken", "httpcontext", "httprequest", "httpresponse", "claimsprincipal", "endpointfilterinvocationcontext":
		return true
	}
	if strings.HasPrefix(clean, "ilogger<") || strings.HasPrefix(clean, "ioptions<") || strings.HasPrefix(clean, "imediator") {
		return true
	}
	if strings.EqualFold(name, "ct") || strings.EqualFold(name, "cancellationToken") {
		return clean == "cancellationtoken"
	}
	return false
}

func splitParams(params string) []string {
	var out []string
	depth := 0
	start := 0
	for i, r := range params {
		switch r {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, params[start:i])
				start = i + 1
			}
		}
	}
	out = append(out, params[start:])
	return out
}

func controllerBasePath(attrs, className string) string {
	if route := attrArg(attrs, "Route"); route != "" {
		return strings.ReplaceAll(route, "[controller]", strings.TrimSuffix(className, "Controller"))
	}
	return ""
}

func httpAttribute(attrs string) (string, string) {
	for _, pair := range []struct{ Attr, Method string }{{"HttpGet", "GET"}, {"HttpPost", "POST"}, {"HttpPut", "PUT"}, {"HttpDelete", "DELETE"}, {"HttpPatch", "PATCH"}} {
		if route := attrArg(attrs, pair.Attr); strings.Contains(attrs, "["+pair.Attr) {
			return pair.Method, route
		}
	}
	return "", ""
}

func attrArg(attrs, name string) string {
	re := regexp.MustCompile(`\[` + name + `(?:\s*\(\s*"([^"]*)"[^\)]*\))?`)
	if m := re.FindStringSubmatch(attrs); len(m) > 0 {
		return m[1]
	}
	return ""
}

func responseStatus(attrs, method, returnType string) int {
	if m := regexp.MustCompile(`StatusCodes\.Status(\d{3})`).FindStringSubmatch(attrs); len(m) == 2 {
		switch m[1] {
		case "200":
			return 200
		case "201":
			return 201
		case "202":
			return 202
		case "204":
			return 204
		}
	}
	return defaultStatus(method, returnType)
}

func defaultStatus(method, responseType string) int {
	if strings.EqualFold(responseType, "void") || responseType == "" && method == "DELETE" {
		return 204
	}
	if method == "POST" {
		return 201
	}
	return 200
}

func inferMinimalResponseType(body string, methodReturnTypes map[string]string) string {
	for _, pattern := range []string{
		`TypedResults\.(?:Ok|Created|Accepted)\s*<\s*([A-Za-z0-9_<>,\.\?\[\]]+)\s*>`,
		`(?:TypedResults|Results)\.(?:Ok|Created|Accepted)\(\s*(?:[^,]+,\s*)?new\s+([A-Za-z0-9_<>,\.\?\[\]]+)\s*\(`,
		`return\s+new\s+([A-Za-z0-9_<>,\.\?\[\]]+)\s*\(`,
	} {
		if m := regexp.MustCompile(pattern).FindStringSubmatch(body); len(m) == 2 {
			return cleanType(m[1])
		}
	}
	if m := regexp.MustCompile(`(?:TypedResults|Results)\.(?:Ok|Created|Accepted)\(\s*([A-Za-z_][A-Za-z0-9_]*)`).FindStringSubmatch(body); len(m) == 2 {
		name := regexp.QuoteMeta(m[1])
		patterns := []string{
			`var\s+` + name + `\s*=\s*new\s+([A-Za-z0-9_<>,\.\?\[\]]+)\s*\(`,
			`([A-Za-z0-9_<>,\.\?\[\]]+)\s+` + name + `\s*=`,
		}
		for _, pattern := range patterns {
			if typed := regexp.MustCompile(pattern).FindStringSubmatch(body); len(typed) == 2 {
				if typed[1] == "var" {
					continue
				}
				return cleanType(typed[1])
			}
		}
	}
	if strings.Contains(body, "NoContent(") {
		return "void"
	}
	for _, pattern := range []string{
		`return\s+await\s+[A-Za-z_][A-Za-z0-9_]*\.([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
		`return\s+[A-Za-z_][A-Za-z0-9_]*\.([A-Za-z_][A-Za-z0-9_]*)\s*\(`,
	} {
		if m := regexp.MustCompile(pattern).FindStringSubmatch(body); len(m) == 2 {
			if typ := methodReturnTypes[m[1]]; typ != "" {
				return typ
			}
		}
	}
	return "Object"
}

// endpointChain returns the full Minimal API registration statement starting at
// start, up to and including the terminating ';'. It tracks (), {}, [] nesting
// and string/char literals so that ';' characters inside a multiline inline
// lambda body do NOT prematurely truncate the chain. Truncating early dropped
// every fluent call chained AFTER the lambda — .RequireAuthorization(...),
// .ExcludeFromDescription(), .WithName(...), .Produces<T>() — causing missing
// security, leaked internal routes, and missing typed responses.
func endpointChain(text string, start int) string {
	if start < 0 || start >= len(text) {
		return ""
	}
	depth := 0
	var stringDelim byte // 0 when not inside a string/char literal
	const maxScan = 20000
	for i := start; i < len(text); i++ {
		if i-start > maxScan {
			return text[start:i]
		}
		c := text[i]
		if stringDelim != 0 {
			switch c {
			case '\\':
				i++ // skip escaped char
			case stringDelim:
				stringDelim = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			stringDelim = c
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
		case ';':
			if depth <= 0 {
				return text[start : i+1]
			}
		}
	}
	return text[start:]
}

func endpointName(chain string) string {
	if m := regexp.MustCompile(`\.WithName\(\s*"([^"]+)"\s*\)`).FindStringSubmatch(chain); len(m) == 2 {
		return m[1]
	}
	return ""
}

func responseTypeFromProduces(chain string) string {
	patterns := []string{
		`\.Produces<\s*([A-Za-z0-9_<>,\.\?\[\]]+)\s*>`,
		`\.Produces(?:ResponseType)?\(\s*typeof\(\s*([A-Za-z0-9_<>,\.\?\[\]]+)\s*\)`,
	}
	for _, pattern := range patterns {
		if m := regexp.MustCompile(pattern).FindStringSubmatch(chain); len(m) == 2 {
			return cleanType(m[1])
		}
	}
	return ""
}

func minimalResponseStatus(chain, method, responseType string) int {
	if status := statusFromText(chain); status != 0 {
		return status
	}
	return defaultStatus(method, responseType)
}

func statusFromText(text string) int {
	switch {
	case strings.Contains(text, "NoContent("):
		return 204
	case strings.Contains(text, "Created("), strings.Contains(text, "CreatedAtRoute("):
		return 201
	case strings.Contains(text, "Accepted("):
		return 202
	case strings.Contains(text, "Ok("):
		return 200
	}
	if m := regexp.MustCompile(`StatusCodes\.Status(\d{3})`).FindStringSubmatch(text); len(m) == 2 {
		if code, err := strconv.Atoi(m[1]); err == nil {
			return code
		}
	}
	if m := regexp.MustCompile(`,\s*(\d{3})\s*\)`).FindStringSubmatch(text); len(m) == 2 {
		if code, err := strconv.Atoi(m[1]); err == nil {
			return code
		}
	}
	return 0
}

func minimalHandlerSignature(text, name string) (string, string, bool) {
	pattern := `(?s)(?:public|private|internal|protected)?\s*(?:static\s+)?(?:async\s+)?([A-Za-z0-9_<>,\.\?\[\]\s]+?)\s+` + regexp.QuoteMeta(name) + `\s*\(([^)]*)\)`
	if m := regexp.MustCompile(pattern).FindStringSubmatch(text); len(m) == 3 {
		return strings.TrimSpace(m[1]), m[2], true
	}
	return "", "", false
}

func minimalHandlerBody(text, name string) string {
	pattern := `(?s)(?:public|private|internal|protected)?\s*(?:static\s+)?(?:async\s+)?[A-Za-z0-9_<>,\.\?\[\]\s]+?\s+` + regexp.QuoteMeta(name) + `\s*\([^)]*\)\s*\{`
	loc := regexp.MustCompile(pattern).FindStringIndex(text)
	if loc == nil {
		return ""
	}
	start := loc[1]
	depth := 1
	for i := start; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start:i]
			}
		}
	}
	return text[start:]
}

func chainedMetadata(chain string, constants map[string]string) []string {
	return authorizeMetadata(chain, constants)
}

func authorizeMetadata(text string, constants map[string]string) []string {
	var scopes []string

	// Match the common ASP.NET Core auth patterns:
	//   .RequireAuthorization("policy")
	//   .RequireAuthorization("policyA", "policyB")
	//   [Authorize(Policy = "name")]
	//   [Authorize(Roles = "admin,ops")]
	//   [Authorize("policyName")]      // positional policy form
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`RequireAuthorization\(([^)]*)\)`),
		regexp.MustCompile(`Authorize\(\s*Policy\s*=\s*"([^"]+)"`),
		regexp.MustCompile(`Authorize\(\s*Roles\s*=\s*"([^"]+)"`),
		regexp.MustCompile(`Authorize\(\s*"([^"]+)"`),
	}
	for _, re := range patterns {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			for _, value := range m[1:] {
				if value == "" {
					continue
				}
				// Roles attribute may carry comma-delimited values
				// inside the single quoted string; split here.
				for _, part := range strings.Split(value, ",") {
					part = strings.TrimSpace(part)
					part = strings.Trim(part, `"`)
					if resolved := resolveCSharpStringConstant(part, constants); resolved != "" {
						part = resolved
					}
					if part != "" {
						scopes = append(scopes, part)
					}
				}
			}
		}
	}
	return scopes
}

func extractStringConstants(text string) map[string]string {
	out := map[string]string{}
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`const\s+string\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"([^"]+)"`),
		regexp.MustCompile(`static\s+readonly\s+string\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"([^"]+)"`),
		regexp.MustCompile(`public\s+const\s+string\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"([^"]+)"`),
	}
	for _, re := range patterns {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			out[m[1]] = m[2]
		}
	}
	return out
}

func resolveCSharpStringConstant(expr string, constants map[string]string) string {
	expr = strings.TrimSpace(expr)
	expr = strings.Trim(expr, `"`)
	if expr == "" {
		return ""
	}
	if v, ok := constants[expr]; ok {
		return v
	}
	if idx := strings.LastIndex(expr, "."); idx >= 0 {
		if v, ok := constants[expr[idx+1:]]; ok {
			return v
		}
	}
	return ""
}

func unwrapResponseType(t string) string {
	t = cleanType(t)
	for _, prefix := range []string{"ActionResult<", "IActionResult<", "Task<", "ValueTask<"} {
		if strings.HasPrefix(t, prefix) && strings.HasSuffix(t, ">") {
			return unwrapResponseType(strings.TrimSuffix(strings.TrimPrefix(t, prefix), ">"))
		}
	}
	if strings.HasPrefix(t, "Results<") && strings.HasSuffix(t, ">") {
		inner := strings.TrimSuffix(strings.TrimPrefix(t, "Results<"), ">")
		for _, part := range splitParams(inner) {
			part = cleanType(part)
			if part == "NoContent" || part == "NotFound" || part == "BadRequest" {
				continue
			}
			return unwrapResponseType(part)
		}
	}
	for _, prefix := range []string{"Ok<", "Created<", "CreatedAtRoute<", "Accepted<"} {
		if strings.HasPrefix(t, prefix) && strings.HasSuffix(t, ">") {
			return unwrapResponseType(strings.TrimSuffix(strings.TrimPrefix(t, prefix), ">"))
		}
	}
	if t == "IActionResult" || t == "ActionResult" || t == "IResult" {
		return "Object"
	}
	return t
}

func typeSchema(t string) map[string]interface{} {
	switch strings.TrimSuffix(strings.ToLower(cleanType(t)), "?") {
	case "string", "guid":
		return map[string]interface{}{"type": "string"}
	case "int", "long", "short":
		return map[string]interface{}{"type": "integer"}
	case "decimal", "double", "float":
		return map[string]interface{}{"type": "number"}
	case "bool", "boolean":
		return map[string]interface{}{"type": "boolean"}
	default:
		if strings.HasPrefix(t, "IEnumerable<") || strings.HasPrefix(t, "List<") {
			inner := strings.TrimSuffix(t[strings.Index(t, "<")+1:], ">")
			return map[string]interface{}{"type": "array", "items": typeSchema(inner)}
		}
		return map[string]interface{}{"$ref": "#/components/schemas/" + cleanType(t)}
	}
}

func isSimpleType(t string) bool {
	switch strings.TrimSuffix(strings.ToLower(cleanType(t)), "?") {
	case "string", "guid", "int", "long", "short", "decimal", "double", "float", "bool", "boolean", "cancellationtoken":
		return true
	default:
		return false
	}
}

func cleanType(t string) string {
	t = strings.TrimSpace(t)
	t = strings.TrimPrefix(t, "System.")
	return strings.TrimSpace(t)
}

func pathParamNames(path string) map[string]bool {
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`\{([^}:]+)(?::[^}]+)?\}`).FindAllStringSubmatch(path, -1) {
		out[m[1]] = true
	}
	return out
}

func normalizeRoute(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	path = regexp.MustCompile(`\{([^}:]+):[^}]+\}`).ReplaceAllString(path, `{$1}`)
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path
}

func joinPaths(parts ...string) string {
	var out string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if out == "" {
			out = p
			continue
		}
		out = strings.TrimRight(out, "/") + "/" + strings.TrimLeft(p, "/")
	}
	return normalizeRoute(out)
}

func operationID(method, path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	var words []string
	words = append(words, strings.ToLower(method))
	for _, seg := range segments {
		if seg == "" || strings.HasPrefix(seg, "{") {
			continue
		}
		words = append(words, strings.ReplaceAll(seg, "-", "_"))
	}
	return strings.Join(words, "_")
}

func tagFromPath(path string) string {
	for _, seg := range strings.Split(strings.Trim(path, "/"), "/") {
		if seg != "" && !strings.HasPrefix(seg, "{") {
			return strings.TrimSuffix(title(seg), "s")
		}
	}
	return "Default"
}

func tagFromController(name string) string {
	name = strings.TrimSuffix(name, "Controller")
	return title(name)
}

func title(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	if s == "" {
		return "Default"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func extractCSharpJsonWireName(attrs string) string {
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`\[JsonPropertyName\(\s*"([^"]+)"`),
		regexp.MustCompile(`\[JsonProperty\(\s*"([^"]+)"`),
		regexp.MustCompile(`\[JsonProperty\(\s*PropertyName\s*=\s*"([^"]+)"`),
	} {
		if m := re.FindStringSubmatch(attrs); len(m) > 1 {
			return m[1]
		}
	}
	return ""
}

func lowerFirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func summaryBefore(text string, pos int) string {
	before := text[:pos]
	start := strings.LastIndex(before, "///")
	if start < 0 || pos-start > 500 {
		return ""
	}
	block := before[start:]
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "///"))
		line = strings.TrimSpace(strings.TrimPrefix(line, "<summary>"))
		line = strings.TrimSpace(strings.TrimSuffix(line, "</summary>"))
		if line != "" && !strings.HasPrefix(line, "<") {
			return line
		}
	}
	return ""
}

func lineCol(text string, pos int) (int, int) {
	line, col := 1, 1
	for i, r := range text {
		if i >= pos {
			break
		}
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
