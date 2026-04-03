package index

import (
	"regexp"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// scanJavaFile extracts type declarations from a Java source file.
func (s *Scanner) scanJavaFile(path string, source []byte, tree *tree_sitter.Tree) {
	root := tree.RootNode()

	// Extract package declaration
	pkg := ""
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child.Kind() == "package_declaration" {
			// package_declaration -> scoped_identifier or identifier
			for j := uint(0); j < child.ChildCount(); j++ {
				n := child.Child(j)
				if n.Kind() == "scoped_identifier" || n.Kind() == "identifier" {
					pkg = n.Utf8Text(source)
				}
			}
		}
	}

	// Extract imports
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		if child.Kind() == "import_declaration" {
			importPath := ""
			for j := uint(0); j < child.ChildCount(); j++ {
				n := child.Child(j)
				if n.Kind() == "scoped_identifier" || n.Kind() == "identifier" {
					importPath = n.Utf8Text(source)
				}
			}
			if importPath != "" {
				parts := strings.Split(importPath, ".")
				simpleName := parts[len(parts)-1]
				if simpleName != "*" {
					s.index.AddImport(path, simpleName, importPath)
				}
			}
		}
	}

	// Walk top-level declarations
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Kind() {
		case "class_declaration":
			s.extractJavaClass(child, source, path, pkg)
		case "interface_declaration":
			s.extractJavaInterface(child, source, path, pkg)
		case "enum_declaration":
			s.extractJavaEnum(child, source, path, pkg)
		}
	}
}

func (s *Scanner) extractJavaClass(node *tree_sitter.Node, source []byte, file, pkg string) {
	name := ""
	description := ""
	var fields []FieldDecl
	superClass := ""
	var interfaces []string
	var generics []string
	discriminatorProperty := ""
	var discriminatorMapping map[string]string
	readOnly := false
	deprecated := false
	var classBodyNode *tree_sitter.Node

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "identifier":
			if name == "" {
				name = child.Utf8Text(source)
			}
		case "modifiers":
			// Check for class-level annotations
			for j := uint(0); j < child.ChildCount(); j++ {
				ann := child.Child(j)
				if ann.Kind() == "marker_annotation" || ann.Kind() == "annotation" {
					annName := ""
					annValue := ""
					for l := uint(0); l < ann.ChildCount(); l++ {
						ac := ann.Child(l)
						if ac.Kind() == "identifier" {
							annName = ac.Utf8Text(source)
						}
						if ac.Kind() == "annotation_argument_list" {
							annValue = ac.Utf8Text(source)
						}
					}
					if annName == "Schema" && annValue != "" {
						description = extractSchemaDescription(annValue)
					}
					// Improvement #4: @JsonTypeInfo discriminator
					if annName == "JsonTypeInfo" && annValue != "" {
						discriminatorProperty = extractJsonTypeInfoProperty(annValue)
					}
					// Improvement #4: @JsonSubTypes mapping
					if annName == "JsonSubTypes" && annValue != "" {
						discriminatorMapping = extractJsonSubTypesMapping(annValue)
					}
					// Improvement #5: Lombok @Value → readOnly
					if annName == "Value" {
						readOnly = true
					}
					// v5 #10: @Deprecated on class
					if annName == "Deprecated" {
						deprecated = true
					}
				}
			}
		case "type_parameters":
			generics = extractTypeParams(child, source)
		case "superclass":
			for j := uint(0); j < child.ChildCount(); j++ {
				n := child.Child(j)
				if n.Kind() == "type_identifier" || n.Kind() == "generic_type" {
					superClass = n.Utf8Text(source)
				}
			}
		case "super_interfaces":
			for j := uint(0); j < child.ChildCount(); j++ {
				n := child.Child(j)
				if n.Kind() == "type_list" {
					for k := uint(0); k < n.ChildCount(); k++ {
						tn := n.Child(k)
						if tn.Kind() == "type_identifier" || tn.Kind() == "generic_type" {
							interfaces = append(interfaces, tn.Utf8Text(source))
						}
					}
				}
			}
		case "class_body":
			fields = extractJavaFields(child, source)
			classBodyNode = child
		}
	}

	// Fall back to class-level JavaDoc for description
	if description == "" {
		description = extractClassJavaDoc(node, source)
	}

	if name == "" {
		return
	}

	qualified := name
	if pkg != "" {
		qualified = pkg + "." + name
	}

	// Improvement #5: Lombok @Value → mark all fields as readOnly
	if readOnly {
		for i := range fields {
			if fields[i].Annotations == nil {
				fields[i].Annotations = make(map[string]string)
			}
			fields[i].Annotations["JsonProperty"] = "(access = READ_ONLY)"
		}
	}

	// Improvement #5: Lombok @NonNull on fields → Required
	// v5 #1: @Nonnull on fields → Required
	for i := range fields {
		if _, hasNonNull := fields[i].Annotations["NonNull"]; hasNonNull {
			fields[i].Required = true
		}
		if _, hasNonnull := fields[i].Annotations["Nonnull"]; hasNonnull {
			fields[i].Required = true
		}
	}

	startPos := node.StartPosition()
	decl := &TypeDecl{
		Name:                  name,
		Qualified:             qualified,
		Kind:                  "class",
		Package:               pkg,
		SourceFile:            file,
		Line:                  int(startPos.Row) + 1,
		Column:                int(startPos.Column) + 1,
		Fields:                fields,
		SuperClass:            superClass,
		Interfaces:            interfaces,
		Generics:              generics,
		DiscriminatorProperty: discriminatorProperty,
		DiscriminatorMapping:  discriminatorMapping,
		ReadOnly:              readOnly,
		Deprecated:            deprecated,
	}

	if description != "" {
		decl.Description = description
	}

	s.index.Add(qualified, decl)

	// v5 #2: Walk class body for nested type declarations
	if classBodyNode != nil {
		for i := uint(0); i < classBodyNode.ChildCount(); i++ {
			child := classBodyNode.Child(i)
			switch child.Kind() {
			case "class_declaration":
				s.extractJavaClass(child, source, file, qualified)
			case "interface_declaration":
				s.extractJavaInterface(child, source, file, qualified)
			case "enum_declaration":
				s.extractJavaEnum(child, source, file, qualified)
			}
		}
	}
}

func (s *Scanner) extractJavaInterface(node *tree_sitter.Node, source []byte, file, pkg string) {
	name := ""
	var generics []string
	discriminatorProperty := ""
	var discriminatorMapping map[string]string
	var interfaceBodyNode *tree_sitter.Node

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "identifier":
			if name == "" {
				name = child.Utf8Text(source)
			}
		case "type_parameters":
			generics = extractTypeParams(child, source)
		case "modifiers":
			// Improvement #4: Discriminator annotations on interfaces
			for j := uint(0); j < child.ChildCount(); j++ {
				ann := child.Child(j)
				if ann.Kind() == "marker_annotation" || ann.Kind() == "annotation" {
					annName := ""
					annValue := ""
					for l := uint(0); l < ann.ChildCount(); l++ {
						ac := ann.Child(l)
						if ac.Kind() == "identifier" {
							annName = ac.Utf8Text(source)
						}
						if ac.Kind() == "annotation_argument_list" {
							annValue = ac.Utf8Text(source)
						}
					}
					if annName == "JsonTypeInfo" && annValue != "" {
						discriminatorProperty = extractJsonTypeInfoProperty(annValue)
					}
					if annName == "JsonSubTypes" && annValue != "" {
						discriminatorMapping = extractJsonSubTypesMapping(annValue)
					}
				}
			}
		case "interface_body":
			interfaceBodyNode = child
		}
	}

	if name == "" {
		return
	}

	qualified := name
	if pkg != "" {
		qualified = pkg + "." + name
	}

	startPos := node.StartPosition()
	s.index.Add(qualified, &TypeDecl{
		Name:                  name,
		Qualified:             qualified,
		Kind:                  "interface",
		Package:               pkg,
		SourceFile:            file,
		Line:                  int(startPos.Row) + 1,
		Column:                int(startPos.Column) + 1,
		Generics:              generics,
		DiscriminatorProperty: discriminatorProperty,
		DiscriminatorMapping:  discriminatorMapping,
	})

	// v5 #2: Walk interface body for nested type declarations
	if interfaceBodyNode != nil {
		for i := uint(0); i < interfaceBodyNode.ChildCount(); i++ {
			child := interfaceBodyNode.Child(i)
			switch child.Kind() {
			case "class_declaration":
				s.extractJavaClass(child, source, file, qualified)
			case "interface_declaration":
				s.extractJavaInterface(child, source, file, qualified)
			case "enum_declaration":
				s.extractJavaEnum(child, source, file, qualified)
			}
		}
	}
}

func (s *Scanner) extractJavaEnum(node *tree_sitter.Node, source []byte, file, pkg string) {
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
				ec := child.Child(j)
				if ec.Kind() == "enum_constant" {
					for k := uint(0); k < ec.ChildCount(); k++ {
						n := ec.Child(k)
						if n.Kind() == "identifier" {
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

	qualified := name
	if pkg != "" {
		qualified = pkg + "." + name
	}

	startPos := node.StartPosition()
	s.index.Add(qualified, &TypeDecl{
		Name:       name,
		Qualified:  qualified,
		Kind:       "enum",
		Package:    pkg,
		SourceFile: file,
		Line:       int(startPos.Row) + 1,
		Column:     int(startPos.Column) + 1,
		EnumValues: values,
	})
}

func extractJavaFields(classBody *tree_sitter.Node, source []byte) []FieldDecl {
	var fields []FieldDecl

	for i := uint(0); i < classBody.ChildCount(); i++ {
		child := classBody.Child(i)
		if child.Kind() != "field_declaration" {
			continue
		}

		fieldType := ""
		fieldName := ""
		fieldDescription := ""
		fieldInitializer := ""
		annotations := make(map[string]string)
		isStatic := false
		isFinal := false

		for j := uint(0); j < child.ChildCount(); j++ {
			n := child.Child(j)
			switch n.Kind() {
			case "modifiers":
				modText := n.Utf8Text(source)
				if strings.Contains(modText, "static") {
					isStatic = true
				}
				if strings.Contains(modText, "final") {
					isFinal = true
				}
				for k := uint(0); k < n.ChildCount(); k++ {
					ann := n.Child(k)
					if ann.Kind() == "marker_annotation" || ann.Kind() == "annotation" {
						annName := ""
						annValue := ""
						for l := uint(0); l < ann.ChildCount(); l++ {
							ac := ann.Child(l)
							if ac.Kind() == "identifier" {
								annName = ac.Utf8Text(source)
							}
							if ac.Kind() == "annotation_argument_list" {
								annValue = ac.Utf8Text(source)
							}
						}
						if annName != "" {
							annotations[annName] = annValue
						}
					}
				}
			case "type_identifier", "generic_type", "array_type",
				"integral_type", "floating_point_type", "boolean_type", "void_type":
				fieldType = n.Utf8Text(source)
			case "variable_declarator":
				// v5 #9: Extract field name and initializer
				declText := n.Utf8Text(source)
				for k := uint(0); k < n.ChildCount(); k++ {
					vn := n.Child(k)
					if vn.Kind() == "identifier" {
						fieldName = vn.Utf8Text(source)
						break
					}
				}
				// Extract initializer if present (e.g., Status.ACTIVE)
				if eqIdx := strings.Index(declText, "="); eqIdx >= 0 {
					fieldInitializer = strings.TrimSpace(declText[eqIdx+1:])
				}
			}
		}

		if fieldName == "" || fieldType == "" {
			continue
		}

		if isStatic {
			continue
		}
		if isFinal && fieldName == strings.ToUpper(fieldName) && strings.Contains(fieldName, "_") {
			continue
		}

		// Extract @Schema description if present
		if schemaArgs, ok := annotations["Schema"]; ok && schemaArgs != "" {
			desc := extractSchemaDescription(schemaArgs)
			if desc != "" {
				fieldDescription = desc
			}
		}

		// Extract field-level JavaDoc from preceding comment sibling
		if fieldDescription == "" {
			fieldDescription = extractFieldJavaDoc(classBody, i, source)
		}

		fieldPos := child.StartPosition()
		fd := FieldDecl{
			Name:        fieldName,
			Type:        fieldType,
			Description: fieldDescription,
			Annotations: annotations,
			Line:        int(fieldPos.Row) + 1,
			Column:      int(fieldPos.Column) + 1,
		}

		if jp, ok := annotations["JsonProperty"]; ok && jp != "" {
			fd.JSONName = extractAnnotationStringValue(jp)
		}
		if fd.JSONName == "" {
			fd.JSONName = fieldName
		}

		_, hasNotNull := annotations["NotNull"]
		_, hasNonNull := annotations["NonNull"]
		_, hasNonnull := annotations["Nonnull"]
		_, hasNotBlank := annotations["NotBlank"]
		_, hasNotEmpty := annotations["NotEmpty"]
		fd.Required = hasNotNull || hasNonNull || hasNonnull || hasNotBlank || hasNotEmpty

		// v5 #1: @Nullable on fields → nullable schema property
		_, hasNullable := annotations["Nullable"]
		fd.Nullable = hasNullable

		// v5 #10: @Deprecated on fields
		_, hasDeprecated := annotations["Deprecated"]
		fd.Deprecated = hasDeprecated

		// v5 #9: Field initializer → default value
		if fieldInitializer != "" {
			// For enum-style defaults like Status.ACTIVE, extract just the constant name
			if strings.Contains(fieldInitializer, ".") {
				parts := strings.Split(fieldInitializer, ".")
				fd.DefaultValue = parts[len(parts)-1]
			} else {
				fd.DefaultValue = fieldInitializer
			}
		}

		fields = append(fields, fd)
	}

	return fields
}

// extractSchemaDescription extracts the description from @Schema annotation args.
func extractSchemaDescription(args string) string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	for _, part := range strings.Split(args, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "description") {
			if idx := strings.Index(part, "="); idx >= 0 {
				val := strings.TrimSpace(part[idx+1:])
				if len(val) >= 2 && strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") {
					return val[1 : len(val)-1]
				}
			}
		}
	}
	return ""
}

// extractFieldJavaDoc extracts JavaDoc from a comment preceding a field_declaration.
func extractFieldJavaDoc(classBody *tree_sitter.Node, fieldIdx uint, source []byte) string {
	child := classBody.Child(fieldIdx)
	if child == nil {
		return ""
	}

	prev := child.PrevSibling()
	if prev == nil {
		return ""
	}

	if prev.Kind() == "block_comment" || prev.Kind() == "line_comment" || prev.Kind() == "comment" {
		text := prev.Utf8Text(source)
		// Clean up comment markers
		text = strings.TrimPrefix(text, "/**")
		text = strings.TrimPrefix(text, "/*")
		text = strings.TrimPrefix(text, "//")
		text = strings.TrimSuffix(text, "*/")
		var lines []string
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "*")
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "@") {
				continue
			}
			lines = append(lines, line)
		}
		if len(lines) > 0 {
			return strings.Join(lines, " ")
		}
	}
	return ""
}

// extractClassJavaDoc extracts the JavaDoc description from a class-level block comment.
func extractClassJavaDoc(classNode *tree_sitter.Node, source []byte) string {
	prev := classNode.PrevSibling()
	if prev == nil {
		return ""
	}
	if prev.Kind() != "block_comment" && prev.Kind() != "comment" {
		return ""
	}
	text := prev.Utf8Text(source)
	text = strings.TrimPrefix(text, "/**")
	text = strings.TrimPrefix(text, "/*")
	text = strings.TrimSuffix(text, "*/")

	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "@") {
			continue
		}
		lines = append(lines, line)
	}
	if len(lines) > 0 {
		return strings.Join(lines, " ")
	}
	return ""
}

func extractAnnotationStringValue(argList string) string {
	// Simple extraction: ("value") or (value = "name")
	argList = strings.TrimPrefix(argList, "(")
	argList = strings.TrimSuffix(argList, ")")
	argList = strings.TrimSpace(argList)

	if strings.HasPrefix(argList, "\"") && strings.HasSuffix(argList, "\"") {
		return argList[1 : len(argList)-1]
	}

	// value = "name" pattern
	if idx := strings.Index(argList, "="); idx >= 0 {
		val := strings.TrimSpace(argList[idx+1:])
		if strings.HasPrefix(val, "\"") && strings.HasSuffix(val, "\"") {
			return val[1 : len(val)-1]
		}
	}

	return ""
}

func extractTypeParams(node *tree_sitter.Node, source []byte) []string {
	var params []string
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "type_parameter" {
			for j := uint(0); j < child.ChildCount(); j++ {
				n := child.Child(j)
				if n.Kind() == "identifier" {
					params = append(params, n.Utf8Text(source))
					break
				}
			}
		}
	}
	return params
}

// reJsonTypeInfoProperty matches property = "type" in @JsonTypeInfo args.
var reJsonTypeInfoProperty = regexp.MustCompile(`property\s*=\s*"([^"]+)"`)

// extractJsonTypeInfoProperty extracts the discriminator property from @JsonTypeInfo annotation.
func extractJsonTypeInfoProperty(args string) string {
	if m := reJsonTypeInfoProperty.FindStringSubmatch(args); len(m) > 1 {
		return m[1]
	}
	return ""
}

// reJsonSubType matches @Type(value = ClassName.class, name = "discriminatorValue") entries.
var reJsonSubType = regexp.MustCompile(`value\s*=\s*(\w+)\.class`)
var reJsonSubTypeName = regexp.MustCompile(`name\s*=\s*"([^"]+)"`)

// extractJsonSubTypesMapping extracts discriminator value → subtype name mapping.
func extractJsonSubTypesMapping(args string) map[string]string {
	args = strings.TrimPrefix(args, "(")
	args = strings.TrimSuffix(args, ")")
	args = strings.TrimSpace(args)
	args = strings.TrimPrefix(args, "{")
	args = strings.TrimSuffix(args, "}")

	mapping := make(map[string]string)

	// Split by @Type or @JsonSubTypes.Type
	chunks := strings.Split(args, "@Type")
	for _, chunk := range chunks {
		chunk = strings.TrimSpace(chunk)
		chunk = strings.TrimPrefix(chunk, ",")
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		valueMatch := reJsonSubType.FindStringSubmatch(chunk)
		nameMatch := reJsonSubTypeName.FindStringSubmatch(chunk)
		if len(valueMatch) > 1 {
			className := valueMatch[1]
			discValue := className // default: use class name as discriminator value
			if len(nameMatch) > 1 {
				discValue = nameMatch[1]
			}
			mapping[discValue] = className
		}
	}

	if len(mapping) == 0 {
		return nil
	}
	return mapping
}
