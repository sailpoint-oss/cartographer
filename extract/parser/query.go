package parser

import (
	"fmt"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// Capture represents a query capture (node + capture name + text).
type Capture struct {
	Name string            // capture name from the query (e.g. "class_name")
	Node *tree_sitter.Node // AST node
	Text string            // source text of the captured node
}

// Match is a group of captures from a single pattern match.
type Match struct {
	PatternIndex uint16
	Captures     []Capture
}

// Query compiles and runs a tree-sitter S-expression query against the given
// tree, returning every match grouped by pattern.
func (p *Pool) Query(lang string, tree *tree_sitter.Tree, source []byte, queryStr string) ([]Match, error) {
	p.mu.Lock()
	language, ok := p.languages[lang]
	p.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("language %q not registered", lang)
	}

	q, qErr := tree_sitter.NewQuery(language, queryStr)
	if qErr != nil {
		return nil, fmt.Errorf("compile query: %s", qErr.Error())
	}
	defer q.Close()

	captureNames := q.CaptureNames()

	cursor := tree_sitter.NewQueryCursor()
	defer cursor.Close()

	qm := cursor.Matches(q, tree.RootNode(), source)

	var matches []Match
	for {
		m := qm.Next()
		if m == nil {
			break
		}
		if len(m.Captures) == 0 {
			continue
		}

		match := Match{PatternIndex: uint16(m.PatternIndex)}
		for _, c := range m.Captures {
			nodeCopy := c.Node
			match.Captures = append(match.Captures, Capture{
				Name: captureNames[c.Index],
				Node: &nodeCopy,
				Text: nodeCopy.Utf8Text(source),
			})
		}
		matches = append(matches, match)
	}
	return matches, nil
}

// QueryCaptures runs a query and returns a flat list of all captures.
// Useful when you just need capture names + text without match grouping.
func (p *Pool) QueryCaptures(lang string, tree *tree_sitter.Tree, source []byte, queryStr string) ([]Capture, error) {
	matches, err := p.Query(lang, tree, source, queryStr)
	if err != nil {
		return nil, err
	}
	var all []Capture
	for _, m := range matches {
		all = append(all, m.Captures...)
	}
	return all, nil
}
