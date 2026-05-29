// Package sourceloc provides shared source location tracking for extraction.
package sourceloc

import (
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Location represents a source code position.
type Location struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`   // 1-based
	Column int    `json:"column,omitempty"` // 1-based
}

const ExtensionKey = "x-source"

// EmitExtensions returns the structured x-source extension for embedding in
// OpenAPI. Only non-zero fields are included.
func (l Location) EmitExtensions() map[string]interface{} {
	source := l.SourceObject()
	if len(source) == 0 {
		return map[string]interface{}{}
	}
	return map[string]interface{}{ExtensionKey: source}
}

// SourceObject returns the object value stored under x-source.
func (l Location) SourceObject() map[string]interface{} {
	ext := make(map[string]interface{})
	if l.File != "" {
		ext["file"] = l.File
	}
	if l.Line > 0 {
		ext["line"] = l.Line
	}
	if l.Column > 0 {
		ext["column"] = l.Column
	}
	return ext
}

// ApplyTo merges the structured x-source extension into the given map.
func (l Location) ApplyTo(m map[string]interface{}) {
	source := l.SourceObject()
	if len(source) > 0 {
		m[ExtensionKey] = source
	}
}

// IsZero returns true if the location has no data.
func (l Location) IsZero() bool {
	return l.File == "" && l.Line == 0 && l.Column == 0
}

// FromTreeSitter creates a Location from a tree-sitter node's start position.
func FromTreeSitter(file string, node *tree_sitter.Node) Location {
	if node == nil {
		return Location{File: file}
	}
	start := node.StartPosition()
	return Location{
		File:   file,
		Line:   int(start.Row) + 1,    // tree-sitter is 0-based
		Column: int(start.Column) + 1, // tree-sitter is 0-based
	}
}
