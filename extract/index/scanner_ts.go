package index

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// scanTypeScriptFile extracts type declarations from a TypeScript source file.
func (s *Scanner) scanTypeScriptFile(path string, source []byte, tree *tree_sitter.Tree) {
	root := tree.RootNode()

	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Kind() {
		case "class_declaration", "export_statement":
			s.extractTSDeclaration(child, source, path)
		case "interface_declaration":
			s.extractTSInterface(child, source, path)
		case "type_alias_declaration":
			s.extractTSTypeAlias(child, source, path)
		case "enum_declaration":
			s.extractTSEnum(child, source, path)
		}
	}
}

func (s *Scanner) extractTSDeclaration(node *tree_sitter.Node, source []byte, file string) {
	// Handle export_statement wrapping a class_declaration
	if node.Kind() == "export_statement" {
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			switch child.Kind() {
			case "class_declaration":
				s.extractTSClass(child, source, file)
			case "interface_declaration":
				s.extractTSInterface(child, source, file)
			case "type_alias_declaration":
				s.extractTSTypeAlias(child, source, file)
			case "enum_declaration":
				s.extractTSEnum(child, source, file)
			}
		}
		return
	}

	if node.Kind() == "class_declaration" {
		s.extractTSClass(node, source, file)
	}
}

func (s *Scanner) extractTSClass(node *tree_sitter.Node, source []byte, file string) {
	name := ""
	var fields []FieldDecl
	var generics []string

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "type_identifier":
			if name == "" {
				name = child.Utf8Text(source)
			}
		case "type_parameters":
			generics = extractTSTypeParams(child, source)
		case "class_body":
			fields = extractTSClassFields(child, source)
		}
	}

	if name == "" {
		return
	}

	startPos := node.StartPosition()
	s.index.Add(name, &TypeDecl{
		Name:       name,
		Qualified:  name,
		Kind:       "class",
		SourceFile: file,
		Line:       int(startPos.Row) + 1,
		Column:     int(startPos.Column) + 1,
		Fields:     fields,
		Generics:   generics,
	})
}

func (s *Scanner) extractTSInterface(node *tree_sitter.Node, source []byte, file string) {
	name := ""
	var fields []FieldDecl
	var generics []string

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "type_identifier":
			if name == "" {
				name = child.Utf8Text(source)
			}
		case "type_parameters":
			generics = extractTSTypeParams(child, source)
		case "interface_body", "object_type":
			fields = extractTSInterfaceFields(child, source)
		}
	}

	if name == "" {
		return
	}

	startPos := node.StartPosition()
	s.index.Add(name, &TypeDecl{
		Name:       name,
		Qualified:  name,
		Kind:       "interface",
		SourceFile: file,
		Line:       int(startPos.Row) + 1,
		Column:     int(startPos.Column) + 1,
		Fields:     fields,
		Generics:   generics,
	})
}

func (s *Scanner) extractTSTypeAlias(node *tree_sitter.Node, source []byte, file string) {
	name := ""
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "type_identifier" {
			name = child.Utf8Text(source)
			break
		}
	}

	if name == "" {
		return
	}

	startPos := node.StartPosition()
	s.index.Add(name, &TypeDecl{
		Name:       name,
		Qualified:  name,
		Kind:       "interface", // treat type aliases like interfaces for schema generation
		SourceFile: file,
		Line:       int(startPos.Row) + 1,
		Column:     int(startPos.Column) + 1,
	})
}

func (s *Scanner) extractTSEnum(node *tree_sitter.Node, source []byte, file string) {
	name := ""
	var values []string

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "identifier":
			if name == "" {
				name = child.Utf8Text(source)
			}
		case "enum_body":
			for j := uint(0); j < child.ChildCount(); j++ {
				member := child.Child(j)
				if member.Kind() == "enum_assignment" || member.Kind() == "property_identifier" {
					// Extract the name
					for k := uint(0); k < member.ChildCount(); k++ {
						n := member.Child(k)
						if n.Kind() == "property_identifier" || n.Kind() == "identifier" {
							values = append(values, n.Utf8Text(source))
							break
						}
					}
				}
			}
		}
	}

	if name == "" {
		return
	}

	startPos := node.StartPosition()
	s.index.Add(name, &TypeDecl{
		Name:       name,
		Qualified:  name,
		Kind:       "enum",
		SourceFile: file,
		Line:       int(startPos.Row) + 1,
		Column:     int(startPos.Column) + 1,
		EnumValues: values,
	})
}

func extractTSClassFields(body *tree_sitter.Node, source []byte) []FieldDecl {
	var fields []FieldDecl

	for i := uint(0); i < body.ChildCount(); i++ {
		child := body.Child(i)
		if child.Kind() != "public_field_definition" && child.Kind() != "property_declaration" {
			continue
		}

		fieldName := ""
		fieldType := ""
		optional := false
		annotations := make(map[string]string)

		for j := uint(0); j < child.ChildCount(); j++ {
			n := child.Child(j)
			switch n.Kind() {
			case "property_identifier":
				fieldName = n.Utf8Text(source)
			case "type_annotation":
				for k := uint(0); k < n.ChildCount(); k++ {
					tn := n.Child(k)
					if tn.Kind() != ":" {
						fieldType = tn.Utf8Text(source)
					}
				}
			case "?":
				optional = true
			case "decorator":
				annName, annValue := extractTSDecorator(n, source)
				if annName != "" {
					annotations[annName] = annValue
				}
			}
		}

		if fieldName == "" {
			continue
		}

		fieldPos := child.StartPosition()
		fd := FieldDecl{
			Name:        fieldName,
			Type:        fieldType,
			JSONName:    fieldName,
			Required:    !optional,
			Nullable:    optional, // v5 #1: TS optional (?) fields are nullable
			Annotations: annotations,
			Line:        int(fieldPos.Row) + 1,
			Column:      int(fieldPos.Column) + 1,
		}

		// v5 #10: Check for @deprecated JSDoc tag on preceding comment
		if prev := child.PrevSibling(); prev != nil {
			if prev.Kind() == "comment" || prev.Kind() == "block_comment" {
				commentText := prev.Utf8Text(source)
				if strings.Contains(commentText, "@deprecated") {
					fd.Deprecated = true
				}
			}
		}

		fields = append(fields, fd)
	}

	return fields
}

func extractTSInterfaceFields(body *tree_sitter.Node, source []byte) []FieldDecl {
	var fields []FieldDecl

	for i := uint(0); i < body.ChildCount(); i++ {
		child := body.Child(i)
		if child.Kind() != "property_signature" {
			continue
		}

		fieldName := ""
		fieldType := ""
		optional := false

		for j := uint(0); j < child.ChildCount(); j++ {
			n := child.Child(j)
			switch n.Kind() {
			case "property_identifier":
				fieldName = n.Utf8Text(source)
			case "type_annotation":
				for k := uint(0); k < n.ChildCount(); k++ {
					tn := n.Child(k)
					if tn.Kind() != ":" {
						fieldType = tn.Utf8Text(source)
					}
				}
			case "?":
				optional = true
			}
		}

		if fieldName == "" {
			continue
		}

		fieldPos := child.StartPosition()
		fields = append(fields, FieldDecl{
			Name:     fieldName,
			Type:     fieldType,
			JSONName: fieldName,
			Required: !optional,
			Line:     int(fieldPos.Row) + 1,
			Column:   int(fieldPos.Column) + 1,
		})
	}

	return fields
}

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

func extractTSTypeParams(node *tree_sitter.Node, source []byte) []string {
	var params []string
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "type_parameter" {
			for j := uint(0); j < child.ChildCount(); j++ {
				n := child.Child(j)
				if n.Kind() == "type_identifier" {
					params = append(params, n.Utf8Text(source))
					break
				}
			}
		}
	}
	return params
}

// stripQuotes removes leading/trailing quotes from a string.
func stripQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
