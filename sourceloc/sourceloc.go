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

// EmitExtensions returns x-source-* extensions for embedding in OpenAPI.
// Only non-zero fields are included.
func (l Location) EmitExtensions() map[string]interface{} {
	ext := make(map[string]interface{})
	if l.File != "" {
		ext["x-source-file"] = l.File
	}
	if l.Line > 0 {
		ext["x-source-line"] = l.Line
	}
	if l.Column > 0 {
		ext["x-source-column"] = l.Column
	}
	return ext
}

// ApplyTo merges x-source-* extensions into the given map.
func (l Location) ApplyTo(m map[string]interface{}) {
	if l.File != "" {
		m["x-source-file"] = l.File
	}
	if l.Line > 0 {
		m["x-source-line"] = l.Line
	}
	if l.Column > 0 {
		m["x-source-column"] = l.Column
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
