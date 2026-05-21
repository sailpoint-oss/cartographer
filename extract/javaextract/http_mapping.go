package javaextract

import tree_sitter "github.com/tree-sitter/go-tree-sitter"

func methodDeclHasHTTPMapping(methodNode *tree_sitter.Node, source []byte, framework string) bool {
	for i := uint(0); i < methodNode.ChildCount(); i++ {
		child := methodNode.Child(i)
		if child.Kind() != "modifiers" {
			continue
		}
		for j := uint(0); j < child.ChildCount(); j++ {
			name, _ := extractAnnotation(child.Child(j), source)
			if isHTTPMappingAnnotation(name, framework) {
				return true
			}
		}
	}
	return false
}

func isHTTPMappingAnnotation(name, framework string) bool {
	switch framework {
	case "spring":
		switch name {
		case "GetMapping", "PostMapping", "PutMapping", "DeleteMapping", "PatchMapping", "RequestMapping":
			return true
		}
	case "jaxrs":
		switch name {
		case "GET", "POST", "PUT", "DELETE", "PATCH", "Path", "HttpMethod":
			return true
		}
	}
	return false
}
