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
	for _, f := range files {
		result.Operations = append(result.Operations, extractMinimalAPIOperations(f)...)
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

func extractMinimalAPIOperations(f sourceFile) []*Operation {
	groupPrefixes := map[string]string{"app": ""}
	groupRe := regexp.MustCompile(`(?m)(?:var\s+)?(\w+)\s*=\s*(\w+)\.MapGroup\(\s*"([^"]*)"\s*\)`)
	for _, m := range groupRe.FindAllStringSubmatch(f.text, -1) {
		parent := groupPrefixes[m[2]]
		groupPrefixes[m[1]] = joinPaths(parent, m[3])
	}
	endpointRe := regexp.MustCompile(`(?s)(\w+)\.Map(Get|Post|Put|Delete|Patch)\(\s*"([^"]*)"\s*,\s*(?:async\s*)?\(([^)]*)\)\s*=>`)
	var ops []*Operation
	for _, loc := range endpointRe.FindAllStringSubmatchIndex(f.text, -1) {
		m := endpointRe.FindStringSubmatch(f.text[loc[0]:loc[1]])
		receiver, method, route, params := m[1], strings.ToUpper(m[2]), m[3], m[4]
		prefix := groupPrefixes[receiver]
		path := normalizeRoute(joinPaths(prefix, route))
		line, col := lineCol(f.text, loc[0])
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
		op.ResponseType = inferMinimalResponseType(f.text[loc[1]:min(len(f.text), loc[1]+600)])
		op.ResponseStatus = defaultStatus(method, op.ResponseType)
		op.Security = chainedMetadata(f.text[loc[1]:min(len(f.text), loc[1]+500)])
		ops = append(ops, op)
	}
	return ops
}

func extractControllerOperations(f sourceFile) []*Operation {
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
				Security:       authorizeMetadata(methodAttrs + "\n" + attrs),
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
			props[p.Name] = typeSchema(p.Type)
		}
		if len(props) > 0 {
			out[name] = map[string]interface{}{"type": "object", "properties": props}
		}
	}
	return out
}

type property struct{ Name, Type string }

func parseProperties(text string) []property {
	var out []property
	propRe := regexp.MustCompile(`(?:public\s+)?([A-Za-z0-9_<>,\[\]?]+)\s+(\w+)\s*(?:\{|[,)]|$)`)
	for _, m := range propRe.FindAllStringSubmatch(text, -1) {
		if m[1] == "class" || m[1] == "record" || m[2] == "get" || m[2] == "set" {
			continue
		}
		out = append(out, property{Name: lowerFirst(m[2]), Type: cleanType(m[1])})
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

func inferMinimalResponseType(body string) string {
	for _, pattern := range []string{`TypedResults\.Ok\((?:await\s+)?[^\)]*<([A-Za-z0-9_]+)>`, `Results\.Ok\((?:await\s+)?([A-Za-z0-9_]+)`, `TypedResults\.Created\([^,]+,\s*([A-Za-z0-9_]+)`} {
		if m := regexp.MustCompile(pattern).FindStringSubmatch(body); len(m) == 2 {
			return cleanType(m[1])
		}
	}
	return "Object"
}

func chainedMetadata(chain string) []string {
	return authorizeMetadata(chain)
}

func authorizeMetadata(text string) []string {
	var scopes []string
	re := regexp.MustCompile(`RequireAuthorization\(\s*"([^"]+)"\s*\)|Authorize\([^)]*Policy\s*=\s*"([^"]+)"`)
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		for _, value := range m[1:] {
			if value != "" {
				scopes = append(scopes, value)
			}
		}
	}
	return scopes
}

func unwrapResponseType(t string) string {
	t = cleanType(t)
	for _, prefix := range []string{"ActionResult<", "IActionResult<", "Task<", "ValueTask<"} {
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
	t = strings.TrimPrefix(t, "Task<")
	t = strings.TrimSuffix(t, ">")
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
