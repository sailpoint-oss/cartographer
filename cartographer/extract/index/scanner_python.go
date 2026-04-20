package index

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// scanPythonFile extracts type declarations from a Python source file.
//
// The Python type system is nominal (there is no structural typing like TS),
// so we look for three patterns that are conventionally used to define DTOs:
//
//  1. Pydantic BaseModel subclasses (`class X(BaseModel):`)
//  2. @dataclass / @dataclasses.dataclass decorated classes
//  3. Enum / IntEnum / StrEnum subclasses
//
// For any other top-level class we still index it as kind="class" with its
// annotated fields, so route handlers that reference ordinary classes still
// get a best-effort schema.
func (s *Scanner) scanPythonFile(path string, source []byte, tree *tree_sitter.Tree) {
	root := tree.RootNode()
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Kind() {
		case "class_definition":
			s.extractPythonClass(child, source, path, nil)
		case "decorated_definition":
			s.extractPythonDecorated(child, source, path)
		}
	}
}

// extractPythonDecorated walks a decorated_definition node, collecting any
// decorators and then processing the wrapped class/function.
func (s *Scanner) extractPythonDecorated(node *tree_sitter.Node, source []byte, file string) {
	var decorators []string
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "decorator":
			decorators = append(decorators, strings.TrimPrefix(strings.TrimSpace(child.Utf8Text(source)), "@"))
		case "class_definition":
			s.extractPythonClass(child, source, file, decorators)
		}
	}
}

func (s *Scanner) extractPythonClass(node *tree_sitter.Node, source []byte, file string, decorators []string) {
	name := ""
	superclasses := []string{}
	var body *tree_sitter.Node

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "identifier":
			if name == "" {
				name = child.Utf8Text(source)
			}
		case "argument_list":
			superclasses = parsePythonSuperclasses(child, source)
		case "block":
			body = child
		}
	}

	if name == "" || strings.HasPrefix(name, "_") {
		return
	}

	kind := classifyPythonClass(superclasses, decorators)
	if kind == "" {
		// Not a recognized model / enum — but still worth indexing so
		// handler signatures that reference it resolve to something.
		kind = "class"
	}

	docstring := extractPythonDocstring(body, source)
	startPos := node.StartPosition()
	decl := &TypeDecl{
		Name:        name,
		Qualified:   name,
		Kind:        kind,
		SourceFile:  file,
		Line:        int(startPos.Row) + 1,
		Column:      int(startPos.Column) + 1,
		Description: docstring,
		SuperClass:  joinFirst(superclasses),
		Interfaces:  superclasses,
	}

	if kind == "enum" {
		decl.EnumValues = extractPythonEnumValues(body, source)
	} else {
		decl.Fields = extractPythonFields(body, source)
	}

	if containsString(decorators, "deprecated") {
		decl.Deprecated = true
	}

	s.index.Add(name, decl)
}

func parsePythonSuperclasses(argList *tree_sitter.Node, source []byte) []string {
	var out []string
	for i := uint(0); i < argList.ChildCount(); i++ {
		child := argList.Child(i)
		switch child.Kind() {
		case "identifier":
			out = append(out, child.Utf8Text(source))
		case "attribute":
			// e.g. `pydantic.BaseModel` — keep the tail as simple name
			text := child.Utf8Text(source)
			if idx := strings.LastIndex(text, "."); idx >= 0 {
				out = append(out, text[idx+1:])
			} else {
				out = append(out, text)
			}
		case "keyword_argument":
			// `class X(BaseModel, extra="allow")` — skip keyword args
		}
	}
	return out
}

// classifyPythonClass returns one of "class" (pydantic/dataclass DTO), "enum",
// or "" (not recognized).
func classifyPythonClass(superclasses, decorators []string) string {
	for _, sc := range superclasses {
		switch sc {
		case "BaseModel", "GenericModel":
			return "class"
		case "Enum", "IntEnum", "StrEnum", "Flag", "IntFlag":
			return "enum"
		case "TypedDict":
			return "interface"
		case "NamedTuple":
			return "class"
		}
	}
	for _, d := range decorators {
		// normalise decorator text, e.g. "dataclass" / "dataclasses.dataclass"
		base := d
		if idx := strings.Index(base, "("); idx >= 0 {
			base = base[:idx]
		}
		if idx := strings.LastIndex(base, "."); idx >= 0 {
			base = base[idx+1:]
		}
		switch strings.TrimSpace(base) {
		case "dataclass", "attrs", "define", "frozen":
			return "class"
		}
	}
	return ""
}

// extractPythonDocstring returns the first string literal inside a class block,
// which by convention is the class docstring.
func extractPythonDocstring(body *tree_sitter.Node, source []byte) string {
	if body == nil {
		return ""
	}
	for i := uint(0); i < body.ChildCount(); i++ {
		child := body.Child(i)
		if child.Kind() != "expression_statement" {
			continue
		}
		// First child of expression_statement is the string
		for j := uint(0); j < child.ChildCount(); j++ {
			inner := child.Child(j)
			if inner.Kind() == "string" {
				return cleanPythonDocstring(inner.Utf8Text(source))
			}
		}
		// Only the first expression_statement is the docstring
		return ""
	}
	return ""
}

func cleanPythonDocstring(s string) string {
	s = strings.TrimSpace(s)
	// Strip triple quotes
	for _, q := range []string{`"""`, `'''`, `"`, `'`} {
		if strings.HasPrefix(s, q) && strings.HasSuffix(s, q) {
			s = strings.TrimPrefix(s, q)
			s = strings.TrimSuffix(s, q)
			break
		}
	}
	return strings.TrimSpace(s)
}

// extractPythonFields walks a class body and extracts typed class attributes.
//
// Pydantic / dataclass convention:
//
//	class User(BaseModel):
//	    """docstring"""
//	    id: str
//	    name: str = "Bob"
//	    age: int | None = None
//	    email: str = Field(..., description="email")
func extractPythonFields(body *tree_sitter.Node, source []byte) []FieldDecl {
	if body == nil {
		return nil
	}

	var fields []FieldDecl
	var pendingComment string

	for i := uint(0); i < body.ChildCount(); i++ {
		child := body.Child(i)
		if child.Kind() == "comment" {
			pendingComment = strings.TrimSpace(strings.TrimPrefix(child.Utf8Text(source), "#"))
			continue
		}
		if child.Kind() != "expression_statement" {
			pendingComment = ""
			continue
		}
		// The assignment is the first child of expression_statement
		for j := uint(0); j < child.ChildCount(); j++ {
			inner := child.Child(j)
			if inner.Kind() != "assignment" {
				continue
			}
			f := parsePythonFieldAssignment(inner, source)
			if f.Name == "" {
				continue
			}
			if f.Description == "" {
				f.Description = pendingComment
			}
			pos := inner.StartPosition()
			f.Line = int(pos.Row) + 1
			f.Column = int(pos.Column) + 1
			fields = append(fields, f)
		}
		pendingComment = ""
	}

	return fields
}

// parsePythonFieldAssignment parses a single typed attribute assignment.
//
// Tree-sitter exposes assignments with the following fields:
//
//	left       -> target (identifier)
//	type       -> type annotation (anything to the right of `:` and before `=`)
//	right      -> RHS expression (the default value / Field(...))
func parsePythonFieldAssignment(node *tree_sitter.Node, source []byte) FieldDecl {
	fd := FieldDecl{Annotations: make(map[string]string)}

	// Walk the assignment looking for the three named fields.
	if left := node.ChildByFieldName("left"); left != nil {
		if left.Kind() == "identifier" {
			fd.Name = left.Utf8Text(source)
		}
	}
	if typeNode := node.ChildByFieldName("type"); typeNode != nil {
		fd.Type = normalisePythonType(typeNode.Utf8Text(source))
		fd.Nullable = strings.Contains(fd.Type, "Optional[") ||
			strings.Contains(fd.Type, "| None") ||
			strings.Contains(fd.Type, "None |") ||
			strings.HasSuffix(fd.Type, "None")
	}
	if right := node.ChildByFieldName("right"); right != nil {
		text := right.Utf8Text(source)
		fd.DefaultValue = strings.TrimSpace(text)

		// Field(..., description="foo", deprecated=True, alias="Foo") — extract interesting kwargs
		if isPydanticFieldCall(right, source) {
			props := extractPydanticFieldKwargs(right, source)
			if desc := props["description"]; desc != "" {
				fd.Description = desc
			}
			if alias := props["alias"]; alias != "" {
				fd.JSONName = alias
			}
			if props["deprecated"] == "True" || props["deprecated"] == "true" {
				fd.Deprecated = true
			}
			if props["default"] == "..." || strings.HasPrefix(fd.DefaultValue, "Field(...") {
				// Required field (... sentinel in Pydantic)
				fd.Required = true
			}
			if _, hasDefault := props["default"]; hasDefault && props["default"] != "..." {
				fd.Required = false
			}
		}
	} else {
		// No RHS → annotated-only field → required by default
		fd.Required = true
	}

	if fd.JSONName == "" {
		fd.JSONName = fd.Name
	}

	// If type ends with `None` and no default, still required but nullable.
	if fd.Required && fd.DefaultValue != "" {
		fd.Required = false
	}

	return fd
}

func normalisePythonType(t string) string {
	return strings.TrimSpace(t)
}

func isPydanticFieldCall(node *tree_sitter.Node, source []byte) bool {
	if node.Kind() != "call" {
		return false
	}
	// function name is the first child
	fn := node.ChildByFieldName("function")
	if fn == nil {
		return false
	}
	name := fn.Utf8Text(source)
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		name = name[idx+1:]
	}
	return name == "Field"
}

func extractPydanticFieldKwargs(call *tree_sitter.Node, source []byte) map[string]string {
	out := make(map[string]string)
	args := call.ChildByFieldName("arguments")
	if args == nil {
		return out
	}
	// Check for the ellipsis / value first positional arg (default).
	for i := uint(0); i < args.ChildCount(); i++ {
		child := args.Child(i)
		switch child.Kind() {
		case "ellipsis":
			out["default"] = "..."
		case "keyword_argument":
			k := child.ChildByFieldName("name")
			v := child.ChildByFieldName("value")
			if k == nil || v == nil {
				continue
			}
			kt := strings.TrimSpace(k.Utf8Text(source))
			vt := strings.TrimSpace(v.Utf8Text(source))
			vt = strings.Trim(vt, `"'`)
			out[kt] = vt
		}
	}
	return out
}

func extractPythonEnumValues(body *tree_sitter.Node, source []byte) []string {
	if body == nil {
		return nil
	}
	var values []string
	for i := uint(0); i < body.ChildCount(); i++ {
		child := body.Child(i)
		if child.Kind() != "expression_statement" {
			continue
		}
		for j := uint(0); j < child.ChildCount(); j++ {
			inner := child.Child(j)
			if inner.Kind() != "assignment" {
				continue
			}
			left := inner.ChildByFieldName("left")
			if left == nil || left.Kind() != "identifier" {
				continue
			}
			name := left.Utf8Text(source)
			if strings.HasPrefix(name, "_") {
				continue
			}
			values = append(values, name)
		}
	}
	return values
}

func joinFirst(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[0]
}

func containsString(xs []string, needle string) bool {
	for _, x := range xs {
		if x == needle {
			return true
		}
		// tolerate `dataclass()` or `dataclasses.dataclass` by stripping args/namespace
		base := x
		if i := strings.Index(base, "("); i >= 0 {
			base = base[:i]
		}
		if i := strings.LastIndex(base, "."); i >= 0 {
			base = base[i+1:]
		}
		if base == needle {
			return true
		}
	}
	return false
}
