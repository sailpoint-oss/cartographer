package javaextract

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

// walkCompilationUnit extracts operations from top-level and nested types.
func walkCompilationUnit(root *tree_sitter.Node, source []byte, filePath string, ctx *extractContext) []*Operation {
	var ops []*Operation
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Kind() {
		case "class_declaration":
			ops = append(ops, walkClassDeclaration(child, source, filePath, ctx)...)
		case "interface_declaration":
			ops = append(ops, extractInterfaceOperations(child, source, filePath, ctx)...)
		}
	}
	return ops
}

func walkClassDeclaration(classNode *tree_sitter.Node, source []byte, filePath string, ctx *extractContext) []*Operation {
	className := extractClassName(classNode, source)
	indexClassMethods(classNode, source, className, ctx)

	var ops []*Operation
	ops = append(ops, extractClassOperations(classNode, source, filePath, ctx)...)

	var classBody *tree_sitter.Node
	for i := uint(0); i < classNode.ChildCount(); i++ {
		if classNode.Child(i).Kind() == "class_body" {
			classBody = classNode.Child(i)
			break
		}
	}
	if classBody == nil {
		return ops
	}
	for i := uint(0); i < classBody.ChildCount(); i++ {
		nested := classBody.Child(i)
		if nested.Kind() == "class_declaration" {
			ops = append(ops, walkClassDeclaration(nested, source, filePath, ctx)...)
		}
	}
	return ops
}

// extractInterfaceOperations emits operations from interface methods that
// carry HTTP mapping annotations (common Spring MVC interface pattern).
func extractInterfaceOperations(ifaceNode *tree_sitter.Node, source []byte, filePath string, ctx *extractContext) []*Operation {
	if !isControllerLike(ifaceNode, source) {
		return nil
	}
	ctx.controllerCount++
	className := extractClassName(ifaceNode, source)
	basePath := ""
	var classTags []string
	framework := ""

	for i := uint(0); i < ifaceNode.ChildCount(); i++ {
		child := ifaceNode.Child(i)
		if child.Kind() != "modifiers" {
			continue
		}
		for j := uint(0); j < child.ChildCount(); j++ {
			ann := child.Child(j)
			annName, annArgs := extractAnnotation(ann, source)
			switch annName {
			case "RequestMapping", "Path":
				basePath = extractMappingPathFromString(annArgs, ctx.constants)
				if annName == "Path" {
					framework = "jaxrs"
				} else {
					framework = "spring"
				}
			case "Tag", "Api":
				if tag := extractStringFromAnnotationArgs(annArgs, ctx.constants); tag != "" {
					classTags = append(classTags, tag)
				}
			}
			if annName == "RestController" || annName == "Controller" {
				if framework == "" {
					framework = "spring"
				}
			}
		}
	}
	if framework == "" {
		framework = "spring"
	}

	var ifaceBody *tree_sitter.Node
	for i := uint(0); i < ifaceNode.ChildCount(); i++ {
		if ifaceNode.Child(i).Kind() == "interface_body" {
			ifaceBody = ifaceNode.Child(i)
			break
		}
	}
	if ifaceBody == nil {
		return nil
	}

	var ops []*Operation
	for i := uint(0); i < ifaceBody.ChildCount(); i++ {
		method := ifaceBody.Child(i)
		if method.Kind() != "method_declaration" {
			continue
		}
		op := extractMethodOperation(method, source, basePath, framework, classTags, nil, ctx, className, "")
		if op != nil {
			op.File = filePath
			if className != "" && len(classTags) == 0 {
				classTags = []string{className}
				op.Tags = classTags
			}
			ops = append(ops, op)
		} else if methodDeclHasHTTPMapping(method, source, framework) {
			ctx.unmappedHandlerMethods++
		}
	}
	return ops
}

func isControllerLike(typeNode *tree_sitter.Node, source []byte) bool {
	for i := uint(0); i < typeNode.ChildCount(); i++ {
		child := typeNode.Child(i)
		if child.Kind() != "modifiers" {
			continue
		}
		for j := uint(0); j < child.ChildCount(); j++ {
			name, _ := extractAnnotation(child.Child(j), source)
			switch name {
			case "RestController", "Controller", "Path", "RequestMapping", "Tag", "Api":
				return true
			}
		}
	}
	return false
}
