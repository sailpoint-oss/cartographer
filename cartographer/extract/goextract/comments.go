// Copyright (c) 2020-2025. Sailpoint Technologies, Inc. All rights reserved.

package goextract

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"strings"
)

// CommentParser parses structured comments for OpenAPI annotations.
type CommentParser struct{}

// NewCommentParser creates a new CommentParser.
func NewCommentParser() *CommentParser {
	return &CommentParser{}
}

// ParseFuncComments extracts OpenAPI annotations from function comments.
// Also extracts regular godoc comments as descriptions when no explicit annotation is found.
// Supported annotations:
//
//	@openapi:id operationId
//	@openapi:summary Short summary
//	@openapi:description Detailed description
//	@openapi:tags Tag1,Tag2
//	@openapi:deprecated true
//	@openapi:experimental true
//	@openapi:private true
//
// OpenAPI 3.2 annotations:
//
//	@openapi:tag:summary Short tag description
//	@openapi:tag:parent ParentTagName
//	@openapi:tag:kind resource|action|collection
//	@openapi:streaming text/event-stream
//	@openapi:stream-item EventType
//	@openapi:querystring QueryParamsSchema
func (cp *CommentParser) ParseFuncComments(funcDecl *ast.FuncDecl, file *ast.File) map[string]string {
	annotations := make(map[string]string)

	if funcDecl.Doc == nil {
		return annotations
	}

	var godocLines []string
	exampleIdx := 0

	// Process each comment line
	for _, comment := range funcDecl.Doc.List {
		text := comment.Text

		// Remove comment markers
		text = strings.TrimPrefix(text, "//")
		text = strings.TrimPrefix(text, "/*")
		text = strings.TrimSuffix(text, "*/")
		text = strings.TrimSpace(text)

		// Check if it's an OpenAPI annotation
		if strings.HasPrefix(text, "@openapi:example") {
			raw := strings.TrimPrefix(text, "@openapi:example")
			raw = strings.TrimSpace(raw)
			annotations[fmt.Sprintf("example_%d", exampleIdx)] = raw
			exampleIdx++
		} else if strings.HasPrefix(text, "@openapi:") {
			cp.parseAnnotation(text, annotations)
		} else if text != "" && !strings.HasPrefix(text, "@") {
			// Regular godoc comment line (not an annotation)
			godocLines = append(godocLines, text)
		}
	}

	// If no explicit description annotation, use godoc comments
	if _, hasDesc := annotations["description"]; !hasDesc && len(godocLines) > 0 {
		annotations["godoc"] = strings.Join(godocLines, " ")
	}

	// If no explicit summary annotation and we have godoc, use first sentence as summary
	if _, hasSummary := annotations["summary"]; !hasSummary && len(godocLines) > 0 {
		firstLine := godocLines[0]
		// Use first sentence or entire first line if short
		if idx := strings.Index(firstLine, "."); idx > 0 && idx < 80 {
			annotations["godoc_summary"] = firstLine[:idx+1]
		} else if len(firstLine) < 80 {
			annotations["godoc_summary"] = firstLine
		}
	}

	return annotations
}

// parseAnnotation parses a single annotation line.
func (cp *CommentParser) parseAnnotation(line string, annotations map[string]string) {
	// Remove @openapi: prefix
	line = strings.TrimPrefix(line, "@openapi:")
	line = strings.TrimSpace(line)

	// Split on first space or colon
	parts := strings.SplitN(line, " ", 2)
	if len(parts) < 2 {
		parts = strings.SplitN(line, ":", 2)
	}

	if len(parts) == 2 {
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		annotations[key] = value
	}
}

// ApplyAnnotations applies parsed annotations to an OperationInfo.
// Supports both explicit @openapi: annotations and fallback to godoc comments.
func (cp *CommentParser) ApplyAnnotations(op *OperationInfo, annotations map[string]string) {
	cp.ApplyAnnotationsWithSource(op, annotations, "")
}

// ApplyAnnotationsWithSource applies parsed annotations to an OperationInfo and records provenance
// when sourceLocation is provided (file:line).
func (cp *CommentParser) ApplyAnnotationsWithSource(op *OperationInfo, annotations map[string]string, sourceLocation string) {
	if id, ok := annotations["id"]; ok && id != "" {
		op.ID = id
	}

	// Explicit summary takes precedence
	if summary, ok := annotations["summary"]; ok && summary != "" {
		op.Summary = summary
		if sourceLocation != "" {
			op.DocSources = append(op.DocSources, DocSource{
				SourceKind:     "OPENAPI_DIRECTIVE",
				Field:          "operation.summary",
				SourceLocation: sourceLocation,
				Confidence:     "high",
			})
		}
	} else if godocSummary, ok := annotations["godoc_summary"]; ok && godocSummary != "" {
		// Fall back to godoc-extracted summary
		op.Summary = godocSummary
		if sourceLocation != "" {
			op.DocSources = append(op.DocSources, DocSource{
				SourceKind:     "COMMENT",
				Field:          "operation.summary",
				SourceLocation: sourceLocation,
				Confidence:     "medium",
			})
		}
	}

	// Explicit description takes precedence
	if description, ok := annotations["description"]; ok && description != "" {
		op.Description = description
		if sourceLocation != "" {
			op.DocSources = append(op.DocSources, DocSource{
				SourceKind:     "OPENAPI_DIRECTIVE",
				Field:          "operation.description",
				SourceLocation: sourceLocation,
				Confidence:     "high",
			})
		}
	} else if godoc, ok := annotations["godoc"]; ok && godoc != "" {
		// Fall back to godoc comments as description
		op.Description = godoc
		if sourceLocation != "" {
			op.DocSources = append(op.DocSources, DocSource{
				SourceKind:     "COMMENT",
				Field:          "operation.description",
				SourceLocation: sourceLocation,
				Confidence:     "medium",
			})
		}
	}

	if tags, ok := annotations["tags"]; ok && tags != "" {
		// Split comma-separated tags
		tagList := strings.Split(tags, ",")
		for i, tag := range tagList {
			tagList[i] = strings.TrimSpace(tag)
		}
		op.Tags = tagList
	}

	if deprecated, ok := annotations["deprecated"]; ok {
		op.Deprecated = cp.parseBool(deprecated)
	}

	if experimental, ok := annotations["experimental"]; ok {
		op.Experimental = cp.parseBool(experimental)
	}

	if private, ok := annotations["private"]; ok {
		op.Private = cp.parseBool(private)
	}

	// OpenAPI 3.2 streaming annotations
	if streaming, ok := annotations["streaming"]; ok && streaming != "" {
		op.IsStreaming = true
		op.StreamMediaType = streaming
	}

	if streamItem, ok := annotations["stream-item"]; ok && streamItem != "" {
		op.StreamItemType = streamItem
	}

	// OpenAPI 3.2 querystring annotation
	if querystring, ok := annotations["querystring"]; ok && querystring != "" {
		op.QueryStringSchema = querystring
	}

	// Parse @openapi:example directives (stored as example_0, example_1, ...)
	for i := 0; ; i++ {
		raw, ok := annotations[fmt.Sprintf("example_%d", i)]
		if !ok {
			break
		}
		ex := parseExampleDirective(raw)
		op.Examples = append(op.Examples, ex)
	}
}

// GenerateSummaryFromFuncName generates a human-readable summary from a function name.
// Example: "CreateTenant" -> "Create Tenant", "GetUserByID" -> "Get User By ID"
func (cp *CommentParser) GenerateSummaryFromFuncName(funcName string) string {
	if funcName == "" {
		return ""
	}

	runes := []rune(funcName)
	var result []rune

	for i, r := range runes {
		// Insert space before uppercase letters (except at start)
		if i > 0 && r >= 'A' && r <= 'Z' {
			// Don't add space if previous char was also uppercase (handles acronyms like "ID")
			// But do add space if next char is lowercase (handles "IDName" -> "ID Name")
			prevUpper := runes[i-1] >= 'A' && runes[i-1] <= 'Z'
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'

			if !prevUpper || nextLower {
				result = append(result, ' ')
			}
		}
		result = append(result, r)
	}

	return string(result)
}

// ApplyFallbackSummary applies a generated summary if none exists.
func (cp *CommentParser) ApplyFallbackSummary(op *OperationInfo, funcName string) {
	if op.Summary == "" && funcName != "" {
		op.Summary = cp.GenerateSummaryFromFuncName(funcName)
	}
}

// ParseTagAnnotations extracts tag-specific annotations for OpenAPI 3.2.
// Returns a TagInfo if tag annotations are found, nil otherwise.
func (cp *CommentParser) ParseTagAnnotations(annotations map[string]string, tagName string) *TagInfo {
	tag := &TagInfo{
		Name: tagName,
	}

	hasTagAnnotations := false

	if summary, ok := annotations["tag:summary"]; ok && summary != "" {
		tag.Summary = summary
		hasTagAnnotations = true
	}

	if parent, ok := annotations["tag:parent"]; ok && parent != "" {
		tag.Parent = parent
		hasTagAnnotations = true
	}

	if kind, ok := annotations["tag:kind"]; ok && kind != "" {
		tag.Kind = kind
		hasTagAnnotations = true
	}

	if description, ok := annotations["tag:description"]; ok && description != "" {
		tag.Description = description
		hasTagAnnotations = true
	}

	if !hasTagAnnotations {
		return nil
	}

	return tag
}

// parseBool parses a boolean value from a string.
func (cp *CommentParser) parseBool(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "true" || value == "yes" || value == "1"
}

// ExtractTagsFromComments extracts conventional comment patterns for tags.
// Examples:
//   - "Package api provides..." -> might indicate a tag
//   - Function names might provide hints
func (cp *CommentParser) ExtractTagsFromComments(comments []*ast.Comment) []string {
	tags := make([]string, 0)

	for _, comment := range comments {
		text := comment.Text
		text = strings.TrimPrefix(text, "//")
		text = strings.TrimPrefix(text, "/*")
		text = strings.TrimSuffix(text, "*/")
		text = strings.TrimSpace(text)

		// Look for swagger-style tags
		// swagger:route GET /path tag
		if strings.HasPrefix(text, "swagger:route") {
			parts := strings.Fields(text)
			if len(parts) >= 4 {
				tags = append(tags, parts[3])
			}
		}
	}

	return tags
}

// parseExampleDirective parses a raw @openapi:example directive value.
// Format: "<summary>: <json-value>" or just "<json-value-or-string>"
func parseExampleDirective(raw string) ExampleInfo {
	ex := ExampleInfo{}

	// Try "summary: json-value" format
	if idx := strings.Index(raw, ":"); idx > 0 {
		candidate := strings.TrimSpace(raw[:idx])
		rest := strings.TrimSpace(raw[idx+1:])
		// If the part before the colon looks like a summary (no braces/brackets)
		// and the rest parses as JSON, treat it as summary: value
		if rest != "" && !strings.ContainsAny(candidate, "{}[]\"") {
			var parsed interface{}
			if err := json.Unmarshal([]byte(rest), &parsed); err == nil {
				ex.Summary = candidate
				ex.Value = parsed
				return ex
			}
		}
	}

	// Try the whole thing as JSON
	var parsed interface{}
	if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
		ex.Value = parsed
		return ex
	}

	// Fall back to treating it as a plain string value
	if raw != "" {
		ex.Value = raw
	}
	return ex
}
