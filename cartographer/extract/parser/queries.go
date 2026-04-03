package parser

import "embed"

// Embedded tree-sitter query files for each language/framework.
//
//go:embed queries
var QueriesFS embed.FS

// LoadQuery reads an embedded query file (e.g., "queries/java/spring-controllers.scm").
func LoadQuery(path string) (string, error) {
	data, err := QueriesFS.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
