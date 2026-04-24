package sourcemap

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type PathLineRange struct {
	Path      string
	StartLine int
	EndLine   int
}

func BuildPathLineMap(specPath string) ([]PathLineRange, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, sourceLookupError(specPath, err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, sourceLookupError(specPath, err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil
	}

	var pathsNode *yaml.Node
	var pathsIdx int
	for i := 0; i < len(root.Content)-1; i += 2 {
		if root.Content[i].Value == "paths" {
			pathsNode = root.Content[i+1]
			pathsIdx = i
			break
		}
	}
	if pathsNode == nil || pathsNode.Kind != yaml.MappingNode {
		return nil, nil
	}

	totalLines := countLines(data)
	pathsEndLine := findEndOfMapping(root, pathsIdx, totalLines)

	out := make([]PathLineRange, 0, len(pathsNode.Content)/2)
	for i := 0; i < len(pathsNode.Content)-1; i += 2 {
		keyNode := pathsNode.Content[i]
		start := keyNode.Line
		end := pathsEndLine
		if i+2 < len(pathsNode.Content) {
			end = pathsNode.Content[i+2].Line - 1
		}
		out = append(out, PathLineRange{
			Path:      keyNode.Value,
			StartLine: start,
			EndLine:   end,
		})
	}
	return out, nil
}

func FindPathForLine(ranges []PathLineRange, line int) string {
	for _, r := range ranges {
		if line >= r.StartLine && line <= r.EndLine {
			return r.Path
		}
	}
	return ""
}

func countLines(data []byte) int {
	n := 1
	for _, b := range data {
		if b == '\n' {
			n++
		}
	}
	return n
}

func findEndOfMapping(root *yaml.Node, mapKeyIdx, totalLines int) int {
	nextKeyIdx := mapKeyIdx + 2
	if nextKeyIdx < len(root.Content) {
		return root.Content[nextKeyIdx].Line - 1
	}
	return totalLines
}

func LoadFromSpecFile(specPath string) (*SourceMap, error) {
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, sourceLookupError(specPath, err)
	}
	var spec map[string]any
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse %s: %w", specPath, err)
	}
	return BuildFromSpec(spec), nil
}
